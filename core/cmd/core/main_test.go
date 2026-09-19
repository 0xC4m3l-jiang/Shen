// 装配层的回归测试：验证「配置 → 策略 → 判定 → 决策」这条链真的接对了。
//
// 为什么测试放在这里而不是模块里：它同时依赖 policy 与 judge 的**真实实现**，
// 而 MD-22 规定模块的测试禁止依赖其他模块的真实实例。
// 只有装配层（cmd）允许把具体实现拼起来。
//
// 跑法：go test -v ./core/cmd/core
package main

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shen/core/internal/contract"
	"shen/core/internal/control"
	"shen/core/internal/director"
	"shen/core/internal/judge"
	"shen/core/internal/policy"
	"shen/core/internal/store"
)

// replayConfig 是回放用的配置：三条规则，分别打 UA / 路径 / 方法。
// 版本号与校验和由 policy 负责，这里只关心「规则命中 → 分数」。
const replayConfig = `core:
  listen: "127.0.0.1:9443"
shadow: true
session:
  cookie_name: "sid"
thresholds:
  route_mirage: 0.70
  block: 0.95
guard:
  false_route_budget: 0.001
store:
  driver: "memory"
  redis: {addr: "", password: ""}
  clickhouse: {addr: "", database: ""}
  postgres: {dsn: ""}
policy:
  policy_id: "replay"
  version: 1
  gray_pct: 0
rules:
  - id: "ua-headless"
    weight: 0.6
    match: {field: "user_agent", op: "contains", value: "HeadlessChrome"}
  - id: "path-git"
    weight: 0.3
    match: {field: "path", op: "prefix", value: "/.git"}
  - id: "method-put"
    weight: 0.2
    match: {field: "method", op: "equals", value: "PUT"}
whitelist:
  source_cidrs: []
  user_agents: []
  path_prefixes: []
`

// replayCases 是回放样本。前两条是 Agent / 扫描器，后两条是负样本 ——
// 其中一条刻意补齐人类请求头但探路径，防止「只看 UA」这种误判（MD-7）。
var replayCases = []struct {
	name string
	obs  contract.Observation
	want float64
	hits []string
}{
	{
		name: "无头浏览器探源码",
		obs:  contract.Observation{UserAgent: "HeadlessChrome/120.0.0.0", Method: "GET", Path: "/.git/config"},
		want: 0.9,
		hits: []string{"ua-headless", "path-git"},
	},
	{
		name: "脚本批量扫路径",
		obs:  contract.Observation{UserAgent: "curl/8.5.0", Method: "GET", Path: "/.git/config"},
		want: 0.3,
		hits: []string{"path-git"},
	},
	{
		name: "正常浏览器访问首页",
		obs: contract.Observation{
			UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Safari/537.36",
			Method:    "GET",
			Path:      "/index.html",
		},
		want: 0,
	},
	{
		name: "伪造 UA 但探路径",
		obs: contract.Observation{
			UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Safari/537.36",
			Method:    "PUT",
			Path:      "/.git/config",
		},
		want: 0.5,
		hits: []string{"path-git", "method-put"},
	},
}

// TestRuleReplay 是开发期的「规则回放」：把样本观测喂进真实的判定引擎，打印命中明细。
//
// 判定响应禁止回显分值（ST-7），所以在 director 落地之前，这里是唯一能看见
// 「规则到底命中没有」的地方。用 -v 跑可以看到完整表格。
func TestRuleReplay(t *testing.T) {
	loader, err := policy.Load(strings.NewReader(replayConfig))
	if err != nil {
		t.Fatalf("装载回放配置失败：%v", err)
	}
	engine := judge.New(loader)
	ctx := context.Background()

	t.Logf("规则回放（policy → judge）：")
	t.Logf("%-22s %-6s %-6s %s", "样本", "分数", "规则数", "命中信号")

	for _, c := range replayCases {
		v, err := engine.Judge(ctx, contract.JudgeRequest{DecisionID: c.name, Observed: c.obs})
		if err != nil {
			t.Fatalf("%s：判定失败：%v", c.name, err)
		}
		if math.Abs(v.Score-c.want) > 1e-9 {
			t.Errorf("%s：分数 = %v，期望 %v", c.name, v.Score, c.want)
		}
		got := make([]string, 0, len(v.Signals))
		for _, s := range v.Signals {
			got = append(got, s.ID)
		}
		if strings.Join(got, ",") != strings.Join(c.hits, ",") {
			t.Errorf("%s：命中信号 = %v，期望 %v", c.name, got, c.hits)
		}
		t.Logf("%-22s %-6v %-6d %s", c.name, v.Score, len(v.Signals), strings.Join(got, ","))
	}
}

// TestShadowModeNeverActs 是影子模式的回归判据：即使分数拉满，也绝不改道、绝不拦截。
func TestShadowModeNeverActs(t *testing.T) {
	loader, err := policy.Load(strings.NewReader(replayConfig))
	if err != nil {
		t.Fatalf("装载配置失败：%v", err)
	}
	decider := control.NewShadowDecider(judge.New(loader))

	d, err := decider.Decide(context.Background(), contract.JudgeRequest{
		DecisionID: "shadow-regression",
		Observed:   contract.Observation{UserAgent: "HeadlessChrome/120", Method: "PUT", Path: "/.git/config"},
	})
	if err != nil {
		t.Fatalf("决策失败：%v", err)
	}
	if d.Action != contract.ActionOrigin {
		t.Fatalf("影子模式必须放行，实际 = %s", d.Action)
	}
	if d.Severity != contract.SeverityNone {
		t.Fatalf("severity 只能是 none，实际 = %s", d.Severity)
	}
	if d.Verdict.Score == 0 {
		t.Fatal("影子模式仍必须照算判定（分数不应为零）")
	}
}

