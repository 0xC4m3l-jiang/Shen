package proxy

// SHEN-M1-02：把「系统是否在工作、会不会露馅」变成**可观测的数字**。
//
// 三项在边缘侧就能算清（不需要核心参与、不需要改 proto）：
//
//	reroute_ratio          —— 本窗口内改道占比（系统是否在工作）
//	failopen_ratio         —— 本窗口内因判定失败而放行的占比（引擎是不是已经失效）
//	session_flip_count     —— 同一会话去向改变的次数（**会不会露馅**的直接度量）
//
// 第四项 `breaker_open_duration` 由核心侧持有（熔断状态不在边缘），不在这里算（见变更包「遗留」）。
//
// 为什么放在适配器：这三项都是「执行面看到的事实」；核心只回答「要不要改道」，
// 它看不到「改道失败后退回了哪」。而且边缘本来就持有一份有界缓存（判定缓存、变体钉定），
// 再加一张**有界**的会话去向表不改变架构性质。

import (
	"sort"
	"sync"
	"time"
)

const (
	// metricsWindow 是滚动窗口：太短会被单条请求带偏，太长则故障反映不出来。
	metricsWindow = 60 * time.Second
	// sessionRouteTTL 是「会话最近去向」的记忆时长（与判定缓存的量级一致）。
	sessionRouteTTL = 5 * time.Minute
	// maxTrackedSessions 是会话表的硬上限：超了整体清空（可丢失的观测缓存，与判定缓存同一策略）。
	maxTrackedSessions = 20000
	// failOpenAlertRatio 是「引擎可能已经失效」的告警阈值。
	failOpenAlertRatio = 0.5
	// rerouteAlertBaseline 是「改道率异常偏低」的基线（低于它说明系统几乎没在工作）。
	rerouteAlertBaseline = 0.05
	// minSampleForRatio 是算比例的最小样本数：样本太少时比例没有意义，不告警。
	minSampleForRatio = 20
	// metricsInterval 是快照推进事件流的间隔（告警延迟上限）。
	metricsInterval = 15 * time.Second
)

// svcMetrics 是一个服务（Host）维度的计数。
type svcMetrics struct {
	Total    int
	Mirage   int
	Failopen int
	Fallback int
	Refused  int
	Flips    int
}

// metricsSnapshot 是某一时刻的只读快照（会被序列化进事件载荷）。
type metricsSnapshot struct {
	WindowSec     float64 `json:"window_sec"`
	Total         int     `json:"total"`
	Mirage        int     `json:"mirage"`
	Failopen      int     `json:"failopen"`
	Fallback      int     `json:"fallback"`
	Refused       int     `json:"refused"`
	RerouteRatio  float64 `json:"reroute_ratio"`
	FailopenRatio float64 `json:"failopen_ratio"`
	// SessionFlipCount 是**进程内累计**的会话翻转次数（单调递增，适合画趋势）。
	SessionFlipCount int `json:"session_flip_count"`
	// SessionFlipNew 是**上次快照以来**新增的翻转数 —— 告警只认它，否则一次翻转会永久告警。
	SessionFlipNew int                    `json:"session_flip_new"`
	ByService      map[string]svcMetrics  `json:"by_service,omitempty"`
	Alerts         []string               `json:"alerts,omitempty"`
	Sampled        bool                   `json:"sampled"`
	Extra          map[string]interface{} `json:"-"`
}

// sessionRoute 是「这个会话上一次去了哪」。
type sessionRoute struct {
	executed string
	at       time.Time
}

// routeMetrics 是按窗口滚动的路由指标（有界内存、可丢失）。
type routeMetrics struct {
	mu        sync.Mutex
	startedAt time.Time
	now       func() time.Time
	byService map[string]*svcMetrics
	sessions  map[string]sessionRoute
	// flipsTotal 累计（跨窗口，单调）；flipsNew 是「上次快照以来」的增量（告警只认它）。
	flipsTotal int
	flipsNew   int
}

func newRouteMetrics(now func() time.Time) *routeMetrics {
	if now == nil {
		now = time.Now
	}
	return &routeMetrics{
		startedAt: now(),
		now:       now,
		byService: map[string]*svcMetrics{},
		sessions:  map[string]sessionRoute{},
	}
}

