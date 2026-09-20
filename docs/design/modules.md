# 模块能力

> 规则 ID 前缀 `MD`。写作要求见 [`README.md`](README.md) §2。
>
> **来源**：外部设计稿 [`../background/research/deception-engine-design.md`](../background/research/deception-engine-design.md) §4.6 / §8.2；
> 模块边界规则 M-1…M-4 承接本次重构前的 [`../README.md`](../README.md)。
> **已确认**：用户于 2026-09-17 确认 **MD-1…MD-17 全部通过**；
> 同日**模块清单轮追加确认 §1.1（当时 21 个模块）与 MD-18…MD-22**（此后新增 `control`、`decoy`、`honeypot`、`ai-capability`，现为 **24 个有效模块**，见下表）；
> ✅ 用户于同日确认**新增 `control` 为第 22 个模块**（原为 `architecture.md` §10.2 缺口 12）。
> ✅ 用户于同日确认：**阶段 2 拆为 `2a` / `2b`**、**③④ 合并为 `adapter-proxy`**（第 12 行保留编号登记为已合并）、
> **L1 语言由 Lua 改为 Go**（[ADR-0008](../background/decisions/0008-edge-language-go.md)）。
> ✅ 用户于同日确认 **`MD-23` / `MD-24`** 与改写后的 **`MD-12`（决策取值只在 director 定义一次） / `MD-13`（severity 与决策取值并列输出）**。
> ✅ 用户于 2026-09-20 确认**新增 `ai-capability` 为第 25 个模块**（L4 · Python · `analysis/aicap/`）与
> [`architecture.md`](architecture.md) 的 **`AR-33`**（欺骗内容生成必须经护栏出口）—— 依据
> [ADR-0023](../background/decisions/0023-deception-content-injection.md)。
> ⚠️ MD-8 的强制手段是 CI 测试，**不是编译器** —— 可被绕过的风险登记在实现期处理。

---

## 1. 权威模块清单

> **本节是模块的唯一权威清单。** 一个模块 = **一个源码目录** + **一份模块文档** + **一个独立可运行的测试套件**。
> 本节替代了原先的「组件清单」—— 原表混用运行单元与源码单元，与 `MD-1`（一个模块只属于一层） / `MD-2`（一模块一份文档，必须用固定模板） / `ST-1`（代码必须按 §1.1 的顶层目录组织） 无法对齐。

### 1.1 源码模块（25 行，其中第 12 行已合并）

#### 阶段图例

| 阶段 | 含义 |
| --- | --- |
| `1` | **MVP** —— 只观察，不处置 |
| `2a` | **接管与引流打通** —— 在请求路径上，能把流量引流到指定后端；**不含任何欺骗内容** |
| `2b` | **处置内容** —— 投毒内容生成、假路径、蜜饵注入、响应改写 |
| `3` | 高交互与智能 |

> `2` 原为「接管与处置」一档，2026-09-17 经用户确认**拆为 `2a` / `2b`**：
> 引流打通与欺骗内容生成是两件可独立验证的事，混在一档会让「引流是否打通」无法单独判定。
>
> **`2a` 的划分依据**：`adapter-proxy` · `adapter-dns`（引流执行）· `director`（**引流的前提**——
> 核心必须能返回 `route_mirage`，否则适配器永远只会放行）· `policy`（灰度比例决定引流范围）。
> **`2b` 的划分依据**：`responder` · `edge-injection` · `isolation` · `console` —— 都产出或管理**欺骗内容与处置状态**。

