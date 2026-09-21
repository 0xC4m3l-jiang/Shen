# AI 能力（模型能力）详解

> **读者**：要评审「这套 AI 能力设计合不合理、哪里要优化」的人。
> **本文是参考层**（`docs/kb/`，不具约束力）—— 规则在 [`../design/`](../design/README.md)，
> 字节契约在 [`../spec/ai-contract.md`](../spec/ai-contract.md)，
> 状态与证据在 [`capabilities.md`](capabilities.md)，模块设计在 [`../modules/ai-capability.md`](../modules/ai-capability.md)。
>
> 写这份文档的原因：**「AI 能力」现在散在三个地方**（`analysis/llm/` 的纪律层 · `analysis/aicap/` 的出口 ·
> `analysis/intent|chain|strategy/` 的消费者），而其中**没有任何一条生产链路真的在调模型**。
> 先把「我们打算让模型做什么、今天用什么代替、边界在哪」完整写出来，再谈优化才有共同语言。
>
> 最后更新：2026-09-20（对着当时的代码逐项核对，不是印象）。
> **状态列是 2026-09-20 的快照**，权威状态在 [`capabilities.md`](capabilities.md)（本文不重复维护它）。

---

## 0. 一页速览

**一句话结论**：**今天没有任何一条生产链路在调模型**。模型只出现在两个「待接」的位置 ——
`ai-capability` 的 `produce`（阶段 B）与 L4 分析（意图 / 攻击链 / 策略，今天全是确定性 Python）。

| # | 能力 | 代码位置 | 今天靠什么产出 | 模型接了吗 | 产物 | 消费者 | 状态 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | **欺骗内容生成** | [`analysis/aicap/`](../../analysis/aicap) | 确定性模板生成器 | ❌ 待接（阶段 B） | 内容对象 + 内容清单 | 核心 `policy` → 适配器 `edge/proxy` | ✅ 阶段 A（通路已通） |
| 2 | **意图识别** | [`analysis/intent/`](../../analysis/intent) | 正则规则表（`rule_hits`） | ❌ **未接** | `Envelope{category, confidence, evidence_ids, rationale}` | `chain` / `strategy` / 遥测结论 | ✅ 确定性版 |
| 3 | **攻击链还原** | [`analysis/chain/`](../../analysis/chain) | 阶段排序 + 证据校验 | ❌ **未接** | `{stages[], broken_decoy_signals[]}` | `strategy` | ✅ 确定性版 |
| 4 | **策略生成** | [`analysis/strategy/`](../../analysis/strategy) | 阈值下界 + 灰度上限 | ❌ **未接** | `{decoy_selection[], gray_pct, threshold_suggestions{}}` | 核心 `policy`（经版本化下发） | ✅ 确定性版 |
| 5 | **双阶段收尾** | [`analysis/llm/twophase.py`](../../analysis/llm/twophase.py) | —— | ❌ **无生产调用方** | 只含事实类字段的收尾结论 | —— | 🟡 骨架（只有单测） |
| 6 | **内容轮换**（识破信号 → 清单版本 +1） | `strategy` 已产出信号，**消费方未接** | —— | ❌ | 清单 `version` 递增 | 生成器 → 核心 | ❌ 未接通 |
| 7 | **动态沙箱规格** | 未立项 | —— | ❌ | 待定 | 动态沙箱（形态待用户裁定） | ❌ 未立项 |

> 第 2–4 项的**提示词已经写好了**（`analysis/llm/resources/prompts/{intent,chain,strategy}.md`），
> 但**只被单测渲染过**，生产代码一行都没用它们 —— 详见 §10 关键发现 1。

---

## 1. 模型在系统里的位置

### 1.1 三条硬约束（先看这个，再看能力）

| 约束 | 规则 | 对「模型能力」的含义 |
| --- | --- | --- |
| **热路径永不调模型** | `AR-30`（响应路径字节一致）· `AR-29`（判定预算 5ms） | 模型只能出现在**离线/近线**；任何「请求时生成」的设计一律不成立 |
| **生成必须经唯一出口并过护栏** | `AR-33` | **任何** LLM 生成 ⇒ 必须走 `ai-capability` 的 `generate()` 并过护栏；别处 import 模型客户端会被 `make archcheck` 拦。口径于 2026-09-20 经用户确认由「欺骗内容生成」**放宽为任何生成**（[ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md) 决定 4）—— 于是 L4 的意图 / 攻击链 / 策略在阶段 3 接模型时**必须**登记成 kind 走这个出口 |
| **模型不得持有执行能力** | `AR-32` · `SB-1` / `SB-2` | 不提供工具调用 / 函数调用 / 网络外呼 / 命令执行；只输出**结构化结论** |

### 1.2 数据流（模型出现的位置用 ★ 标出）

```text
① 欺骗内容（离线，已通路的形状）
   生成器（★ 模型，阶段 B） → 过护栏 → 内容清单（文件） → 核心装载 → 策略面下发 → 适配器改道侧注入

② L4 分析（近线，今天确定性）
   遥测事件 → 去重（AR-14） → 意图（★ 模型？） → 攻击链（★ 模型？） → 策略（★ 模型？）
            → 结论**作为事件**上报（不写存储、不下发指令、不碰响应，AR-32）

③ 动态沙箱（未来）
   沙箱规格（★ 模型，kind 待定） → 产物 → 沙箱消费        ← 形态与层次待用户裁定
```

---

## 2. 能力 1：欺骗内容生成（`kind=content`）

