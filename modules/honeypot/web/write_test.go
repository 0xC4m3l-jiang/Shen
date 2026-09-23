// 本文件是**受限写路径与写后读**（方案 `C06` / 场景 `T18`·`T19`）的回归。
//
// 要钉住的四条：① 写后读（同会话、跨资源一致）② 跨会话隔离（含**不污染场景基础数据**）
// ③ 幂等与 CAS（重复/并发冲突不双写）④ 配额与非法输入受控拒绝（不 500、不留半状态）。
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// loginSession 走一次合成登录并返回 (cookie, 该会话状态的用户 id)。
func loginSession(t *testing.T, h *Handler, user string) string {
	t.Helper()
	resp := do(h, http.MethodPost, "/admin/login", url.Values{
		"username": {user}, "password": {"x"},
	}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if resp.Code != http.StatusFound {
		t.Fatalf("合成登录应 302，实际 %d", resp.Code)
	}
	cookie := sessionCookieFrom(resp)
	if cookie == "" {
		t.Fatal("demo 场景应下发会话 cookie")
	}
	return cookie
}

func TestWriteThenReadInSameSession(t *testing.T) {
	h, _ := newHandler(t, Options{})
	cookie := loginSession(t, h, "alice")
	id := h.scenario.Users[0].ID
	wasEnabled := h.scenario.Users[0].Enabled

	// 写：禁用这个合成账号
	resp := do(h, http.MethodPost, "/admin/api/users/"+id, `{"enabled":false}`,
		map[string]string{"Content-Type": "application/json", "Cookie": cookie})
	if resp.Code != http.StatusOK {
		t.Fatalf("受限写应 200，实际 %d（%s）", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"changed":true`) || !strings.Contains(body, `"version":1`) {
		t.Fatalf("写响应应报告 changed=true 与提交后的版本，实际 %s", body)
	}

	// 读①：用户列表（同会话）
	list := do(h, http.MethodGet, "/admin/api/users?page=1", "", map[string]string{"Cookie": cookie})
	if !strings.Contains(list.Body.String(), `"id":"`+id+`","name"`) {
		t.Fatalf("列表里应还有这个账号：%s", list.Body.String())
	}
	if !strings.Contains(list.Body.String(), `"enabled":false`) {
		t.Fatalf("写后读必须看到新值（同会话），实际 %s", list.Body.String())
	}

	// 读②：**跨资源**一致 —— 审计里出现这条变更，且 target 指向同一个对象 id
	audit := do(h, http.MethodGet, "/admin/api/audit?page=1&page_size=20", "",
		map[string]string{"Cookie": cookie})
	if !strings.Contains(audit.Body.String(), `"action":"user.disable"`) ||
		!strings.Contains(audit.Body.String(), `"target":"`+id+`"`) {
		t.Fatalf("审计里应出现 user.disable → %s（跨资源一致），实际 %s", id, audit.Body.String())
	}
	if !strings.Contains(audit.Body.String(), `"actor":"alice"`) {
		t.Fatalf("审计的 actor 应是本会话的操作者，实际 %s", audit.Body.String())
	}
	// 审计**最新在前**：刚做的变更必须在第一页第一条 —— 否则"写后读"在数据上成立却看不见。
	first := firstAuditItem(t, audit.Body.String())
	if first["actor"] != "alice" || first["target"] != id {
		t.Fatalf("审计第一条应是本次变更（最新在前），实际 %v", first)
	}
	_ = wasEnabled
}

func TestWriteIsIsolatedAcrossSessionsAndDoesNotTouchBase(t *testing.T) {
	h, _ := newHandler(t, Options{})
	a := loginSession(t, h, "alice")
	b := loginSession(t, h, "bob")
	id := h.scenario.Users[0].ID
	base := h.scenario.Users[0].Enabled

	if resp := do(h, http.MethodPost, "/admin/api/users/"+id, `{"enabled":false}`,
		map[string]string{"Content-Type": "application/json", "Cookie": a}); resp.Code != http.StatusOK {
		t.Fatalf("A 会话的写应成功，实际 %d", resp.Code)
	}

	// B 会话：看不到 A 的改动（**跨会话隔离**）
	view := do(h, http.MethodGet, "/admin/api/users?page=1", "", map[string]string{"Cookie": b})
	if !strings.Contains(view.Body.String(), fmt.Sprintf(`"enabled":%t`, base)) {
		t.Fatalf("B 会话必须看到**原始**值（跨会话隔离），实际 %s", view.Body.String())
	}
	// 注意：场景**基础数据本身**就含一条 `user.disable`（合成历史），所以不能按 action 判；
	// 要按"谁改的 + 改的是哪个对象"判 —— 否则断言会因为基础数据而假通过/假失败。
	auditB := do(h, http.MethodGet, "/admin/api/audit?page=1&page_size=50", "",
		map[string]string{"Cookie": b})
	if strings.Contains(auditB.Body.String(), `"actor":"alice"`) ||
		strings.Contains(auditB.Body.String(), fmt.Sprintf(`"actor":"alice","action":"user.disable","target":"%s"`, id)) {
		t.Fatalf("B 会话的审计里不该出现 A 的改动：%s", auditB.Body.String())
	}

	// 场景**基础数据**不得被写操作污染（深拷贝的回归：否则新会话/banner 都会串）
	if h.scenario.Users[0].Enabled != base {
		t.Fatal("写操作污染了场景基础数据（应为深拷贝）")
	}
	c := loginSession(t, h, "carol")
	viewC := do(h, http.MethodGet, "/admin/api/users?page=1", "", map[string]string{"Cookie": c})
	if !strings.Contains(viewC.Body.String(), fmt.Sprintf(`"enabled":%t`, base)) {
		t.Fatalf("新会话必须看到原始值，实际 %s", viewC.Body.String())
	}
}

func TestWriteIsIdempotent(t *testing.T) {
	h, _ := newHandler(t, Options{})
	cookie := loginSession(t, h, "alice")
	id := h.scenario.Users[1].ID
	base := h.scenario.Users[1].Enabled

	first := do(h, http.MethodPost, "/admin/api/users/"+id, fmt.Sprintf(`{"enabled":%t}`, !base),
		map[string]string{"Content-Type": "application/json", "Cookie": cookie})
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"changed":true`) {
		t.Fatalf("首次写应 changed=true，实际 %d %s", first.Code, first.Body.String())
	}
	// 同值重放：幂等成功（changed=false）且**不推进版本**
	second := do(h, http.MethodPost, "/admin/api/users/"+id, fmt.Sprintf(`{"enabled":%t}`, !base),
		map[string]string{"Content-Type": "application/json", "Cookie": cookie})
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"changed":false`) {
		t.Fatalf("同值重放应幂等成功且 changed=false，实际 %d %s", second.Code, second.Body.String())
	}
	if !strings.Contains(second.Body.String(), `"version":1`) {
		t.Fatalf("幂等命中不得推进版本，实际 %s", second.Body.String())
	}
	// 审计只应有一条（重复请求不重复记账）
	audit := do(h, http.MethodGet, "/admin/api/audit?page=1&page_size=50", "",
		map[string]string{"Cookie": cookie})
	if n := strings.Count(audit.Body.String(), fmt.Sprintf(`"target":"%s"`, id)); n != 1 {
		t.Fatalf("同一变更只应记一条审计，实际 %d 条：%s", n, audit.Body.String())
	}
}

func TestWriteVersionConflictIsRetryable(t *testing.T) {
	h, _ := newHandler(t, Options{})
	cookie := loginSession(t, h, "alice")
	id := h.scenario.Users[2].ID
	base := h.scenario.Users[2].Enabled

	// 用一个过期的版本写 ⇒ 409，并且响应里带**当前版本**（客户端据此重试）
	conflict := do(h, http.MethodPost, "/admin/api/users/"+id,
		fmt.Sprintf(`{"enabled":%t,"version":99}`, !base),
		map[string]string{"Content-Type": "application/json", "Cookie": cookie})
	if conflict.Code != http.StatusConflict {
		t.Fatalf("版本不符应 409，实际 %d（%s）", conflict.Code, conflict.Body.String())
	}
	if !strings.Contains(conflict.Body.String(), `"version":0`) {
		t.Fatalf("409 必须带当前版本，实际 %s", conflict.Body.String())
	}
	// 审计里不得有这条被拒的写（拒绝 != 提交）
	audit := do(h, http.MethodGet, "/admin/api/audit?page=1&page_size=50", "",
		map[string]string{"Cookie": cookie})
	if strings.Contains(audit.Body.String(), fmt.Sprintf(`"target":"%s"`, id)) {
		t.Fatalf("被拒的写不得写入审计：%s", audit.Body.String())
	}

	// 用正确版本重试 ⇒ 成功；**只提交一次**（不双写）
	ok := do(h, http.MethodPost, "/admin/api/users/"+id,
		fmt.Sprintf(`{"enabled":%t,"version":0}`, !base),
		map[string]string{"Content-Type": "application/json", "Cookie": cookie})
	if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), `"changed":true`) {
		t.Fatalf("带正确版本的写应成功，实际 %d %s", ok.Code, ok.Body.String())
	}
	after := do(h, http.MethodGet, "/admin/api/audit?page=1&page_size=50", "",
		map[string]string{"Cookie": cookie})
	if n := strings.Count(after.Body.String(), fmt.Sprintf(`"target":"%s"`, id)); n != 1 {
		t.Fatalf("重试后应恰好一条审计（不双写），实际 %d：%s", n, after.Body.String())
	}
}

func TestConfigWriteThenReadAndValidation(t *testing.T) {
	h, _ := newHandler(t, Options{})
	cookie := loginSession(t, h, "alice")
	key := h.scenario.Config[0].Key

	ok := do(h, http.MethodPost, "/admin/api/config/"+key, `{"value":"9.9.9"}`,
		map[string]string{"Content-Type": "application/json", "Cookie": cookie})
	if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), `"value":"9.9.9"`) {
		t.Fatalf("配置写应成功并回显新值，实际 %d %s", ok.Code, ok.Body.String())
	}
	view := do(h, http.MethodGet, "/admin/api/config?page=1&page_size=20", "",
		map[string]string{"Cookie": cookie})
	if !strings.Contains(view.Body.String(), `"value":"9.9.9"`) {
		t.Fatalf("写后读应看到新配置值，实际 %s", view.Body.String())
	}

	// 非法输入：空值 / 不存在的键 / 缺字段
	for name, tc := range map[string]struct {
		path string
		body string
		want int
	}{
		"空值":     {"/admin/api/config/" + key, `{"value":"  "}`, http.StatusBadRequest},
		"不存在的键":  {"/admin/api/config/nope.key", `{"value":"x"}`, http.StatusNotFound},
		"缺字段":    {"/admin/api/config/" + key, `{}`, http.StatusBadRequest},
		"坏 JSON": {"/admin/api/config/" + key, `{"value":`, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			resp := do(h, http.MethodPost, tc.path, tc.body,
				map[string]string{"Content-Type": "application/json", "Cookie": cookie})
			if resp.Code != tc.want {
				t.Fatalf("期望 %d，实际 %d（%s）", tc.want, resp.Code, resp.Body.String())
			}
			if !strings.Contains(resp.Body.String(), `"error"`) {
				t.Fatalf("错误响应应是结构化 JSON：%s", resp.Body.String())
			}
		})
	}
}

func TestWriteQuotaIsBoundedAndRejectsControlled(t *testing.T) {
	h, _ := newHandler(t, Options{})
	cookie := loginSession(t, h, "alice")
	sess, ok := h.sessionOf(&http.Request{Header: http.Header{
		"Cookie": []string{sessionCookieFromRaw(cookie)},
	}})
	if !ok {
		t.Fatal("应能取到会话状态")
	}
	// 配额用尽（直接置满：配额是**单元**行为，用 200 次真实写去构造它只是慢）
	sess.writes = defaultMaxWritesPerSession
	id := h.scenario.Users[1].ID

	resp := do(h, http.MethodPost, "/admin/api/users/"+id, `{"enabled":false}`,
		map[string]string{"Content-Type": "application/json", "Cookie": cookie})
	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("配额用尽应 429（受控拒绝，不是 500），实际 %d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), `"error":"quota_exceeded"`) {
		t.Fatalf("错误键应可判定，实际 %s", resp.Body.String())
	}

	// 审计条目到顶同样拒绝后续写（资源上限的第二种）
	h2, _ := newHandler(t, Options{})
	cookie2 := loginSession(t, h2, "bob")
	sess2, _ := h2.sessionOf(&http.Request{Header: http.Header{
		"Cookie": []string{sessionCookieFromRaw(cookie2)},
	}})
	sess2.audit = make([]AuditEvent, defaultMaxAuditEntries)
	resp2 := do(h2, http.MethodPost, "/admin/api/users/"+h2.scenario.Users[1].ID, `{"enabled":false}`,
		map[string]string{"Content-Type": "application/json", "Cookie": cookie2})
	if resp2.Code != http.StatusTooManyRequests {
		t.Fatalf("审计到顶应 429，实际 %d", resp2.Code)
	}
}

func TestPageWriteRedirectsAndShowsNewValue(t *testing.T) {
	h, _ := newHandler(t, Options{})
	cookie := loginSession(t, h, "alice")
	// 用**第一页上的**用户（默认 page_size=3 ⇒ 只有前三个在第一页），并取反当前值 ——
	// 否则断言会在"这一行不在本页""本来就是目标值"这些与写路径无关的原因上失败。
	id := h.scenario.Users[0].ID
	target := !h.scenario.Users[0].Enabled
	page0 := do(h, http.MethodGet, adminPrefix+"/users", "", map[string]string{"Cookie": cookie})
	before := "enabled"
	if !h.scenario.Users[0].Enabled {
		before = "disabled"
	}
	if !strings.Contains(page0.Body.String(), before) {
		t.Fatalf("前置：第一页应显示写前状态 %q", before)
	}

	// 页面表单写：走 POST/Redirect/GET（303），刷新不会重复提交
	resp := do(h, http.MethodPost, adminPrefix+"/users/"+id, url.Values{
		"enabled": {fmt.Sprintf("%t", target)},
	}.Encode(), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded", "Cookie": cookie,
	})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("页面写成功应 303，实际 %d（%s）", resp.Code, resp.Body.String())
	}
	if loc := resp.Header().Get("Location"); !strings.HasPrefix(loc, adminPrefix+"/users") {
		t.Fatalf("应重定向回列表页，实际 %q", loc)
	}
	page := do(h, http.MethodGet, adminPrefix+"/users", "", map[string]string{"Cookie": cookie})
	want := "disabled"
	if target {
		want = "enabled"
	}
	if !strings.Contains(page.Body.String(), want) {
		t.Fatalf("列表页应显示写后的状态 %q，实际 %s", want, page.Body.String())
	}

	// 非法取值：完整错误页（不是 500、不是半截 HTML）
	bad := do(h, http.MethodPost, adminPrefix+"/users/"+id, url.Values{
		"enabled": {"maybe"},
	}.Encode(), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded", "Cookie": cookie,
	})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("非法取值应 400，实际 %d", bad.Code)
	}
	body := bad.Body.String()
	for _, must := range []string{"<!doctype html>", "</html>", h.scenario.Org} {
		if !strings.Contains(body, must) {
			t.Fatalf("400 页缺少 %q：%s", must, body)
		}
	}
}

func TestWriteRequiresSession(t *testing.T) {
	h, sink := newHandler(t, Options{})
	id := h.scenario.Users[0].ID
	api := do(h, http.MethodPost, "/admin/api/users/"+id, `{"enabled":false}`,
		map[string]string{"Content-Type": "application/json"})
	if api.Code != http.StatusUnauthorized {
		t.Fatalf("未登录的 API 写应 401，实际 %d", api.Code)
	}
	page := do(h, http.MethodPost, adminPrefix+"/users/"+id, "enabled=false",
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if page.Code != http.StatusFound {
		t.Fatalf("未登录的页面写应 302 → 登录页，实际 %d", page.Code)
	}
	// 被拒也要留痕（可见性），且不得出现 applied
	events := sink.wait(t, 2)
	if len(events) < 2 {
		t.Fatalf("两次被拒都应产出事件，实际 %+v", events)
	}
	for _, ev := range events {
		if ev.Kind == EventStateChange && ev.Outcome == "applied" {
			t.Fatalf("未认证的写不得被记成 applied：%+v", ev)
		}
	}
}

// firstAuditItem 取审计列表的第一条（断言"最新在前"用）。
func firstAuditItem(t *testing.T, body string) map[string]any {
	t.Helper()
	var doc struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("审计响应不是合法 JSON：%v", err)
	}
	if len(doc.Items) == 0 {
		t.Fatal("审计列表为空")
	}
	return doc.Items[0]
}

// sessionCookieFromRaw 从 Set-Cookie 原文取出 `name=value`（测试里手工造请求时用）。
func sessionCookieFromRaw(raw string) string {
	head, _, _ := strings.Cut(raw, ";")
	return head
}
