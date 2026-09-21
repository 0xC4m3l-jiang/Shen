package honeypot

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── 替身（MD-22：本模块的测试不依赖核心、不依赖真实存储）─────────────────────

// recordingSession 把一次会话的录制内容收在内存里，供断言。
//
// 必须带锁：`Serve` 跑在服务端 goroutine 里、断言跑在测试 goroutine 里 ——
// 不加锁就是数据竞争（`-race` 会抓）。
type recordingSession struct {
	mu          sync.Mutex
	id          string
	remote      string
	transcripts []string // "in:…" / "out:…"
	creds       []string
	files       []string
}

func (s *recordingSession) ID() string { return s.id }

func (s *recordingSession) Transcript(d Direction, line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transcripts = append(s.transcripts, fmt.Sprintf("%s:%s", d, line))
}

func (s *recordingSession) Credential(user, secret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creds = append(s.creds, user+"/"+secret)
}

func (s *recordingSession) File(name string, size int, uploaded bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files = append(s.files, fmt.Sprintf("%s:%d:%v", name, size, uploaded))
}

// recorded 返回转录快照（加锁读）。
func (s *recordingSession) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.transcripts...)
}

// recordingFactory 造 recordingSession，并按会话 ID 留档。
type recordingFactory struct {
	mu       sync.Mutex
	sessions []*recordingSession
}

func (f *recordingFactory) NewSession(protocol string, remote net.Addr) Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := &recordingSession{id: fmt.Sprintf("%s-%d", protocol, len(f.sessions)+1), remote: remote.String()}
	f.sessions = append(f.sessions, s)
	return s
}

func (f *recordingFactory) all() []*recordingSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*recordingSession(nil), f.sessions...)
}

// holdProtocol 是一个「占住连接不放」的协议：Serve 阻塞到 ctx 取消 —— 用它测并发上限。
type holdProtocol struct{ name string }

func (h holdProtocol) Name() string     { return h.name }
func (h holdProtocol) DefaultPort() int { return 1 }

func (h holdProtocol) Serve(ctx context.Context, conn net.Conn, _ Session) error {
	<-ctx.Done()
	return ctx.Err()
}

// ── ① 注册表（MD-16 的前提：名字唯一，否则「谁生效」取决于注册顺序）──────────

func TestRegistryRejectsNilEmptyAndDuplicate(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(nil); err == nil {
		t.Error("注册 nil 适配器必须报错")
	}
	if err := reg.Register(Banner{ProtocolName: ""}); err == nil {
		t.Error("空协议名必须报错")
	}
	if err := reg.Register(Banner{ProtocolName: "ssh"}); err != nil {
		t.Fatalf("首次注册应当成功：%v", err)
	}
	err := reg.Register(Banner{ProtocolName: "ssh"})
	var dup *DuplicateError
	if !errors.As(err, &dup) {
		t.Fatalf("重复注册应当返回 DuplicateError，实际 %v", err)
	}
	if dup.Name != "ssh" {
		t.Errorf("错误里必须带协议名，实际 %q", dup.Name)
	}
}

func TestRegistryLookupAndNames(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(Banner{ProtocolName: "ssh"})
	_ = reg.Register(Banner{ProtocolName: "redis"})

	if _, ok := reg.Lookup("ssh"); !ok {
		t.Error("ssh 应当可查")
	}
	if _, ok := reg.Lookup("mysql"); ok {
		t.Error("未注册的协议不该可查")
	}
	if got := len(reg.Names()); got != 2 {
		t.Errorf("应当有 2 个协议，实际 %d", got)
	}
}

// ── ② Banner：最小但真实的适配器（框架怎么用，由它示范）──────────────────────

func TestBannerServesAndRecordsBothDirections(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Banner{
		ProtocolName: "ssh",
		Lines:        []string{"SSH-2.0-OpenSSH_9.6"},
		MaxLines:     2,
	}); err != nil {
		t.Fatal(err)
	}
	factory := &recordingFactory{}
	runner := NewRunner(reg, factory, Limits{})
	if err := runner.Start(context.Background(), "ssh", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Stop() })

	conn := dialRunner(t, runner, "ssh")
	defer func() { _ = conn.Close() }()

	banner, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("应当先收到问候：%v", err)
	}
	if !strings.Contains(banner, "SSH-2.0-OpenSSH") {
		t.Errorf("问候内容不对：%q", banner)
	}
	if _, err := fmt.Fprintf(conn, "USER root\n"); err != nil {
		t.Fatal(err)
	}

	// 等对端读到我们写的那一行并录制（读循环在服务端）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sessions := factory.all()
		if len(sessions) == 1 && len(sessions[0].recorded()) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	sessions := factory.all()
	if len(sessions) != 1 {
		t.Fatalf("应当有 1 次会话，实际 %d", len(sessions))
	}
	got := strings.Join(sessions[0].recorded(), "|")
	if !strings.Contains(got, "out:SSH-2.0-OpenSSH_9.6") {
		t.Errorf("发出去的内容必须被录制：%s", got)
	}
	if !strings.Contains(got, "in:USER root") {
		t.Errorf("对手写进来的内容必须被录制：%s", got)
	}
	if sessions[0].ID() == "" {
		t.Error("会话 ID 是一等字段（AR-25），不能为空")
	}
}

