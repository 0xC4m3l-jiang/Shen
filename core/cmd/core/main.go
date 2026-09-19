// Command core 是核心进程入口。
//
// 阶段 2a：策略不再是空规则集，决策路径由 director 交付。启动时从配置装载策略
// （规则 + 阈值 + 灰度 + 影子开关）；影子模式（默认）仍由 ShadowDecider 顶替，
// 关闭影子后才用 director 产出真实三值。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"

	judgev1 "shen/api/judge/v1"
	policyv1 "shen/api/policy/v1"
	telemetryv1 "shen/api/telemetry/v1"
	"shen/core/internal/contract"
	"shen/core/internal/control"
	"shen/core/internal/decoy"
	"shen/core/internal/director"
	"shen/core/internal/honeypot"
	"shen/core/internal/isolation"
	"shen/core/internal/judge"
	"shen/core/internal/policy"
	"shen/core/internal/responder"
	"shen/core/internal/session"
	"shen/core/internal/store"
	"shen/core/internal/telemetry"
)

const (
	defaultListen = "127.0.0.1:9443"

	// exampleConfig 是配置缺失时的提示路径。
	// 不设为缺省值：默认路径会让进程静默读到一份陈旧配置。
	exampleConfig = "deploy/config/config.example.yaml"
)

func main() {
	flag.Parse()
	if err := run(); err != nil {
		log.Fatalf("core: %v", err)
	}
}

// checkConfig 是开发期的配置干跑开关：只装载并校验配置、打印策略摘要，不开端口。
// 它让「配置对不对」成为一条命令（make check-config），不必把进程真起起来。
var checkConfig = flag.Bool("check-config", false, "只装载并校验配置，打印策略摘要后退出")

func run() error {
	addr := os.Getenv("SHEN_LISTEN")
	if addr == "" {
		addr = defaultListen
	}

	// store 是核心唯一的 I/O 出口：其他模块禁止直连 Redis / ClickHouse / PostgreSQL。
	// 本期（只观察）用内存实现；真实存储接入时换实现。
	stores := store.NewMemStores(nil)

	// 策略是数据，不是编译进代码的分支。
	//
	// 启动时装载一次：非法配置让进程启动失败，而不是带着坏配置跑（docs/spec/config.md §1）。
	loader, err := loadPolicy()
	if err != nil {
		return err
	}

	ctx := context.Background()
	snap, err := loader.Snapshot(ctx)
	if err != nil {
		return err
	}
	printPolicy(snap)

	if *checkConfig {
		return nil
	}

	if err := loader.Publish(ctx, stores.Policy); err != nil {
		return err
	}

	engine := judge.New(loader)

	// 欺骗面（阶段 2b）：诱饵 / 后端池 / 响应 / 隔离 —— 构造与自检集中在 assembleDeception。
	surf, err := assembleDeception(ctx, loader, stores)
	if err != nil {
		return err
	}
	printDeception(ctx, surf.surface, surf.pool)

	// CookieName 是业务自身的 session cookie 名（会话身份三级优先级的第 ① 级）。
	sess := session.New("sid")
	collector := telemetry.New(stores.Event)

	// 决策路径（阶段 2a）：影子模式用 ShadowDecider（恒放行，只算不处置）；
	// 关闭影子后才用 director 产出真实三值（阈值 → 三值 + 灰度收敛）。
	var decider control.Decider
	if loader.Shadow() {
		decider = control.NewShadowDecider(engine)
	} else {
		// MD-25 / INT-25：把诱饵前缀集与白名单交给决策层 ——
		// 使「诱饵面不被阻断」与「白名单先于引流判定」成为代码而非口头约定。
		wl, werr := loader.Whitelist(ctx)
		if werr != nil {
			return werr
		}
		dir, derr := director.New(engine, loader, loader, director.Config{
			DecoyPrefixes: decoyPrefixes(surf.assets),
			Whitelist:     wl,
		})
		if derr != nil {
			return derr
		}
		decider = dir
	}

	// 明文 gRPC 只允许回环监听：跨节点部署**必须**换 mTLS（见 docs/design/structure.md §4）。
	// 这里故意**不提供**开关：能通过开关开启的不安全部署，总会有人开启。
	if err := assertPlaintextListenIsLocal(addr); err != nil {
		return err
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	// nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection
	// 已接受项（非缺陷）：明文 gRPC **只允许本机**，上面 assertPlaintextListenIsLocal 已做失败关闭；
	// 跨节点部署必须换 mTLS（见 docs/design/structure.md §4）。
	srv := grpc.NewServer()
	// 本期监听 127.0.0.1，明文 gRPC 可接受：TLS 在接入层（Envoy/Nginx）终结，
	// 且 S1 只在同机内走（适配器与核心同主机）。
	// 若改为跨节点部署，必须换成 mTLS（见 docs/design/structure.md §4 的部署形态对比）。
	//
	// NI-10：熔断落在服务面（看到全部请求），错误率超阈值时自动纯放行。
	breaker := control.NewBreaker(control.BreakerConfig{})
	// 观测面（写侧 + 读侧）：控制台要能回答「这个请求为什么被判成这样、然后去了哪」。
	// 写侧把每次判定同时落成**事件**（供 UI 直接读）与**判定记录**（数据模型要求，structure.md §3）。
	observer := decisionRecorder{events: collector, decisions: stores.Decision, logger: newDecisionLogger()}
	judgev1.RegisterDeceptionJudgeServer(srv,
		control.NewJudgeService(decider, sess,
			control.WithBreaker(breaker),
			control.WithIsolation(surf.isolate),
			control.WithDecisionRecorder(observer)))
	telemetryv1.RegisterDeceptionTelemetryServer(srv,
		control.NewTelemetryServiceWith(collector, control.WithEventLister(eventLister{events: stores.Event})))
	// 策略面（S4）：把当前策略版本投影后下发给适配器，并接收它们的回执（AR-13 / ST-8）。
	// 服务端落在 policy 模块（它持有快照与校验和）—— 不新增模块，见 ADR-0018。
	policyv1.RegisterDeceptionPolicyServer(srv, policy.NewServer(loader, stores.Policy))

	// 优雅退出：收到信号后停止接收新请求，给在途请求留出时间。
	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	mode := "接管模式（产出真实三值决策）"
	if loader.Shadow() {
		mode = "影子模式（只算判定、不处置，INT-11）"
	}
	log.Printf("核心已启动（%s）：%s", mode, addr)

	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		log.Printf("收到 %s，正在退出", sig)
	}

	stopped := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		srv.Stop() // 宽限期内未退出则强制停止
	}

	return nil
}

