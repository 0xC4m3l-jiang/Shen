package gateway

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// 桥接协议：proxy 每条连接先送一行目标 Host（CRLF/LF 结尾），随后是原始 HTTP 字节。
// 网关把这条连接与对应会话的一条 yamux 流做双向 io.Copy —— 本文件是全模块唯一的转发代码。

// bridgeFirstLineTimeout：proxy 连上后应立即送 Host 行（本地回环，1s 足够）。
const bridgeFirstLineTimeout = 3 * time.Second

// maxHostLine 限制首行长度（Host 最长 253 + 端口，给足余量；过长按坏协议处理）。
const maxHostLine = 512

// ServeBridge 在回环地址上服务 proxy 的取流请求。
func (s *Server) ServeBridge(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("bridge accept：%w", err)
			}
		}
		go func() { _ = s.bridgeOne(conn) }()
	}
}

// bridgeOne 服务一条 proxy 连接：读首行分派三种模式。
//
//	"?<host>"  查询模式：proxy 问「该域名是否属于某接入凭证」（分流判定）→ 立即回 OK/NO；
//	"GET/HEAD" 健康探测（容器 healthcheck）→ HTTP 应答；
//	"<host>"   数据模式：随后的字节与对应会话的隧道流双向桥接。
func (s *Server) bridgeOne(conn net.Conn) error {
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(bridgeFirstLineTimeout))
	first, err := readHostLine(conn)
	if err != nil {
		s.writeBadGateway(conn, "桥接协议错误（首行应为目标 Host）："+err.Error())
		return err
	}
	_ = conn.SetReadDeadline(time.Time{})
	if strings.HasPrefix(first, "?") {
		host := strings.TrimPrefix(first, "?")
		reply := "NO\n"
		if s.keys.HostKnown(host) {
			reply = "OK\n"
		}
		s.logf("gateway: 桥查询 %s ⇒ %s", host, strings.TrimSpace(reply))
		_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		_, _ = conn.Write([]byte(reply))
		return nil
	}
	if strings.HasPrefix(first, "GET ") || strings.HasPrefix(first, "HEAD ") {
		s.serveHealth(conn, first)
		return nil
	}
	host := first

	sess, err := s.pool.LookupHost(host)
	if err != nil {
		// 「域名从未接入」与「接入过但当前离线」分开说，运维一眼定位。
		if !s.keys.HostKnown(host) {
			s.writeBadGateway(conn, "该域名未接入反向隧道（未签发凭证或域名不在白名单）")
		} else {
			s.writeBadGateway(conn, err.Error())
		}
		return err
	}
	stream, err := sess.OpenStream()
	if err != nil {
		s.writeBadGateway(conn, "隧道不可用（连接器会话已断开，等待重连）："+err.Error())
		return err
	}
	defer func() { _ = stream.Close() }()

	// 双向字节桥：**任一方向结束即整体拆除**。
	// 为什么不等两端都 EOF：HTTP/1.1 的请求/响应边界由协议自己表达（Content-Length /
	// Connection: close），隧道是纯字节管道；一侧关闭（业务回完并 close，或 proxy 关连接）
	// 即代表本次交换结束，留着另一侧的拷贝只会挂着等一个永远不会来的 EOF。
	// 这也是 yamux 流半关闭语义的稳妥降级：EOF 即整流关闭。
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(stream, conn); done <- struct{}{} }()
	go func() { _, _ = io.Copy(conn, stream); done <- struct{}{} }()
	<-done
	_ = conn.Close()
	_ = stream.Close()
	<-done
	return nil
}

// readHostLine 逐字节直读首行（Host 行极短；不用 bufio 避免多读字节后回写的竞态）。
func readHostLine(conn net.Conn) (string, error) {
	var b []byte
	one := make([]byte, 1)
	for len(b) < maxHostLine {
		n, err := conn.Read(one)
		if err != nil {
			return "", err
		}
		if n == 0 {
			continue
		}
		if one[0] == '\n' {
			host := strings.TrimRight(string(b), "\r")
			if host == "" {
				return "", fmt.Errorf("空 Host 行")
			}
			return host, nil
		}
		b = append(b, one[0])
	}
	return "", fmt.Errorf("host 行过长")
}

// writeBadGateway 用纯 HTTP 502 回绝（proxy 会把它作为上游错误呈现给观测面）。
// 刻意**不带任何 x-shen-* 头**：该响应会经代理回到攻击者可见面（OH-2 泄漏纪律）。
func (s *Server) writeBadGateway(conn net.Conn, reason string) {
	body := reason + "\n"
	resp := "HTTP/1.1 502 Bad Gateway\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		fmt.Sprintf("Content-Length: %d\r\n", len(body)) +
		"Connection: close\r\n\r\n" + body
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, _ = conn.Write([]byte(resp))
}

// serveHealth 应答容器健康探测（GET/HEAD /healthz → 200，其余 → 404）。
func (s *Server) serveHealth(conn net.Conn, requestLine string) {
	path := strings.Fields(requestLine)
	status, body := "200 OK", "ok\n"
	if len(path) < 2 || path[1] != "/healthz" {
		status, body = "404 Not Found", "not found\n"
	}
	resp := "HTTP/1.1 " + status + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		fmt.Sprintf("Content-Length: %d\r\n", len(body)) +
		"Connection: close\r\n\r\n" + body
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, _ = conn.Write([]byte(resp))
}
