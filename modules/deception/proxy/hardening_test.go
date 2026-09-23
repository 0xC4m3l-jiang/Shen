// 本文件是**第二轮代码检验（`docs/background/第二次代码检验和优化建议.md`）**里
// 那批加固项的回归：每一项都在用例名上点出编号，便于对着审查单逐条核对。
package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	judgev1 "shen/common/api/judge/v1"
	telemetryv1 "shen/common/api/telemetry/v1"
)

// ── N1：快照必须一次发布完（不得 Store 之后原地补字段）──────────────────────

// TestApplyEdgePolicyPublishesChecksumAtomically 断言**任何时刻**读到的快照都带 checksum。
//
// 旧实现先 `Store(&remoteState{checksum: ""})` 再 `cur.checksum = ...` 原地写：并发读能观察到
// 「version 已经变了、checksum 还是空」的中间态，而 checksum 是缓存键的一部分（FIX-2）。
func TestApplyEdgePolicyPublishesChecksumAtomically(t *testing.T) {
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, &stubJudge{}, &stubReporter{})
	h.buildRemote = func(name, _ string) (caddyhttp.MiddlewareHandler, error) { return fakeBackend{name: name}, nil }

	raw := decoyPolicyFixture(t, 7,
		[]policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}}, nil)
	if err := h.applyEdgePolicy(context.Background(), raw, "sum-7"); err != nil {
		t.Fatal(err)
	}
	st := h.remotePolicy()
	if st.checksum != "sum-7" {
		t.Fatalf("checksum 必须随发布一起可见，实际 %q", st.checksum)
	}
	if got := h.policyRevision(st); got != "sum-7" {
		t.Fatalf("缓存键的策略维度必须立刻是新 checksum，实际 %q", got)
	}
}

// TestPolicySnapshotReadsAreCoherent 用 `-race` 压并发读：发布期间不得出现撕裂快照。
func TestPolicySnapshotReadsAreCoherent(t *testing.T) {
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, &stubJudge{}, &stubReporter{})
	h.buildRemote = func(name, _ string) (caddyhttp.MiddlewareHandler, error) { return fakeBackend{name: name}, nil }
	backends := []policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	bad := make(chan string, 8)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				st := h.remotePolicy()
				if st == nil {
					continue
				}
				// 不变量：发布过的快照**必须**同时具备 version 与 checksum。
				if st.version == 0 || st.checksum == "" {
					select {
					case bad <- "读到半成品快照：version/checksum 不齐":
					default:
					}
					return
				}
				b, ok := st.backends["hp"]
				if ok && b == nil {
					select {
					case bad <- "后端表里有 nil 项":
					default:
					}
					return
				}
			}
		}()
	}
	for v := uint64(1); v <= 40; v++ {
		raw := decoyPolicyFixture(t, v, backends, nil)
		if err := h.applyEdgePolicy(context.Background(), raw, "sum"); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	close(bad)
	if msg := <-bad; msg != "" {
		t.Fatal(msg)
	}
}

