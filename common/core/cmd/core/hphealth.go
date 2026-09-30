package main

import (
	"context"
	"log"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"shen/common/core/internal/contract"
)

// 蜜罐健康探测：只探测**已启用**的后端，TCP 拨号（不发任何应用层数据 —— 蜜罐不该因为探测产生噪声事件）。
// 结果写回后端池（Healthy），并随同步上报给管控台。只在状态**变化**时写日志。

const (
	probeTimeout     = time.Second
	probeInterval    = 15 * time.Second
	probeConcurrency = 8
)

// probeResult 是一次探测结果（与管控台 deception.HoneypotProbe 手工对齐）。
type probeResult struct {
	Name      string    `json:"name"`
	Addr      string    `json:"addr"`
	Healthy   bool      `json:"healthy"`
	LatencyMS int64     `json:"latency_ms"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// healthProber 周期探测；list 返回当前后端（热替换后自动跟上），mark 写回后端池。
type healthProber struct {
	list  func(context.Context) ([]contract.HoneypotBackend, error)
	mark  func(ctx context.Context, name string, healthy bool)
	dial  func(ctx context.Context, network, addr string) (net.Conn, error)
	mu    sync.RWMutex
	last  map[string]probeResult
	logf  func(string, ...any)
	clock func() time.Time
}

func newHealthProber(list func(context.Context) ([]contract.HoneypotBackend, error),
	mark func(context.Context, string, bool)) *healthProber {
	d := &net.Dialer{Timeout: probeTimeout}
	return &healthProber{list: list, mark: mark, dial: d.DialContext, last: map[string]probeResult{},
		logf: log.Printf, clock: time.Now}
}

// dialAddr 把登记地址（host:port 或 http(s)://host[:port]）转成可拨号的 host:port。
func dialAddr(addr string) string {
	a := strings.TrimSpace(addr)
	if !strings.Contains(a, "://") {
		return a
	}
	u, err := url.Parse(a)
	if err != nil || u.Host == "" {
		return a
	}
	if u.Port() != "" {
		return u.Host
	}
	if u.Scheme == "https" {
		return net.JoinHostPort(u.Hostname(), "443")
	}
	return net.JoinHostPort(u.Hostname(), "80")
}

// once 并发探测一轮（并发 ≤ probeConcurrency）。
func (p *healthProber) once(ctx context.Context) {
	backends, err := p.list(ctx)
	if err != nil {
		return
	}
	sem := make(chan struct{}, probeConcurrency)
	results := make(chan probeResult, len(backends))
	var wg sync.WaitGroup
	for _, b := range backends {
		if !b.Enabled {
			continue
		}
		wg.Add(1)
		go func(b contract.HoneypotBackend) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			start := p.clock()
			r := probeResult{Name: b.Name, Addr: b.Addr, CheckedAt: start.UTC()}
			cctx, cancel := context.WithTimeout(ctx, probeTimeout)
			conn, err := p.dial(cctx, "tcp", dialAddr(b.Addr))
			cancel()
			if err != nil {
				r.Error = err.Error()
			} else {
				_ = conn.Close()
				r.Healthy, r.LatencyMS = true, p.clock().Sub(start).Milliseconds()
			}
			results <- r
		}(b)
	}
	wg.Wait()
	close(results)
	next := map[string]probeResult{}
	for r := range results {
		next[r.Name] = r
	}
	p.mu.Lock()
	prev := p.last
	p.last = next
	p.mu.Unlock()
	for name, r := range next {
		if p.mark != nil {
			p.mark(ctx, name, r.Healthy)
		}
		if old, ok := prev[name]; !ok || old.Healthy != r.Healthy {
			if r.Healthy {
				p.logf("蜜罐健康：%s（%s）可用，%dms", name, r.Addr, r.LatencyMS)
			} else {
				p.logf("WARN 蜜罐健康：%s（%s）不可用：%s —— 命中它的改道会回落真实业务（NI-5）", name, r.Addr, r.Error)
			}
		}
	}
}

// run 周期探测直到 ctx 结束。
func (p *healthProber) run(ctx context.Context) {
	for {
		p.once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(probeInterval):
		}
	}
}

// results 返回最近一轮结果（按名排序，输出稳定）。
func (p *healthProber) results() []probeResult {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]probeResult, 0, len(p.last))
	for _, r := range p.last {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
