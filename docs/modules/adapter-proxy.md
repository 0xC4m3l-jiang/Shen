# 模块：`adapter-proxy`

| 项 | 内容 |
| --- | --- |
| 模块名 | `adapter-proxy` |
| 所属层 | `L1` |
| 实现语言 | `Go`（[ADR-0008](../background/decisions/0008-edge-language-go.md)） |
| 负责人 | —— |
| 状态 | ✅ `edge/proxy/` 已实现（**39 测试**，含 `-race`、内嵌 Caddy 端到端与策略面应用语义：后端表 / 白名单 / 响应改写规则）；⏳ 端到端接管仍需真实接入演练 |
| 底座 | **内嵌 Caddy**（Apache-2.0）承担转发与 TLS 终结 · [ADR-0017](../background/decisions/0017-caddy-l1-base.md) |
| 最后更新 | 2026-09-19 |

> 权威清单见 [`../design/modules.md`](../design/modules.md) §1.1 **第 11 行**。
> 本模块由原第 11 行 `adapter-reverse-proxy` 与第 12 行 `adapter-sidecar` **合并**而来 ——
> 两者的代码完全相同，差别只在部署位置与 upstream 指向（见 §5）。

---

## 1. 职责

### 做什么

**在请求路径上**做一件事：把请求交给核心判定，按结果选择后端。覆盖两种部署形态：

| 形态 | 对应原始形态 | 部署位置 | upstream 指向 |
| --- | --- | --- | --- |
| **前置代理** | ③ 反向代理前置（`INT-3`） | 独立节点 / 独立 Pod | 业务真实地址 |
| **边车** | ④ Sidecar（`INT-4`） | 业务 Pod 内 | `127.0.0.1:<业务端口>` |

严格按 `AR-6` 只做**四件事**：

1. **拦截请求** —— 收下客户端请求，不转发直到拿到决策（或超时降级）；
2. **查本地判定缓存**，未命中才调核心的判定面（`api/judge/v1`）；
3. **按结果执行** —— `route_origin` 透传到业务 · `route_mirage` 引流到指定后端 · `block` 拦截；
4. **异步上报遥测**（`api/telemetry/v1`），不阻塞请求。

**另外消费策略面**（接缝 `S4`）：从核心拉取**改道后端表 · 白名单 · 响应改写规则 · AI 内容开关与清单**（`api/policy/v1` 的 `Pull`），
按「远端覆盖本地、白名单取并集」应用，并把结果回执给核心（`Ack`）。
拉不到就继续用本地 env —— 策略面**不是**请求路径上的依赖（`NI-1`）。契约见 [`../spec/policy-payload.md`](../spec/policy-payload.md) 与 [ADR-0018](../background/decisions/0018-policy-plane-pull-model.md)。

**AI 欺骗内容的注入**（`ADR-0023` / `AR-33`）在改道侧：命中资源（与请求路径**精确相等**）→ 按
`variant = fnv1a(会话键) mod N` 选变体（确定性，`AR-30`）→ 再验校验和 → 引用 `edge-injection` 改写
（插在 `marker` 之前）。会话→槽位有**钉定缓存**（TTL = 判定缓存窗）：**冻结的是槽位选择**，不是内容体 —— 清单换代后槽位由 `fnv1a(会话)` 重算得到同值（不漂移），内容体取自新版本。
开关**取与**：适配器本地 `SHEN_PROXY_INJECT_CONTENT`（默认 `false`）+ 策略载荷 `inject_enabled`。
本模块**不生成内容、不调模型**（`AR-7` / `AR-32`）—— 它只查表、验证、改写。

**转发与 TLS 终结不由本模块实现**：交给**内嵌 Caddy**（[ADR-0017](../background/decisions/0017-caddy-l1-base.md)）。
本模块以 Caddy 中间件 `http.handlers.shen_proxy` 存在，只产出「决定」与「后端地址」；
`reverse_proxy` 是 Caddy 的，本模块不写一字节转发逻辑。

另外承担三件与转发/引流正确性直接相关的职责：

- 白名单（内部 IP / 健康检查 / 监控探针）**必须先于**引流判定生效（`INT-25`）；
- 透传真实来源 IP（`INT-23`），不得因接入导致业务侧丢失客户端 IP；
- **对外可见面卫生**（`OH-2`）：清掉会暴露我们代理栈的响应头 —— `Via` 一律删除；
  `Server` 只在上游给出时保留（上游没给就删掉 Caddy 的默认值）；**错误响应同样不能带指纹**；
