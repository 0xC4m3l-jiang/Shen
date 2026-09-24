package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── 测试替身 ────────────────────────────────────────────────────────────────

// recordingSink 记录事件（用来断言「密码从不进事件」）。
type recordingSink struct {
	mu     sync.Mutex
	events []Event
}

func (s *recordingSink) Event(_ context.Context, ev Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
	return nil
}

func (s *recordingSink) all() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

// wait 等事件到达（有界等待，最多 2s）。
//
// 为什么必须等：事件出口是**异步有界**的（`W5`）—— 请求返回 ≠ 事件已投递。
// 直接断言切片会让「事件丢了」和「还没送到」看起来一样（真正的丢事件反而测不出来）。
func (s *recordingSink) wait(t *testing.T, want int) []Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := s.all(); len(got) >= want {
			return got
		}
		time.Sleep(time.Millisecond)
	}
	got := s.all()
	t.Fatalf("等待 %d 条事件超时（实际 %d）", want, len(got))
	return got
}

// count 返回当前已投递的事件数（不做等待）。
func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// fixedRand 让会话 id 可预期（测试里不依赖随机数）。
type fixedRand struct{ b byte }

func (f fixedRand) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = f.b
	}
	return len(p), nil
}

func newHandler(t *testing.T, opts Options) (*Handler, *recordingSink) {
	t.Helper()
	sink := &recordingSink{}
	opts.Events = sink
	if opts.Rand == nil {
		opts.Rand = fixedRand{b: 0x5a}
	}
	return New(opts), sink
}

