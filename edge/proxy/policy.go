package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/netip"
	"strings"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	policyv1 "shen/api/policy/v1"
	"shen/edge/injection"
)

// PolicyClient 是本端对**策略面**（`api/policy/v1`，接缝 S4）的依赖。
//
// 接口由**消费方（本模块）**定义，单测用替身（`MD-22`）；生产实现是生成的
// `policyv1.DeceptionPolicyClient`。
type PolicyClient interface {
	Pull(ctx context.Context, in *policyv1.PolicyPullRequest, opts ...grpc.CallOption) (*policyv1.PolicySnapshot, error)
	Ack(ctx context.Context, in *policyv1.PolicyAck, opts ...grpc.CallOption) (*policyv1.PolicyAckReply, error)
}

// EdgePolicySchemaVersion 是本端**能读懂**的载荷版本（契约：`docs/spec/policy-payload.md`）。
//
// 读到更高的版本**必须**回执 `applied=false` 而不是猜着用 —— 猜错的代价是
// 「按自己看不懂的后端表把真实用户送去幻境」。
const EdgePolicySchemaVersion = 1

// edgePolicy 是策略面下发的边缘文档。字段与核心侧 `policy.edgeDoc` **手工保持一致**：
// 两端分属不同平面（`TB-24` 只允许经 wire format 通信），因此没有共享的 Go 类型，
// 改动其一**必须**同时改另一处与 `docs/spec/policy-payload.md`。
type edgePolicy struct {
	SchemaVersion int             `json:"schema_version"`
	PolicyID      string          `json:"policy_id"`
	Version       uint64          `json:"version"`
	Backends      []policyBackend `json:"backends"`
	Whitelist     policyWhitelist `json:"whitelist"`
	// InjectRules 是响应改写规则。**指针**是刻意的（与核心侧 `policy.edgeDoc` 对齐）：
	//   - nil（字段缺省）→ 继续用本地 env 的规则；
	//   - 非 nil（可以是空数组）→ 用远端的：空数组 = 明确「没有规则」，运营能据此主动关掉注入。
	InjectRules *[]policyInjectRule `json:"inject_rules"`
	// InjectEnabled 是 **AI 欺骗内容注入**的下发级开关（`ADR-0023` 决定 4）。
	// 恒出现（不是指针）：适配器必须能区分「明确关闭」与「这份载荷没谈这件事」。
	InjectEnabled bool `json:"inject_enabled"`
	// ContentManifest 是内容清单（含内容体）。nil = 没有内容 ⇒ 一律报 `no_content`。
	ContentManifest *contentManifest `json:"content_manifest"`
}

// policyInjectRule 是一条响应改写规则（与核心侧手工对齐，见 docs/spec/policy-payload.md）。
type policyInjectRule struct {
	Kind    string `json:"kind,omitempty"`
	Snippet string `json:"snippet"`
	Marker  string `json:"marker,omitempty"`
}

type policyBackend struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Enabled bool   `json:"enabled"`
}

type policyWhitelist struct {
	SourceCIDRs []string `json:"source_cidrs"`
}

// remoteState 是「策略面现在给了我什么」的不可变快照。
//
// 请求路径只做一次 atomic 读；应用新策略时整块替换 —— 这样就不会出现
// 「后端表换了一半、白名单还是旧的」这种中间状态。
type remoteState struct {
	version  uint64
	checksum string
	backends map[string]caddyhttp.MiddlewareHandler // 逻辑名 -> 已构造好的改道后端
	cidrs    []netip.Prefix
	// injectRulesProvided 为真 = 策略面**显式**下发了 `inject_rules` 段（可能是空数组）。
	// 它决定「未下发」与「下发空」的区别（见 applyEdgePolicy）。
	injectRulesProvided bool
	// injector 是远端规则构造出的注入器；远端规则为空时为 nil（= 不注入）。
	injector Injector
	// injectEnabled 是远端下发的 AI 内容注入开关（`inject_enabled`）。
	injectEnabled bool
	// content 是已索引的远端内容清单；无可用内容时为 nil（= 报 `no_content`）。
	content *contentIndex
}

// remotePolicy 返回当前生效的远端策略；未应用过时为 nil。
func (h *Handler) remotePolicy() *remoteState { return h.remote.Load() }

