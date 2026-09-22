package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"shen/common/core/internal/contract"
)

// 夹具分三段拼装：head（core…policy）· rulesBlock · tail（whitelist）。
// 拆开是为了能单独删掉某一段，构造「缺少必填项」的负样本。
const (
	head = `core:
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
  policy_id: "core-rules"
  version: 3
  gray_pct: 0
`

	rulesBlock = `rules:
  - id: "ua-headless"
    weight: 0.6
    match: {field: "user_agent", op: "contains", value: "HeadlessChrome"}
  - id: "path-probe"
    weight: 0.3
    match:
      field: "path"
      op: "prefix"
      value: "/.git"
`

	tail = `whitelist:
  source_cidrs: ["10.0.0.0/8"]
  user_agents: ["kube-probe"]
  path_prefixes: ["/healthz"]
`
)

const validYAML = head + rulesBlock + tail

// mutate 在夹具里替换一段文本，替换不到就报错 —— 防止夹具改坏后负样本静默变成正样本。
func mutate(t *testing.T, s, old, new string) string {
	t.Helper()
	if !strings.Contains(s, old) {
		t.Fatalf("夹具缺少片段：%q", old)
	}
	return strings.Replace(s, old, new, 1)
}

func mustLoad(t *testing.T, s string) *Loader {
	t.Helper()
	l, err := Load(strings.NewReader(s))
	if err != nil {
		t.Fatalf("装载应当成功，却失败：%v", err)
	}
	return l
}

// TestLoadAcceptsQueryField 断言 `query` 是合法规则字段（正样本）。
//
// 为什么要有它：字段白名单是「新增可匹配字段」的唯一闸门（`validField`）——
// 只测「未知字段被拒」时，把 `query` 漏掉的回归不会被抓住（它会以「未知字段」的名义被拒，
// 而负样本本来期望的就是拒绝）。依据见 docs/spec/config.md §2.4。
// TestLoadAcceptsPathNormAndPathPrefix 断言新增字段/算符都在白名单里（正样本）。
//
// 负样本（未知字段/算符被拒）在同表的 TestLoadRejectsInvalidConfig 里 —— 两侧都要有：
// 只测一侧时，「漏加白名单」会表现为「新增字段被当未知拒绝」，而负样本恰好期望拒绝。
func TestLoadAcceptsPathNormAndPathPrefix(t *testing.T) {
	yaml := mutate(t, validYAML, `field: "path"`, `field: "path_norm"`)
	yaml = mutate(t, yaml, `op: "prefix"`, `op: "path_prefix"`)

	l := mustLoad(t, yaml)
	rules, err := l.Rules(context.Background())
	if err != nil {
		t.Fatalf("取规则失败：%v", err)
	}
	found := false
	for _, r := range rules {
		if r.Match.Field == "path_norm" && r.Match.Op == "path_prefix" {
			found = true
		}
	}
	if !found {
		t.Fatalf("path_norm + path_prefix 的规则应被装载，得到 %+v", rules)
	}
}

func TestLoadAcceptsQueryField(t *testing.T) {
	l := mustLoad(t, mutate(t, validYAML, `field: "path"`, `field: "query"`))

	rules, err := l.Rules(context.Background())
	if err != nil {
		t.Fatalf("取规则失败：%v", err)
	}
	found := false
	for _, r := range rules {
		if r.Match.Field == "query" {
			found = true
		}
	}
	if !found {
		t.Fatalf("query 字段的规则应被装载，得到 %+v", rules)
	}
}

