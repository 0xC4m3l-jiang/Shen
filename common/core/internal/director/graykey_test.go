package director

// SHEN-M1-03：灰度归属必须是**会话级**，不能含路径与时间窗。
//
// 这组用例断言的是「同一会话在各处请求得到同一去向」，而不是某个具体百分比 ——
// 百分比属于策略，一致性属于正确性。

import (
	"fmt"
	"net/netip"
	"testing"

	"shen/common/core/internal/contract"
)

// TestGrayKey_IsSessionLevel 断言：同一会话、不同路径与 decision_id ⇒ 同一个灰度键。
func TestGrayKey_IsSessionLevel(t *testing.T) {
	keys := map[string]struct{}{}
	for i, path := range []string{"/index.html", "/api/user", "/api/orders", "/", "/static/app.js?v=1"} {
		req := contract.JudgeRequest{
			DecisionID: fmt.Sprintf("id-%d", i), // 不同路径/时间窗会派生不同 decision_id
			Session:    contract.SessionKey{ID: "sess-1"},
			Observed:   contract.Observation{Path: path},
		}
		keys[grayKey(req)] = struct{}{}
	}
	if len(keys) != 1 {
		t.Fatalf("同一会话应只有一个灰度键，实际 %d 个：%v", len(keys), keys)
	}
}

// TestGrayKey_Fallbacks 断言退路顺序：会话 → 源 IP → decision_id。
func TestGrayKey_Fallbacks(t *testing.T) {
	// 无会话、有源 IP ⇒ 用源 IP。
	req := contract.JudgeRequest{DecisionID: "id-1"}
	req.Observed.SourceIP = netip.MustParseAddr("203.0.113.9")
	if got := grayKey(req); got != "i:203.0.113.9" {
		t.Fatalf("无会话时应退到源 IP，实际 %q", got)
	}
	// 会话与源 IP 都没有 ⇒ 才退到 decision_id。
	if got := grayKey(contract.JudgeRequest{DecisionID: "id-2"}); got != "d:id-2" {
		t.Fatalf("两者皆无时应退到 decision_id，实际 %q", got)
	}
}

// TestGrayAllows_StablePerSession 断言：同一会话的结论不随路径/decision_id 改变 ——
// 这正是 M1-03 要修的行为（修前会翻转）。
func TestGrayAllows_StablePerSession(t *testing.T) {
	const pct = 50
	first := grayAllows(grayKey(contract.JudgeRequest{
		DecisionID: "id-0", Session: contract.SessionKey{ID: "sess-fixed"},
	}), pct)
	for i := 1; i < 200; i++ {
		got := grayAllows(grayKey(contract.JudgeRequest{
			DecisionID: fmt.Sprintf("id-%d", i),
			Session:    contract.SessionKey{ID: "sess-fixed"},
			Observed:   contract.Observation{Path: fmt.Sprintf("/p%d", i)},
		}), pct)
		if got != first {
			t.Fatalf("同一会话第 %d 次结论翻转（%v → %v）：灰度仍在按路径/时间窗生效", i, first, got)
		}
	}
}

// TestGrayAllows_Edges 边界：0% 全放行、100% 全改道、同键确定、不同键能落两侧。
func TestGrayAllows_Edges(t *testing.T) {
	if grayAllows("s:any", 0) {
		t.Fatal("0% 必须全部放行")
	}
	if !grayAllows("s:any", 100) {
		t.Fatal("100% 必须全部改道")
	}
	a, b := grayAllows("s:stable", 42), grayAllows("s:stable", 42)
	if a != b {
		t.Fatal("同一键必须确定性")
	}
	var yes, no int
	for i := range 200 {
		if grayAllows(fmt.Sprintf("s:k%d", i), 50) {
			yes++
		} else {
			no++
		}
	}
	if yes == 0 || no == 0 {
		t.Fatalf("50%% 灰度下 200 个不同键应两侧都有，实际 yes=%d no=%d", yes, no)
	}
}
