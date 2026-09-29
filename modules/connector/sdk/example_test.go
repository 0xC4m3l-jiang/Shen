package connector_test

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	connector "shen/modules/connector/sdk"
)

// ExampleConnect 是业务侧接入的标准形态：三行代码 + 一个 defer。
//
// 环境变量（由运维在部署时注入）：
//
//	SHEN_CONNECTOR_GATEWAY  网关地址，如 shen-gw.example.com:9446
//	SHEN_CONNECTOR_KEY      控制台签发的接入凭证（shc-...，签发时一次性显示）
//	SHEN_CONNECTOR_UPSTREAM 本地真实业务地址，如 http://127.0.0.1:8080
//	SHEN_CONNECTOR_NAME     服务名（与凭证一致）
//	SHEN_CONNECTOR_HOSTS    声明域名（逗号分隔，凭证白名单的子集）
func ExampleConnect() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c, err := connector.Connect(ctx, connector.Config{
		Gateway:  os.Getenv("SHEN_CONNECTOR_GATEWAY"),
		AuthKey:  os.Getenv("SHEN_CONNECTOR_KEY"),
		Upstream: os.Getenv("SHEN_CONNECTOR_UPSTREAM"),
		Name:     os.Getenv("SHEN_CONNECTOR_NAME"),
		Hosts:    []string{"shop.example.com"},
	})
	if err != nil {
		fmt.Println("接入失败：", err)
		return
	}
	c.OnStateChange(func(online bool) {
		if online {
			fmt.Println("已接入蜃楼（业务零入站暴露）")
		} else {
			fmt.Println("隧道断开，自动重连中……")
		}
	})
	defer func() { _ = c.Close() }() // 计划内下线（网关审计区分主动下线与意外掉线）

	<-ctx.Done() // 随业务进程生命周期
}