**定位**：产出「资源 × 变体」的**欺骗页面片段**，注入改道侧响应，让对手以为自己在看真实业务。这是**唯一**已经完整接通到边缘的 AI 能力。

### 2.1 输入（`TaskSpec`，契约 §1.1）

| 字段 | 谁给 | 含攻击者可控内容？ |
| --- | --- | --- |
| `kind` | 调用方 | ❌ 固定 `"content"`（未登记即拒绝） |
| `session_id` | 调用方 | ❌ 生成器用 `generate:<profile>:<resource>:<variant>`，不涉及真实会话 |
| `deadline_s` | 调用方 | ❌ |
| `payload.resource` | 调用方（配置里的资源清单） | ❌ 站点自己的路径 |
| `payload.variant` | 调用方（`0..N-1`） | ❌ |
| `payload.version` | 调用方（轮换时递增） | ❌ |
| `payload.profile_id` | 调用方（画像标识） | ❌ |

> **注意**：本能力是**唯一**已经走完护栏的能力，也是**唯一**有 `payload` 全字段表的。
> 其余能力（意图/链/策略）的输入是**遥测事件**，里面**大量攻击者可控内容** —— 那才是 `AR-31` 最需要的地方。

### 2.2 输出契约（`ContentObject`，契约 §2）

`content_id`（确定性）· `resource` · `profile_id` · `variant` · `body`（HTML 片段）· `marker` · `checksum`（SHA-256）· `version` · `generated_at` · `generator`。

### 2.3 提示词（模型看到什么）

[`analysis/aicap/resources/prompts/content.md`](../../analysis/aicap/resources/prompts/content.md)，三段式：
**# 任务**（指令区：生成 HTML 片段 + 允许/禁止清单 + 输出契约）→ **# 画像**（画像术语，去敏）→
**# 不可信数据**（`{{untrusted_data}}`，`AR-31` 的数据区）。

### 2.4 护栏与上限

护栏的**五道关（前置 + 后置四关）与失败语义是任务无关的**，权威定义在
[`../spec/ai-contract.md`](../spec/ai-contract.md) §1.6 —— 本文不复制它。
本能力**填的参数**：

| 项 | 值 |
| --- | --- |
| 受检字段（`checked_fields`） | `("body",)` —— 黑名单与风格只作用在正文上 |
| 长度用途 | `deception_content`（登记在 `llm.limits.PURPOSE_LIMITS`） |
| 任务上限 | `max_output=65536` ⇒ 有效上限 = `min(65536, 65536)` |
| 风格术语 | `PROFILE_VOCAB` 的 8 条「Service …」，正文至少命中一个 |
| 真实业务标识 | 部署方经 `identifiers` 在启动时注入（命中即拒，`AR-24`） |

### 2.5 确定性要求

`AR-30`：**热路径**必须按 `(会话, 资源)` 命中同一内容（原文见 [`../design/architecture.md`](../design/architecture.md)§5）——
它禁止的是**响应路径**上的非确定性，**不要求生成器可复现**。
阶段 A 的生成器**恰好**是确定性的（模板、无随机、无时间、无采样），那是**工程性质**（让通路可回归、清单可复现）；
**接模型后它不成立**（实测 `temperature=0` 也 3 次 3 个结果，见
[`../background/research/ai-live-probe/README.md`](../background/research/ai-live-probe/README.md) §1.2）。
热路径的一致改由三条保证：产物冻结进清单 · `content_id` 由内容体算出 · 会话钉定
（[ADR-0026](../background/decisions/0026-cloud-model-backend.md) 决定 2）。

### 2.6 失败语义

护栏拒绝 ⇒ `Envelope{accepted:false, rejected_reason}`，**不入库、不进清单**；程序性错误（未登记 kind / 缺声明）⇒ 抛异常。

### 2.7 今天怎么验

`make ai-check`（17 项端到端）· `python -m analysis.aicap --out …` · 控制台 DAG 的「内容注入」跳。

---

## 3. 能力 2–4：L4 分析三件套（今天全是确定性）

这三者的**形状**已经是「模型的形状」（结构化结论 + 证据引用 + 置信度 + 置信理由），只是**填内容的是规则**。

### 3.1 意图识别（`intent`）

| 项 | 内容 |
| --- | --- |
| 输入 | 一批 `Observation`（去重后的判定事件） |
| 输出契约 | `{category, confidence, evidence_ids[], rationale}` |
| 今天的实现 | [`analysis/intent/recognize.py`](../../analysis/intent/recognize.py)：`CATEGORIES` 五类 + `rule_hits` 正则规则表 |
| 约束 | `CATEGORIES` 定义了五类。⚠️ **但 schema 并没有守住这个闭集**：`INTENT_SCHEMA` 对 `category` 只查 `(str,)` + 长度 ≤32；越界值会被**静默回落**成 `"reconnaissance"`（`recognize.py:90`）。今天该回落不可达（`_PATTERNS` 只用五类），但**加第六类时会静默误标而不是报错** —— 见 §8.5 优化点 17 |
| 提示词 | 已写好：[`llm/resources/prompts/intent.md`](../../analysis/llm/resources/prompts/intent.md)（**只被单测渲染**） |
| 证据纪律 | `evidence_ids` 必须来自真实存在的 `event_id`，引用不存在即整轮作废（`AR-12`） |

### 3.2 攻击链还原（`chain`）

