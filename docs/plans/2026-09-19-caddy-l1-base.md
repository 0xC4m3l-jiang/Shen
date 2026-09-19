# 变更包：L1 反向代理底座换成内嵌 Caddy（adapter-proxy 重构）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | `edge/proxy`（`adapter-proxy`）转发层从 `httputil.ReverseProxy` 换成内嵌 Caddy |
| 日期 | 2026-09-19 |
| 状态 | 已验证 |
| 涉及模块 | `adapter-proxy`（[`../design/modules.md`](../design/modules.md) §1.1 第 11 行，阶段 2a） |
| 决策数 | 已答 4 项 / 待定 2 项 |
| 关联 | [ADR-0017](../background/decisions/0017-caddy-l1-base.md) · 上轮 [`2026-09-18-docs-wiring-cleanup.md`](2026-09-18-docs-wiring-cleanup.md) · [`docs/log.md`](../log.md) |

---

## 1. 需求与验收

**要解决什么**：把 `adapter-proxy` 的转发层从 Go 标准库 `net/http/httputil.ReverseProxy` 换成**内嵌 Caddy**，
由 Caddy 承担转发与 TLS 终结；判定逻辑（白名单 → `decision_id` → 缓存 → gRPC 判定 → 三值路由）保持自研 Go 插件不变。

**做完之后**：`cmd/proxy` 单进程同时承担 L0（TLS 终结）+ L1（判定路由），`SHEN_PROXY_TLS_MODE`
（`off|manual|acme`）驱动 TLS；`route_origin` / `route_mirage` 交给 Caddy `reverse_proxy`，
`block` 短路 403，mirage→origin 回落语义不变。

**验收判据**：

1. `make gate` 绿（含 `-race`、`make trace`、`make licensecheck` —— Caddy 依赖无 AGPL/SSPL/BSL）。
2. `make dev` 绿。
3. 内嵌 Caddy 端到端集成测试通过：动态后端 + 响应注入 + TLS 握手 + mirage→origin 回落。

**不做什么**：形态① mirror、② DNS 不动；`edge/injection` 逻辑不变（`Injector` 接口保留）；
不引入 Caddyfile DSL；`acme` 仅提供骨架（`manual` 为主路径）。

---

## 2. 设计逻辑

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 集成方式 | 内嵌库（`import caddy/v2`，`caddy.Run()`），不用 xcaddy / Caddyfile | 单进程、程序化配置、走 `make gate` 同一套门禁 | `edge/proxy/embed.go` |
| ② | 配置面 | 全 env（`SHEN_PROXY_*`），Caddy 配置程序化生成 | 适配器不引入配置解析库 | `cmd/proxy/main.go` |
| ③ | TLS | Caddy 终结：`off|manual|acme`，默认 `off` 保持向后兼容 | 满足 `INT-22`（TLS 可读），结案「TLS 归谁」 | `embed.go` `BuildConfig` |
| ④ | Handler 形态 | 从 `http.Handler` 改为 `caddyhttp.MiddlewareHandler`（Caddy 模块 `http.handlers.shen_proxy`） | 转发交给 Caddy `reverse_proxy`，判定胶水不写转发 | `handler.go` |

**仍未定**：

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | `acme` 的邮箱 / issuer / 80-443 入站前提 | 公网域名接入 | ADR-0017「未解决」 |
| 2 | Caddy 内嵌的二进制体积控制（`-trimpath` / 剥离可选模块） | 最小镜像 | 接入演练实测 |

**接缝与接口**：`JudgeClient` / `TelemetryClient` / `Injector` 三个接口**不变**；`edge/proxy` 现
`import edge/injection`（同层 L1→L1，非跨平面，`make archcheck` 通过）在 `Provision` 里建注入器。

**数据流**（失败路径）：白名单（`INT-25`）→ 缓存 → gRPC 判定（≤3ms，失败折叠放行 `NI-3/4/5`）→
三值路由：`route_origin`→Caddy `reverse_proxy`(origin)；`route_mirage`→注入 transport + 失败回落 origin（`NI-1`）；
`block`→403（`ADR-0002` 语义不变）。

---

## 3. 追溯矩阵

