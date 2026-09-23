package proxy

import (
	"context"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// 本文件是**幻境后端的活跃健康门禁**（建议书 §8 第 7 项：`enabled` 不是健康）。
//
// 为什么需要它（而不是"能不能连上就看请求结果"）：
//   - 配置里的 `enabled: true` 只说明"这个后端被登记并允许使用"，不说明它**现在活着** ——
//     后端挂掉后，每条诱饵请求都要**先吃满一次连接失败**才走到固定 502（延迟变成可见特征）；
//   - 而且这时没有任何**运维可见**的信号：只有翻日志才知道某个幻境早就不在了。
//
// 判据与边界（刻意保守）：
//   - 探针只做**一次 TCP 连通**（最快、最不依赖后端语义）；它证明"能连上"，**不**证明"HTTP 正常" ——
//     TCP 通但协议坏的后端仍会被判健康，那种情况由请求路径的失败语义处理（原有行为不变）；
//   - 只探测**远端策略里启用且有地址**的后端；本地 env 后端不探（没有地址解析来源，且不需要）；
//   - 判为不健康**不改变安全语义**：诱饵路由仍然固定 502（`delivery_result=backend_unhealthy`），
//     评分改道仍然**回落业务**（`NI-5`）—— 它只是让这两条路**更快、更可见**；
//   - 状态转移才打一行日志（不刷屏）。

// defaultBackendHealthInterval 是探针间隔。
//
// 为什么默认**开**：不开的话"健康"就永远等于 `enabled`（正是建议书点出的问题）。
// 每个后端每 30s 一次 TCP 连通，代价可以忽略。
const defaultBackendHealthInterval = 30 * time.Second

// defaultBackendHealthTimeout 是单次探测的超时（短：探测不该拖住自己）。
const defaultBackendHealthTimeout = time.Second

// backendHealth 是后端健康状态的进程内视图（**可丢失**：重启即重新探测）。
type backendHealth struct {
	mu sync.RWMutex
	// unhealthy: 后端名 → 最近一次失败原因（不在表里 = 健康）
	unhealthy map[string]string
}

func newBackendHealth() *backendHealth {
	return &backendHealth{unhealthy: map[string]string{}}
}

// isUnhealthy 返回该后端当前是否被判为不健康（含原因）。
func (h *backendHealth) isUnhealthy(name string) (string, bool) {
	if h == nil {
		return "", false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	reason, bad := h.unhealthy[name]
	return reason, bad
}

// mark 记录一次探测结果，返回是否发生了**状态转移**（只有转移才值得打日志）。
//
// 语义：连续失败才判不健康（`failures` 计数由调用方累计）；一次成功即恢复。
func (h *backendHealth) set(name string, err error) (changed bool) {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	_, was := h.unhealthy[name]
	if err == nil {
		if was {
			delete(h.unhealthy, name)
			return true
		}
		return false
	}
	h.unhealthy[name] = err.Error()
	return !was
}

// backendAddresses 从当前快照取「后端名 → dial 地址」（http/https 的 host:port）。
//
// 只用远端策略里的后端：本地 env 后端在本模块里没有统一的可解析地址来源，
// 且它们的"坏"在请求路径上已经能被发现（不影响判据的完整性）。
func (h *Handler) backendAddresses(st *remoteState) map[string]string {
	if st == nil {
		return nil
	}
	out := make(map[string]string, len(st.addresses))
	for name, addr := range st.addresses {
		out[name] = addr
	}
	return out
}

// probeOnce 对所有已知后端做**一轮** TCP 探测（导出给测试直接调用，避免依赖定时器）。
func (h *Handler) probeOnce(ctx context.Context) {
	st := h.remotePolicy()
	addrs := h.backendAddresses(st)
	if len(addrs) == 0 {
		return
	}
	timeout := time.Duration(h.BackendHealthTimeout)
	if timeout <= 0 {
		timeout = defaultBackendHealthTimeout
	}
	names := make([]string, 0, len(addrs))
	for name := range addrs {
		names = append(names, name)
	}
	sort.Strings(names) // 确定性：日志与探测顺序可复现（AR-30 精神）
	for _, name := range names {
		addr := addrs[name]
		d := net.Dialer{Timeout: timeout}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
		}
		if h.health.set(name, err) {
			if err != nil {
				log.Printf("proxy: 幻境后端 %q（%s）探针失败 ⇒ 标记不健康：%v"+
					"（诱饵路由仍固定 502；评分改道仍回落业务）", name, addr, err)
			} else {
				log.Printf("proxy: 幻境后端 %q（%s）恢复健康", name, addr)
			}
		}
	}
}

// wake 请求**立刻探一轮**（非阻塞、有界：已有待处理的唤醒就丢弃这一次）。
//
// 为什么需要它：探针间隔（默认 30s）通常远长于策略拉取间隔（验证档 5s）——
// 只靠定时器意味着"新后端就位后最多 30s 内健康状态未知"，那段时间请求要走一次真实连接失败。
// 策略应用完成时敲一下，窗口就收敛到"探测本身要花的毫秒级"。
func (h *Handler) wake() {
	select {
	case h.healthWake <- struct{}{}:
	default: // 已经有一次待处理的唤醒，不必排队（唤醒的语义是"该看一眼了"）
	}
}

// runHealthProbes 周期探测，并在策略变更时立即补一轮（间隔 ≤ 0 时不起协程）。
func (h *Handler) runHealthProbes(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	// 启动先探一轮：让"第一次请求"就吃到健康判据，而不是先失败一次再学乖。
	h.probeOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.probeOnce(ctx)
		case <-h.healthWake:
			h.probeOnce(ctx)
		}
	}
}

// backendUnhealthy 是 `delivery_result` 的登记取值之一：后端**已知不可达**（探针判定）。
//
// 与 `backend_unavailable` 的区别：后者是"配置里就没有可用后端"（未登记/未启用/已撤销），
// 前者是"登记了、启用了，但探针说它现在连不上" —— 两者的运维动作不同（改配置 vs 修后端）。
const backendUnhealthy = "backend_unhealthy"

// healthReason 是给事件/日志用的一句话（空 = 健康）。
func (h *Handler) healthReason(name string) string {
	reason, bad := h.health.isUnhealthy(name)
	if !bad {
		return ""
	}
	return strings.TrimSpace(reason)
}
