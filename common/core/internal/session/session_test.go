package session

import (
	"context"
	"net/netip"
	"testing"

	"shen/common/core/internal/contract"
)

func obs(headers map[string]string, tls string) contract.Observation {
	return contract.Observation{
		SourceIP:       netip.MustParseAddr("203.0.113.7"),
		UserAgent:      "curl/8.0",
		TLSFingerprint: tls,
		Headers:        headers,
	}
}

// TestKey_SessionHintWinsAndIsMinimal 断言适配器送来的**最小身份**是优先级① 的首选来源，
// 且整段 Cookie 不再参与（口径统一的落点：适配器「哪段算会话」与核心「按名取哪个」是同一个值）。
func TestKey_SessionHintWinsAndIsMinimal(t *testing.T) {
	e := New("sid")

	// 有 hint：直接采用（即使 headers 里有别的 cookie，也不参与）。
	withHint := obs(map[string]string{"cookie": "a=1; sid=from-header"}, "fp-tls")
	withHint.SessionHint = "from-hint"
	k, err := e.Key(context.Background(), withHint)
	if err != nil {
		t.Fatal(err)
	}
	if k.ID != "from-hint" || k.Source != contract.SourceBusinessCookie {
		t.Fatalf("有 session_hint 时应采用它，得到 %+v", k)
	}

	// 空白 hint 视为「没有」：回退到按名解析（兼容旧适配器）。
	blank := obs(map[string]string{"cookie": "sid=from-header"}, "fp-tls")
	blank.SessionHint = "   "
	k, err = e.Key(context.Background(), blank)
	if err != nil {
		t.Fatal(err)
	}
	if k.ID != "from-header" || k.Source != contract.SourceBusinessCookie {
		t.Fatalf("空白 hint 应回退到按名解析，得到 %+v", k)
	}

	// 既没有 hint、也没有该 cookie：退到 TLS 指纹（优先级②）。
	none := obs(map[string]string{"cookie": "other=1"}, "fp-tls")
	k, err = e.Key(context.Background(), none)
	if err != nil {
		t.Fatal(err)
	}
	if k.Source != contract.SourceTLSTicket {
		t.Fatalf("应退到 TLS 指纹，得到 %+v", k)
	}
}

// TestKey_PriorityBusinessCookie：验证优先级 ①（业务自身的 session cookie）。
func TestKey_PriorityBusinessCookie(t *testing.T) {
	e := New("sid")
	k, err := e.Key(context.Background(), obs(map[string]string{"cookie": "a=1; sid=abc123; b=2"}, "fp-tls"))
	if err != nil {
		t.Fatal(err)
	}
	if k.ID != "abc123" || k.Source != contract.SourceBusinessCookie {
		t.Fatalf("期望取到业务 cookie，得到 %+v", k)
	}
}

// TestKey_FallsBackToTLSTicket 覆盖优先级 ②（无业务 cookie 时）。
func TestKey_FallsBackToTLSTicket(t *testing.T) {
	e := New("sid")
	k, err := e.Key(context.Background(), obs(map[string]string{"cookie": "a=1"}, "fp-tls"))
	if err != nil {
		t.Fatal(err)
	}
	if k.Source != contract.SourceTLSTicket {
		t.Fatalf("期望退到 TLS ticket，得到 %+v", k)
	}
}

// TestKey_FallsBackToFingerprint 覆盖优先级 ③（兜底，仅首跳可用）。
func TestKey_FallsBackToFingerprint(t *testing.T) {
	e := New("sid")
	k, err := e.Key(context.Background(), obs(map[string]string{}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if k.Source != contract.SourceFingerprint {
		t.Fatalf("期望退到指纹，得到 %+v", k)
	}
	// 同一 IP+UA 必须稳定
	k2, _ := e.Key(context.Background(), obs(map[string]string{}, ""))
	if k.ID != k2.ID {
		t.Fatalf("指纹不稳定：%s != %s", k.ID, k2.ID)
	}
}

// TestKey_NoNewClientVisibleTrace：
// 提取会话身份不得向请求头写入任何东西（不得留下客户端可见痕迹）。
func TestKey_NoNewClientVisibleTrace(t *testing.T) {
	h := map[string]string{"cookie": "sid=abc"}
	before := len(h)
	e := New("sid")
	if _, err := e.Key(context.Background(), obs(h, "")); err != nil {
		t.Fatal(err)
	}
	if len(h) != before {
		t.Fatal("提取会话身份时改动了请求头 —— 不得留下客户端可见痕迹")
	}
}
