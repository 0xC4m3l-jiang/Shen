package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	connectorv1 "shen/common/api/connector/v1"
	"shen/modules/connector/wire"

	"github.com/hashicorp/yamux"
)

// 测试假密钥：shc- 前缀不命中 secrets-check 规则①（sk- 形态）；功能与真值等价。
const testKey = "shc-live-0123456789abcdef0123456789abcdef"

func hashOf(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// ── KeyTable ────────────────────────────────────────────────────────────────

func TestKeyTableValidation(t *testing.T) {
	tbl := &KeyTable{}
	tbl.Replace([]ConsoleKey{{
		ID: "cred-1", Name: "商城", Hosts: []string{"shop.example.com", "*.mall.example.com"},
		KeyHash: hashOf(testKey),
	}})
	if _, err := tbl.Validate(testKey, "商城", []string{"shop.example.com"}); err != nil {
		t.Fatalf("合法握手不应被拒：%v", err)
	}
	// 通配命中。
	if _, err := tbl.Validate(testKey, "商城", []string{"a.mall.example.com"}); err != nil {
		t.Fatalf("通配白名单应放行：%v", err)
	}
	for name, tc := range map[string]struct {
		key, svc string
		hosts    []string
	}{
		"错误key":  {"shc-wrong-key-0000000000000000000000000000", "商城", []string{"shop.example.com"}},
		"服务名不一致": {testKey, "别的东西", []string{"shop.example.com"}},
		"域名超白名单": {testKey, "商城", []string{"evil.example.com"}},
	} {
		if _, err := tbl.Validate(tc.key, tc.svc, tc.hosts); err == nil {
			t.Fatalf("%s 应被拒绝", name)
		}
	}
	// 吊销后拒绝。
	tbl.Replace([]ConsoleKey{{ID: "cred-1", Name: "商城", Hosts: []string{"shop.example.com"}, KeyHash: hashOf(testKey), Revoked: true}})
	if _, err := tbl.Validate(testKey, "商城", nil); err == nil || !strings.Contains(err.Error(), "吊销") {
		t.Fatalf("吊销 key 应被拒且有明确错误：%v", err)
	}
	if !tbl.HostKnown("shop.example.com") || tbl.HostKnown("unknown.example.com") {
		t.Fatal("HostKnown 判定不对")
	}
	if h := NormalizeHost("Shop.Example.com:443."); h != "shop.example.com" {
		t.Fatalf("Host 归一不对：%q", h)
	}
	if !HostMatches("*.mall.example.com", "a.mall.example.com") || HostMatches("*.mall.example.com", "mall.example.com") {
		t.Fatal("通配匹配语义不对")
	}
}

// ── 会话池并发 ──────────────────────────────────────────────────────────────

// 竞态钉住：旧会话的**延迟** Unregister 不得误删重连后的新会话（身份校验 / 代际消歧）。
func TestPoolUnregisterRaceWithReconnect(t *testing.T) {
	p := NewPool()
	old := &Session{CredentialID: "c1", Name: "A", Hosts: []string{"a.example.com"}, OpenedAt: time.Now()}
	p.Register(old)
	// 重连：新会话顶替（Register 返回被顶替的旧会话）。
	fresh := &Session{CredentialID: "c1", Name: "A", Hosts: []string{"a.example.com"}, OpenedAt: time.Now()}
	superseded := p.Register(fresh)
	if len(superseded) != 1 || superseded[0] != old {
		t.Fatalf("旧会话应被顶替：%v", superseded)
	}
	// 旧会话的 Unregister 此时才姗姗来迟：不得影响新会话。
	p.Unregister(old)
	if s, err := p.LookupHost("a.example.com"); err != nil || s != fresh {
		t.Fatalf("新会话不应被旧会话的注销误删：%v %v", s, err)
	}
}

// 并发注册/查找/注销不竞态（-race 下钉住）。
func TestPoolConcurrentAccess(t *testing.T) {
	p := NewPool()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				s := &Session{CredentialID: "c", Name: "A", Hosts: []string{"a.example.com"}, OpenedAt: time.Now()}
				p.Register(s)
				_, _ = p.LookupHost("a.example.com")
				p.Unregister(s)
			}
		}(i)
	}
	wg.Wait()
}

// ── 全内存端到端：握手 → 桥接真流量 → 顶替 ────────────────────────────────

// fakeConsole 记录网关上报，供断言。
type fakeConsole struct {
	*httptest.Server
	mu        sync.Mutex
	keys      []ConsoleKey
	registers []string
	sessions  []SessionReport
}