- **协议与流式的边界行为**（已实测锁定）：客户端↔本进程协商到 **HTTP/2**；本进程↔上游默认 **HTTP/1.1**（`reverse_proxy` 默认，与 nginx/Envoy 同类；上游协议版本对攻击者不可见）；
  **协议升级（WebSocket 101）与流式（SSE / 分块）透传**；**大响应不缓冲**（超过 1 MiB 不注入、原样透传）；**不读请求体**（8 MiB 上传原样送达上游）。

### 明确不做什么

- **禁止**实现判定逻辑、**禁止**做 LLM 推理、**禁止**维护会话状态、**禁止**写入核心状态（`AR-7` / `MD-9`）；
- **禁止**对**业务侧**响应做任何改写 —— 改写只允许作用于蜜罐侧（`INT-8`）；
- **禁止**持有跨请求业务状态（`structure.md` §4 对 L1 的要求）；
- **禁止**阻塞调用（`MD-11`）—— 所有对核心的调用**必须**带超时；
- **禁止**自研 L0 能力（`AR-3`）：转发与 TLS 终结**复用现成组件** —— 内嵌 Caddy
  （[ADR-0017](../background/decisions/0017-caddy-l1-base.md)），**禁止**自研代理、**禁止**自研 TLS / ACME；
  形态 ③④ 下请求路径上**只有一个七层组件**（本进程），**禁止**叠加第二个（`AR-4`）；
- **禁止**要求客户修改业务应用代码（`INT-7`）。

> 规则 `MD-1`：本模块**不得**跨层拆分。本模块同时覆盖 `INT-3` 与 `INT-4` 两种形态，
> 但**只属于 L1 一层** —— 两种形态是同一个模块的两种部署方式，不是两个模块。

## 2. 输入 / 输出契约

| 方向 | 契约 | 定义位置 |
| --- | --- | --- |
| 输入（HTTP） | 客户端的原始 HTTP 请求；来源 IP 由 L0 透传或 `X-Forwarded-For` | `../design/integration.md` 的 `INT-23` |
| 输入（gRPC） | `DeceptionJudge.Judge` | [`../../api/judge/v1/judge.proto`](../../api/judge/v1/judge.proto) |
| 输出（gRPC） | `DeceptionTelemetry.Report` | [`../../api/telemetry/v1/telemetry.proto`](../../api/telemetry/v1/telemetry.proto) |
| 输出（HTTP） | 转发到 upstream（业务真实地址或引流后端） | —— |

本模块内部的数据结构（**不跨模块共享**，依 `MD-5`）：

| 类型 | 用途 |
| --- | --- |
| `Handler` | **Caddy 模块**（`http.handlers.shen_proxy`）：判定胶水 + 三值路由；无状态 |
| `Config` | 判定胶水的可调项（upstream / 引流后端表 / 白名单 / 超时 / 影子开关）；单测用它构造 `Handler` |
| `decisionCache` | 按 `decision_id` 记忆判定结果，进程内、有 TTL、可丢失 |
| `TLSConfig` · `Options` · `BuildConfig` | 进程级装配：监听地址 + TLS 模式 → Caddy 配置 |
| `PolicyClient` | 策略面客户端（**消费方定义**的最小接口）；单测用替身 |
| `remoteState` | 已应用策略的原子快照（版本 · 校验和 · 改道后端 · 白名单 CIDR）—— 请求路径只做一次原子读 |

> 判定的**取值**（`route_origin` / `route_mirage` / `block`）与 `severity` 的定义
> 在 [`../design/terminology.md`](../design/terminology.md) §4，本模块只做映射、不做判定。

## 3. 依赖

| 允许依赖 | 原因 |
| --- | --- |
| `api/judge/v1` · `api/telemetry/v1` · `api/policy/v1` | `ST-3` 规定的**唯一**调核心方式（判定 / 遥测 / 策略三个面） |
| `github.com/caddyserver/caddy/v2`（Apache-2.0） | **转发与 TLS 终结的底座**（`AR-3`：复用现成组件）；台账见 [`../spec/dependencies.md`](../spec/dependencies.md)。**耦合面与升级检查表见 §3.1** |
| `edge/injection` | L1 处置模块，`ST-5` 要求它**被适配器引用、不独立部署**；`handler.go` 的 `Provision` 用它建注入器。**不是**适配器之间的依赖（`MD-4` 禁止的是 `adapter-*` 互依） |
| Go 标准库 `net/http` / `net` / `time` | 判定胶水、观测构造、超时 |
| `google.golang.org/grpc` | 生成 stub 的运行时（间接依赖，非新增） |

