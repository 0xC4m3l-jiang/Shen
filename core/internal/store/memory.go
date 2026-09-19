package store

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"shen/core/internal/contract"
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

// DecisionMemory 是 DecisionStore 的内存实现。
type DecisionMemory struct {
	mu sync.RWMutex
	m  map[string]contract.Decision
}

// NewDecisionMemory 构造内存实现。
func NewDecisionMemory() *DecisionMemory {
	return &DecisionMemory{m: map[string]contract.Decision{}}
}

// GetCached 取判定缓存。
func (s *DecisionMemory) GetCached(_ context.Context, decisionID string) (contract.Decision, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.m[decisionID]
	return d, ok, nil
}

// PutCached 写判定缓存。
func (s *DecisionMemory) PutCached(_ context.Context, d contract.Decision, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[d.DecisionID] = d
	return nil
}

// Archive 归档判定（生产实现写入 ClickHouse）。
func (s *DecisionMemory) Archive(ctx context.Context, d contract.Decision) error {
	return s.PutCached(ctx, d, 0)
}

// ── EventStore ──────────────────────────────────────────

// EventMemory 是 EventStore 的内存实现。
type EventMemory struct {
	mu sync.RWMutex
	m  map[string]struct{}
}

// NewEventMemory 构造内存实现。
func NewEventMemory() *EventMemory { return &EventMemory{m: map[string]struct{}{}} }

// Write 幂等写入：同 EventID 已存在时返回 false。
func (s *EventMemory) Write(_ context.Context, ev contract.Event) (bool, error) {
	if ev.EventID == "" {
		return false, ErrEmptyEventID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.m[ev.EventID]; dup {
		return false, nil
	}
	s.m[ev.EventID] = struct{}{}
	return true, nil
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
		Decision:  NewDecisionMemory(),
		Event:     NewEventMemory(),
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
