package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	judgev1 "shen/api/judge/v1"
)

// targetOrigin 是「业务真实地址」在本模块内部的键。
// 用一个不可能与引流后端名冲突的值，避免核心给的后端名恰好叫 origin。
const targetOrigin = "\x00origin"

// defaultCacheMaxEntries 是本地判定缓存的默认容量上限（MD-10：缓存必须有容量上限）。
const defaultCacheMaxEntries = 65536

// reportTimeout 是单次遥测上报的超时。上报是异步的，但仍需要上限，
// 否则上游卡住会让上报 worker 永远挂着、缓冲被填满。
const reportTimeout = 3 * time.Second

// 引流后端响应头超时的默认值。业务侧**不设**此超时（慢接口是业务自己的行为）。
const mirageDefaultTimeout = 10 * time.Second

// maxInjectBytes 是注入缓冲上限：只改写小页面，大响应（下载 / 流式）原样透传。
const maxInjectBytes = 1 << 20 // 1 MiB

// ── 判定取值折叠 ─────────────────────────────────────────────────────────────

// actionOf 把核心的响应折叠成**必定合法**的决策取值。
//
// 任何未识别、未匹配、解析失败或不确定的状态一律回落 route_origin（NI-5）。
func actionOf(resp *judgev1.JudgeResponse) judgev1.Action {
	if resp == nil {
		return judgev1.Action_ACTION_ORIGIN
	}
	switch resp.GetAction() {
	case judgev1.Action_ACTION_MIRAGE, judgev1.Action_ACTION_BLOCK:
		return resp.GetAction()
	default:
		// ACTION_ORIGIN 与 ACTION_UNSPECIFIED 都按放行处理。
		return judgev1.Action_ACTION_ORIGIN
	}
}

// ── 观测构造 ─────────────────────────────────────────────────────────────────

// observationFrom 把 HTTP 请求转成观测。
//
// ⚠️ **刻意不读请求体。** 本模块在请求路径上，读 body 会把上游收到的请求体吃掉，
// 而大文件上传全量缓冲会同时拖垮延迟与内存。判定面契约（api/judge/v1 的 Observation）
// 里也没有 body 字段 —— 阈值校准用的是头与路径。这与形态①的接收端不同：
// 那边是副本，可以放心读。
//
// 注意 INT-22：启用**误导处置**需要能读出请求体。那是阶段 2b 的事，
// 且当前契约没有承载它的字段 —— 已登记为未决项。
func observationFrom(r *http.Request, trustXFF bool) *judgev1.Observation {
	headers := make(map[string]string, len(r.Header))
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[strings.ToLower(k)] = v[0]
		}
	}
	return &judgev1.Observation{
		SourceIp:  clientIP(r, trustXFF),
		UserAgent: r.Header.Get("User-Agent"),
		Method:    r.Method,
		Path:      r.URL.Path,
		Headers:   headers,
	}
}

// clientIP 取真实客户端 IP（INT-23：不得因接入导致业务侧丢失来源 IP）。
func clientIP(r *http.Request, trustXFF bool) string {
	if trustXFF {
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			// XFF 是逗号分隔列表，第一段是原始客户端。
			// 只取第一段，后缀与「是否找到分隔符」都用不上。
			first, _, _ := strings.Cut(v, ",")
			return strings.TrimSpace(first)
		}
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		return strings.TrimSpace(host[:i])
	}
	return strings.TrimSpace(host)
}

// ── 白名单 ───────────────────────────────────────────────────────────────────

// whitelisted 判断来源是否免判定（INT-25）。空名单一律不命中。
func whitelisted(ip string, list []netip.Prefix) bool {
	if len(list) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, p := range list {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// parsePrefixesFromList 把 CIDR 字符串列表解析成 netip.Prefix（Caddy 模块配置是 []string）。
func parsePrefixesFromList(list []string) ([]netip.Prefix, error) {
	if len(list) == 0 {
		return nil, nil
	}
	var out []netip.Prefix
	for _, item := range list {
		p, err := netip.ParsePrefix(strings.TrimSpace(item))
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ── decision_id 派生（ST-10）────────────────────────────────────────────────

// decisionID 按（来源标识, 会话, 路径, 时间窗）派生（ST-10）。
// 同一组合得到同一 ID，以便核心按这个键做幂等与去重。
//
// 「会话」这里取 Cookie 头的原值 —— 只是读，不解析、不外传（INT-20）。
//
// 形态①的接收端（deception/mirror）有一份**同样语义**的实现。两处刻意不共享代码：
// 适配器之间必须能各自独立部署（INT-5），而这段逻辑只有十几行且由 ST-10 固定；
// 改其一时必须同时改另一处。
func decisionID(r *http.Request, trustXFF bool, window time.Duration, now time.Time) string {
	key := strings.Join([]string{
		clientIP(r, trustXFF),
		r.Header.Get("Cookie"),
		r.URL.Path,
		strconv.FormatInt(now.Truncate(window).Unix(), 10),
	}, "\x00")
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16])
}

// ── 响应注入判据 ────────────────────────────────────────────────────────────

// injectable 报告该响应能否注入，并返回它的内容类型。
//
// 三条「不注入」的理由集中在这里：非 HTML、带压缩编码（注入会直接损坏编码）、
// 超过缓冲上限（大响应 / 下载应当流式透传）。
func injectable(resp *http.Response) (contentType string, ok bool) {
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(strings.ToLower(ct), "text/html") {
		return "", false
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		return "", false
	}
	if resp.ContentLength > maxInjectBytes {
		return "", false
	}
	return ct, true
}

// ── 判定缓存 ─────────────────────────────────────────────────────────────────

// decisionCache 是按 decision_id 记忆判定的进程内缓存。
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
