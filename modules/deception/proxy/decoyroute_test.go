package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
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
	if err := h.applyEdgePolicy(context.Background(), decoyPolicyFixture(t, 1, backends, routes), ""); err != nil {
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
		got, ok := matchDecoyRoute(routes, c.path, "")
		if ok != c.wantMatched || got.id != c.wantID {
			t.Errorf("matchDecoyRoute(%q) = (%q, %v)，期望 (%q, %v)", c.path, got.id, ok, c.wantID, c.wantMatched)
		}
	}
	if _, ok := matchDecoyRoute(nil, "/x", ""); ok {
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
		[]policyDecoy{{ID: "dev-api", Path: "/portal/api/content", Backend: "hp"}}), ""); err != nil {
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

// TestDecoyRouteRevokedByPolicy 断言撤销语义（N6）：载荷里没有这条路由 ≠ 归属立刻还给业务。
//
// 一条诱饵路径一旦对外出现过，旧链接 / 爬虫 / 对手笔记会继续用它。撤销后在**租约期内**
// 边缘仍用固定 502 结束它（不回生产、不进判定），到期才真正释放归属（见下一个测试）。
func TestDecoyRouteRevokedByPolicy(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	report := &stubReporter{}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, judge, report)
	h.buildRemote = func(name, _ string) (caddyhttp.MiddlewareHandler, error) { return fakeBackend{name: name}, nil }
	origin := &countingBackend{name: "origin"}
	h.origin = origin

	backends := []policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}}
	route := []policyDecoy{{ID: "dev-api", Path: "/portal/api/content", Backend: "hp"}}
	if err := h.applyEdgePolicy(context.Background(), decoyPolicyFixture(t, 1, backends, route), ""); err != nil {
		t.Fatal(err)
	}
	// 再下发一版：诱饵段为空 ⇒ 路由被撤销，但进入租约期。
	if err := h.applyEdgePolicy(context.Background(), decoyPolicyFixture(t, 2, backends, nil), ""); err != nil {
		t.Fatal(err)
	}

	w := do(h, http.MethodGet, "http://svc.example/portal/api/content", "", nil)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("租约期内该路径应返回固定 502（不回生产），实际 %d", w.Code)
	}
	if got := w.Header().Get("X-Backend"); got != "" {
		t.Fatalf("租约期内不得投递任何后端，实际落点 %q", got)
	}
	if n := origin.calls.Load(); n != 0 {
		t.Fatalf("租约期内不得回业务源站，实际 %d 次", n)
	}
	if n := judge.callCount(); n != 0 {
		t.Fatalf("诱饵路径不进判定链路，实际调核心 %d 次", n)
	}
}

// TestDecoyTombstoneLeaseExpires 断言租约到期后归属**真的**还给业务（否则就成了永久劫持）。
func TestDecoyTombstoneLeaseExpires(t *testing.T) {
	base := time.Now()
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	report := &stubReporter{}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, judge, report)
	h.DecoyLease = caddy.Duration(time.Hour)
	h.now = func() time.Time { return base }
	h.buildRemote = func(name, _ string) (caddyhttp.MiddlewareHandler, error) { return fakeBackend{name: name}, nil }
	origin := &countingBackend{name: "origin"}
	h.origin = origin

	backends := []policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}}
	route := []policyDecoy{{ID: "dev-api", Path: "/portal/api/content", Backend: "hp"}}
	must := func(version uint64, routes []policyDecoy) {
		t.Helper()
		if err := h.applyEdgePolicy(context.Background(), decoyPolicyFixture(t, version, backends, routes), ""); err != nil {
			t.Fatal(err)
		}
	}
	must(1, route)
	must(2, nil) // 撤销 ⇒ 进租约
	if len(h.remotePolicy().tombstones) != 1 {
		t.Fatalf("撤销后应有 1 条搜索碑，实际 %d", len(h.remotePolicy().tombstones))
	}

	// 租约未到期：仍受保护。
	if w := do(h, http.MethodGet, "http://svc.example/portal/api/content", "", nil); w.Code != http.StatusBadGateway {
		t.Fatalf("租约内应 502，实际 %d", w.Code)
	}

	// 时间推进过租约，再下发一版（策略应用时才重算碑）⇒ 碑被清掉，归属还给业务。
	h.now = func() time.Time { return base.Add(2 * time.Hour) }
	must(3, nil)
	if n := len(h.remotePolicy().tombstones); n != 0 {
		t.Fatalf("租约到期后不应留碑，实际 %d", n)
	}
	w := do(h, http.MethodGet, "http://svc.example/portal/api/content", "", nil)
	if got := w.Header().Get("X-Backend"); got != "origin" {
		t.Fatalf("租约到期后该路径应回到业务源站，实际 %q（状态 %d）", got, w.Code)
	}
	if n := judge.callCount(); n != 1 {
		t.Fatalf("租约到期后应恢复常规判定链路，实际调核心 %d 次", n)
	}
}

// TestDecoyTombstoneRevivedByReRegistration 断言“重新登记”就撤销搜索碑（路径又有主了）。
func TestDecoyTombstoneRevivedByReRegistration(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, judge, &stubReporter{})
	h.buildRemote = func(name, _ string) (caddyhttp.MiddlewareHandler, error) { return fakeBackend{name: name}, nil }
	origin := &countingBackend{name: "origin"}
	h.origin = origin
	backends := []policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}}
	route := []policyDecoy{{ID: "dev-api", Path: "/portal/api/content", Backend: "hp"}}
	steps := []struct {
		version uint64
		routes  []policyDecoy
	}{{1, route}, {2, nil}, {3, route}}
	for _, step := range steps {
		if err := h.applyEdgePolicy(context.Background(), decoyPolicyFixture(t, step.version, backends, step.routes), ""); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(h.remotePolicy().tombstones); n != 0 {
		t.Fatalf("重新登记后不应再有碑，实际 %d", n)
	}
	if w := do(h, http.MethodGet, "http://svc.example/portal/api/content", "", nil); w.Header().Get("X-Backend") != "hp" {
		t.Fatalf("重新登记后应重新投递诱饵后端，实际 %q", w.Header().Get("X-Backend"))
	}
}

