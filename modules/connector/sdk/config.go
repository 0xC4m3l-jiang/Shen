// Package connector 是反向隧道的**业务侧 SDK**：真实 Web 服务用三行代码接入蜃楼。
//
//	c, err := connector.Connect(ctx, connector.Config{
//	    Gateway:  "shen-gw.example.com:9446",
//	    AuthKey:  os.Getenv("SHEN_CONNECTOR_KEY"),
//	    Upstream: "http://127.0.0.1:8080",
//	    Name:     "shop",
//	    Hosts:    []string{"shop.example.com"},
//	})
//	defer c.Close() // 断线自动重连直到 ctx 取消
//
// 数据面语义：网关每转发一个连接，就经隧道开一条流；SDK accept 流并与
// Upstream 建一条 TCP 连接做双向 io.Copy —— 业务侧零暴露入站端口。
package connector

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// SDKVersion 是连接器自报版本（网关与控制台观测展示）。
const SDKVersion = "shen-sdk/v1.0.0"

// Config 是接入参数（环境变量同名映射见 cmd/shen-connector）。
type Config struct {
	// Gateway 是网关地址（host:port，如 shen-gw.example.com:9446）。
	Gateway string
	// AuthKey 是控制台签发的接入凭证（shc- 开头；签发时一次性显示）。
	AuthKey string
	// Upstream 是本地真实业务地址（http://127.0.0.1:8080）。
	Upstream string
	// Name 是服务名（必须与凭证一致）。
	Name string
	// Hosts 是声明的域名（必须是凭证白名单的子集）。
	Hosts []string

	// TLS 可覆盖默认配置（默认走系统根证书、TLS 1.2+）。
	// 本地开发的自签证书可用 &tls.Config{InsecureSkipVerify: true}（勿用于生产）。
	TLS *tls.Config

	// 重连参数：指数退避从 MinBackoff 起步，每失败翻倍封顶 MaxBackoff，带随机抖动。
	MinBackoff time.Duration // 默认 1s
	MaxBackoff time.Duration // 默认 30s

	// DialTimeout 是单次拨号超时（默认 10s）。
	DialTimeout time.Duration

	// Logf 是状态日志（默认静默；二进制入口接标准日志）。
	Logf func(format string, args ...any)

	// dialAddr 是 normalize 后的拨号地址（Upstream 去掉 scheme 后的 host:port）。
	// Upstream 原样保留（含 scheme）：它要随握手上报控制台做自动登记。
	dialAddr string
}

func (c *Config) normalize() error {
	c.Gateway = strings.TrimSpace(c.Gateway)
	if c.Gateway == "" {
		return fmt.Errorf("connector: Gateway 不能为空")
	}
	if _, _, err := net.SplitHostPort(c.Gateway); err != nil {
		return fmt.Errorf("connector: Gateway 须为 host:port 形态：%q", c.Gateway)
	}
	if !strings.HasPrefix(c.AuthKey, "shc-") {
		return fmt.Errorf("connector: AuthKey 应为控制台签发的 shc- 凭证")
	}
	u, err := url.Parse(strings.TrimSpace(c.Upstream))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("connector: Upstream 须为 http(s)://主机[:端口]，如 http://127.0.0.1:8080")
	}
	c.dialAddr = u.Host                        // 拨号只用 host:port；路径与查询由隧道里的原始请求自带
	c.Upstream = strings.TrimSpace(c.Upstream) // 原样保留（含 scheme）：握手 meta 上报控制台登记
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		return fmt.Errorf("connector: Name 不能为空")
	}
	if len(c.Hosts) == 0 {
		return fmt.Errorf("connector: Hosts 至少声明一个域名")
	}
	if c.MinBackoff <= 0 {
		c.MinBackoff = time.Second
	}
	if c.MaxBackoff < c.MinBackoff {
		c.MaxBackoff = 30 * time.Second
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = 10 * time.Second
	}
	if c.Logf == nil {
		c.Logf = func(string, ...any) {}
	}
	return nil
}

// tlsConfig 组装拨号 TLS 配置。
func (c *Config) tlsConfig() *tls.Config {
	if c.TLS != nil {
		return c.TLS
	}
	return &tls.Config{MinVersion: tls.VersionTLS12} // 系统根证书
}
