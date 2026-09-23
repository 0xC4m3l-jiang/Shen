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
//
// 优先级**只在这里定义一次**（`INT-19`）：适配器送来的 `SessionHint` 只是「按名字取出的候选值」，
// 最终用哪一级仍然由本函数说了算。
func (e *Extractor) Key(_ context.Context, obs contract.Observation) (contract.SessionKey, error) {
	// ① 业务自身的 session cookie —— 零痕迹（Agent 本来就会带）
	//
	// 两个来源（取先有的那个）：
	//   · `SessionHint`：新适配器按 `session.cookie_name` 取出**并只送这一个值**（推荐，认证材料不跨接缝）；
	//   · `headers["cookie"]` 里的同名项：旧适配器仍能工作（兼容路径，不删）。
	if v := strings.TrimSpace(obs.SessionHint); v != "" {
		return contract.SessionKey{ID: v, Source: contract.SourceBusinessCookie}, nil
	}
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
