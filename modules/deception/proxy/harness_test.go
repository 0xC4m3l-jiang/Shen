package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"google.golang.org/grpc"

	judgev1 "shen/common/api/judge/v1"
)

// ── 测试脚手架的参数透传（方案 §11.2 / §11.4 的 T01）────────────────────────
//
// 踩过的坑：`newTestHandler` 把 `cfg.DecisionTimeout` / `cfg.Window` 默认好了，
// 却**没有塞进 Handler** —— 于是单测里 `context.WithTimeout(ctx, 0)`（死线已过）
// 且 `Truncate(0)`（时间窗形同不存在）：用例看着通过，跑的却不是被测参数。
// 这一组用例把「helper 与输入一致」变成可执行判据。

// deadlineJudge 记录每次判定拿到的 context 剩余预算（用于断言硬超时真的传下去了）。
type deadlineJudge struct {
	got chan time.Duration
}

func (j *deadlineJudge) Judge(ctx context.Context, _ *judgev1.JudgeRequest, _ ...grpc.CallOption) (*judgev1.JudgeResponse, error) {
	if d, ok := ctx.Deadline(); ok {
		j.got <- time.Until(d)
	} else {
		j.got <- -1 // 没有死线：等于没有硬超时（NI-4 失效）
	}
	return &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}, nil
}

func TestHandlerHarnessPropagatesConfig(t *testing.T) {
	const budget = 120 * time.Millisecond
	j := &deadlineJudge{got: make(chan time.Duration, 4)}
	h := newTestHandler(t, Config{
		Upstream:        "http://127.0.0.1:9",
		DecisionTimeout: budget,
		Window:          30 * time.Second,
	}, j, nil)

	// Handler 的配置字段必须与输入一致（否则 Validate 与运行期看到的是另一套参数）。
	if got := time.Duration(h.DecisionTimeout); got != budget {
		t.Fatalf("DecisionTimeout 未透传：Handler=%v，输入=%v", got, budget)
	}
	if got := time.Duration(h.Window); got != 30*time.Second {
		t.Fatalf("Window 未透传：Handler=%v", got)
	}
	if err := h.Validate(); err != nil {
		t.Fatalf("透传后的配置应通过 Validate：%v", err)
	}

	do(h, http.MethodGet, "http://svc.example/.git/config", "", nil)
	select {
	case remaining := <-j.got:
		if remaining <= 0 {
			t.Fatalf("判定调用拿到的是已过期的死线（剩余 %v）—— 硬超时没有真的生效", remaining)
		}
		if remaining > budget {
			t.Fatalf("判定预算应 ≤ 配置的 %v，实际剩余 %v", budget, remaining)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("判定替身没有被调用（helper 与链路对不上）")
	}
}

// TestHarnessWindowControlsDecisionID 断言时间窗也真的传下去了：
// 同一窗口内 `decision_id` 相同，跨窗口则不同（`ST-10` 的「时间窗」分量）。
func TestHarnessWindowControlsDecisionID(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	j := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	report := &stubReporter{}
	h := newTestHandler(t, Config{
		Upstream: "http://127.0.0.1:9",
		Window:   30 * time.Second,
		Now:      func() time.Time { return clock },
	}, j, report)

	first := decisionIDOf(t, h, report)
	second := decisionIDOf(t, h, report)
	if first != second {
		t.Fatalf("同一时间窗内 decision_id 必须相同：%q vs %q", first, second)
	}
	clock = clock.Add(31 * time.Second) // 跨过窗口
	third := decisionIDOf(t, h, report)
	if third == first {
		t.Fatalf("跨窗口后 decision_id 必须变化（时间窗未生效）：仍是 %q", third)
	}
}

// decisionIDOf 发一条请求并取回上报事件里的 `decision_id`（缓存命中也会上报，故每次都拿得到）。
func decisionIDOf(t *testing.T, h *Handler, report *stubReporter) string {
	t.Helper()
	before := report.count()
	do(h, http.MethodGet, "http://svc.example/.git/config", "", nil)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		report.mu.Lock()
		if n := len(report.events); n > before {
			raw := append([]byte(nil), report.events[n-1].GetPayload()...)
			report.mu.Unlock()
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatalf("事件载荷不是合法 JSON：%v", err)
			}
			got, _ := payload["decision_id"].(string)
			if got == "" {
				t.Fatal("上报事件里应有 decision_id")
			}
			return got
		}
		report.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("2 秒内没有收到上报事件")
	return ""
}
