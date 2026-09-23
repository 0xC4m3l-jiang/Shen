package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ── 会话身份口径统一（方案 FIX-5 前半 / §5.2）──────────────────────────────
//
// 三条要钉住的语义：
//  ① 适配器只取**约定的那一个 cookie 的值**（最小身份），不是整段 Cookie 头；
//  ② 整段 Cookie **不跨接缝**（认证材料留在适配器这一侧）；
//  ③ `decision_id` 与**变体选择**用的是同一个身份值（否则「同一个会话」在三处是三个意思）。

func TestSessionHintExtractsOnlyConfiguredCookie(t *testing.T) {
	cases := []struct {
		name   string
		cookie string
		want   string
	}{
		{"只取约定的那个", "sid=abc; other=zzz", "abc"},
		{"顺序无关", "other=zzz; sid=abc", "abc"},
		{"同名多项取第一个", "sid=first; sid=second", "first"},
		{"没有这个名字", "other=zzz", ""},
		{"完全没有 cookie", "", ""},
		{"值里有等号", "sid=a=b", "a=b"},
		// 名字两侧的空白**不宽容**：`sid = abc` 不是合法的 cookie-pair（RFC 6265 的 name 不含空白），
		// 核心的 `session.cookieValue` 也是这个口径 —— 两侧必须同口径，否则「谁是会话」又会分成两套。
		{"名字两侧空白不宽容（与核心同口径）", "  sid = abc ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://svc.example/", nil)
			if c.cookie != "" {
				r.Header.Set("Cookie", c.cookie)
			}
			if got := sessionHint(r, "sid"); got != c.want {
				t.Fatalf("sessionHint(%q) = %q，期望 %q", c.cookie, got, c.want)
			}
		})
	}
	// 名字为空时不得靠猜：返回空（核心回退到 TLS/指纹，见 INT-19）。
	r := httptest.NewRequest(http.MethodGet, "http://svc.example/", nil)
	r.Header.Set("Cookie", "sid=abc")
	if got := sessionHint(r, "  "); got != "" {
		t.Fatalf("cookie 名为空时应返回空串，得到 %q", got)
	}
}

func TestObservationCarriesMinimalSessionAndNoRawCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://svc.example/api/me", nil)
	r.Header.Set("Cookie", "sid=sess-1; auth_token=SECRET-JWT; csrf=zzz")

	obs := observationFrom(r, false, "sid")

	if got := obs.GetSessionHint(); got != "sess-1" {
		t.Fatalf("session_hint 应是约定的那一个 cookie 值，得到 %q", got)
	}
	if _, forwarded := obs.GetHeaders()["cookie"]; forwarded {
		t.Fatal("整段 Cookie 头**禁止**跨接缝（§5.2：认证材料留在适配器侧）")
	}
	for k, v := range obs.GetHeaders() {
		if v == "SECRET-JWT" || v == "zzz" {
			t.Fatalf("观测里出现了其它 cookie 的值（键 %q）—— 它们与判定无关，不该出接缝", k)
		}
	}
}

// TestVariantUsesSameIdentityAsDecisionID 断言「同一个会话」在变体选择与 decision_id 上是同一个东西：
// 无关 cookie 变化不影响两者；会话 cookie 变化则两者都变。
func TestVariantUsesSameIdentityAsDecisionID(t *testing.T) {
	manifest := testManifest(8)
	h := contentHandler(t, manifest, true)
	fixedNow := time.Unix(1_700_000_000, 0)

	req := func(cookie string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://svc.example"+testResource, nil)
		r.Header.Set("Cookie", cookie)
		return r
	}
	sameSession := []string{"sid=sess-1; a=1", "sid=sess-1; b=2"}
	otherSession := "sid=sess-2; a=1"

	var firstVariant int
	var firstID string
	for i, cookie := range sameSession {
		r := req(cookie)
		v := h.variantOfSession(h.sessionKeyOf(r), manifestIndex(t, manifest))
		id := decisionID(h.sessionKeyOf(r), clientIP(r, false), r.URL.Path, time.Minute, fixedNow)
		if i == 0 {
			firstVariant, firstID = v, id
			continue
		}
		if v != firstVariant {
			t.Fatalf("同会话（仅无关 cookie 不同）必须落在同一变体槽位：%d vs %d", firstVariant, v)
		}
		if id != firstID {
			t.Fatalf("同会话必须得到同一 decision_id：%s vs %s", firstID, id)
		}
	}

	r := req(otherSession)
	if id := decisionID(h.sessionKeyOf(r), clientIP(r, false), r.URL.Path, time.Minute, fixedNow); id == firstID {
		t.Fatal("换会话必须换 decision_id")
	}
}

// manifestIndex 取夹具清单索引（与 `contentHandler` 内部用的是同一份；`newContentIndex` 返回 nil 时直接失败）。
func manifestIndex(t *testing.T, m *contentManifest) *contentIndex {
	t.Helper()
	idx := newContentIndex(m)
	if idx == nil {
		t.Fatal("夹具清单应可索引")
	}
	return idx
}
