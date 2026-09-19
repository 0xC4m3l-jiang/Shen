package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	policyv1 "shen/api/policy/v1"
	"shen/core/internal/contract"
	"shen/core/internal/store"
)

// EdgePayloadSchemaVersion 是下发给 L1 适配器的策略文档版本号（`docs/spec/policy-payload.md`）。
//
// 适配器按它判断自己能不能读懂；读不懂**必须**回执 `applied=false` 而不是猜着应用 ——
// 猜错的代价是「按过期的后端表把真实用户送去幻境」。
const EdgePayloadSchemaVersion = 1

// edgeDoc 是下发给适配器的策略文档（契约见 `docs/spec/policy-payload.md`）。
//
// 它是**投影**：只带适配器真正消费的字段。整份配置（含规则、阈值、诱饵资产）留在核心，
// 不下发 —— 下发用不到的数据等于给边缘多一份会过期的复制。
type edgeDoc struct {
	SchemaVersion int           `json:"schema_version"`
	PolicyID      string        `json:"policy_id"`
	Version       uint64        `json:"version"`
	Backends      []edgeBackend `json:"backends"`
	Whitelist     edgeWhitelist `json:"whitelist"`
	// InjectRules 是响应改写规则。**指针**是刻意的：
	//   - nil（未配置 injects 段）→ omitempty 省略字段，适配器继续用它自己的本地规则；
	//   - 非 nil（可以是空数组）→ 显式下发，空数组 = 「没有规则」。
	// 否则「没配」与「配了空」无法区分，运营就没有关闭注入的手段。
	InjectRules *[]edgeInjectRule `json:"inject_rules,omitempty"`
}

// edgeInjectRule 是一条响应改写规则（字段与适配器侧 `edge.proxy.policyInjectRule` 手工对齐）。
type edgeInjectRule struct {
	Kind    string `json:"kind,omitempty"`
	Snippet string `json:"snippet"`
	Marker  string `json:"marker,omitempty"`
}

// edgeBackend 是幻境后端池的一个条目（逻辑名 → 可拨号地址）。
type edgeBackend struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Enabled bool   `json:"enabled"`
}

// edgeWhitelist 只带 CIDR：适配器用它在**改道判定之前**放行内部来源（`INT-25`）。
//
// UA / 路径前缀两类白名单由核心内的 `director` 消费（`contract.Whitelist`），不进本投影。
type edgeWhitelist struct {
	SourceCIDRs []string `json:"source_cidrs"`
}

// Server 实现 `api/policy/v1` 的策略面（接缝 S4）。
//
// 只做三件事：把当前策略版本**投影**成边缘文档下发（`Pull`）、记录适配器回执（`Ack`）、
// 明确拒绝对未实现的 `Watch`（而不是静默挂住调用方）。
//
// 它**不做判定、不做决策、不写业务存储** —— 只经 `store.PolicyStore` 落回执（`MD-20`）。
type Server struct {
	policyv1.UnimplementedDeceptionPolicyServer

	loader *Loader
	store  store.PolicyStore
	now    func() time.Time
}

var _ policyv1.DeceptionPolicyServer = (*Server)(nil)

// NewServer 构造策略面服务端。ps 是回执台账（`store` 是核心唯一的 I/O 出口，`MD-20`）。
func NewServer(l *Loader, ps store.PolicyStore) *Server {
	if l == nil || ps == nil {
		panic("policy: NewServer 需要 loader 与 PolicyStore")
	}
	return &Server{loader: l, store: ps, now: time.Now}
}

// Pull 返回当前策略版本对边缘的投影。
//
// `checksum` 覆盖**下发的那串字节**（不是配置文件）—— 适配器据此先验完整性再应用（`ST-8`）。
func (s *Server) Pull(ctx context.Context, req *policyv1.PolicyPullRequest) (*policyv1.PolicySnapshot, error) {
	snap, err := s.loader.Snapshot(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "policy: 取快照失败：%v", err)
	}
	// 请求指定了别的策略集 → 明确拒绝。回一份「别的策略」比报错危险得多。
	if want := req.GetPolicyId(); want != "" && want != snap.PolicyID {
		return nil, status.Errorf(codes.NotFound, "policy: 本实例只有策略集 %q（请求的是 %q）", snap.PolicyID, want)
	}

	payload, err := s.edgePayload(ctx, snap)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "policy: 组装边缘策略失败：%v", err)
	}
	sum := sha256.Sum256(payload)

	return &policyv1.PolicySnapshot{
		PolicyId: snap.PolicyID,
		Version:  snap.Version,
		Payload:  payload,
		Checksum: hex.EncodeToString(sum[:]),
		GrayPct:  uint32(snap.GrayPct),
	}, nil
}

