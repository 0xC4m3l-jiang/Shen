package decoy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"shen/core/internal/contract"
)

// ── 替身（MD-22）───────────────────────────────────────────────────────────

type stubStore struct {
	m   map[string]contract.DecoyAsset
	err error
}

func newStub(assets ...contract.DecoyAsset) *stubStore {
	s := &stubStore{m: map[string]contract.DecoyAsset{}}
	for _, a := range assets {
		s.m[a.ID] = a
	}
	return s
}

func (s *stubStore) List(context.Context) ([]contract.DecoyAsset, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]contract.DecoyAsset, 0, len(s.m))
	for _, a := range s.m {
		out = append(out, a)
	}
	return out, nil
}

func (s *stubStore) Get(_ context.Context, id string) (contract.DecoyAsset, bool, error) {
	if s.err != nil {
		return contract.DecoyAsset{}, false, s.err
	}
	a, ok := s.m[id]
	return a, ok, nil
}

func asset(id string, kind contract.DecoyKind, path string, enabled bool) contract.DecoyAsset {
	return contract.DecoyAsset{ID: id, Kind: kind, Path: path, Content: "tmpl", Enabled: enabled}
}

func mustEngine(t *testing.T, st AssetStore) *Engine {
	t.Helper()
	e, err := New(st, 0)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	return e
}

// ── 匹配（最长前缀）────────────────────────────────────────────────────────

func TestMatchLongestPrefixWins(t *testing.T) {
	e := mustEngine(t, newStub(
		asset("broad", contract.DecoyBait, "/internal", true),
		asset("narrow", contract.DecoyBait, "/internal/openapi.json", true),
	))
	got, ok, err := e.Match(context.Background(), "/internal/openapi.json")
	if err != nil {
		t.Fatalf("匹配失败：%v", err)
	}
	if !ok || got.ID != "narrow" {
		t.Fatalf("应命中更长的前缀，得到 %+v（ok=%v）", got, ok)
	}
}

func TestMatchMiss(t *testing.T) {
	e := mustEngine(t, newStub(asset("a", contract.DecoyBait, "/_bait", true)))
	if _, ok, _ := e.Match(context.Background(), "/api/users"); ok {
		t.Error("非诱饵路径不得命中")
	}
}

func TestMatchIgnoresDisabled(t *testing.T) {
	e := mustEngine(t, newStub(asset("a", contract.DecoyBait, "/_bait", false)))
	if _, ok, _ := e.Match(context.Background(), "/_bait/x"); ok {
		t.Error("未启用的诱饵不得命中")
	}
}

func TestMatchEmptyPathAssetIgnored(t *testing.T) {
	e := mustEngine(t, newStub(asset("a", contract.DecoyBait, "", true)))
	if _, ok, _ := e.Match(context.Background(), "/anything"); ok {
		t.Error("空路径的资产不得匹配一切")
	}
}

func TestEnabledFilters(t *testing.T) {
	e := mustEngine(t, newStub(
		asset("on", contract.DecoyBait, "/a", true),
		asset("off", contract.DecoyMCP, "/b", false),
	))
	got, err := e.Enabled(context.Background())
	if err != nil {
		t.Fatalf("失败：%v", err)
	}
	if len(got) != 1 || got[0].ID != "on" {
		t.Errorf("只应返回启用的资产，得到 %+v", got)
	}
}

// ── 多态（ADR-0016）────────────────────────────────────────────────────────

func TestVariantDeterministic(t *testing.T) {
	e := mustEngine(t, newStub())
	ctx := context.Background()
	first, err := e.Variant(ctx, "session-abc")
	if err != nil {
		t.Fatalf("失败：%v", err)
	}
	for i := 0; i < 20; i++ {
		got, _ := e.Variant(ctx, "session-abc")
		if got != first {
			t.Fatalf("同会话必须恒同变体：%d vs %d", first, got)
		}
	}
	if first >= VariantCount {
		t.Errorf("变体号必须落在 [0,%d)，得到 %d", VariantCount, first)
	}
}

func TestVariantDiffersAcrossSessions(t *testing.T) {
	e := mustEngine(t, newStub())
	ctx := context.Background()
	seen := map[uint8]bool{}
	for _, id := range []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8"} {
		v, _ := e.Variant(ctx, id)
		seen[v] = true
	}
	if len(seen) < 2 {
		t.Error("不同会话应能取到不同变体（否则多态没有意义）")
	}
}

func TestVariantRejectsEmptySession(t *testing.T) {
	e := mustEngine(t, newStub())
	for _, id := range []string{"", "   "} {
		if _, err := e.Variant(context.Background(), id); err == nil {
			t.Errorf("空会话键 %q 应被拒绝", id)
		}
	}
}

// ── 投放片段 ───────────────────────────────────────────────────────────────

func TestPlacementsPerKind(t *testing.T) {
	ctx := context.Background()
	kinds := []contract.DecoyKind{
		contract.DecoyDeveloperAPI, contract.DecoyInstructionFile,
		contract.DecoyMCP, contract.DecoyDataset, contract.DecoyBait,
	}
	for _, k := range kinds {
		e := mustEngine(t, newStub(asset("a", k, "/_bait/x", true)))
		got, err := e.Placements(ctx, "a")
		if err != nil {
			t.Fatalf("%s：失败：%v", k, err)
		}
		if len(got) == 0 {
			t.Errorf("%s：应有投放片段（投放才是最后一公里）", k)
		}
		for _, p := range got {
			if !strings.Contains(p, "/_bait/x") {
				t.Errorf("%s：片段应含诱饵路径，得到 %q", k, p)
			}
		}
	}
}

func TestPlacementsUnknownAsset(t *testing.T) {
	e := mustEngine(t, newStub())
	if _, err := e.Placements(context.Background(), "nope"); err == nil {
		t.Error("不存在的资产应报错")
	}
}

func TestPlacementsDisabledAsset(t *testing.T) {
	e := mustEngine(t, newStub(asset("a", contract.DecoyBait, "/x", false)))
	if _, err := e.Placements(context.Background(), "a"); err == nil {
		t.Error("未启用的资产不应产出投放片段")
	}
}

// ── 失败路径与构造 ─────────────────────────────────────────────────────────

func TestStoreErrorPropagates(t *testing.T) {
	st := newStub()
	st.err = errors.New("pg down")
	e := mustEngine(t, st)
	ctx := context.Background()
	if _, err := e.Enabled(ctx); err == nil {
		t.Error("Enabled 应上抛存储错误")
	}
	if _, _, err := e.Match(ctx, "/x"); err == nil {
		t.Error("Match 应上抛存储错误")
	}
	if _, err := e.Placements(ctx, "a"); err == nil {
		t.Error("Placements 应上抛存储错误")
	}
}

func TestNewRejectsNil(t *testing.T) {
	if _, err := New(nil, 0); err == nil {
		t.Error("store 为 nil 应构造失败")
	}
}
