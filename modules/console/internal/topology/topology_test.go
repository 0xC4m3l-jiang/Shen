package topology

import (
	"encoding/json"
	"testing"
	"time"
)

func at(sec int) time.Time { return time.Date(2026, 9, 20, 12, 0, sec, 0, time.UTC) }

// TestBuildPairsIntentAndExecution 守住图的核心能力：**区分意图与实际**。
// 影子模式下核心判 route_mirage（意图），适配器实际仍走业务源站（执行）。
func TestBuildPairsIntentAndExecution(t *testing.T) {
	g := Build(Input{
		Judged: []JudgedEvent{{
			DecisionID: "d1", At: at(1), Method: "GET", Path: "/.git/config", UA: "HeadlessChrome/120",
			Action: ActionMirage, Executed: "origin", Shadow: true, Status: 200, Bytes: 100, DurationMs: 1.5,
		}},
		Decisions: []DecisionEvent{{
			DecisionID: "d1", At: at(1), Method: "GET", Path: "/.git/config", SourceIP: "203.0.113.9",
			Action: ActionMirage, Severity: "none", Backend: "ssh-01", Score: 0.9,
			Signals: []string{"ua-headless", "path-probe"},
		}},
	}, 0.9)

	if !g.Shadow {
		t.Fatal("影子模式标记应被带出（页面上要提示只观测）")
	}
	if g.Totals.Requests != 1 || g.Totals.ToOrigin != 1 || g.Totals.ToMirage != 0 {
		t.Fatalf("意图是改道、执行是源站：应计 to_origin=1（实际 %+v）", g.Totals)
	}
	if g.Totals.HighRisk != 1 || g.Totals.Alerts != 0 {
		t.Fatalf("score=0.9 应计高风险、且**不得**计入真实告警（实际 %+v）", g.Totals)
	}
	if !hasEdge(g, NodeJudge, NodeOrigin) {
		t.Fatal("应有一条 judge → origin 的执行边")
	}
}

// TestBuildFallbackAndBlock 覆盖 NI-5 回落与 403 拦截两条分支。
func TestBuildFallbackAndBlock(t *testing.T) {
	g := Build(Input{
		Judged: []JudgedEvent{
			{DecisionID: "d1", At: at(1), Method: "GET", Path: "/a", Action: ActionMirage, Executed: "origin_fallback", Backend: "ssh-01"},
			{DecisionID: "d2", At: at(2), Method: "GET", Path: "/b", Action: ActionBlock, Executed: "block", Status: 403},
		},
		Decisions: []DecisionEvent{
			{DecisionID: "d1", At: at(1), Action: ActionMirage, Severity: "none", Score: 0.5},
			{DecisionID: "d2", At: at(2), Action: ActionBlock, Severity: "high", Score: 0.99},
		},
	}, 0.9)

	if g.Totals.Fallback != 1 || g.Totals.Blocked != 1 {
		t.Fatalf("应有 1 条回落 + 1 条拦截（实际 %+v）", g.Totals)
	}
	if g.Totals.Alerts != 1 {
		t.Fatalf("severity=high 与 block 应计 1 条真实告警（实际 %+v）", g.Totals)
	}
	if !hasEdge(g, nodeFallbackForTest(), NodeOrigin) || !hasEdge(g, NodeJudge, NodeBlock) {
		t.Fatal("应有回落边与拦截边")
	}
}

// TestBuildWithoutCoreDecision 覆盖「白名单 / 缓存 / 判定失败」这三种**没有核心判定事件**的请求：
// 图仍要画出来（不能因为缺一半数据就丢记录）。
func TestBuildWithoutCoreDecision(t *testing.T) {
	g := Build(Input{
		Judged: []JudgedEvent{
			{DecisionID: "w1", At: at(1), Method: "GET", Path: "/healthz", Executed: "whitelist"},
			{DecisionID: "c1", At: at(2), Method: "GET", Path: "/p", Executed: "cache"},
			{DecisionID: "f1", At: at(3), Method: "POST", Path: "/x", Executed: "failopen", DecisionError: "DeadlineExceeded"},
		},
	}, 0.9)

	if g.Totals.Requests != 3 || g.Totals.Unjudged != 2 || g.Totals.FailOpen != 1 {
		t.Fatalf("三条请求都应入图，且白名单/缓存计未判定（实际 %+v）", g.Totals)
	}
	if !hasEdge(g, NodeAdapter, NodeWhitelist) || !hasEdge(g, NodeAdapter, NodeCache) || !hasEdge(g, NodeAdapter, NodeFailOpen) {
		t.Fatal("三条分支边都应存在")
	}
	found := false
	for _, n := range g.Nodes {
		if n.ID == NodeFailOpen && n.Note != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("判定失败节点应带上失败原因（否则看不到为什么没判）")
	}
}

