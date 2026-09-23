package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"google.golang.org/grpc"

	"github.com/caddyserver/caddy/v2"

	judgev1 "shen/common/api/judge/v1"
	telemetryv1 "shen/common/api/telemetry/v1"
)

// ── 替身（MD-22：不依赖核心真实实例）─────────────────────────────────────────

type stubJudge struct {
	mu      sync.Mutex
	resp    *judgev1.JudgeResponse
	err     error
	delay   time.Duration
	calls   int
	lastReq *judgev1.JudgeRequest
}

func (s *stubJudge) Judge(ctx context.Context, in *judgev1.JudgeRequest, _ ...grpc.CallOption) (*judgev1.JudgeResponse, error) {
	s.mu.Lock()
	s.calls++
	s.lastReq = in
	resp, err, delay := s.resp, s.err, s.delay
	s.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err() // 模拟核心超时
		}
	}
	return resp, err
}

func (s *stubJudge) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubJudge) request() *judgev1.JudgeRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastReq
}

type stubReporter struct {
	mu     sync.Mutex
	events []*telemetryv1.TelemetryEvent
}

func (s *stubReporter) Report(_ context.Context, in *telemetryv1.TelemetryEvent, _ ...grpc.CallOption) (*telemetryv1.ReportAck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, in)
	return &telemetryv1.ReportAck{Accepted: 1}, nil
}

func (s *stubReporter) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// ── 测试脚手架 ───────────────────────────────────────────────────────────────

// fakeBackend 是一个替身后端（caddyhttp.MiddlewareHandler）：
// 回显自己的身份；err 非空时模拟「不可达」。
type fakeBackend struct {
	name string
	err  error
}

func (f fakeBackend) ServeHTTP(w http.ResponseWriter, r *http.Request, _ caddyhttp.Handler) error {
	if f.err != nil {
		return f.err
	}
	w.Header().Set("X-Backend", f.name)
	body, _ := io.ReadAll(r.Body)
	_, _ = w.Write([]byte(f.name + ":" + string(body)))
	return nil
}

// noopNext 是 Handler.ServeHTTP 的第三个参数（链上后继）。本模块是末端，next 不产生内容。
var noopNext caddyhttp.Handler = caddyhttp.HandlerFunc(func(http.ResponseWriter, *http.Request) error { return nil })

// newTestHandler 用 Config（transport-agnostic 视图）+ 替身构造一个 Handler，
// 后端用 fakeBackend（不依赖真实 reverse_proxy / 核心）。
// prefixStrings 把 netip.Prefix 列表转成 Handler 的 JSON 形态（`whitelist` 字段是 []string）。
//
// 为什么两份都要设：`whitelist` 是解析后的匹配用副本，`Whitelist` 是「配置原样」——
// helper 只设一份时，`Validate()` 之类的自检就会看到与生产不同的配置。
func prefixStrings(list []netip.Prefix) []string {
	if list == nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, p.String())
	}
	return out
}

