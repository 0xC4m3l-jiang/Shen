package api

import (
	"net/http"
	"strings"
	"testing"

	"shen/modules/console/internal/rbac"
)

func TestUnauthenticatedAndUnknownRoutes(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/api/v1/overview", "/api/v1/stream", "/api/v1/services", "/api/v1/users"} {
		if rec := h.anon().do("GET", path, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("未登录访问 %s 应为 401，实际 %d", path, rec.Code)
		}
	}
	if rec := h.anon().do("GET", "/api/v1/nope", nil); rec.Code != http.StatusNotFound {
		t.Errorf("未知接口应 404，实际 %d", rec.Code)
	}
	if rec := h.anon().do("GET", "/api/summary", nil); rec.Code != http.StatusNotFound {
		t.Errorf("旧的未鉴权接口必须已移除，实际 %d", rec.Code)
	}
	rec := h.anon().do("GET", "/healthz", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("健康检查应公开且带安全头：%d %v", rec.Code, rec.Header())
	}
}

func TestMustChangePasswordGate(t *testing.T) {
	h := newHarness(t)
	c := h.login("admin", adminPass, "")
	if rec := c.do("GET", "/api/v1/overview", nil); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "password_change_required") {
		t.Fatalf("首次登录未改密时业务接口应被拒：%d %s", rec.Code, rec.Body.String())
	}
	sess := decode[sessionView](t, c.do("GET", "/api/v1/auth/session", nil))
	if !sess.User.MustChange || len(sess.Permissions) != 1 {
		t.Fatalf("改密前只应暴露 self:manage：%+v", sess)
	}
	rec := c.do("POST", "/api/v1/auth/password", map[string]string{"old_password": adminPass, "new_password": adminNewPass})
	if rec.Code != http.StatusOK {
		t.Fatalf("改密失败：%d %s", rec.Code, rec.Body.String())
	}
	old := *c
	c.absorb(rec)
	if rec := old.do("GET", "/api/v1/auth/session", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("改密后旧会话 Cookie 必须失效：%d", rec.Code)
	}
	if rec := c.do("GET", "/api/v1/system/status", nil); rec.Code != http.StatusOK {
		t.Fatalf("改密后应可访问：%d %s", rec.Code, rec.Body.String())
	}
}

func TestLoginRejectsCrossSiteAndForms(t *testing.T) {
	h := newHarness(t)
	body := map[string]string{"username": "admin", "password": adminPass}
	if rec := h.anon().do("POST", "/api/v1/auth/login", body, withHeader("Origin", "https://evil.example")); rec.Code != http.StatusForbidden {
		t.Errorf("跨站登录应被拒（防登录 CSRF）：%d", rec.Code)
	}
	if rec := h.anon().do("POST", "/api/v1/auth/login", body, withHeader("Content-Type", "application/x-www-form-urlencoded")); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("表单提交应被拒：%d", rec.Code)
	}
	rec := h.anon().do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong-password-x"})
	if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "不存在") {
		t.Errorf("口令错误应为 401 且不泄露账号是否存在：%d %s", rec.Code, rec.Body.String())
	}
	ok := h.anon().do("POST", "/api/v1/auth/login", body)
	ck := ok.Result().Cookies()
	if len(ck) != 1 || !ck[0].HttpOnly || ck[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("会话 Cookie 必须 HttpOnly + SameSite=Strict：%+v", ck)
	}
}

func TestCSRFOriginAndCORS(t *testing.T) {
	h := newHarness(t)
	c := h.login("admin", adminPass, adminNewPass)
	svc := map[string]any{"name": "商城", "upstream": "http://10.0.0.5:8080", "hosts": []string{"shop.example.com"}, "enabled": true}

	cases := []struct {
		name string
		opts []reqOpt
		code string
	}{
		{"缺 CSRF 头", []reqOpt{noCSRF()}, "csrf_failed"},
		{"错误 CSRF 头", []reqOpt{withHeader("X-CSRF-Token", "forged")}, "csrf_failed"},
		{"跨站 Origin", []reqOpt{withHeader("Origin", "https://evil.example")}, "origin_rejected"},
		{"Fetch Metadata 跨站", []reqOpt{withHeader("Sec-Fetch-Site", "cross-site")}, "origin_rejected"},
	}
	for _, tc := range cases {
		rec := c.do("POST", "/api/v1/services", svc, tc.opts...)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), tc.code) {
			t.Errorf("%s：期望 403 %s，实际 %d %s", tc.name, tc.code, rec.Code, rec.Body.String())
		}
	}
	pre := c.do("OPTIONS", "/api/v1/services", nil, withHeader("Access-Control-Request-Method", "POST"))
	if pre.Code != http.StatusForbidden || pre.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("跨域预检必须被拒且无 CORS 头：%d %v", pre.Code, pre.Header())
	}
	if rec := c.do("POST", "/api/v1/services", svc); rec.Code != http.StatusCreated {
		t.Fatalf("合法请求应成功：%d %s", rec.Code, rec.Body.String())
	}
	entries := h.srv.audit.Recent(50, "")
	var denied, created bool
	for _, e := range entries {
		denied = denied || (e.Action == "request.denied" && e.Result == "denied")
		created = created || (e.Action == "registry.create" && e.Result == "ok")
	}
	if !denied || !created {
		t.Errorf("拒绝与成功的写操作都应进审计：%+v", entries)
	}
}

