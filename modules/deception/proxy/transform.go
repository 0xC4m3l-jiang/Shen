// 本文件是**响应改写与响应卫生**这一块责任：
//
//	① injectingTransport —— 在改道侧把诱饵 / AI 欺骗内容改写进响应体；
//	② headerSanitizer    —— 出站前清掉暴露我们代理栈的响应头（OH-2）；
//	③ trackingWriter     —— 记录「有没有向客户端写出过字节」，供引流失败时判断能否回落。
//
// 三者与「判定怎么来、后端在哪、遥测怎么报」无关，因此不放在 handler.go 里，
// 也**不依赖整个 Handler**：transport 只拿它真正需要的三个方法（`injectionSource`）。
package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// ── 响应注入判据 ────────────────────────────────────────────────────────────

// injectable 报告该响应能否注入，并返回它的内容类型（N10）。
//
// 不注入的情形集中在这里，每一类都有具体的错误代价：
//
//	① 非 HTML —— 改它会使响应体的声明类型与实际内容不符；
//	② 压缩 / 任何非 identity 编码 —— 改字节会直接破坏编码；
//	③ 超过缓冲上限 —— 大响应 / 下载应当流式透传；
//	④ **无正文的方法与状态**（HEAD / 1xx / 204 / 304）—— 它们本来就没有体，
//	   不能因为“注入成功”就凭空造一个体出来（会与声明的 Content-Length 矛皾）；
//	⑤ **部分响应**（206 / 带 Content-Range）—— 改写单段会破坏范围语义；
//	⑥ **`Cache-Control: no-transform`** —— 上游明确禁止中介改写内容（HTTP 语义约定）。
func injectable(resp *http.Response) (contentType string, ok bool) {
	if resp.Request != nil && resp.Request.Method == http.MethodHead {
		return "", false
	}
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified ||
		(resp.StatusCode >= 100 && resp.StatusCode < 200) {
		return "", false
	}
	if resp.StatusCode == http.StatusPartialContent || resp.Header.Get("Content-Range") != "" {
		return "", false
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Cache-Control")), "no-transform") {
		return "", false
	}
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

// ── 注入 transport ──────────────────────────────────────────────────────────

// injectionSource 是改写响应时真正需要的**全部**能力。
//
// 三个方法而已，而不是 `*Handler`：Handler 还持有 gRPC 客户端、判定缓存、遥测队列与策略状态，
// 那些都属于「请求怎么被判定」而不属于「响应字节怎么被改写」。收敛成接口后：
//   - 改写逻辑可以脱离 Handler 单测（塞一个替身即可，见 transform_test.go）；
//   - 读代码的人一眼看到改写的依赖面 —— 不会以为它还会去调核心或写遥测。
type injectionSource interface {
	// currentInjector 返回该快照的静态注入器（本地 env 或策略面下发），可为 nil。
	currentInjector(st *remoteState) Injector
	// contentInjectionReady 报告该快照下能否注入 AI 内容（本地兜底开关 ∧ 下发开关 ∧ 有清单）。
	contentInjectionReady(st *remoteState) (bool, *contentIndex)
	// injectContent 把命中的内容片段改写进响应体，返回 (新体, 内容标识, 是否改写)。
	injectContent(r *http.Request, idx *contentIndex, contentType string, body []byte) ([]byte, string, bool)
}

// injectingTransport 包一层 RoundTripper，在**引流后端**的 HTML 响应上注入诱饵与 AI 欺骗内容。
//
// INT-8：只改引流侧 —— 本 transport 只挂在 mirage 的 reverse_proxy 上，业务侧不经过它。
// 任何异常（非 HTML、读失败、超限）都**原样透传**：改写不是业务链路上的失败点。
//
// 两条注入源（各自独立，可同时存在）：
//
//	① **静态规则**（本地 env / 策略面 `inject_rules`）—— 由 `currentInjector()` 给；
//	② **AI 欺骗内容**（策略面 `content_manifest`）—— 需开关成立 + 命中资源 + 校验和相符。
//
// 无论哪种，最终都走 `deception/injection` 的同一份改写语义（`ST-5`）。
type injectingTransport struct {
	// src 而不是注入器本身：注入规则可经**策略面**在运行期变（`applyEdgePolicy`），
	// 而 transport 是建后端的时刻就挂上的 —— 持注入器会把规则钉死在当时那一份。
	src  injectionSource
	base http.RoundTripper
	// snapshotOf 取本请求钉定的策略快照（N1）。nil 时回落当前生效值 ——
	// 单测直接调 transport 没有 ServeHTTP 铺好的上下文。
	snapshotOf func(*http.Request) *remoteState
}

// policySnapshotFrom 是取快照的缺省方式：从请求上下文里取（ServeHTTP 钉好的那一份），取不到返回 nil。
func policySnapshotFrom(req *http.Request) *remoteState {
	if req == nil {
		return nil
	}
	st, _ := req.Context().Value(policySnapshotKey{}).(*remoteState)
	return st
}

func (t *injectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if resp == nil {
		return resp, nil
	}

	// 注入结果的槽（没有槽时静默丢弃 —— 比如单测里直接调 transport）。
	outcome, _ := req.Context().Value(injectOutcomeKey{}).(*injectOutcome)

	// 每请求取一次**本请求钉定的快照**（N1）：注入规则与内容清单必须与本次路由/后端同一版本。
	// 快照由 ServeHTTP 放进请求上下文；单测直接调 transport 时回落到当前值。
	st := policySnapshotFrom(req)
	if st == nil {
		// 请求上没钉快照（单测直接调 transport / 非 ServeHTTP 路径）⇒ 用 src 当前生效的那份。
		if h, ok := t.src.(*Handler); ok {
			st = h.remotePolicy()
		}
	}
	if t.snapshotOf != nil {
		st = t.snapshotOf(req)
	}
	inj := t.src.currentInjector(st)
	contentReady, idx := t.src.contentInjectionReady(st)
	if !contentReady && outcome != nil {
		// 到了改道侧但内容注入被关 ⇒ 如实上报「开关关闭」（不是"没内容"）。
		outcome.set(InjectDisabled, "")
	}
	if inj == nil && !contentReady {
		// 什么都不用做：不读 body、不改写、不谎报。
		return resp, nil
	}

	ct, ok := injectable(resp)
	if !ok {
		if contentReady && outcome != nil {
			// 开关开着但响应不可改写（非 HTML / 已压缩 / 超限）⇒ 没有可用内容。
			outcome.set(InjectNoContent, "")
		}
		return resp, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxInjectBytes+1))
	if err != nil {
		// 读失败：把错误抛给 reverse_proxy（此时尚未向客户端写出任何字节，
		// forwardMirage 可安全回落业务）。已读部分丢弃，但**原 Body 由这里关闭**。
		_ = resp.Body.Close()
		return nil, err
	}
	if len(body) > maxInjectBytes {
		// 太大：把已读部分接回去，原样透传。
		// 关闭责任**必须**跟着新 Body 走（FIX-8）：从前这里用 `io.NopCloser(io.MultiReader(...))`
		// 把原 Body 包起来，Close 变成空操作 ⇒ 底层连接不被回收（连接泄漏）。
		resp.Body = &prefixBody{prefix: bytes.NewReader(body), rest: resp.Body}
		return resp, nil
	}
	_ = resp.Body.Close()

	out := body
	if inj != nil {
		if next, changed := inj.Inject(ct, out); changed {
			out = next
		}
	}
	if contentReady {
		next, contentID, changed := t.src.injectContent(req, idx, ct, out)
		if changed {
			out = next
			if outcome != nil {
				outcome.set(InjectApplied, contentID)
			}
		} else if outcome != nil {
			outcome.set(InjectNoContent, "")
		}
	}

	resp.Body = io.NopCloser(bytes.NewReader(out))
	resp.ContentLength = int64(len(out))
	resp.Header.Set("Content-Length", strconv.Itoa(len(out)))
	resp.Header.Del("Content-Encoding") // 已解出明文并改写，去掉压缩标记
	// **校验器必须跟着正文一起失效**（`N10`）：`ETag` / `Last-Modified` 是"这段字节的身份"，
	// 我们刚刚把它改了。留着它们会让条件请求（`If-None-Match` / `If-Modified-Since`）得到 304 ——
	// 客户端据此认为内容没变，于是**看不到注入的线索**，而我们这边还以为投放成功了。
	// 不重算 ETag 而直接删除：重算需要与上游同一套口径（弱/强校验、分块、压缩前/后），
	// 删掉只会让缓存重新取一次完整响应 —— 对诱饵路径而言这是**正确**的代价。
	resp.Header.Del("ETag")
	resp.Header.Del("Last-Modified")
	return resp, nil
}

