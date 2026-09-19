# 变更包 · 2026-09-18 · 全仓代码审查与加固

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 整体审查：模块逻辑合理性 · 开源替换评估 · 架构稳定性；并修复审查发现的缺陷 |
| 日期 | 2026-09-18 |
| 状态 | 已实现 |
| 涉及模块 | `edge/proxy`（4 处缺陷）· `edge/mirror`（1 处）· `core/internal/policy`（1 处）· `core/internal/director`（补 `INT-25`）· `core/internal/store`（内存清理） |
| 决策数 | 已答 0 项（按既有设计加固）/ 待定 6 项（见 §5） |
| 关联 | [`../log.md`](../log.md) 同日条目 · [`2026-09-18-deception-modules-impl.md`](2026-09-18-deception-modules-impl.md) |

## 1. 审查范围与方法

- **范围**：`core/` + `edge/` 全部非测试代码（6834 行）+ 测试（3646 行）+ `scripts/` 门禁工具
- **方法**：① 静态分析器（`go vet` / `staticcheck` / `errcheck`）② 逐模块读代码核对设计承诺 ③ 针对**并发、生命周期、超时、资源上限、错误路径**定向审查 ④ 开源替换评估
- **基线**：`docs/design/`（153 条规则）+ 各模块文档的 §1 职责 / §4 规则表

> 静态分析器初始**零告警** —— 本轮所有发现都来自人工核对与定向审查（这正是机器检查覆盖不到的部分）。

## 2. 发现与处置

### 2.1 已修复（8 处，全部附回归测试）

| # | 发现 | 类型 | 证据 | 处置 |
| --- | --- | --- | --- | --- |
| 1 | **引流侧上游无任何超时** → 蜜罐挂死会把客户端一起拖住（违反 `NI-1`：引擎不得影响业务可用性） | 🔴 稳定性 | `buildProxy` 用默认 Transport（无 `ResponseHeaderTimeout`） | 引流侧加 `ResponseHeaderTimeout`（默认 10s，可配）+ 两侧加连接层超时（dial/TLS/idle）；超时走 ErrorHandler 回落业务。测试：`TestMirageTimeoutFallsBackToOrigin` |
| 2 | **压缩响应被注入** → 往 gzip 字节流塞明文 = 损坏响应（客户端直接报错） | 🔴 正确性 | `injectResponse` 未检查 `Content-Encoding` | 加编码检查：非 `identity` 一律不注入。测试：`TestCompressedResponseIsNotInjected` · `TestIdentityEncodingStillInjected` |
| 3 | **`trackingWriter` 未实现 `http.Hijacker`** → 协议升级（WebSocket / 101）经引流路径一律 502 | 🔴 功能性 | `httputil.ReverseProxy` 在 101 时强制断言 `Hijacker` | 透传 `Hijack()`。测试：`TestTrackingWriterIsHijackable` · `TestProtocolUpgradePassesThroughMirage`（裸 TCP 验证 101） |
| 4 | **注入读失败返回 `nil`** → 已部分读出的字节被当作完整响应发出 = 截断响应 | 🟠 正确性 | `injectResponse` 的 `if err != nil { return nil }` | 改为返回错误 → `ModifyResponse` 失败路径 → 回落业务（截断比回落更糟） |
| 5 | **`INT-25` 核心侧白名单未消费** —— 规则要求「白名单必须先于引流判定」，但 `director` 从不读配置里的 `whitelist` | 🟠 设计缺口 | `grep whitelist` 在 core 非测试代码中零命中 | `director.Config.Whitelist` + 前置短路（命中即 `route_origin`，**连判定都不做**）；装配层接线。测试 5 例 |
| 6 | **`policy` 报错指向错误字段** —— 配错 `guard.false_route_budget` 时却报「缺少必填项 thresholds」 | 🟡 可运维性 | `ratio()` 硬编码 `missing("thresholds")` | `ratio` 改为带字段路径。测试：`guard 缺键须报对字段` |
| 7 | **内存存储无界增长** —— TTL 条目只在**被读到**时删除；「只写不读」的键（一次性会话、已解除隔离）会一直堆积 | 🟠 稳定性 | `store/memory.go` 无任何 `delete` 逻辑 | 写入超限（10 万）时清理已过期项（**不改变语义**：过期项本就等于不存在） |
| 8 | **mirror 调核心无超时** —— 核心挂死会一直占住镜像源的连接 | 🟡 稳定性 | `ctx := req.Context()`，无 deadline | 加 3s 上限（镜像不在业务路径，无需 3ms 级）；超时照常返回 202 并记「判定缺失」 |

