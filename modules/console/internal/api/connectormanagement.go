package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"shen/modules/console/internal/connector"
	"shen/modules/console/internal/rbac"
	"shen/modules/console/internal/registry"
)

// 接入管理面：`/api/v1/connectors`（浏览器侧，RBAC 沿用 registry:read/write）。
//
// 凭证管理三原则（与 llm 提供方同一纪律）：
//
//	① key 明文只在签发 / 重置的**响应**里出现一次，此后只剩哈希与脱敏提示；
//	② 审计只记「干了什么」不记密钥内容；
//	③ 每次变更带版本号（重置 / 吊销即版本+1）。
func (s *Server) connectorRoutes(route func(string, rbac.Permission, http.HandlerFunc)) {
	route("GET /api/v1/connectors", rbac.RegistryRead, s.withConnector(s.handleConnectorOverview))
	route("POST /api/v1/connectors/credentials", rbac.RegistryWrite, s.withConnector(s.handleIssueCredential))
	route("POST /api/v1/connectors/credentials/{id}/revoke", rbac.RegistryWrite, s.withConnector(s.handleRevokeCredential))
	route("POST /api/v1/connectors/credentials/{id}/reset", rbac.RegistryWrite, s.withConnector(s.handleResetCredential))
}

// withConnector：未装配连接器服务时统一 503。
func (s *Server) withConnector(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Connector == nil {
			writeError(w, http.StatusServiceUnavailable, "connector_disabled", "连接器接入未启用")
			return
		}
		h(w, r)
	}
}

// connectorOverview 是「接入管理」页的聚合视图：网关状态 + 凭证（带实时在线态）+ 会话 + KPI。
type connectorOverview struct {
	Gateway struct {
		// Configured：网关是否曾拉取过 key 表（部署与令牌配置都正确才会出现联系记录）。
		Configured bool      `json:"configured"`
		LastSeen   time.Time `json:"last_seen"`
	} `json:"gateway"`
	Credentials []credentialWithStatus         `json:"credentials"`
	Sessions    []connector.SessionState       `json:"sessions"`
	Events      []connector.SessionEventRecord `json:"events"`
	Kpi         connectorKpi                   `json:"kpi"`
}

type credentialWithStatus struct {
	connector.CredentialView
	// Online：该凭证当前是否有在线会话（凭证卡片的状态灯）。
	Online bool `json:"online"`
}

type connectorKpi struct {
	OnlineConnections  int   `json:"online_connections"`
	TotalCredentials   int   `json:"total_credentials"`
	RevokedCredentials int   `json:"revoked_credentials"`
	DisconnectsToday   int   `json:"disconnects_today"`
	RttMedianMs        int64 `json:"rtt_median_ms"`
}

// 会话心跳 TTL：超过它没有网关上报，会话判离线（网关 keepalive 15s，留 4 倍余量）。
const sessionStaleTTL = 60 * time.Second

func (s *Server) handleConnectorOverview(w http.ResponseWriter, _ *http.Request) {
	svc := s.cfg.Connector
	out := connectorOverview{}
	if at, ok := svc.Sessions().GatewaySeen(); ok {
		out.Gateway.Configured, out.Gateway.LastSeen = true, at
	}
	sessions := svc.Sessions().List(sessionStaleTTL)
	out.Sessions = sessions
	out.Events = svc.Sessions().Events(50)
	creds := svc.Keys().List()
	out.Credentials = make([]credentialWithStatus, 0, len(creds))
	for _, c := range creds {
		out.Credentials = append(out.Credentials, credentialWithStatus{CredentialView: c, Online: svc.Sessions().HasOnlineCredential(c.ID)})
	}
	out.Kpi = connectorKpi{
		TotalCredentials: len(creds), RevokedCredentials: countRevoked(creds),
		DisconnectsToday: svc.Sessions().TodayDisconnects(), RttMedianMs: svc.Sessions().RttMedianMs(),
	}
	for _, sess := range sessions {
		if sess.Online {
			out.Kpi.OnlineConnections++
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func countRevoked(creds []connector.CredentialView) int {
	n := 0
	for _, c := range creds {
		if c.Revoked {
			n++
		}
	}
	return n
}

// handleIssueCredential 签发凭证：响应里带**一次性明文 key**。
func (s *Server) handleIssueCredential(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	var body connector.IssueInput
	if !decodeJSON(w, r, &body) {
		return
	}
	// 域名合法性：与登记表同一口径（支持最左通配）。
	cleaned := make([]string, 0, len(body.Hosts))
	for _, h := range body.Hosts {
		norm, err := registry.NormalizePattern(h)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_field", "hosts："+err.Error())
			return
		}
		cleaned = append(cleaned, norm)
	}
	body.Hosts = cleaned
	if !s.checkCredentialHosts(w, r, body) {
		return
	}
	view, key, err := s.cfg.Connector.Keys().Issue(body, p.Username)
	if err != nil {
		s.failConnector(w, r, err)
		return
	}
	s.record(r, p, "connector.credential.issue", view.Name, "ok",
		"hosts="+strings.Join(view.Hosts, ",")+" · hint="+view.KeyHint)
	writeJSON(w, http.StatusCreated, map[string]any{"credential": view, "key": key})
}

// checkCredentialHosts：凭证域名不得与**其他服务**的登记冲突（自动注册要建同域名路由）。
// 同名服务（手动或既有连接器登记）自身占用的域名放行——那是同一个服务的续签场景。
// 返回 false 时已写出冲突响应。
func (s *Server) checkCredentialHosts(w http.ResponseWriter, _ *http.Request, in connector.IssueInput) bool {
	for _, svc := range s.registry.List() {
		if strings.EqualFold(svc.Name, in.Name) {
			continue
		}
		for _, h := range svc.Hosts {
			for _, want := range in.Hosts {
				if strings.EqualFold(h, want) {
					writeError(w, http.StatusConflict, "duplicate",
						"域名 "+want+" 已属于服务「"+svc.Name+"」")
					return false
				}
			}
		}
	}
	return true
}

func (s *Server) handleRevokeCredential(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	id := r.PathValue("id")
	view, err := s.cfg.Connector.Keys().Revoke(id)
	if err != nil {
		s.failConnector(w, r, err)
		return
	}
	s.record(r, p, "connector.credential.revoke", view.Name, "ok", view.KeyHint)
	writeJSON(w, http.StatusOK, map[string]any{"credential": view})
}

func (s *Server) handleResetCredential(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	id := r.PathValue("id")
	view, key, err := s.cfg.Connector.Keys().Reset(id)
	if err != nil {
		s.failConnector(w, r, err)
		return
	}
	s.record(r, p, "connector.credential.reset", view.Name, "ok",
		"更换密钥（旧密钥即刻作废）· "+view.KeyHint)
	writeJSON(w, http.StatusOK, map[string]any{"credential": view, "key": key})
}

// failConnector 把凭证表领域错误映射成状态码。
func (s *Server) failConnector(w http.ResponseWriter, r *http.Request, err error) {
	var ve *connector.ValidationError
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, "invalid_field", err.Error())
	case errors.Is(err, connector.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, connector.ErrDuplicate):
		writeError(w, http.StatusConflict, "duplicate", err.Error())
	default:
		s.fail(w, r, err)
	}
}
