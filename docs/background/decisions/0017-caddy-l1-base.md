# 0017. L1 反向代理底座：内嵌 Caddy + 自研 Go 插件（替代 Go 标准库自研转发层）

- 状态：✅ **已采纳**
- 日期：2026-09-19
- 影响范围：[`../../design/language.md`](../../design/language.md) §1 的 **L1 行**；[`../../design/architecture.md`](../../design/architecture.md) 的 `AR-3` / `AR-4` 注释、§7 拓扑、§8.5 步骤 1 行；[`../../design/integration.md`](../../design/integration.md) 的 `INT-22` 落地方式；[`../../design/modules.md`](../../design/modules.md) §1.1 第 11 行底座描述；[`../../design/structure.md`](../../design/structure.md) §1.3 / §1.6；[`../../modules/adapter-proxy.md`](../../modules/adapter-proxy.md)
- 决策者：用户
- 依据：`TB-4`（替换某层选型**必须**先有决策记录）；用户明确「复用 Caddy」

---

## 背景

[`adapter-proxy.md`](../../modules/adapter-proxy.md) 原实现用 Go 标准库 `net/http/httputil.ReverseProxy`
做转发层，判定逻辑（白名单 → `decision_id` → 缓存 → gRPC 判定 → 三值路由）自研。
`adapter-proxy.md` §8 留下未决项 #1「**TLS 终结归谁**」，且讨论稿 D13（
[`../notes/implementation-discussion.md`](../notes/implementation-discussion.md) §6.1）提出
「Caddy 核心 + 只写 Go 插件」替代自研 `adapter-proxy` 的转发层，当时未拍板。

用户随后明确「**复用 Caddy**」，把 L1 反向代理的底座从「Go 标准库自研转发」换成「内嵌 Caddy」。

## 候选

| 候选 | 内容 | 真实优势 | 代价 |
| --- | --- | --- | --- |
| **A** | **内嵌 Caddy**（`github.com/caddyserver/caddy/v2`）| 复用成熟转发 + TLS 终结（auto-HTTPS / 手动证书）；单进程同时承担 L0（TLS）与 L1（判定路由），满足 `AR-4`（不叠加第二层 L7 代理） | 传递依赖多（certmagic / smallstep / quic-go 等），需许可审计与体积评估 |
| **B** | **维持 Go 标准库 `httputil.ReverseProxy` 自研**（现行基线） | 零新依赖，门禁摩擦最小 | 需自研 TLS 终结（或依赖上游 L0 卸载）；「TLS 归谁」未决项悬而未决，`INT-22`（误导处置需读请求体）无法在形态 ③④ 落地 |
| **C** | **Envoy + Go `ext_proc`** | 复用 Envoy 的 L0 能力 | 两组件才能完成形态 ③；边车形态两进程挤在 Pod 里（ADR-0008 已否决） |

## 决定

采用**候选 A（内嵌 Caddy）**：

1. 转发层与 TLS 终结由内嵌 Caddy 承担（`import github.com/caddyserver/caddy/v2`，程序化构建 server + TLS，`caddy.Run()`）；
2. 判定逻辑保持自研 Go 插件不变 —— `edge/proxy` 的 `Handler` 实现 `caddyhttp.MiddlewareHandler`
   （Caddy 模块 `http.handlers.shen_proxy`），转发前做判定，按结果把后端交给 Caddy `reverse_proxy`；
3. **不用 xcaddy、不用 Caddyfile DSL** —— 配置统一走环境变量（`SHEN_PROXY_*`），Caddy 自身配置程序化生成；
4. **TLS 由 Caddy 终结** —— `SHEN_PROXY_TLS_MODE`（`off|manual|acme`，默认 `off` 保持向后兼容）。

## 理由

1. **复用 Caddy 的成熟转发与 TLS**，不自研 L0（`AR-3`）。Go 标准库 `ReverseProxy` 是「现成实现」
   但**没有 TLS 终结 / auto-HTTPS / 证书管理**；Caddy 把这些一并覆盖，且比自研更久经考验。
2. **单组件满足 `AR-4`**：Caddy 单进程同时承担 L0（TLS 终结）+ L1（判定路由），
   不叠加第二层七层代理（ADR-0008 候选 B 的「TLS 归谁」缺口就此补上）。
