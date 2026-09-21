package proxy

import (
	"context"
	"net/netip"
	"time"

	"google.golang.org/grpc"

	judgev1 "shen/common/api/judge/v1"
	telemetryv1 "shen/common/api/telemetry/v1"
)

// JudgeClient 是本端对核心判定面的依赖。
// 生成的 DeceptionJudgeClient 天然满足它；单测用替身。
type JudgeClient interface {
	Judge(ctx context.Context, in *judgev1.JudgeRequest, opts ...grpc.CallOption) (*judgev1.JudgeResponse, error)
}

// TelemetryClient 是本端对核心遥测面的依赖。
type TelemetryClient interface {
	Report(ctx context.Context, in *telemetryv1.TelemetryEvent, opts ...grpc.CallOption) (*telemetryv1.ReportAck, error)
}

// Injector 是本端对「响应改写」的依赖。
//
// 接口由**消费方（本模块）**定义，便于单测用替身（`MD-22`）；
// 生产实现是 `deception/injection`（L1 处置模块，`ST-5` 要求它**被适配器引用、不独立部署**）。
// 注意 `MD-4` 禁止的是**适配器之间**互相依赖（`adapter-*` 三个之间）；
// 处置模块不属适配器，因此 `handler.go` 的 `Provision` 里直接 import 它是合规的。
type Injector interface {
	// Inject 把诱饵注入响应体；返回改写后的体与是否真的改了。
	Inject(contentType string, body []byte) ([]byte, bool)
}

// Config 是本模块的全部可调项。
//
// 它**只从环境变量构造**（见 cmd/proxy），不引入配置解析库 ——
// 一是适配器要能塞进最小镜像，二是这层不该有自己的配置文件格式。
type Config struct {
	// Upstream 是业务真实地址，例如 http://127.0.0.1:9000。
	Upstream string

	// Mirage 是引流后端表：逻辑名 -> 地址。核心在 route_mirage 时给出名字，
	// 本模块据此查表。名字查不到一律回落业务（NI-5）。
	Mirage map[string]string

	// Whitelist 是免判定的来源网段（内部 IP / 健康检查 / 监控探针）。
	// 它**必须先于**引流判定生效（INT-25）。
	Whitelist []netip.Prefix

	// DecisionTimeout 是对核心调用的硬超时（NI-4）。超时即放行。
	DecisionTimeout time.Duration

	// CacheTTL 是本地判定缓存的存活时间。
	CacheTTL time.Duration

	// Window 是 decision_id 的时间窗。同一窗口内同一次请求得到同一个 ID（ST-10）。
	Window time.Duration

	// Shadow 为真时**只观测、不处置**：照算判定并上报，但永不改道、永不拦截。
	// 首次上线**必须**为真（INT-11），因此默认值也是真。
	Shadow bool

	// TrustXFF 决定是否信任 X-Forwarded-For 取客户端 IP（INT-23）。
	TrustXFF bool

	// ReportQueue 是遥测上报的缓冲深度。满了就丢并计数 ——
	// 宁可丢事件，也不能让上报拖慢请求或被上游抖动放大。
	ReportQueue int

	// CacheMaxEntries 是本地判定缓存的最大条目数（MD-10：缓存必须有容量上限）。
	// 0 表示用默认值。满则整体清空 —— 缓存本就可丢失（未命中重调核心，语义等价），
	// 清空比逐条淘汰简单，且能抗「大量不同路径填满缓存」的对抗性填满。
	CacheMaxEntries int

	// Now 注入时钟，使 decision_id 可复现、可测。为空时用 time.Now。
	Now func() time.Time

	// Injector 在**引流后端**的 HTML 响应上注入诱饵。为空则不注入。
	//
	// 改写**只能**作用于引流侧（INT-8：禁止改写业务侧响应），
	// 且只改小页面（大响应原样流式透传）。
	Injector Injector

	// MirageResponseTimeout 是**引流后端**的响应头超时。0 时用默认值。
	//
	// 蜜罐挂死不能拖住客户端（NI-1）—— 超时后回落真实业务。
	// 业务侧**不设**此超时：慢接口是业务自己的行为。
	MirageResponseTimeout time.Duration
}
