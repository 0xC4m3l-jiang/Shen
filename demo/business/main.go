// 方式一（推荐给非嵌入式场景）的**业务进程**：独立运行的真实业务。
//
// 关键点：只监听 127.0.0.1 —— 防火墙可以拒绝一切入站连接；
// 外部流量要到达这里，唯一的路是「连接器向蜃楼网关拨号建立的隧道」。
//
// 启动它之后，业务侧的接入工作只剩一件：运行 shen-connector（见 ../connector.env.example
// 与 ../up.sh，由脚本代劳）。业务代码本身**没有任何一行**和蜃楼相关。
package main

import (
	"log"
	"net/http"

	"shen/demo/shop"
)

func main() {
	addr := "127.0.0.1:9001"
	log.Printf("蜃景商城（演示业务）已启动：http://%s（仅本机回环，零入站暴露）", addr)
	log.Printf("外部访问路径：蜃楼业务入口 + Host: demo.shop.local —— 连接器负责把两者接起来")
	if err := http.ListenAndServe(addr, shop.New(shop.NewStore())); err != nil {
		log.Fatalf("业务启动失败：%v", err)
	}
}
