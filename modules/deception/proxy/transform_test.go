package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── 解耦验证：改写逻辑不再依赖整个 Handler ───────────────────────────────────
//
// 改造前 `injectingTransport` 持 `*Handler`（gRPC 客户端 + 判定缓存 + 遥测队列 + 策略状态全在里面），
// 于是「改写这段字节」的测试必须先把 Handler 拼起来。现在它的依赖面被收敛成
// `injectionSource` 三个方法（见 transform.go），因此可以用**替身**直接测。
//
// 本文件里的替身**不是 Handler** —— 它能跑通，就是解耦成立的可执行证据。

// recordingSource 是 injectionSource 的完全替身：可配置开关、内容与改写结果。
type recordingSource struct {
	inj        Injector
	ready      bool
	idx        *contentIndex
	rewrite    bool
	content    string
	contentID  string
	callCount  int
	lastCT     string
	lastBodyLn int
}

func (s *recordingSource) currentInjector(*remoteState) Injector { return s.inj }

func (s *recordingSource) contentInjectionReady(*remoteState) (bool, *contentIndex) {
	return s.ready, s.idx
}

func (s *recordingSource) injectContent(_ *http.Request, _ *contentIndex, contentType string, body []byte) ([]byte, string, bool) {
	s.callCount++
	s.lastCT = contentType
	s.lastBodyLn = len(body)
	if !s.rewrite {
		return body, "", false
	}
	return append(append([]byte(nil), body...), s.content...), s.contentID, true
}

// mustOutcome 造一个挂了结果槽的请求，返回槽与请求。
func mustOutcome(t *testing.T) (*http.Request, *injectOutcome) {
	t.Helper()
	slot := &injectOutcome{}
	r := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	return r.WithContext(withInjectOutcome(r.Context(), slot)), slot
}

func TestTransformAppliedWithStubSource(t *testing.T) {
	src := &recordingSource{ready: true, idx: &contentIndex{}, rewrite: true, content: "<!--AI-->", contentID: "c-1"}
	tr := &injectingTransport{src: src, base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResp("<html><body>hi</body></html>"), nil
	})}
	r, slot := mustOutcome(t)

	resp, err := tr.RoundTrip(r)
	if err != nil {
		t.Fatalf("RoundTrip 失败：%v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<!--AI-->") {
		t.Fatalf("替身源应能完成注入，得到 %q", body)
	}
	if got, id := slot.get(); got != InjectApplied || id != "c-1" {
		t.Fatalf("注入结果应为 applied/c-1，得到 %q/%q", got, id)
	}
	if src.lastCT == "" || src.lastBodyLn == 0 {
		t.Fatalf("替身应收到内容类型与正文：%q / %d", src.lastCT, src.lastBodyLn)
	}
}

func TestTransformNoContentWithStubSource(t *testing.T) {
	src := &recordingSource{ready: true, idx: &contentIndex{}} // 开关开但没有可用内容
	tr := &injectingTransport{src: src, base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResp("<html><body>hi</body></html>"), nil
	})}
	r, slot := mustOutcome(t)

	resp, err := tr.RoundTrip(r)
	if err != nil {
		t.Fatalf("RoundTrip 失败：%v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "<html><body>hi</body></html>" {
		t.Fatalf("无内容时必须原样透传，得到 %q", body)
	}
	if got, _ := slot.get(); got != InjectNoContent {
		t.Fatalf("应如实上报 no_content，得到 %q", got)
	}
	if src.callCount != 1 {
		t.Fatalf("开关开着就应请求一次内容（由它决定有没有），实际 %d 次", src.callCount)
	}
}

func TestTransformDisabledWithStubSource(t *testing.T) {
	src := &recordingSource{} // 本地兜底关、无静态注入器
	tr := &injectingTransport{src: src, base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResp("<html><body>hi</body></html>"), nil
	})}
	r, slot := mustOutcome(t)

	resp, err := tr.RoundTrip(r)
	if err != nil {
		t.Fatalf("RoundTrip 失败：%v", err)
	}
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("读响应失败：%v", err)
	}
	if got, _ := slot.get(); got != InjectDisabled {
		t.Fatalf("开关关闭时应上报 disabled（不是 no_content），得到 %q", got)
	}
	if src.callCount != 0 {
		t.Fatalf("开关关闭时不得去查内容，实际 %d 次", src.callCount)
	}
}

// 静态注入器与内容注入两条源**同时**存在时都要生效（顺序：静态在前，内容在后）。
func TestTransformBothSourcesApply(t *testing.T) {
	src := &recordingSource{inj: &stubInjector{marker: "<!--STATIC-->"}, ready: true, rewrite: true, content: "<!--AI-->", contentID: "c-2"}
	tr := &injectingTransport{src: src, base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResp("<html><body>hi</body></html>"), nil
	})}
	r, slot := mustOutcome(t)

	resp, err := tr.RoundTrip(r)
	if err != nil {
		t.Fatalf("RoundTrip 失败：%v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"<!--STATIC-->", "<!--AI-->"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("两条注入源都必须生效，缺 %q：%q", want, body)
		}
	}
	if got, _ := slot.get(); got != InjectApplied {
		t.Fatalf("内容注入成功应上报 applied，得到 %q", got)
	}
}
