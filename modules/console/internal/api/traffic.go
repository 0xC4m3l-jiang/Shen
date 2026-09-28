package api

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"shen/modules/console/internal/geoip"
)

// 落点分层（页面与统计共用）：把适配器的 executed 归成五类。
const (
	layerOrigin   = "origin"   // 真实业务（含白名单 / 缓存 / 判定失败放行）
	layerFallback = "fallback" // 判成改道但幻境不可用 ⇒ 回落业务
	layerMirage   = "mirage"   // 评分改道进入幻境
	layerDecoy    = "decoy"    // 专属诱饵路由
	layerBlock    = "block"    // 403
)

// TrafficRow 是一条请求的合并视图：适配器的实际落点 + 核心的判定意图 + 归属地 + 所属服务。
type TrafficRow struct {
	DecisionID     string         `json:"decision_id"`
	At             time.Time      `json:"at"`
	Host           string         `json:"host"`
	SourceIP       string         `json:"source_ip"`
	Geo            geoip.Location `json:"geo"`
	Method         string         `json:"method"`
	Path           string         `json:"path"`
	UserAgent      string         `json:"user_agent"`
	Action         string         `json:"action"`
	Executed       string         `json:"executed"`
	Dispatched     string         `json:"dispatched,omitempty"` // 缓存命中时的实际落点
	Layer          string         `json:"layer"`
	DeliveryResult string         `json:"delivery_result,omitempty"`
	Backend        string         `json:"backend,omitempty"`
	Status         int            `json:"status,omitempty"`
	DurationMs     float64        `json:"duration_ms,omitempty"`
	Score          *float64       `json:"score,omitempty"`
	Signals        []string       `json:"signals,omitempty"`
	Severity       string         `json:"severity,omitempty"`
	Shadow         bool           `json:"shadow"`
	// InMirage：请求**确实**进入了幻境或诱饵（诱饵须投递成功）。
	InMirage bool `json:"in_mirage"`
	// ShadowMirage：影子模式下「本会改道」—— 默认栈只观测，靠它看出「如果放开会进蜃楼」。
	ShadowMirage bool   `json:"shadow_mirage"`
	Alert        bool   `json:"alert"`
	ServiceID    string `json:"service_id,omitempty"`
	ServiceName  string `json:"service_name,omitempty"`
	Source       string `json:"source"` // adapter（有执行记录）/ core（只有判定记录）
}

func layerOf(executed, action string) string {
	switch executed {
	case "mirage":
		return layerMirage
	case "decoy":
		return layerDecoy
	case "block":
		return layerBlock
	case "origin_fallback":
		return layerFallback
	case "origin", "whitelist", "failopen":
		return layerOrigin
	case "":
		switch action {
		case "route_mirage":
			return layerMirage
		case "block":
			return layerBlock
		}
	}
	return layerOrigin
}

// landing 返回请求的**实际**落点：缓存命中时 executed 只说「未重新判定」，真正去向在 dispatched。
// 老版本适配器没有 dispatched ⇒ 返回空串，由 layerOf 按判定意图归层（不计入「已流入蜃楼」：没有证据）。
func landing(j *judgedRecord) string {
	if j.Executed != "cache" {
		return j.Executed
	}
	if j.Dispatched != "" {
		return j.Dispatched
	}
	return ""
}

// rowFromJudged 用适配器记录构造一行（判定意图稍后由 mergeDecision 补齐）。
func (s *Server) rowFromJudged(at time.Time, j *judgedRecord) TrafficRow {
	action := normAction(j.Action)
	executed := landing(j)
	row := TrafficRow{
		DecisionID: j.DecisionID, At: at, Host: j.Host, SourceIP: j.SourceIP,
		Method: j.Method, Path: j.Path, UserAgent: j.UA, Action: action,
		Executed: j.Executed, Dispatched: j.Dispatched, Layer: layerOf(executed, action), DeliveryResult: j.DeliveryResult,
		Backend: j.Backend, Status: j.Status, DurationMs: j.DurationMs, Shadow: j.Shadow, Source: "adapter",
	}
	row.InMirage = executed == "mirage" || (executed == "decoy" && j.DeliveryResult == "delivered")
	row.ShadowMirage = j.Shadow && action == "route_mirage"
	return row
}

