package proxy

// SHEN-M3-02 的**端到端**用例：真 Caddy + 真 gRPC + 真业务后端。
//
// 纯函数用例（`writeguard_test.go`）只能证明「判断表是对的」；这里证明的是
// **路由真的按这个判断走**：改道不可用时，写请求没有落到真实业务，读请求仍按 NI-1 回落。
//
// 为什么必须端到端：`dispatch` → `forwardMirage` 有三条不同的失败路径，
// 只有真跑一遍才能确认「谁都没悄悄把 POST 转发给源站」。

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
)

func TestReroutedWriteNeverReachesOrigin(t *testing.T) {
	judgeAddr := startJudgeServer(t)

	var originHits, originWrites atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits.Add(1)
		if r.Method == http.MethodPost {
			originWrites.Add(1)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>origin</body></html>"))
	}))
	defer origin.Close()

	var hpHits atomic.Int64
	hp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hpHits.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>honeypot</body></html>"))
	}))
	defer hp.Close()

	// 关键：`Mirage` 里**没有** "dead"。判分侧会把 /dead/* 判成改道到 "dead"，
	// 于是 dispatch 走「后端名查不到」这条失败路径 —— 正是要验证的分叉点。
	h := &Handler{
		Upstream:        origin.URL,
		Mirage:          map[string]string{"hp": hp.URL},
		DecisionTimeout: caddy.Duration(500 * time.Millisecond),
		CacheTTL:        caddy.Duration(time.Minute),
		Window:          caddy.Duration(time.Minute),
		ReportQueue:     8,
		CoreAddr:        judgeAddr,
	}
	addr := startForwardingProxy(t, h)
	client := &http.Client{Timeout: 5 * time.Second}

	do := func(method, path string) (int, string) {
		t.Helper()
		var body io.Reader
		if method == http.MethodPost {
			body = strings.NewReader("amount=1")
		}
		req, err := http.NewRequest(method, "http://"+addr+path, body)
		if err != nil {
			t.Fatalf("构造请求失败：%v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s 失败：%v", method, path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}

	// ① 改道不可用 + 写请求 ⇒ 503，且源站零命中（这是本条需求的核心）。
	code, body := do(http.MethodPost, "/dead/orders")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("改道不可用的 POST 应返回 503，实际 %d（body=%q）", code, body)
	}
	if got := originWrites.Load(); got != 0 {
		t.Fatalf("写请求回流了业务：源站收到 %d 次写（业务数据会被改）", got)
	}
	if got := originHits.Load(); got != 0 {
		t.Fatalf("写请求不应触达源站任何接口，实际命中 %d 次", got)
	}
	if body != "" {
		t.Fatalf("503 不应带正文（避免泄露信息），实际 %q", body)
	}

	// ② 同一条路径的**读**请求仍按 NI-1 回落业务 —— 不能因为这条需求把读也一起挡死。
	code, body = do(http.MethodGet, "/dead/index")
	if code != http.StatusOK {
		t.Fatalf("改道不可用的 GET 应回落业务（200），实际 %d", code)
	}
	if !strings.Contains(body, "origin") {
		t.Fatalf("GET 应拿到源站内容，实际 %q", body)
	}
	if got := originHits.Load(); got != 1 {
		t.Fatalf("GET 应恰好命中源站一次，实际 %d", got)
	}

	// ③ 改道后端**可用**时，写请求照常投递给蜜罐（证明上面的 503 只发生在失败路径）。
	code, body = do(http.MethodPost, "/mirage/orders")
	if code != http.StatusOK {
		t.Fatalf("蜜罐可用时 POST 应正常返回，实际 %d", code)
	}
	if !strings.Contains(body, "honeypot") {
		t.Fatalf("POST 应落到蜜罐，实际 %q", body)
	}
	if got := hpHits.Load(); got != 1 {
		t.Fatalf("蜜罐应收到 1 次请求，实际 %d", got)
	}
	if got := originWrites.Load(); got != 0 {
		t.Fatalf("蜜罐可用时写请求也不得触达源站，实际 %d 次写", got)
	}
}
