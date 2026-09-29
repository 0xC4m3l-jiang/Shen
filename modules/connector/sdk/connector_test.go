package connector

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"shen/modules/connector/gateway"
)

// 测试假密钥：shc- 前缀不命中 secrets-check 规则①（sk- 形态）。
const testKey = "shc-live-0123456789abcdef0123456789abcdef"

func testKeyHash(t *testing.T) string {
	t.Helper()
	sum := sha256.Sum256([]byte(testKey))
	return hex.EncodeToString(sum[:])
}

// 全内存端到端：真网关（真 TLS 监听）+ SDK + 假控制台 + 本地业务；断线自动重连。
func TestSDKConnectServeAndReconnect(t *testing.T) {
	// 本地真实业务（业务侧只听回环：零入站暴露的模拟）。
	business := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "hello-from-business")
	}))
	defer business.Close()
	bizAddr := business.Listener.Addr().String()

	// 假控制台 + 真网关（真 TLS 自签证书）。
	fc := newFakeConsole(t, "商城", []string{"shop.example.com"}, testKeyHash(t))
	srv, tlsAddr, bridgeAddr := newTLSGateway(t, fc.URL)

	// SDK 接入（自签证书：跳过校验，仅测试）。
	c, err := Connect(context.Background(), Config{
		Gateway: tlsAddr, AuthKey: testKey, Upstream: "http://" + bizAddr,
		Name: "商城", Hosts: []string{"shop.example.com"},
		TLS:        &tls.Config{InsecureSkipVerify: true},
		MinBackoff: 30 * time.Millisecond, MaxBackoff: 100 * time.Millisecond,
		Logf: func(f string, a ...any) { t.Logf("sdk: "+f, a...) },
	})
	if err != nil {
		t.Fatalf("首连失败：%v", err)
	}
	defer func() { _ = c.Close() }()

	// ① 经网关桥发请求 → 穿隧道 → 到达本地业务。
	if got := bridgeGet(t, bridgeAddr, "shop.example.com"); !strings.Contains(got, "hello-from-business") {
		t.Fatalf("桥接响应不对：%q", got)
	}
	// ② 网关已按 Host 登记会话。
	if s, err := srv.Pool().LookupHost("shop.example.com"); err != nil || s.Name != "商城" {
		t.Fatalf("网关会话路由不对：%v %v", s, err)
	}

	// ③ 掐断当前会话（模拟网络中断）→ SDK 自动重连 → 新代际会话上线 → 继续服务。
	var oldSession string
	for _, s := range srv.Pool().List() {
		oldSession = s.ID
		_ = s.Close()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		online := false
		for _, s := range srv.Pool().List() {
			if s.ID != oldSession && s.Name == "商城" {
				online = true
			}
		}
		if online {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("SDK 未在期限内自动重连")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := bridgeGet(t, bridgeAddr, "shop.example.com"); !strings.Contains(got, "hello-from-business") {
		t.Fatalf("重连后桥接响应不对：%q", got)
	}
}

// 首连被拒（凭证无效）应立即返回错误而不是无限重试。
func TestSDKFatalAuthError(t *testing.T) {
	fc := newFakeConsole(t, "商城", []string{"shop.example.com"}, testKeyHash(t))
	_, _, _ = newTLSGateway(t, fc.URL)
	_, err := Connect(context.Background(), Config{
		Gateway: "gw.invalid:9446", AuthKey: "shc-wrong-key-000000000000000000000000",
		Upstream: "http://127.0.0.1:1", Name: "商城", Hosts: []string{"shop.example.com"},
	})
	if err == nil {
		t.Fatal("错误凭证应首连即败")
	}
}

// 退避计算：单调不减、封顶、带抖动范围。
func TestNextBackoff(t *testing.T) {
	base := time.Second
	max := 30 * time.Second
	for i := 0; i < 100; i++ {
		got := nextBackoff(base, max)
		if got < 500*time.Millisecond || got > 45*time.Second {
			t.Fatalf("退避越界（0.5×下限–1.5×上限）：%v", got)
		}
	}
	if nextBackoff(max, max) > 45*time.Second {
		t.Fatal("封顶后不应超过 1.5×上限")
	}
}

// ── 测试基建 ────────────────────────────────────────────────────────────────

// bridgeGet 经网关桥发一次 HTTP GET（proxy 语义：首行 Host + 原始 HTTP）。
func bridgeGet(t *testing.T, bridgeAddr, host string) string {
	t.Helper()
	conn, err := net.Dial("tcp", bridgeAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	req := fmt.Sprintf("%s\nGET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", host, host)
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	n, _ := conn.Read(buf)
	return string(buf[:n])
}

// fakeConsole：网关集成面的替身（key 表 + 登记与会话上报记数）。
type fakeConsole struct {
	*httptest.Server
	mu   sync.Mutex
	keys []map[string]any
}

func newFakeConsole(t *testing.T, name string, hosts []string, keyHash string) *fakeConsole {
	t.Helper()
	fc := &fakeConsole{keys: []map[string]any{{
		"id": "cred-1", "name": name, "hosts": hosts, "key_hash": keyHash, "revoked": false,
	}}}
	fc.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/integration/keys":
			fc.mu.Lock()
			defer fc.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": fc.keys})
		case "/api/v1/integration/register", "/api/v1/integration/sessions":
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(fc.Close)
	return fc
}

// newTLSGateway 起真网关：TLS 监听（自签证书）+ 回环桥。
func newTLSGateway(t *testing.T, consoleURL string) (*gateway.Server, string, string) {
	t.Helper()
	cert := selfSignedCert(t)
	srv := gateway.New(gateway.Config{ConsoleURL: consoleURL,
		IntegrationToken: "test-integration-token", GatewayNode: "gw-test",
		KeyRefresh: time.Hour, Logf: func(f string, a ...any) { t.Logf("gateway: "+f, a...) }})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	conns := make(chan net.Conn, 8)
	go srv.Run(ctx, conns)

	tlsLn, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tlsLn.Close() })
	go func() {
		for {
			conn, err := tlsLn.Accept()
			if err != nil {
				return
			}
			conns <- conn
		}
	}()

	bridgeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.ServeBridge(ctx, bridgeLn) }()
	t.Cleanup(func() { _ = bridgeLn.Close() })
	return srv, tlsLn.Addr().String(), bridgeLn.Addr().String()
}

// selfSignedCert 生成测试用自签证书（ECDSA P-256，一分钟有效即够测试用）。
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "shen-gateway-test"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