// ── ③ 运行框架：并发上限（MD-16）· 对称回收（MD-15）· 未知协议 ────────────────

func TestRunnerRejectsConnectionsOverLimit(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(holdProtocol{name: "mysql"})
	runner := NewRunner(reg, &recordingFactory{}, Limits{MaxConnsPerProtocol: 1, GracePeriod: 100 * time.Millisecond})
	if err := runner.Start(context.Background(), "mysql", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runner.Stop() }()

	first := dialRunner(t, runner, "mysql")
	defer func() { _ = first.Close() }()

	// 等第一条连接真的进入 Serve（active=1），否则第二条可能先到。
	waitFor(t, func() bool { return runner.Stats("mysql").Active == 1 })

	second := dialRunner(t, runner, "mysql")
	defer func() { _ = second.Close() }()
	// 超限连接必须被立即关闭：读会拿到 EOF。
	_ = second.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := second.Read(make([]byte, 1)); err == nil {
		t.Error("超限连接必须被拒绝（MD-16：超限即拒绝并记录）")
	}

	st := runner.Stats("mysql")
	if st.Rejected != 1 {
		t.Errorf("超限必须被计数：Rejected=%d", st.Rejected)
	}
	if st.Accepted < 2 {
		t.Errorf("接受总数应当包含被拒的那条：Accepted=%d", st.Accepted)
	}
}

func TestRunnerUnknownProtocolAndDoubleStart(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(holdProtocol{name: "redis"})
	runner := NewRunner(reg, &recordingFactory{}, Limits{})
	defer func() { _ = runner.Stop() }()

	err := runner.Start(context.Background(), "ftp", "127.0.0.1:0")
	var unknown *UnknownProtocolError
	if !errors.As(err, &unknown) {
		t.Fatalf("未注册协议应当返回 UnknownProtocolError，实际 %v", err)
	}

	if err := runner.Start(context.Background(), "redis", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(context.Background(), "redis", "127.0.0.1:0"); err == nil {
		t.Error("同一协议重复启动必须报错（否则会有两个监听抢同一协议）")
	}
}

func TestRunnerStopIsSymmetricAndClosesListener(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(holdProtocol{name: "ssh"})
	runner := NewRunner(reg, &recordingFactory{}, Limits{GracePeriod: 50 * time.Millisecond})
	if err := runner.Start(context.Background(), "ssh", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	addr, _ := runner.Addr("ssh")
	conn := dialRunner(t, runner, "ssh")

	// Stop 必须在宽限期内返回：在途连接会被强制关闭（MD-15 的对称回收）。
	done := make(chan error, 1)
	go func() { done <- runner.Stop() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop 不该报错：%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop 超时未返回 —— 资源回收不对称")
	}

	_ = conn.Close()
	if _, err := net.DialTimeout("tcp", addr.String(), 200*time.Millisecond); err == nil {
		t.Error("Stop 之后监听必须已关闭")
	}
	if _, ok := runner.Addr("ssh"); ok {
		t.Error("Stop 之后不该再报告监听地址")
	}
}

// ── ④ 未实现错误（故意失败，不静默返回空结果）───────────────────────────────

func TestNotImplementedIsExplicit(t *testing.T) {
	err := NotImplemented("SSH 密钥交换")
	if err == nil {
		t.Fatal("NotImplemented 必须返回错误")
	}
	if !strings.Contains(err.Error(), "尚未实现") {
		t.Errorf("错误信息必须说明「尚未实现」，实际：%v", err)
	}
	if errors.Is(err, io.EOF) {
		t.Error("不该与任何既有错误混淆")
	}
}

// ── 测试辅助 ────────────────────────────────────────────────────────────────

func dialRunner(t *testing.T, r *Runner, protocol string) net.Conn {
	t.Helper()
	addr, ok := r.Addr(protocol)
	if !ok {
		t.Fatalf("协议 %s 未在运行", protocol)
	}
	conn, err := net.DialTimeout("tcp", addr.String(), time.Second)
	if err != nil {
		t.Fatalf("拨号失败：%v", err)
	}
	return conn
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待条件超时")
}