// prefixBody 把「已读前缀 + 剩余流」拼成一个 Body，并**保证原 Body 被关闭恰好一次**。
//
// 为什么单独一个类型：`io.MultiReader` 只解决读，不解决关。而 net/http 的约定是
// 「调用方必须 Close 响应体，否则连接不可复用」—— 少了这一步就是连接泄漏（FIX-8）。
type prefixBody struct {
	prefix *bytes.Reader
	rest   io.ReadCloser
	once   sync.Once
}

func (b *prefixBody) Read(p []byte) (int, error) {
	if b.prefix.Len() > 0 {
		return b.prefix.Read(p)
	}
	return b.rest.Read(p)
}

// Close 关闭底层 Body；重复调用只关一次（Close 必须幂等）。
func (b *prefixBody) Close() error {
	var err error
	b.once.Do(func() { err = b.rest.Close() })
	return err
}

// ── trackingWriter ──────────────────────────────────────────────────────────

// caddyDefaultServerHeader 是 Caddy 在服务器层给我们加上的 `Server` 值。
//
// 它必须被清掉：规则要求 `Server` 头**与上游一致或直接透传**（`OH-2` 适用位置表）。
// 上游自己带了 `Server`（例如 nginx）时我们**原样保留** —— 那才是「与上游一致」。
const caddyDefaultServerHeader = "Caddy"

