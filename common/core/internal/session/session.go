package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"shen/common/core/internal/contract"
)

// Extractor 按三级优先级提取会话身份。
//
// 三条来源全部不在客户端留下新痕迹。
type Extractor struct {
	// CookieName 是业务自身的 session cookie 名（优先级 ①）；
	// 留空则跳过第 ① 级，退到 ② 或 ③。
	CookieName string
}

// New 构造提取器。
func New(cookieName string) *Extractor { return &Extractor{CookieName: cookieName} }

// Key 按优先级取会话身份：① 业务 cookie → ② TLS ticket → ③ 指纹（仅首跳）。
func (e *Extractor) Key(_ context.Context, obs contract.Observation) (contract.SessionKey, error) {
	// ① 业务自身的 session cookie —— 零痕迹（Agent 本来就会带）
	if e.CookieName != "" {
		if v := cookieValue(obs.Headers["cookie"], e.CookieName); v != "" {
			return contract.SessionKey{ID: v, Source: contract.SourceBusinessCookie}, nil
		}
	}

	// ② TLS session ticket —— 零痕迹
	if obs.TLSFingerprint != "" {
		return contract.SessionKey{ID: "tls:" + obs.TLSFingerprint, Source: contract.SourceTLSTicket}, nil
	}

	// ③ 兜底：源 IP + UA 指纹 —— 仅首跳可用；换出口 IP 即失效
	sum := sha256.Sum256([]byte(obs.SourceIP.String() + "\x00" + obs.UserAgent))
	return contract.SessionKey{ID: "fp:" + hex.EncodeToString(sum[:8]), Source: contract.SourceFingerprint}, nil
}

// cookieValue 从 Cookie 头取指定名；不依赖 net/http，便于单测。
func cookieValue(header, name string) string {
	for _, part := range strings.Split(header, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && k == name {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

var _ Session = (*Extractor)(nil)
