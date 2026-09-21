package telemetry

import (
	"sync"
	"sync/atomic"

	"shen/common/core/internal/contract"
)

// 订阅缓冲的默认容量与上限。
//
// 这是**投递队列**，不是历史存储 —— 订阅者跟不上时丢的是**最旧**的：
// 观测台上「最近发生了什么」比「十分钟前漏了什么」更值钱。
const (
	DefaultBufferCapacity = 256
	MaxBufferCapacity     = 4096
)

// Subscription 是一个订阅者的投递队列。
//
// 它**不是** thread-safe 之外的任何东西：没有持久化、没有重放、
// 断了就是断了（补漏靠 `WatchEventsRequest.since` 从 store 补，不靠这里）。
type Subscription struct {
	ch       chan contract.Event
	dropped  atomic.Uint64
	capacity int
	hub      *Hub
	once     sync.Once
}

// Events 是投递队列的读端。队列被关闭时该 channel 关闭。
func (s *Subscription) Events() <-chan contract.Event { return s.ch }

// Dropped 是**累计**被丢弃的事件数（缓冲满时丢最旧 / 塞不进）。
//
// 它必须能被订阅者看到：丢包是无背压的必然代价，**不可见**的丢包会让页面把
// 「漏了」误读成「没发生」—— 那比丢包本身更危险。
func (s *Subscription) Dropped() uint64 { return s.dropped.Load() }

// Buffered 是当前排队待投递的条数（越大说明这个订阅者越慢）。
func (s *Subscription) Buffered() int { return len(s.ch) }

// Capacity 是本订阅者的队列容量。
func (s *Subscription) Capacity() int { return s.capacity }

// Close 注销订阅者并关闭队列。可重复调用。
//
// **关闭必须在写锁内**：`Publish` 持读锁投递，写锁与读锁互斥 ⇒ 关闭的那一刻不可能有
// 正在进行的投递，因此「向已关闭的 channel 发送」这个 panic 在**结构上**不可能发生。
// 这不是防御性编程，它就是本设计的正确性条件之一。
func (s *Subscription) Close() {
	s.once.Do(func() {
		s.hub.mu.Lock()
		defer s.hub.mu.Unlock()
		delete(s.hub.subs, s)
		close(s.ch)
	})
}

// offer 把一条事件放进队列。**永不阻塞**（`ADR-0027` 的「无背压」）：
//
//	队列没满   → 直接入队
//	队列满了   → 丢**最旧**一条并记账，再塞新的；仍塞不进（消费者正在读）再记一笔
//
// 因此调用方（上报路径）**不可能**被订阅者拖慢 —— 这是本设计最要紧的一条性质。
func (s *Subscription) offer(ev contract.Event) {
	select {
	case s.ch <- ev:
		return
	default:
	}
	select {
	case <-s.ch:
		s.dropped.Add(1) // 丢最旧
	default:
	}
	select {
	case s.ch <- ev:
	default:
		s.dropped.Add(1) // 连最旧都丢不掉（消费者正在读）⇒ 新来的这条也进不去
	}
}

// Hub 把**已写入**的事件广播给所有订阅者。
//
// 三条不可破的纪律（`ADR-0027`）：
//
//	① **不阻塞写入路径** —— 投递全在 `select/default` 分支里，永不等待订阅者；
//	② **无背压** —— 缓冲满就丢最旧并计数，不反压到上报方；
//	③ **零订阅者零成本** —— 没有订阅者时 Publish 只取一次读锁。
//
// 它**不是**事件存储：不落盘、不重放、进程重启即清空（历史在 `store`，见 `MD-20`）。
type Hub struct {
	mu   sync.RWMutex
	subs map[*Subscription]struct{}
}

// NewHub 构造广播中心。
func NewHub() *Hub { return &Hub{subs: map[*Subscription]struct{}{}} }

// Subscribe 新增一个订阅者。capacity ≤ 0 用默认值，超上限截断（不报错：容量是保护值，不是契约）。
//
// 调用方**必须** Close（否则订阅者会累积到进程退出）。
func (h *Hub) Subscribe(capacity int) *Subscription {
	switch {
	case capacity <= 0:
		capacity = DefaultBufferCapacity
	case capacity > MaxBufferCapacity:
		capacity = MaxBufferCapacity
	}
	s := &Subscription{ch: make(chan contract.Event, capacity), capacity: capacity, hub: h}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

// Publish 把（已写入的）事件投给所有订阅者。
//
// **不返回错误**：投递失败不成事件 —— 上报方不应该因为「有人在看」而承担失败（`NI-1`）。
func (h *Hub) Publish(evs []contract.Event) {
	if len(evs) == 0 {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs {
		for _, ev := range evs {
			s.offer(ev)
		}
	}
}

// Subscribers 是当前订阅者数（进日志与自检用）。
func (h *Hub) Subscribers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}
