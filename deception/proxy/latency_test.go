package proxy

// 本文件给 `AR-29`（业务路径 P99 额外延迟 ≤ 5ms）提供**空载下界**。
//
// 它**不是** AR-29 的验收证据：单机、无并发、无真实载荷、核心不可达（走 fail-open 快路径）。
// 真实验收要在接入演练里按真实载荷测（实验 E3，见 docs/background/notes/pending-experiments.md）。
// 它的作用是：**在真实验收之前，先知道框架本身要花多少**。

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
)

const benchBody = "<html><body>origin-body</body></html>"

func benchOrigin(b *testing.B) *httptest.Server {
	b.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, benchBody)
	}))
	b.Cleanup(srv.Close)
	return srv
}

func startBenchProxy(b *testing.B, upstream string) string {
	b.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	h := &Handler{
		Upstream:        upstream,
		DecisionTimeout: caddy.Duration(3 * time.Millisecond),
		CacheTTL:        caddy.Duration(time.Minute),
		Window:          caddy.Duration(time.Minute),
		ReportQueue:     1024,
		CoreAddr:        "127.0.0.1:1", // 不可达：这是「核心故障」下的最快路径（fail-open 基线）
	}
	cfg, err := BuildConfig(Options{Listen: addr, Handler: h, TLS: TLSConfig{Mode: TLSModeOff}})
	if err != nil {
		b.Fatal(err)
	}
	if err := caddy.Run(cfg); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = caddy.Stop() })
	return addr
}

func benchGet(b *testing.B, url string) {
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 64}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(url)
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("状态码 %d", resp.StatusCode)
		}
	}
}

// BenchmarkAddedLatencyDirect 是基线：直连业务（不经引擎）。
func BenchmarkAddedLatencyDirect(b *testing.B) {
	origin := benchOrigin(b)
	benchGet(b, origin.URL+"/x")
}

// BenchmarkAddedLatencyThroughProxy 是经引擎（真实转发路径 + fail-open）的空载下界。
//
// 额外延迟 ≈ 两者 ns/op 之差。
func BenchmarkAddedLatencyThroughProxy(b *testing.B) {
	origin := benchOrigin(b)
	addr := startBenchProxy(b, origin.URL)
	benchGet(b, fmt.Sprintf("http://%s/x", addr))
}
