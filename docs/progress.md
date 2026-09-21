# 模块规划进度

> **本文件 = 模块实现进度的唯一维护处**，与 [`log.md`](log.md)（变更日志）同在 `docs/` 下，供**人工审计**对照。
> 权威模块清单在 [`design/modules.md`](design/modules.md) §1.1（**24 个有效模块**，共 25 行；第 12 行已合并）；代码结构实况（包级依赖 / 进程边界 / 接缝）见 [`design/structure.md`](design/structure.md) §1.6。
> **每轮开发落地后必须同步更新本文件**（状态列 / 阶段表），并在 [`log.md`](log.md) 追加一条 —— 两处对齐，缺一不算完成。

| 项 | 值 |
| --- | --- |
| 权威清单 | [`design/modules.md`](design/modules.md) §1.1 |
| 代码结构实况 | [`design/structure.md`](design/structure.md) **§1.6** |
| 文档规则 | `MD-2`（一模块一文件）· `MD-17`（实现前必须创建）· `MD-18`（新增模块须先改清单） |
| 模块文档模板 | [`modules/_template.md`](modules/_template.md) |
| 阶段图例 | `1` = MVP（只观察）· `2a` = 接管与引流打通 · `2b` = 处置内容 · `3` = 高交互与智能 |
| 起环境 | `make up`（Docker 全套：核心 + 代理 + 控制台 + L4 + 演示业务站）· `make dev`（本地一键验证） |

> **本清单共 25 行**（第 12 行已合并 ⇒ **24 个有效模块**）。
> 阶段 2b 起新增 `decoy`（诱饵面，[ADR-0010](background/decisions/0010-functional-camouflage.md)）与
> `honeypot`（蜜罐入口与后端池，[ADR-0011](background/decisions/0011-honeypot-entry-external-backends.md)）。
>
> **本表不写易漂的数字**（文件数 / 测试函数数 / 行数）—— 那些每轮都变，写死必漂
> （2026-09-21 审视时，本表 §1 已有 **5 行**发过时）。要准确值自己取：`go test ./<目录>/ -v` · `wc -l` · `make pytest`。
> 跨模块的运行链断点（谁真的在调用链上）见 [`modules/README.md`](modules/README.md) §0.4。

---

## 1. 全部模块