func newTestHandler(t *testing.T, cfg Config, judge JudgeClient, report TelemetryClient) *Handler {
	t.Helper()
	if judge == nil {
		t.Fatal("judge 不能为 nil")
	}
	if cfg.DecisionTimeout == 0 {
		cfg.DecisionTimeout = 50 * time.Millisecond
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = time.Minute
	}
	if cfg.Window == 0 {
		cfg.Window = time.Minute
	}
	if cfg.ReportQueue == 0 {
		cfg.ReportQueue = 8
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	cacheCap := cfg.CacheMaxEntries
	if cacheCap <= 0 {
		cacheCap = defaultCacheMaxEntries
	}

	ctx, cancel := context.WithCancel(context.Background())
	// ⚠️ 这里必须把 Config 的每一项都透传进 Handler（方案 §11.2 的「先修 helper」）。
	// 曾经漏掉 `DecisionTimeout` / `Window`：于是单测里 `context.WithTimeout(ctx, 0)`（死线已过）
	// 与 `Truncate(0)`（时间窗形同不存在）—— 用例看着通过，跑的却不是被测参数。
	// 回归用例：`TestHandlerHarnessPropagatesConfig`。
	h := &Handler{
		Upstream:              cfg.Upstream,
		Mirage:                cfg.Mirage,
		Whitelist:             prefixStrings(cfg.Whitelist),
		DecisionTimeout:       caddy.Duration(cfg.DecisionTimeout),
		CacheTTL:              caddy.Duration(cfg.CacheTTL),
		Window:                caddy.Duration(cfg.Window),
		SessionCookie:         cfg.SessionCookie,
		MirageResponseTimeout: caddy.Duration(cfg.MirageResponseTimeout),
		Shadow:                cfg.Shadow,
		TrustXFF:              cfg.TrustXFF,
		ReportQueue:           cfg.ReportQueue,
		CacheMaxEntries:       cacheCap,

		whitelist: cfg.Whitelist,
		judge:     judge,
		report:    report,
		injector:  cfg.Injector,
		cache:     newDecisionCache(cfg.CacheTTL, cacheCap, now),
		now:       now,
		events:    make(chan *telemetryv1.TelemetryEvent, cfg.ReportQueue),
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
		origin:    fakeBackend{name: "origin"},
		mirage:    map[string]caddyhttp.MiddlewareHandler{},
	}
	for name := range cfg.Mirage {
		h.mirage[name] = fakeBackend{name: name}
	}
	if report != nil {
		go h.runReporter()
	}
	t.Cleanup(h.Close)
	return h
}

func do(h *Handler, method, target, body string, hdr map[string]string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	if err := h.ServeHTTP(w, r, noopNext); err != nil {
		// 测试里把 error 折叠成 500 便于断言（生产由 Caddy 错误路由处理）。
		w.WriteHeader(http.StatusBadGateway)
	}
	return w
}

// ── ① 白名单先于引流判定（INT-25）───────────────────────────────────────────

func TestWhitelistSkipsCore(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_MIRAGE, Backend: "hp"}}
	h := newTestHandler(t, Config{
		Upstream:  "http://127.0.0.1:9",
		Mirage:    map[string]string{"hp": "http://127.0.0.1:9"},
		Whitelist: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
	}, judge, nil)

	// 显式指定来源：httptest.NewRequest 默认给的是 192.0.2.1:1234。
	r := httptest.NewRequest(http.MethodGet, "http://svc.example/health", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()
	if err := h.ServeHTTP(w, r, noopNext); err != nil {
		t.Fatalf("白名单命中应透传：%v", err)
	}

	if got := w.Header().Get("X-Backend"); got != "origin" {
		t.Errorf("白名单命中应直接透传业务，实际到 %q", got)
	}
	if n := judge.callCount(); n != 0 {
		t.Errorf("白名单命中**必须**不调核心，实际调了 %d 次", n)
	}
}

// ── ② 影子模式永不改道（INT-11）────────────────────────────────────────────

func TestShadowNeverDiverts(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_MIRAGE, Backend: "hp"}}
	h := newTestHandler(t, Config{
		Upstream: "http://127.0.0.1:9",
		Mirage:   map[string]string{"hp": "http://127.0.0.1:9"},
		Shadow:   true,
	}, judge, nil)

	w := do(h, http.MethodGet, "http://svc.example/", "", nil)

	if got := w.Header().Get("X-Backend"); got != "origin" {
		t.Errorf("影子模式**禁止**改道，实际到 %q", got)
	}
	if n := judge.callCount(); n != 1 {
		t.Errorf("影子模式仍须照算判定（供阈值校准），实际调核心 %d 次", n)
	}
}

func TestShadowAlsoSuppressesBlock(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_BLOCK}}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9", Shadow: true}, judge, nil)

	w := do(h, http.MethodGet, "http://svc.example/", "", nil)

	if w.Code != http.StatusOK {
		t.Errorf("影子模式**禁止**拦截，实际状态码 %d", w.Code)
	}
	if got := w.Header().Get("X-Backend"); got != "origin" {
		t.Errorf("影子模式应透传业务，实际到 %q", got)
	}
}

// ── ③ 引流与回落（NI-1 / NI-5）─────────────────────────────────────────────

func TestMirageRoutesToNamedBackend(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_MIRAGE, Backend: "hp"}}
	h := newTestHandler(t, Config{
		Upstream: "http://127.0.0.1:9",
		Mirage:   map[string]string{"hp": "http://127.0.0.1:9"},
	}, judge, nil)

	w := do(h, http.MethodGet, "http://svc.example/", "", nil)

	if got := w.Header().Get("X-Backend"); got != "hp" {
		t.Errorf("route_mirage 应改道到 hp，实际到 %q", got)
	}
}

