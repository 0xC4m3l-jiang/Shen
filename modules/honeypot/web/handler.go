package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 本文件是**场景的执行**：把一套 `Scenario` 服务成一排路由。
//
// 与前一份文件的分工：`scenario.go` 是**数据**（虚构组织/账号/配置/审计 + 分页规则），
// 本文件是**行为**（登录 → 会话 → 页面/API）。行为只读数据、不改数据 ——
// 诱饵后端不需要真的写状态，所有"写"都是合成回应（方案 C03：受限读写、合成演示会话）。

// scratchSessionCookie 是**诱饵专用**的会话 cookie 名。
//
// 刻意用场景化的中性名字（`atlas_session`）而不是 `demo/decoy/honeypot` 之类：
// 它会出现在攻击者的浏览器里（OH-1 / OH-2 的判据是"会不会出现在攻击者的屏幕上"）。
const scratchSessionCookie = "atlas_session"

// defaultMaxBodyBytes 是请求体上限（配额）：登录表单远小于它，超限直接 413。
const defaultMaxBodyBytes = 64 << 10 // 64 KiB

// defaultSessionTTL 是合成会话的存活期。
const defaultSessionTTL = 30 * time.Minute

// maxSessions 是合成会话表的容量上限（长跑不得无界增长）。
const maxSessions = 4096

// Options 是装配参数（装配层用，测试里逐项可控）。
type Options struct {
	// ScenarioID 选一套场景包（未知 id 回落到内置默认场景）。
	ScenarioID string
	// Packs 是**外部场景包**（`LoadPacks` 的产物）；空 = 只用内置默认场景。
	// 选中的 id 不在其中时回落到内置场景 —— 配置写错不该让整条诱饵路由变成 502（启动日志会点名）。
	Packs map[string]Scenario
	// Events 可空：合成交互的事件出口（谁发生了什么）。**禁止**往里塞请求体/密码。
	Events EventSink
	// MaxBodyBytes 0 = 默认 64 KiB。
	MaxBodyBytes int64
	// SessionTTL 0 = 默认 30 分钟。
	SessionTTL time.Duration
	// Now 与 Rand 可注入，使会话行为可测（内容本身与时钟无关）。
	Now  func() time.Time
	Rand io.Reader
}

// Handler 是 Web 诱饵后端的 HTTP 处理器（一个 http.Handler）。
type Handler struct {
	scenario Scenario
	events   EventSink
	maxBody  int64
	ttl      time.Duration
	now      func() time.Time
	rand     io.Reader

	mu       sync.Mutex
	sessions map[string]time.Time // 合成会话：id -> 到期时刻
}

// New 构造处理器（未知场景 id 回落默认场景，不报错：诱饵后端不该因配置写错就整体不可用）。
func New(opts Options) *Handler {
	sc := SelectScenario(opts.Packs, opts.ScenarioID)
	if sc.PageSize <= 0 {
		sc.PageSize = 3
	}
	if sc.MaxPageSize <= 0 {
		sc.MaxPageSize = 20
	}
	h := &Handler{
		scenario: sc,
		events:   opts.Events,
		maxBody:  opts.MaxBodyBytes,
		ttl:      opts.SessionTTL,
		now:      opts.Now,
		rand:     opts.Rand,
		sessions: map[string]time.Time{},
	}
	if h.maxBody <= 0 {
		h.maxBody = defaultMaxBodyBytes
	}
	if h.ttl <= 0 {
		h.ttl = defaultSessionTTL
	}
	if h.now == nil {
		h.now = time.Now
	}
	if h.rand == nil {
		h.rand = rand.Reader
	}
	return h
}

// Scenario 返回当前场景（只读；供装配层打印/自检）。
func (h *Handler) Scenario() Scenario { return h.scenario }

// ServeHTTP 路由表：
//
//	GET  /healthz             存活（给探针；不含场景信息）
//	GET  /admin/login         合成登录页
//	POST /admin/login         合成登录：**从不校验凭据**；按场景决定「失败」或「演示会话」
//	GET  /admin/              控制台首页（需合成会话）
//	GET  /admin/{users,config,audit}?page=N   合成列表页（与 API 同一份分页）
//	GET  /api/v1/admin/{users,config,audit}?page=&page_size=   合成 API（JSON）
//	其它                      合成 404
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSafeHeaders(w)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	case r.URL.Path == "/admin/login" && r.Method == http.MethodGet:
		h.render(w, http.StatusOK, tmplLogin, h.loginView())
	case r.URL.Path == "/admin/login" && r.Method == http.MethodPost:
		h.handleLogin(w, r)
	case r.URL.Path == "/admin/" || r.URL.Path == "/admin":
		if !h.authed(r) {
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		h.render(w, http.StatusOK, tmplDashboard, h.dashboardView())
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/admin/"):
		h.handleAdminPage(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/admin/"):
		h.handleAdminAPI(w, r)
	default:
		h.render(w, http.StatusNotFound, tmplError, errView{
			Title: "Not found", Code: http.StatusNotFound,
			Detail: "The requested resource does not exist.",
		})
	}
}

