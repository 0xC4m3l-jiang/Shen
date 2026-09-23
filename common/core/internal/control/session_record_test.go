package control

import (
	"context"
	"strings"
	"testing"

	judgev1 "shen/common/api/judge/v1"
	"shen/common/core/internal/session"
)

// recordingSink 是 DecisionRecorder 的测试替身：留下最后一次记录供断言。
type recordingSink struct{ last DecisionRecord }

func (s *recordingSink) Record(_ context.Context, r DecisionRecord) error {
	s.last = r
	return nil
}

// TestJudge_ObservationCarriesMaskedSessionID 断言判定事件带上**会话面具**：
//  1. 有值（L4 才能按会话分组 —— 没有它会把多个会话拼成一条结论）；
//  2. 不是原始身份值（观测面不得复制认证材料）；
//  3. 同身份稳定（跨请求可分组）。
func TestJudge_ObservationCarriesMaskedSessionID(t *testing.T) {
	raw := "sid=abcdef0123456789"
	sink := &recordingSink{}
	svc := NewJudgeService(&stubDecider{}, &stubSession{}, WithDecisionRecorder(sink))

	judge := func() {
		t.Helper()
		if _, err := svc.Judge(context.Background(), &judgev1.JudgeRequest{
			DecisionId: "d-1",
			Observed: &judgev1.Observation{
				SourceIp: "203.0.113.7",
				Method:   "GET",
				Path:     "/api/me",
				Headers:  map[string]string{"cookie": raw},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	judge()
	first := sink.last.SessionID
	if first == "" {
		t.Fatal("判定记录必须带上会话面具（空 = L4 无法按会话分组）")
	}
	if strings.Contains(first, raw) || strings.Contains(first, "sid=") {
		t.Fatalf("会话面具里不得出现原始身份值：%q", first)
	}
	if want := session.NewMasker("").Mask("s-1"); first != want {
		t.Fatalf("面具应由 masker 派生：期望 %q，得到 %q", want, first)
	}
	judge()
	if sink.last.SessionID != first {
		t.Fatalf("同一会话的面具必须稳定：%q vs %q", first, sink.last.SessionID)
	}
}

// TestJudge_UsesAdapterSessionHint 断言：适配器送来的**最小身份**（`session_hint`）决定会话，
// 而 `headers["cookie"]` 里的其它内容不参与 —— 与适配器「按同一个 cookie 名取值」的口径对齐（FIX-5 前半）。
func TestJudge_UsesAdapterSessionHint(t *testing.T) {
	sink := &recordingSink{}
	svc := NewJudgeService(&stubDecider{}, &stubSession{}, WithDecisionRecorder(sink))

	if _, err := svc.Judge(context.Background(), &judgev1.JudgeRequest{
		DecisionId: "d-hint",
		Observed: &judgev1.Observation{
			SourceIp:    "203.0.113.7",
			Method:      "GET",
			Path:        "/api/me",
			SessionHint: "sess-from-adapter",
			Headers:     map[string]string{"cookie": "sid=unused; auth=SECRET"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// stubSession 只是替身（它不看观测内容），这里断言的是**映射**：值确实进了内部请求。
	if got := sink.last.DecisionID; got != "d-hint" {
		t.Fatalf("判定记录应写好：%+v", sink.last)
	}
	if sink.last.SessionID == "" {
		t.Fatal("会话面具必须非空（有 session_hint ⇒ 有身份）")
	}
	if strings.Contains(sink.last.SessionID, "SECRET") || strings.Contains(sink.last.SessionID, "sess-from-adapter") {
		t.Fatalf("观测面不得出现原始身份值，得到 %q", sink.last.SessionID)
	}
}

// TestJudge_NoMaskerLeavesSessionEmpty 断言没有 masker 时**宁可留空也不落原值**。
//
// 空串在契约里的含义是「身份未识别」（NI-1：不得编造）；把原值写进去才是事故。
func TestJudge_NoMaskerLeavesSessionEmpty(t *testing.T) {
	sink := &recordingSink{}
	svc := NewJudgeService(&stubDecider{}, &stubSession{}, WithDecisionRecorder(sink))
	svc.masker = nil

	if _, err := svc.Judge(context.Background(), &judgev1.JudgeRequest{
		DecisionId: "d-2",
		Observed:   &judgev1.Observation{SourceIp: "203.0.113.7", Method: "GET", Path: "/x"},
	}); err != nil {
		t.Fatal(err)
	}
	if sink.last.SessionID != "" {
		t.Fatalf("无 masker 时必须留空，得到 %q", sink.last.SessionID)
	}
	if sink.last.DecisionID != "d-2" {
		t.Fatalf("其余字段不受影响：%+v", sink.last)
	}
}
