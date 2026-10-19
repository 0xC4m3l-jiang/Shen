package proxy

// SHEN-M3-02：改道侧**写操作绝不触达真实业务**。
//
// 这里测的是决策函数本身（纯函数），因为「改道不可用」的三条路径（后端不存在 /
// 探针不健康 / 转发失败）都必须走同一个判断 —— 把判断收敛成一个函数，才不会
// 三条路径里漏掉一条。端到端行为另有集成用例覆盖。

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestFallbackOnMirageFailure 断言：写方法一律拒绝、安全方法才允许回落业务。
func TestFallbackOnMirageFailure(t *testing.T) {
	cases := []struct {
		method string
		want   string
		why    string
	}{
		{http.MethodPost, fallbackRefuse, "POST 可能落库（表单/GraphQL/回调），不能回流业务"},
		{http.MethodPut, fallbackRefuse, "PUT 会改资源"},
		{http.MethodPatch, fallbackRefuse, "PATCH 会改资源"},
		{http.MethodDelete, fallbackRefuse, "DELETE 会删资源"},
		{http.MethodGet, fallbackOrigin, "GET 无副作用，按 NI-1 回落业务"},
		{http.MethodHead, fallbackOrigin, "HEAD 无副作用"},
		{http.MethodOptions, fallbackOrigin, "OPTIONS 无副作用"},
		{"", fallbackOrigin, "空方法按未知处理：宁可回落，也不要凭空 503"},
	}
	for _, c := range cases {
		if got := fallbackOnMirageFailure(c.method); got != c.want {
			t.Errorf("%s｜%s：得到 %q，期望 %q", c.method, c.why, got, c.want)
		}
	}
}

// TestRefuseReroutedWrite_Returns503AndNoStore 断言：拒绝时是 503（不是 403，也不是 500），
// 带 no-store，且不带正文（不泄露任何信息）。
func TestRefuseReroutedWrite_Returns503AndNoStore(t *testing.T) {
	h := &Handler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(nil))

	h.refuseReroutedWrite(rec, req, "hp", "test")

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("状态码应为 503，实际 %d", res.StatusCode)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control 应为 no-store，实际 %q", got)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("拒绝响应不应带正文（避免泄露任何信息），实际 %q", rec.Body.String())
	}
}
