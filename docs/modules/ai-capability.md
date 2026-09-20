# 模块：`ai-capability`

| 项 | 内容 |
| --- | --- |
| 模块名 | `ai-capability`（第 25 行） |
| 所属层 | `L4`（依据 `MD-1`：一个模块只属于一层） |
| 实现语言 | `Python`（依据 [`../design/language.md`](../design/language.md) §1 的 L4 行、`TB-2` / `TB-20`） |
| 负责人 | —— |
| 状态 | 阶段 A 已实现（通路 · 开关 · 强制护栏）；模型后端与风格画像属阶段 B |
| 最后更新 | 2026-09-20 |

---

## 1. 职责

**做什么**（一句话：**唯一的、强制走护栏的生成出口**）：

- 对外只有一件事：`generate(TaskSpec) → Envelope`（`{accepted, data}`，`AR-16`）；
- **内核任务无关**：内核（`service.py` / `model.py` / `tasks/_registry.py` / `guardrail/`）只做
  「取任务 → 前置护栏 → 生成 → 后置护栏 → 交 `Sink`」，**不认识**「内容」——
  判据：把 `kind=content` 整块删掉，内核仍应原样可用（[ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md) 决定 1）；
- **接入一个新消费方 = 加 `produce`/`build` + 一条登记**，走
  [`../spec/ai-contract.md`](../spec/ai-contract.md) §6 的三步，内核**一行不改**；
- **任务注册表**：每个 `kind` 一条登记，必须声明 `schema` + `guardrail_profile` + `limits`，
  缺一样即**启动期断言失败**；未登记的 `kind` 直接拒绝；
- **强制护栏**（不依赖调用方自觉）：前置提示词三段式（`AR-31` / `AR-24`）→ 生成 → 后置独立校验
  （`schema` → 黑名单三类 → 长度 → 风格一致性，`AR-15` / `AR-22` / `AR-23`）；
  受检字段与上限**由任务声明**，且**声明了就必须真的生效**；
- **产物出口是缝隙**（`aicap/ports.py` 的 `Sink`），内核不知道它是不是内容库；
  只有过了护栏才会调 `put`；
- **阶段 A 的任务**：`kind=content` —— 产出「资源 × 变体」的欺骗内容对象，
  并（由 `__main__` 的生成器）产出**内容清单**文件给核心装载；
- **可开关**：`kinds` 未启用即不产出（`ai.enabled=false` 时对系统零影响）。

**明确不做什么**：

- **不实现判定**（`AR-2` / `AR-5`）—— 它不接触请求，也不产出任何决策；
- **不在请求路径上**（`AR-30` / `AR-29`）—— 热路径永不调模型；生成是**离线**批量任务；
- **不持有执行能力**（`AR-32` / `SB-1` / `SB-2`）：不下发指令、不生成载荷、不调外部系统；
- **不直接回写响应**（`AR-32`）：内容**只**经 `policy` 投影成策略载荷间接生效；
- **不自己写核心存储**（`MD-20`）—— Python 侧产出文件，核心侧装载进 `store.ContentStore`；
- **不做边缘的执行**：改写由 `edge-injection` 在适配器内完成（`ST-5`）。

> `MD-1`：本模块不跨层拆分。⚠️ 它**不是** L1/L2 的执行器 —— 见上「不做什么」。
>
> 📖 **要评审「这套 AI 能力设计合不合理、哪里要优化」** → [`../kb/ai-capabilities.md`](../kb/ai-capabilities.md)
> （7 个能力逐个详解 · 模型后端硬约束 · 16 条优化点候选 · 6 条关键发现 · **§12 模块生命周期** · **§13 怎么接入被使用**）。
> 本文只写本模块自身的职责与契约；状态表的权威在本文 §5，运行期时序在 `kb` 那份 §12。

## 2. 输入 / 输出契约

| 方向 | 契约 | 定义位置 |
| --- | --- | --- |
| 输入 | `TaskSpec`（`kind` / `session_id` / `deadline_s` / `payload`） | [`../spec/ai-contract.md`](../spec/ai-contract.md) §1.1 |
| 输出 | `Envelope` 与**产物**（`Artifact`：只需 `to_wire()`） | 同上 §1.2 / §2 |
| 输出（文件） | 内容清单（JSON）—— `kind=content` 专有 | 同上 §3 |
| 接入新消费方 | 三步（登记 + 产物出口） | 同上 §6 |
| 消费方 | 核心 `policy`（装载 + 投影）· 适配器 `edge/proxy`（消费） | 同上 §3 / §4 |

