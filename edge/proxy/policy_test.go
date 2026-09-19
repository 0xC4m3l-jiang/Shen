package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"google.golang.org/grpc"

	judgev1 "shen/api/judge/v1"
	policyv1 "shen/api/policy/v1"
)

// 注意：本包在 `edge/` 下，**禁止** import `core/internal/*`（ST-3，archcheck 在编译期核对）。
// 因此载荷结构与核心侧 `policy.edgeDoc` 是两处手工对齐的副本 —— 契约文档：docs/spec/policy-payload.md。

// ── 替身（MD-22）─────────────────────────────────────────────────────────────

// stubPolicy 是策略面客户端的替身：返回固定载荷，记录回执。
//
// 带锁是**必须**的：轮询跑在独立 goroutine 里（`runPolicyPolling`），
// 测试 goroutine 同时读回执 —— 不加锁就是数据竞争（`-race` 会抓）。
type stubPolicy struct {
	mu    sync.Mutex
	snap  *policyv1.PolicySnapshot
	err   error
	acks  []*policyv1.PolicyAck
	calls int
}

func (s *stubPolicy) Pull(_ context.Context, _ *policyv1.PolicyPullRequest, _ ...grpc.CallOption) (*policyv1.PolicySnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.snap, nil
}

func (s *stubPolicy) Ack(_ context.Context, in *policyv1.PolicyAck, _ ...grpc.CallOption) (*policyv1.PolicyAckReply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acks = append(s.acks, in)
	return &policyv1.PolicyAckReply{Ok: true}, nil
}

// ackList 返回回执快照（加锁读，避免与轮询 goroutine 竞争）。
func (s *stubPolicy) ackList() []*policyv1.PolicyAck {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*policyv1.PolicyAck(nil), s.acks...)
}

// policyFixture 造一份合法的策略载荷（schema v1），checksum 与 payload 自洽。
func policyFixture(t *testing.T, version uint64, backends []policyBackend, cidrs []string) (*policyv1.PolicySnapshot, []byte) {
	t.Helper()
	doc := edgePolicy{
		SchemaVersion: EdgePolicySchemaVersion,
		PolicyID:      "core-rules",
		Version:       version,
		Backends:      backends,
		Whitelist:     policyWhitelist{SourceCIDRs: cidrs},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return &policyv1.PolicySnapshot{
		PolicyId: "core-rules", Version: version, Payload: raw, Checksum: hex.EncodeToString(sum[:]),
	}, raw
}

// remoteTestHandler 造一个已装配远端后端构造器的 Handler（后端用替身，不依赖真实 Caddy）。
func remoteTestHandler(t *testing.T, cfg Config) *Handler {
	t.Helper()
	h := newTestHandler(t, cfg, &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}}, nil)
	h.buildRemote = func(name, _ string) (caddyhttp.MiddlewareHandler, error) { return fakeBackend{name: name}, nil }
	return h
}

// ── 应用语义（ADR-0018）─────────────────────────────────────────────────────

