package telemetry

import (
	"context"

	"shen/core/internal/contract"
)

// Collector 归一化并幂等上报事件。
//
// 不依赖系统时钟 —— Event.CreatedAt 由调用方注入。
type Collector struct {
	sink Sink
}

// New 构造采集器。sink 为 nil 时 panic。
func New(sink Sink) *Collector {
	if sink == nil {
		panic("telemetry: Sink 不能为 nil")
	}
	return &Collector{sink: sink}
}

// Report 上报单个事件。
func (c *Collector) Report(ctx context.Context, ev contract.Event) (Result, error) {
	return c.ReportBatch(ctx, []contract.Event{ev})
}

// ReportBatch 批量上报。
//
// 无 EventID 的事件被丢弃 —— 没有幂等键就无法保证去重，宁可丢也不写脏数据。
func (c *Collector) ReportBatch(ctx context.Context, evs []contract.Event) (Result, error) {
	var res Result
	for _, ev := range evs {
		if ev.EventID == "" {
			continue
		}
		written, err := c.sink.Write(ctx, ev)
		if err != nil {
			return res, err
		}
		if written {
			res.Accepted++
		} else {
			res.Duplicated++
		}
	}
	return res, nil
}

var _ Telemetry = (*Collector)(nil)
