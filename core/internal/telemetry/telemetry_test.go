package telemetry

import (
	"context"
	"testing"
	"time"

	"shen/core/internal/contract"
)

// stubSink 是 Sink 的测试替身 —— 单测不依赖 store 的真实实现。
type stubSink struct {
	seen map[string]bool
}

func newStubSink() *stubSink { return &stubSink{seen: map[string]bool{}} }

func (s *stubSink) Write(_ context.Context, ev contract.Event) (bool, error) {
	if s.seen[ev.EventID] {
		return false, nil
	}
	s.seen[ev.EventID] = true
	return true, nil
}

func ev(id string) contract.Event {
	return contract.Event{
		EventID:   id,
		Type:      "request_observed",
		CreatedAt: time.Unix(0, 0), // 由调用方注入，不取系统时钟
	}
}

// TestReport_Idempotent：同一 event_id 重复上报不产生重复记录。
func TestReport_Idempotent(t *testing.T) {
	c := New(newStubSink(), nil)

	first, err := c.Report(context.Background(), ev("e-1"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Accepted != 1 || first.Duplicated != 0 {
		t.Fatalf("首次上报期望 accepted=1，得到 %+v", first)
	}

	again, err := c.Report(context.Background(), ev("e-1"))
	if err != nil {
		t.Fatal(err)
	}
	if again.Accepted != 0 || again.Duplicated != 1 {
		t.Fatalf("重复上报期望 duplicated=1，得到 %+v", again)
	}
}

// TestReportBatch_DropsEventsWithoutID 断言无幂等键的事件被丢弃。
func TestReportBatch_DropsEventsWithoutID(t *testing.T) {
	c := New(newStubSink(), nil)
	res, err := c.ReportBatch(context.Background(), []contract.Event{ev(""), ev("e-2"), ev("e-2")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Accepted != 1 || res.Duplicated != 1 {
		t.Fatalf("期望 accepted=1 / duplicated=1，得到 %+v", res)
	}
}
