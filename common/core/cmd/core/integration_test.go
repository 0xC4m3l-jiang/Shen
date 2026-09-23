package main

// 欺骗引擎的**端到端集成测试**。
//
// 放在装配层（cmd）的原因与 main_test.go 相同：它同时依赖多个模块的**真实实现**
// （policy / judge / director / honeypot / decoy / responder / isolation / control），
// 而 MD-22 只约束模块自身的测试 —— 装配层正是「把具体实现拼起来」的地方。
//
// 这个文件本身就是「模块之间能快速接入」的证据：全部接线只需要构造函数 + 接口。

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	judgev1 "shen/common/api/judge/v1"
	"shen/common/core/internal/contract"
	"shen/common/core/internal/control"
	"shen/common/core/internal/decoy"
	"shen/common/core/internal/director"
	"shen/common/core/internal/honeypot"
	"shen/common/core/internal/isolation"
	"shen/common/core/internal/judge"
	"shen/common/core/internal/policy"
	"shen/common/core/internal/responder"
	"shen/common/core/internal/session"
	"shen/common/core/internal/store"
)

// deceptionConfig 是集成用的配置：接管模式（shadow=false）、全量改道（gray_pct=100）、
// 阈值降到 0.5，并带一个诱饵资产与一个幻境后端。
const deceptionConfig = `core:
  listen: "127.0.0.1:19443"
shadow: false
session:
  cookie_name: "sid"
thresholds:
  route_mirage: 0.50
  block: 0.95
guard:
  false_route_budget: 0.001
store:
  driver: "memory"
  redis: {addr: "", password: ""}
  clickhouse: {addr: "", database: ""}
  postgres: {dsn: ""}
policy:
  policy_id: "integration"
  version: 1
  gray_pct: 100
rules:
  - id: "ua-agent"
    weight: 0.9
    match: {field: "user_agent", op: "contains", value: "HeadlessChrome"}
whitelist:
  source_cidrs: []
  user_agents: []
  path_prefixes: []
decoys:
  assets:
    - id: "dev-api"
      kind: "developer_api"
      path: "/portal/api/content"
      content: "site-developer-api"
      enabled: true
honeypots:
  - name: "mirage"
    type: "ssh"
    addr: "10.0.0.9:2222"
    enabled: true
`

// agentObs 是一个高置信 Agent 的首跳观测。
func agentObs() contract.Observation {
	return contract.Observation{
		UserAgent: "HeadlessChrome/120.0.0.0",
		Method:    "GET",
		Path:      "/portal/api/content",
		Headers:   map[string]string{"cookie": "sid=agent-session"},
	}
}

