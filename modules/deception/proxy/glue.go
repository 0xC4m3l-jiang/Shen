package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	judgev1 "shen/common/api/judge/v1"
)

// targetOrigin 是「业务真实地址」在本模块内部的键。
// 用一个不可能与引流后端名冲突的值，避免核心给的后端名恰好叫 origin。
const targetOrigin = "\x00origin"

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

// maxDecodeRounds 是查询串规范化的**解码轮数上限**（含第一轮）。
//
// 为什么是 3：一轮治不了「二次编码」（`%252e` → `%2e`，攻击者本意是 `.`），
// 实测里二次编码就是绕过手段之一；而每多一轮都是对**攻击者可控输入**的线性工作，
// 所以把成本钉死在这里：解到不动点或解满 3 轮即停。代价（三重及以上编码仍可能绕过）已登记。
const maxDecodeRounds = 3

// queryOf 取查询串的**规范化匹配视图**（不含前导 `?`）：反复解码到不动点，上限 `maxDecodeRounds` 轮。
//
// 三条约定（与 proto / 规则引擎三方一致，见 `docs/spec/config.md` §2.4）：
//   - 解到不动点：`%252e` → `%2e` → `.`；`+` 与 `%20` → 空格 —— 否则编码形态不可见；
//   - **有上限**：把「对攻击者可控输入」的工作量钉死（代价：三重编码仍可能绕过）；
//   - 出错即停、原样传递：宁可少匹配，也不把载荷弄丢（`AR-31`）。
//
// 它**只用于匹配**：转发给上游的仍是原始请求行（原样字节见 `rawQueryOf`）。
//
// 形态①的接收端（`deception/mirror`）有一份**同样语义**的实现。两处刻意不共享代码：
// 适配器之间必须能各自独立部署（`INT-5`）—— 改其一时必须同时改另一处。
func queryOf(r *http.Request) string {
	cur := r.URL.RawQuery
	// 尝试 maxDecodeRounds **次**（不是「轮数 + 1」）：这样常量就是字面意思 ——
	// 三层编码（`%25252e`）刚好解满，四层及以上停下来（成本钉死，已登记）。
	for attempt := 0; attempt < maxDecodeRounds; attempt++ {
		next, err := url.QueryUnescape(cur)
		if err != nil || next == cur {
			break
		}
		cur = next
	}
	return cur
}

// rawQueryOf 取**原样**查询串（未解码），供审计与「按编码形态匹配」的规则使用（`AR-31`）。
func rawQueryOf(r *http.Request) string { return r.URL.RawQuery }

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
		Query:     queryOf(r),
		QueryRaw:  rawQueryOf(r),
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
