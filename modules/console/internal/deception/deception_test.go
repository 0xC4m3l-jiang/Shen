package deception

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func baseDataset() Dataset {
	return Dataset{
		Honeypots: []Honeypot{{Name: "mirage-web", Type: "web-clone", Addr: "127.0.0.1:18081", Enabled: true},
			{Name: "mirage-db", Type: "mysql", Addr: "127.0.0.1:3306"}},
		Decoys: []DecoyAsset{
			{ID: "dev-api", Kind: "developer_api", Path: "/portal/api", Hosts: []string{"shop.example.com"}, Backend: "mirage-web", Enabled: true},
			{ID: "env", Kind: "bait", Path: "/.env", Hosts: []string{"*.example.com"}, Backend: "mirage-web", Enabled: true},
		},
		Whitelist: Whitelist{SourceCIDRs: []string{"10.0.0.0/8"}, UserAgents: []string{"kube-probe"}, PathPrefixes: []string{"/healthz"}},
		Blacklist: []BlackRule{{ID: "pay", PathPrefix: "/pay/callback", Reason: "支付回调"}},
	}.normalizeNil()
}

var services = []ServiceRef{
	{ID: "svc-shop", Name: "shop", Hosts: []string{"shop.example.com"}, Enabled: true},
	{ID: "svc-blog", Name: "blog", Hosts: []string{"blog.example.org"}, Enabled: true},
}

func hasErr(r Report, field, sub string) bool {
	for _, e := range r.Errors {
		if strings.HasPrefix(e.Field, field) && strings.Contains(e.Reason, sub) {
			return true
		}
	}
	return false
}