| # | 模块 | 介绍（做什么） | 层 | 语言 | 源码目录 | 文档 | 代码 | 阶段 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | `judge` | **判定**：观测 → 风险分 + 信号 + 证据；含 Agent 指纹（置信度/证据链）与消费会话特征 | 核心 | Go | `common/core/internal/judge/` | ✅ [`modules/judge.md`](modules/judge.md) | ✅ 已实现 | 1 |
| 2 | `director` | **决策**：风险分 → 三值（放行/改道/拦截）+ 后端名，按灰度确定性收敛 | 核心 | Go | `common/core/internal/director/` | ✅ [`modules/director.md`](modules/director.md) | ✅ 已实现 | 2a |
| 3 | `responder` | **响应生成**：产出「像真实业务」的响应（快模板 + 慢预生成）；同会话同资源同答案（`AR-30`） | 核心 | Go | `common/core/internal/responder/` | ✅ [`modules/responder.md`](modules/responder.md) | ✅ 已实现（**装配 + 启动自检**；未接请求链路） | 2b |
| 4 | `session` | **会话**：三级优先级提取身份 + 会话状态（速度/挑战逃逸）+ 归因令牌（蜜标/水印） | 核心 | Go | `common/core/internal/session/` | ✅ [`modules/session.md`](modules/session.md) | ✅ 已实现 | 1 |
| 5 | `isolation` | **隔离**：隔离记录 / TTL 过期 / 查询短路（命中则不调核心，客户端**不可见**） | 核心 | Go | `common/core/internal/isolation/` | ✅ [`modules/isolation.md`](modules/isolation.md) | ✅ 已实现（已接线：`control.WithIsolation`） | 2b |
| 6 | `policy` | **策略**：规则与阈值的版本化装载 + 灰度比例 + **下发与回执**（`common/api/policy/v1` 的 `Pull` / `Ack`）+ 边缘投影（后端表 / 白名单 / **响应改写规则**） | 核心 | Go | `common/core/internal/policy/` | ✅ [`modules/policy.md`](modules/policy.md) | ✅ 已实现（`Watch` 未实现，见 [ADR-0018](background/decisions/0018-policy-plane-pull-model.md)） | 2a |
| 7 | `telemetry` | **遥测**：事件归一化 + 批量异步上报 + 失败缓冲重放 | 核心 | Go | `common/core/internal/telemetry/` | ✅ [`modules/telemetry.md`](modules/telemetry.md) | ✅ 已实现 | 1 |
| 8 | `store` | **存储**：Redis / ClickHouse / PostgreSQL 访问层 —— **核心唯一的 I/O 出口** | 核心 | Go | `common/core/internal/store/` | ✅ [`modules/store.md`](modules/store.md) | ✅ 接口 + 内存实现（真实后端待接，`NI-13`） | 1 |
| 9 | `edge-injection` | **投毒改写 / 假路径 / 蜜饵注入**（被适配器引用，**不独立部署** `ST-5`） | L1 | Go | `modules/deception/injection/` | ✅ [`modules/edge-injection.md`](modules/edge-injection.md) | ✅ 已实现 | 2b |
| 10 | `adapter-mirror` | **① 旁路镜像**接收端：只读采集，**不处置**（不在请求路径上） | L1 | 配置 + Go | `modules/deception/mirror/` | ✅ [`modules/adapter-mirror.md`](modules/adapter-mirror.md) | ✅ 接收端 + 单测；`config/` 三份模板 | 1 |
| 11 | **`adapter-proxy`**（③前置 + ④边车） | **同一实现**：拦截 → 调核心 → 按决策选 upstream → 异步上报；转发与 TLS 终结用内嵌 Caddy（[ADR-0017](background/decisions/0017-caddy-l1-base.md)）；改道后端表与白名单可经**策略面**下发（[ADR-0018](background/decisions/0018-policy-plane-pull-model.md)） | L1 | Go | `modules/deception/proxy/` | ✅ [`modules/adapter-proxy.md`](modules/adapter-proxy.md) | ✅ 已实现（内嵌 Caddy 端到端 + 策略面消费 + `-race`） | 2a |
| ~~12~~ | ~~`adapter-sidecar`~~ | ❌ 已合并入第 11 行（同一份代码的另一种部署形态） | —— | —— | ❌ 已合并入第 11 行 | —— | —— | —— |
| 13 | `adapter-dns` | **② DNS 引流**：按来源解析到引擎或真实服务（**纯配置，无源码**） | L1 | 配置 | `modules/deception/dns/` | ✅ [`modules/adapter-dns.md`](modules/adapter-dns.md) | ✅ **配置已就绪**（Corefile + 生效校验/回退说明，验证靠两条 `dig`） | 2a |
| 14 | `honeypot-protocol` | 协议仿真（SSH / MySQL / Redis / FTP…）+ 交互捕获；**可选自研** | L2 | Go / Rust | `modules/honeypot/protocol/` | ✅ [`modules/honeypot-protocol.md`](modules/honeypot-protocol.md) | ✅ **框架 + 单测**（协议栈待设计，见模块文档 §8） | 3 |
| 15 | `honeypot-shell` | 命令表分发 + 内存文件系统 + 文件投递（含会话水印）；**可选自研** | L2 | Go / Rust | `modules/honeypot/shell/` | ✅ [`modules/honeypot-shell.md`](modules/honeypot-shell.md) | ⏸ **推迟**（先交付接入架构；内容层待调研） | 3 |
| 16 | `netpolicy` | 微隔离 + 假拓扑 + 运行时检测（复用 Cilium / Tetragon） | L3 | 声明式 + eBPF | `modules/deception/netpolicy/` | ✅ [`modules/netpolicy.md`](modules/netpolicy.md) | 🟡 **声明式产物已就绪**（三份模板，无源码） | 3 |
| 17 | `intent` | **意图识别**（侦察 / 利用 / 横向 / 窃取） | L4 | Python | `analysis/intent/` | ✅ [`modules/intent.md`](modules/intent.md)（设计） | ✅ **已实现（双路：规则 / 模型）**（`--llm` 才走模型） | 3 |
| 18 | `chain` | **攻击链还原** + **识破信号识别**（触发诱饵再生成） | L4 | Python | `analysis/chain/` | ✅ [`modules/chain.md`](modules/chain.md)（设计） | ✅ **已实现（双路）** | 3 |
| 19 | `strategy` | **策略生成** + 诱饵再生成决策（经 `policy` 下发） | L4 | Python | `analysis/strategy/` | ✅ [`modules/strategy.md`](modules/strategy.md)（设计） | ✅ **已实现（双路）** | 3 |
| 20 | `llm-components` | LLM 契约纪律（`AR-15`…`AR-27`）+ **间接注入防护** + 内容预生成 | L4 | Python | `analysis/llm/` | ✅ [`modules/llm-components.md`](modules/llm-components.md)（设计） | ✅ **已实现**（+ 三任务契约 `schemas.py` + 模型适配器 `deepseek.py`） | 3 |
| 25 | `ai-capability` | **AI 能力服务**：可开关的生成出口（强制检查）—— 产出欺骗内容 + 内容清单，经策略面下发到改道侧；**已登记四个 kind**（`content` + L4 三任务）；内容有**模板与模型两条路**（`--llm`） | L4 | Python | `analysis/aicap/` | ✅ [`modules/ai-capability.md`](modules/ai-capability.md) | ✅ **已实现**（阶段 A 通路 + 开关 + 检查；L4 三任务与 `kind=content` 均已接模型，默认关） | 3 |
| 21 | `console` | 控制台：**观测（只读）** —— 告警 · 逐判定日志 · 请求流动 · **配置快照** · **观测新鲜度** · **实时流** | 控制台 | **Go + 静态页**（[ADR-0020](background/decisions/0020-console-minimal-static-ui.md)：暂不引前端工具链） | `modules/console/` | ✅ [`modules/console.md`](modules/console.md) | ✅ **已实现（最小可用）**（页面 + **11 个**只读接口，清单见 [`spec/console-api.md`](spec/console-api.md) §2） | 2b |
| 22 | `control` | **服务面**：gRPC 入参映射 · 会话身份提取 · 熔断（`NI-10`）· **禁止回显的强制点** | 核心 | Go | `common/core/internal/control/` | ✅ [`modules/control.md`](modules/control.md) | ✅ 已实现 | 1 |
| 23 | `decoy` | **诱饵面**：五类（Developer API / 指令文件 / MCP / 数据集 / 蜜饵）+ 多态轮换 | 核心 | Go | `common/core/internal/decoy/` | ✅ [`modules/decoy.md`](modules/decoy.md) | ✅ 已实现（定义与多态；资产→边缘通路另计） | 2b |
| 24 | `honeypot` | **蜜罐入口**：类型注册 + `config` 开关 + 后端池解析；**不实现具体蜜罐**（接第三方） | 核心 | Go | `common/core/internal/honeypot/` | ✅ [`modules/honeypot.md`](modules/honeypot.md) | ✅ 已实现 | 2b |

