package telemetry

import (
	"context"

	"shen/common/core/internal/contract"
)

// Collector 归一化并幂等上报事件。
//
// 不依赖系统时钟 —— Event.CreatedAt 由调用方注入。
type Collector struct {
	sink Sink
	// pub 可空：为空时没有观测面订阅者，推送整段跳过（零成本）。
	pub Publisher
}

// New 构造采集器。sink 为 nil 时 panic；pub 可为 nil（无推送）。
func New(sink Sink, pub Publisher) *Collector {
	if sink == nil {
		panic("telemetry: Sink 不能为 nil")
	}
	return &Collector{sink: sink, pub: pub}
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
	var written []contract.Event
	for _, ev := range evs {
		if ev.EventID == "" {
			continue
		}
		ok, err := c.sink.Write(ctx, ev)
		if err != nil {
			return res, err
		}
		if ok {
			res.Accepted++
			written = append(written, ev)
		} else {
			res.Duplicated++
		}
	}
	// 推送是**旁路**（`ADR-0027` 语义①）：只推**真正落库**的事件（幂等命中不重复推），
	// 且在返回**之前**完成 —— Hub.Publish 不阻塞、不返回错误，所以这里夺不走上报路径的时间。
	if c.pub != nil {
		c.pub.Publish(written)
	}
	return res, nil
}

var _ Telemetry = (*Collector)(nil)
