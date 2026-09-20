// Package topology 把观测面事件聚合成**流量调度图**（DAG）的纯数据模型。
//
// 为什么单独成包：图是"看数据"的核心逻辑（join、落点归并、告警分级），必须能脱离 HTTP 与 Caddy
// 单测（MD-22：模块测试用替身）。main.go 只做「取事件 → 转成本包输入 → 返回 JSON」。
//
// 图要回答的问题只有一句：**流量进来后实际走到哪了** —— 业务源站，还是幻境后端。
// 因此必须区分两件容易混淆的事：
//
//	意图：核心判成什么（decision 事件的 action / score / signals）
//	实际：适配器执行了什么（request_judged 事件的 executed）—— 影子模式仍是 origin（INT-11），
//	      幻境后端不可用会回落业务（NI-5）。
package topology

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// 节点 id（固定骨架；幻境后端按名字动态生成子节点）。
const (
	NodeClient    = "client"
	NodeAdapter   = "adapter"
	NodeWhitelist = "branch:whitelist"
	NodeCache     = "branch:cache"
	NodeFailOpen  = "branch:failopen"
	NodeJudge     = "judge"
	NodeOrigin    = "origin"
	NodeBlock     = "block"
	NodeL4        = "analysis"
)

// 事件里的 action 取值（核心侧枚举的字符串形式）。
const (
	ActionMirage = "ACTION_MIRAGE"
	ActionBlock  = "ACTION_BLOCK"
)

// JudgedEvent 是适配器上报的一次请求执行结果（request_judged）。
// 注意 JSON 标签与契约严格对齐（docs/spec/events.md §2.2）：键名漂移图就会画错。
// DecisionID 与 At 不在载荷里（前者是事件信封的 event_id，后者是 created_at），由调用方填。
type JudgedEvent struct {
	DecisionID    string    `json:"-"`
	At            time.Time `json:"-"`
	Method        string    `json:"method"`
	Path          string    `json:"path"`
	UA            string    `json:"ua"`
	Action        string    `json:"action"`
	Executed      string    `json:"executed"`
	Backend       string    `json:"backend"`
	Shadow        bool      `json:"shadow"`
	Status        int       `json:"status"`
	Bytes         int       `json:"bytes"`
	DurationMs    float64   `json:"duration_ms"`
	DecisionError string    `json:"decision_error"`
}

