package policy

import (
	"context"
	"sync/atomic"

	"shen/common/core/internal/contract"
)

// Live 是「当前生效的 Loader」的持有者：读侧一次原子读、零锁；写侧整体替换（Swap）。
//
// 为什么需要它：Loader 本身是不可变快照（AR-9），而管控台同步要求**运行中**换策略。
// 不可变 + 原子指针 = 每个请求看到的是某一份完整快照，永远不会读到"改了一半"的状态。
// 本类型没有模块级变量；它不承载请求级状态。
type Live struct {
	p atomic.Pointer[Loader]
}

// NewLive 用初始快照构造持有者（l 不能为 nil）。
func NewLive(l *Loader) *Live {
	if l == nil {
		panic("policy: NewLive 需要非 nil 的 Loader")
	}
	v := &Live{}
	v.p.Store(l)
	return v
}

// Current 返回当前快照。同一次处理内应只取一次，保证读到的各段来自同一份快照。
func (v *Live) Current() *Loader { return v.p.Load() }

// Swap 原子替换当前快照（nil 被忽略）。
func (v *Live) Swap(l *Loader) {
	if l != nil {
		v.p.Store(l)
	}
}

// ── 以下为 Loader 读方法的转发（让消费方继续只依赖接口）───────────────────────

func (v *Live) Rules(ctx context.Context) ([]contract.Rule, error) { return v.Current().Rules(ctx) }
func (v *Live) Snapshot(ctx context.Context) (contract.PolicySnapshot, error) {
	return v.Current().Snapshot(ctx)
}
func (v *Live) Thresholds(ctx context.Context) (contract.Thresholds, error) {
	return v.Current().Thresholds(ctx)
}
func (v *Live) GrayPct(ctx context.Context) (uint8, error) { return v.Current().GrayPct(ctx) }
func (v *Live) Whitelist(ctx context.Context) (contract.Whitelist, error) {
	return v.Current().Whitelist(ctx)
}
func (v *Live) Decoys(ctx context.Context) ([]contract.DecoyAsset, error) {
	return v.Current().Decoys(ctx)
}
func (v *Live) Honeypots(ctx context.Context) ([]contract.HoneypotBackend, error) {
	return v.Current().Honeypots(ctx)
}
func (v *Live) Injects(ctx context.Context) ([]contract.InjectRule, bool, error) {
	return v.Current().Injects(ctx)
}
func (v *Live) Blacklist(ctx context.Context) ([]contract.NoDeceptionRule, error) {
	return v.Current().Blacklist(ctx)
}
func (v *Live) AI(ctx context.Context) (contract.AIConfig, error) { return v.Current().AI(ctx) }

var _ Policy = (*Live)(nil)