| 项 | 内容 |
| --- | --- |
| 输入 | 一批 `Observation` + 证据索引（`EvidenceIndex`） |
| 输出契约 | `{stages[{name, evidence_ids[], confidence}], broken_decoy_signals[]}` —— 实现里**没有** `conclusion` 字段（只有提示词模板里写了它，而那份模板未接通） |
| 今天的实现 | [`analysis/chain/reconstruct.py`](../../analysis/chain/reconstruct.py)：按 `STAGE_ORDER` 排序 + `assert_exists` 逐条校验证据 |
| 识破信号 | 四类：`skipped` / `hit_without_followup` / `explicit_compare` / `multi_session_same_method`（[`chain/broken.py`](../../analysis/chain/broken.py)） |
| 提示词 | 已写好：[`chain.md`](../../analysis/llm/resources/prompts/chain.md)（**只被单测渲染**） |

### 3.3 策略生成（`strategy`）

| 项 | 内容 |
| --- | --- |
| 输入 | `StrategyInput`（意图类别 / 链阶段 / 识破信号 / 可用诱饵 / 当前灰度） |
| 输出契约 | `{decoy_selection[], gray_pct, threshold_suggestions{route_mirage, block}}` |
| 今天的实现 | [`analysis/strategy/generate.py`](../../analysis/strategy/generate.py)：阈值**下界** `route_mirage≥0.3` / `block≥0.6`、灰度上限 20%（`INT-11`） |
| **不做什么** | 不做判定（`AR-2`）、不做决策（`MD-12`）、不执行（`SB-1`）、不直接回写响应（`AR-32`）—— 只出**数据** |
| 提示词 | 已写好：[`strategy.md`](../../analysis/llm/resources/prompts/strategy.md)（**只被单测渲染**） |
| 今天常被拒 | `没有可用诱饵：不生成无依据的策略` —— 演示栈里诱饵面 0 个资产启用（`make dev` 里 `strategy: accepted=False` 就是这个原因，不是 bug） |

---

## 4. 能力 5：双阶段收尾（模型超时后把结果救回来）

| 项 | 内容 |
| --- | --- |
| 问题 | 模型跑超时了，整轮结果丢掉太浪费；重跑一遍完整任务代价更大 |
| 机制 | 阶段 1（超时 T1）→ 阶段 2 **复用同一会话**（超时 T2），只允许输出**事实类字段** |
| 规则 | `AR-19`（禁止丢整轮）· `AR-20`（收尾契约**必须不同**，只允许事实字段）· `AR-21`（两阶段皆失败 ⇒ 不写中间数据 + 释放租约） |
| 今天的实现 | [`analysis/llm/twophase.py`](../../analysis/llm/twophase.py) 的 `run_two_phase()`；收尾提示词 [`finalize.md`](../../analysis/llm/resources/prompts/finalize.md)（明确禁止「已完成/成功/达成」类字段） |
| 状态 | 🟡 **骨架**：只有单测调用它；**没有任何生产调用方**（`grep run_two_phase` 只有测试） |

---

## 5. 能力 6–7：未接通 / 未立项

| 能力 | 现状 | 缺什么 |
| --- | --- | --- |
| **内容轮换** | `chain` 已产出识破信号（四类），`strategy` 已把它转成阈值建议 | **没有消费方把清单 `version` +1** —— 即「被识破之后内容不会自己换」 |
| **动态沙箱规格** | 未立项。ADR-0023 只写了一句「为阶段 C 的第二个消费方（动态沙箱生成规格）留了同一出口」 | 形态 / 层次 / 语言待裁定；出口侧已解耦（[ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md)），接进来不需改内核 |

---

## 6. 模型后端必须满足什么（硬约束）

要接一个模型，它**必须**同时满足下面全部；任何一条不满足就不是「暂不支持」，而是**架构冲突**。

| # | 要求 | 为什么 | 今天的判据 |
| --- | --- | --- | --- |
| 1 | **输出结构化 JSON**，可被**独立代码层**校验 | `AR-15`：提示词不承担校验责任 | `guardrail/inspect.py` 的第 1 关；失败即拒绝 |
| 2 | **不提供任何执行面**（无工具调用 / 函数调用 / 外呼 / 命令执行） | `AR-32` / `SB-1` / `SB-2` | `assert_no_execution_surface()`（**子串**匹配 `tool`/`exec`/`request`/`http`… —— `execute()` 这类命名会漏，故用子串） |
| 3 | **不得出网**（本地/自托管），除非另开 ADR | 观测数据外发的合规风险 | 阶段 A 无模型；阶段 B 引入前须过许可台账（`TB-16`） |
| 4 | 生成链路**可复现**（同输入同字节）或**有固定种子** | 阶段 A 的工程性质（**不是** `AR-30` 的要求：`AR-30` 只管**响应路径**） | 阶段 A 靠模板；接模型后不可复现（已实测），热路径改由「产物冻结 + `content_id` 由内容体算出 + 会话钉定」保证（ADR-0026 决定 2） |
| 5 | **不依赖系统时钟做判定** | `MD-6` 的精神 | `generated_at` 只作审计，不进任何判定 |
| 6 | 能**复用同一会话**做阶段 2 | `AR-19` | `Session` Protocol 的 `session_id` + `run()` |
| 7 | 未配置时**显式失败**，不得降级成模板 | `AR-15` | `UnconfiguredClient.complete()` 抛 `Unavailable` |
| 8 | 长度可控 / 可截断（并**记录**截断） | `AR-18` / `AR-23` | `llm.limits` 的分用途上限 + `Truncation` 记录 |

**模型的接口只有一件事**：

