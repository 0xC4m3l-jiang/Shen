package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	judgev1 "shen/common/api/judge/v1"
	policyv1 "shen/common/api/policy/v1"
	telemetryv1 "shen/common/api/telemetry/v1"
	"shen/modules/deception/injection"
)

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

	// DegradedCacheTTL 是**判定失败**时的降级缓存预算（N2），0 = 默认（1s，且不超过 CacheTTL）。
	//
	// 为什么需要它：核心不可用时，若把失败也按正常 TTL 缓存，同键请求在故障恢复后还会
	// 继续用降级结果；若完全不缓存，则**每条请求**都要白等满判定预算（无界重试）。
	// 因此故障只给一个**独立的短预算**，并且命中它时事件照实记「降级来源」。
	DegradedCacheTTL caddy.Duration `json:"degraded_cache_ttl,omitempty"`

	// DecoyLease 是**诱饵路由撤销之后的墓碑租约**（N6），0 = 默认（24h）；负数 = 永久。
	//
	// 为什么需要它：一条诱饵路径一旦对外出现过，旧链接 / 爬虫 / 对手笔记都会继续用它。
	// 若「路由被删除」就立刻把归属还给业务，那些残留请求会**直接打到生产**上；
	// 租约期内在边缘继续用固定错误结束这些路径（不回生产），租约到期才释放归属。
	DecoyLease caddy.Duration `json:"decoy_lease,omitempty"`

	// BackendHealthInterval 是幻境后端的**健康探针间隔**（`enabled` 之外的第二个判据）。
	// 符号约定：`0` = 没配 ⇒ 用默认 30s；**负数 = 显式关闭**
	// （关掉后"健康"就等于 `enabled` —— 这是刻意可关的，所以不是"设 0 关掉"）。
	BackendHealthInterval caddy.Duration `json:"backend_health_interval,omitempty"`

	// BackendHealthTimeout 是单次探测超时（0 = 默认 1s）。
	BackendHealthTimeout caddy.Duration `json:"backend_health_timeout,omitempty"`

	// ForwardCredentials 为真时把请求的认证材料（Authorization / 业务会话 Cookie）**原样**
	// 转发给幻境后端。默认 **false**（剥离）—— 幻境不是业务，不该收到生产认证材料（W4）。
	ForwardCredentials bool `json:"forward_credentials,omitempty"`

	// CacheTTL 是本地判定缓存的存活时间。
	CacheTTL caddy.Duration `json:"cache_ttl,omitempty"`

	// Window 是 decision_id 的时间窗（ST-10）。
	Window caddy.Duration `json:"window,omitempty"`

	// SessionCookie 是业务自身的 session cookie 名（**必须与核心的 `session.cookie_name` 一致**）。
	//
	// 为什么要它：会话身份的口径必须两端一致 —— 适配器用它取出**最小身份值**（只这一个 cookie 的值），
	// 既用于 decision_id / 变体选择，也随观测送核心（`session_hint`）。空 = 用默认值 `sid`。
	SessionCookie string `json:"session_cookie,omitempty"`

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

	// InjectContent 是 **AI 欺骗内容的本地兜底开关**（`SHEN_PROXY_INJECT_CONTENT`，默认 false）。
	//
	// 它与策略载荷的 `inject_enabled` **取与**（`ADR-0023` 决定 4）：两者都为真才注入内容。
	// 默认 false 是刻意的：即使策略面说"开"，边缘也需要一次本地显式同意。
	InjectContent bool `json:"inject_content,omitempty"`

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

	// health 是幻境后端的活跃健康状态（`enabled` 不是健康：见 health.go）。
	health *backendHealth
	// healthWake 让「策略刚变」立刻触发一轮探测（缓冲 1：唤醒语义是"该看一眼了"）。
	healthWake chan struct{}

	// policy 是策略面客户端；nil = 未接策略面（单测与「不拉策略」的部署）。
	policy PolicyClient

	// buildRemote 把远端后端地址构造成 Caddy 后端。Provision 时接真实实现，
	// 单测注入替身（MD-22：模块测试禁止依赖其他模块与真实 Caddy）。
	buildRemote func(name, address string) (caddyhttp.MiddlewareHandler, error)

	cache *decisionCache

	// pins 是会话→变体槽位的钉定缓存（AI 内容注入用；轮换只对新会话生效，`ADR-0023`）。
	pins *variantPins

	// eventSeq 让逐请求事件的 id 唯一（同一 decision_id 的多条请求不再互相覆盖）。
	eventSeq atomic.Uint64

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
	// 接口 Injector 由本模块定义；这里接上 deception/injection 的纯变换实现。
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
	// 降级缓存预算（`N2`）：正数 = 用它｜0 = 默认 1s｜**负数 = 关闭降级缓存**。
	// 规则只在 `applyDegradedTTL` 一处实现（`Provision` 与单测脚手架共用）。
	h.cache.applyDegradedTTL(time.Duration(h.DegradedCacheTTL))
	// 会话钉定的 TTL 与判定缓存同窗：两者都是「可丢失的缓存」，过期重算得到同一结果（确定性）。
	h.pins = newVariantPins(time.Duration(h.CacheTTL), cacheCap, now)

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

	// 幻境后端健康：`enabled` 只说明"允许使用"，不说明"现在活着"（建议书 §8 第 7 项）。
	h.health = newBackendHealth()
	h.healthWake = make(chan struct{}, 1)
	// 符号约定（与字段注释一致）：负数 = **显式关闭**；0 = 没配 ⇒ 用默认间隔。
	if h.BackendHealthInterval >= 0 {
		interval := time.Duration(h.BackendHealthInterval)
		if interval == 0 {
			interval = defaultBackendHealthInterval
		}
		go h.runHealthProbes(h.ctx, interval)
	}

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
	started := time.Now()
	// **本请求固定使用的策略快照**（N1）：白名单 / 诱饵路由 / 后端表 / 注入规则全部取自同一份。
	//
	// 为什么必须在最前面取一次：策略面每 interval 换一次快照，若各个判断各自原子读，
	// 一个请求可能「白名单是旧的、后端表是新的」—— 这类撕裂在故障排查里几乎无法重现。
	st := h.remotePolicy()
	r = r.WithContext(withPolicySnapshot(r.Context(), st))
	// decision_id 在**最前面**就派生好：白名单命中也要上报逐判定事件（图上要能看见这条分支），
	// 而事件 id 就是 decision_id（幂等键，AR-11）。
	// 会话身份：由适配器按约定的 cookie 名取出**最小身份值**（整段 Cookie 不参与、也不跨接缝）。
	session := sessionHint(r, h.sessionCookieName())
	id := decisionID(session, clientIP(r, h.TrustXFF), r.URL.Path, time.Duration(h.Window), h.now())
	// 本地缓存的键**不是** decision_id：判定还看 method / Host / 查询串 / UA 与策略版本
	// （ST-10 只约束 decision_id 的派生，与本地缓存键分开 —— 见 cache.go 的 cacheKey）。
	key := cacheKey(id, r, h.policyRevision(st))

	// 注入结果的槽挂在请求上下文上：改写发生在 transport 里（改道侧），
	// 而上报发生在这里 —— 中间隔着 Caddy 的转发链，上下文是唯一不被它包一层的传递面。
	outcome := &injectOutcome{}
	r = r.WithContext(withInjectOutcome(r.Context(), outcome))
	// `R07`：把**入口算出的会话键**钉进上下文 —— 后面 `stripCredentials` 会摘掉业务 Cookie，
	// 而内容变体选择必须仍按"同一个会话"进行（否则剥离即塌缩）。
	r = r.WithContext(withSessionKey(r.Context(), session))

	// ⓪ 专属诱饵路由（方案 §9.2 / C01）：路径归属在**白名单与判定之前**。
	//
	// 为什么在最先：已登记的诱饵路径属于幻境，而「业务白名单不能把它送回 origin」
	// （§9.2 原话）—— 否则内部探针打到诱饵路径时会看到真站。
	// 影子模式下**不投递**（INT-11：只观测不处置），请求照常走后面的判定/白名单链路。
	if !h.Shadow {
		if route, ok := h.matchDecoy(st, r); ok {
			result, derr := h.deliverDecoy(st, sw, r, next, route)
			h.reportRoute(r, id, judgev1.Action_ACTION_MIRAGE,
				routeInfo{executed: executedDecoy, backend: route.backend, decoyResult: result, cause: derr}, sw, started)
			return derr
		}
	} else if h.LogRequests && len(decoyRoutesOf(st)) > 0 {
		if route, ok := h.matchDecoy(st, r); ok {
			log.Printf("proxy: 诱饵路由命中 %s（资产 %s）但当前是**影子模式** ⇒ 不投递（INT-11），照常走判定",
				r.URL.Path, route.id)
		}
	}

	// ① 白名单先于改道判定（INT-25）—— 命中则不调核心，直接透传。
	// 本地白名单（env）与远端白名单（策略面）取**并集**：护栏只增不减（见 applyEdgePolicy）。
	ip := clientIP(r, h.TrustXFF)
	if whitelisted(ip, h.whitelist) || h.remoteWhitelisted(st, ip) {
		ferr := h.forward(st, sw, r, next, targetOrigin)
		h.reportRoute(r, id, judgev1.Action_ACTION_ORIGIN, routeInfo{executed: executedWhitelist}, sw, started)
		return ferr
	}

	// ② 本地判定缓存（AR-6 第 2 件事）：命中则不调核心，按缓存结果处置。
	if ent, ok := h.cache.lookup(key); ok {
		if ent.degraded {
			// **降级缓存命中**（N2）：上一轮判定失败过，这里仍然是「引擎没能判定」。
			// 照实记 failopen + 降级来源 —— 不能因为缓存命中就伪装成一次正常判定。
			ferr := h.forward(st, sw, r, next, targetOrigin)
			h.reportRoute(r, id, ent.action,
				routeInfo{executed: executedFailOpen, cause: errDegradedCache}, sw, started)
			return ferr
		}
		executed, derr := h.dispatch(st, sw, r, next, ent.action, ent.backend)
		// 落点记 `cache`（“未重新判定”）；图按 action/backend 归到意图分支上。
		h.reportRoute(r, id, ent.action, routeInfo{executed: executedCache, backend: ent.backend, dispatched: executed}, sw, started)
		return derr
	}

	// ③ 调核心判定。任何失败都已折叠成「放行」（NI-3 / NI-4 / NI-5）。
	act, backend, err := h.decide(r, id)

	// ④ 按结果路由 → 再异步上报（AR-6 第 3 / 4 件事）。
	if err != nil {
		// 判定失败：放行到业务（NI-3），落点记 failopen —— 这是“为什么没判”在图上的唯一痕迹。
		//
		// **不写正常缓存**（N2）：降级结果有独立的短预算，到期后同键请求会重新调核心
		// （恢复重判）；正常 TTL 会让故障期的旧结果在核心恢复后继续生效。
		h.cache.putDegraded(key, act, backend)
		ferr := h.forward(st, sw, r, next, targetOrigin)
		h.reportRoute(r, id, act, routeInfo{executed: executedFailOpen, cause: err}, sw, started)
		return ferr
	}
	h.cache.put(key, act, backend)
	executed, derr := h.dispatch(st, sw, r, next, act, backend)
	h.reportRoute(r, id, act, routeInfo{executed: executed, backend: backend}, sw, started)
	return derr
}

