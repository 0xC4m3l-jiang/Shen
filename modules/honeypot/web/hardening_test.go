// 本文件是**第二轮代码检验（`docs/background/第二次代码检验和优化建议.md`）**里
// Web 与治理那一批（`W1`–`W6`）的回归。
package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ── W1：cookie 作用域必须同时覆盖页面与 API（用**真 cookie jar**，不手工塞 cookie）──

func TestCookieJarLoginThenAPICallSucceeds(t *testing.T) {
	h, _ := newHandler(t, Options{})
	srv := httptest.NewServer(h)
	defer srv.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	// 不自动跟随重定向：这样能同时断言「登录返回 302」与「cookie jar 记住了它」。
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// ① 打开登录页 → ② 提交表单 → ③ **直接**调 API（不手工加任何 Cookie 头）
	if resp, err := client.Get(srv.URL + "/admin/login"); err != nil {
		t.Fatal(err)
	} else {
		_ = resp.Body.Close()
	}
	resp, err := client.PostForm(srv.URL+"/admin/login", url.Values{
		"username": {"alice"}, "password": {"x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("合成登录应 302，实际 %d", resp.StatusCode)
	}

	api, err := client.Get(srv.URL + adminPrefix + "/api/users?page=1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = api.Body.Close() }()
	if api.StatusCode != http.StatusOK {
		t.Fatalf(
			"cookie jar 登录后应能直接调用 API（W1：页面与 API 必须在同一 cookie 作用域内），实际 %d",
			api.StatusCode,
		)
	}
	// 兼容入口仍在，但**不在 cookie 的 Path 内** —— 这条断言把差异钉住（README 的说明以此为准）。
	legacy, err := client.Get(srv.URL + "/api/v1/admin/users?page=1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = legacy.Body.Close() }()
	if legacy.StatusCode != http.StatusUnauthorized {
		t.Fatalf("兼容入口不在 cookie Path 内，浏览器链路应为 401，实际 %d", legacy.StatusCode)
	}
}

func TestCookieScopeAndSecureFlag(t *testing.T) {
	h, _ := newHandler(t, Options{ScenarioID: "atlas"})
	resp := do(h, http.MethodPost, "/admin/login", url.Values{
		"username": {"alice"}, "password": {"x"},
	}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	raw := resp.Header().Get("Set-Cookie")
	if !strings.Contains(raw, "Path="+adminPrefix) {
		t.Fatalf("cookie Path 应覆盖 /admin（页面 + /admin/api/*），实际 %q", raw)
	}
	if strings.Contains(raw, "Secure") {
		t.Fatalf("默认不进 HTTPS 语境时不应带 Secure（本地复测可用），实际 %q", raw)
	}

	secured, _ := newHandler(t, Options{ScenarioID: "atlas", CookieSecure: true})
	resp = do(secured, http.MethodPost, "/admin/login", url.Values{
		"username": {"alice"}, "password": {"x"},
	}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if raw := resp.Header().Get("Set-Cookie"); !strings.Contains(raw, "Secure") {
		t.Fatalf("显式开启后必须带 Secure（HTTPS 接入时），实际 %q", raw)
	}
}

func TestLegacyAPIPathStillServesData(t *testing.T) {
	h, _ := newHandler(t, Options{})
	h.scenario.Outcome = LoginDemo
	login := do(h, http.MethodPost, "/admin/login", url.Values{
		"username": {"alice"}, "password": {"x"},
	}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	cookie := sessionCookieFrom(login)
	for _, path := range []string{"/admin/api/users", "/api/v1/admin/users"} {
		resp := do(h, http.MethodGet, path+"?page=1", "", map[string]string{"Cookie": cookie})
		if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"items"`) {
			t.Fatalf("%s 应返回合成 JSON，实际 %d %s", path, resp.Code, resp.Body.String())
		}
	}
}

// ── W2：分页边界（极端页号不得 panic、空页是空数组、翻页链接保留 page_size）─────

func TestPaginationBoundsDoNotPanicAndStayConsistent(t *testing.T) {
	h, _ := newHandler(t, Options{})
	login := do(h, http.MethodPost, "/admin/login", url.Values{
		"username": {"alice"}, "password": {"x"},
	}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	cookie := sessionCookieFrom(login)

	cases := []struct {
		name  string
		query string
	}{
		{"极大正整数", "page=9223372036854775807"},
		{"溢出字面量", "page=99999999999999999999999999"},
		{"非数字", "page=abc"},
		{"数字后带字母", "page=12abc"},
		{"负数", "page=-5"},
		{"零页", "page=0"},
		{"超上限页", "page=1000001"},
		{"极大页大小", "page_size=9223372036854775807"},
		{"页大小超场景上限", "page_size=99999"},
		{"空页大小", "page_size="},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := do(h, http.MethodGet, "/admin/api/users?"+c.query, "",
				map[string]string{"Cookie": cookie})
			if resp.Code != http.StatusOK {
				t.Fatalf("边界参数应仍返回 200（W2：先与总页数比较，不计算坏偏移），实际 %d", resp.Code)
			}
			body := resp.Body.String()
			if !strings.Contains(body, `"items"`) {
				t.Fatalf("缺少 items 字段：%s", body)
			}
			// 空页必须是**空数组**而不是 null（否则读取方分不清「空页」与「没有这条资源」）。
			if strings.Contains(body, `"items":null`) {
				t.Fatalf("空页必须是 []（不是 null）：%s", body)
			}
		})
	}
}

func TestPaginationNextLinkKeepsPageSize(t *testing.T) {
	h, _ := newHandler(t, Options{})
	login := do(h, http.MethodPost, "/admin/login", url.Values{
		"username": {"alice"}, "password": {"x"},
	}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	cookie := sessionCookieFrom(login)

	resp := do(h, http.MethodGet, "/admin/users?page=1&page_size=2", "",
		map[string]string{"Cookie": cookie})
	body := resp.Body.String()
	if !strings.Contains(body, "page_size=2") {
		t.Fatalf("翻页链接必须保留 page_size（否则每页条数会突然跳回默认值）：%s", body)
	}
	if !strings.Contains(body, "page=2") {
		t.Fatalf("首页应有下一页链接：%s", body)
	}
}

// ── W3：错误页必须完整（含公共头字段），模板坏掉时回落静态页 ────────────────────

func TestErrorPagesAreCompleteHTML(t *testing.T) {
	h, _ := newHandler(t, Options{})
	// 404 用例需要已认证（未认证时 /admin/* 会 302、/admin/api/* 会 401，那是正确行为）。
	login := do(h, http.MethodPost, "/admin/login", url.Values{
		"username": {"alice"}, "password": {"x"},
	}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	auth := map[string]string{"Cookie": sessionCookieFrom(login)}
	// 413：先放宽上限构造一次超限请求。
	big := strings.Repeat("a", int(h.maxBody)+10)
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		hdr    map[string]string
		want   int
		// json=true 的路径按 API 契约返回 JSON（`error=not_found`）而不是 HTML 页 ——
		// 「正文完整」对两种表示的要求不同：HTML 要能渲染完，JSON 要能解析出结构化的错误键。
		json bool
	}{
		{
			name: "400 表单读不出来", method: http.MethodPost, path: "/admin/login", body: "%zz",
			hdr:  map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			want: http.StatusBadRequest,
		},
		{
			name: "413 表单超限", method: http.MethodPost, path: "/admin/login", body: "username=" + big,
			hdr:  map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			want: http.StatusRequestEntityTooLarge,
		},
		{
			name: "401 登录失败", method: http.MethodPost, path: "/admin/login",
			body: url.Values{"username": {"a"}, "password": {"b"}}.Encode(),
			hdr:  map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			want: http.StatusUnauthorized,
		},
		{name: "404 未知页面", method: http.MethodGet, path: "/admin/nope", hdr: auth, want: http.StatusNotFound},
		{
			name: "404 未知接口", method: http.MethodGet, path: "/admin/api/nope",
			hdr: auth, want: http.StatusNotFound, json: true,
		},
	}
	// 401 需要 fail 语义的场景。
	h.scenario.Outcome = LoginFails
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := do(h, c.method, c.path, c.body, c.hdr)
			if resp.Code != c.want {
				t.Fatalf("状态码应为 %d，实际 %d", c.want, resp.Code)
			}
			body := resp.Body.String()
			if c.json {
				if !strings.Contains(body, `"error":"not_found"`) {
					t.Fatalf("API 错误正文应是结构化 JSON（%s），实际：%s", "error=not_found", body)
				}
				return
			}
			for _, must := range []string{"<!doctype html>", "</html>", h.scenario.Org, h.scenario.Product} {
				if !strings.Contains(body, must) {
					t.Fatalf("错误页缺少 %q（W3：公共头字段必须齐全）：%s", must, body)
				}
			}
			// 标题不能是空的「 · 」：那说明视图字段没传全。
			if strings.Contains(body, "<title> · </title>") {
				t.Fatalf("错误页标题为空（视图缺 Org/Product）：%s", body)
			}
		})
	}
}

func TestBrokenTemplateFallsBackToStaticPage(t *testing.T) {
	h, _ := newHandler(t, Options{})
	broken := templateMustFail(t)
	resp := httptest.NewRecorder()
	h.render(resp, http.StatusOK, broken, h.errViewOf("x", 500, "y"))
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("模板执行失败应回落 500，实际 %d", resp.Code)
	}
	body := resp.Body.String()
	if !strings.Contains(body, "<!doctype html>") || !strings.Contains(body, "</html>") {
		t.Fatalf("兜底页也必须是完整 HTML：%s", body)
	}
	if strings.Contains(body, "{{") {
		t.Fatalf("兜底页不得带未渲染的模板语法：%s", body)
	}
}

// ── W4：凭据卫生（r.Form 与 r.PostForm 两张表都要清）──────────────────────────

func TestLoginRequestDoesNotRetainPassword(t *testing.T) {
	h, _ := newHandler(t, Options{})
	const secret = "SECRET-PASSWORD-MUST-NOT-SURVIVE"
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(url.Values{
		"username": {"alice"}, "password": {secret},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)

	for name, values := range map[string]url.Values{"PostForm": req.PostForm, "Form": req.Form} {
		if values == nil {
			continue
		}
		if got := values.Get("password"); got != "" {
			t.Fatalf("%s 里仍留着密码（ParseForm 会同时填两张表，只清一张等于没清）", name)
		}
	}
}

// ── W5：事件必须带 At/Path/脱敏会话；慢 sink 不阻塞；队列满可观测 ──────────────

func TestEventsCarryTimePathAndScrubbedSession(t *testing.T) {
	h, sink := newHandler(t, Options{})
	login := do(h, http.MethodPost, "/admin/login", url.Values{
		"username": {"alice"}, "password": {"x"},
	}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	cookie := sessionCookieFrom(login)
	if cookie == "" {
		t.Fatal("demo 场景应下发会话 cookie")
	}
	value := strings.TrimPrefix(cookie, scratchSessionCookie+"=")
	do(h, http.MethodGet, "/admin/users", "", map[string]string{"Cookie": cookie})

	events := sink.wait(t, 2)
	kinds := map[EventKind]bool{}
	for _, ev := range events {
		kinds[ev.Kind] = true
		if ev.At.IsZero() {
			t.Errorf("事件缺 At（%+v）", ev)
		}
		if ev.Path == "" {
			t.Errorf("事件缺 Path（%+v）", ev)
		}
		if ev.Session == "" {
			t.Errorf("带会话的请求其事件应有脱敏 Session（%+v）", ev)
		}
		if ev.Session != scrubSession(value) {
			t.Errorf("Session 应是脱敏后的可关联标识，实际 %q", ev.Session)
		}
		if strings.Contains(fmt.Sprintf("%+v", ev), value) {
			t.Fatalf("事件里出现了 cookie 原值（等于把凭据抄进观测面）：%+v", ev)
		}
	}
	if !kinds[EventLoginAttempt] || !kinds[EventPageView] {
		t.Fatalf("登录与页面浏览都应产出事件（page_view 原来只在声明里存在），实际 %v", kinds)
	}
}

// slowSink 模拟慢接收方（下游观测面卡住）。
type slowSink struct {
	calls  atomic.Int64
	delay  time.Duration
	events chan Event
}

func (s *slowSink) Event(_ context.Context, ev Event) error {
	s.calls.Add(1)
	time.Sleep(s.delay)
	if s.events != nil {
		s.events <- ev
	}
	return nil
}

func TestSlowSinkDoesNotBlockRequestsAndDropsAreCounted(t *testing.T) {
	sink := &slowSink{delay: 200 * time.Millisecond}
	h := New(Options{ScenarioID: "atlas", Rand: fixedRand{7}, Events: sink, EventQueue: 1})
	defer h.Close()

	form := url.Values{"username": {"a"}, "password": {"b"}}.Encode()
	start := time.Now()
	for i := 0; i < 5; i++ {
		resp := do(h, http.MethodPost, "/admin/login", form, map[string]string{
			"Content-Type": "application/x-www-form-urlencoded"})
		if resp.Code != http.StatusFound {
			t.Fatalf("慢 sink 不得影响合成交互，实际 %d", resp.Code)
		}
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("请求被 sink 拖住了（%s）：投递必须是异步的", elapsed)
	}
	// 队列只有 1 ⇒ 必然丢（丢弃数必须可读，不能静默）。
	if h.DroppedEvents() == 0 {
		t.Fatal("队列满时必须计数丢弃（DroppedEvents）")
	}
}

// ── W6：登录配额 · 会话逐出 · 优雅退出 ───────────────────────────────────────

func TestLoginRateLimitRejectsWithoutSession(t *testing.T) {
	// `R13` 之后，**按来源限流要求先声明可信前跳**：测试客户端的对端是 192.0.2.1，
	// 只有它在可信名单里，下面的 X-Forwarded-For 才会被采信（否则一律按对端计来源，
	// 两个"不同来源"会落进同一个桶 —— 那正是修复要防的伪造场景）。
	h, sink := newHandler(t, Options{
		LoginBurst:  2,
		LoginWindow: time.Minute,
		TrustedProxies: []netip.Prefix{
			netip.MustParsePrefix("192.0.2.1/32"),
		},
	})
	form := url.Values{"username": {"a"}, "password": {"b"}}.Encode()
	hdr := map[string]string{
		"Content-Type":    "application/x-www-form-urlencoded",
		"X-Forwarded-For": "203.0.113.7",
	}
	for i := 0; i < 2; i++ {
		if resp := do(h, http.MethodPost, "/admin/login", form, hdr); resp.Code != http.StatusFound {
			t.Fatalf("配额内第 %d 次应 302，实际 %d", i+1, resp.Code)
		}
	}
	third := do(h, http.MethodPost, "/admin/login", form, hdr)
	if third.Code != http.StatusTooManyRequests {
		t.Fatalf("超配额应 429，实际 %d", third.Code)
	}
	if sessionCookieFrom(third) != "" {
		t.Fatal("被配额拒绝时不得下发会话 cookie")
	}
	if !strings.Contains(third.Body.String(), "</html>") {
		t.Fatalf("429 也应是完整合成错误页：%s", third.Body.String())
	}
	// 另一个来源不受影响（配额是按来源的，不是全局的）。
	other := map[string]string{
		"Content-Type":    "application/x-www-form-urlencoded",
		"X-Forwarded-For": "198.51.100.9",
	}
	if resp := do(h, http.MethodPost, "/admin/login", form, other); resp.Code != http.StatusFound {
		t.Fatalf("其他来源不应被牵连，实际 %d", resp.Code)
	}
	found := false
	for _, ev := range sink.wait(t, 3) {
		if ev.Outcome == outcomeThrottled {
			found = true
		}
	}
	if !found {
		t.Fatal("被节流的尝试也要产出事件（否则“为什么被拒”无可观测）")
	}
}

func TestSessionTableFullEvictsOldestNotEveryone(t *testing.T) {
	h, _ := newHandler(t, Options{})
	now := time.Now()
	h.now = func() time.Time { return now }
	// 直接把表填满：逐出逻辑是**单元**行为，用 4096 次登录去构造它只是慢。
	for i := range maxSessions {
		h.sessions[fmt.Sprintf("s-%05d", i)] = &sessionState{
			expires: now.Add(time.Duration(i) * time.Second),
		}
	}
	newest := fmt.Sprintf("s-%05d", maxSessions-1)
	if _, err := h.newSession("probe"); err != nil {
		t.Fatal(err)
	}
	if _, alive := h.sessions[newest]; !alive {
		t.Fatal("逐出不得波及最新到期的会话（原来“整体清空”会把所有人踢掉）")
	}
	if _, alive := h.sessions["s-00000"]; alive {
		t.Fatal("应逐出最早到期的会话")
	}
	if len(h.sessions) > maxSessions {
		t.Fatalf("容量上限必须守住，实际 %d", len(h.sessions))
	}
}

func TestHandlerCloseIsIdempotentAndStopsEmitter(t *testing.T) {
	h, sink := newHandler(t, Options{})
	h.Close()
	h.Close() // 幂等：不得 panic / 不得死锁
	if got := sink.count(); got != 0 {
		t.Fatalf("关闭后不应再投递（当前 %d 条）", got)
	}
	// 关闭后再 emit 不得 panic（非阻塞丢弃）。
	h.emit(Event{Kind: EventPageView, Outcome: "after-close"})
	if h.DroppedEvents() != 0 && sink.count() != 0 {
		t.Fatalf("关闭后的事件不得被投递：dropped=%d sink=%d", h.DroppedEvents(), sink.count())
	}
}

// templateMustFail 造一个**执行期**必定失败的模板（渲染时数据缺字段）。
func templateMustFail(t *testing.T) *template.Template {
	t.Helper()
	return template.Must(template.New("broken").Parse(`{{.NoSuchField.Sub}}`))
}

// failingRand 让会话 id 生成失败（模拟内核随机源不可用）——用来触达 500 分支。
type failingRand struct{}

func (failingRand) Read([]byte) (int, error) { return 0, errRandUnavailable{} }

// errRandUnavailable 是本测试用的随机源失败错误。
type errRandUnavailable struct{}

func (errRandUnavailable) Error() string { return "模拟随机源不可用" }

// TestServerErrorPageIsComplete 补 500 这一档（建议书 §5 第 2 条点名 400/401/404/413/500 逐个检查）。
//
// 为什么单独立一例：500 是**唯一**由"我们自己的依赖坏了"触发的分支（随机源/场景数据），
// 它最容易写成"只有状态码没有正文"——而那正是对手能一眼看出的破绽。
func TestServerErrorPageIsComplete(t *testing.T) {
	h := New(Options{ScenarioID: "atlas", Rand: failingRand{}})
	resp := do(h, http.MethodPost, "/admin/login", url.Values{
		"username": {"a"}, "password": {"b"},
	}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("随机源不可用应 500，实际 %d", resp.Code)
	}
	body := resp.Body.String()
	for _, must := range []string{"<!doctype html>", "</html>", h.scenario.Org, h.scenario.Product} {
		if !strings.Contains(body, must) {
			t.Fatalf("500 页缺少 %q（状态码对、正文是半成品）：%s", must, body)
		}
	}
	if sessionCookieFrom(resp) != "" {
		t.Fatal("没建起会话就不得下发 cookie")
	}
}

// ── 第三次深度优化 R12 / R13 ────────────────────────────────────────────────

// errorSink 每次投递都失败（用来验证失败**被计数**，而不是静默消失）。
type errorSink struct{ calls atomic.Int64 }

func (s *errorSink) Event(context.Context, Event) error {
	s.calls.Add(1)
	return errors.New("上游采集器不可用")
}

// blockedSink 永远不返回（模拟卡住的出口）。
type blockedSink struct{ release chan struct{} }

func (s *blockedSink) Event(context.Context, Event) error {
	<-s.release
	return nil
}

// TestEventSinkErrorsAreCounted（`R12`）：出口报错必须能被读到 ——
// 否则运维看到"一切正常"，而实际一条事件都没送出去。
func TestEventSinkErrorsAreCounted(t *testing.T) {
	sink := &errorSink{}
	h := New(Options{ScenarioID: "atlas", Rand: fixedRand{9}, Events: sink, EventQueue: 8})
	defer h.Close()

	login := do(h, http.MethodPost, "/admin/login", url.Values{
		"username": {"alice"}, "password": {"x"},
	}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if login.Code != http.StatusFound {
		t.Fatalf("登录应成功（出口失败不得影响交互），实际 %d", login.Code)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && h.EventFailures() == 0 {
		time.Sleep(time.Millisecond)
	}
	if h.EventFailures() == 0 {
		t.Fatal("出口报错必须被计数（EventFailures）—— 静默失败等于观测面撒谎")
	}
}

// TestCloseIsBoundedWhenSinkIsStuck（`R12`）：卡住的出口不得让退出流程无限等待。
func TestCloseIsBoundedWhenSinkIsStuck(t *testing.T) {
	old := closeTimeout
	closeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { closeTimeout = old })

	sink := &blockedSink{release: make(chan struct{})}
	t.Cleanup(func() { close(sink.release) })
	h := New(Options{ScenarioID: "atlas", Rand: fixedRand{3}, Events: sink, EventQueue: 4})
	do(h, http.MethodPost, "/admin/login", "username=a&password=b",
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})

	start := time.Now()
	h.Close()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Close 必须在有界时间内返回（出口卡住时也不能无限等），实际 %s", elapsed)
	}
}

// TestClientIPHonorsTrustedProxies（`R13`）：只有可信前跳的 XFF 才被采信，且取**最后一段**。
func TestClientIPHonorsTrustedProxies(t *testing.T) {
	trustEngine := []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	mk := func(remote, xff string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://x/", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}
	cases := []struct {
		name    string
		remote  string
		xff     string
		trusted []netip.Prefix
		want    string
	}{
		{"默认不信任：忽略伪造 XFF", "203.0.113.9:5555", "1.2.3.4", nil, "203.0.113.9"},
		{"可信前跳：取最后一段（代理追加的那段）", "127.0.0.1:5555", "1.2.3.4, 203.0.113.9", trustEngine, "203.0.113.9"},
		{"可信前跳但无 XFF：用对端", "127.0.0.1:5555", "", trustEngine, "127.0.0.1"},
		{"不可信前跳 + 多段 XFF：仍用对端", "198.51.100.7:5555", "1.2.3.4, 5.6.7.8", trustEngine, "198.51.100.7"},
		{"对端非法地址：原样返回", "not-an-ip", "1.2.3.4", trustEngine, "not-an-ip"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := clientIPOf(mk(c.remote, c.xff), c.trusted); got != c.want {
				t.Fatalf("clientIPOf = %q，期望 %q", got, c.want)
			}
		})
	}
}