### 2.2 已确认无问题（清白的部分）

| 项 | 结论 | 证据 |
| --- | --- | --- |
| 静态分析 | vet / staticcheck / errcheck **零告警** | `make gate` |
| 数据竞争 | 全仓 `-race` 通过 | `make test` |
| 依赖方向 | 5 个新模块仅 import `contract`；无模块 import 他模块具体类型 | `go list -f '{{.Imports}}'` |
| 错误处理 | 无 `TODO`/`FIXME`；无被忽略的错误（errcheck 覆盖） | grep + errcheck |
| goroutine | 仅 3 处启动（reporter / gRPC serve / graceful stop），均有终止路径 | grep + 读代码 |
| `context.Background()` | 仅出现在进程生命周期与长生命周期组件根 context（已注释理由） | grep |
| telemetry 职责 | 与 `telemetry.md` §1 一致（**不做持久缓冲**是设计选择，不是遗漏） | 文档 vs 代码核对 |

## 3. 开源替换评估（是否需要换开源项目）

> 结论：**该复用的都已经复用**；自研的恰好是设计指定的「智能决策层」。
> 依据 `architecture.md` §1「复用三原则」① 基础设施层全部复用开源 ② 智能决策层全部自研。

| # | 组件 | 当前实现 | 开源候选 | 评估 |
| --- | --- | --- | --- | --- |
| 1 | L0 接入（代理 / 网关 / TLS） | **复用现成组件** | Envoy / Nginx / HAProxy | ✅ 已符合（`AR-3` 禁止自研） |
| 2 | L1 反向代理 / 边车 | 自研 Go（标准库 `httputil.ReverseProxy` + 约 340 行装配） | Envoy `ext_proc` / Pingora / Caddy | ✅ **保留**。[ADR-0008](../background/decisions/0008-edge-language-go.md) 已评估并否决；转发内核是标准库（久经考验），自研的只是「拦截→查缓存→调核心→选择后端」这段业务胶水 |
| 3 | 蜜罐（协议仿真 / 高交互） | **未实现（预留）** | Beelzebub · Oubliette · honey-ai · RabbitHole | ✅ 按 [ADR-0011](../background/decisions/0011-honeypot-entry-external-backends.md) **接第三方** |
| 4 | L3 微隔离 / eBPF | **未实现** | Cilium / Tetragon | ✅ 设计已决定复用 |
| 5 | L4 LLM 推理 | **未实现** | vLLM / Transformers / PyTorch | ✅ 设计已决定复用（`TB-2`） |
| 6 | 熔断器 | 自研（93 行，固定窗口 + 冷却半开） | `sony/gobreaker`（MIT） | 🟡 **可换但不必要**：自研更小、有 5 例单测、语义（半开冷却）已按 `NI-10` 定制。若团队更信任成熟库，替换成本仅一个文件 |
| 7 | 配置校验 | 自研（`KnownFields` + 显式字段校验） | JSON Schema 库 / `go-playground/validator` | ✅ **保留**：零依赖，且能给出字段级中文报错（本轮刚修好其报错准确性） |
| 8 | YAML 解析 | `gopkg.in/yaml.v3` | — | ✅ 已用成熟库 |
| 9 | 存储客户端（Redis / CH / PG） | 接口就位 + 内存占位 | `go-redis` · `clickhouse-go` · `pgx` | ⬜ **接真实存储时应引入**（许可均宽松，`TB-16` 可过） |
| 10 | 会话滑窗 / 速率计数 | **未实现**（ADR-0012 设计已定） | — | ⬜ 见 §5 #1 |
| 11 | 控制台 | **未实现** | 现成 admin 模板 | ⬜ 需 `npm install` —— 项目规则禁止未授权安装 |
| 12 | 差异哨兵（`sentinel`，非模块） | **未实现** | Honeyval（arXiv 2605.29963） | 🟡 设计称其为「最小可信路径第 0 步」，建议优先，可改造 Honeyval |

**一句话**：没有任何一处「本该用开源却自研了」的地方；唯一可讨论的是熔断器（换与不换都合理）。

## 4. 架构稳定性评估

### 4.1 强项（有证据）

