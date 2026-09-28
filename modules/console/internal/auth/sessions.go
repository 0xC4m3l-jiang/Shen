package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"

	"shen/modules/console/internal/rbac"
)

// Session 是一次登录的服务端状态（创建后**不再变更**的可快照部分）。
//
// 浏览器只持有不透明的会话令牌（HttpOnly Cookie）；表里以令牌的 SHA-256 为键 ——
// 内存转储或调试输出里看不到可直接复用的令牌。
type Session struct {
	Username   string
	Role       rbac.Role
	CSRF       string // 写请求必须在 X-CSRF-Token 头里回带它（与会话绑定）
	MustChange bool   // 首次登录/被重置口令：除改密与登出外全部拒绝
	Source     string // 登录来源地址（仅审计）
	CreatedAt  time.Time
	LastSeen   time.Time // 创建时的快照；随后的活跃时间在 entry.lastSeen 里原子滑动
}

// entry 是表里的实际存储：info 不可变，lastSeen 由每次 Lookup 原子滑动。
// 把可变状态收进单独的 atomic.Int64，而不是放在 Session 里 ——
// 否则「按值拷贝 Session 返回给调用方」会与并发 Store 形成宽窄混用的数据竞争。
type entry struct {
	info     Session
	lastSeen atomic.Int64 // unixNano
}

// SessionStore 是内存会话表：空闲超时 + 绝对超时 + 容量上限。
//
// 并发模型：RWMutex + 原子 lastSeen。
//   - Lookup 是每请求必经的热路径，只持读锁：命中且未过期时用原子写滑动活跃时间，
//     不产生任何写锁竞争；过期删除走「读锁内判定 → 短暂升级写锁删单条」。
//   - Create / Delete / DeleteUser 是低频管理操作，走写锁。
//
// 为什么不落盘：进程重启后要求重新登录是可接受的代价，换来的是「会话令牌从不写盘」。
type SessionStore struct {
	mu       sync.RWMutex
	byKey    map[string]*entry
	idle     time.Duration
	absolute time.Duration
	max      int
	now      func() time.Time
}

// NewSessionStore 创建会话表。
func NewSessionStore(idle, absolute time.Duration, max int, now func() time.Time) *SessionStore {
	if now == nil {
		now = time.Now
	}
	return &SessionStore{byKey: map[string]*entry{}, idle: idle, absolute: absolute, max: max, now: now}
}

func keyOf(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Create 新建会话，返回令牌与会话副本。
func (s *SessionStore) Create(username string, role rbac.Role, mustChange bool, source string) (string, Session, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", Session{}, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return "", Session{}, err
	}
	now := s.now()
	e := &entry{info: Session{
		Username: username, Role: role, CSRF: csrf, MustChange: mustChange,
		Source: source, CreatedAt: now, LastSeen: now,
	}}
	e.lastSeen.Store(now.UnixNano())
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictLocked(now)
	s.byKey[keyOf(token)] = e
	return token, e.info, nil
}

// Lookup 校验令牌并滑动空闲超时；过期即删除（读锁判定，写锁删除）。
func (s *SessionStore) Lookup(token string) (Session, bool) {
	if token == "" {
		return Session{}, false
	}
	key := keyOf(token) // 哈希在锁外算：SHA-256 是热路径上最贵的部分
	now := s.now()
	s.mu.RLock()
	e, ok := s.byKey[key]
	if !ok {
		s.mu.RUnlock()
		return Session{}, false
	}
	if s.expired(e, now) {
		// 升级为写锁只删这一条；重查防止期间被 Create 复用同名键或已被并发删除。
		s.mu.RUnlock()
		s.mu.Lock()
		if cur, still := s.byKey[key]; still && s.expired(cur, now) {
			delete(s.byKey, key)
		}
		s.mu.Unlock()
		return Session{}, false
	}
	s.mu.RUnlock()
	e.lastSeen.Store(now.UnixNano()) // 原子滑动，无需写锁
	return e.info, true
}

// Delete 注销单个会话。
func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byKey, keyOf(token))
}

// DeleteUser 注销某账号的全部会话（改角色 / 禁用 / 删除 / 改口令后必须调用）。
func (s *SessionStore) DeleteUser(username string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k, e := range s.byKey {
		if e.info.Username == username {
			delete(s.byKey, k)
			n++
		}
	}
	return n
}

// Len 返回当前会话数（含未清理的过期会话；仅用于观测与测试）。
func (s *SessionStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byKey)
}

func (s *SessionStore) expired(e *entry, now time.Time) bool {
	last := time.Unix(0, e.lastSeen.Load())
	return now.Sub(last) >= s.idle || now.Sub(e.info.CreatedAt) >= s.absolute
}

// evictLocked 在新增前腾位置：先清过期，仍满则淘汰最久未活动的会话。
func (s *SessionStore) evictLocked(now time.Time) {
	for k, e := range s.byKey {
		if s.expired(e, now) {
			delete(s.byKey, k)
		}
	}
	for s.max > 0 && len(s.byKey) >= s.max {
		var oldestKey string
		var oldest int64
		for k, e := range s.byKey {
			if oldestKey == "" || e.lastSeen.Load() < oldest {
				oldestKey, oldest = k, e.lastSeen.Load()
			}
		}
		delete(s.byKey, oldestKey)
	}
}
