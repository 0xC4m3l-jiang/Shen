package proxy

// 本文件是 **NI-1 的强制测试 `V-1…V-4`**（定义见 docs/design/constraints.md 的 NI-1 表）：
//
//	V-1 | 杀死引擎进程            | 业务请求 100% 正常
//	V-2 | 向决策注入 > 超时预算的延迟 | 业务请求 100% 正常
//	V-3 | 返回 malformed protobuf  | 业务请求 100% 正常
//	V-4 | 返回非法决策值           | 回落放行到真实业务
//
// V-5（CPU 饱和下业务 P99 不劣化）**不在这里** —— 它需要基线 P99 与真实负载，
// 属接入演练（实验 E3，见 docs/background/notes/pending-experiments.md）。
//
// 为什么要走**真进程路径**（真 Caddy + 真 gRPC 客户端 + 真业务后端）而不是替身：
// 这几条断言的正是「适配器与核心之间的真实链路在故障下会不会把业务带下水」，
// 替身会绕开最可能出问题的那一段（连接、编解码、超时传播）。

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"google.golang.org/grpc"

	judgev1 "shen/common/api/judge/v1"
)

// vAssertAllRequestsSucceed：连打 N 次，**每一次**都必须是业务响应。
//
// 用「100%」而不是「多数成功」：`NI-1` 的原话是「可用性与正确性**不受影响**」，
// 一次失败就是一次业务故障，不允许用比例掩盖。
func vAssertAllRequestsSucceed(t *testing.T, proxyAddr, wantBody string) {
	t.Helper()
	const n = 20
	for i := 0; i < n; i++ {
		resp, err := http.Get("http://" + proxyAddr + fmt.Sprintf("/v?i=%d", i))
		if err != nil {
			t.Fatalf("第 %d 次请求失败（业务被带下水了）：%v", i+1, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("第 %d 次请求状态码 %d，期望 200", i+1, resp.StatusCode)
		}
		if string(body) != wantBody {
			t.Fatalf("第 %d 次请求体 %q，期望 %q（业务侧响应当原样透传）", i+1, body, wantBody)
		}
	}
}

// startVProxy 起一个指向指定核心地址的转发实例（明文，影子模式关闭 → 走真实决策路径）。
func startVProxy(t *testing.T, coreAddr, upstream string) string {
	t.Helper()
	h := &Handler{
		Upstream:        upstream,
		DecisionTimeout: caddy.Duration(50 * time.Millisecond), // 故意给一点预算，让 V-2 能超时
		CacheTTL:        caddy.Duration(time.Second),
		Window:          caddy.Duration(time.Second),
		ReportQueue:     8,
		CoreAddr:        coreAddr,
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
	return "127.0.0.1:" + port
}

// ── V-1：杀死引擎进程 ────────────────────────────────────────────────────────

func TestV1_KilledCoreKeepsBusinessAlive(t *testing.T) {
	origin := htmlServer(t, "origin")
	wantBody := "<html><body>origin-body</body></html>"

	// 先起一个真 gRPC 判定面，再**停掉它** —— 这才是「杀死引擎」，而不是指一个从未存在的端口。
	coreAddr, srv := startStoppableJudgeServer(t)
	srv.Stop()

	proxyAddr := startVProxy(t, coreAddr, origin.URL)
	vAssertAllRequestsSucceed(t, proxyAddr, wantBody)
}

// ── V-2：决策延迟 > 超时预算 ─────────────────────────────────────────────────

func TestV2_SlowCoreKeepsBusinessAlive(t *testing.T) {
	origin := htmlServer(t, "origin")
	wantBody := "<html><body>origin-body</body></html>"

	// 真 gRPC 服务端：每次判定延迟 400ms —— 远超 DecisionTimeout(50ms)。
	coreAddr := startSlowJudgeServer(t, 400*time.Millisecond)
	proxyAddr := startVProxy(t, coreAddr, origin.URL)

	start := time.Now()
	vAssertAllRequestsSucceed(t, proxyAddr, wantBody)
	elapsed := time.Since(start)

	// 20 次请求必须走「超时 → 放行」这条快路径：总耗时不该接近 20×400ms。
	if elapsed > 3*time.Second {
		t.Errorf("20 次请求耗时 %v —— 说明在等核心的慢响应而不是超时放行（NI-4）", elapsed)
	}
}

// ── V-3：malformed protobuf ─────────────────────────────────────────────────

func TestV3_MalformedCoreResponseKeepsBusinessAlive(t *testing.T) {
	origin := htmlServer(t, "origin")
	wantBody := "<html><body>origin-body</body></html>"

	// 忠实模拟「返回 malformed protobuf」：一个**裸 TCP 服务**，收到 gRPC 前言后回垃圾字节。
	// （用真 gRPC 服务端是造不出非法 protobuf 的 —— wire 层本身就拒绝。）
	garbageAddr := startGarbageTCPServer(t)
	proxyAddr := startVProxy(t, garbageAddr, origin.URL)

	vAssertAllRequestsSucceed(t, proxyAddr, wantBody)
}

// ── V-4：非法决策值 ─────────────────────────────────────────────────────────

func TestV4_IllegalDecisionValueFallsBackToOrigin(t *testing.T) {
	origin := htmlServer(t, "origin")
	wantBody := "<html><body>origin-body</body></html>"

	// 两种非法：未识别枚举（UNSPECIFIED）与**越界值**（99）。二者都必须回落放行（NI-5）。
	for _, action := range []judgev1.Action{judgev1.Action_ACTION_UNSPECIFIED, judgev1.Action(99)} {
		coreAddr := startFixedJudgeServer(t, action, "no-such-backend")
		proxyAddr := startVProxy(t, coreAddr, origin.URL)
		vAssertAllRequestsSucceed(t, proxyAddr, wantBody)
	}
}

// ── 辅助：几种「坏核心」的真实现 ─────────────────────────────────────────────

// startStoppableJudgeServer 起一个判定面并**把句柄交出来**，供 V-1 「杀死引擎」用。
func startStoppableJudgeServer(t *testing.T) (string, *grpc.Server) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	judgev1.RegisterDeceptionJudgeServer(srv, &fixedJudgeServer{action: judgev1.Action_ACTION_ORIGIN})
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(srv.Stop)
	return ln.Addr().String(), srv
}

// startSlowJudgeServer 起一个「判定很慢」的真 gRPC 服务端。
func startSlowJudgeServer(t *testing.T, delay time.Duration) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	judgev1.RegisterDeceptionJudgeServer(srv, &slowJudgeServer{delay: delay})
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(srv.Stop)
	return ln.Addr().String()
}

