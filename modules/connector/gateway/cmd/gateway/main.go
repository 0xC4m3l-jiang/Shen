// Command gateway 是反向隧道网关：连接器拨入的 TLS 终结点。
//
// 部署位置：平台侧（与 proxy 同机或同网络命名空间；公网可达的 9446 由 LB/防火墙控制）。
// 启动依赖：控制台集成令牌（拉 key 表 / 上报）+ TLS 证书。
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"shen/modules/connector/gateway"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "探测本进程 /healthz 后退出（容器健康检查用）")
	flag.Parse()

	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		log.Fatalf("gateway: 配置错误：\n%v", err)
	}
	if *healthcheck {
		if err := probe("http://" + cfg.Bridge + "/healthz"); err != nil {
			log.Fatalf("gateway: 健康检查失败：%v", err)
		}
		return
	}

	srv := gateway.New(gateway.Config{
		Listen:           cfg.Listen,
		TLSCert:          cfg.TLSCert,
		Bridge:           cfg.Bridge,
		ConsoleURL:       cfg.ConsoleURL,
		IntegrationToken: cfg.IntegrationToken,
		GatewayNode:      cfg.Node,
		KeyRefresh:       cfg.KeyRefresh,
		Logf:             log.Printf,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ① 连接器拨入（TLS）。
	tlsLn, err := tls.Listen("tcp", cfg.Listen, &tls.Config{
		Certificates: []tls.Certificate{cfg.TLSCert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: nil, // 默认套件（Go 默认已排除弱套件）
	})
	if err != nil {
		log.Fatalf("gateway: 监听 %s 失败：%v", cfg.Listen, err)
	}
	conns := make(chan net.Conn, 64)
	go func() {
		for {
			conn, err := tlsLn.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("gateway: accept：%v", err)
					continue
				}
			}
			conns <- conn
		}
	}()

	// ② 供 proxy 取流的回环字节桥（同端口应答 GET /healthz，容器健康检查用）。
	bridgeLn, err := net.Listen("tcp", cfg.Bridge)
	if err != nil {
		log.Fatalf("gateway: 监听桥 %s 失败：%v", cfg.Bridge, err)
	}
	go func() { _ = srv.ServeBridge(ctx, bridgeLn) }()

	log.Printf("gateway: 已启动：连接器入口 tls://%s · 字节桥 %s · 控制台 %s · 节点标识 %s",
		cfg.Listen, cfg.Bridge, cfg.ConsoleURL, cfg.Node)
	srv.Run(ctx, conns)
	log.Printf("gateway: 已退出")
}

type config struct {
	Listen           string
	TLSCert          tls.Certificate
	Bridge           string
	ConsoleURL       string
	IntegrationToken string
	Node             string
	KeyRefresh       time.Duration
}

func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		Listen:     env(getenv, "SHEN_GATEWAY_LISTEN", "0.0.0.0:9446"),
		Bridge:     env(getenv, "SHEN_GATEWAY_BRIDGE", "127.0.0.1:9447"),
		ConsoleURL: env(getenv, "SHEN_GATEWAY_CONSOLE", "http://127.0.0.1:9445"),
		Node:       env(getenv, "SHEN_GATEWAY_NODE", defaultNode()),
	}
	var errs []error
	certPath, keyPath := getenv("SHEN_GATEWAY_TLS_CERT"), getenv("SHEN_GATEWAY_TLS_KEY")
	if certPath == "" || keyPath == "" {
		errs = append(errs, errors.New("SHEN_GATEWAY_TLS_CERT / SHEN_GATEWAY_TLS_KEY：必须提供 TLS 证书与私钥路径"))
	} else if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
		errs = append(errs, fmt.Errorf("加载 TLS 证书失败：%w", err))
	} else {
		cfg.TLSCert = cert
	}
	cfg.IntegrationToken = strings.TrimSpace(getenv("SHEN_GATEWAY_INTEGRATION_TOKEN"))
	if cfg.IntegrationToken == "" {
		errs = append(errs, errors.New("SHEN_GATEWAY_INTEGRATION_TOKEN：必须提供控制台集成令牌"))
	}
	var err error
	if cfg.KeyRefresh, err = time.ParseDuration(env(getenv, "SHEN_GATEWAY_KEY_REFRESH", "30s")); err != nil || cfg.KeyRefresh <= 0 {
		errs = append(errs, errors.New("SHEN_GATEWAY_KEY_REFRESH 须为正时长（如 30s）"))
	}
	return cfg, errors.Join(errs...)
}

func env(getenv func(string) string, key, def string) string {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		return v
	}
	return def
}

func defaultNode() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "gateway"
	}
	return host
}

func probe(url string) error {
	// 桥只收 Host 行 + 字节；健康检查发一个 HEAD /healthz 也走同一入口。
	// 简化：直接 TCP 探测端口（不依赖 HTTP 语义）。
	host := strings.TrimPrefix(url, "http://")
	conn, err := net.DialTimeout("tcp", host, 3*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}