> ✅ 第 12 行 `adapter-sidecar` **已合并入第 11 行 `adapter-proxy`**（同一份代码的另一种部署形态）。
> 编号保留不重排 —— 编号是引用句柄（同 `D-6` 精神）。因此共 **25 行**（含已合并行），其中 **24 个有效模块**。

---

## 1a. 实现方式总览（自研 / 复用开源）

> 依据 [`design/architecture.md`](design/architecture.md) §1「复用三原则」：
> **基础设施层（代理 / 网关 / 边车 / 隔离）全部复用开源；智能决策层（判定 / 欺骗 / 编排）全部自研**。
> 一句话：**业务逻辑全自研，基础设施与存储全复用开源。** 本表是既有结论的汇总，不是新决策。

| 实现方式 | 模块 | 依据 |
| --- | --- | --- |
| **复用开源组件，不自研** | L0 接入层（Envoy / Nginx / HAProxy / iptables-TEE / CoreDNS） | `AR-3` · 复用三原则① |
| **自研（Go，核心）** | `judge` · `director` · `responder` · `session` · `isolation` · `policy` · `telemetry` · `store` · `control` | 复用三原则② · `TB-22`（判定只在核心自研一次） |
| **自研（Go，L1）** | `adapter-proxy`（③前置 + ④边车）· `adapter-mirror`（接收端）· `edge-injection` | [ADR-0008](background/decisions/0008-edge-language-go.md) · [ADR-0017](background/decisions/0017-caddy-l1-base.md)（转发与 TLS 复用**内嵌 Caddy**）· 复用底座 + 自研判定胶水 |
| **纯配置（复用 CoreDNS）** | `adapter-dns`（② DNS 引流，无源码） | [`design/structure.md`](design/structure.md) §1.5 · 复用三原则① |
| **自研（Go/Rust，L2）+ 内容来源未决** | `honeypot-protocol` · `honeypot-shell` | 复用三原则②；蜜罐内容「克隆 / 模板 / 影子实例」未决（见下） |
| **复用开源 eBPF + 自研编排（L3）** | `netpolicy` | 复用三原则①（Cilium / Tetragon）+ ②（编排自研）；是否进 MVP 未决 |
| **自研（Python，L4）+ 调用云模型** | `intent` · `chain` · `strategy` · `llm-components` | 复用三原则② · `TB-2`；**运行期依赖仍只有 3 项**（`grpcio` / `protobuf` / `PyYAML`）—— 模型经自写的标准库适配器（`analysis/llm/deepseek.py`）调用，**不引** PyTorch / vLLM / Transformers / NetworkX；依据 [ADR-0031](background/decisions/0031-analysis-reuse-and-model-backend.md) 决定 4 与 [`background/research/l4-oss-reuse.md`](background/research/l4-oss-reuse.md) |
| **自研（TypeScript，控制台）** | `console` | [`design/language.md`](design/language.md) §1（前端生态） |