// applyEdgePolicy 应用一份策略载荷。
//
// 校验顺序（任一不通过都**不应用**，调用方据此回执 `applied=false`）：
//  1. JSON 能解析；2. `schema_version` 本端读得懂；3. 载荷非空且版本与 checksum 自洽；
//  4. 后端地址是合法 URL —— 一个坏后端**不能**把整张表带下水，只在表里去掉它并记账。
//
// 合并语义（`ADR-0018`）：
//   - **后端表**：按逻辑名覆盖 —— 远端定义的同名项覆盖本地，本地独有的项保留（兜底）；
//   - **白名单**：**并集** —— 本地项永远保留。白名单是防误伤的护栏（`INT-25`），
//     缩小它会把运维探针判成攻击者，因此远端只能增加，不能删。
func (h *Handler) applyEdgePolicy(ctx context.Context, raw []byte) error {
	var doc edgePolicy
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("载荷不是合法 JSON：%w", err)
	}
	if doc.SchemaVersion != EdgePolicySchemaVersion {
		return fmt.Errorf("载荷 schema_version=%d，本端只支持 %d（升级适配器后再应用）",
			doc.SchemaVersion, EdgePolicySchemaVersion)
	}

	backends := map[string]caddyhttp.MiddlewareHandler{}
	var dropped []string
	for _, b := range doc.Backends {
		if !b.Enabled {
			continue
		}
		if _, _, err := upstreamAddr(b.Address); err != nil {
			// 坏地址只丢这一条：整表作废会让所有改道一起失效（更糟）。
			dropped = append(dropped, fmt.Sprintf("%s(%v)", b.Name, err))
			continue
		}
		if h.buildRemote == nil {
			return fmt.Errorf("未装配远端后端构造器（buildRemote）")
		}
		rp, err := h.buildRemote(b.Name, b.Address)
		if err != nil {
			dropped = append(dropped, fmt.Sprintf("%s(%v)", b.Name, err))
			continue
		}
		backends[b.Name] = rp
	}

	cidrs := make([]netip.Prefix, 0, len(doc.Whitelist.SourceCIDRs))
	for _, s := range doc.Whitelist.SourceCIDRs {
		p, err := netip.ParsePrefix(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("白名单网段 %q 非法：%w", s, err)
		}
		cidrs = append(cidrs, p)
	}

	// 注入规则：字段**缺省** = 不动本地规则；**显式空数组** = 明确无规则。
	var inj Injector
	if doc.InjectRules != nil {
		rules := make([]injection.Rule, 0, len(*doc.InjectRules))
		for _, r := range *doc.InjectRules {
			rules = append(rules, injection.Rule{Kind: r.Kind, Snippet: r.Snippet, Marker: r.Marker})
		}
		if len(rules) > 0 {
			built, err := injection.New(rules)
			if err != nil {
				// 规则非法（例如片段为空）→ 整份策略不应用：宁可继续用旧规则，不可半应用。
				return fmt.Errorf("注入规则非法：%w", err)
			}
			inj = built
		}
	}

	h.remote.Store(&remoteState{
		version:             doc.Version,
		checksum:            "",
		backends:            backends,
		cidrs:               cidrs,
		injectRulesProvided: doc.InjectRules != nil,
		injector:            inj,
		injectEnabled:       doc.InjectEnabled,
		content:             newContentIndex(doc.ContentManifest),
	})
	if len(dropped) > 0 {
		log.Printf("proxy: 策略 v%d 中有 %d 个后端地址不可用，已跳过：%s",
			doc.Version, len(dropped), strings.Join(dropped, ", "))
	}
	_ = ctx
	return nil
}

