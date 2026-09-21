package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	policyv1 "shen/common/api/policy/v1"
	"shen/common/core/internal/store"
)

// serverYAML = 合法配置 + 幻境后端池：策略面下发的边缘文档要有真实来源才测得出东西。
const serverYAML = validYAML + `honeypots:
  - name: "hp-b"
    type: "ssh"
    addr: "127.0.0.1:2222"
    enabled: true
  - name: "hp-a"
    type: "mysql"
    addr: "127.0.0.1:3306"
    enabled: false
`

func newServerFixture(t *testing.T) (*Server, *store.PolicyMemory) {
	t.Helper()
	ps := store.NewPolicyMemory()
	return NewServer(mustLoad(t, serverYAML), ps), ps
}

// TestPullProjectsEdgePayload 覆盖投影的三件事：schema 版本 · 三段数据（policy_id/version/backends/whitelist）· 校验和。
func TestPullProjectsEdgePayload(t *testing.T) {
	srv, _ := newServerFixture(t)
	ctx := context.Background()

	got, err := srv.Pull(ctx, &policyv1.PolicyPullRequest{})
	if err != nil {
		t.Fatalf("Pull 应当成功，却失败：%v", err)
	}

	// 校验和必须覆盖**下发的那串字节**（ST-8），否则适配器验不出被改过的载荷。
	sum := sha256.Sum256(got.GetPayload())
	if want := hex.EncodeToString(sum[:]); got.GetChecksum() != want {
		t.Errorf("checksum 必须等于 sha256(payload)：got %s want %s", got.GetChecksum(), want)
	}

	var doc struct {
		SchemaVersion int    `json:"schema_version"`
		PolicyID      string `json:"policy_id"`
		Version       uint64 `json:"version"`
		Backends      []struct {
			Name    string `json:"name"`
			Address string `json:"address"`
			Enabled bool   `json:"enabled"`
		} `json:"backends"`
		Whitelist struct {
			SourceCIDRs []string `json:"source_cidrs"`
		} `json:"whitelist"`
	}
	if err := json.Unmarshal(got.GetPayload(), &doc); err != nil {
		t.Fatalf("载荷必须是合法 JSON：%v\n%s", err, got.GetPayload())
	}

	if doc.SchemaVersion != EdgePayloadSchemaVersion {
		t.Errorf("schema_version 应为 %d，实际 %d", EdgePayloadSchemaVersion, doc.SchemaVersion)
	}
	if doc.PolicyID != "core-rules" || got.GetPolicyId() != "core-rules" {
		t.Errorf("policy_id 应为 core-rules：载荷 %q / 快照 %q", doc.PolicyID, got.GetPolicyId())
	}
	if doc.Version != 3 || got.GetVersion() != 3 {
		t.Errorf("version 应为 3：载荷 %d / 快照 %d", doc.Version, got.GetVersion())
	}
	// 后端与 loader 的池一致（顺序固定为按名排序 ⇒ 校验和稳定）。
	backends, err := mustLoad(t, serverYAML).Honeypots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Backends) != len(backends) {
		t.Fatalf("后端条数应一致：载荷 %d / 配置 %d", len(doc.Backends), len(backends))
	}
	if doc.Backends[0].Name != "hp-a" || doc.Backends[1].Name != "hp-b" {
		t.Errorf("后端应按名排序：%+v", doc.Backends)
	}
	if doc.Backends[1].Address != "127.0.0.1:2222" || !doc.Backends[1].Enabled {
		t.Errorf("后端字段投影有误：%+v", doc.Backends[1])
	}
	if len(doc.Whitelist.SourceCIDRs) != 1 || doc.Whitelist.SourceCIDRs[0] != "10.0.0.0/8" {
		t.Errorf("白名单应带 10.0.0.0/8：%+v", doc.Whitelist.SourceCIDRs)
	}
}

