package web

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 本文件是**场景的执行**：把一套 `Scenario` 服务成一排路由。
//
// 与前一份文件的分工：`scenario.go` 是**数据**（虚构组织/账号/配置/审计 + 分页规则），
// 本文件是**行为**（登录 → 会话 → 页面/API）。行为只读数据、不改数据 ——
// 诱饵后端不需要真的写状态，所有"写"都是合成回应（方案 C03：受限读写、合成演示会话）。

// adminPrefix 是页面与 API **共用的**前缀（`W1`）。
//
// 为什么必须共用：浏览器只在匹配的路径上回传 cookie。页面在 `/admin/...`、API 在 `/api/v1/admin/...`
// 时，cookie 的 `Path` 无论设哪个都无法覆盖两边 —— 真人浏览器登录后调 API 会拿到 401，
// 而"手工塞 cookie"的测试永远看不见。共用前缀后 `Path=/admin` 同时覆盖页面与 API。
const adminPrefix = "/admin"

// outcomeThrottled 是「登录被配额拒绝」的事件结果值（登记值，改它要同步模块文档）。
const outcomeThrottled = "throttled"

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
	// CookieSecure 决定合成会话 cookie 是否带 `Secure`（`SHEN_WEB_COOKIE_SECURE`）。
	//
	// 依据**客户端到诱饵的连接**，不是内部回源那一跳：现场用 HTTPS 访问幻境就应当打开。
	CookieSecure bool
	// LoginBurst / LoginWindow 是单个来源的登录配额（0 = 默认 10 次/分钟）。
	LoginBurst  int
	LoginWindow time.Duration
	// EventQueue 是事件队列深度（0 = 默认 256）。
	EventQueue int
	// RequestLog 打开逐请求日志（默认关：事件已经记录了交互，日志只用于本地排查）。
	RequestLog bool
	// Now 与 Rand 可注入，使会话行为可测（内容本身与时钟无关）。
	Now  func() time.Time
	Rand io.Reader
}

