package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	judgev1 "shen/common/api/judge/v1"
	"shen/common/core/internal/contract"
	"shen/common/core/internal/control"
	"shen/common/core/internal/session"
	"shen/common/core/internal/store"
	"shen/common/core/internal/telemetry"
)

// 装配层回归：判定 → 观测面（事件 + 判定记录）这条链的形状。
//
// 放在 cmd/core 的理由与 main_test.go 相同：它同时用到 control / session / store / telemetry
// 的真实实现，而 MD-22 禁止模块级测试依赖其他模块的真实实例。

// recordingSink 是一个只记账的 telemetry.Sink。
type recordingSink struct {
	mu     sync.Mutex
	events []contract.Event
}

func (s *recordingSink) Write(_ context.Context, ev contract.Event) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
	return true, nil
}

func (s *recordingSink) ids() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.events))
	for _, ev := range s.events {
		out = append(out, ev.EventID)
	}
	return out
}

// stubObserverDecider 是 Decider 的替身（本用例只关心判定结果怎么被记下来）。
type stubObserverDecider struct{}

func (stubObserverDecider) Decide(_ context.Context, req contract.JudgeRequest) (contract.Decision, error) {
	return contract.Decision{DecisionID: req.DecisionID, Action: contract.ActionMirage, Backend: "hp"}, nil
}

// TestDecisionRecorderKeepsEveryJudgement 断言同一 decision_id 下的多次判定**各自成一条事件**。
//
// 为什么必须这样：`decision_id` 按（来源标识, 会话, 路径, 时间窗）派生（ST-10），不含 UA / 方法；
// 而适配器的本地缓存键覆盖了这些输入（proxy 的 cacheKey），于是同窗口内不同 UA 会真的判两次 ——
// 两次都是事实。事件 id 若仍等于 decision_id，第二次会被幂等键（AR-11）折叠：路由对了、证据少一条。
func TestDecisionRecorderKeepsEveryJudgement(t *testing.T) {
	sink := &recordingSink{}
	rec := &decisionRecorder{
		events:    telemetry.New(sink, nil),
		decisions: store.NewDecisionMemory(nil),
	}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := rec.Record(ctx, control.DecisionRecord{
			DecisionID: "d-1", Method: "GET", Path: "/admin", UserAgent: "sqlmap/1.7",
		}); err != nil {
			t.Fatalf("第 %d 次记录失败：%v", i+1, err)
		}
	}

	ids := sink.ids()
	if len(ids) != 2 {
		t.Fatalf("同一 decision_id 的两次判定应各成一条事件，得到 %v", ids)
	}
	if ids[0] == ids[1] {
		t.Fatalf("事件 id 必须逐判定唯一，两次都是 %q", ids[0])
	}
	for _, id := range ids {
		if !strings.HasPrefix(id, "decision:d-1:") {
			t.Errorf("事件 id 应保留 decision_id 前缀（便于按判定聚合）：%q", id)
		}
	}
}

// TestDecisionEventCarriesMaskedSession 是跨模块的落地检查：
// 会话身份经核心提取 → **脱敏** → 进事件载荷（L4 靠它按会话分组，且这里不得出现原始 Cookie 值）。
func TestDecisionEventCarriesMaskedSession(t *testing.T) {
	sink := &recordingSink{}
	rec := &decisionRecorder{
		events:    telemetry.New(sink, nil),
		decisions: store.NewDecisionMemory(nil),
	}
	svc := control.NewJudgeService(
		stubObserverDecider{},
		session.New("sid"),
		control.WithDecisionRecorder(rec),
		control.WithSessionMasker(session.NewMasker("test-key")),
	)

	const raw = "sid=abcdef0123456789"
	if _, err := svc.Judge(context.Background(), &judgev1.JudgeRequest{
		DecisionId: "d-7",
		Observed: &judgev1.Observation{
			SourceIp: "203.0.113.9",
			Method:   "GET",
			Path:     "/admin",
			Headers:  map[string]string{"cookie": raw},
		},
	}); err != nil {
		t.Fatalf("判定失败：%v", err)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.events) != 1 {
		t.Fatalf("期望 1 条事件，得到 %d", len(sink.events))
	}
	var payload map[string]any
	if err := json.Unmarshal(sink.events[0].Payload, &payload); err != nil {
		t.Fatalf("事件载荷不是合法 JSON：%v", err)
	}
	got, _ := payload["session_id"].(string)
	if got == "" {
		t.Fatal("判定事件必须带 session_id（L4 靠它按会话分组）")
	}
	if strings.Contains(string(sink.events[0].Payload), "abcdef0123456789") {
		t.Fatal("事件载荷里不得出现原始会话值（观测面不得复制认证材料）")
	}
	if want := session.NewMasker("test-key").Mask("abcdef0123456789"); got != want {
		t.Fatalf("session_id 应是面具值：期望 %q，得到 %q", want, got)
	}
}
