package gateway

// SHEN-M4-03（连接器侧）：把「服务名」提升为**接入单元的键**。
//
// 凭证表是「现在有哪些服务、占着哪些 Host」的事实源。本组用例锁住三件事：
//   1. 同一服务多条凭证（轮换期并存）合并成一条，Hosts 取并集；
//   2. 只有**全部**凭证都被吊销，服务才算吊销；
//   3. 输出确定（排序），否则控制台/策略每次拉到的东西顺序都在变。

import (
	"reflect"
	"testing"
)

func TestServicesMergesKeysByServiceName(t *testing.T) {
	tbl := &KeyTable{}
	tbl.Replace([]ConsoleKey{
		{ID: "k-1", Name: "site-a", Hosts: []string{"A.example.com", "a.example.com:8443"}},
		{ID: "k-2", Name: "SITE-A", Hosts: []string{"api.example.com"}}, // 同服务、轮换期并存
		{ID: "k-3", Name: "site-b", Hosts: []string{"*.b.example.com"}},
	})

	got := tbl.Services()
	if len(got) != 2 {
		t.Fatalf("服务数 = %d，期望 2（同一服务应合并）", len(got))
	}
	// 按名称排序：site-a / site-b
	a := got[0]
	if a.Name != "site-a" {
		t.Fatalf("排序不正确：%+v", got)
	}
	wantHosts := []string{"a.example.com", "api.example.com"}
	if !reflect.DeepEqual(a.Hosts, wantHosts) {
		t.Fatalf("Hosts 应为归一化后的并集 %v，实际 %v", wantHosts, a.Hosts)
	}
	if !reflect.DeepEqual(a.KeyIDs, []string{"k-1", "k-2"}) {
		t.Fatalf("KeyIDs 不正确：%v", a.KeyIDs)
	}
	if a.Revoked {
		t.Fatal("还有可用凭证时不该判定为吊销")
	}
}

func TestServicesRevokedOnlyWhenAllKeysRevoked(t *testing.T) {
	tbl := &KeyTable{}
	tbl.Replace([]ConsoleKey{
		{ID: "k-1", Name: "site-a", Hosts: []string{"a.example.com"}, Revoked: true},
		{ID: "k-2", Name: "site-a", Hosts: []string{"a2.example.com"}, Revoked: false},
		{ID: "k-3", Name: "site-b", Hosts: []string{"b.example.com"}, Revoked: true},
	})
	byName := map[string]Service{}
	for _, s := range tbl.Services() {
		byName[s.Name] = s
	}
	if byName["site-a"].Revoked {
		t.Fatal("site-a 还有一条可用凭证，不该判吊销")
	}
	if !byName["site-b"].Revoked {
		t.Fatal("site-b 的凭证全被吊销，应判吊销")
	}
}

func TestServicesIgnoresUnnamedKeys(t *testing.T) {
	tbl := &KeyTable{}
	tbl.Replace([]ConsoleKey{
		{ID: "k-1", Name: "", Hosts: []string{"x.example.com"}},
		{ID: "k-2", Name: "   ", Hosts: []string{"y.example.com"}},
		{ID: "k-3", Name: "site-a", Hosts: []string{"a.example.com"}},
	})
	got := tbl.Services()
	if len(got) != 1 || got[0].Name != "site-a" {
		t.Fatalf("无名凭证不该制造服务：%+v", got)
	}
}

func TestServicesIsDeterministic(t *testing.T) {
	tbl := &KeyTable{}
	tbl.Replace([]ConsoleKey{
		{ID: "k-b", Name: "site-b", Hosts: []string{"z.example.com", "b.example.com"}},
		{ID: "k-a", Name: "site-a", Hosts: []string{"y.example.com", "a.example.com"}},
	})
	first := tbl.Services()
	second := tbl.Services()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("两次调用结果不一致（不确定）：%v vs %v", first, second)
	}
	for _, svc := range first {
		for i := 1; i < len(svc.Hosts); i++ {
			if svc.Hosts[i-1] > svc.Hosts[i] {
				t.Fatalf("Hosts 未排序：%v", svc.Hosts)
			}
		}
	}
}

// TestServicesDisappearWhenKeysRemoved 断言「删除服务 = 凭证表里没有它 = 视图里也没有它」。
func TestServicesDisappearWhenKeysRemoved(t *testing.T) {
	tbl := &KeyTable{}
	tbl.Replace([]ConsoleKey{
		{ID: "k-1", Name: "site-a", Hosts: []string{"a.example.com"}},
		{ID: "k-2", Name: "site-b", Hosts: []string{"b.example.com"}},
	})
	if len(tbl.Services()) != 2 {
		t.Fatalf("应有两个服务")
	}
	tbl.Replace([]ConsoleKey{{ID: "k-1", Name: "site-a", Hosts: []string{"a.example.com"}}})
	got := tbl.Services()
	if len(got) != 1 || got[0].Name != "site-a" {
		t.Fatalf("删除服务后视图里不该残留：%+v", got)
	}
	if tbl.HostKnown("b.example.com") {
		t.Fatal("删除服务后它的 Host 也不该再被认作已接入")
	}
}
