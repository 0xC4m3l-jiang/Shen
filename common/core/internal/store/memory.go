package store

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"shen/common/core/internal/contract"
)

// ErrNoPolicy 表示尚未发布过任何策略版本。
var ErrNoPolicy = errors.New("store: 尚无策略版本")

// ErrEmptyEventID 表示事件缺少幂等键，无法保证去重。
var ErrEmptyEventID = errors.New("store: EventID 为空，无法保证幂等")

// 内存实现按实体各建一个类型。这不是冗余 —— Go 不支持方法重载，
// 若用一个 struct 同时实现 SessionStore 与 IsolationStore，两者的 Get/Put 签名会冲突。
// 按实体拆同时也让单测替身最小化。

// maxMemEntries 是单个内存存储的容量上限。
//
// 内存实现只用于开发与阶段 1；但「只写不读」的键（一次性会话、已解除的隔离、过期的预生成内容）
// 会让 map 无限增长 —— 长跑时就是内存泄漏。写入超限时先清理**已过期**条目：
// 这不改变语义（过期项在 Get 时本来就当作不存在），只是把释放时机提前。
const maxMemEntries = 100_000

// sweepExpiredIfFull 在 map 达到上限时清理已过期条目。
//
// 三个带 TTL 的存储共用同一策略，避免三份重复的清理循环。
func sweepExpiredIfFull[V any](m map[string]V, expired func(V) bool) {
	if len(m) < maxMemEntries {
		return
	}
	for k, v := range m {
		if expired(v) {
			delete(m, k)
		}
	}
}

// ── SessionStore ────────────────────────────────────────

type sessionEntry struct {
	key       contract.SessionKey
	expiresAt time.Time
}

// SessionMemory 是 SessionStore 的内存实现。
type SessionMemory struct {
	mu  sync.RWMutex
	now func() time.Time
	m   map[string]sessionEntry
}

// NewSessionMemory 构造内存实现；now 为 nil 时使用 time.Now。
//
// 时钟由外部注入，使过期行为可测、可回放。
func NewSessionMemory(now func() time.Time) *SessionMemory {
	if now == nil {
		now = time.Now
	}
	return &SessionMemory{now: now, m: map[string]sessionEntry{}}
}

// Get 取会话身份；不存在或已过期时返回 ok=false。
func (s *SessionMemory) Get(_ context.Context, id string) (contract.SessionKey, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.m[id]
	if !ok || (!e.expiresAt.IsZero() && s.now().After(e.expiresAt)) {
		return contract.SessionKey{}, false, nil
	}
	return e.key, true, nil
}

// Put 写会话身份；ttl <= 0 表示不过期。
func (s *SessionMemory) Put(_ context.Context, k contract.SessionKey, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sweepExpiredIfFull(s.m, func(e sessionEntry) bool {
		return !e.expiresAt.IsZero() && s.now().After(e.expiresAt)
	})
	var exp time.Time
	if ttl > 0 {
		exp = s.now().Add(ttl)
	}
	s.m[k.ID] = sessionEntry{key: k, expiresAt: exp}
	return nil
}

// ── IsolationStore ──────────────────────────────────────

// IsolationMemory 是 IsolationStore 的内存实现。
type IsolationMemory struct {
	mu  sync.RWMutex
	now func() time.Time
	m   map[string]contract.IsolationHit
}

// NewIsolationMemory 构造内存实现。
func NewIsolationMemory(now func() time.Time) *IsolationMemory {
	if now == nil {
		now = time.Now
	}
	return &IsolationMemory{now: now, m: map[string]contract.IsolationHit{}}
}

// Get 查隔离名单；未命中或 TTL 到期均返回 Hit=false。
func (s *IsolationMemory) Get(_ context.Context, key contract.SessionKey) (contract.IsolationHit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hit, ok := s.m[key.ID]
	if !ok {
		return contract.IsolationHit{}, nil
	}
	// TTL 到期即自动解除
	if !hit.ExpiresAt.IsZero() && s.now().After(hit.ExpiresAt) {
		return contract.IsolationHit{}, nil
	}
	return hit, nil
}

// Put 写隔离记录。
func (s *IsolationMemory) Put(_ context.Context, key contract.SessionKey, hit contract.IsolationHit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sweepExpiredIfFull(s.m, func(h contract.IsolationHit) bool {
		return !h.ExpiresAt.IsZero() && s.now().After(h.ExpiresAt)
	})
	s.m[key.ID] = hit
	return nil
}