| 规则 ID | 模块文档 | 代码 | 测试 | 验证命令 |
| --- | --- | --- | --- | --- |
| `AR-3`（L0 复用现成组件） | `adapter-proxy.md` §1/§3 | `embed.go` `BuildConfig` + Caddy | `embed_test.go::TestEmbeddedCaddyEndToEnd` | `make gate` |
| `AR-4`（每层只选一个组件） | `adapter-proxy.md` §1 | `embed.go` 单进程 Caddy | 同上 | `make gate` |
| `AR-6`（四件事） | `adapter-proxy.md` §1 | `handler.go` `ServeHTTP` | `proxy_test.go` 多例 | `make gate` |
| `AR-7`（适配器禁止判定） | `adapter-proxy.md` §1 | 判定只在核心，proxy 只做胶水 | `proxy_test.go` | `make gate` |
| `INT-8`（只改蜜罐侧） | `adapter-proxy.md` §1/§6 | `handler.go` `injectingTransport` | `proxy_test.go::TestInjectingTransport*` | `make gate` |
| `INT-11`（影子模式） | `adapter-proxy.md` §4 | `handler.go` `dispatch` | `TestShadowNeverDiverts` | `make gate` |
| `INT-22`（TLS 可读 body） | `adapter-proxy.md` §8 | `embed.go` TLS 终结 | `TestTLSConfigValidate` | `make gate` |
| `INT-23`（来源 IP） | `adapter-proxy.md` §4 | `glue.go` `clientIP` | `TestTrustXFF*` | `make gate` |
| `INT-25`（白名单先于判定） | `adapter-proxy.md` §4 | `handler.go` `ServeHTTP` ① | `TestWhitelistSkipsCore` | `make gate` |
| `NI-1`（不影响业务） | `adapter-proxy.md` §6 | `handler.go` `forwardMirage` | `TestMirageBackendDownFallsBackToOrigin` + 集成测试 | `make gate` |
| `NI-3/4/5`（失败放行/超时/不确定回落） | `adapter-proxy.md` §6 | `handler.go` `decide` / `actionOf` | `TestCoreError/Timeout/Unspecified*` | `make gate` |
| `ST-10`（decision_id） | `adapter-proxy.md` §4 | `glue.go` `decisionID` | `TestDecisionID*` | `make gate` |
| `ST-11`（超时降级） | `adapter-proxy.md` §4 | `handler.go` `decide` | `TestCoreTimeoutFallsBackToOrigin` | `make gate` |
| `MD-10`（缓存容量上限） | `adapter-proxy.md` §5 | `glue.go` `decisionCache` | `TestCacheRespectsCapacityCap` | `make gate` |
| `MD-22`（测试独立） | `adapter-proxy.md` §7 | 替身 `stubJudge`/`stubReporter` | 全测试 | `make gate` |
| `TB-16`（许可审计） | `spec/dependencies.md` | `go.mod` caddy | `licensecheck` | `make gate` |
| `TB-24`（禁止 CGO） | `structure.md` §2.2 | 纯 Go | `archcheck` | `make gate` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `edge/proxy/glue.go` | 新增 | 判定胶水（`decisionCache`/`decisionID`/`whitelist`/`clientIP`/`actionOf`/`observationFrom`/`injectable`）transport-agnostic 迁移 |
| `edge/proxy/handler.go` | 新增 | Caddy 中间件 `Handler`：`caddyhttp.MiddlewareHandler` + `Provision`/`Validate`/`Cleanup` + `injectingTransport` + `trackingWriter` |
| `edge/proxy/embed.go` | 新增 | 程序化 Caddy 配置（`BuildConfig`）+ TLS（`off|manual|acme`）+ 模块注册 |
| `edge/proxy/proxy.go` | 删除 | 旧 `httputil.ReverseProxy` 转发层，被 glue.go + handler.go 取代 |
| `edge/proxy/iface.go` | 修改 | 包注释改为 Caddy 底座；`Config` + 三接口**不变** |
| `edge/proxy/proxy_test.go` | 重写 | 19 例移植到 Caddy handler（替身后端 fakeBackend）+ 注入 transport 单测 |
| `edge/proxy/embed_test.go` | 新增 | 端到端集成测试（= spike）：真实 Caddy + gRPC + TLS + 回落 |
| `edge/proxy/cmd/proxy/main.go` | 重写 | env 解析（含 `SHEN_PROXY_TLS_*`）→ 程序化配置 → `caddy.Run` → 优雅退出 |
| `edge/proxy/config/front-proxy.example.env` | 修改 | 新增 TLS env 示例 |
| `edge/proxy/README.md` | 修改 | 底座描述 |

**关键类型**：`Handler`（导出 Caddy 模块 `http.handlers.shen_proxy`）、`TLSConfig`/`Options`/`BuildConfig`（导出装配 API）；
`injectingTransport`/`trackingWriter`/`decisionCache`（内部）。三个对外接口 `JudgeClient`/`TelemetryClient`/`Injector` **不变**。

