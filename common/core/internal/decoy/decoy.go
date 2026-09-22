package decoy

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"shen/common/core/internal/contract"
)

// Engine 是 Surface 的实现。它无状态（AR-9）：资产在 store，变体是纯函数。
type Engine struct {
	store    AssetStore
	variants uint8
}

// New 构造诱饵面。store 为 nil 时返回错误；variants 为 0 时用 VariantCount。
func New(st AssetStore, variants uint8) (*Engine, error) {
	if st == nil {
		return nil, fmt.Errorf("decoy: AssetStore 不能为 nil")
	}
	if variants == 0 {
		variants = VariantCount
	}
	return &Engine{store: st, variants: variants}, nil
}

// Enabled 返回启用中的诱饵资产。
func (e *Engine) Enabled(ctx context.Context) ([]contract.DecoyAsset, error) {
	all, err := e.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("decoy: 读取诱饵资产失败：%w", err)
	}
	out := make([]contract.DecoyAsset, 0, len(all))
	for _, a := range all {
		if a.Enabled {
			out = append(out, a)
		}
	}
	return out, nil
}

// Match 按最长前缀匹配诱饵路径。
func (e *Engine) Match(ctx context.Context, path string) (contract.DecoyAsset, bool, error) {
	assets, err := e.Enabled(ctx)
	if err != nil {
		return contract.DecoyAsset{}, false, err
	}
	var best contract.DecoyAsset
	found := false
	for _, a := range assets {
		if a.Path == "" || !strings.HasPrefix(path, a.Path) {
			continue
		}
		if !found || len(a.Path) > len(best.Path) {
			best, found = a, true
		}
	}
	return best, found, nil
}

// Variant 由会话键派生（纯函数）：同会话恒同变体，不同会话可不同。
func (e *Engine) Variant(_ context.Context, sessionID string) (uint8, error) {
	if strings.TrimSpace(sessionID) == "" {
		return 0, fmt.Errorf("decoy: 会话键不能为空")
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(sessionID))
	return uint8(h.Sum32() % uint32(e.variants)), nil
}

// Intents 产出该资产的**投放意图**（数据：媒介 / 形态 / 目标 / 值 / 说明）。
//
// 它是投放的唯一领域出口：格式无关、可断言，供上层按媒介分流（`render.Placements` 则是它的渲染结果）。
func (e *Engine) Intents(ctx context.Context, assetID string) ([]PlacementIntent, error) {
	a, err := e.enabledAsset(ctx, assetID)
	if err != nil {
		return nil, err
	}
	return intentsFor(a), nil
}

// Placements 产出可直接粘贴的投放片段（= `Intents` 经媒介模板渲染的结果）。
//
// 保留它是因为它是运营实际要的东西（能直接粘到 nginx / 手册里的字节）；
// 需要按媒介自己处理时用 `Intents`。
func (e *Engine) Placements(ctx context.Context, assetID string) ([]string, error) {
	intents, err := e.Intents(ctx, assetID)
	if err != nil {
		return nil, err
	}
	return Render(intents), nil
}

// enabledAsset 取一个**启用中**的资产；不存在或未启用即报错（不静默产出空投放）。
func (e *Engine) enabledAsset(ctx context.Context, assetID string) (contract.DecoyAsset, error) {
	a, ok, err := e.store.Get(ctx, assetID)
	if err != nil {
		return contract.DecoyAsset{}, fmt.Errorf("decoy: 读取诱饵 %q 失败：%w", assetID, err)
	}
	if !ok {
		return contract.DecoyAsset{}, fmt.Errorf("decoy: 诱饵 %q 不存在", assetID)
	}
	if !a.Enabled {
		return contract.DecoyAsset{}, fmt.Errorf("decoy: 诱饵 %q 未启用", assetID)
	}
	return a, nil
}

var _ Surface = (*Engine)(nil)
