package control

import (
	"context"
	"sort"
	"time"

	"google.golang.org/grpc"
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
	hub       *telemetry.Hub   // 可空；为空时 WatchEvents 返回 Unimplemented（不挂住订阅方）
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

// WithEventHub 给遥测面挂上**事件推送**（观测面订阅，`ADR-0027`）。
func WithEventHub(h *telemetry.Hub) TelemetryOption {
	return func(s *TelemetryService) { s.hub = h }
}

// 订阅的默认参数（**不是**契约值：契约里 0 = 服务端默认）。
const (
	defaultCatchUpLimit = 500
	maxCatchUpLimit     = 5000
	// watchStatusEvery 是状态帧的检查周期；**只在有变化时**才真发（见 WatchEvents）。
	watchStatusEvery = 2 * time.Second
)

// WatchEvents 把新事件推给订阅者（观测面推送，`ADR-0027`）。
//
// 两个容易被写错的点：
//
//	① **先订阅、再补漏**：反过来的话，补漏那段时间新产生的事件会**永久丢失**（读历史读不到未来的）。
//	   代价是「历史与缓冲可能各含同一条」⇒ 用 `seen` 去重（只可能重一次）。
//	② **断开不是错误**：订阅方关连接 ⇒ 返回 nil（正常结束），不要报错。
func (s *TelemetryService) WatchEvents(
	in *telemetryv1.WatchEventsRequest, stream grpc.ServerStreamingServer[telemetryv1.WatchEvent],
) error {
	if s.hub == nil {
		// 未装配推送 ⇒ 显式 Unimplemented（让订阅方**立刻**知道这条路没通），不挂住调用方
		// —— 与 `policy.Watch` 同一约定（ADR-0018）。
		return status.Error(codes.Unimplemented, "control: 核心未装配事件推送（观测面订阅）")
	}

	types := in.GetEventTypes()

	// ① 先订阅（开始缓冲），再补漏。
	sub := s.hub.Subscribe(0)
	defer sub.Close()
	seen := map[string]struct{}{}
	if since := in.GetSince(); since != nil {
		if err := s.catchUp(stream, types, seen, since.AsTime(), int(in.GetCatchUpLimit())); err != nil {
			return err
		}
	}

	// ② 转流：事件 + （有变化时的）状态帧。
	ticker := time.NewTicker(watchStatusEvery)
	defer ticker.Stop()
	var sentDropped uint64
	sentBuffered := -1
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case ev, ok := <-sub.Events():
			if !ok {
				return nil
			}
			if _, dup := seen[ev.EventID]; dup {
				delete(seen, ev.EventID) // 只可能与补漏重叠一次，删掉以免集合无界增长
				continue
			}
			if !wantedType(types, ev.Type) {
				continue
			}
			if err := stream.Send(&telemetryv1.WatchEvent{
				Body: &telemetryv1.WatchEvent_Event{Event: toProtoEvent(ev)},
			}); err != nil {
				return err
			}
		case <-ticker.C:
			dropped, buffered := sub.Dropped(), sub.Buffered()
			if dropped == sentDropped && buffered == sentBuffered {
				continue // 没变化就不发：状态帧是告警信号，不是心跳
			}
			sentDropped, sentBuffered = dropped, buffered
			if err := stream.Send(&telemetryv1.WatchEvent{Body: &telemetryv1.WatchEvent_Status{
				Status: &telemetryv1.WatchStatus{
					Dropped:    dropped,
					BufferSize: uint32(buffered),
					Capacity:   uint32(sub.Capacity()),
				},
			}}); err != nil {
				return err
			}
		}
	}
}

// catchUp 把 `since` 之后的历史补发出去（按**时间正序**，客户端按序插入才不会乱序）。
//
// 记录已发过的 id 到 `seen`：补漏与缓冲可能重叠（见 WatchEvents 的第一个坑）。
func (s *TelemetryService) catchUp(
	stream grpc.ServerStreamingServer[telemetryv1.WatchEvent],
	types []string, seen map[string]struct{}, since time.Time, askLimit int,
) error {
	if s.lister == nil {
		return nil // 没装读侧 ⇒ 补不了漏，但仍然转流（不因补漏失败而拒绝订阅）
	}
	limit := askLimit
	if limit <= 0 {
		limit = defaultCatchUpLimit
	}
	if limit > maxCatchUpLimit {
		limit = maxCatchUpLimit
	}
	evs, err := s.lister.ListEvents(stream.Context(), limit, since, "")
	if err != nil {
		return status.Errorf(codes.Internal, "control: 补漏读历史失败：%v", err)
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].CreatedAt.Before(evs[j].CreatedAt) })
	for _, ev := range evs {
		if !wantedType(types, ev.Type) {
			continue
		}
		seen[ev.EventID] = struct{}{}
		if err := stream.Send(&telemetryv1.WatchEvent{
			Body: &telemetryv1.WatchEvent_Event{Event: toProtoEvent(ev)},
		}); err != nil {
			return err
		}
	}
	return nil
}

// wantedType 判断事件类型是否在订阅范围内；**空 = 全部**。
func wantedType(types []string, t string) bool {
	if len(types) == 0 {
		return true
	}
	for _, want := range types {
		if want == t {
			return true
		}
	}
	return false
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
