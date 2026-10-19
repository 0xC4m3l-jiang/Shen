package proxy

// SHEN-M4-02：幻境后端按**服务**分流。
//
// 判定仍由核心做（要不要改道），这里只解析「改到哪个池」——
// 池的选择属于部署拓扑（一个服务一个幻境后端），不属于判定逻辑（AR-2）。

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBuildServiceTables 断言接入单元表被正确展开（并丢弃非法项）。
func TestBuildServiceTables(t *testing.T) {
	hosts, backends := buildServiceTables([]policyService{
		{ID: "site-a", Hosts: []string{"A.Example.COM:8443", "a.example.com."}, MirageBackend: "hp-a"},
		{ID: "site-b", Hosts: []string{"*.b.example.com"}}, // 不声明后端 ⇒ 用核心给的默认池
		{ID: "  ", Hosts: []string{"ignored.example.com"}},  // 空 id 丢弃：归属不可判定
		{ID: "bad", Hosts: []string{"*", "a*b.example.com"}}, // 非法通配丢弃（只支持最左标签）
	})

	if got := hosts["a.example.com"]; got != "site-a" {
		t.Fatalf("Host 归一化失败：a.example.com → %q", got)
	}
	if got := hosts["*.b.example.com"]; got != "site-b" {
		t.Fatalf("通配模式应原样保留：%q", got)
	}
	if _, ok := hosts["ignored.example.com"]; ok {
		t.Fatal("空 id 的服务不得建立归属")
	}
	if _, ok := hosts["a*b.example.com"]; ok {
		t.Fatal("非法通配（不在最左标签）必须丢弃")
	}
	if backends["site-a"] != "hp-a" {
		t.Fatalf("服务后端未登记：%v", backends)
	}
	if _, ok := backends["site-b"]; ok {
		t.Fatal("未声明后端的服务不该出现在表里（应走默认池）")
	}
}

// TestBackendForService 断言选池规则：服务声明了就用它，否则沿用核心给的名字。
func TestBackendForService(t *testing.T) {
	st := &remoteState{
		hostServices:    map[string]string{"a.example.com": "site-a", "*.b.example.com": "site-b"},
		serviceBackends: map[string]string{"site-a": "hp-a"},
	}
	h := &Handler{}

	cases := []struct {
		host, fallback, want, why string
	}{
		{"a.example.com", "mirage", "hp-a", "服务声明了后端 ⇒ 用它的"},
		{"x.b.example.com", "mirage", "mirage", "服务没声明后端 ⇒ 沿用核心给的默认池"},
		{"unknown.example.org", "mirage", "mirage", "Host 未命中任何服务 ⇒ 默认池"},
		{"a.example.com", "", "hp-a", "服务声明了自己的池 ⇒ 即使核心没给名字也用它（否则只能回落业务）"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = c.host
		if got := h.backendForService(st, r, c.fallback); got != c.want {
			t.Errorf("%s（Host=%q）：得到 %q，期望 %q", c.why, c.host, got, c.want)
		}
	}
}

// TestBackendForServiceWithoutTablesKeepsLegacy 断言旧载荷（无服务表）行为不变。
func TestBackendForServiceWithoutTablesKeepsLegacy(t *testing.T) {
	h := &Handler{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "a.example.com"
	if got := h.backendForService(&remoteState{}, r, "mirage"); got != "mirage" {
		t.Fatalf("无服务表时应原样返回核心给的后端，实际 %q", got)
	}
	if got := h.backendForService(nil, r, "mirage"); got != "mirage" {
		t.Fatalf("快照为 nil 时应原样返回，实际 %q", got)
	}
}

// TestServiceOfHostWildcardExcludesApex 断言通配不含根域（与诱饵归属同口径）。
func TestServiceOfHostWildcardExcludesApex(t *testing.T) {
	hosts := map[string]string{"*.b.example.com": "site-b"}
	if got := serviceOfHost(hosts, "b.example.com"); got != "" {
		t.Fatalf("通配不得命中根域，实际 %q", got)
	}
	if got := serviceOfHost(hosts, "x.b.example.com"); got != "site-b" {
		t.Fatalf("通配应命中子域，实际 %q", got)
	}
}