func newFakeConsole(t *testing.T, keys []ConsoleKey) *fakeConsole {
	t.Helper()
	fc := &fakeConsole{}
	fc.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fc.mu.Lock()
		defer fc.mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/integration/keys":
			out := make([]map[string]any, 0, len(fc.keys))
			for _, k := range fc.keys {
				out = append(out, map[string]any{"id": k.ID, "name": k.Name, "hosts": k.Hosts,
					"key_hash": k.KeyHash, "revoked": k.Revoked})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": out})
		case "/api/v1/integration/register":
			var body struct{ Name string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			fc.registers = append(fc.registers, body.Name)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/v1/integration/sessions":
			var rep SessionReport
			_ = json.NewDecoder(r.Body).Decode(&rep)
			fc.sessions = append(fc.sessions, rep)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	fc.keys = keys
	t.Cleanup(fc.Close)
	return fc
}

func (fc *fakeConsole) waitSession(t *testing.T, online bool) SessionReport {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		fc.mu.Lock()
		for _, rep := range fc.sessions {
			if rep.Online == online && rep.SessionID != "" {
				fc.mu.Unlock()
				return rep
			}
		}
		fc.mu.Unlock()
		select {
		case <-deadline:
			t.Fatalf("超时等待会话上报（online=%v）：%+v", online, fc.sessions)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (fc *fakeConsole) registered() []string {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return append([]string(nil), fc.registers...)
}

// dialConnector 模拟连接器：建立 yamux 客户端会话并送握手，返回会话与控制流。
func dialConnector(t *testing.T, conn net.Conn, key, name string, hosts []string, localAddr string) (*yamux.Session, *yamux.Stream, *connectorv1.HandshakeResponse) {
	t.Helper()
	mux, err := yamux.Client(conn, nil)
	if err != nil {
		t.Fatalf("yamux 客户端失败：%v", err)
	}
	ctrl, err := mux.OpenStream()
	if err != nil {
		t.Fatalf("开控制流失败：%v", err)
	}
	if err := wire.Write(ctrl, &connectorv1.HandshakeRequest{AuthKey: key,
		Meta: &connectorv1.ServiceMeta{Name: name, Hosts: hosts, LocalAddr: localAddr, ConnectorVersion: "test"}}, 10*time.Second); err != nil {
		t.Fatalf("发送握手失败：%v", err)
	}
	resp, err := wire.ReadHandshakeResponse(ctrl, 10*time.Second)
	if err != nil {
		t.Fatalf("读应答失败：%v", err)
	}
	return mux, ctrl, resp
}

func newTestServer(t *testing.T, consoleURL string) (*Server, string, chan net.Conn) {
	t.Helper()
	srv := New(Config{Listen: "test", Bridge: "test", ConsoleURL: consoleURL,
		IntegrationToken: "test-integration-token", GatewayNode: "gw-test",
		KeyRefresh: time.Hour, Logf: func(string, ...any) {}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	conns := make(chan net.Conn, 8)
	go srv.Run(ctx, conns)
	bridgeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.ServeBridge(ctx, bridgeLn) }()
	t.Cleanup(func() { _ = bridgeLn.Close() })
	return srv, bridgeLn.Addr().String(), conns
}

func TestGatewayHandshakeBridgeAndSupersede(t *testing.T) {
	fc := newFakeConsole(t, []ConsoleKey{{ID: "cred-1", Name: "商城",
		Hosts: []string{"shop.example.com"}, KeyHash: hashOf(testKey)}})
	srv, bridgeAddr, conns := newTestServer(t, fc.URL)
	waitKeys(t, srv, 1)

	// ① 错误 key：握手被拒并收到明确错误。
	badC, badS := net.Pipe()
	conns <- badS
	_, _, resp := dialConnector(t, badC, "shc-wrong-key-0000000000000000000000", "商城", []string{"shop.example.com"}, "http://127.0.0.1:8080")
	if resp.Ok || resp.Error == "" {
		t.Fatalf("错误 key 应被拒并带原因：%+v", resp)
	}

	// ② 正常接入：握手 OK → 自动登记 → 桥接真 HTTP 流量。
	c1, s1 := net.Pipe()
	conns <- s1
	mux1, ctrl1, resp := dialConnector(t, c1, testKey, "商城", []string{"shop.example.com"}, "http://127.0.0.1:8080")
	if !resp.Ok || resp.SessionId == "" {
		t.Fatalf("握手应成功：%+v", resp)
	}
	// 连接器侧：accept 数据流并扮演真实业务。
	go func() {
		for {
			stream, err := mux1.Accept()
			if err != nil {
				return
			}
			go echoBusiness(stream)
		}
	}()
	// 桥接：proxy 语义 —— 首行 Host，随后原始 HTTP。
	br, err := net.Dial("tcp", bridgeAddr)
	if err != nil {
		t.Fatal(err)
	}
	req := "GET /health HTTP/1.1\r\nHost: shop.example.com\r\nConnection: close\r\n\r\n"
	if _, err := br.Write([]byte("shop.example.com\n" + req)); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(br)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "200 OK") || !strings.Contains(string(raw), "hello-from-business") {
		t.Fatalf("桥接响应不对：%q", raw)
	}
	// 自动登记与上线事件已到控制台。
	if rep := fc.waitSession(t, true); rep.Name != "商城" || rep.GatewayNode != "gw-test" {
		t.Fatalf("上线上报不对：%+v", rep)
	}
	regs := fc.registered()
	if len(regs) == 0 || regs[0] != "商城" {
		t.Fatalf("自动登记上报缺失：%v", regs)
	}
	_ = ctrl1

	// ③ 同凭证重连：新连接顶替旧连接（旧 ctrl 流被关闭）。
	c2, s2 := net.Pipe()
	conns <- s2
	mux2, ctrl2, resp2 := dialConnector(t, c2, testKey, "商城", []string{"shop.example.com"}, "http://127.0.0.1:8080")
	if !resp2.Ok || resp2.SessionId == resp.SessionId {
		t.Fatalf("重连应是新会话（新代际）：%+v vs %+v", resp2, resp)
	}
	// 旧控制流被关闭（顶替）。
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := ctrl1.Read(make([]byte, 1)); err != nil {
			break // 旧连接已关
		}
		if time.Now().After(deadline) {
			t.Fatal("旧会话应被顶替关闭")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// 新会话可继续桥接。
	go func() {
		for {
			stream, err := mux2.Accept()
			if err != nil {
				return
			}
			go echoBusiness(stream)
		}
	}()
	br2, err := net.Dial("tcp", bridgeAddr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := br2.Write([]byte("shop.example.com\n" + req)); err != nil {
		t.Fatal(err)
	}
	raw2, _ := io.ReadAll(br2)
	if !strings.Contains(string(raw2), "hello-from-business") {
		t.Fatalf("顶替后新会话应继续服务：%q", raw2)
	}
	_ = ctrl2
}

// 桥接的 502 语义与 /healthz。
func TestBridgeErrorsAndHealthz(t *testing.T) {
	fc := newFakeConsole(t, []ConsoleKey{{ID: "cred-1", Name: "商城",
		Hosts: []string{"shop.example.com"}, KeyHash: hashOf(testKey)}})
	_, bridgeAddr, _ := newTestServer(t, fc.URL)

	// 健康检查。
	hc, err := net.Dial("tcp", bridgeAddr)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = hc.Write([]byte("GET /healthz HTTP/1.1\r\nHost: x\r\n\r\n"))
	out, _ := io.ReadAll(hc)
	if !strings.Contains(string(out), "200 OK") {
		t.Fatalf("healthz 应 200：%q", out)
	}

	// 查询模式：proxy 分流判定问「域名是否属于某凭证」——白名单内 OK、外 NO。
	// key 表由网关后台拉取（异步）：轮询等待初次拉取完成。
	queryOK := func() bool {
		q, err := net.Dial("tcp", bridgeAddr)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = q.Write([]byte("?shop.example.com\n"))
		resp, _ := io.ReadAll(q)
		return strings.HasPrefix(string(resp), "OK")
	}
	deadline := time.Now().Add(3 * time.Second)
	for !queryOK() {
		if time.Now().After(deadline) {
			t.Fatal("白名单域名查询应 OK（key 表初次拉取超时）")
		}
		time.Sleep(20 * time.Millisecond)
	}
	q2, err := net.Dial("tcp", bridgeAddr)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = q2.Write([]byte("?none.example.com\n"))
	if resp, _ := io.ReadAll(q2); !strings.HasPrefix(string(resp), "NO") {
		t.Fatalf("未知域名查询应 NO：%q", resp)
	}

	// 未接入的域名 → 502 + 「未接入」说明。
	c, err := net.Dial("tcp", bridgeAddr)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Write([]byte("none.example.com\nGET / HTTP/1.1\r\n\r\n"))
	out, _ = io.ReadAll(c)
	if !strings.Contains(string(out), "502") || !strings.Contains(string(out), "未接入") {
		t.Fatalf("未知域名应 502 且说明原因：%q", out)
	}
}

// echoBusiness 扮演连接器背后的真实业务：读走请求、回一个固定响应。
func echoBusiness(stream net.Conn) {
	defer func() { _ = stream.Close() }()
	buf := make([]byte, 4096)
	_ = stream.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := stream.Read(buf); err != nil && err != io.EOF {
		return
	}
	body := "hello-from-business"
	resp := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n" +
		"Content-Length: " + itoa(len(body)) + "\r\nConnection: close\r\n\r\n" + body
	_ = stream.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, _ = stream.Write([]byte(resp))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// waitKeys 等 key 表首次拉取到位（Run 的 keyLoop 启动即拉，但与拨号之间存在竞态）。
func waitKeys(t *testing.T, srv *Server, n int) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if srv.Keys().Len() == n {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("超时等待 key 表拉取（want=%d got=%d）", n, srv.Keys().Len())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