| 强项 | 证据 |
| --- | --- |
| **依赖单向 + 接口由消费方定义** | `go list` 显示新模块仅依赖 `contract`；`control` 定义 `IsolationChecker`、`proxy` 定义 `Injector`，不 import 对方 |
| **降级路径完备** | `NI-1/3/4/5/10` 均有实现 + 断言（kill 核心 / 超时 / 非法值 / 后端不可达 / 存储故障 → 全部回落放行） |
| **无状态 + 状态外置** | 所有核心模块无请求级状态；`AR-9` 由结构保证（`store` 是唯一 I/O 出口，`MD-20`） |
| **失败可观测** | 适配器 `DroppedEvents`、熔断 `Opened()`、装配期策略/诱饵/后端池摘要日志 |
| **测试密度** | 3646 行测试 / 6834 行代码 ≈ **53%**；含端到端 5 例 + 适配器 24 例 |
| **启动期自检** | `AR-30` 一致性自检（不成立即启动失败）；配置非法即启动失败（禁止静默降级） |

### 4.2 风险点（按严重度）

| # | 风险 | 影响 | 现状 |
| --- | --- | --- | --- |
| 1 | **内存存储是占位实现** | 重启丢数据；`EventMemory` / `DecisionMemory` 无 TTL，仍会无界增长 | 设计明确「真实存储接入时换实现」；接口已就位 |
| 2 | **`ADR-0012` 会话级判别未实现** | 判别只看首跳单事件；综述结论是「单事件判别必然失效」；慢热型 Agent 无手段（且 `severity` 恒 `none`） | 设计已定（`SessionFeatures`），代码未落地 |
| 3 | **`responder` 无对外服务面** | 生成能力就绪，但边缘拿不到（适配器不能 import 核心内部包，`ST-3`） | 需新契约或由核心侧提供 HTTP 面 |
| 4 | **长连接与会话粘性**（综述空白 E） | WebSocket/HTTP2 多路复用下粘性未验证（本轮已修升级透传，但粘性本身未验证） | 已知未决（`G2`） |
| 5 | **`E2`（TLS 指纹一致性）未实测** | 设计自述「可能推翻整个欺骗命题」 | P0 未决，非本轮范围 |
| 6 | **无 LICENSE 文件** | 许可审计持续报「未声明」 | 产品决策，交付前必须定 |

## 5. 遗留建议（未在轮内实施，需决策或更大工作）

| # | 建议 | 理由 | 优先级 |
| --- | --- | --- | --- |
| 1 | 实现 `ADR-0012` 会话级判别（`contract.SessionFeatures` + `session` 状态抽象 + `judge` 消费） | 判别能力缺一半；综述与参考实现一致指向「必须会话级有状态」 | **P0** |
| 2 | 接真实存储（Redis / ClickHouse / PostgreSQL） | 内存实现是占位；多副本一致性、持久化都依赖它 | **P0** |
| 3 | 给 `responder` 一个对外服务面（新 `api/` 契约，或核心侧 HTTP 面） | 否则生成能力到不了边缘 | P1 |
| 4 | 实现 `telemetry` 的「接收缓冲 + 落库补偿」 | `telemetry.md` 把可靠重放推给核心侧，但该缓冲不存在 | P1 |
| 5 | `TrustXFF=false` 时**剥离**入站 `X-Forwarded-For` | 当前会连同伪造值一起透传给业务，可能污染业务侧日志（`INT-23` 只要求「不丢失真实 IP」） | P1 |
| 6 | 实现差异哨兵 `sentinel` | 设计称其为「最小可信路径第 0 步」—— 唯一能客观回答「欺骗成不成立」的装置 | P1 |
| 7 | 评估熔断器是否换 `sony/gobreaker` | 非必要；若团队偏好成熟库 | P2 |

## 6. 验证证据

```console
$ go vet ./... && staticcheck ./... && errcheck ./...     # 零告警
$ make gate
门禁通过。  （fmt · vet · staticcheck · errcheck · archcheck · trace · licensecheck · go test -race）

$ go test -count=1 ./...
15 个包 ok

$ make dev
✅ 通过：配置校验 · 非法配置被拒 · 启动 · 判定面 · 冒烟 · 规则回放
```

**新增回归测试 11 例**：proxy 5 例（超时回落 / gzip 不注入 / identity 仍注入 / Hijacker / 101 升级）· director 5 例（白名单 UA/CIDR/路径前缀 / 非白名单仍判定 / 空项不匹配一切）· policy 2 例（报错字段 / 白名单副本）。

## 7. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 全仓审查 + 修复 8 处缺陷 + 开源替换与稳定性评估 | 用户「整体 review」要求 |