// Handler 是 Web 诱饵后端的 HTTP 处理器（一个 http.Handler）。
type Handler struct {
	scenario Scenario
	maxBody  int64
	ttl      time.Duration
	now      func() time.Time
	rand     io.Reader

	cookieSecure bool
	requestLog   bool
	login        *loginLimiter
	emitter      *asyncEmitter

	mu       sync.Mutex
	sessions map[string]*sessionState // 合成会话：id -> 到期时刻 + **可变场景状态**（C06）
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
		scenario:     sc,
		maxBody:      opts.MaxBodyBytes,
		ttl:          opts.SessionTTL,
		now:          opts.Now,
		rand:         opts.Rand,
		cookieSecure: opts.CookieSecure,
		requestLog:   opts.RequestLog,
		login:        newLoginLimiter(opts.LoginBurst, opts.LoginWindow),
		sessions:     map[string]*sessionState{},
	}
	if opts.Events != nil {
		h.emitter = newAsyncEmitter(opts.Events, opts.EventQueue)
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
		if _, ok := h.sessionOf(r); !ok {
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		h.emitFor(r, Event{Kind: EventPageView, Outcome: "dashboard"})
		h.render(w, http.StatusOK, tmplDashboard, h.dashboardView())
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, adminPrefix+"/api/"):
		// 受限写路径（C06）：JSON API 与页面表单走**同一套状态机**（写后读两边一致）。
		h.handleAdminWrite(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, adminPrefix+"/"):
		h.handleAdminPageWrite(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, adminPrefix+"/api/"):
		// 浏览器链路（`W1`）：与页面同前缀 ⇒ cookie（Path=/admin）自动带上。
		h.handleAdminAPI(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/admin/"):
		h.handleAdminPage(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/admin/"):
		// 兼容入口（工具 / 手工复测）：同一份数据、同一套分页；**不**声称浏览器可用
		// —— 这条路径不在 cookie 的 Path 内，真人浏览器需要显式带 cookie。
		h.handleAdminAPI(w, r)
	default:
		h.render(w, http.StatusNotFound, tmplError,
			h.errViewOf("Not found", http.StatusNotFound, "The requested resource does not exist."))
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
			h.render(w, http.StatusRequestEntityTooLarge, tmplError,
				h.errViewOf("Request too large", http.StatusRequestEntityTooLarge,
					"The submitted form exceeds the allowed size."))
			return
		}
		h.render(w, http.StatusBadRequest, tmplError,
			h.errViewOf("Bad request", http.StatusBadRequest, "The submitted form could not be read."))
		return
	}
	// 只取用户名（用于合成会话的展示与事件）；**其余字段读完即弃**。
	user := strings.TrimSpace(r.PostForm.Get("username"))
	// 凭据清理（`W4`）：`ParseForm` 会把同一份表单放进 `PostForm` **与** `Form` 两张表，
	// 只删一张等于把密码原样留在请求对象上（谁后来读了 `r.Form` 都能拿到）。
	// 注意：Go 的字符串无法安全清零，这里只能确保**不再有引用**，不能承诺内存被擦除。
	r.PostForm.Del("password")
	if r.Form != nil {
		r.Form.Del("password")
	}
	// 登录配额（`W6`）：合成登录台也要有速率上限 —— 否则「无限次尝试」既是资源放大器，
	// 也是一个可被对手观察到「不在乎任何凭据」的异常信号。
	if !h.allowLogin(clientIPOf(r)) {
		h.emit(Event{Kind: EventLoginAttempt, Path: r.URL.Path, User: user, Outcome: outcomeThrottled})
		h.render(w, http.StatusTooManyRequests, tmplError,
			h.errViewOf("Too many attempts", http.StatusTooManyRequests,
				"Too many sign-in attempts from this address. Try again later."))
		return
	}

	if h.requestLog {
		log.Printf("web: 合成登录尝试 来源=%s 用户=%q 场景=%s", clientIPOf(r), user, h.scenario.ID)
	}
	if h.scenario.Outcome == LoginFails {
		h.emit(Event{Kind: EventLoginAttempt, Path: r.URL.Path, User: user, Outcome: string(LoginFails)})
		h.render(w, http.StatusUnauthorized, tmplError,
			h.errViewOf("Sign-in failed", http.StatusUnauthorized,
				"The provided credentials were not accepted."))
		return
	}
	id, err := h.newSession(user)
	if err != nil {
		h.render(w, http.StatusInternalServerError, tmplError,
			h.errViewOf("Unavailable", http.StatusInternalServerError,
				"The console is temporarily unavailable."))
		return
	}
	// `Secure` 由**客户端到诱饵的连接语义**决定，不由内部回源协议决定（W1 的纠正）：
	// 现场若用 HTTPS 访问幻境，就应当设 `SHEN_WEB_COOKIE_SECURE=1`（本进程可能只看到明文那一跳）。
	// 默认 false 是为了本地/manual 复测可用（明文下设了它浏览器就不回传，演示会话当场失效）。
	//
	// `Path=/admin`（`W1`）：**页面与 API 必须在同一个前缀下** —— 浏览器只在匹配的路径上回传 cookie，
	// 原来 API 挂在 `/api/v1/admin/*`，于是真人浏览器登录后调 API 会拿到 401，而手工塞 cookie 的单测
	// 永远看不见这个问题（幻境自己的 API 也要按浏览器真实行为走）。兼容入口仍在（见 ServeHTTP 路由表），
	// 但**浏览器链路**是 `/admin/api/*`。
	// nosemgrep: go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure
	http.SetCookie(w, &http.Cookie{
		Name:     scratchSessionCookie,
		Value:    id,
		Path:     adminPrefix,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.ttl.Seconds()),
	})
	// 会话标识显式带上：这一刻 cookie 刚生成、**还没进请求**（浏览器要下一次才会回传），
	// 靠 `sessionMarker(r)` 取只会拿到空 —— 而"哪次会话由哪次登录建立"恰恰是最该关联的一笔。
	h.emit(Event{
		Kind: EventLoginAttempt, Path: r.URL.Path, User: user,
		Outcome: string(LoginDemo), Session: scrubSession(id),
	})
	http.Redirect(w, r, "/admin/", http.StatusFound)
}