// handleLogin 处理登录尝试。
//
// **从不校验凭据**：这里没有认证服务、没有密码比对、没有外部调用（方案 §9.3 的硬边界）。
// 表单里的其它字段（含 `password`）读完即弃：**不入日志、不入会话、不入事件、不回显**。
func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	// 配额：请求体超限直接 413（不做部分解析）。
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBody)
	if err := r.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.render(w, http.StatusRequestEntityTooLarge, tmplError, errView{
				Title: "Request too large", Code: http.StatusRequestEntityTooLarge,
				Detail: "The submitted form exceeds the allowed size.",
			})
			return
		}
		h.render(w, http.StatusBadRequest, tmplError, errView{
			Title: "Bad request", Code: http.StatusBadRequest,
			Detail: "The submitted form could not be read.",
		})
		return
	}
	// 只取用户名（用于合成会话的展示与事件）；**其余字段读完即弃**。
	user := strings.TrimSpace(r.PostForm.Get("username"))
	r.PostForm.Del("password") // 显式丢弃：本服务不保留、不比对、不外传凭据

	if h.scenario.Outcome == LoginFails {
		h.emit(Event{Kind: EventLoginAttempt, User: user, Outcome: string(LoginFails)})
		h.render(w, http.StatusUnauthorized, tmplError, errView{
			Title: "Sign-in failed", Code: http.StatusUnauthorized,
			Detail: "The provided credentials were not accepted.",
		})
		return
	}
	id, err := h.newSession()
	if err != nil {
		h.render(w, http.StatusInternalServerError, tmplError, errView{
			Title: "Unavailable", Code: http.StatusInternalServerError,
			Detail: "The console is temporarily unavailable.",
		})
		return
	}
	// nosemgrep: go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure
	// 刻意不设 `Secure`：本后端在**明文侧**（TLS 由 L0/接入层终结，见 ADR-0019），设了它浏览器就不会回传，
	// 演示会话当场失效、这一档语义也就测不出来。它**不是**生产会话：只在合成场景里用，且 Path 限定 `/admin`。
	http.SetCookie(w, &http.Cookie{
		Name:     scratchSessionCookie,
		Value:    id,
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.ttl.Seconds()),
	})
	h.emit(Event{Kind: EventLoginAttempt, User: user, Outcome: string(LoginDemo)})
	http.Redirect(w, r, "/admin/", http.StatusFound)
}

// handleAdminPage 渲染合成列表页（与 API 共用同一份数据与分页）。
func (h *Handler) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	if !h.authed(r) {
		http.Redirect(w, r, "/admin/login", http.StatusFound)
		return
	}
	page := pageParam(r)
	switch r.URL.Path {
	case "/admin/users":
		p := paginate(h.scenario.Users, page, h.pageSizeParam(r), h.scenario.MaxPageSize)
		h.render(w, http.StatusOK, tmplList, h.listView("Users", metaOf("/admin/users", p),
			[]string{"ID", "Name", "Role", "State", "Last seen"}, userRows(p.Items)))
	case "/admin/config":
		p := paginate(h.scenario.Config, page, h.pageSizeParam(r), h.scenario.MaxPageSize)
		h.render(w, http.StatusOK, tmplList, h.listView("Configuration", metaOf("/admin/config", p),
			[]string{"Key", "Value"}, cfgRows(p.Items)))
	case "/admin/audit":
		p := paginate(h.scenario.Audit, page, h.pageSizeParam(r), h.scenario.MaxPageSize)
		h.render(w, http.StatusOK, tmplList, h.listView("Audit log", metaOf("/admin/audit", p),
			[]string{"Time", "Actor", "Action", "Target"}, auditRows(p.Items)))
	default:
		h.render(w, http.StatusNotFound, tmplError, errView{
			Title: "Not found", Code: http.StatusNotFound, Detail: "The requested page does not exist.",
		})
	}
}

