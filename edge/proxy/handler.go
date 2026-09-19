package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	judgev1 "shen/api/judge/v1"
	policyv1 "shen/api/policy/v1"
	telemetryv1 "shen/api/telemetry/v1"
	"shen/edge/injection"
)

// 上游 Transport 的连接层超时。没有超时的 Transport 会让挂死的上游一直占住连接与 goroutine。
const dialTimeout = 5 * time.Second

// Handler 是接入形态③（反向代理前置）与④（Sidecar）的 Caddy 中间件。
//
// 它**无状态**：进程可随时被杀死重建，唯一丢失的是判定缓存，
// 代价仅是下一次同 ID 请求多调一次核心。
//
// 它同时是 Caddy 模块（`http.handlers.shen_proxy`）：Provision 时从 JSON 配置
// 构建 gRPC 客户端、注入器与各后端的 reverse_proxy handler；请求时走
// 白名单 → 缓存 → gRPC 判定 → 三值路由，转发交给 Caddy 的 reverse_proxy。
type Handler struct {
	// ── Caddy 模块配置（JSON 可序列化）──────────────────────────────────────
	// Upstream 是业务真实地址，例如 http://127.0.0.1:9000。
	Upstream string `json:"upstream,omitempty"`

	// Mirage 是引流后端表：逻辑名 -> 地址（http://host:port）。
	Mirage map[string]string `json:"mirage,omitempty"`

	// Whitelist 是免判定的来源网段（CIDR 字符串）。
	// 它**必须先于**引流判定生效（INT-25）。
	Whitelist []string `json:"whitelist,omitempty"`

	// DecisionTimeout 是对核心调用的硬超时（NI-4）。超时即放行。
	DecisionTimeout caddy.Duration `json:"decision_timeout,omitempty"`

	// CacheTTL 是本地判定缓存的存活时间。
	CacheTTL caddy.Duration `json:"cache_ttl,omitempty"`

	// Window 是 decision_id 的时间窗（ST-10）。
	Window caddy.Duration `json:"window,omitempty"`

	// MirageResponseTimeout 是**引流后端**的响应头超时。0 时用默认值。
	MirageResponseTimeout caddy.Duration `json:"mirage_response_timeout,omitempty"`

	// Shadow 为真时只观测、不处置（INT-11）。
	Shadow bool `json:"shadow,omitempty"`

	// LogRequests 打开**逐请求**定位日志（白名单命中 / 判定缓存命中 / 判定结果）。
	//
	// 为什么默认关：观测面（事件 + 控制台）已经记录了每一次判定，生产环境不需要再来一份逐请求日志；
	// 但本地排查（“这个请求为什么走了 origin”）没有它就得去翻控制台，很慢。
	// 部署侧按需打开：`SHEN_PROXY_LOG_REQUESTS=1`（演示 compose 默认开）。
	LogRequests bool `json:"log_requests,omitempty"`

	// TrustXFF 决定是否信任 X-Forwarded-For 取客户端 IP（INT-23）。
	TrustXFF bool `json:"trust_xff,omitempty"`

	// ReportQueue 是遥测上报的缓冲深度。
	ReportQueue int `json:"report_queue,omitempty"`

	// CacheMaxEntries 是本地判定缓存的最大条目数（MD-10）。0 用默认值。
	CacheMaxEntries int `json:"cache_max_entries,omitempty"`

	// CoreAddr 是核心判定/遥测面的 gRPC 地址。Provision 时据此建客户端。
	CoreAddr string `json:"core_addr,omitempty"`

	// Inject 是注入片段（已按 `;;` 拆分）。空 = 不注入。
	Inject []string `json:"inject,omitempty"`

	// PolicyID 是本端期望的策略集标识（空 = 用核心当前的策略集）。
	PolicyID string `json:"policy_id,omitempty"`

	// AdapterID 是策略回执里的适配器标识（空 → "proxy"）。
	// 没有它就只能知道「有人应用了」，不知道「谁应用了」—— 版本对账做不成（AR-13）。
	AdapterID string `json:"adapter_id,omitempty"`

	// PolicyInterval 是策略面的拉取间隔；0 = 不拉取（完全用本地配置）。
	PolicyInterval caddy.Duration `json:"policy_interval,omitempty"`

	// ── 运行时状态（不序列化）──────────────────────────────────────────────
	judge    JudgeClient
	report   TelemetryClient
	injector Injector

	whitelist []netip.Prefix

	origin caddyhttp.MiddlewareHandler
	mirage map[string]caddyhttp.MiddlewareHandler

	// remote 是策略面当前生效的远端策略（整块原子替换：不会出现「后端表换了、白名单还是旧的」）。
	remote atomic.Pointer[remoteState]

	// policy 是策略面客户端；nil = 未接策略面（单测与「不拉策略」的部署）。
	policy PolicyClient

	// buildRemote 把远端后端地址构造成 Caddy 后端。Provision 时接真实实现，
	// 单测注入替身（MD-22：模块测试禁止依赖其他模块与真实 Caddy）。
	buildRemote func(name, address string) (caddyhttp.MiddlewareHandler, error)

	cache *decisionCache

	events  chan *telemetryv1.TelemetryEvent
	dropped atomic.Uint64

	conn *grpc.ClientConn

	ctx    context.Context
	cancel context.CancelFunc

	done      chan struct{}
	closeOnce sync.Once

	now func() time.Time
}