| # | 模块 | 层 | 语言 | 源码目录 | 模块文档 | 职责 | 阶段 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | `judge` | 核心 | Go | `core/internal/judge/` | `docs/modules/judge.md` | 把观测变成判定：风险分、信号列表、证据链 | 1 |
| 2 | `director` | 核心 | Go | `core/internal/director/` | `docs/modules/director.md` | 决策与后端选择；输出决策取值与 `severity`（见 §4） | **2a** |
| 3 | `responder` | 核心 | Go | `core/internal/responder/` | `docs/modules/responder.md` | 投毒内容生成、引流目标选择、话术模板渲染 | 2b |
| 4 | `session` | 核心 | Go | `core/internal/session/` | `docs/modules/session.md` | 会话身份提取（`INT-19` 优先级）与状态抽象 | 1 |
| 5 | `isolation` | 核心 | Go | `core/internal/isolation/` | `docs/modules/isolation.md` | 隔离记录写入 / TTL 过期 / 查询短路 | 2b |
| 6 | `policy` | 核心 | Go | `core/internal/policy/` | `docs/modules/policy.md` | 策略版本、灰度比例、**下发与回执**（`api/policy/v1`：`Pull` + `Ack`；`Watch` 未实现，见 [ADR-0018](../background/decisions/0018-policy-plane-pull-model.md)） | **2a** |
| 7 | `telemetry` | 核心 | Go | `core/internal/telemetry/` | `docs/modules/telemetry.md` | 事件归一化、批量异步上报、失败缓冲重放 | 1 |
| 8 | `store` | 核心 | Go | `core/internal/store/` | `docs/modules/store.md` | Redis / ClickHouse / PostgreSQL 访问层 —— **核心唯一的 I/O 出口** | 1 |
| 22 | `control` | 核心 | Go | `core/internal/control/` | `docs/modules/control.md` | gRPC 服务面：入参映射、会话身份提取、**禁止回显的强制点**、遥测与策略面的对账 | 1 |
| 23 | `decoy` | 核心 | Go | `core/internal/decoy/` | `docs/modules/decoy.md` | **诱饵面**：功能性伪装诱饵（Developer API / 指令文件 / MCP / 消耗战数据集）+ 三类蜜饵 + SSRF 蜜饵；产出诱饵定义与投放片段。依据 [ADR-0010](../background/decisions/0010-functional-camouflage.md) | 2b |
| 24 | `honeypot` | 核心 | Go | `core/internal/honeypot/` | `docs/modules/honeypot.md` | **蜜罐入口与后端池**：类型注册 · `config` 开关 · 生命周期 · 把 `route_mirage` 的逻辑后端名解析到实例。**不实现具体蜜罐**（接第三方）。依据 [ADR-0011](../background/decisions/0011-honeypot-entry-external-backends.md) | 2b |
| 9 | `edge-injection` | L1 | Go | `edge/injection/` | `docs/modules/edge-injection.md` | 投毒响应改写、假路径、蜜饵注入（被适配器引用，**不独立部署**，`ST-5`） | 2b |
| 10 | `adapter-mirror` | L1 | 配置 + Go | `edge/mirror/` | `docs/modules/adapter-mirror.md` | 只读采集，**不处置**（**不在请求路径**，见 `INT-6`） | **1** |
| 11 | **`adapter-proxy`** | L1 | Go | `edge/proxy/` | `docs/modules/adapter-proxy.md` | ③ 反向代理前置 **与** ④ Sidecar 的**同一份实现**：拦截 → 调核心 → 按决策选 upstream → 异步上报。两形态的差别只在部署位置与 upstream 指向。转发与 **TLS 终结**用**内嵌 Caddy**（[ADR-0017](../background/decisions/0017-caddy-l1-base.md)）；模块身份与对外接口不变 | **2a** |
| ~~12~~ | ~~`adapter-sidecar`~~ | L1 | Go | ~~`edge/sidecar/`~~ | —— | ❌ **已合并入第 11 行 `adapter-proxy`**（同一份代码的另一种部署形态，不构成独立模块） | —— |
| 13 | `adapter-dns` | L1 | 配置 | `edge/dns/` | `docs/modules/adapter-dns.md` | 按来源解析到引擎或真实服务（**纯配置，无源码**） | **2a** |
| 14 | `honeypot-protocol` | L2 | Go / Rust | `deception/honeypot/` | `docs/modules/honeypot-protocol.md` | 协议仿真（SSH / MySQL / Redis / FTP）+ 交互捕获 | 3 |
| 15 | `honeypot-shell` | L2 | Go / Rust | `deception/shell/` | `docs/modules/honeypot-shell.md` | 命令表分发、内存文件系统、文件投递 | 3 |
| 16 | `netpolicy` | L3 | 声明式 + eBPF | `deception/netpolicy/` | `docs/modules/netpolicy.md` | 微隔离、假拓扑、运行时检测与阻断 | 3 |
| 17 | `intent` | L4 | Python | `analysis/intent/` | `docs/modules/intent.md` | 意图识别 | 3 |
| 18 | `chain` | L4 | Python | `analysis/chain/` | `docs/modules/chain.md` | 攻击链还原（引用须校验证据存在，`AR-12`） | 3 |
| 19 | `strategy` | L4 | Python | `analysis/strategy/` | `docs/modules/strategy.md` | 策略生成 | 3 |
| 20 | `llm-components` | L4 | Python | `analysis/llm/` | `docs/modules/llm-components.md` | 契约校验、容错解析、双阶段收尾、内容黑名单（`AR-15`…`AR-27`） | 3 |
| 25 | `ai-capability` | L4 | Python | `analysis/aicap/` | `docs/modules/ai-capability.md` | **AI 能力服务**：可开关的共享**生成出口**（唯一入口 `generate(TaskSpec) → Envelope`），**内部强制走护栏**（前置提示词 + 后置独立校验）；阶段 A 产出欺骗内容并经策略面下发到改道侧。依据 [ADR-0023](../background/decisions/0023-deception-content-injection.md) / `AR-33` | 3 |
| 21 | `console` | 控制台 | TypeScript | `console/` | `docs/modules/console.md` | 策略编排、蜜饵资产管理、人工干预、审计查询 | 2b |

