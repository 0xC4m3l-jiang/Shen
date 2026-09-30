package director

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"sync/atomic"
	"time"

	"shen/common/core/internal/contract"
	"shen/common/core/internal/judge"
)

// defaultBackend 是阶段 2a 的改道目标逻辑名。
//
// 后端池的选择（多后端、健康检查、按会话粘性）是阶段 3 的事；2a 只有一个逻辑名，
// 由适配器的引流后端表（如 SHEN_PROXY_MIRAGE=mirage=<addr>）匹配。
const defaultBackend = "mirage"

// Config 是本模块的全部可调项。
type Config struct {
	// Backend 是改道目标的逻辑名。空则用 defaultBackend。
	Backend string

	// BlockEnabled 决定是否允许产出 block（Q5：默认关）。
	//
	// 项目的全部价值在**透明改道**；可见拦截会自曝存在、推走对手。
	// 因此默认不产出 —— 它只作「改道不可行」时的最后手段（NI-1）。
	BlockEnabled bool

	// DecoyPrefixes 是诱饵面路径前缀集。
	//
	// MD-25：**诱饵面必须 observe-only** —— 在这些路径上**禁止**产出 block。
	// 理由：采集链路的每一步都在诱饵面上，在诱饵面阻断 = 在第一步就掉断自己的情报源。
	// 由装配层从诱饵资产汇总（单一事实源），本模块只消费前缀集。
	DecoyPrefixes []string

	// Whitelist 是免判定的来源集合（INT-25）。
	//
	// 内部 IP / 健康检查 / 监控探针**必须在引流判定前**放行 ——
	// 否则监控探针会被当作 Agent 处理（误调度）。
	// 命中即直接 route_origin，**连判定都不做**（与适配器侧同语义）。
	Whitelist contract.Whitelist

	// NoDeception 是「禁止欺骗路径」（黑名单）：命中时 route_mirage 降级为放行，
	// 并附加信号 `deception_excluded`（判定照常、可观测，只是不改道）。
	NoDeception []contract.NoDeceptionRule

	// Now 注入时钟。它**不参与判定**（灰度只依赖 decision_id，ST-10），
	// 仅为将来的可观测留口。为空时用 time.Now。
	Now func() time.Time
}

// Engine 是 Director 的实现。它**无状态**：决策是输入的纯函数，多副本天然一致（AR-9）。
type Engine struct {
	judge   judge.Judge
	thr     ThresholdSource
	gray    GraySource
	backend string
	block   bool
	// surf 是**可热替换**的路径面（诱饵前缀 / 白名单 / 禁止欺骗路径）：管控台同步时整体替换，
	// 热路径每个请求只做一次原子读、零锁 —— 读到的永远是某一份完整的面（不会半新半旧）。
	surf atomic.Pointer[surface]
	now  func() time.Time
}

// surface 是一份不可变的路径面。
type surface struct {
	decoyPrefixes []string
	whitelist     contract.Whitelist
	noDeception   []contract.NoDeceptionRule
}

func newSurface(decoyPrefixes []string, wl contract.Whitelist, nd []contract.NoDeceptionRule) *surface {
	rules := make([]contract.NoDeceptionRule, 0, len(nd))
	for _, r := range nd {
		if strings.TrimSpace(r.PathPrefix) != "" { // 空前缀会匹配一切 ⇒ 等于关掉整个欺骗面，丢弃
			r.PathPrefix = contract.NormalizePath(r.PathPrefix)
			rules = append(rules, r)
		}
	}
	return &surface{decoyPrefixes: normalizePrefixes(decoyPrefixes), whitelist: wl, noDeception: rules}
}

// UpdateSurface 原子替换路径面（管控台同步的落点）。并发安全：与 Decide 无锁并行。
func (e *Engine) UpdateSurface(decoyPrefixes []string, wl contract.Whitelist, nd []contract.NoDeceptionRule) {
	e.surf.Store(newSurface(decoyPrefixes, wl, nd))
}