// policyFixtureWithInjects 造一份**带指定注入规则形态**的载荷。
//
//	injectJSON == ""   → 字段**缺省**（适配器应继续用本地规则）
//	injectJSON == "[]" → **显式空数组**（适配器应关掉注入）
//	其它               → 按给定 JSON 作为 `inject_rules` 的值
func policyFixtureWithInjects(t *testing.T, version uint64, injectJSON string) (*policyv1.PolicySnapshot, []byte) {
	t.Helper()
	doc := map[string]any{
		"schema_version": EdgePolicySchemaVersion,
		"policy_id":      "core-rules",
		"version":        version,
		"backends":       []any{},
		"whitelist":      map[string]any{"source_cidrs": []string{}},
	}
	if injectJSON != "" {
		var v any
		if err := json.Unmarshal([]byte(injectJSON), &v); err != nil {
			t.Fatalf("夹具注入片段不是合法 JSON：%v", err)
		}
		doc["inject_rules"] = v
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return &policyv1.PolicySnapshot{
		PolicyId: "core-rules", Version: version, Payload: raw, Checksum: hex.EncodeToString(sum[:]),
	}, raw
}

// TestApplyEdgePolicyInjectSemantics：注入规则三种下发形态的合并语义（ADR-0018 / spec 载荷契约）。
func TestApplyEdgePolicyInjectSemantics(t *testing.T) {
	ctx := context.Background()
	local := &stubInjector{marker: "<!--LOCAL-->"}

	t.Run("字段缺省 → 保留本地规则", func(t *testing.T) {
		h := remoteTestHandler(t, Config{})
		h.injector = local
		_, raw := policyFixtureWithInjects(t, 20, "")
		if err := h.applyEdgePolicy(ctx, raw); err != nil {
			t.Fatal(err)
		}
		if h.currentInjector() != Injector(local) {
			t.Error("未下发 inject_rules 时，本地 env 规则必须继续生效")
		}
	})

	t.Run("显式空数组 → 关掉注入", func(t *testing.T) {
		h := remoteTestHandler(t, Config{})
		h.injector = local
		_, raw := policyFixtureWithInjects(t, 21, "[]")
		if err := h.applyEdgePolicy(ctx, raw); err != nil {
			t.Fatal(err)
		}
		if h.currentInjector() != nil {
			t.Error("显式空数组应当关掉注入（运营要有主动关闭的手段）")
		}
	})

	t.Run("非空 → 用远端规则", func(t *testing.T) {
		h := remoteTestHandler(t, Config{})
		h.injector = local
		_, raw := policyFixtureWithInjects(t, 22, `[{"kind":"developer_api","snippet":"<!--REMOTE-->"}]`)
		if err := h.applyEdgePolicy(ctx, raw); err != nil {
			t.Fatal(err)
		}
		inj := h.currentInjector()
		if inj == nil || inj == Injector(local) {
			t.Fatal("下发非空规则时必须改用远端规则")
		}
		if _, ok := inj.Inject("text/html", []byte("<html><body></body></html>")); !ok {
			t.Error("远端规则应当能改写 HTML")
		}
	})
}

// TestRemoteInjectRuleRewritesDivertedResponse：注入真的落在**改道侧**响应上，
// 且三种形态（本地规则 / 显式关掉 / 远端规则）行为可区分。
func TestRemoteInjectRuleRewritesDivertedResponse(t *testing.T) {
	ctx := context.Background()
	h := &Handler{injector: &stubInjector{marker: "<!--LOCAL-->"}}
	tr := &injectingTransport{handler: h, base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResp("<html><body>hi</body></html>"), nil
	})}
	get := func() string {
		t.Helper()
		resp, err := tr.RoundTrip(httptest.NewRequest(http.MethodGet, "http://x/", nil))
		if err != nil {
			t.Fatalf("RoundTrip 失败：%v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}

	if got := get(); !strings.Contains(got, "<!--LOCAL-->") {
		t.Fatalf("未接策略面时应注入本地规则，得到 %q", got)
	}

	// 显式空数组：运营主动关掉注入。
	_, raw := policyFixtureWithInjects(t, 30, "[]")
	if err := h.applyEdgePolicy(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if got := get(); strings.Contains(got, "<!--LOCAL-->") || strings.Contains(got, "<!--REMOTE-->") {
		t.Errorf("显式空数组必须完全关掉注入，得到 %q", got)
	}

	// 远端规则接管：本地规则不再生效。
	_, raw = policyFixtureWithInjects(t, 31, `[{"snippet":"<!--REMOTE-->","marker":"</body>"}]`)
	if err := h.applyEdgePolicy(ctx, raw); err != nil {
		t.Fatal(err)
	}
	got := get()
	if !strings.Contains(got, "<!--REMOTE-->") {
		t.Errorf("远端规则应当生效，得到 %q", got)
	}
	if strings.Contains(got, "<!--LOCAL-->") {
		t.Errorf("远端接管后本地规则**不得**再注入，得到 %q", got)
	}
}

// TestApplyEdgePolicyOverlaysBackends：远端后端按名覆盖，本地独有的项保留（兜底）。
func TestApplyEdgePolicyOverlaysBackends(t *testing.T) {
	h := remoteTestHandler(t, Config{Mirage: map[string]string{"local-only": "http://127.0.0.1:1"}})

	_, raw := policyFixture(t, 7, []policyBackend{
		{Name: "hp-a", Address: "http://127.0.0.1:2222", Enabled: true},
		{Name: "hp-off", Address: "http://127.0.0.1:3333", Enabled: false},
	}, nil)
	if err := h.applyEdgePolicy(context.Background(), raw); err != nil {
		t.Fatalf("应用策略应当成功：%v", err)
	}

	if _, ok := h.mirageHandler("hp-a"); !ok {
		t.Error("远端启用的后端应当可用")
	}
	if _, ok := h.mirageHandler("hp-off"); ok {
		t.Error("enabled=false 的后端不得入表")
	}
	if _, ok := h.mirageHandler("local-only"); !ok {
		t.Error("本地独有的后端必须保留（策略面拉不到时的兜底）")
	}
	if got := h.remoteVersion(); got != 7 {
		t.Errorf("已应用版本应为 7，实际 %d", got)
	}
}

// TestApplyEdgePolicyRejectsBadInput：schema 读不懂 / JSON 非法 → 整份拒绝；
// 坏地址 → 只丢那一条（整表作废会让所有改道一起失效）。
func TestApplyEdgePolicyRejectsBadInput(t *testing.T) {
	h := remoteTestHandler(t, Config{})

	if err := h.applyEdgePolicy(context.Background(), []byte("{not json")); err == nil {
		t.Error("非法 JSON 应当报错")
	}
	future := []byte(`{"schema_version":99,"policy_id":"x","version":1,"backends":[],"whitelist":{"source_cidrs":[]}}`)
	if err := h.applyEdgePolicy(context.Background(), future); err == nil {
		t.Error("读不懂的 schema 版本应当报错（禁止猜着应用）")
	}
	if h.remoteVersion() != 0 {
		t.Error("失败的载荷不得改变已生效策略")
	}

	_, raw := policyFixture(t, 8, []policyBackend{
		{Name: "bad", Address: "127.0.0.1:2222", Enabled: true}, // 缺 scheme
		{Name: "good", Address: "http://127.0.0.1:2222", Enabled: true},
	}, nil)
	if err := h.applyEdgePolicy(context.Background(), raw); err != nil {
		t.Fatalf("坏地址不应让整份策略作废：%v", err)
	}
	if _, ok := h.mirageHandler("bad"); ok {
		t.Error("非法地址的后端不得入表")
	}
	if _, ok := h.mirageHandler("good"); !ok {
		t.Error("合法后端应当入表")
	}
}

// TestRemoteWhitelistIsAdditive：远端白名单只增不减（并集）—— 护栏缩小会把运维探针判成攻击者（INT-25）。
func TestRemoteWhitelistIsAdditive(t *testing.T) {
	h := remoteTestHandler(t, Config{Whitelist: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}})

	_, raw := policyFixture(t, 9, nil, []string{"192.168.0.0/16"})
	if err := h.applyEdgePolicy(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	if !h.remoteWhitelisted("192.168.5.5") {
		t.Error("远端白名单应当生效")
	}
	if h.remoteWhitelisted("203.0.113.9") {
		t.Error("不在名单里的地址不得命中")
	}
	if !whitelisted("10.1.2.3", h.whitelist) {
		t.Error("本地白名单必须保留（不是被远端替换掉）")
	}
}

// TestRemoteBackendIsUsedByRequestPath：命中远端后端名时，请求真的走远端那条后端。
func TestRemoteBackendIsUsedByRequestPath(t *testing.T) {
	judge := &stubJudge{resp: &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_MIRAGE, Backend: "remote-hp"}}
	h := newTestHandler(t, Config{}, judge, nil)
	h.buildRemote = func(name, _ string) (caddyhttp.MiddlewareHandler, error) { return fakeBackend{name: name}, nil }

	// 应用前：后端名不在表里 → 回落业务（NI-5）。
	w := do(h, "GET", "http://site.example/x", "", nil)
	if got := w.Header().Get("X-Backend"); got != "origin" {
		t.Fatalf("应用策略前后端名未知，应回落 origin，实际 %q", got)
	}

	_, raw := policyFixture(t, 10, []policyBackend{
		{Name: "remote-hp", Address: "http://127.0.0.1:2222", Enabled: true},
	}, nil)
	if err := h.applyEdgePolicy(context.Background(), raw); err != nil {
		t.Fatal(err)
	}

	w = do(h, "GET", "http://site.example/x", "", nil)
	if got := w.Header().Get("X-Backend"); got != "remote-hp" {
		t.Errorf("应用策略后应改道到远端后端，实际 %q", got)
	}
}

// TestPullFailureKeepsCurrentPolicy：拉取失败**只影响策略**，请求照常走本地配置（NI-1）。
func TestPullFailureKeepsCurrentPolicy(t *testing.T) {
	h := remoteTestHandler(t, Config{Mirage: map[string]string{"local": "http://127.0.0.1:1"}})
	stub := &stubPolicy{err: errors.New("connection refused")}
	h.policy = stub
	h.AdapterID = "proxy@test"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go h.runPolicyPolling(ctx, 50*time.Millisecond)
	time.Sleep(120 * time.Millisecond)

	if h.remoteVersion() != 0 {
		t.Error("拉取失败时不得改变已生效策略")
	}
	if _, ok := h.mirageHandler("local"); !ok {
		t.Error("拉取失败时本地后端必须仍然可用")
	}
	if n := len(stub.ackList()); n != 0 {
		t.Errorf("拉取失败不该产生回执，实际 %d 条", n)
	}
}

// TestPolicyAppliedAndAcked：成功应用后必须回执 applied=true 且带适配器标识（AR-13），且同版本不重复回执。
func TestPolicyAppliedAndAcked(t *testing.T) {
	snap, _ := policyFixture(t, 11, []policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}}, nil)
	stub := &stubPolicy{snap: snap}
	h := remoteTestHandler(t, Config{})
	h.policy = stub
	h.AdapterID = "proxy@10.0.0.5:8081"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go h.runPolicyPolling(ctx, 30*time.Millisecond)
	time.Sleep(150 * time.Millisecond)

	if h.remoteVersion() != 11 {
		t.Fatalf("应当应用版本 11，实际 %d", h.remoteVersion())
	}
	acks := stub.ackList()
	if len(acks) != 1 {
		t.Fatalf("应用成功应回执且同版本不重复，实际 %d 条", len(acks))
	}
	a := acks[0]
	if !a.GetApplied() || a.GetAdapterId() != "proxy@10.0.0.5:8081" || a.GetVersion() != 11 {
		t.Errorf("回执内容有误：%+v", a)
	}
}

// TestPolicyChecksumMismatchIsRejected：载荷被改过 → 拒绝应用并回执 applied=false（ST-8）。
func TestPolicyChecksumMismatchIsRejected(t *testing.T) {
	snap, raw := policyFixture(t, 12, []policyBackend{{Name: "hp", Address: "http://127.0.0.1:2222", Enabled: true}}, nil)
	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)-2] = 'x' // 改一个字节，checksum 不再匹配
	snap.Payload = tampered

	stub := &stubPolicy{snap: snap}
	h := remoteTestHandler(t, Config{})
	h.policy = stub
	h.AdapterID = "proxy@test"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go h.runPolicyPolling(ctx, 30*time.Millisecond)
	time.Sleep(120 * time.Millisecond)

	if h.remoteVersion() != 0 {
		t.Error("校验和不匹配时不得应用")
	}
	acks := stub.ackList()
	if len(acks) != 1 || acks[0].GetApplied() {
		t.Fatalf("必须回执 applied=false，实际 %+v", acks)
	}
	if acks[0].GetReason() == "" {
		t.Error("applied=false 必须带原因")
	}
}
