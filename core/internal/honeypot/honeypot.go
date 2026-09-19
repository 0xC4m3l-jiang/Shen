package honeypot

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"shen/core/internal/contract"
)

// Engine 是 Pool 的实现。
//
// 它持有后端池（数据），多副本各自持有一份相同配置即可 —— 池定义**不是**业务状态，
// 而是配置的投影，因此不违反 AR-9（无状态多副本）。
type Engine struct {
	mu       sync.RWMutex
	backends map[string]contract.HoneypotBackend
	// known 是已登记类型，**按给定顺序**保存（最多十来项，成员判断用线性查找即可）。
	// 保存顺序而不是排序，使 Types() 的输出天然稳定而不需要再排一次。
	known []string
}

// New 从配置构造后端池。
//
// 校验（任一条不满足即返回错误，禁止带着坏配置运行）：
//   - Name 非空且唯一；
//   - Type 必须属于 knownTypes（默认 KnownTypes）；
//   - Addr 非空。
//
// 默认全部 off：蜜罐是幻境后端，只有显式启用才是「在池里」（INT-11：首次上线影子模式）。
func New(backends []contract.HoneypotBackend, knownTypes []string) (*Engine, error) {
	if len(knownTypes) == 0 {
		knownTypes = KnownTypes
	}
	known := make([]string, 0, len(knownTypes))
	for _, t := range knownTypes {
		if strings.TrimSpace(t) == "" {
			return nil, fmt.Errorf("honeypot: 已知类型不能为空")
		}
		if !slices.Contains(known, t) {
			known = append(known, t)
		}
	}

	e := &Engine{
		backends: make(map[string]contract.HoneypotBackend, len(backends)),
		known:    known,
	}
	seen := make(map[string]bool, len(backends))
	for i, b := range backends {
		if err := e.validate(b); err != nil {
			return nil, fmt.Errorf("honeypot: backends[%d]：%w", i, err)
		}
		if seen[b.Name] {
			return nil, fmt.Errorf("honeypot: 后端名 %q 重复 —— 逻辑名必须唯一", b.Name)
		}
		seen[b.Name] = true
		e.backends[b.Name] = b
	}
	return e, nil
}

func (e *Engine) validate(b contract.HoneypotBackend) error {
	if strings.TrimSpace(b.Name) == "" {
		return fmt.Errorf("name 不能为空")
	}
	if !slices.Contains(e.known, b.Type) {
		return fmt.Errorf("type=%q 未登记（已知类型：%s）", b.Type, strings.Join(e.known, " / "))
	}
	if strings.TrimSpace(b.Addr) == "" {
		return fmt.Errorf("addr 不能为空")
	}
	return nil
}

// Resolve 解析逻辑后端名。
//
// 只有**启用且健康**的后端才会被解析成功 —— 否则调用方回落业务（NI-5），
// 宁可漏引流，不可把流量送进不可用的幻境。
func (e *Engine) Resolve(_ context.Context, name string) (contract.HoneypotBackend, bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	b, ok := e.backends[name]
	if !ok || !b.Enabled || !b.Healthy {
		return contract.HoneypotBackend{}, false, nil
	}
	return b, true, nil
}

// List 返回后端池副本（按名字排序，保证输出稳定）。
func (e *Engine) List(_ context.Context) ([]contract.HoneypotBackend, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]contract.HoneypotBackend, 0, len(e.backends))
	for _, b := range e.backends {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Register 登记或更新一个后端（管理面）。
func (e *Engine) Register(_ context.Context, b contract.HoneypotBackend) error {
	if err := e.validate(b); err != nil {
		return fmt.Errorf("honeypot: 登记失败：%w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.backends[b.Name] = b
	return nil
}

// SetEnabled 打开 / 关闭某个后端；名字不存在时报错。
func (e *Engine) SetEnabled(_ context.Context, name string, enabled bool) error {
	return e.mutate(name, func(b *contract.HoneypotBackend) { b.Enabled = enabled })
}

// SetHealthy 更新健康状态；名字不存在时报错。
func (e *Engine) SetHealthy(_ context.Context, name string, healthy bool) error {
	return e.mutate(name, func(b *contract.HoneypotBackend) { b.Healthy = healthy })
}

func (e *Engine) mutate(name string, f func(*contract.HoneypotBackend)) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, ok := e.backends[name]
	if !ok {
		return fmt.Errorf("honeypot: 后端 %q 不存在", name)
	}
	f(&b)
	e.backends[name] = b
	return nil
}

// Types 返回池中出现过的类型，按登记顺序去重。
func (e *Engine) Types(_ context.Context) ([]string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	present := make(map[string]bool, len(e.backends))
	for _, b := range e.backends {
		present[b.Type] = true
	}
	out := make([]string, 0, len(present))
	for _, t := range e.known {
		if present[t] {
			out = append(out, t)
		}
	}
	return out, nil
}

var _ Pool = (*Engine)(nil)
