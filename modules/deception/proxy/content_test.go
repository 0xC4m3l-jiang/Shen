package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 本文件守住 AI 欺骗内容的**消费侧**（`ADR-0023` / `AR-33`）：
// 确定性命中（`AR-30`）· 会话钉定 · 开关取与 · 注入只在改道侧 · 任何失败都不阻断（`NI-1`）。
// 契约见 docs/spec/policy-payload.md §2 与 docs/spec/events.md §2.2。

const (
	testResource = "/api/users"
	testMarker   = "</body>"
)

func testManifest(variants int) *contentManifest {
	entries := []contentEntry{{Resource: testResource, ProfileID: "site-a"}}
	for v := 0; v < variants; v++ {
		body := "<section>content-" + string(rune('0'+v)) + "</section>"
		entries[0].Bodies = append(entries[0].Bodies, contentBody{
			VariantID: v,
			ContentID: "c-" + string(rune('0'+v)),
			Checksum:  checksumOf(body),
			Body:      body,
			Marker:    testMarker,
		})
	}
	return &contentManifest{Version: 1, Selector: selectorSession, Variants: variants, Entries: entries}
}

// contentHandler 造一个「内容注入可用」的 Handler（策略面已给清单 + 开关都开）。
func contentHandler(t *testing.T, m *contentManifest, enabled bool) *Handler {
	t.Helper()
	now := time.Now
	h := &Handler{InjectContent: true}
	h.now = now
	h.pins = newVariantPins(time.Minute, 16, now)
	h.remote.Store(&remoteState{injectEnabled: enabled, content: newContentIndex(m)})
	return h
}

// ── 变体选择：确定性 + 分布（AR-30 / ADR-0016）──────────────────────────────

func TestVariantForIsDeterministic(t *testing.T) {
	for _, session := range []string{"sid=a", "sid=b", "", "sid=长中文会话"} {
		first := variantFor(session, 8)
		if second := variantFor(session, 8); first != second {
			t.Fatalf("同一会话两次得到不同槽位：%d vs %d（AR-30 禁止响应路径非确定性）", first, second)
		}
		if first < 0 || first >= 8 {
			t.Fatalf("槽位越界：%d", first)
		}
	}
	if got := variantFor("任意", 1); got != 0 {
		t.Fatalf("N=1 时槽位必须恒为 0，实际 %d", got)
	}
}

func TestVariantForDistributesAcrossSessions(t *testing.T) {
	// 跨会话要分散到多个变体（多态的意义）；只命中一个槽位说明哈希塌了。
	seen := map[int]bool{}
	for i := 0; i < 200; i++ {
		seen[variantFor("sid="+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('0'+i%10)), 8)] = true
	}
	if len(seen) < 4 {
		t.Fatalf("200 个会话只落在 %d 个变体上（多态形同虚设）", len(seen))
	}
}

func TestVariantPinsHonorTTLAndVersion(t *testing.T) {
	now := time.Now()
	pins := newVariantPins(time.Minute, 4, func() time.Time { return now })
	pins.put("sid=a", 3, 1)

	if v, ok := pins.get("sid=a", 1); !ok || v != 3 {
		t.Fatalf("钉定应命中：v=%d ok=%v", v, ok)
	}
	// 换代（清单 version 变了）⇒ 钉定失效并重算；槽位由哈希决定 ⇒ 重算必得同值（不漂移）。
	if _, ok := pins.get("sid=a", 2); ok {
		t.Error("清单版本变了以后旧钉定必须失效（否则槽位选择会钉在旧清单上）")
	}
	pins.put("sid=a", 3, 2)
	now = now.Add(2 * time.Minute)
	if _, ok := pins.get("sid=a", 2); ok {
		t.Error("钉定必须过期（TTL = 判定缓存窗）")
	}
}

func TestVariantPinsCapacityIsBounded(t *testing.T) {
	now := time.Now()
	pins := newVariantPins(time.Minute, 2, func() time.Time { return now })
	pins.put("a", 0, 1)
	pins.put("b", 1, 1)
	pins.put("c", 2, 1) // 满则清空
	if len(pins.m) != 1 {
		t.Fatalf("钉定缓存必须有上限（MD-10）：实际 %d 条", len(pins.m))
	}
}

