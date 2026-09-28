package auth

import (
	"sync"
	"time"
)

// Limiter 是登录失败计数器：窗口内失败达到阈值即锁定一段时间。
//
// 同一次登录同时按「账号」与「来源」两个键计数：只按账号会被换号撞库绕过，
// 只按来源会被分布式来源绕过 —— 两个都锁才有意义。表有容量上限，防止被随机键撑爆。
type Limiter struct {
	mu          sync.Mutex
	entries     map[string]*limitEntry
	maxFailures int
	window      time.Duration
	lockout     time.Duration
	maxEntries  int
	now         func() time.Time
}

type limitEntry struct {
	failures    int
	first       time.Time
	lockedUntil time.Time
}

// NewLimiter 创建计数器。
func NewLimiter(maxFailures int, window, lockout time.Duration, maxEntries int, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{
		entries: map[string]*limitEntry{}, maxFailures: maxFailures,
		window: window, lockout: lockout, maxEntries: maxEntries, now: now,
	}
}

// Locked 报告键是否处于锁定期，并返回剩余时间。
func (l *Limiter) Locked(key string) (time.Duration, bool) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return 0, false
	}
	if now.Before(e.lockedUntil) {
		return e.lockedUntil.Sub(now), true
	}
	return 0, false
}

// Fail 记录一次失败；达到阈值时进入锁定。
func (l *Limiter) Fail(key string) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok || now.Sub(e.first) >= l.window {
		if !ok {
			l.makeRoomLocked(now)
		}
		e = &limitEntry{first: now}
		l.entries[key] = e
	}
	e.failures++
	if e.failures >= l.maxFailures {
		e.lockedUntil = now.Add(l.lockout)
		e.failures = 0
		e.first = now
	}
}

// Success 清除键的失败记录（仅账号键；来源键不因一次成功而清零，防止「一个真账号掩护撞库」）。
func (l *Limiter) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *Limiter) makeRoomLocked(now time.Time) {
	if l.maxEntries <= 0 || len(l.entries) < l.maxEntries {
		return
	}
	for k, e := range l.entries {
		if now.Sub(e.first) >= l.window && !now.Before(e.lockedUntil) {
			delete(l.entries, k)
		}
	}
	// 仍然满：丢弃最早的非锁定条目（锁定条目优先保留，否则撞库者可以靠刷键解锁）。
	for len(l.entries) >= l.maxEntries {
		var victim string
		var oldest time.Time
		for k, e := range l.entries {
			if now.Before(e.lockedUntil) {
				continue
			}
			if victim == "" || e.first.Before(oldest) {
				victim, oldest = k, e.first
			}
		}
		if victim == "" {
			return // 全是锁定条目：宁可超额也不解锁
		}
		delete(l.entries, victim)
	}
}