// sessionCookieName 返回生效的 session cookie 名（空 = 默认 `sid`）。
//
// 默认值与核心的 `config.example.yaml`（`session.cookie_name: sid`）一致：
// 两边不一致时，`session_hint` 会取不到值、会话身份静默退化到 TLS/IP 指纹 ——
// 因此核心启动日志会打印它用的名字，便于对账。
func (h *Handler) sessionCookieName() string {
	if v := strings.TrimSpace(h.SessionCookie); v != "" {
		return v
	}
	return DefaultSessionCookie
}

// matchDecoy 在**该快照的**诱饵路由表里做最长匹配（段边界 + 归一化，见 decoyroute.go）。
//
// 匹配范围包含**活路由与搜索碑**（N6）：搜索碑路径在租约期内仍不属于业务。
func (h *Handler) matchDecoy(st *remoteState, r *http.Request) (policyDecoyRoute, bool) {
	return matchDecoyRoute(decoyRoutesOf(st), r.URL.Path, r.Host)
}

// decoyRoutesOf 是匹配用的路由表：活路由 + 未被覆盖的搜索碑（活路由优先，见 applyEdgePolicy）。
func decoyRoutesOf(st *remoteState) []policyDecoyRoute {
	if st == nil {
		return nil
	}
	if len(st.tombstones) == 0 {
		return st.decoys
	}
	all := make([]policyDecoyRoute, 0, len(st.decoys)+len(st.tombstones))
	all = append(all, st.decoys...)
	all = append(all, st.tombstones...)
	return all
}

