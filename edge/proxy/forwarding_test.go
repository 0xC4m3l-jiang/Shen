package proxy

// 本文件补的是**转发路径的边界行为**：升级（WebSocket）· 流式（SSE/分块）· 大响应 · 大上传。
//
// 为什么单独成文件：这些场景的共同点是「不能用 httptest 的默认形态验证」——
//   · 升级要拿到底层连接（hijack）；
//   · 流式要证明**不是全量缓冲**（用到达时间判断）；
//   · 大响应要跨过注入的缓冲上限（1 MiB）；
//   · 大上传要证明我们**不读请求体**（读了就会把上游收到的 body 吃掉）。
//
// 它们都是「不影响原始业务」（NI-1）的边界：任何一条坏了，业务形态就会出问题。

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
)

// startForwardingProxy 起一个只做转发的实例（核心不可达 → 一律放行到业务），返回监听地址。
//
// 用它而不是 httptest：这些行为依赖真实的 HTTP 服务器与连接语义。
func startForwardingProxy(t *testing.T, h *Handler) string {
	t.Helper()
	port := freePort(t)
	cfg, err := BuildConfig(Options{
		Listen:  "127.0.0.1:" + port,
		Handler: h,
		TLS:     TLSConfig{Mode: TLSModeOff},
	})
	if err != nil {
		t.Fatalf("BuildConfig 失败：%v", err)
	}
	if err := caddy.Run(cfg); err != nil {
		t.Fatalf("caddy.Run 失败：%v", err)
	}
	t.Cleanup(func() { _ = caddy.Stop() })
	return "127.0.0.1:" + port
}

// forwardingHandler 造一个「核心不可达」的 handler：所有请求 fail-open 到业务（NI-3）。
func forwardingHandler(upstream string) *Handler {
	return &Handler{
		Upstream:        upstream,
		DecisionTimeout: caddy.Duration(3 * time.Millisecond),
		CacheTTL:        caddy.Duration(time.Minute),
		Window:          caddy.Duration(time.Minute),
		ReportQueue:     8,
		CoreAddr:        "127.0.0.1:1", // 不可达：保证走 fail-open 分支
	}
}

// ── ① 协议升级（WebSocket）：101 必须能穿过去 ────────────────────────────────

func TestWebSocketUpgradePassesThrough(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "not an upgrade", http.StatusBadRequest)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		_, _ = buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = buf.Flush()

		line, err := buf.ReadString('\n')
		if err != nil {
			return
		}
		_, _ = buf.WriteString("echo:" + line)
		_ = buf.Flush()
	}))
	defer origin.Close()

	addr := startForwardingProxy(t, forwardingHandler(origin.URL))

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("拨号失败：%v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	req := "GET /ws HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatal(err)
	}

	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("读状态行失败（升级没有穿过转发层？）：%v", err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("升级应返回 101，实际 %q", strings.TrimSpace(status))
	}
	// 吃掉剩余的升级响应头。
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("读升级响应头失败：%v", err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}

	if _, err := io.WriteString(conn, "hello\n"); err != nil {
		t.Fatal(err)
	}
	echo, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("升级后双向传输失败：%v", err)
	}
	if strings.TrimSpace(echo) != "echo:hello" {
		t.Errorf("升级后的数据应原样回显，实际 %q", echo)
	}
}

// ── ② 流式（SSE / 分块）：必须边产边送，不能全量缓冲 ─────────────────────────

func TestStreamingResponseIsNotBuffered(t *testing.T) {
	const chunks = 3
	const gap = 150 * time.Millisecond

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i := 1; i <= chunks; i++ {
			_, _ = fmt.Fprintf(w, "data: %d\n\n", i)
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(gap)
		}
	}))
	defer origin.Close()

	addr := startForwardingProxy(t, forwardingHandler(origin.URL))

	start := time.Now()
	resp, err := http.Get("http://" + addr + "/stream")
	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	br := bufio.NewReader(resp.Body)
	first, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("读第一块失败：%v", err)
	}
	elapsed := time.Since(start)

	if !strings.Contains(first, "data: 1") {
		t.Errorf("第一块内容不对：%q", first)
	}
	// 全量缓冲的话，第一块要等到全部产完（≥ 3×gap）才会到达。
	if budget := 2 * gap; elapsed > budget {
		t.Errorf("第一块等了 %v（预算 %v）—— 说明响应被全量缓冲，流式被破坏", elapsed, budget)
	}

	rest, _ := io.ReadAll(br)
	if all := first + string(rest); !strings.Contains(all, "data: 3") {
		t.Errorf("后续块必须完整送达，实际 %q", all)
	}
}