func TestBuildEmptyInput(t *testing.T) {
	g := Build(Input{}, 0.9)
	if g.Totals.Requests != 0 || len(g.Edges) != 0 {
		t.Fatalf("空输入应得到空图（实际 %+v / %d 条边）", g.Totals, len(g.Edges))
	}
	if len(g.Notes) == 0 {
		t.Fatal("即使没有流量也要给出说明（页面不能是一片空白）")
	}
}

func hasEdge(g Graph, from, to string) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}

func nodeFallbackForTest() string { return "branch:fallback" }

// TestJudgedEventUnmarshalKeepsDecisionID 守住"图能拿到判定 id"这条链路。
//
// 事故背景：JudgedEvent.DecisionID 曾是 `json:"-"`（id 取自事件信封），后来适配器把
// decision_id 放进载荷、事件 id 改为逐请求唯一 —— 标签没同步，于是**图上所有"意图"整列缺失**
// （join 落空、分值恒 0），而接口本身不报错。这个用例就是那条防线的锚点。
func TestJudgedEventUnmarshalKeepsDecisionID(t *testing.T) {
	payload := []byte(`{"decision_id":"d-9f3c1a2b","method":"GET","path":"/.git/config","executed":"mirage","status":200}`)
	var j JudgedEvent
	if err := json.Unmarshal(payload, &j); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	if j.DecisionID != "d-9f3c1a2b" {
		t.Fatalf("decision_id 丢了（标签可能还是 json:\"-\"）：%q", j.DecisionID)
	}
	if j.Executed != "mirage" || j.Path != "/.git/config" {
		t.Fatalf("其余字段也不对：%+v", j)
	}
	// 并且 join 必须真的成功：有配对判定时，图上的分值/信号/意图都要出现。
	g := BuildRequests(Input{
		Judged:    []JudgedEvent{j},
		Decisions: []DecisionEvent{{DecisionID: "d-9f3c1a2b", Action: ActionMirage, Score: 0.9, Signals: []string{"ua-headless"}}},
	}, 0.9)
	if len(g) != 1 || g[0].Action != ActionMirage || g[0].Score != 0.9 {
		t.Fatalf("join 失败：%+v", g)
	}
}

// TestDecisionPayloadUsesDesignTerms 用**真实载荷形状**（snake_case + 设计术语 action）验证图：
// 决策标签要说"改道"，block 要计真实告警，影子模式判断要正确。
//
// 事故背景：常量曾用枚举名（ACTION_MIRAGE），而载荷是 route_mirage ⇒ 标签恒"放行"、告警恒 0。
func TestDecisionPayloadUsesDesignTerms(t *testing.T) {
	judged := []byte(`{"decision_id":"d1","method":"GET","path":"/.git/config","executed":"origin","shadow":true}`)
	decision := []byte(`{"decision_id":"d1","action":"route_mirage","severity":"none","score":0.9,"signals":["ua-headless"],"method":"GET","path":"/.git/config","source_ip":"203.0.113.9"}`)
	var j JudgedEvent
	var d DecisionEvent
	if err := json.Unmarshal(judged, &j); err != nil {
		t.Fatalf("judged: %v", err)
	}
	if err := json.Unmarshal(decision, &d); err != nil {
		t.Fatalf("decision: %v", err)
	}
	g := BuildRequests(Input{Judged: []JudgedEvent{j}, Decisions: []DecisionEvent{d}}, 0.9)
	if len(g) != 1 {
		t.Fatalf("应有一条链路：%+v", g)
	}
	rg := g[0]
	if rg.Action != ActionMirage {
		t.Fatalf("意图应为 %s，实际 %q", ActionMirage, rg.Action)
	}
	found := false
	for _, n := range rg.Chain {
		if n.ID == "decision" {
			found = true
			if n.Value != "改道（route_mirage）" {
				t.Fatalf("决策节点应显示改道，实际 %q", n.Value)
			}
		}
	}
	if !found {
		t.Fatal("链路里应有决策节点")
	}
	if rg.RealAlert {
		t.Fatal("severity=none 且非 block ⇒ 不应计真实告警")
	}
	if !rg.HighRisk {
		t.Fatal("score=0.9 ≥ 阈值 ⇒ 应计高风险（仅显示）")
	}

	// block ⇒ 真实告警
	var blocked DecisionEvent
	if err := json.Unmarshal([]byte(`{"decision_id":"d2","action":"block","severity":"none","score":0.99}`), &blocked); err != nil {
		t.Fatalf("blocked: %v", err)
	}
	g2 := BuildRequests(Input{Judged: []JudgedEvent{{DecisionID: "d2", Executed: "block"}}, Decisions: []DecisionEvent{blocked}}, 0.9)
	if len(g2) != 1 || !g2[0].RealAlert {
		t.Fatalf("action=block 应计真实告警：%+v", g2)
	}
}