func TestUnknownBackendFallsBackToOrigin(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_MIRAGE, Backend: "没这个后端"}}
	h := newTestHandler(t, Config{
		Upstream: "http://127.0.0.1:9",
		Mirage:   map[string]string{"hp": "http://127.0.0.1:9"},
	}, judge, nil)

	w := do(h, http.MethodGet, "http://svc.example/", "", nil)

	if got := w.Header().Get("X-Backend"); got != "origin" {
		t.Errorf("后端名查不到必须回落业务（NI-5），实际到 %q", got)
	}
}

func TestMirageBackendDownFallsBackToOrigin(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_MIRAGE, Backend: "hp"}}
	h := newTestHandler(t, Config{
		Upstream: "http://127.0.0.1:9",
		Mirage:   map[string]string{"hp": "http://127.0.0.1:9"},
	}, judge, nil)

	// 把 hp 换成「不可达」的后端，模拟引流后端挂了。
	h.mirage["hp"] = fakeBackend{name: "hp", err: errors.New("connection refused")}

	w := do(h, http.MethodGet, "http://svc.example/", "", nil)

	if got := w.Header().Get("X-Backend"); got != "origin" {
		t.Errorf("引流后端不可达**必须**回落业务（NI-1），实际到 %q（状态码 %d）", got, w.Code)
	}
}

// ── ④ 核心失败的三种回落（NI-3 / NI-4 / NI-5）───────────────────────────────

func TestCoreErrorFallsBackToOrigin(t *testing.T) {
	judge := &stubJudge{err: context.DeadlineExceeded}
	h := newTestHandler(t, Config{
		Upstream: "http://127.0.0.1:9",
		Mirage:   map[string]string{"hp": "http://127.0.0.1:9"},
	}, judge, nil)

	w := do(h, http.MethodGet, "http://svc.example/", "", nil)

	if got := w.Header().Get("X-Backend"); got != "origin" {
		t.Errorf("核心失败必须放行（NI-3），实际到 %q", got)
	}
}

func TestCoreTimeoutFallsBackToOrigin(t *testing.T) {
	judge := &stubJudge{
		resp:  &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_MIRAGE, Backend: "hp"},
		delay: time.Second, // 远超 DecisionTimeout
	}
	h := newTestHandler(t, Config{
		Upstream:        "http://127.0.0.1:9",
		Mirage:          map[string]string{"hp": "http://127.0.0.1:9"},
		DecisionTimeout: 20 * time.Millisecond,
	}, judge, nil)

	start := time.Now()
	w := do(h, http.MethodGet, "http://svc.example/", "", nil)
	elapsed := time.Since(start)

	if got := w.Header().Get("X-Backend"); got != "origin" {
		t.Errorf("核心超时必须放行（NI-4），实际到 %q", got)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("硬超时没有生效：耗时 %v", elapsed)
	}
}

func TestUnspecifiedActionFallsBackToOrigin(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_UNSPECIFIED}}
	h := newTestHandler(t, Config{
		Upstream: "http://127.0.0.1:9",
		Mirage:   map[string]string{"hp": "http://127.0.0.1:9"},
	}, judge, nil)

	w := do(h, http.MethodGet, "http://svc.example/", "", nil)

	if got := w.Header().Get("X-Backend"); got != "origin" {
		t.Errorf("未识别的取值必须回落 route_origin（NI-5），实际到 %q", got)
	}
}

// ── ⑤ 拦截（仅在非影子模式）────────────────────────────────────────────────

func TestBlockReturnsForbidden(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_BLOCK}}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9", Shadow: false}, judge, nil)

	w := do(h, http.MethodGet, "http://svc.example/", "", nil)

	if w.Code != http.StatusForbidden {
		t.Errorf("block 应返回 403，实际 %d", w.Code)
	}
}

// ── ⑥ 判定缓存（AR-6 第 2 件事）─────────────────────────────────────────────

func TestCacheAvoidsSecondCoreCall(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, judge, nil)

	for i := 0; i < 3; i++ {
		do(h, http.MethodGet, "http://svc.example/same", "", nil)
	}

	if n := judge.callCount(); n != 1 {
		t.Errorf("同一 decision_id 应只调核心一次，实际 %d 次", n)
	}
}

// ── ⑦ decision_id 派生（ST-10）─────────────────────────────────────────────

