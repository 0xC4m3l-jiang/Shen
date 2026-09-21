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
