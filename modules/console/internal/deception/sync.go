package deception

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// CoreReport 是核心每轮同步后上报的状态（与 cmd/core 的 consoleReport 手工对齐）。
type CoreReport struct {
	AppliedRev    uint64          `json:"applied_rev"`
	PolicyVersion uint64          `json:"policy_version"`
	BaseVersion   int64           `json:"base_version"`
	Applied       bool            `json:"applied"`
	Reason        string          `json:"reason,omitempty"`
	Source        string          `json:"source"` // console | cache | file
	EdgeAcks      []EdgeAck       `json:"edge_acks"`
	Honeypots     []HoneypotProbe `json:"honeypots"`
}

// EdgeAck 是一个边缘适配器最近一次回执。
type EdgeAck struct {
	AdapterID  string    `json:"adapter_id"`
	Version    uint64    `json:"version"`
	Applied    bool      `json:"applied"`
	Reason     string    `json:"reason,omitempty"`
	ReceivedAt time.Time `json:"received_at"`
}

// HoneypotProbe 是一次蜜罐健康探测结果。
type HoneypotProbe struct {
	Name      string    `json:"name"`
	Addr      string    `json:"addr"`
	Healthy   bool      `json:"healthy"`
	LatencyMS int64     `json:"latency_ms"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// SyncStatus 是同步链路的对外视图（「数据集 vN · 核心已应用 vM · 边缘已确认 vK」）。
type SyncStatus struct {
	Configured    bool            `json:"configured"`
	DatasetRev    uint64          `json:"dataset_rev"`
	CoreRev       uint64          `json:"core_rev"`
	PolicyVersion uint64          `json:"policy_version"`
	EdgeMinVer    uint64          `json:"edge_min_version"`
	EdgeInSync    int             `json:"edge_in_sync"`
	EdgeTotal     int             `json:"edge_total"`
	Applied       bool            `json:"applied"`
	Reason        string          `json:"reason,omitempty"`
	Source        string          `json:"source,omitempty"`
	LastPullAt    *time.Time      `json:"last_pull_at,omitempty"`
	LastReportAt  *time.Time      `json:"last_report_at,omitempty"`
	EdgeAcks      []EdgeAck       `json:"edge_acks"`
	Honeypots     []HoneypotProbe `json:"honeypots"`
	State         string          `json:"state"` // synced | pending | error | offline | unconfigured
}

// SystemAlert 是一条系统告警（与流量告警分区展示）。
type SystemAlert struct {
	ID       string    `json:"id"`
	Severity string    `json:"severity"` // critical | warning
	Title    string    `json:"title"`
	Detail   string    `json:"detail"`
	Since    time.Time `json:"since"`
}

// 判据阈值（保守取值：核心默认每 15s 拉一次、每轮都上报）。
const (
	reportStaleAfter = 90 * time.Second
	lagGrace         = 60 * time.Second
)

// SyncState 保存核心最近一次上报（并发安全：写少读多）。
type SyncState struct {
	mu           sync.RWMutex
	configured   bool
	last         *CoreReport
	lastReportAt time.Time
	lastPullAt   time.Time
	rev          uint64
	revAt        time.Time
	prevHealth   map[string]bool
	now          func() time.Time
}

// NewSyncState 构造状态表；configured=false 表示未配置核心同步令牌（集成端点 503）。
func NewSyncState(configured bool, now func() time.Time) *SyncState {
	if now == nil {
		now = time.Now
	}
	return &SyncState{configured: configured, now: now, prevHealth: map[string]bool{}}
}

// NoteRev 记下当前投影修订号首次出现的时刻（用于判断核心「落后多久」）。
func (s *SyncState) NoteRev(rev uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rev != s.rev {
		s.rev, s.revAt = rev, s.now()
	}
}

// RecordPull 记下核心的一次拉取。
func (s *SyncState) RecordPull() {
	s.mu.Lock()
	s.lastPullAt = s.now()
	s.mu.Unlock()
}

// ReportDelta 是一次上报相对上一次的**变化**（审计只记变化，防刷屏）。
type ReportDelta struct {
	Health         map[string]bool // 健康状态翻转的蜜罐 → 新状态
	AppliedChanged bool            // 应用结论翻转（接管 ↔ 被拒）
	First          bool            // 首次上报（核心接入）
}

// RecordReport 保存核心上报并返回相对上一次的变化。
func (s *SyncState) RecordReport(r CoreReport) ReportDelta {
	s.mu.Lock()
	defer s.mu.Unlock()
	delta := ReportDelta{First: s.last == nil}
	if s.last != nil && s.last.Applied != r.Applied {
		delta.AppliedChanged = true
	}
	cp := r
	cp.EdgeAcks = append([]EdgeAck(nil), r.EdgeAcks...)
	cp.Honeypots = append([]HoneypotProbe(nil), r.Honeypots...)
	s.last, s.lastReportAt = &cp, s.now()
	changed := map[string]bool{}
	delta.Health = changed
	for _, h := range r.Honeypots {
		if prev, ok := s.prevHealth[h.Name]; !ok || prev != h.Healthy {
			if ok {
				changed[h.Name] = h.Healthy
			}
			s.prevHealth[h.Name] = h.Healthy
		}
	}
	return delta
}

// Status 计算同步链路视图。datasetRev 是当前投影修订号。
func (s *SyncState) Status(datasetRev uint64) SyncStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := SyncStatus{Configured: s.configured, DatasetRev: datasetRev, EdgeAcks: []EdgeAck{}, Honeypots: []HoneypotProbe{}}
	if !s.lastPullAt.IsZero() {
		t := s.lastPullAt
		st.LastPullAt = &t
	}
	switch {
	case !s.configured:
		st.State = "unconfigured"
		return st
	case s.last == nil:
		st.State = "offline"
		return st
	}
	t := s.lastReportAt
	st.LastReportAt = &t
	r := s.last
	st.CoreRev, st.PolicyVersion, st.Applied, st.Reason, st.Source = r.AppliedRev, r.PolicyVersion, r.Applied, r.Reason, r.Source
	st.EdgeAcks = latestAcks(r.EdgeAcks)
	st.Honeypots = append(st.Honeypots, r.Honeypots...)
	st.EdgeTotal = len(st.EdgeAcks)
	for i, a := range st.EdgeAcks {
		if i == 0 || a.Version < st.EdgeMinVer {
			st.EdgeMinVer = a.Version
		}
		if a.Version >= r.PolicyVersion && a.Applied {
			st.EdgeInSync++
		}
	}
	switch {
	case s.now().Sub(s.lastReportAt) > reportStaleAfter:
		st.State = "offline"
	case !r.Applied:
		st.State = "error"
	case r.AppliedRev < datasetRev || st.EdgeInSync < st.EdgeTotal:
		st.State = "pending"
	default:
		st.State = "synced"
	}
	return st
}

// latestAcks 每个适配器只保留版本最高的一条回执（按适配器名排序，输出稳定）。
func latestAcks(in []EdgeAck) []EdgeAck {
	best := map[string]EdgeAck{}
	for _, a := range in {
		if cur, ok := best[a.AdapterID]; !ok || a.Version > cur.Version ||
			(a.Version == cur.Version && a.ReceivedAt.After(cur.ReceivedAt)) {
			best[a.AdapterID] = a
		}
	}
	out := make([]EdgeAck, 0, len(best))
	for _, a := range best {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AdapterID < out[j].AdapterID })
	return out
}

// Alerts 计算系统告警：核心不可达 / 合并被拒 / 核心落后 / 边缘落后或失败 / 被引用蜜罐不健康 / 未初始化 / 基线漂移。
func (s *SyncState) Alerts(ds Dataset, seed SeedOutcome) []SystemAlert {
	st := s.Status(ds.ProjectionRev)
	s.mu.RLock()
	now, revAt, lastAt := s.now(), s.revAt, s.lastReportAt
	s.mu.RUnlock()
	out := []SystemAlert{}
	add := func(id, sev, title, detail string, since time.Time) {
		out = append(out, SystemAlert{ID: id, Severity: sev, Title: title, Detail: detail, Since: since})
	}
	if !ds.Initialized {
		add("dataset_not_initialized", "warning", "欺骗管控数据集未初始化",
			"未找到部署配置（SHEN_CONSOLE_SEED_CONFIG）可供初始化：核心仍按自己的部署配置运行；在「欺骗管控」首次保存即视为接管。", ds.UpdatedAt)
	}
	if seed.Drifted {
		add("seed_drift", "warning", "部署配置中的欺骗域已变更但不再生效",
			"数据集已由管控台接管，部署配置里的 honeypots/decoys/whitelist/blacklist/injects 修改不会生效 —— 请在管控台修改。", ds.UpdatedAt)
	}
	switch st.State {
	case "unconfigured":
		add("core_sync_unconfigured", "warning", "核心同步未启用",
			"未设置 SHEN_CONSOLE_CORE_SYNC_TOKEN：界面上的修改不会下发到核心。", time.Time{})
		return out
	case "offline":
		detail := "核心尚未连接管控台同步通道（检查核心的 SHEN_CORE_CONSOLE_URL / SHEN_CORE_CONSOLE_TOKEN）。"
		if !lastAt.IsZero() {
			detail = fmt.Sprintf("核心已 %s 未上报同步状态：核心继续按最后一份正确配置运行，但新修改不会生效。", now.Sub(lastAt).Round(time.Second))
		}
		add("core_unreachable", "critical", "核心同步中断", detail, lastAt)
		return out
	}
	if !st.Applied {
		add("merge_rejected", "critical", "核心拒绝了最新的欺骗配置", "核心终检未通过，已保留上一份正确配置："+st.Reason, lastAt)
	} else if st.CoreRev < ds.ProjectionRev && !revAt.IsZero() && now.Sub(revAt) > lagGrace {
		add("core_behind", "warning", "核心尚未应用最新配置",
			fmt.Sprintf("数据集 r%d，核心已应用 r%d（超过 %s）。", ds.ProjectionRev, st.CoreRev, lagGrace), revAt)
	}
	for _, a := range st.EdgeAcks {
		switch {
		case a.Version >= st.PolicyVersion && !a.Applied:
			add("edge_failed:"+a.AdapterID, "critical", "边缘适配器应用策略失败",
				fmt.Sprintf("%s 回执 v%d applied=false：%s", a.AdapterID, a.Version, a.Reason), a.ReceivedAt)
		case a.Version < st.PolicyVersion && now.Sub(a.ReceivedAt) > lagGrace:
			add("edge_lagging:"+a.AdapterID, "warning", "边缘适配器版本落后",
				fmt.Sprintf("%s 已确认 v%d，当前策略 v%d。", a.AdapterID, a.Version, st.PolicyVersion), a.ReceivedAt)
		}
	}
	referenced := map[string]int{}
	for _, d := range ds.Decoys {
		if d.Enabled {
			referenced[d.Backend]++
		}
	}
	for _, h := range st.Honeypots {
		if h.Healthy {
			continue
		}
		if n := referenced[h.Name]; n > 0 {
			add("honeypot_unhealthy:"+h.Name, "critical", "被引用的蜜罐不可用",
				fmt.Sprintf("%s（%s）被 %d 个启用诱饵引用但探测失败：%s —— 命中这些诱饵的请求会回落真实业务。", h.Name, h.Addr, n, h.Error), h.CheckedAt)
		} else {
			add("honeypot_unhealthy:"+h.Name, "warning", "蜜罐不可用", fmt.Sprintf("%s（%s）探测失败：%s", h.Name, h.Addr, h.Error), h.CheckedAt)
		}
	}
	return out
}