// CaddyModule 把 Handler 注册为 Caddy 模块 `http.handlers.shen_proxy`。
//
// 用指针接收器：Handler 含 atomic.Uint64（noCopy），值接收器会触发 copylocks。
func (*Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.shen_proxy",
		New: func() caddy.Module { return new(Handler) },
	}
}

// Provision 从 JSON 配置构建运行时依赖：gRPC 客户端、注入器、白名单、
// 判定缓存与各后端 reverse_proxy handler。
func (h *Handler) Provision(ctx caddy.Context) error {
	if strings.TrimSpace(h.CoreAddr) == "" {
		return fmt.Errorf("proxy: CoreAddr 不能为空（SHEN_CORE_ADDR）")
	}

	// 与核心之间走明文 gRPC：TLS 由本进程（Caddy）或上游 L0 终结。
	conn, err := grpc.NewClient(h.CoreAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("proxy: 建 gRPC 客户端失败：%w", err)
	}

	// 预热连接（治 K-24）：gRPC 是**惰性建连**的，第一次调用要付建连成本；
	// 而判定预算只有 3ms（AR-29）—— 于是"重启后第一条请求"常常判定超时后放行：
	// 业务不受影响（NI-3 生效），但那条**没有观测记录**，很难排查。
	// 启动阶段就把连接建立起来（最多等 2s；失败只记日志，**不阻断启动**：核心还没起来也要能起）。
	if werr := warmUp(ctx, conn, 2*time.Second); werr != nil {
		log.Printf("proxy: 预热核心连接未完成（不影响启动；首请求可能按 NI-3 放行）：%v", werr)
	}
	h.conn = conn
	h.judge = judgev1.NewDeceptionJudgeClient(conn)
	h.report = telemetryv1.NewDeceptionTelemetryClient(conn)

	// 诱饵注入（L1）：把配置片段注入**引流侧**的 HTML 响应。
	// 接口 Injector 由本模块定义；这里接上 edge/injection 的纯变换实现。
	if len(h.Inject) > 0 {
		inj, ierr := injection.New(injectRules(h.Inject))
		if ierr != nil {
			return fmt.Errorf("proxy: 建注入器失败：%w", ierr)
		}
		h.injector = inj
	}

	if h.whitelist, err = parsePrefixesFromList(h.Whitelist); err != nil {
		return fmt.Errorf("proxy: 白名单非法：%w", err)
	}

	now := time.Now
	h.now = now

	cacheCap := h.CacheMaxEntries
	if cacheCap <= 0 {
		cacheCap = defaultCacheMaxEntries
	}
	h.cache = newDecisionCache(time.Duration(h.CacheTTL), cacheCap, now)

	// 后端表：origin + 各引流后端，全部是 Caddy reverse_proxy。
	originRP, err := h.buildBackend(ctx, h.Upstream, false, 0)
	if err != nil {
		return fmt.Errorf("proxy: 业务地址无效：%w", err)
	}
	h.origin = originRP

	// 远端策略里的改道后端由同一个构造路径产出（同一个超时与注入边界），
	// 避免「远端后端」与「env 后端」行为不一致。
	h.buildRemote = func(_ string, address string) (caddyhttp.MiddlewareHandler, error) {
		return h.buildBackend(ctx, address, true, time.Duration(h.MirageResponseTimeout))
	}

	h.mirage = map[string]caddyhttp.MiddlewareHandler{}
	for name, addr := range h.Mirage {
		if name == "" || strings.Contains(name, "\x00") {
			return fmt.Errorf("proxy: 引流后端名非法：%q", name)
		}
		rp, perr := h.buildBackend(ctx, addr, true, time.Duration(h.MirageResponseTimeout))
		if perr != nil {
			return fmt.Errorf("proxy: 引流后端 %q 地址无效：%w", name, perr)
		}
		h.mirage[name] = rp
	}

	// 上报 worker 的生命周期挂在 Handler 自己的 context 上，由 Cleanup 取消。
	h.ctx, h.cancel = context.WithCancel(context.Background())
	h.done = make(chan struct{})
	queue := h.ReportQueue
	if queue <= 0 {
		queue = 8
	}
	h.events = make(chan *telemetryv1.TelemetryEvent, queue)
	go h.runReporter()

	// 策略面（S4）：与判定/遥测走同一条 gRPC 连接（同主机、同一个进程边界）。
	// 拉不到就继续用本地配置（NI-1）—— 策略面不是请求路径上的依赖。
	h.policy = policyv1.NewDeceptionPolicyClient(conn)
	if h.PolicyInterval > 0 {
		go h.runPolicyPolling(h.ctx, time.Duration(h.PolicyInterval))
	}

	return nil
}

