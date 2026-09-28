// Command console 是**管控台 API**：把核心的观测面读出来，经带鉴权的 `/api/v1` 提供给独立前端。
//
// 形态（前后端解耦）：
//   - 本进程**只提供 API**，不再内嵌任何页面；前端是独立的 Vue 工程，由 nginx 容器同源提供并反代 `/api/`；
//   - 默认只监听回环（127.0.0.1:9445）：浏览器必须经同源前端访问，其他站点的脚本读不到响应（无 CORS）。
//
// 边界（不做的事）：
//   - **不参与请求级判定**（`AR-10`）：它是旁观者，挂了不影响业务；
//   - **不下发策略**：反向链接器只是「登记 + 按域名归类流量」，生效配置仍走核心配置与策略面；
//   - **不主动连接业务上游**：登记里的 upstream 只做格式校验与展示。
//
// 本进程只负责**装配**：配置 → 核心 gRPC 读面 → 账号 / 登记 / 归属地 / 审计 → v1 路由 → HTTP 服务。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	telemetryv1 "shen/common/api/telemetry/v1"
	"shen/modules/console/internal/api"
	"shen/modules/console/internal/audit"
	"shen/modules/console/internal/auth"
	"shen/modules/console/internal/geoip"
	"shen/modules/console/internal/llm"
	"shen/modules/console/internal/registry"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "探测本进程 /healthz 后退出（容器健康检查用；无需镜像内置 curl/wget）")
	flag.Parse()

	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		log.Fatalf("console: 配置错误：\n%v", err)
	}
	if *healthcheck {
		os.Exit(probe(cfg.Listen))
	}
	if err := run(cfg); err != nil {
		log.Fatalf("console: %v", err)
	}
}

func run(cfg config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("创建数据目录 %s 失败（容器内须挂载可写卷）：%w", cfg.DataDir, err)
	}
	geo, err := openGeo(cfg.GeoDB)
	if err != nil {
		return err
	}
	authSvc, err := auth.New(auth.Config{
		DataDir: cfg.DataDir, SessionIdle: cfg.SessionIdle, SessionAbsolute: cfg.SessionTTL,
		APIToken: cfg.APIToken, BootstrapUser: cfg.BootstrapUser, BootstrapPassword: cfg.BootstrapPassword,
		Logf: log.Printf,
	})
	if err != nil {
		return err
	}
	reg, err := registry.Open(filepath.Join(cfg.DataDir, "services.json"), nil)
	if err != nil {
		return err
	}
	aud, err := audit.Open(filepath.Join(cfg.DataDir, "audit.log"), 2000, 0, nil)
	if err != nil {
		return err
	}
	defer func() { _ = aud.Close() }()
	// 大模型分析：提供方密钥用 AES-256-GCM 落盘；出站只在用户显式操作（测试连通 / 发消息）时发生。
	llmSvc, err := llm.New(llm.Config{DataDir: cfg.DataDir, MasterKey: cfg.SecretKey})
	if err != nil {
		return err
	}
	defer func() { _ = llmSvc.Close() }()
	if llmSvc.MasterKeyGenerated() {
		log.Printf("console: WARN 未设置 SHEN_CONSOLE_SECRET_KEY ⇒ 已在数据目录生成大模型密钥的主密钥 %s（与密文同卷；生产请经 secrets 注入 SHEN_CONSOLE_SECRET_KEY_FILE）", llm.MasterKeyFile)
	}

	// 与核心之间走本机明文 gRPC（同一网络命名空间的回环；跨节点必须换 mTLS）。
	conn, err := grpc.NewClient(cfg.CoreAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("连接核心失败：%w", err)
	}
	defer func() { _ = conn.Close() }()

	server := api.New(telemetryv1.NewDeceptionTelemetryClient(conn), authSvc, reg, geo, aud, api.Config{
		AllowedOrigins: cfg.AllowedOrigins, TrustedProxies: cfg.TrustedProxies, TokenSources: cfg.TokenSources,
		CookieSecure: cfg.CookieSecure, SessionAbsolute: cfg.SessionTTL, AlertScore: cfg.AlertScore,
		MaxStreams: cfg.MaxStreams, LLM: llmSvc, Logf: log.Printf,
	})
	warnings(cfg)

	// 注意：**不设 WriteTimeout** —— SSE 是长连接，设了会被定期掐断。读侧超时照常设置。
	srv := &http.Server{
		Addr: cfg.Listen, Handler: server.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 120 * time.Second,
		MaxHeaderBytes: 32 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Printf("console API 已启动：监听 %s，核心 %s，数据目录 %s", cfg.Listen, cfg.CoreAddr, cfg.DataDir)

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		log.Printf("console: 收到退出信号，正在关闭（SSE 连接会被断开，页面自动重连）")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func openGeo(path string) (*geoip.DB, error) {
	if path == "" {
		return geoip.Default()
	}
	return geoip.OpenFile(path)
}

// warnings 对「能跑但不安全」的组合给出启动告警（不拒绝启动：本机开发需要 http）。
func warnings(cfg config) {
	host, _, err := net.SplitHostPort(cfg.Listen)
	loopback := err == nil && (host == "127.0.0.1" || host == "::1" || host == "localhost")
	if !loopback && !cfg.CookieSecure {
		log.Printf("console: ⚠️ 监听非回环地址 %s 但 SHEN_CONSOLE_COOKIE_SECURE=false：生产环境必须经 HTTPS 并开启 Secure Cookie", cfg.Listen)
	}
	if len(cfg.AllowedOrigins) == 0 {
		log.Printf("console: 未设置 SHEN_CONSOLE_ALLOWED_ORIGINS：只接受与 Host 同源的浏览器请求")
	}
}

// probe 访问本进程的 /healthz；0 = 健康。
func probe(listen string) int {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return 1
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
