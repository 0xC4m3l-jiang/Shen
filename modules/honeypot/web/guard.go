package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 本文件是**合成交互的治理与出口**：登录配额 · 事件投递 · 身份脱敏 · 客户端识别。
//
// 三者都属于「诱饵后端自身也要有纪律」这一块：它不是业务系统，但仍然联网、仍然接收输入 ——
// 无限尝试、同步阻塞的上游、未经脱敏的会话标识，都会从这里变成可被对手利用或观察的信号。

// ── 登录配额（W6）────────────────────────────────────────────────────────────

// defaultLoginBurst 是单个来源在一个窗口内允许的登录尝试次数（默认 10 次/分钟）。
//
// 为什么要有上限（而不只是"合成登录不校验凭据所以无所谓"）：
// ① 资源：每次尝试都会创建合成会话（表是有容量上限的）；
// ② 破绽：一个**永不拒绝、永不节流**的登录台本身就是异常 —— 真实产品一定有速率限制。
const defaultLoginBurst = 10

// defaultLoginWindow 是配额窗口（默认 1 分钟）。
const defaultLoginWindow = time.Minute

// loginLimiter 是按来源 IP 的令牌桶（每个来源独立，满了才拒绝）。
//
// 它是**可丢失**的进程内状态：重启即重置（诱饵后端不落盘任何东西）。
type loginLimiter struct {
	mu     sync.Mutex
	seen   map[string]*loginBucket
	burst  float64
	window time.Duration
	cap    int
}

type loginBucket struct {
	tokens float64
	last   time.Time
}

// maxLoginSources 是配额表的容量上限（大量不同来源不能把它撑成无界增长）。
const maxLoginSources = 8192

func newLoginLimiter(burst int, window time.Duration) *loginLimiter {
	if burst <= 0 {
		burst = defaultLoginBurst
	}
	if window <= 0 {
		window = defaultLoginWindow
	}
	return &loginLimiter{
		seen:   map[string]*loginBucket{},
		burst:  float64(burst),
		window: window,
		cap:    maxLoginSources,
	}
}

// allow 消耗一个令牌；返回 false 表示这一来源在当前窗口内已经超配额。
func (l *loginLimiter) allow(source string, now time.Time) bool {
	if l == nil {
		return true
	}
	if source == "" {
		source = "unknown"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.seen) >= l.cap {
		// 表满：清掉已经回满的桶（它们与"没见过"等价），仍满则整体重置 ——
		// 配额是可丢失的护栏，不清空就失去护栏作用（攻击者可用海量来源把表撑满）。
		for k, b := range l.seen {
			if l.tokensAt(b, now) >= l.burst {
				delete(l.seen, k)
			}
		}
		if len(l.seen) >= l.cap {
			l.seen = map[string]*loginBucket{}
		}
	}
	b, ok := l.seen[source]
	if !ok {
		b = &loginBucket{tokens: l.burst, last: now}
		l.seen[source] = b
	}
	refilled := l.tokensAt(b, now)
	b.last = now
	if refilled < 1 {
		b.tokens = refilled
		return false
	}
	b.tokens = refilled - 1
	return true
}

// tokensAt 计算某桶在 now 时刻的令牌数（按时间线性回满，上限 burst）。
func (l *loginLimiter) tokensAt(b *loginBucket, now time.Time) float64 {
	if b.last.IsZero() {
		return l.burst
	}
	elapsed := now.Sub(b.last)
	if elapsed <= 0 {
		return b.tokens
	}
	return min(l.burst, b.tokens+float64(elapsed)/float64(l.window)*l.burst)
}

// ── 事件出口（W5）───────────────────────────────────────────────────────────

// defaultEventQueue 是事件队列深度（满了就计数丢弃 —— 诱饵交互不能因为上游慢而变慢）。
const defaultEventQueue = 256

// eventTimeout 是单次投递的超时：慢接收方不能拖住投递 goroutine，更不能拖住请求。
const eventTimeout = 2 * time.Second