// TestDeceptionChainEndToEnd 串起整条欺骗链：
//
//	session 身份 → judge 判定 → director 决策（三值 + 后端名）
//	→ honeypot 解析后端 → decoy 命中诱饵 → responder 生成响应（一致性）
//
// 每一跳都只通过接口调用，没有任何模块 import 另一个模块的具体类型。
func TestDeceptionChainEndToEnd(t *testing.T) {
	ctx := context.Background()
	loader, err := policy.Load(strings.NewReader(deceptionConfig))
	if err != nil {
		t.Fatalf("装载配置失败：%v", err)
	}
	stores := store.NewMemStores(nil)

	// ① 会话身份
	sess := session.New("sid")
	key, err := sess.Key(ctx, agentObs())
	if err != nil {
		t.Fatalf("会话身份提取失败：%v", err)
	}
	if key.ID == "" {
		t.Fatal("会话键不能为空")
	}

	// ② 判定 → ③ 决策
	dir, err := director.New(judge.New(loader), loader, loader, director.Config{})
	if err != nil {
		t.Fatalf("构造 director 失败：%v", err)
	}
	d, err := dir.Decide(ctx, contract.JudgeRequest{DecisionID: "e2e-1", Session: key, Observed: agentObs()})
	if err != nil {
		t.Fatalf("决策失败：%v", err)
	}
	if d.Action != contract.ActionMirage {
		t.Fatalf("高分 Agent 应被改道，实际 %s（分数 %.2f）", d.Action, d.Verdict.Score)
	}
	if d.Backend == "" {
		t.Fatal("route_mirage 必须带后端名")
	}
	if d.Severity != contract.SeverityNone {
		t.Errorf("severity 只能是 none（TM-13 / MD-24），得到 %s", d.Severity)
	}

	// ④ 幻境后端池解析
	backends, err := loader.Honeypots(ctx)
	if err != nil {
		t.Fatalf("读取后端池失败：%v", err)
	}
	pool, err := honeypot.New(backends, nil)
	if err != nil {
		t.Fatalf("构造后端池失败：%v", err)
	}
	backend, ok, err := pool.Resolve(ctx, d.Backend)
	if err != nil {
		t.Fatalf("解析后端失败：%v", err)
	}
	if !ok {
		t.Fatalf("director 给出的后端名 %q 应能在池中解析到", d.Backend)
	}
	if backend.Type != "ssh" || backend.Addr != "10.0.0.9:2222" {
		t.Errorf("解析到的后端不正确：%+v", backend)
	}

	// ⑤ 诱饵面：命中 + 多态 + 投放片段
	assets, err := loader.Decoys(ctx)
	if err != nil {
		t.Fatalf("读取诱饵资产失败：%v", err)
	}
	for _, a := range assets {
		if err := stores.Decoy.Put(ctx, a); err != nil {
			t.Fatalf("写入诱饵资产失败：%v", err)
		}
	}
	surface, err := decoy.New(stores.Decoy, 0)
	if err != nil {
		t.Fatalf("构造诱饵面失败：%v", err)
	}
	// 只传**路径**：查询串不属于路径（`Match` 对含 `?` 的输入会显式报错，见 decoy 的单测）。
	hit, ok, err := surface.Match(ctx, "/portal/api/content")
	if err != nil {
		t.Fatalf("匹配诱饵失败：%v", err)
	}
	if !ok || hit.ID != "dev-api" {
		t.Fatalf("应命中 dev-api 诱饵，得到 %+v（ok=%v）", hit, ok)
	}
	v1, _ := surface.Variant(ctx, key.ID)
	v2, _ := surface.Variant(ctx, key.ID)
	if v1 != v2 {
		t.Error("同会话的诱饵变体必须确定性（ADR-0016）")
	}
	placements, err := surface.Placements(ctx, hit.ID)
	if err != nil {
		t.Fatalf("生成投放片段失败：%v", err)
	}
	if len(placements) == 0 {
		t.Error("诱饵必须有投放片段（投放才是最后一公里）")
	}

	// ⑥ 响应生成：一致性不变量（AR-30）
	resp, err := responder.New(stores.Content)
	if err != nil {
		t.Fatalf("构造 responder 失败：%v", err)
	}
	rreq := contract.RespondRequest{SessionID: key.ID, Resource: "GET /portal/api/content", AssetID: hit.ID, Kind: hit.Kind}
	first, err := resp.Respond(ctx, rreq)
	if err != nil {
		t.Fatalf("生成响应失败：%v", err)
	}
	for i := 0; i < 5; i++ {
		again, _ := resp.Respond(ctx, rreq)
		if !bytes.Equal(first.Body, again.Body) {
			t.Fatalf("同一 (会话, 资源) 必须逐字节一致（AR-30）\n%s\nvs\n%s", first.Body, again.Body)
		}
	}
	if len(first.Body) == 0 {
		t.Error("响应体不能为空")
	}
	if first.Headers["Content-Type"] == "" {
		t.Error("响应必须带 Content-Type")
	}
	if len(first.Headers) != 1 {
		t.Errorf("不得新增响应头（NI-9），得到 %v", first.Headers)
	}
}

// TestLowScoreAgentStaysOnOrigin 反例：低分请求放行 —— 误调度率是首要约束。
func TestLowScoreAgentStaysOnOrigin(t *testing.T) {
	ctx := context.Background()
	loader, err := policy.Load(strings.NewReader(deceptionConfig))
	if err != nil {
		t.Fatalf("装载配置失败：%v", err)
	}
	dir, err := director.New(judge.New(loader), loader, loader, director.Config{})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	human := contract.Observation{
		UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
		Method:    "GET",
		Path:      "/index.html",
	}
	d, err := dir.Decide(ctx, contract.JudgeRequest{DecisionID: "e2e-low", Observed: human})
	if err != nil {
		t.Fatalf("决策失败：%v", err)
	}
	if d.Action != contract.ActionOrigin {
		t.Errorf("正常用户必须放行，实际 %s", d.Action)
	}
}

