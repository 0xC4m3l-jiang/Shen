package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"shen/modules/deception/injection"
)

// AI 欺骗内容的**消费侧**（`ADR-0023` / `AR-33`）：
//
//	核心（生成期过护栏）→ 策略载荷 `content_manifest` → 本文件：命中 → 再验 → 注入改道侧
//
// 三条纪律（都在本文件里）：
//
//	① **热路径不调模型**：本文件只查表 + 纯改写，无网络、无随机、无时钟（`AR-30`）；
//	② **确定性命中**：`variant = fnv1a(会话键) mod N`，同会话恒同槽位；
//	③ **任何一步不成立就原样返回**：注入失败**禁止**变成业务失败（`NI-1`）。
//
// 契约：`docs/spec/policy-payload.md` §2 的 `content_manifest`。

// inject 的四个取值（**唯一权威表**见 `docs/spec/events.md` §2.2 —— 改它必须同步本文与契约文档）。
const (
	InjectApplied   = "applied"    // 改道侧响应被内容注入实际改写
	InjectDisabled  = "disabled"   // 走到改道侧，但开关关闭（下发级或本地兜底）
	InjectNoContent = "no_content" // 开关开着、也在改道侧，但没有可用内容
	InjectOff       = "off"        // 未涉及注入（不是改道侧）
)

// contentManifest 是策略载荷里的 `content_manifest`（与核心侧 `edgeContentManifest` **手工对齐**：
// 两端分属不同平面，`ST-3` / `TB-24` 禁止共享 Go 类型；改动其一必须同时改另一处与契约文档）。
type contentManifest struct {
	Version  uint64         `json:"version"`
	Selector string         `json:"selector"`
	Variants int            `json:"variants"`
	Entries  []contentEntry `json:"entries"`
}

type contentEntry struct {
	Resource  string        `json:"resource"`
	ProfileID string        `json:"profile_id"`
	Bodies    []contentBody `json:"bodies"`
}

type contentBody struct {
	VariantID int    `json:"variant_id"`
	ContentID string `json:"content_id"`
	Checksum  string `json:"checksum"`
	Body      string `json:"body"`
	Marker    string `json:"marker,omitempty"`
}

// contentIndex 是按资源与变体**预索引**的清单：策略应用时构建一次，请求路径只查表。
//
// 预索引而不是每次线性扫：请求路径上每一毫秒都在 `AR-29` 的预算里，而清单应用是低频事件。
type contentIndex struct {
	version  uint64
	variants int
	entries  map[string]map[int]contentBody
}

// newContentIndex 构建索引；结构不合法（selector 不认识 / variants < 1）时返回 nil。
//
// nil 的语义 = 「没有可用内容」⇒ 适配器报 `no_content`，**不报错、不阻断**（`NI-1`）。
func newContentIndex(m *contentManifest) *contentIndex {
	if m == nil || m.Selector != selectorSession || m.Variants < 1 {
		return nil
	}
	idx := &contentIndex{
		version:  m.Version,
		variants: m.Variants,
		entries:  make(map[string]map[int]contentBody, len(m.Entries)),
	}
	for _, entry := range m.Entries {
		if entry.Resource == "" {
			continue
		}
		bodies := make(map[int]contentBody, len(entry.Bodies))
		for _, body := range entry.Bodies {
			if body.VariantID < 0 || body.VariantID >= m.Variants || body.Body == "" {
				continue // 坏条跳过：宁可漏注入，不可注入错内容
			}
			bodies[body.VariantID] = body
		}
		if len(bodies) > 0 {
			idx.entries[entry.Resource] = bodies
		}
	}
	if len(idx.entries) == 0 {
		return nil
	}
	return idx
}

// selectorSession 是当前唯一合法的变体选择器（与 `spec/ai-contract.md` §3 一致）。
const selectorSession = "session"

// fnv1a 是 FNV-1a 32 位哈希（确定性：同输入恒同输出 —— `AR-30` 要求响应路径无非确定性）。
func fnv1a(s string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	h := uint32(offset32)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime32
	}
	return h
}

