package gateway

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"time"

	connectorv1 "shen/common/api/connector/v1"
	"shen/modules/connector/wire"

	"github.com/hashicorp/yamux"
)

// 握手与流 0 的参数。
const (
	handshakeTimeout = 10 * time.Second // 流 0 首条消息的读限
	pingInterval     = 15 * time.Second // RTT 探测（兼作活跃心跳）
	sessionTTL       = 60 * time.Second // 无任何活动的兜底过期
	reportInterval   = 20 * time.Second // 会话心跳上报（控制台 TTL 60s 的三倍频）
)

// Server 是网关：TLS 接入 + 会话池 + 控制台客户端。
type Server struct {
	cfg     Config
	keys    *KeyTable
	pool    *Pool
	console *ConsoleClient
	logf    func(string, ...any)
}

// Config 是网关运行参数。
type Config struct {
	Listen           string // 连接器拨入（TLS）
	TLSCert          tls.Certificate
	Bridge           string        // 供 proxy 取流的回环字节桥
	ConsoleURL       string        // 控制台地址
	IntegrationToken string        // 控制台集成令牌
	GatewayNode      string        // 本网关节点标识（观测展示）
	KeyRefresh       time.Duration // key 表拉取周期
	Now              func() time.Time
	Logf             func(format string, args ...any)
}

// New 创建网关。
func New(cfg Config) *Server {
	if cfg.KeyRefresh <= 0 {
		cfg.KeyRefresh = 30 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	return &Server{cfg: cfg, keys: &KeyTable{}, pool: NewPool(),
		console: NewConsoleClient(cfg.ConsoleURL, cfg.IntegrationToken), logf: cfg.Logf}
}

// Pool / Keys 暴露内部状态（桥接与测试用）。
func (s *Server) Pool() *Pool             { return s.pool }
func (s *Server) Keys() *KeyTable         { return s.keys }
func (s *Server) Console() *ConsoleClient { return s.console }

// Run 启动全部循环直到 ctx 取消：key 表拉取、会话过期清扫、心跳上报、TLS 接入。
// 监听器由调用方创建并传入（测试用 net.Pipe / 回环监听替代真实 TLS）。
func (s *Server) Run(ctx context.Context, conns <-chan net.Conn) {
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); s.keyLoop(ctx) }()
	go func() { defer wg.Done(); s.expireLoop(ctx) }()
	go func() { defer wg.Done(); s.reportLoop(ctx) }()
	go func() {
		for conn := range conns {
			wg.Add(1)
			go func(c net.Conn) { defer wg.Done(); s.handleConnector(ctx, c) }(conn)
		}
	}()
	<-ctx.Done()
	wg.Wait()
}

// keyLoop 定期拉取凭证哈希表（吊销在这个周期内生效）。
func (s *Server) keyLoop(ctx context.Context) {
	t := time.NewTicker(s.cfg.KeyRefresh)
	defer t.Stop()
	pull := func() {
		keys, err := s.console.PullKeys(ctx)
		if err != nil {
			s.logf("gateway: 拉取 key 表失败（沿用旧表）：%v", err)
			return
		}
		s.keys.Replace(keys)
	}
	pull() // 启动即拉一次：连接器拨入前必须有表
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pull()
		}
	}
}

// expireLoop 会话过期兜底清扫（正常下线由 yamux Accept 报错路径处理）。
func (s *Server) expireLoop(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, sess := range s.pool.ExpireStale(sessionTTL) {
				s.logf("gateway: 会话 %s（%s）心跳超时摘除", sess.ID, sess.Name)
				s.closeSession(context.Background(), sess, false, "心跳超时")
			}
		}
	}
}

// reportLoop 周期上报在线会话心跳（控制台据此刷新 last_heartbeat 与 RTT）。
func (s *Server) reportLoop(ctx context.Context) {
	t := time.NewTicker(reportInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, sess := range s.pool.List() {
				_ = s.console.ReportSession(ctx, s.reportOf(sess, true, ""))
			}
		}
	}
}