func do(h http.Handler, method, target, body string, hdr map[string]string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// ── ① 场景自洽（C04：页面 → API → 数据指同一批对象）────────────────────────

func TestScenarioIsSelfConsistent(t *testing.T) {
	sc := ScenarioFor("atlas")

	known := map[string]bool{}
	for _, u := range sc.Users {
		known[u.ID] = true
	}
	if len(known) == 0 {
		t.Fatal("场景必须有账号")
	}
	for i, e := range sc.Audit {
		if !known[e.Actor] {
			t.Errorf("审计事件 #%d 的 actor=%q 不在账号表里 —— 场景不自洽", i, e.Actor)
		}
		// target 可以是账号，也可以是资源名（如 deployment/storage.bucket）；账号时必须存在。
		if strings.HasPrefix(e.Target, "u-") && !known[e.Target] {
			t.Errorf("审计事件 #%d 的 target=%q 不在账号表里", i, e.Target)
		}
	}
	if !strings.HasSuffix(sc.Host, ".example") {
		t.Errorf("合成主机名必须用保留域 .example（永不解析到真实主机），得到 %q", sc.Host)
	}
}

// TestScenarioHasNoRealSecretsOrPII 断言场景包里没有真实密钥/内网地址/个人信息（C04 的验收判据）。
func TestScenarioHasNoRealSecretsOrPII(t *testing.T) {
	sc := ScenarioFor("atlas")
	var blob strings.Builder
	for _, u := range sc.Users {
		blob.WriteString(u.ID + " " + u.Name + " " + u.Role + "\n")
	}
	for _, c := range sc.Config {
		blob.WriteString(c.Key + "=" + c.Value + "\n")
	}
	for _, e := range sc.Audit {
		blob.WriteString(e.Action + " " + e.Target + "\n")
	}
	text := blob.String()

	// 值一律是占位符（不是可用凭证）。
	for _, c := range sc.Config {
		if strings.Contains(c.Key, "password") || strings.Contains(c.Key, "token") {
			if !strings.Contains(c.Value, "PLACEHOLDER") {
				t.Errorf("敏感配置项 %q 的值必须是占位符，得到 %q", c.Key, c.Value)
			}
		}
	}
	// 不出现私网地址 / 真实内网后缀（合成素材同样不该指向真实基础设施）。
	for _, bad := range []string{"10.", "192.168.", "127.0.0.1", ".internal", ".local", ".lan", ".corp"} {
		if strings.Contains(text, bad) {
			t.Errorf("场景数据里出现了 %q —— 合成数据禁止指向真实基础设施", bad)
		}
	}
}

// ── ② 页面与 API 同源（同一份数据 + 同一套分页）────────────────────────────

func TestPagesAndAPIAgree(t *testing.T) {
	h, _ := newHandler(t, Options{})
	cookie := demoSessionCookie(t, h)

	cases := []struct{ path, apiPath string }{
		{"/admin/users", "/api/v1/admin/users"},
		{"/admin/config", "/api/v1/admin/config"},
		{"/admin/audit", "/api/v1/admin/audit"},
	}
	for _, c := range cases {
		page := do(h, http.MethodGet, c.path+"?page=1", "", map[string]string{"Cookie": cookie})
		if page.Code != http.StatusOK {
			t.Fatalf("%s 应 200，得到 %d", c.path, page.Code)
		}
		api := do(h, http.MethodGet, c.apiPath+"?page=1", "", map[string]string{"Cookie": cookie})
		if api.Code != http.StatusOK {
			t.Fatalf("%s 应 200，得到 %d", c.apiPath, api.Code)
		}
		var got struct {
			Items    []map[string]any `json:"items"`
			Total    int              `json:"total"`
			PageSize int              `json:"page_size"`
			NextPage *int             `json:"next_page"`
		}
		if err := json.Unmarshal(api.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s 不是合法 JSON：%v", c.apiPath, err)
		}
		if got.Total == 0 || len(got.Items) == 0 {
			t.Fatalf("%s 应有数据：total=%d items=%d", c.apiPath, got.Total, len(got.Items))
		}
		// 页面里的条目数与 API 的第一页条目数一致（同一个 paginate 产出）。
		rows := strings.Count(strings.Split(page.Body.String(), "<table>")[1], "<tr>") - 1 // 减去表头行
		if rows != len(got.Items) {
			t.Errorf("%s 页面行数 %d ≠ %s 第一页条目数 %d —— 页面与 API 必须同源", c.path, rows, c.apiPath, len(got.Items))
		}
		// 页面上必须出现 API 第一页的第一个对象标识（同一批对象）。
		if id, ok := got.Items[0]["id"].(string); ok && !strings.Contains(page.Body.String(), id) {
			t.Errorf("%s 页面里没有 API 的第一个对象 %q", c.path, id)
		}
	}
}

func TestPaginationClampsAndEnds(t *testing.T) {
	h, _ := newHandler(t, Options{})
	cookie := demoSessionCookie(t, h)

	var big struct {
		PageSize int  `json:"page_size"`
		NextPage *int `json:"next_page"`
	}
	body := do(h, http.MethodGet, "/api/v1/admin/audit?page=1&page_size=1000", "", map[string]string{"Cookie": cookie})
	if err := json.Unmarshal(body.Body.Bytes(), &big); err != nil {
		t.Fatal(err)
	}
	sc := h.Scenario()
	if big.PageSize != sc.MaxPageSize {
		t.Errorf("page_size 超过上限应夹到 %d，得到 %d", sc.MaxPageSize, big.PageSize)
	}

	var end struct {
		Items    []map[string]any `json:"items"`
		NextPage *int             `json:"next_page"`
	}
	tail := do(h, http.MethodGet, "/api/v1/admin/audit?page=999", "", map[string]string{"Cookie": cookie})
	if err := json.Unmarshal(tail.Body.Bytes(), &end); err != nil {
		t.Fatal(err)
	}
	if len(end.Items) != 0 || end.NextPage != nil {
		t.Errorf("越界页应为空且没有下一页：items=%d next=%v", len(end.Items), end.NextPage)
	}
}

// ── ③ 认证边界：从不校验凭据、密码读完即弃 ────────────────────────────────

func TestLoginNeverValidatesCredentials(t *testing.T) {
	h, sink := newHandler(t, Options{})
	form := url.Values{"username": {"root"}, "password": {"definitely-not-the-password"}}.Encode()
	resp := do(h, http.MethodPost, "/admin/login", form, nil)

	// 演示场景：进入合成会话（302 → /admin/）并只在**诱饵专用** cookie 里留下不透明 id。
	if resp.Code != http.StatusFound {
		t.Fatalf("demo 场景应 302，得到 %d", resp.Code)
	}
	cookie := sessionCookieFrom(resp)
	if cookie == "" {
		t.Fatal("demo 场景应下发合成会话 cookie")
	}
	if strings.Contains(cookie, "password") || strings.Contains(cookie, "definitely") {
		t.Fatalf("会话 cookie 不得由提交的凭据派生，得到 %q", cookie)
	}
	events := sink.wait(t, 1)
	if len(events) != 1 || events[0].Kind != EventLoginAttempt {
		t.Fatalf("应记一条 login_attempt 事件，得到 %+v", events)
	}
	if events[0].Outcome != string(LoginDemo) {
		t.Fatalf("事件应记场景结果，得到 %q", events[0].Outcome)
	}
}

func TestFailScenarioAlwaysRejects(t *testing.T) {
	h := New(Options{ScenarioID: "atlas", Rand: fixedRand{1}})
	h.scenario.Outcome = LoginFails // 直接改场景：两种登录语义都要可测

	form := url.Values{"username": {"admin"}, "password": {"x"}}.Encode()
	resp := do(h, http.MethodPost, "/admin/login", form, nil)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("fail 场景应 401，得到 %d", resp.Code)
	}
	if sessionCookieFrom(resp) != "" {
		t.Fatal("失败语义不得下发会话 cookie")
	}
}

