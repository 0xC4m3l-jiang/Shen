package proxy

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 反向隧道接入（可选能力）：`TunnelGateway` 指向网关的回环字节桥（如 127.0.0.1:9447）时，
// 业务侧（route_origin）转发按请求 Host 分流：
//
//   - Host 命中任一接入凭证的域名白名单 → 经网关隧道送达连接器背后的真实业务（零入站暴露）；
//   - 未命中 → 直连 Upstream（存量单上游行为分毫未动）。
//
// 桥协议（对端是 modules/connector/gateway/bridge.go，本文件刻意不 import 它——
// 适配器不依赖隧道模块的代码，两份协议说明互为镜像）：
//
//	"?<host>\n"  查询：网关立刻回 "OK\n"（域名属于某接入凭证）或 "NO\n"，随后关闭连接；
//	"<host>\n"   数据：随后的原始 HTTP 字节与对应连接器会话的一条隧道流双向桥接。
//
// 本文件仍遵守 backend.go 的分层纪律：它是 proxy 里唯一知道「隧道桥怎么说话」的地方。

// 查询缓存：每请求查一次网关太贵；域名集合变化低频，用短 TTL 缓存钉住。
// 命中缓存的请求路径只付一次隧道拨号（连接池复用后连拨号都省了）。
const (
	tunnelKnownTTL   = 15 * time.Second // 正结果缓存：域名在白名单里
	tunnelUnknownTTL = 5 * time.Second  // 负结果缓存：新接入的域名尽快被发现
	tunnelQueryWait  = 2 * time.Second  // 单次查询的全程预算（回环，超过即视为网关不可用）
)

// tunnelTransport 按请求 Host 分流：隧道域名走桥，其余走原有直连 transport。
type tunnelTransport struct {
	base   http.RoundTripper // 原有 origin transport（直连 Upstream）
	tunnel *http.Transport   // 隧道侧（DialContext 拨桥）
	gw     string            // 桥地址
	logf   func(format string, args ...any)

	mu    sync.RWMutex
	known map[string]tunnelKnownEntry // host → 查询结果缓存
}

type tunnelKnownEntry struct {
	known bool
	at    time.Time
}

// newTunnelTransport 组装分流 transport（origin 专用；幻境后端不经隧道）。
func newTunnelTransport(base http.RoundTripper, bridge string, logf func(string, ...any)) *tunnelTransport {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	t := &tunnelTransport{base: base, gw: bridge, logf: logf, known: map[string]tunnelKnownEntry{}}
	t.tunnel = &http.Transport{
		DialContext:         t.dialBridge,
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   false, // 桥是 HTTP/1.1 字节管道
	}
	return t
}

// RoundTrip 分流入口。
func (t *tunnelTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	h := tunnelNormalizeHost(host)
	if h == "" || !t.hostKnown(req.Context(), h) {
		return t.base.RoundTrip(req)
	}
	// 隧道路径：归一 Host（桥按它路由；连接池按它复用——每条连接拨号时声明了自己的 Host），
	// 并经 context 把 Host 带给 DialContext（net.Dialer 的签名里没有它）。
	// 注意：Caddy 的 HTTPTransport 在**它自己的** RoundTrip 里才填 URL.Scheme——
	// 包在外面的我们看到的是空 scheme，标准库 transport 会立即拒绝（unsupported
	// protocol scheme ""）。桥是明文 HTTP/1.1，这里显式补上；同时深拷贝 URL，
	// 不污染共享的原请求（reverseproxy 会复用它）。
	u := *req.URL
	u.Scheme = "http"
	u.Host = h
	creq := req.WithContext(context.WithValue(req.Context(), tunnelHostKey{}, h))
	creq.URL = &u
	creq.Host = h
	resp, err := t.tunnel.RoundTrip(creq)
	if err != nil {
		t.logf("proxy: 隧道转发失败（host=%s）：%v", h, err)
	}
	return resp, err
}

// CloseIdleEntries 关闭两侧空闲连接（进程退出路径用）。
func (t *tunnelTransport) CloseIdleConnections() {
	t.tunnel.CloseIdleConnections()
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

// dialBridge 拨网关桥并声明目标 Host（数据模式）。
func (t *tunnelTransport) dialBridge(ctx context.Context, network, _ string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", t.gw)
	if err != nil {
		t.logf("proxy: 拨网关桥失败：%v", err)
		return nil, fmt.Errorf("拨网关桥 %s 失败：%w", t.gw, err)
	}
	// 拨号目标由请求决定：从上下文里取（RoundTrip 已把归一 Host 放进请求，
	// 这里从 req Host 传不进来 —— DialContext 拿不到请求；用 context 传递）。
	host, _ := ctx.Value(tunnelHostKey{}).(string)
	if host == "" {
		_ = conn.Close()
		t.logf("proxy: 隧道拨号缺少目标 Host（内部错误：context 未带）")
		return nil, fmt.Errorf("隧道拨号缺少目标 Host（内部错误）")
	}
	if _, err := conn.Write([]byte(host + "\n")); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("向网关桥声明 Host 失败：%w", err)
	}
	return conn, nil
}

type tunnelHostKey struct{}

// hostKnown 带缓存地问网关：该域名是否属于某个接入凭证。
// 网关不可达时视为「不是隧道域名」—— 隧道故障不能拖垮存量直连业务（NI-1 的精神）。
func (t *tunnelTransport) hostKnown(ctx context.Context, host string) bool {
	t.mu.RLock()
	e, ok := t.known[host]
	t.mu.RUnlock()
	if ok && time.Since(e.at) < (map[bool]time.Duration{true: tunnelKnownTTL, false: tunnelUnknownTTL}[e.known]) {
		return e.known
	}
	known := t.queryGateway(ctx, host)
	t.mu.Lock()
	t.known[host] = tunnelKnownEntry{known: known, at: time.Now()}
	t.mu.Unlock()
	return known
}

// queryGateway 走查询模式（"?<host>\n" → OK / NO）。
func (t *tunnelTransport) queryGateway(ctx context.Context, host string) bool {
	qctx, cancel := context.WithTimeout(ctx, tunnelQueryWait)
	defer cancel()
	d := &net.Dialer{}
	conn, err := d.DialContext(qctx, "tcp", t.gw)
	if err != nil {
		t.logf("proxy: 隧道网关查询失败（按直连处理）：%v", err)
		return false
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("?" + host + "\n")); err != nil {
		return false
	}
	_ = conn.SetReadDeadline(time.Now().Add(tunnelQueryWait))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return false
	}
	return strings.HasPrefix(line, "OK")
}

// tunnelNormalizeHost 归一 Host：小写、去端口、去尾点（与网关侧同一口径）。
func tunnelNormalizeHost(raw string) string {
	h := strings.ToLower(strings.TrimSpace(raw))
	if hostOnly, _, err := net.SplitHostPort(h); err == nil {
		h = hostOnly
	}
	h = strings.Trim(h, "[]")
	return strings.TrimSuffix(h, ".")
}
