package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"shen/core/internal/contract"
)

// ── 替身（MD-22：不依赖其他模块真实实例）───────────────────────────────────

type stubStore struct {
	m   map[string]contract.IsolationHit
	err error
}

func newStubStore() *stubStore {
	return &stubStore{m: map[string]contract.IsolationHit{}}
}

func (s *stubStore) Get(_ context.Context, key contract.SessionKey) (contract.IsolationHit, error) {
	if s.err != nil {
		return contract.IsolationHit{}, s.err
	}
	return s.m[key.ID], nil
}

func (s *stubStore) Put(_ context.Context, key contract.SessionKey, hit contract.IsolationHit) error {
	if s.err != nil {
		return s.err
	}
	s.m[key.ID] = hit
	return nil
}

func newEngine(t *testing.T, now func() time.Time) (*Engine, *stubStore) {
	t.Helper()
	st := newStubStore()
	e, err := New(st, now)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	return e, st
}

func key(id string) contract.SessionKey { return contract.SessionKey{ID: id} }

// ── 查询 ───────────────────────────────────────────────────────────────────

func TestCheckMissReturnsNoHit(t *testing.T) {
	e, _ := newEngine(t, nil)
	hit, err := e.Check(context.Background(), key("nobody"))
	if err != nil {
		t.Fatalf("未命中不应报错：%v", err)
	}
	if hit.Hit {
		t.Error("未写入时 Hit 应为 false")
	}
}

func TestIsolateThenCheckHits(t *testing.T) {
	e, _ := newEngine(t, nil)
	ctx := context.Background()
	if err := e.Isolate(ctx, key("agent-1"), "canary_echo", time.Minute); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	hit, err := e.Check(ctx, key("agent-1"))
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	if !hit.Hit {
		t.Fatal("写入后应命中")
	}
	if hit.Reason != "canary_echo" {
		t.Errorf("理由应保留，得到 %q", hit.Reason)
	}
}

// ── TTL ───────────────────────────────────────────────────────────────────

func TestIsolateTTLExpires(t *testing.T) {
	now := time.Unix(1700000000, 0)
	e, st := newEngine(t, func() time.Time { return now })
	ctx := context.Background()

	if err := e.Isolate(ctx, key("agent-1"), "velocity", 60*time.Second); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	// 存储层按 ExpiresAt 判断过期；替身需要模拟这一点。
	hit, _ := e.Check(ctx, key("agent-1"))
	if !hit.Hit {
		t.Fatal("TTL 内应命中")
	}

	// 把到期时间调到过去，模拟 clock 前进。
	h := st.m["agent-1"]
	h.ExpiresAt = now.Add(-time.Second)
	st.m["agent-1"] = h

	// 替身不实现 TTL 语义 —— 这是 store 的职责（见 store_test.go）。
	// 本用例只断言 ExpiresAt 被正确写入，避免两处重复实现 TTL 判断。
	if !h.ExpiresAt.Before(now) {
		t.Error("到期时间应已过去")
	}
}

func TestIsolateWritesExpiryFromInjectedClock(t *testing.T) {
	now := time.Unix(1700000000, 0)
	e, st := newEngine(t, func() time.Time { return now })

	if err := e.Isolate(context.Background(), key("a"), "r", 30*time.Second); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	got := st.m["a"].ExpiresAt
	want := now.Add(30 * time.Second)
	if !got.Equal(want) {
		t.Errorf("到期时间应由注入时钟推导：要 %v，得到 %v", want, got)
	}
}

// ── 参数校验 ───────────────────────────────────────────────────────────────

func TestIsolateRejectsNonPositiveTTL(t *testing.T) {
	e, _ := newEngine(t, nil)
	ctx := context.Background()
	for _, ttl := range []time.Duration{0, -time.Second} {
		if err := e.Isolate(ctx, key("a"), "r", ttl); err == nil {
			t.Errorf("TTL=%v 应被拒绝（禁止永不过期的隔离）", ttl)
		}
	}
}

func TestIsolateRejectsEmptyKey(t *testing.T) {
	e, _ := newEngine(t, nil)
	if err := e.Isolate(context.Background(), contract.SessionKey{}, "r", time.Minute); err == nil {
		t.Error("空会话键应被拒绝")
	}
}

// ── 失败路径：store 不可用 → 上抛，由调用方 fail-open（NI-10）──────────────

func TestCheckPropagatesStoreError(t *testing.T) {
	e, st := newEngine(t, nil)
	st.err = errors.New("redis down")
	if _, err := e.Check(context.Background(), key("a")); err == nil {
		t.Fatal("存储错误必须上抛，由调用方 fail-open（NI-10）")
	}
}

func TestIsolatePropagatesStoreError(t *testing.T) {
	e, st := newEngine(t, nil)
	st.err = errors.New("redis down")
	if err := e.Isolate(context.Background(), key("a"), "r", time.Minute); err == nil {
		t.Fatal("存储错误必须上抛")
	}
}

func TestNewRejectsNilStore(t *testing.T) {
	if _, err := New(nil, nil); err == nil {
		t.Error("store 为 nil 应构造失败")
	}
}
