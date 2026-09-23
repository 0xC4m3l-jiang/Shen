package web

import (
	"context"
	"time"
)

// 本文件是**对外契约**：装配层只依赖这些类型，不依赖具体 handler。

// EventKind 是合成交互的事件种类（登记值；新增必须同步这里与模块文档）。
type EventKind string

const (
	// EventLoginAttempt：有人提交了合成登录表单。
	EventLoginAttempt EventKind = "login_attempt"
	// EventPageView：有人打开了合成管理台的某个页面/接口。
	EventPageView EventKind = "page_view"
	// EventStateChange：有人对**合成对象**发起了受限写操作（`C06`）。
	//
	// `Outcome` 取四个登记值：`applied`（值真的变了）· `noop`（幂等命中，值本来就一样）·
	// `rejected`（被拒：不存在/取值非法/版本冲突/配额用尽）· 空（未涉及）。
	EventStateChange EventKind = "state_change"
)

// Event 是一条**合成交互事件**：谁在什么时候碰了哪个场景资源。
//
// 两条硬约束（方案 §5.2）：
//
//	① **禁止**携带请求体 —— 尤其是密码：误填的真实凭据默认丢弃正文，不进事件；
//	② 只放**场景内**的标识（用户名/路径/结果），不放原始认证材料；
//	③ 会话只能以 `Session`（**脱敏后的可关联标识**）出现，禁止写 cookie 原值。
//
// 字段要求（`W5`）：`At` 与 `Path` 必须由 handler 补齐（不再允许"声明了但没人填"），
// 会话关联必须脱敏 —— 否则事件读起来方便，代价是把一份凭据抄进了观测面。
type Event struct {
	Kind    EventKind
	At      time.Time // 事件时刻（handler 用注入的时钟补齐）
	Path    string    // 合成资源路径（如 /admin/users）
	User    string    // 合成登录里提交的用户名（可为空）
	Outcome string    // 合成结果：demo / fail / throttled / page / dashboard / api …
	// Session 是**脱敏后的**会话标识（哈希前缀）：同一次会话的事件能对上，
	// 但拿到它无法冒充那次会话（`scrubSession`）。
	Session string
}

// EventSink 是事件出口（**由消费方定义**：装配层接上自己的观测面，测试接替身）。
//
// 实现**必须**是非阻塞或极短的：它跑在诱饵后端的请求路径上，
// 慢接收方不能拖住合成交互（诱饵侧同样不该被上游抖动放大）。
type EventSink interface {
	Event(ctx context.Context, ev Event)
}