// Close 停止上报 worker。调用后可继续服务，只是事件会入队后无人消费；
// 因此正常用法是「不再服务之后再 Close」。
func (h *Handler) Close() {
	h.closeOnce.Do(func() {
		if h.cancel != nil {
			h.cancel()
		}
		if h.done != nil {
			close(h.done)
		}
	})
}

// Cleanup 关闭 gRPC 连接并停止上报 worker（Caddy 卸载配置时调用）。
func (h *Handler) Cleanup() error {
	h.Close()
	if h.conn != nil {
		return h.conn.Close()
	}
	return nil
}

// Validate 校验配置（在 Provision 之后调用）。凡是启动后才会暴露的问题都提前拦住。
func (h *Handler) Validate() error {
	if strings.TrimSpace(h.Upstream) == "" {
		return fmt.Errorf("proxy: Upstream 不能为空")
	}
	if h.DecisionTimeout <= 0 {
		return fmt.Errorf("proxy: DecisionTimeout 必须为正")
	}
	if h.CacheTTL <= 0 {
		return fmt.Errorf("proxy: CacheTTL 必须为正")
	}
	if h.Window <= 0 {
		return fmt.Errorf("proxy: Window 必须为正")
	}
	return nil
}

// DroppedEvents 返回因缓冲满而丢弃的事件数。它**必须**可被观测 ——
// 静默丢弃遥测等于让运营看到一个偏低的数字还以为是真相。
func (h *Handler) DroppedEvents() uint64 { return h.dropped.Load() }

// ── 请求处理 ─────────────────────────────────────────────────────────────────

