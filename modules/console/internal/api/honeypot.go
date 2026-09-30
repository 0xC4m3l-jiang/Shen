package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"shen/modules/console/internal/deception"
)

// 投递结果的中文说明（与适配器 delivery_result 取值一一对应）。
var deliveryLabels = map[string]string{
	"delivered":           "已投递到诱饵后端",
	"backend_unavailable": "后端未登记 / 未启用 / 已撤销 ⇒ 固定 502（不回源）",
	"delivery_failed":     "后端中途失败 ⇒ 不回生产",
	"tombstoned":          "路由已撤销，租约内固定 502",
}

type deliveriesView struct {
	Window       window            `json:"window"`
	Total        int               `json:"total"`
	ByLayer      map[string]int    `json:"by_layer"`
	ByResult     map[string]int    `json:"by_result"`
	Labels       map[string]string `json:"labels"`
	Failures     []TrafficRow      `json:"failures"`
	Rows         []TrafficRow      `json:"rows"`
	Interactions map[string]any    `json:"interaction_events"`
}

// handleDeliveries 汇总进入（或尝试进入）幻境 / 诱饵的请求：落点、投递结果与失败原因。
func (s *Server) handleDeliveries(w http.ResponseWriter, r *http.Request) {
	rows, win, err := s.loadRows(r.Context(), parseWindow(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v := deliveriesView{Window: win, ByLayer: map[string]int{}, ByResult: map[string]int{}, Labels: deliveryLabels,
		Interactions: map[string]any{"connected": false,
			"note": "合成管理台的登录 / 浏览 / 受限写事件目前只进蜜罐后端本地日志，尚未回流核心（根 README §8 成熟度「明确还没做」）"}}
	limit := intParam(r, "limit", 200, 1, 1000)
	for _, row := range rows {
		if row.Layer != layerMirage && row.Layer != layerDecoy && row.Layer != layerFallback {
			continue
		}
		v.Total++
		v.ByLayer[row.Layer]++
		if row.DeliveryResult != "" {
			v.ByResult[row.DeliveryResult]++
		}
		failed := row.Layer == layerFallback || (row.Layer == layerDecoy && row.DeliveryResult != "delivered")
		if failed && len(v.Failures) < 100 {
			v.Failures = append(v.Failures, row)
		}
		if len(v.Rows) < limit {
			v.Rows = append(v.Rows, row)
		}
	}
	if v.Failures == nil {
		v.Failures = []TrafficRow{}
	}
	if v.Rows == nil {
		v.Rows = []TrafficRow{}
	}
	writeJSON(w, http.StatusOK, v)
}

type backendView struct {
	Name     string `json:"name"`
	Layer    string `json:"layer"`
	Requests int    `json:"requests"`
	OK       int    `json:"ok"`
	Failed   int    `json:"failed"`
	// Unconfirmed：有改道意图但没有落点证据的请求数（见 landing）。
	Unconfirmed int       `json:"unconfirmed"`
	LastSeen    time.Time `json:"last_seen"`
}

// handleBackends 按「观测到的」后端聚合（管控台读不到核心后端表：这里只反映实际流量走过的后端）。
func (s *Server) handleBackends(w http.ResponseWriter, r *http.Request) {
	rows, win, err := s.loadRows(r.Context(), parseWindow(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	byName := map[string]*backendView{}
	for _, row := range rows {
		if row.Layer != layerMirage && row.Layer != layerDecoy && row.Layer != layerFallback {
			continue
		}
		name := row.Backend
		if name == "" {
			name = "（未记录后端名）"
		}
		b, ok := byName[name+"|"+row.Layer]
		if !ok {
			b = &backendView{Name: name, Layer: row.Layer}
			byName[name+"|"+row.Layer] = b
		}
		b.Requests++
		switch {
		case row.InMirage:
			b.OK++
		case row.Layer == layerFallback, row.Layer == layerDecoy && row.DeliveryResult != "delivered":
			b.Failed++
		default:
			// 落点未被证实（老版本适配器的缓存命中没有 dispatched）：既不算成功也不算失败。
			b.Unconfirmed++
		}
		if row.At.After(b.LastSeen) {
			b.LastSeen = row.At
		}
	}
	out := make([]backendView, 0, len(byName))
	for _, b := range byName {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Requests > out[j].Requests })
	writeJSON(w, http.StatusOK, map[string]any{"window": win, "backends": out,
		"registered": s.registeredBackends(out),
		"note":       "流量统计由时间窗内的实际流量推导；已登记但空闲的蜜罐与健康状态见「蜜罐池 · 登记与健康」"})
}

// registeredView 是蜜罐池里一个已登记后端的运维视图（登记 + 健康 + 引用 + 流量）。
type registeredView struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Addr       string `json:"addr"`
	Enabled    bool   `json:"enabled"`
	Health     string `json:"health"` // healthy | unhealthy | unknown | disabled
	LatencyMS  int64  `json:"latency_ms"`
	Error      string `json:"error,omitempty"`
	References int    `json:"references"` // 引用它的启用诱饵数
	Requests   int    `json:"requests"`
	Idle       bool   `json:"idle"` // 时间窗内无流量
}

// registeredBackends 合并蜜罐池登记、核心上报的健康探测与流量统计（未启用数据集时返回空）。
func (s *Server) registeredBackends(traffic []backendView) []registeredView {
	out := []registeredView{}
	if s.cfg.Deception == nil || s.cfg.Sync == nil {
		return out
	}
	ds := s.cfg.Deception.Get()
	st := s.cfg.Sync.Status(ds.ProjectionRev)
	probes := map[string]deception.HoneypotProbe{}
	for _, p := range st.Honeypots {
		probes[p.Name] = p
	}
	refs := map[string]int{}
	for _, d := range ds.Decoys {
		if d.Enabled {
			refs[d.Backend]++
		}
	}
	reqs := map[string]int{}
	for _, t := range traffic {
		reqs[t.Name] += t.Requests
	}
	for _, h := range ds.Honeypots {
		v := registeredView{Name: h.Name, Type: h.Type, Addr: h.Addr, Enabled: h.Enabled,
			References: refs[h.Name], Requests: reqs[h.Name], Health: "unknown"}
		v.Idle = v.Requests == 0
		switch p, ok := probes[h.Name]; {
		case !h.Enabled:
			v.Health = "disabled"
		case ok && p.Healthy:
			v.Health, v.LatencyMS = "healthy", p.LatencyMS
		case ok:
			v.Health, v.Error = "unhealthy", p.Error
		}
		out = append(out, v)
	}
	return out
}

type analysisView struct {
	EventID     string          `json:"event_id"`
	At          time.Time       `json:"at"`
	Kind        string          `json:"kind"`
	Accepted    bool            `json:"accepted"`
	Data        json.RawMessage `json:"data,omitempty"`
	Reason      string          `json:"rejected_reason,omitempty"`
	Analyzed    int             `json:"analyzed"`
	EvidenceIDs []string        `json:"evidence_ids,omitempty"`
}

// handleAnalysis 返回 L4 结论（只读展示：结论是数据，不改策略、不干预请求）。
func (s *Server) handleAnalysis(w http.ResponseWriter, r *http.Request) {
	events, err := s.listEvents(r.Context(), analysisEventType, intParam(r, "limit", 100, 1, 2000), time.Time{})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]analysisView, 0, len(events))
	for _, ev := range events {
		var p struct {
			Kind        string          `json:"kind"`
			Accepted    bool            `json:"accepted"`
			Data        json.RawMessage `json:"data"`
			Reason      string          `json:"rejected_reason"`
			Analyzed    int             `json:"analyzed"`
			EvidenceIDs []string        `json:"evidence_ids"`
		}
		if json.Unmarshal(ev.Raw, &p) != nil {
			continue
		}
		out = append(out, analysisView{EventID: ev.EventID, At: ev.CreatedAt, Kind: p.Kind, Accepted: p.Accepted,
			Data: p.Data, Reason: p.Reason, Analyzed: p.Analyzed, EvidenceIDs: p.EvidenceIDs})
	}
	writeJSON(w, http.StatusOK, out)
}

