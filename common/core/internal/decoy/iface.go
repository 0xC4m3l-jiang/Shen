// Package decoy 管理**诱饵面**：诱饵资产的定义、匹配、多态与投放片段。
//
// 依据 ADR-0010：欺骗的核心机制是**功能性伪装** —— 诱饵必须呈现为站点的合法功能
// （开发者 API / 指令文件 / MCP 工具 / 数据集 / 三类蜜饵）。
//
// 边界（不做的事）：
//   - 不做判定（AR-2）· 不做决策（MD-12）· 不生成内容（那是 responder）· 不注入（那是 edge-injection）；
//   - **诱饵面必须 observe-only**（MD-25）—— 集中常量在决策执行处统一豁免阻断。
package decoy

import (
	"context"

	"shen/common/core/internal/contract"
)

// VariantCount 是诱饵变体数（多态，ADR-0016）。
//
// 多态**只在会话边界选择**、会话内冻结 —— 因此它与 AR-30（同会话同资源同答案）同时成立。
const VariantCount uint8 = 4

// AssetStore 是本模块对存储的依赖。
//
// 接口由消费方（本模块）定义，故 store 不 import decoy —— 依赖方向单向。
// 生产实现是 store.DecoyStore。
type AssetStore interface {
	List(ctx context.Context) ([]contract.DecoyAsset, error)
	Get(ctx context.Context, id string) (contract.DecoyAsset, bool, error)
}

// Surface 是本模块对外的唯一契约。
type Surface interface {
	// Enabled 返回启用中的诱饵资产。
	Enabled(ctx context.Context) ([]contract.DecoyAsset, error)

	// Match 按请求路径找命中的诱饵（**最长前缀**匹配）。
	//
	// 未命中返回 ok=false —— 调用方按「不是诱饵路径」处理。
	Match(ctx context.Context, path string) (contract.DecoyAsset, bool, error)

	// Variant 返回该会话的诱饵变体号（0..VariantCount-1）。
	//
	// 确定性（同会话恒同值）—— 这是多态与 AR-30 共存的关键（ADR-0016）。
	Variant(ctx context.Context, sessionID string) (uint8, error)

	// Placements 返回该资产的**投放片段**（可直接粘贴到 JS / nginx / SQL）。
	//
	// 「投放才是最后一公里」：没有可直接使用的片段，运营不会用，诱饵面就停在演示状态。
	Placements(ctx context.Context, assetID string) ([]string, error)
}
