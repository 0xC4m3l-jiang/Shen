package telemetry

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"shen/core/internal/contract"
)

// 这一组测试钉的是 `ADR-0027` 的三条纪律 —— 它们**不是**性能优化，是设计前提：
//
//	① 不阻塞写入路径（订阅者拖不死上报）
//	② 无背压（缓冲满就丢最旧，且丢包可计数）
//	③ 零订阅者零成本
//
// 以及一条容易被忽略的契约细节：**幂等命中不重复推送**。

func evOf(id string) contract.Event {
	return contract.Event{EventID: id, Type: "decision", CreatedAt: time.Unix(0, 0)}
}

// 纪律①+②：订阅者**完全不读**，发布量远超容量 —— Publish 必须立即返回，且丢包被记账。
//
// 这是最关键的一条：核心的上报路径（适配器 → 核心）绝不能被「有人在看」拖慢。
func TestHub_PublishNeverBlocksAndDropsOldest(t *testing.T) {
	h := NewHub()
	sub := h.Subscribe(2) // 容量刻意开小
	defer sub.Close()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			h.Publish([]contract.Event{evOf(fmt.Sprintf("e-%d", i))})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish 阻塞了 —— 违反「不阻塞写入路径」（ADR-0027 纪律①）")
	}
	if sub.Dropped() == 0 {
		t.Fatal("缓冲溢出必须记账 —— 不可见的丢包会让页面把「漏了」读成「没发生」（纪律②）")
	}
	if got := sub.Buffered(); got != 2 {
		t.Fatalf("容量 2 应留住 2 条，实际 %d", got)
	}
	// 留住的必须是**最后两条**（丢的是最旧，不是最新）。
	var kept []string
	for i := 0; i < 2; i++ {
		select {
		case ev := <-sub.Events():
			kept = append(kept, ev.EventID)
		case <-time.After(time.Second):
			t.Fatal("队列里应有 2 条")
		}
	}
	if kept[0] != "e-998" || kept[1] != "e-999" {
		t.Errorf("应留最后两条 [e-998 e-999]，实际 %v", kept)
	}
}

// 纪律③：没有订阅者时 Publish 是空操作（不 panic、不分配、不阻塞）。
func TestHub_PublishWithoutSubscribersIsNoop(t *testing.T) {
	h := NewHub()
	h.Publish([]contract.Event{evOf("x")})
	if h.Subscribers() != 0 {
		t.Fatalf("不应有订阅者，实际 %d", h.Subscribers())
	}
	h.Publish(nil) // 空切片同样不得 panic
}

// 订阅/关闭与并发 Publish 交叉 —— **必须在 `-race` 下跑**。
//
// 它钉的是一条正确性条件：`Close` 在写锁内关 channel，而 Publish 持读锁投递，
// 两者互斥 ⇒ 「向已关闭的 channel 发送」在结构上不可能发生（否则这里会 panic）。
func TestHub_ConcurrentPublishAndClose(t *testing.T) {
	h := NewHub()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					h.Publish([]contract.Event{evOf("c")})
				}
			}
		}()
	}
	for i := 0; i < 100; i++ {
		sub := h.Subscribe(4)
		sub.Close()
		sub.Close() // 重复关闭必须是安全的
	}
	close(stop)
	wg.Wait()
	if h.Subscribers() != 0 {
		t.Fatalf("全部关闭后不应有订阅者，实际 %d", h.Subscribers())
	}
}

// 契约细节：**幂等命中不重复推送**。
//
// 消费者只该看到「真正落库」的事件 —— 否则控制台会把同一次判定画两遍。
func TestCollector_DoesNotPublishDuplicates(t *testing.T) {
	h := NewHub()
	sub := h.Subscribe(4)
	defer sub.Close()
	c := New(newStubSink(), h)

	if _, err := c.ReportBatch(context.Background(), []contract.Event{evOf("d-1"), evOf("d-1")}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-sub.Events():
		if got.EventID != "d-1" {
			t.Errorf("应收到 d-1，实际 %s", got.EventID)
		}
	case <-time.After(time.Second):
		t.Fatal("落库成功的事件应被推送")
	}
	select {
	case second := <-sub.Events():
		t.Fatalf("幂等命中不得重复推送，却收到了 %s", second.EventID)
	case <-time.After(50 * time.Millisecond):
	}
}

// 未装配推送（pub 为 nil）时，上报必须照常工作 —— 推送是**旁路**，不是前置条件。
func TestCollector_ReportsWithoutPublisher(t *testing.T) {
	c := New(newStubSink(), nil)
	res, err := c.Report(context.Background(), evOf("n-1"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Accepted != 1 {
		t.Fatalf("无推送时上报应照常：%+v", res)
	}
}