**上位约束**：`AR-3`（复用现成组件）· `AR-4`（单一组件）· `AR-6`（四件事）· `TB-16`（许可）· `ST-3`（不 import core/internal）。

---

## 5. 测试与场景

| # | 场景 | 输入/前置 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 1 | 白名单先于判定 | 127.0.0.0/8 命中 | 透传 origin，不调核心 | ✅ | `TestWhitelistSkipsCore` |
| 2 | 影子模式永不改道 | Shadow=true | 照算判定但到 origin | ✅ | `TestShadowNeverDiverts` / `TestShadowAlsoSuppressesBlock` |
| 3 | route_mirage 改道 | MIRAGE→hp | 到 hp | ✅ | `TestMirageRoutesToNamedBackend` |
| 4 | 后端名查不到回落 | MIRAGE→未知名 | 到 origin（`NI-5`） | ✅ | `TestUnknownBackendFallsBackToOrigin` |
| 5 | 引流后端不可达回落 | mirage down | 到 origin（`NI-1`） | ✅ | `TestMirageBackendDownFallsBackToOrigin` + 集成测试 `/dead` |
| 6 | 核心失败/超时/非法值 | err / delay / UNSPECIFIED | 放行 origin | ✅ | `TestCoreError/Timeout/Unspecified*` |
| 7 | block 短路 | BLOCK | 403 | ✅ | `TestBlockReturnsForbidden` + 集成测试 `/block` |
| 8 | 判定缓存 | 同 decision_id ×3 | 只调核心 1 次 | ✅ | `TestCacheAvoidsSecondCoreCall` |
| 9 | decision_id 派生 | 同窗口同请求 / 跨窗口 / 跨路径 | 稳定 / 变化 | ✅ | `TestDecisionID*` |
| 10 | 请求体不破坏 | POST body | 原样到业务 | ✅ | `TestUpstreamReceivesIntactBody` |
| 11 | 来源 IP 透传 | XFF | `TrustXFF` 生效/忽略 | ✅ | `TestTrustXFF*` |
| 12 | 注入只改蜜罐侧 | HTML mirage | 注入；origin 不注入 | ✅ | `TestInjectingTransport*` + 集成测试 `/mirage` vs `/` |
| 13 | TLS 配置校验 | off/manual/acme/非法 | 缺字段报错 | ✅ | `TestTLSConfigValidate` |
| 14 | 配置装配校验 | 空 Listen / 非法 tls_mode | 报错 | ✅ | `TestBuildConfigRejectsBadOptions` |
| 15 | 内嵌 Caddy 端到端 | 真实 Caddy + gRPC + 自签 TLS | 三值路由 + 注入 + 回落 + TLS 握手 | ✅ | `TestEmbeddedCaddyEndToEnd` |

**没有覆盖的**：`acme` 真实签发（无公网域名环境）；`AR-29` P99 实测（登记为 `E3` 实验，待接入演练）；
真实 WebSocket 升级经 Caddy reverse_proxy（`trackingWriter` 已保 `Hijack`/`Flush`/`Unwrap` 接口，但未做裸 TCP 升级测试）。

---

## 6. 验证证据

```console
$ make gate
…
✓ github.com/caddyserver/caddy/v2  Apache-2.0  允许
许可审计通过：没有传染性或限制性许可。
go test -race ./...
ok  	shen/edge/proxy	1.987s
门禁通过。

$ make trace
追溯检查通过。

$ make dev
✅ 通过：配置校验 · 非法配置被拒 · 启动 · 判定面 · 冒烟 · 规则回放

$ go test ./edge/proxy/ -run TestEmbeddedCaddyEndToEnd -v
--- PASS: TestEmbeddedCaddyEndToEnd (0.02s)
```

**关键指标**：`edge/proxy` 29 个测试全绿（含 `-race`）；Caddy 依赖许可全为 Apache-2.0 / MIT / BSD-3-Clause / CC0，
无 AGPL / SSPL / BSL。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | `acme` 签发细节（邮箱 / issuer / 80-443 入站） | 公网域名接入 | ADR-0017「未解决」 |
| 2 | Caddy 二进制体积控制 | 最小镜像 | 接入演练实测 |
| 3 | `AR-29` P99 实测 | 能否按形态 ③④ 上线 | `E3` 实验（pending-experiments.md） |

---

## 7.1 审视记录（L 档，做法见全局技能 `audit`）

