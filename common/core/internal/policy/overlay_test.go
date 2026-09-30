package policy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	policyv1 "shen/common/api/policy/v1"
	"shen/common/core/internal/store"
)

func sampleOverlay() Overlay {
	return Overlay{
		Honeypots: []OverlayHoneypot{
			{Name: "hp-web", Type: "web-clone", Addr: "127.0.0.1:18081", Enabled: true},
			{Name: "hp-db", Type: "mysql", Addr: "127.0.0.1:3306", Enabled: false},
		},
		Decoys: []OverlayDecoy{
			{ID: "d-api", Kind: "developer_api", Path: "/internal/api", Hosts: []string{"app.example.com"},
				Content: "dev-portal", Backend: "hp-web", Enabled: true},
		},
		Whitelist: OverlayWhitelist{SourceCIDRs: []string{"10.1.0.0/16"}, UserAgents: []string{"probe"}, PathPrefixes: []string{"/healthz"}},
		Blacklist: []OverlayBlack{{ID: "pay", PathPrefix: "/pay/callback", Reason: "支付回调，合规要求不欺骗"}},
	}
}

func TestWithOverlayReplacesDomainsAndVersion(t *testing.T) {
	base := mustLoad(t, validYAML)
	got, err := base.WithOverlay(sampleOverlay(), 3_000_007)
	if err != nil {
		t.Fatalf("覆盖应当成功：%v", err)
	}
	ctx := context.Background()
	snap, _ := got.Snapshot(ctx)
	if snap.Version != 3_000_007 {
		t.Errorf("版本应被改写为 3000007，得到 %d", snap.Version)
	}
	if base.BaseVersion() != 3 || got.BaseVersion() != 3 {
		t.Errorf("基线版本必须保持部署配置里的 3")
	}
	hps, _ := got.Honeypots(ctx)
	if len(hps) != 2 || hps[0].Name != "hp-web" {
		t.Errorf("蜜罐池未被覆盖：%+v", hps)
	}
	ds, _ := got.Decoys(ctx)
	if len(ds) != 1 || ds[0].Backend != "hp-web" || ds[0].Hosts[0] != "app.example.com" {
		t.Errorf("诱饵未被覆盖：%+v", ds)
	}
	bl, _ := got.Blacklist(ctx)
	if len(bl) != 1 || bl[0].PathPrefix != "/pay/callback" {
		t.Errorf("黑名单未生效：%+v", bl)
	}
	wl, _ := got.Whitelist(ctx)
	if len(wl.SourceCIDRs) != 1 || wl.SourceCIDRs[0].String() != "10.1.0.0/16" {
		t.Errorf("白名单未被覆盖：%+v", wl)
	}
	// 规则 / 阈值只来自部署配置（红线：管控台不下发判定策略）。
	if len(snap.Rules) != 2 {
		t.Errorf("规则集必须保持部署配置的 2 条，得到 %d", len(snap.Rules))
	}
	// 原 Loader 不受影响（不可变）。
	if s0, _ := base.Snapshot(ctx); s0.Version != 3 {
		t.Errorf("基线 Loader 被改写了：version=%d", s0.Version)
	}
	// 覆盖可以层层重做但永远基于基线：再套一次得到的是同样的结果（不叠加）。
	again, err := got.WithOverlay(sampleOverlay(), 3_000_008)
	if err != nil {
		t.Fatal(err)
	}
	if ds2, _ := again.Decoys(ctx); len(ds2) != 1 {
		t.Errorf("二次覆盖不应叠加：%d", len(ds2))
	}
}

