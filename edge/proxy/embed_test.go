package proxy

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"google.golang.org/grpc"

	judgev1 "shen/api/judge/v1"
	telemetryv1 "shen/api/telemetry/v1"
)

// ── 集成测试（= spike）：内嵌 Caddy + 每请求动态后端 + 响应注入 + TLS 握手 + mirage→origin 回落 ──
//
// 这是「换底座」的 go/no-go 判据落地：用真实 Caddy + 真实 reverse_proxy + 真实 gRPC 往返，
// 一次性验证四件事都可靠表达。Caddy 是进程级全局单例，本测试必须独占（不 t.Parallel）。

// testJudgeServer 是按路径路由的替身判定面（真实 gRPC 服务）。
type testJudgeServer struct {
	judgev1.UnimplementedDeceptionJudgeServer
}

func (s *testJudgeServer) Judge(_ context.Context, req *judgev1.JudgeRequest) (*judgev1.JudgeResponse, error) {
	path := req.GetObserved().GetPath()
	switch {
	case strings.Contains(path, "block"):
		return &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_BLOCK}, nil
	case strings.Contains(path, "dead"):
		return &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_MIRAGE, Backend: "dead"}, nil
	case strings.Contains(path, "mirage"):
		return &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_MIRAGE, Backend: "hp"}, nil
	default:
		return &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}, nil
	}
}

type testTelemetryServer struct {
	telemetryv1.UnimplementedDeceptionTelemetryServer
}

func (s *testTelemetryServer) Report(_ context.Context, _ *telemetryv1.TelemetryEvent) (*telemetryv1.ReportAck, error) {
	return &telemetryv1.ReportAck{Accepted: 1}, nil
}

func (s *testTelemetryServer) ReportBatch(_ context.Context, _ *telemetryv1.TelemetryBatch) (*telemetryv1.ReportAck, error) {
	return &telemetryv1.ReportAck{Accepted: 1}, nil
}

// startJudgeServer 起一个明文 gRPC 判定/遥测面，返回地址。
func startJudgeServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败：%v", err)
	}
	srv := grpc.NewServer()
	judgev1.RegisterDeceptionJudgeServer(srv, &testJudgeServer{})
	telemetryv1.RegisterDeceptionTelemetryServer(srv, &testTelemetryServer{})
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(srv.Stop)
	return ln.Addr().String()
}

