package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	judgev1 "shen/common/api/judge/v1"
)

// ── 专属诱饵路由（方案 §9.2 / C01）──────────────────────────────────────────
//
// 三条要钉住的语义：① 归一化 + 路径段边界 + 最长匹配 ② 故障**绝不回生产**
// ③ 影子模式不投递（INT-11：只观测不处置）。

// countingBackend 记录被调用了多少次（用来证明「没有回源」）。
type countingBackend struct {
	name  string
	calls atomic.Int64
}

func (b *countingBackend) ServeHTTP(w http.ResponseWriter, _ *http.Request, _ caddyhttp.Handler) error {
	b.calls.Add(1)
	w.Header().Set("X-Backend", b.name)
	w.WriteHeader(http.StatusOK)
	return nil
}

// decoyPolicyFixture 造一份带 `decoys` 段的边缘载荷。
func decoyPolicyFixture(t *testing.T, version uint64, backends []policyBackend, decoys []policyDecoy) []byte {
	t.Helper()
	doc := edgePolicy{
		SchemaVersion: EdgePolicySchemaVersion,
		PolicyID:      "core-rules",
		Version:       version,
		Backends:      backends,
		Whitelist:     policyWhitelist{SourceCIDRs: []string{}},
		Decoys:        decoys,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// decoyHandler 造一个已装配远端后端构造器、并已应用诱饵路由的 Handler。
func decoyHandler(t *testing.T, cfg Config, judge JudgeClient, routes []policyDecoy, backends []policyBackend) (*Handler, *countingBackend) {
	t.Helper()
	report := &stubReporter{}
	if judge == nil {
		judge = &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	}
	h := newTestHandler(t, cfg, judge, report)
	h.buildRemote = func(name, _ string) (caddyhttp.MiddlewareHandler, error) { return fakeBackend{name: name}, nil }
	origin := &countingBackend{name: "origin"}
	h.origin = origin
	if err := h.applyEdgePolicy(context.Background(), decoyPolicyFixture(t, 1, backends, routes)); err != nil {
		t.Fatalf("应用诱饵路由失败：%v", err)
	}
	return h, origin
}

// TestDecoyRouteMatchSemantics 断言匹配语义：归一化 + 段边界 + 最长优先。
func TestDecoyRouteMatchSemantics(t *testing.T) {
	routes := []policyDecoyRoute{
		{path: "/.git", id: "git", backend: "hp"},
		{path: "/.git/config", id: "git-config", backend: "hp2"},
		{path: "/admin", id: "admin", backend: "hp3"},
	}

	cases := []struct {
		path        string
		wantID      string
		wantMatched bool
	}{
		{"/.git/config", "git-config", true},           // 最长优先
		{"/.git/HEAD", "git", true},                    // 段边界之内
		{"/static/../.git/config", "git-config", true}, // 归一化
		{"/.gitignore", "", false},                     // 段边界：不是 /.git 之下
		{"/administrator", "", false},                  // 段边界：不是 /admin 之下
		{"/", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := matchDecoyRoute(routes, c.path)
		if ok != c.wantMatched || got.id != c.wantID {
			t.Errorf("matchDecoyRoute(%q) = (%q, %v)，期望 (%q, %v)", c.path, got.id, ok, c.wantID, c.wantMatched)
		}
	}
	if _, ok := matchDecoyRoute(nil, "/x"); ok {
		t.Error("空路由表不得命中")
	}
}

// TestDecoyRouteDeliversWithoutCallingCore 断言命中诱饵路由时：落到诱饵后端、**不调核心**、上报 `executed=decoy`。
func TestDecoyRouteDeliversWithoutCallingCore(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_BLOCK}} // 故意给 block：诱饵路由不该问它
	report := &stubReporter{}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, judge, report)
	h.buildRemote = func(name, _ string) (caddyhttp.MiddlewareHandler, error) { return fakeBackend{name: name}, nil }
	origin := &countingBackend{name: "origin"}
	h.origin = origin
	if err := h.applyEdgePolicy(context.Background(), decoyPolicyFixture(t, 1,
		[]policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}},
		[]policyDecoy{{ID: "dev-api", Path: "/portal/api/content", Backend: "hp"}})); err != nil {
		t.Fatal(err)
	}

	w := do(h, http.MethodGet, "http://svc.example/portal/api/content?v=1", "", nil)
	if got := w.Header().Get("X-Backend"); got != "hp" {
		t.Fatalf("应投递到诱饵后端 hp，实际 %q", got)
	}
	if n := judge.callCount(); n != 0 {
		t.Fatalf("诱饵路由是**静态归属**，不得调核心判定；实际调了 %d 次", n)
	}
	if n := origin.calls.Load(); n != 0 {
		t.Fatalf("命中诱饵的请求不得进业务源站；实际 %d 次", n)
	}
	if got := executedOf(t, report); got != executedDecoy {
		t.Fatalf("上报的落点应为 %q，实际 %q", executedDecoy, got)
	}
}