// handleAdminPage 渲染合成列表页（与 API 共用同一份数据与分页）。
func (h *Handler) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.sessionOf(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusFound)
		return
	}
	page := pageParam(r)
	h.emitFor(r, Event{Kind: EventPageView, Outcome: "page"})
	switch r.URL.Path {
	case "/admin/users":
		p := paginate(sess.users, page, h.pageSizeParam(r), h.scenario.MaxPageSize)
		h.render(w, http.StatusOK, tmplList, h.listView("Users", metaOf("/admin/users", p),
			[]string{"ID", "Name", "Role", "State", "Last seen"}, userRows(p.Items)))
	case "/admin/config":
		p := paginate(sess.config, page, h.pageSizeParam(r), h.scenario.MaxPageSize)
		h.render(w, http.StatusOK, tmplList, h.listView("Configuration", metaOf("/admin/config", p),
			[]string{"Key", "Value"}, cfgRows(p.Items)))
	case "/admin/audit":
		p := paginate(latestFirst(sess.audit), page, h.pageSizeParam(r), h.scenario.MaxPageSize)
		h.render(w, http.StatusOK, tmplList, h.listView("Audit log", metaOf("/admin/audit", p),
			[]string{"Time", "Actor", "Action", "Target"}, auditRows(p.Items)))
	default:
		h.render(w, http.StatusNotFound, tmplError,
			h.errViewOf("Not found", http.StatusNotFound, "The requested page does not exist."))
	}
}

// handleAdminAPI 返回合成 JSON（与页面同源：同一个 `paginate`，同一批对象）。
func (h *Handler) handleAdminAPI(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.sessionOf(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		return
	}
	page := pageParam(r)
	size := h.pageSizeParam(r)
	h.emitFor(r, Event{Kind: EventPageView, Outcome: "api"})
	resource, ok := apiResource(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	var body any
	switch resource {
	case "users":
		body = paginate(sess.users, page, size, h.scenario.MaxPageSize)
	case "config":
		body = paginate(sess.config, page, size, h.scenario.MaxPageSize)
	case "audit":
		body = paginate(latestFirst(sess.audit), page, size, h.scenario.MaxPageSize)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// apiResource 把两种 API 路径形状归一到资源名：
// `/admin/api/{users,config,audit}`（浏览器链路）与 `/api/v1/admin/{...}`（兼容入口）。
func apiResource(path string) (string, bool) {
	trimmed := ""
	switch {
	case strings.HasPrefix(path, adminPrefix+"/api/"):
		trimmed = strings.TrimPrefix(path, adminPrefix+"/api/")
	case strings.HasPrefix(path, "/api/v1/admin/"):
		trimmed = strings.TrimPrefix(path, "/api/v1/admin/")
	default:
		return "", false
	}
	switch trimmed {
	case "users", "config", "audit":
		return trimmed, true
	default:
		return "", false
	}
}

// allowLogin 报告该来源是否还有登录配额（`W6`）。
func (h *Handler) allowLogin(source string) bool {
	return h.login.allow(source, h.now())
}

// evictOldestLocked 逐出最早到期的 n 条会话（调用方持锁）。
func (h *Handler) evictOldestLocked(n int) {
	type entry struct {
		id  string
		exp time.Time
	}
	all := make([]entry, 0, len(h.sessions))
	for id, st := range h.sessions {
		all = append(all, entry{id: id, exp: st.expires})
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].exp.Equal(all[j].exp) {
			return all[i].exp.Before(all[j].exp)
		}
		return all[i].id < all[j].id // 同到期时刻按 id，保证确定性（可测）
	})
	for i := range min(n, len(all)) {
		delete(h.sessions, all[i].id)
	}
}

// ── 合成会话（内存、有 TTL、有容量上限）─────────────────────────────────────

// newSession 建一个合成会话：不透明 id + **该会话自己的可变场景状态**（`C06` 的隔离单位）。
//
// 为什么状态随会话走：写后读要求"同会话可见"，跨会话隔离要求"别人看不到" ——
// 一个 `*sessionState`（登录时从场景基础数据深拷贝）同时满足这两条。
func (h *Handler) newSession(actor string) (string, error) {
	buf := make([]byte, 16)
	if _, err := io.ReadFull(h.rand, buf); err != nil {
		return "", err
	}
	id := hex.EncodeToString(buf)
	exp := h.now().Add(h.ttl)
	state := newSessionState(h.scenario, actor, exp)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.sessions) >= maxSessions {
		// 容量满：先清已过期的；仍满则**逐出最早到期的少数几条**（`W6`）。
		//
		// 为什么不是整体清空（原实现）：那会让**所有正在演示的会话同时掉线** ——
		// 对一个正在观察幻境的对手来说，"整个管理台突然把我踢出去"本身就是一个异常信号；
		// 而逐出最老的一批只影响最不可能还在活动的那部分。
		now := h.now()
		for k, st := range h.sessions {
			if !now.Before(st.expires) {
				delete(h.sessions, k)
			}
		}
		if len(h.sessions) >= maxSessions {
			h.evictOldestLocked(maxSessions/8 + 1)
		}
	}
	h.sessions[id] = state
	return id, nil
}

