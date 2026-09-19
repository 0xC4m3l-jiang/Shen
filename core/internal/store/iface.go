// Package store 是核心唯一的 I/O 出口。
//
// 其他核心模块访问外部存储必须经本包，禁止直连 Redis / ClickHouse / PostgreSQL。
// 本包禁止反向依赖任何业务模块。
//
// 接口按实体拆小：每个核心模块只依赖它用得着的那一个。
// 若是一个大接口，Go 无法部分实现，单测替身就必须实现全部方法。
package store

import (
	"context"
	"time"

	"shen/core/internal/contract"
)

// SessionStore 承载会话状态（生产实现：Redis，带 TTL）。
type SessionStore interface {
	Get(ctx context.Context, id string) (contract.SessionKey, bool, error)
	Put(ctx context.Context, k contract.SessionKey, ttl time.Duration) error
}

// IsolationStore 承载隔离名单（生产实现：Redis 生效 + PostgreSQL 审计）。
type IsolationStore interface {
	Get(ctx context.Context, key contract.SessionKey) (contract.IsolationHit, error)
	Put(ctx context.Context, key contract.SessionKey, hit contract.IsolationHit) error
}

// DecisionQuery 是判定记录的查询条件（观测面读侧：控制台看「判定怎么流动」）。
type DecisionQuery struct {
	// Limit 是返回条数上限（newest first）。0 → DefaultListLimit。
	Limit int
	// Since 非零时只返回该时刻之后的记录。
	Since time.Time
}

// DecisionStore 承载判定结果（生产实现：Redis 缓存 + ClickHouse 归档）。
type DecisionStore interface {
	GetCached(ctx context.Context, decisionID string) (contract.Decision, bool, error)
	PutCached(ctx context.Context, d contract.Decision, ttl time.Duration) error
	Archive(ctx context.Context, d contract.Decision) error
	// List 返回最近的判定记录（newest first）。
	//
	// 它服务于**观测面读侧**：控制台要能回答「刚才那些请求被判成了什么、为什么」。
	// 生产实现（ClickHouse）应按时间倒序 + 分区裁剪实现，不要全表扫。
	List(ctx context.Context, q DecisionQuery) ([]contract.Decision, error)
}

// EventQuery 是遥测事件的查询条件（观测面读侧：控制台看告警与流量访问）。
type EventQuery struct {
	// Limit 是返回条数上限（newest first）。0 → DefaultListLimit。
	Limit int
	// Since 非零时只返回该时刻之后的事件。
	Since time.Time
	// Type 非空时只返回该类型的事件。
	Type string
}

// 观测面的默认取值：缓冲有上限，避免无界增长（与 MD-10 的容量上限同一精神）。
const (
	DefaultListLimit = 200
	// DefaultEventBuffer 是内存实现保留的最近事件条数。
	DefaultEventBuffer = 4096
)

// EventStore 承载遥测事件（生产实现：ClickHouse，只增不改）。
type EventStore interface {
	// Write 幂等：同 EventID 已存在时返回 false，不产生重复记录。
	Write(ctx context.Context, ev contract.Event) (bool, error)
	// WriteBatch 返回实际写入条数。
	WriteBatch(ctx context.Context, evs []contract.Event) (int, error)
	// List 返回最近的事件（newest first）—— 观测面读侧。
	List(ctx context.Context, q EventQuery) ([]contract.Event, error)
}

// PolicyStore 承载策略版本（生产实现：PostgreSQL，版本只增）与适配器回执。
type PolicyStore interface {
	Current(ctx context.Context) (contract.PolicySnapshot, error)
	Publish(ctx context.Context, p contract.PolicySnapshot) error
	// RecordAck 记录适配器对一个版本的处置结果（`AR-13` 的版本对账）。
	//
	// 回执是**可重放的幂等事实**，不是只增日志：同一 (policy_id, version, adapter_id)
	// 重复上报只保留最新一条（适配器重启后会重报，不能因此堆出一串记录）。
	RecordAck(ctx context.Context, a contract.PolicyAck) error
	// Acks 返回已记录的适配器回执（对账用：谁应用了哪个版本、为什么没应用）。
	Acks(ctx context.Context) ([]contract.PolicyAck, error)
}

// DecoyStore 承载诱饵资产（生产实现：PostgreSQL）。
// 资产是**数据**（可配置、可轮换），不是行为（ADR-0010）。
type DecoyStore interface {
	List(ctx context.Context) ([]contract.DecoyAsset, error)
	Get(ctx context.Context, id string) (contract.DecoyAsset, bool, error)
	Put(ctx context.Context, a contract.DecoyAsset) error
}

// ContentStore 承载**预生成的欺骗内容**（生产实现：PostgreSQL / Redis）。
//
// 键就是一致性键（会话 + 资源），使 AR-30（同会话同资源同答案）在存储层显式化：
// 内容由 L4 离线预生成落库，热路径只读。
type ContentStore interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Put(ctx context.Context, key string, body []byte, ttl time.Duration) error
}
