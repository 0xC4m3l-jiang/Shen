package judge

import (
	"context"
	"net/netip"
	"testing"

	"shen/common/core/internal/contract"
)

type stubRules []contract.Rule

func (s stubRules) Rules(context.Context) ([]contract.Rule, error) { return s, nil }

func reqWithUA(ua string) contract.JudgeRequest {
	return contract.JudgeRequest{
		DecisionID: "d-1",
		Observed: contract.Observation{
			SourceIP:  netip.MustParseAddr("203.0.113.7"),
			UserAgent: ua,
			Path:      "/api/me",
		},
	}
}

// TestJudge_PositiveAndNegative：每条判定规则至少一个正样本 + 一个负样本。
func TestJudge_PositiveAndNegative(t *testing.T) {
	e := New(stubRules{{
		ID:     "ua-headless",
		Weight: 0.6,
		Match:  contract.Match{Field: "user_agent", Op: "contains", Value: "HeadlessChrome"},
	}})

	// 正样本：命中
	got, err := e.Judge(context.Background(), reqWithUA("HeadlessChrome/120.0"))
	if err != nil {
		t.Fatalf("正样本返回错误: %v", err)
	}
	if len(got.Signals) != 1 || got.Score != 0.6 {
		t.Fatalf("正样本：期望 1 个信号 / 分数 0.6，得到 %+v", got)
	}

	// 负样本：不命中
	got, err = e.Judge(context.Background(), reqWithUA("Mozilla/5.0 (Macintosh)"))
	if err != nil {
		t.Fatalf("负样本返回错误: %v", err)
	}
	if len(got.Signals) != 0 || got.Score != 0 {
		t.Fatalf("负样本：期望 0 个信号 / 分数 0，得到 %+v", got)
	}
}

// TestJudge_MatchesQueryField 断言 `query` 是一个**可匹配字段**（编码形态也已解码，故同一条规则都命中）。
//
// 依据见 docs/spec/config.md §2.4：`path` 与 `query` 是两个独立字段（HTTP 语义），
// 合起来建模会让「路径穿越」与「参数注入」分不开、也说不清证据命中哪一部分。
func TestJudge_MatchesQueryField(t *testing.T) {
	e := New(stubRules{{
		ID:     "query-traversal",
		Weight: 0.7,
		Match:  contract.Match{Field: "query", Op: "contains", Value: "../"},
	}})

	judgeQuery := func(q string) contract.Verdict {
		t.Helper()
		v, err := e.Judge(context.Background(), contract.JudgeRequest{
			DecisionID: "d-1",
			Observed: contract.Observation{
				SourceIP: netip.MustParseAddr("203.0.113.7"),
				Method:   "GET",
				Path:     "/download",
				Query:    q,
			},
		})
		if err != nil {
			t.Fatalf("query=%q 判定失败：%v", q, err)
		}
		return v
	}

	// 正样本：解码后的明文形态（适配器已解一次 —— 见 proxy/mirror 的 queryOf）。
	if got := judgeQuery("file=../etc/passwd"); len(got.Signals) != 1 || got.Score != 0.7 {
		t.Fatalf("正样本：期望 1 信号 / 0.7，得到 %+v", got)
	}
	// 负样本：查询串里没有穿越（且 path 字段不参与这条规则 —— 只有 query 变才改结果）。
	if got := judgeQuery("file=report.csv"); len(got.Signals) != 0 {
		t.Fatalf("负样本：不应命中，得到 %+v", got)
	}
	// 证据带上命中字段与它的值（观测面据此解释「为什么是 0.7」）。
	v := judgeQuery("file=../etc/passwd")
	if len(v.Evidence) != 1 || v.Evidence[0].Kind != "query" || v.Evidence[0].Value != "file=../etc/passwd" {
		t.Fatalf("证据应记录 query 字段的取值，得到 %+v", v.Evidence)
	}
}

// TestJudge_ScoreCappedAtOne 断言风险分被截顶在 1。
func TestJudge_ScoreCappedAtOne(t *testing.T) {
	e := New(stubRules{
		{ID: "a", Weight: 0.8, Match: contract.Match{Field: "path", Op: "prefix", Value: "/api"}},
		{ID: "b", Weight: 0.8, Match: contract.Match{Field: "path", Op: "prefix", Value: "/api"}},
	})
	got, err := e.Judge(context.Background(), reqWithUA("x"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Score != 1 {
		t.Fatalf("期望分数截顶为 1，得到 %v", got.Score)
	}
}

// TestJudge_NilRuleSourcePanics 断言构造期不静默接受空依赖。
func TestJudge_NilRuleSourcePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("期望 New(nil) panic")
		}
	}()
	New(nil)
}