// sessionOf 取请求对应的**会话状态**（未登录/已过期 ⇒ false）。
//
// 它同时是"写后读"的可见性保证：同一会话的读取与写入拿到的是**同一个** `*sessionState`。
func (h *Handler) sessionOf(r *http.Request) (*sessionState, bool) {
	c, err := r.Cookie(scratchSessionCookie)
	if err != nil || c.Value == "" {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.sessions[c.Value]
	if !ok {
		return nil, false
	}
	if !h.now().Before(st.expires) {
		delete(h.sessions, c.Value)
		return nil, false
	}
	return st, true
}

// ── 视图（模板随代码分发：ST-22；不含任何真实素材）─────────────────────────

// errView 是合成错误页的视图。
//
// `Org` / `Product` 必须齐全（`W3`）：公共头与 `<title>` 读它们，缺了就渲染出空标题 ——
// 状态码对、正文却是半成品，正是「错误页看起来像坏掉的页面」那种破绽。
type errView struct {
	Org     string
	Product string
	Title   string
	Code    int
	Detail  string
}

// errViewOf 造一个补齐公共字段的错误页视图（所有错误分支都走它）。
func (h *Handler) errViewOf(title string, code int, detail string) errView {
	return errView{Org: h.scenario.Org, Product: h.scenario.Product, Title: title, Code: code, Detail: detail}
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
	// 链接**必须带上 page_size**（`W2`）：翻页时丢掉它会让「每页 10 条」突然变成默认 3 条，
	// 这种跳变在真实产品里不会出现，是对手可以察觉的不一致。
	link := func(page int) string {
		return fmt.Sprintf("%s?page=%d&page_size=%d", base, page, p.PageSize)
	}
	if p.NextPage != nil {
		m.HasNext = true
		m.NextHref = link(*p.NextPage)
	}
	if p.Page > 1 {
		m.PrevHref = link(p.Page - 1)
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
	if n, ok := intParam(r, "page_size"); ok {
		return n
	}
	return h.scenario.PageSize
}

func pageParam(r *http.Request) int {
	if n, ok := intParam(r, "page"); ok {
		return n
	}
	return 1
}

// maxPageParam 是查询参数里页号的硬上限（`W2`）。
//
// 为什么要有它：即使 `paginate` 已经不会溢出，也不该把「页号 = 2⁶³-1」这种请求当成正常输入；
// 超过上限直接按非法处理（回落默认页），与「非数字」同等对待。
const maxPageParam = 1_000_000

// intParam 严格解析正整数查询参数：**整串都是数字**才算（`strconv.Atoi`），
// 非法 / 非正 / 超上限一律返回 false 由调用方回落默认值。
//
// 为什么不用 `fmt.Sscanf("%d")`：它只解析**前缀** —— `page=12abc` 会被当成 12，
// 于是「伪造的怪参数」与「正常参数」得到同一个结果，边界行为也就没法测。
func intParam(r *http.Request, name string) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 || n > maxPageParam {
		return 0, false
	}
	return n, true
}

// render 渲染模板并写出。**先渲染到缓冲区再写状态码**（`W3`）。
//
// 为什么不能直接 `t.Execute(w, ...)`：一旦模板执行到一半失败，状态码与半截 HTML 已经发给客户端 ——
// 对手看到的是一个「断了的管理台」，比干净的错误页更可疑，而且本地无从察觉（原来把错误丢掉了）。
// 现在：渲染失败 ⇒ 记一行（不含数据）+ 回落到静态兜底页（200 字节、无模板依赖）。
func (h *Handler) render(w http.ResponseWriter, code int, t *template.Template, data any) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		log.Printf("web: 模板渲染失败（回落到静态错误页，状态码按 %d 返回）：%v", http.StatusInternalServerError, err)
		writeFallbackPage(w, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(buf.Bytes())
}

// writeFallbackPage 写一个**不依赖任何模板**的兜底页（模板自身坏掉时的最后一道）。
func writeFallbackPage(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, fallbackErrorPage)
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	// 同一个理由：先编码到缓冲区，失败时不要发出半截 JSON。
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		log.Printf("web: JSON 编码失败（返回 not_found）：%v", err)
		buf.Reset()
		_ = json.NewEncoder(&buf).Encode(map[string]string{"error": "not_found"})
		code = http.StatusNotFound
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(buf.Bytes())
}

