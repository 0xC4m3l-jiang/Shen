package store

import (
	"context"
	"testing"
	"time"

	"shen/common/core/internal/contract"
)

// TestEventStore_Idempotent：同 event_id 不产生重复记录。
func TestEventStore_Idempotent(t *testing.T) {
	s := NewEventMemory()
	ctx := context.Background()

	first, err := s.Write(ctx, contract.Event{EventID: "e-1"})
	if err != nil || !first {
		t.Fatalf("首次写入期望 true，得到 %v / %v", first, err)
	}
	second, err := s.Write(ctx, contract.Event{EventID: "e-1"})
	if err != nil || second {
		t.Fatalf("重复写入期望 false，得到 %v / %v", second, err)
	}
}

// TestEventStore_RejectsEmptyID 断言无幂等键的事件被拒 —— 宁可报错也不写脏数据。
func TestEventStore_RejectsEmptyID(t *testing.T) {
	if _, err := NewEventMemory().Write(context.Background(), contract.Event{}); err != ErrEmptyEventID {
		t.Fatalf("期望 ErrEmptyEventID，得到 %v", err)
	}
}

// TestEventStore_WriteBatchCountsOnlyNew 断言批量写入只计新增。
func TestEventStore_WriteBatchCountsOnlyNew(t *testing.T) {
	s := NewEventMemory()
	ctx := context.Background()
	if _, err := s.Write(ctx, contract.Event{EventID: "e-1"}); err != nil {
		t.Fatal(err)
	}
	n, err := s.WriteBatch(ctx, []contract.Event{{EventID: "e-1"}, {EventID: "e-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("期望新增 1 条，得到 %d", n)
	}
}

// TestIsolation_TTLExpiry：隔离到期自动解除；时钟注入使行为可回放。
func TestIsolation_TTLExpiry(t *testing.T) {
	now := time.Unix(1000, 0)
	s := NewIsolationMemory(func() time.Time { return now })
	ctx := context.Background()
	key := contract.SessionKey{ID: "s-1"}

	if err := s.Put(ctx, key, contract.IsolationHit{Hit: true, ExpiresAt: now.Add(10 * time.Second)}); err != nil {
		t.Fatal(err)
	}

	hit, err := s.Get(ctx, key)
	if err != nil || !hit.Hit {
		t.Fatalf("TTL 内期望命中，得到 %+v / %v", hit, err)
	}

	now = now.Add(11 * time.Second) // 时钟前推
	hit, err = s.Get(ctx, key)
	if err != nil || hit.Hit {
		t.Fatalf("TTL 外期望自动解除，得到 %+v / %v", hit, err)
	}
}

// TestSession_TTLExpiry 覆盖会话状态带 TTL。
func TestSession_TTLExpiry(t *testing.T) {
	now := time.Unix(1000, 0)
	s := NewSessionMemory(func() time.Time { return now })
	ctx := context.Background()

	if err := s.Put(ctx, contract.SessionKey{ID: "s-1"}, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get(ctx, "s-1"); !ok {
		t.Fatal("TTL 内应命中")
	}
	now = now.Add(11 * time.Second)
	if _, ok, _ := s.Get(ctx, "s-1"); ok {
		t.Fatal("TTL 外应过期")
	}
}

// TestPolicy_VersionMonotonic：版本只增，回滚即发布新版本。
func TestPolicy_VersionMonotonic(t *testing.T) {
	s := NewPolicyMemory()
	ctx := context.Background()

	if err := s.Publish(ctx, contract.PolicySnapshot{PolicyID: "p", Version: 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(ctx, contract.PolicySnapshot{PolicyID: "p", Version: 2}); err == nil {
		t.Fatal("期望同版本被拒")
	}
	if err := s.Publish(ctx, contract.PolicySnapshot{PolicyID: "p", Version: 1}); err == nil {
		t.Fatal("期望降版本被拒")
	}
	if err := s.Publish(ctx, contract.PolicySnapshot{PolicyID: "p", Version: 3}); err != nil {
		t.Fatalf("更高版本应被接受: %v", err)
	}
}

// TestPolicy_CurrentBeforePublish 断言未发布时的显式错误，而不是静默零值。
func TestPolicy_CurrentBeforePublish(t *testing.T) {
	if _, err := NewPolicyMemory().Current(context.Background()); err != ErrNoPolicy {
		t.Fatalf("期望 ErrNoPolicy，得到 %v", err)
	}
}

// TestDecisionStore_CacheRoundTrip 断言判定缓存可读写。
func TestDecisionStore_CacheRoundTrip(t *testing.T) {
	s := NewDecisionMemory()
	ctx := context.Background()
	d := contract.Decision{DecisionID: "d-1", Action: contract.ActionMirage, Backend: "lou-1"}

	if _, ok, _ := s.GetCached(ctx, "d-1"); ok {
		t.Fatal("写之前不应命中")
	}
	if err := s.PutCached(ctx, d, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetCached(ctx, "d-1")
	if err != nil || !ok || got.Backend != "lou-1" {
		t.Fatalf("缓存往返失败: %+v / %v / %v", got, ok, err)
	}
}

// TestMemStores_AllFiveImplementations 断言打包体五个实现都满足各自接口。
func TestMemStores_AllFiveImplementations(t *testing.T) {
	m := NewMemStores(nil)
	if m.Session == nil || m.Isolation == nil || m.Decision == nil || m.Event == nil || m.Policy == nil {
		t.Fatal("MemStores 不应有 nil 字段")
	}
}