// TestPullIsDeterministic 同内容两次下发必须逐字节一致 —— 否则适配器每次轮询都会以为策略变了。
func TestPullIsDeterministic(t *testing.T) {
	srv, _ := newServerFixture(t)
	ctx := context.Background()

	a, err := srv.Pull(ctx, &policyv1.PolicyPullRequest{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := srv.Pull(ctx, &policyv1.PolicyPullRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if string(a.GetPayload()) != string(b.GetPayload()) {
		t.Errorf("两次 Pull 载荷不同：\n%s\n%s", a.GetPayload(), b.GetPayload())
	}
	if a.GetChecksum() != b.GetChecksum() {
		t.Errorf("两次 Pull 校验和不同：%s / %s", a.GetChecksum(), b.GetChecksum())
	}
}

// TestPullRejectsUnknownPolicyID：请求别的策略集时必须报错 —— 回一份「别的策略」比报错危险得多。
func TestPullRejectsUnknownPolicyID(t *testing.T) {
	srv, _ := newServerFixture(t)
	_, err := srv.Pull(context.Background(), &policyv1.PolicyPullRequest{PolicyId: "someone-else"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("应返回 NotFound，实际 %v", err)
	}
}

// TestWatchIsUnimplemented：本轮不做流式推送（ADR-0018），必须显式告诉调用方，不能静默挂住。
func TestWatchIsUnimplemented(t *testing.T) {
	srv, _ := newServerFixture(t)
	if err := srv.Watch(&policyv1.PolicyWatchRequest{}, nil); status.Code(err) != codes.Unimplemented {
		t.Fatalf("应返回 Unimplemented，实际 %v", err)
	}
}

// TestAckRecordsAndIsIdempotent 覆盖 AR-13 的对账语义：
// ① 回执落库（谁应用了哪个版本）；② 同一适配器+版本只留最新一条；③ 缺 adapter_id 拒收。
func TestAckRecordsAndIsIdempotent(t *testing.T) {
	srv, ps := newServerFixture(t)
	ctx := context.Background()

	ack := func(adapter string, applied bool, reason string) *policyv1.PolicyAck {
		return &policyv1.PolicyAck{
			PolicyId: "core-rules", Version: 3, AdapterId: adapter, Applied: applied, Reason: reason,
		}
	}

	if _, err := srv.Ack(ctx, ack("proxy@10.0.0.5:8081", true, "")); err != nil {
		t.Fatalf("首次回执应当成功：%v", err)
	}
	// 同一适配器重报（重启后常见）→ 覆盖，不新增。
	if _, err := srv.Ack(ctx, ack("proxy@10.0.0.5:8081", false, "校验和不匹配")); err != nil {
		t.Fatalf("重报应当成功：%v", err)
	}
	if _, err := srv.Ack(ctx, ack("proxy@10.0.0.6:8081", true, "")); err != nil {
		t.Fatalf("另一适配器回执应当成功：%v", err)
	}

	got, err := ps.Acks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("回执应去重成 2 条（每适配器一条），实际 %d 条：%+v", len(got), got)
	}
	var failed int
	for _, a := range got {
		if a.PolicyID != "core-rules" || a.Version != 3 {
			t.Errorf("回执版本信息有误：%+v", a)
		}
		if !a.Applied {
			failed++
			if a.Reason == "" {
				t.Error("applied=false 的回执必须带原因，否则运营不知道发生了什么")
			}
		}
	}
	if failed != 1 {
		t.Errorf("应有 1 条「未应用」回执，实际 %d", failed)
	}

	// 无法对账的回执（没写谁报的）必须被拒 —— 否则台账里会堆一堆匿名记录。
	if _, err := srv.Ack(ctx, ack("", true, "")); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("缺 adapter_id 应返回 InvalidArgument，实际 %v", err)
	}
}

// TestInjectsSectionValidation：`injects` 段的校验（`ST-24` 的数据，非法必须让启动失败）。
func TestInjectsSectionValidation(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{name: "合法（带分类与标记）", yaml: "injects:\n  - kind: \"developer_api\"\n    snippet: \"<!-- a -->\"\n    marker: \"</head>\"\n"},
		{name: "合法（省略分类与标记）", yaml: "injects:\n  - snippet: \"<!-- b -->\"\n"},
		{name: "空片段", yaml: "injects:\n  - kind: \"developer_api\"\n    snippet: \"\"\n", wantErr: "snippet 不能为空"},
		{name: "片段只有空白", yaml: "injects:\n  - snippet: \"   \"\n", wantErr: "snippet 不能为空"},
		{name: "未登记的分类", yaml: "injects:\n  - kind: \"whatever\"\n    snippet: \"<!-- c -->\"\n", wantErr: "未登记"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(strings.NewReader(serverYAML + tc.yaml))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("应当通过校验，却失败：%v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("应当报错（含 %q），却通过了", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("错误应当包含 %q，实际：%v", tc.wantErr, err)
			}
		})
	}
}