// handleConnector 服务一条连接器 TLS 连接：yamux 会话 → 流 0 握手 → 生命周期。
func (s *Server) handleConnector(ctx context.Context, conn net.Conn) {
	remote := "unknown"
	if a := conn.RemoteAddr(); a != nil {
		remote = a.String()
	}
	defer func() { _ = conn.Close() }()

	mux, err := yamuxServer(conn)
	if err != nil {
		s.logf("gateway: 建立多路复用失败（%s）：%v", remote, err)
		return
	}
	// 流 0 = 控制信道：连接器先开流并送握手。
	ctrl, err := mux.AcceptStream()
	if err != nil {
		s.logf("gateway: 等待控制流失败（%s）：%v", remote, err)
		return
	}
	req, err := wire.ReadHandshakeRequest(ctrl, handshakeTimeout)
	if err != nil {
		s.logf("gateway: 握手读取失败（%s）：%v", remote, err)
		_ = wire.Write(ctrl, &connectorv1.HandshakeResponse{Ok: false, Error: "握手格式错误"}, 5*time.Second)
		return
	}
	meta := req.GetMeta()
	declared := normalizeDeclaredHosts(meta.GetHosts())
	key, vErr := s.keys.Validate(req.GetAuthKey(), meta.GetName(), declared)
	if vErr != nil {
		s.logf("gateway: 握手拒绝（%s）：%v", remote, vErr)
		_ = wire.Write(ctrl, &connectorv1.HandshakeResponse{Ok: false, Error: vErr.Error()}, 5*time.Second)
		return
	}

	sess := &Session{CredentialID: key.ID, Name: key.Name, Hosts: declared,
		LocalAddr: meta.GetLocalAddr(), RemoteIP: remote, ConnectorVer: meta.GetConnectorVersion(),
		OpenedAt: s.cfg.Now().UTC(), sess: mux}
	for _, old := range s.pool.Register(sess) { // 同凭证旧会话顶替
		s.logf("gateway: 会话 %s 被新连接顶替（%s）", old.ID, old.Name)
		s.closeSession(ctx, old, false, "被新连接顶替")
	}
	s.logf("gateway: 会话建立 %s（%s · hosts=%v · %s）", sess.ID, key.Name, declared, remote)

	if err := wire.Write(ctrl, &connectorv1.HandshakeResponse{Ok: true, SessionId: sess.ID, KeepaliveSeconds: uint32(pingInterval.Seconds())}, 5*time.Second); err != nil {
		s.pool.Unregister(sess)
		return
	}
	// 自动登记 + 上线事件（异步：不挡数据面）。
	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.console.ReportRegister(cctx, key.Name, declared, meta.GetLocalAddr()); err != nil {
			s.logf("gateway: 自动登记上报失败（%s）：%v", key.Name, err)
		}
		if err := s.console.ReportSession(cctx, s.reportOf(sess, true, "连接建立")); err != nil {
			s.logf("gateway: 会话上报失败（%s）：%v", sess.ID, err)
		}
	}()

	// 生命周期：ping 探活 + 控制流事件（closing 等）直到连接结束。
	pingStop := make(chan struct{})
	go func() {
		t := time.NewTicker(pingInterval)
		defer t.Stop()
		for {
			select {
			case <-pingStop:
				return
			case <-t.C:
				if _, err := sess.Ping(); err != nil {
					return // 连接已断：主循环的 Accept 也会报错
				}
			}
		}
	}()
	for {
		ev, err := wire.ReadEvent(ctrl, 0) // 无限等待：事件稀疏，连接断开时 ctrl 读报错
		if err != nil {
			close(pingStop)
			planned := sess.PlannedClose()
			s.pool.Unregister(sess) // 身份校验：不会误删重连后的新会话
			s.logf("gateway: 会话结束 %s（%s · planned=%v）", sess.ID, sess.Name, planned)
			cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			s.closeSession(cctx, sess, planned, disconnectDetail(planned))
			cancel()
			return
		}
		if ev.GetKind() == "closing" {
			sess.plannedClose.Store(true)
			s.logf("gateway: 会话 %s（%s）收到计划内下线通知", sess.ID, sess.Name)
		}
	}
}

func disconnectDetail(planned bool) string {
	if planned {
		return "计划内下线"
	}
	return "连接断开"
}

// closeSession 关闭会话并上报下线事件。
func (s *Server) closeSession(ctx context.Context, sess *Session, planned bool, detail string) {
	_ = sess.Close()
	_ = s.console.ReportSession(ctx, s.reportOf(sess, false, detail))
}

func (s *Server) reportOf(sess *Session, online bool, detail string) SessionReport {
	planned := sess.PlannedClose() && !online
	return SessionReport{SessionID: sess.ID, CredentialID: sess.CredentialID, Name: sess.Name,
		Hosts: sess.Hosts, LocalAddr: sess.LocalAddr, ConnectorIP: sess.RemoteIP,
		ConnectorVersion: sess.ConnectorVer, GatewayNode: s.cfg.GatewayNode,
		Online: online, PlannedClose: planned, RttMs: sess.RttMs()}
}

func normalizeDeclaredHosts(hosts []string) []string {
	out := make([]string, 0, len(hosts))
	seen := map[string]bool{}
	for _, h := range hosts {
		n := NormalizeHost(h)
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return SortedHosts(out)
}

// yamuxServer 用本网关的统一参数建会话（keepalive 由库内 ping 承担）。
func yamuxServer(conn net.Conn) (*yamux.Session, error) {
	cfg := yamux.DefaultConfig()
	cfg.KeepAliveInterval = pingInterval
	return yamux.Server(conn, cfg)
}