// deception 是装配好的**欺骗面**（阶段 2b）。
//
// 它把「核心自己持有、但暂未对外暴露」的模块收在一处，使 run() 只看到一条装配线；
// assets 留着是因为 MD-25 的诱饵前缀集要从**同一份数据**汇总（单一事实源）。
type deception struct {
	surface decoy.Surface
	pool    honeypot.Pool
	isolate isolation.Isolation
	assets  []contract.DecoyAsset
}

// assembleDeception 构造欺骗面的四个模块，并做启动期一致性自检。
//
// 它们都是**数据驱动**的（ST-24）：诱饵资产与幻境后端池来自配置，
// 资产与预生成内容经 store（唯一 I/O 出口，MD-20）。
// 本函数是**唯一**把具体实现拼起来的地方 —— 各模块自身只依赖接口。
func assembleDeception(ctx context.Context, loader *policy.Loader, stores *store.MemStores) (*deception, error) {
	// 诱饵面：把配置里的资产写入存储，供匹配、多态与投放片段生成。
	assets, err := loader.Decoys(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range assets {
		if err := stores.Decoy.Put(ctx, a); err != nil {
			return nil, err
		}
	}
	surface, err := decoy.New(stores.Decoy, 0)
	if err != nil {
		return nil, err
	}

	// 幻境后端池：类型注册 + config 开关；**不实现具体蜜罐**（ADR-0011）。
	backends, err := loader.Honeypots(ctx)
	if err != nil {
		return nil, err
	}
	pool, err := honeypot.New(backends, nil)
	if err != nil {
		return nil, err
	}

	// 响应生成：一致性不变量（AR-30）；高保真内容由 L4 离线预生成落库。
	respond, err := responder.New(stores.Content)
	if err != nil {
		return nil, err
	}
	if err := assertConsistency(ctx, respond); err != nil {
		return nil, err
	}

	// 隔离：命中则不再调用决策层，客户端**不可见**。
	iso, err := isolation.New(stores.Isolation, nil)
	if err != nil {
		return nil, err
	}

	return &deception{surface: surface, pool: pool, isolate: iso, assets: assets}, nil
}

// assertConsistency 验证 AR-30：同一输入两次必须逐字节一致。
//
// 这是「幻境不可区分」的前提（AR-26 的启动期断言精神）—— 它坏了就该启动失败，
// 而不是带着一个会被一眼识破的引擎继续跑。
func assertConsistency(ctx context.Context, r responder.Responder) error {
	probe := contract.RespondRequest{SessionID: "startup-probe", Resource: "GET /__selftest", Kind: contract.DecoyBait}
	first, err := r.Respond(ctx, probe)
	if err != nil {
		return err
	}
	second, err := r.Respond(ctx, probe)
	if err != nil {
		return err
	}
	if !bytes.Equal(first.Body, second.Body) {
		return errors.New("responder: 一致性不变量被破坏（AR-30）—— 同输入两次结果不同")
	}
	return nil
}

// ── 观测面适配器（把 store / telemetry 接到 control 定义的接口上）────────────

// decisionRecorder 把一次判定记成两类东西：
//
//	① **事件**（type = `decision`）：控制台直接读它渲染「流量在引擎中如何流动」；
//	② **判定记录**（`store.DecisionStore.Archive`）：数据模型要求的实体（`structure.md` §3）。
//
// 事件优先：读侧靠它。任一失败都只返回错误，由服务面记日志 —— **禁止**影响判定响应（`NI-1`）。
type decisionRecorder struct {
	events    telemetry.Telemetry
	decisions store.DecisionStore
	// logger 是**逐判定**的结构化日志出口（运维面）。
	// 与响应面的区别：日志是内部的，可以带分值/信号；响应禁止回显（ST-7）。
	logger *slog.Logger
}

// newDecisionLogger 构造逐判定日志器。
//
// SHEN_LOG_FORMAT=text（默认，便于人读）| json（便于 jq/日志管道）。
// 为什么用 slog 而不用标准 log：逐判定需要**字段化**（decision_id / action / score / signals），
// 文本拼接出来的行不好筛。启动与装载类日志仍走标准 log（稳定、可 grep，脚本在依赖它）。
func newDecisionLogger() *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SHEN_LOG_FORMAT")), "json") {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func (r decisionRecorder) Record(ctx context.Context, rec control.DecisionRecord) error {
	payload, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := r.events.Report(ctx, contract.Event{
		EventID:   "decision:" + rec.DecisionID, // 幂等键：同一 decision_id 不重复记（AR-11）
		Type:      "decision",
		ActorID:   rec.SourceIP,
		Payload:   payload,
		CreatedAt: rec.At,
	}); err != nil {
		return err
	}
	if err := r.decisions.Archive(ctx, contract.Decision{
		DecisionID: rec.DecisionID,
		Action:     actionFromString(rec.Action),
		Severity:   severityFromString(rec.Severity),
		Backend:    rec.Backend,
	}); err != nil {
		return err
	}
	r.logDecision(rec)
	return nil
}