```python
class AnalysisClient(Protocol):
    def complete(self, prompt: str, *, session_id: str, timeout: float) -> str: ...
```

「把提示词变成文本」—— 没有别的。`analysis/aicap/model.py` 是唯一的接缝：
`resolve(client)` 会核对**无执行面**，没配就返回 `UnconfiguredClient`。

---

## 7. 模型接缝：怎么接、接在哪

| # | 步骤 | 位置 |
| --- | --- | --- |
| 1 | 实现 `AnalysisClient`（或适配现有推理后端） | 能力**之外**（部署侧注入）；能力内只有 `aicap/model.py` 是接缝 |
| 2 | 把一个任务的 `produce` 换成「渲染好的提示词 → 模型 → 候选输出」 | `analysis/aicap/tasks/<任务>.py` |
| 3 | 其余一行不改 | 前置护栏、后置四关、`Sink`、`Envelope` 全在 `service.run_task()` 内 |

**为什么只换 `produce`**：提示词由内核渲染并校验（三段式 + 数据区），产物由 `build` 建，出口由 `Sink` 拿 ——
所以「换成模型」不会动到任何护栏。**这一点有单测证明**（假 kind 走完整内核，见
[`../plans/2026-09-20-aicap-decoupling.md`](../plans/2026-09-20-aicap-decoupling.md) §6.2）。

**门禁**：`analysis/` 下只有 `aicap/service.py` · `aicap/model.py` · `llm/` 自身与测试可以 import 模型客户端，其余一律拦（`AR-33` 项）。

---

## 8. 可优化点候选（**我列出的，不是决定** —— 请评审）

分组列出，每条给出「现状 → 问题 → 候选做法 → 代价」。标 ★ 的是我认为**优先级最高**的。

### 8.1 能力覆盖（缺什么）

| # | 现状 | 问题 | 候选做法 | 代价 |
| --- | --- | --- | --- | --- |
| 1 | 意图/链/策略全是规则，提示词写了但没接模型 | 规则覆盖不了新变体（调研引用的数据：规则型 WAF 对新变体检测率 < 12%） | 阶段 3 接模型 —— **出口问题已定**：`AR-33` 已放宽为「任何生成」（2026-09-20 用户确认），所以它们**必须**登记成 kind 走 `aicap`，不能自己在 `llm/` 里接（见 §10 发现 1/5） | 模型成本 + 确定性论证（§6 第 4 条） |
| 2 | 内容轮换没接通 | 内容**被识破后不会换**（长期可指纹） | 让 `strategy` 的识破信号 → 清单 `version` +1（闭环最后一跳） | 需要定「谁触发、什么时候触发、要不要人工确认」 |
| 3 | 动态沙箱规格未立项 | 第二个消费方落不了地 | 先定形态（模块 15 `honeypot-shell`？Rust 安全底座？L4 新模块？） | 只有你能定 |

### 8.2 质量与多样性

