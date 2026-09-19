package honeypot

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Runner 是协议适配器的**运行框架**：为一个协议启动监听、限制并发、对称回收。
//
// 它只依赖 `Registry` 与 `SessionFactory` 两个契约，不 import 任何核心内部包（`ST-3` / `ST-4`）——
// 事件怎么上行、会话怎么落库都由消费方提供的实现决定。
type Runner struct {
	reg     *Registry
	factory SessionFactory
	limits  Limits

	mu       sync.Mutex
	servers  map[string]*server
	wg       sync.WaitGroup
	stopping bool
}

// server 是一个协议的运行态。
type server struct {
	protocol Protocol
	listener net.Listener
	cancel   context.CancelFunc

	active   atomic.Int64
	accepted atomic.Uint64
	rejected atomic.Uint64

	// conns 记录在途连接，供宽限到期后强制关闭。
	conns sync.Map
}

// NewRunner 构造运行框架。零值的 Limits 字段用默认值（并发上限必须存在，`MD-16`）。
func NewRunner(reg *Registry, factory SessionFactory, limits Limits) *Runner {
	if limits.MaxConnsPerProtocol <= 0 {
		limits.MaxConnsPerProtocol = DefaultMaxConns
	}
	if limits.GracePeriod <= 0 {
		limits.GracePeriod = DefaultGracePeriod
	}
	return &Runner{
		reg:     reg,
		factory: factory,
		limits:  limits,
		servers: map[string]*server{},
	}
}

// Start 为一个**已注册**的协议启动监听。
//
// addr 形如 `127.0.0.1:2222` 或 `:2222`；传 `127.0.0.1:0` 可由内核分配端口，
// 之后用 Addr 取实际地址（单测用得上）。
func (r *Runner) Start(ctx context.Context, protocol, addr string) error {
	p, ok := r.reg.Lookup(protocol)
	if !ok {
		return &UnknownProtocolError{Name: protocol}
	}
	if r.factory == nil {
		return fmt.Errorf("honeypot: SessionFactory 不能为 nil（会话录制口由消费方提供）")
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("honeypot: 协议 %s 监听 %s 失败：%w", protocol, addr, err)
	}

	cctx, cancel := context.WithCancel(ctx)
	srv := &server{protocol: p, listener: ln, cancel: cancel}

	r.mu.Lock()
	if r.stopping {
		r.mu.Unlock()
		cancel()
		_ = ln.Close()
		return fmt.Errorf("honeypot: Runner 正在停止，不再接受新的监听")
	}
	if _, dup := r.servers[protocol]; dup {
		r.mu.Unlock()
		cancel()
		_ = ln.Close()
		return &DuplicateError{Name: protocol}
	}
	r.servers[protocol] = srv
	r.mu.Unlock()

	go srv.acceptLoop(cctx, r)
	return nil
}

// Addr 返回某协议的实际监听地址（未运行 → false）。
func (r *Runner) Addr(protocol string) (net.Addr, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.servers[protocol]
	if !ok {
		return nil, false
	}
	return s.listener.Addr(), true
}

// Stats 返回某协议的运行计数（未运行 → 零值）。
func (r *Runner) Stats(protocol string) Stats {
	r.mu.Lock()
	s, ok := r.servers[protocol]
	r.mu.Unlock()
	if !ok {
		return Stats{}
	}
	return Stats{
		Active:   int(s.active.Load()),
		Accepted: s.accepted.Load(),
		Rejected: s.rejected.Load(),
	}
}

// acceptLoop 收连接：先查并发上限，超限即拒并记录（`MD-16`）。
func (s *server) acceptLoop(ctx context.Context, r *Runner) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // 监听被关闭（Stop 或 ctx 取消）——正常退出路径
		}
		if ctx.Err() != nil {
			_ = conn.Close()
			return
		}
		s.accepted.Add(1)

		if int(s.active.Load()) >= r.limits.MaxConnsPerProtocol {
			// MD-16：并发必须有上限，超限即拒绝并记录 —— 否则蜜罐本身成了对手的资源耗尽入口。
			s.rejected.Add(1)
			_ = conn.Close()
			continue
		}

		s.active.Add(1)
		s.conns.Store(conn, struct{}{})
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			defer s.active.Add(-1)
			defer s.conns.Delete(conn)
			defer func() { _ = conn.Close() }()

			sess := r.factory.NewSession(s.protocol.Name(), conn.RemoteAddr())
			if err := s.protocol.Serve(ctx, conn, sess); err != nil && ctx.Err() == nil {
				// 会话级错误只记日志：一个坏会话不该拖垮整个监听。
				log.Printf("honeypot: 协议 %s 会话 %s 结束：%v", s.protocol.Name(), sess.ID(), err)
			}
		}()
	}
}

// Stop 对称回收（`MD-15`）：停止收新连接 → 等在途连接收尾（宽限期）→ 强制关闭 → 等 goroutine 退出。
//
// 关于「进程组回收」（`MD-14`）：那条规则适用于**子进程形态**的蜜罐（外部可执行文件）。
// 本框架跑的是进程内适配器，能做的上限就是「关连接 + 等」；若某个 Serve 忽略 ctx 长时间不返回，
// 框架无法更狠地终止它 —— 这一点是刻意的，写在文档里，不假装做到了。
func (r *Runner) Stop() error {
	r.mu.Lock()
	r.stopping = true
	servers := make([]*server, 0, len(r.servers))
	for _, s := range r.servers {
		servers = append(servers, s)
	}
	r.servers = map[string]*server{}
	r.mu.Unlock()

	for _, s := range servers {
		s.cancel()
		_ = s.listener.Close()
	}

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(r.limits.GracePeriod):
		for _, s := range servers {
			s.conns.Range(func(k, _ any) bool {
				if c, ok := k.(net.Conn); ok {
					_ = c.Close()
				}
				return true
			})
		}
		<-done
	}
	return nil
}

// UnknownProtocolError 表示请求启动一个没注册过的协议。
type UnknownProtocolError struct {
	Name string
}

func (e *UnknownProtocolError) Error() string {
	return fmt.Sprintf("honeypot: 协议 %q 未注册（先 Register 适配器）", e.Name)
}