### 1.2 工具（不是模块）

| 工具 | 源码目录 | 职责 | 依据 |
| --- | --- | --- | --- |
| `sentinel` | `scripts/sentinel/` | **差异哨兵**：对同一组 URL 在真实业务与蜜罐各打一次，逐字段 diff（12 项） | 最小可信路径**第 0 步** |
| `doctor` | `scripts/doctor/` | 接入自检五项 | `INT-17` |
| `check-leak` | `scripts/check-leak/` | `OH-1` 禁用串扫描 | `OH-3` |

### 1.3 不产生源码模块的

| 项 | 为什么 |
| --- | --- |
| L0 接入层 | `AR-3` **禁止**自研；产出是 `deploy/` 下的配置模板 |
| 安全底座（沙箱 / 单向通道 / 零信任） | 主要是部署配置与 seccomp / Cilium 策略，不是源码 |

### 1.4 核心模块的依赖方向

```text
        任何一个 核心 模块
             ├──► store        （唯一允许的 I/O 出口）
             └──► 同层其他核心模块
        store ──✖──► 业务模块    （禁止反向依赖）
```

| ID | 规则 | 验证方式 |
| --- | --- | --- |
| **MD-18** | 模块**必须**对应 §1.1 的一行；新增、拆分或合并模块**必须**同步更新本节与 [`structure.md`](structure.md) §1 的目录布局。 | `make archcheck`（清单与目录逐行核对）+ 评审 |
| **MD-19** | 每个模块**必须**有唯一源码目录，映射关系**必须**记录在 §1.1；清单外的目录**禁止**承载业务逻辑（`api/` 的生成物、`core/internal/contract/` 的共享类型、`scripts/` 的工具、`deploy/` 的配置除外）。 | `make archcheck`（目录检查） |
| **MD-20** | 核心模块访问外部存储**必须**经 `store`；**禁止**其他核心模块直接连接 Redis / ClickHouse / PostgreSQL。`store` **禁止**反向依赖任何业务模块。 | `make archcheck`（依赖方向检查，即 `TB-15` 的门禁项） |
| **MD-21** | **阶段 1（MVP）的交付必须只包含 §1.1 标记为 `1` 的模块**；实现标记为 `2` / `3` 的模块**必须先**取得用户确认并更新本节。 | 交付清单核对 |
| **MD-22** | 每个模块**必须**有**独立可运行**的测试套件；测试**禁止**依赖其他模块的真实实例（用替身或契约桩）。 | 单模块测试可独立执行 |

---

## 2. 模块边界

| ID | 规则 | 验证方式 |
| --- | --- | --- |
| **MD-1** | **一个模块必须只属于一层**（L0–L4 / 核心 / 底座 / 控制台）；**禁止**同一模块跨层拆分。 | 架构评审 |
| **MD-2** | **一个模块一份文档**：`docs/modules/<module-kebab>.md`，**禁止**合并多个模块；**必须**使用 [`../modules/_template.md`](../modules/_template.md) 的固定章节（一节都不许删，不适用时写「不适用」并说明原因）。 | 文档评审 |
| **MD-3** | **跨模块与对外契约放 [`../spec/`](../spec/)**；模块文档**禁止**定义对外契约。 | 文档评审 |
| **MD-4** | 模块依赖方向**必须**单向：适配器 → 核心 → 数据面；**禁止**反向依赖，**禁止**适配器互相依赖。 | 依赖检查 |
| **MD-5** | 跨模块共享的**类型定义必须**收敛到一处，**禁止**各自定义同名类型：**跨进程**契约 → [`../spec/`](../spec/)（协议见 [`structure.md`](structure.md) 的 `api/`）；**进程内**共享类型 → `core/internal/contract/`。 | 代码评审 + 代码生成检查 |
| **MD-6** | 核心模块**禁止**做 I/O 之外的副作用：**禁止**发起外呼、**禁止**写业务存储、**禁止**依赖系统时钟做判定（时间**必须**由调用方注入）。 | 代码评审 + 单元测试（可注入时钟） |
| **MD-7** | 判定规则**必须**单测覆盖：每条规则至少一个正样本 + 一个负样本（含伪造 UA、补齐人类请求头的绕过样本）。 | CI + 覆盖率报告 |
| **MD-8** | 判定器的**分支枚举必须**有穷尽性测试 —— 新增分支而未补测试**必须**导致 CI 失败。 | CI |
| **MD-25** | **诱饵面必须 observe-only** —— 在诱饵面上**禁止**产出 `challenge` / `isolate` / `block` 处置。理由：采集链路的每一步都在诱饵面上，在诱饵面阻断 = 掐断自己的情报源。实现上**必须**由一个集中常量（诱饵前缀集）在决策执行处统一豁免，并附断言。依据 [ADR-0010](../background/decisions/0010-functional-camouflage.md)。 | 单元测试 + 决策执行处断言 |
| **MD-26** | **蜜罐必须经 `honeypot` 模块管理**：具体蜜罐实现**禁止**编译进核心，**必须**是可替换后端（满足蜜罐契约）。蜜罐类型的启用**必须**由配置决定（`ST-24`）。依据 [ADR-0011](../background/decisions/0011-honeypot-entry-external-backends.md)。 | 依赖检查 + 配置校验 |

