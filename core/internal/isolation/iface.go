// Package isolation 记录与查询隔离名单：命中的来源在后续请求里不再调用核心。
//
// 它**不产生可见处置** —— 命中隔离只是让后续请求不再问核心（`route_origin` 行为），
// 客户端不可见。这与 `challenge`（挑战页）不同，见 ADR-0002。
//
// 状态**必须外置**（AR-9：核心无状态多副本），且读写**必须**经 store（MD-20）。
// 存储不可用时**必须** fail-open：回退调用核心判定，**禁止**阻断业务（NI-10）。
package isolation

import (
	"context"
	"time"

	"shen/core/internal/contract"
)

// Store 是本模块对存储的依赖。
//
// 接口由消费方（本模块）定义，故 store 不 import isolation —— 依赖方向单向。
// 生产实现是 store.IsolationStore。
type Store interface {
	Get(ctx context.Context, key contract.SessionKey) (contract.IsolationHit, error)
	Put(ctx context.Context, key contract.SessionKey, hit contract.IsolationHit) error
}

// Isolation 是本模块对外的唯一契约。
type Isolation interface {
	// Check 查询隔离状态。命中时调用方**不再**走判定与决策。
	//
	// 返回错误表示存储不可用 —— 调用方**必须** fail-open（回退调用核心判定，NI-10）。
	Check(ctx context.Context, key contract.SessionKey) (contract.IsolationHit, error)

	// Isolate 写入隔离记录，到期自动解除。
	// ttl 必须为正；ttl <= 0 时返回错误（禁止写入永不过期的隔离）。
	Isolate(ctx context.Context, key contract.SessionKey, reason string, ttl time.Duration) error
}