func TestVariantOfSessionPinsFirstResult(t *testing.T) {
	h := contentHandler(t, testManifest(8), true)
	idx := h.remotePolicy().content
	session := "sid=fixed"
	want := variantFor(session, 8)
	if got := h.variantOfSession(session, idx); got != want {
		t.Fatalf("首次选择应为哈希值 %d，实际 %d", want, got)
	}
	// 手工把钉定改成另一个槽位：后续选择必须**沿用钉定**（会话内冻结）。
	h.pins.put(session, (want+1)%8, idx.version)
	if got := h.variantOfSession(session, idx); got != (want+1)%8 {
		t.Fatalf("钉定命中时必须复用，实际 %d", got)
	}
}

// ── 索引与命中 ──────────────────────────────────────────────────────────────

func TestNewContentIndexRejectsBadShapes(t *testing.T) {
	cases := map[string]*contentManifest{
		"nil":       nil,
		"selector":  {Version: 1, Selector: "cookie", Variants: 8},
		"variants0": {Version: 1, Selector: selectorSession, Variants: 0},
		"无可用条目":     {Version: 1, Selector: selectorSession, Variants: 8, Entries: []contentEntry{{Resource: ""}}},
	}
	for name, m := range cases {
		if idx := newContentIndex(m); idx != nil {
			t.Errorf("%s：应返回 nil（= 没有可用内容），实际 %+v", name, idx)
		}
	}
}

func TestNewContentIndexSkipsBadBodies(t *testing.T) {
	m := testManifest(2)
	m.Entries[0].Bodies = append(m.Entries[0].Bodies,
		contentBody{VariantID: 9, ContentID: "c-bad", Checksum: "x", Body: "越界的变体"},
		contentBody{VariantID: 1, ContentID: "c-empty", Checksum: "x", Body: ""},
	)
	idx := newContentIndex(m)
	if idx == nil {
		t.Fatal("有可用内容时不应返回 nil")
	}
	if n := len(idx.entries[testResource]); n != 2 {
		t.Fatalf("坏条必须被跳过（越界 + 空体），剩下 2 条，实际 %d", n)
	}
}

func TestContentForMatchesExactPathAndVerifiesChecksum(t *testing.T) {
	h := contentHandler(t, testManifest(8), true)
	idx := h.remotePolicy().content

	req := httptest.NewRequest("GET", "http://shop.example.com"+testResource, nil)
	if _, ok := h.contentFor(req, idx); !ok {
		t.Fatal("精确路径应命中")
	}
	// 前缀不算命中（阶段 A 不做前缀匹配）。
	other := httptest.NewRequest("GET", "http://shop.example.com"+testResource+"/42", nil)
	if _, ok := h.contentFor(other, idx); ok {
		t.Errorf("阶段 A 只做精确匹配：%s/42 不该命中", testResource)
	}

	// 校验和不符 ⇒ 丢弃（宁可漏注入，不可注入错内容）。
	bad := testManifest(1)
	bad.Entries[0].Bodies[0].Checksum = checksumOf("别的字节")
	h2 := contentHandler(t, bad, true)
	if _, ok := h2.contentFor(req, h2.remotePolicy().content); ok {
		t.Error("校验和不符的内容必须被丢弃")
	}
}

// ── 注入本身 ────────────────────────────────────────────────────────────────

func TestInjectContentInsertsBeforeMarker(t *testing.T) {
	h := contentHandler(t, testManifest(8), true)
	idx := h.remotePolicy().content
	req := httptest.NewRequest("GET", "http://shop.example.com"+testResource, nil)
	req.Header.Set("Cookie", "sid=a")
	page := []byte("<html><body>原页面</body></html>")

	out, contentID, changed := h.injectContent(req, idx, "text/html; charset=utf-8", page)
	if !changed {
		t.Fatal("应当发生改写")
	}
	if contentID == "" {
		t.Error("改写成功必须带上 content_id（否则图上定位不到是哪份内容）")
	}
	if !strings.Contains(string(out), "原页面") {
		t.Error("原页面内容不应被抹掉（改写是插入，不是替换）")
	}
	if !strings.Contains(string(out), "</section>") {
		t.Errorf("注入片段未出现在响应里：%s", out)
	}
	if strings.Index(string(out), "</section>") > strings.Index(string(out), testMarker) {
		t.Error("片段必须插在标记之前（edge-injection 的既有语义）")
	}
}