| # | 现状 | 问题 | 候选做法 | 代价 |
| --- | --- | --- | --- | --- |
| 4 ★ | `kind=content` 是模板生成器，N=8 个固定变体 | 「像不像」未知；变体数与措辞规律本身是**指纹面** | 阶段 B 接模型 + [`Faker`](https://github.com/joke2k/faker)（MIT，有 `seed()` ⇒ 可保住**生成期**可复现；那是阶段 A 的工程性质，不是 `AR-30` 的要求），详见 [`../background/research/ai-oss-reuse.md`](../background/research/ai-oss-reuse.md) §3.3 | 引入依赖（须先过许可台账，已补） |
| 5 | 变体只由 `(resource, 会话哈希)` 决定，**没有时间维度** | 同一变体可能被长期复用 | 引入「按时间窗的确定性轮换」（需保住热路径的会话钉定，而非生成期可复现） | ADR-0023 失效条件 1 |
| 6 | 风格一致性**只查术语命中** | 命中术语 ≠ 像那个文档画像 | 换 embedding 相似度 / 或引入「像不像」的独立评分器 | 需要一个**离线**的相似度模型 + 阈值标定 |
| 7 | 没有**质量抽样评估** | 没人知道生成内容到底像不像 | 建一个小的人工/自动评审集（抽 N 条打分） | 需要评测口径（技能 `evidence-and-decisions` §3） |

### 8.3 安全与护栏

| # | 现状 | 问题 | 候选做法 | 代价 |
| --- | --- | --- | --- | --- |
| 8 ★ | 黑名单只有正则 + 启动期注入的真实标识 | 认不出「人名 / 电话 / 邮箱」这类 PII，也认不出没登记过的内网命名 | 引入 [Presidio](https://github.com/data-privacy-stack/presidio)（MIT）的识别器，**处置语义留在本地**（ADR-0024 §3.2） | 模型文件体积；须离线 |
| 9 | 注入防护靠**结构**（数据区 + 标记 + 长度自检），没有**分类器** | 结构与分类不可互换（前者是断言、后者是概率） | **不建议**换成分类器；可选：加一层「可疑信号」**旁路**观测（不参与拒绝） | 若加，必须明确它**不**决定放行/拒绝 |
| 10 | 注入样本回归集还没建（ADR-0015 未解决 2） | 防注入能力没有回归保障 | 建一组「观测里含指令」的样本，断言结论不变 | 需要真实样本或合成样本 |

### 8.4 工程与运维

| # | 现状 | 问题 | 候选做法 | 代价 |
| --- | --- | --- | --- | --- |
| 11 ★ | **今天没有任何生产链路在调模型**，但纪律层（`llm/`）已经写完了 | 纪律层是「按有模型时设计的」，接模型那天才会第一次被真正检验 | 阶段 B 立项时**先跑一轮小规模真实生成**，把 `llm/` 的每一条纪律都用真实输出验一遍 | 需要先有可用的本地模型 |
| 12 | 没有成本 / 配额控制 | 内容量 × 变体数 × 模型调用量没有上限概念 | 生成器加「本次最多调用 N 次 / 最多产出 M 条」并在清单里记 | 需要定口径（按条还是按 token） |
| 13 | 没有模型不可用时的降级策略（当前**显式失败**） | 离线批量任务的一次模型抖动会让整批作废 | 讨论：离线任务是否允许「部分成功 + 断点续跑」；**热路径仍然禁止降级** | 与 `AR-15`（禁止默认值）的边界需要写清 |
| 14 | 双阶段收尾的 T1/T2 没有观测指标 | 不知道收尾救回了多少结果 | 加两项指标（触发次数 / 救回率） | 需要 `metrics.md` 的口径 |

### 8.5 契约与可验证

| # | 现状 | 问题 | 候选做法 | 代价 |
| --- | --- | --- | --- | --- |
| 15 | schema 是自研的 74 行迷你校验器 | 跨语言消费方（Rust 沙箱）要自己再写一份 | 评估换成 [JSON Schema](https://json-schema.org/)（标准、跨语言） | 换掉 `llm/contract.py` 的语义（要保 fail-closed），需 ADR |
| 16 | 提示词**没有版本标识** | 换了模板后，无法回答「这份内容是哪版提示词产出的」 | 把模板内容哈希写进产物（与 `generator` 并列） | 契约加一个字段 |
| 17 | `INTENT_SCHEMA` 无法表达**枚举闭集**，靠代码分支兜底 | 越界的 `category` 被**静默回落**成 `"reconnaissance"`（`recognize.py:90`）—— 今天不可达，但违背 `AR-15`（禁止默认值）的**精神**；一旦新增第六类就会被静默误标 | （a）删回落分支，越界即 `reject`；（b）给 `llm.contract.Schema` 加「枚举」字段类型，让闭集进契约 | 这属**代码改动**，本轮未动（纯文档轮）；建议单开一个小轮（S 档） |

---

## 9. 今天怎么验（命令与看什么）

| 想确认什么 | 命令 | 看什么 |
| --- | --- | --- |
| 护栏真的关着 | `analysis/.venv/bin/pytest` | 79 例全绿（含假 kind 走内核、四关各自能拒） |
| 生成 → 护栏 → 清单 | `python -m analysis.aicap --out /tmp/m.json --resources /,/api --variants 8 --version 1` | 汇总行：生成 N 条 / 拒绝 M 条 / 清单字节数 |
| 端到端（含字节一致） | `make ai-check` | 17 项全绿；基线 sha256 逐个相等 |
| 出口没被绕过 + 能力独立 | `make archcheck` | `AR-33` 项（模型客户端唯一出口）· `MD-4` 项（依赖白名单） |
| 攻击者内容只在数据区 | `pytest -k ar31` | 观测里含指令时结论不变 |
| L4 分析链路 | `make dev` | 第 6 段输出：取事件 / 去重 / 结论条数 |
| 结论不回显给对手 | `make leakcheck` | 响应面字符串 ↔ 禁用清单 |

---

## 10. 关键发现（这轮读代码发现的，可能是问题）

> ⚠️ **本节是 2026-09-20 读代码时的快照**，不是持续维护的列表。其中：
> **发现 1 / 5 里关于「`AR-33` 文字口径」的那一半已经解决**（口径已于 2026-09-20 经用户确认放宽为「任何 LLM 生成」，见 §1.1 与 §8.1 优化点 1）；
> 但它们的另一半 **仍未解决** —— 「两套提示词系统没合并」与「四个模型任务没登记成 kind」。

| # | 发现 | 证据 | 为什么值得你看一眼 |
| --- | --- | --- | --- |
| 1 ★ | **两套提示词系统**：`analysis/llm/resources/prompts/`（intent / chain / strategy / finalize）与 `analysis/aicap/resources/prompts/content.md` | `grep -rn "prompts.render\|prompts.load\|assert_startup" analysis/`（排除 `tests/`）→ 生产里只有 `aicap/service.py:135` 调 `guardrail_prompts.render`，以及 `aicap/guardrail/prompts.py` / `aicap/tasks/_registry.py` 的 `assert_startup`；**`llm/` 那四个模板在生产里一次都没被 `render` 过**（只在 `test_llm_discipline.py` 里） | 前四个模板是「按有模型时写好的」，但既没有生产调用方，**也不在 `aicap` 注册表里** ⇒ 它们不受 `AR-33` 出口约束。接模型时要先决定：这四条走 `aicap` 的 kind 登记，还是留在 `llm/` 里另开一条路径 |
| 2 | `llm.prompts.assert_startup` **不在生产启动路径上** | `grep` 只有 `test_llm_discipline.py` 调用；生产启动只校验 `aicap` 的模板目录 | `AR-24` 说「启动期必须校验模板存在与占位符齐全」—— 对那四个模板，今天只有**测试**在守 |
| 3 | `llm.twophase.run_two_phase` **没有生产调用方** | `grep -rn run_two_phase` → 只有 `twophase.py` 自己与 `test_llm_discipline.py` | 双阶段收尾是**骨架**：规则写完了、测试过了，但没有任何真实链路用它 |
| 4 | `strategy` 在演示栈里**经常被拒** | `make dev` 输出 `strategy: accepted=False data={}`；`generate()` 返回 `没有可用诱饵`。**准确成因**：可用诱饵从**事件里的 `backend` 字段**取（`worker.py` 的 `_decoys()`），演示栈里没有幻境后端 ⇒ 事件不带 `backend` ⇒ 诱饵列表为空 | 这是**设计内的 fail-closed**（不生成无依据的策略），不是 bug。但它意味着「策略生成」在幻境后端接通前**基本不产出** |
| 5 | L4 的四个模型任务（intent/chain/strategy/finalize）**不在 `aicap` 的 kind 注册表里** | `analysis/aicap/tasks/_registry.py` 只 `return {CONTENT_TASK.kind: CONTENT_TASK}`（只登记 `content`） | 与发现 1 是同一件事的两面：`AR-33` 的**文字**只覆盖欺骗内容，而**门禁**是全局的 ⇒ 接模型时会撞上一个「文字没写、门禁会拦」的缝 |
| 6 | `intent` 的越界类别被**静默回落**成 `reconnaissance`，且 schema 守不住闭集 | `analysis/intent/recognize.py`：`INTENT_SCHEMA` 只有 `Field("category", (str,), True, 32)`；写入前是 `category if category in CATEGORIES else "reconnaissance"` | 与 `AR-15`（禁止默认值）的精神相左。今天不可达（`_PATTERNS` 只用五类），但**加第六类的那天会静默误标**。见 §8.5 优化点 17 |

---

## 11. 术语与边界速查

| 词 | 含义 | 不要误解成 |
| --- | --- | --- |
| **唯一出口** | `aicap.service.generate()` —— 所有生成必须经它并过护栏 | 不是「所有 AI 功能都写在这个文件里」 |
| **护栏** | 前置（提示词三段式）+ 后置四关（结构 / 黑名单 / 长度 / 风格） | 不是「模型自己会守规矩」 |
| **受检字段** | 任务声明的、后置第 2–4 关作用的输出字段（`checked_fields`） | 不是「所有输出字段都查」 |
| **数据区** | 提示词里放**攻击者可控内容**的那一段（`AR-31`） | 不是「净化过的输入」—— **禁止**为净化而丢弃原始观测 |
| **无执行面** | 分析客户端不得暴露工具 / 函数调用 / 外呼 / 执行 | 不是「禁止模型说话」—— 它只输出结构化结论 |
| **确定性**（生成期） | 同输入同字节输出 —— **阶段 A 的工程性质，不是 `AR-30` 的要求** | 不是「模型温度=0」就够 —— 实测模型 3 次 3 个结果（详见 [`ai-live-probe/README.md`](ai-live-probe/README.md)） |
| **确定性**（响应路径） | 同会话同资源恒同答案（`AR-30`） | 不靠生成期可复现：靠**产物冻结 + `content_id` 由内容体算出 + 会话钉定** |

---

## 12. 模块的生命周期

> **状态表**（什么状态存在哪里 / 活多久 / 多副本一致性）的权威在
> [`../modules/ai-capability.md`](../modules/ai-capability.md) §5 —— **本节不重复那张表**，只讲**运行期时序**。

### 12.1 一句话

它**不是常驻服务**，而是两种东西：一个**可被反复调用的库** + 一个**一次性的离线批次任务**。
因此它没有「在线生命周期」，只有「批次生命周期」与「被装载后的驻留期」。

### 12.2 全生命周期时间轴

```text
[0] **生成侧**进程启动（生成器；未来的第二个消费方同理）
     └─ **必须**调 service.startup_assert()      ← 不合格就起不来（fail-closed，AR-33 / AR-24）
     └─ ⚠️ 只有**生成侧**调它：核心与适配器是 Go、只读产物字节，**永远不调**（§13.1）
[1] 批次开始（离线任务）
     └─ argparse 先解析入参，随后**校验**：--out / --kind / --profile / --resources
        / --variants / --version / --now / --identifiers / --quiet
     └─ 建一个内存内容库（它就是 Sink）
[2] 逐条生成  （资源 × 变体，两层循环）
     └─ generate(TaskSpec) = 取任务 → 前置护栏 → produce → 后置四关 → build → sink.put
     └─ **护栏拒绝**：不入库、不进清单（只记日志与汇总计数）—— **批次继续**
     └─ ⚠️ **异常不按条捕获**：produce 抛错（如模型未配置的 Unavailable）⇒ **整批中断**
        （`__main__.py` 只在启动断言那一处有 try；阶段 A 的模板生成器不会触达）
[3] 批次结束
     └─ 一条都没过护栏 ⇒ 退出码 1，**不写清单**（清单不值得写）
     └─ 否则 build_manifest → write_manifest（**确定性字节**，同输入同字节）
[4] 生成器退出 —— 进程结束即结束，没有常驻状态
──────────────── 生成期结束，以下是消费期 ────────────────
[5] 核心启动
     └─ 读 ai.manifest → LoadContentManifest：
        · **结构性**问题（路径空 / 读不了 / 解析失败 / variant 重复 / 内容体为空 /
          variants 与配置不一致）⇒ **启动失败**
        · **单条**坏（内容体 > 64 KiB、校验和不符）⇒ **丢该条 + warn**（不整份作废）
     └─ SeedContentStore → store.ContentStore（内容驻留到进程结束）
[6] 适配器拉策略（Pull）
     └─ 核心在**每次 Pull** 时把 `inject_enabled` + `content_manifest` 投影进载荷
        （**不是**启动时定死）
     └─ 适配器索引 content_manifest
[7] 运行期（每个走到改道侧的请求）
     └─ 三个条件**取与**：本地兜底开关 · 下发级开关 · **有可索引的清单**；缺一 ⇒ 原样返回 + 上报 `disabled`
     └─ 条件成立后：**先**按会话哈希选槽位（会话钉定，TTL = 判定缓存窗）→ **再**验校验和 → 注入
     └─ 命中不了资源 / 无可用内容 ⇒ 原样返回 + 上报 `no_content`（四个取值见 §13.4 第 7 项）
[8] 轮换（阶段 B 未接通）
     └─ 重新生成 version+1 的清单 → 核心重启装载 → 新会话拿新内容，老会话沿用钉定槽位
[9] 关闭
     └─ 生成器：进程退出即结束
     └─ 核心：内容在内存，**重启需重新装载**（真实存储未接）
     └─ 适配器：钉定缓存是**可丢失缓存**（有 TTL 与容量上限，丢了只是换一个变体）
```

### 12.3 三个阶段的「寿命」

| 阶段 | 寿命 |
| --- | --- |
| **生成**（离线批次） | 秒~分钟 |
| **装载**（核心） | 进程寿命（重启需重装） |
| **注入**（适配器） | 请求本身 + 钉定 TTL |

> 状态在哪、多副本一致性、以及每个字段的完整行 —— 权威在
> [`../modules/ai-capability.md`](../modules/ai-capability.md) §5（本节不复制它）。

### 12.4 时间轴上的失败分支

> **失败模式的权威表**在 [`../modules/ai-capability.md`](../modules/ai-capability.md) §6。
> 本节只回答「**哪一个节点**失败、往下怎么走」——两者的视角不同，内容不重复。

| 时间轴节点 | 失败时 | 业务受影响？ |
| --- | --- | --- |
| `[0]` `startup_assert()` 不过 | 进程**起不来**（好过带病生成） | ❌ |
| `[2]` `produce` 抛错（模型不可用） | **整批中断**（**不是**「该条作废」） | ❌ |
| `[2]` 后置护栏拒绝 | 该条不入库、不进清单；**批次继续** | ❌ |
| `[3]` 一批全被拒 | 退出码 1，不写清单 | ❌ |
| `[5]` 清单**结构性**坏 / `variants` 不一致 | **核心启动失败** | ❌（核心起不来 ≠ 业务坏；但核心是判定面，要当事故看） |
| `[5]` 清单**单条**坏 | 丢该条 + warn | ❌ |
| `[7]` 三个条件缺一 / 无可用内容 | 原样返回（`disabled` / `no_content`） | ❌ |
| `[7]` 注入本身出错 | 不注入、原样返回 | ❌ |

> 这张表的最后一列全是 ❌ —— 这就是 `NI-1`（不影响原始业务）在本模块的具体形态。

### 12.5 三层开关：怎么「秒级关掉」

| 层 | 键 | 关闭后 | 生效时机 |
| --- | --- | --- | --- |
| **能力级** | `ai.enabled` + `ai.kinds` | 不生成、不新增内容（存量仍在库）。⚠️ 阶段 A 里 `ai.kinds` **只被解析与校验**，还没有消费方（[`../spec/config.md`](../spec/config.md) §2.13） | **重启**核心 |
| **下发级** | `inject_enabled`（策略载荷） | 适配器停止注入（`inject=disabled`） | 下一次 `Pull` —— **秒级关闭只在这一层成立** |
| **兜底级** | `SHEN_PROXY_INJECT_CONTENT`（默认 `false`） | 同上一行（策略面不可达时的本地急停） | 适配器重启 |

> **没有热重载路径**（[`../spec/config.md`](../spec/config.md) §1：不支持 SIGHUP / 文件监听）——
> 所以上表里的「重启 / 重载」实际上只有「**重启**」一条路。
> **关闭 ≠ 回退改道**：只影响「我们改写多少」，幻境后端与判定链完全不变。这是默认全关也能安心上线的理由。

---

## 13. 怎么接入被使用

### 13.1 三种角色（谁做什么、谁不要做什么）

| 角色 | 是谁 | 必须做 | **不要**做 |
| --- | --- | --- | --- |
| **生成侧调用方** | 离线任务 · 未来第二个消费方 | 启动时 `startup_assert()`；给 `TaskSpec`；给一个 `Sink` | 不要跳过护栏、不要自己 import 模型客户端（门禁会拦，`AR-33`） |
| **下游消费方** | 核心 `policy` · 适配器 `edge/proxy` · 未来的其它语言侧 | 只读产物字节、**独立校验**；**不调** `startup_assert()`（那是生成侧的事） | 不要重复实现护栏（`AR-33`）、不要改写业务侧（`INT-8`） |
| **部署方** | 运维 / 交付 | 配三层开关、注入真实业务标识、装载清单 | 不要把清单当成可热改的运行时配置（核心只在启动时装载） |

### 13.2 生成侧：两种用法

**① 作为库**（未来的第二个消费方走这条）：

```python
from analysis.aicap import service

service.startup_assert()                        # 启动期：不合格就别继续
envelope = service.generate(                    # 唯一出口（AR-33）
    service.TaskSpec(kind="content", session_id="generate:...", deadline_s=30.0, payload={...}),
    sink=my_sink,                               # 产物去哪由**你**决定（可以不传）
    identifiers=("AcmeCorp",),                 # 部署方注入的真实标识（AR-22）
)
```

**② 作为离线任务**（`python -m analysis.aicap`；CLI 本身任务无关，`--kind` 选已登记的 kind —— 今天只有 `content`）：

```sh
python -m analysis.aicap --out deploy/content/manifest.json --kind content \
    --profile site-a --resources /,/api/users --variants 8 --version 1 \
    --identifiers "AcmeCorp,acme-internal"
```

退出码：`0` = 至少产出一条并写出清单 · `1` = **一条都没过护栏**（不写清单）·
`2` = **入参非法或启动期断言失败**（根本没开始生成）。
其余可调参数：`--now`（生成时刻，只作审计）· `--quiet`（只打印汇总行）。

**③ 接入一个新 `kind`**：三步（登记 + 产物出口），写在哪见
[`../spec/ai-contract.md`](../spec/ai-contract.md) §6 —— 本节不重复（`MD-3`）。

### 13.3 消费侧：核心 + 适配器

```yaml
# 核心配置（deploy/config/config.example.yaml 是模板；真实配置由 SHEN_CONFIG 指向、不入库）
# 键名与默认值的权威表：docs/spec/config.md §2.13
ai:
  enabled: true              # 能力级总开关（默认 false）
  kinds: ["content"]
  manifest: /etc/shen/content/manifest.json   # 生成器产出的清单
  content:
    variants: 8              # **必须**与清单里的 variants 一致，不一致 ⇒ 拒绝装载
```

```sh
# 适配器：本地兜底开关（默认 false）
SHEN_PROXY_INJECT_CONTENT=true
```

三条必须知道的行为：

1. **核心只在启动时装载清单** —— 换了清单要**重启核心**（适配器不用重启）；
2. **清单的严格度分两级**（`LoadContentManifest`）：**结构性**问题（读不了 / 解析失败 / variant 重复 /
   内容体为空 / `variants` 与配置不一致）⇒ **启动失败**；**单条**坏（内容体 > 64 KiB、校验和不符）⇒
   **丢该条 + warn**（宁可漏注入，不可注入错内容）；
3. **清单文件必须能被核心读到** —— 容器部署时要把**清单**也挂进核心容器
   （配置模板只挂了配置本身；`ai.manifest` 指向的路径在容器内要存在）。

### 13.4 接线核对清单

| # | 做什么 | 看什么 |
| --- | --- | --- |
| 1 | 跑生成器 | 退出码 0，汇总行给出「生成 N 条 / 拒绝 M 条 / bytes」 |
| 2 | 核对变体数 | 清单的 `variants` == 配置的 `ai.content.variants` |
| 3 | 打开能力开关 | `ai.enabled: true`（默认 `false`） |
| 4 | 指向清单 | `ai.manifest: <路径>`（容器部署：该文件**必须也挂进核心容器**；见 §13.3 第 3 条） |
| 5 | 重启核心看装载日志 | `AI 内容已装载：…（内容版本 v… · 变体 … · 资源 … · 内容 … 条）` |
| 6 | 适配器兜底开关 | `SHEN_PROXY_INJECT_CONTENT=true` |
| 7 | 走一次改道侧请求 | 逐请求事件 `inject=applied` 且带 `content_id`（控制台 DAG 可见「内容注入」跳）。`inject` 共**四个**取值：`applied`（真注入了）· `disabled`（开关关）· `no_content`（开关开但无可用内容）· `off`（压根没走到改道侧） |
| 8 | 确认业务侧**逐字节未变** | 业务侧响应的 sha256 与直连一致（`INT-8`） |

**`make ai-check` 覆盖其中的 1 / 3 / 4 / 6 / 7 / 8**（含业务侧字节一致与 DAG 注入跳）；
**第 2 项与第 5 项要人工看一眼** —— 脚本里没有这两条断言（`variants` 一致、装载日志）。

### 13.5 六个常见误解

| 误解 | 事实 |
| --- | --- |
| 「打开 `ai.enabled` 就会注入」 | 不够：还要**下发级**生效（下一次 `Pull`）+ 适配器**兜底**开关 |
| 「改了清单文件，核心就会换内容」 | 不会 —— 核心**只在启动时**装载，要重启 |
| 「关掉注入 = 回退改道」 | 不会 —— 只影响「我们改写多少」，幻境与判定链不变 |
| 「离线生成挂了会影响线上业务」 | 不会（`NI-1`）；最坏结果是「没有内容可注入」，响应原样返回 |
| 「变体是每次请求随机选的」 | 不是 —— 按**会话哈希**选后再**会话钉定**（同会话同资源同答案，`AR-30`） |
| 「沙箱 / 适配器侧自己生成就行」 | 不行 —— 那会绕开护栏（`AR-33`），且门禁的结构检查就是拦这件事的 |

---

## 14. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-20 | 首版：7 个能力逐个详解（输入 / 输出 / 提示词 / 护栏 / 确定性 / 失败语义 / 怎么验）· 模型后端 8 条硬约束 · 接缝 · 16 条优化点候选 · 6 条关键发现 | 用户要求「把对应模型能力介绍写详细，后面再分析是否合理」；对着代码逐项核对 |
| 2026-09-20 | 新增 §12 模块生命周期（全生命周期时间轴 · 三阶段寿命 · 逐步失败语义 · 三层开关）与 §13 接入与使用（三种角色 · 两种用法 · 消费侧配置 · 8 项接线清单 · 六个常见误解） | 用户要求「说清这个模块的生命周期是什么、怎么接入被使用」 |