| 禁止依赖 | 原因 |
| --- | --- |
| `core/internal/*` | `ST-3`，**编译期强制**（Go 的 `internal/` 规则） |
| 任何数据库驱动（Redis / ClickHouse / PostgreSQL） | `MD-20` 规定核心唯一的 I/O 出口是 `store`；适配器更不得直连 |
| 任何判定 / 规则引擎库 | `AR-7` 禁止在适配器实现判定 |

### 3.1 与 Caddy 的耦合面与升级检查表

**耦合面（自动提取，别手抄）**：`make caddy-surface`。

| 类别 | 我们的依赖 | 升级时的风险 |
| --- | --- | --- |
| 包 | `caddy/v2` · `/caddyconfig` · `/modules/caddyhttp` · `/modules/caddyhttp/reverseproxy` · `/modules/caddytls` · `/modules/standard` | 路径改名 → 编译失败（**可见**） |
| Caddy 模块契约 | `caddy.Module` / `Provisioner` / `Validator` / `CleanerUpper` / `caddyhttp.MiddlewareHandler` | 接口变化 → 编译失败（**可见**） |
| 配置类型 | `caddy.Config` · `caddyhttp.Server/RouteList/App` · `caddytls.TLS/ConnectionPolicies/FileLoader/AutomateLoader` · `caddyconfig.JSON` | 字段改名 → 编译失败（**可见**） |
| 以字符串引用的模块 ID | `http.handlers.shen_proxy`（我们自己）· `http.handlers.headers`（错误路径删头）· `http.handlers.reverse_proxy` | **不会编译失败** → 启动失败或**静默失效** |
| 默认值假设 | `caddyhttp.ServerHeader == "Caddy"`（`headerSanitizer` 按值判断才删） | **不会编译失败** → 指纹清洗静默失效（违反 `OH-2`） |
| 单位假设 | `caddy.Duration` = 纳秒（我们所有超时/TTL 字段） | **不会编译失败** → 阈值整体错位 |

**升级流程（四条命令，一条都不能省）**：

```sh
make caddy-surface     # ① 先看耦合面：对着 Caddy 的 changelog 逐条核
go get github.com/caddyserver/caddy/v2@<新版本>
make gate              # ② 兼容锁在 edge/proxy/caddy_compat_test.go —— 红了会直接告诉你哪条假设变了
make bench             # ③ AR-29 空载下界不能明显退化
make dev               # ④ 端到端（核心 + 冒烟 + 回放）
```

> **禁止**只跑 `go build` 就宣布升级完成：上表里带「不会编译失败」的三类正是升级最容易静默坏的地方，
> 其中「默认 `Server` 头」直接关系 `OH-2`（对手可见面不得暴露我们的栈）。
> 安全网位置：[`../../edge/proxy/caddy_compat_test.go`](../../edge/proxy/caddy_compat_test.go)（编译期断言 + 四类假设断言）。

> 规则 `MD-4`：依赖方向**必须**单向（适配器 → 核心 → 数据面）。
> 本模块**禁止**被核心反向依赖，**禁止**依赖其他适配器（`edge/mirror` · `edge/dns`）。

## 4. 关键规则