// Watch 未实现（[ADR-0018](../../../docs/background/decisions/0018-policy-plane-pull-model.md)：
// 本轮用 `Pull` 轮询，`Watch` 等「亚秒级生效」真成为需求再加）。
//
// 返回 `Unimplemented` 而不是空流：让调用方**立刻**知道这条路没通，而不是挂在那里等。
func (s *Server) Watch(*policyv1.PolicyWatchRequest, grpc.ServerStreamingServer[policyv1.PolicyDelta]) error {
	return status.Error(codes.Unimplemented,
		"策略面 Watch 未实现：本轮用 Pull 轮询（见 ADR-0018）")
}

// Ack 记录一次适配器回执（`AR-13`：各层必须回执以便版本对账）。
//
// 回执是可重放的幂等事实（同一适配器+版本只留最新一条），因此重复上报不报错。
func (s *Server) Ack(ctx context.Context, in *policyv1.PolicyAck) (*policyv1.PolicyAckReply, error) {
	a := contract.PolicyAck{
		PolicyID:   in.GetPolicyId(),
		Version:    in.GetVersion(),
		AdapterID:  in.GetAdapterId(),
		Applied:    in.GetApplied(),
		Reason:     in.GetReason(),
		ReceivedAt: s.now(),
	}
	if err := s.store.RecordAck(ctx, a); err != nil {
		// 缺适配器标识属调用方的问题（无法对账），如实回 InvalidArgument。
		return nil, status.Errorf(codes.InvalidArgument, "policy: 回执无法入账：%v", err)
	}
	return &policyv1.PolicyAckReply{Ok: true}, nil
}

// edgePayload 把当前策略投影成边缘文档（JSON）。
//
// 确定性：字段序固定（struct 序）+ 后端按名排序 ⇒ 同内容必得同一 checksum（`AR-30` 精神：
// 下发的字节要可复现，否则适配器每次拉取都会以为策略变了）。
func (s *Server) edgePayload(ctx context.Context, snap contract.PolicySnapshot) ([]byte, error) {
	backends, err := s.loader.Honeypots(ctx)
	if err != nil {
		return nil, err
	}
	wl, err := s.loader.Whitelist(ctx)
	if err != nil {
		return nil, err
	}

	doc := edgeDoc{
		SchemaVersion: EdgePayloadSchemaVersion,
		PolicyID:      snap.PolicyID,
		Version:       snap.Version,
		Backends:      make([]edgeBackend, 0, len(backends)),
		Whitelist:     edgeWhitelist{SourceCIDRs: make([]string, 0, len(wl.SourceCIDRs))},
	}
	for _, b := range backends {
		doc.Backends = append(doc.Backends, edgeBackend{Name: b.Name, Address: b.Addr, Enabled: b.Enabled})
	}
	sort.Slice(doc.Backends, func(i, j int) bool { return doc.Backends[i].Name < doc.Backends[j].Name })
	for _, p := range wl.SourceCIDRs {
		doc.Whitelist.SourceCIDRs = append(doc.Whitelist.SourceCIDRs, p.String())
	}
	sort.Strings(doc.Whitelist.SourceCIDRs)

	// 注入规则：**不排序**（执行顺序是配置的一部分），但保持「未配置」与「显式空」的区别。
	if rules, provided, err := s.loader.Injects(ctx); err != nil {
		return nil, err
	} else if provided {
		out := make([]edgeInjectRule, 0, len(rules))
		for _, r := range rules {
			out = append(out, edgeInjectRule{Kind: r.Kind, Snippet: r.Snippet, Marker: r.Marker})
		}
		doc.InjectRules = &out
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("序列化边缘策略文档失败：%w", err)
	}
	return raw, nil
}