// headerSanitizer 在响应写出前清掉会暴露我们代理栈的头。
//
// 它只做两件最小的事，避免误伤业务响应（`INT-8`：业务侧响应不得改写）：
//  1. 删 `Via` —— 那是**我们这一跳**的产物，任何情况下都不该让对手看到；
//  2. 只在 `Server` 恰好等于 Caddy 默认值时删它 —— 上游的值一律保留。
type headerSanitizer struct {
	http.ResponseWriter
	done bool
	// status / wrote 是**响应观测**：逐判定事件要报「返回给客户端的状态码与字节数」。
	// 放在这里是因为它已经包住了整个请求的 ResponseWriter，不需要再加一层包装。
	status int
	wrote  int
}

func (s *headerSanitizer) WriteHeader(code int) {
	s.status = code
	s.sanitize()
	s.ResponseWriter.WriteHeader(code)
}

func (s *headerSanitizer) Write(b []byte) (int, error) {
	n, err := func() (int, error) {
		s.sanitize()
		return s.ResponseWriter.Write(b)
	}()
	s.wrote += n
	return n, err
}

// statusCode 返回实际状态码；从未显式写过头就是 200（net/http 的默认行为）。
func (s *headerSanitizer) statusCode() int {
	if s.status == 0 {
		return http.StatusOK
	}
	return s.status
}

// bytesWritten 返回实际写出的响应体字节数。
func (s *headerSanitizer) bytesWritten() int { return s.wrote }

func (s *headerSanitizer) sanitize() {
	if s.done {
		return
	}
	s.done = true
	h := s.ResponseWriter.Header()
	h.Del("Via")
	if strings.EqualFold(strings.TrimSpace(h.Get("Server")), caddyDefaultServerHeader) {
		h.Del("Server")
	}
}

// Unwrap 让 Caddy 仍能找到被包住的 ResponseWriter（保留其可选接口）。
func (s *headerSanitizer) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Flush 透传：流式响应（SSE / 分块）不能被这层包装破坏。
func (s *headerSanitizer) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack 透传：协议升级（WebSocket / 101）必须仍然可用。
func (s *headerSanitizer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("proxy: 底层 ResponseWriter 不支持 Hijack")
	}
	return hj.Hijack()
}

// trackingWriter 记录「是否已经向客户端写出过字节」。
//
// 这个信息决定了引流后端失败时能不能安全回落业务：
// 没写过 → 可以重放到业务；写过 → 只能如实报错，否则会输出半截响应。
type trackingWriter struct {
	http.ResponseWriter
	wrote bool
}

func (t *trackingWriter) WriteHeader(code int) {
	t.wrote = true
	t.ResponseWriter.WriteHeader(code)
}

func (t *trackingWriter) Write(b []byte) (int, error) {
	t.wrote = true
	return t.ResponseWriter.Write(b)
}

// Unwrap 让 Caddy 能找到被包住的 ResponseWriter（保留其可选接口）。
func (t *trackingWriter) Unwrap() http.ResponseWriter { return t.ResponseWriter }

// Flush 透传，避免破坏流式响应（SSE / 分块传输）。
func (t *trackingWriter) Flush() {
	if f, ok := t.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack 透传，使协议升级（WebSocket / 101 Switching Protocols）在引流路径上仍可用。
func (t *trackingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := t.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("proxy: 底层 ResponseWriter 不支持 Hijack")
	}
	t.wrote = true
	return hj.Hijack()
}
