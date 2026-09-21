package director

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"shen/common/core/internal/contract"
)

// ── 替身（MD-22：不依赖其他模块真实实例）───────────────────────────────────

type stubJudge struct {
	v   contract.Verdict
	err error
}

func (s *stubJudge) Judge(context.Context, contract.JudgeRequest) (contract.Verdict, error) {
	return s.v, s.err
}

type stubThresholds struct {
	th  contract.Thresholds
	err error
}

func (s *stubThresholds) Thresholds(context.Context) (contract.Thresholds, error) {
	return s.th, s.err
}

type stubGray struct {
	pct uint8
	err error
}

func (s *stubGray) GrayPct(context.Context) (uint8, error) { return s.pct, s.err }

// testThr 与 config.example.yaml 的默认阈值一致：mirage 0.7 / block 0.95。
var testThr = contract.Thresholds{Mirage: 0.7, Block: 0.95}

func newEngine(t *testing.T, score float64, gray uint8, blockEnabled bool) *Engine {
	t.Helper()
	e, err := New(
		&stubJudge{v: contract.Verdict{Score: score}},
		&stubThresholds{th: testThr},
		&stubGray{pct: gray},
		Config{BlockEnabled: blockEnabled},
	)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	return e
}

func decide(t *testing.T, e *Engine, id string) contract.Decision {
	t.Helper()
	d, err := e.Decide(context.Background(), contract.JudgeRequest{DecisionID: id})
	if err != nil {
		t.Fatalf("Decide 出错：%v", err)
	}
	return d
}

// ── 三值 × 阈值边界（MD-8 分支穷尽性）──────────────────────────────────────

func TestClassifyBoundaries(t *testing.T) {
	cases := []struct {
		name  string
		score float64
		block bool
		want  contract.Action
	}{
		{"低于 mirage", 0.5, false, contract.ActionOrigin},
		{"恰在 mirage", 0.7, false, contract.ActionMirage},
		{"mirage 与 block 之间", 0.8, false, contract.ActionMirage},
		{"block 开关关：高分仍改道", 0.99, false, contract.ActionMirage},
		{"block 开关开：达到 block", 0.95, true, contract.ActionBlock},
		{"block 开关开：低于 block", 0.9, true, contract.ActionMirage},
		{"零分", 0, false, contract.ActionOrigin},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEngine(t, c.score, 100, c.block)
			if got := decide(t, e, "id").Action; got != c.want {
				t.Errorf("score=%.2f block=%v：要 %v，得到 %v", c.score, c.block, c.want, got)
			}
		})
	}
}

// ── block 默认不产出（Q5）──────────────────────────────────────────────────

func TestBlockDefaultOff(t *testing.T) {
	e := newEngine(t, 1.0, 100, false)
	if got := decide(t, e, "id").Action; got != contract.ActionMirage {
		t.Errorf("block 默认关时满分数应改道，得到 %v", got)
	}
}

func TestBlockBackendEmpty(t *testing.T) {
	e := newEngine(t, 1.0, 100, true)
	d := decide(t, e, "id")
	if d.Action != contract.ActionBlock {
		t.Fatalf("要 block，得到 %v", d.Action)
	}
	if d.Backend != "" {
		t.Errorf("block 时后端名应为空，得到 %q", d.Backend)
	}
}

// ── 灰度（Q4）──────────────────────────────────────────────────────────────

func TestGrayZeroFallsBackToOrigin(t *testing.T) {
	e := newEngine(t, 1.0, 0, false)
	if got := decide(t, e, "id").Action; got != contract.ActionOrigin {
		t.Errorf("灰度 0%% 时改道应全部回落 origin，得到 %v", got)
	}
}

func TestGrayHundredAlwaysDiverts(t *testing.T) {
	e := newEngine(t, 1.0, 100, false)
	if got := decide(t, e, "id").Action; got != contract.ActionMirage {
		t.Errorf("灰度 100%% 应全改道，得到 %v", got)
	}
}

func TestGrayDeterministic(t *testing.T) {
	e := newEngine(t, 1.0, 50, false)
	first := decide(t, e, "same-decision-id").Action
	for i := 0; i < 20; i++ {
		if got := decide(t, e, "same-decision-id").Action; got != first {
			t.Fatalf("灰度必须确定性：同 decision_id 得到 %v 与 %v", first, got)
		}
	}
}

func TestGrayDoesNotAffectOrigin(t *testing.T) {
	// 低分在灰度 0/50/100 下都应是 origin —— 灰度只作用于「本会改道」的请求。
	for _, g := range []uint8{0, 50, 100} {
		e := newEngine(t, 0.1, g, false)
		if got := decide(t, e, "id").Action; got != contract.ActionOrigin {
			t.Errorf("灰度 %d：低分应 origin，得到 %v", g, got)
		}
	}
}

