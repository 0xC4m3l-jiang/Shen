# AI 能力层（`analysis/llm` + `analysis/aicap`）的开源复用审查

> **本文件是证据材料，按 `AGENTS.md` 的分层不具约束力。** 结论里经用户确认的部分按 §5 的规矩升格
> （本轮的选型结论已升格为 [ADR-0024](../decisions/0024-ai-oss-reuse-boundary.md)）。
> 调研时间：2026-09-20 · 方法：技能 [`evidence-and-decisions`](../../../.pi/skills/evidence-and-decisions/SKILL.md) §1。

---

## 0. 要回答的问题

**一句话**：`analysis/llm/`（9 个模块）与 `analysis/aicap/`（9 个模块）里手写的能力，
有哪些已经有成熟开源实现可以直接复用，从而**减少不必要的自研**？

**什么发现会推翻它**：

- 若某项手写能力在开源侧**没有**对等实现 → 该结论从「可复用」变为「空白，自研正确」；
- 若开源实现**默认行为与已确认规则冲突**（`AR-15` 禁止修复 / `AR-21` 禁止写半成品 / `AR-30` 禁止非确定性）
  → 该结论从「可复用」变为「不可直接复用，除非有可证的对齐模式」；
- 若候选**许可**不可接受或**项目已归档** → 直接出局，无论功能多贴合。

**范围边界**（本轮不审）：`intent/` `chain/` `strategy/` `worker/`（AI 能力的**消费者**）、
`core/internal/policy/ai.go` 与 `edge/injection/`（跨语言侧的装载与改写）—— 见 §6 局限。

---

## 1. 结论先行（每条附证据级）

| # | 结论 | 证据级 |
| --- | --- | --- |
| 1 | **手写量比预想的小，但「纯自研」这个事实成立**：两个包共 18 个模块，除 `PyYAML` 外**零第三方依赖**（逐文件 import 清单实测）。真正与开源存在**功能重叠**的只有 6 处，其余 12 处是契约/纪律层，开源侧没有对等物。 | **A** |
| 2 | **最值得复用的一处是 JSON 容错提取**：`json_repair`（MIT · 5.1k★ · 2026-09-17 有提交）提供 `strict=True` —— 遇结构问题**抛 `ValueError` 而不是修复**，正好落在 `AR-15` / `AR-21` 要求的 fail-closed 语义上。这是本轮唯一「默认行为就能对齐」的候选。 | 许可 **A**（LICENSE 逐字核）；行为 **B**（维护者文档，未实测） |
| 3 | **护栏框架（guardrails-ai / NeMo Guardrails）不建议引入**：两者都是「框架 + Hub + 服务」形态（含 Flask 服务端、远程 validator 拉取），而本模块要的是「一个函数 + 启动期断言 + 门禁结构检查」。`guardrails-ai` 的 `fix` / `reask` 动作与 `AR-15`（禁止修复后放行）直接冲突。 | 形态与动作 **B**（仓库 README）；许可 **A** |
| 4 | **`protectai/llm-guard` 已归档**（API `archived=true`，最后推送 2026-07-08），**禁止引入** —— 它是最常被推荐的「LLM 输入输出扫描器」，若按二手文章选型会踩中。同类已归档的还有 `Azure/PyRIT`。 | **A**（GitHub API `archived` 字段） |
| 5 | **发现一处与复用无关但更严重的缺口**：`make licensecheck` 的台账**只审 Go 模块**（`go list -deps` 反推），`analysis/requirements.txt` 的运行期依赖**没有许可审计面** —— 而 `TB-16` 要求「依赖必须经许可与漏洞审计」。在引入任何 Python 依赖之前必须先补上这一面。 | **A**（`scripts/licensecheck/main.go` 源码 + 台账内容） |
| 6 | **顺带发现环境漂移**：`analysis/requirements.txt` 锁 `protobuf==7.36.2`，而 `analysis/.venv` 装的是 `7.35.1`（`pip freeze` 实测）—— 锁文件与实际运行环境不一致，`make pyenv` 未在锁变更后重跑。 | **A**（`pip freeze` + 锁文件比对） |