func TestDecisionIDStableWithinWindowAndDiffersAcross(t *testing.T) {
	base := time.Unix(1700000000, 0)
	clock := base

	// 会话分量是**最小身份值**（不是整段 Cookie 头）—— 见 `sessionHint` 与方案 §5.2。
	id1 := decisionID("sid-1", "203.0.113.7", "/a", time.Minute, clock)
	if id2 := decisionID("sid-1", "203.0.113.7", "/a", time.Minute, clock); id1 != id2 {
		t.Errorf("同一窗口内同请求必须得到同一 ID（ST-10）：%s vs %s", id1, id2)
	}

	clock = base.Add(2 * time.Minute) // 跨窗口
	if id3 := decisionID("sid-1", "203.0.113.7", "/a", time.Minute, clock); id3 == id1 {
		t.Errorf("跨时间窗必须得到不同 ID（ST-10），实际仍是 %s", id3)
	}
}

func TestDecisionIDSeparatesByPath(t *testing.T) {
	now := time.Now()
	a := decisionID("sid-1", "203.0.113.7", "/a", time.Minute, now)
	b := decisionID("sid-1", "203.0.113.7", "/b", time.Minute, now)
	if a == b {
		t.Error("不同路径必须得到不同 decision_id")
	}
	// 会话变了也要变（否则「换会话」不会换判定身份 —— 那正是会话语义的根）。
	if c := decisionID("sid-2", "203.0.113.7", "/a", time.Minute, now); c == a {
		t.Error("不同会话必须得到不同 decision_id")
	}
}

// ── ⑧ 请求体不得被吃掉（本模块在请求路径上，读 body 就破坏转发）─────────────

func TestUpstreamReceivesIntactBody(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, judge, nil)

	const payload = "这是请求体，必须原样到达业务"
	w := do(h, http.MethodPost, "http://svc.example/upload", payload, nil)

	if body := w.Body.String(); body != "origin:"+payload {
		t.Errorf("业务侧收到的请求体被破坏：%q", body)
	}
}

// ── ⑨ 来源 IP 透传与观测（INT-23）──────────────────────────────────────────

func TestTrustXFFPutsRealClientIPInObservation(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9", TrustXFF: true}, judge, nil)

	do(h, http.MethodGet, "http://svc.example/", "", map[string]string{
		"X-Forwarded-For": "203.0.113.7, 10.0.0.1",
	})

	req := judge.request()
	if req == nil {
		t.Fatal("核心没有被调用")
	}
	if got := req.GetObserved().GetSourceIp(); got != "203.0.113.7" {
		t.Errorf("应取 XFF 第一段，实际 %q", got)
	}
	if got := req.GetObserved().GetPath(); got != "/" {
		t.Errorf("观测里的路径不对：%q", got)
	}
}

func TestXFFIgnoredWhenNotTrusted(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9", TrustXFF: false}, judge, nil)

	do(h, http.MethodGet, "http://svc.example/", "", map[string]string{
		"X-Forwarded-For": "203.0.113.7",
	})

	if got := judge.request().GetObserved().GetSourceIp(); got == "203.0.113.7" {
		t.Error("不信任 XFF 时不得采信它")
	}
}

// ── ⑩ 遥测上报（AR-6 第 4 件事）───────────────────────────────────────────

func TestReportsJudgedEvent(t *testing.T) {
	rep := &stubReporter{}
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, judge, rep)

	do(h, http.MethodGet, "http://svc.example/", "", nil)

	deadline := time.Now().Add(time.Second)
	for rep.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if rep.count() != 1 {
		t.Fatalf("应上报 1 条事件，实际 %d 条", rep.count())
	}
}

// ── ⑪ 配置校验：非法地址 / 缺字段必须构造失败，而不是启动后才发现 ─────────

func TestUpstreamAddrRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"空地址":         "",
		"无 scheme":    "127.0.0.1:9000",
		"无 host":      "http://",
		"不支持的 scheme": "ftp://127.0.0.1:9000",
	}
	for name, raw := range cases {
		if _, _, err := upstreamAddr(raw); err == nil {
			t.Errorf("%s（%q）：应当解析失败", name, raw)
		}
	}
}

func TestValidateRejectsInvalidConfig(t *testing.T) {
	cases := map[string]*Handler{
		"缺上游":     {DecisionTimeout: 1, CacheTTL: 1, Window: 1},
		"超时为 0":   {Upstream: "http://127.0.0.1:9", CacheTTL: 1, Window: 1},
		"TTL 为 0": {Upstream: "http://127.0.0.1:9", DecisionTimeout: 1, Window: 1},
		"窗口为 0":   {Upstream: "http://127.0.0.1:9", DecisionTimeout: 1, CacheTTL: 1},
	}
	for name, h := range cases {
		if err := h.Validate(); err == nil {
			t.Errorf("%s：应当校验失败", name)
		}
	}
}

