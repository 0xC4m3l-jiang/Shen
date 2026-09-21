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

// Placements 产出可直接粘贴的投放片段。
func (e *Engine) Placements(ctx context.Context, assetID string) ([]string, error) {
	a, ok, err := e.store.Get(ctx, assetID)
	if err != nil {
		return nil, fmt.Errorf("decoy: 读取诱饵 %q 失败：%w", assetID, err)
	}
	if !ok {
		return nil, fmt.Errorf("decoy: 诱饵 %q 不存在", assetID)
	}
	if !a.Enabled {
		return nil, fmt.Errorf("decoy: 诱饵 %q 未启用", assetID)
	}
	return placements(a), nil
}

// placements 按诱饵形态给出投放片段（模板随代码分发，ST-22）。
func placements(a contract.DecoyAsset) []string {
	switch a.Kind {
	case contract.DecoyDeveloperAPI:
		return []string{
			fmt.Sprintf("<!-- 页脚开发者 API（可见） -->\n<section id=\"dev-api\"><a href=\"%s\">Developer API</a></section>", a.Path),
			fmt.Sprintf("# nginx：把开发者 API 暴露给自动化客户端\nlocation %s { proxy_pass %s; }", a.Path, a.Content),
		}
	case contract.DecoyInstructionFile:
		return []string{
			fmt.Sprintf("# 投放位置：仓库根 / 文档目录（%s）", a.Path),
			fmt.Sprintf("echo '%s' > ./%s", a.Content, strings.TrimPrefix(a.Path, "/")),
		}
	case contract.DecoyMCP:
		return []string{
			fmt.Sprintf("// MCP 诱饵：把工具服务端点写进开发者文档\n{\"mcpServers\":{\"site-tools\":{\"url\":\"%s\"}}}", a.Path),
		}
	case contract.DecoyDataset:
		return []string{
			fmt.Sprintf("// 消耗战数据集：在 JS / 手册中暴露分页接口\nfetch('%s?page=1')", a.Path),
		}
	case contract.DecoyBait:
		return []string{
			// 片段会被投放进客户环境（可能被对手读到）—— 措辞必须中性（OH-1 / OH-2）。
			fmt.Sprintf("-- 投放位置：配置包 / 手册\nINSERT INTO app_accounts(login, secret) VALUES ('%s', '%s');", a.Path, a.Content),
			fmt.Sprintf("<a href=\"%s\" style=\"display:none\">下载</a>", a.Path),
		}
	default:
		return nil
	}
}

var _ Surface = (*Engine)(nil)