func TestWithOverlayRejectsInvalid(t *testing.T) {
	base := mustLoad(t, validYAML)
	cases := map[string]func(o *Overlay){
		"非法诱饵形态":   func(o *Overlay) { o.Decoys[0].Kind = "fake_admin" },
		"后端不存在":    func(o *Overlay) { o.Decoys[0].Backend = "nope" },
		"后端未启用":    func(o *Overlay) { o.Decoys[0].Backend = "hp-db" },
		"启用诱饵缺主机":  func(o *Overlay) { o.Decoys[0].Hosts = nil },
		"裸星号主机":    func(o *Overlay) { o.Decoys[0].Hosts = []string{"*"} },
		"CIDR 非法":  func(o *Overlay) { o.Whitelist.SourceCIDRs = []string{"10.0.0.0/33"} },
		"黑名单缺原因":   func(o *Overlay) { o.Blacklist[0].Reason = " " },
		"诱饵落在黑名单下": func(o *Overlay) { o.Decoys[0].Path = "/pay/callback/x" },
		"注入片段为空":   func(o *Overlay) { o.InjectsProvided = true; o.Injects = []OverlayInject{{Kind: "dataset"}} },
		"注入分类未登记":  func(o *Overlay) { o.InjectsProvided = true; o.Injects = []OverlayInject{{Kind: "x", Snippet: "<a>"}} },
		"蜜罐重名":     func(o *Overlay) { o.Honeypots[1].Name = "hp-web" },
		"诱饵超出规模上限": func(o *Overlay) { o.Decoys = make([]OverlayDecoy, maxOverlayDecoys+1) },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			o := sampleOverlay()
			mut(&o)
			if _, err := base.WithOverlay(o, 3_000_001); err == nil {
				t.Fatalf("应当被终检拒绝")
			}
		})
	}
	if _, err := base.WithOverlay(sampleOverlay(), 0); err == nil {
		t.Error("版本 0 必须被拒绝")
	}
	if _, err := (&Loader{}).WithOverlay(sampleOverlay(), 1); err == nil {
		t.Error("没有基线的 Loader 必须被拒绝")
	}
}

func TestWithOverlayInjectsProvidedVsUnset(t *testing.T) {
	base := mustLoad(t, validYAML)
	ctx := context.Background()

	o := sampleOverlay()
	l, err := base.WithOverlay(o, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, provided, _ := l.Injects(ctx); provided {
		t.Error("未提供注入规则时必须保持「未配置」")
	}

	o.InjectsProvided = true
	l, err = base.WithOverlay(o, 11)
	if err != nil {
		t.Fatal(err)
	}
	if rules, provided, _ := l.Injects(ctx); !provided || len(rules) != 0 {
		t.Errorf("显式空必须是 provided=true 且 0 条：%v %d", provided, len(rules))
	}
}

func TestBlacklistSectionInYAML(t *testing.T) {
	ok := validYAML + "blacklist:\n  - {id: \"pay\", path_prefix: \"/pay\", reason: \"合规\"}\n"
	if bl, _ := mustLoad(t, ok).Blacklist(context.Background()); len(bl) != 1 {
		t.Fatalf("应解析出 1 条黑名单")
	}
	for name, doc := range map[string]string{
		"缺 reason": validYAML + "blacklist:\n  - {id: \"pay\", path_prefix: \"/pay\"}\n",
		"非绝对路径":    validYAML + "blacklist:\n  - {id: \"pay\", path_prefix: \"pay\", reason: \"r\"}\n",
		"非归一化":     validYAML + "blacklist:\n  - {id: \"pay\", path_prefix: \"/pay//x\", reason: \"r\"}\n",
		"重复 id":    validYAML + "blacklist:\n  - {id: \"a\", path_prefix: \"/a\", reason: \"r\"}\n  - {id: \"a\", path_prefix: \"/b\", reason: \"r\"}\n",
	} {
		if _, err := Load(strings.NewReader(doc)); err == nil {
			t.Errorf("%s：应当被拒绝", name)
		}
	}
}

// TestPullReflectsSwap 断言：Swap 之后下一次 Pull 立即返回新版本、新载荷与匹配的校验和。
func TestPullReflectsSwap(t *testing.T) {
	base := mustLoad(t, serverYAML)
	live := NewLive(base)
	srv := NewServerLive(live, store.NewPolicyMemory())
	ctx := context.Background()

	before, err := srv.Pull(ctx, &policyv1.PolicyPullRequest{})
	if err != nil {
		t.Fatal(err)
	}
	next, err := base.WithOverlay(sampleOverlay(), 3_000_001)
	if err != nil {
		t.Fatal(err)
	}
	live.Swap(next)
	after, err := srv.Pull(ctx, &policyv1.PolicyPullRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if after.GetVersion() != 3_000_001 || after.GetChecksum() == before.GetChecksum() {
		t.Fatalf("Swap 后应拿到新版本与新校验和：v=%d", after.GetVersion())
	}
	var doc struct {
		Decoys []struct {
			ID string `json:"id"`
		} `json:"decoys"`
	}
	if err := json.Unmarshal(after.GetPayload(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Decoys) != 1 || doc.Decoys[0].ID != "d-api" {
		t.Errorf("载荷应只含管控台下发的诱饵：%+v", doc.Decoys)
	}
}
