// SDK 进程内接入示例：业务与连接器同进程（deployment 的另一种形态）。
//
// 运行（需控制台已签发凭证、网关在跑）：
//
//	export SHEN_CONNECTOR_GATEWAY=127.0.0.1:9446
//	export SHEN_CONNECTOR_KEY=shc-...        # 控制台「接入管理」签发
//	go run ./modules/connector/example/embedded
//
// 与独立二进制（cmd/shen-connector）的差别只在部署形态：本例把「本地业务」
// 与连接器放进同一个进程 —— 业务 Handler 由 SDK 内部循环服务，外部访问路径不变。
// 业务侧只听回环：零入站暴露。
package main

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	connector "shen/modules/connector/sdk"
)

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	gateway := getenv("SHEN_CONNECTOR_GATEWAY", "127.0.0.1:9446")
	authKey := getenv("SHEN_CONNECTOR_KEY", "")
	name := getenv("SHEN_CONNECTOR_NAME", "示例业务")
	hosts := strings.Split(strings.TrimSpace(getenv("SHEN_CONNECTOR_HOSTS", "demo.example.local")), ",")
	if authKey == "" {
		log.Fatal("缺少 SHEN_CONNECTOR_KEY：请先在控制台「接入管理」签发凭证")
	}

	// 本地业务：一个最小示例站（真实业务换成你自己的 Handler 即可，接入方式不变）。
	biz := http.NewServeMux()
	biz.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body><h1>示例业务</h1><p>经蜃楼反向隧道访问成功。</p></body></html>"))
	})
	bizLn, err := net.Listen("tcp", "127.0.0.1:0") // 随机回环端口：业务只听回环
	if err != nil {
		log.Fatalf("业务监听失败：%v", err)
	}
	log.Printf("业务已启动：http://%s（仅本机回环）", bizLn.Addr())
	go func() { _ = http.Serve(bizLn, biz) }()

	// 连接器 SDK：拨号 → 握手 → 断线自动重连，直到 ctx 取消。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tun, err := connector.Connect(ctx, connector.Config{
		Gateway:  gateway,
		AuthKey:  authKey,
		Upstream: "http://" + bizLn.Addr().String(),
		Name:     name,
		Hosts:    hosts,
		TLS:      &tls.Config{InsecureSkipVerify: true}, // 自签证书仅本地开发；生产删除
		Logf:     func(f string, a ...any) { log.Printf("连接器："+f, a...) },
	})
	if err != nil {
		log.Fatalf("接入失败：%v", err)
	}
	log.Printf("已接入：外部经 Host %s 即可访问本业务", hosts)
	<-ctx.Done()
	log.Printf("计划内下线")
	_ = tun.Close()
}