func TestLoadValidConfig(t *testing.T) {
	l := mustLoad(t, validYAML)
	snap, err := l.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("取快照失败：%v", err)
	}

	if snap.PolicyID != "core-rules" || snap.Version != 3 || snap.GrayPct != 0 {
		t.Fatalf("快照标识不正确：%+v", snap)
	}
	if len(snap.Rules) != 2 {
		t.Fatalf("规则数 = %d，期望 2", len(snap.Rules))
	}
	if snap.Rules[0].ID != "ua-headless" || snap.Rules[0].Match.Value != "HeadlessChrome" {
		t.Fatalf("规则内容不正确：%+v", snap.Rules[0])
	}
	if snap.Rules[1].Match.Op != "prefix" || snap.Rules[1].Match.Field != "path" {
		t.Fatalf("块式 match 解析不正确：%+v", snap.Rules[1])
	}

	// 校验和必须等于载荷的 sha256（小写 hex）。
	sum := sha256.Sum256(snap.Payload)
	if snap.Checksum != hex.EncodeToString(sum[:]) {
		t.Fatalf("校验和与载荷不一致：%s", snap.Checksum)
	}
	if len(snap.Checksum) != 64 {
		t.Fatalf("校验和长度 = %d，期望 64", len(snap.Checksum))
	}

	// 载荷只含三段，且键序固定；密钥与运行参数禁止进入载荷（ST-20）。
	body := string(snap.Payload)
	for _, forbidden := range []string{"password", "dsn", "listen", "false_route_budget"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("载荷不应包含 %q：%s", forbidden, body)
		}
	}
	ip := strings.Index(body, `"policy"`)
	ir := strings.Index(body, `"rules"`)
	iw := strings.Index(body, `"whitelist"`)
	if ip < 0 || ir < 0 || iw < 0 || !(ip < ir && ir < iw) {
		t.Fatalf("载荷键序必须为 policy → rules → whitelist：%s", body)
	}
	if !strings.Contains(body, `"source_cidrs":["10.0.0.0/8"]`) {
		t.Fatalf("载荷缺少 whitelist 内容：%s", body)
	}
}

func TestChecksumIsReproducible(t *testing.T) {
	a, _ := mustLoad(t, validYAML).Snapshot(context.Background())
	b, _ := mustLoad(t, validYAML).Snapshot(context.Background())
	if a.Checksum != b.Checksum {
		t.Fatalf("同内容两次装载校验和不同：%s vs %s", a.Checksum, b.Checksum)
	}

	changed := mutate(t, validYAML, "weight: 0.6", "weight: 0.7")
	c, _ := mustLoad(t, changed).Snapshot(context.Background())
	if c.Checksum == a.Checksum {
		t.Fatal("改动规则权重后校验和未变化")
	}
}