// resetLocked 滚动窗口：到点就把计数清零（会话表不清 —— 翻转是跨窗口的事实）。
func (m *routeMetrics) resetLocked(now time.Time) {
	if now.Sub(m.startedAt) < metricsWindow {
		return
	}
	m.startedAt = now
	m.byService = map[string]*svcMetrics{}
	// 翻转是**跨窗口的事实**（对手可能在上一个窗口看到了另一种环境），不随窗口清零。
}

// record 记一次请求的执行结果。`executed` 取 `executed*` 常量；`service` 空 = 未归属。
//
// 翻转判定：同一会话上一次去向与本次不同 ⇒ `Flips++`。**只比「去向」**，不比状态码：
// 对手看到的是「进了哪儿」，不是「回了什么状态」。
func (m *routeMetrics) record(service, session, executed string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.resetLocked(now)

	svc, ok := m.byService[service]
	if !ok {
		svc = &svcMetrics{}
		m.byService[service] = svc
	}
	svc.Total++
	// 只看「实际落点」：判定意图（action）不代表对手看到了什么，executed 才代表。
	switch executed {
	case executedMirage:
		svc.Mirage++
	case executedFailOpen:
		svc.Failopen++
	case executedFallback:
		svc.Fallback++
	case executedRefused:
		svc.Refused++
	}

	if session == "" {
		return // 没有会话身份就无法谈「同一会话的去向」，不制造假翻转
	}
	if len(m.sessions) >= maxTrackedSessions {
		m.sessions = map[string]sessionRoute{}
	}
	if prev, seen := m.sessions[session]; seen && prev.executed != executed && now.Sub(prev.at) <= sessionRouteTTL {
		svc.Flips++
		m.flipsTotal++
		m.flipsNew++
	}
	m.sessions[session] = sessionRoute{executed: executed, at: now}
}

// snapshot 返回当前窗口的只读快照（含告警判定）。
func (m *routeMetrics) snapshot() metricsSnapshot {
	if m == nil {
		return metricsSnapshot{}
	}
	m.mu.Lock()
	now := m.now()
	m.resetLocked(now)
	total, mirage, failopen, fallback, refused := 0, 0, 0, 0, 0
	flipsTotal, flipsNew := m.flipsTotal, m.flipsNew
	byService := make(map[string]svcMetrics, len(m.byService))
	for name, c := range m.byService {
		byService[name] = *c
		total += c.Total
		mirage += c.Mirage
		failopen += c.Failopen
		fallback += c.Fallback
		refused += c.Refused
		_ = c.Flips // 按服务的翻转在 ByService 里，总量用累计值（见 flipsTotal）
	}
	window := now.Sub(m.startedAt)
	m.flipsNew = 0 // 增量只对"下一次快照"有意义：告警读一次就算消费掉
	m.mu.Unlock()

	snap := metricsSnapshot{
		WindowSec:        window.Seconds(),
		Total:            total,
		Mirage:           mirage,
		Failopen:         failopen,
		Fallback:         fallback,
		Refused:          refused,
		SessionFlipCount: flipsTotal,
		SessionFlipNew:   flipsNew,
		ByService:        byService,
		Sampled:          total >= minSampleForRatio,
	}
	if total > 0 {
		snap.RerouteRatio = float64(mirage) / float64(total)
		snap.FailopenRatio = float64(failopen) / float64(total)
	}
	snap.Alerts = alertsOf(snap)
	return snap
}

// alertsOf 是**纯函数**（便于单测）：从快照判告警原因，空 = 无告警。
//
// 三条判据，各自有明确的「为什么」：
//
//	① failopen_ratio ≥ 0.5 —— 大部分请求都没判定成功 ⇒ 引擎/链路已经坏了；
//	② session_flip_count > 0 —— 同一会话去向变过 ⇒ 对手可能已经看到两种环境；
//	③ reroute_ratio < 0.05 且样本足够 —— 系统名义在跑，实际几乎不改道（灰度/策略配错）。
func alertsOf(snap metricsSnapshot) []string {
	var out []string
	if snap.Sampled && snap.FailopenRatio >= failOpenAlertRatio {
		out = append(out, "failopen_ratio")
	}
	if snap.SessionFlipNew > 0 {
		out = append(out, "session_flip")
	}
	if snap.Sampled && snap.RerouteRatio < rerouteAlertBaseline {
		out = append(out, "reroute_below_baseline")
	}
	sort.Strings(out)
	return out
}