// runPolicyPolling 定期拉取策略：启动拉一次，之后每 interval 一次（`ADR-0018`）。
//
// 任何失败都**只影响策略**，绝不影响请求路径（`NI-1`）：
// 沿用上一次成功应用的版本，并把失败原因限速成「状态变化时才打一条」。
func (h *Handler) runPolicyPolling(ctx context.Context, interval time.Duration) {
	if h.policy == nil || interval <= 0 {
		return
	}
	adapterID := h.AdapterID
	if adapterID == "" {
		adapterID = "proxy"
	}
	lastErr := ""
	lastRejected := ""
	pull := func() {
		pullCtx, cancel := context.WithTimeout(ctx, policyTimeout)
		defer cancel()

		snap, err := h.policy.Pull(pullCtx, &policyv1.PolicyPullRequest{PolicyId: h.PolicyID})
		if err != nil {
			if msg := err.Error(); msg != lastErr {
				lastErr = msg
				// 限速：同样的失败只报一次，避免每 60s 刷一屏「策略面不可达」。
				log.Printf("proxy: 拉取策略失败（继续用当前策略）：%v", err)
			}
			return
		}
		lastErr = ""

		// 校验和先验：字节被改过就不应用（ST-8）。核心与适配器之间没有 TLS 时，这是唯一的完整性迹象。
		sum := sha256.Sum256(snap.GetPayload())
		if want := hex.EncodeToString(sum[:]); snap.GetChecksum() != want {
			// 限速：同一份坏载荷每轮都回执会在核心侧堆出一串重复记录。只在**首次见到**时回执。
			if snap.GetChecksum() != lastRejected {
				lastRejected = snap.GetChecksum()
				h.ack(pullCtx, snap, adapterID, false, "校验和不匹配")
				log.Printf("proxy: 策略 v%d 校验和不匹配，拒绝应用", snap.GetVersion())
			}
			return
		}
		lastRejected = ""

		// 同版本同校验和 → 不重复重建后端（也不重复回执）。
		if cur := h.remotePolicy(); cur != nil && cur.version == snap.GetVersion() && cur.checksum == snap.GetChecksum() {
			return
		}

		if aerr := h.applyEdgePolicy(pullCtx, snap.GetPayload()); aerr != nil {
			h.ack(pullCtx, snap, adapterID, false, aerr.Error())
			log.Printf("proxy: 策略 v%d 应用失败（继续用当前策略）：%v", snap.GetVersion(), aerr)
			return
		}
		if cur := h.remotePolicy(); cur != nil {
			cur.checksum = snap.GetChecksum()
		}
		h.ack(pullCtx, snap, adapterID, true, "")
		n := 0
		if cur := h.remotePolicy(); cur != nil {
			n = len(cur.backends)
		}
		log.Printf("proxy: 已应用策略 %s v%d（%d 个改道后端）", snap.GetPolicyId(), snap.GetVersion(), n)
	}

	pull()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pull()
		}
	}
}

// ack 上报回执（`AR-13` 的版本对账）。回执失败只记日志 —— 对账不该影响服务。
func (h *Handler) ack(ctx context.Context, snap *policyv1.PolicySnapshot, adapterID string, applied bool, reason string) {
	if h.policy == nil {
		return
	}
	_, err := h.policy.Ack(ctx, &policyv1.PolicyAck{
		PolicyId:  snap.GetPolicyId(),
		Version:   snap.GetVersion(),
		AdapterId: adapterID,
		Applied:   applied,
		Reason:    reason,
	})
	if err != nil {
		log.Printf("proxy: 回执上报失败（不影响服务）：%v", err)
	}
}

// policyTimeout 是单次策略拉取/回执的超时。策略面慢不能拖住进程退出。
const policyTimeout = 3 * time.Second

// currentInjector 返回**当前**生效的注入器：
//   - 策略面显式下发了 `inject_rules` → 用远端的（显式空数组 = 无规则，返回 nil）；
//   - 未下发该段 → 用本地 env 构造的注入器。
func (h *Handler) currentInjector() Injector {
	if st := h.remotePolicy(); st != nil && st.injectRulesProvided {
		return st.injector
	}
	return h.injector
}

// remoteWhitelisted 报告来源 IP 是否命中远端白名单（`INT-25`）。
func (h *Handler) remoteWhitelisted(ip string) bool {
	st := h.remotePolicy()
	if st == nil || len(st.cidrs) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, p := range st.cidrs {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// remoteVersion 返回已应用策略的版本（未应用过为 0）。
func (h *Handler) remoteVersion() uint64 {
	if st := h.remotePolicy(); st != nil {
		return st.version
	}
	return 0
}

// 保证 atomic.Pointer 的类型稳定（编译期自检）。
var _ = atomic.Pointer[remoteState]{}
