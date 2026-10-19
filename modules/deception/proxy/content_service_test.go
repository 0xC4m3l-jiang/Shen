package proxy

// SHEN-M4-01：内容索引必须有**服务维度**，否则两个服务声明同名资源会互相覆盖。
//
// 这组用例同时锁住向后兼容：没有 `services[]` 的旧载荷，行为必须与改造前逐字节一致
// （所有请求按「不分服务」查表，先到先得）。

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// contentIdx 取当前生效的内容索引（测试用；生产路径走 `contentInjectionReady`）。
func contentIdx(t *testing.T, h *Handler) *contentIndex {
	t.Helper()
	st := h.remotePolicy()
	if st == nil || st.content == nil {
		t.Fatal("清单索引缺失：策略快照里没有 init 过的 content index")
	}
	return st.content
}

// svcManifest 造一份带服务维度的清单：两个服务各自声明同一个资源 /api/users。
func svcManifest(t *testing.T) *contentManifest {
	t.Helper()
	bodyA := `<html><body>site-a users</body></html>`
	bodyB := `<html><body>site-b users</body></html>`
	return &contentManifest{
		Version:  1,
		Selector: selectorSession,
		Variants: 1,
		Services: []contentService{
			{ID: "site-a", Hosts: []string{"a.example.com", "A.EXAMPLE.COM:8443"}},
			{ID: "site-b", Hosts: []string{"*.b.example.com"}},
		},
		Entries: []contentEntry{
			{Service: "site-a", Resource: "/api/users", ProfileID: "site-a", Bodies: []contentBody{
				{VariantID: 0, ContentID: "c-a", Checksum: checksumOf(bodyA), Body: bodyA, Marker: testMarker},
			}},
			{Service: "site-b", Resource: "/api/users", ProfileID: "site-b", Bodies: []contentBody{
				{VariantID: 0, ContentID: "c-b", Checksum: checksumOf(bodyB), Body: bodyB, Marker: testMarker},
			}},
		},
	}
}

// reqFor 造一个带 Host 的请求（`httptest.NewRequest` 默认 Host 是 example.com）。
func reqFor(host, path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Host = host
	return r
}

// TestContentIndexSeparatesServicesByName 断言：同名资源在两个服务下**各有各的正文**。
func TestContentIndexSeparatesServicesByName(t *testing.T) {
	h := contentHandler(t, svcManifest(t), true)

	gotA, okA := h.contentFor(reqFor("a.example.com", "/api/users"), contentIdx(t, h))
	if !okA {
		t.Fatal("site-a 应命中自己的内容")
	}
	gotB, okB := h.contentFor(reqFor("x.b.example.com", "/api/users"), contentIdx(t, h))
	if !okB {
		t.Fatal("site-b 应命中自己的内容（通配 Host）")
	}
	if gotA.ContentID == gotB.ContentID {
		t.Fatalf("两个服务命中了同一条内容（串号）：%q", gotA.ContentID)
	}
	if gotA.ContentID != "c-a" || gotB.ContentID != "c-b" {
		t.Fatalf("服务与内容不对应：site-a=%q site-b=%q", gotA.ContentID, gotB.ContentID)
	}
}

// TestContentIndexHostNormalization 断言：大小写、端口、尾点不影响归属。
func TestContentIndexHostNormalization(t *testing.T) {
	h := contentHandler(t, svcManifest(t), true)
	for _, host := range []string{"a.example.com", "A.EXAMPLE.COM", "a.example.com:8443", "a.example.com."} {
		body, ok := h.contentFor(reqFor(host, "/api/users"), contentIdx(t, h))
		if !ok || body.ContentID != "c-a" {
			t.Fatalf("Host %q 应归到 site-a，实际 ok=%v id=%q", host, ok, body.ContentID)
		}
	}
}

// TestContentIndexUnknownHostFallsBackToUnscoped 断言：Host 没命中任何服务时，
// 只看得见「不分服务」的条目 —— 不能顺手借到别的服务的内容。
func TestContentIndexUnknownHostFallsBackToUnscoped(t *testing.T) {
	m := svcManifest(t)
	scoped := `<html><body>global</body></html>`
	m.Entries = append(m.Entries, contentEntry{
		Resource: "/index.html", ProfileID: "generic",
		Bodies: []contentBody{{VariantID: 0, ContentID: "c-global", Checksum: checksumOf(scoped), Body: scoped, Marker: testMarker}},
	})
	h := contentHandler(t, m, true)

	if body, ok := h.contentFor(reqFor("unknown.example.org", "/index.html"), contentIdx(t, h)); !ok || body.ContentID != "c-global" {
		t.Fatalf("未命中服务的请求应看得见不分服务的条目，实际 ok=%v", ok)
	}
	if _, ok := h.contentFor(reqFor("unknown.example.org", "/api/users"), contentIdx(t, h)); ok {
		t.Fatal("未命中服务的请求不得借到某个服务专属的内容（串号）")
	}
}

// TestContentIndexScopedWinsOverUnscoped 断言：同一资源同时有专属与通用条目时，专属优先。
func TestContentIndexScopedWinsOverUnscoped(t *testing.T) {
	m := svcManifest(t)
	generic := `<html><body>generic users</body></html>`
	m.Entries = append(m.Entries, contentEntry{
		Resource: "/api/users", ProfileID: "generic",
		Bodies: []contentBody{{VariantID: 0, ContentID: "c-generic", Checksum: checksumOf(generic), Body: generic, Marker: testMarker}},
	})
	h := contentHandler(t, m, true)

	if body, _ := h.contentFor(reqFor("a.example.com", "/api/users"), contentIdx(t, h)); body.ContentID != "c-a" {
		t.Fatalf("专属内容应优先于通用内容，实际 %q", body.ContentID)
	}
	if body, _ := h.contentFor(reqFor("unknown.example.org", "/api/users"), contentIdx(t, h)); body.ContentID != "c-generic" {
		t.Fatalf("未命中服务时应退到通用内容，实际 %q", body.ContentID)
	}
}

// TestContentIndexWithoutServicesKeepsLegacyBehavior 断言向后兼容：
// 载荷里没有 `services[]` 时，行为与改造前一致（任何 Host 都命中同一条）。
func TestContentIndexWithoutServicesKeepsLegacyBehavior(t *testing.T) {
	// 旧载荷的形态：既没有 `services[]`，条目上也没有 `service` 字段。
	m := svcManifest(t)
	m.Services = nil
	m.Entries = m.Entries[:1]
	m.Entries[0].Service = ""
	h := contentHandler(t, m, true)

	for _, host := range []string{"a.example.com", "whatever.example.org"} {
		if body, ok := h.contentFor(reqFor(host, "/api/users"), contentIdx(t, h)); !ok || body.ContentID != "c-a" {
			t.Fatalf("无 services 表时应与改造前一致（任何 Host 命中同一条），Host=%q ok=%v id=%q", host, ok, body.ContentID)
		}
	}
}
