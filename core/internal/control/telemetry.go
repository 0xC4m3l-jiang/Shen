package control

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

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
	lister    EventLister      // 可空；为空时 ListEvents 返回空（控制台会显示「暂无数据」）
	snapshot  SnapshotProvider // 可空；为空时 GetCoreSnapshot 返回 Unimplemented（不返回全零假快照）
}

// NewTelemetryService 构造服务端。collector 为 nil 时 panic。
func NewTelemetryService(c telemetry.Telemetry) *TelemetryService {
	if c == nil {
		panic("control: telemetry.Telemetry 不能为 nil")
	}
	return &TelemetryService{collector: c}
}

// TelemetryOption 是遥测面服务端的可选装配项。
type TelemetryOption func(*TelemetryService)

// WithEventLister 给遥测面挂上读侧（控制台看告警与流量访问）。
func WithEventLister(l EventLister) TelemetryOption {
	return func(s *TelemetryService) { s.lister = l }
}

// WithSnapshotProvider 给遥测面挂上**只读快照**（控制台看「现在按什么在跑」）。
func WithSnapshotProvider(p SnapshotProvider) TelemetryOption {
	return func(s *TelemetryService) { s.snapshot = p }
}

// NewTelemetryServiceWith 构造带可选装配项的遥测面服务端。
func NewTelemetryServiceWith(c telemetry.Telemetry, opts ...TelemetryOption) *TelemetryService {
	s := NewTelemetryService(c)
	for _, o := range opts {
		o(s)
	}
	return s
}

// ListEvents 返回最近事件（观测面读侧）。
// 未装配读侧时返回空列表而不是错误：控制台要能显示「暂无数据」，
// 而不是因为核心没接存储就整页报错。
func (s *TelemetryService) ListEvents(ctx context.Context, in *telemetryv1.ListEventsRequest) (*telemetryv1.ListEventsResponse, error) {
	if s.lister == nil {
		return &telemetryv1.ListEventsResponse{}, nil
	}
	var since time.Time
	if in.GetSince() != nil {
		since = in.GetSince().AsTime()
	}
	events, err := s.lister.ListEvents(ctx, int(in.GetLimit()), since, in.GetEventType())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "control: 读事件失败：%v", err)
	}
	out := make([]*telemetryv1.TelemetryEvent, 0, len(events))
	for _, ev := range events {
		out = append(out, toProtoEvent(ev))
	}
	return &telemetryv1.ListEventsResponse{Events: out}, nil
}

// GetCoreSnapshot 返回核心当前生效状态的只读摘要（控制台「配置」页）。
//
// 未装配快照提供方时返回 `Unimplemented` 而**不是**全零快照：
// 全零会被页面读成「策略版本 0 / 变体 0」，那是**假的**——
// 宁可让控制台显示「核心未提供」，也不要给它一份看起来正常的错数据（`AR-15` 的精神）。
func (s *TelemetryService) GetCoreSnapshot(
	ctx context.Context, _ *telemetryv1.GetCoreSnapshotRequest,
) (*telemetryv1.CoreSnapshot, error) {
	if s.snapshot == nil {
		return nil, status.Error(codes.Unimplemented, "control: 核心未装配快照读侧")
	}
	snap, err := s.snapshot.CoreSnapshot(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "control: 取快照失败：%v", err)
	}
	return toProtoSnapshot(snap), nil
}

// toProtoSnapshot 把内部快照映射成 proto（字段一一对应，不做加工）。
func toProtoSnapshot(s contract.CoreSnapshot) *telemetryv1.CoreSnapshot {
	return &telemetryv1.CoreSnapshot{
		PolicyId:            s.Policy.PolicyID,
		PolicyVersion:       s.Policy.Version,
		PolicyChecksum:      s.Policy.Checksum,
		RuleCount:           int32(s.Policy.RuleCount),
		WhitelistCount:      int32(s.Policy.WhitelistCount),
		AiEnabled:           s.AI.Enabled,
		AiKinds:             s.AI.Kinds,
		AiModel:             s.AI.Model,
		AiManifestPath:      s.AI.ManifestPath,
		AiContentVariants:   int32(s.AI.Variants),
		AiRotateCooldown:    s.AI.RotateCooldown,
		AiManifestLoaded:    s.AI.ManifestLoaded,
		AiManifestVersion:   s.AI.ManifestVersion,
		AiManifestResources: int32(s.AI.ManifestResources),
		AiManifestContents:  int32(s.AI.ManifestContents),
	}
}

// toProtoEvent 把内部事件映射回 proto。
func toProtoEvent(ev contract.Event) *telemetryv1.TelemetryEvent {
	return &telemetryv1.TelemetryEvent{
		EventId:   ev.EventID,
		EventType: ev.Type,
		ActorId:   ev.ActorID,
		SessionId: ev.SessionID,
		Payload:   ev.Payload,
		CreatedAt: timestamppb.New(ev.CreatedAt),
	}
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