| 规则 | 与本模块的关系 |
| --- | --- |
| `AR-6` | **四件事的清单** —— 超出即为越界 |
| `AR-7` | 四件**禁止**做的事 |
| `AR-3` | L0 禁止自研：转发与 TLS **复用现成组件**（内嵌 Caddy，[ADR-0017](../background/decisions/0017-caddy-l1-base.md)） |
| `AR-4` | 每层只选一个组件：形态 ③④ 下请求路径上只有本进程一个七层组件；客户若保留 L0，它只做入口路由（`INT-22` 已由本进程终结 TLS） |
| `AR-4` | 每层只选一个组件，**禁止**多层七层代理叠加争抢流量控制 |
| `AR-29` | 业务路径 P99 额外延迟 ≤ 5 ms —— 本模块是主要贡献者之一 |
| `MD-9` / `MD-11` | 只做四件事；禁止阻塞调用 |
| `INT-3` / `INT-4` | 本模块覆盖的两种形态 |
| `INT-5` | 四种形态**各自独立工作** —— 本模块不依赖任何其他形态 |
| `INT-7` | 禁止要求客户改业务代码 |
| `INT-8` | 禁止改写业务侧响应 |
| `INT-11` | **首次上线必须影子模式** —— 默认配置必须为「只观测」 |
| `INT-22` | TLS **必须**能读出请求体：由本进程终结 TLS（`SHEN_PROXY_TLS_MODE`）是启用误导处置的前提 |
| `INT-23` | 透传真实来源 IP |
| `INT-25` | 白名单**必须**在引流判定前生效 |
| `OH-1` / `OH-2` | 对外可见面**禁止**留下我们的栈指纹：`Via` 一律清除；`Server` 只在上游给出时保留（见 §1 的可见面卫生） |
| `NI-1` | 总则：引擎完全故障时业务不受影响 |
| `NI-3` / `NI-4` / `NI-5` | 失败放行 · 硬超时 · 不确定状态回落 `route_origin` |
| `NI-10` | 熔断后自动纯放行 |
| `ST-3` | 只能经 `api/` 调核心 |
| `ST-10` | `decision_id` 按（来源标识, 会话, 路径, 时间窗）派生并随请求传入 |
| `ST-11` | 未命中缓存才调核心；适配器侧**必须**实现超时降级 |
| `TB-20` | 本层语言为 Go（`ADR-0008`） |
| `TB-24` | 禁止 CGO / FFI，跨进程只走 wire format |
| `AR-30` | 响应路径**禁止**非确定性：变体由会话哈希决定，会话内冻结（钉定） |
| `AR-33` | 内容**必须**来自过了护栏的 `ai-capability`（本模块只消费，不生成、不调模型） |

## 5. 状态与生命周期

| 状态 | 存在哪里 | 生命周期 | 多副本一致性 |
| --- | --- | --- | --- |
| 判定缓存 | 进程内存，键为 `decision_id` | TTL 由配置定（`INT-24` 要求集中定义） | **可丢失、可不一致** —— 未命中就调核心，语义上等价 |
| 白名单 | 启动时从配置载入 | 进程存活期 | 各副本各自载入，配置相同 |
| 引流后端表（本地） | 启动时从 env 载入 | 进程存活期 | 同上 |
| 引流后端表（远端） | 策略面下发，**整块原子替换** | 进程存活期；拉取失败沿用上一次成功版本 | 各副本可能短暂停在**不同策略版本** —— 回执（`Ack`）就是给核心做这层对账用的（`AR-13`） |
| 白名单（远端） | 策略面下发，与本地**取并集**（护栏只增不减） | 同上 | 同上 |
| 响应改写规则（远端） | 策略面下发；**字段缺省**时沿用本地 env 规则，**显式空数组**时明确无规则 | 进程存活期；失败沿用上一次成功版本 | 各副本可能短暂停在**不同策略版本** —— 回执（`Ack`）即为此对账（`AR-13`） |
| AI 内容清单（远端） | 策略面下发（`content_manifest`），**整块取代** | 同上 | 同上 |
| 会话→变体槽位钉定 | 进程内存，键为会话键（Cookie 原值） | TTL = 判定缓存窗；有容量上限（`MD-10`） | **可丢失** —— 丢了重算得同一槽位（哈希决定），不漂移 |
| TLS 证书 | 进程内由 Caddy 持有：`manual` 从文件装载 · `acme` 自动签发与续期 | 进程存活期（`acme` 由 Caddy 负责续期） | 各副本各自持有，证书内容一致 |
| **禁止写盘的进程侧文件** | ❌ **配置自动保存已关闭**（`Admin.Config.Persist=false`）：Caddy 默认会把整份配置写进 `$XDG_DATA_HOME/caddy/autosave.json`，本进程**不得**这么做 | —— | —— |
| 证书存储目录 | ⚠️ certmagic 仍在 `$XDG_DATA_HOME/caddy/`（默认）写 `instance.uuid` / `last_clean.json` / `locks` | 进程存活期 | 容器需可写目录；只读根文件系统必须把 `XDG_DATA_HOME` 指向可写位置 |
| **会话身份** | ❌ **不持有** | —— | —— |

> `INT-20`：会话身份**禁止**上行、**禁止**回传。本模块只在派生 `decision_id` 时**读**请求里的来源标识，
> 不解析、不存储、不外传会话身份。
>
> `AR-9` 要求核心无状态多副本。本模块更是**无状态**的：进程可随时被杀死重建，
> 唯一丢失的是判定缓存，代价仅是多调一次核心。

## 6. 失败模式与降级

