package connector

// SHEN-M4-03：删除服务时**级联吊销**它的接入凭证。
//
// 为什么要按服务名批量吊销：删除服务的人只知道服务名，不该被迫先查出凭证 id 列表；
// 「一条凭证 = 一个服务」的模型让这件事成立。语义与单条吊销一致 —— 留痕不删除。

import (
	"path/filepath"
	"testing"
	"time"
)

func newKeyStore(t *testing.T) *KeyStore {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) }
	s, err := OpenKeys(filepath.Join(t.TempDir(), "keys.json"), now)
	if err != nil {
		t.Fatalf("打开凭证表失败：%v", err)
	}
	return s
}

func TestRevokeByServiceRevokesOnlyTargetService(t *testing.T) {
	s := newKeyStore(t)
	// 一个服务一条凭证（`Issue` 按名称唯一）；轮换走 `Reset` 而不是再签发一条。
	for _, in := range []IssueInput{
		{Name: "SITE-A", Hosts: []string{"a.example.com"}},
		{Name: "site-b", Hosts: []string{"b.example.com"}},
	} {
		if _, _, err := s.Issue(in, "admin"); err != nil {
			t.Fatalf("签发失败：%v", err)
		}
	}

	n, err := s.RevokeByService("site-a")
	if err != nil {
		t.Fatalf("级联吊销失败：%v", err)
	}
	if n != 1 {
		t.Fatalf("应吊销 1 条（名称大小写不敏感），实际 %d", n)
	}

	revoked := map[string]bool{}
	for _, v := range s.List() {
		revoked[v.Name+"|"+v.ID] = v.Revoked
	}
	for _, v := range s.List() {
		switch v.Name {
		case "site-a", "SITE-A":
			if !v.Revoked {
				t.Fatalf("site-a 的凭证应已吊销：%+v", v)
			}
		case "site-b":
			if v.Revoked {
				t.Fatalf("别的服务的凭证不该被牵连：%+v", v)
			}
		}
	}
}

func TestRevokeByServiceIsIdempotentAndCountsOnlyLive(t *testing.T) {
	s := newKeyStore(t)
	if _, _, err := s.Issue(IssueInput{Name: "site-a", Hosts: []string{"a.example.com"}}, "admin"); err != nil {
		t.Fatalf("签发失败：%v", err)
	}
	if n, err := s.RevokeByService("site-a"); err != nil || n != 1 {
		t.Fatalf("首次应吊销 1 条，实际 n=%d err=%v", n, err)
	}
	// 再吊销一次：已经吊销的不再计数（否则审计里会出现"又吊销了 N 条"的假象）。
	if n, err := s.RevokeByService("site-a"); err != nil || n != 0 {
		t.Fatalf("重复吊销应计 0 条，实际 n=%d err=%v", n, err)
	}
}

func TestRevokeByServiceIgnoresBlankAndUnknownNames(t *testing.T) {
	s := newKeyStore(t)
	if _, _, err := s.Issue(IssueInput{Name: "site-a", Hosts: []string{"a.example.com"}}, "admin"); err != nil {
		t.Fatalf("签发失败：%v", err)
	}
	for _, name := range []string{"", "   ", "no-such-service"} {
		if n, err := s.RevokeByService(name); err != nil || n != 0 {
			t.Fatalf("名称为 %q 时应吊销 0 条，实际 n=%d err=%v", name, n, err)
		}
	}
	// 空名/未知名不得顺手把 site-a 也吊销了。
	for _, v := range s.List() {
		if v.Revoked {
			t.Fatalf("不该误伤其他服务：%+v", v)
		}
	}
}

func TestRevokeByServicePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")
	now := func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) }
	s, err := OpenKeys(path, now)
	if err != nil {
		t.Fatalf("打开凭证表失败：%v", err)
	}
	if _, _, err := s.Issue(IssueInput{Name: "site-a", Hosts: []string{"a.example.com"}}, "admin"); err != nil {
		t.Fatalf("签发失败：%v", err)
	}
	if _, err := s.RevokeByService("site-a"); err != nil {
		t.Fatalf("级联吊销失败：%v", err)
	}

	// 重新打开：吊销状态必须落盘（否则重启后凭证又活了）。
	reopened, err := OpenKeys(path, now)
	if err != nil {
		t.Fatalf("重开失败：%v", err)
	}
	for _, v := range reopened.List() {
		if !v.Revoked {
			t.Fatalf("吊销状态未落盘：%+v", v)
		}
	}
}