本模块内部的数据结构（不跨模块）：

- **内核侧**（任务无关）：`Artifact` / `Sink`（`aicap/ports.py`）· `Task` / `GuardrailProfile` / `TaskLimits`（登记表）；
- **插件侧**（`kind=content` 专有）：`ContentObject` · `ContentStore`（内存实现）· 清单聚合。

> `MD-5`：跨模块共享的类型**禁止**各自定义 —— 内容对象与清单字段一律以 `spec/ai-contract.md` 为准。
> 契约里已有的东西**禁止**在本模块重新定义（信封复用 `analysis/llm/envelope.py`，黑名单复用 `analysis/llm/blacklist.py`）。

## 3. 依赖

| 允许依赖 | 原因 |
| --- | --- |
| `analysis.llm.*`（`contract` / `envelope` / `blacklist` / `limits` / `prompts` / `untrusted` / `client`） | 纪律层：契约校验、信封、黑名单、长度、提示词资源、不可信数据区 —— **本模块是 `llm.client` 的允许调用者**（`AR-33`） |

| 禁止依赖 | 原因 |
| --- | --- |
| 任何核心 / 边缘 / 控制台代码 | 平面隔离（`ST-2` / `ST-4`）；跨语言只走 wire format（`TB-24`） |
| 事件/存储客户端（`analysis.telemetry`） | 它不经遥测面写结论（那是 `analysis.worker` 的事）；内容经清单文件交给核心 |
| 任何网络出站库 | 阶段 A 无模型后端；阶段 B 的模型必须本地/自托管（除非单独 ADR） |

> `MD-4` 依赖方向单向；`MD-6` 的「核心模块禁止外呼」不适用本模块（它不是核心模块），
> 但**它同样禁止在生成期依赖系统时钟做判定**：`generated_at` 只作审计，不参与任何决策。

**复用候选（尚未引入 —— 不是已批准的依赖）**：本模块的两处手写生成能力已对过开源实现，
判据与边界见 [ADR-0024](../background/decisions/0024-ai-oss-reuse-boundary.md)（**只看默认行为是否 fail-closed**）：

| 能力 | 候选 | 为什么现在**不**引 |
| --- | --- | --- |
| `AR-33` 唯一出口 + 任务注册表 + 启动期断言（`service.py` / `tasks/_registry.py`） | `guardrails-ai` · `NeMo Guardrails`（均 Apache-2.0） | 两者都是「框架 + Hub + 服务端」形态，而本模块要的是「一个函数 + 启动期断言 + 门禁结构检查」；`guardrails-ai` 的 `fix` / `reask` 动作与 `AR-15`（禁止修复后放行）直接冲突，且框架会进调用图 ⇒ **`AR-33` 的机器判据失效** |
| 阶段 A 的确定性模板生成器（`tasks/content.py`） | `Faker`（MIT，19.4k★）· `Mimesis`（MIT） | 两者都有 `seed()` ⇒ **可满足 `AR-30` 的确定性要求**；但落地属阶段 B（与真实模型后端一起做），且需先验 `seed()` 的跨版本稳定性（ADR-0024 失效条件 5） |

## 4. 关键规则

| 规则 | 与本模块的关系 |
| --- | --- |
| `AR-33` | **本模块存在的理由**：欺骗内容生成必须经本模块的唯一出口并强制过护栏；模型客户端只允许被本能力自身 import（`service.py` 出口 · `model.py` 接缝；能力之外一律禁止——门禁结构检查） |
| `AR-15` | 后置校验失败**必须**抛异常/拒绝 —— 禁止默认值、禁止静默降级 |
| `AR-16` | 输出统一信封 `{accepted, data}`；`accepted=false` 必须记拒绝原因 |
| `AR-22` | 黑名单三类（泄露 / 自曝 / 超长）在入库**之前**校验 |
| `AR-23` | 长度按用途设上限（内容用途 `deception_content`） |
| `AR-24` | 提示词**必须**是仓库内资源文件，启动期校验存在与占位符齐全 |
| `AR-30` | 阶段 A 的生成器**必须**确定性（同输入逐字节同输出）—— 这是热路径字节一致的前提 |
| `AR-31` | 攻击者可控内容只经数据区进入提示词；原始观测不做净化 |
| `AR-32` | 不持执行能力；不在请求路径上 |
| `MD-2` / `MD-17` | 本文档先于实现（九章固定） |
| `MD-5` | 契约类型只在 `spec/` 定义 |
| `MD-20` | 不直连存储；核心侧装载由 `policy` 经 `store` 完成 |
| `NI-1` | 关闭时对业务与判定**零影响**；任何失败都不阻断请求 |

