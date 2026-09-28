package geoip

import (
	"strings"
	"sync"
	"testing"
)

func mustDefault(t *testing.T) *DB {
	t.Helper()
	db, err := Default()
	if err != nil {
		t.Fatalf("内嵌数据文件无法装载：%v", err)
	}
	return db
}

// 期望值取自数据文件本身（用独立脚本按官方算法交叉核对过）：
// 这里钉住的是「读取器与官方算法一致」，而不是「归属地绝对准确」。
func TestLookupKnownAddresses(t *testing.T) {
	db := mustDefault(t)
	cases := []struct {
		ip, country, province, city string
	}{
		{"114.114.114.114", "中国", "江苏省", "南京市"},
		{"202.96.128.86", "中国", "广东省", "广州市"},
		{"1.1.1.1", "Australia", "Queensland", "Brisbane"},
	}
	for _, tc := range cases {
		got := db.Lookup(tc.ip)
		if got.Scope != ScopePublic || got.Country != tc.country || got.Province != tc.province || got.City != tc.city {
			t.Errorf("Lookup(%s) = %+v，期望 %s/%s/%s", tc.ip, got, tc.country, tc.province, tc.city)
		}
		if !strings.Contains(got.Label, tc.province) {
			t.Errorf("Label 应包含省份：%q", got.Label)
		}
	}
	if isp := db.Lookup("202.96.128.86").ISP; isp != "电信" {
		t.Errorf("运营商字段解析错误：%q", isp)
	}
	if city := db.Lookup("8.8.8.8").City; city != "" {
		t.Errorf("库中为 0 的字段应为空串，实际 %q", city)
	}
}

func TestSpecialScopes(t *testing.T) {
	db := mustDefault(t)
	cases := map[string]Scope{
		"127.0.0.1":       ScopeLoopback,
		"::1":             ScopeLoopback,
		"10.1.2.3":        ScopePrivate,
		"192.168.10.8":    ScopePrivate,
		"100.64.1.1":      ScopePrivate,
		"fd00::1":         ScopePrivate,
		"::ffff:10.0.0.1": ScopePrivate,
		"192.0.2.1":       ScopeReserved,
		"240.0.0.1":       ScopeReserved,
		"224.0.0.1":       ScopeReserved,
		"not-an-ip":       ScopeUnknown,
		"":                ScopeUnknown,
		"2400:3200::1":    ScopePublic,
		"203.0.113.9":     ScopeReserved,
		"169.254.169.254": ScopePrivate,
		"0.0.0.0":         ScopeReserved,
		"198.18.0.1":      ScopeReserved,
		"255.255.255.255": ScopeReserved,
	}
	for ip, want := range cases {
		if got := db.Lookup(ip); got.Scope != want {
			t.Errorf("Lookup(%q).Scope = %s，期望 %s（%+v）", ip, got.Scope, want, got)
		}
	}
}

func TestRejectsCorruptData(t *testing.T) {
	if _, err := Open([]byte("short")); err == nil {
		t.Fatal("过短数据必须报错")
	}
	bad := make([]byte, len(embedded))
	copy(bad, embedded)
	bad[12], bad[13], bad[14], bad[15] = 0xff, 0xff, 0xff, 0x7f // 段索引结束指针越界
	if _, err := Open(bad); err == nil {
		t.Fatal("越界指针必须报错")
	}
}

func TestConcurrentLookup(t *testing.T) {
	db := mustDefault(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if db.Lookup("114.114.114.114").City != "南京市" {
					t.Error("并发查询结果不一致")
					return
				}
			}
		}()
	}
	wg.Wait()
	if db.BuiltAt() == 0 {
		t.Error("应能读出数据文件生成时间")
	}
}
