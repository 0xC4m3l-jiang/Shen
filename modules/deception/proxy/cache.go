// 本文件是**本地判定缓存**这一块责任：缓存键怎么派生、缓存怎么失效、容量上限在哪。
//
// 拆出独立文件的原因：缓存键的正确性取决于「判定到底看了哪些输入」，
// 那是一条独立且容易出错的规则（FIX-2 就是在这里踩的坑），不该埋在处理流程中间。
package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"sync"
	"time"

	judgev1 "shen/common/api/judge/v1"
)

// defaultCacheMaxEntries 是本地判定缓存的默认容量上限（MD-10：缓存必须有容量上限）。
const defaultCacheMaxEntries = 65536

// reportTimeout 是单次遥测上报的超时。上报是异步的，但仍需要上限，
// 否则上游卡住会让上报 worker 永远挂着、缓冲被填满。
const reportTimeout = 3 * time.Second

// cacheKey 由 `decision_id` 与**其余参与判定的输入**一起派生（FIX-2）。
//
// 为什么不能只用 `decision_id`：它按（来源 IP, 会话, 路径, 时间窗）派生（`ST-10`），
// 而判定实际还会看 method / Host / 查询串 / UA 以及**策略版本**。只用 decision_id 时，
// 同一 IP + 同一会话 + 同一路径下，「GET /admin」与「POST /admin」会共用一条缓存结果 ——
// 后到的请求拿到前一条的动作（可能把只读探测的判定套到写请求上）。
//
// 与**事件幂等键分离**：`decision_id` 仍是契约里的幂等键（`ST-10` 不变），
// 这里只是本地缓存的键。缓存本就可丢失，键更严只会多调几次核心，不会改变判定结果。
//
// policyRev 为空（未接过策略面）时退化成「本地配置 + 本次输入」，语义仍然正确。
func cacheKey(id string, r *http.Request, policyRev string) string {
	h := sha256.New()
	for _, part := range []string{
		id, r.Method, r.URL.Path, r.URL.RawQuery,
		r.Header.Get("User-Agent"), r.Host, policyRev,
	} {
		_, _ = io.WriteString(h, part)
		_, _ = h.Write([]byte{0}) // 分隔符：避免 "a"+"bc" 与 "ab"+"c" 撞成同一个键
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// decisionCache 是按缓存键记忆判定的进程内缓存。
//
// 它是**可丢失**的：未命中就调核心，语义上等价。有 TTL 失效规则与容量上限（MD-10）；
// 满则整体清空，不做逐条 LRU —— 一是最小实现，二是清空能抗「大量不同路径填满缓存」的对抗性填满。
type decisionCache struct {
	mu  sync.Mutex
	m   map[string]cacheEntry
	ttl time.Duration
	cap int
	now func() time.Time
}

type cacheEntry struct {
	action  judgev1.Action
	backend string
	expires time.Time
}

func newDecisionCache(ttl time.Duration, cap int, now func() time.Time) *decisionCache {
	return &decisionCache{m: map[string]cacheEntry{}, ttl: ttl, cap: cap, now: now}
}

func (c *decisionCache) get(id string) (judgev1.Action, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[id]
	if !ok {
		return judgev1.Action_ACTION_ORIGIN, "", false
	}
	if c.now().After(e.expires) {
		delete(c.m, id)
		return judgev1.Action_ACTION_ORIGIN, "", false
	}
	return e.action, e.backend, true
}

func (c *decisionCache) put(id string, act judgev1.Action, backend string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= c.cap {
		// 满则清空（MD-10 容量上限）。缓存本就可丢失，清空恢复正常请求的缓存能力。
		c.m = map[string]cacheEntry{}
	}
	c.m[id] = cacheEntry{action: act, backend: backend, expires: c.now().Add(c.ttl)}
}
