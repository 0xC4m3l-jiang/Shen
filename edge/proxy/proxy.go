// Package proxy 是接入形态③（反向代理前置）与形态④（Sidecar）的同一份实现。
//
// 它**在请求路径上**：收下客户端请求，交给核心判定，按结果选择后端。
// 两种形态的差别只在部署位置与 upstream 指向 —— 前置代理的 upstream 是业务真实地址，
// 边车的 upstream 是同 Pod 内的 127.0.0.1:<业务端口>。**代码完全相同**。
//
// 转发与 TLS 终结由**内嵌 Caddy** 承担（[ADR-0017](../../docs/background/decisions/0017-caddy-l1-base.md)）：
// `Handler` 是 Caddy 模块 `http.handlers.shen_proxy`（`caddyhttp.MiddlewareHandler`），
// 判定逻辑自研、只做四件事，转发交给 Caddy 的 `reverse_proxy`。
//
// 设计边界：本包只允许使用 api/ 生成的 proto 类型，不得 import core/internal/
// —— 这由 Go 的 internal 目录规则在编译期强制。
//
// 只做四件事（AR-6）：拦截请求 · 查本地判定缓存 · 按结果执行 · 异步上报遥测。
// 不做判定、不做 LLM 推理、不维护会话状态、不写核心状态（AR-7）。
//
// 实现按职责拆成三个文件：
//
//   - iface.go   —— 对外契约：JudgeClient / TelemetryClient / Injector 接口 + transport-agnostic 的 Config
//   - glue.go    —— 判定胶水（transport-agnostic）：decisionCache / decisionID / whitelist / clientIP / actionOf / observationFrom / injectable
//   - handler.go —— Caddy 中间件 Handler（caddyhttp.MiddlewareHandler）+ injectingTransport + trackingWriter
//   - embed.go   —— 程序化 Caddy 配置（BuildConfig）+ TLS（off|manual|acme）+ 模块注册
package proxy
