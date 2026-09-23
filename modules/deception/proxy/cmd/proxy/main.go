// Command proxy 是接入形态③（反向代理前置）与形态④（Sidecar）的进程入口。
//
// 同一份二进制覆盖两种形态，差别只在环境变量：
//
//	③ 前置代理：SHEN_PROXY_UPSTREAM 指向业务真实地址
//	④ Sidecar：  SHEN_PROXY_UPSTREAM 指向 127.0.0.1:<同 Pod 内业务端口>
//
// 转发与 TLS 终结由内嵌 Caddy 承担：本进程用 env 解析出配置，程序化生成 Caddy 配置
// 并 caddy.Run()。判定逻辑在 deception/proxy 的 Caddy 模块 `http.handlers.shen_proxy` 里。
//
// 默认是**影子模式**（SHEN_PROXY_SHADOW=true）：照算判定并上报，但永不改道、永不拦截。
// 首次上线必须如此（INT-11）。
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/caddyserver/caddy/v2"
	_ "github.com/caddyserver/caddy/v2/modules/standard" // 注册 http / tls / reverse_proxy 等标准模块

	"shen/modules/deception/proxy"
)

const (
	defaultListen   = "127.0.0.1:8081"
	defaultCoreAddr = "127.0.0.1:9443"

	// 默认与设计的 S1 接缝一致：判定面 deadline ≤ 3ms（见 architecture.md §8.6）。
	defaultDecisionTimeout = 3 * time.Millisecond
	defaultCacheTTL        = 60 * time.Second
	defaultWindow          = 60 * time.Second
	defaultReportQueue     = 1024
	defaultCacheMax        = 65536
	shutdownGrace          = 10 * time.Second

	// 默认策略拉取间隔：60s（ADR-0018）。0 = 不拉策略，完全用本地配置。
	defaultPolicyInterval = 60 * time.Second

	// 幻境后端健康探针（见 modules/deception/proxy/health.go）。
	defaultBackendHealthInterval = 30 * time.Second
	defaultBackendHealthTimeout  = time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("proxy: %v", err)
	}
}