// withPolicySnapshot 把「本请求固定使用的策略快照」放进请求上下文（N1）。
//
// 改写发生在 transport 里（Caddy 转发链内部），它拿不到 ServeHTTP 的局部变量，
// 因此快照必须能经请求上下文抵达 —— 否则注入规则可能与路由/后端来自不同版本。
func withPolicySnapshot(ctx context.Context, st *remoteState) context.Context {
	return context.WithValue(ctx, policySnapshotKey{}, st)
}

// policySnapshotOf 取请求上钉定的快照；没有（单测直接调 transport / 非请求路径）时回落到当前值。
func (h *Handler) policySnapshotOf(r *http.Request) *remoteState {
	if r != nil {
		if st, ok := r.Context().Value(policySnapshotKey{}).(*remoteState); ok {
			return st
		}
	}
	return h.remotePolicy()
}

// policySnapshotKey 是请求上下文里的键（未导出类型：只有本包能写）。
type policySnapshotKey struct{}

// deliverDecoy 把请求投递到诱饵后端，并回答「投递发生了什么」（W8）。
//
// **故障绝不回生产**（方案 §9.2）：后端不可用/已撤销时返回固定 502，而不是回落到业务 ——
// 已登记为诱饵的请求回源，既会让对手看见真站，也可能把非幂等操作重复执行。
// 它同时是「专属诱饵路由上的非幂等重放」这个 P0 边界的落地点（Q2 的中间路线）。
//
// 返回的 result 是事件里的 `delivery_result`：`matched`/`delivered`/`backend_unavailable`/
// `delivery_failed`/`tombstoned`。只看 `executed=decoy` 分不出「投出去了」与「后端没了」。
func (h *Handler) deliverDecoy(st *remoteState, w http.ResponseWriter, r *http.Request, next caddyhttp.Handler, route policyDecoyRoute) (string, error) {
	if reason := h.healthReason(route.backend); reason != "" {
		// 探针判定后端不可达（**不是**配置问题）：仍固定 502、绝不回生产（§9.2），
		// 但结果值不同 —— 运维动作也不同（修后端 vs 改配置）。
		log.Printf("proxy: 诱饵 %s（路径 %s）的后端 %q 探针判定不健康 ⇒ 固定 502（不回生产）：%s",
			route.id, r.URL.Path, route.backend, reason)
		w.WriteHeader(http.StatusBadGateway)
		return backendUnhealthy, nil
	}
	if route.revoked {
		// 搜索碑（N6）：这条路径曾被登记为诱饵，现已撤销但租约未到期。
		// 它仍**不属于业务** —— 残留链接 / 扫描器继续拿到固定错误，而不是突然看到真站。
		log.Printf("proxy: 诱饵 %s（路径 %s）已撤销，租约内按固定 502 结束（不到业务）", route.id, r.URL.Path)
		w.WriteHeader(http.StatusBadGateway)
		return decoyRevoked, nil
	}
	rp, ok := h.mirageHandler(st, route.backend)
	if !ok {
		// 后端未登记 / 未启用 / 已被撤销：固定错误（不回生产）。
		log.Printf("proxy: 诱饵 %s（路径 %s）的后端 %q 不可用 ⇒ 固定 502，不回生产（§9.2）",
			route.id, r.URL.Path, route.backend)
		w.WriteHeader(http.StatusBadGateway)
		return decoyBackendUnavailable, nil
	}
	// 凭证边界（W4）：幻境不是业务，默认**不把**生产认证材料带过去（Authorization / 业务会话 Cookie）。
	h.stripCredentials(r)
	if err := rp.ServeHTTP(w, r, next); err != nil {
		// 后端中途失败也**不回生产**：调用方按错误处理（Caddy 错误路由），客户端看到 5xx。
		log.Printf("proxy: 诱饵 %s 的后端 %q 投递失败：%v（不回生产，§9.2）", route.id, route.backend, err)
		return decoyDeliveryFailed, err
	}
	return decoyDelivered, nil
}

