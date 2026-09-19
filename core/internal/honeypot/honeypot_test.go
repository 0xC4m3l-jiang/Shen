package honeypot

import (
	"context"
	"testing"

	"shen/core/internal/contract"
)

func backend(name, typ, addr string, enabled, healthy bool) contract.HoneypotBackend {
	return contract.HoneypotBackend{Name: name, Type: typ, Addr: addr, Enabled: enabled, Healthy: healthy}
}

func mustNew(t *testing.T, bs ...contract.HoneypotBackend) *Engine {
	t.Helper()
	e, err := New(bs, nil)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	return e
}

// ── 解析（route_mirage 的落点）────────────────────────────────────────────

func TestResolveEnabledHealthy(t *testing.T) {
	e := mustNew(t, backend("mirage", "ssh", "10.0.0.9:2222", true, true))
	got, ok, err := e.Resolve(context.Background(), "mirage")
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if !ok {
		t.Fatal("启用且健康的后端应解析成功")
	}
	if got.Addr != "10.0.0.9:2222" {
		t.Errorf("地址应原样返回，得到 %q", got.Addr)
	}
}

func TestResolveUnknownName(t *testing.T) {
	e := mustNew(t)
	if _, ok, _ := e.Resolve(context.Background(), "没有这个后端"); ok {
		t.Error("未登记的名字不得解析成功（NI-5：回落业务）")
	}
}

func TestResolveDisabledIsNotResolved(t *testing.T) {
	e := mustNew(t, backend("mirage", "ssh", "addr", false, true))
	if _, ok, _ := e.Resolve(context.Background(), "mirage"); ok {
		t.Error("未启用的后端不得解析成功（默认 off，INT-11）")
	}
}

func TestResolveUnhealthyIsNotResolved(t *testing.T) {
	e := mustNew(t, backend("mirage", "ssh", "addr", true, false))
	if _, ok, _ := e.Resolve(context.Background(), "mirage"); ok {
		t.Error("不健康的后端不得解析成功（宁可漏引流，不可断业务）")
	}
}

func TestResolveEmptyName(t *testing.T) {
	e := mustNew(t, backend("mirage", "ssh", "addr", true, true))
	if _, ok, _ := e.Resolve(context.Background(), ""); ok {
		t.Error("空名字不得解析成功")
	}
}

// ── 配置校验 ───────────────────────────────────────────────────────────────

func TestNewRejectsInvalid(t *testing.T) {
	cases := map[string][]contract.HoneypotBackend{
		"名字为空":  {backend("", "ssh", "a", true, true)},
		"名字重复":  {backend("x", "ssh", "a", true, true), backend("x", "redis", "b", true, true)},
		"类型未登记": {backend("x", "telnet", "a", true, true)},
		"地址为空":  {backend("x", "ssh", "", true, true)},
	}
	for name, bs := range cases {
		if _, err := New(bs, nil); err == nil {
			t.Errorf("%s：应构造失败", name)
		}
	}
}

func TestNewAcceptsEmptyPool(t *testing.T) {
	// 空池合法：阶段 2a 的后端表可为空，此时 route_mirage 全部回落业务。
	e, err := New(nil, nil)
	if err != nil {
		t.Fatalf("空池应合法：%v", err)
	}
	if _, ok, _ := e.Resolve(context.Background(), "mirage"); ok {
		t.Error("空池不应解析出任何后端")
	}
}

func TestNewRejectsEmptyTypeInRegistry(t *testing.T) {
	if _, err := New(nil, []string{"ssh", ""}); err == nil {
		t.Error("已知类型列表含空串应被拒绝")
	}
}

// ── 管理面 ─────────────────────────────────────────────────────────────────

func TestRegisterThenResolve(t *testing.T) {
	e := mustNew(t)
	ctx := context.Background()
	if err := e.Register(ctx, backend("hp1", "redis", "r:6379", true, true)); err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	if _, ok, _ := e.Resolve(ctx, "hp1"); !ok {
		t.Error("登记后应能解析")
	}
}

func TestRegisterRejectsUnknownType(t *testing.T) {
	e := mustNew(t)
	if err := e.Register(context.Background(), backend("hp1", "gopher", "a", true, true)); err == nil {
		t.Error("未登记类型不得通过 Register")
	}
}

func TestSetEnabledToggles(t *testing.T) {
	e := mustNew(t, backend("hp1", "ssh", "a", false, true))
	ctx := context.Background()
	if err := e.SetEnabled(ctx, "hp1", true); err != nil {
		t.Fatalf("开关失败：%v", err)
	}
	if _, ok, _ := e.Resolve(ctx, "hp1"); !ok {
		t.Error("打开后应能解析")
	}
	if err := e.SetEnabled(ctx, "hp1", false); err != nil {
		t.Fatalf("开关失败：%v", err)
	}
	if _, ok, _ := e.Resolve(ctx, "hp1"); ok {
		t.Error("关闭后不应解析")
	}
}

func TestSetEnabledUnknownName(t *testing.T) {
	e := mustNew(t)
	if err := e.SetEnabled(context.Background(), "nope", true); err == nil {
		t.Error("操作不存在的后端应报错")
	}
}

func TestSetHealthy(t *testing.T) {
	e := mustNew(t, backend("hp1", "ssh", "a", true, false))
	ctx := context.Background()
	if _, ok, _ := e.Resolve(ctx, "hp1"); ok {
		t.Fatal("初始不健康不应解析")
	}
	if err := e.SetHealthy(ctx, "hp1", true); err != nil {
		t.Fatalf("健康更新失败：%v", err)
	}
	if _, ok, _ := e.Resolve(ctx, "hp1"); !ok {
		t.Error("恢复健康后应能解析")
	}
}

// ── 列表与类型 ─────────────────────────────────────────────────────────────

func TestListSortedAndCopied(t *testing.T) {
	e := mustNew(t,
		backend("b", "ssh", "a", true, true),
		backend("a", "redis", "c", true, true),
	)
	got, err := e.List(context.Background())
	if err != nil {
		t.Fatalf("列表失败：%v", err)
	}
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("应按名字排序，得到 %+v", got)
	}
	// 改写返回值不得影响内部池
	got[0].Addr = "被改写"
	again, _ := e.List(context.Background())
	if again[0].Addr == "被改写" {
		t.Error("List 应返回副本")
	}
}

func TestTypesDedupAndOrder(t *testing.T) {
	e := mustNew(t,
		backend("b", "redis", "a", true, true),
		backend("a", "ssh", "c", true, true),
		backend("c", "redis", "d", true, true),
	)
	got, err := e.Types(context.Background())
	if err != nil {
		t.Fatalf("取类型失败：%v", err)
	}
	// KnownTypes 顺序：ssh 在 redis 之前
	if len(got) != 2 || got[0] != "ssh" || got[1] != "redis" {
		t.Errorf("应按 KnownTypes 顺序去重，得到 %v", got)
	}
}
