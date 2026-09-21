package responder

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"shen/common/core/internal/contract"
)

// ── 替身（MD-22）───────────────────────────────────────────────────────────

type stubContent struct {
	m   map[string][]byte
	err error
}

func newStubContent() *stubContent { return &stubContent{m: map[string][]byte{}} }

func (s *stubContent) Get(_ context.Context, key string) ([]byte, bool, error) {
	if s.err != nil {
		return nil, false, s.err
	}
	b, ok := s.m[key]
	return b, ok, nil
}

func (s *stubContent) Put(_ context.Context, key string, body []byte, _ time.Duration) error {
	if s.err != nil {
		return s.err
	}
	s.m[key] = append([]byte(nil), body...)
	return nil
}

func mustEngine(t *testing.T, cs ContentStore) *Engine {
	t.Helper()
	e, err := New(cs)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	return e
}

func req(session, resource string, kind contract.DecoyKind) contract.RespondRequest {
	return contract.RespondRequest{SessionID: session, Resource: resource, AssetID: "asset-1", Kind: kind}
}

// ── 一致性不变量（AR-30）──────────────────────────────────────────────────

func TestSameSessionResourceGivesIdenticalBody(t *testing.T) {
	e := mustEngine(t, newStubContent())
	ctx := context.Background()
	kinds := []contract.DecoyKind{
		contract.DecoyDeveloperAPI, contract.DecoyDataset,
		contract.DecoyInstructionFile, contract.DecoyBait,
	}
	for _, k := range kinds {
		first, err := e.Respond(ctx, req("s1", "GET /api/me?x=1", k))
		if err != nil {
			t.Fatalf("%s：失败：%v", k, err)
		}
		for i := 0; i < 5; i++ {
			again, _ := e.Respond(ctx, req("s1", "GET /api/me?x=1", k))
			if !bytes.Equal(first.Body, again.Body) {
				t.Fatalf("%s：同会话同资源必须逐字节一致（AR-30）\n第一次：%s\n第 %d 次：%s",
					k, first.Body, i, again.Body)
			}
			if first.Status != again.Status {
				t.Fatalf("%s：状态码也必须一致", k)
			}
		}
	}
}

func TestDifferentResourceMayDiffer(t *testing.T) {
	e := mustEngine(t, newStubContent())
	ctx := context.Background()
	a, _ := e.Respond(ctx, req("s1", "GET /api/user/1", contract.DecoyDataset))
	b, _ := e.Respond(ctx, req("s1", "GET /api/user/2", contract.DecoyDataset))
	if bytes.Equal(a.Body, b.Body) {
		t.Error("不同资源应有不同内容（否则多页抓取会立刻发现是同一份）")
	}
}

func TestBodyHasNoCurrentTime(t *testing.T) {
	// 模板不得依赖系统时钟 —— 否则同一资源两次请求会不同（AR-30 要求纯函数）。
	e := mustEngine(t, newStubContent())
	ctx := context.Background()
	for _, k := range []contract.DecoyKind{contract.DecoyDeveloperAPI, contract.DecoyDataset} {
		got, _ := e.Respond(ctx, req("s", "GET /a", k))
		if bytes.Contains(got.Body, []byte("2026")) || bytes.Contains(got.Body, []byte("2025")) {
			t.Errorf("%s：模板体不得含当前时间，得到 %s", k, got.Body)
		}
	}
}

// ── 预生成内容优先 ─────────────────────────────────────────────────────────

func TestPregeneratedContentWins(t *testing.T) {
	cs := newStubContent()
	ctx := context.Background()
	key := ConsistencyKey("s1", "GET /api/me")
	pregenerated := []byte(`{"from":"pregenerated"}`)
	if err := cs.Put(ctx, key, pregenerated, time.Minute); err != nil {
		t.Fatalf("写预生成内容失败：%v", err)
	}
	e := mustEngine(t, cs)
	got, err := e.Respond(ctx, req("s1", "GET /api/me", contract.DecoyDeveloperAPI))
	if err != nil {
		t.Fatalf("失败：%v", err)
	}
	if !bytes.Equal(got.Body, pregenerated) {
		t.Errorf("应优先使用预生成内容，得到 %s", got.Body)
	}
}

func TestStoreErrorFallsBackToTemplate(t *testing.T) {
	cs := newStubContent()
	cs.err = errors.New("pg down")
	e := mustEngine(t, cs)
	got, err := e.Respond(context.Background(), req("s", "GET /a", contract.DecoyBait))
	if err != nil {
		t.Fatalf("存储故障不得让响应生成失败（应回落模板）：%v", err)
	}
	if len(got.Body) == 0 {
		t.Error("回落模板应产出内容")
	}
}

// ── 响应形状（NI-9 / MD-23）────────────────────────────────────────────────

func TestNoNewCookiesOrHeaders(t *testing.T) {
	e := mustEngine(t, newStubContent())
	got, _ := e.Respond(context.Background(), req("s", "GET /a", contract.DecoyDeveloperAPI))
	if len(got.Headers) != 1 {
		t.Fatalf("只允许 Content-Type 一个响应头（NI-9），得到 %v", got.Headers)
	}
	if _, ok := got.Headers["Content-Type"]; !ok {
		t.Errorf("应设 Content-Type，得到 %v", got.Headers)
	}
	if got.Status != 200 {
		t.Errorf("默认状态码应为 200，得到 %d", got.Status)
	}
}

