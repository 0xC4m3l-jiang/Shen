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
