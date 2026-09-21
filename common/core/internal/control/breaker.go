package control

import (
	"sync"
	"time"
)

// BreakerConfig 是熔断器的可调项。零值字段用默认。
type BreakerConfig struct {
	// MinSamples 是开始判断前的最小样本数 —— 样本太少时错误率不可信。
	MinSamples int
	// MaxFailRatio 是错误率阈值：窗口内错误率**超过**它即跳闸。
	MaxFailRatio float64
	// Cooldown 是跳闸后的冷却期；期满转半开（清空计数、放行试探）。
	Cooldown time.Duration
	// Now 注入时钟，使跳闸/恢复可复现、可测。
	Now func() time.Time
}

// Breaker 是 NI-10 的熔断实现：决策错误率超阈值时自动切换为纯放行。
//
// 它落在**服务面**（看到全部请求，天然能统计错误率），不属于决策逻辑。
// 状态**各副本本地** —— 每个副本独立跳闸。这不是业务状态，是可丢弃的降级开关，
// 因此不违反 AR-9 的无状态多副本。
//
// 采用固定窗口 + 冷却期：跳闸后冷却期内全放行；冷却期满清空计数从头统计（半开）。
// 不做滑动窗口与按错误类型分类 —— 那是调优，先有正确的降级语义。
type Breaker struct {
	cfg BreakerConfig

	mu       sync.Mutex
	fails    int
	total    int
	opened   bool
	openedAt time.Time
}

// NewBreaker 构造熔断器。
func NewBreaker(cfg BreakerConfig) *Breaker {
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = 20
	}
	if cfg.MaxFailRatio <= 0 || cfg.MaxFailRatio >= 1 {
		cfg.MaxFailRatio = 0.5
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 5 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Breaker{cfg: cfg}
}

// Allow 报告当前是否允许走决策路径。熔断打开时返回 false（调用方应纯放行）。
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.opened {
		return true
	}
	// 冷却期满 → 半开：清空计数，放行试探。
	if b.cfg.Now().Sub(b.openedAt) >= b.cfg.Cooldown {
		b.opened = false
		b.fails, b.total = 0, 0
		return true
	}
	return false
}

// Record 记录一次决策结果。err 非空算失败。
func (b *Breaker) Record(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.opened {
		return // 跳闸期间的请求没走决策，不计数
	}
	b.total++
	if err != nil {
		b.fails++
	}
	if b.total >= b.cfg.MinSamples && float64(b.fails)/float64(b.total) > b.cfg.MaxFailRatio {
		b.opened = true
		b.openedAt = b.cfg.Now()
	}
}

// Opened 报告当前是否跳闸（供启动日志与观测）。
func (b *Breaker) Opened() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.opened
}