// DecisionEvent 是核心记录的一次判定（decision）。
type DecisionEvent struct {
	DecisionID string    `json:"decision_id"`
	At         time.Time `json:"at"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	SourceIP   string    `json:"source_ip"`
	UserAgent  string    `json:"user_agent"`
	Action     string    `json:"action"`
	Severity   string    `json:"severity"`
	Backend    string    `json:"backend"`
	Score      float64   `json:"score"`
	Signals    []string  `json:"signals"`
}

// AnalysisEvent 是 L4 的一条结论（analysis）。
type AnalysisEvent struct {
	Kind        string    `json:"kind"`
	Accepted    bool      `json:"accepted"`
	EvidenceIDs []string  `json:"evidence_ids"`
	At          time.Time `json:"-"`
}

// Input 是一次构建的全部输入。
type Input struct {
	Judged    []JudgedEvent
	Decisions []DecisionEvent
	Analysis  []AnalysisEvent
}

// Node 是图上的一个节点。
type Node struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Kind     string `json:"kind"` // client|adapter|branch|judge|decision|origin|mirage|block|analysis
	Count    int    `json:"count"`
	Alerts   int    `json:"alerts"`    // 真实告警（block 或 severity≠none）
	HighRisk int    `json:"high_risk"` // 仅显示用：score ≥ 阈值
	Note     string `json:"note,omitempty"`
}

// Edge 是图上的一条边（带计数与告警标记）。
type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Count    int    `json:"count"`
	Alerts   int    `json:"alerts"`
	HighRisk int    `json:"high_risk"`
	Kind     string `json:"kind"` // normal|mirage|fallback|block|alert
}

// Totals 是图上的汇总计数（用于一眼判断流量构成）。
type Totals struct {
	Requests int `json:"requests"`
	Alerts   int `json:"alerts"`
	HighRisk int `json:"high_risk"`
	Unjudged int `json:"unjudged"` // 白名单 / 缓存：没有核心判定
	FailOpen int `json:"fail_open"`
	ToOrigin int `json:"to_origin"`
	ToMirage int `json:"to_mirage"`
	Fallback int `json:"fallback"`
	Blocked  int `json:"blocked"`
	L4Notes  int `json:"l4_conclusions"`
}

// Graph 是返回给页面的完整模型。
type Graph struct {
	Nodes      []Node   `json:"nodes"`
	Edges      []Edge   `json:"edges"`
	Totals     Totals   `json:"totals"`
	Shadow     bool     `json:"shadow"`
	AlertScore float64  `json:"alert_score"`
	Notes      []string `json:"notes"`
}

// add 累加一个节点上的计数（每次经过该节点的请求各算一次）。
func (n *Node) add(alerts, highRisk int) {
	n.Count++
	n.Alerts += alerts
	n.HighRisk += highRisk
}

// 内部：边累加器。
type counter struct {
	count    int
	alerts   int
	highRisk int
}

func (c *counter) add(alerts, highRisk int) {
	c.count++
	c.alerts += alerts
	c.highRisk += highRisk
}

// Build 把事件聚合成图。
//
// alertScore 是**仅显示用**的高风险阈值（默认 0.9）：它不改变任何处置，也不参与真实告警口径
// （真实告警 = block 或 severity≠none，见 docs/spec/metrics.md）。
func Build(in Input, alertScore float64) Graph {
	nodes := map[string]*Node{}
	edges := map[string]*counter{}
	edgeKind := map[string]string{}
	var totals Totals
	shadow := false

	node := func(id, label, kind string) *Node {
		if n, ok := nodes[id]; ok {
			return n
		}
		n := &Node{ID: id, Label: label, Kind: kind}
		nodes[id] = n
		return n
	}
	edge := func(from, to, kind string) *counter {
		key := from + "→" + to
		c, ok := edges[key]
		if !ok {
			c = &counter{}
			edges[key] = c
			edgeKind[key] = kind
		}
		return c
	}

	decisions := map[string]DecisionEvent{}
	for _, d := range in.Decisions {
		decisions[d.DecisionID] = d
	}
	l4ByDecision := map[string]int{}
	for _, a := range in.Analysis {
		if !a.Accepted {
			continue
		}
		totals.L4Notes++
		for _, ev := range a.EvidenceIDs {
			l4ByDecision[ev]++
		}
	}

	sort.Slice(in.Judged, func(i, j int) bool { return in.Judged[i].At.Before(in.Judged[j].At) })

	node(NodeClient, "客户端", "client")
	node(NodeAdapter, "适配器 (L1)", "adapter")

	for _, j := range in.Judged {
		dec, paired := decisions[j.DecisionID]
		totals.Requests++
		shadow = shadow || j.Shadow

		// 告警分级：真实告警按现行口径；高风险仅显示。
		alerts, highRisk := 0, 0
		if paired {
			if dec.Action == ActionBlock || (dec.Severity != "" && dec.Severity != "none") {
				alerts = 1
			}
			if dec.Score >= alertScore {
				highRisk = 1
			}
		}
		totals.Alerts += alerts
		totals.HighRisk += highRisk

		node(NodeClient, "客户端", "client").add(alerts, highRisk)
		node(NodeAdapter, "适配器 (L1)", "adapter").add(alerts, highRisk)
		edge(NodeClient, NodeAdapter, "normal").add(alerts, highRisk)

		// 判定：有核心事件 → 真判定；只有适配器事件 → 白名单/缓存/失败放行。
		decisionID := NodeJudge
		switch j.Executed {
		case "whitelist":
			decisionID = NodeWhitelist
			totals.Unjudged++
			node(NodeWhitelist, "白名单命中（未判定）", "branch").add(alerts, highRisk)
			edge(NodeAdapter, NodeWhitelist, "normal").add(alerts, highRisk)
		case "cache":
			decisionID = NodeCache
			totals.Unjudged++
			node(NodeCache, "判定缓存命中（未重判）", "branch").add(alerts, highRisk)
			edge(NodeAdapter, NodeCache, "normal").add(alerts, highRisk)
		case "failopen":
			decisionID = NodeFailOpen
			totals.FailOpen++
			node(NodeFailOpen, "判定失败（NI-3 放行）", "branch").Note = firstNonEmpty(j.DecisionError, "核心不可达或超时")
			edge(NodeAdapter, NodeFailOpen, "alert").add(alerts, highRisk)
		default:
			node(NodeJudge, "核心判定", "judge").add(alerts, highRisk)
			edge(NodeAdapter, NodeJudge, "normal").add(alerts, highRisk)
		}

		// 意图节点（三值）—— 缓存/白名单/失败放行没有核心判定，就不画意图节点。
		destKind := "normal"
		switch j.Executed {
		case "mirage":
			name := j.Backend
			if name == "" {
				name = "（未命名）"
			}
			dest := "mirage:" + name
			node(dest, "幻境后端: "+name, "mirage").add(alerts, highRisk)
			edge(decisionID, dest, "mirage").add(alerts, highRisk)
			totals.ToMirage++
		case "origin_fallback":
			node("branch:fallback", "幻境不可用 → 回落业务 (NI-5)", "branch").add(alerts, highRisk)
			edge(decisionID, "branch:fallback", "fallback").add(alerts, highRisk)
			node(NodeOrigin, "业务源站", "origin").add(alerts, highRisk)
			edge("branch:fallback", NodeOrigin, "fallback").add(alerts, highRisk)
			totals.Fallback++
		case "block":
			node(NodeBlock, "拦截 (403)", "block").add(alerts, highRisk)
			edge(decisionID, NodeBlock, "block").add(alerts, highRisk)
			totals.Blocked++
		default: // origin / cache / whitelist / failopen 都落到业务源站
			node(NodeOrigin, "业务源站", "origin").add(alerts, highRisk)
			edge(decisionID, NodeOrigin, destKind).add(alerts, highRisk)
			totals.ToOrigin++
		}

		// L4 结论注解（不影响主链路形状）。
		if l4ByDecision[j.DecisionID] > 0 {
			node(NodeL4, "L4 分析结论", "analysis").add(0, 0)
			edge(decisionID, NodeL4, "normal").add(0, 0)
		}
	}

	// 稳定输出：节点按 kind+id，边按 from→to，保证页面与测试可预期。
	out := Graph{Shadow: shadow, AlertScore: alertScore}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, *n)
	}
	sort.Slice(out.Nodes, func(i, j int) bool {
		if out.Nodes[i].Kind != out.Nodes[j].Kind {
			return out.Nodes[i].Kind < out.Nodes[j].Kind
		}
		return out.Nodes[i].ID < out.Nodes[j].ID
	})
	for key, c := range edges {
		from, to, _ := splitEdge(key)
		out.Edges = append(out.Edges, Edge{
			From: from, To: to, Count: c.count, Alerts: c.alerts, HighRisk: c.highRisk, Kind: edgeKind[key],
		})
	}
	sort.Slice(out.Edges, func(i, j int) bool {
		if out.Edges[i].From != out.Edges[j].From {
			return out.Edges[i].From < out.Edges[j].From
		}
		return out.Edges[i].To < out.Edges[j].To
	})
	out.Totals = totals
	out.Notes = buildNotes(totals, shadow, alertScore)
	return out
}

func splitEdge(key string) (string, string, bool) {
	for i := 0; i < len(key); i++ {
		if key[i] == 0xe2 { // "→" 的第一个字节（UTF-8）
			return key[:i], key[i+len("→"):], true
		}
	}
	return key, "", false
}

// buildNotes 明确告诉读图的人"哪些地方现在还没有数据"，避免把缺口当成没问题。
func buildNotes(totals Totals, shadow bool, alertScore float64) []string {
	notes := []string{
		fmt.Sprintf("高风险阈值 %.2f 仅用于显示（不改变任何处置；真实告警 = 拦截或 severity≠none）", alertScore),
	}
	if shadow {
		notes = append(notes, "当前影子模式（INT-11）：意图已判，但执行仍在业务源站 —— 图上会同时出现意图边与实际执行边")
	}
	if totals.ToMirage == 0 {
		notes = append(notes, "没有任何请求进入幻境后端（未登记可用幻境后端，或处于影子模式）")
	}
	if totals.Alerts == 0 {
		notes = append(notes, "真实告警为 0：severity 档位未定（恒为 none），影子模式下也不会产生拦截")
	}
	if totals.FailOpen > 0 {
		notes = append(notes, fmt.Sprintf("有 %d 条判定失败后放行（NI-3）：业务不受影响，但这些请求没有判定记录", totals.FailOpen))
	}
	return notes
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ── 逐请求链路（每个请求一条独立 DAG；不与其他请求聚合）─────────────────────────
//
// 与 Build（聚合拓扑）的区别：Build 回答"整体流量怎么分布"，BuildRequests 回答
// "**这一条**请求实际怎么走的"——每一跳都带该请求自己的值（分值/信号/落点/状态/字节/耗时）。

// ChainNode 是某条请求链路上的一跳。
type ChainNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`            // client|adapter|branch|judge|decision|origin|mirage|block|analysis
	Value string `json:"value,omitempty"` // 这一跳上该请求的具体值（画在方框里）
	Alert bool   `json:"alert,omitempty"` // 真实告警所在的一跳
	Warn  bool   `json:"warn,omitempty"`  // 高风险（仅显示）或回落/失败

	// 以下是**点开这一跳**要看的三段（页面上逐步可点，用于定位"哪儿需要优化"）：
	Request  string `json:"request,omitempty"`  // 这一步收到的输入是什么
	Response string `json:"response,omitempty"` // 这一步给出/返回了什么
	Why      string `json:"why,omitempty"`      // 为什么会走到这一步（规则依据 + 约束）
}

