package proxy

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"

	judgev1 "shen/common/api/judge/v1"
)

// ── FIX-1 回归：引流后端的响应头超时**必须真的生效** ─────────────────────────
//
// 缺陷形态：`ResponseHeaderTimeout` 在 `HTTPTransport.Provision` **之后**赋值。
// Caddy 是在 Provision 里读它构建内部 `http.Transport` 的（vendor 的 httptransport.go），
// 所以后赋值只改到外壳结构体 —— 真正的 transport 仍是零超时，蜜罐挂死会一直拖住客户端。
//
// 本用例断言的是**可观测行为**（进程挂死 → 返回），不是内部字段：
// 诱饵后端读完请求后不写响应头，请求必须在超时预算内以「引流失败 → 回落业务」结束。

// hangingServer 起一个「读完请求就不写响应头」的后端（模拟挂死的蜜罐），并返回命中计数。
//
// 计数用来证明**请求真的走到了这个后端** —— 否则超时没生效也可能是压根没走引流侧。
func hangingServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	release := make(chan struct{})
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-release:
		case <-r.Context().Done(): // 客户端（我们）放弃等待
		}
	}))
	// 注册晚于 httptest.NewServer ⇒ LIFO 下先执行：先放行阻塞的 handler，再关服务。
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	return srv, &hits
}

func TestMirageResponseHeaderTimeoutIsEffective(t *testing.T) {
	origin := htmlServer(t, "origin")
	hang, hits := hangingServer(t)
	judgeAddr := startJudgeServer(t)

	const budget = 200 * time.Millisecond
	h := &Handler{
		Upstream:              origin.URL,
		Mirage:                map[string]string{"hp": hang.URL},
		DecisionTimeout:       caddy.Duration(50 * time.Millisecond),
		CacheTTL:              caddy.Duration(time.Second),
		Window:                caddy.Duration(time.Second),
		MirageResponseTimeout: caddy.Duration(budget),
		ReportQueue:           8,
		CoreAddr:              judgeAddr,
	}

	port := freePort(t)
	cfg, err := BuildConfig(Options{Listen: "127.0.0.1:" + port, Handler: h, TLS: TLSConfig{Mode: TLSModeOff}})
	if err != nil {
		t.Fatalf("BuildConfig 失败：%v", err)
	}
	if err := caddy.Run(cfg); err != nil {
		t.Fatalf("caddy.Run 失败：%v", err)
	}
	t.Cleanup(func() { _ = caddy.Stop() })

	client := &http.Client{Timeout: 5 * time.Second} // 兜底：真挂死时让用例失败而不是永久卡住
	started := time.Now()
	resp, err := client.Get("http://127.0.0.1:" + port + "/mirage")
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("请求失败（超时未生效会卡到客户端兜底超时）：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := hits.Load(); got == 0 {
		t.Fatal("请求没到引流后端 —— 用例没走到被测分支")
	}
	// 超时必须远早于客户端兜底超时：给足调度余量（CI 上 200ms 预算 → 2s 内必须返回）。
	if elapsed > 2*time.Second {
		t.Fatalf("引流后端不写响应头时耗时 %v —— 响应头超时没有生效（FIX-1）", elapsed)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("引流失败应回落业务并返回 200，实际 %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Backend"); got != "origin" {
		t.Fatalf("引流后端超时应回落业务（NI-1），实际 X-Backend=%q", got)
	}
}

// 引流后端**立即**返回时不受超时影响：超时只治挂死，不得把正常慢响应也变成回落。
func TestMirageResponseHeaderTimeoutKeepsFastBackend(t *testing.T) {
	origin := htmlServer(t, "origin")
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond) // 慢，但在 1s 预算内
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Backend", "hp")
		_, _ = w.Write([]byte("<html><body>hp-body</body></html>"))
	}))
	t.Cleanup(slow.Close)
	judgeAddr := startJudgeServer(t)

	h := &Handler{
		Upstream:              origin.URL,
		Mirage:                map[string]string{"hp": slow.URL},
		DecisionTimeout:       caddy.Duration(50 * time.Millisecond),
		CacheTTL:              caddy.Duration(time.Second),
		Window:                caddy.Duration(time.Second),
		MirageResponseTimeout: caddy.Duration(time.Second),
		ReportQueue:           8,
		CoreAddr:              judgeAddr,
	}
	port := freePort(t)
	cfg, err := BuildConfig(Options{Listen: "127.0.0.1:" + port, Handler: h, TLS: TLSConfig{Mode: TLSModeOff}})
	if err != nil {
		t.Fatalf("BuildConfig 失败：%v", err)
	}
	if err := caddy.Run(cfg); err != nil {
		t.Fatalf("caddy.Run 失败：%v", err)
	}
	t.Cleanup(func() { _ = caddy.Stop() })

	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get("http://127.0.0.1:" + port + "/mirage")
	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("X-Backend"); got != "hp" {
		t.Fatalf("预算内的慢后端不应被超时打断，实际 X-Backend=%q", got)
	}
}

// 决策 = 改道但后端名不在表里时仍回落业务（与超时无关的另一条回落路径，防回归）。
func TestMirageUnknownBackendFallsBack(t *testing.T) {
	origin := htmlServer(t, "origin")
	judgeAddr := startJudgeServer(t)
	h := &Handler{
		Upstream:        origin.URL,
		DecisionTimeout: caddy.Duration(50 * time.Millisecond),
		CacheTTL:        caddy.Duration(time.Second),
		Window:          caddy.Duration(time.Second),
		ReportQueue:     8,
		CoreAddr:        judgeAddr,
	}
	port := freePort(t)
	cfg, err := BuildConfig(Options{Listen: "127.0.0.1:" + port, Handler: h, TLS: TLSConfig{Mode: TLSModeOff}})
	if err != nil {
		t.Fatalf("BuildConfig 失败：%v", err)
	}
	if err := caddy.Run(cfg); err != nil {
		t.Fatalf("caddy.Run 失败：%v", err)
	}
	t.Cleanup(func() { _ = caddy.Stop() })

	// judge 替身把包含 "mirage" 的路径判成改道后端 "hp"，而本配置没有 hp 表项。
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get("http://127.0.0.1:" + port + "/mirage")
	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("未知后端应回落业务 200，实际 %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Backend"); got != "origin" {
		t.Fatalf("未知后端必须回落业务（NI-5），实际 X-Backend=%q", got)
	}
}

// 决策面替身的取值语义在本文件里被复用：确认它仍然按路径给三值（防止上游用例改动后静默偏题）。
func TestJudgeStubRoutingAssumption(t *testing.T) {
	srv := &testJudgeServer{}
	resp, err := srv.Judge(t.Context(), &judgev1.JudgeRequest{Observed: &judgev1.Observation{Path: "/mirage"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetAction() != judgev1.Action_ACTION_MIRAGE || resp.GetBackend() != "hp" {
		t.Fatalf("替身路由假设已变（本文件依赖 /mirage → 改道 hp）：%v / %q", resp.GetAction(), resp.GetBackend())
	}
}