| 失败情形 | 行为 | 是否满足 `NI-1` | 依据 |
| --- | --- | --- | --- |
| 白名单命中 | 直接透传，**不调核心** | ✅ | `INT-25` |
| 核心不可达 | 透传到业务，记一条「判定缺失」事件 | ✅ | `NI-3` |
| 核心响应超时 | 硬超时（预算见 `AR-29`）后透传 | ✅ | `NI-4` / `ST-11` |
| 核心返回非法 / 未识别 `action` | 按 `route_origin` 处理 | ✅ | `NI-5` |
| 请求无法解析（畸形 HTTP） | 透传（不因解析失败而阻断） | ✅ | `NI-5` |
| 熔断打开 | 纯放行，不再调核心 | ✅ | `NI-10` |
| 引流后端不可达 | **回落到业务真实地址**，并记异常事件 | ✅ | `NI-1` —— 引流失败绝不能变成业务失败 |
| upstream 也不可达 | 让错误如实返回给客户端（**不得**用蜜罐内容兜底业务失败） | ✅ | `NI-1` / `INT-8` |
| 资源耗尽（CPU / 内存） | 受 cgroup 上限约束；压满时退化为纯转发 | ✅ | `NI-7` |
| TLS 配置非法（`manual` 缺证书 / `acme` 缺域名 / 未知取值） | **启动失败**并指出缺失字段 | ✅ | [`ADR-0017`](../background/decisions/0017-caddy-l1-base.md) · `TLSConfig.Validate` |
| TLS 握手失败（证书过期 / 客户端不信任） | 如实返回握手失败 —— **禁止**降级成明文（静默削弱加密比失败更槽） | ✅ | `OH-1` |
| 上游不可达（**错误路径**） | 返回 502，且响应**不带** `Server` / `Via` 指纹 —— 错误响应由 Caddy 服务器层处理，清洗点在 `BuildConfig` 的 `errors` 路由 | ✅ | `OH-2` |
| 策略面不可达 / 超时 | 沿用当前策略（拉不到就用本地 env），**只记日志**；同样的失败只报一次 | ✅ | [`ADR-0018`](../background/decisions/0018-policy-plane-pull-model.md) · `NI-1` |
| 策略载荷校验和不匹配 / `schema_version` 读不懂 / JSON 非法 | **拒绝应用**并回执 `applied=false` + 原因；继续用当前策略 | ✅ | `ST-8` · `spec/policy-payload.md` |
| 载荷里个别后端地址非法 | 只丢那一条并记账（整表作废会让所有改道一起失效） | ✅ | 同上 |
| 注入规则非法（片段为空等） | **整份策略不应用**（宁可继续用旧规则，不可半应用）；回执 `applied=false` + 原因 | ✅ | `spec/policy-payload.md` |
| 内容清单结构不可索引（selector 不认识 / variants < 1 / 条目全坏） | 当作「没有内容」：上报 `inject=no_content`，**不报错、不阻断** | ✅ | `spec/ai-contract.md` §3 |
| 内容校验和不符 / 资源未命中 / 变体缺失 / 找不到 `marker` / 非 HTML | 原样返回响应，上报 `inject=no_content` | ✅ | `ADR-0023` · `NI-1` |
| 内容注入开关关（本地或下发级任一） | 完全不动响应体，上报 `inject=disabled` | ✅ | `ADR-0023` 决定 4 |
| 输出不合格（LLM） | 不适用 —— 本模块不做 LLM（生成侧在 `ai-capability`，过了护栏才下发） | —— | `AR-33` |

> ⚠️ **「引流后端不可达 → 回落业务」是本模块最关键的一条降级。**
> 它同时满足 `NI-1`（业务不受影响）与 `INT-8`（不改写业务侧响应）：
> 我们既不能因为蜜罐挂了而阻断业务，也不能拿蜜罐内容去顶替业务响应。

## 7. 测试

