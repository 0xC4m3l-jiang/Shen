package control

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	telemetryv1 "shen/api/telemetry/v1"
	"shen/core/internal/contract"
	"shen/core/internal/telemetry"
)

// stubCollector 是 telemetry.Telemetry 的测试替身 —— 不依赖 store 的真实实现。
type stubCollector struct {
	got []contract.Event
	dup map[string]bool
}

func newStubCollector() *stubCollector { return &stubCollector{dup: map[string]bool{}} }

func (s *stubCollector) Report(ctx context.Context, ev contract.Event) (telemetry.Result, error) {
	return s.ReportBatch(ctx, []contract.Event{ev})
}

func (s *stubCollector) ReportBatch(_ context.Context, evs []contract.Event) (telemetry.Result, error) {
	var r telemetry.Result
	for _, ev := range evs {
		if ev.EventID == "" {
			continue
		}
		if s.dup[ev.EventID] {
			r.Duplicated++
			continue
		}
		s.dup[ev.EventID] = true
		s.got = append(s.got, ev)
		r.Accepted++
	}
	return r, nil
}

var _ telemetry.Telemetry = (*stubCollector)(nil)

// TestTelemetryService_ReportIdempotent：同一个 event_id 上报两次，第二次只算重复。
func TestTelemetryService_ReportIdempotent(t *testing.T) {
	svc := NewTelemetryService(newStubCollector())
	ctx := context.Background()

	in := &telemetryv1.TelemetryEvent{
		EventId:   "e-1",
		EventType: "request_observed",
		SessionId: "s-1",
		Payload:   []byte("{}"),
		CreatedAt: timestamppb.New(time.Unix(0, 0)),
	}

	first, err := svc.Report(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if first.GetAccepted() != 1 || first.GetDuplicated() != 0 {
		t.Fatalf("首次上报期望 accepted=1，得到 %+v", first)
	}

	again, err := svc.Report(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if again.GetAccepted() != 0 || again.GetDuplicated() != 1 {
		t.Fatalf("重复上报期望 duplicated=1，得到 %+v", again)
	}
}

// TestTelemetryService_ReportBatch：批量上报只计新增。
func TestTelemetryService_ReportBatch(t *testing.T) {
	svc := NewTelemetryService(newStubCollector())

	res, err := svc.ReportBatch(context.Background(), &telemetryv1.TelemetryBatch{
		Events: []*telemetryv1.TelemetryEvent{{EventId: "a"}, {EventId: "b"}, {EventId: "a"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.GetAccepted() != 2 || res.GetDuplicated() != 1 {
		t.Fatalf("期望 accepted=2 / duplicated=1，得到 %+v", res)
	}
}

// TestFromProtoEvent_KeepsInjectedTime：时间由调用方注入，不得被改写。
func TestFromProtoEvent_KeepsInjectedTime(t *testing.T) {
	ts := time.Unix(1700000000, 0)
	got := fromProtoEvent(&telemetryv1.TelemetryEvent{EventId: "e", CreatedAt: timestamppb.New(ts)})
	if !got.CreatedAt.Equal(ts) {
		t.Fatalf("时间应原样保留，得到 %v", got.CreatedAt)
	}
}

// TestFromProtoEvent_MissingTimeIsZero：proto 未带时间时留零值，不取系统时钟。
func TestFromProtoEvent_MissingTimeIsZero(t *testing.T) {
	got := fromProtoEvent(&telemetryv1.TelemetryEvent{EventId: "e"})
	if !got.CreatedAt.IsZero() {
		t.Fatalf("缺失时间时应留零值，不得取系统时钟，得到 %v", got.CreatedAt)
	}
}
