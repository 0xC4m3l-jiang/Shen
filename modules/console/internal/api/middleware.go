package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"shen/modules/console/internal/audit"
	"shen/modules/console/internal/rbac"
)

// principal 是一次请求的已认证主体（会话或只读令牌）。
type principal struct {
	Username   string
	Role       rbac.Role
	CSRF       string
	MustChange bool
	Token      bool   // 只读自动化令牌（不是浏览器会话）
	session    string // 会话令牌原值（仅用于登出 / 改密轮换）
}

type ctxKey struct{}

func principalOf(r *http.Request) principal {
	p, _ := r.Context().Value(ctxKey{}).(principal)
	return p
}

const (
	maxBodyBytes       = 64 << 10
	maxConfigBodyBytes = 1 << 20
)

func safeMethod(m string) bool { return m == http.MethodGet || m == http.MethodHead }

// secure 给所有响应加安全头，并拒绝跨域预检：本服务**永不**输出 CORS 允许头 ——
// 浏览器只能从同源（nginx 前端）调用，其他站点的脚本读不到任何响应。
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if r.Method == http.MethodOptions {
			writeError(w, http.StatusForbidden, "cors_disabled", "本接口不支持跨域调用")
			return
		}
		if !safeMethod(r.Method) {
			limit := int64(maxBodyBytes)
			// 欺骗管控数据集的写入 / 预检 / 核心上报可能较大（上千条诱饵）：单独放宽到 1 MiB。
			if strings.HasPrefix(r.URL.Path, "/api/v1/config/") || r.URL.Path == "/api/v1/integration/deception/report" {
				limit = maxConfigBodyBytes
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

// checkOrigin 执行来源校验（Fetch Metadata + Origin 白名单）。返回 false 时已写出 403。
func (s *Server) checkOrigin(w http.ResponseWriter, r *http.Request) bool {
	site := r.Header.Get("Sec-Fetch-Site")
	origin := r.Header.Get("Origin")
	if site == "cross-site" || (site == "same-site" && !s.originAllowed(origin, r)) {
		s.deny(w, r, "origin_rejected", "拒绝跨站请求")
		return false
	}
	if origin != "" && !s.originAllowed(origin, r) {
		s.deny(w, r, "origin_rejected", "来源 "+origin+" 不在允许列表")
		return false
	}
	return true
}

func (s *Server) originAllowed(origin string, r *http.Request) bool {
	if origin == "" || origin == "null" {
		return false
	}
	if s.origins[strings.ToLower(strings.TrimSuffix(origin, "/"))] {
		return true
	}
	if len(s.origins) > 0 {
		return false
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host) // 未配置白名单时只接受同源
}

func (s *Server) deny(w http.ResponseWriter, r *http.Request, code, msg string) {
	if !safeMethod(r.Method) {
		p := principalOf(r)
		s.record(r, p, "request.denied", r.URL.Path, "denied", code)
	}
	writeError(w, http.StatusForbidden, code, msg)
}

// authenticate 解析请求主体：优先会话 Cookie；其次只读令牌（仅限允许的来源网段）。
func (s *Server) authenticate(r *http.Request) (principal, bool) {
	if c, err := r.Cookie(s.cookieName()); err == nil && c.Value != "" {
		if sess, ok := s.auth.Authenticate(c.Value); ok {
			return principal{Username: sess.Username, Role: sess.Role, CSRF: sess.CSRF,
				MustChange: sess.MustChange, session: c.Value}, true
		}
	}
	if raw := r.Header.Get("Authorization"); strings.HasPrefix(raw, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))
		if s.auth.CheckAPIToken(token) && s.tokenSourceAllowed(r) {
			return principal{Username: "api-token", Role: rbac.Viewer, Token: true}, true
		}
	}
	return principal{}, false
}

// protect 包装受保护路由：来源 → 认证 → 首次改密闸门 → 授权 → CSRF。默认拒绝。
func (s *Server) protect(perm rbac.Permission, allowMustChange bool, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkOrigin(w, r) {
			return
		}
		p, ok := s.authenticate(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "未登录或会话已过期")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, p))
		if p.Token && !safeMethod(r.Method) {
			s.deny(w, r, "token_read_only", "只读令牌不能执行写操作")
			return
		}
		if p.MustChange && !allowMustChange {
			s.deny(w, r, "password_change_required", "首次登录或口令被重置：请先修改口令")
			return
		}
		if perm != "" && !rbac.Can(p.Role, perm) {
			s.deny(w, r, "forbidden", "当前角色无权访问此功能")
			return
		}
		if !safeMethod(r.Method) && !p.Token {
			got := r.Header.Get("X-CSRF-Token")
			if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(p.CSRF)) != 1 {
				s.deny(w, r, "csrf_failed", "CSRF 校验失败：请刷新页面后重试")
				return
			}
		}
		h(w, r)
	}
}

// source 返回客户端地址：直接对端不在可信代理网段时就用它；否则从 XFF **右侧**跳过可信代理取第一个。
// 只看右侧是因为左侧段可由客户端任意伪造。
func (s *Server) source(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	if !containsAddr(s.cfg.TrustedProxies, peer) {
		return peer.String()
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		a = a.Unmap()
		if !containsAddr(s.cfg.TrustedProxies, a) {
			return a.String()
		}
	}
	return peer.String()
}

func (s *Server) tokenSourceAllowed(r *http.Request) bool {
	a, err := netip.ParseAddr(s.source(r))
	return err == nil && containsAddr(s.cfg.TokenSources, a.Unmap())
}

func containsAddr(prefixes []netip.Prefix, a netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func (s *Server) record(r *http.Request, p principal, action, target, result, detail string) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Record(audit.Entry{Actor: p.Username, Role: string(p.Role), Action: action,
		Target: target, Result: result, Source: s.source(r), Detail: detail}); err != nil {
		s.cfg.Logf("console: 写审计失败：%v", err)
	}
}

// decodeJSON 严格解码请求体：只接受 application/json（跨站表单无法伪造）、拒绝未知字段与尾随内容。
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "bad_content_type", "请求体必须是 application/json")
		return false
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "请求体过大")
		} else {
			writeError(w, http.StatusBadRequest, "bad_json", "请求体格式错误："+err.Error())
		}
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_json", "请求体只能包含一个 JSON 对象")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: msg, Code: code})
}
