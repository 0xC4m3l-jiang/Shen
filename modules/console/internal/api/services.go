package api

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"shen/modules/console/internal/registry"
)

// serviceSummary 是服务卡片：登记信息 + 时间窗内的流量摘要。
type serviceSummary struct {
	registry.Service
	Stats    Stats     `json:"stats"`
	LastSeen time.Time `json:"last_seen,omitempty"`
	Spark    []int     `json:"spark"` // 迷你趋势（总流量，按 trendBuckets 分桶）
}

func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	services := s.registry.List()
	rows, win, err := s.loadRows(r.Context(), parseWindow(r))
	if err != nil {
		// 核心不可用时登记表仍然要能看、能改：返回登记 + 明确的错误说明，而不是整页失败。
		out := make([]serviceSummary, 0, len(services))
		for _, svc := range services {
			out = append(out, serviceSummary{Service: svc, Stats: Stats{ByLayer: map[string]int{}, ByAction: map[string]int{}}})
		}
		writeJSON(w, http.StatusOK, map[string]any{"services": out, "window": win, "traffic_error": err.Error()})
		return
	}
	grouped := map[string][]TrafficRow{}
	for _, row := range rows {
		if row.ServiceID != "" {
			grouped[row.ServiceID] = append(grouped[row.ServiceID], row)
		}
	}
	out := make([]serviceSummary, 0, len(services))
	for _, svc := range services {
		mine := grouped[svc.ID]
		st, trend, _, _ := aggregate(mine, win, 0)
		sum := serviceSummary{Service: svc, Stats: st, Spark: make([]int, len(trend))}
		for i, b := range trend {
			for _, n := range b.ByLayer {
				sum.Spark[i] += n
			}
		}
		if len(mine) > 0 {
			sum.LastSeen = mine[0].At
		}
		out = append(out, sum)
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": out, "window": win})
}

func (s *Server) handleGetService(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.registry.Get(r.PathValue("id"))
	if !ok {
		s.fail(w, r, registry.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, svc)
}

func (s *Server) handleCreateService(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	var body registry.Input
	if !decodeJSON(w, r, &body) {
		return
	}
	svc, err := s.registry.Create(body, p.Username)
	if err != nil {
		s.record(r, p, "registry.create", body.Name, "failed", err.Error())
		s.fail(w, r, err)
		return
	}
	s.record(r, p, "registry.create", svc.ID, "ok", svc.Name)
	writeJSON(w, http.StatusCreated, svc)
}

func (s *Server) handleUpdateService(w http.ResponseWriter, r *http.Request) {
	p, id := principalOf(r), r.PathValue("id")
	var body struct {
		registry.Input
		Version uint64 `json:"version"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	svc, err := s.registry.Update(id, body.Version, body.Input, p.Username)
	if err != nil {
		s.record(r, p, "registry.update", id, "failed", err.Error())
		s.fail(w, r, err)
		return
	}
	s.record(r, p, "registry.update", id, "ok", "version="+strconv.FormatUint(svc.Version, 10))
	writeJSON(w, http.StatusOK, svc)
}

func (s *Server) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	p, id := principalOf(r), r.PathValue("id")
	version, err := strconv.ParseUint(r.URL.Query().Get("version"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "删除必须携带 version 参数（防止误删他人刚改过的记录）")
		return
	}
	if err := s.registry.Delete(id, version); err != nil {
		s.record(r, p, "registry.delete", id, "failed", err.Error())
		s.fail(w, r, err)
		return
	}
	s.record(r, p, "registry.delete", id, "ok", "")
	w.WriteHeader(http.StatusNoContent)
}

// serviceTraffic 是单服务详情：是否流入蜃楼、流量明细与来源。
type serviceTraffic struct {
	Service registry.Service `json:"service"`
	Window  window           `json:"window"`
	Stats   Stats            `json:"stats"`
	Funnel  []funnelStep     `json:"funnel"`
	Trend   []Bucket         `json:"trend"`
	Sources []SourceStat     `json:"top_sources"`
	Geo     []GeoStat        `json:"geo"`
	Rows    []TrafficRow     `json:"rows"`
}

type funnelStep struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

func (s *Server) handleServiceTraffic(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.registry.Get(r.PathValue("id"))
	if !ok {
		s.fail(w, r, registry.ErrNotFound)
		return
	}
	rows, win, err := s.loadRows(r.Context(), parseWindow(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	mine := make([]TrafficRow, 0, len(rows))
	judgedHigh := 0
	for _, row := range rows {
		if row.ServiceID != svc.ID {
			continue
		}
		mine = append(mine, row)
		if row.Action == "route_mirage" {
			judgedHigh++
		}
	}
	st, trend, sources, geos := aggregate(mine, win, 10)
	funnel := []funnelStep{
		{"requests", "进入引擎的请求", st.Total},
		{"judged_mirage", "判定为改道（含影子）", judgedHigh},
		{"decoy_hit", "命中专属诱饵路由", st.ByLayer[layerDecoy]},
		{"in_mirage", "实际进入蜃楼", st.InMirage},
	}
	writeJSON(w, http.StatusOK, serviceTraffic{Service: svc, Window: win, Stats: st, Funnel: funnel,
		Trend: trend, Sources: sources, Geo: geos, Rows: head(mine, intParam(r, "limit", 200, 1, 1000))})
}

// handleUnregisteredHosts 列出流量里出现过、但未归到任何登记服务的域名（方便补登记）。
func (s *Server) handleUnregisteredHosts(w http.ResponseWriter, r *http.Request) {
	rows, win, err := s.loadRows(r.Context(), parseWindow(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type hostStat struct {
		Host     string    `json:"host"`
		Count    int       `json:"count"`
		LastSeen time.Time `json:"last_seen"`
	}
	counts := map[string]*hostStat{}
	for _, row := range rows {
		if row.ServiceID != "" || row.Host == "" {
			continue
		}
		h, ok := counts[row.Host]
		if !ok {
			h = &hostStat{Host: row.Host}
			counts[row.Host] = h
		}
		h.Count++
		if row.At.After(h.LastSeen) {
			h.LastSeen = row.At
		}
	}
	out := make([]hostStat, 0, len(counts))
	for _, h := range counts {
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	writeJSON(w, http.StatusOK, map[string]any{"window": win, "hosts": head(out, 50)})
}