// ── ⑫ 缓存容量上限（MD-10）────────────────────────────────────────────────

func TestCacheRespectsCapacityCap(t *testing.T) {
	c := newDecisionCache(time.Minute, 2, time.Now)
	c.put("a", judgev1.Action_ACTION_ORIGIN, "")
	c.put("b", judgev1.Action_ACTION_ORIGIN, "")
	c.put("c", judgev1.Action_ACTION_ORIGIN, "") // 触发满则清空
	c.mu.Lock()
	n := len(c.m)
	c.mu.Unlock()
	if n > 2 {
		t.Errorf("缓存容量上限失效：cap=2 但条目数 %d", n)
	}
}

// ── ⑬ 注入 transport（只改引流侧，INT-8 / ST-5）────────────────────────────

// stubInjector 在 HTML 体末尾追加一个标记。
type stubInjector struct{ marker string }

func (s *stubInjector) Inject(ct string, body []byte) ([]byte, bool) {
	if !strings.Contains(ct, "text/html") {
		return body, false
	}
	return append(append([]byte(nil), body...), []byte(s.marker)...), true
}

// roundTripFunc 把函数变成 http.RoundTripper。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func htmlResp(body string) *http.Response {
	return &http.Response{
		Header:        http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

// injectingTransportFor 造一个挂在给定注入器上的注入 transport。
//
// Handler 只用来提供「当前注入器」——transport 现在按请求读它，
// 这样策略面下发的规则可以热变更（见 handler.go 的 currentInjector）。
func injectingTransportFor(base http.RoundTripper, inj Injector) *injectingTransport {
	return &injectingTransport{src: &Handler{injector: inj}, base: base}
}

func TestInjectingTransportInjectsHTML(t *testing.T) {
	tr := injectingTransportFor(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResp("<html><body>hi</body></html>"), nil
	}), &stubInjector{marker: "<!--DECOY-->"})

	resp, err := tr.RoundTrip(httptest.NewRequest(http.MethodGet, "http://x/", nil))
	if err != nil {
		t.Fatalf("RoundTrip 失败：%v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<!--DECOY-->") {
		t.Errorf("引流侧 HTML 响应应注入诱饵，得到 %q", body)
	}
	if !strings.Contains(string(body), "<body>hi</body>") {
		t.Errorf("注入必须只增不改，得到 %q", body)
	}
	if resp.Header.Get("Content-Length") == "" {
		t.Error("改写后必须重设 Content-Length")
	}
}

func TestInjectingTransportLeavesNonHTML(t *testing.T) {
	tr := injectingTransportFor(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			Header: http.Header{"Content-Type": []string{"application/json"}},
			Body:   io.NopCloser(strings.NewReader(`{"a":1}`)),
		}, nil
	}), &stubInjector{marker: "<!--DECOY-->"})

	resp, err := tr.RoundTrip(httptest.NewRequest(http.MethodGet, "http://x/", nil))
	if err != nil {
		t.Fatalf("RoundTrip 失败：%v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if body := string(body); body != `{"a":1}` {
		t.Errorf("非 HTML 响应必须原样返回，得到 %q", body)
	}
}

func TestInjectingTransportSkipsCompressed(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte("<html><body>hi</body></html>"))
	_ = zw.Close()

	tr := injectingTransportFor(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			Header: http.Header{
				"Content-Type":     []string{"text/html; charset=utf-8"},
				"Content-Encoding": []string{"gzip"},
			},
			Body: io.NopCloser(bytes.NewReader(buf.Bytes())),
		}, nil
	}), &stubInjector{marker: "<!--DECOY-->"})

	resp, err := tr.RoundTrip(httptest.NewRequest(http.MethodGet, "http://x/", nil))
	if err != nil {
		t.Fatalf("RoundTrip 失败：%v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("压缩响应必须仍是合法 gzip：%v", err)
	}
	plain, _ := io.ReadAll(zr)
	if strings.Contains(string(plain), "<!--DECOY-->") {
		t.Error("压缩响应**禁止**注入（会损坏编码）")
	}
}

