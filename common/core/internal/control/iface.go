// Package control 是 gRPC 服务面（接缝 S1 的服务端：接收适配器的判定请求）。
//
// 它是「禁止回显判定信息」的强制点 —— 返回体不得含分值、规则名、决策枚举。
package control

import (
	"context"

	"shen/common/core/internal/contract"
)

// Decider 是服务面需要的决策能力。
//
// 阶段 1（影子模式：只算不处置）由 ShadowDecider 实现；阶段 2 由 director 模块实现。
// 接口由消费方定义，故 control 不 import director —— 依赖方向单向。
type Decider interface {
	Decide(ctx context.Context, req contract.JudgeRequest) (contract.Decision, error)
}