func run() error {
	upstream := os.Getenv("SHEN_PROXY_UPSTREAM")
	if upstream == "" {
		return errors.New("必须设置 SHEN_PROXY_UPSTREAM（业务真实地址，例如 http://127.0.0.1:9000）")
	}

	listen := env("SHEN_PROXY_LISTEN", defaultListen)
	coreAddr := env("SHEN_CORE_ADDR", defaultCoreAddr)

	mirage, err := parseMirage(os.Getenv("SHEN_PROXY_MIRAGE"))
	if err != nil {
		return err
	}
	whitelist := splitList(os.Getenv("SHEN_PROXY_WHITELIST"))

	timeout, err := envDuration("SHEN_PROXY_DECISION_TIMEOUT", defaultDecisionTimeout)
	if err != nil {
		return err
	}
	cacheTTL, err := envDuration("SHEN_PROXY_CACHE_TTL", defaultCacheTTL)
	if err != nil {
		return err
	}
	// 会话身份第 ① 级的 cookie 名：必须与核心的 `session.cookie_name` 一致（不一致时身份会退化到指纹）。
	sessionCookie := env("SHEN_PROXY_SESSION_COOKIE", proxy.DefaultSessionCookie)

	window, err := envDuration("SHEN_PROXY_WINDOW", defaultWindow)
	if err != nil {
		return err
	}
	queue, err := envInt("SHEN_PROXY_REPORT_QUEUE", defaultReportQueue)
	if err != nil {
		return err
	}
	cacheMax, err := envInt("SHEN_PROXY_CACHE_MAX", defaultCacheMax)
	if err != nil {
		return err
	}
	tlsCfg, err := parseTLS()
	if err != nil {
		return err
	}
	policyInterval, err := envDuration("SHEN_PROXY_POLICY_INTERVAL", defaultPolicyInterval)
	if err != nil {
		return err
	}
	// 幻境后端健康探针（建议书 §8 第 7 项）：`enabled` 不是健康。
	// 0/未设 = 默认 30s；**负数 = 关闭**（关掉后"健康"就等于 enabled —— 这是刻意可关的）。
	healthInterval, err := envDuration("SHEN_PROXY_BACKEND_HEALTH_INTERVAL", defaultBackendHealthInterval)
	if err != nil {
		return err
	}
	healthTimeout, err := envDuration("SHEN_PROXY_BACKEND_HEALTH_TIMEOUT", defaultBackendHealthTimeout)
	if err != nil {
		return err
	}
	// 降级缓存预算（`N2`）：0 = 库内默认（1s）；**负数 = 关闭降级缓存**（判定失败不写缓存）。
	degradedTTL, err := envDuration("SHEN_PROXY_DEGRADED_CACHE_TTL", 0)
	if err != nil {
		return err
	}
	// 诱饵路由撤销后的搜索碑租约（`N6`）：0 = 默认 24h；**负数 = 永久**（不释放归属）。
	decoyLease, err := envDuration("SHEN_PROXY_DECOY_LEASE", 0)
	if err != nil {
		return err
	}

	handler := &proxy.Handler{
		Upstream:        upstream,
		Mirage:          mirage,
		Whitelist:       whitelist,
		DecisionTimeout: caddy.Duration(timeout),
		CacheTTL:        caddy.Duration(cacheTTL),
		Window:          caddy.Duration(window),
		SessionCookie:   sessionCookie,
		Shadow:          envBool("SHEN_PROXY_SHADOW", true),
		LogRequests:     envBool("SHEN_PROXY_LOG_REQUESTS", false),
		TrustXFF:        envBool("SHEN_PROXY_TRUST_XFF", false),
		ReportQueue:     queue,
		CacheMaxEntries: cacheMax,
		CoreAddr:        coreAddr,
		Inject:          splitInject(os.Getenv("SHEN_PROXY_INJECT")),
		// AI 欺骗内容注入的**本地兜底开关**（ADR-0023 决定 4）：默认 false，
		// 与策略载荷的 inject_enabled **取与** —— 即使策略面说"开"，边缘也要一次本地同意。
		InjectContent: envBool("SHEN_PROXY_INJECT_CONTENT", false),
		// 策略面（S4）：定期拉取，远端覆盖本地、本地兜底（ADR-0018）。
		PolicyID:       strings.TrimSpace(os.Getenv("SHEN_PROXY_POLICY_ID")),
		AdapterID:      env("SHEN_PROXY_ADAPTER_ID", "proxy@"+listen),
		PolicyInterval: caddy.Duration(policyInterval),

		BackendHealthInterval: caddy.Duration(healthInterval),
		BackendHealthTimeout:  caddy.Duration(healthTimeout),

		// 第二轮检验的另外三个可调项（此前**只在文档里存在**、代码未接线 —— 补上）。
		DegradedCacheTTL:   caddy.Duration(degradedTTL),
		DecoyLease:         caddy.Duration(decoyLease),
		ForwardCredentials: envBool("SHEN_PROXY_FORWARD_CREDENTIALS", false),
	}

	cfg, err := proxy.BuildConfig(proxy.Options{
		Listen:      listen,
		Handler:     handler,
		TLS:         tlsCfg,
		GracePeriod: shutdownGrace,
	})
	if err != nil {
		return err
	}

	// Caddy 单进程同时承担 L0（TLS 终结）+ L1（判定路由）。
	if err := caddy.Run(cfg); err != nil {
		return fmt.Errorf("启动 Caddy 失败：%w", err)
	}

	mode := "影子模式（只观测、不处置）"
	if !handler.Shadow {
		mode = "接管模式（可按决策改道）"
	}
	tlsDesc := "明文"
	switch tlsCfg.Mode {
	case proxy.TLSModeManual:
		tlsDesc = "TLS（手动证书）"
	case proxy.TLSModeACME:
		tlsDesc = "TLS（ACME，域名 " + tlsCfg.Domain + "）"
	}
	log.Printf("proxy 已启动：监听 %s（%s），核心 %s，业务 %s，%s",
		listen, tlsDesc, coreAddr, upstream, mode)
	if w := proxy.SelfTerminationWarning(tlsCfg); w != "" {
		log.Printf("警告：%s", w)
	}
	if policyInterval > 0 {
		log.Printf("策略面：每 %s 拉取一次（适配器标识 %s）", policyInterval, handler.AdapterID)
	} else {
		log.Printf("策略面未启用（SHEN_PROXY_POLICY_INTERVAL=0）：改道后端表与白名单只用本地 env")
	}
	if len(mirage) > 0 {
		log.Printf("引流后端 %d 个", len(mirage))
	} else {
		// 没有引流后端时 route_mirage 一律回落业务 —— 说清楚，免得排查时困惑。
		log.Printf("未配置引流后端（SHEN_PROXY_MIRAGE 为空）：所有 route_mirage 都会回落业务")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("收到 %s，正在退出", sig)

	return caddy.Stop()
}

// ── 环境变量解析 ─────────────────────────────────────────────────────────────

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envBool 解析布尔；无法识别时用默认值并在日志里说一声 —— 静默取默认会掩盖配置错误。
func envBool(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		log.Printf("警告：%s=%q 不是布尔值，按默认 %v 处理", key, raw, fallback)
		return fallback
	}
	return v
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s=%q 不是合法时长（如 3ms / 60s）：%w", key, raw, err)
	}
	return v, nil
}

func envInt(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s=%q 不是整数：%w", key, raw, err)
	}
	return v, nil
}

// parseTLS 解析 TLS 相关环境变量（`SHEN_PROXY_TLS_MODE` / `SHEN_PROXY_DOMAIN` /
// `SHEN_PROXY_CERT_FILE` / `SHEN_PROXY_KEY_FILE`）。
func parseTLS() (proxy.TLSConfig, error) {
	mode := proxy.TLSMode(strings.ToLower(strings.TrimSpace(env("SHEN_PROXY_TLS_MODE", string(proxy.TLSModeOff)))))
	cfg := proxy.TLSConfig{
		Mode:     mode,
		Domain:   strings.TrimSpace(os.Getenv("SHEN_PROXY_DOMAIN")),
		CertFile: strings.TrimSpace(os.Getenv("SHEN_PROXY_CERT_FILE")),
		KeyFile:  strings.TrimSpace(os.Getenv("SHEN_PROXY_KEY_FILE")),
	}
	if err := cfg.Validate(); err != nil {
		return proxy.TLSConfig{}, err
	}
	return cfg, nil
}

// splitList 把逗号分隔的列表拆成非空元素（CIDR 白名单等）。
func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// splitInject 解析 `片段1;;片段2` 形式的注入片段（空 = 不注入）。
//
// 用 `;;` 分隔而不是 `;` —— HTML 片段里常有分号（实体、样式）。
func splitInject(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, s := range strings.Split(raw, ";;") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// parseMirage 解析 `名字=地址,名字=地址` 形式的引流后端表。
func parseMirage(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	out := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		name, addr, ok := strings.Cut(pair, "=")
		name, addr = strings.TrimSpace(name), strings.TrimSpace(addr)
		if !ok || name == "" || addr == "" {
			return nil, fmt.Errorf("SHEN_PROXY_MIRAGE 的 %q 不是「名字=地址」", pair)
		}
		out[name] = addr
	}
	return out, nil
}
