// Package director 把 judge 的风险分变成决策：去哪儿（三值）+ 多重手（severity）。
//
// 它是「判定与决策分离」的决策侧：judge 只算分与信号，director 决定去向。
// 判定逻辑不在本模块（AR-2），本模块只消费 Verdict.Score。
//
// 只做三件事：取阈值 → 定三值 → 灰度收敛。不做熔断（在 control 服务面）、
// 不选后端池（阶段 3）、不读配置（阈值与灰度经接口注入）。
package director

import (
	"context"

	"shen/core/internal/contract"
)

// ThresholdSource 提供决策阈值。
//
// 接口由消费方（director）定义，故 policy 不 import director —— 依赖方向单向。
// 阶段 2a 由 policy.Loader 实现（阈值是配置数据，ST-23 / INT-24）。
type ThresholdSource interface {
	Thresholds(ctx context.Context) (contract.Thresholds, error)
}

// GraySource 提供灰度比例（0..100）：本会改道的请求里，允许真正改道的百分比。
//
// 0 = 全放行（影子模式的自然延续），100 = 全改道（INT-12 的放开阶梯顶端）。
type GraySource interface {
	GrayPct(ctx context.Context) (uint8, error)
}

// Director 是本模块对外的唯一契约。
//
// 它的 Decide 与 control.Decider 签名一致，由装配层（main）接到服务面。
type Director interface {
	Decide(ctx context.Context, req contract.JudgeRequest) (contract.Decision, error)
}