// TestConfiguredDecoyPathIsNeverBlocked 验证装配层的 MD-25 接线：
// 配置里的诱饵路径经 `decoyPrefixes` 汇总后交给决策层，诱饵面上永远不产出 block。
func TestConfiguredDecoyPathIsNeverBlocked(t *testing.T) {
	ctx := context.Background()
	// block 阈值降到 0.85，使满分（0.9）真的会触发 block —— 否则测不出「豁免」。
	cfg := strings.Replace(deceptionConfig, "block: 0.95", "block: 0.85", 1)
	loader, err := policy.Load(strings.NewReader(cfg))
	if err != nil {
		t.Fatalf("装载配置失败：%v", err)
	}
	assets, err := loader.Decoys(ctx)
	if err != nil {
		t.Fatalf("读取诱饵资产失败：%v", err)
	}
	prefixes := decoyPrefixes(assets)
	if len(prefixes) == 0 {
		t.Fatal("配置里的诱饵路径应被汇总成前缀集")
	}

	// block 开关**打开**，分数拉满 —— 但路径在诱饵面上。
	dir, err := director.New(judge.New(loader), loader, loader, director.Config{
		BlockEnabled:  true,
		DecoyPrefixes: prefixes,
	})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	d, err := dir.Decide(ctx, contract.JudgeRequest{DecisionID: "md25", Observed: agentObs()})
	if err != nil {
		t.Fatalf("决策失败：%v", err)
	}
	if d.Action == contract.ActionBlock {
		t.Error("诱饵面禁止 block（MD-25）—— 在诱饵面阻断等于自断情报源")
	}

	// 同一高分落在非诱饵路径上 → 可以 block（证明豁免是**针对路径**的，不是全局关掉了 block）。
	other := contract.Observation{UserAgent: "HeadlessChrome/120", Method: "GET", Path: "/admin/db/dump"}
	d2, err := dir.Decide(ctx, contract.JudgeRequest{DecisionID: "md25-2", Observed: other})
	if err != nil {
		t.Fatalf("决策失败：%v", err)
	}
	if d2.Action != contract.ActionBlock {
		t.Errorf("非诱饵路径在 block 开关打开时应 block，得到 %s", d2.Action)
	}
}

// ── 隔离短路：命中隔离时**不得**调用决策层（客户端不可见）────────────────────

// countingDecider 记录被调用次数；被调用即失败（隔离命中时不应发生）。
type countingDecider struct{ calls int }

func (c *countingDecider) Decide(context.Context, contract.JudgeRequest) (contract.Decision, error) {
	c.calls++
	return contract.Decision{Action: contract.ActionMirage, Backend: "should-not-happen"}, nil
}

func TestIsolationShortCircuitsWithoutCallingCore(t *testing.T) {
	ctx := context.Background()
	stores := store.NewMemStores(nil)
	sess := session.New("sid")
	iso, err := isolation.New(stores.Isolation, nil)
	if err != nil {
		t.Fatalf("构造隔离模块失败：%v", err)
	}

	obs := agentObs()
	key, err := sess.Key(ctx, obs)
	if err != nil {
		t.Fatalf("会话身份提取失败：%v", err)
	}
	if err := iso.Isolate(ctx, key, "canary_echo", time.Minute); err != nil {
		t.Fatalf("写入隔离失败：%v", err)
	}

	dec := &countingDecider{}
	svc := control.NewJudgeService(dec, sess, control.WithIsolation(iso))

	resp, err := svc.Judge(ctx, &judgev1.JudgeRequest{
		DecisionId: "iso-1",
		Observed: &judgev1.Observation{
			UserAgent: obs.UserAgent,
			Method:    obs.Method,
			Path:      obs.Path,
			Headers:   obs.Headers,
		},
	})
	if err != nil {
		t.Fatalf("判定面调用失败：%v", err)
	}
	if dec.calls != 0 {
		t.Errorf("隔离命中时不得调用决策层，实际调用 %d 次", dec.calls)
	}
	if resp.GetAction() != judgev1.Action_ACTION_ORIGIN {
		t.Errorf("隔离短路必须返回放行（客户端不可见），实际 %s", resp.GetAction())
	}
	if resp.GetBackend() != "" {
		t.Errorf("放行不得带后端名，实际 %q", resp.GetBackend())
	}
}

// TestIsolationStoreFailureFailsOpen 覆盖 NI-10：隔离存储不可用时**必须**回退调用核心，
// 而不是阻断业务。
func TestIsolationStoreFailureFailsOpen(t *testing.T) {
	ctx := context.Background()
	sess := session.New("sid")

	// 用会报错的存储替身（store 不可用）。
	failing := &failingIsolation{}
	iso, err := isolation.New(failing, nil)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}

	dec := &countingDecider{}
	svc := control.NewJudgeService(dec, sess, control.WithIsolation(iso))

	if _, err := svc.Judge(ctx, &judgev1.JudgeRequest{
		DecisionId: "iso-fail",
		Observed:   &judgev1.Observation{UserAgent: "x", Method: "GET", Path: "/"},
	}); err != nil {
		t.Fatalf("隔离存储故障时不得让判定面失败：%v", err)
	}
	if dec.calls != 1 {
		t.Errorf("隔离存储不可用必须 fail-open（回退调用决策层），实际调用 %d 次", dec.calls)
	}
}

// failingIsolation 是「存储不可用」的替身。
type failingIsolation struct{}

// errStoreDown 模拟存储不可用。
var errStoreDown = errors.New("store down")

func (f *failingIsolation) Get(context.Context, contract.SessionKey) (contract.IsolationHit, error) {
	return contract.IsolationHit{}, errStoreDown
}

func (f *failingIsolation) Put(context.Context, contract.SessionKey, contract.IsolationHit) error {
	return errStoreDown
}
