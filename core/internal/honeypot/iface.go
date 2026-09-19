// Package honeypot 是蜜罐的**入口与后端池**：类型注册、config 开关、后端池解析。
//
// MD-26：蜜罐**必须**经本模块管理 —— 具体蜜罐实现**禁止**编译进核心，
// **必须**是可替换后端；蜜罐类型的启用**必须**由配置决定（ST-24）。
// 本条由结构保证：本包不 import 任何蜜罐实现，只持有 `contract.HoneypotBackend` 数据。
//
// 它**不实现任何具体蜜罐**（ADR-0011）—— 具体实现是第三方或可选的 honeypot-protocol，
// 经本模块登记与解析。后端池是**数据**（可配置），不是编译进代码的分支（ST-24）。
//
// 它**不主动连接任何目标**（SB-6）：只承接 director 的 route_mirage 送来的流量。
package honeypot

import (
	"context"

	"shen/core/internal/contract"
)

// KnownTypes 是设计登记过的蜜罐类型（见 docs/modules/honeypot.md §1）。
//
// 新增类型 = 在这里加一项 + 提供一个满足契约的后端，**不改代码逻辑**。
var KnownTypes = []string{
	"ssh", "mysql", "redis", "ftp", "elasticsearch",
	"nginx-admin", "web-clone", "internal-wiki", "database",
}

// Pool 是本模块对外的唯一契约。
type Pool interface {
	// Resolve 把 route_mirage 的逻辑后端名解析到具体后端。
	//
	// 未登记、未启用或不健康时返回 ok=false —— 调用方据此回落业务（NI-5）。
	Resolve(ctx context.Context, name string) (contract.HoneypotBackend, bool, error)

	// List 返回全部后端（供 console 管理与健康面板）。
	List(ctx context.Context) ([]contract.HoneypotBackend, error)

	// Register 登记或更新一个后端（管理面；console 用）。
	Register(ctx context.Context, b contract.HoneypotBackend) error

	// SetEnabled 打开 / 关闭某个后端（config 开关的运行期等价操作）。
	SetEnabled(ctx context.Context, name string, enabled bool) error

	// SetHealthy 更新健康状态（健康检查用）。
	SetHealthy(ctx context.Context, name string, healthy bool) error

	// Types 返回本池中出现过的类型（去重后按 KnownTypes 顺序）。
	Types(ctx context.Context) ([]string, error)
}