// asyncEmitter 把合成交互事件**异步、有界**地投给装配层给的 EventSink。
//
// 三条性质（`W5` 的验收）：
//
//	① 请求路径只做一次非阻塞入队 ⇒ 慢 sink 不影响响应时间（原来的实现是同步调 sink）；
//	② 队列满 ⇒ **计数丢弃**（`Dropped`），不阻塞、不 panic、不静默（丢多少必须能读出来）；
//	③ 投递失败 ⇒ 计数（`Failures`）并配一行日志，不重试（诱饵事件的可靠投递不是本项目的目标，
//	   但"丢了多少"必须是事实）。
type asyncEmitter struct {
	sink    EventSink
	ch      chan Event
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once

	dropped  atomic.Uint64
	failures atomic.Uint64
}

func newAsyncEmitter(sink EventSink, queue int) *asyncEmitter {
	if queue <= 0 {
		queue = defaultEventQueue
	}
	e := &asyncEmitter{
		sink:    sink,
		ch:      make(chan Event, queue),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go e.run()
	return e
}

// emit 非阻塞入队；队列满就丢弃并计数（绝不阻塞请求）。
func (e *asyncEmitter) emit(ev Event) {
	if e == nil || e.sink == nil {
		return
	}
	select {
	case e.ch <- ev:
	default:
		e.dropped.Add(1)
	}
}

func (e *asyncEmitter) run() {
	defer close(e.stopped)
	for {
		select {
		case <-e.done:
			return
		case ev := <-e.ch:
			ctx, cancel := context.WithTimeout(context.Background(), eventTimeout)
			func() {
				defer func() {
					// sink 是我们自己的装配层，但"自己的代码不会 panic"不是可以赌的事：
					// 一条事件出口炸掉整个诱饵后端，代价远大于少一条事件。
					if r := recover(); r != nil {
						e.failures.Add(1)
					}
				}()
				e.sink.Event(ctx, ev)
			}()
			cancel()
		}
	}
}

// Close 停止投递（幂等）。已入队的事件不保证送达 —— 进程退出路径上不值得等。
func (e *asyncEmitter) Close() {
	if e == nil {
		return
	}
	e.once.Do(func() {
		close(e.done)
		<-e.stopped
	})
}

// Dropped 返回因队列满而丢弃的事件数（必须可观测）。
func (e *asyncEmitter) Dropped() uint64 {
	if e == nil {
		return 0
	}
	return e.dropped.Load()
}

// Failures 返回投递失败（含 sink panic）的次数。
func (e *asyncEmitter) Failures() uint64 {
	if e == nil {
		return 0
	}
	return e.failures.Load()
}

// ── 身份脱敏与客户端识别 ────────────────────────────────────────────────────

// scrubSession 把合成会话 id 变成**可关联但不可复用**的短标识。
//
// 为什么不能直接写会话 id：它是浏览器里的 cookie 值 —— 等于把一份凭据抄进事件/日志
// （`W5` 要求"脱敏关联"）。哈希前缀保留了"同一次会话的事件能对上"这个唯一用途。
func scrubSession(id string) string {
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:4]) // 8 hex 字符：够区分，不足以还原
}

// clientIPOf 取请求来源 IP。
//
// 优先级：`X-Forwarded-For` 的第一段 → `RemoteAddr`。为什么敢信 XFF：本后端只经由**我们自己的**
// 前跳（引擎/接入层）到达，不直接对外（`SHEN_WEB_LISTEN` 默认回环，验证档只在共享网络命名空间内）；
// 不取 XFF 的话，所有请求的 `RemoteAddr` 都是前跳地址 ⇒ 配额会把全部访问者算成同一个人。
func clientIPOf(r *http.Request) string {
	if r == nil {
		return ""
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first := strings.TrimSpace(strings.Split(xff, ",")[0])
		if first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}
