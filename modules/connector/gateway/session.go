package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/yamux"
)

// 会话查找错误（桥接时回给 proxy 的 502 原因）。
var (
	ErrNoSession  = errors.New("该域名当前没有在线的连接器会话")
	ErrHostUnrout = errors.New("该域名不属于任何已接入的连接器")
)

// Session 是一条连接器隧道会话：一个连接器进程的一条 yamux 连接。
type Session struct {
	ID           string // sess-<gen>-<rand>：gen 即代际号，重连竞态的消歧键
	CredentialID string
	Name         string
	Hosts        []string // 已规范化的声明域名（凭证白名单子集）
	LocalAddr    string
	RemoteIP     string
	ConnectorVer string
	OpenedAt     time.Time
	sess         *yamux.Session
	lastSeen     atomic.Int64 // unix ms；keepalive/活动时刷新
	rttMs        atomic.Int64 // 最近一次 Ping RTT
	plannedClose atomic.Bool  // 连接器发送过 closing 事件
}

func (s *Session) LastSeen() time.Time { return time.UnixMilli(s.lastSeen.Load()) }
func (s *Session) RttMs() int64        { return s.rttMs.Load() }
func (s *Session) PlannedClose() bool  { return s.plannedClose.Load() }

func (s *Session) touch() { s.lastSeen.Store(time.Now().UnixMilli()) }

// Pool 是会话池：Host → 会话的路由表 + 按代际号消歧的注册/注销。
//
// 并发模型：RWMutex；热路径（每请求 LookupHost）只拿读锁做 O(会话数×hosts) 匹配
// （会话数是个位数到几十，远小于一次 TLS 握手）；注册/注销拿写锁。
type Pool struct {
	mu      sync.RWMutex
	byID    map[string]*Session
	byHost  map[string]*Session
	gen     uint64
	nowFunc func() time.Time
}

// NewPool 创建会话池。
func NewPool() *Pool {
	return &Pool{byID: map[string]*Session{}, byHost: map[string]*Session{}, nowFunc: time.Now}
}

// Register 登记会话（Host 路由指向它）并返回被顶替的旧会话（同一凭证重复拨入：
// 新连接取代旧连接，旧的由调用方关闭——防同一个服务的两条隧道分流）。
func (p *Pool) Register(s *Session) (superseded []*Session) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gen++
	if s.ID == "" {
		buf := make([]byte, 6)
		_, _ = rand.Read(buf)
		s.ID = fmt.Sprintf("sess-%d-%s", p.gen, hex.EncodeToString(buf))
	}
	s.lastSeen.Store(s.OpenedAt.UnixMilli())
	// 同凭证的旧会话：顶替并从路由摘除（保留在返回值里由 serve 层关闭）。
	for _, old := range p.byID {
		if old.CredentialID == s.CredentialID && old.ID != s.ID {
			p.unmapLocked(old)
			superseded = append(superseded, old)
		}
	}
	p.byID[s.ID] = s
	for _, h := range s.Hosts {
		p.byHost[h] = s
	}
	return superseded
}

// Unregister 摘除会话。**身份校验**：只有路由表里仍是这个会话实例时才摘——
// 重连竞态（旧连接的延迟 Unregister 晚于新连接的 Register）不会误删新会话，
// 会话 ID 中的代际号让日志可对账。
func (p *Pool) Unregister(s *Session) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unmapLocked(s)
}

func (p *Pool) unmapLocked(s *Session) {
	if cur, ok := p.byID[s.ID]; ok && cur == s {
		delete(p.byID, s.ID)
	}
	for h, cur := range p.byHost {
		if cur == s {
			delete(p.byHost, h)
		}
	}
}

// LookupHost 按请求 Host 找会话：精确匹配优先，其次最长的通配后缀。
// 命中的会话刷新活跃时间（桥接即活动，等价于心跳）。
func (p *Pool) LookupHost(rawHost string) (*Session, error) {
	h := NormalizeHost(rawHost)
	if h == "" {
		return nil, ErrHostUnrout
	}
	p.mu.RLock()
	var best *Session
	var bestLen int
	if s, ok := p.byHost[h]; ok {
		best = s
	} else {
		for pattern, s := range p.byHost {
			if HostMatches(pattern, h) && len(pattern) > bestLen {
				best, bestLen = s, len(pattern)
			}
		}
	}
	p.mu.RUnlock()
	if best == nil {
		return nil, ErrNoSession
	}
	best.touch()
	return best, nil
}

// ExpireStale 关闭超过 ttl 没有任何活动且 keepalive 已死的会话，返回被摘除的会话。
// yamux 的 keepalive 失败会主动关连接（Accept 报错走 Unregister），这里是兜底：
// 防止「TCP 半死连接」既不报错也不流量的极端情况占着路由。
func (p *Pool) ExpireStale(ttl time.Duration) []*Session {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	var expired []*Session
	for _, s := range p.byID {
		if now.Sub(s.LastSeen()) > ttl {
			p.unmapLocked(s)
			expired = append(expired, s)
		}
	}
	return expired
}

// List 返回全部会话快照（观测上报用）。
func (p *Pool) List() []*Session {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*Session, 0, len(p.byID))
	for _, s := range p.byID {
		out = append(out, s)
	}
	return out
}

// OpenStream 为一条 proxy 连接打开一条数据流（桥接的取流动作）。
func (s *Session) OpenStream() (*yamux.Stream, error) {
	s.touch()
	return s.sess.OpenStream()
}

// Ping 测一次 RTT（观测上报用）。
func (s *Session) Ping() (time.Duration, error) {
	d, err := s.sess.Ping()
	if err == nil {
		s.rttMs.Store(d.Milliseconds())
		s.touch()
	}
	return d, err
}

// Close 关闭隧道（幂等）。
func (s *Session) Close() error { return s.sess.Close() }