// TestRequestPinsOneSnapshot 断言**一个请求只用一份快照**（N1）。
//
// 证明方式：请发请求前先钉住旧快照 A，随后把生效策略换成 B；请求上下文里的钉子
// 必须是 A（注入规则也走 A），而 `remotePolicy()` 已经是 B。
func TestRequestPinsOneSnapshot(t *testing.T) {
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, &stubJudge{}, &stubReporter{})
	h.injector = &stubInjector{marker: "<!--LOCAL-->"}
	h.buildRemote = func(name, _ string) (caddyhttp.MiddlewareHandler, error) { return fakeBackend{name: name}, nil }
	backends := []policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}}
	route := []policyDecoy{{ID: "admin", Path: "/admin", Backend: "hp"}}

	// A：诱饵路由在。
	if err := h.applyEdgePolicy(context.Background(), decoyPolicyFixture(t, 1, backends, route), "sum-a"); err != nil {
		t.Fatal(err)
	}
	stA := h.remotePolicy()

	// B：换成没有诱饵路由的一版（同一个 checksum 参数位另给值）。
	if err := h.applyEdgePolicy(context.Background(), decoyPolicyFixture(t, 2, backends, nil), "sum-b"); err != nil {
		t.Fatal(err)
	}
	if h.remotePolicy().checksum != "sum-b" {
		t.Fatal("新策略应已生效")
	}

	req := httptest.NewRequest(http.MethodGet, "http://svc.example/admin/", nil)
	req = req.WithContext(withPolicySnapshot(req.Context(), stA))
	if got := h.policySnapshotOf(req); got != stA {
		t.Fatal("请求上钉定的快照必须是 A（后续换成 B 不得影响本请求）")
	}
	// 直接后果：同一个请求里，诱饵匹配与白名单/后端表都从 A 看。
	// A 里 /admin 是**活路由**；B 里它只剩搜索碑（撤销保护，N6）—— 两者确实不同。
	if route, ok := h.matchDecoy(h.policySnapshotOf(req), req); !ok || route.revoked {
		t.Fatalf("本请求应用 A 的诱饵路由表（A 里 /admin 是活路由），实际 ok=%v revoked=%v", ok, route.revoked)
	}
	if n := len(h.remotePolicy().decoys); n != 0 {
		t.Fatalf("当前生效快照 B 里不应有活路由，实际 %d 条", n)
	}
	if got := h.policySnapshotOf(httptest.NewRequest(http.MethodGet, "http://svc.example/", nil)); got != h.remotePolicy() {
		t.Fatal("没有钉子的请求应回落到当前生效快照")
	}
}

// ── N2：只有成功的判定才进正常缓存；故障走独立短预算的降级缓存 ──────────────

func TestDecisionCacheDegradedBudgetIsSeparate(t *testing.T) {
	now := time.Now()
	c := newDecisionCache(time.Minute, 8, func() time.Time { return now })
	c.degradedTTL = 2 * time.Second

	c.put("ok", judgev1.Action_ACTION_MIRAGE, "hp")
	c.putDegraded("bad", judgev1.Action_ACTION_ORIGIN, "")

	e1, ok1 := c.lookup("ok")
	e2, ok2 := c.lookup("bad")
	if !ok1 || !ok2 {
		t.Fatal("两条都应可命中")
	}
	if e1.degraded {
		t.Error("正常结果不得标记为降级")
	}
	if !e2.degraded {
		t.Error("降级结果必须标记为降级（否则会被当成正常放行统计）")
	}
	if got := e1.expires.Sub(now); got != time.Minute {
		t.Errorf("正常结果应用完整 TTL，实际 %s", got)
	}
	if got := e2.expires.Sub(now); got != 2*time.Second {
		t.Errorf("降级结果应用独立短预算，实际 %s", got)
	}

	// 降级预算到期 ⇒ 消失（⇒ 同键请求会重新调核心 = 恢复重判）。
	now = now.Add(3 * time.Second)
	if _, ok := c.lookup("bad"); ok {
		t.Error("降级条目到期后必须失效（恢复重判的前提）")
	}
	if _, ok := c.lookup("ok"); !ok {
		t.Error("正常条目不应受影响")
	}
}

