package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"shen/common/core/internal/contract"
	"shen/common/core/internal/policy"
	"shen/common/core/internal/store"
)

// fakeConsole 模拟管控台的同步通道。
type fakeConsole struct {
	mu      sync.Mutex
	state   string
	rev     uint64
	overlay policy.Overlay
	down    bool
	reports []consoleReport
	token   string
}

func (f *fakeConsole) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.down {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+f.token {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/integration/deception":
			if f.state == "ok" && r.URL.Query().Get("since_rev") == strconv.FormatUint(f.rev, 10) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			body := map[string]any{"state": f.state}
			if f.state == "ok" {
				proj := map[string]any{"rev": f.rev, "digest": "d"}
				raw, _ := json.Marshal(f.overlay)
				_ = json.Unmarshal(raw, &proj)
				body["projection"] = proj
			}
			_ = json.NewEncoder(w).Encode(body)
		case "/api/v1/integration/deception/report":
			var rep consoleReport
			if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
				t.Errorf("上报不是合法 JSON：%v", err)
			}
			f.reports = append(f.reports, rep)
		default:
			http.NotFound(w, r)
		}
	})
}

func goodOverlay() policy.Overlay {
	return policy.Overlay{
		Honeypots: []policy.OverlayHoneypot{{Name: "mirage-web", Type: "web-clone", Addr: "127.0.0.1:18081", Enabled: true}},
		Decoys: []policy.OverlayDecoy{{ID: "mcp", Kind: "mcp", Path: "/mcp", Hosts: []string{"shop.example.com"},
			Backend: "mirage-web", Enabled: true}},
		Blacklist: []policy.OverlayBlack{{ID: "pay", PathPrefix: "/pay", Reason: "合规"}},
	}
}

type syncFixture struct {
	console *fakeConsole
	srv     *httptest.Server
	base    *policy.Loader
	live    *policy.Live
	stores  *store.MemStores
	sync    *consoleSync
	logs    []string
}

func newSyncFixture(t *testing.T, cache string) *syncFixture {
	t.Helper()
	base, err := policy.Load(strings.NewReader(deceptionConfig))
	if err != nil {
		t.Fatal(err)
	}
	f := &syncFixture{console: &fakeConsole{state: "ok", rev: 7, overlay: goodOverlay(), token: "tok"},
		base: base, live: policy.NewLive(base), stores: store.NewMemStores(nil)}
	f.srv = httptest.NewServer(f.console.handler(t))
	t.Cleanup(f.srv.Close)
	apply := func(next *policy.Loader) error {
		assets, _ := next.Decoys(context.Background())
		if err := f.stores.Decoy.Replace(context.Background(), assets); err != nil {
			return err
		}
		f.live.Swap(next)
		return nil
	}
	f.sync = newConsoleSync(syncConfig{URL: f.srv.URL, Token: "tok", Interval: time.Second, CachePath: cache},
		base, apply, f.stores.Policy.Acks, func() []probeResult { return []probeResult{{Name: "mirage-web", Healthy: true}} })
	f.sync.logf = func(format string, a ...any) { f.logs = append(f.logs, format) }
	return f
}

func (f *syncFixture) version(t *testing.T) uint64 {
	s, err := f.live.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s.Version
}

func TestConsoleSyncTakesOverAndSkipsUnchanged(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "sync.json")
	f := newSyncFixture(t, cache)
	ctx := context.Background()
	if err := f.sync.once(ctx); err != nil {
		t.Fatal(err)
	}
	if v := f.version(t); v != 1*versionStride+7 {
		t.Fatalf("版本应为 基线×1e6+rev：%d", v)
	}
	if ds, _ := f.live.Decoys(ctx); len(ds) != 1 || ds[0].ID != "mcp" {
		t.Fatalf("诱饵应被管控台数据替换：%+v", ds)
	}
	if bl, _ := f.live.Blacklist(ctx); len(bl) != 1 {
		t.Fatalf("黑名单应生效：%+v", bl)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("应写入本地缓存：%v", err)
	}
	// 第二轮：since_rev=7 ⇒ 304，不重复应用。
	before := f.live.Current()
	if err := f.sync.once(ctx); err != nil || f.live.Current() != before {
		t.Fatalf("未变化时不应重新应用：%v", err)
	}
	f.sync.report(ctx)
	if n := len(f.console.reports); n != 1 || !f.console.reports[0].Applied || f.console.reports[0].AppliedRev != 7 ||
		len(f.console.reports[0].Honeypots) != 1 {
		t.Fatalf("上报内容不对：%+v", f.console.reports)
	}
}