// ── NI-5：未识别 / 非法 → 放行 ─────────────────────────────────────────────

func TestNegativeScoreFallsBackToOrigin(t *testing.T) {
	e := newEngine(t, -1, 100, false)
	if got := decide(t, e, "id").Action; got != contract.ActionOrigin {
		t.Errorf("负分应回落 origin（NI-5），得到 %v", got)
	}
}

// ── severity 恒 none（TM-13 / MD-24）──────────────────────────────────────

func TestSeverityAlwaysNone(t *testing.T) {
	for _, s := range []float64{0, 0.8, 1} {
		e := newEngine(t, s, 100, true)
		if got := decide(t, e, "id").Severity; got != contract.SeverityNone {
			t.Errorf("severity 只能是 none，得到 %v", got)
		}
	}
}

// ── 失败上抛（NI-3）────────────────────────────────────────────────────────

func TestJudgeErrorPropagates(t *testing.T) {
	e, err := New(&stubJudge{err: errors.New("boom")}, &stubThresholds{th: testThr}, &stubGray{}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, derr := e.Decide(context.Background(), contract.JudgeRequest{}); derr == nil {
		t.Error("judge 出错必须上抛")
	}
}

func TestThresholdErrorPropagates(t *testing.T) {
	e, err := New(&stubJudge{}, &stubThresholds{err: errors.New("boom")}, &stubGray{}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, derr := e.Decide(context.Background(), contract.JudgeRequest{}); derr == nil {
		t.Error("取阈值失败必须上抛")
	}
}

func TestGrayErrorPropagates(t *testing.T) {
	e, err := New(&stubJudge{v: contract.Verdict{Score: 1}}, &stubThresholds{th: testThr}, &stubGray{err: errors.New("boom")}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, derr := e.Decide(context.Background(), contract.JudgeRequest{}); derr == nil {
		t.Error("取灰度失败必须上抛")
	}
}

// ── MD-25：诱饵面 observe-only ───────────────────────────────────────────

func newDecoyEngine(t *testing.T, score float64, prefixes []string) *Engine {
	t.Helper()
	e, err := New(
		&stubJudge{v: contract.Verdict{Score: score}},
		&stubThresholds{th: testThr},
		&stubGray{pct: 100},
		Config{BlockEnabled: true, DecoyPrefixes: prefixes},
	)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	return e
}

func decidePath(t *testing.T, e *Engine, path string) contract.Decision {
	t.Helper()
	d, err := e.Decide(context.Background(), contract.JudgeRequest{
		DecisionID: "md25",
		Observed:   contract.Observation{Path: path},
	})
	if err != nil {
		t.Fatalf("Decide 失败：%v", err)
	}
	return d
}

func TestDecoyPathsNeverBlocked(t *testing.T) {
	e := newDecoyEngine(t, 1.0, []string{"/portal/", "/_bait/"})
	for _, p := range []string{"/portal/api/content", "/_bait/credential/x/login"} {
		d := decidePath(t, e, p)
		if d.Action == contract.ActionBlock {
			t.Errorf("%s：诱饵面禁止 block（MD-25：在诱饵面阻断 = 自断情报源）", p)
		}
		if d.Action != contract.ActionMirage {
			t.Errorf("%s：诱饵面高分应改道，得到 %s", p, d.Action)
		}
	}
}

func TestNonDecoyPathStillBlocks(t *testing.T) {
	e := newDecoyEngine(t, 1.0, []string{"/portal/"})
	d := decidePath(t, e, "/admin/db/dump")
	if d.Action != contract.ActionBlock {
		t.Errorf("非诱饵路径在 block 开关打开时应 block，得到 %s", d.Action)
	}
}

func TestEmptyDecoyPrefixDoesNotMatchEverything(t *testing.T) {
	// 空前缀若不丢弃，会匹配一切路径，使 block 彻底失效。
	e := newDecoyEngine(t, 1.0, []string{"", "   "})
	if got := decidePath(t, e, "/anything").Action; got != contract.ActionBlock {
		t.Errorf("空/空白前缀必须被丢弃（否则 block 失效），得到 %s", got)
	}
}

func TestDecoyPathLowScoreStaysOrigin(t *testing.T) {
	e := newDecoyEngine(t, 0.1, []string{"/portal/"})
	if got := decidePath(t, e, "/portal/api/content").Action; got != contract.ActionOrigin {
		t.Errorf("诱饵面低分仍应放行，得到 %s", got)
	}
}

// ── INT-25：白名单先于引流判定 ─────────────────────────────────────────

func newWhitelistEngine(t *testing.T, w contract.Whitelist) *Engine {
	t.Helper()
	e, err := New(
		&stubJudge{v: contract.Verdict{Score: 1.0}},
		&stubThresholds{th: testThr},
		&stubGray{pct: 100},
		Config{BlockEnabled: true, Whitelist: w},
	)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	return e
}

func TestWhitelistByUserAgentSkipsJudgement(t *testing.T) {
	e := newWhitelistEngine(t, contract.Whitelist{UserAgents: []string{"health-probe"}})
	d, err := e.Decide(context.Background(), contract.JudgeRequest{
		DecisionID: "wl",
		Observed:   contract.Observation{UserAgent: "health-probe", Path: "/anything"},
	})
	if err != nil {
		t.Fatalf("失败：%v", err)
	}
	if d.Action != contract.ActionOrigin {
		t.Errorf("白名单必须放行，得到 %s", d.Action)
	}
	if d.Verdict.Score != 0 {
		t.Errorf("白名单命中不该做判定（验证：交叉验证 Score=%.2f 说明 judge 被调了）", d.Verdict.Score)
	}
}

func TestWhitelistByCIDR(t *testing.T) {
	e := newWhitelistEngine(t, contract.Whitelist{
		SourceCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	})
	d, _ := e.Decide(context.Background(), contract.JudgeRequest{
		DecisionID: "wl-cidr",
		Observed:   contract.Observation{SourceIP: netip.MustParseAddr("10.1.2.3"), Path: "/x"},
	})
	if d.Action != contract.ActionOrigin {
		t.Errorf("内网来源必须放行（INT-25），得到 %s", d.Action)
	}
}

func TestWhitelistByPathPrefix(t *testing.T) {
	e := newWhitelistEngine(t, contract.Whitelist{PathPrefixes: []string{"/healthz"}})
	d, _ := e.Decide(context.Background(), contract.JudgeRequest{
		DecisionID: "wl-path",
		Observed:   contract.Observation{Path: "/healthz/ready"},
	})
	if d.Action != contract.ActionOrigin {
		t.Errorf("健康检查路径必须放行，得到 %s", d.Action)
	}
}

func TestNonWhitelistedStillJudged(t *testing.T) {
	e := newWhitelistEngine(t, contract.Whitelist{UserAgents: []string{"health-probe"}})
	d, _ := e.Decide(context.Background(), contract.JudgeRequest{
		DecisionID: "not-wl",
		Observed:   contract.Observation{UserAgent: "HeadlessChrome/120", Path: "/admin"},
	})
	if d.Verdict.Score == 0 {
		t.Error("非白名单必须照常判定（否则白名单形同关闭一切判定）")
	}
}

func TestEmptyWhitelistEntriesDoNotMatchEverything(t *testing.T) {
	e := newWhitelistEngine(t, contract.Whitelist{UserAgents: []string{""}, PathPrefixes: []string{""}})
	d, _ := e.Decide(context.Background(), contract.JudgeRequest{
		DecisionID: "empty-wl",
		Observed:   contract.Observation{UserAgent: "HeadlessChrome/120", Path: "/admin"},
	})
	if d.Verdict.Score == 0 {
		t.Error("空白名单项不得匹配一切（否则等于关掉判定）")
	}
}

// ── 构造校验 ───────────────────────────────────────────────────────────────

func TestNewRejectsNil(t *testing.T) {
	if _, err := New(nil, &stubThresholds{}, &stubGray{}, Config{}); err == nil {
		t.Error("judge 为 nil 应失败")
	}
	if _, err := New(&stubJudge{}, nil, &stubGray{}, Config{}); err == nil {
		t.Error("ThresholdSource 为 nil 应失败")
	}
	if _, err := New(&stubJudge{}, &stubThresholds{}, nil, Config{}); err == nil {
		t.Error("GraySource 为 nil 应失败")
	}
}

// ── 后端名（阶段 2a 固定逻辑名）────────────────────────────────────────────

func TestMirageCarriesBackendName(t *testing.T) {
	e := newEngine(t, 0.8, 100, false)
	if got := decide(t, e, "id").Backend; got != defaultBackend {
		t.Errorf("改道须带后端名 %q，得到 %q", defaultBackend, got)
	}
}

func TestOriginHasNoBackend(t *testing.T) {
	e := newEngine(t, 0.1, 100, false)
	if got := decide(t, e, "id").Backend; got != "" {
		t.Errorf("放行时后端名应为空，得到 %q", got)
	}
}
