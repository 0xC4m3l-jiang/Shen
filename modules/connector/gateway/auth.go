// Package gateway 是反向隧道的**平台侧**：连接器拨入的 TLS 终结点 + 会话池 +
// 供代理取流的回环字节桥。
//
// 分层纪律（与 proxy 的「模块里无转发逻辑」一脉相承）：
//   - serve.go：连接器接入与会话生命周期（控制面）；
//   - session.go：会话池与 Host 路由（纯状态）；
//   - bridge.go：proxy 连接 → yamux 流的字节桥（唯一的转发代码，且是 io.Copy）。
package gateway

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
)

// 校验错误（握手失败时回给连接器的人话，不含密钥）。
var (
	ErrUnknownKey   = errors.New("接入凭证无效（不存在或已重置）")
	ErrRevokedKey   = errors.New("接入凭证已吊销")
	ErrNameMismatch = errors.New("服务名与凭证不一致（凭证绑定唯一服务）")
	ErrHostsExceed  = errors.New("声明的域名超出凭证白名单")
)

// ConsoleKey 是从控制台拉取的一条凭证校验材料。
type ConsoleKey struct {
	ID      string
	Name    string
	Hosts   []string
	KeyHash string
	Revoked bool
}

// KeyTable 是凭证哈希表的内存快照：控制台是事实源，网关定期整表替换。
// 校验走恒定时间比较，且遍历全部 key —— 不因「第几个命中」泄露时序。
type KeyTable struct {
	mu   sync.RWMutex
	keys []ConsoleKey
}

// Replace 用控制台拉取的整表替换本地快照。
func (t *KeyTable) Replace(keys []ConsoleKey) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.keys = keys
}

// Len 返回当前 key 数（健康观测用）。
func (t *KeyTable) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.keys)
}

// Validate 校验握手：key 哈希（恒定时间）→ 服务名一致 → hosts 是白名单子集。
// hosts 入参应为已规范化的声明域名。
func (t *KeyTable) Validate(authKey string, name string, hosts []string) (ConsoleKey, error) {
	sum := sha256.Sum256([]byte(authKey))
	presented := hex.EncodeToString(sum[:])
	t.mu.RLock()
	defer t.mu.RUnlock()
	var match *ConsoleKey
	for i := range t.keys {
		k := &t.keys[i]
		// 逐 key 恒定时间比较；即便已命中也继续走完（不提前退出）。
		if subtle.ConstantTimeCompare([]byte(presented), []byte(k.KeyHash)) == 1 {
			cp := *k
			match = &cp
		}
	}
	if match == nil {
		return ConsoleKey{}, ErrUnknownKey
	}
	if match.Revoked {
		return ConsoleKey{}, ErrRevokedKey
	}
	if !strings.EqualFold(match.Name, strings.TrimSpace(name)) {
		return ConsoleKey{}, fmt.Errorf("%w：凭证属于 %q，声明为 %q", ErrNameMismatch, match.Name, name)
	}
	for _, h := range hosts {
		if !hostAllowed(match.Hosts, h) {
			return ConsoleKey{}, fmt.Errorf("%w：%s 不在白名单", ErrHostsExceed, h)
		}
	}
	return *match, nil
}

// hostAllowed：精确命中或最左一级通配命中（与桥接路由同一语义）。
func hostAllowed(patterns []string, host string) bool {
	h := NormalizeHost(host)
	if h == "" {
		return false
	}
	for _, p := range patterns {
		if HostMatches(NormalizeHost(p), h) {
			return true
		}
	}
	return false
}