// TestDecoyRouteNeverFallsBackToOrigin 断言**诱饵后端不可用时固定报错、不回生产**（§9.2 的硬边界）。
func TestDecoyRouteNeverFallsBackToOrigin(t *testing.T) {
	// 载荷里**没有** `hp` 这个后端 ⇒ 路由命中但送不到任何地方。
	h, origin := decoyHandler(t, Config{Upstream: "http://127.0.0.1:9"}, nil,
		[]policyDecoy{{ID: "dev-api", Path: "/portal/api/content", Backend: "hp"}}, nil)

	w := do(h, http.MethodGet, "http://svc.example/portal/api/content", "", nil)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("后端不可用应返回 502（固定错误），实际 %d", w.Code)
	}
	if n := origin.calls.Load(); n != 0 {
		t.Fatalf("诱饵请求**禁止**回生产；源站被调用 %d 次", n)
	}
}

// TestDecoyRouteSkippedInShadow 断言影子模式**不投递**（INT-11：只观测不处置），照常走判定链路。
func TestDecoyRouteSkippedInShadow(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	h, origin := decoyHandler(t, Config{Upstream: "http://127.0.0.1:9", Shadow: true}, judge,
		[]policyDecoy{{ID: "dev-api", Path: "/portal/api/content", Backend: "hp"}},
		[]policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}})

	w := do(h, http.MethodGet, "http://svc.example/portal/api/content", "", nil)
	if got := w.Header().Get("X-Backend"); got != "origin" {
		t.Fatalf("影子模式应照常走业务源站，实际 %q", got)
	}
	if n := judge.callCount(); n != 1 {
		t.Fatalf("影子模式下该请求仍应被判定（观测完整），实际调核心 %d 次", n)
	}
	if n := origin.calls.Load(); n != 1 {
		t.Fatalf("影子模式应落到源站 1 次，实际 %d", n)
	}
}

// TestDecoyRouteRevokedByPolicy 断言「载荷里没有这条路由」= 撤销生效（不需要额外回执语义）。
func TestDecoyRouteRevokedByPolicy(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	report := &stubReporter{}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, judge, report)
	h.buildRemote = func(name, _ string) (caddyhttp.MiddlewareHandler, error) { return fakeBackend{name: name}, nil }
	origin := &countingBackend{name: "origin"}
	h.origin = origin

	backends := []policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}}
	route := []policyDecoy{{ID: "dev-api", Path: "/portal/api/content", Backend: "hp"}}
	if err := h.applyEdgePolicy(context.Background(), decoyPolicyFixture(t, 1, backends, route)); err != nil {
		t.Fatal(err)
	}
	// 再下发一版：诱饵段为空 ⇒ 路由被撤销。
	if err := h.applyEdgePolicy(context.Background(), decoyPolicyFixture(t, 2, backends, nil)); err != nil {
		t.Fatal(err)
	}

	w := do(h, http.MethodGet, "http://svc.example/portal/api/content", "", nil)
	if got := w.Header().Get("X-Backend"); got != "origin" {
		t.Fatalf("撤销后该路径应回到业务源站（不再投递），实际 %q", got)
	}
	if n := judge.callCount(); n != 1 {
		t.Fatalf("撤销后应恢复常规判定链路，实际调核心 %d 次", n)
	}
}