// stripCredentials 在投递到幻境之前剥离**生产认证材料**（W4）。
//
// 剔哪些：`Authorization` / `Proxy-Authorization` 与**业务会话 cookie**（按配置的 cookie 名）。
// 不剔什么：其余 Cookie 要留着 —— 幻境自己的会话 cookie（如 `atlas_session`）是诱饵体验的一部分。
// `ForwardCredentials` 为真时不剔（默认关闭：幻境不是业务，不该拿到能冒充用户的材料）。
func (h *Handler) stripCredentials(r *http.Request) {
	if h.ForwardCredentials {
		return
	}
	r.Header.Del("Authorization")
	r.Header.Del("Proxy-Authorization")
	name := h.sessionCookieName()
	if name == "" {
		return
	}
	cookies := r.Cookies()
	if len(cookies) == 0 {
		return
	}
	kept := cookies[:0]
	stripped := 0
	for _, c := range cookies {
		if c.Name == name {
			stripped++
			continue
		}
		kept = append(kept, c)
	}
	if stripped == 0 {
		return
	}
	r.Header.Del("Cookie")
	for _, c := range kept {
		r.AddCookie(c) // 逐个加回（AddCookie 会自己拼 Cookie 头）
	}
}

// dispatch 按决策结果选择路径。
//
// Shadow 为真时**永不改道、永不拦截**（INT-11：首次上线必须影子模式）；
// 决策照算、照上报，只是不执行。
func (h *Handler) dispatch(st *remoteState, w http.ResponseWriter, r *http.Request, next caddyhttp.Handler, act judgev1.Action, backend string) (string, error) {
	if h.Shadow {
		return executedFor(true, act, false, false), h.forward(st, w, r, next, targetOrigin)
	}
	switch act {
	case judgev1.Action_ACTION_MIRAGE:
		// 后端名查不到一律回落业务（NI-5）—— 宁可漏改道，不可断业务。
		if _, ok := h.mirageHandler(st, backend); !ok {
			return executedFor(false, act, false, false), h.forward(st, w, r, next, targetOrigin)
		}
		fellBack, err := h.forwardMirage(st, w, r, next, backend)
		return executedFor(false, act, true, fellBack), err
	case judgev1.Action_ACTION_BLOCK:
		// 403 是对手可见的处置 —— 这是**已承认的设计**（ADR-0002：引擎是欺骗调度器，
		// 不是 WAF，可见拦截交接入层）。block 只用于「明确拒绝已知恶意」，不用于透明误导；
		// route_mirage 才是本项目的核心价值。
		w.WriteHeader(http.StatusForbidden)
		return executedFor(false, act, false, false), nil
	default:
		// ACTION_ORIGIN 与任何未识别取值都放行。
		return executedFor(false, act, false, false), h.forward(st, w, r, next, targetOrigin)
	}
}

