package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── 观测的查询串字段（query）─────────────────────────────────────────────────
//
// 为什么单独立一例：`query` 是**解码后的匹配视图**（proto / 适配器 / 规则引擎三方约定），
// 漏了解码就等于「编码即绕过」照旧 —— 而那正是这一字段存在的理由。
// 口径见 docs/spec/config.md §2.4。

func TestQueryOfReachesFixedPoint(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"明文穿越", "file=../../etc/passwd", "file=../../etc/passwd"},
		{"百分号编码穿越", "file=%2e%2e%2f%2e%2e%2fetc%2fpasswd", "file=../../etc/passwd"},
		{"加号当空格", "q=union+select", "q=union select"},
		{"百分号编码空格", "q=union%20select", "q=union select"},
		{"无查询串", "", ""},
		{"只有键", "debug", "debug"},
		// 关键性质：**解到不动点**（上限 3 轮）—— 二次编码是实测的绕过手段，必须解掉。
		{"双层编码解到不动点", "file=%252e%252e%252f", "file=../"},
		{"双层编码的 SQLi", "q=union%2520select", "q=union select"},
		{"三层编码也解掉（上限内）", "file=%25252e", "file=."},
		// 上限之外停下来：每多一次都是对攻击者可控输入的线性工作，成本钉死（代价已登记）。
		{"四层编码解到上限就停", "file=%2525252e", "file=%2e"},
		// 非法转义：原样传递（从不让解码失败把载荷弄丢）。
		{"非法转义原样传递", "q=100%", "q=100%"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			target := "http://svc.example/download"
			if c.raw != "" {
				target += "?" + c.raw
			}
			r := httptest.NewRequest(http.MethodGet, target, nil)
			if got := queryOf(r); got != c.want {
				t.Fatalf("queryOf(%q) = %q，期望 %q", c.raw, got, c.want)
			}
		})
	}
}

// 观测必须带上 query，且 path 仍**不含**查询串（HTTP 语义：两者是不同的事实）。
func TestObservationCarriesQuerySeparatelyFromPath(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://svc.example/download?file=%2e%2e%2fetc%2fpasswd", nil)
	obs := observationFrom(r, false)

	if got, want := obs.GetPath(), "/download"; got != want {
		t.Fatalf("path 必须不含查询串：期望 %q，得到 %q", want, got)
	}
	if got, want := obs.GetQuery(), "file=../etc/passwd"; got != want {
		t.Fatalf("query 必须是规范化后的形态：期望 %q，得到 %q", want, got)
	}
	if got, want := obs.GetQueryRaw(), "file=%2e%2e%2fetc%2fpasswd"; got != want {
		t.Fatalf("query_raw 必须是**原样**字节（审计用）：期望 %q，得到 %q", want, got)
	}
}