// variantFor 按会话键与变体数取槽位（`AR-30` + `ADR-0016`：多态在会话边界选择、会话内冻结）。
func variantFor(session string, variants int) int {
	if variants <= 1 {
		return 0
	}
	return int(fnv1a(session) % uint32(variants))
}

// sessionKeyOf 取会话键：**最小身份值**（与 `decisionID` 的会话分量**同一个来源**）。
//
// 取法有两级（`R07`）：
//  1. **请求上下文里有入口钉定的值** ⇒ 用它。这是必需的：投递到幻境前会**剥离业务会话 Cookie**
//     （`W4`），若这里仍从请求头取，剥离后所有请求都会退化成同一个空槽位 ——
//     变体选择随之塌缩（"每个会话看到自己的那一份"就不再成立）。
//  2. 没有钉定值（例如单测直接调 transport）⇒ 回落到按名读 cookie（行为与从前一致）。
//
// 空 Cookie 也合法：它只是一个会话值 —— 于是所有「没有 cookie 的请求」落同一个槽位（确定性优先）。
func (h *Handler) sessionKeyOf(r *http.Request) string {
	if v, ok := r.Context().Value(sessionKeyKey{}).(string); ok && v != "" {
		return v
	}
	return sessionHint(r, h.sessionCookieName())
}

// sessionKeyKey 是请求上下文里的会话键（未导出类型：只有本包能写）。
type sessionKeyKey struct{}

// withSessionKey 把入口算出的会话键钉进上下文（`R07`：剥离凭证前先固定身份）。
func withSessionKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, sessionKeyKey{}, key)
}

// ── 会话钉定（轮换只对新会话生效）────────────────────────────────────────────

// variantPins 是「会话 → 变体槽位」的进程内缓存（与清单版本绑定）。
//
// **它冻结的是「槽位选择」，不是「内容体」**（说法必须准确，否则读代码的人会以为轮换对老会话无影响）：
// 清单换代（轮换，`version` 递增）时钉定条目失效、槽位重算 —— 而槽位由 `fnv1a(会话)` 决定，
// 重算必得**同一槽位**（不漂移）；内容体则取自新版本清单。
//
// 它的真实作用有两个：① 避免每请求重算哈希（热路径，`AR-29`）；② 让「槽位」这一层显式可测。
// 容量与 TTL 都受限于判定缓存窗（可丢失的缓存，`MD-10`：有上限与失效规则）。
type variantPins struct {
	mu  sync.Mutex
	m   map[string]pinEntry
	ttl time.Duration
	cap int
	now func() time.Time
}

type pinEntry struct {
	variant int
	version uint64
	expires time.Time
}

func newVariantPins(ttl time.Duration, cap int, now func() time.Time) *variantPins {
	if cap <= 0 {
		cap = defaultCacheMaxEntries
	}
	return &variantPins{m: map[string]pinEntry{}, ttl: ttl, cap: cap, now: now}
}

func (p *variantPins) get(session string, version uint64) (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.m[session]
	if !ok {
		return 0, false
	}
	if p.now().After(e.expires) {
		delete(p.m, session)
		return 0, false
	}
	if e.version != version {
		// 清单换代：钉定条目属于旧版本 → 失效（槽位由哈希重算，必得同值；内容体取自新版本）。
		delete(p.m, session)
		return 0, false
	}
	return e.variant, true
}

func (p *variantPins) put(session string, variant int, version uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.m) >= p.cap {
		p.m = map[string]pinEntry{} // 满则清空（可丢失的缓存；与判定缓存同一策略）
	}
	p.m[session] = pinEntry{variant: variant, version: version, expires: p.now().Add(p.ttl)}
}

// variantOfSession 取会话的变体槽位：先查钉定，未命中则按哈希算并钉定。
func (h *Handler) variantOfSession(session string, idx *contentIndex) int {
	if v, ok := h.pins.get(session, idx.version); ok {
		return v
	}
	v := variantFor(session, idx.variants)
	h.pins.put(session, v, idx.version)
	return v
}

// ── 注入结果上报 ─────────────────────────────────────────────────────────────

