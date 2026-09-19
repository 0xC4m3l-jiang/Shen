// Package judge 把观测变成判定。
//
// 它是判定逻辑的唯一实现处 —— 边缘层与适配器都禁止实现判定。
// 本模块不发起外呼、不写业务存储、不依赖系统时钟（时间由调用方注入）。
package judge

import (
	"context"

	"shen/core/internal/contract"
)

// RuleSource 提供判定规则。
//
// 规则是数据，由 policy 模块下发；本接口由消费方定义，
// 因此 judge 不 import policy —— 依赖方向单向。
type RuleSource interface {
	Rules(ctx context.Context) ([]contract.Rule, error)
}

// Judge 是本模块对外的唯一契约。
//
// 依赖方只依赖本接口，不依赖 Engine 具体类型（本项目强制约定）。
type Judge interface {
	Judge(ctx context.Context, req contract.JudgeRequest) (contract.Verdict, error)
}
