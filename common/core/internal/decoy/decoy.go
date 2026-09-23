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

// Match 按**归一化路径上的路径段边界**做最长匹配（方案 §9.2）。
//
// 两条硬要求（都是踩过的边界）：
//   - **归一化**：请求路径先 `path.Clean`（与规则侧的 `path_norm` 同一口径）——
//     否则 `/static/../.git/config` 这类「加一层无害前缀」会绕开诱饵路由；
//   - **路径段边界**：`/.git` 命中 `/.git` 与 `/.git/config`，**不**命中 `/.gitignore`；
//     `/admin` 不命中 `/administrator`（纯字符串前缀会把它们混在一起）。
//
// 嵌套资产按最长匹配优先（`/.git` 与 `/.git/config` 同时登记时后者胜出）。
func (e *Engine) Match(ctx context.Context, path string) (contract.DecoyAsset, bool, error) {
	assets, err := e.Enabled(ctx)
	if err != nil {
		return contract.DecoyAsset{}, false, err
	}
	// 只接受**路径**：查询串是另一件事（与规则侧的 `path` / `query` 分家同一口径）。
	// 混在一起时**报错**而不是自行截断 —— 自行截断就是「静默解释匹配歧义」（方案 §9.2 禁止）。
	if strings.Contains(path, "?") {
		return contract.DecoyAsset{}, false, fmt.Errorf(
			"decoy: Match 只接受路径（%q 含查询串）—— 请传 path，查询串走 query 字段", path)
	}
	req := contract.NormalizePath(path)
	if req == "" {
		return contract.DecoyAsset{}, false, nil
	}
	var best contract.DecoyAsset
	bestLen := -1
	for _, a := range assets {
		ap := contract.NormalizePath(a.Path)
		if ap == "" || !contract.PathSegmentPrefix(req, ap) {
			continue
		}
		if len(ap) > bestLen {
			best, bestLen = a, len(ap)
		}
	}
	return best, bestLen >= 0, nil
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
