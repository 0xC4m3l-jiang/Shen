package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	policyv1 "shen/common/api/policy/v1"
	"shen/modules/deception/injection"
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
	// Decoys 是**诱饵路由表**：专属路径 → 幻境后端逻辑名（方案 §9.2 / C01）。
	//
	// 核心只投影**启用中**的资产 ⇒ 这里没有「撤销」这种中间态：载荷里没有 = 边缘没有这条路由。
	Decoys []policyDecoy `json:"decoys,omitempty"`
}

// policyDecoy 是诱饵路由表的一行（与核心侧 `policy.edgeDecoy` 手工对齐）。
type policyDecoy struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	// Hosts 是**归属声明**（`W7`）：空 = 未声明 ⇒ 任何主机都匹配（旧载荷的形态）；
	// 非空 ⇒ 只有 Host 命中其中之一才接管这条路径。
	Hosts   []string `json:"hosts,omitempty"`
	Backend string   `json:"backend"`
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
	// declared 是**远端声明过的全部后端名**（含 `enabled: false` 的）。
	//
	// 为什么必须单独记：载荷里的 `enabled: false` 表示「核心已撤销这个后端」，
	// 而本地 env 表里可能有同名项 —— 撤销必须**优先于**本地兜底（方案 §9.2），
	// 否则「禁用」只是「从远端表里悄悄消失」，流量照样落到那个后端上（D06 的 P0 边界风险）。
	declared map[string]struct{}
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
	// decoys 是**专属诱饵路由表**（已归一化路径 + 按路径长度降序，便于最长匹配）。
	//
	// 它与 `backends` 分开：后端表是「能去哪」，路由表是「什么路径该去哪」。
	// 空 = 没有诱饵路由（此时请求照常走判定 / 白名单 / 兜底）。
	decoys []policyDecoyRoute
	// addresses 是「后端名 → TCP dial 地址（host:port）」：只给**健康探针**用（`health.go`）。
	// 与 backends 分开：backends 是构造好的 Caddy handler（不可反查地址），探针只需要地址。
	addresses map[string]string
	// tombstones 是**已撤销但仍未过租约的诱饵路径**（N6）。
	//
	// 一条诱饵路径一旦对外出现过，旧链接 / 爬虫 / 对手笔记都会继续用它。若路由一被删除
	// 就立刻把归属还给业务，那些残留请求会**直接打到生产**；租约期内在边缘继续用固定
	// 错误结束它们（不回生产），到期才真正释放归属。
	tombstones []policyDecoyRoute
}

// policyDecoyRoute 是一条已归一化的诱饵路由。
type policyDecoyRoute struct {
	path    string // 归一化路径（`contract.NormalizePath` 同一口径 —— 适配器侧用同一份语义）
	id      string // 资产 id（日志与事件用）
	backend string // 幻境后端逻辑名
	// hosts 是归属声明（已小写；`*.` 前缀表示后缀通配）；空 = 声明缺失 ⇒ 任何主机都匹配。
	hosts []string
	// revoked 为真 = 这是一条**搜索碑**（路由已撤销、租约未到期）：不投递、不回业务，固定 502。
	revoked bool
	// leaseUntil 是搜索碑的到期时刻（仅 revoked 时有意义）；零值 = 永久（不过期）。
	leaseUntil time.Time
}

// remotePolicy 返回当前生效的远端策略；未应用过时为 nil。
func (h *Handler) remotePolicy() *remoteState { return h.remote.Load() }

// policyRevision 是策略版本的稳定标识，供**本地判定缓存键**使用（FIX-2）。
//
// 为何要它：策略换代后旧动作可能已不成立（改道后端表、白名单、注入规则都变了），
// 缓存必须随之失效；否则新策略下发后，同键请求会继续沿用旧动作直到 TTL 到期。
// 优先用 checksum（内容决定标识）；无 checksum 时退到版本号；未接过策略面则空串。
//
// st 由调用方传入而不是在这里原子读：一个请求必须用**同一份快照**（N1）。
func (h *Handler) policyRevision(st *remoteState) string {
	if st == nil {
		return ""
	}
	if st.checksum != "" {
		return st.checksum
	}
	return "v" + strconv.FormatUint(st.version, 10)
}