func TestValidateAcceptsBaseline(t *testing.T) {
	if r := Validate(baseDataset(), services); !r.OK() {
		t.Fatalf("基线数据应通过：%+v", r.Errors)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name, field, sub string
		mut              func(*Dataset)
	}{
		{"未登记蜜罐类型", "honeypots[0].type", "未登记", func(d *Dataset) { d.Honeypots[0].Type = "tomcat" }},
		{"蜜罐地址非法", "honeypots[0].addr", "host:port", func(d *Dataset) { d.Honeypots[0].Addr = "nohost" }},
		{"诱饵形态非法", "decoys[0].kind", "未登记", func(d *Dataset) { d.Decoys[0].Kind = "fake_admin" }},
		{"路径非归一化", "decoys[0].path", "归一化", func(d *Dataset) { d.Decoys[0].Path = "/portal//api" }},
		{"裸星号", "decoys[0].hosts[0]", "裸 *", func(d *Dataset) { d.Decoys[0].Hosts = []string{"*"} }},
		{"后端不存在", "decoys[0].backend", "不在蜜罐池", func(d *Dataset) { d.Decoys[0].Backend = "nope" }},
		{"后端未启用", "decoys[0].backend", "未启用", func(d *Dataset) { d.Decoys[0].Backend = "mirage-db" }},
		{"嵌套路由且主机相交", "decoys[1].path", "嵌套路由", func(d *Dataset) { d.Decoys[1].Path = "/portal/api/v2" }},
		{"诱饵落在黑名单下", "decoys[0].path", "禁止欺骗", func(d *Dataset) { d.Decoys[0].Path = "/pay/callback/x" }},
		{"CIDR 非法", "whitelist.source_cidrs[0]", "CIDR", func(d *Dataset) { d.Whitelist.SourceCIDRs[0] = "10.0.0.0/40" }},
		{"黑名单缺原因", "blacklist[0].reason", "原因", func(d *Dataset) { d.Blacklist[0].Reason = "" }},
		{"注入片段空", "injects[0].snippet", "不能为空", func(d *Dataset) { d.Injects = []Inject{{Kind: "dataset"}} }},
		{"绑定未登记服务", "bindings[0].service_id", "未登记", func(d *Dataset) { d.Bindings = []Binding{{ServiceID: "svc-x"}} }},
		{"通配覆盖已绑定服务", "decoys[1].hosts", "不可表达", func(d *Dataset) {
			d.Bindings = []Binding{{ServiceID: "svc-shop", DecoyIDs: []string{"dev-api"}}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := baseDataset().Clone()
			c.mut(&d)
			if r := Validate(d, services); !hasErr(r, c.field, c.sub) {
				t.Fatalf("应在 %s 报「%s」：%+v", c.field, c.sub, r.Errors)
			}
		})
	}
}

func TestValidateWarnsWhitelistCoversDecoy(t *testing.T) {
	d := baseDataset()
	d.Whitelist.PathPrefixes = []string{"/portal"}
	r := Validate(d, services)
	if !r.OK() || len(r.Warnings) == 0 {
		t.Fatalf("应为警告而非错误：%+v", r)
	}
}

func TestProjectReplacementSemantics(t *testing.T) {
	d := baseDataset()
	d.Decoys[1].Hosts = []string{"shop.example.com", "blog.example.org"} // 精确主机：可表达
	d.Decoys = append(d.Decoys, DecoyAsset{ID: "mcp", Kind: "mcp", Path: "/mcp", Hosts: []string{"ops.example.net"}, Backend: "mirage-web", Enabled: true})
	d.Bindings = []Binding{{ServiceID: "svc-shop", DecoyIDs: []string{"mcp"}}}
	if r := Validate(d, services); !r.OK() {
		t.Fatalf("应通过：%+v", r.Errors)
	}
	p, _ := Project(d, services)
	hosts := map[string][]string{}
	for _, a := range p.Decoys {
		hosts[a.ID] = a.Hosts
	}
	// 被绑定的 mcp：自身主机 ∪ shop 的主机。
	if got := strings.Join(hosts["mcp"], ","); got != "ops.example.net,shop.example.com" {
		t.Errorf("绑定资产应并上服务主机：%s", got)
	}
	// 未绑定的 env：去掉 shop 的主机，只剩 blog。
	if got := strings.Join(hosts["env"], ","); got != "blog.example.org" {
		t.Errorf("未绑定资产应减去已绑定服务的主机：%s", got)
	}
	// dev-api 只声明了 shop.example.com ⇒ 投影后无主机 ⇒ 剔除。
	if _, ok := hosts["dev-api"]; ok {
		t.Errorf("主机被全部减掉的资产应从投影中剔除")
	}
	// 摘要确定性。
	if p2, _ := Project(d, services); p2.Digest != p.Digest {
		t.Error("同内容必得同摘要")
	}
}

func TestStoreSaveConflictHistoryRollback(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.Save(0, "alice", "初始", baseDataset())
	if err != nil || v1.Version != 1 || !v1.Initialized {
		t.Fatalf("首存：%+v %v", v1, err)
	}
	if _, err := s.Save(0, "bob", "", baseDataset()); !errors.Is(err, ErrConflict) {
		t.Fatalf("过期版本必须 409：%v", err)
	}
	d2 := v1.Clone()
	d2.Decoys = d2.Decoys[:1]
	v2, err := s.Save(1, "bob", DiffSummary(v1, d2), d2)
	if err != nil || v2.Version != 2 || !strings.Contains(v2.Summary, "诱饵 -1") {
		t.Fatalf("二存：%+v %v", v2.Summary, err)
	}
	v3, err := s.Rollback(1, 2, "admin")
	if err != nil || v3.Version != 3 || len(v3.Decoys) != 2 {
		t.Fatalf("回滚应生成 v3 且恢复 2 条诱饵：%+v %v", v3.Version, err)
	}
	if h := s.History(); len(h) != 3 || h[0].Version != 3 {
		t.Fatalf("时间线应为 v3,v2,v1：%+v", h)
	}
	// 重新打开：状态与历史都在。
	s2, err := Open(dir, nil)
	if err != nil || s2.Get().Version != 3 || len(s2.History()) != 3 {
		t.Fatalf("重开后状态丢失：%v", err)
	}
	if _, err := s2.VersionAt(99); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在的版本应 ErrNotFound：%v", err)
	}
}

func TestStoreProjectionRevMonotonic(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	r1, ch, _ := s.EnsureProjection("a")
	r2, ch2, _ := s.EnsureProjection("a")
	r3, _, _ := s.EnsureProjection("b")
	r4, _, _ := s.EnsureProjection("a")
	if r1 != 1 || !ch || r2 != 1 || ch2 || r3 != 2 || r4 != 3 {
		t.Fatalf("修订号必须只增、同摘要零写入：%d %v %d %v %d %d", r1, ch, r2, ch2, r3, r4)
	}
}

func TestStoreHistoryPruned(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	for i := uint64(0); i < MaxHistory+5; i++ {
		if _, err := s.Save(i, "x", "", baseDataset()); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(s.History()); n != MaxHistory+1 {
		t.Fatalf("历史应保留 %d 个旧版本 + 当前：%d", MaxHistory, n)
	}
}

func TestStoreConcurrentSave(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	var wg sync.WaitGroup
	var ok, conflict int
	var mu sync.Mutex
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Save(0, "x", "", baseDataset())
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				ok++
			} else if errors.Is(err, ErrConflict) {
				conflict++
			}
			_ = s.Get()
		}()
	}
	wg.Wait()
	if ok != 1 || conflict != 15 {
		t.Fatalf("同一版本并发保存只能成功一次：ok=%d conflict=%d", ok, conflict)
	}
}

