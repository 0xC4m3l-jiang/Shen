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
	// `query_raw` 让「编码本身就是信号」也能写规则（`query` 里已经没有编码了）。
	enc := New(stubRules{{ID: "encoded-dots", Weight: 0.4, Match: contract.Match{Field: "query_raw", Op: "contains", Value: "%2e%2e"}}})
	vEnc, err := enc.Judge(context.Background(), contract.JudgeRequest{
		DecisionID: "d-2",
		Observed: contract.Observation{
			Path:     "/download",
			Query:    "file=../etc/passwd",         // 规范化后：没有编码
			QueryRaw: "file=%2e%2e%2fetc%2fpasswd", // 原样：编码形态仍在
		},
	})
	if err != nil {
		t.Fatalf("query_raw 判定失败：%v", err)
	}
	if len(vEnc.Signals) != 1 {
		t.Fatalf("按编码形态的规则应命中 query_raw，得到 %+v", vEnc)
	}

	// 证据带上命中字段与它的值（观测面据此解释「为什么是 0.7」）。
	v := judgeQuery("file=../etc/passwd")
	if len(v.Evidence) != 1 || v.Evidence[0].Kind != "query" || v.Evidence[0].Value != "file=../etc/passwd" {
		t.Fatalf("证据应记录 query 字段的取值，得到 %+v", v.Evidence)
	}
}

// TestJudge_PathPrefixIsSegmentBoundary 断言 `path_prefix` 只命中**路径段边界**：
//
//	/.git        命中 /.git 与 /.git/config
//	/.git        不命中 /.gitignore（合法静态文件的误伤就是这里）
//	/admin       不命中 /administrator
//
// 而已有的 `prefix`（纯字符串前缀）行为**不变** —— 新增算符而不是改语义，既有部署不受影响。
func TestJudge_PathPrefixIsSegmentBoundary(t *testing.T) {
	newEngine := func(op string) *Engine {
		return New(stubRules{{ID: "r", Weight: 0.3, Match: contract.Match{Field: "path_norm", Op: op, Value: "/.git"}}})
	}
	judgePath := func(e *Engine, p string) contract.Verdict {
		t.Helper()
		v, err := e.Judge(context.Background(), contract.JudgeRequest{
			DecisionID: "d-1",
			Observed:   contract.Observation{Path: p},
		})
		if err != nil {
			t.Fatalf("判定失败：%v", err)
		}
		return v
	}

	seg := newEngine("path_prefix")
	for _, hit := range []string{"/.git", "/.git/config"} {
		if v := judgePath(seg, hit); len(v.Signals) != 1 {
			t.Errorf("path_prefix 应命中 %q，得到 %+v", hit, v)
		}
	}
	for _, miss := range []string{"/.gitignore", "/.gitlab/ci.yml"} {
		if v := judgePath(seg, miss); len(v.Signals) != 0 {
			t.Errorf("path_prefix 不应命中 %q，得到 %+v", miss, v)
		}
	}

	// 对照：`prefix` 保持纯字符串前缀语义（`/.gitignore` 依旧命中 —— 既有行为不动）。
	plain := newEngine("prefix")
	if v := judgePath(plain, "/.gitignore"); len(v.Signals) != 1 {
		t.Errorf("prefix 必须保持「纯字符串前缀」语义（不改既有匹配面），得到 %+v", v)
	}

	// 空值不命中（空前缀等于「什么都命中」，那不是规则）。
	empty := New(stubRules{{ID: "r", Weight: 0.3, Match: contract.Match{Field: "path_norm", Op: "path_prefix", Value: ""}}})
	if v := judgePath(empty, "/anything"); len(v.Signals) != 0 {
		t.Errorf("空路径前缀必须不命中，得到 %+v", v)
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
