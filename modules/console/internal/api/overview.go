package api

import (
	"net/http"
	"sort"
	"time"

	"shen/modules/console/internal/geoip"
)

// Stats 是一批请求行的聚合（总览与单服务详情共用同一口径）。
type Stats struct {
	Total        int            `json:"total"`
	ByLayer      map[string]int `json:"by_layer"`
	ByAction     map[string]int `json:"by_action"`
	InMirage     int            `json:"in_mirage"`
	ShadowMirage int            `json:"shadow_mirage"`
	MirageRatio  float64        `json:"mirage_ratio"` // (in_mirage + shadow_mirage) / total；total=0 时为 0
	Alerts       int            `json:"alerts"`
	UniqueIPs    int            `json:"unique_ips"`
	DecoyFailed  int            `json:"decoy_failed"` // 诱饵路由命中但未投递成功（固定 502）
}

// SourceStat 是一个来源的聚合。
type SourceStat struct {
	IP       string         `json:"ip"`
	Geo      geoip.Location `json:"geo"`
	Count    int            `json:"count"`
	InMirage int            `json:"in_mirage"`
	LastSeen time.Time      `json:"last_seen"`
}

// GeoStat 是按归属地的聚合。
type GeoStat struct {
	Label string      `json:"label"`
	Scope geoip.Scope `json:"scope"`
	Count int         `json:"count"`
}

// Bucket 是趋势图的一个时间桶。
type Bucket struct {
	At      time.Time      `json:"at"`
	ByLayer map[string]int `json:"by_layer"`
}

// Overview 是总览页的数据。
type Overview struct {
	Window   window       `json:"window"`
	Stats    Stats        `json:"stats"`
	Trend    []Bucket     `json:"trend"`
	Sources  []SourceStat `json:"top_sources"`
	Geo      []GeoStat    `json:"geo"`
	Recent   []TrafficRow `json:"recent"`
	Services int          `json:"services_registered"`
	GeoBuilt uint32       `json:"geo_db_built_at"`
	// ConfigSync 是欺骗管控的版本对账与蜜罐健康摘要（未启用数据集时为 null）。
	ConfigSync   *configSyncCard `json:"config_sync"`
	SystemAlerts int             `json:"system_alerts"`
}

// configSyncCard 是总览页「配置同步」卡片的数据。
type configSyncCard struct {
	State          string `json:"state"`
	DatasetVersion uint64 `json:"dataset_version"`
	DatasetRev     uint64 `json:"dataset_rev"`
	CoreRev        uint64 `json:"core_rev"`
	EdgeInSync     int    `json:"edge_in_sync"`
	EdgeTotal      int    `json:"edge_total"`
	HoneypotsTotal int    `json:"honeypots_total"`
	HoneypotsOK    int    `json:"honeypots_healthy"`
	Initialized    bool   `json:"initialized"`
}

func (s *Server) configSyncCard() *configSyncCard {
	st := s.configSyncSummary()
	if st == nil {
		return nil
	}
	ds := s.cfg.Deception.Get()
	c := &configSyncCard{State: st.State, DatasetVersion: ds.Version, DatasetRev: st.DatasetRev, CoreRev: st.CoreRev,
		EdgeInSync: st.EdgeInSync, EdgeTotal: st.EdgeTotal, Initialized: ds.Initialized}
	for _, h := range ds.Honeypots {
		if h.Enabled {
			c.HoneypotsTotal++
		}
	}
	for _, p := range st.Honeypots {
		if p.Healthy {
			c.HoneypotsOK++
		}
	}
	return c
}

const trendBuckets = 30