func TestConsoleSyncKeepsLastGoodOnInvalidAndOutage(t *testing.T) {
	f := newSyncFixture(t, "")
	ctx := context.Background()
	_ = f.sync.once(ctx)
	good := f.live.Current()

	f.console.mu.Lock()
	f.console.rev = 8
	f.console.overlay.Decoys[0].Kind = "fake_admin" // 非法 ⇒ 核心终检拒绝
	f.console.mu.Unlock()
	if err := f.sync.once(ctx); err != nil {
		t.Fatalf("数据非法不算通道错误：%v", err)
	}
	if f.live.Current() != good {
		t.Fatal("非法数据必须保留 last-good")
	}
	if st := f.sync.snapshot(ctx); st.Applied || st.AppliedRev != 7 || st.Reason == "" {
		t.Fatalf("应上报 applied=false 并保留 r7：%+v", st)
	}
	// 同一坏修订号不会被反复终检：since_rev=8 ⇒ 304。
	if f.sync.seenRev != 8 {
		t.Fatalf("seenRev 应推进到 8：%d", f.sync.seenRev)
	}

	f.console.mu.Lock()
	f.console.down = true
	f.console.mu.Unlock()
	if err := f.sync.once(ctx); err == nil {
		t.Fatal("管控台不可达应返回错误（触发退避）")
	}
	if f.live.Current() != good {
		t.Fatal("不可达时必须保留 last-good")
	}
}

func TestConsoleSyncNotInitializedKeepsFileConfig(t *testing.T) {
	f := newSyncFixture(t, "")
	f.console.state = "not_initialized"
	if err := f.sync.once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.live.Current() != f.base {
		t.Fatal("未初始化时必须沿用部署配置")
	}
	if st := f.sync.snapshot(context.Background()); !st.Applied || st.Source != "file" {
		t.Fatalf("未初始化不算被拒：%+v", st)
	}
}

func TestConsoleSyncRestoresCache(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "sync.json")
	first := newSyncFixture(t, cache)
	_ = first.sync.once(context.Background())

	second := newSyncFixture(t, cache)
	second.console.down = true
	second.sync.restoreCache()
	if v := second.version(t); v != versionStride+7 {
		t.Fatalf("重启后应先用缓存 r7 接管：%d", v)
	}
	// 缓存被篡改 ⇒ 忽略。
	raw, _ := os.ReadFile(cache)
	_ = os.WriteFile(cache, []byte(strings.Replace(string(raw), "shop.example.com", "evil.example.com", 1)), 0o600)
	third := newSyncFixture(t, cache)
	third.sync.restoreCache()
	if third.live.Current() != third.base {
		t.Fatal("校验和不符的缓存必须被忽略")
	}
}

func TestLoadSyncConfig(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if c, err := loadSyncConfig(env(nil)); err != nil || c.URL != "" {
		t.Fatalf("未配置应为关闭：%+v %v", c, err)
	}
	if c, err := loadSyncConfig(env(map[string]string{"SHEN_CORE_CONSOLE_URL": "http://x"})); err != nil || c.URL != "" || c.Note == "" {
		t.Fatalf("缺令牌应关闭同步并点名原因（不阻断启动）：%+v %v", c, err)
	}
	if _, err := loadSyncConfig(env(map[string]string{"SHEN_CORE_CONSOLE_URL": "x", "SHEN_CORE_CONSOLE_TOKEN": "t"})); err == nil {
		t.Fatal("非 http(s) 地址必须报错")
	}
	c, err := loadSyncConfig(env(map[string]string{"SHEN_CORE_CONSOLE_URL": "http://x/", "SHEN_CORE_CONSOLE_TOKEN": "t",
		"SHEN_CORE_CONSOLE_SYNC_INTERVAL": "30s"}))
	if err != nil || c.URL != "http://x" || c.Interval != 30*time.Second {
		t.Fatalf("解析错误：%+v %v", c, err)
	}
}

func TestHealthProberMarksAndLogsChanges(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lis.Close() }()
	backends := []contract.HoneypotBackend{
		{Name: "up", Addr: lis.Addr().String(), Enabled: true},
		{Name: "down", Addr: "127.0.0.1:1", Enabled: true},
		{Name: "off", Addr: "127.0.0.1:1", Enabled: false},
	}
	marked := map[string]bool{}
	var mu sync.Mutex
	p := newHealthProber(func(context.Context) ([]contract.HoneypotBackend, error) { return backends, nil },
		func(_ context.Context, n string, h bool) { mu.Lock(); marked[n] = h; mu.Unlock() })
	var logs []string
	p.logf = func(f string, a ...any) { logs = append(logs, f) }
	p.once(context.Background())
	res := p.results()
	if len(res) != 2 || !marked["up"] || marked["down"] {
		t.Fatalf("只探测启用后端，结果：%+v %+v", res, marked)
	}
	n := len(logs)
	p.once(context.Background())
	if len(logs) != n {
		t.Fatal("状态未变化时不应再写日志")
	}
	if got := dialAddr("https://hp.example.com"); got != "hp.example.com:443" {
		t.Errorf("URL 地址应转成 host:port：%s", got)
	}
}