| 类型 | 覆盖什么 | 位置 |
| --- | --- | --- |
| 单元 | `decision_id` 派生（同输入同 ID、跨时间窗不同 ID）；`Action` → upstream 映射；白名单**先于**判定；配置校验 | `edge/proxy/*_test.go` |
| 单元（替身） | 核心返回三种 `action` + 非法 `action` 时的路由行为（`MD-22`：**禁止**依赖核心真实实例） | 同上 |
| 单元（配置） | `TLSConfig.Validate` 三取值 + 非法值；`BuildConfig` 拒绝空监听 / 非法模式；`upstreamAddr` 拒绝非法地址 | `edge/proxy/embed_test.go` |
| 集成（内嵌 Caddy） | **真 Caddy + 真 `reverse_proxy` + 真 gRPC**：TLS 握手 · 三值路由 · 引流侧注入 · 引流失败回落 · block 短路 —— 即换底座的 go/no-go | `edge/proxy/embed_test.go`（`TestEmbeddedCaddyEndToEnd`） |
| 集成 | 与真实核心的 gRPC 往返；`decision_id` 原样回传（`ST-10`） | 待接入演练 |
| 故障注入（**`NI-1` 的 `V-1…V-4`**） | `V-1` 杀死核心 · `V-2` 决策延迟超预算 · `V-3` malformed protobuf（裸 TCP 回垃圾）· `V-4` 非法决策值（UNSPECIFIED 与越界 99）—— 每条都连打 20 次，**要求 100% 正常** | `edge/proxy/failopen_test.go`（真 Caddy + 真 gRPC + 真业务后端） |
| 故障注入（`V-5`） | CPU 饱和下业务 P99 不劣化 —— 需基线 P99 与真实负载 | 待补：**属接入演练**（实验 `E3`），见 [`../background/notes/pending-experiments.md`](../background/notes/pending-experiments.md) |
| 基准（`AR-29` 空载下界） | 直连 vs 经引擎的额外延迟；本机实测 ≈ **+48 µs**（85.7 − 37.7），约为 5 ms 预算的 1% | `edge/proxy/latency_test.go`（`make bench`） |
| 属性测试 | 对任意请求，**未识别状态一律 `route_origin`** | 待补 |
| 分支穷尽性 | `Action` 三个取值 + 非法值，四个分支全覆盖 | `MD-8` |
| 单元（策略应用） | 远端后端按名覆盖 + 本地保留 · `enabled=false` 不入表 · 坏地址只丢一条 · schema 读不懂整份拒绝 · 白名单并集 | `edge/proxy/policy_test.go` |
| 单元（策略失败路径） | 拉取失败沿用当前策略且不产生回执 · 校验和不匹配拒绝应用并回执 `applied=false` · 成功应用回执 `applied=true` 且同版本不重复回执 | 同上 |
| 单元（响应改写规则） | 字段缺省 → 保留本地规则 · 显式空数组 → 关掉注入 · 非空 → 远端规则接管且本地规则不再注入 · 远端规则真的改写改道侧响应 | 同上（`TestApplyEdgePolicyInjectSemantics` · `TestRemoteInjectRuleRewritesDivertedResponse`） |
| 单元（可见面卫生） | `Via` 一律删除 · `Server` 为 Caddy 默认值时删除、为上游值（含大小写/空白差异）时保留 · 业务响应头与状态码**不得**被改写 | `edge/proxy/proxy_test.go`（`TestHeaderSanitizer*`） |
| 集成（错误路径指纹） | 上游不可达 → 502 且无 `Server` / `Via` | `edge/proxy/embed_test.go`（`TestNoProxyFingerprintOnErrorPath`） |
| 集成（转发边界） | **协议升级**（101 后仍可双向收发）· **流式不被全量缓冲**（首块到达时间）· **大响应（2 MiB）不注入不截断** · **大上传（8 MiB）完整送达** · **观测不含 body** · **h2 下行 / h1.1 上行** | `edge/proxy/forwarding_test.go` |

> `MD-22`：本模块的测试**必须独立可运行**，用替身实现 `JudgeClient` / `TelemetryClient`，
> **禁止**依赖真实核心或真实存储。