func TestInjectContentLeavesNonHTMLAlone(t *testing.T) {
	h := contentHandler(t, testManifest(8), true)
	idx := h.remotePolicy().content
	req := httptest.NewRequest("GET", "http://shop.example.com"+testResource, nil)
	page := []byte(`{"json":true}</body>`)

	out, _, changed := h.injectContent(req, idx, "application/json", page)
	if changed {
		t.Error("非 HTML 不得改写")
	}
	if string(out) != string(page) {
		t.Error("非 HTML 必须原样返回")
	}
}

func TestInjectContentSkippedWhenMarkerMissing(t *testing.T) {
	h := contentHandler(t, testManifest(8), true)
	idx := h.remotePolicy().content
	req := httptest.NewRequest("GET", "http://shop.example.com"+testResource, nil)
	page := []byte("<html><body>没有结束标记（故意缺 marker）")

	_, _, changed := h.injectContent(req, idx, "text/html", page)
	if changed {
		t.Error("找不到标记时必须跳过（不阻断响应）")
	}
}

// ── transport 层：开关取值与上报 ─────────────────────────────────────────────

type stubTransport struct {
	resp *http.Response
	err  error
}

func (s stubTransport) RoundTrip(*http.Request) (*http.Response, error) { return s.resp, s.err }

func htmlResponse(body string) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": {"text/html; charset=utf-8"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

// runTransport 跑一次注入 transport，返回（响应体, 注入结果, 内容标识）。
func runTransport(t *testing.T, h *Handler, page string) (string, string, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "http://shop.example.com"+testResource, nil)
	req.Header.Set("Cookie", "sid=a")
	outcome := &injectOutcome{}
	req = req.WithContext(withInjectOutcome(req.Context(), outcome))

	tr := &injectingTransport{src: h, base: stubTransport{resp: htmlResponse(page)}}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("transport 不应失败：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读响应体失败：%v", err)
	}
	status, contentID := outcome.get()
	return string(raw), status, contentID
}

func TestTransportReportsApplied(t *testing.T) {
	h := contentHandler(t, testManifest(8), true)
	body, status, contentID := runTransport(t, h, "<html><body>真页面</body></html>")
	if status != InjectApplied {
		t.Fatalf("开关都开 + 命中 + 可改写 ⇒ %s，实际 %s", InjectApplied, status)
	}
	if contentID == "" || !strings.Contains(body, "</section>") {
		t.Fatalf("应注入内容片段并带 content_id：id=%q body=%s", contentID, body)
	}
}

func TestTransportReportsDisabledWhenSwitchOff(t *testing.T) {
	// 策略说开、本地兜底关（默认）⇒ disabled，且**完全不动响应体**（ADR-0023 决定 4）。
	h := contentHandler(t, testManifest(8), true)
	h.InjectContent = false
	page := "<html><body>真页面</body></html>"
	body, status, contentID := runTransport(t, h, page)
	if status != InjectDisabled {
		t.Fatalf("本地开关关闭 ⇒ %s，实际 %s", InjectDisabled, status)
	}
	if body != page || contentID != "" {
		t.Fatalf("关闭时响应必须逐字节不变：%q", body)
	}

	// 本地开、下发级关 ⇒ 同样是 disabled。
	h2 := contentHandler(t, testManifest(8), false)
	_, status2, _ := runTransport(t, h2, page)
	if status2 != InjectDisabled {
		t.Fatalf("下发级开关关闭 ⇒ %s，实际 %s", InjectDisabled, status2)
	}
}

