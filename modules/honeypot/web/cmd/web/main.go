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
	"strconv"
	"strings"
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

	h := web.New(web.Options{
		ScenarioID:   scenarioID,
		MaxBodyBytes: maxBody,
		SessionTTL:   ttl,
		Events:       eventLogger{},
	})
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

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("web: 监听失败：%v", err)
	}
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

// ── env 解析（与其它适配器同一套写法：写错即启动失败，不悄悄回落）──────────────

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
