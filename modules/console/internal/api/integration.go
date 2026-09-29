package api

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"shen/modules/console/internal/connector"
)

// 集成面：`/api/v1/integration/*` —— **网关专用**（不面向浏览器）。
//
// 与会话面的区别：这里不用 RBAC（网关不是人），用**集成令牌**（恒定时间比较 +
// 来源网段限制，与只读令牌同款纪律）；写操作有（注册上报与会话上报），但都是
// 网关的被动接收，控制台自身仍然不出站。
func (s *Server) integrationRoutes() {
	m := s.mux
	m.HandleFunc("GET /api/v1/integration/keys", s.withIntegration(s.handleIntegrationKeys))
	m.HandleFunc("POST /api/v1/integration/register", s.withIntegration(s.handleIntegrationRegister))
	m.HandleFunc("POST /api/v1/integration/sessions", s.withIntegration(s.handleIntegrationSession))
}

// withIntegration：未启用统一 503（接口存在，只是没配令牌）；令牌不对 401。
func (s *Server) withIntegration(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Connector == nil || s.cfg.IntegrationToken == "" {
			writeError(w, http.StatusServiceUnavailable, "integration_disabled", "连接器集成面未启用（未配置集成令牌）")
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.IntegrationToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "integration_unauthorized", "集成令牌无效")
			return
		}
		if !s.tokenSourceAllowed(r) {
			writeError(w, http.StatusForbidden, "source_rejected", "来源网段不在集成面允许列表")
			return
		}
		h(w, r)
	}
}

// handleIntegrationKeys：网关定期拉取凭证哈希表（吊销在这个周期内生效）。
func (s *Server) handleIntegrationKeys(w http.ResponseWriter, r *http.Request) {
	s.cfg.Connector.Sessions().TouchGateway()
	writeJSON(w, http.StatusOK, map[string]any{
		"keys":    s.cfg.Connector.Keys().GatewayKeys(),
		"now":     s.now().UTC(),
		"refresh": 30, // 建议拉取周期（秒）；网关侧可配置
	})
}

// handleIntegrationRegister：连接器握手成功后的自动登记上报。
func (s *Server) handleIntegrationRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string   `json:"name"`
		Hosts     []string `json:"hosts"`
		LocalAddr string   `json:"local_addr"` // 连接器侧真实业务地址
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	svc, created, err := s.registry.UpsertConnector(body.Name, body.Hosts, body.LocalAddr, "gateway")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	result := "updated"
	if created {
		result = "created"
	} else if svc.Source != "connector" {
		result = "manual_kept" // 同名手动登记存在：不覆盖（观测面能看到这条事实）
	}
	s.record(r, principal{Username: "gateway"}, "connector.register", svc.Name, result,
		strings.Join(svc.Hosts, ","))
	writeJSON(w, http.StatusOK, map[string]any{"service": svc, "result": result})
}

// handleIntegrationSession：会话状态上报（连接建立 / 断开 / 心跳刷新）。
// 状态过渡（online/offline/reconnect）写审计；纯心跳不写（防刷屏）。
func (s *Server) handleIntegrationSession(w http.ResponseWriter, r *http.Request) {
	var body connector.SessionState
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.SessionID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_field", "session_id 不能为空")
		return
	}
	detail := r.URL.Query().Get("detail") // 可选：网关给的说明（如「计划内下线」「连接断开」）
	transition := s.cfg.Connector.Sessions().Upsert(body, detail)
	if body.CredentialID != "" && body.Online {
		s.cfg.Connector.Keys().Touch(body.CredentialID)
	}
	if transition != "" {
		action := "connector.session." + transition
		s.record(r, principal{Username: "gateway"}, action, body.Name, transition,
			body.SessionID+auditDetail(detail))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "transition": transition})
}

func auditDetail(detail string) string {
	if detail == "" {
		return ""
	}
	return "：" + detail
}