// startFixedJudgeServer 起一个「永远返回指定 action」的真 gRPC 服务端。
func startFixedJudgeServer(t *testing.T, action judgev1.Action, backend string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	judgev1.RegisterDeceptionJudgeServer(srv, &fixedJudgeServer{action: action, backend: backend})
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(srv.Stop)
	return ln.Addr().String()
}

// startGarbageTCPServer 起一个「回垃圾字节」的裸 TCP 服务：模拟 malformed protobuf。
func startGarbageTCPServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 4096)
				_, _ = c.Read(buf) // 吃掉 gRPC 前言
				_, _ = c.Write([]byte("this-is-not-protobuf\x00\x01\x02"))
			}(conn)
		}
	}()
	return ln.Addr().String()
}

// slowJudgeServer 是一个「慢」判定面（真 gRPC 服务）。
type slowJudgeServer struct {
	judgev1.UnimplementedDeceptionJudgeServer
	delay time.Duration
}

func (s *slowJudgeServer) Judge(ctx context.Context, _ *judgev1.JudgeRequest) (*judgev1.JudgeResponse, error) {
	select {
	case <-time.After(s.delay):
		return &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// fixedJudgeServer 是一个「永远返回同一个 action」的判定面（真 gRPC 服务）。
type fixedJudgeServer struct {
	judgev1.UnimplementedDeceptionJudgeServer
	action  judgev1.Action
	backend string
}

func (s *fixedJudgeServer) Judge(context.Context, *judgev1.JudgeRequest) (*judgev1.JudgeResponse, error) {
	return &judgev1.JudgeResponse{Action: s.action, Backend: s.backend}, nil
}
