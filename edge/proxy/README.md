# 适配器 · ③ 反向代理前置 + ④ Sidecar

| 项 | 值 |
| --- | --- |
| 所属层 | L1 |
| 语言 | Go（[ADR-0008](../../docs/background/decisions/0008-edge-language-go.md)） |
| 底座 | **内嵌 Caddy**（Apache-2.0）：转发 + TLS 终结（[ADR-0017](../../docs/background/decisions/0017-caddy-l1-base.md)） |
| 阶段 | **2a**（接管与改道打通）· **消费策略面**（`api/policy/v1`）|
| 模块文档 | [`../../docs/modules/adapter-proxy.md`](../../docs/modules/adapter-proxy.md) |
| 模块清单 | [`../../docs/design/modules.md`](../../docs/design/modules.md) §1.1 第 11 行 |

## 它是什么

**在请求路径上**：收下客户端请求 → 交给核心判定 → 按结果选择后端。

**③ 与 ④ 是同一份代码的两种部署形态**，差别只在环境变量：

| 形态 | 部署位置 | `SHEN_PROXY_UPSTREAM` |
| --- | --- | --- |
| ③ 反向代理前置 | 独立节点 / 独立 Pod | 业务真实地址，如 `http://10.0.0.20:9000` |
| ④ Sidecar | 业务 Pod 内 | `http://127.0.0.1:9000` |

转发与 TLS 终结由**内嵌 Caddy** 承担；本模块是 Caddy 中间件 `http.handlers.shen_proxy`，
只产出「决定」与「后端地址」—— 模块里没有一行转发逻辑，也没有判定逻辑。

## 只做四件事（`AR-6`）

1. 拦截请求；
2. 查本地判定缓存，未命中才调核心（`api/judge/v1`）；
3. 按结果执行 —— `route_origin` 透传 · `route_mirage` 引流 · `block` 拦截；
4. 异步上报遥测（`api/telemetry/v1`）。

外加两件与改道正确性直接相关的事：**白名单先于改道判定**（`INT-25`）、**透传真实来源 IP**（`INT-23`）。

## 从策略面取改道后端 · 白名单 · 响应改写规则（`S4`）

适配器按间隔向核心拉取策略（`api/policy/v1` 的 `Pull`），把**改道后端表**与**白名单**应用到运行态：

| 项 | 语义 |
| --- | --- |
| 合并 | **远端覆盖本地**（后端表按逻辑名覆盖）；**白名单取并集**（护栏只增不减，`INT-25`）；**响应改写规则**：字段缺省 = 沿用本地 env，出现（含空数组）= 远端整份接管 |
| 完整性 | 先验 `sha256(payload) == checksum` 再应用（`ST-8`）；不匹配 → 拒绝 + 回执 `applied=false` |
| 失败 | 拉不到就**沿用当前策略**（本地 env 兜底）—— 策略面不是请求路径上的依赖（`NI-1`） |
| 回执 | 应用成功/失败都回执，带适配器标识（`AR-13` 版本对账） |
| 未启用 | `SHEN_PROXY_POLICY_INTERVAL=0` 时完全不拉，只用本地 env |

载荷格式见 [`../../docs/spec/policy-payload.md`](../../docs/spec/policy-payload.md)；决策背景见 [ADR-0018](../../docs/background/decisions/0018-policy-plane-pull-model.md)。

## 默认是影子模式

`SHEN_PROXY_SHADOW=true` 时照算判定并上报，但**永不改道、永不拦截**（`INT-11`）。
首次上线必须如此。放开要按 `INT-12` 的阶梯逐级推进。

## TLS 终结（`INT-22`）

**默认是 `off`（交客户 L0 终结）** —— 因为 `E2` 实测显示：本栈（Go 标准库 crypto/tls）与常见公有站栈在 **ServerHello 扩展顺序**上可区分（JA3S `772,4865,43-51` vs `772,4865,51-43`），
对手看一眼握手就能把我们与真实站分开。交给客户 L0（**与真实站同款栈**）终结，指纹**天然一致**，且我们收到的仍是明文（`INT-22` 前提不破）。
自终结保留给「客户没有 L0」的场景，**启用时会在启动日志打警告**。依据 [ADR-0019](../../docs/background/decisions/0019-tls-termination-belongs-to-l0.md)。

