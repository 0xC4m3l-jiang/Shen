package connector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTestKeys(t *testing.T) (*KeyStore, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenKeys(filepath.Join(dir, "creds.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestIssueStoresHashNeverPlaintext(t *testing.T) {
	s, dir := openTestKeys(t)
	view, key, err := s.Issue(IssueInput{Name: "商城", Hosts: []string{"shop.example.com"}, Owner: "电商组"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// 明文形态：shc- 前缀、64 hex；提示只露末 4 位。
	if !strings.HasPrefix(key, "shc-") || len(key) != len("shc-")+64 {
		t.Fatalf("key 形态不对：%q", key)
	}
	if view.KeyHint != "shc-****"+key[len(key)-4:] {
		t.Fatalf("提示应为末 4 位：%+v", view)
	}
	// 落盘文件与视图序列化都不得出现明文。
	raw, _ := os.ReadFile(filepath.Join(dir, "creds.json"))
	if strings.Contains(string(raw), key) {
		t.Fatal("落盘文件出现明文 key")
	}
	vj, _ := json.Marshal(view)
	if strings.Contains(string(vj), "key_hash") {
		t.Fatalf("视图序列化含哈希字段：%s", vj)
	}
	// 网关校验表带哈希与绑定信息。
	gk := s.GatewayKeys()
	if len(gk) != 1 || gk[0].Name != "商城" || len(gk[0].KeyHash) != 64 {
		t.Fatalf("网关校验表不对：%+v", gk)
	}
}

func TestResetAndRevokeLifecycle(t *testing.T) {
	s, _ := openTestKeys(t)
	view, key1, _ := s.Issue(IssueInput{Name: "A", Hosts: []string{"a.example.com"}}, "admin")
	r2, key2, err := s.Reset(view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if key1 == key2 || r2.Version <= view.Version {
		t.Fatalf("重置应换 key 并递增版本：%q %q %d", key1, key2, r2.Version)
	}
	// 重置已吊销的凭证应复活。
	if _, err := s.Revoke(view.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(view.ID); !got.Revoked {
		t.Fatal("吊销后应标记 revoked")
	}
	r3, _, err := s.Reset(view.ID)
	if err != nil || r3.Revoked {
		t.Fatalf("重置应复活凭证：%v %+v", err, r3)
	}
	if _, err := s.Revoke("missing"); err == nil {
		t.Fatal("吊销不存在的凭证应报错")
	}
}

func TestIssueRejectsDuplicates(t *testing.T) {
	s, _ := openTestKeys(t)
	if _, _, err := s.Issue(IssueInput{Name: "A", Hosts: []string{"a.example.com"}}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Issue(IssueInput{Name: "a", Hosts: []string{"b.example.com"}}, "admin"); err == nil {
		t.Fatal("同名（不区分大小写）应拒绝")
	}
	if _, _, err := s.Issue(IssueInput{Name: "B", Hosts: []string{"a.example.com"}}, "admin"); err == nil {
		t.Fatal("域名被其他凭证占用应拒绝")
	}
}

// 会话观测表：上下线过渡、惰性过期与 KPI。
func TestSessionTransitionsAndStaleExpiry(t *testing.T) {
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	s := NewSessionStore(func() time.Time { return now })

	if tr := s.Upsert(SessionState{SessionID: "s1", CredentialID: "c1", Name: "A", Online: true, RttMs: 20}, ""); tr != EventOnline {
		t.Fatalf("首次上报应为 online 过渡：%q", tr)
	}
	if tr := s.Upsert(SessionState{SessionID: "s1", CredentialID: "c1", Name: "A", Online: true, RttMs: 25}, ""); tr != "" {
		t.Fatalf("心跳刷新不应产生过渡：%q", tr)
	}
	if tr := s.Upsert(SessionState{SessionID: "s1", CredentialID: "c1", Name: "A", Online: false}, "计划内下线"); tr != EventOffline {
		t.Fatalf("下线应产生 offline 过渡：%q", tr)
	}
	if tr := s.Upsert(SessionState{SessionID: "s1", CredentialID: "c1", Name: "A", Online: true}, ""); tr != EventReconnect {
		t.Fatalf("恢复应产生 reconnect 过渡：%q", tr)
	}
	// 惰性过期：时钟前进超过 TTL，List 把在线会话判离线并记事件。
	now = now.Add(2 * time.Minute)
	list := s.List(time.Minute)
	if len(list) != 1 || list[0].Online {
		t.Fatalf("心跳超时应判离线：%+v", list)
	}
	if s.TodayDisconnects() != 2 { // 一次下线 + 一次过期
		t.Fatalf("今日断线计数不对：%d", s.TodayDisconnects())
	}
	events := s.Events(10)
	if len(events) < 4 || events[0].Kind != EventOffline {
		t.Fatalf("事件环应最新在前且含过期事件：%+v", events)
	}
	// 网关联系时间。
	if _, ok := s.GatewaySeen(); ok {
		t.Fatal("初始无网关联系")
	}
	s.TouchGateway()
	if _, ok := s.GatewaySeen(); !ok {
		t.Fatal("TouchGateway 后应有联系记录")
	}
}