func TestTransportReportsNoContent(t *testing.T) {
	// 开关都开，但资源没命中 ⇒ no_content，响应不变。
	h := contentHandler(t, testManifest(8), true)
	req := httptest.NewRequest("GET", "http://shop.example.com/别的路径", nil)
	outcome := &injectOutcome{}
	req = req.WithContext(withInjectOutcome(req.Context(), outcome))
	tr := &injectingTransport{src: h, base: stubTransport{resp: htmlResponse("<html><body>x</body></html>")}}
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("transport 不应失败：%v", err)
	}
	if status, _ := outcome.get(); status != InjectNoContent {
		t.Fatalf("未命中资源 ⇒ %s，实际 %s", InjectNoContent, status)
	}

	// 非 HTML 响应 ⇒ 同样是 no_content。
	h2 := contentHandler(t, testManifest(8), true)
	req2 := httptest.NewRequest("GET", "http://shop.example.com"+testResource, nil)
	outcome2 := &injectOutcome{}
	req2 = req2.WithContext(withInjectOutcome(req2.Context(), outcome2))
	tr2 := &injectingTransport{src: h2, base: stubTransport{resp: &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": {"application/json"}},
		Body:          io.NopCloser(strings.NewReader("{}")),
		ContentLength: 2,
	}}}
	if _, err := tr2.RoundTrip(req2); err != nil {
		t.Fatalf("transport 不应失败：%v", err)
	}
	if status, _ := outcome2.get(); status != InjectNoContent {
		t.Fatalf("非 HTML ⇒ %s，实际 %s", InjectNoContent, status)
	}
}

// ── 策略载荷解析（契约字段名）───────────────────────────────────────────────

func TestApplyEdgePolicyParsesAIContent(t *testing.T) {
	// 载荷里没有后端与白名单，因此不需要装配后端构造器（buildRemote）。
	h := &Handler{}
	payload := `{
	  "schema_version": 1,
	  "policy_id": "core-rules",
	  "version": 3,
	  "backends": [],
	  "whitelist": {"source_cidrs": []},
	  "inject_enabled": true,
	  "content_manifest": {
	    "version": 2,
	    "selector": "session",
	    "variants": 8,
	    "entries": [{"resource": "/api/users", "profile_id": "site-a",
	      "bodies": [{"variant_id": 1, "content_id": "c-1", "checksum": "` + checksumOf("<b>x</b>") + `",
	                  "body": "<b>x</b>", "marker": "</body>"}]}]
	  }
	}`
	if err := h.applyEdgePolicy(context.Background(), []byte(payload)); err != nil {
		t.Fatalf("应用载荷失败：%v", err)
	}
	st := h.remotePolicy()
	if !st.injectEnabled {
		t.Error("inject_enabled=true 未被解析")
	}
	if st.content == nil || st.content.version != 2 || st.content.variants != 8 {
		t.Fatalf("content_manifest 未被索引：%+v", st.content)
	}
	if _, ok := st.content.entries["/api/users"][1]; !ok {
		t.Error("清单条目未被索引到（资源/变体）")
	}
}

func TestApplyEdgePolicyWithoutContentKeepsDefaults(t *testing.T) {
	// 载荷里没有后端与白名单，因此不需要装配后端构造器（buildRemote）。
	h := &Handler{}
	payload := `{"schema_version": 1, "policy_id": "core-rules", "version": 4,
	  "backends": [], "whitelist": {"source_cidrs": []}}`
	if err := h.applyEdgePolicy(context.Background(), []byte(payload)); err != nil {
		t.Fatalf("应用载荷失败：%v", err)
	}
	st := h.remotePolicy()
	if st.injectEnabled || st.content != nil {
		t.Error("载荷没谈内容时：开关必须为 false、清单必须为 nil（默认全关）")
	}
}

// 守住载荷字段名的漂移：字段名来自 docs/spec/policy-payload.md，改一处必须改另一处。
func TestContentManifestWireKeys(t *testing.T) {
	m := testManifest(2)
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	for _, key := range []string{"version", "selector", "variants", "entries"} {
		if _, ok := got[key]; !ok {
			t.Errorf("content_manifest 缺契约键 %q", key)
		}
	}
	entry := got["entries"].([]any)[0].(map[string]any)
	for _, key := range []string{"resource", "profile_id", "bodies"} {
		if _, ok := entry[key]; !ok {
			t.Errorf("entries[] 缺契约键 %q", key)
		}
	}
	body := entry["bodies"].([]any)[0].(map[string]any)
	for _, key := range []string{"variant_id", "content_id", "checksum", "body", "marker"} {
		if _, ok := body[key]; !ok {
			t.Errorf("bodies[] 缺契约键 %q", key)
		}
	}
}
