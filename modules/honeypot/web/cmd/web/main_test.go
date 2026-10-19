package main

import (
	"net/netip"
	"testing"
)

// 本文件守的是 `K-30` 那一类缺陷：**开关写进了文档、却没接线**。
//
// `envPrefixes` 是 `SHEN_WEB_TRUSTED_PROXIES` 的唯一读入点 —— 它一旦写错，
// 要么限流能被人伪造 `X-Forwarded-For` 绕过，要么所有来源并成一个配额桶（两种都静默生效）。
// 所以这里的断言只关心一件事：**写错必须报错，而不是悄悄回落成空列表**。

func TestEnvPrefixes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []netip.Prefix
		err  bool
	}{
		{name: "未设 = 空列表（只信直接对端）", raw: "", want: nil},
		{name: "单个网段", raw: "127.0.0.1/32", want: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}},
		{
			name: "多个网段 + 空格 + 空段",
			raw:  " 127.0.0.1/32 , ,::1/128 ",
			want: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("::1/128")},
		},
		{
			name: "带主机位的网段被归一化（10.1.2.3/8 ⇒ 10.0.0.0/8）",
			raw:  "10.1.2.3/8",
			want: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		},
		{name: "不是 CIDR ⇒ 报错", raw: "127.0.0.1", err: true},
		{name: "前缀长度越界 ⇒ 报错", raw: "127.0.0.1/64", err: true},
		{name: "一段坏则整条坏（不部分生效）", raw: "127.0.0.1/32,oops", err: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SHEN_WEB_TRUSTED_PROXIES", c.raw)
			got, err := envPrefixes("SHEN_WEB_TRUSTED_PROXIES")
			if c.err {
				if err == nil {
					t.Fatalf("期望报错，实际通过：%v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("不期望报错：%v", err)
			}
			if len(got) != len(c.want) {
				t.Fatalf("网段数 = %d，期望 %d（got=%v）", len(got), len(c.want), got)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("第 %d 段 = %v，期望 %v", i, got[i], c.want[i])
				}
			}
		})
	}
}
