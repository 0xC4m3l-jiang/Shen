package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"reflect"
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

// TestHandlerHarnessPropagatesConfig 逐字段断言：**`Config` 的每一项都必须落到 `Handler` 上**。
//
// 为什么用反射而不是手写断言（`FIX` / `N9` 的教训）：手写清单会随字段增加而**腐烂** ——
// 曾经漏掉 `DecisionTimeout` / `Window`：单测里 `context.WithTimeout(ctx, 0)`（死线已过）与
// `Truncate(0)`（时间窗形同不存在）⇒ **用例看着通过，跑的却不是被测参数**。
// 反射版本对"新增字段忘接线"自动失败，不需要谁记得回来补清单。
//
// 做法：把每个字段都设成**非零**，构造后检查同名的 Handler 字段也非零 —— 漏接线 ⇒ Handler 侧是零值 ⇒ 失败。
// configFieldAliases 是 `Config` → `Handler` 的**少数**名字映射。
//
// 只有"同一个东西在两个地方用了不同名字"才登记（例如导出给装配层的 `Now` 与 Handler 内部的 `now`）；
// 它**不是**跳过清单 —— 映射里出现不存在的字段照样会让用例失败（见下面的腐烂检查）。
var configFieldAliases = map[string]string{
	"Now":      "now",      // Config 用导出名（跨包可见），Handler 里的时钟字段不导出
	"Injector": "injector", // 同上：注入器是注入进 Handler 的依赖，字段不导出
}

func TestHandlerHarnessPropagatesConfig(t *testing.T) {
	const budget = 120 * time.Millisecond
	j := &deadlineJudge{got: make(chan time.Duration, 4)}
	cfg := Config{
		Upstream: "http://127.0.0.1:9",
		Mirage:   map[string]string{"hp": "http://127.0.0.1:2222"},
		// 刻意**不含**测试客户端的来源网段（httptest 用 192.0.2.1）：否则命中白名单就不调核心，
		// 下面那条"判定真的被调用"的行为断言会与"白名单字段非零"互相打脸。
		Whitelist:             []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")},
		DecisionTimeout:       budget,
		CacheTTL:              30 * time.Second,
		Window:                45 * time.Second,
		Shadow:                true,
		TrustXFF:              true,
		ReportQueue:           4,
		CacheMaxEntries:       16,
		Now:                   func() time.Time { return time.Unix(1_700_000_000, 0) },
		Injector:              &stubInjector{marker: "<!--probe-->"},
		SessionCookie:         "sid2",
		MirageResponseTimeout: 3 * time.Second,
		DegradedCacheTTL:      2 * time.Second,
		DecoyLease:            time.Hour,
		ForwardCredentials:    true,
		BackendHealthInterval: 90 * time.Second,
		BackendHealthTimeout:  2 * time.Second,
		LogRequests:           true,
		InjectContent:         true,
	}
	h := newTestHandler(t, cfg, j, nil)

	cv := reflect.ValueOf(cfg)
	hv := reflect.ValueOf(h).Elem()
	ht := hv.Type()
	usedAliases := map[string]bool{}
	for i := range cv.NumField() {
		name := cv.Type().Field(i).Name
		if cv.Field(i).IsZero() {
			// 测试自身的完整性：漏设一个字段就会让这条用例**永远查不出**那个字段（静默失效）。
			t.Fatalf("测试自身有误：Config.%s 没设成非零，本用例查不出它的透传", name)
		}
		hname := name
		if alias, ok := configFieldAliases[name]; ok {
			hname = alias
			usedAliases[name] = true
		}
		if _, ok := ht.FieldByName(hname); !ok {
			t.Fatalf("Handler 缺少 Config.%s 的落点字段（%s）：没有落点就不会被透传", name, hname)
		}
		if hv.FieldByName(hname).IsZero() {
			t.Fatalf("Config.%s 没有透传进 Handler.%s（helper 漏接线 —— 用例会跑在另一套参数上）", name, hname)
		}
	}
	// 别名表**自身防腐烂**：指向不存在的 Config 字段说明表已经过期（与其它的"豁免会腐烂"同一纪律）。
	for name := range configFieldAliases {
		if !usedAliases[name] {
			t.Fatalf("configFieldAliases 里的 %q 不是 Config 的字段（别名表过期，删掉它）", name)
		}
	}

	// 行为面再验一次：硬超时真的生效（配置值 ⇒ 判定调用拿到的死线）。
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
