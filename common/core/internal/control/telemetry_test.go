package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	telemetryv1 "shen/common/api/telemetry/v1"
	"shen/common/core/internal/contract"
	"shen/common/core/internal/telemetry"
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

// ── 只读快照（控制台「配置」页）─────────────────────────────────────────────

// stubSnapshot 是 SnapshotProvider 的测试替身。
type stubSnapshot struct {
	snap contract.CoreSnapshot
	err  error
}

func (s stubSnapshot) CoreSnapshot(context.Context) (contract.CoreSnapshot, error) {
	return s.snap, s.err
}

// 未装配快照提供方 ⇒ Unimplemented，**不得**回全零快照。
//
// 这条测试守的是一条设计决定：全零快照会被页面读成「策略版本 0 / 变体 0」，
// 也就是一份**看起来正常的错数据** —— 那比报错更危险（AR-15 的精神）。
func TestTelemetryService_SnapshotWithoutProviderIsUnimplemented(t *testing.T) {
	svc := NewTelemetryServiceWith(newStubCollector())
	got, err := svc.GetCoreSnapshot(context.Background(), &telemetryv1.GetCoreSnapshotRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("未装配时应返回 Unimplemented，实际 err=%v", err)
	}
	if got != nil {
		t.Fatalf("Unimplemented 时不得回快照，实际 %+v", got)
	}
}

// 装配后逐字段映射（含「清单未装载」时的默认值）。
func TestTelemetryService_SnapshotMapsFields(t *testing.T) {
	want := contract.CoreSnapshot{
		Policy: contract.PolicyState{
			PolicyID: "site-a", Version: 7, Checksum: "abc123", RuleCount: 3, WhitelistCount: 4,
		},
		AI: contract.AIState{
			Enabled: true, Kinds: []string{"content"}, Model: "deepseek-flash",
			ManifestPath: "/etc/shen/manifest.json", Variants: 8, RotateCooldown: "30m0s",
			ManifestLoaded: true, ManifestVersion: 2, ManifestResources: 5, ManifestContents: 16,
		},
	}
	svc := NewTelemetryServiceWith(newStubCollector(), WithSnapshotProvider(stubSnapshot{snap: want}))
	got, err := svc.GetCoreSnapshot(context.Background(), &telemetryv1.GetCoreSnapshotRequest{})
	if err != nil {
		t.Fatalf("取快照失败：%v", err)
	}
	if got.GetPolicyId() != "site-a" || got.GetPolicyVersion() != 7 || got.GetRuleCount() != 3 ||
		got.GetWhitelistCount() != 4 {
		t.Errorf("策略面字段映射错了：%+v", got)
	}
	if !got.GetAiEnabled() || got.GetAiModel() != "deepseek-flash" || got.GetAiContentVariants() != 8 ||
		got.GetAiRotateCooldown() != "30m0s" || !got.GetAiManifestLoaded() ||
		got.GetAiManifestVersion() != 2 || got.GetAiManifestContents() != 16 {
		t.Errorf("AI 面字段映射错了：%+v", got)
	}
}

// 提供方报错 ⇒ Internal（不吞成成功、不回半份数据）。
func TestTelemetryService_SnapshotProviderErrorIsInternal(t *testing.T) {
	svc := NewTelemetryServiceWith(newStubCollector(),
		WithSnapshotProvider(stubSnapshot{err: errStubSnapshot}))
	if _, err := svc.GetCoreSnapshot(context.Background(), &telemetryv1.GetCoreSnapshotRequest{}); status.Code(err) != codes.Internal {
		t.Fatalf("提供方报错时应返回 Internal，实际 %v", err)
	}
}

var errStubSnapshot = errors.New("stub: 读不到策略")