func TestEmptyRulesIsAllowed(t *testing.T) {
	l := mustLoad(t, head+"rules: []\n"+tail)
	rules, err := l.Rules(context.Background())
	if err != nil {
		t.Fatalf("取规则失败：%v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("规则数 = %d，期望 0", len(rules))
	}
	snap, _ := l.Snapshot(context.Background())
	if !strings.Contains(string(snap.Payload), `"rules":[]`) {
		t.Fatalf("空规则集必须序列化为 []，实际：%s", snap.Payload)
	}
}

func TestEmptyWhitelistListsAreAllowed(t *testing.T) {
	wl := "whitelist:\n  source_cidrs: []\n  user_agents: []\n  path_prefixes: []\n"
	mustLoad(t, head+rulesBlock+wl)
}

func TestRulesAndSnapshotReturnCopies(t *testing.T) {
	l := mustLoad(t, validYAML)
	ctx := context.Background()

	first, _ := l.Rules(ctx)
	first[0].ID = "被调用方改写"
	first[0].Match.Value = "被调用方改写"

	second, _ := l.Rules(ctx)
	if second[0].ID != "ua-headless" || second[0].Match.Value != "HeadlessChrome" {
		t.Fatalf("内部快照被调用方改写：%+v", second[0])
	}

	snap, _ := l.Snapshot(ctx)
	snap.Payload[0] = 'X'
	snap.Rules[0].ID = "被调用方改写"
	again, _ := l.Snapshot(ctx)
	if again.Payload[0] == 'X' || again.Rules[0].ID != "ua-headless" {
		t.Fatal("返回的快照与内部状态共享了可变内存")
	}
}

// TestShadowFlag：director（阶段 2a）交付后，shadow=false 是合法运维动作
// —— INT-11 只要求「首次上线」影子，不是永远。
func TestShadowFlag(t *testing.T) {
	if !mustLoad(t, validYAML).Shadow() {
		t.Error("默认 shadow: true 时 Shadow() 应为 true")
	}
	if mustLoad(t, mutate(t, validYAML, "shadow: true", "shadow: false")).Shadow() {
		t.Error("shadow: false 时 Shadow() 应为 false")
	}
}

func TestWhitelistExposedToDirector(t *testing.T) {
	l := mustLoad(t, validYAML)
	w, err := l.Whitelist(context.Background())
	if err != nil {
		t.Fatalf("读取白名单失败：%v", err)
	}
	// 返回的是副本：改写不得影响内部状态。
	w.UserAgents = append(w.UserAgents, "被改写")
	again, _ := l.Whitelist(context.Background())
	for _, ua := range again.UserAgents {
		if ua == "被改写" {
			t.Error("Whitelist 应返回副本")
		}
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"缺 core 段", rulesBlock + tail, "缺少必填项 core"},
		{"缺 core.listen", mutate(t, validYAML, "core:\n  listen: \"127.0.0.1:9443\"", "core: {}"), "缺少必填项 core.listen"},
		{"listen 非法", mutate(t, validYAML, `listen: "127.0.0.1:9443"`, `listen: "9443"`), "不是合法的 host:port"},
		{"cookie_name 为空", mutate(t, validYAML, `cookie_name: "sid"`, `cookie_name: ""`), "session.cookie_name 不能为空"},
		{"嵌套未知键", mutate(t, validYAML, `cookie_name: "sid"`, "cookie_name: \"sid\"\n  cookie: \"x\""), "解析配置失败"},
		{"顶层未知键", mutate(t, validYAML, "shadow: true", "shadow: true\nshadows: true"), "解析配置失败"},
		{"阈值乱序", mutate(t, validYAML, "route_mirage: 0.70", "route_mirage: 0.99"), "不得大于"},
		{"阈值越界", mutate(t, validYAML, "block: 0.95", "block: 1.5"), "越界"},
		{"误调度率越界", mutate(t, validYAML, "false_route_budget: 0.001", "false_route_budget: -1"), "越界"},
		// 回归：早前 ratio() 一律报「thresholds」，配错 guard 时会把排障引向错误字段。
		{"guard 缺键须报对字段", mutate(t, validYAML, "guard:\n  false_route_budget: 0.001", "guard: {}"), "guard.false_route_budget"},
		{"缺 store.redis", mutate(t, validYAML, "  redis: {addr: \"\", password: \"\"}\n", ""), "缺少必填项 store.redis"},
		{"驱动未实现", mutate(t, validYAML, `driver: "memory"`, `driver: "redis"`), "未实现"},
		{"version 为 0", mutate(t, validYAML, "version: 3", "version: 0"), "policy.version=0 非法"},
		{"gray_pct 越界", mutate(t, validYAML, "gray_pct: 0", "gray_pct: 101"), "gray_pct=101 越界"},
		{"policy_id 为空", mutate(t, validYAML, `policy_id: "core-rules"`, `policy_id: ""`), "policy.policy_id 不能为空"},
		{"缺 rules 键", head + tail, "缺少必填项 rules"},
		{"缺 match", mutate(t, validYAML, "    match: {field: \"user_agent\", op: \"contains\", value: \"HeadlessChrome\"}\n", ""), "缺少必填项 rules[0].match"},
		{"规则 id 重复", mutate(t, validYAML, `id: "path-probe"`, `id: "ua-headless"`), "重复"},
		{"规则 id 为空", mutate(t, validYAML, `id: "path-probe"`, `id: ""`), "rules[1].id 不能为空"},
		{"权重为 0", mutate(t, validYAML, "weight: 0.6", "weight: 0"), "weight=0 越界"},
		{"权重超过 1", mutate(t, validYAML, "weight: 0.3", "weight: 1.5"), "weight=1.5 越界"},
		{"未知 field", mutate(t, validYAML, `field: "path"`, `field: "header"`), "未知"},
		{"未知 op", mutate(t, validYAML, `op: "prefix"`, `op: "regex"`), "未知"},
		{"空 value", mutate(t, validYAML, `value: "/.git"`, `value: ""`), "不能为空"},
		{"缺 whitelist", head + rulesBlock, "缺少必填项 whitelist"},
		{"非法 CIDR", mutate(t, validYAML, `source_cidrs: ["10.0.0.0/8"]`, `source_cidrs: ["10.0.0.0"]`), "不是合法的 CIDR"},
		{"path_prefix 不以 / 开头", mutate(t, validYAML, `path_prefixes: ["/healthz"]`, `path_prefixes: ["healthz"]`), "必须以 / 开头"},
		{"白名单项为空", mutate(t, validYAML, `user_agents: ["kube-probe"]`, `user_agents: [""]`), "whitelist.user_agents[0] 不能为空"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(strings.NewReader(c.yaml))
			if err == nil {
				t.Fatal("非法配置必须装载失败，却成功了")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("错误信息未指出问题：得到 %q，期望包含 %q", err.Error(), c.want)
			}
		})
	}
}