`SHEN_PROXY_TLS_MODE` 三取值：

| 取值 | 含义 | 需要 |
| --- | --- | --- |
| `off`（默认） | 明文，等价于旧行为（TLS 交客户 L0） | —— |
| `manual` | 手动证书 / 内部 CA（自用、内网） | `SHEN_PROXY_CERT_FILE` · `SHEN_PROXY_KEY_FILE` |
| `acme` | auto-HTTPS，Caddy 自动签发与续期 | `SHEN_PROXY_DOMAIN`（需公网域名 + 80/443 入站） |

`INT-22` 要求 TLS 能读出请求体才允许启用误导处置 —— 由本进程终结 TLS 即满足这个前提。
配置缺字段**启动即失败**，不等到第一个请求；TLS 握手失败**禁止**降级成明文。

## 目录内容

| 路径 | 内容 |
| --- | --- |
| `iface.go` | 对外契约：`JudgeClient` / `TelemetryClient` / `Injector` / `Config` |
| `handler.go` | 判定胶水 + 三值路由：Caddy 模块 `http.handlers.shen_proxy` |
| `glue.go` | 纯函数：`decision_id` · 白名单 · 观测构造 · 注入判据 · 判定缓存 |
| `embed.go` | 进程装配：`TLSConfig` 校验 · `BuildConfig`（http server + tls app） |
| `policy.go` | 策略面客户端：拉取 → 验校验和/schema → 合并应用（后端表覆盖 · 白名单并集 · 注入规则接管）→ 回执 |
| `proxy_test.go` | 单测 25 例（全部用替身，不依赖真实核心） |
| `policy_test.go` | 策略面单测 9 例：应用语义 · 注入规则三态 · 失败路径 · 回执 |
| `embed_test.go` | 单测 3 例（TLS 三取值 / 装配校验 / 关自动保存）+ 端到端 1 例（真 Caddy + 真 gRPC） |
| `cmd/proxy/` | 进程入口（环境变量 → `BuildConfig` → `caddy.Run`） |
| `config/` | 配置模板：`front-proxy.example.env`（③）· `sidecar.example.yaml`（④） |

## 关于「不读请求体」

本模块**刻意不读请求体**。它在请求路径上，读 body 会把上游收到的请求体吃掉；
全量缓冲大文件上传还会同时拖垮延迟与内存。判定面契约（`api/judge/v1` 的 `Observation`）
里也没有 body 字段 —— 阈值校准用的是头与路径。

（形态 ① 的接收端可以放心读，因为它处理的是副本。两处的差异是刻意的。）

`INT-22` 要求**启用误导处置**时能读出请求体 —— 本模块已用内嵌 Caddy 终结 TLS，
但「把请求体送进判定/处置链」仍需契约与阶段 2b 的设计。

## 怎么跑

```sh
make build
SHEN_PROXY_UPSTREAM=http://127.0.0.1:9000 go run ./edge/proxy/cmd/proxy
```

配 TLS：

```sh
SHEN_PROXY_TLS_MODE=manual \
SHEN_PROXY_CERT_FILE=/etc/shen/tls/tls.crt \
SHEN_PROXY_KEY_FILE=/etc/shen/tls/tls.key \
SHEN_PROXY_UPSTREAM=http://127.0.0.1:9000 \
go run ./edge/proxy/cmd/proxy
```

配置项见 [`config/front-proxy.example.env`](config/front-proxy.example.env)。

## 状态

✅ **已实现**（29 个测试用例，含 `-race`；`make gate` 绿）。

包含一条**内嵌 Caddy 端到端测试**（`TestEmbeddedCaddyEndToEnd`）：真 Caddy + 真 `reverse_proxy`
+ 真 gRPC 判定面，验证 TLS 握手、三值路由、引流侧注入、引流失败回落、block 短路。

⚠️ **端到端引流尚未打通**：核心当前是 `control.ShadowDecider`，恒返回 `route_origin`，
无法自己造出 `route_mirage`。要看到真实改道，需要 `director`（§1.1 第 2 行，阶段 `2a`）。
本模块侧已用替身与端到端测试验证过改道路径。

⚠️ **`AR-29`（P99 ≤ 5 ms）换底座后未重测** —— 见
[`../../docs/modules/adapter-proxy.md`](../../docs/modules/adapter-proxy.md) §8 未决项 5/6。