// ── W7：归属声明（hosts）参与匹配 ───────────────────────────────────────────
//
// 语义三条：
//
//	① 声明了主机 ⇒ 只有 Host 命中才接管这条路径（别的站点上的同名路径照常走判定）；
//	② 未声明（旧载荷没有这个字段）⇒ 任何主机都匹配，行为与从前一致（不悄悄"谁都不接管"）；
//	③ `*.example.com` 只覆盖子域，**不含**根域本身。
func TestDecoyRouteRespectsHostOwnership(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	report := &stubReporter{}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, judge, report)
	h.buildRemote = func(name, _ string) (caddyhttp.MiddlewareHandler, error) { return fakeBackend{name: name}, nil }
	origin := &countingBackend{name: "origin"}
	h.origin = origin

	backends := []policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}}
	routes := []policyDecoy{
		{ID: "owned", Path: "/admin", Hosts: []string{"console.example", "*.corp.example"}, Backend: "hp"},
		{ID: "unscoped", Path: "/legacy", Backend: "hp"},
	}
	if err := h.applyEdgePolicy(context.Background(), decoyPolicyFixture(t, 1, backends, routes), ""); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		host      string
		path      string
		wantRoute string
	}{
		{"console.example", "/admin/users", "hp"},      // 精确声明
		{"CONSOLE.EXAMPLE:8443", "/admin/users", "hp"}, // 大小写 + 端口不影响
		{"team.corp.example", "/admin", "hp"},          // 通配命中子域
		{"corp.example", "/admin", "origin"},           // ⚠️ 通配**不含**根域
		{"other.example", "/admin/users", "origin"},    // 未声明的主机 ⇒ 不接管
		{"other.example", "/legacy", "hp"},             // 未声明主机 + 未声明归属 ⇒ 照旧接管
	}
	for _, c := range cases {
		w := do(h, http.MethodGet, "http://"+c.host+c.path, "", nil)
		if got := w.Header().Get("X-Backend"); got != c.wantRoute {
			t.Errorf("host=%s path=%s：期望落点 %q，实际 %q（状态 %d）", c.host, c.path, c.wantRoute, got, w.Code)
		}
	}
	// 未命中归属的请求要走**判定链路**（不是被固定 502）：它只是"别人的路径"。
	if n := judge.callCount(); n == 0 {
		t.Fatal("未命中归属的请求必须照常判定（W7：不接管 ≠ 拒绝服务）")
	}
}

func TestHostMatchesSemantics(t *testing.T) {
	cases := []struct {
		hosts []string
		host  string
		want  bool
	}{
		{nil, "anything.example", true},                      // 未声明 ⇒ 任何主机
		{[]string{}, "anything.example", true},               // 空声明同上
		{[]string{"a.example"}, "a.example:8443", true},      // 端口剥离
		{[]string{"a.example"}, "A.EXAMPLE", true},           // 大小写不敏感
		{[]string{"a.example"}, "b.example", false},          // 不命中
		{[]string{"*.example"}, "x.example", true},           // 通配子域
		{[]string{"*.example"}, "example", false},            // 根域不算命中
		{[]string{"*.example"}, "x.example.evil.com", false}, // 后缀必须完整对齐
		{[]string{"*.a.example"}, "x.a.example", true},
		{[]string{"*.a.example"}, "a.example", false},
	}
	for _, c := range cases {
		if got := hostMatches(c.hosts, c.host); got != c.want {
			t.Errorf("hostMatches(%v, %q) = %v，期望 %v", c.hosts, c.host, got, c.want)
		}
	}
}

// TestNoPolicyMeansNoDecoyProtection 显式钉住一条**已知边界**：还没有拿到策略时，诱饵路径没有保护。
//
// 为什么要把"没做到"写成测试：建议书 §1.1 点出「影子模式、策略尚未装载、路由被删除/禁用后，
// 请求转回常规链路，可能到业务」。这是**当前事实**，不是 bug 报告 —— 把它写成可断言的用例，
// 将来有人补上"启动即保护"时这条会**主动失败**，提醒改这里与文档，而不是悄悄改变行为。
func TestNoPolicyMeansNoDecoyProtection(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}
	h := newTestHandler(t, Config{Upstream: "http://127.0.0.1:9"}, judge, &stubReporter{})
	origin := &countingBackend{name: "origin"}
	h.origin = origin

	// 从未应用过策略（等价于"策略面还没就绪"）：/admin 走常规链路 ⇒ 落到业务。
	w := do(h, http.MethodGet, "http://svc.example/admin/login", "", nil)
	if got := w.Header().Get("X-Backend"); got != "origin" {
		t.Fatalf("策略未就绪时该路径应走常规链路到业务（已知边界），实际落点 %q", got)
	}
	if n := origin.calls.Load(); n != 1 {
		t.Fatalf("应恰好落到源站 1 次，实际 %d", n)
	}
	if n := judge.callCount(); n != 1 {
		t.Fatalf("应照常判定（观测完整），实际调核心 %d 次", n)
	}
}