// emit 发出一条合成交互事件（`W5`）：
//   - `At` / `Path` 由这里补齐（调用点不必逐个记得设，它们是最容易漏的字段）；
//   - 会话标识**脱敏后**才进事件（`Session` 是可关联的哈希前缀，不是 cookie 值本身）；
//   - 投递是异步有界的（见 `asyncEmitter`）：慢接收方不影响合成交互的响应时间。
func (h *Handler) emit(ev Event) {
	if h.emitter == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = h.now()
	}
	if ev.Session == "" {
		ev.Session = h.sessionMarker(nil)
	}
	h.emitter.emit(ev)
}

// emitFor 发一条带请求上下文的事件（补 path / 脱敏会话）。
func (h *Handler) emitFor(r *http.Request, ev Event) {
	ev.Path = r.URL.Path
	ev.Session = h.sessionMarker(r)
	h.emit(ev)
}

// sessionMarker 取请求的合成会话并脱敏（无会话时为空）。
func (h *Handler) sessionMarker(r *http.Request) string {
	if r == nil {
		return ""
	}
	c, err := r.Cookie(scratchSessionCookie)
	if err != nil {
		return ""
	}
	return scrubSession(c.Value)
}

// Close 停止事件投递（幂等）。装配层在进程退出前调用。
func (h *Handler) Close() { h.emitter.Close() }

// DroppedEvents 返回因队列满而丢弃的合成交互事件数（可观测，不静默）。
func (h *Handler) DroppedEvents() uint64 { return h.emitter.Dropped() }

// EventFailures 返回投递失败的次数（含 sink panic）。
func (h *Handler) EventFailures() uint64 { return h.emitter.Failures() }

var _ http.Handler = (*Handler)(nil)