// ── ③ 大响应（跨过注入缓冲上限）：原样透传，不得被注入或截断 ─────────────────

func TestLargeResponseIsNotInjectedOrTruncated(t *testing.T) {
	const size = 2 << 20 // 2 MiB：超过注入缓冲上限（1 MiB）

	big := strings.Repeat("A", size-len("</body>")) + "</body>"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", fmt.Sprint(len(big)))
		_, _ = io.WriteString(w, big)
	}))
	defer origin.Close()

	h := forwardingHandler(origin.URL)
	h.Inject = []string{"<!--INJECTED-->"} // 有注入规则，但大响应不该被注入
	addr := startForwardingProxy(t, h)

	resp, err := http.Get("http://" + addr + "/big")
	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读响应失败：%v", err)
	}

	if len(body) != len(big) {
		t.Errorf("大响应必须原样透传：期望 %d 字节，实际 %d", len(big), len(body))
	}
	if strings.Contains(string(body), "<!--INJECTED-->") {
		t.Error("超过缓冲上限的响应**禁止**注入（会破坏流式与大文件语义）")
	}
}

// ── ④ 大上传：我们**不读**请求体，上游必须收到完整内容 ───────────────────────

func TestLargeUploadReachesUpstreamIntact(t *testing.T) {
	const size = 8 << 20 // 8 MiB

	var got int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusInternalServerError)
			return
		}
		got = int(n)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer origin.Close()

	addr := startForwardingProxy(t, forwardingHandler(origin.URL))

	payload := bytes.Repeat([]byte("x"), size)
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/upload", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("上传失败：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("上游应返回 204，实际 %d", resp.StatusCode)
	}
	if got != size {
		t.Errorf("上游必须收到完整请求体：期望 %d 字节，实际 %d", size, got)
	}
}

// ── ⑤ 判定面契约：上游收到的观测必须不含 body（我们不读 body，INT-22 的边界）──

func TestObservationDoesNotCarryBody(t *testing.T) {
	// 这条不需要真进程：它约束的是**我们构造观测的方式**（纯函数）。
	req := httptest.NewRequest(http.MethodPost, "http://x/upload", strings.NewReader("SECRET-BODY"))
	obs := observationFrom(req, false)

	raw, err := json.Marshal(obs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "SECRET-BODY") {
		t.Error("观测**禁止**携带请求体内容（我们不读 body：读了就会吃掉上游的 body）")
	}
}

// ── ⑥ 客户端协议与上游协议（h2 下行 / h1.1 上行）─────────────────────────────
//
// 实测得到的两个事实（本测试把它们**锁成断言**，避免以后被误改）：
//
//	· 客户端↔我们：TLS 上协商到 **HTTP/2**（攻击者看到的就是 h2）；
//	· 我们↔上游：默认走 **HTTP/1.1** —— 这是 `reverse_proxy` 的默认行为，与 nginx / Envoy 同类。
//	  它**不是**缺陷：上游是业务或幻境后端，服务端之间的协议版本对攻击者不可见。
//	  若某个后端**要求** h2（少见），再给传输层加一个显式开关 —— 目前不加（最小实现）。
func TestClientUsesHTTP2UpstreamUsesHTTP11(t *testing.T) {
	certFile, keyFile := genSelfSignedCert(t, t.TempDir())
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream-Proto", r.Proto)
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer origin.Close()

	h := forwardingHandler(origin.URL)
	port := freePort(t)
	cfg, err := BuildConfig(Options{
		Listen:  "127.0.0.1:" + port,
		Handler: h,
		TLS:     TLSConfig{Mode: TLSModeManual, CertFile: certFile, KeyFile: keyFile},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := caddy.Run(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = caddy.Stop() })

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		ForceAttemptHTTP2: true,
	}}
	resp, err := client.Get("https://127.0.0.1:" + port + "/")
	if err != nil {
		t.Fatalf("h2 请求失败：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.ProtoMajor != 2 {
		t.Errorf("客户端应协商到 HTTP/2（攻击者看到的就是它），实际 %s", resp.Proto)
	}
	if got := resp.Header.Get("X-Upstream-Proto"); got != "HTTP/1.1" {
		t.Errorf("到上游默认应是 HTTP/1.1（reverse_proxy 的默认行为），实际 %q", got)
	}
}
