// Package api 是管控台的**版本化 HTTP 接口**（`/api/v1`）：前端只通过它取数，
// 后端不再提供任何页面（前后端解耦，前端由独立的 nginx 容器同源提供）。
//
// 每条路由注册时声明所需权限；中间件统一执行来源校验、认证、首次改密闸门、授权与 CSRF，**默认拒绝**。
// 边界：只读观测 + 反向链接器登记；不下发策略、不参与请求级判定（`AR-10`）。
package api

import (
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	telemetryv1 "shen/common/api/telemetry/v1"
	"shen/modules/console/internal/audit"
	"shen/modules/console/internal/auth"
	"shen/modules/console/internal/geoip"
	"shen/modules/console/internal/llm"
	"shen/modules/console/internal/rbac"
	"shen/modules/console/internal/registry"
)

// Config 是接口层的运行参数。
type Config struct {
	AllowedOrigins  []string       // 浏览器 Origin 白名单；空 = 只接受与 Host 同源
	TrustedProxies  []netip.Prefix // 只有来自这些网段的请求才采信 X-Forwarded-For
	TokenSources    []netip.Prefix // 只读令牌允许的来源网段
	CookieSecure    bool           // 生产必须为 true（TLS 由 L0 终结）
	SessionAbsolute time.Duration  // 会话 Cookie 的 Max-Age
	AlertScore      float64        // 「高风险」显示阈值（只影响标色）
	MaxStreams      int            // 并发 SSE 连接上限
	Heartbeat       time.Duration  // SSE 心跳间隔
	LLM             *llm.Service   // 大模型分析；nil = 未启用（相关接口统一 503）
	Now             func() time.Time
	Logf            func(format string, args ...any)
}

// Server 是 v1 接口。
type Server struct {
	core     telemetryv1.DeceptionTelemetryClient
	auth     *auth.Service
	registry *registry.Store
	geo      *geoip.DB
	audit    *audit.Log
	cfg      Config
	origins  map[string]bool
	mux      *http.ServeMux
	streams  atomic.Int32
}

// New 装配接口并注册全部路由。
func New(core telemetryv1.DeceptionTelemetryClient, authSvc *auth.Service, reg *registry.Store,
	geo *geoip.DB, aud *audit.Log, cfg Config) *Server {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.AlertScore <= 0 || cfg.AlertScore > 1 {
		cfg.AlertScore = 0.9
	}
	if cfg.MaxStreams <= 0 {
		cfg.MaxStreams = 64
	}
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = 15 * time.Second
	}
	if cfg.SessionAbsolute <= 0 {
		cfg.SessionAbsolute = 12 * time.Hour
	}
	s := &Server{core: core, auth: authSvc, registry: reg, geo: geo, audit: aud, cfg: cfg,
		origins: map[string]bool{}, mux: http.NewServeMux()}
	for _, o := range cfg.AllowedOrigins {
		if o = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(o), "/")); o != "" {
			s.origins[o] = true
		}
	}
	s.routes()
	return s
}

// Handler 返回带安全头的根处理器。
func (s *Server) Handler() http.Handler { return s.secure(s.mux) }