func TestRoleSeparation(t *testing.T) {
	h := newHarness(t)
	admin := h.login("admin", adminPass, adminNewPass)
	for name, role := range map[string]rbac.Role{"ops.deception": rbac.DeceptionOperator,
		"ops.honeypot": rbac.HoneypotOperator, "reader": rbac.Viewer} {
		rec := admin.do("POST", "/api/v1/users", map[string]any{"username": name, "role": role, "password": "Temporary-Pass-2026"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("创建 %s 失败：%d %s", name, rec.Code, rec.Body.String())
		}
	}
	svc := map[string]any{"name": "门户", "upstream": "https://portal.internal", "hosts": []string{"portal.example.com"}, "enabled": true}
	cases := []struct {
		user   string
		method string
		path   string
		body   any
		want   int
	}{
		{"ops.deception", "GET", "/api/v1/deception/config", nil, http.StatusBadGateway}, // 有权限，核心替身无快照 ⇒ 502
		{"ops.deception", "GET", "/api/v1/honeypot/deliveries", nil, http.StatusForbidden},
		{"ops.deception", "POST", "/api/v1/services", svc, http.StatusCreated},
		{"ops.deception", "GET", "/api/v1/users", nil, http.StatusForbidden},
		{"ops.honeypot", "GET", "/api/v1/honeypot/deliveries", nil, http.StatusOK},
		{"ops.honeypot", "GET", "/api/v1/deception/flow", nil, http.StatusForbidden},
		{"ops.honeypot", "POST", "/api/v1/services", svc, http.StatusForbidden},
		{"ops.honeypot", "GET", "/api/v1/services", nil, http.StatusOK},
		{"reader", "GET", "/api/v1/deception/flow", nil, http.StatusOK},
		{"reader", "GET", "/api/v1/honeypot/backends", nil, http.StatusOK},
		{"reader", "POST", "/api/v1/services", svc, http.StatusForbidden},
		{"reader", "GET", "/api/v1/audit", nil, http.StatusForbidden},
	}
	h.core.snapErr = errString("未装配快照")
	for _, tc := range cases {
		c := h.login(tc.user, "Temporary-Pass-2026", "Changed-Pass-2026x")
		if rec := c.do(tc.method, tc.path, tc.body); rec.Code != tc.want {
			t.Errorf("%s %s %s：期望 %d，实际 %d %s", tc.user, tc.method, tc.path, tc.want, rec.Code, rec.Body.String())
		}
		h.auth.Logout(c.cookie.Value)
		_ = h.auth.ResetPassword(tc.user, "Temporary-Pass-2026")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestReadOnlyAPIToken(t *testing.T) {
	h := newHarness(t)
	bearer := withHeader("Authorization", "Bearer "+apiToken)
	if rec := h.anon().do("GET", "/api/v1/overview", nil, bearer); rec.Code != http.StatusOK {
		t.Fatalf("回环来源的只读令牌应可读：%d %s", rec.Code, rec.Body.String())
	}
	if rec := h.anon().do("GET", "/api/v1/overview", nil, bearer, withRemote("203.0.113.5:4000")); rec.Code != http.StatusUnauthorized {
		t.Errorf("非允许来源的令牌应被拒：%d", rec.Code)
	}
	if rec := h.anon().do("POST", "/api/v1/services", map[string]any{"name": "x"}, bearer); rec.Code != http.StatusForbidden {
		t.Errorf("只读令牌不能写：%d", rec.Code)
	}
	if rec := h.anon().do("GET", "/api/v1/users", nil, bearer); rec.Code != http.StatusForbidden {
		t.Errorf("只读令牌只有 viewer 权限：%d", rec.Code)
	}
}