// RequestGraph 是**单条请求**的链路（页面一行一个 DAG）。
type RequestGraph struct {
	DecisionID string      `json:"decision_id"`
	At         time.Time   `json:"at"`
	Method     string      `json:"method"`
	Path       string      `json:"path"`
	UA         string      `json:"ua"`
	SourceIP   string      `json:"source_ip"`
	Action     string      `json:"action"`
	Score      float64     `json:"score"`
	Signals    []string    `json:"signals"`
	Severity   string      `json:"severity"`
	Executed   string      `json:"executed"`
	Backend    string      `json:"backend"`
	Status     int         `json:"status"`
	Bytes      int         `json:"bytes"`
	DurationMs float64     `json:"duration_ms"`
	RealAlert  bool        `json:"real_alert"`
	HighRisk   bool        `json:"high_risk"`
	L4         int         `json:"l4_conclusions"`
	Unjudged   bool        `json:"unjudged"` // 白名单 / 缓存 / 判定失败：没有核心判定
	Chain      []ChainNode `json:"chain"`
}

// BuildRequests 把事件摊平成**逐请求链路**，最新的在前。
func BuildRequests(in Input, alertScore float64) []RequestGraph {
	decisions := map[string]DecisionEvent{}
	for _, d := range in.Decisions {
		decisions[d.DecisionID] = d
	}
	l4 := map[string]int{}
	for _, a := range in.Analysis {
		if !a.Accepted {
			continue
		}
		for _, ev := range a.EvidenceIDs {
			l4[ev]++
		}
	}

	out := make([]RequestGraph, 0, len(in.Judged))
	for _, j := range in.Judged {
		dec, paired := decisions[j.DecisionID]
		rg := RequestGraph{
			DecisionID: j.DecisionID, At: j.At, Method: j.Method, Path: j.Path, UA: j.UA,
			Executed: j.Executed, Backend: j.Backend, Status: j.Status, Bytes: j.Bytes,
			DurationMs: j.DurationMs, L4: l4[j.DecisionID],
		}
		if paired {
			rg.Action, rg.Score, rg.Signals = dec.Action, dec.Score, dec.Signals
			rg.Severity, rg.SourceIP = dec.Severity, dec.SourceIP
			if dec.Action == ActionBlock || (dec.Severity != "" && dec.Severity != "none") {
				rg.RealAlert = true
			}
			rg.HighRisk = dec.Score >= alertScore
		}
		rg.Unjudged = j.Executed == "whitelist" || j.Executed == "cache" || j.Executed == "failopen"

		// 链路：客户端 → 适配器 → （分支 or 核心判定）→ 意图 → 实际落点（→ L4）
		obs := fmt.Sprintf("%s %s（来源 %s · UA %s）", j.Method, j.Path,
			firstNonEmpty(rg.SourceIP, "未采集"), firstNonEmpty(j.UA, "未采集"))
		rg.Chain = append(rg.Chain,
			ChainNode{
				ID: "client", Label: "客户端", Kind: "client", Value: firstNonEmpty(rg.SourceIP, "来源未采集"),
				Request:  obs,
				Response: "—（这是链路的起点，观测由适配器采集）",
				Why:      "请求到达接入层后进入引擎：L0 负责 TLS 与路由，适配器负责观测与处置执行（判定逻辑只在核心，AR-2）。",
			},
			ChainNode{
				ID: "adapter", Label: "适配器 (L1)", Kind: "adapter", Value: j.Method + " " + j.Path,
				Request:  obs,
				Response: fmt.Sprintf("判定来源：%s", adapterSource(j.Executed)),
				Why:      "适配器按顺序做四件事：白名单 → 本地判定缓存 → 调核心判定 → 异步上报（AR-6）；它只执行处置，不做判定（AR-7）。",
			},
		)
		switch j.Executed {
		case "whitelist":
			rg.Chain = append(rg.Chain, ChainNode{
				ID: "branch:whitelist", Label: "白名单命中", Kind: "branch", Value: "跳过判定（INT-25）",
				Request:  obs,
				Response: "不出判定，直接放行到业务",
				Why: "命中白名单（INT-25）：内部探针 / 健康检查 / 监控必须在引流判定**之前**放行；" +
					"本地（env）与远端（策略面）白名单取**并集**，护栏只增不减。",
			})
		case "cache":
			rg.Chain = append(rg.Chain, ChainNode{
				ID: "branch:cache", Label: "判定缓存命中", Kind: "branch", Value: "复用同窗判定（ST-10）",
				Request:  obs,
				Response: fmt.Sprintf("复用判定 decision_id=%s（未重新调用核心）", j.DecisionID),
				Why: "命中本地判定缓存（ST-10）：键是 (来源, 会话, 方法, 路径) 加时间窗；" +
					"**同窗内谁先到谁定调** —— 这是已知取舍（K-20），排查误判时要先看这里。",
			})
		case "failopen":
			rg.Chain = append(rg.Chain, ChainNode{
				ID: "branch:failopen", Label: "判定失败", Kind: "branch",
				Value: firstNonEmpty(j.DecisionError, "核心不可达/超时"), Alert: true,
				Request:  obs,
				Response: "没有判定结果 ⇒ 按放行处理",
				Why: "调核心失败（不可达 / 超时，判定预算 3ms，AR-29）⇒ 按 NI-3 / NI-4 **放行到真实业务**：" +
					"业务优先于观测（NI-1）；代价是这条请求没有判定记录（K-24 讲的就是这个）。",
			})
		default:
			rg.Chain = append(rg.Chain, ChainNode{
				ID: "judge", Label: "核心判定", Kind: "judge", Value: judgeValue(rg), Warn: rg.HighRisk,
				Request:  obs,
				Response: judgeValue(rg),
				Why: fmt.Sprintf("judge 按配置里的规则逐条匹配（权重求和，1.0 截断）⇒ 分值 %.2f，命中 %d 条规则%s；"+
					"是否处置由 director 按阈值与灰度决定。", rg.Score, len(rg.Signals), signalSuffix(rg.Signals)),
			})
		}
		if paired && !rg.Unjudged {
			rg.Chain = append(rg.Chain, ChainNode{
				ID: "decision", Label: "决策", Kind: "decision", Value: actionLabel(dec.Action),
				Request:  fmt.Sprintf("分值 %.2f · 信号 %s", rg.Score, joinSignals(rg.Signals)),
				Response: actionLabel(dec.Action),
				Why: "director 输出**三值**（放行 / 改道 / 拦截）+ severity 旁路字段；" +
					"影子模式下只算不执行（INT-11）——所以下一跳可能仍是业务源站。",
			})
		}
		switch j.Executed {
		case "mirage":
			rg.Chain = append(rg.Chain, ChainNode{
				ID: "mirage", Label: "幻境后端", Kind: "mirage", Value: firstNonEmpty(j.Backend, "未命名"),
				Request:  obs,
				Response: routeValue(rg),
				Why: fmt.Sprintf("决策为改道 ⇒ 转发到幻境后端 %q（改道后端表由策略面下发，ADR-0018）；"+
					"注入只发生在改道侧（INT-8：业务侧响应零改写）。", j.Backend),
			})
		case "origin_fallback":
			rg.Chain = append(rg.Chain,
				ChainNode{
					ID: "branch:fallback", Label: "幻境不可用", Kind: "branch", Value: "回落业务（NI-5）", Warn: true,
					Request:  obs,
					Response: "放弃改道，改走业务源站",
					Why: "决策是改道，但后端**未登记或不可达**（或中途失败且尚未写出字节）⇒ 回落真实业务：" +
						"宁可漏改道，不可断业务（NI-5 / NI-1）。",
				},
				ChainNode{
					ID: "origin", Label: "业务源站", Kind: "origin", Value: routeValue(rg),
					Request:  obs,
					Response: routeValue(rg),
					Why:      "这是**回落**到业务（不是原本就放行）——路由决策与最终落点不一致时，这一跳就是原因。",
				},
			)
		case "block":
			rg.Chain = append(rg.Chain, ChainNode{
				ID: "block", Label: "拦截", Kind: "block", Value: "403", Alert: true,
				Request:  obs,
				Response: routeValue(rg),
				Why: "决策为拦截 ⇒ 返回 403。403 是对手**可见**的处置，属已承认的设计（ADR-0002）：" +
					"block 只用于「明确拒绝已知恶意」，透明误导由 route_mirage 承担。",
			})
		default:
			rg.Chain = append(rg.Chain, ChainNode{
				ID: "origin", Label: "业务源站", Kind: "origin", Value: routeValue(rg),
				Request:  obs,
				Response: routeValue(rg),
				Why:      originWhy(rg),
			})
		}
		if rg.L4 > 0 {
			rg.Chain = append(rg.Chain, ChainNode{
				ID: "analysis", Label: "L4 分析", Kind: "analysis", Value: fmt.Sprintf("%d 条结论引用", rg.L4),
				Request:  "该判定的证据（decision_id 作为证据 ID）",
				Response: fmt.Sprintf("%d 条结论引用了这条判定", rg.L4),
				Why: "近线 worker 读遥测事件 → 态势去重（AR-14）→ 意图 / 攻击链 / 策略 → 结论作为事件回写（AR-12：" +
					"引用必须真实存在）；L4 **不在请求路径上**，不影响这条请求的处置。",
			})
		}
		out = append(out, rg)
	}
	// 最新在前：页面每 5 秒刷新时，新流量出现在最上面（"动态"）。
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

func judgeValue(rg RequestGraph) string {
	parts := []string{fmt.Sprintf("分值 %.2f", rg.Score)}
	if len(rg.Signals) > 0 {
		parts = append(parts, "信号 "+joinSignals(rg.Signals))
	}
	return joinNonEmpty(parts, " · ")
}

func routeValue(rg RequestGraph) string {
	if rg.Status == 0 && rg.Bytes == 0 && rg.DurationMs == 0 {
		return "返回信息未采集"
	}
	return fmt.Sprintf("%d · %d 字节 · %.1fms", rg.Status, rg.Bytes, rg.DurationMs)
}

func joinSignals(signals []string) string {
	var b strings.Builder
	for i, sig := range signals {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(sig)
	}
	return b.String()
}

func joinNonEmpty(parts []string, sep string) string {
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString(sep)
		}
		b.WriteString(part)
	}
	return b.String()
}