// handleAdminAPI 返回合成 JSON（与页面同源：同一个 `paginate`，同一批对象）。
func (h *Handler) handleAdminAPI(w http.ResponseWriter, r *http.Request) {
	if !h.authed(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		return
	}
	page := pageParam(r)
	size := h.pageSizeParam(r)
	var body any
	switch r.URL.Path {
	case "/api/v1/admin/users":
		body = paginate(h.scenario.Users, page, size, h.scenario.MaxPageSize)
	case "/api/v1/admin/config":
		body = paginate(h.scenario.Config, page, size, h.scenario.MaxPageSize)
	case "/api/v1/admin/audit":
		body = paginate(h.scenario.Audit, page, size, h.scenario.MaxPageSize)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// ── 合成会话（内存、有 TTL、有容量上限）─────────────────────────────────────

func (h *Handler) newSession() (string, error) {
	buf := make([]byte, 16)
	if _, err := io.ReadFull(h.rand, buf); err != nil {
		return "", err
	}
	id := hex.EncodeToString(buf)
	exp := h.now().Add(h.ttl)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.sessions) >= maxSessions {
		// 容量满：先清过期；仍满则整体清空（会话是**可丢失**的演示状态，清掉只影响那次演示）。
		now := h.now()
		for k, e := range h.sessions {
			if !now.Before(e) {
				delete(h.sessions, k)
			}
		}
		if len(h.sessions) >= maxSessions {
			h.sessions = map[string]time.Time{}
		}
	}
	h.sessions[id] = exp
	return id, nil
}

// authed 判断请求是否带一个未过期的合成会话。
func (h *Handler) authed(r *http.Request) bool {
	c, err := r.Cookie(scratchSessionCookie)
	if err != nil || c.Value == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	exp, ok := h.sessions[c.Value]
	if !ok {
		return false
	}
	if !h.now().Before(exp) {
		delete(h.sessions, c.Value)
		return false
	}
	return true
}

// ── 视图（模板随代码分发：ST-22；不含任何真实素材）─────────────────────────

type errView struct {
	Title  string
	Code   int
	Detail string
}

type loginView struct {
	Org     string
	Product string
}

type listView struct {
	Org     string
	Product string
	Title   string
	Headers []string
	Rows    []tableRow
	Meta    pageMeta // 分页元数据（与 API 同源：由同一个 paginate 产出）
}

// pageMeta 是**分页的呈现元数据**（HTML 页用；API 直接返回 `Page[T]` 本身）。
type pageMeta struct {
	Page     int
	PageSize int
	Total    int
	HasNext  bool
	NextHref string
	PrevHref string
}

// metaOf 把任意类型的 `Page[T]` 转成呈现元数据（页面与 API 因此不可能各算一套分页）。
func metaOf[T any](base string, p Page[T]) pageMeta {
	m := pageMeta{Page: p.Page, PageSize: p.PageSize, Total: p.Total}
	if p.NextPage != nil {
		m.HasNext = true
		m.NextHref = fmt.Sprintf("%s?page=%d", base, *p.NextPage)
	}
	if p.Page > 1 {
		m.PrevHref = fmt.Sprintf("%s?page=%d", base, p.Page-1)
	}
	return m
}

type tableRow struct {
	Cells []string
}

func (h *Handler) loginView() loginView {
	return loginView{Org: h.scenario.Org, Product: h.scenario.Product}
}

func (h *Handler) dashboardView() struct {
	Org, Product, Host   string
	Users, Config, Audit int
} {
	return struct {
		Org, Product, Host   string
		Users, Config, Audit int
	}{
		Org: h.scenario.Org, Product: h.scenario.Product, Host: h.scenario.Host,
		Users: len(h.scenario.Users), Config: len(h.scenario.Config), Audit: len(h.scenario.Audit),
	}
}

func (h *Handler) listView(title string, meta pageMeta, headers []string, rows []tableRow) listView {
	return listView{Org: h.scenario.Org, Product: h.scenario.Product, Title: title, Headers: headers, Rows: rows, Meta: meta}
}

func userRows(users []User) []tableRow {
	out := make([]tableRow, 0, len(users))
	for _, u := range users {
		state := "enabled"
		if !u.Enabled {
			state = "disabled"
		}
		out = append(out, tableRow{Cells: []string{u.ID, u.Name, u.Role, state, u.LastSeen}})
	}
	return out
}

func cfgRows(items []ConfigItem) []tableRow {
	out := make([]tableRow, 0, len(items))
	for _, c := range items {
		out = append(out, tableRow{Cells: []string{c.Key, c.Value}})
	}
	return out
}

func auditRows(events []AuditEvent) []tableRow {
	out := make([]tableRow, 0, len(events))
	for _, e := range events {
		out = append(out, tableRow{Cells: []string{e.At, e.Actor, e.Action, e.Target}})
	}
	return out
}

// ── 工具 ────────────────────────────────────────────────────────────────────

// setSafeHeaders 设最小安全头：禁嗅探、禁缓存（管理台页面不该被中介缓存）。
// **不设**任何标识我们实现的头（OH-2：对手可见面不得暴露栈指纹）。
func setSafeHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}

func (h *Handler) pageSizeParam(r *http.Request) int {
	if v := r.URL.Query().Get("page_size"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return h.scenario.PageSize
}

func pageParam(r *http.Request) int {
	var n int
	if _, err := fmt.Sscanf(r.URL.Query().Get("page"), "%d", &n); err == nil && n > 0 {
		return n
	}
	return 1
}

func (h *Handler) render(w http.ResponseWriter, code int, t *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if err := t.Execute(w, data); err != nil {
		// 渲染失败不再写响应（头已发出）；只记一行（不含数据）。
		_ = err
	}
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func (h *Handler) emit(ev Event) {
	if h.events == nil {
		return
	}
	h.events.Event(context.Background(), ev)
}

var _ http.Handler = (*Handler)(nil)