---

## 2. 检索记录

| # | 源 | 检索式 / 取数方式 | 时间 | 命中 / 纳入 |
| --- | --- | --- | --- | --- |
| R1 | GitHub API `repos/{owner}/{repo}` | 15 个候选仓库的 `license.spdx_id` / `stargazers_count` / `pushed_at` / `archived` | 2026-09-20 | 15 / 15 |
| R2 | `raw.githubusercontent.com` 直取 `LICENSE` | 逐仓库取许可正文首行（**证据级 A 的来源**） | 2026-09-20 | 15 / 15 |
| R3 | 仓库 README（`raw.githubusercontent.com`） | `json_repair`（行为、`strict` 模式、schema 导向）、`guardrails-ai`（`OnFailAction`、Hub、服务端）、`Faker`（`seed()`） | 2026-09-20 | 3 / 3 |
| R4 | 维护者文档 | `pydantic` 的 `docs/concepts/strict_mode.md`（默认 lax 强制转换 vs `strict=True`） | 2026-09-20 | 1 / 1 |
| R5 | 搜索引擎（Exa / 综合） | 「guardrails-ai vs NeMo Guardrails vs LLM Guard」「json_repair license」「Presidio PII 离线」「Faker vs Mimesis 性能」 | 2026-09-20 | 二手对比文章 5+ 篇，**仅作线索**（证据级 C，未写入结论） |
| R6 | PyPI JSON API | `protobuf` 版本存在性（确认锁文件可解，漂移是环境问题而非锁缺陷） | 2026-09-20 | 1 / 1 |

**纳入标准**：与 `analysis/llm` / `analysis/aicap` 的某项手写能力存在**功能重叠**，且是**可安装的库或框架**（不是论文、不是服务、不是红队工具）。
**排除记录**：红队/评测工具（`NVIDIA/garak`、`Azure/PyRIT`）—— 它们的对手是「被测模型」，不是「生成内容的质量」；
纯提示词技术（spotlighting / delimiting）—— 见 §4 空白矩阵，它们**没有**可复用的库形态。

---

## 3. 逐项对比表

**判定口径**：`✅ 可复用` ／ `🟡 有条件复用`（需对齐模式或留到阶段 B）／ `⛔ 不复用`（默认行为冲突或形态不匹配）。

### 3.1 契约与解析类