// TestJudgeFailureCachesDegradedNotNormal 断言故障期间的行为（N2）：
// ① 不写正常缓存；② 短窗内不重复调核心（避免无界重试）；③ 窗口过后**恢复重判**。
func TestJudgeFailureCachesDegradedNotNormal(t *testing.T) {
	judge := &stubJudge{err: context.DeadlineExceeded}
	report := &stubReporter{}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, judge, report)
	h.cache.degradedTTL = 3 * time.Second
	base := time.Now()
	clock := base
	// 注意：缓存有自己的时钟缝（`decisionCache.now`），必须与 h.now 一起换 ——
	// 否则「时间推进」测试只是在骗过 handler，缓存仍按真实时钟判定。
	h.now = func() time.Time { return clock }
	h.cache.now = func() time.Time { return clock }

	first := do(h, http.MethodGet, "http://svc.example/a", "", nil)
	if first.Code != http.StatusOK {
		t.Fatalf("判定失败应放行到业务，实际 %d", first.Code)
	}
	if n := judge.callCount(); n != 1 {
		t.Fatalf("首次应调核心 1 次，实际 %d", n)
	}

	// 同键第二次：命中**降级缓存** ⇒ 不再调核心（故障期不无界重试），但仍记 failopen。
	second := do(h, http.MethodGet, "http://svc.example/a", "", nil)
	if n := judge.callCount(); n != 1 {
		t.Fatalf("降级缓存窗口内不应重复调核心，实际 %d 次", n)
	}
	if second.Code != http.StatusOK {
		t.Fatalf("降级缓存命中仍应放行，实际 %d", second.Code)
	}

	// 核心恢复 + 降级窗口过期 ⇒ 重新判定并采用**真实**结果。
	judge.mu.Lock()
	judge.err = nil
	judge.resp = &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_MIRAGE, Backend: "hp"}
	judge.mu.Unlock()
	h.mirage = map[string]caddyhttp.MiddlewareHandler{"hp": fakeBackend{name: "hp"}}

	clock = base.Add(4 * time.Second)
	third := do(h, http.MethodGet, "http://svc.example/a", "", nil)
	if n := judge.callCount(); n != 2 {
		t.Fatalf("降级缓存过期后必须恢复重判，实际调核心 %d 次", n)
	}
	if got := third.Header().Get("X-Backend"); got != "hp" {
		t.Fatalf("重判后应采用真实判定（改道 hp），实际落点 %q（状态 %d）", got, third.Code)
	}
}

// TestWarningFailopenIsNotCachedAsNormal 断言降级结果**不会**把后续的正常判定挡住：
// 先失败（写降级）→ 核心恢复 → 降级过期后换成成功结果（上一条已覆盖）；
// 这里补另一半：成功结果**必须**进正常缓存（否则缓存等于没生效）。
func TestSuccessfulDecisionUsesNormalCache(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, judge, &stubReporter{})
	do(h, http.MethodGet, "http://svc.example/b", "", nil)
	do(h, http.MethodGet, "http://svc.example/b", "", nil)
	if n := judge.callCount(); n != 1 {
		t.Fatalf("成功结果应进正常缓存（第二次不调核心），实际调核心 %d 次", n)
	}
}

// ── W4：幻境默认拿不到生产认证材料 ─────────────────────────────────────────

// capturingBackend 记录它收到的请求头（W4 的观测点）。
type capturingBackend struct {
	mu      sync.Mutex
	header  http.Header
	seen    int
	session string
}

func (c *capturingBackend) ServeHTTP(w http.ResponseWriter, r *http.Request, _ caddyhttp.Handler) error {
	c.mu.Lock()
	c.header = r.Header.Clone()
	c.session = r.Header.Get("X-Seen-Session")
	c.seen++
	c.mu.Unlock()
	w.Header().Set("X-Backend", "captured")
	return nil
}

