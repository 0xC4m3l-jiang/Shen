// Command shen-connector 是反向隧道的**独立连接器进程**：非 Go 业务用它接入蜃楼
// （Go 业务可以直接用 shen/modules/connector/sdk 包）。
//
// 全部配置来自环境变量（与 SDK Config 同名映射）：
//
//	SHEN_CONNECTOR_GATEWAY  网关地址（host:port）——必填
//	SHEN_CONNECTOR_KEY      接入凭证（shc-...）——必填
//	SHEN_CONNECTOR_UPSTREAM 本地真实业务地址——必填
//	SHEN_CONNECTOR_NAME     服务名——必填
//	SHEN_CONNECTOR_HOSTS    声明域名（逗号分隔）——必填
//	SHEN_CONNECTOR_TLS_INSECURE  true = 跳过证书校验（仅本地开发的自签证书）
//
// 生命周期：跟随进程；收到 SIGINT/SIGTERM 发「计划内下线」再退出。
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	connector "shen/modules/connector/sdk"
)

func main() {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		log.Fatalf("shen-connector: 配置错误：\n%v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 守护进程语义：接入失败（网关未就绪 / key 表尚未拉到本凭证 / 网络抖动）不退出，
	// 指数退避重建隧道，直到成功或收到退出信号。SDK 的「首连失败即返回」是**库**语义
	// （库用户自己决定重试策略）；独立进程的重试策略在这里收口。
	backoff := time.Second
	for {
		tun, err := connector.Connect(ctx, cfg)
		if err == nil {
			tun.OnStateChange(func(online bool) {
				if online {
					log.Printf("shen-connector: 已接入 %s（服务 %q · 域名 %v → %s）",
						cfg.Gateway, cfg.Name, cfg.Hosts, cfg.Upstream)
				} else {
					log.Printf("shen-connector: 隧道断开，自动重连中")
				}
			})
			log.Printf("shen-connector: 运行中（Ctrl-C 优雅下线）")
			<-ctx.Done()
			log.Printf("shen-connector: 收到退出信号，计划内下线")
			_ = tun.Close()
			return
		}
		log.Printf("shen-connector: 接入失败（%v），%s 后重试", err, backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		if backoff *= 2; backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func loadConfig(getenv func(string) string) (connector.Config, error) {
	cfg := connector.Config{
		Gateway:    strings.TrimSpace(getenv("SHEN_CONNECTOR_GATEWAY")),
		AuthKey:    strings.TrimSpace(getenv("SHEN_CONNECTOR_KEY")),
		Upstream:   strings.TrimSpace(getenv("SHEN_CONNECTOR_UPSTREAM")),
		Name:       strings.TrimSpace(getenv("SHEN_CONNECTOR_NAME")),
		Hosts:      splitHosts(getenv("SHEN_CONNECTOR_HOSTS")),
		MinBackoff: time.Second,
		MaxBackoff: 30 * time.Second,
		Logf:       func(f string, a ...any) { log.Printf("shen-connector: "+f, a...) },
	}
	var errs []string
	for k, v := range map[string]string{
		"SHEN_CONNECTOR_GATEWAY": cfg.Gateway, "SHEN_CONNECTOR_KEY": cfg.AuthKey,
		"SHEN_CONNECTOR_UPSTREAM": cfg.Upstream, "SHEN_CONNECTOR_NAME": cfg.Name,
	} {
		if v == "" {
			errs = append(errs, k+"：必须设置")
		}
	}
	if len(cfg.Hosts) == 0 {
		errs = append(errs, "SHEN_CONNECTOR_HOSTS：至少一个域名（逗号分隔）")
	}
	if strings.EqualFold(strings.TrimSpace(getenv("SHEN_CONNECTOR_TLS_INSECURE")), "true") {
		cfg.TLS = &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}
	}
	if len(errs) > 0 {
		return connector.Config{}, fmt.Errorf("%s", strings.Join(errs, "；"))
	}
	return cfg, nil
}

func splitHosts(raw string) []string {
	var out []string
	for _, h := range strings.Split(raw, ",") {
		if h = strings.TrimSpace(h); h != "" {
			out = append(out, h)
		}
	}
	return out
}