// logDecision 打一条逐判定日志 —— 本地排查"这个请求为什么被判成这样、然后去了哪"的第一入口。
//
// 只记观测面已有的事实，不推断、不加工（与 docs/spec/logs.md 的字段表一致）。
func (r decisionRecorder) logDecision(rec control.DecisionRecord) {
	if r.logger == nil {
		return
	}
	r.logger.Info("decision",
		slog.String("decision_id", rec.DecisionID),
		slog.String("action", rec.Action),
		slog.String("severity", rec.Severity),
		slog.String("backend", rec.Backend),
		slog.Float64("score", rec.Score),
		slog.Any("signals", rec.Signals),
		slog.String("method", rec.Method),
		slog.String("path", rec.Path),
		slog.String("source_ip", rec.SourceIP),
		slog.String("user_agent", rec.UserAgent),
		slog.Time("at", rec.At),
	)
}

// eventLister 把 store 的事件读侧接到 `control.EventLister` 上。
type eventLister struct{ events store.EventStore }

func (l eventLister) ListEvents(ctx context.Context, limit int, since time.Time, eventType string) ([]contract.Event, error) {
	return l.events.List(ctx, store.EventQuery{Limit: limit, Since: since, Type: eventType})
}

// actionFromString / severityFromString 把观测记录里的可读值还原成枚举。
// 只用于**归档**（给数据模型留一条记录），不参与判定 —— 未知值一律回落放行侧。
func actionFromString(s string) contract.Action {
	switch s {
	case "route_mirage", "ACTION_MIRAGE":
		return contract.ActionMirage
	case "block", "ACTION_BLOCK":
		return contract.ActionBlock
	default:
		return contract.ActionOrigin
	}
}

// severityFromString 目前恒返回 `SeverityNone`：`terminology.md` §4.2 的档位表**尚未登记**其他取值
// （`MD-24` / `TM-13`：新增档位必须先实测并登记）。这里不做字符串猜测 —— 猜错会把未登记的档位写进归档。
func severityFromString(string) contract.Severity {
	return contract.SeverityNone
}