| # | 发现 | 类型 | 证据（命令/位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 旧 `proxy.go`（`httputil.ReverseProxy`）已删，无悬空引用 | 删除 | `grep -rl "httputil" edge/proxy/` 空；`go build ./edge/proxy/` 绿 | 已删，理由：转发层被 Caddy 取代（ADR-0017） | ✅ |
| 2 | `edge/proxy` 新增 `import edge/injection`（原「不 import」注释作废） | 漂移 | `iface.go` 旧注释 vs `handler.go` 实际 import | 已更新 `iface.go` 包注释与 `adapter-proxy.md` §3 依赖表 | ✅ |
| 3 | `Config` struct 与 `Handler` JSON 字段并存（两套配置视图） | 语义 | `iface.go`（Config）vs `handler.go`（Handler 字段） | Config 保留为 transport-agnostic 测试面，Handler JSON 字段为 Caddy 模块面；`adapter-proxy.md` §2 已注明 | ✅ |
| 4 | `structure.md` §1.6.1 进程 3 仍写「HTTP 服务入口 / net/http」旧描述 | 漂移 | `structure.md` §1.6.1 | 已由并发同步更新为 Caddy 底座 | ✅ |
| 5 | `make trace` 的 3 条豁免与 1 条提示均为既有项，非本轮引入 | 已登记 | `scripts/tracecheck/allow.txt` · TC-1（edge/mirror 缺 iface.go） | 保持，未新增豁免 | ✅ |
| 6 | `iface.go` 的 `Injector` 注释写着「不 import edge/injection —— 由装配层接上实现」，但 `handler.go` 已在 `Provision` 里 import 并建注入器 | **漂移（文档↔代码）** | `grep -n "shen/edge/injection" edge/proxy/handler.go` vs 旧注释 | 改注释说清边界（`MD-4` 禁的是**适配器之间**互依；`ST-5` 要求处置模块**被适配器引用**），并在 `docs/modules/adapter-proxy.md` §3 新增 `edge/injection` 依赖行 | ✅ |
| 7 | 另一处旧计数：「19 单测」仍留在 `docs/design/structure.md` §1.5 与 `docs/modules/README.md` §4.2 | **过期状态** | `grep -rn "19 单测" docs/` | 改为 29 测试（含内嵌 Caddy 端到端） | ✅ |
| 8 | 本轮新增的 `embed_test.go` 未过 `gofmt` 且有一处未检查的 `resp.Body.Close()`（`errcheck`） | 门禁红 | `make gate` → `fmt-check` / `errcheck` 失败 | 已格式化并改为 `defer func() { _ = resp.Body.Close() }()` | ✅ |
| 9 | 计划验收里要求的「`tls_mode` 校验、装配校验」用例缺失（只有配置字段没有测） | 漏洞 → 补齐 | `grep -n "TestTLSConfigValidate" edge/proxy/` 原本为空 | 新增 `TestTLSConfigValidate`（7 例）与 `TestBuildConfigRejectsBadOptions`（3 例） | ✅ |
| 10 | **真二进制验证揭出真缺陷**：内嵌 Caddy 默认把整份配置写到 `$XDG_DATA_HOME/caddy/autosave.json`（macOS 为 `~/Library/Application Support/Caddy/`），请求路径上的边车/前置**不得**产生这种写入（只读根文件系统会报错） | 缺陷（部署面） | 实跑 `/tmp/shen-proxy` 的日志行 `autosaved config (load with --resume flag)` + 家目录出现 `autosave.json` | `BuildConfig` 增加 `Admin.Config.Persist=false` 关闭自动保存；新增 `TestBuildConfigDisablesAdminAndAutosave` 锁定；重跑真二进制已无该日志行且文件不再生成 | ✅ |
| 11 | **同一轮实跑发现但仍存在**：certmagic 仍在 `$XDG_DATA_HOME/caddy/` 写 `instance.uuid` / `last_clean.json` / `locks`（Caddy 核心**没有**内存存储模块，改不了；只能文档化） | 已知限制 → 文档化 | 实跑后 `ls ~/Library/Application\ Support/Caddy/` | 登记为 `adapter-proxy.md` §5 + §8 未决项 9（容器必须可写或把 `XDG_DATA_HOME` 指向可写路径） | ✅ |

> 历史记录类文件（`docs/background/` · `docs/plans/` · `docs/log.md` · `docs/kb/`）不在审视范围。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 转发层 `httputil.ReverseProxy` → 内嵌 Caddy；Handler 改 `caddyhttp.MiddlewareHandler`；TLS 由 Caddy 终结（`off|manual|acme`） | ADR-0017 · 用户「复用 Caddy」 |
