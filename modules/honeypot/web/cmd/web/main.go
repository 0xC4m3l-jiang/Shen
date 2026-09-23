// Command web 是**Web 诱饵后端**的进程入口：把一套合成管理台场景服务成一个普通 HTTP 后端。
//
// 用法（它**不是**接入形态，也不在请求路径上直连客户端 —— 由诱饵路由指过来）：
//
//	SHEN_WEB_LISTEN=127.0.0.1:19090 go run ./modules/honeypot/web/cmd/web
//
// 环境变量：
//
//	SHEN_WEB_LISTEN       监听地址（默认 127.0.0.1:19090 —— **只监听本机**：它是幻境后端，不该对外暴露）
//	SHEN_WEB_SCENARIO     场景包标识（默认 atlas；未知 id 回落默认场景，不报错）
//	SHEN_WEB_MAX_BODY     请求体上限（字节，默认 65536；超限 413）
//	SHEN_WEB_SESSION_TTL  合成会话存活期（默认 30m）
//
// 三条纪律（与模块设计一致）：
//
//	① **不发起任何出站连接**：本进程只有 `http.Server`，没有出站客户端 —— 合成交互不该有外呼面；
//	② **不碰生产**：没有认证校验、没有数据库、没有真实素材（场景数据在代码里，确定性生成）；
//	③ **失败不回落业务**：那是适配器 `deliverDecoy` 的职责（故障固定 502、绝不回生产）。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"shen/modules/honeypot/web"
)

const (
	defaultListen  = "127.0.0.1:19090"
	defaultMaxBody = 64 << 10 // 64 KiB
	defaultTTL     = 30 * time.Minute
)

func main() {
	listen := env("SHEN_WEB_LISTEN", defaultListen)
	scenarioID := env("SHEN_WEB_SCENARIO", "atlas")

	maxBody, err := envBytes("SHEN_WEB_MAX_BODY", defaultMaxBody)
	if err != nil {
		log.Fatalf("web: %v", err)
	}
	ttl, err := envDuration("SHEN_WEB_SESSION_TTL", defaultTTL)
	if err != nil {
		log.Fatalf("web: %v", err)
	}
	loginBurst, err := envInt("SHEN_WEB_LOGIN_BURST", defaultLoginBurst)
	if err != nil {
		log.Fatalf("web: %v", err)
	}
	loginWindow, err := envDuration("SHEN_WEB_LOGIN_WINDOW", defaultLoginWindow)
	if err != nil {
		log.Fatalf("web: %v", err)
	}
	eventQueue, err := envInt("SHEN_WEB_EVENT_QUEUE", defaultEventQueue)
	if err != nil {
		log.Fatalf("web: %v", err)
	}

	// 场景包（可选）：`SHEN_WEB_SCENARIOS` 指向 JSON 文件（格式见 docs/spec/decoy-scenario.md）。
	// 文件存在即**必须**合法：坏素材宁可启动失败，也不带出去（见 pack.go 的三条设计要点）。
	packs := loadScenarioPacks(env("SHEN_WEB_SCENARIOS", ""), scenarioID)

	h := web.New(web.Options{
		ScenarioID:   scenarioID,
		Packs:        packs,
		MaxBodyBytes: maxBody,
		SessionTTL:   ttl,
		CookieSecure: envBool("SHEN_WEB_COOKIE_SECURE", false),
		LoginBurst:   loginBurst,
		LoginWindow:  loginWindow,
		EventQueue:   eventQueue,
		RequestLog:   envBool("SHEN_WEB_REQUEST_LOG", false),
		Events:       eventLogger{},
	})
	defer h.Close()
	sc := h.Scenario()

	srv := &http.Server{
		Addr:              listen,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		// 读整个请求的上限与人工设计的 body 上限一致（+ 头部余量），避免慢速大包占资源。
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("web 诱饵后端已启动：监听 %s（场景 %s · %s · 账号 %d · 配置 %d · 审计 %d · 登录语义 %s）",
		listen, sc.ID, sc.Org, len(sc.Users), len(sc.Config), len(sc.Audit), sc.Outcome)

	// 优雅退出（`W6`）：收到 SIGINT/SIGTERM 后先停收新请求，再等在途请求收尾。
	// 为什么需要它：诱饵后端重启时被硬切断的合成会话与半截响应，都是对手可见的异常。
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-stop
		log.Printf("web: 收到 %s ⇒ 停止接收新请求（最多等 %s）", sig, shutdownGrace)
		ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("web: 优雅退出超时，强制关闭：%v", err)
			_ = srv.Close()
		}
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("web: 监听失败：%v", err)
	}
	log.Printf("web: 已停止（丢弃事件 %d 条 · 投递失败 %d 次）", h.DroppedEvents(), h.EventFailures())
}

// eventLogger 把合成交互事件打一行到 stdout（运维面；不含请求体、不含凭据）。
//
// 生产部署应把它换成真正的观测面（事件 → 策略面/收集器）；这里保持最小实现：
// 有输出、可 grep，且**永不**带正文。
type eventLogger struct{}

func (eventLogger) Event(_ context.Context, ev web.Event) {
	log.Printf("web: 合成交互 kind=%s path=%s user=%q outcome=%s",
		ev.Kind, ev.Path, ev.User, ev.Outcome)
}

// loadScenarioPacks 装载外部场景包；未配置时返回 nil（= 只用内置包）。
//
// 三条行为：
//   - 路径为空 ⇒ 返回 nil，不打日志（内置包是默认形态）；
//   - 路径非空且文件非法 ⇒ **启动失败**（`log.Fatalf`）：坏素材不带出去；
//   - 选中的 id 不在包里 ⇒ 记一行 WARN（回落内置场景是**安全**的，但配置写错要可见）。
func loadScenarioPacks(path, scenarioID string) map[string]web.Scenario {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	packs, err := web.LoadPacks(path)
	if err != nil {
		log.Fatalf("web: %v", err)
	}
	ids := make([]string, 0, len(packs))
	for id := range packs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	log.Printf("场景包已装载：%s（%d 套：%s）", path, len(packs), strings.Join(ids, ", "))
	if _, ok := packs[scenarioID]; !ok {
		log.Printf("WARN SHEN_WEB_SCENARIO=%q 不在场景包里 ⇒ 回落到内置场景（改 id 或补场景）", scenarioID)
	}
	return packs
}

// 登录配额与事件队列的默认值（与库内默认一致：库默认用于**直接 new** 的场景，这里给入口用）。
const (
	defaultLoginBurst  = 10
	defaultLoginWindow = time.Minute
	defaultEventQueue  = 256
	// shutdownGrace 是优雅退出时等待在途请求的上限。
	shutdownGrace = 5 * time.Second
)

// ── env 解析（与其它适配器同一套写法：写错即启动失败，不悄悄回落）──────────────

// envBool 解析布尔型开关：只认 1/true/yes/on 与 0/false/no/off，其余**启动失败**。
func envBool(key string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch raw {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		log.Fatalf("web: %s 取值 %q 不是布尔（用 1/0、true/false、yes/no、on/off）", key, raw)
		return fallback
	}
}

// envInt 解析正整数环境变量（<= 0 视为写错 ⇒ 启动失败，不悄悄回落）。
func envInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, errors.New(key + " 必须是正整数")
	}
	return n, nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBytes(key string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, errors.New(key + " 必须是正整数字节数")
	}
	return n, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, errors.New(key + " 必须是正的时长（如 30m）")
	}
	return d, nil
}
