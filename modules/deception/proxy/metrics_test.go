package proxy

// SHEN-M1-02：路由指标的账本与告警判据。
//
// 这里测的是**纯逻辑**（窗口计数、翻转判定、告警阈值），不启 Caddy ——
// 指标算错比指标缺失更危险：缺失只是看不见，算错会让运维照着假数据做决定。

import (
	"strings"
	"testing"
	"time"
)

func newTestMetrics(start time.Time) (*routeMetrics, *time.Time) {
	clock := start
	m := newRouteMetrics(func() time.Time { return clock })
	return m, &clock
}

func TestMetricsCountsByServiceAndRatios(t *testing.T) {
	m, _ := newTestMetrics(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	m.record("a.example.com", "s1", executedMirage)
	m.record("a.example.com", "s2", executedMirage)
	m.record("a.example.com", "s3", executedOrigin)
	m.record("b.example.com", "s4", executedFailOpen)

	snap := m.snapshot()
	if snap.Total != 4 || snap.Mirage != 2 || snap.Failopen != 1 {
		t.Fatalf("计数不正确：%+v", snap)
	}
	if snap.RerouteRatio != 0.5 {
		t.Fatalf("改道率应为 0.5，实际 %.2f", snap.RerouteRatio)
	}
	if snap.FailopenRatio != 0.25 {
		t.Fatalf("failopen 占比应为 0.25，实际 %.2f", snap.FailopenRatio)
	}
	// 按服务下钻（M1-02 的验收要求）。
	if got := snap.ByService["a.example.com"]; got.Total != 3 || got.Mirage != 2 {
		t.Fatalf("按服务下钻不正确：%+v", got)
	}
	if got := snap.ByService["b.example.com"]; got.Failopen != 1 {
		t.Fatalf("b 服务的 failopen 计数不正确：%+v", got)
	}
}

func TestMetricsCountsSessionFlips(t *testing.T) {
	m, _ := newTestMetrics(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	// 同一会话：mirage → origin → mirage ⇒ 2 次翻转
	m.record("a.example.com", "sess-1", executedMirage)
	m.record("a.example.com", "sess-1", executedOrigin)
	m.record("a.example.com", "sess-1", executedMirage)
	// 同一会话连续两次同去向 ⇒ 不增
	m.record("a.example.com", "sess-2", executedOrigin)
	m.record("a.example.com", "sess-2", executedOrigin)

	if snap := m.snapshot(); snap.SessionFlipCount != 2 {
		t.Fatalf("翻转次数应为 2，实际 %d", snap.SessionFlipCount)
	}
}

func TestMetricsIgnoresSessionsWithoutIdentity(t *testing.T) {
	m, _ := newTestMetrics(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	// 空会话：无法判定「同一会话」，不得制造假翻转。
	m.record("a.example.com", "", executedMirage)
	m.record("a.example.com", "", executedOrigin)
	if snap := m.snapshot(); snap.SessionFlipCount != 0 {
		t.Fatalf("无会话身份时不得计翻转，实际 %d", snap.SessionFlipCount)
	}
}

func TestMetricsWindowResetsCountersButKeepsFlips(t *testing.T) {
	start := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	m, clock := newTestMetrics(start)
	m.record("a.example.com", "s1", executedMirage)
	m.record("a.example.com", "s1", executedOrigin) // 1 次翻转

	*clock = start.Add(metricsWindow + time.Second) // 滚出窗口
	m.record("a.example.com", "s2", executedMirage)
	snap := m.snapshot()
	if snap.Total != 1 || snap.Mirage != 1 {
		t.Fatalf("窗口应已重置（只保留新窗口的 1 条），实际 %+v", snap)
	}
	// 翻转是**跨窗口的事实**（对手可能在上一个窗口看到了另一种环境），不能跟着清零。
	if snap.SessionFlipCount != 1 {
		t.Fatalf("翻转计数不该随窗口清零，实际 %d", snap.SessionFlipCount)
	}
}

func TestAlertsOfIsPureAndThresholded(t *testing.T) {
	// 样本太少：比例没有意义 ⇒ 不告警。
	if got := alertsOf(metricsSnapshot{Total: 5, Failopen: 5, FailopenRatio: 1, RerouteRatio: 0}); len(got) != 0 {
		t.Fatalf("样本不足时不该告警，实际 %v", got)
	}
	// 引擎坏了：大部分请求走 failopen。
	got := alertsOf(metricsSnapshot{Total: 100, Failopen: 80, FailopenRatio: 0.8, RerouteRatio: 0.2, Sampled: true})
	if len(got) == 0 || got[0] != "failopen_ratio" {
		t.Fatalf("failopen 高企应告警，实际 %v", got)
	}
	// 会话翻转：直接度量「会不会露馅」。
	got = alertsOf(metricsSnapshot{Total: 100, RerouteRatio: 0.9, SessionFlipNew: 1, Sampled: true})
	if len(got) != 1 || got[0] != "session_flip" {
		t.Fatalf("翻转应单独告警，实际 %v", got)
	}
	// 名义在跑、实际不改道：灰度/策略配错的典型征象。
	got = alertsOf(metricsSnapshot{Total: 100, RerouteRatio: 0.01, Sampled: true})
	if len(got) != 1 || got[0] != "reroute_below_baseline" {
		t.Fatalf("改道率过低应告警，实际 %v", got)
	}
	// 正常：无告警。
	if got := alertsOf(metricsSnapshot{Total: 100, RerouteRatio: 0.6, FailopenRatio: 0, Sampled: true}); len(got) != 0 {
		t.Fatalf("正常窗口不该告警，实际 %v", got)
	}
}

func TestMetricsNilSafe(t *testing.T) {
	var m *routeMetrics
	m.record("a.example.com", "s1", executedMirage) // 不得 panic
	if snap := m.snapshot(); snap.Total != 0 {
		t.Fatalf("nil 账本应返回空快照，实际 %+v", snap)
	}
}

// TestSessionFlipAlertFiresOnce 断言：翻转告警**只报一次**（读一次快照即消费掉增量），
// 否则一次翻转会让系统永久告警 —— 那和没有告警一样（运维会学会忽略它）。
func TestSessionFlipAlertFiresOnce(t *testing.T) {
	m, _ := newTestMetrics(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	m.record("a.example.com", "s1", executedMirage)
	m.record("a.example.com", "s1", executedOrigin) // 一次翻转

	first := m.snapshot()
	if first.SessionFlipNew != 1 || len(first.Alerts) == 0 {
		t.Fatalf("第一次快照应看到新增翻转并告警：%+v", first)
	}
	second := m.snapshot()
	if second.SessionFlipNew != 0 {
		t.Fatalf("第二次快照不该重复报同一次翻转：%+v", second)
	}
	if second.SessionFlipCount != 1 {
		t.Fatalf("累计值应保持单调（=1），实际 %d", second.SessionFlipCount)
	}
	for _, a := range second.Alerts {
		if a == "session_flip" {
			t.Fatalf("同一次翻转不该再告警：%v", second.Alerts)
		}
	}
}

// TestSessionHashIsStableAndIrreversible 断言会话摘要是「稳定的短摘要」：
// 同会话恒同值（评估要能对上）、不同会话不同值、且**不含原值**（原值属认证材料，禁止进遥测）。
func TestSessionHashIsStableAndIrreversible(t *testing.T) {
	raw := "sess-abcdef-12345"
	h1 := sessionHash(raw)
	if h1 == "" || len(h1) != 16 {
		t.Fatalf("摘要应为 16 位十六进制，实际 %q", h1)
	}
	if sessionHash(raw) != h1 {
		t.Fatal("同一会话两次摘要不一致（评估会对不上）")
	}
	if sessionHash(raw+"x") == h1 {
		t.Fatal("不同会话不该得到同一个摘要")
	}
	if strings.Contains(h1, raw) || strings.Contains(h1, "abcdef") {
		t.Fatalf("摘要里泄漏了会话原值：%q", h1)
	}
	if got := sessionHash(""); got != "" {
		t.Fatalf("无会话身份时应为空串（键恒在），实际 %q", got)
	}
}