3. **同语言零门禁摩擦**：Caddy 是 Go（Apache-2.0），`make gate` 的 fmt / vet / staticcheck / errcheck /
   `-race` 全适用，不需要第二套工具链（对比 ADR-0008 否决 Lua 的理由）。
4. **`INT-22` 可落地**：TLS 由 Caddy 终结后，形态 ③④ 的引擎能读到请求体，误导处置的前提满足
   （当前契约尚无 body 字段，仍是阶段 2b 的事，但「TLS 归谁」这个前提已结案）。

## 与 `AR-3` / `AR-4` 的关系

- `AR-3`（L0 禁止自研）：Caddy 是现成开源组件（Apache-2.0），满足「复用现成组件」。
- `AR-4`（每层只选一个组件）：Caddy 单进程承担 L0+L1，请求路径上**只有一个代理进程**，
  不叠加第二层七层代理。客户原有的 LB 属 L0 且职责不同（入口路由），与 `AR-4` 举例
  「Istio 与 Cilium 同时争抢流量控制」不是一类问题。

## 后果

| 类型 | 内容 |
| --- | --- |
| ✅ 正面 | TLS 终结（auto-HTTPS / 手动证书 / 内部 CA）就绪；`INT-22` 前提满足；转发与 TLS 复用久经考验的实现；`AR-4` 单组件达成 |
| ⚠️ 负面 | Caddy 传递依赖多（certmagic / smallstep / quic-go / otel 等），二进制体积与依赖审计成本上升；内嵌 Caddy 的 API 面较大（需摸清模块注册 / Provision / TLS 策略） |
| 🔧 需同步 | `language.md` §1 L1 行 · `architecture.md`（AR-3/AR-4 注释、§7、§8.5）· `integration.md`（INT-22）· `modules.md` §1.1 第 11 行 · `structure.md` §1.3/§1.6 · `adapter-proxy.md` · `spec/dependencies.md` 台账 |

## 失效条件

出现下列任一情形时**必须**重开本记录：

- **许可或体积失控** —— Caddy 依赖引入 AGPL / SSPL / BSL 传染性许可（`TB-16`），或二进制体积 /
  镜像尺寸超出接入形态的可接受范围 → 重开（本轮 `make licensecheck` 已确认无传染性许可，见变更包 §6）；
- **`AR-29` 实测超标** —— 形态 ③④ 接入演练实测「判定 + TLS」的 P99 明显超出 5 ms 预算，
  且候选 B 在同条件下达标 → 重开；
- **Caddy 内嵌无法可靠表达某种必需的接入语义**（本轮 spike 已验证四件事：动态后端 + 注入 +
  TLS + mirage→origin 回落均可表达）→ 重开。

## 与 ADR-0008 的关系

ADR-0008 决定「L1 用 Go、`net/http/httputil.ReverseProxy` 做转发底座」。
本记录**取代其「Go 标准库自研转发底座」部分**（语言仍为 Go），保留 ADR-0008 的 ID 与正文，
仅在其文首标注被取代的部分（`D-6`：不得删除、不得重新编号）。

## 未解决

- **Caddy 升级的耦合面与检查表**：耦合面用 `make caddy-surface` 自动提取；
  其中三类依赖（模块 ID 字符串 · 默认 `Server` 头值 · `caddy.Duration` 单位）**不会造成编译失败**，
  只会静默失效 —— 已用 [`../../edge/proxy/caddy_compat_test.go`](../../edge/proxy/caddy_compat_test.go) 锁住，
  升级流程见 [`../../modules/adapter-proxy.md`](../../modules/adapter-proxy.md) §3.1；
- **是否把 Caddy 绑定收进更小的边界**（把 `handler.go` 拆成「Caddy 模块外壳」与「纯逻辑」两个文件、
  让 `policy.go` 不再出现 Caddy 类型）：属**可选的降耦增强**，当前耦合面已可枚举且有测试兜底。

- **`acme` 模式的证书签发细节**（邮箱 / issuer 选择 / 80-443 入站前提）—— 本轮实现 `manual` 为主路径，
  `acme` 仅提供 `AutomateLoader` 骨架，签发策略与存储留待公网接入轮实测。
- **Caddy 内嵌的二进制体积控制**（`-trimpath` / 剥离可选模块）—— 本轮未做，体积评估留待接入演练。