// Service 是一个**接入单元**的对外视图（`SHEN-M4-03`）：服务名 + 归属域名 + 凭证状态。
//
// 为什么要有它：控制台/策略/审计都需要「现在有哪些服务、它们占着哪些 Host」，
// 而这个事实源在连接器的凭证表里。把视图收敛到一处（而不是让每个消费方各写一遍去重与合并），
// 才能保证「删除一条凭证 = 这个服务从所有视图里消失」，不留孤儿数据。
type Service struct {
	Name    string   // 服务名（凭证绑定的唯一服务）
	Hosts   []string // 归属域名（同一服务多条凭证时取并集，已排序）
	KeyIDs  []string // 绑定到该服务的凭证 id（便于审计与一键吊销）
	Revoked bool     // **全部**凭证都已吊销才算吊销；还有一条可用就不算
}

// Services 返回按服务名去重后的接入单元视图（按名称排序，保证确定性）。
//
// 同一服务可能有多条凭证（轮换期并存）：这里合并成一条，Hosts 取并集、KeyIDs 全部列出。
// 名字大小写不敏感 —— 与 `Validate` 的服务名比对同一口径。
func (t *KeyTable) Services() []Service {
	t.mu.RLock()
	byName := make(map[string]*Service, len(t.keys))
	for _, k := range t.keys {
		key := strings.ToLower(strings.TrimSpace(k.Name))
		if key == "" {
			continue // 无名凭证无法归属到任何服务：不制造一条空服务
		}
		svc, ok := byName[key]
		if !ok {
			svc = &Service{Name: strings.TrimSpace(k.Name), Revoked: true}
			byName[key] = svc
		}
		svc.KeyIDs = append(svc.KeyIDs, k.ID)
		svc.Hosts = append(svc.Hosts, k.Hosts...)
		if !k.Revoked {
			svc.Revoked = false
		}
	}
	t.mu.RUnlock()

	out := make([]Service, 0, len(byName))
	for _, svc := range byName {
		out = append(out, Service{
			Name:    svc.Name,
			Hosts:   sortedUniqueHosts(svc.Hosts),
			KeyIDs:  sortedCopy(svc.KeyIDs),
			Revoked: svc.Revoked,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// sortedUniqueHosts 归一化 + 去重 + 排序（Host 大小写/端口/尾点不同也算同一个）。
func sortedUniqueHosts(hosts []string) []string {
	seen := make(map[string]struct{}, len(hosts))
	out := make([]string, 0, len(hosts))
	for _, raw := range hosts {
		h := NormalizeHost(raw)
		if h == "" {
			continue
		}
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// sortedCopy 返回排序后的副本（确定性：同一份输入两次调用得到同一串顺序）。
func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// HostsOf 返回某凭证名的白名单（握手应答与排障用；找不到返回空）。
func (t *KeyTable) HostsOf(name string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, k := range t.keys {
		if strings.EqualFold(k.Name, name) {
			return append([]string(nil), k.Hosts...)
		}
	}
	return nil
}

// HostKnown 报告 host 是否命中**任意**凭证的白名单（桥接区分「没接入」与「离线」）。
func (t *KeyTable) HostKnown(rawHost string) bool {
	h := NormalizeHost(rawHost)
	if h == "" {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, k := range t.keys {
		for _, pattern := range k.Hosts {
			if HostMatches(NormalizeHost(pattern), h) {
				return true
			}
		}
	}
	return false
}

// NormalizeHost 把请求 Host 归一到可比较形态：小写、去端口、去尾点（与登记表同口径）。
func NormalizeHost(raw string) string {
	h := strings.ToLower(strings.TrimSpace(raw))
	if hostOnly, _, err := net.SplitHostPort(h); err == nil {
		h = hostOnly
	}
	h = strings.Trim(h, "[]")
	return strings.TrimSuffix(h, ".")
}

// HostMatches 判断 host 是否命中模式（精确或最左一级通配）。
func HostMatches(pattern, host string) bool {
	if pattern == host {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		return strings.HasSuffix(host, suffix) && len(host) > len(suffix)
	}
	return false
}

// SortedHosts 输出排序副本（日志与上报的确定性）。
func SortedHosts(hosts []string) []string {
	out := append([]string(nil), hosts...)
	sort.Strings(out)
	return out
}