func TestDecoyDeliveryStripsProductCredentials(t *testing.T) {
	judge := &stubJudge{}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9", SessionCookie: "sid"}, judge, &stubReporter{})
	cap := &capturingBackend{}
	h.buildRemote = func(string, string) (caddyhttp.MiddlewareHandler, error) { return cap, nil }

	raw := decoyPolicyFixture(t, 1,
		[]policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}},
		[]policyDecoy{{ID: "admin", Path: "/admin", Backend: "hp"}})
	if err := h.applyEdgePolicy(context.Background(), raw, "s"); err != nil {
		t.Fatal(err)
	}

	w := do(h, http.MethodGet, "http://svc.example/admin/", "", map[string]string{
		"Authorization": "Bearer production-token",
		"Cookie":        "sid=business-session; atlas_session=decoy-session",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("诱饵应成功投递，实际 %d", w.Code)
	}
	cap.mu.Lock()
	hdr := cap.header
	cap.mu.Unlock()
	if got := hdr.Get("Authorization"); got != "" {
		t.Errorf("幻境不得收到 Authorization，实际 %q", got)
	}
	cookie := hdr.Get("Cookie")
	if strings.Contains(cookie, "business-session") {
		t.Errorf("幻境不得收到业务会话 cookie，实际 %q", cookie)
	}
	if !strings.Contains(cookie, "atlas_session=decoy-session") {
		t.Errorf("幻境自己的 cookie 必须保留（那是诱饵体验的一部分），实际 %q", cookie)
	}
}

func TestForwardCredentialsKeepsThemWhenExplicitlyEnabled(t *testing.T) {
	judge := &stubJudge{}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9", SessionCookie: "sid"}, judge, &stubReporter{})
	h.ForwardCredentials = true
	cap := &capturingBackend{}
	h.buildRemote = func(string, string) (caddyhttp.MiddlewareHandler, error) { return cap, nil }

	raw := decoyPolicyFixture(t, 1,
		[]policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}},
		[]policyDecoy{{ID: "admin", Path: "/admin", Backend: "hp"}})
	if err := h.applyEdgePolicy(context.Background(), raw, "s"); err != nil {
		t.Fatal(err)
	}
	do(h, http.MethodGet, "http://svc.example/admin/", "", map[string]string{
		"Authorization": "Bearer production-token", "Cookie": "sid=business-session",
	})
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.header.Get("Authorization") == "" || !strings.Contains(cap.header.Get("Cookie"), "business-session") {
		t.Fatal("显式打开 ForwardCredentials 时应原样转发（这是运维的明确选择）")
	}
}

// ── W8：投递结果必须可与「匹配到」区分 ─────────────────────────────────────

func payloadOf(t *testing.T, r *stubReporter, i int) map[string]any {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) <= i {
		t.Fatalf("期望至少 %d 条事件，实际 %d", i+1, len(r.events))
	}
	var m map[string]any
	if err := json.Unmarshal(r.events[i].Payload, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDecoyResultDistinguishesDeliveryOutcomes(t *testing.T) {
	judge := &stubJudge{}
	report := &stubReporter{}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, judge, report)
	// 构造失败 ⇒ 该后端在快照里没有可用项（名字仍被 declared 记住）⇒「不可用」分支。
	h.buildRemote = func(string, string) (caddyhttp.MiddlewareHandler, error) {
		return nil, errBackendUnavailable
	}
	h.mirage = map[string]caddyhttp.MiddlewareHandler{}

	raw := decoyPolicyFixture(t, 1,
		[]policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}},
		[]policyDecoy{{ID: "admin", Path: "/admin", Backend: "hp"}})
	if err := h.applyEdgePolicy(context.Background(), raw, "s"); err != nil {
		t.Fatal(err)
	}

	// 后端不可用：executed 仍是 decoy（契约值），但 delivery_result 必须点出真因。
	w := do(h, http.MethodGet, "http://svc.example/admin/", "", nil)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("后端不可用应固定 502，实际 %d", w.Code)
	}
	waitEvents(t, report, 1)
	p := payloadOf(t, report, 0)
	if p["executed"] != "decoy" || p["delivery_result"] != "backend_unavailable" {
		t.Fatalf("executed=%v delivery_result=%v，期望 decoy/backend_unavailable", p["executed"], p["delivery_result"])
	}
	if int(p["status"].(float64)) != http.StatusBadGateway {
		t.Fatalf("事件里的最终状态必须是真实返回码，实际 %v", p["status"])
	}

	// 后端可用 ⇒ delivered（要**重新下发**一版，快照里的后端表才会重建）。
	h.buildRemote = func(name, _ string) (caddyhttp.MiddlewareHandler, error) { return fakeBackend{name: name}, nil }
	if err := h.applyEdgePolicy(context.Background(), decoyPolicyFixture(t, 2,
		[]policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}},
		[]policyDecoy{{ID: "admin", Path: "/admin", Backend: "hp"}}), "s1b"); err != nil {
		t.Fatal(err)
	}
	if w := do(h, http.MethodGet, "http://svc.example/admin/", "", nil); w.Code != http.StatusOK {
		t.Fatalf("后端可用应 200，实际 %d", w.Code)
	}
	waitEvents(t, report, 2)
	if p := payloadOf(t, report, 1); p["delivery_result"] != "delivered" {
		t.Fatalf("delivery_result=%v，期望 delivered", p["delivery_result"])
	}

	// 撤销后（租约内）⇒ tombstoned。
	if err := h.applyEdgePolicy(context.Background(), decoyPolicyFixture(t, 3,
		[]policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}}, nil), "s2"); err != nil {
		t.Fatal(err)
	}
	if w := do(h, http.MethodGet, "http://svc.example/admin/", "", nil); w.Code != http.StatusBadGateway {
		t.Fatalf("租约内应 502，实际 %d", w.Code)
	}
	waitEvents(t, report, 3)
	if p := payloadOf(t, report, 2); p["delivery_result"] != "tombstoned" {
		t.Fatalf("delivery_result=%v，期望 tombstoned", p["delivery_result"])
	}
}

