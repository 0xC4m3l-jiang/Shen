package proxy

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	judgev1 "shen/api/judge/v1"
)

// TestRequestJudgedWireContract 守住 `request_judged` 的**跨语言契约**。
//
// 控制台的流量调度图靠这些字段画「实际落点 + 返回信息」（executed / status / bytes / duration_ms），
// 所以键集一旦漂移，图就会悄悄画错。规矩见 docs/spec/events.md §4：
// 改键必须同时改契文档、重新生成夹具 api/telemetry/v1/testdata/request_judged_event.json、并同步两侧测试。
func TestRequestJudgedWireContract(t *testing.T) {
	raw, err := os.ReadFile("../../api/telemetry/v1/testdata/request_judged_event.json")
	if err != nil {
		t.Fatalf("读不到契约夹具：%v", err)
	}
	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("夹具不是合法 JSON：%v", err)
	}

	h := &Handler{Shadow: false}
	req := httptest.NewRequest("GET", "http://shop.example.com/.git/config", nil)
	req.Header.Set("User-Agent", "HeadlessChrome/120")
	got := h.judgedEventPayload(req, judgev1.Action_ACTION_MIRAGE, routeInfo{
		executed:   executedFallback,
		backend:    "mirage",
		status:     200,
		bytes:      8123,
		durationMs: 4.2,
	})

	if len(got) != len(want) {
		t.Errorf("键数量不一致：夹具 %d 个，实际 %d 个（增删键必须同步 events.md 与夹具）", len(want), len(got))
	}
	for key, wantValue := range want {
		gotValue, ok := got[key]
		if !ok {
			t.Errorf("契约键 %q 在真实载荷里缺失（已漂移）", key)
			continue
		}
		left, _ := json.Marshal(gotValue)
		right, _ := json.Marshal(wantValue)
		if string(left) != string(right) {
			t.Errorf("键 %q 值不一致：夹具 %s，实际 %s", key, right, left)
		}
	}
}

// TestExecutedFor 穷举「实际落点」的取值。
//
// 这是图上区分「核心判成什么」与「适配器实际走了哪」的唯一依据，必须逐分支钉死：
// 影子模式只观测（INT-11）、后端不可用回落业务（NI-5）、拦截走 403。
func TestExecutedFor(t *testing.T) {
	cases := []struct {
		name          string
		shadow        bool
		act           judgev1.Action
		mirageFound   bool
		mirageFellBak bool
		want          string
	}{
		{"影子模式一律记 origin（只观测）", true, judgev1.Action_ACTION_MIRAGE, true, false, executedOrigin},
		{"放行", false, judgev1.Action_ACTION_ORIGIN, false, false, executedOrigin},
		{"未识别取值按放行", false, judgev1.Action_ACTION_UNSPECIFIED, false, false, executedOrigin},
		{"拦截返回 403", false, judgev1.Action_ACTION_BLOCK, false, false, executedBlock},
		{"改道成功", false, judgev1.Action_ACTION_MIRAGE, true, false, executedMirage},
		{"后端未登记 ⇒ 回落源站（NI-5）", false, judgev1.Action_ACTION_MIRAGE, false, false, executedFallback},
		{"后端中途失败 ⇒ 回落源站（NI-5）", false, judgev1.Action_ACTION_MIRAGE, true, true, executedFallback},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := executedFor(tc.shadow, tc.act, tc.mirageFound, tc.mirageFellBak); got != tc.want {
				t.Errorf("executedFor(%v, %v, %v, %v) = %q，期望 %q",
					tc.shadow, tc.act, tc.mirageFound, tc.mirageFellBak, got, tc.want)
			}
		})
	}
}