// forward 把请求交给指定后端的 Caddy reverse_proxy。
func (h *Handler) forward(st *remoteState, w http.ResponseWriter, r *http.Request, next caddyhttp.Handler, target string) error {
	if target != targetOrigin {
		rp, ok := h.mirageHandler(st, target)
		if !ok {
			return h.forward(st, w, r, next, targetOrigin)
		}
		h.stripCredentials(r) // W4：幻境默认拿不到生产认证材料
		return rp.ServeHTTP(w, r, next)
	}
	return h.origin.ServeHTTP(w, r, next)
}

// mirageHandler 按逻辑名找改道后端：**远端策略优先，本地配置兜底**（ADR-0018）。
//
// 远端优先：策略面是 ST-24（策略是数据）的正式通路；
// 本地兜底：策略面拉不到时边缘照常工作（NI-1）。
func (h *Handler) mirageHandler(st *remoteState, name string) (caddyhttp.MiddlewareHandler, bool) {
	if st != nil {
		if rp, ok := st.backends[name]; ok {
			return rp, true
		}
		// 远端**声明过**这个名字但没给出可用后端（`enabled: false` 或地址坏）⇒ 视为**已撤销**：
		// 此时**不得**回落到本地同名项（方案 §9.2「撤销优先于本地兜底」）。
		// 否则「核心禁用了某个后端」会变成「本地 env 里还有，于是照旧改道过去」——
		// 禁用指令失效，且没有任何可见信号（D06 的 P0 边界风险）。
		if _, declared := st.declared[name]; declared {
			return nil, false
		}
	}
	// 远端没声明过的名字仍走本地配置兜底：这是 ADR-0018 的既定语义
	// （远端覆盖本地；本地只在策略面没谈及该名字时兜底）。
	rp, ok := h.mirage[name]
	return rp, ok
}