> **2026-09-19 措辞修正（经用户确认）**：`MD-5` 与 `MD-19` 的原文只说「收敛到 `api/`」、例外清单也不含
> `core/internal/contract/`，与 [`structure.md`](structure.md) §1.2 / §1.6.2（把 `contract` 定义为「只放类型、无逻辑」的
> 进程内词汇表叶子）及 `scripts/archcheck` 的结构性目录白名单**不一致**。本次把两条改准：
> **跨进程**契约 → `../spec/` + `api/`；**进程内**共享类型 → `core/internal/contract/`。
> 规则效力未变（仍然禁止各自定义同名类型、禁止清单外目录承载业务逻辑），只消除歧义。

---

## 3. 适配器职责边界（不许越界）

```text
适配器只做四件事：
  1. 拦截请求
  2. 查本地判定缓存；未命中则调用核心 Judge（deadline ≤ 3ms，超时 fail-open）
  3. 按结果执行：透传 / 投毒 / 引流
  4. 异步上报遥测（幂等）

适配器不做的事：
  1. 不实现判定逻辑
  2. 不做 LLM 推理
  3. 不维护会话状态
  4. 不写入核心状态（遥测可上报，状态变更必须经核心）
```

| ID | 规则 | 验证方式 |
| --- | --- | --- |
| **MD-9** | 适配器**必须**只做上述四件事；**禁止**出现 §3「不做的事」中的任一行为。 | 适配器代码评审 + 依赖方向检查 |
| **MD-10** | 适配器**禁止**持有跨请求的**业务**状态；判定缓存与策略缓存**允许**，但**必须**有容量上限与失效规则。 | 代码评审 + 压力测试 |
| **MD-11** | 适配器**禁止**引入阻塞调用（不得阻塞代理事件循环）。 | 性能测试 + 代码评审 |

---

## 4. 决策与加重

> **决策取值 = 三值**（`route_origin` / `route_mirage` / `block`）；**加重强度 = `severity` 旁路字段**。
> 取值与写法见 [`terminology.md`](terminology.md) §4；候选对比、负面后果与**失效条件**见 [ADR-0002](../background/decisions/0002-decision-model.md)。

### 4.1 归属

决策类型**只在 `director` 模块定义**（见 §1.1 第 2 行）。`director` **必须**同时输出**两样**：

```text
decision  : route_origin | route_mirage | block      ← 去哪儿
severity  : none                                     ← 多重手（旁路，不改变去向）
```

### 4.2 为什么不要可见处置（记录理由，避免重复讨论）

`challenge`（挑战页）是**评估过**的选项，**未采纳**，理由两条：

1. **它会自曝** —— 可见拦截一出现，对手就知道「有东西在拦我」，透明改道随之失效（`D0` 推论 3）
2. **MVP 阶段执行不了** —— 第 1 步是旁路镜像，引擎**不在请求路径上**，无法返回 403 或弹挑战页（`INT-6`（镜像形态不适用「引擎在请求路径上」的规则））

因此引擎是**欺骗调度器**，不是 WAF；可见拦截交给接入层的其他组件。

