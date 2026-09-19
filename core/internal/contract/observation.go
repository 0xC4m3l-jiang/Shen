package contract

import "net/netip"

// JudgeRequest 是适配器经接缝 S1（适配器 ↔ 核心的 gRPC）送入核心的判定请求。
type JudgeRequest struct {
	DecisionID string      // 由适配器派生，重试复用同一值
	Session    SessionKey  // 已提取的会话身份
	Observed   Observation // 本次观察
}

// Observation 是一次请求的首跳可用信息。
//
// 会话第一跳的决策就是终局（中途改道会被立刻识破），
// 因此判定只能靠这些首跳信息来做。
type Observation struct {
	SourceIP       netip.Addr
	UserAgent      string
	Method         string
	Path           string
	TLSFingerprint string
	Headers        map[string]string
}

// Field 取用于规则匹配的字段值；未知字段返回空串。
func (o Observation) Field(name string) string {
	switch name {
	case "user_agent":
		return o.UserAgent
	case "path":
		return o.Path
	case "method":
		return o.Method
	case "source_ip":
		return o.SourceIP.String()
	case "tls_fingerprint":
		return o.TLSFingerprint
	default:
		return ""
	}
}