func TestLoadRejectsDegenerateInput(t *testing.T) {
	if _, err := Load(nil); err == nil {
		t.Fatal("nil 读取器必须报错")
	}
	if _, err := Load(strings.NewReader("")); err == nil {
		t.Fatal("空配置必须报错")
	}
	if _, err := Load(strings.NewReader(validYAML + "---\n" + validYAML)); err == nil {
		t.Fatal("多文档配置必须报错")
	}
}

// fakePolicyStore 是 store.PolicyStore 的替身。
// MD-22：模块测试禁止依赖其他模块的真实实例。
type fakePolicyStore struct {
	mu    sync.Mutex
	cur   *contract.PolicySnapshot
	calls int
	acks  []contract.PolicyAck
}

func (f *fakePolicyStore) Current(context.Context) (contract.PolicySnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cur == nil {
		return contract.PolicySnapshot{}, errors.New("store: 尚无策略版本")
	}
	return *f.cur, nil
}

func (f *fakePolicyStore) Publish(_ context.Context, p contract.PolicySnapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cur != nil && p.Version <= f.cur.Version {
		return errors.New("store: 策略版本必须单调递增")
	}
	cp := p
	f.cur = &cp
	f.calls++
	return nil
}

// RecordAck / Acks 只是把回执收在内存里 —— 本测试只关心发布语义，回执形状在 server_test.go 里测。
func (f *fakePolicyStore) RecordAck(_ context.Context, a contract.PolicyAck) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acks = append(f.acks, a)
	return nil
}

func (f *fakePolicyStore) Acks(context.Context) ([]contract.PolicyAck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]contract.PolicyAck(nil), f.acks...), nil
}

func TestPublishEnforcesMonotonicVersion(t *testing.T) {
	fake := &fakePolicyStore{}
	ctx := context.Background()

	l3 := mustLoad(t, validYAML) // version: 3
	if err := l3.Publish(ctx, fake); err != nil {
		t.Fatalf("首次发布应当成功：%v", err)
	}

	// 同版本再发布必须被拒绝（版本只增）。
	if err := l3.Publish(ctx, fake); err == nil {
		t.Fatal("同版本二次发布必须被拒绝")
	}

	// 回滚 = 用旧内容 + 更高的版本号发布。
	l4 := mustLoad(t, mutate(t, validYAML, "version: 3", "version: 4"))
	if err := l4.Publish(ctx, fake); err != nil {
		t.Fatalf("更高版本应当发布成功：%v", err)
	}
	if fake.calls != 2 {
		t.Fatalf("台账写入次数 = %d，期望 2", fake.calls)
	}

	if err := l3.Publish(ctx, nil); err == nil {
		t.Fatal("nil 台账必须报错")
	}
}

func TestConcurrentReadsAreRaceFree(t *testing.T) {
	l := mustLoad(t, validYAML)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, err := l.Rules(ctx); err != nil {
					t.Errorf("并发取规则失败：%v", err)
					return
				}
				if _, err := l.Snapshot(ctx); err != nil {
					t.Errorf("并发取快照失败：%v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestExampleConfigLoads 是漂移守卫：仓库内的示例配置必须始终能通过全部校验。
// 样例与 schema 分叉时，这里会先红。
func TestExampleConfigLoads(t *testing.T) {
	f, err := os.Open("../../../../deploy/config/config.example.yaml")
	if err != nil {
		t.Fatalf("打开示例配置失败：%v", err)
	}
	defer func() { _ = f.Close() }()

	l, err := Load(f)
	if err != nil {
		t.Fatalf("示例配置必须合法：%v", err)
	}
	snap, _ := l.Snapshot(context.Background())
	if snap.PolicyID == "" || snap.Version == 0 || snap.Checksum == "" {
		t.Fatalf("示例配置的快照不完整：%+v", snap)
	}
}