| 手写能力 | 位置 | 开源候选 | 许可（A） | 活跃度（A） | 冲突点 | 判定 |
| --- | --- | --- | --- | --- | --- | --- |
| 三段式 JSON 容错提取（`AR-17`） | `llm/extract.py`（91 行） | [`mangiucugna/json_repair`](https://github.com/mangiucugna/json_repair) | **MIT** | 5102★ · 2026-09-17 · 未归档 | 默认**修复**（补引号/括号/默认值）；但提供 `strict=True`：遇重复键、缺分隔符、多顶层元素等**抛 `ValueError`** —— 与 `AR-15` / `AR-21` 同向 | ✅ **可复用**（须用 `strict=True`；落地前须做行为对齐测试，见 §5 待核 T-1） |
| 独立契约校验层（`AR-15`） | `llm/contract.py`（74 行） | [`pydantic/pydantic`](https://github.com/pydantic/pydantic) · [`python-jsonschema/jsonschema`](https://github.com/python-jsonschema/jsonschema) · [`jcrist/msgspec`](https://github.com/jcrist/msgspec) | MIT · MIT · BSD-3 | 28835★ · 未归档（另两者同） | **pydantic 默认 lax 模式会强制转换**（`'123'` → `123`）—— 与 `AR-15`「禁止修正、禁止默认值」正面冲突；`strict=True` 可关掉，但「长度上限 + 额外字段禁止 + 中文错误信息」这套现成语义仍需自己叠 | 🟡 **阶段 B 再评估**：当前 74 行、零依赖、语义完全自控；换库的收益（类型系统）不抵换库的语义风险 |
| 统一信封 `{accepted, data}`（`AR-16`） | `llm/envelope.py`（45 行） | —— | —— | —— | 无对等物：这是**本项目的跨模块契约**，不是通用模式 | ⛔ **不复用**（自研正确） |
| 分用途长度上限 + 显式截断记录（`AR-18` / `AR-23`） | `llm/limits.py`（73 行） | `tiktoken`（MIT，token 计数）不覆盖「按用途分档 + 记录丢弃数量」 | —— | —— | 语义是**本项目独有的纪律**（截断必须可审计） | ⛔ **不复用** |
| 提示词资源化 + 占位符校验（`AR-24`） | `llm/prompts.py`（87 行） | [`pallets/jinja`](https://github.com/pallets/jinja)（BSD-3） | BSD-3 | 未归档 | jinja2 提供表达式、过滤器、宏 —— 模板里可写逻辑，与 `AR-31`（指令区与数据区严格分离）的精神相反；`{{x}}` 双花括号是**刻意无逻辑**的选择 | ⛔ **不复用** |
| 双阶段超时收尾（`AR-19`…`AR-21`） | `llm/twophase.py`（134 行） | [`jd/tenacity`](https://github.com/jd/tenacity)（Apache-2.0） | Apache-2.0 | 未归档 | tenacity 解决的是**重试**；本项目要的是「超时后**复用同一会话**收尾，两阶段皆失败则整体作废」—— 重试语义正相反 | ⛔ **不复用** |

### 3.2 内容与安全类

| 手写能力 | 位置 | 开源候选 | 许可（A） | 活跃度（A） | 冲突点 | 判定 |
| --- | --- | --- | --- | --- | --- | --- |
| 生成内容黑名单三类（`AR-22`）：泄露 / 自曝 / 超长 | `llm/blacklist.py`（87 行） | [`data-privacy-stack/presidio`](https://github.com/data-privacy-stack/presidio)（原 `microsoft/presidio`，**仓库已迁移**）· [`Yelp/detect-secrets`](https://github.com/Yelp/detect-secrets) | MIT · Apache-2.0 | 10956★ · 2026-09-20 · 未归档（detect-secrets 4635★） | 二者都是**检测器**（返回命中 + 置信度），不含「命中即拒绝、不入库、不下发」的处置语义；Presidio 的 NER 需要**模型文件**（离线可跑但要占体积） | 🟡 **阶段 B 复用 Presidio 作为 `AR-22` 泄露类的识别器**（本项目负责处置语义，不做识别器） |
| 不可信数据区（`AR-31`） | `llm/untrusted.py`（51 行） | 无对口库：spotlighting / delimiting 是**提示词技术**（论文），不是可安装实现 | —— | —— | 没有库可复用 | ⛔ **不复用**（见 §4 空白 C-2） |
| 分析客户端无执行面断言（`AR-32`） | `llm/client.py`（59 行） | 无对口库：通用 LLM SDK 的方向相反（**提供**工具调用） | —— | —— | 本项的价值是「**不用**某个能力」，不是「用了谁」 | ⛔ **不复用**（自研正确） |
| 内容地址化 ID / 校验和 / 确定性清单（`AR-30`） | `aicap/content.py`（268 行） | 概念通用（CAS），实现用 stdlib `hashlib` 已足够 | —— | —— | 清单字段是本项目契约（`spec/ai-contract.md`） | ⛔ **不复用** |
| 风格一致性校验（`AR-33` 第四关） | `aicap/guardrail/inspect.py`（84 行） | 无对口库（术语命中是项目独有的画像判据） | —— | —— | —— | ⛔ **不复用** |

### 3.3 生成与护栏框架类

| 手写能力 | 位置 | 开源候选 | 许可（A） | 活跃度（A） | 冲突点 | 判定 |
| --- | --- | --- | --- | --- | --- | --- |
| 唯一出口 + 任务注册表 + 启动期断言（`AR-33`） | `aicap/service.py`（122 行）+ `tasks/_registry.py`（159 行） | [`guardrails-ai/guardrails`](https://github.com/guardrails-ai/guardrails) · [`NVIDIA-NeMo/Guardrails`](https://github.com/NVIDIA-NeMo/Guardrails)（原 `NVIDIA/NeMo-Guardrails`，**仓库已迁移**） | Apache-2.0 · Apache-2.0 | 7435★ · 2026-09-18 · 未归档（NeMo 7172★） | ① 形态：框架 + Hub + Flask 服务端 vs 「一个函数 + 启动期断言」；② `guardrails-ai` 的 `fix` / `reask` 与 `AR-15` 冲突（它有 `OnFailAction.EXCEPTION`，但默认不是）；③ Hub 侧已**停止托管远程推理**（2026-08 截止），validator 需逐个 `pip` 装 —— 依赖面不可控 | ⛔ **不复用框架**；可借鉴其 `OnFailAction` 的命名 |
| 确定性模板生成器（阶段 A 的 `kind=content`） | `aicap/tasks/content.py`（163 行） | [`joke2k/faker`](https://github.com/joke2k/faker) · [`lk-geimfari/mimesis`](https://github.com/lk-geimfari/mimesis) | MIT · MIT | 19404★ · 2026-09-15 · 未归档（Mimesis 4842★） | 两者都提供 `seed()` / `seed_instance()` → **能保住生成期可复现**（阶段 A 的工程性质；**不是** `AR-30` 的要求 —— 见 ADR-0026 决定 2）；Faker 的假数据（人名/订单/日志）正是本任务要的「像真」素材 | ✅ **阶段 B 建议复用 Faker**（`seed()` 固定 ⇒ 生成期字节可复现；Mimesis 更快但生态与 locale 覆盖更小） |
| 模型后端与结构化输出（阶段 B） | 未实现 | [`dottxt-ai/outlines`](https://github.com/dottxt-ai/outlines) · [`567-labs/instructor`](https://github.com/567-labs/instructor) | Apache-2.0 · MIT | 15843★ · 2026-09-19 · 未归档（Instructor 13926★） | Outlines 依赖本地推理后端（vLLM / transformers）；Instructor 依赖 pydantic + 各家 provider SDK（**可能出网**，须按 ADR-0023 失效条件 4 另行评估） | 🟡 **阶段 B 单独评估**（本项即 ADR-0023 未解决 1） |

**许可小结**：15 个候选**全部**是 MIT / Apache-2.0 / BSD-3-Clause，**没有** AGPL / SSPL / BUSL 类。
（`NVIDIA-NeMo/Guardrails` 的 GitHub API 返回 `NOASSERTION`，但仓库 `LICENSE.md` 逐字写着
`SPDX-License-Identifier: Apache-2.0` —— **以仓库文本为准**，这是 API 字段不可靠的一个实例。）

---

## 4. 空白矩阵

把「能力」与「开源是否有对等实现」两个轴交叉，标出空白在哪：

| 能力 | 开源有对等实现？ | 实现与 `AR-15`/`AR-21`/`AR-30` 兼容？ | 结论 |
| --- | --- | --- | --- |
| JSON 容错提取 | 有（`json_repair`） | **可**（`strict=True`） | **可复用** |
| Schema 校验 | 有（pydantic / jsonschema / msgspec） | 部分（需 strict） | 阶段 B 再评估 |
| 提示词模板 | 有（jinja2 / mustache） | **不兼容**（可写逻辑） | 空白：自研正确 |
| 超时与收尾 | 有（tenacity / anyio） | **不兼容**（重试 ≠ 复用会话收尾） | 空白：自研正确 |
| PII / 泄露识别 | 有（Presidio / detect-secrets） | 兼容（只是识别器） | 阶段 B 复用 |
| 假数据生成 | 有（Faker / Mimesis） | **可**（`seed()`） | 阶段 B 复用 |
| 护栏框架 | 有（guardrails-ai / NeMo） | **不兼容**（形态 + `fix`/`reask`） | 空白：自研正确 |
| 结构化输出（约束解码） | 有（Outlines / Instructor） | 待评估（后端与出网） | 阶段 B 评估 |
| **数据/指令分离（`AR-31`）** | **无库** —— 只有论文技术 | —— | **空白 C-2：自研是唯一选项** |
| **统一信封 + 长度纪律 + 无执行面（`AR-16`/`AR-18`/`AR-23`/`AR-32`）** | **无** | —— | **空白 C-3：这些是本项目的契约，不是通用组件** |

**空白 C-2 值得单独记一笔**：`AR-31`（间接提示注入防护）是**整条链上唯一没有开源实现可复用**的安全机制。
检索到的所有「prompt injection 检测器」都在做**分类**（这段文本像不像攻击），而 `AR-31` 要求的是
**结构**（攻击者内容只能进数据区、不得进指令区 + 长度与标记自检）—— 分类器会给「像不像」的概率，
`AR-31` 要的是「有没有做到」的断言。两者不可互换。

---

## 5. 待核清单

| # | 未核的主张 | 为什么没核 | 影响 |
| --- | --- | --- | --- |
| T-1 | `json_repair(strict=True)` 是否仍能处理 **markdown 围栏**与**前置叙述文本**（`AR-17` 的第 ② 段） | 需要装入环境实跑；本轮不改代码，不引入依赖 | 决定「可复用」能否落地。**落地前必须先写行为对齐测试**（旧提取器 vs 新提取器，同一组样本逐条比对），不通过则不换 |
| T-2 | `Presidio` 的离线 NER 模型体积与启动开销 | 同上（阶段 B） | 影响「是否值得引」 |
| T-3 | `Outlines` / `Instructor` 安装后的传递依赖里是否有需要出网的组件 | 同上（阶段 B） | ADR-0023 失效条件 4 |
| T-4 | 二手对比文章（证据级 **C**）中关于各框架「延迟开销 ~50ms」「24 个 guardrail 基准」等数字 | 无原始数据可核，按纪律**不得作为基线** | 无（未写入任何结论） |

---

## 6. 局限

1. **只审了两个包的「功能重叠」，没审性能**。本轮没有做基准 —— 按技能 `evidence-and-decisions` §3，
   要声称「换库更快 / 更准」必须先定指标与失效判据、设对照组，那是另一轮工作。
2. **没装任何一个候选库**。所有行为判断来自**维护者文档与仓库源码**（证据级 A/B），不是实测。
   因此 §3 的 `✅ 可复用` 是「**值得进入落地评估**」，不是「已验证可替换」。
3. **只审了 AI 能力本体**。`intent/` `chain/` `strategy/` `worker/`（消费者）与跨语言侧
   （`core/internal/policy/ai.go` · `edge/injection/`）不在本轮范围；它们的复用问题是另一份材料。
4. **检索源偏代码托管**。论文库只作了旁证（spotlighting 一类），
   没有做系统性的学术检索 —— 若后续要声称「开源侧没有实现」，应当补一轮 §1.2 的多源检索。
5. **许可核验到「仓库 LICENSE 正文」为止**，没有逐文件核对是否夹带其他许可的第三方代码。
   这是 `TB-16` 的既有口径（审依赖声明的许可），不是本轮新增的放宽。

---

## 7. 与其他材料的关系

| 材料 | 关系 |
| --- | --- |
| [ADR-0023](../decisions/0023-deception-content-injection.md) 未解决 1 / 2 | 本材料回答了它的**调研部分**（候选、许可、形态、冲突点）；**选型决定**见 [ADR-0024](../decisions/0024-ai-oss-reuse-boundary.md) |
| [`../../design/language.md`](../../design/language.md) `TB-16` | 本材料发现的「Python 依赖无审计面」是它的实现缺口，已在 `scripts/licensecheck/` 补上 |
| [`../../modules/llm-components.md`](../../modules/llm-components.md) §3 / §8 | 结论已回写（依赖表 + 未决项） |
| [`../../modules/ai-capability.md`](../../modules/ai-capability.md) §3 / §8 | 同上 |
| [`../../kb/known-issues.md`](../../kb/known-issues.md) | 环境漂移与审计缺口已登记 |