type alertItem struct {
	TrafficRow
	Reasons []string `json:"reasons"`
	Level   string   `json:"level"` // critical / warning / info
}

// handleAlerts 列出需要关注的请求：真实告警（拦截 / severity≠none）、诱饵投递失败、高风险分值。
func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	rows, win, err := s.loadRows(r.Context(), parseWindow(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]alertItem, 0)
	counts := map[string]int{}
	for _, row := range rows {
		item := alertItem{TrafficRow: row}
		if row.Alert {
			item.Reasons, item.Level = append(item.Reasons, "拦截或 severity≠none（现行告警口径）"), "critical"
		}
		if row.Layer == layerDecoy && row.DeliveryResult != "" && row.DeliveryResult != "delivered" {
			item.Reasons = append(item.Reasons, "诱饵投递失败："+deliveryLabels[row.DeliveryResult])
			if item.Level == "" {
				item.Level = "warning"
			}
		}
		if row.Score != nil && *row.Score >= s.cfg.AlertScore {
			item.Reasons = append(item.Reasons, "高风险分值（仅显示，不改变处置）")
			if item.Level == "" {
				item.Level = "info"
			}
		}
		if len(item.Reasons) == 0 {
			continue
		}
		counts[item.Level]++
		if len(out) < 500 {
			out = append(out, item)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"window": win, "alerts": out, "counts": counts,
		"alert_score": s.cfg.AlertScore, "system_alerts": s.systemAlerts()})
}
