package control

import (
	"context"
	"time"

	"shen/core/internal/contract"
)

// 本文件是**观测面**的依赖契约：服务面对「记录一次判定」（写侧）与「列出最近事件」（读侧）的最小要求。
//
// 为什么单独定义而不是直接用 `store` 的类型：接口由**消费方**定义（与 `IsolationChecker` 同一规矩），
// 这样服务面不依赖具体存储形状，装配层（`cmd/core`）用一个薄适配器把 `store` 接上；
// 单测也可以用替身（`MD-22`）。

// DecisionRecord 是一次判定的**可观测快照**。
//
// 它比 `contract.Decision` 多了「请求身份」（来源 IP / 方法 / 路径 / UA）与「判定细节」（分值 / 命中信号），
// 因此能回答人工测试与排障最关心的那句话：**这个请求为什么被判成这样、然后去了哪**。
//
// ⚠️ 它**只进观测面**，禁止回给适配器或客户端（`ST-7`：判定响应不回显分值 / 规则名 / 枚举）。
type DecisionRecord struct {
	DecisionID string // 幂等键（ST-10）
	SourceIP   string
	Method     string
	Path       string
	UserAgent  string

	Action   string // 三值（放行 / 改道 / 拦截）
	Severity string // 旁路字段
	Backend  string // 仅改道时非空

	Score   float64  // 风险分（观测面可见）
	Signals []string // 命中信号 ID（观测面可见）

	At time.Time // 由调用方注入（MD-6）
}

// DecisionRecorder 是**写侧**依赖：每完成一次判定就记一笔。
//
// 记录失败**不得**影响请求（`NI-1`）：调用方只记日志，不把错误抛给客户端。
type DecisionRecorder interface {
	Record(ctx context.Context, r DecisionRecord) error
}

// EventLister 是**读侧**依赖：列最近的遥测事件（控制台看告警与流量访问）。
type EventLister interface {
	ListEvents(ctx context.Context, limit int, since time.Time, eventType string) ([]contract.Event, error)
}