func (s *Server) now() time.Time { return s.cfg.Now() }

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	m.HandleFunc("POST /api/v1/auth/login", s.handleLogin)

	// 只要已登录（含「必须先改密」状态）即可访问的会话类接口。
	m.HandleFunc("GET /api/v1/auth/session", s.protect("", true, s.handleSession))
	m.HandleFunc("POST /api/v1/auth/logout", s.protect("", true, s.handleLogout))
	m.HandleFunc("POST /api/v1/auth/password", s.protect(rbac.SelfManage, true, s.handleChangePassword))
	m.HandleFunc("GET /api/v1/me/preferences", s.protect(rbac.SelfManage, false, s.handleGetPrefs))
	m.HandleFunc("PUT /api/v1/me/preferences", s.protect(rbac.SelfManage, false, s.handlePutPrefs))

	route := func(pattern string, perm rbac.Permission, h http.HandlerFunc) {
		if !rbac.Known(perm) {
			panic("api: 路由 " + pattern + " 引用了未登记的权限 " + string(perm))
		}
		m.HandleFunc(pattern, s.protect(perm, false, h))
	}
	route("GET /api/v1/system/status", rbac.OverviewRead, s.handleStatus)
	route("GET /api/v1/overview", rbac.OverviewRead, s.handleOverview)
	route("GET /api/v1/traffic", rbac.OverviewRead, s.handleTraffic)
	route("GET /api/v1/stream", rbac.StreamRead, s.handleStream)

	route("GET /api/v1/deception/flow", rbac.DeceptionRead, s.handleFlow)
	route("GET /api/v1/deception/graphs", rbac.DeceptionRead, s.handleGraphs)
	route("GET /api/v1/deception/topology", rbac.DeceptionRead, s.handleTopology)
	route("GET /api/v1/deception/trace", rbac.DeceptionRead, s.handleTrace)
	route("GET /api/v1/deception/config", rbac.DeceptionRead, s.handleConfig)

	route("GET /api/v1/honeypot/deliveries", rbac.HoneypotRead, s.handleDeliveries)
	route("GET /api/v1/honeypot/backends", rbac.HoneypotRead, s.handleBackends)

	route("GET /api/v1/analysis", rbac.AnalysisRead, s.handleAnalysis)
	route("GET /api/v1/alerts", rbac.AlertsRead, s.handleAlerts)

	route("GET /api/v1/services", rbac.RegistryRead, s.handleListServices)
	route("GET /api/v1/services/unregistered", rbac.RegistryRead, s.handleUnregisteredHosts)
	route("POST /api/v1/services", rbac.RegistryWrite, s.handleCreateService)
	route("GET /api/v1/services/{id}", rbac.RegistryRead, s.handleGetService)
	route("PUT /api/v1/services/{id}", rbac.RegistryWrite, s.handleUpdateService)
	route("DELETE /api/v1/services/{id}", rbac.RegistryWrite, s.handleDeleteService)
	route("GET /api/v1/services/{id}/traffic", rbac.RegistryRead, s.handleServiceTraffic)

	route("GET /api/v1/users", rbac.UsersAdmin, s.handleListUsers)
	route("POST /api/v1/users", rbac.UsersAdmin, s.handleCreateUser)
	route("PATCH /api/v1/users/{name}", rbac.UsersAdmin, s.handleUpdateUser)
	route("POST /api/v1/users/{name}/password", rbac.UsersAdmin, s.handleResetPassword)
	route("DELETE /api/v1/users/{name}", rbac.UsersAdmin, s.handleDeleteUser)
	route("GET /api/v1/audit", rbac.AuditRead, s.handleAudit)
	s.llmRoutes(route)

	// 其余 /api/ 路径一律 404（不回落到任何页面）。
	m.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "接口不存在")
	})
}

// fail 把领域错误映射成 HTTP 状态码。
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ce *coreError
	var ve *registry.ValidationError
	var locked *auth.LockedError
	switch {
	case errors.As(err, &ce):
		writeError(w, http.StatusBadGateway, "core_unavailable", err.Error())
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, "invalid_field", err.Error())
	case errors.As(err, &locked):
		w.Header().Set("Retry-After", strconv.Itoa(int(locked.RetryAfter.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "locked", err.Error())
	case errors.Is(err, registry.ErrNotFound), errors.Is(err, auth.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, registry.ErrConflict):
		writeError(w, http.StatusConflict, "version_conflict", err.Error())
	case errors.Is(err, registry.ErrDuplicate), errors.Is(err, auth.ErrUserExists):
		writeError(w, http.StatusConflict, "duplicate", err.Error())
	case errors.Is(err, registry.ErrFull):
		writeError(w, http.StatusInsufficientStorage, "full", err.Error())
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrDisabled):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", err.Error())
	case errors.Is(err, auth.ErrPasswordPolicy), errors.Is(err, auth.ErrInvalidUsername),
		errors.Is(err, auth.ErrInvalidRole), errors.Is(err, auth.ErrLastAdmin):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		s.cfg.Logf("console: %s %s 内部错误：%v", r.Method, r.URL.Path, err)
		writeError(w, http.StatusInternalServerError, "internal", "内部错误（详见服务端日志）")
	}
}

func (s *Server) cookieName() string {
	if s.cfg.CookieSecure {
		return "__Host-shen_console" // __Host- 前缀：强制 Secure、Path=/、不带 Domain
	}
	return "shen_console"
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: token, Path: "/",
		MaxAge: int(s.cfg.SessionAbsolute.Seconds()), HttpOnly: true,
		Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode})
}

// handleStatus 返回管控台自身与核心的连通性（页面顶栏的状态灯）。
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	coreOK, coreErr := true, ""
	if _, err := s.listEvents(r.Context(), decisionEventType, 1, time.Time{}); err != nil {
		coreOK, coreErr = false, err.Error()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"core_reachable":  coreOK,
		"core_error":      coreErr,
		"streams_active":  s.streams.Load(),
		"streams_max":     s.cfg.MaxStreams,
		"geo_db_built_at": s.geo.BuiltAt(),
		"alert_score":     s.cfg.AlertScore,
		"server_time":     s.now().UTC(),
		"interaction_events": map[string]any{
			"connected": false,
			"note":      "合成交互事件（登录 / 浏览 / 受限写）目前只进蜜罐后端本地日志，尚未回流核心",
		},
	})
}