func TestInjectingTransportNoInjectorLeavesUntouched(t *testing.T) {
	tr := injectingTransportFor(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResp("<html><body>hi</body></html>"), nil
	}), nil) // 无注入器

	resp, err := tr.RoundTrip(httptest.NewRequest(http.MethodGet, "http://x/", nil))
	if err != nil {
		t.Fatalf("RoundTrip 失败：%v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if got := string(body); got != "<html><body>hi</body></html>" {
		t.Errorf("未配置注入时必须原样返回，得到 %q", got)
	}
}

// ── ⑭ trackingWriter 必须透传可选接口 ──────────────────────────────────────

func TestTrackingWriterIsHijackable(t *testing.T) {
	var w http.ResponseWriter = &trackingWriter{ResponseWriter: httptest.NewRecorder()}
	if _, ok := w.(http.Hijacker); !ok {
		t.Fatal("trackingWriter 必须实现 http.Hijacker，否则 WebSocket 等升级请求会 502")
	}
	if _, ok := w.(http.Flusher); !ok {
		t.Fatal("trackingWriter 必须实现 http.Flusher（SSE / 流式响应）")
	}
	if _, ok := w.(interface{ Unwrap() http.ResponseWriter }); !ok {
		t.Fatal("trackingWriter 必须实现 Unwrap（让 Caddy 找到底层 writer 的可选接口）")
	}
}

// ── ⑮ 对外可见面卫生（OH-2）：不得暴露代理栈指纹 ────────────────────────────

func TestHeaderSanitizerStripsProxyFingerprints(t *testing.T) {
	cases := []struct {
		name       string
		server     string
		wantServer string
	}{
		{"Caddy 的默认值必须删掉", caddyDefaultServerHeader, ""},
		{"上游自己的 Server 必须原样保留", "nginx/1.24.0", "nginx/1.24.0"},
		{"大小写不敏感", "caddy", ""},
		{"带空白的也要认出来", " Caddy ", ""},
		{"上游值恰好像我们的默认值也照删（那本来就是我们的）", "Caddy", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			rec.Header().Set("Server", tc.server)
			rec.Header().Set("Via", "1.1 Caddy") // reverse_proxy 会加这个

			s := &headerSanitizer{ResponseWriter: rec}
			s.WriteHeader(http.StatusOK)

			if got := rec.Header().Get("Server"); got != tc.wantServer {
				t.Errorf("Server 头：期望 %q，实际 %q", tc.wantServer, got)
			}
			if got := rec.Header().Get("Via"); got != "" {
				t.Errorf("Via 是我们这一跳的产物，必须删掉，实际 %q", got)
			}
		})
	}
}

// TestHeaderSanitizerAlsoCleansOnWrite：有些处理器不显式 WriteHeader，只 Write。
func TestHeaderSanitizerAlsoCleansOnWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Server", caddyDefaultServerHeader)
	rec.Header().Set("Via", "1.1 Caddy")

	s := &headerSanitizer{ResponseWriter: rec}
	if _, err := s.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if got := rec.Header().Get("Server"); got != "" {
		t.Errorf("Write 路径也必须清洗 Server，实际 %q", got)
	}
	if got := rec.Header().Get("Via"); got != "" {
		t.Errorf("Write 路径也必须清洗 Via，实际 %q", got)
	}
}

// TestHeaderSanitizerKeepsOtherHeaders：只动这两个头，别的（含业务自定义头）一律不动。
func TestHeaderSanitizerKeepsOtherHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "text/html")
	rec.Header().Set("X-Origin-Marker", "yes")
	rec.Header().Set("Server", "nginx")

	s := &headerSanitizer{ResponseWriter: rec}
	s.WriteHeader(http.StatusCreated)

	if got := rec.Header().Get("Content-Type"); got != "text/html" {
		t.Errorf("业务响应头不得被改写（INT-8）：Content-Type=%q", got)
	}
	if got := rec.Header().Get("X-Origin-Marker"); got != "yes" {
		t.Errorf("业务自定义头必须保留：%q", got)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("状态码不得被改写：%d", rec.Code)
	}
}

// TestActionName 守住"日志用设计术语"这条：三值映射不能漂。
func TestActionName(t *testing.T) {
	cases := map[judgev1.Action]string{
		judgev1.Action_ACTION_ORIGIN:      "route_origin",
		judgev1.Action_ACTION_MIRAGE:      "route_mirage",
		judgev1.Action_ACTION_BLOCK:       "block",
		judgev1.Action_ACTION_UNSPECIFIED: "route_origin",
	}
	for act, want := range cases {
		if got := actionName(act); got != want {
			t.Errorf("actionName(%v) = %q，期望 %q", act, got, want)
		}
	}
}