// ServeHTTP 处理一个请求，走完 AR-6 的四件事。
//
// 流程：① 白名单先于引流判定（INT-25）→ ② 派生 decision_id 并查本地缓存
// actionName 把决策枚举映射成**设计术语**（terminology.md §4 的三值）。
//
// 为什么要它：日志里写 route_mirage / block / route_origin 才与控制台、核心日志、文档用同一套词；
// 写 ACTION_MIRAGE 这种枚举名，排查时还得心算一层。
func actionName(act judgev1.Action) string {
	switch act {
	case judgev1.Action_ACTION_MIRAGE:
		return "route_mirage"
	case judgev1.Action_ACTION_BLOCK:
		return "block"
	default:
		return "route_origin"
	}
}

// → ③ 未命中则调核心判定（失败折叠成放行）→ ④ 异步上报 → 按结果路由。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// ⓪ 对外可见面卫生（OH-2 适用位置表）：
	//    · Caddy 在**服务器层无条件**写 `Server: Caddy`，reverse_proxy 还会加 `Via: x.y Caddy`；
	//    · 两者都会出现在**对手的屏幕上** —— 按 OH-2 的判据（会不会出现在攻击者的屏幕上）必须清掉。
	//    · `Server` 的规则是「与上游一致或直接透传」：上游给了就保留（下面按值判断），我们的默认值删掉。
	sw := &headerSanitizer{ResponseWriter: w}
	r.Header.Del("Via") // 也不让上游 / 幻境后端看到我们的栈指纹

	// ① 白名单先于改道判定（INT-25）—— 命中则不调核心，直接透传。
	// 本地白名单（env）与远端白名单（策略面）取**并集**：护栏只增不减（见 applyEdgePolicy）。
	ip := clientIP(r, h.TrustXFF)
	if whitelisted(ip, h.whitelist) || h.remoteWhitelisted(ip) {
		if h.LogRequests {
			log.Printf("proxy: 白名单命中，跳过判定：%s %s 来源=%s", r.Method, r.URL.Path, ip)
		}
		return h.forward(sw, r, next, targetOrigin)
	}

	// ② 派生 decision_id（ST-10）并查本地判定缓存（AR-6 第 2 件事）。
	id := decisionID(r, h.TrustXFF, time.Duration(h.Window), h.now())
	if act, backend, ok := h.cache.get(id); ok {
		if h.LogRequests {
			log.Printf("proxy: 判定缓存命中：%s %s decision_id=%s → %s（后端 %q）",
				r.Method, r.URL.Path, id, actionName(act), backend)
		}
		return h.dispatch(sw, r, next, act, backend)
	}

	// ③ 调核心判定。任何失败都已折叠成「放行」（NI-3 / NI-4 / NI-5）。
	act, backend, err := h.decide(r, id)
	h.cache.put(id, act, backend)
	if h.LogRequests {
		// 本地排查的主线索：判定 id + 结果 + 后端 +（若有）失败原因。
		log.Printf("proxy: 判定：%s %s decision_id=%s → %s（后端 %q，失败=%v）",
			r.Method, r.URL.Path, id, actionName(act), backend, err)
	}

	// ④ 异步上报（AR-6 第 4 件事）—— 不阻塞请求。
	h.enqueueEvent(r, id, act, err)

	return h.dispatch(sw, r, next, act, backend)
}