// TestFailOpenDoesNotInheritStaleDecision 守住一条反面规则：判定失败的请求**不得**继承同 id 的旧判定。
// 否则图上会出现"分值 0.90 + 落点 failopen"这种自相矛盾的组合（实测踩过）。
func TestFailOpenDoesNotInheritStaleDecision(t *testing.T) {
	g := BuildRequests(Input{
		Judged:    []JudgedEvent{{DecisionID: "d1", Executed: "failopen", DecisionError: "DeadlineExceeded"}},
		Decisions: []DecisionEvent{{DecisionID: "d1", Action: ActionMirage, Score: 0.9, Signals: []string{"ua-headless"}}},
	}, 0.9)
	if len(g) != 1 {
		t.Fatalf("应有一条链路：%+v", g)
	}
	rg := g[0]
	if rg.Score != 0 || rg.Action != "" || len(rg.Signals) != 0 {
		t.Fatalf("判定失败不该带判定值（实际 score=%v action=%q signals=%v）", rg.Score, rg.Action, rg.Signals)
	}
	if !rg.Unjudged {
		t.Fatal("判定失败应计为未判定")
	}
	for _, n := range rg.Chain {
		if n.ID == "decision" {
			t.Fatal("判定失败不应出现决策节点")
		}
	}
}

// TestInjectionHopAppearsOnlyWhenApplied 守住新增的「内容注入」跳（ADR-0023 / AR-33）：
// 只有**真的改写了**才画这一跳；没注入时如实留在 inject 字段里，不编出一跳。
func TestInjectionHopAppearsOnlyWhenApplied(t *testing.T) {
	withContent := BuildRequests(Input{Judged: []JudgedEvent{{
		DecisionID: "d1", At: at(1), Method: "GET", Path: "/api/users",
		Action: ActionMirage, Executed: "mirage", Backend: "mirage-1",
		Inject: "applied", ContentID: "c-1a2b3c4d5e6f7081",
	}}}, 0.9)
	if len(withContent) != 1 {
		t.Fatalf("应有一条链路：%+v", withContent)
	}
	rg := withContent[0]
	if rg.Inject != "applied" || rg.ContentID != "c-1a2b3c4d5e6f7081" {
		t.Fatalf("注入结果应透传到图：inject=%q content_id=%q", rg.Inject, rg.ContentID)
	}
	var hop *ChainNode
	for i := range rg.Chain {
		if rg.Chain[i].ID == "inject" {
			hop = &rg.Chain[i]
		}
	}
	if hop == nil {
		t.Fatalf("inject=applied 时应有注入跳：%+v", rg.Chain)
	}
	// 跳的三段文字必须齐全（scripts/traffic 的 --check-graph 按这四个字段核）。
	for name, value := range map[string]string{
		"label": hop.Label, "value": hop.Value, "request": hop.Request,
		"response": hop.Response, "why": hop.Why,
	} {
		if value == "" {
			t.Errorf("注入跳缺 %s（图上会显示半截）", name)
		}
	}

	// 没注入（例如开关关闭、资源没命中）⇒ 不得出现注入跳。
	noContent := BuildRequests(Input{Judged: []JudgedEvent{{
		DecisionID: "d2", At: at(1), Method: "GET", Path: "/api/users",
		Action: ActionMirage, Executed: "mirage", Backend: "mirage-1", Inject: "no_content",
	}}}, 0.9)
	for _, node := range noContent[0].Chain {
		if node.ID == "inject" {
			t.Fatalf("未注入时不得有注入跳：%+v", noContent[0].Chain)
		}
	}
}
