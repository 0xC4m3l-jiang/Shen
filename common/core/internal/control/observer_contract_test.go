package control

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// 事件载荷是**跨语言契约**：Python 侧 L4（analysis/events.py）与控制台都按它解析。
// 本测试把「真实的 DecisionRecord 序列化结果」与入库夹具逐字段对照 —— 任何一方改键名都会红。
//
// 契约正文：docs/spec/events.md · 夹具：common/api/telemetry/v1/testdata/decision_event.json
func TestDecisionRecordWireContract(t *testing.T) {
	fixturePath := "../../../../common/api/telemetry/v1/testdata/decision_event.json"
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("读不到契约夹具 %s：%v", fixturePath, err)
	}
	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("夹具不是合法 JSON：%v", err)
	}

	rec := DecisionRecord{
		DecisionID: "d-9f3c1a2b",
		SourceIP:   "203.0.113.9",
		Method:     "GET",
		Path:       "/.git/config",
		Query:      "file=../../etc/passwd",
		QueryRaw:   "file=%2e%2e%2f%2e%2e%2fetc%2fpasswd",
		UserAgent:  "HeadlessChrome/120",
		SessionID:  "9d2a6f0c5b1e4a37",
		Action:     "route_origin",
		Severity:   "none",
		Score:      0.9,
		Signals:    []string{"ua-headless", "path-probe"},
		At:         time.Date(2026, 9, 19, 10, 0, 0, 0, time.FixedZone("CST", 8*3600)),
	}
	encoded, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("序列化结果不是合法 JSON：%v", err)
	}

	for key, wantValue := range want {
		gotValue, ok := got[key]
		if !ok {
			t.Errorf("契约键 %q 在真实序列化结果里缺失（夹具与 Go 结构体已漂移）", key)
			continue
		}
		if !jsonEqual(gotValue, wantValue) {
			t.Errorf("键 %q 的值不一致：夹具 %v，实际 %v", key, wantValue, gotValue)
		}
	}
	if len(got) != len(want) {
		t.Errorf("键数量不一致：夹具 %d 个，实际 %d 个（有新增/删除字段未同步契约）", len(want), len(got))
	}
}

func jsonEqual(a, b any) bool {
	left, err1 := json.Marshal(a)
	right, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(left) == string(right)
}
