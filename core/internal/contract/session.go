package contract

// SessionSource 是会话身份的来源，按优先级排列。
//
// 三条来源全部禁止在客户端留下新痕迹 —— 不得新增 cookie、不得新增响应头。
type SessionSource uint8

const (
	SourceBusinessCookie SessionSource = iota // ① 最优：业务自身的 session cookie
	SourceTLSTicket                           // ② TLS session ticket / resumption
	SourceFingerprint                         // ③ 兜底：源 IP + UA + 头指纹（仅首跳可用）
)

// String 返回唯一写法。
func (s SessionSource) String() string {
	switch s {
	case SourceBusinessCookie:
		return "business_cookie"
	case SourceTLSTicket:
		return "tls_ticket"
	case SourceFingerprint:
		return "fingerprint"
	default:
		return "unknown"
	}
}

// SessionKey 是 session 模块的输出。
//
// 本值禁止上行到业务后端，禁止回传给客户端，也不得出现在任何日志的对外面。
type SessionKey struct {
	ID      string        // 会话 ID
	Source  SessionSource // 取值来自哪一级
	ActorID string        // 归因后的 actor；未归因时为空
}