## 8. 未决项

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | ✅ **已结案（2026-09-19，后被 [ADR-0019](../background/decisions/0019-tls-termination-belongs-to-l0.md) 收窄）** —— **默认交客户 L0 终结 TLS**（`SHEN_PROXY_TLS_MODE=off`）；自终结（`manual`/`acme`）保留为「客户没有 L0」时的备选，**启用即在启动日志警告**（`E2` 实测：本栈与公有站栈的 ServerHello 扩展顺序可区分） | —— | [`ADR-0019`](../background/decisions/0019-tls-termination-belongs-to-l0.md) · §3.1 |
| 2 | ✅ **已结案（2026-09-19）**：改道后端表**两者都要** —— 本地 env 是兜底，策略面下发是正式通路（远端覆盖本地，[ADR-0018](../background/decisions/0018-policy-plane-pull-model.md)）。原问：静态配置还是核心下发 | —— | —— |
| 10 | **诱饵资产不经策略面下发**（核心侧尚无来源与归属）—— ⚠️ **AI 欺骗内容已单独接通**（阶段 A：`ai-capability` → `content_manifest` → 本模块注入，`ADR-0023`）；本项只剩诱饵资产一条 | 诱饵面内容到不了响应 | [ADR-0018](../background/decisions/0018-policy-plane-pull-model.md)「未解决」 |
| 11 | **同一 `(IP, 会话, 路径, 60s)` 内共享一个决策**（`decision_id` 不含 UA）—— 实测可复现「探针先到 → 真实用户被改道」 | 误调度率（`guard.false_route_budget`）；改它要动 `ST-10` | [`../kb/known-issues.md`](../kb/known-issues.md) `K-20` |
| 12 | **`ST-17`（存活/就绪探针）未实现** —— 本进程不暴露任何探针端点，因此 `config/sidecar.example.yaml` 里的 `livenessProbe` / `readinessProbe` 已**删掉**（模板不得声称代码没有的东西） | 交付形态缺少探针语义；k8s 部署只能用 TCP 探活 | 实现时要先定「探针端点是否开在对外监听上」（`OH-2`：它会被任何客户端请求到）—— 属接入物料轮 |
| 3 | **真实引流依赖 `director`** —— 核心当前是 `control.ShadowDecider`，恒返回 `route_origin`；本模块无法自己造出 `route_mirage` | 端到端引流验证 | 见 [`../modules/control.md`](control.md) 与 §1.1 第 2 行 |
| 4 | **判定缓存的键与 TTL 取值** —— 属实测问题（同 `severity` 档位） | 命中率与一致性 | [`../design/terminology.md`](../design/terminology.md) §4.2 |
| 5 | **`AR-29` 的 P99 是否达标** —— 本模块是主要贡献者 | 能否按形态 ③④ 上线 | 接入演练实测（`E1` 已有空载下界：P99 116 µs） |
| 6 | **换底座后的 `AR-29` 未重测** —— 内嵌 Caddy 后转发层换实现，旧空载下界不再直接适用 | 同第 5 项 | 接入演练实测（`E`-series）· [`../background/notes/pending-experiments.md`](../background/notes/pending-experiments.md) |
| 7 | **`tls_mode=acme` 未实测** —— 只做了装配与配置校验，未在真实公网域名上验证签发与续期 | 形态 ③④ 的公网部署 | 接入演练 |
| 8 | **Caddy admin API 已禁用**（缩攻击面），且**配置自动保存也已关闭**（不写 `autosave.json`，见 §5）⇒ 本进程**没有**运行时改配置的入口；后端表若要热更新需另设机制 | 策略下发面（`S4`）落到 L1 侧时 | 阶段 2b 之后 |
| 9 | **certmagic 的存储目录仍写在 `$XDG_DATA_HOME/caddy/`** —— 已实测会产生写入（`instance.uuid` / `last_clean.json` / `locks`）；只读根文件系统的容器必须把该变量指向可写路径（或挂载目录） | 边车/最小镜像部署（`INT-9`） | 接入物料（部署清单里写清） |