// forwardMirage 把请求交给引流后端；引流后端失败且尚未写出任何字节时，
// 回落到业务真实地址（NI-1 —— 引流失败绝不能变成业务失败）。
//
// 用 trackingWriter 记录「是否已写出字节」：没写过 → 可安全重放到业务；
// 写过 → 只能把错误如实抛出（由 Caddy 错误路由处理），否则会输出半截响应。
func (h *Handler) forwardMirage(st *remoteState, w http.ResponseWriter, r *http.Request, next caddyhttp.Handler, backend string) (bool, error) {
	if reason := h.healthReason(backend); reason != "" {
		// 评分改道：后端探针判定不可达 ⇒ **回落业务**（`NI-1`：引流失败绝不能变成业务失败）。
		// 与"转发失败后回落"同一条语义，只是不必先吃满一次连接失败。
		log.Printf("proxy: 幻境后端 %q 探针判定不健康，改道回落业务：%s", backend, reason)
		return true, h.origin.ServeHTTP(w, r, next)
	}
	rp, ok := h.mirageHandler(st, backend)
	if !ok {
		// 理论上到不了这里（dispatch 已查过表）；真到了就回落业务，不报 500。
		return true, h.origin.ServeHTTP(w, r, next)
	}
	tw := &trackingWriter{ResponseWriter: w}
	// **凭证边界必须覆盖每一条送达幻境的路径**（`R04`，严重）：
	// 专属诱饵路由（`deliverDecoy`）与本地 env 后端（`forward`）此前已剥离，只有这里漏了 ——
	// 于是"评分改道"会把生产 `Authorization` / 业务会话 Cookie 原样交给幻境后端。
	//
	// 为什么用**克隆**而不是直接改 `r`：本函数还有一条"后端不可达 ⇒ 回落业务"的路径（`NI-1`），
	// 回落用的必须是**凭证完整**的原请求。直接剥离 `r` 会让回落后的业务请求丢掉认证材料 ——
	// 那就把"少给幻境一点"变成了"业务被登出"。克隆是浅拷贝（`Body` 共享，见 Go 文档），
	// 对 GET 无影响；POST 的 body 本就不可重放（既有边界，非本处引入）。
	mreq := r.Clone(r.Context())
	h.stripCredentials(mreq)
	if err := rp.ServeHTTP(tw, mreq, next); err != nil {
		if !tw.wrote {
			log.Printf("proxy: 引流后端不可达，回落业务：%v", err)
			return true, h.origin.ServeHTTP(w, r, next)
		}
		log.Printf("proxy: 引流后端中途失败：%v", err)
		return false, err
	}
	return false, nil
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
		Observed:   observationFrom(r, h.TrustXFF, h.sessionCookieName()),
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

// executed 的取值（唯一权威表见 docs/spec/events.md §2.2 与 docs/modules/adapter-proxy.md）。
const (
	executedWhitelist = "whitelist"       // 白名单命中，未调核心（INT-25）
	executedCache     = "cache"           // 本地判定缓存命中，未调核心（ST-10）
	executedFailOpen  = "failopen"        // 调核心失败，按 NI-3 放行
	executedOrigin    = "origin"          // route_origin（含影子模式下的一切处置）
	executedFallback  = "origin_fallback" // route_mirage 但后端不可用 ⇒ 回落源站（NI-5）
	executedMirage    = "mirage"          // route_mirage 且成功转发到幻境后端
	executedBlock     = "block"           // block，返回 403
	executedDecoy     = "decoy"           // 专属诱饵路由命中（未调核心；§9.2）
)

// `delivery_result` 的取值：把「匹配到诱饵路由」与「真的投递出去了」分开（W8）。
//
// 为什么需要：`executed=decoy` 在「后端不可用 ⇒ 固定 502」与「成功投递」时**取值相同**，
// 只看它会把失败当成投递成功（图上分不出「诱饵在服务」与「诱饵早坏了」）。
const (
	decoyDelivered          = "delivered"           // 已交给诱饵后端（最终状态看 status）
	decoyBackendUnavailable = "backend_unavailable" // 后端未登记 / 未启用 / 已撤销 ⇒ 固定 502
	decoyDeliveryFailed     = "delivery_failed"     // 后端中途失败（不回生产）
	decoyRevoked            = "tombstoned"          // 路由已撤销，租约内固定 502（N6）
)

// errDegradedCache 是「命中降级缓存」的原因。它必须与真正的判定失败区分开：
// 前者是「上一轮失败过，这一轮没再调核心」，后者是「这一轮调了，失败了」。
var errDegradedCache = errors.New("命中判定降级缓存（上一轮判定失败，未重新调用核心）")

// executedFor 决定「实际落点」的取值 —— 这是图上区分「核心判成什么」与「实际走了哪」的唯一依据。
//
// 纯函数（不碰任何依赖），因此可以穷举测试；`shadow` 优先于一切（影子模式只观测）。
func executedFor(shadow bool, act judgev1.Action, mirageFound bool, mirageFellBack bool) string {
	if shadow {
		return executedOrigin
	}
	switch act {
	case judgev1.Action_ACTION_MIRAGE:
		if !mirageFound || mirageFellBack {
			return executedFallback
		}
		return executedMirage
	case judgev1.Action_ACTION_BLOCK:
		return executedBlock
	default:
		return executedOrigin
	}
}

// routeInfo 是一次请求的「执行结果」，也是逐判定事件的载荷来源。
type routeInfo struct {
	executed   string  // 见 executed* 常量
	backend    string  // 实际使用的幻境后端名（仅 mirage 时非空）
	dispatched string  // 仅缓存命中时使用：缓存决策实际走到的落点
	status     int     // 返回给客户端的状态码（在 reportRoute 里补齐）
	bytes      int     // 响应体字节数（在 reportRoute 里补齐）
	durationMs float64 // 从进入中间件到响应结束（在 reportRoute 里补齐）
	cause      error   // 判定失败原因（非 nil 时必记 warn 日志）
	inject     string  // 注入结果（见 Inject* 常量；空 = 未涉及）
	contentID  string  // 实际注入的内容标识（空 = 未注入）
	// decoyResult 仅专属诱饵路由使用（W8）：`delivered` / `backend_unavailable` /
	// `delivery_failed` / `tombstoned`。空 = 本请求不是诱饵路由请求。
	decoyResult string
}

// reportRoute 收尾：补全响应观测 → 记日志（失败必记；逐请求按开关）→ 异步上报逐判定事件。
func (h *Handler) reportRoute(r *http.Request, id string, act judgev1.Action, info routeInfo, sw *headerSanitizer, started time.Time) {
	info.status = sw.statusCode()
	info.bytes = sw.bytesWritten()
	info.durationMs = float64(time.Since(started).Microseconds()) / 1000.0
	// 注入结果由改道侧的 transport 写进请求上下文（此处读取）。
	if info.inject == "" {
		info.inject, info.contentID = injectResultOf(r)
	}

	// 逐请求事件里的两个新键（`docs/spec/events.md` §2.2）：注入结果与内容标识。
	// 它们与 `executed` **相互独立**：executed 说"去了哪"，inject 说"我们改写了多少"。
	if info.inject == "" {
		if info.executed == executedMirage {
			// 走了改道侧但没有任何注入记录（例：响应根本不是可改写的 HTTP 响应）
			// ⇒ 如实报「没有可用内容」，而不是让运营以为注入"应该发生但没发生"。
			info.inject = InjectNoContent
		} else {
			info.inject = InjectOff
		}
	}

	if info.cause != nil {
		// 判定失败是「引擎没能判定」的唯一线索，**不受 SHEN_PROXY_LOG_REQUESTS 控制**（K-24 踩过）。
		log.Printf("proxy: 判定失败，按 NI-3 放行到业务：%s %s decision_id=%s 耗时=%.1fms 原因=%v",
			r.Method, r.URL.Path, id, info.durationMs, info.cause)
	} else if h.LogRequests {
		// 逐请求的主线索：判定意图 + 实际落点 + 返回信息（图/手册都按这四个维度看）。
		log.Printf("proxy: 路由：%s %s decision_id=%s 判定=%s 落点=%s（后端 %q）状态=%d 字节=%d 耗时=%.1fms",
			r.Method, r.URL.Path, id, actionName(act), info.executed, info.backend,
			info.status, info.bytes, info.durationMs)
	}
	h.enqueueEvent(r, id, act, info)
}

// ── 遥测上报 ─────────────────────────────────────────────────────────────────

// judgedEventPayload 构造 `request_judged` 事件的载荷。
//
// 字段表是**跨语言契约**（docs/spec/events.md §2.2）：夹具
// api/telemetry/v1/testdata/request_judged_event.json 与契约测试都读它，改键必须同步三处。
func (h *Handler) judgedEventPayload(id string, r *http.Request, act judgev1.Action, info routeInfo) map[string]any {
	// 注入结果**必须是四个登记值之一**：调用方没设时按「未涉及」上报 ——
	// 空串会让契约的读取方（控制台、脚本）多出一个未登记取值。
	inject, contentID := info.inject, info.contentID
	if inject == "" {
		inject, contentID = InjectOff, ""
	}
	return map[string]any{
		// decision_id 放在**载荷里**（事件 id 改为逐请求唯一，见 enqueueEvent）：控制台靠它 join 核心判定。
		"decision_id": id,
		"method":      r.Method,
		"path":        r.URL.Path,
		"ua":          r.UserAgent(),
		"action":      act.String(),
		"shadow":      h.Shadow,
		// 判定失败的原因要留下 —— 否则运营看到的是「全是放行」而不知道核心挂了。
		"decision_error": errString(info.cause),
		// 以下为「实际落点 + 返回信息」（图与验证页靠它们）：
		"executed":    info.executed,
		"backend":     info.backend,
		"status":      info.status,
		"bytes":       info.bytes,
		"duration_ms": info.durationMs,
		// 诱饵投递结果（W8）。降级来源不另开键：它已经在 `decision_error` 里（N2）。
		"delivery_result": info.decoyResult,
		// AI 欺骗内容的注入结果（`ADR-0023`）：
		"inject":     inject,
		"content_id": contentID,
	}
}

func (h *Handler) enqueueEvent(r *http.Request, id string, act judgev1.Action, info routeInfo) {
	if h.report == nil {
		return
	}
	payload, err := json.Marshal(h.judgedEventPayload(id, r, act, info))
	if err != nil {
		payload = nil
	}
	// 事件 id **逐请求唯一**（而不是等于 decision_id）：判定缓存命中的请求会共享同一个 decision_id，
	// 若用 decision_id 当事件 id，它们会被遥测的幂等键（AR-11）折叠成一条 —— 图上就看不到"每条流量"了。
	seq := h.eventSeq.Add(1)
	ev := &telemetryv1.TelemetryEvent{
		EventId:   fmt.Sprintf("judged:%s:%d", id, seq),
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

// injectRules 把注入片段（已按 `;;` 拆分）映射成 deception/injection 的 Rule。
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
