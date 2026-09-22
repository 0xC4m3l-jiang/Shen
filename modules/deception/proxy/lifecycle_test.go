package proxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── FIX-8 回归：改写分支的 Body 关闭责任 ─────────────────────────────────────
//
// 缺陷形态：超限分支把原 Body 包进 `io.NopCloser(io.MultiReader(...))` —— 读能走通，
// 但 Close 变成空操作，底层连接永不回收（连接泄漏）。net/http 的约定是
// 「调用方必须 Close 响应体，否则连接不可复用」，所以关闭责任必须跟着新 Body 走。
//
// 本用例用一个**计数的 ReadCloser** 直接断言「原 Body 恰好被关一次」，
// 不靠「响应看着正常」推测（大响应走 origin 的端到端用例证明不了这个分支）。

// countingBody 记录 Close 次数；可选在第 failAt 次读取后返回错误。
type countingBody struct {
	r      *strings.Reader
	closes int
	reads  int
	failAt int
}

func newCountingBody(s string) *countingBody {
	return &countingBody{r: strings.NewReader(s), failAt: -1}
}

func (b *countingBody) Read(p []byte) (int, error) {
	if b.failAt >= 0 && b.reads >= b.failAt {
		return 0, errors.New("合成了读错误")
	}
	b.reads++
	return b.r.Read(p)
}

func (b *countingBody) Close() error {
	b.closes++
	return nil
}

// noopInjector 是一个「有注入器但从不改写」的替身。
//
// 为什么需要它：注入 transport 在「静态注入器为 nil 且内容注入未就绪」时会**提前返回**，
// 根本不读 body —— 那条路径不涉及关闭责任。要走到读/改写分支，必须有非 nil 的注入器。
type noopInjector struct{}

func (noopInjector) Inject(_ string, body []byte) ([]byte, bool) { return body, false }

// stubSource 是 injectionSource 的替身：默认只有一个不改写的注入器。
type stubSource struct {
	inj Injector
}

func (s stubSource) currentInjector() Injector { return s.inj }
func (stubSource) contentInjectionReady() (bool, *contentIndex) {
	return false, nil
}
func (stubSource) injectContent(_ *http.Request, _ *contentIndex, _ string, _ []byte) ([]byte, string, bool) {
	return nil, "", false
}

// stubSourceWithInjector 造一个走进改写分支的注入源。
func stubSourceWithInjector() stubSource { return stubSource{inj: noopInjector{}} }

func htmlResponseWithBody(body io.ReadCloser, contentLength int64) *http.Response {
	return &http.Response{
		Header:        http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:          body,
		ContentLength: contentLength,
	}
}

// TestOverLimitBodyClosesOriginalExactlyOnce 覆盖「响应超过注入上限」这条分支。
//
// 上半：超限时内容必须**一个字节都不丢**（已读前缀接回剩余流）。
// 下半：外层的 Close 必须把原 Body 关掉，且只关一次（重复 Close 由 once 吸收）。
func TestOverLimitBodyClosesOriginalExactlyOnce(t *testing.T) {
	// 比上限多 1KB：注入器会先读满 maxInjectBytes+1 才判定「太大」。
	payload := strings.Repeat("a", maxInjectBytes+1024)
	body := newCountingBody(payload)

	tr := &injectingTransport{src: stubSourceWithInjector(), base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		// ContentLength = -1：长度未知的大响应（分块传输），也必须走同一条保护。
		return htmlResponseWithBody(body, -1), nil
	})}

	resp, err := tr.RoundTrip(httptest.NewRequest(http.MethodGet, "http://x/", nil))
	if err != nil {
		t.Fatalf("RoundTrip 失败：%v", err)
	}
	if _, ok := resp.Body.(*prefixBody); !ok {
		// 不在这里终止：还要验证关闭责任（缺陷实现是 NopCloser → 关闭数会是 0）。
		t.Errorf("超限响应应换成「前缀 + 剩余流」的 Body，实际 %T", resp.Body)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读超限响应体失败：%v", err)
	}
	if string(got) != payload {
		t.Fatalf("超限透传不得丢字节：原 %d 字节，得 %d 字节", len(payload), len(got))
	}
	if body.closes != 0 {
		t.Fatalf("读的过程中不应关闭原 Body（还没读完），实际关了 %d 次", body.closes)
	}

	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Close 失败：%v", err)
	}
	if body.closes != 1 {
		t.Fatalf("原 Body 必须恰好被关一次，实际 %d 次（0 = 连接泄漏，>1 = 双重关闭）", body.closes)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("重复 Close 不应报错：%v", err)
	}
	if body.closes != 1 {
		t.Fatalf("重复 Close 必须被 once 吸收，实际关了 %d 次", body.closes)
	}
}

// TestReadErrorClosesOriginalOnce 覆盖「读 body 出错」这条分支：
// 错误要抛给 reverse_proxy（此时尚未写客户端），但原 Body 仍须被关掉。
func TestReadErrorClosesOriginalOnce(t *testing.T) {
	body := newCountingBody(strings.Repeat("a", 4096))
	body.failAt = 1 // 读一次就报错

	tr := &injectingTransport{src: stubSourceWithInjector(), base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResponseWithBody(body, -1), nil
	})}

	_, err := tr.RoundTrip(httptest.NewRequest(http.MethodGet, "http://x/", nil))
	if err == nil {
		t.Fatal("读失败必须上抛（供 forwardMirage 判断能否回落业务）")
	}
	if body.closes != 1 {
		t.Fatalf("读失败路径也必须关闭原 Body 恰好一次，实际 %d 次", body.closes)
	}
}

// TestSmallBodyClosesOriginalOnce 覆盖正常改写路径：读完即关，且换成新体。
func TestSmallBodyClosesOriginalOnce(t *testing.T) {
	body := newCountingBody("<html><body>hi</body></html>")
	tr := &injectingTransport{src: stubSourceWithInjector(), base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResponseWithBody(body, int64(len("<html><body>hi</body></html>"))), nil
	})}

	resp, err := tr.RoundTrip(httptest.NewRequest(http.MethodGet, "http://x/", nil))
	if err != nil {
		t.Fatalf("RoundTrip 失败：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if body.closes != 1 {
		t.Fatalf("原 Body 必须读完即关恰好一次，实际 %d 次", body.closes)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "<html><body>hi</body></html>" {
		t.Fatalf("无注入源时正文必须原样，得到 %q", got)
	}
}

// TestNonHTMLBodyNotClosedByUs 覆盖「不可改写」分支：非 HTML 直接透传，**不动**原 Body
// （不读、不关 —— 关闭责任仍属于下游消费者）。
func TestNonHTMLBodyNotClosedByUs(t *testing.T) {
	body := newCountingBody("{\"json\":true}")
	resp := htmlResponseWithBody(body, 13)
	resp.Header.Set("Content-Type", "application/json")

	tr := &injectingTransport{src: stubSourceWithInjector(), base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return resp, nil
	})}
	out, err := tr.RoundTrip(httptest.NewRequest(http.MethodGet, "http://x/", nil))
	if err != nil {
		t.Fatalf("RoundTrip 失败：%v", err)
	}
	// 只断言「还是原来那个 Body 对象」：一旦被替换或被包一层，关闭责任就变了。
	if _, same := out.Body.(*countingBody); !same {
		t.Fatalf("非 HTML 响应不得替换 Body，实际 %T", out.Body)
	}
	if body.closes != 0 {
		t.Fatalf("透传分支不得替下游关闭 Body，实际关了 %d 次", body.closes)
	}
}
