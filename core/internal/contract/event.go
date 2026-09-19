package contract

import "time"

// Event 是 telemetry 模块的输入。
//
// EventID 是幂等键：重复上报不得产生重复记录。
type Event struct {
	EventID   string
	Type      string
	ActorID   string
	SessionID string
	Payload   []byte
	// CreatedAt 由调用方注入。
	// 核心禁止依赖系统时钟做判定 —— 时间必须由调用方注入，才可测、可回放。
	CreatedAt time.Time
}
