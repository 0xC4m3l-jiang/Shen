// Package telemetry 做事件归一化与幂等批量上报。
//
// 同一 event_id 重复上报不得产生重复记录。
package telemetry

import (
	"context"

	"shen/core/internal/contract"
)

// Sink 是本模块需要的存储能力 —— 由 store 模块实现。
//
// 依赖方定义接口是本项目的强制约定；因此 telemetry 不 import store。
type Sink interface {
	// Write 幂等写入：同 EventID 已存在时不重复写入并返回 false。
	Write(ctx context.Context, ev contract.Event) (written bool, err error)
}

// Telemetry 是本模块对外的唯一契约。
type Telemetry interface {
	Report(ctx context.Context, ev contract.Event) (Result, error)
	ReportBatch(ctx context.Context, evs []contract.Event) (Result, error)
}

// Publisher 把**已写入**的事件广播出去（观测面推送，`ADR-0027`）。
//
// 依赖方定义接口：telemetry 不 import 具体的广播实现（依赖方向单向）。
// `*Hub` 满足它。
type Publisher interface {
	Publish(evs []contract.Event)
}

// Result 是上报回执，供适配器对账。
type Result struct {
	Accepted   uint32 // 实际写入
	Duplicated uint32 // 幂等命中（已存在）
}