// applyEdgePolicy 应用一份策略载荷。
//
// 校验顺序（任一不通过都**不应用**，调用方据此回执 `applied=false`）：
//  1. JSON 能解析；2. `schema_version` 本端读得懂；3. 载荷非空且版本与 checksum 自洽；
//
// **checksum 参数**是 N1 的收口：快照必须**一次构造完再发布**，不得先 Store 再原地补字段 ——
// 原地写会让并发请求读到「一半的新快照」（例如空 checksum ⇒ 缓存键失去策略维度）。
//  4. 后端地址是合法 URL —— 一个坏后端**不能**把整张表带下水，只在表里去掉它并记账。
//
// 合并语义（`ADR-0018`）：
//   - **后端表**：按逻辑名覆盖 —— 远端定义的同名项覆盖本地，本地独有的项保留（兜底）；
//   - **白名单**：**并集** —— 本地项永远保留。白名单是防误伤的护栏（`INT-25`），
//     缩小它会把运维探针判成攻击者，因此远端只能增加，不能删。
func (h *Handler) applyEdgePolicy(ctx context.Context, raw []byte, checksum string) error {
	var doc edgePolicy
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("载荷不是合法 JSON：%w", err)
	}
	if doc.SchemaVersion != EdgePolicySchemaVersion {
		return fmt.Errorf("载荷 schema_version=%d，本端只支持 %d（升级适配器后再应用）",
			doc.SchemaVersion, EdgePolicySchemaVersion)
	}

	backends := map[string]caddyhttp.MiddlewareHandler{}
	addresses := make(map[string]string, len(doc.Backends))
	declared := make(map[string]struct{}, len(doc.Backends))
	var dropped []string
	var revokedLocal []string
	for _, b := range doc.Backends {
		declared[b.Name] = struct{}{}
		if !b.Enabled {
			// 撤销：远端声明了这个名字但明确禁用。记下来，路由时**不再**回落本地同名项。
			if _, inLocal := h.mirage[b.Name]; inLocal {
				revokedLocal = append(revokedLocal, b.Name)
			}
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
		if dial, _, derr := upstreamAddr(b.Address); derr == nil {
			addresses[b.Name] = dial
		}
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

	// 诱饵路由表：归一化路径 + 按路径长度**降序**（匹配时取第一个命中的即最长匹配）；
	// 后端名可解析性在**路由时**判断（后端表可能在同一份载荷里；顺序与健康都会变）。
	decoys := make([]policyDecoyRoute, 0, len(doc.Decoys))
	for _, d := range doc.Decoys {
		path := normalizeRoutePath(d.Path)
		if path == "" || d.Backend == "" {
			// 空路径 = 什么都命中（那不是路由，是漏洞）；空后端 = 送不到任何地方。
			continue
		}
		hosts := make([]string, 0, len(d.Hosts))
		for _, h := range d.Hosts {
			if norm := normalizeHostPattern(h); norm != "" {
				hosts = append(hosts, norm)
			}
		}
		decoys = append(decoys, policyDecoyRoute{path: path, id: d.ID, backend: d.Backend, hosts: hosts})
	}
	sort.Slice(decoys, func(i, j int) bool {
		if len(decoys[i].path) != len(decoys[j].path) {
			return len(decoys[i].path) > len(decoys[j].path)
		}
		return decoys[i].path < decoys[j].path // 同长度按字典序，保证确定性（AR-30）
	})

	// 搜索碑（N6）：上一份快照里有、本份里没有的路径 ⇒ 进入租约期，期内仍不属于业务。
	// 重新登记的路径从碑里移除（它又有主了）；过期碑被清理，归属真正还给业务。
	tombstones := h.nextTombstones(decoys)

	h.remote.Store(&remoteState{
		version:             doc.Version,
		checksum:            checksum,
		backends:            backends,
		declared:            declared,
		cidrs:               cidrs,
		injectRulesProvided: doc.InjectRules != nil,
		injector:            inj,
		injectEnabled:       doc.InjectEnabled,
		content:             newContentIndex(doc.ContentManifest),
		decoys:              decoys,
		tombstones:          tombstones,
		addresses:           addresses,
	})
	// 策略刚变 ⇒ 立刻看一眼新后端是否活着（否则要等到下一个探针周期）。
	if h.healthWake != nil {
		h.wake()
	}
	if len(dropped) > 0 {
		log.Printf("proxy: 策略 v%d 中有 %d 个后端地址不可用，已跳过：%s",
			doc.Version, len(dropped), strings.Join(dropped, ", "))
	}
	if len(revokedLocal) > 0 {
		// 撤销要**看得见**：否则运维会以为「本地还配着，应该还能用」（实测过这种误判）。
		log.Printf("proxy: 策略 v%d 撤销了后端 %s —— 本地同名项不再兜底（撤销优先于本地兜底）",
			doc.Version, strings.Join(revokedLocal, ", "))
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

		// checksum 与载荷一起进快照（N1）：发布之后**不再**改任何字段。
		if aerr := h.applyEdgePolicy(pullCtx, snap.GetPayload(), snap.GetChecksum()); aerr != nil {
			h.ack(pullCtx, snap, adapterID, false, aerr.Error())
			log.Printf("proxy: 策略 v%d 应用失败（继续用当前策略）：%v", snap.GetVersion(), aerr)
			return
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

// decoyLease 返回搜索碑租约（N6）：默认 24h；“负数”表示不过期（永久）。
func (h *Handler) decoyLease() time.Duration {
	d := time.Duration(h.DecoyLease)
	if d == 0 {
		return defaultDecoyLease
	}
	return d
}

// clock 是 nil 安全的时钟（Provision 会装 time.Now；单测里可能什么都没装）。
func (h *Handler) clock() time.Time {
	if h.now == nil {
		return time.Now()
	}
	return h.now()
}

// nextTombstones 计算下一份快照的搜索碑集。
//
// 规则（决定性与可测）：
//  1. 上一份里活路由 + 已有碑，减去本份的活路由 ⇒ 候选；
//  2. 候选延用**原有到期时刻**（不因每次下拉而续期），新撤销的用 now+租约；
//  3. 到期清掉 —— 释放归属于业务是**有意的**动作，但要到期才发生。
func (h *Handler) nextTombstones(live []policyDecoyRoute) []policyDecoyRoute {
	prev := h.remotePolicy()
	if prev == nil && len(live) == 0 {
		return nil
	}
	now := h.clock()
	lease := h.decoyLease()
	// 活路由按 **(归属, 路径)** 记：同一条路径可以被不同 Host 分别声明（多站点），
	// 撤销其中一个站点不该影响另一个 —— 这就是 `R05` 的核心（原来只按 path 记）。
	liveKeys := make(map[string]struct{}, len(live))
	for _, d := range live {
		liveKeys[routeKey(d.hosts, d.path)] = struct{}{}
	}
	out := make([]policyDecoyRoute, 0, len(live))
	seen := map[string]struct{}{}
	if prev != nil {
		for _, old := range append(append([]policyDecoyRoute{}, prev.decoys...), prev.tombstones...) {
			key := routeKey(old.hosts, old.path)
			if _, ok := liveKeys[key]; ok {
				continue // 又有主了（同一归属 + 同一路径）
			}
			if _, dup := seen[key]; dup {
				continue
			}
			until := old.leaseUntil
			if until.IsZero() && !old.revoked {
				// 新撤销：从本刻开始算租约。“永久”用零值表示。
				if lease < 0 {
					until = time.Time{}
				} else {
					until = now.Add(lease)
				}
			}
			if !until.IsZero() && !now.Before(until) {
				continue // 租约到期 ⇒ 释放归属
			}
			seen[key] = struct{}{}
			// **归属必须跟着墓碑走**（`R05`）：丢了 hosts 的碑会按"声明缺失 = 任何主机"匹配一切，
			// 于是「撤销 a.example 的 /x」会把 b.example 的 /x 也一并 502（错保护别人的站点），
			// 或者反过来被同路径的另一 Host 抵消掉（该保护的没保护）。两种方向都违背 §9.2 的归属承诺。
			out = append(out, policyDecoyRoute{
				path: old.path, id: old.id, hosts: append([]string(nil), old.hosts...),
				revoked: true, leaseUntil: until,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].path) != len(out[j].path) {
			return len(out[i].path) > len(out[j].path)
		}
		return out[i].path < out[j].path
	})
	return out
}

// routeKey 是**路由归属键**：归一化后的主机集合 + 归一化路径。
//
// 为什么不是 path 单键（`R05`）：一条路径可以在多个主机上各归各的资产；
// 用 path 单键会让"撤销 A 站点的 /x"同时抹掉 B 站点的同名路由（或反过来抵消）。
func routeKey(hosts []string, path string) string {
	if len(hosts) == 0 {
		return "|" + path // 空归属：与显式声明区分开（它在匹配语义上等于"任何主机"）
	}
	hs := append([]string(nil), hosts...)
	sort.Strings(hs)
	return strings.Join(hs, ",") + "|" + path
}

// hostMatches 报告请求的 Host 是否命中归属声明（`W7`）。
//
// 语义（与核心校验同一口径）：
//   - 声明为空 ⇒ **匹配**（旧载荷没有这个字段；此时行为与从前一致，不悄悄变成"谁都不接管"）；
//   - 精确主机名 ⇒ 完全相等（大小写不敏感，端口已剥离）；
//   - `*.example.com` ⇒ 后缀匹配，且**必须**有前缀标签（`example.com` 本身不算命中 -*-
//     这是通配符最短形式，避免"通配声明覆盖了根域"这种常见误配）。
func hostMatches(hosts []string, host string) bool {
	if len(hosts) == 0 {
		return true
	}
	h := normalizeRequestHost(host)
	if h == "" {
		return false
	}
	for _, pattern := range hosts {
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(pattern, "*")
			if strings.HasSuffix(h, suffix) && len(h) > len(suffix) {
				return true
			}
			continue
		}
		if h == pattern {
			return true
		}
	}
	return false
}

// normalizeRequestHost 把请求的 Host 归一化成匹配视图：小写、去端口、去尾点。
func normalizeRequestHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.TrimSuffix(h, ".")
	if hostOnly, _, err := net.SplitHostPort(h); err == nil {
		h = hostOnly
	}
	return h
}

// normalizeHostPattern 归一化一条归属声明；返回空串表示这条声明不可用（**丢弃**而不是放行）。
func normalizeHostPattern(raw string) string {
	h := normalizeRequestHost(raw)
	if h == "" || h == "*" {
		return ""
	}
	if strings.HasPrefix(h, "*.") && len(h) > 2 {
		return h
	}
	if strings.Contains(h, "*") {
		return "" // 只支持最左标签通配；其它形态直接丢弃（宁可不接管，也不要模糊归属）
	}
	return h
}

// defaultDecoyLease 是搜索碑租约的默认值（N6）。
const defaultDecoyLease = 24 * time.Hour

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

// currentInjector 返回**该快照**的注入器：
//   - 策略面显式下发了 `inject_rules` → 用远端的（显式空数组 = 无规则，返回 nil）；
//   - 未下发该段 → 用本地 env 构造的注入器。
func (h *Handler) currentInjector(st *remoteState) Injector {
	if st != nil && st.injectRulesProvided {
		return st.injector
	}
	return h.injector
}

// remoteWhitelisted 报告来源 IP 是否命中**该快照的**远端白名单（`INT-25`）。
func (h *Handler) remoteWhitelisted(st *remoteState, ip string) bool {
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
