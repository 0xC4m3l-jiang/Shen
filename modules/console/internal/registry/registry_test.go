package registry

import (
	"errors"
	"path/filepath"
	"testing"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "services.json")
	s, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func shop() Input {
	return Input{Name: "商城主站", Upstream: "http://10.0.1.20:8080", Owner: "电商组",
		Hosts: []string{"Shop.Example.com:443", "shop.example.com", "*.cdn.example.com"}, Enabled: true}
}

func TestCreateNormalizesAndPersists(t *testing.T) {
	s, path := open(t)
	svc, err := s.Create(shop(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(svc.Hosts) != 2 || svc.Hosts[0] != "*.cdn.example.com" || svc.Hosts[1] != "shop.example.com" {
		t.Fatalf("域名应归一化、去重并排序：%v", svc.Hosts)
	}
	if svc.Version != 1 || svc.UpdatedBy != "admin" {
		t.Fatalf("初始版本/操作者不对：%+v", svc)
	}
	reopened, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reopened.Get(svc.ID); !ok || got.Name != "商城主站" {
		t.Fatalf("重启后应能读回：%+v %v", got, ok)
	}
}

func TestValidation(t *testing.T) {
	s, _ := open(t)
	bad := []Input{
		{Name: "", Upstream: "http://a", Hosts: []string{"a.example"}},
		{Name: "x", Upstream: "ftp://a", Hosts: []string{"a.example"}},
		{Name: "x", Upstream: "http://user:pw@a", Hosts: []string{"a.example"}},
		{Name: "x", Upstream: "http://a/?q=1", Hosts: []string{"a.example"}},
		{Name: "x", Upstream: "http://a", Hosts: nil},
		{Name: "x", Upstream: "http://a", Hosts: []string{"*"}},
		{Name: "x", Upstream: "http://a", Hosts: []string{"a.*.example"}},
		{Name: "x", Upstream: "http://a", Hosts: []string{"bad_host!"}},
	}
	for i, in := range bad {
		_, err := s.Create(in, "admin")
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("用例 %d 应返回字段校验错误，实际 %v", i, err)
		}
	}
}

func TestUniquenessAndOptimisticConcurrency(t *testing.T) {
	s, _ := open(t)
	svc, err := s.Create(shop(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	dupHost := Input{Name: "另一个", Upstream: "http://b", Hosts: []string{"SHOP.example.com"}, Enabled: true}
	if _, err := s.Create(dupHost, "admin"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("重复域名应被拒绝：%v", err)
	}
	dupName := Input{Name: "商城主站", Upstream: "http://b", Hosts: []string{"b.example.com"}}
	if _, err := s.Create(dupName, "admin"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("重复名称应被拒绝：%v", err)
	}

	edit := shop()
	edit.Owner = "平台组"
	updated, err := s.Update(svc.ID, 1, edit, "ops")
	if err != nil || updated.Version != 2 || updated.Owner != "平台组" {
		t.Fatalf("按版本更新失败：%+v %v", updated, err)
	}
	if _, err := s.Update(svc.ID, 1, edit, "ops"); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧版本更新应冲突：%v", err)
	}
	if err := s.Delete(svc.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧版本删除应冲突：%v", err)
	}
	if err := s.Delete(svc.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get(svc.ID); ok {
		t.Fatal("删除后不应再存在")
	}
}

func TestMatchPrefersExactThenLongestWildcard(t *testing.T) {
	s, _ := open(t)
	wide, _ := s.Create(Input{Name: "泛域名", Upstream: "http://a", Hosts: []string{"*.example.com"}, Enabled: true}, "admin")
	narrow, _ := s.Create(Input{Name: "CDN", Upstream: "http://b", Hosts: []string{"*.cdn.example.com"}, Enabled: true}, "admin")
	exact, _ := s.Create(Input{Name: "精确", Upstream: "http://c", Hosts: []string{"img.cdn.example.com"}, Enabled: true}, "admin")
	off, _ := s.Create(Input{Name: "停用", Upstream: "http://d", Hosts: []string{"old.example.org"}, Enabled: false}, "admin")

	cases := map[string]string{
		"img.cdn.example.com":  exact.ID,
		"a.cdn.example.com":    narrow.ID,
		"www.example.com:8443": wide.ID,
		"example.com":          "",
		"old.example.org":      "",
		"":                     "",
	}
	for host, want := range cases {
		got, _ := s.Match(host)
		if got != want {
			t.Errorf("Match(%q) = %q，期望 %q", host, got, want)
		}
	}
	_ = off
}