| ID | 规则 | 验证方式 |
| --- | --- | --- |
| **MD-12** | 决策取值**必须**只在 `director` 定义一次；**禁止**在核心之外复制该枚举，**禁止**把决策取值写进持久化契约（`../spec/`）、指标名或对外接口。 | 代码评审 + 依赖检查 |
| **MD-13** | `severity` **必须**与决策取值**并列输出**；**禁止**并入决策枚举。 | 单元测试 |
| **MD-23** | `severity` 产生的加重**必须可归因于环境**（网络抖动 / 服务端负载 / 数据本就不全），**禁止**产生可归因于防御行为的信号：不加响应头、不改状态码、不引入新 cookie、**禁止** `429` / `503` 这类语义明确的限流响应。 | 自动检查（`OH-3`）+ 单元测试 |
| **MD-24** | `severity` 的档位**必须**只使用 [`terminology.md`](terminology.md) §4.2 登记的值；新增档位**必须先**实测并登记。 | 配置校验 |

---

## 5. 失败模式与降级

| 失败情形 | 行为 | 依据 |
| --- | --- | --- |
| 输入非法 / 未识别 / 未匹配 / 解析失败 | **必须**回落放行到真实业务 | [`constraints.md`](constraints.md) 的 NI-5 |
| 判定超时 | **必须**返回「无意见」并放行 | NI-4 |
| 引擎完全故障 | 业务请求**必须** 100% 正常 | NI-1 / NI-3 |
| 外部依赖（Redis / 遥测存储）不可用 | 隔离名单读不到 → 回退调用核心判定（fail-open）；**禁止**阻断业务；**必须**告警 | NI-10 |
| 资源耗尽（CPU / 内存） | **必须**受 cgroup 上限约束，不与业务抢资源 | NI-7 |
| LLM 输出不合格 | 校验失败**必须**抛异常；两阶段均失败 → 整体作废 | [`architecture.md`](architecture.md) 的 AR-15 / AR-21 |
| 蜜罐子进程超时 | **必须**按进程组回收；双层次超时兜底 | MD-14 |

| ID | 规则 | 验证方式 |
| --- | --- | --- |
| **MD-14** | 蜜罐与协议仿真子进程**必须**：① 启动时创建独立进程组 ② 超时后先 `SIGTERM`、宽限后 `SIGKILL`、**按进程组**终止 ③ 双层超时（外层 watchdog + 内层略短于外层）。 | 专项测试（假可执行文件启动真子进程） |
| **MD-15** | 蜜罐**必须**资源创建与释放对称：启动监听 / 事件循环 / 线程时登记，停止时按登记**逆序**回收；清理**必须**在独立线程池执行，不阻塞主循环。 | 反复启停测试（无累积泄漏） |
| **MD-16** | 蜜罐并发连接数、线程数**必须**设上限，超限即拒绝并记录。 | 压力测试 |

---

## 6. 模块文档的去向

**已创建模块文档**：阶段 1 的 7 个模块（`judge` `session` `telemetry` `store` `control` `adapter-mirror`，加 `_template.md` 与索引）。
权威清单见 **§1.1**（25 行，其中第 12 行已合并 ⇒ **24 个有效模块**），按 `MD-2` 与 `MD-17` **一模块一文件**创建到 [`../modules/`](../modules/)。

> **关于第 25 行的位置**：`ai-capability` 是 L4 模块，紧跟在同层第 20 行 `llm-components` 之后。
> 它与 `llm-components` 的分工：**`llm-components` 是纪律层**（契约 / 容错 / 长度 / 黑名单，被各处引用），
> **`ai-capability` 是服务层**（唯一出口 + 强制护栏 + 任务注册表）。两者不同层职责，**禁止**互相替代。

> **关于第 22 行的位置**：`control` 是核心模块，本应排在核心组内；但编号**必须稳定**
> （同 `D-6`（规则 ID 不得重新编号、不得复用）的精神：编号是引用句柄，`MD-18` 靠它对接目录）。
> 因此它**编号 22、排在第 8 行之后** —— 编号不按位置递增，是刻意的。

| ID | 规则 | 验证方式 |
| --- | --- | --- |
| **MD-17** | 模块文档**必须**在实现前创建；**禁止**先写代码再补文档。 | 提交评审（同一次变更内的文件顺序） |

---

## 7. 未决项

| # | 未决 | 去向 |
| --- | --- | --- |
| 1 | `severity` 的具体档位（见 [`terminology.md`](terminology.md) §4.2）—— 属**实测问题** | 实现期实测后登记 |
| 2 | 会话状态存储：本地 LRU vs Redis（多副本影响） | [`../background/notes/implementation-discussion.md`](../background/notes/implementation-discussion.md) |
| 3 | 蜜罐后端从哪来（克隆 / 模板 / 影子实例） | 同上 |
| 4 | L3 网络欺骗层是否进 MVP | 同上 |
