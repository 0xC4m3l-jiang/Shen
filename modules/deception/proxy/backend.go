// 本文件是**转发后端构造**这一块责任：把配置里的地址变成 Caddy 的 reverse_proxy handler。
//
// 从这个模块拆出独立文件的原因：它是唯一与 Caddy transport 细节打交道的地方 ——
// 请求处理（handler.go）、响应改写（transform.go）、判定缓存（cache.go）都不该知道
// 「上游 transport 怎么建、超时设在哪、TLS 怎么开」。
package proxy

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
)

// 上游 Transport 的连接层超时。没有超时的 Transport 会让挂死的上游一直占住连接与 goroutine。
const dialTimeout = 5 * time.Second

// 引流后端响应头超时的默认值。业务侧**不设**此超时（慢接口是业务自己的行为）。
const mirageDefaultTimeout = 10 * time.Second

// buildBackend 构造一个指向 target 的 Caddy reverse_proxy handler。
//
// isMirage 决定失败处理方式：引流后端失败可以安全回落到业务（NI-1），
// 业务本身失败则如实报错 —— **不得**用蜜罐内容顶替业务失败（INT-8）。
// 引流侧另加响应头超时（蜜罐挂死不能拖住客户端），并挂注入 transport。
func (h *Handler) buildBackend(ctx caddy.Context, target string, isMirage bool, mirageTimeout time.Duration) (*reverseproxy.Handler, error) {
	dial, tlsUpstream, err := upstreamAddr(target)
	if err != nil {
		return nil, err
	}

	tr := &reverseproxy.HTTPTransport{
		DialTimeout: caddy.Duration(dialTimeout),
	}
	if tlsUpstream {
		// 上游是 https：启用到上游的 TLS（校验策略由 TLS 配置决定）。
		tr.TLS = new(reverseproxy.TLSConfig)
	}

	// 引流侧的响应头超时**必须在 Provision 之前设置**：Provision 会据此构建内部
	// `http.Transport`（见 Caddy 的 `HTTPTransport.Provision`），在那之后赋值只改到外壳
	// 结构体，真正的 transport 仍是零超时 ⇒ 蜜罐挂死会一直拖住客户端（FIX-1）。
	// 回归用例：`TestMirageResponseHeaderTimeoutIsEffective`。
	if isMirage {
		to := mirageTimeout
		if to <= 0 {
			to = mirageDefaultTimeout
		}
		tr.ResponseHeaderTimeout = caddy.Duration(to)
	}

	// 手工构造的 transport 需要自行 Provision（才会构建内部 http.Transport）。
	if err := tr.Provision(ctx); err != nil {
		return nil, err
	}

	var rt http.RoundTripper = tr
	if isMirage {
		// 注入 transport **无条件**挂在改道侧：
		//   · 静态规则（本地 env / 策略面 `inject_rules`）与
		//   · AI 欺骗内容（策略面 `content_manifest`，`ADR-0023`）
		// 都经它执行；它同时负责上报注入结果（off / disabled / no_content / applied）。
		// 两种规则都没有时它仍会被调用，但会立即原样返回（不读 body、不改写）。
		//
		// 依赖的是 `injectionSource`（三个方法），不是整个 Handler —— 见 transform.go。
		rt = &injectingTransport{src: h, base: tr, snapshotOf: h.policySnapshotOf}
	}

	rp := &reverseproxy.Handler{
		Upstreams: reverseproxy.UpstreamPool{{Dial: dial}},
		Transport: rt,
	}
	if err := rp.Provision(ctx); err != nil {
		return nil, err
	}
	return rp, nil
}

// upstreamAddr 解析 scheme://host:port 形式的地址，返回 Caddy reverse_proxy 的
// Dial 地址（host:port，无 scheme）与是否需要到上游的 TLS。
func upstreamAddr(raw string) (dial string, tlsUpstream bool, err error) {
	if strings.TrimSpace(raw) == "" {
		return "", false, fmt.Errorf("地址为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false, err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", false, fmt.Errorf("地址必须带 scheme 与 host：%s", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return u.Host, false, nil
	case "https":
		return u.Host, true, nil
	default:
		return "", false, fmt.Errorf("不支持的 scheme：%s", raw)
	}
}
