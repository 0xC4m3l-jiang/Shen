package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	telemetryv1 "shen/common/api/telemetry/v1"
	"shen/modules/console/internal/topology"
)

// handleFlow 返回核心判定流（意图：分值 / 信号 / 三值），附归属地。
func (s *Server) handleFlow(w http.ResponseWriter, r *http.Request) {
	events, err := s.listEvents(r.Context(), decisionEventType, intParam(r, "limit", 300, 1, 2000), time.Time{})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type flowView struct {
		flowRecord
		Geo any `json:"geo"`
	}
	out := make([]flowView, 0, len(events))
	for _, ev := range events {
		if ev.Flow != nil {
			out = append(out, flowView{flowRecord: *ev.Flow, Geo: s.geo.Lookup(ev.Flow.SourceIP)})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// observations 取构建链路 / 拓扑所需的三类事件（按类型分别取：避免 limit 被其他类型挤占）。
func (s *Server) observations(ctx context.Context, limit int) (topology.Input, error) {
	in := topology.Input{}
	judged, err := s.listEvents(ctx, judgedEventType, limit, time.Time{})
	if err != nil {
		return in, err
	}
	for _, ev := range judged {
		var j topology.JudgedEvent
		if json.Unmarshal(ev.Raw, &j) != nil {
			continue
		}
		if j.DecisionID == "" {
			j.DecisionID = ev.EventID
		}
		j.At = ev.CreatedAt
		in.Judged = append(in.Judged, j)
	}
	decisions, err := s.listEvents(ctx, decisionEventType, limit, time.Time{})
	if err != nil {
		return in, err
	}
	for _, ev := range decisions {
		var d topology.DecisionEvent
		if json.Unmarshal(ev.Raw, &d) == nil && d.DecisionID != "" {
			in.Decisions = append(in.Decisions, d)
		}
	}
	analysis, err := s.listEvents(ctx, analysisEventType, limit, time.Time{})
	if err != nil {
		return in, err
	}
	for _, ev := range analysis {
		var a topology.AnalysisEvent
		if json.Unmarshal(ev.Raw, &a) == nil {
			a.At = ev.CreatedAt
			in.Analysis = append(in.Analysis, a)
		}
	}
	return in, nil
}

// handleGraphs 返回逐请求链路；带 decision_id 时只返回那一条（判定流里点击看链路）。
//
// 过滤时取**全缓冲**（与 trace 同口径）：目标是较早的判定时，默认 limit 200 可能
// 已经取不到它的执行记录，从而把「在缓冲里」误报成「无链路」。
func (s *Server) handleGraphs(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("decision_id"))
	limit := intParam(r, "limit", 200, 1, 2000)
	if id != "" {
		limit = maxListLimit
	}
	in, err := s.observations(r.Context(), limit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	graphs := topology.BuildRequests(in, s.cfg.AlertScore)
	if id == "" {
		writeJSON(w, http.StatusOK, graphs)
		return
	}
	out := make([]topology.RequestGraph, 0, 1)
	for _, g := range graphs {
		if g.DecisionID == id {
			out = append(out, g)
			break
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	in, err := s.observations(r.Context(), intParam(r, "limit", 500, 1, 2000))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, topology.Build(in, s.cfg.AlertScore))
}

type traceView struct {
	DecisionID string            `json:"decision_id"`
	Judged     json.RawMessage   `json:"judged,omitempty"`
	Decision   json.RawMessage   `json:"decision,omitempty"`
	Analysis   []json.RawMessage `json:"analysis,omitempty"`
	Alerts     traceAlerts       `json:"alerts"`
	Notes      []string          `json:"notes"`
}

type traceAlerts struct {
	Real     bool   `json:"real"`
	HighRisk bool   `json:"high_risk"`
	Reason   string `json:"reason"`
}

// handleTrace 返回某条 decision_id 的完整链路。
//
// 修正旧实现的关联口径：request_judged 的事件 id 是 `judged:<decision_id>:<seq>`，
// 必须按**载荷里的** decision_id 关联（旧代码比较事件 id，永远匹配不上）。
func (s *Server) handleTrace(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("decision_id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "缺 decision_id 参数")
		return
	}
	out := traceView{DecisionID: id}
	out.Notes = append(out.Notes, fmt.Sprintf("高风险阈值 %.2f 仅用于显示，不改变处置", s.cfg.AlertScore))
	for _, t := range []string{judgedEventType, decisionEventType, analysisEventType} {
		events, err := s.listEvents(r.Context(), t, maxListLimit, time.Time{})
		if err != nil {
			s.fail(w, r, err)
			return
		}
		for _, ev := range events {
			switch t {
			case judgedEventType:
				if out.Judged == nil && ev.Judged != nil && ev.Judged.DecisionID == id {
					out.Judged = ev.Raw
				}
			case decisionEventType:
				if out.Decision == nil && ev.Flow != nil && ev.Flow.DecisionID == id {
					out.Decision = ev.Raw
					f := ev.Flow
					out.Alerts.HighRisk = f.Score >= s.cfg.AlertScore
					if normAction(f.Action) == "block" || (f.Severity != "" && f.Severity != "none") {
						out.Alerts.Real, out.Alerts.Reason = true, "拦截 或 severity≠none（现行告警口径）"
					}
				}
			case analysisEventType:
				var a topology.AnalysisEvent
				if json.Unmarshal(ev.Raw, &a) != nil {
					continue
				}
				for _, evidence := range a.EvidenceIDs {
					if evidence == id {
						out.Analysis = append(out.Analysis, ev.Raw)
						break
					}
				}
			}
		}
	}
	if out.Judged == nil {
		out.Notes = append(out.Notes, "没有适配器执行记录：可能已被核心缓冲挤出")
	}
	if out.Decision == nil {
		out.Notes = append(out.Notes, "没有核心判定记录：白名单 / 判定缓存命中 / 判定失败放行 / 专属诱饵路由（不调核心）")
	}
	writeJSON(w, http.StatusOK, out)
}

type configView struct {
	Policy map[string]any `json:"policy"`
	AI     map[string]any `json:"ai"`
}

// handleConfig 返回核心生效状态的只读快照；取不到时报错，**不伪造全零配置**（AR-15 的精神）。
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	snap, err := s.core.GetCoreSnapshot(ctx, &telemetryv1.GetCoreSnapshotRequest{})
	if err != nil {
		s.fail(w, r, &coreError{err: fmt.Errorf("读取核心快照失败：%w", err)})
		return
	}
	writeJSON(w, http.StatusOK, configView{
		Policy: map[string]any{
			"policy_id": snap.GetPolicyId(), "version": snap.GetPolicyVersion(), "checksum": snap.GetPolicyChecksum(),
			"rule_count": snap.GetRuleCount(), "whitelist_count": snap.GetWhitelistCount(),
		},
		AI: map[string]any{
			"enabled": snap.GetAiEnabled(), "kinds": snap.GetAiKinds(), "model": snap.GetAiModel(),
			"manifest_path": snap.GetAiManifestPath(), "variants": snap.GetAiContentVariants(),
			"rotate_cooldown": snap.GetAiRotateCooldown(), "manifest_loaded": snap.GetAiManifestLoaded(),
			"manifest_version": snap.GetAiManifestVersion(), "manifest_resources": snap.GetAiManifestResources(),
			"manifest_contents": snap.GetAiManifestContents(),
		},
	})
}
