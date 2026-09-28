package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"shen/modules/console/internal/llm"
	"shen/modules/console/internal/rbac"
)

// 大模型分析接口。边界：模型输出只给人看，不回灌策略；出站只在用户显式操作时发生。
const (
	maxAnalysisTraffic = 50       // 一次会话最多带多少条流量（上下文太大既贵又稀释重点）
	maxContextBytes    = 48 << 10 // 流量快照上限
	llmTrafficSpan     = 24 * time.Hour
)

func (s *Server) llmRoutes(route func(string, rbac.Permission, http.HandlerFunc)) {
	route("GET /api/v1/llm/providers", rbac.LLMUse, s.withLLM(s.handleListProviders))
	route("POST /api/v1/llm/providers", rbac.LLMAdmin, s.withLLM(s.handleCreateProvider))
	route("PUT /api/v1/llm/providers/{id}", rbac.LLMAdmin, s.withLLM(s.handleUpdateProvider))
	route("DELETE /api/v1/llm/providers/{id}", rbac.LLMAdmin, s.withLLM(s.handleDeleteProvider))
	route("POST /api/v1/llm/providers/{id}/test", rbac.LLMAdmin, s.withLLM(s.handleTestProvider))
	// 基础能力：按已填的地址与密钥探测可用模型清单（供提供方表单「获取模型列表」按钮）。
	route("POST /api/v1/llm/models/probe", rbac.LLMAdmin, s.withLLM(s.handleProbeModels))
	route("GET /api/v1/llm/usage", rbac.LLMUse, s.withLLM(s.handleUsage))
	route("GET /api/v1/llm/conversations", rbac.LLMUse, s.withLLM(s.handleListConversations))
	route("POST /api/v1/llm/conversations", rbac.LLMUse, s.withLLM(s.handleCreateConversation))
	route("GET /api/v1/llm/conversations/{id}", rbac.LLMUse, s.withLLM(s.handleGetConversation))
	route("POST /api/v1/llm/conversations/{id}/messages", rbac.LLMUse, s.withLLM(s.handleSendMessage))
	route("DELETE /api/v1/llm/conversations/{id}", rbac.LLMUse, s.withLLM(s.handleDeleteConversation))
}

// withLLM：未装配大模型服务时统一 503（不是 404：接口存在，只是没启用）。
func (s *Server) withLLM(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.LLM == nil {
			writeError(w, http.StatusServiceUnavailable, "llm_disabled", "大模型分析未启用")
			return
		}
		h(w, r)
	}
}

// failLLM 把 llm 包的领域错误映射成状态码；其余交给通用 fail。
func (s *Server) failLLM(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case llm.IsValidation(err):
		writeError(w, http.StatusBadRequest, "invalid_field", err.Error())
	case errors.Is(err, llm.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, llm.ErrDuplicate):
		writeError(w, http.StatusConflict, "duplicate", err.Error())
	case errors.Is(err, llm.ErrConflict):
		writeError(w, http.StatusConflict, "version_conflict", err.Error())
	case errors.Is(err, llm.ErrBusy):
		writeError(w, http.StatusConflict, "busy", err.Error())
	case errors.Is(err, llm.ErrDisabled), errors.Is(err, llm.ErrNoModel):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, llm.ErrFull):
		writeError(w, http.StatusInsufficientStorage, "full", err.Error())
	default:
		var callErr *llm.CallError
		if errors.As(err, &callErr) {
			// 上游模型接口拒绝（密钥无效 / 限流 / 地址不对…）：不是本服务的内部错误。
			writeError(w, http.StatusBadGateway, "provider_error", err.Error())
			return
		}
		s.fail(w, r, err)
	}
}

func (s *Server) handleListProviders(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"providers": s.cfg.LLM.Providers().List()})
}

