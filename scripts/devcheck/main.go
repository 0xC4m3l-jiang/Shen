// Command devcheck 是开发期的在线冒烟工具：对一个已经在跑的核心发判定请求，
// 检查响应形状、三值闭集与幂等重试的一致性。
//
// 它**不是产品代码**：不在请求路径上、不参与发布、不持有业务逻辑。
// 工具目录属 MD-19 的豁免范围（scripts/ 的工具）。
//
// 用法：
//
//	go run ./scripts/devcheck -addr 127.0.0.1:9443
//	make smoke                      # 同上，地址取 SHEN_DEV_ADDR
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	judgev1 "shen/api/judge/v1"
)

// smokeCase 是一条冒烟样本：一个观测。
// 冒烟不校验分数 —— 判定响应禁止回显分值（ST-7），分数只能靠 `make replay` 看。
var smokeCases = []struct {
	name string
	obs  *judgev1.Observation
}{
	{
		name: "无头浏览器探源码",
		obs: &judgev1.Observation{
			SourceIp:  "203.0.113.7",
			UserAgent: "HeadlessChrome/120.0.0.0",
			Method:    "GET",
			Path:      "/.git/config",
		},
	},
	{
		name: "正常浏览器",
		obs: &judgev1.Observation{
			SourceIp:  "198.51.100.20",
			UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Safari/537.36",
			Method:    "GET",
			Path:      "/index.html",
		},
	},
	{
		name: "会话 cookie 走会话身份",
		obs: &judgev1.Observation{
			SourceIp:  "203.0.113.7",
			UserAgent: "curl/8.5.0",
			Method:    "PUT",
			Path:      "/api/v1/orders",
			Headers:   map[string]string{"cookie": "sid=devcheck-session"},
		},
	},
}

func main() {
	addr := flag.String("addr", "127.0.0.1:9443", "核心判定面地址（host:port）")
	timeout := flag.Duration("timeout", 5*time.Second, "整体超时")
	flag.Parse()

	if err := run(*addr, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "冒烟失败：%v\n", err)
		os.Exit(1)
	}
	fmt.Println("冒烟通过。")
}

func run(addr string, timeout time.Duration) error {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("连接 %s 失败：%w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	cli := judgev1.NewDeceptionJudgeClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fmt.Printf("判定面 %s：%d 个样本\n", addr, len(smokeCases))
	for _, c := range smokeCases {
		// decision_id 由适配器派生（ST-10）；这里是开发工具，用样本名造一个稳定值即可。
		req := &judgev1.JudgeRequest{DecisionId: "devcheck-" + c.name, Observed: c.obs}

		resp, err := cli.Judge(ctx, req)
		if err != nil {
			return fmt.Errorf("%s：调用判定面失败：%w", c.name, err)
		}
		if err := checkShape(c.name, resp); err != nil {
			return err
		}

		// 同一 decision_id 重试必须得到同一结果（ST-10 幂等）。
		again, err := cli.Judge(ctx, req)
		if err != nil {
			return fmt.Errorf("%s：重试失败：%w", c.name, err)
		}
		if again.GetAction() != resp.GetAction() ||
			again.GetSeverity() != resp.GetSeverity() ||
			again.GetBackend() != resp.GetBackend() {
			return fmt.Errorf("%s：同一 decision_id 两次结果不一致（违反 ST-10 幂等）", c.name)
		}

		fmt.Printf("  [通过] %-18s action=%-13s severity=%-5s backend=%q\n",
			c.name, strings.ToLower(strings.TrimPrefix(resp.GetAction().String(), "ACTION_")),
			strings.ToLower(strings.TrimPrefix(resp.GetSeverity().String(), "SEVERITY_")),
			resp.GetBackend())
	}
	return nil
}

// checkShape 校验响应形状：三值闭集、severity 只在登记档位、backend 只随 route_mirage 出现。
//
// 判定响应禁止回显分值、规则名与证据（ST-7）—— 本函数同时是这条规则的形状断言。
func checkShape(name string, resp *judgev1.JudgeResponse) error {
	if resp == nil {
		return fmt.Errorf("%s：响应为空", name)
	}
	switch resp.GetAction() {
	case judgev1.Action_ACTION_ORIGIN, judgev1.Action_ACTION_MIRAGE, judgev1.Action_ACTION_BLOCK:
	default:
		return fmt.Errorf("%s：决策取值越界（三值闭集，MD-12）：%v", name, resp.GetAction())
	}
	if resp.GetSeverity() != judgev1.Severity_SEVERITY_NONE {
		return fmt.Errorf("%s：severity 只能是已登记的 none（TM-13）：%v", name, resp.GetSeverity())
	}
	if resp.GetBackend() != "" && resp.GetAction() != judgev1.Action_ACTION_MIRAGE {
		return fmt.Errorf("%s：backend 仅 route_mirage 时非空，实际 action=%v backend=%q",
			name, resp.GetAction(), resp.GetBackend())
	}
	return nil
}