// dispatch 按决策结果选择路径。
//
// Shadow 为真时**永不改道、永不拦截**（INT-11：首次上线必须影子模式）；
// 决策照算、照上报，只是不执行。
func (h *Handler) dispatch(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler, act judgev1.Action, backend string) error {
	if h.Shadow {
		return h.forward(w, r, next, targetOrigin)
	}
	switch act {
	case judgev1.Action_ACTION_MIRAGE:
		// 后端名查不到一律回落业务（NI-5）—— 宁可漏改道，不可断业务。
		if _, ok := h.mirageHandler(backend); !ok {
			return h.forward(w, r, next, targetOrigin)
		}
		return h.forwardMirage(w, r, next, backend)
	case judgev1.Action_ACTION_BLOCK:
		// 403 是对手可见的处置 —— 这是**已承认的设计**（ADR-0002：引擎是欺骗调度器，
		// 不是 WAF，可见拦截交接入层）。block 只用于「明确拒绝已知恶意」，不用于透明误导；
		// route_mirage 才是本项目的核心价值。
		w.WriteHeader(http.StatusForbidden)
		return nil
	default:
		// ACTION_ORIGIN 与任何未识别取值都放行。
		return h.forward(w, r, next, targetOrigin)
	}
}

// forward 把请求交给指定后端的 Caddy reverse_proxy。
func (h *Handler) forward(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler, target string) error {
	if target != targetOrigin {
		rp, ok := h.mirageHandler(target)
		if !ok {
			return h.forward(w, r, next, targetOrigin)
		}
		return rp.ServeHTTP(w, r, next)
	}
	return h.origin.ServeHTTP(w, r, next)
}

// mirageHandler 按逻辑名找改道后端：**远端策略优先，本地配置兜底**（ADR-0018）。
//
// 远端优先：策略面是 ST-24（策略是数据）的正式通路；
// 本地兜底：策略面拉不到时边缘照常工作（NI-1）。
func (h *Handler) mirageHandler(name string) (caddyhttp.MiddlewareHandler, bool) {
	if st := h.remotePolicy(); st != nil {
		if rp, ok := st.backends[name]; ok {
			return rp, true
		}
	}
	rp, ok := h.mirage[name]
	return rp, ok
}

// forwardMirage 把请求交给引流后端；引流后端失败且尚未写出任何字节时，
// 回落到业务真实地址（NI-1 —— 引流失败绝不能变成业务失败）。
//
// 用 trackingWriter 记录「是否已写出字节」：没写过 → 可安全重放到业务；
// 写过 → 只能把错误如实抛出（由 Caddy 错误路由处理），否则会输出半截响应。
func (h *Handler) forwardMirage(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler, backend string) error {
	rp, ok := h.mirageHandler(backend)
	if !ok {
		// 理论上到不了这里（dispatch 已查过表）；真到了就回落业务，不报 500。
		return h.origin.ServeHTTP(w, r, next)
	}
	tw := &trackingWriter{ResponseWriter: w}
	if err := rp.ServeHTTP(tw, r, next); err != nil {
		if !tw.wrote {
			log.Printf("proxy: 引流后端不可达，回落业务：%v", err)
			return h.origin.ServeHTTP(w, r, next)
		}
		log.Printf("proxy: 引流后端中途失败：%v", err)
		return err
	}
	return nil
}

// ── 与核心交互 ───────────────────────────────────────────────────────────────

// decide 调一次核心判定面。返回的 error 仅用于上报，调用方不必处理 ——
// 判定失败已经折叠成「放行」。
func (h *Handler) decide(r *http.Request, id string) (judgev1.Action, string, error) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.DecisionTimeout))
	defer cancel()

	started := time.Now()
	resp, err := h.judge.Judge(ctx, &judgev1.JudgeRequest{
		DecisionId: id,
		Observed:   observationFrom(r, h.TrustXFF),
	})
	if err != nil {
		// 核心不可达 / 超时 —— 放行（NI-3 / NI-4）。
		//
		// ⚠️ 这条日志**不受 `SHEN_PROXY_LOG_REQUESTS` 控制**：它是"引擎没能判定"的唯一线索。
		// 只开逐请求日志的话，默认部署里就完全看不到"为什么这条没判"（实测踩过：重启后第一条
		// 请求因连接建立吃掉 3ms 预算而超时，业务照常但观测面没有记录，见 K-24）。
		log.Printf("proxy: 判定失败，按 NI-3 放行到业务：%s %s decision_id=%s 耗时=%s 原因=%v",
			r.Method, r.URL.Path, id, time.Since(started).Round(time.Microsecond), err)
		return judgev1.Action_ACTION_ORIGIN, "", err
	}
	return actionOf(resp), resp.GetBackend(), nil
}