// ── DecisionStore ───────────────────────────────────────

// decisionEntry 给判定缓存加过期时刻（与 Session / Content / Isolation 三个存储同一形状）。
type decisionEntry struct {
	decision  contract.Decision
	expiresAt time.Time
	// archivedAt 是**本存储**记录该条的时刻（不是驱动层的时间）：
	// `contract.Decision` 本身不带时间戳（时间戳只存在观测面的事件载荷里），
	// 而 `DecisionQuery.Since` 要有东西可比 —— 就用写入时刻，并在文档里写清口径。
	archivedAt time.Time
}

// DecisionMemory 是 DecisionStore 的内存实现。
//
// 两类数据各有一处归属，避免「缓存」与「归档」共用一张无界 map：
//   - m 是**判定缓存**（按 decision_id 去重，有 TTL 与容量上限）；
//   - recent 是**最近判定记录**（观测面读侧，newest first，有容量上限）。
type DecisionMemory struct {
	mu     sync.RWMutex
	now    func() time.Time
	m      map[string]decisionEntry
	recent []decisionEntry
}

// NewDecisionMemory 构造内存实现；now 为 nil 时使用 time.Now。
//
// 时钟由外部注入，使 TTL 行为可测、可回放（MD-6：判定不得依赖系统时钟）。
func NewDecisionMemory(now func() time.Time) *DecisionMemory {
	if now == nil {
		now = time.Now
	}
	return &DecisionMemory{now: now, m: map[string]decisionEntry{}}
}

// GetCached 取判定缓存；不存在或已过期时返回 ok=false。
func (s *DecisionMemory) GetCached(_ context.Context, decisionID string) (contract.Decision, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[decisionID]
	if !ok {
		return contract.Decision{}, false, nil
	}
	if !e.expiresAt.IsZero() && !s.now().Before(e.expiresAt) {
		delete(s.m, decisionID)
		return contract.Decision{}, false, nil
	}
	return e.decision, true, nil
}

// PutCached 写判定缓存；ttl <= 0 时用 DefaultDecisionCacheTTL。
func (s *DecisionMemory) PutCached(_ context.Context, d contract.Decision, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putLocked(d, ttl)
	return nil
}

// putLocked 写缓存条目。调用方必须持锁。
//
// 满则清理已过期条目；清完仍满就整体清空 —— 与适配器侧判定缓存同一策略（MD-10 容量上限），
// 缓存本就可丢失，清空不会改变判定语义。
func (s *DecisionMemory) putLocked(d contract.Decision, ttl time.Duration) {
	sweepExpiredIfFull(s.m, func(e decisionEntry) bool {
		return !e.expiresAt.IsZero() && !s.now().Before(e.expiresAt)
	})
	if len(s.m) >= maxMemEntries {
		s.m = map[string]decisionEntry{}
	}
	if ttl <= 0 {
		ttl = DefaultDecisionCacheTTL
	}
	s.m[d.DecisionID] = decisionEntry{decision: d, expiresAt: s.now().Add(ttl)}
}

