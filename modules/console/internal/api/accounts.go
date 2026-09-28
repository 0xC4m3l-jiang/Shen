package api

import (
	"errors"
	"net/http"

	"shen/modules/console/internal/auth"
	"shen/modules/console/internal/rbac"
)

// sessionView 是登录 / 会话查询的响应：前端据此渲染可见标签页与写操作按钮。
type sessionView struct {
	User        sessionUser       `json:"user"`
	Permissions []rbac.Permission `json:"permissions"`
	CSRFToken   string            `json:"csrf_token"`
	Preferences *auth.Preferences `json:"preferences,omitempty"`
}

type sessionUser struct {
	Username   string    `json:"username"`
	Role       rbac.Role `json:"role"`
	MustChange bool      `json:"must_change"`
}

func (s *Server) sessionOf(p principal) sessionView {
	v := sessionView{
		User:        sessionUser{Username: p.Username, Role: p.Role, MustChange: p.MustChange},
		Permissions: rbac.PermissionsOf(p.Role),
		CSRFToken:   p.CSRF,
	}
	if prefs, err := s.auth.Preferences(p.Username); err == nil {
		v.Preferences = &prefs
	}
	if p.MustChange {
		v.Permissions = []rbac.Permission{rbac.SelfManage}
	}
	return v
}

// handleLogin 是唯一的匿名写接口：同样执行来源校验并只接受 JSON（跨站表单无法触发，防登录 CSRF）。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) {
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	source := s.source(r)
	token, sess, err := s.auth.Login(body.Username, body.Password, source)
	actor := principal{Username: body.Username}
	if err != nil {
		result := "failed"
		var locked *auth.LockedError
		if errors.As(err, &locked) {
			result = "locked"
		}
		s.record(r, actor, "auth.login", body.Username, result, err.Error())
		s.fail(w, r, err)
		return
	}
	p := principal{Username: sess.Username, Role: sess.Role, CSRF: sess.CSRF, MustChange: sess.MustChange}
	s.record(r, p, "auth.login", sess.Username, "ok", "")
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, s.sessionOf(p))
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.sessionOf(principalOf(r)))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	if p.session != "" {
		s.auth.Logout(p.session)
	}
	s.record(r, p, "auth.logout", p.Username, "ok", "")
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	token, sess, err := s.auth.ChangePassword(p.Username, body.OldPassword, body.NewPassword, s.source(r))
	if err != nil {
		s.record(r, p, "auth.password_change", p.Username, "failed", err.Error())
		s.fail(w, r, err)
		return
	}
	s.record(r, p, "auth.password_change", p.Username, "ok", "")
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, s.sessionOf(principal{Username: sess.Username, Role: sess.Role, CSRF: sess.CSRF}))
}

func (s *Server) handleGetPrefs(w http.ResponseWriter, r *http.Request) {
	prefs, err := s.auth.Preferences(principalOf(r).Username)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (s *Server) handlePutPrefs(w http.ResponseWriter, r *http.Request) {
	var body auth.Preferences
	if !decodeJSON(w, r, &body) {
		return
	}
	prefs, err := s.auth.SetPreferences(principalOf(r).Username, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (s *Server) handleListUsers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"users": s.auth.Users(), "roles": rbac.Roles()})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	var body struct {
		Username string    `json:"username"`
		Role     rbac.Role `json:"role"`
		Password string    `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	u, err := s.auth.CreateUser(body.Username, body.Role, body.Password)
	if err != nil {
		s.record(r, p, "user.create", body.Username, "failed", err.Error())
		s.fail(w, r, err)
		return
	}
	s.record(r, p, "user.create", u.Username, "ok", "role="+string(u.Role))
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	p, name := principalOf(r), r.PathValue("name")
	var body struct {
		Role     *rbac.Role `json:"role"`
		Disabled *bool      `json:"disabled"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if name == p.Username && body.Disabled != nil && *body.Disabled {
		writeError(w, http.StatusBadRequest, "invalid_request", "不能停用当前登录的账号")
		return
	}
	u, err := s.auth.UpdateUser(name, body.Role, body.Disabled)
	if err != nil {
		s.record(r, p, "user.update", name, "failed", err.Error())
		s.fail(w, r, err)
		return
	}
	s.record(r, p, "user.update", name, "ok", "role="+string(u.Role))
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	p, name := principalOf(r), r.PathValue("name")
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.auth.ResetPassword(name, body.Password); err != nil {
		s.record(r, p, "user.password_reset", name, "failed", err.Error())
		s.fail(w, r, err)
		return
	}
	s.record(r, p, "user.password_reset", name, "ok", "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	p, name := principalOf(r), r.PathValue("name")
	if name == p.Username {
		writeError(w, http.StatusBadRequest, "invalid_request", "不能删除当前登录的账号")
		return
	}
	if err := s.auth.DeleteUser(name); err != nil {
		s.record(r, p, "user.delete", name, "failed", err.Error())
		s.fail(w, r, err)
		return
	}
	s.record(r, p, "user.delete", name, "ok", "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 200, 1, 1000)
	writeJSON(w, http.StatusOK, map[string]any{"entries": s.audit.Recent(limit, r.URL.Query().Get("actor"))})
}