// New 构造决策引擎。judge / thr / gray 任一为 nil 时返回错误。
func New(j judge.Judge, thr ThresholdSource, gray GraySource, cfg Config) (*Engine, error) {
	if j == nil {
		return nil, fmt.Errorf("director: judge 不能为 nil")
	}
	if thr == nil {
		return nil, fmt.Errorf("director: ThresholdSource 不能为 nil")
	}
	if gray == nil {
		return nil, fmt.Errorf("director: GraySource 不能为 nil")
	}
	backend := cfg.Backend
	if backend == "" {
		backend = defaultBackend
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	e := &Engine{
		judge:   j,
		thr:     thr,
		gray:    gray,
		backend: backend,
		block:   cfg.BlockEnabled,
		now:     now,
	}
	e.surf.Store(newSurface(cfg.DecoyPrefixes, cfg.Whitelist, cfg.NoDeception))
	return e, nil
}

// normalizePrefixes 丢掉空串，避免空前缀匹配一切（那会让 block 彻底失效）。
func normalizePrefixes(in []string) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// Decide 算判定并给出决策。
//
// 任何失败都上抛 —— 由调用方（服务面 → 适配器）按 fail-open 放行（NI-3 / NI-4 / NI-5）。
func (e *Engine) Decide(ctx context.Context, req contract.JudgeRequest) (contract.Decision, error) {
	// INT-25：白名单**先于**引流判定 —— 命中即放行，连判定都不做。
	//
	// 返回零值 Verdict：白名单流量本就无需打分（也不该污染观测数据）。
	sf := e.surf.Load() // 本次决策只读这一份面
	if sf.whitelisted(req.Observed) {
		return contract.Decision{
			DecisionID: req.DecisionID,
			Action:     contract.ActionOrigin,
			Severity:   contract.SeverityNone,
		}, nil
	}

	v, err := e.judge.Judge(ctx, req)
	if err != nil {
		return contract.Decision{}, err
	}
	th, err := e.thr.Thresholds(ctx)
	if err != nil {
		return contract.Decision{}, err
	}

	action, backend := e.classify(sf, v.Score, th, req.Observed.Path)

	// 灰度只作用于「本会改道」的请求：按 decision_id 哈希确定性判定。
	if action == contract.ActionMirage {
		gray, gerr := e.gray.GrayPct(ctx)
		if gerr != nil {
			return contract.Decision{}, gerr
		}
		if !grayAllows(req.DecisionID, gray) {
			action, backend = contract.ActionOrigin, ""
		}
	}

	// 禁止欺骗路径：本会改道 ⇒ 降级放行，并留下可观测的信号（不改变分数）。
	if action == contract.ActionMirage {
		if rule, hit := sf.excluded(req.Observed.Path); hit {
			action, backend = contract.ActionOrigin, ""
			v.Signals = append(append([]contract.Signal(nil), v.Signals...), contract.Signal{
				ID: contract.SignalDeceptionExcluded, Detail: rule.ID,
			})
		}
	}

	return contract.Decision{
		DecisionID: req.DecisionID,
		Action:     action,
		// 档位未实测登记前只能是 none（TM-13 / MD-24）。
		Severity: contract.SeverityNone,
		Backend:  backend,
		Verdict:  v,
	}, nil
}

// whitelisted 报告该观测是否属于免判定来源（INT-25）。
//
// 三项**任一**命中即可：源网段 / UA / 路径前缀。空项一律跳过（空串不该匹配一切）。
//
// 路径前缀用 **归一化 + 路径段边界**（`N5`），与适配器的诱饵路由同一口径：
// 原来的裸 `strings.HasPrefix` 会让配置的 `/admin` 命中 `/administrator`、`/admin.html` ——
// 那是把**运维探针误判成内部来源**，也就是把判定面整块让给了一个同样以前缀开头的陌生路径。
// 这个方向只会**收紧**（命中的更少），不会扩大白名单（`INT-25` 的护栏只增不减是针对策略下发说的）。
func (sf *surface) whitelisted(o contract.Observation) bool {
	for _, p := range sf.whitelist.SourceCIDRs {
		if p.IsValid() && p.Contains(o.SourceIP) {
			return true
		}
	}
	for _, ua := range sf.whitelist.UserAgents {
		if ua != "" && ua == o.UserAgent {
			return true
		}
	}
	path := contract.NormalizePath(o.Path)
	for _, pfx := range sf.whitelist.PathPrefixes {
		if pfx == "" {
			continue
		}
		if contract.PathSegmentPrefix(path, contract.NormalizePath(pfx)) {
			return true
		}
	}
	return false
}

// classify 把分数映射为三值 + 后端名。
//
// block 只在开关打开时产出；否则高分请求落到 route_mirage。
// 任何未落入已知分支的分数都得到 route_origin（NI-5 的唯一出口）。
//
// MD-25：**在诱饵面上禁止 block** —— 诱饵面是情报采集面，在那里阻断等于自断情报源。
func (e *Engine) classify(sf *surface, score float64, th contract.Thresholds, path string) (contract.Action, string) {
	if e.block && score >= th.Block && !sf.onDecoy(path) {
		return contract.ActionBlock, ""
	}
	if score >= th.Mirage {
		return contract.ActionMirage, e.backend
	}
	return contract.ActionOrigin, ""
}

// onDecoy 报告请求路径是否落在诱饵面上（MD-25）。
//
// MD-25 要求「集中常量 + 断言」：前缀集由装配层从**诱饵资产的同一份数据**汇总
// （单一事实源，见 cmd/core 的 decoyPrefixes），本函数是该不变量的执行点；
// 「诱饵面永不 block」由 director_test 与 cmd/core 的集成测试断言。
func (sf *surface) onDecoy(path string) bool {
	got := contract.NormalizePath(path)
	for _, p := range sf.decoyPrefixes {
		// 与白名单、与适配器的诱饵路由**同一个判据**（`N5`）：归一化 + 路径段边界。
		// 三者口径不一致时，同一路径会在核心与边缘得到不同归类（"幻境面永不 block" 也会漏）。
		if contract.PathSegmentPrefix(got, contract.NormalizePath(p)) {
			return true
		}
	}
	return false
}

// excluded 报告路径是否命中禁止欺骗路径（归一化 + 路径段边界，与白名单同一口径 N5）。
func (sf *surface) excluded(path string) (contract.NoDeceptionRule, bool) {
	if len(sf.noDeception) == 0 {
		return contract.NoDeceptionRule{}, false
	}
	got := contract.NormalizePath(path)
	for _, r := range sf.noDeception {
		if contract.PathSegmentPrefix(got, r.PathPrefix) {
			return r, true
		}
	}
	return contract.NoDeceptionRule{}, false
}

// grayAllows 判断本会改道的请求是否落在灰度范围内。
//
// **必须确定性**：同一 decision_id 必得同一结果 —— 用随机数会让重试得到不同决策，
// 直接破坏 ST-10 的幂等。
func grayAllows(decisionID string, grayPct uint8) bool {
	switch {
	case grayPct == 0:
		return false // 0% = 全放行（影子模式的自然延续）
	case grayPct >= 100:
		return true // 100% = 全改道
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(decisionID))
	return h.Sum32()%100 < uint32(grayPct)
}

var _ Director = (*Engine)(nil)