// aggregate 单次遍历完成全部统计（O(n)）。
func aggregate(rows []TrafficRow, win window, topN int) (Stats, []Bucket, []SourceStat, []GeoStat) {
	st := Stats{ByLayer: map[string]int{}, ByAction: map[string]int{}}
	width := time.Duration(win.Seconds) * time.Second / trendBuckets
	if width <= 0 {
		width = time.Second
	}
	trend := make([]Bucket, trendBuckets)
	for i := range trend {
		trend[i] = Bucket{At: win.Start.Add(time.Duration(i) * width), ByLayer: map[string]int{}}
	}
	sources := map[string]*SourceStat{}
	geos := map[string]*GeoStat{}
	for _, row := range rows {
		st.Total++
		st.ByLayer[row.Layer]++
		if row.Action != "" {
			st.ByAction[row.Action]++
		}
		if row.InMirage {
			st.InMirage++
		}
		if row.ShadowMirage {
			st.ShadowMirage++
		}
		if row.Alert {
			st.Alerts++
		}
		if row.Layer == layerDecoy && row.DeliveryResult != "" && row.DeliveryResult != "delivered" {
			st.DecoyFailed++
		}
		if idx := int(row.At.Sub(win.Start) / width); idx >= 0 && idx < trendBuckets {
			trend[idx].ByLayer[row.Layer]++
		}
		key := row.SourceIP
		if key == "" {
			key = "（未知来源）"
		}
		src, ok := sources[key]
		if !ok {
			src = &SourceStat{IP: row.SourceIP, Geo: row.Geo}
			sources[key] = src
		}
		src.Count++
		if row.InMirage || row.ShadowMirage {
			src.InMirage++
		}
		if row.At.After(src.LastSeen) {
			src.LastSeen = row.At
		}
		g, ok := geos[row.Geo.Label]
		if !ok {
			g = &GeoStat{Label: row.Geo.Label, Scope: row.Geo.Scope}
			geos[row.Geo.Label] = g
		}
		g.Count++
	}
	st.UniqueIPs = len(sources)
	if st.Total > 0 {
		st.MirageRatio = float64(st.InMirage+st.ShadowMirage) / float64(st.Total)
	}
	srcList := make([]SourceStat, 0, len(sources))
	for _, v := range sources {
		srcList = append(srcList, *v)
	}
	sort.Slice(srcList, func(i, j int) bool {
		if srcList[i].Count != srcList[j].Count {
			return srcList[i].Count > srcList[j].Count
		}
		return srcList[i].IP < srcList[j].IP
	})
	geoList := make([]GeoStat, 0, len(geos))
	for _, v := range geos {
		geoList = append(geoList, *v)
	}
	sort.Slice(geoList, func(i, j int) bool {
		if geoList[i].Count != geoList[j].Count {
			return geoList[i].Count > geoList[j].Count
		}
		return geoList[i].Label < geoList[j].Label
	})
	return st, trend, head(srcList, topN), head(geoList, topN)
}

func head[T any](in []T, n int) []T {
	if len(in) > n {
		return in[:n]
	}
	return in
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	rows, win, err := s.loadRows(r.Context(), parseWindow(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	st, trend, sources, geos := aggregate(rows, win, 10)
	writeJSON(w, http.StatusOK, Overview{
		Window: win, Stats: st, Trend: trend, Sources: sources, Geo: geos,
		Recent: head(rows, 50), Services: len(s.registry.List()), GeoBuilt: s.geo.BuiltAt(),
		ConfigSync: s.configSyncCard(), SystemAlerts: len(s.systemAlerts()),
	})
}

// handleTraffic 返回时间窗内的请求行（可按 host / layer / ip 过滤）。
func (s *Server) handleTraffic(w http.ResponseWriter, r *http.Request) {
	rows, win, err := s.loadRows(r.Context(), parseWindow(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	q := r.URL.Query()
	layer, ip := q.Get("layer"), q.Get("ip")
	limit := intParam(r, "limit", 200, 1, 1000)
	out := make([]TrafficRow, 0, min(limit, len(rows)))
	for _, row := range rows {
		if (layer != "" && row.Layer != layer) || (ip != "" && row.SourceIP != ip) {
			continue
		}
		out = append(out, row)
		if len(out) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"window": win, "rows": out})
}