func mergeDecision(row *TrafficRow, f *flowRecord) {
	score := f.Score
	row.Score, row.Signals, row.Severity = &score, f.Signals, f.Severity
	if row.SourceIP == "" {
		row.SourceIP = f.SourceIP
	}
	if row.UserAgent == "" {
		row.UserAgent = f.UserAgent
	}
	if a := normAction(f.Action); a != "" {
		row.Action = a
	}
	row.Alert = row.Action == "block" || (f.Severity != "" && !strings.EqualFold(f.Severity, "none"))
}

// enrich 补归属地与所属服务（查询都是纯内存，逐行调用即可）。
func (s *Server) enrich(row *TrafficRow) {
	row.Geo = s.geo.Lookup(row.SourceIP)
	if row.Host == "" {
		return
	}
	if id, ok := s.registry.Match(row.Host); ok {
		if svc, found := s.registry.Get(id); found {
			row.ServiceID, row.ServiceName = svc.ID, svc.Name
		}
	}
}

// window 描述一次聚合的时间范围与数据覆盖情况。
type window struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Seconds   int       `json:"seconds"`
	Truncated bool      `json:"truncated"`          // 核心缓冲不足以覆盖整个时间窗
	Coverage  time.Time `json:"coverage_start"`     // 实际拿到的最早数据时刻
	Note      string    `json:"note,omitempty"`     // 给页面的一句话说明
	Freshest  time.Time `json:"freshest,omitempty"` // 最新一条数据的时刻（观测新鲜度）
}

var windowChoices = map[string]time.Duration{
	"5m": 5 * time.Minute, "15m": 15 * time.Minute, "1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour,
}

func parseWindow(r *http.Request) time.Duration {
	if d, ok := windowChoices[r.URL.Query().Get("window")]; ok {
		return d
	}
	return 15 * time.Minute
}

// loadRows 取时间窗内的请求行（新的在前）。
//
// 口径：以适配器的 request_judged 为主（每个经过边缘的请求都有一条），按 decision_id 补核心判定；
// 只有核心判定、没有执行记录的（旁路镜像 / 执行记录被挤出缓冲）单独成行并标注 source=core。
func (s *Server) loadRows(ctx context.Context, span time.Duration) ([]TrafficRow, window, error) {
	now := s.now()
	win := window{Start: now.Add(-span), End: now, Seconds: int(span.Seconds())}
	judged, err := s.listEvents(ctx, judgedEventType, maxListLimit, win.Start)
	if err != nil {
		return nil, win, err
	}
	decisions, err := s.listEvents(ctx, decisionEventType, maxListLimit, win.Start)
	if err != nil {
		return nil, win, err
	}
	byID := make(map[string]*flowRecord, len(decisions))
	for _, ev := range decisions {
		if ev.Flow != nil && ev.Flow.DecisionID != "" {
			if _, seen := byID[ev.Flow.DecisionID]; !seen {
				byID[ev.Flow.DecisionID] = ev.Flow
			}
		}
	}
	rows := make([]TrafficRow, 0, len(judged)+len(decisions))
	used := make(map[string]bool, len(judged))
	for _, ev := range judged {
		if ev.Judged == nil {
			continue
		}
		row := s.rowFromJudged(ev.CreatedAt, ev.Judged)
		if f, ok := byID[row.DecisionID]; ok {
			mergeDecision(&row, f)
			used[row.DecisionID] = true
		}
		s.enrich(&row)
		rows = append(rows, row)
	}
	for id, f := range byID {
		if used[id] {
			continue
		}
		action := normAction(f.Action)
		row := TrafficRow{DecisionID: id, At: f.At, SourceIP: f.SourceIP, Method: f.Method, Path: f.Path,
			UserAgent: f.UserAgent, Action: action, Layer: layerOf("", action), Source: "core"}
		mergeDecision(&row, f)
		s.enrich(&row)
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].At.After(rows[j].At) })

	win.Coverage = win.Start
	if len(judged) >= maxListLimit || len(decisions) >= maxListLimit {
		win.Truncated = true
		win.Note = "核心内存缓冲只保留最近 4096 条事件：时间窗早段数据已被挤出，统计只覆盖 coverage_start 之后"
		if n := len(rows); n > 0 {
			win.Coverage = rows[n-1].At
		}
	}
	if len(rows) > 0 {
		win.Freshest = rows[0].At
	}
	return rows, win, nil
}