// TestDirectorDivertsHighScore 是阶段 2a 的端到端判据：关闭影子后，
// 高分请求真的被改道（route_mirage + 后端名），低分仍放行。
func TestDirectorDivertsHighScore(t *testing.T) {
	cfg := strings.Replace(replayConfig, "shadow: true", "shadow: false", 1)
	cfg = strings.Replace(cfg, "gray_pct: 0", "gray_pct: 100", 1)
	loader, err := policy.Load(strings.NewReader(cfg))
	if err != nil {
		t.Fatalf("装载配置失败：%v", err)
	}
	dir, err := director.New(judge.New(loader), loader, loader, director.Config{})
	if err != nil {
		t.Fatal(err)
	}

	// 高分：ua-headless(0.6) + path-git(0.3) = 0.9 ≥ 0.70 → 改道
	high, err := dir.Decide(context.Background(), contract.JudgeRequest{
		DecisionID: "e2e-divert",
		Observed:   contract.Observation{UserAgent: "HeadlessChrome/120", Method: "GET", Path: "/.git/config"},
	})
	if err != nil {
		t.Fatalf("决策失败：%v", err)
	}
	if high.Action != contract.ActionMirage {
		t.Fatalf("接管模式高分应改道，实际 = %s", high.Action)
	}
	if high.Backend == "" {
		t.Error("改道必须带后端名")
	}

	// 低分：正常浏览器 → 放行
	low, _ := dir.Decide(context.Background(), contract.JudgeRequest{
		DecisionID: "e2e-origin",
		Observed:   contract.Observation{UserAgent: "Mozilla/5.0", Method: "GET", Path: "/index.html"},
	})
	if low.Action != contract.ActionOrigin {
		t.Errorf("低分应放行，实际 = %s", low.Action)
	}
}

// TestDirectorGrayZeroStaysOrigin：即使 shadow=false，灰度 0 也不改道 ——
// 影子模式等价于灰度 0（INT-12 的阶梯起点）。
func TestDirectorGrayZeroStaysOrigin(t *testing.T) {
	cfg := strings.Replace(replayConfig, "shadow: true", "shadow: false", 1)
	loader, err := policy.Load(strings.NewReader(cfg)) // gray_pct: 0
	if err != nil {
		t.Fatal(err)
	}
	dir, err := director.New(judge.New(loader), loader, loader, director.Config{})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := dir.Decide(context.Background(), contract.JudgeRequest{
		DecisionID: "gray-zero",
		Observed:   contract.Observation{UserAgent: "HeadlessChrome/120", Method: "PUT", Path: "/.git/config"},
	})
	if d.Action != contract.ActionOrigin {
		t.Fatalf("灰度 0 必须全放行（INT-12 起点），实际 = %s", d.Action)
	}
}

func TestLoadPolicyRequiresConfigPath(t *testing.T) {
	t.Setenv("SHEN_CONFIG", "")
	if _, err := loadPolicy(); err == nil || !strings.Contains(err.Error(), "SHEN_CONFIG") {
		t.Fatalf("未设置 SHEN_CONFIG 必须报错并提示，实际：%v", err)
	}
}

func TestLoadPolicyRejectsMissingFile(t *testing.T) {
	t.Setenv("SHEN_CONFIG", filepath.Join(t.TempDir(), "不存在.yaml"))
	if _, err := loadPolicy(); err == nil || !strings.Contains(err.Error(), "打开配置失败") {
		t.Fatalf("配置文件缺失必须报错，实际：%v", err)
	}
}

func TestLoadPolicyRejectsInvalidContent(t *testing.T) {
	bad := strings.Replace(replayConfig, "weight: 0.3", "weight: 1.5", 1)
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatalf("写临时配置失败：%v", err)
	}
	t.Setenv("SHEN_CONFIG", path)

	_, err := loadPolicy()
	if err == nil || !strings.Contains(err.Error(), "weight") {
		t.Fatalf("非法配置必须报错并指出字段，实际：%v", err)
	}
}

// TestLoadPolicyWritesVersionLedger 覆盖装配里的一步：装载后写版本台账，且版本只增。
func TestLoadPolicyWritesVersionLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(replayConfig), 0o600); err != nil {
		t.Fatalf("写临时配置失败：%v", err)
	}
	t.Setenv("SHEN_CONFIG", path)

	loader, err := loadPolicy()
	if err != nil {
		t.Fatalf("装载失败：%v", err)
	}
	ledger := store.NewMemStores(nil).Policy
	ctx := context.Background()
	if err := loader.Publish(ctx, ledger); err != nil {
		t.Fatalf("首次发布失败：%v", err)
	}
	cur, err := ledger.Current(ctx)
	if err != nil {
		t.Fatalf("读台账失败：%v", err)
	}
	if cur.Version != 1 || cur.PolicyID != "replay" || len(cur.Rules) != 3 {
		t.Fatalf("台账内容不正确：%+v", cur)
	}
	if err := loader.Publish(ctx, ledger); err == nil {
		t.Fatal("同版本二次发布必须被拒绝（版本只增）")
	}
}