// htmlServer 起一个返回 HTML 的测试后端（带可辨识的身份头与体）。
func htmlServer(t *testing.T, name string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Backend", name)
		_, _ = w.Write([]byte("<html><body>" + name + "-body</body></html>"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// freePort 取一个空闲端口（Caddy 要绑定它）。
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("取端口失败：%v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return strconv.Itoa(port)
}

// genSelfSignedCert 生成自签证书与私钥，写到 dir，返回 (cert, key) 路径。
func genSelfSignedCert(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成私钥失败：%v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("生成证书失败：%v", err)
	}
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	certOut, err := os.Create(certPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	_ = certOut.Close()

	keyOut, err := os.Create(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	_ = keyOut.Close()

	return certPath, keyPath
}

func TestEmbeddedCaddyEndToEnd(t *testing.T) {
	judgeAddr := startJudgeServer(t)
	origin := htmlServer(t, "origin")
	hp := htmlServer(t, "hp")

	// 一个「立刻关掉」的后端，模拟引流后端不可达（NI-1 回落）。
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	certFile, keyFile := genSelfSignedCert(t, t.TempDir())

	h := &Handler{
		Upstream:        origin.URL,
		Mirage:          map[string]string{"hp": hp.URL, "dead": deadURL},
		DecisionTimeout: caddy.Duration(3 * time.Millisecond),
		CacheTTL:        caddy.Duration(time.Minute),
		Window:          caddy.Duration(time.Minute),
		ReportQueue:     8,
		CoreAddr:        judgeAddr,
		Inject:          []string{"<!--DECOY-->"},
	}

	port := freePort(t)
	cfg, err := BuildConfig(Options{
		Listen:  "127.0.0.1:" + port,
		Handler: h,
		TLS:     TLSConfig{Mode: TLSModeManual, CertFile: certFile, KeyFile: keyFile},
	})
	if err != nil {
		t.Fatalf("BuildConfig 失败：%v", err)
	}

	if err := caddy.Run(cfg); err != nil {
		t.Fatalf("caddy.Run 失败：%v", err)
	}
	t.Cleanup(func() { _ = caddy.Stop() })

	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	base := "https://127.0.0.1:" + port

	get := func(path string) (int, string, http.Header) {
		t.Helper()
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s 失败：%v", path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body), resp.Header
	}

	// ① route_origin：透传业务，不注入。
	code, body, hdr := get("/")
	if code != http.StatusOK {
		t.Fatalf("origin 应 200，实际 %d", code)
	}
	if !strings.Contains(body, "origin-body") {
		t.Errorf("origin 应透传业务体，得到 %q", body)
	}
	if strings.Contains(body, "<!--DECOY-->") {
		t.Error("业务侧响应**禁止**注入（INT-8）")
	}
	if got := hdr.Get("X-Backend"); got != "origin" {
		t.Errorf("应到 origin，实际 X-Backend=%q", got)
	}

	// ② route_mirage：改道 hp，且只改 mirage 侧（注入诱饵）。
	code, body, hdr = get("/mirage")
	if code != http.StatusOK {
		t.Fatalf("mirage 应 200，实际 %d", code)
	}
	if !strings.Contains(body, "hp-body") {
		t.Errorf("mirage 应改道 hp，得到 %q", body)
	}
	if !strings.Contains(body, "<!--DECOY-->") {
		t.Errorf("引流侧 HTML 应注入诱饵，得到 %q", body)
	}
	if got := hdr.Get("X-Backend"); got != "hp" {
		t.Errorf("应到 hp，实际 X-Backend=%q", got)
	}

	// ③ 引流后端不可达 → 回落业务（NI-1，最关键一条）。
	code, body, hdr = get("/dead")
	if code != http.StatusOK {
		t.Fatalf("回落应 200，实际 %d", code)
	}
	if !strings.Contains(body, "origin-body") {
		t.Errorf("引流后端不可达必须回落业务，得到 %q", body)
	}
	if got := hdr.Get("X-Backend"); got != "origin" {
		t.Errorf("回落应到 origin，实际 X-Backend=%q", got)
	}

	// ④ block：短路 403。
	code, _, _ = get("/block")
	if code != http.StatusForbidden {
		t.Errorf("block 应 403，实际 %d", code)
	}
}

// TestBuildConfigDisablesAdminAndAutosave 锁定两条硬化：
// ① admin API 禁用；② **配置自动保存禁用** —— 否则 Caddy 会把整份配置写进
// $XDG_DATA_HOME/caddy/autosave.json，在只读根文件系统的边车/容器里就是多余写入。
func TestBuildConfigDisablesAdminAndAutosave(t *testing.T) {
	cfg, err := BuildConfig(Options{Listen: "127.0.0.1:8081", Handler: &Handler{}, TLS: TLSConfig{Mode: TLSModeOff}})
	if err != nil {
		t.Fatalf("BuildConfig 失败：%v", err)
	}
	if cfg.Admin == nil || !cfg.Admin.Disabled {
		t.Error("admin API 必须禁用（缩攻击面）")
	}
	if cfg.Admin.Config == nil || cfg.Admin.Config.Persist == nil || *cfg.Admin.Config.Persist {
		t.Error("配置自动保存必须禁用（不得向容器/家目录写 autosave.json）")
	}
}

// TestForwardingSemanticsWithTLS 锁定换底座后**对业务不可见**的转发语义（`INT-7` / `INT-23` / `INT-8`）：
//
//   - `Host` 原样传递（业务虚主机路由不能被我们改掉）；
//   - `X-Forwarded-For` 带真实客户端 IP，`X-Forwarded-Proto=https`（TLS 由本进程终结后，
//     业务侧必须能看出外面是 HTTPS，否则会生成 http 链接 / 重定向环）；
//   - 请求方法 / 查询串 / 请求体原样；
//   - 业务侧响应（状态码 / 自定义头 / 体）原样透传 —— **不得**注入（`INT-8`）。
func TestForwardingSemanticsWithTLS(t *testing.T) {
	certFile, keyFile := genSelfSignedCert(t, t.TempDir())
	judgeAddr := startJudgeServer(t)

	type seen struct {
		host, xff, xfp, xfh, method, query, body string
	}
	got := make(chan seen, 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- seen{
			host:   r.Host,
			xff:    r.Header.Get("X-Forwarded-For"),
			xfp:    r.Header.Get("X-Forwarded-Proto"),
			xfh:    r.Header.Get("X-Forwarded-Host"),
			method: r.Method,
			query:  r.URL.RawQuery,
			body:   string(body),
		}
		w.Header().Set("X-Origin-Marker", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ORIGIN-PAYLOAD"))
	}))
	t.Cleanup(origin.Close)

	h := &Handler{
		Upstream:        origin.URL,
		DecisionTimeout: caddy.Duration(3 * time.Millisecond),
		CacheTTL:        caddy.Duration(time.Minute),
		Window:          caddy.Duration(time.Minute),
		ReportQueue:     8,
		CoreAddr:        judgeAddr,
		Inject:          []string{"<!--DECOY-->"},
	}
	port := freePort(t)
	cfg, err := BuildConfig(Options{
		Listen:  "127.0.0.1:" + port,
		Handler: h,
		TLS:     TLSConfig{Mode: TLSModeManual, CertFile: certFile, KeyFile: keyFile},
	})
	if err != nil {
		t.Fatalf("BuildConfig 失败：%v", err)
	}
	if err := caddy.Run(cfg); err != nil {
		t.Fatalf("caddy.Run 失败：%v", err)
	}
	t.Cleanup(func() { _ = caddy.Stop() })

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	req, err := http.NewRequest(http.MethodPut,
		"https://127.0.0.1:"+port+"/api/items?page=2&size=10", strings.NewReader("PAYLOAD"))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "shop.example.com" // 业务虚主机：必须原样到达业务
	req.Header.Set("Cookie", "sid=abc")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)

	s := <-got
	if s.host != "shop.example.com" {
		t.Errorf("Host 必须原样传递（INT-7），实际 %q", s.host)
	}
	if s.xff == "" {
		t.Error("业务侧必须看得到来源 IP（INT-23）：X-Forwarded-For 为空")
	}
	if s.xfp != "https" {
		t.Errorf("TLS 由本进程终结，X-Forwarded-Proto 应为 https（INT-22/INT-23），实际 %q", s.xfp)
	}
	if s.xfh != "shop.example.com" {
		t.Errorf("X-Forwarded-Host 应为原始 Host，实际 %q", s.xfh)
	}
	if s.method != http.MethodPut || s.query != "page=2&size=10" || s.body != "PAYLOAD" {
		t.Errorf("方法/查询串/体必须原样：method=%s query=%q body=%q", s.method, s.query, s.body)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("业务状态码必须原样透传，实际 %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Origin-Marker") != "yes" {
		t.Error("业务侧响应头必须原样透传")
	}
	if string(respBody) != "ORIGIN-PAYLOAD" {
		t.Errorf("业务侧响应体**禁止**改写（INT-8），实际 %q", respBody)
	}
}

// TestTLSConfigValidate 覆盖 SHEN_PROXY_TLS_MODE 的三种取值与非法值
// —— 配置错误必须在启动前报出来，不能等到第一个请求。
func TestTLSConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     TLSConfig
		wantErr bool
	}{
		{name: "off 无需其它字段", cfg: TLSConfig{Mode: TLSModeOff}},
		{name: "manual 带证书", cfg: TLSConfig{Mode: TLSModeManual, CertFile: "/tmp/c.pem", KeyFile: "/tmp/k.pem"}},
		{name: "manual 缺证书", cfg: TLSConfig{Mode: TLSModeManual}, wantErr: true},
		{name: "manual 只有私钥", cfg: TLSConfig{Mode: TLSModeManual, KeyFile: "/tmp/k.pem"}, wantErr: true},
		{name: "acme 带域名", cfg: TLSConfig{Mode: TLSModeACME, Domain: "shop.example.com"}},
		{name: "acme 缺域名", cfg: TLSConfig{Mode: TLSModeACME}, wantErr: true},
		{name: "非法取值", cfg: TLSConfig{Mode: TLSMode("tls1.3")}, wantErr: true},
	}
	for _, tc := range cases {
		err := tc.cfg.Validate()
		if tc.wantErr && err == nil {
			t.Errorf("%s：应当报错，实际通过", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s：不应报错，实际 %v", tc.name, err)
		}
	}
}

// TestBuildConfigRejectsBadOptions 覆盖进程级装配的两个前置校验：
// 监听地址为空、TLS 模式非法 —— 都要在 caddy.Run 之前失败。
func TestBuildConfigRejectsBadOptions(t *testing.T) {
	if _, err := BuildConfig(Options{Handler: &Handler{}, TLS: TLSConfig{Mode: TLSModeOff}}); err == nil {
		t.Error("Listen 为空应当报错")
	}
	if _, err := BuildConfig(Options{Listen: ":8081", Handler: &Handler{}, TLS: TLSConfig{Mode: TLSMode("bogus")}}); err == nil {
		t.Error("非法 tls_mode 应当报错")
	}
	if _, err := BuildConfig(Options{Listen: ":8081", Handler: &Handler{}, TLS: TLSConfig{Mode: TLSModeManual}}); err == nil {
		t.Error("manual 缺证书应当报错")
	}
}
