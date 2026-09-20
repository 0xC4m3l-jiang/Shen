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