**开源存储（`store` 背后的真实后端，尚未实现）**：

| 存储 | 承载 | 依据 |
| --- | --- | --- |
| Redis | 会话 / 判定缓存 / 幂等 / 隔离 | [`design/structure.md`](design/structure.md) §3 |
| ClickHouse | 遥测事件 / 判定归档 | 同上 |
| PostgreSQL | 策略 / 资产 / 画像 / 攻击链 | 同上 |

**与实现方式相关的未决项**（不替用户拍板，去向见各处）：

| 未决 | 影响模块 | 去向 |
| --- | --- | --- |
| D1 数据平面底座是否用 Envoy | L0 / L1 接入 | [`background/notes/implementation-discussion.md`](background/notes/implementation-discussion.md) §6.1 |
| D6 会话存储（本地 LRU vs Redis） | `session` | 同上 |
| D7 蜜罐后端从哪来（克隆 / 模板 / 影子实例） | `honeypot-protocol` · `honeypot-shell` | 同上 |
| L3 网络欺骗层是否进 MVP | `netpolicy` | [`design/modules.md`](design/modules.md) §7 |

---

## 2. 阶段 1（MVP）—— ✅ 已全部完成

| # | 模块 | 文档 | 代码 |
| --- | --- | --- | --- |
| 1 | `judge` | ✅ | ✅ 含单测 |
| 4 | `session` | ✅ | ✅ 含单测 |
| 7 | `telemetry` | ✅ | ✅ 含单测 |
| 8 | `store` | ✅ | ✅ 含单测（内存实现） |
| 10 | `adapter-mirror` | ✅ | ✅ 含单测 |
| 22 | `control` ⚠️ | ✅ | ✅ 含单测 |

**MVP 的目的**：接一个真实站点，**只观察**，积累 Agent 行为样本、校准判定阈值（`NI-6`（首次上线必须运行在影子模式））。
它**不处置** —— 这是 `control.ShadowDecider` 恒返回 `route_origin` 的原因。

> ✅ **阶段 1 的未闭合项已闭合**（2026-09-18）：判定引擎曾**没有规则来源**
> （`main.go` 传空规则集，管线跑得通但恒产出零分、无信号）。
> 现由 **`policy` 模块**（阶段 2a）从配置文件装载规则并供给 `judge.RuleSource`；
> **禁止**再以空规则集静默启动 —— 配置缺失或非法**必须**让进程启动失败。

---

## 2.1 阶段 2a —— 🟡 进行中

| # | 模块 | 文档 | 代码 |
| --- | --- | --- | --- |
| 6 | `policy` | ✅ | ✅ 含单测 + **下发面已实现**（`Pull` / `Ack`；含 AI 内容清单装载与投影） |
| 11 | `adapter-proxy` | ✅ | ✅ 含 `-race`；内嵌 Caddy + 策略面消费：后端表 / 白名单 / 注入规则 / **AI 内容清单** |
| 13 | `adapter-dns` | ✅ | 🟡 `Corefile.example` 已就绪 |
| 2 | `director` | ✅ | ✅ 6 项决策已确认并实现（阈值→三值 + 灰度 + 后端名） |

---

## 2.2 开发期验证入口

只服务于**开发内循环**，不是交付形态。

| 命令 | 作用 | 说明 |
| --- | --- | --- |
| `make check-config` | 配置干跑：装载 + 校验 + 打印策略摘要，**不开端口** | 对应 `core -check-config`；路径取 `SHEN_CONFIG` |
| `make replay` | 规则回放：打印「样本观测 → 分数 → 命中信号」 | 判定响应禁止回显分值（`ST-7`），规则是否命中只能在这里看 |
| `make smoke` | 在线冒烟：对已在跑的核心发判定请求，校验响应形状与 `decision_id` 幂等 | 工具：`scripts/devcheck/`（不是产品代码） |
| `make dev` | **一键**：配置干跑（合法 + 非法）→ 起核心 → 在线冒烟 → 规则回放 → 关核心 | 编排：`scripts/dev/smoke.sh`，用临时配置与临时端口 |

> 配置契约（字段、约束、JSON Schema）见 [`spec/config.md`](spec/config.md)。