// adapterSource 说明这次判定是从哪来的（页面上"为什么没调核心"一眼可见）。
func adapterSource(executed string) string {
	switch executed {
	case "whitelist":
		return "白名单（未调核心）"
	case "cache":
		return "本地判定缓存（未调核心）"
	case "failopen":
		return "调核心失败 ⇒ 放行"
	default:
		return "核心判定"
	}
}

func signalSuffix(signals []string) string {
	if len(signals) == 0 {
		return ""
	}
	return "（" + joinSignals(signals) + "）"
}

// originWhy 解释"为什么最终落在业务源站" —— 三种情形要分清，否则会误判引擎没工作。
func originWhy(rg RequestGraph) string {
	if rg.Unjudged {
		return "未经过判定（白名单 / 缓存命中 / 判定失败后放行）⇒ 直接到业务源站。"
	}
	if rg.executedOriginBecauseShadow() {
		return "决策可能是放行，也可能是改道/拦截但**影子模式**不执行（INT-11）：引擎照算判定、照上报，只观测不处置。"
	}
	return "决策为放行（route_origin）⇒ 业务响应**原样透传**（INT-8：业务侧响应零改写）。"
}

// executedOriginBecauseShadow 判断"落在源站"是否因为影子模式（意图不是放行时才有意义）。
func (rg RequestGraph) executedOriginBecauseShadow() bool {
	return rg.Action != "" && rg.Action != "ACTION_ORIGIN"
}

// actionLabel 把核心的三值决策转成人话（与页面其余部分一致）。
func actionLabel(action string) string {
	switch action {
	case ActionMirage:
		return "改道（route_mirage）"
	case ActionBlock:
		return "拦截（block）"
	default:
		return "放行（route_origin）"
	}
}