## 5. 状态与生命周期

| 状态 | 存在哪里 | 生命周期 | 多副本一致性 |
| --- | --- | --- | --- |
| 任务注册表 / 护栏档案 | 代码（`tasks/_registry.py`） | 进程生命周期；启动期断言 | 进程内常量，天然一致 |
| 提示词模板 | 仓库资源（`resources/prompts/*.md`） | 随版本分发（`AR-24`） | 随代码版本一致 |
| 生成结果（内容对象） | **内容库**：核心侧 `store.ContentStore`（内存实现） | 装载期 `Put`；重启需重新装载 | 每个核心副本各自装载同一份清单 ⇒ 逐字节一致 |
| 内容清单（文件） | 部署物料（`ai.manifest` 指向的路径） | 生成器产出；轮换时重新产出 + 版本递增 | 同一份文件 ⇒ 投影结果一致 |

> `AR-9` 的「核心无状态」不因本模块而破：清单是**配置类输入**（随策略版本化），不进请求状态。
> 阶段 A 的生成器**不常驻**（进程退出即结束），因此没有运行期状态。

## 6. 失败模式与降级

| 失败情形 | 行为 | 是否满足 `NI-1` | 依据 |
| --- | --- | --- | --- |
| `ai.enabled=false` / `kinds` 未含该 kind | 不生成、不新增内容（存量内容仍在库） | ✅（默认即此态） | `ADR-0023` 决定 4 |
| 未登记的 `kind` | 拒绝（`Envelope{accepted:false}` 或抛异常）；**禁止**猜着生成 | ✅ | `AR-33` |
| 任务缺护栏档案 / schema / limits | **启动期断言失败**（fail-closed） | ✅（起不来 ≠ 业务受影响） | `AR-33` |
| 提示词模板缺失 / 占位符不齐 | 启动期断言失败（`AR-24`） | ✅ | `AR-24` |
| 模型未配置而任务需要模型 | `Unavailable` 显式失败；**禁止**用模板冒充模型输出 | ✅ | `AR-15` |
| 后置校验任一关不过 | 内容**不入库**、**不进清单**；拒绝原因进日志 | ✅ | `AR-15` / `AR-22` / `AR-23` |
| 内容体超单条上限（64 KiB） | 该条**不入清单** + warn（不整份作废） | ✅ | `spec/ai-contract.md` §3 |
| 清单文件缺失 / 版本读不懂 / `variants` 与配置不一致 | 核心**启动失败**（配置类输入的严格度） | ✅（宁可起不来，不可下发错内容） | `ST-21` 的精神 |
| 核心侧单条校验和不符 | 丢弃该条 + warn（宁可漏注入，不可注入错内容） | ✅ | `spec/ai-contract.md` §0 |
| 适配器侧任一条件不成立 | 原样返回响应（不注入）；上报 `inject` 为对应取值：**开关关 → `disabled`**，开关开但没可用内容 → `no_content` | ✅ | `NI-1` / `INT-8` |

## 7. 测试