// List 返回最近的判定记录（newest first）。
//
// `q.Since` 按**本存储的写入时刻**过滤（见 decisionEntry.archivedAt）：判定载荷本身无时间戳。
func (s *DecisionMemory) List(_ context.Context, q DecisionQuery) ([]contract.Decision, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]contract.Decision, 0, limit)
	for _, e := range s.recent {
		if !q.Since.IsZero() && e.archivedAt.Before(q.Since) {
			continue
		}
		out = append(out, e.decision)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Archive 归档判定：写入最近记录（生产实现写入 ClickHouse）并更新判定缓存。
//
// 两条写入在**同一个锁**内完成：读侧看到的是「要么两条都有、要么都没有」，不会出现半条。
func (s *DecisionMemory) Archive(_ context.Context, d contract.Decision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putLocked(d, DefaultDecisionCacheTTL)
	// newest first：头部插入，超上限就截断尾部（观测缓冲不得无界增长）。
	s.recent = append([]decisionEntry{{decision: d, archivedAt: s.now()}}, s.recent...)
	if len(s.recent) > DefaultDecisionBuffer {
		s.recent = s.recent[:DefaultDecisionBuffer]
	}
	return nil
}

// ── EventStore ──────────────────────────────────────────

// EventMemory 是 EventStore 的内存实现。
//
// 它同时保留**最近 DefaultEventBuffer 条事件本体**，供观测面读侧（控制台看告警与流量）使用。
// 内存实现不追求完整历史 —— 那是真实存储（ClickHouse）的事；这里只要「够看最近的」。
type EventMemory struct {
	mu  sync.RWMutex
	now func() time.Time
	// m 是幂等键 → **去重窗口到期时刻**（`AR-11`）：窗口内重复上报折叠，窗口外按新事件收。
	m map[string]time.Time
	// recent 是最近写入的事件（newest first）。有上限：观测缓冲不得无界增长。
	recent []contract.Event
}

// NewEventMemory 构造内存实现；now 为 nil 时使用 time.Now。
func NewEventMemory(now func() time.Time) *EventMemory {
	if now == nil {
		now = time.Now
	}
	return &EventMemory{now: now, m: map[string]time.Time{}}
}

// Write 幂等写入：同一 EventID 在去重窗口内已存在时返回 false。
func (s *EventMemory) Write(_ context.Context, ev contract.Event) (bool, error) {
	if ev.EventID == "" {
		return false, ErrEmptyEventID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if exp, dup := s.m[ev.EventID]; dup && now.Before(exp) {
		return false, nil
	}
	// 去重键不可无界增长：满则先清过期；清完仍满就整体清空。
	// 清空只影响去重窗口的内存记账（可能重新接受一条旧事件），不丢任何事件本体。
	sweepExpiredIfFull(s.m, func(exp time.Time) bool { return !now.Before(exp) })
	if len(s.m) >= maxMemEntries {
		s.m = map[string]time.Time{}
	}
	s.m[ev.EventID] = now.Add(DefaultEventDedupWindow)
	// newest first：头部插入，超上限就截断尾部。
	s.recent = append([]contract.Event{ev}, s.recent...)
	if len(s.recent) > DefaultEventBuffer {
		s.recent = s.recent[:DefaultEventBuffer]
	}
	return true, nil
}

// List 返回最近的事件（newest first），支持按时间与类型过滤。
func (s *EventMemory) List(_ context.Context, q EventQuery) ([]contract.Event, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]contract.Event, 0, limit)
	for _, ev := range s.recent {
		if !q.Since.IsZero() && ev.CreatedAt.Before(q.Since) {
			continue
		}
		if q.Type != "" && ev.Type != q.Type {
			continue
		}
		out = append(out, ev)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// WriteBatch 批量幂等写入，返回实际写入条数。
func (s *EventMemory) WriteBatch(ctx context.Context, evs []contract.Event) (int, error) {
	n := 0
	for _, ev := range evs {
		ok, err := s.Write(ctx, ev)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

// ── PolicyStore ─────────────────────────────────────────

// PolicyMemory 是 PolicyStore 的内存实现。
type PolicyMemory struct {
	mu sync.RWMutex
	p  *contract.PolicySnapshot
	// acks 按 (policy_id, version, adapter_id) 存回执：同一适配器对同一版本只保留最新一条。
	acks map[string]contract.PolicyAck
}

// NewPolicyMemory 构造内存实现。
func NewPolicyMemory() *PolicyMemory { return &PolicyMemory{} }

// Current 取当前策略版本；未发布过时返回 ErrNoPolicy。
func (s *PolicyMemory) Current(_ context.Context) (contract.PolicySnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.p == nil {
		return contract.PolicySnapshot{}, ErrNoPolicy
	}
	return *s.p, nil
}

// Publish 发布策略版本。版本只增：低于等于当前版本时拒绝；
// 回滚不是回退版本号，而是发布一个内容为旧版的新版本。
func (s *PolicyMemory) Publish(_ context.Context, p contract.PolicySnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p != nil && p.Version <= s.p.Version {
		return errors.New("store: 策略版本必须单调递增")
	}
	cp := p
	s.p = &cp
	return nil
}

// ackKey 是回执的去重键：一个适配器对一个版本只有一条最新回执。
func ackKey(a contract.PolicyAck) string {
	return a.PolicyID + "\x00" + strconv.FormatUint(a.Version, 10) + "\x00" + a.AdapterID
}

// RecordAck 记录适配器回执（幂等：同一键只保留最新一条）。
func (s *PolicyMemory) RecordAck(_ context.Context, a contract.PolicyAck) error {
	if a.AdapterID == "" {
		// 没有适配器标识的回执无法对账（AR-13 要的是「谁应用了哪个版本」）。
		return errors.New("store: 回执缺少 adapter_id，无法对账")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.acks == nil {
		s.acks = map[string]contract.PolicyAck{}
	}
	s.acks[ackKey(a)] = a
	return nil
}

// Acks 返回已记录的回执；顺序不保证（对账不依赖顺序）。
func (s *PolicyMemory) Acks(_ context.Context) ([]contract.PolicyAck, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]contract.PolicyAck, 0, len(s.acks))
	for _, a := range s.acks {
		out = append(out, a)
	}
	return out, nil
}

// ── DecoyStore ───────────────────────────────────────────

// DecoyMemory 是 DecoyStore 的内存实现。
type DecoyMemory struct {
	mu sync.RWMutex
	m  map[string]contract.DecoyAsset
}

// NewDecoyMemory 构造内存实现。
func NewDecoyMemory() *DecoyMemory {
	return &DecoyMemory{m: map[string]contract.DecoyAsset{}}
}

// List 返回全部诱饵资产（副本，保证快照不可变）。
func (s *DecoyMemory) List(_ context.Context) ([]contract.DecoyAsset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]contract.DecoyAsset, 0, len(s.m))
	for _, a := range s.m {
		out = append(out, a)
	}
	return out, nil
}

// Get 按 ID 取诱饵资产。
func (s *DecoyMemory) Get(_ context.Context, id string) (contract.DecoyAsset, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.m[id]
	return a, ok, nil
}

// Put 写诱饵资产。
func (s *DecoyMemory) Put(_ context.Context, a contract.DecoyAsset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[a.ID] = a
	return nil
}

// ── ContentStore ─────────────────────────────────────────

// contentEntry 为预生成内容加 TTL。
type contentEntry struct {
	body      []byte
	expiresAt time.Time
}

// ContentMemory 是 ContentStore 的内存实现。
type ContentMemory struct {
	mu  sync.RWMutex
	now func() time.Time
	m   map[string]contentEntry
}

// NewContentMemory 构造内存实现；now 为 nil 时使用 time.Now。
func NewContentMemory(now func() time.Time) *ContentMemory {
	if now == nil {
		now = time.Now
	}
	return &ContentMemory{now: now, m: map[string]contentEntry{}}
}

// Get 取预生成内容；不存在或已过期时返回 ok=false。
func (s *ContentMemory) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.m[key]
	if !ok {
		return nil, false, nil
	}
	if !e.expiresAt.IsZero() && s.now().After(e.expiresAt) {
		return nil, false, nil
	}
	return append([]byte(nil), e.body...), true, nil
}

