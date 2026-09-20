package topology

import (
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
