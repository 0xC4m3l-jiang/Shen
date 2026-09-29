package connector

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	connectorv1 "shen/common/api/connector/v1"
	"shen/modules/connector/wire"

	"github.com/hashicorp/yamux"
)

// errFatalAuth：网关明确拒绝凭证 —— 重连无意义（换 key 或改配置才有可能恢复）。
var errFatalAuth = errors.New("网关拒绝接入凭证")

// Tunnel 是一条接入隧道：内部自动维护「拨号 → 握手 → 服务 → 断线退避重连」循环。
type Tunnel struct {
	cfg   Config
	inner context.Context // Close 时取消
	stop  context.CancelFunc

	mu       sync.Mutex // 保护 online 回调通知
	online   bool
	onChange func(online bool)

	// firstErr：首次连接的结果（Connect 语义 = 首连必须成功；
	// 之后断线进入后台重连，错误经 Logf 与 OnChange 暴露）。
	firstErr chan error
}

// Connect 建立接入隧道。首连失败（网络/凭证/白名单）直接返回错误；
// 首连成功后断线由内部自动重连，直到 ctx 取消或 Close。
func Connect(ctx context.Context, cfg Config) (*Tunnel, error) {
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	inner, stop := context.WithCancel(ctx)
	t := &Tunnel{cfg: cfg, inner: inner, stop: stop, firstErr: make(chan error, 1)}
	go t.run()
	select {
	case err := <-t.firstErr:
		if err != nil {
			stop()
			return nil, err
		}
		return t, nil
	case <-ctx.Done():
		stop()
		return nil, ctx.Err()
	}
}

// OnStateChange 注册状态回调（在线状态变化时调用；用于业务侧打点）。
func (t *Tunnel) OnStateChange(fn func(online bool)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onChange = fn
}

func (t *Tunnel) setOnline(v bool) {
	t.mu.Lock()
	fn, changed := t.onChange, v != t.online
	t.online = v
	t.mu.Unlock()
	if changed && fn != nil {
		fn(v)
	}
}

// Close 主动下线：先发 closing 事件（网关把「计划内下线」与意外掉线分开记账）再断开。
// 幂等。
func (t *Tunnel) Close() error {
	t.stop()
	return nil
}

func (t *Tunnel) run() {
	backoff := t.cfg.MinBackoff
	first := true // 首连结果尚未通知（Connect 还在等）
	for {
		err := t.once(func() {
			if first {
				t.firstErr <- nil // 首连成功：立即放行 Connect（服务循环继续跑）
				first = false
			}
		})
		t.setOnline(false)
		select {
		case <-t.inner.Done():
			return
		default:
		}
		if first {
			// 首连失败：直接把错误交给调用方（重建 Tunnel 是业务的决策）。
			t.firstErr <- err
			return
		}
		if errors.Is(err, errFatalAuth) {
			// 运行中凭证被吊销/重置：重试无意义，隧道终止（业务应换 key 重建）。
			t.cfg.Logf("connector: %v，隧道终止", err)
			return
		}
		if err != nil {
			t.cfg.Logf("connector: 隧道断开（%v），%s 后重连", err, backoff)
		}
		select {
		case <-time.After(backoff):
		case <-t.inner.Done():
			return
		}
		backoff = nextBackoff(backoff, t.cfg.MaxBackoff)
	}
}

// once 执行一轮「拨号 → 握手 → 服务数据流直到会话结束」。
// onOnline 在握手成功那一刻回调（用于通知 Connect 首连结果）。
func (t *Tunnel) once(onOnline func()) error {
	d := &net.Dialer{Timeout: t.cfg.DialTimeout}
	raw, err := d.DialContext(t.inner, "tcp", t.cfg.Gateway)
	if err != nil {
		return fmt.Errorf("拨号网关失败：%w", err)
	}
	conn := tls.Client(raw, t.cfg.tlsConfig())
	if err := conn.HandshakeContext(t.inner); err != nil {
		_ = conn.Close()
		return fmt.Errorf("TLS 握手失败：%w", err)
	}
	defer func() { _ = conn.Close() }()

	cfg := yamux.DefaultConfig()
	cfg.KeepAliveInterval = 15 * time.Second
	mux, err := yamux.Client(conn, cfg)
	if err != nil {
		return fmt.Errorf("建立多路复用失败：%w", err)
	}
	defer func() { _ = mux.Close() }()

	// 流 0：握手。
	ctrl, err := mux.OpenStream()
	if err != nil {
		return fmt.Errorf("开控制流失败：%w", err)
	}
	if err := wire.Write(ctrl, &connectorv1.HandshakeRequest{AuthKey: t.cfg.AuthKey,
		Meta: &connectorv1.ServiceMeta{Name: t.cfg.Name, Hosts: t.cfg.Hosts,
			LocalAddr: t.cfg.Upstream, ConnectorVersion: SDKVersion}}, 10*time.Second); err != nil {
		return fmt.Errorf("发送握手失败：%w", err)
	}
	resp, err := wire.ReadHandshakeResponse(ctrl, 10*time.Second)
	if err != nil {
		return fmt.Errorf("读取握手应答失败：%w", err)
	}
	if !resp.GetOk() {
		// 凭证类拒绝：放弃重连（继续重试只会再被拒）。
		return fmt.Errorf("%w：%s", errFatalAuth, resp.GetError())
	}
	t.setOnline(true)
	onOnline()
	t.cfg.Logf("connector: 已接入（会话 %s）", resp.GetSessionId())

	// 数据面：accept 一条流 = 网关转发来一个连接；与本地业务做双向桥。
	for {
		stream, err := mux.Accept()
		if err != nil {
			select {
			case <-t.inner.Done():
				// 主动关闭：先告知网关计划内下线（尽力而为）。
				_ = wire.Write(ctrl, &connectorv1.SessionEvent{
					SessionId: resp.GetSessionId(), Kind: "closing", Detail: "SDK Close"}, 2*time.Second)
				return nil
			default:
				return fmt.Errorf("隧道会话结束：%w", err)
			}
		}
		go t.serve(stream)
	}
}

// serve 把一条隧道流桥到本地业务。任一方向结束即整体拆除（与网关桥同语义）。
func (t *Tunnel) serve(stream net.Conn) {
	defer func() { _ = stream.Close() }()
	d := &net.Dialer{Timeout: t.cfg.DialTimeout}
	biz, err := d.DialContext(t.inner, "tcp", t.cfg.dialAddr)
	if err != nil {
		// 本地业务不可达：经流回一个纯 HTTP 502（网关侧原样转给 proxy）。
		// 刻意不带任何 x-shen-* 头（OH-2：该响应最终会出现在攻击者可见面）。
		body := "local upstream unavailable\n"
		_, _ = fmt.Fprintf(stream, "HTTP/1.1 502 Bad Gateway\r\nContent-Type: text/plain\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
		return
	}
	defer func() { _ = biz.Close() }()
	done := make(chan struct{}, 2)
	go func() { _, _ = copyStream(biz, stream); done <- struct{}{} }()
	go func() { _, _ = copyStream(stream, biz); done <- struct{}{} }()
	<-done
	_ = stream.Close()
	_ = biz.Close()
	<-done
}

func copyStream(dst, src net.Conn) (int64, error) {
	buf := make([]byte, 32<<10)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			w, werr := dst.Write(buf[:n])
			total += int64(w)
			if werr != nil {
				return total, werr
			}
		}
		if err != nil {
			return total, err
		}
	}
}