// TestInjectsProvidedFlag：「未配置」与「配置为空数组」必须可区分（运营靠它主动关掉注入）。
func TestInjectsProvidedFlag(t *testing.T) {
	ctx := context.Background()

	l := mustLoad(t, serverYAML)
	rules, provided, err := l.Injects(ctx)
	if err != nil || provided || rules != nil {
		t.Fatalf("未配置时应当 provided=false 且无规则：provided=%v rules=%v err=%v", provided, rules, err)
	}

	l = mustLoad(t, serverYAML+"injects: []\n")
	rules, provided, err = l.Injects(ctx)
	if err != nil || !provided || len(rules) != 0 {
		t.Fatalf("显式空数组应当 provided=true 且零规则：provided=%v rules=%v err=%v", provided, rules, err)
	}
}

// TestPullCarriesInjectRules：载荷必须带上 `inject_rules`，且**保持配置顺序**（注入按序执行）。
func TestPullCarriesInjectRules(t *testing.T) {
	srv := NewServer(mustLoad(t, serverYAML+"injects:\n  - kind: \"developer_api\"\n    snippet: \"<!-- first -->\"\n  - snippet: \"<!-- second -->\"\n    marker: \"</head>\"\n"), store.NewPolicyMemory())

	got, err := srv.Pull(context.Background(), &policyv1.PolicyPullRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		InjectRules *[]struct {
			Kind    string `json:"kind"`
			Snippet string `json:"snippet"`
			Marker  string `json:"marker"`
		} `json:"inject_rules"`
	}
	if err := json.Unmarshal(got.GetPayload(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.InjectRules == nil {
		t.Fatal("配置了 injects 时载荷必须带 inject_rules")
	}
	rules := *doc.InjectRules
	if len(rules) != 2 {
		t.Fatalf("应当下发 2 条规则，实际 %d", len(rules))
	}
	if rules[0].Snippet != "<!-- first -->" || rules[1].Snippet != "<!-- second -->" {
		t.Errorf("注入顺序必须与配置一致（顺序是语义的一部分）：%+v", rules)
	}
	if rules[0].Kind != "developer_api" || rules[0].Marker != "" {
		t.Errorf("字段投影有误：%+v", rules[0])
	}
	if rules[1].Marker != "</head>" {
		t.Errorf("标记应当原样下发：%+v", rules[1])
	}
}

// TestPullOmitsInjectRulesWhenUnconfigured：未配置该段时**省略**字段（适配器继续用本地 env 规则）。
func TestPullOmitsInjectRulesWhenUnconfigured(t *testing.T) {
	srv, _ := newServerFixture(t)
	got, err := srv.Pull(context.Background(), &policyv1.PolicyPullRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got.GetPayload()), "inject_rules") {
		t.Errorf("未配置 injects 时不得出现 inject_rules 字段（否则适配器会误以为要关掉本地规则）：%s", got.GetPayload())
	}
}

// TestPullEmitsExplicitEmptyInjectRules：配置为空数组时**显式下发**空数组。
func TestPullEmitsExplicitEmptyInjectRules(t *testing.T) {
	srv := NewServer(mustLoad(t, serverYAML+"injects: []\n"), store.NewPolicyMemory())
	got, err := srv.Pull(context.Background(), &policyv1.PolicyPullRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got.GetPayload()), `"inject_rules":[]`) {
		t.Errorf("显式空数组必须原样下发，实际：%s", got.GetPayload())
	}
}