## 9. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-17 | 创建。由原 `adapter-reverse-proxy`（第 11 行）与 `adapter-sidecar`（第 12 行）合并；语言 Lua → Go | [ADR-0008](../background/decisions/0008-edge-language-go.md) · 用户确认 |
| 2026-09-18 | **接诱饵注入**：`Injector` 接口（由本模块定义，避免适配器互依 MD-4）+ `SHEN_PROXY_INJECT`；只改引流侧 HTML（INT-8），大响应不缓冲 | 本轮开发 |
| 2026-09-19 | **换底座**：转发与 TLS 终结改为内嵌 **Caddy**（`http.handlers.shen_proxy`）；模块身份与对外接口不变（`JudgeClient` / `TelemetryClient` / `Injector`）；新增 `SHEN_PROXY_TLS_MODE=off\|manual\|acme`；未决项 1（TLS 归属）结案；测试 19 → **30**；新增依赖已进许可台账 | [`ADR-0017`](../background/decisions/0017-caddy-l1-base.md) · 用户确认 |
| 2026-09-19 | **TLS 终结默认改交客户 L0**（`E2` 实测：本栈与公有站栈的 ServerHello 扩展顺序可区分）；自终结保留 + **启动告警**（`SelfTerminationWarning`，含测试）；未决项 1 表述按 `ADR-0019` 收窄 | [`ADR-0019`](../background/decisions/0019-tls-termination-belongs-to-l0.md) · 用户确认 |
| 2026-09-19 | **接策略面**（`S4`）：`Pull` 拉取改道后端表与白名单（远端覆盖本地 · 白名单并集）· `Ack` 回执（`AR-13`）· 新增 `SHEN_PROXY_POLICY_INTERVAL` / `SHEN_PROXY_POLICY_ID` / `SHEN_PROXY_ADAPTER_ID`；测试 **30 → 37** | [`../plans/2026-09-19-policy-plane.md`](../plans/ARCHIVE.md) · [ADR-0018](../background/decisions/0018-policy-plane-pull-model.md) · 用户确认（9 项推荐） |
| 2026-09-19 | **策略面下发响应改写规则**：载荷新增可选 `inject_rules`（缺省 = 用本地 env；显式空数组 = 关掉注入）；注入器改为**按请求读当前规则**（支持远端热变更）；测试 **37 → 39** | [`../plans/2026-09-19-content-path-injects.md`](../plans/ARCHIVE.md) · 用户确认（9 项推荐） |
| 2026-09-19 | **可见面卫生（`OH-2` 一致性修复）**：实测发现转发会把 `Via: 1.1 Caddy` 透给对手、错误响应带 `Server: Caddy` —— 新增 `headerSanitizer`（中间件层）+ `errors` 路由（错误路径），并加 5 例单测与 1 例端到端 | [`../plans/2026-09-19-forwarding-deception-hardening.md`](../plans/ARCHIVE.md) |
| 2026-09-19 | **转发边界实测锁定**：协议升级（WebSocket）· 流式不被缓冲 · 2 MiB 响应不注入不截断 · 8 MiB 上传完整送达 · 观测不含 body · h2 下行 + h1.1 上行；新增 6 例集成测试 | [`../plans/2026-09-19-forwarding-boundary-verification.md`](../plans/ARCHIVE.md) |
| 2026-09-19 | **`NI-12` 的 `V-1…V-4` 自动化**（真 Caddy + 真 gRPC/裸 TCP 故障注入，每条连打 20 次要求 100% 正常）· 新增 `AR-29` 空载下界基准与 `make bench`；`V-5` 归入接入演练 | [`../plans/2026-09-19-ni12-vseries-and-ar29-floor.md`](../plans/ARCHIVE.md) |
| 2026-09-20 | **接 AI 欺骗内容注入**（`ADR-0023` / `AR-33`）：消费策略载荷的 `inject_enabled` + `content_manifest`；按（资源精确匹配 + 会话哈希）**确定性命中**变体、**会话钉定**（轮换只对新会话生效）、注入前**再验校验和**；新增 `SHEN_PROXY_INJECT_CONTENT`（默认 `false`，与下发级开关**取与**）；逐请求事件新增 `inject` / `content_id`（`docs/spec/events.md` §2.2 + 夹具同步）；顶层测试函数 58 → **75**（`grep -c '^func Test'`） | [`../plans/2026-09-20-ai-capability-guardrail.md`](../plans/2026-09-20-ai-capability-guardrail.md) · [ADR-0023](../background/decisions/0023-deception-content-injection.md) · 用户确认 |

## 9.1 执行落点与响应观测（供流量调度图）

每次请求结束都会异步上报一条 `request_judged`（见 [`../spec/events.md`](../spec/events.md) §2.2），其中：

- `executed`：**实际落点**，由纯函数 `executedFor(shadow, action, mirageFound, mirageFellBack)` 决定 —— 穷举测试在 `edge/proxy/wire_test.go`；
- `inject` / `content_id`：**注入结果**与内容标识（`applied` / `disabled` / `no_content` / `off`）——
  四个取值各由一例单测锁定（`edge/proxy/content_test.go`）；只有 `applied` + 非空 `content_id` 才在图上多一跳「内容注入」（`ADR-0023`）；
- 事件信封的 `event_id` 是**逐请求唯一**（`judged:<decision_id>:<序>`），`decision_id` 放在**载荷里**：
  判定缓存命中的多条请求共享同一 `decision_id`，若用它当事件 id 会被遥测幂等键（`AR-11`）折叠成一条 —— 图上就看不到"每条流量"了（实测踩过）；
- `status` / `bytes` / `duration_ms`：由 `headerSanitizer` 顺带观测（它本就包住整个请求的 `ResponseWriter`，不再加一层包装）；
- 影子模式下 `action` 可能显示 `route_mirage`（意图）而 `executed=origin`（实际）—— 这正是页面上要区分两者、避免误判"已经改道了"的原因（`INT-11`）。