func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	var in llm.Input
	if !decodeJSON(w, r, &in) {
		return
	}
	view, err := s.cfg.LLM.Providers().Create(in, p.Username)
	if err != nil {
		s.record(r, p, "llm.provider.create", in.Name, "failed", err.Error())
		s.failLLM(w, r, err)
		return
	}
	s.record(r, p, "llm.provider.create", view.ID, "ok", view.Name+" · "+view.BaseURL)
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	var in llm.Input
	if !decodeJSON(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	view, err := s.cfg.LLM.Providers().Update(id, in)
	if err != nil {
		s.record(r, p, "llm.provider.update", id, "failed", err.Error())
		s.failLLM(w, r, err)
		return
	}
	detail := "配置变更"
	if strings.TrimSpace(in.APIKey) != "" {
		detail = "配置变更 + 更换密钥" // 只记「换了」，不记任何密钥内容
	}
	s.record(r, p, "llm.provider.update", id, "ok", detail)
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	id := r.PathValue("id")
	if err := s.cfg.LLM.Providers().Delete(id); err != nil {
		s.failLLM(w, r, err)
		return
	}
	s.record(r, p, "llm.provider.delete", id, "ok", "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	var body struct {
		Model string `json:"model"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	id := r.PathValue("id")
	res, view, err := s.cfg.LLM.Test(r.Context(), p.Username, id, strings.TrimSpace(body.Model))
	if err != nil && !res.At.IsZero() {
		err = nil // 调用失败已写进 res.Error，照常返回 200
	}
	if err != nil {
		s.failLLM(w, r, err)
		return
	}
	result := "ok"
	if !res.OK {
		result = "failed"
	}
	s.record(r, p, "llm.provider.test", id, result, res.Model+" · "+res.Error)
	writeJSON(w, http.StatusOK, map[string]any{"result": res, "provider": view})
}

// handleProbeModels：用表单里已填的地址与密钥（或既有提供方保存的密钥）调上游 /models，
// 返回可用模型清单。只读探测：不改存储、不进 token 账本（/models 不产生计费调用）。
func (s *Server) handleProbeModels(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	var body struct {
		ProviderID string `json:"provider_id"`
		BaseURL    string `json:"base_url"`
		APIKey     string `json:"api_key"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	models, err := s.cfg.LLM.ProbeModels(r.Context(), llm.ProbeInput{BaseURL: body.BaseURL, APIKey: body.APIKey, ProviderID: body.ProviderID})
	if err != nil {
		// 审计只记地址与失败（错误信息已 scrub，不含密钥）
		s.record(r, p, "llm.models.probe", body.BaseURL, "failed", err.Error())
		s.failLLM(w, r, err)
		return
	}
	s.record(r, p, "llm.models.probe", body.BaseURL, "ok", fmt.Sprintf("%d 个模型", len(models)))
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// handleUsage：管理员看全部并附按人明细；运维角色只看自己的用量。
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	admin := rbac.Can(p.Role, rbac.LLMAdmin)
	user := p.Username
	if admin {
		user = ""
	}
	sum := s.cfg.LLM.Usage().Summarize(s.now(), intParam(r, "days", 14, 1, 90), user, admin)
	writeJSON(w, http.StatusOK, map[string]any{"summary": sum, "scope": map[bool]string{true: "all", false: "self"}[admin],
		"in_flight": s.cfg.LLM.InFlight()})
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"conversations": s.cfg.LLM.Conversations().List(principalOf(r).Username)})
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	c, ok := s.cfg.LLM.Conversations().Get(principalOf(r).Username, r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "会话不存在")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.LLM.Conversations().Delete(principalOf(r).Username, r.PathValue("id")); err != nil {
		s.failLLM(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	var body struct {
		ProviderID  string   `json:"provider_id"`
		Model       string   `json:"model"`
		DecisionIDs []string `json:"decision_ids"`
		RedactIP    *bool    `json:"redact_ip"`
		Title       string   `json:"title"`
		Question    string   `json:"question"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	ids := dedupe(body.DecisionIDs)
	if len(ids) == 0 || len(ids) > maxAnalysisTraffic {
		writeError(w, http.StatusBadRequest, "invalid_field", fmt.Sprintf("decision_ids：请选择 1–%d 条流量", maxAnalysisTraffic))
		return
	}
	redact := body.RedactIP == nil || *body.RedactIP // 默认脱敏：来源 IP 发往第三方模型前抹掉末段
	rows, _, err := s.loadRows(r.Context(), llmTrafficSpan)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	picked := pickRows(rows, ids)
	if len(picked) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "所选流量已不在核心的近期缓冲中（只保留最近一段时间），请重新选择")
		return
	}
	c, err := s.cfg.LLM.Start(r.Context(), llm.StartInput{User: p.Username, ProviderID: body.ProviderID, Model: body.Model,
		Title: body.Title, Question: body.Question, DecisionIDs: idsOf(picked), RedactIP: redact,
		Context: trafficContext(picked, redact)})
	if err != nil {
		s.failLLM(w, r, err)
		return
	}
	s.record(r, p, "llm.conversation.create", c.ID, "ok", fmt.Sprintf("%d 条流量 · %s/%s", len(picked), c.ProviderName, c.Model))
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	c, err := s.cfg.LLM.Send(r.Context(), principalOf(r).Username, r.PathValue("id"), body.Content)
	if err != nil {
		s.failLLM(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// pickRows 按所选 decision_id 取行（保持用户选择的顺序；缓冲里已滚出的静默略过，由调用方判空）。
func pickRows(rows []TrafficRow, ids []string) []TrafficRow {
	byID := make(map[string]TrafficRow, len(rows))
	for _, row := range rows {
		if _, seen := byID[row.DecisionID]; !seen {
			byID[row.DecisionID] = row
		}
	}
	out := make([]TrafficRow, 0, len(ids))
	for _, id := range ids {
		if row, ok := byID[id]; ok {
			out = append(out, row)
		}
	}
	return out
}

func idsOf(rows []TrafficRow) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.DecisionID
	}
	return out
}

func dedupe(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// trafficContext 把选中的请求压成 JSON Lines（按时间正序，便于模型看出时序）。
// 只带分析需要的字段，长字段截断；超过总上限就停（并在末尾注明省略数）。
func trafficContext(rows []TrafficRow, redactIP bool) string {
	var b strings.Builder
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		ip := row.SourceIP
		if redactIP {
			ip = RedactIP(ip)
		}
		line := map[string]any{
			"at": row.At.UTC().Format(time.RFC3339), "method": row.Method, "host": row.Host,
			"path": clip(row.Path, 300), "user_agent": clip(row.UserAgent, 200), "source_ip": ip,
			"geo": row.Geo.Label, "action": row.Action, "executed": row.Executed, "layer": row.Layer,
			"status": row.Status, "signals": row.Signals, "deceived": row.InMirage, "shadow": row.Shadow,
		}
		if row.Score != nil {
			line["score"] = *row.Score
		}
		if row.ServiceName != "" {
			line["service"] = row.ServiceName
		}
		if row.DeliveryResult != "" {
			line["delivery_result"] = row.DeliveryResult
		}
		raw, err := json.Marshal(line)
		if err != nil {
			continue
		}
		if b.Len()+len(raw) > maxContextBytes {
			fmt.Fprintf(&b, "{\"note\":\"超出上下文上限，另有 %d 条较早的请求已省略\"}\n", i+1)
			break
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// RedactIP 抹掉来源地址的主机部分：IPv4 → a.b.c.x；IPv6 → 前 3 段 + ::x。无法解析的原样置空。
func RedactIP(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.x", v4[0], v4[1], v4[2])
	}
	parts := strings.Split(ip.String(), ":")
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return strings.Join(parts, ":") + "::x"
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