func TestOpenCorruptFails(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "dataset.json"), []byte("{bad"), 0o600)
	if _, err := Open(dir, nil); err == nil || !strings.Contains(err.Error(), "history") {
		t.Fatalf("损坏文件必须启动失败并点名历史目录：%v", err)
	}
}

const seedYAML = `core: {listen: "127.0.0.1:9443"}
honeypots:
  - {name: "mirage", type: "web-clone", addr: "127.0.0.1:18081", enabled: true}
decoys:
  assets:
    - {id: "dev-api", kind: "developer_api", path: "/portal/api", hosts: ["a.example.com"], backend: "mirage", enabled: true}
whitelist: {source_cidrs: ["10.0.0.0/8"], user_agents: [], path_prefixes: []}
injects: []
`

func TestAutoSeedAndDrift(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(cfg, []byte(seedYAML), 0o600)
	s, _ := Open(filepath.Join(dir, "data"), nil)

	out := AutoSeed(s, cfg)
	if !out.Seeded || !out.Initialized {
		t.Fatalf("首次启动应自动初始化：%+v", out)
	}
	d := s.Get()
	if len(d.Honeypots) != 1 || len(d.Decoys) != 1 || !d.InjectsProvided || d.Version != 1 {
		t.Fatalf("初始化内容不对：%+v", d)
	}
	if again := AutoSeed(s, cfg); again.Seeded || again.Drifted {
		t.Fatalf("已接管后不再初始化、未改动不算漂移：%+v", again)
	}
	_ = os.WriteFile(cfg, []byte(strings.Replace(seedYAML, "10.0.0.0/8", "10.9.0.0/16", 1)), 0o600)
	if drift := AutoSeed(s, cfg); !drift.Drifted {
		t.Fatalf("部署配置改动应报漂移：%+v", drift)
	}
	if miss := AutoSeed(s, filepath.Join(dir, "nope.yaml")); !miss.Missing {
		t.Fatalf("找不到文件应标记 missing：%+v", miss)
	}
}

func TestTemplatesLoad(t *testing.T) {
	tp, err := LoadTemplates()
	if err != nil || len(tp.Honeypots) != len(HoneypotTypes) || len(tp.Decoys) == 0 || len(tp.Injects) != len(InjectKinds) {
		t.Fatalf("内置模板：%v %d/%d/%d", err, len(tp.Honeypots), len(tp.Decoys), len(tp.Injects))
	}
}

func TestSyncStatusAndAlerts(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	ss := NewSyncState(true, clock)
	d := baseDataset()
	d.Initialized, d.ProjectionRev = true, 5

	if st := ss.Status(5); st.State != "offline" {
		t.Fatalf("未上报应为 offline：%s", st.State)
	}
	ss.NoteRev(5)
	ss.RecordReport(CoreReport{AppliedRev: 5, PolicyVersion: 3_000_005, Applied: true, Source: "console",
		EdgeAcks:  []EdgeAck{{AdapterID: "proxy-1", Version: 3_000_005, Applied: true, ReceivedAt: now}},
		Honeypots: []HoneypotProbe{{Name: "mirage-web", Healthy: true}}})
	if st := ss.Status(5); st.State != "synced" || st.EdgeInSync != 1 {
		t.Fatalf("应为 synced：%+v", st)
	}
	changed := ss.RecordReport(CoreReport{AppliedRev: 5, PolicyVersion: 3_000_005, Applied: false, Reason: "终检失败",
		Honeypots: []HoneypotProbe{{Name: "mirage-web", Healthy: false, Error: "connection refused"}}})
	if !changed.AppliedChanged || !(len(changed.Health) == 1 && !changed.Health["mirage-web"]) {
		t.Errorf("健康变化应被报告：%+v", changed)
	}
	ids := map[string]string{}
	for _, a := range ss.Alerts(d, SeedOutcome{}) {
		ids[a.ID] = a.Severity
	}
	if ids["merge_rejected"] != "critical" || ids["honeypot_unhealthy:mirage-web"] != "critical" {
		t.Fatalf("应出现合并被拒与被引用蜜罐不健康（严重）：%+v", ids)
	}
	now = now.Add(2 * time.Minute)
	if st := ss.Status(5); st.State != "offline" {
		t.Fatalf("上报过期应为 offline：%s", st.State)
	}
	if a := NewSyncState(false, clock).Alerts(d, SeedOutcome{}); len(a) != 1 || a[0].ID != "core_sync_unconfigured" {
		t.Fatalf("未配置应只报未启用：%+v", a)
	}
}
