package connector

import (
	"sort"
	"sync"
	"time"
)

// 会话事件的取值。
const (
	EventOnline    = "online"    // 首次上线
	EventReconnect = "reconnect" // 掉线后恢复
	EventOffline   = "offline"   // 下线（计划内或意外）
)

// 事件环上限：足够页面回放「最近发生了什么」，又不让重连风暴撑爆内存。
const maxSessionEvents = 500

// SessionState 是一条连接器会话的观测快照（由网关上报；控制台不出站）。
type SessionState struct {
	SessionID        string    `json:"session_id"` // 网关分配
	CredentialID     string    `json:"credential_id"`
	Name             string    `json:"name"`
	Hosts            []string  `json:"hosts"`
	LocalAddr        string    `json:"local_addr"` // 连接器侧真实业务地址
	ConnectorIP      string    `json:"connector_ip"`
	ConnectorVersion string    `json:"connector_version"`
	GatewayNode      string    `json:"gateway_node"`
	Online           bool      `json:"online"`
	PlannedClose     bool      `json:"planned_close"` // 计划内下线（kind=closing）
	RttMs            int64     `json:"rtt_ms"`
	Since            time.Time `json:"since"`          // 连接建立时间
	LastHeartbeat    time.Time `json:"last_heartbeat"` // 最近一次上报
}

// SessionEventRecord 是事件环里的一条（页面时间线与 KPI 的数据源）。
type SessionEventRecord struct {
	At        time.Time `json:"at"`
	SessionID string    `json:"session_id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"` // online / reconnect / offline
	Detail    string    `json:"detail,omitempty"`
}

// SessionStore 是内存态的会话观测表 + 事件环 + 网关联系时间。
// 纯观测：网关是唯一写入者（经集成面上报），进程重启即清空（下一次上报自然恢复）。
type SessionStore struct {
	mu             sync.RWMutex
	sessions       map[string]*SessionState // 按网关会话 ID
	events         []SessionEventRecord     // 有界环（最旧在前）
	gatewayContact time.Time
	now            func() time.Time
}

// NewSessionStore 创建会话观测表（无持久化：观测快照不值得落盘）。
func NewSessionStore(now func() time.Time) *SessionStore {
	if now == nil {
		now = time.Now
	}
	return &SessionStore{sessions: map[string]*SessionState{}, now: now}
}

// Upsert 记录一次会话上报，返回状态过渡（"" = 无变化，纯心跳刷新）。
// 调用方（集成面）据此写审计，避免心跳刷屏。
func (s *SessionStore) Upsert(next SessionState, detail string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	next.LastHeartbeat = now
	cur, ok := s.sessions[next.SessionID]
	if !ok {
		since := now
		if !next.Since.IsZero() {
			since = next.Since
		}
		next.Since = since
		next.Online = true // 新会话必然在线
		cp := next
		s.sessions[next.SessionID] = &cp
		s.pushEventLocked(SessionEventRecord{At: now, SessionID: next.SessionID, Name: next.Name,
			Kind: EventOnline, Detail: detail})
		return EventOnline
	}
	transition := ""
	switch {
	case next.Online && !cur.Online:
		transition = EventReconnect
	case !next.Online && cur.Online:
		transition = EventOffline
	}
	cp := next
	if transition == "" {
		// 心跳刷新：保留建立时间，其余字段以本次上报为准
		cp.Since = cur.Since
	} else {
		cp.Since = cur.Since
		s.pushEventLocked(SessionEventRecord{At: now, SessionID: next.SessionID, Name: next.Name,
			Kind: transition, Detail: detail})
	}
	s.sessions[next.SessionID] = &cp
	return transition
}

// List 返回全部会话快照；超过 ttl 没有心跳的在线会话被惰性标为离线（并记一次 offline 事件）。
func (s *SessionStore) List(ttl time.Duration) []SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	out := make([]SessionState, 0, len(s.sessions))
	for id, cur := range s.sessions {
		if cur.Online && ttl > 0 && now.Sub(cur.LastHeartbeat) > ttl {
			cp := *cur
			cp.Online = false
			cp.PlannedClose = false
			s.sessions[id] = &cp
			s.pushEventLocked(SessionEventRecord{At: now.UTC(), SessionID: id, Name: cp.Name,
				Kind: EventOffline, Detail: "心跳过期（网关超过 TTL 未上报）"})
			out = append(out, cp)
			continue
		}
		out = append(out, *cur)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Events 返回最近 limit 条事件（最新在前）。
func (s *SessionStore) Events(limit int) []SessionEventRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.events)
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]SessionEventRecord, limit)
	for i := 0; i < limit; i++ {
		out[i] = s.events[n-1-i]
	}
	return out
}

// TouchGateway 记录网关最近一次联系（拉取 key 表）——「网关已部署」的判定依据。
func (s *SessionStore) TouchGateway() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gatewayContact = s.now().UTC()
}

// GatewaySeen 返回 (最近联系时间, 是否曾联系)。
func (s *SessionStore) GatewaySeen() (time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gatewayContact, !s.gatewayContact.IsZero()
}

// TodayDisconnects 统计「今天」的 offline 事件数（KPI）。
func (s *SessionStore) TodayDisconnects() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	today := s.now().Format("2006-01-02")
	n := 0
	for _, e := range s.events {
		if e.Kind == EventOffline && e.At.Format("2006-01-02") == today {
			n++
		}
	}
	return n
}

// RttMedianMs 返回在线会话的 RTT 中位数（毫秒；无在线会话返回 0）。
func (s *SessionStore) RttMedianMs() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var rtts []int64
	for _, cur := range s.sessions {
		if cur.Online && cur.RttMs > 0 {
			rtts = append(rtts, cur.RttMs)
		}
	}
	if len(rtts) == 0 {
		return 0
	}
	sort.Slice(rtts, func(i, j int) bool { return rtts[i] < rtts[j] })
	return rtts[len(rtts)/2]
}

// HasOnlineCredential 报告某凭证当前是否有在线会话（凭证列表的实时状态）。
func (s *SessionStore) HasOnlineCredential(credentialID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, cur := range s.sessions {
		if cur.CredentialID == credentialID && cur.Online {
			return true
		}
	}
	return false
}

func (s *SessionStore) pushEventLocked(e SessionEventRecord) {
	s.events = append(s.events, e)
	if len(s.events) > maxSessionEvents {
		s.events = s.events[len(s.events)-maxSessionEvents:]
	}
}