// warmUp 触发 gRPC 建连并等待就绪（最多 wait）。
//
// 为什么需要它：判定调用有 3ms 预算（AR-29），而 gRPC 的建连发生在**第一次调用**上；
// 重启后第一条请求因此常被超时放行（业务没问题，但观测面缺那条记录 —— 见 K-24）。
// 预热把这段成本从"请求路径"挪到"启动路径"。返回非 nil 只作提示，调用方不应因此启动失败。
func warmUp(ctx context.Context, conn *grpc.ClientConn, wait time.Duration) error {
	conn.Connect()
	deadline := time.Now().Add(wait)
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("连接状态停在 %s（等待 %s 超时）", state, wait)
		}
		tick, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		conn.WaitForStateChange(tick, state)
		cancel()
	}
}

// ── 遥测上报 ─────────────────────────────────────────────────────────────────

func (h *Handler) enqueueEvent(r *http.Request, id string, act judgev1.Action, cause error) {
	if h.report == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"method": r.Method,
		"path":   r.URL.Path,
		"ua":     r.UserAgent(),
		"action": act.String(),
		"shadow": h.Shadow,
		// 判定失败的原因要留下 —— 否则运营看到的是「全是放行」而不知道核心挂了。
		"decision_error": errString(cause),
	})
	if err != nil {
		payload = nil
	}
	ev := &telemetryv1.TelemetryEvent{
		EventId:   id,
		EventType: "request_judged",
		Payload:   payload,
		CreatedAt: timestamppb.New(h.now()),
	}
	select {
	case h.events <- ev:
	default:
		h.dropped.Add(1)
	}
}

func (h *Handler) runReporter() {
	for {
		select {
		case <-h.done:
			return
		case ev := <-h.events:
			ctx, cancel := context.WithTimeout(h.ctx, reportTimeout)
			// 回执里的计数不在这里消费：它是给核心侧对账用的，
			// 适配器只关心「这次上报有没有成功」。
			if _, err := h.report.Report(ctx, ev); err != nil {
				log.Printf("proxy: 遥测上报失败：%v", err)
			}
			cancel()
		}
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// injectRules 把注入片段（已按 `;;` 拆分）映射成 edge/injection 的 Rule。
// 空片段 = 不注入。
func injectRules(snippets []string) []injection.Rule {
	var out []injection.Rule
	for _, s := range snippets {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, injection.Rule{Kind: "configured", Snippet: s})
		}
	}
	return out
}

// ── 后端 reverse_proxy 构造 ─────────────────────────────────────────────────

