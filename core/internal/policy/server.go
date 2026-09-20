package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
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
	// InjectEnabled 是 **AI 欺骗内容注入**的下发级开关（`ADR-0023` 决定 4）。
	// 恒出现（不是指针、无 omitempty）：适配器必须能区分「明确关闭」与「这份载荷没谈这件事」。
	// 它**不**影响 inject_rules（静态规则有自己的显式关闭手段：空数组）。
	InjectEnabled bool `json:"inject_enabled"`
	// ContentManifest 是内容清单的**投影**（含内容体）。nil = 没有内容。
	ContentManifest *edgeContentManifest `json:"content_manifest,omitempty"`
}

// edgeContentManifest 是下发给适配器的内容清单（字段与生成侧清单对齐，见 docs/spec/ai-contract.md §3）。
type edgeContentManifest struct {
	Version  uint64             `json:"version"`
	Selector string             `json:"selector"`
	Variants int                `json:"variants"`
	Entries  []edgeContentEntry `json:"entries"`
}

type edgeContentEntry struct {
	Resource  string            `json:"resource"`
	ProfileID string            `json:"profile_id"`
	Bodies    []edgeContentBody `json:"bodies"`
}

type edgeContentBody struct {
	VariantID int    `json:"variant_id"`
	ContentID string `json:"content_id"`
	Checksum  string `json:"checksum"`
	Body      string `json:"body"`
	Marker    string `json:"marker,omitempty"`
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
	// content 是 AI 内容的投影输入；nil = 本实例未装配内容（inject_enabled=false，无清单）。
	content *ContentSource
}

// ContentSource 是策略面做 AI 内容投影所需的全部输入（`ADR-0023`）。
//
// 三者必须一起给：开关决定「发不发」，清单决定「发什么」，内容库决定「内容体从哪取」
// （`store` 是核心唯一的 I/O 出口，`MD-20`）。
type ContentSource struct {
	// Enabled = 配置 `ai.enabled`（投影成载荷里的 inject_enabled）。
	Enabled bool
	// Manifest 是已装载的清单（Entries 为空 = 没有内容）。
	Manifest contract.ContentManifest
	// Store 是内容库（装载期 Put、投影期 Get）。
	Store store.ContentStore
}

// WithContent 装配 AI 内容的投影输入。未调用时策略载荷恒为
// `inject_enabled=false` 且无 `content_manifest`（= 今天的行为，逐字节一致）。
func (s *Server) WithContent(src ContentSource) *Server {
	s.content = &src
	return s
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

	// AI 欺骗内容（`ADR-0023` / `AR-33`）：开关 + 内容清单。
	//
	// 内容体**从内容库读**（不是从装载时的结构里直接拿）—— 内容库是内容的唯一存放处，
	// 本函数只是它的投影；缺内容（读过期了 / 没写进去）就跳过该条并 warn，不编造、不阻断。
	if src := s.content; src != nil {
		doc.InjectEnabled = src.Enabled
		if doc.ContentManifest, err = s.projectContent(ctx, src); err != nil {
			return nil, err
		}
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("序列化边缘策略文档失败：%w", err)
	}
	return raw, nil
}

// projectContent 把内容清单投影成边缘文档里的 `content_manifest`。
//
// 没有可用条目时返回 nil（= 载荷不带该字段，适配器一律报 `no_content`）—— 空对象与缺字段
// 在适配器侧语义相同，但缺字段更小（少一段空 JSON）。
func (s *Server) projectContent(ctx context.Context, src *ContentSource) (*edgeContentManifest, error) {
	if len(src.Manifest.Entries) == 0 {
		return nil, nil
	}
	out := &edgeContentManifest{
		Version:  src.Manifest.Version,
		Selector: src.Manifest.Selector,
		Variants: src.Manifest.Variants,
		Entries:  make([]edgeContentEntry, 0, len(src.Manifest.Entries)),
	}
	missing := 0
	for _, entry := range src.Manifest.Entries {
		bodies := make([]edgeContentBody, 0, len(entry.Bodies))
		for _, body := range entry.Bodies {
			key := contract.ContentKey(entry.ProfileID, entry.Resource, body.VariantID, src.Manifest.Version)
			raw, ok, err := src.Store.Get(ctx, key)
			if err != nil {
				return nil, fmt.Errorf("policy: 读内容库失败（%s）：%w", key, err)
			}
			if !ok {
				missing++
				continue
			}
			bodies = append(bodies, edgeContentBody{
				VariantID: body.VariantID,
				ContentID: body.ContentID,
				Checksum:  body.Checksum,
				Body:      string(raw),
				Marker:    body.Marker,
			})
		}
		if len(bodies) == 0 {
			continue
		}
		out.Entries = append(out.Entries, edgeContentEntry{
			Resource:  entry.Resource,
			ProfileID: entry.ProfileID,
			Bodies:    bodies,
		})
	}
	if missing > 0 {
		log.Printf("policy: 内容库缺 %d 条内容，已从下发清单里跳过（不影响业务与判定）", missing)
	}
	if len(out.Entries) == 0 {
		return nil, nil
	}
	return out, nil
}
