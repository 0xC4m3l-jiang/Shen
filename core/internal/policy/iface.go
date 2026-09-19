// Package policy 装载策略文档，把它变成版本化的规则与灰度比例。
//
// 它是「策略必须是数据、禁止编译进代码」（ST-24）的实现处：
// 规则不是 Go 分支，而是从配置文件读入、校验后构造的不可变快照。
//
// 本模块不做判定（AR-2：判定只在 judge 实现一次）、不做决策（那是 director）、
// 不计算灰度（灰度是逐请求的函数）、不实现下发面（阶段 2 未实现）。
// 它不发起外呼、不依赖系统时钟（MD-6），访问外部存储只经 store（MD-20）。
package policy

import (
	"context"

	"shen/core/internal/contract"
)

// Policy 是本模块对外的唯一契约。
//
// 依赖方只依赖本接口，不依赖 Loader 具体类型（本项目强制约定）。
type Policy interface {
	// Rules 返回当前的规则集。它是 judge.RuleSource 的实现 ——
	// 接口由消费方定义，所以本模块不 import judge（依赖方向单向）。
	Rules(ctx context.Context) ([]contract.Rule, error)

	// Snapshot 返回当前策略快照（版本 + 校验和 + 载荷）。
	// 供未来的下发面消费；本期无跨进程消费者。
	Snapshot(ctx context.Context) (contract.PolicySnapshot, error)
}