// waitEvents 等到上报 worker 把事件写进替身（异步投递，不能靠 sleep 猜）。
func waitEvents(t *testing.T, r *stubReporter, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r.count() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等待 %d 条事件超时（实际 %d）", want, r.count())
}

// ── N10：不可注入的情形要逐类拒绝（无体 / 部分响应 / no-transform）──────────

func TestInjectableRejectsNoBodyAndNoTransform(t *testing.T) {
	html := func(code int, hdr map[string]string) *http.Response {
		h := http.Header{"Content-Type": {"text/html; charset=utf-8"}}
		for k, v := range hdr {
			h.Set(k, v)
		}
		return &http.Response{StatusCode: code, Header: h, Body: http.NoBody, ContentLength: 10}
	}
	cases := []struct {
		name string
		resp *http.Response
		want bool
	}{
		{"普通 HTML", html(http.StatusOK, nil), true},
		{"204 无体", html(http.StatusNoContent, nil), false},
		{"304 未修改", html(http.StatusNotModified, nil), false},
		{"206 部分响应", html(http.StatusPartialContent, nil), false},
		{"带 Content-Range", html(http.StatusOK, map[string]string{"Content-Range": "bytes 0-9/100"}), false},
		{"no-transform", html(http.StatusOK, map[string]string{"Cache-Control": "public, no-transform"}), false},
		{"压缩响应", html(http.StatusOK, map[string]string{"Content-Encoding": "gzip"}), false},
		{"非 HTML", html(http.StatusOK, map[string]string{"Content-Type": "application/json"}), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := injectable(c.resp); ok != c.want {
				t.Fatalf("可注入性应为 %v，实际 %v", c.want, ok)
			}
		})
	}

	head := html(http.StatusOK, nil)
	head.Request = &http.Request{Method: http.MethodHead}
	if _, ok := injectable(head); ok {
		t.Fatal("HEAD 没有正文，不得注入（否则会凭空造一个体出来）")
	}
}

// ── 工具 ───────────────────────────────────────────────────────────────────

var _ = telemetryv1.ReportAck{} // 保持 telemetry 依赖显式（替身接口来自它）
var _ = caddy.Duration(0)
var _ = policyInjectRule{}

// errBackendUnavailable 是测试用的「后端构造失败」错误。
var errBackendUnavailable = errStub("后端不可用")

// errStub 是最小的 error 替身（避免测试依赖具体错误文案）。
type errStub string

func (e errStub) Error() string { return string(e) }