func TestContentTypePerKind(t *testing.T) {
	e := mustEngine(t, newStubContent())
	ctx := context.Background()
	html := []contract.DecoyKind{contract.DecoyDeveloperAPI, contract.DecoyDataset, contract.DecoyBait}
	for _, k := range html {
		got, _ := e.Respond(ctx, req("s", "GET /a", k))
		if got.Headers["Content-Type"] != "application/json; charset=utf-8" {
			t.Errorf("%s：应是 JSON，得到 %q", k, got.Headers["Content-Type"])
		}
	}
	txt, _ := e.Respond(ctx, req("s", "GET /a", contract.DecoyInstructionFile))
	if txt.Headers["Content-Type"] != "text/plain; charset=utf-8" {
		t.Errorf("指令文件应是 text/plain，得到 %q", txt.Headers["Content-Type"])
	}
}

// ── AR-22 / AR-23：内容黑名单 ───────────────────────────────────────────

func TestTemplatesPassBlacklist(t *testing.T) {
	// 回归保护：我们自己的模板**必须**过黑名单，否则合规内容永远发不出去。
	e := mustEngine(t, newStubContent())
	ctx := context.Background()
	kinds := []contract.DecoyKind{
		contract.DecoyDeveloperAPI, contract.DecoyInstructionFile,
		contract.DecoyMCP, contract.DecoyDataset, contract.DecoyBait,
	}
	for _, k := range kinds {
		got, err := e.Respond(ctx, req("s", "GET /a", k))
		if err != nil {
			t.Fatalf("%s：失败：%v", k, err)
		}
		if err := validateContent(got.Body); err != nil {
			t.Errorf("%s：模板自身命中了黑名单：%v\n%s", k, err, got.Body)
		}
	}
}

func TestBlacklistRejectsLeaks(t *testing.T) {
	cases := map[string]string{
		"私网地址":   `{"db":"10.1.2.3:5432"}`,
		"回环":     `{"url":"http://127.0.0.1:8080"}`,
		"内网主机名":  `{"host":"wiki.internal"}`,
		"真实文件路径": `{"path":"/etc/passwd"}`,
	}
	for name, body := range cases {
		if err := validateContent([]byte(body)); err == nil {
			t.Errorf("%s：应被拒（AR-22 泄露类）：%s", name, body)
		}
	}
}

func TestBlacklistRejectsSelfDisclosure(t *testing.T) {
	cases := []string{
		`{"note":"this is a honeypot"}`,
		`{"note":"蜜罐内容"}`,
		`{"note":"I am an AI assistant"}`,
		`{"note":"as a language model"}`,
		`{"note":"decoy"}`,
	}
	for _, body := range cases {
		if err := validateContent([]byte(body)); err == nil {
			t.Errorf("应被拒（AR-22 自曝类）：%s", body)
		}
	}
}

func TestBlacklistRejectsOversize(t *testing.T) {
	big := bytes.Repeat([]byte("a"), maxResponseBytes+1)
	if err := validateContent(big); err == nil {
		t.Error("超长内容应被拒（AR-23）")
	}
}

func TestBlacklistAcceptsCleanContent(t *testing.T) {
	clean := []byte(`{"content":"站点公开的开发者接口","tenant":"t-1a2b3c4d"}`)
	if err := validateContent(clean); err != nil {
		t.Errorf("干净内容不应被拒：%v", err)
	}
}

func TestUnsafePregeneratedContentFallsBackToTemplate(t *testing.T) {
	cs := newStubContent()
	ctx := context.Background()
	key := ConsistencyKey("s", "GET /api/me")
	unsafe := []byte(`{"note":"这是蜜罐"}`)
	if err := cs.Put(ctx, key, unsafe, time.Minute); err != nil {
		t.Fatalf("写内容失败：%v", err)
	}
	e := mustEngine(t, cs)
	got, err := e.Respond(ctx, req("s", "GET /api/me", contract.DecoyBait))
	if err != nil {
		t.Fatalf("失败：%v", err)
	}
	if bytes.Contains(got.Body, []byte("蜜罐")) {
		t.Errorf("不合格的预生成内容不得发给攻击者，得到 %s", got.Body)
	}
	if err := validateContent(got.Body); err != nil {
		t.Errorf("回落后的内容应合规：%v", err)
	}
}

// ── 参数校验与构造 ─────────────────────────────────────────────────────────

func TestRejectsEmptyKeys(t *testing.T) {
	e := mustEngine(t, newStubContent())
	ctx := context.Background()
	if _, err := e.Respond(ctx, req("", "GET /a", contract.DecoyBait)); err == nil {
		t.Error("空 SessionID 应被拒绝")
	}
	if _, err := e.Respond(ctx, req("s", "", contract.DecoyBait)); err == nil {
		t.Error("空 Resource 应被拒绝")
	}
}

func TestNewRejectsNil(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Error("ContentStore 为 nil 应构造失败")
	}
}

func TestConsistencyKeyStable(t *testing.T) {
	a := ConsistencyKey("s", "r")
	b := ConsistencyKey("s", "r")
	if a != b {
		t.Error("一致性键必须稳定")
	}
	if ConsistencyKey("s", "r") == ConsistencyKey("s", "r2") {
		t.Error("不同资源必须有不同一致性键")
	}
}
