package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeBridge 模拟网关字节桥：查询模式按白名单应答，数据模式回固定业务响应。
type fakeBridge struct {
	t        *testing.T
	ln       net.Listener
	allowed  map[string]bool
	gotHosts chan string // 记录数据模式收到的 Host
}

func newFakeBridge(t *testing.T, allowed ...string) *fakeBridge {
	t.Helper()
	fb := &fakeBridge{t: t, allowed: map[string]bool{}, gotHosts: make(chan string, 16)}
	for _, h := range allowed {
		fb.allowed[h] = true
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fb.ln = ln
	go fb.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return fb
}

func (fb *fakeBridge) addr() string { return fb.ln.Addr().String() }

func (fb *fakeBridge) serve() {
	for {
		conn, err := fb.ln.Accept()
		if err != nil {
			return
		}
		go fb.handle(conn)
	}
}

func (fb *fakeBridge) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return
	}
	first := strings.TrimRight(line, "\r\n")
	// 查询模式：?host → OK / NO。
	if strings.HasPrefix(first, "?") {
		host := strings.TrimPrefix(first, "?")
		if fb.allowed[host] {
			_, _ = conn.Write([]byte("OK\n"))
		} else {
			_, _ = conn.Write([]byte("NO\n"))
		}
		return
	}
	// 健康检查模式。
	if strings.HasPrefix(first, "GET ") || strings.HasPrefix(first, "HEAD ") {
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 3\r\nConnection: close\r\n\r\nok\n"))
		return
	}
	// 数据模式：读走随后的请求字节，回固定业务响应。
	fb.gotHosts <- first
	buf := make([]byte, 4096)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = conn.Read(buf)
	body := "tunnel-business:" + first
	_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
}

func (fb *fakeBridge) waitHost(t *testing.T) string {
	t.Helper()
	select {
	case h := <-fb.gotHosts:
		return h
	case <-time.After(2 * time.Second):
		t.Fatal("数据模式没收到 Host 声明")
		return ""
	}
}

// 分流语义：白名单域名走隧道、其余直连 Upstream；网关不可达时全部直连（存量行为）。
func TestTunnelTransportRoutesByHost(t *testing.T) {
	bridge := newFakeBridge(t, "shop.example.com")

	// 直连侧：假 Upstream（存量业务）。
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "direct-upstream")
	}))
	defer upstream.Close()
	base := &http.Transport{}

	tr := newTunnelTransport(base, bridge.addr(), func(string, ...any) {})

	// ① 白名单域名 → 隧道（响应来自桥后的"业务"）。
	req, _ := http.NewRequest(http.MethodGet, "http://shop.example.com/thing", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("隧道路径失败：%v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), "tunnel-business:shop.example.com") {
		t.Fatalf("隧道路径响应不对：%q", body)
	}
	if host := bridge.waitHost(t); host != "shop.example.com" {
		t.Fatalf("桥收到的 Host 不对：%q", host)
	}

	// ② 非白名单域名 → 直连 Upstream。
	req2, _ := http.NewRequest(http.MethodGet, upstream.URL+"/other", nil)
	req2.Host = "other.example.com"
	resp2, err := tr.RoundTrip(req2)
	if err != nil {
		t.Fatalf("直连路径失败：%v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if !strings.Contains(string(body2), "direct-upstream") {
		t.Fatalf("直连路径响应不对：%q", body2)
	}

	// ③ 查询缓存：同一域名的第二次请求不再发起查询（gotHosts 只进数据模式，这里只验证不炸）。
	req3, _ := http.NewRequest(http.MethodGet, "http://shop.example.com/again", nil)
	if _, err := tr.RoundTrip(req3); err != nil {
		t.Fatalf("缓存路径失败：%v", err)
	}
}

// 网关不可达：所有请求回落直连（隧道故障不能拖垮存量业务，NI-1 精神）。
func TestTunnelTransportFallsBackWhenGatewayDown(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "direct-upstream")
	}))
	defer upstream.Close()

	// 指向一个没人监听的端口。
	tr := newTunnelTransport(&http.Transport{}, "127.0.0.1:1", func(string, ...any) {})
	req, _ := http.NewRequest(http.MethodGet, upstream.URL+"/x", nil)
	req.Host = "shop.example.com"
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("网关不可达时应回落直连：%v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), "direct-upstream") {
		t.Fatalf("回落响应不对：%q", body)
	}
}

func TestTunnelNormalizeHost(t *testing.T) {
	for raw, want := range map[string]string{
		"shop.example.com":     "shop.example.com",
		"Shop.Example.com:443": "shop.example.com",
		"shop.example.com.":    "shop.example.com",
		"  shop.example.com\n": "shop.example.com",
	} {
		if got := tunnelNormalizeHost(raw); got != want {
			t.Errorf("tunnelNormalizeHost(%q)=%q，期望 %q", raw, got, want)
		}
	}
}