// buildBackend 构造一个指向 target 的 Caddy reverse_proxy handler。
//
// isMirage 决定失败处理方式：引流后端失败可以安全回落到业务（NI-1），
// 业务本身失败则如实报错 —— **不得**用蜜罐内容顶替业务失败（INT-8）。
// 引流侧另加响应头超时（蜜罐挂死不能拖住客户端），并挂注入 transport。
func (h *Handler) buildBackend(ctx caddy.Context, target string, isMirage bool, mirageTimeout time.Duration) (*reverseproxy.Handler, error) {
	dial, tlsUpstream, err := upstreamAddr(target)
	if err != nil {
		return nil, err
	}

	tr := &reverseproxy.HTTPTransport{
		DialTimeout: caddy.Duration(dialTimeout),
	}
	if tlsUpstream {
		// 上游是 https：启用到上游的 TLS（校验策略由 TLS 配置决定）。
		tr.TLS = new(reverseproxy.TLSConfig)
	}

	// 手工构造的 transport 需要自行 Provision（才会构建内部 http.Transport）。
	if err := tr.Provision(ctx); err != nil {
		return nil, err
	}

	var rt http.RoundTripper = tr
	if isMirage {
		// 引流侧加响应头超时：蜜罐挂死不能拖住客户端（NI-1）。
		// 超时后 reverse_proxy 返回错误 → forwardMirage 回落真实业务。
		to := mirageTimeout
		if to <= 0 {
			to = mirageDefaultTimeout
		}
		tr.ResponseHeaderTimeout = caddy.Duration(to)

		// 本地有注入规则，或策略面**可能**下发规则 → 都要挂注入 transport；
		// 具体用哪一份注入器由 `currentInjector()` 按请求决定（支持远端热变更）。
		if h.injector != nil || h.PolicyInterval > 0 {
			rt = &injectingTransport{handler: h, base: tr}
		}
	}

	rp := &reverseproxy.Handler{
		Upstreams: reverseproxy.UpstreamPool{{Dial: dial}},
		Transport: rt,
	}
	if err := rp.Provision(ctx); err != nil {
		return nil, err
	}
	return rp, nil
}

// upstreamAddr 解析 scheme://host:port 形式的地址，返回 Caddy reverse_proxy 的
// Dial 地址（host:port，无 scheme）与是否需要到上游的 TLS。
func upstreamAddr(raw string) (dial string, tlsUpstream bool, err error) {
	if strings.TrimSpace(raw) == "" {
		return "", false, fmt.Errorf("地址为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false, err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", false, fmt.Errorf("地址必须带 scheme 与 host：%s", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return u.Host, false, nil
	case "https":
		return u.Host, true, nil
	default:
		return "", false, fmt.Errorf("不支持的 scheme：%s", raw)
	}
}

// ── 注入 transport ──────────────────────────────────────────────────────────

// injectingTransport 包一层 RoundTripper，在**引流后端**的 HTML 响应上注入诱饵。
//
// INT-8：只改引流侧 —— 本 transport 只挂在 mirage 的 reverse_proxy 上，业务侧不经过它。
// 任何异常（非 HTML、读失败、超限）都**原样透传**：改写不是业务链路上的失败点。
type injectingTransport struct {
	// handler 而非注入器本身：注入规则可经**策略面**在运行期变（`applyEdgePolicy`），
	// 而 transport 是建后端的时刻就挂上的 —— 持注入器会把规则钉死在当时那一份。
	handler *Handler
	base    http.RoundTripper
}

func (t *injectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	// 每请求取一次当前注入器：本地 env 或策略面下发的（后者可热变更）。
	inj := t.handler.currentInjector()
	if resp == nil || inj == nil {
		return resp, nil
	}
	ct, ok := injectable(resp)
	if !ok {
		return resp, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxInjectBytes+1))
	if err != nil {
		// 读失败：把错误抛给 reverse_proxy（此时尚未向客户端写出任何字节，
		// forwardMirage 可安全回落业务）。
		_ = resp.Body.Close()
		return nil, err
	}
	if len(body) > maxInjectBytes {
		// 太大：把已读部分接回去，原样透传（不关闭原流，保持后续可读）。
		resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), resp.Body))
		return resp, nil
	}
	_ = resp.Body.Close()

	out, changed := inj.Inject(ct, body)
	if !changed {
		out = body
	}
	resp.Body = io.NopCloser(bytes.NewReader(out))
	resp.ContentLength = int64(len(out))
	resp.Header.Set("Content-Length", strconv.Itoa(len(out)))
	resp.Header.Del("Content-Encoding") // 已解出明文并改写，去掉压缩标记
	return resp, nil
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
}

func (s *headerSanitizer) WriteHeader(code int) {
	s.sanitize()
	s.ResponseWriter.WriteHeader(code)
}

func (s *headerSanitizer) Write(b []byte) (int, error) {
	s.sanitize()
	return s.ResponseWriter.Write(b)
}

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