// TestPasswordNeverRetained 断言**误填的真实密码**既不出现在响应里，也不进事件（方案 §5.2）。
func TestPasswordNeverRetained(t *testing.T) {
	h, sink := newHandler(t, Options{})
	const secret = "SECRET-PASSWORD-MUST-NOT-SURVIVE"
	form := url.Values{
		"username": {"alice"},
		"password": {secret},
		"totp":     {secret + "-2"},
	}.Encode()

	for _, path := range []string{"/admin/login"} {
		resp := do(h, http.MethodPost, path, form, nil)
		if strings.Contains(resp.Body.String(), secret) {
			t.Fatalf("%s 的响应里出现了提交的密码", path)
		}
	}
	for _, ev := range sink.wait(t, 1) {
		blob := fmt.Sprintf("%+v", ev)
		if strings.Contains(blob, secret) {
			t.Fatalf("事件里出现了提交的密码：%+v", ev)
		}
	}
}

// ── ④ 会话与配额 ───────────────────────────────────────────────────────────

func TestUnauthenticatedAccessIsRedirectedOrRejected(t *testing.T) {
	h, _ := newHandler(t, Options{})
	page := do(h, http.MethodGet, "/admin/", "", nil)
	if page.Code != http.StatusFound || page.Header().Get("Location") != "/admin/login" {
		t.Errorf("未登录访问控制台应 302 → /admin/login，得到 %d %q", page.Code, page.Header().Get("Location"))
	}
	api := do(h, http.MethodGet, "/api/v1/admin/users", "", nil)
	if api.Code != http.StatusUnauthorized {
		t.Errorf("未登录访问 API 应 401，得到 %d", api.Code)
	}
}

func TestSessionExpires(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	h, _ := newHandler(t, Options{Now: func() time.Time { return now }, SessionTTL: time.Minute})
	cookie := demoSessionCookie(t, h)

	if resp := do(h, http.MethodGet, "/admin/", "", map[string]string{"Cookie": cookie}); resp.Code != http.StatusOK {
		t.Fatalf("会话有效期内应 200，得到 %d", resp.Code)
	}
	now = now.Add(2 * time.Minute)
	if resp := do(h, http.MethodGet, "/admin/", "", map[string]string{"Cookie": cookie}); resp.Code != http.StatusFound {
		t.Fatalf("会话过期后应回到登录页，得到 %d", resp.Code)
	}
}

func TestBodyLimitReturns413(t *testing.T) {
	h, _ := newHandler(t, Options{MaxBodyBytes: 1024})
	big := "username=" + strings.Repeat("a", 4096)
	resp := do(h, http.MethodPost, "/admin/login", big, nil)
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超过请求体上限应 413，得到 %d", resp.Code)
	}
}

// ── ⑤ 对外可见面：不泄漏我们的实现、不泄漏栈信息 ──────────────────────────

func TestPagesDoNotExposeOurImplementation(t *testing.T) {
	h, _ := newHandler(t, Options{})
	cookie := demoSessionCookie(t, h)

	pages := []struct {
		name, path, cookie string
	}{
		{"登录页", "/admin/login", ""},
		{"控制台", "/admin/", cookie},
		{"用户列表", "/admin/users", cookie},
		{"配置列表", "/admin/config", cookie},
		{"审计列表", "/admin/audit", cookie},
		{"404", "/nope", ""},
	}
	forbidden := []string{"honeypot", "decoy", "mirage", "蜜罐", "蜜饵", "投毒", "goroutine", "panic", ".go:"}
	for _, p := range pages {
		hdr := map[string]string{}
		if p.cookie != "" {
			hdr["Cookie"] = p.cookie
		}
		resp := do(h, http.MethodGet, p.path, "", hdr)
		body := strings.ToLower(resp.Body.String())
		for _, bad := range forbidden {
			if strings.Contains(body, bad) {
				t.Errorf("%s（%s）里出现了 %q —— 对手可见面不得暴露我们或栈信息", p.name, p.path, bad)
			}
		}
	}
}

// ── 工具 ────────────────────────────────────────────────────────────────────

// demoSessionCookie 走一遍合成登录，返回可用的会话 cookie（`atlas_session=…`）。
func demoSessionCookie(t *testing.T, h *Handler) string {
	t.Helper()
	form := url.Values{"username": {"tester"}, "password": {"x"}}.Encode()
	resp := do(h, http.MethodPost, "/admin/login", form, nil)
	c := sessionCookieFrom(resp)
	if c == "" {
		t.Fatalf("合成登录没有下发会话 cookie（状态 %d）", resp.Code)
	}
	return c
}

func sessionCookieFrom(resp *httptest.ResponseRecorder) string {
	for _, c := range resp.Result().Cookies() {
		if c.Name == scratchSessionCookie {
			return c.Name + "=" + c.Value
		}
	}
	return ""
}