// Put 写预生成内容；ttl <= 0 表示不过期。
func (s *ContentMemory) Put(_ context.Context, key string, body []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sweepExpiredIfFull(s.m, func(e contentEntry) bool {
		return !e.expiresAt.IsZero() && s.now().After(e.expiresAt)
	})
	var exp time.Time
	if ttl > 0 {
		exp = s.now().Add(ttl)
	}
	s.m[key] = contentEntry{body: append([]byte(nil), body...), expiresAt: exp}
	return nil
}

// ── 打包 ────────────────────────────────────────────────

// MemStores 把七个内存实现打包，供单测与阶段 1 使用。
type MemStores struct {
	Session   *SessionMemory
	Isolation *IsolationMemory
	Decision  *DecisionMemory
	Event     *EventMemory
	Policy    *PolicyMemory
	Decoy     *DecoyMemory
	Content   *ContentMemory
}

// NewMemStores 构造整套内存实现。
func NewMemStores(now func() time.Time) *MemStores {
	return &MemStores{
		Session:   NewSessionMemory(now),
		Isolation: NewIsolationMemory(now),
		Decision:  NewDecisionMemory(now),
		Event:     NewEventMemory(now),
		Policy:    NewPolicyMemory(),
		Decoy:     NewDecoyMemory(),
		Content:   NewContentMemory(now),
	}
}

var (
	_ SessionStore   = (*SessionMemory)(nil)
	_ IsolationStore = (*IsolationMemory)(nil)
	_ DecisionStore  = (*DecisionMemory)(nil)
	_ EventStore     = (*EventMemory)(nil)
	_ PolicyStore    = (*PolicyMemory)(nil)
	_ DecoyStore     = (*DecoyMemory)(nil)
	_ ContentStore   = (*ContentMemory)(nil)
)