// assertPlaintextListenIsLocal 拒绝把**明文** gRPC 监听到非回环地址。
//
// 本期的进程间通信是明文 + 无认证（判定面、遥测面、策略面都在同一个监听上）：
// 只有「适配器与核心同主机」时才能接受。一旦监听 0.0.0.0，任何能到达该端口的主机
// 都可以伪造判定请求、读取策略下发内容。跨节点部署的正确做法是 mTLS（未实现），
// 因此这里**启动就失败**，而不是把风险留到部署时才发现。
//
// 允许的形式：`127.0.0.1:9443` · `[::1]:9443` · `localhost:9443`。
// 拒绝的形式：`:9443`（所有网卡）· `0.0.0.0:9443` · `10.0.0.5:9443` · 其它主机名。
func assertPlaintextListenIsLocal(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("SHEN_LISTEN=%q 不是 host:port：%w", addr, err)
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("SHEN_LISTEN=%q 未写主机名（等于监听所有网卡）：明文 gRPC 只允许回环地址", addr)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip, perr := netip.ParseAddr(strings.Trim(host, "[]"))
	if perr != nil {
		return fmt.Errorf("SHEN_LISTEN=%q 的主机 %q 无法判定是否回环（只接受回环 IP 或 localhost）：%w", addr, host, perr)
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("SHEN_LISTEN=%q 不是回环地址：明文 gRPC + 无认证只允许同主机部署；"+
			"跨节点需要 mTLS（未实现，见 docs/design/structure.md §4）", addr)
	}
	return nil
}

// printPolicy 打印策略摘要。
//
// 判定响应禁止回显分值、规则名与决策枚举（ST-7），
// 因此「策略是否真的生效」只能靠这行日志证明。
func printPolicy(snap contract.PolicySnapshot) {
	log.Printf("策略已装载 policy_id=%s version=%d checksum=%s 规则=%d 条 灰度=%d%%",
		snap.PolicyID, snap.Version, snap.Checksum, len(snap.Rules), snap.GrayPct)
	if len(snap.Rules) == 0 {
		log.Printf("WARN 规则集为空：判定恒零分、无信号（仅用于链路联调）")
	}
}

// decoyPrefixes 把诱饵资产汇总成路径前缀集（MD-25 的**单一事实源**）。
//
// 只取启用中的资产；空路径被跳过（否则会匹配一切，使 block 失效）。
func decoyPrefixes(assets []contract.DecoyAsset) []string {
	out := make([]string, 0, len(assets))
	for _, a := range assets {
		if a.Enabled && strings.TrimSpace(a.Path) != "" {
			out = append(out, a.Path)
		}
	}
	return out
}

// printDeception 打印欺骗面装配结果（诱饵面 + 幻境后端池）。
//
// 「有没有真的装配上」只能靠这行日志证明 —— 与 printPolicy 同理。
func printDeception(ctx context.Context, surface decoy.Surface, pool honeypot.Pool) {
	assets, err := surface.Enabled(ctx)
	if err != nil {
		log.Printf("WARN 读取诱饵面失败：%v", err)
	} else {
		log.Printf("诱饵面：%d 个资产启用（observe-only，MD-25）", len(assets))
	}

	bs, err := pool.List(ctx)
	if err != nil {
		log.Printf("WARN 读取幻境后端池失败：%v", err)
		return
	}
	enabled := 0
	for _, b := range bs {
		if b.Enabled && b.Healthy {
			enabled++
		}
	}
	log.Printf("幻境后端池：%d 个登记 / %d 个可用（不实现具体蜜罐，ADR-0011）", len(bs), enabled)
	if enabled == 0 {
		// 说清楚，免得排查时困惑：此时 route_mirage 全部回落业务（NI-5）。
		log.Printf("未配置可用的幻境后端：所有 route_mirage 都会回落真实业务")
	}
}

// loadPolicy 从 SHEN_CONFIG 指向的文件装载策略。
//
// 未设置路径、文件不可读、内容非法、版本不递增，都必须让进程启动失败 ——
// 「以空规则集静默启动」是阶段 1 的未闭合项，这里明确禁止。
func loadPolicy() (*policy.Loader, error) {
	path := os.Getenv("SHEN_CONFIG")
	if path == "" {
		return nil, fmt.Errorf("未设置 SHEN_CONFIG（策略是数据，必须显式给出配置路径）。示例：%s", exampleConfig)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开配置失败：%w", err)
	}
	defer func() { _ = f.Close() }()

	loader, err := policy.Load(f)
	if err != nil {
		return nil, fmt.Errorf("装载配置 %s 失败：%w", path, err)
	}
	return loader, nil
}