| 类型 | 覆盖什么 | 位置 |
| --- | --- | --- |
| 单元 | 注册表：缺护栏档案必拒 · 未登记 kind 必拒 · 启动期断言 | `analysis/tests/test_aicap_guardrail.py` |
| 单元 | 后置护栏四关：坏结构必抛（`AR-15`）· 黑名单三类各一例（`AR-22`）· 长度（`AR-23`）· 风格一致性 | 同上 |
| 单元 | 提示词三段式与不可信数据区（`AR-31`）：原文保留、数据区标记成对、缺模板即失败（`AR-24`） | 同上 |
| 单元 | 内容对象：`checksum` 稳定 · `content_id` 幂等 · 同输入逐字节相同（`AR-30` 的前提） | `analysis/tests/test_aicap_content.py` |
| 单元 | 模板生成器：N 个变体互不相同且各自可复现 | 同上 |
| 单元 | 清单文件：形状 · 单条上限 · 重复资源/variant 拒绝 | 同上 |
| 结构 | 「无绕过路径」：`analysis/` 下除出口（`service.py`）·接缝（`model.py`）·`llm/` 自身与测试外，禁止 import 模型客户端 | `make archcheck`（`AR-33` 项） |
| 结构 | **内核任务无关**：`analysis/aicap/**` 的仓内依赖只允许 `analysis.llm` 与自己；`analysis/llm/**` 禁止反向依赖 `aicap` | `make archcheck`（`MD-4` 项） |
| 单元 | **假 kind 走完整内核**（只在测试里存在、字段名与产物都不是「内容」）—— 解耦的可执行证明 | `analysis/tests/test_aicap_guardrail.py::test_run_task_is_task_agnostic` |
| 单元 | 声明即纪律：受检字段缺失/非字符串必拒 · 任务级 `max_output` 真的生效 · 多字段逐个检查 | 同上（`test_run_task_rejects_*` / `test_run_task_*cap*` / `test_run_task_scans_every_checked_field`） |
| 集成 | 生成 → 核心装载 → 投影 → 适配器命中（端到端） | `make dev` + `make ai-check` |

> 故障注入（`NI-12` 的 `V-1…V-5`）与本模块无关：它不在请求路径上，任何失败都不影响业务响应。

## 8. 未决项

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | 模型后端与结构化输出框架（Outlines / Instructor 等） | 阶段 B 的生成质量与多样性 | [ADR-0023](../background/decisions/0023-deception-content-injection.md) 未解决 1；**调研与许可已做**（[`../background/research/ai-oss-reuse.md`](../background/research/ai-oss-reuse.md) §3.3），判据在 [ADR-0024](../background/decisions/0024-ai-oss-reuse-boundary.md) —— 本项仍需在阶段 B 单独评估（含出网风险） |
| 2 | PII / 泄露检测（Presidio 类） | `AR-22` 泄露类的覆盖度 | 同上 2；调研已确认 Presidio 为 MIT、仓库已迁至 `data-privacy-stack/presidio`；**别名 `detect-secrets` 的 `protectai/llm-guard` 已归档，禁止引入**（同上 §3.2） |
| 3 | 风格画像的来源（真实站点采样 → 去敏 → 入库） | 内容「像不像」 | 同上 3 |
| 4 | 轮换与识破信号的接线（`strategy` → 清单版本 +1） | 被识破后的自愈 | 同上 4 |
| 5 | 清单纯量上限与分片拉取接口 | 内容规模 | 同上 5 |
| 6 | 核心侧内容库的真实后端（`store.ContentStore` 换 PostgreSQL/Redis） | 重启不丢内容 | 同上 6 |
| 7 | `AR-33` 的措辞仍只覆盖「欺骗内容生成」 | `intent`/`chain`/`strategy` 接模型时的合法性未定；而门禁的 `AR-33` 项**本来就已经是全局的** | [ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md) 决定 4（🟡 提案，**待用户确认**后才能升格进 `design/`） |
| 8 | 产物契约未用跨语言标准（JSON Schema） | 其它语言侧要自己写校验器 | ADR-0025 未解决 3（需第二个消费方到场才定） |
| 9 | 注册表仍是显式两行登记（无自动发现） | 接入新 kind 要改一个文件 | 有意为之；消费方 ≥ 3 个时再评估（ADR-0025 失效条件 3） |

## 9. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-20 | 首版：阶段 A（通路 · 开关 · 强制护栏）—— 唯一出口 `generate` · 任务注册表 · 三段式提示词 · 四关后置校验 · 内容对象与清单 · 确定性模板生成器 | 用户确认新增模块（`AR-33`）+ [ADR-0023](../background/decisions/0023-deception-content-injection.md) |
| 2026-09-20 | §3 增「复用候选」表 · §8 未决项 1/2 补上调研结论与已归档禁用项（本轮**不改代码**） | [`../plans/2026-09-20-ai-oss-reuse.md`](../plans/2026-09-20-ai-oss-reuse.md) · [ADR-0024](../background/decisions/0024-ai-oss-reuse-boundary.md) |
| 2026-09-20 | **出口解耦**：内核任务无关化（不认识「内容」）· 产物出口改为 `Sink` 缝（`aicap/ports.py`）· 受检字段与任务上限**声明化并真的生效** · 新消费方接入只需 §6 三步 | [`../plans/2026-09-20-aicap-decoupling.md`](../plans/2026-09-20-aicap-decoupling.md) · [ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md) |
