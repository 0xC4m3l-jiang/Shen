package isolation

import (
	"context"
	"fmt"
	"time"

	"shen/common/core/internal/contract"
)

// Engine 是 Isolation 的实现。它无状态（AR-9）：隔离记录全部在 store 里。
type Engine struct {
	store Store
	now   func() time.Time
}

// New 构造隔离引擎。store 为 nil 时返回错误；now 为 nil 时用 time.Now。
//
// 时钟由外部注入，使 TTL 行为可测、可回放（MD-6：禁止依赖系统时钟做判定）。
func New(st Store, now func() time.Time) (*Engine, error) {
	if st == nil {
		return nil, fmt.Errorf("isolation: Store 不能为 nil")
	}
	if now == nil {
		now = time.Now
	}
	return &Engine{store: st, now: now}, nil
}

// Check 查询隔离状态。
//
// 它**不吞掉存储错误** —— NI-10 要求存储不可用时回退调用核心判定，
// 因此把错误交给调用方（control 服务面）决定 fail-open，而不是在这里静默返回未命中。
func (e *Engine) Check(ctx context.Context, key contract.SessionKey) (contract.IsolationHit, error) {
	hit, err := e.store.Get(ctx, key)
	if err != nil {
		return contract.IsolationHit{}, fmt.Errorf("isolation: 查询失败：%w", err)
	}
	return hit, nil
}

// Isolate 写入隔离记录。
//
// TTL 必须为正：永不过期的隔离会让误判无法自愈，且与「到期自动解除」的设计冲突。
func (e *Engine) Isolate(ctx context.Context, key contract.SessionKey, reason string, ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("isolation: TTL 必须为正，得到 %v", ttl)
	}
	if key.ID == "" {
		return fmt.Errorf("isolation: 会话键不能为空")
	}
	hit := contract.IsolationHit{
		Hit:       true,
		Reason:    reason,
		ExpiresAt: e.now().Add(ttl),
	}
	if err := e.store.Put(ctx, key, hit); err != nil {
		return fmt.Errorf("isolation: 写入失败：%w", err)
	}
	return nil
}

var _ Isolation = (*Engine)(nil)
