package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddytls"
)

// init 把 Handler 注册为 Caddy 模块 `http.handlers.shen_proxy`。
// 必须在 caddy.Run 之前完成 —— init 阶段即可保证。
func init() {
	caddy.RegisterModule(new(Handler))
}

// TLSMode 是进程级 TLS 终结方式（SHEN_PROXY_TLS_MODE）。
//
// off    = 明文（默认，向后兼容现有行为）。
// manual = 手动证书 / 内部 CA（自用 / 内网场景）。
// acme   = auto-HTTPS（需公网域名 + 80/443 入站）。
type TLSMode string

const (
	TLSModeOff    TLSMode = "off"
	TLSModeManual TLSMode = "manual"
	TLSModeACME   TLSMode = "acme"
)

// TLSConfig 是进程级的 TLS 配置（env 驱动，不引入配置文件格式）。
type TLSConfig struct {
	Mode     TLSMode
	Domain   string // acme：要签发证书的域名
	Email    string // acme：可选，ACME 账户邮箱
	CertFile string // manual：证书文件路径
	KeyFile  string // manual：私钥文件路径
}

// Validate 校验 TLS 配置，缺什么当场报错，不等到启动后才发现。
func (t TLSConfig) Validate() error {
	switch t.Mode {
	case TLSModeOff:
		return nil
	case TLSModeManual:
		if strings.TrimSpace(t.CertFile) == "" || strings.TrimSpace(t.KeyFile) == "" {
			return fmt.Errorf("tls_mode=manual 需要 SHEN_PROXY_CERT_FILE 与 SHEN_PROXY_KEY_FILE")
		}
		return nil
	case TLSModeACME:
		if strings.TrimSpace(t.Domain) == "" {
			return fmt.Errorf("tls_mode=acme 需要 SHEN_PROXY_DOMAIN")
		}
		return nil
	default:
		return fmt.Errorf("未知 tls_mode：%q（应为 off|manual|acme）", t.Mode)
	}
}

// persistFalse 用于关掉 Caddy 的配置自动保存（见 BuildConfig）。
// 取地址的布尔常量不能被取址，所以用一个包级变量。
var persistFalse = false

// Options 是 BuildConfig 的输入。
type Options struct {
	// Listen 是监听地址（如 127.0.0.1:8081 或 :443）。
	Listen string

	// Handler 是已填好 JSON 配置的 shen_proxy handler（upstream / mirage / coreAddr 等）。
	// 用指针：Handler 含 atomic.Uint64（noCopy），按值传会触发 copylocks。
	Handler *Handler

	// TLS 是进程级 TLS 配置。
	TLS TLSConfig

	// GracePeriod 是优雅退出时给在途请求的收尾时间。
	GracePeriod time.Duration
}

// BuildConfig 程序化组装 Caddy 配置：一个 http server + 单条路由（shen_proxy 中间件），
// 按 TLS 模式决定是否叠加 tls app 与 TLS 监听。
//
// admin API 一律禁用（Disabled）—— 本进程不需要运行时改配置，禁用即缩攻击面。
func BuildConfig(opts Options) (*caddy.Config, error) {
	if err := opts.TLS.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.Listen) == "" {
		return nil, fmt.Errorf("proxy: Listen 不能为空")
	}

	server := &caddyhttp.Server{
		Listen: []string{opts.Listen},
		Routes: caddyhttp.RouteList{{
			HandlersRaw: []json.RawMessage{
				caddyconfig.JSONModuleObject(opts.Handler, "handler", "shen_proxy", nil),
			},
			Terminal: true,
		}},
	}

	// 错误路径的可见面卫生（OH-2）：Caddy 在**服务器层**无条件写 `Server: Caddy`，
	// 而错误响应（上游不可达的 502 等）**不经过**我们的中间件 —— 只能在这里删。
	// 正常响应的清洗在我们的中间件里做（见 handler.go 的 headerSanitizer），
	// 那里能按「上游是否给了 Server」精确判断，这里只能一律删。
	server.Errors = &caddyhttp.HTTPErrorConfig{
		Routes: caddyhttp.RouteList{{
			HandlersRaw: []json.RawMessage{
				json.RawMessage(`{"handler":"headers","response":{"delete":["Server","Via"]}}`),
			},
		}},
	}

	// TLS 监听策略必须在序列化 http app **之前**就绪（否则 useTLS 判为 false，仍回明文）。
	// 同时禁掉 auto-HTTPS 的跳转：本代理用自定义端口（非 80/443），
	// 80/443 的跳转由客户自己的 L0 负责，Caddy 不得擅自去绑 80 端口。
	switch opts.TLS.Mode {
	case TLSModeManual:
		server.TLSConnPolicies = caddytls.ConnectionPolicies{{}}
		server.AutoHTTPS = &caddyhttp.AutoHTTPSConfig{Disabled: true}
	case TLSModeACME:
		server.TLSConnPolicies = caddytls.ConnectionPolicies{{}}
		server.AutoHTTPS = &caddyhttp.AutoHTTPSConfig{DisableRedir: true}
	}

	httpApp := &caddyhttp.App{
		Servers:     map[string]*caddyhttp.Server{"shen": server},
		GracePeriod: caddy.Duration(opts.GracePeriod),
	}

	cfg := &caddy.Config{
		Admin: &caddy.AdminConfig{
			Disabled: true,
			// 除禁用 admin API 外，还要关掉**配置自动保存**：
			// Caddy 默认把整份配置（含后端地址、证书路径等）写到
			// $XDG_DATA_HOME/caddy/autosave.json（macOS：~/Library/Application Support/Caddy/）。
			// 本进程是请求路径上的边车/前置：它**不得**在容器里产生多余写入（只读根文件系统会报错）。
			Config: &caddy.ConfigSettings{Persist: &persistFalse},
		},
		AppsRaw: caddy.ModuleMap{"http": caddyconfig.JSON(httpApp, nil)},
	}

	switch opts.TLS.Mode {
	case TLSModeOff:
		// 明文，无需额外配置。
	case TLSModeManual:
		tlsApp := &caddytls.TLS{
			CertificatesRaw: caddy.ModuleMap{
				"load_files": caddyconfig.JSON(caddytls.FileLoader{{
					Certificate: opts.TLS.CertFile,
					Key:         opts.TLS.KeyFile,
				}}, nil),
			},
		}
		cfg.AppsRaw["tls"] = caddyconfig.JSON(tlsApp, nil)
	case TLSModeACME:
		tlsApp := &caddytls.TLS{
			CertificatesRaw: caddy.ModuleMap{
				"automate": caddyconfig.JSON(caddytls.AutomateLoader{opts.TLS.Domain}, nil),
			},
		}
		cfg.AppsRaw["tls"] = caddyconfig.JSON(tlsApp, nil)
	}

	return cfg, nil
}