// injectOutcome 是一次请求的注入结果（由 transport 在改道侧写，由 handler 在上报时读）。
type injectOutcome struct {
	mu        sync.Mutex
	status    string
	contentID string
}

func (o *injectOutcome) set(status, contentID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.status = status
	o.contentID = contentID
}

func (o *injectOutcome) get() (string, string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.status, o.contentID
}

// injectOutcomeKey 是请求上下文里的键（未导出类型：只有本包能写，避免别处伪造注入结果）。
type injectOutcomeKey struct{}

// withInjectOutcome 把一个结果槽挂进请求上下文（`ServeHTTP` 在进入处置流程前调用）。
func withInjectOutcome(ctx context.Context, o *injectOutcome) context.Context {
	return context.WithValue(ctx, injectOutcomeKey{}, o)
}

// injectResultOf 读一次请求的注入结果；没有槽（例如在单测里直接调 reportRoute）时返回空。
func injectResultOf(r *http.Request) (string, string) {
	o, ok := r.Context().Value(injectOutcomeKey{}).(*injectOutcome)
	if !ok {
		return "", ""
	}
	return o.get()
}

// ── 内容注入的执行 ──────────────────────────────────────────────────────────

// contentInjectionReady 报告「此刻能不能注入内容」，并给出当前清单索引。
//
// 三个条件**取与**（缺一即不注入，且上报 `disabled`）：
//
//	① 适配器的**本地兜底开关**（`SHEN_PROXY_INJECT_CONTENT`，默认 false）；
//	② 策略载荷的**下发级开关** `inject_enabled`（核心配置 `ai.enabled` 的投影）；
//	③ 有**可用的清单**（策略面给过、且结构可索引）。
func (h *Handler) contentInjectionReady(st *remoteState) (bool, *contentIndex) {
	if !h.InjectContent {
		return false, nil
	}
	if st == nil || !st.injectEnabled || st.content == nil {
		return false, nil
	}
	return true, st.content
}

// contentFor 取给这次请求用的内容片段；没有可用内容时返回 ok=false（⇒ 上报 `no_content`）。
//
// 命中规则（阶段 A）：`resource` 与**请求路径精确相等**（区分大小写）；不做前缀匹配
// （前缀匹配需要与路径归一化一起定，属阶段 B —— `spec/ai-contract.md` §2）。
func (h *Handler) contentFor(r *http.Request, idx *contentIndex) (contentBody, bool) {
	entry, ok := idx.entries[r.URL.Path]
	if !ok {
		return contentBody{}, false
	}
	body, ok := entry[h.variantOfSession(h.sessionKeyOf(r), idx)]
	if !ok {
		return contentBody{}, false
	}
	if got := checksumOf(body.Body); !strings.EqualFold(got, body.Checksum) {
		// 校验和不符：丢弃（宁可漏注入，不可注入错内容）。只在事件里体现，不打逐请求日志
		// —— 它说明**下发链路坏了**，由策略面的告警负责，不该在请求路径上刷日志。
		return contentBody{}, false
	}
	return body, true
}

// injectContent 把命中的内容片段注入响应体（走 `deception/injection` 的既有改写语义，`ST-5`）。
//
// 返回 (改写后的字节, 内容标识, 是否真的改写)。任何一步不成立都原样返回 —— 绝不阻断。
func (h *Handler) injectContent(r *http.Request, idx *contentIndex, contentType string, body []byte) ([]byte, string, bool) {
	content, ok := h.contentFor(r, idx)
	if !ok {
		return body, "", false
	}
	engine, err := injection.New([]injection.Rule{{
		Kind:    "content",
		Snippet: content.Body,
		Marker:  content.Marker,
	}})
	if err != nil {
		log.Printf("proxy: 内容注入规则非法（跳过，不影响响应）：%v", err)
		return body, "", false
	}
	out, changed := engine.Inject(contentType, body)
	if !changed {
		return body, "", false
	}
	return out, content.ContentID, true
}

// checksumOf 计算内容体的 SHA-256（小写十六进制）—— 与核心侧 `checksumOfContent` 同义
// （适配器**必须**自己再验一次：核心验的是「装载时的文件」，这里验的是「下发到我这儿的字节」）。
func checksumOf(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
