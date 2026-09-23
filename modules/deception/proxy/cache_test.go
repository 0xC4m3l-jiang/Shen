package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	judgev1 "shen/common/api/judge/v1"
)

// ── FIX-2 回归：本地判定缓存的键必须覆盖**参与判定的输入** ─────────────────────
//
// 缺陷形态：只按 `decision_id` 缓存。而 `decision_id` 按（来源 IP, 会话, 路径, 时间窗）派生
// （`ST-10`），不含 method / Host / 查询串 / UA / 策略版本 —— 于是同一 IP + 同一会话 +
// 同一路径下，`GET /admin` 与 `POST /admin` 共用一条缓存结果，只读探测的判定会被套到写请求上。
//
// 两层验证：① 纯函数 `cacheKey` 的键分离性；② 处理链上的**核心调用次数**（不是状态码 ——
// 状态码相同可能来自缓存也可能来自重新判定，只有调用次数能分辨）。

func TestCacheKeySeparatesDecisionInputs(t *testing.T) {
	const id = "d-1"
	mk := func(method, target, ua string) *http.Request {
		r := httptest.NewRequest(method, target, nil)
		if ua != "" {
			r.Header.Set("User-Agent", ua)
		}
		return r
	}
	base := mk(http.MethodGet, "http://svc.example/admin?x=1", "curl/8")

	// ① 相同输入 ⇒ 同键（缓存命中才有意义）。
	if cacheKey(id, base, "rev-1") != cacheKey(id, mk(http.MethodGet, "http://svc.example/admin?x=1", "curl/8"), "rev-1") {
		t.Fatal("相同输入必须得到同一个键")
	}

	// ② 每一项参与判定的输入变了 ⇒ 键必须变（否则会拿旧动作套到新请求上）。
	cases := []struct {
		name string
		req  *http.Request
		rev  string
	}{
		{"method", mk(http.MethodPost, "http://svc.example/admin?x=1", "curl/8"), "rev-1"},
		{"查询串", mk(http.MethodGet, "http://svc.example/admin?x=2", "curl/8"), "rev-1"},
		{"Host", mk(http.MethodGet, "http://other.example/admin?x=1", "curl/8"), "rev-1"},
		{"UA", mk(http.MethodGet, "http://svc.example/admin?x=1", "sqlmap/1.7"), "rev-1"},
		{"策略版本", mk(http.MethodGet, "http://svc.example/admin?x=1", "curl/8"), "rev-2"},
	}
	seen := map[string]string{cacheKey(id, base, "rev-1"): "基线"}
	for _, c := range cases {
		key := cacheKey(id, c.req, c.rev)
		if other, dup := seen[key]; dup {
			t.Errorf("%s 变化后键与「%s」相同（FIX-2：判定输入不同就必须重新判定）", c.name, other)
		}
		seen[key] = c.name
	}

	// ③ 与判定无关的头不得参与（否则缓存形同虚设，每条流量都去调核心）。
	noise := mk(http.MethodGet, "http://svc.example/admin?x=1", "curl/8")
	noise.Header.Set("X-Trace-Id", "abc")
	if cacheKey(id, noise, "rev-1") != cacheKey(id, base, "rev-1") {
		t.Error("与判定无关的头不得改变缓存键")
	}
}

// TestHandlerRejudgesWhenMethodChanges 是**处理链上**的验证：同 IP / 同路径、不同 method，
// 必须各判一次；完全相同输入第二次必须命中缓存（只判一次）。
func TestHandlerRejudgesWhenMethodChanges(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	fixed := time.Unix(1_700_000_000, 0) // 固定时钟：时间窗不漂移，decision_id 才可比
	h := newTestHandler(t, Config{
		Upstream: "http://127.0.0.1:9",
		Now:      func() time.Time { return fixed },
	}, judge, nil)

	do(h, http.MethodGet, "http://svc.example/admin", "", nil)
	if n := judge.callCount(); n != 1 {
		t.Fatalf("首次判定应调核心 1 次，实际 %d", n)
	}
	do(h, http.MethodGet, "http://svc.example/admin", "", nil)
	if n := judge.callCount(); n != 1 {
		t.Fatalf("完全相同输入应命中缓存（不再调核心），实际 %d 次", n)
	}
	do(h, http.MethodPost, "http://svc.example/admin", "", nil)
	if n := judge.callCount(); n != 2 {
		t.Fatalf("method 变了必须重新判定（FIX-2），实际调用 %d 次", n)
	}
	do(h, http.MethodGet, "http://svc.example/admin?probe=1", "", nil)
	if n := judge.callCount(); n != 3 {
		t.Fatalf("查询串变了必须重新判定（FIX-2），实际调用 %d 次", n)
	}
	do(h, http.MethodGet, "http://svc.example/admin?probe=1", "", map[string]string{"User-Agent": "sqlmap/1.7"})
	if n := judge.callCount(); n != 4 {
		t.Fatalf("UA 变了必须重新判定（FIX-2），实际调用 %d 次", n)
	}
}

// TestPolicyRevisionInvalidatesCache 断言策略换代后旧动作不再沿用。
//
// 走的是**内部**契约（`policyRevision` + `cacheKey`）：策略面热更新很难在单测里完整走一遍，
// 而这里要锁的是「版本进了键」这件事本身。
func TestPolicyRevisionInvalidatesCache(t *testing.T) {
	h := &Handler{}
	if got := h.policyRevision(h.remotePolicy()); got != "" {
		t.Fatalf("未接过策略面时应为空串，得到 %q", got)
	}
	h.remote.Store(&remoteState{version: 7, checksum: "abc"})
	if got := h.policyRevision(h.remotePolicy()); got != "abc" {
		t.Fatalf("有 checksum 时应用 checksum，得到 %q", got)
	}
	h.remote.Store(&remoteState{version: 7})
	if got := h.policyRevision(h.remotePolicy()); got != "v7" {
		t.Fatalf("无 checksum 时应用版本号，得到 %q", got)
	}

	r := httptest.NewRequest(http.MethodGet, "http://svc.example/a", nil)
	a := cacheKey("d-1", r, "v7")
	b := cacheKey("d-1", r, "v8")
	if a == b {
		t.Fatal("策略换代必须改变缓存键（否则新策略下发后仍沿用旧动作）")
	}
}
