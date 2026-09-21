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
