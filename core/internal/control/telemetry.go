package control

import (
	"context"

	telemetryv1 "shen/api/telemetry/v1"
	"shen/core/internal/contract"
	"shen/core/internal/telemetry"
)

// TelemetryService 实现 DeceptionTelemetry 服务（接缝 S1 的遥测面）。
//
// 它的职责只是把 proto 转成内部类型，再做幂等写入 —— 去重逻辑在 telemetry 模块里。
type TelemetryService struct {
	telemetryv1.UnimplementedDeceptionTelemetryServer
	collector telemetry.Telemetry
}

// NewTelemetryService 构造服务端。collector 为 nil 时 panic。
func NewTelemetryService(c telemetry.Telemetry) *TelemetryService {
	if c == nil {
		panic("control: telemetry.Telemetry 不能为 nil")
	}
	return &TelemetryService{collector: c}
}

// Report 接收单个事件。
//
// 返回的 accepted / duplicated 供适配器对账：duplicated 说明这个 event_id
// 之前已经写过（重试或重放），不是错误。
func (s *TelemetryService) Report(ctx context.Context, in *telemetryv1.TelemetryEvent) (*telemetryv1.ReportAck, error) {
	res, err := s.collector.Report(ctx, fromProtoEvent(in))
	if err != nil {
		return nil, err
	}
	return toProtoAck(res), nil
}

// ReportBatch 接收一批事件。
func (s *TelemetryService) ReportBatch(ctx context.Context, in *telemetryv1.TelemetryBatch) (*telemetryv1.ReportAck, error) {
	events := make([]contract.Event, 0, len(in.GetEvents()))
	for _, e := range in.GetEvents() {
		events = append(events, fromProtoEvent(e))
	}
	res, err := s.collector.ReportBatch(ctx, events)
	if err != nil {
		return nil, err
	}
	return toProtoAck(res), nil
}

func fromProtoEvent(in *telemetryv1.TelemetryEvent) contract.Event {
	ev := contract.Event{
		EventID:   in.GetEventId(),
		Type:      in.GetEventType(),
		ActorID:   in.GetActorId(),
		SessionID: in.GetSessionId(),
		Payload:   in.GetPayload(),
	}
	// 时间由调用方注入：缺失时留零值，由 telemetry 侧按未识别处理。
	if ts := in.GetCreatedAt(); ts != nil {
		ev.CreatedAt = ts.AsTime()
	}
	return ev
}

func toProtoAck(r telemetry.Result) *telemetryv1.ReportAck {
	return &telemetryv1.ReportAck{Accepted: r.Accepted, Duplicated: r.Duplicated}
}
