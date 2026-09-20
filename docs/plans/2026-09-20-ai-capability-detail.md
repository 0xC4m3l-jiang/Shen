# 变更包：AI 能力（模型能力）详解文档

| 项 | 值 |
| --- | --- |
| 主题 | 把「AI 能力」完整写出来 —— 7 个能力逐个详解 + 模型后端 8 条硬约束 + 16 条优化点候选 + 5 条关键发现，供用户评审「是否合理 / 哪里要优化」 |
| 日期 | 2026-09-20 |
| 状态 | 已实现 |
| 涉及模块 | `ai-capability`（[`../design/modules.md`](../design/modules.md) §1.1 第 25 行）· `llm-components`（第 20 行）· 消费者 `intent`（17）· `chain`（18）· `strategy`（19） |
| 决策数 | 已答 2 项 / 待定 4 项（均不阻塞本轮） |
| 关联 | 上轮变更包 [`2026-09-20-aicap-decoupling.md`](2026-09-20-aicap-decoupling.md) · [ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md) · [ADR-0024](../background/decisions/0024-ai-oss-reuse-boundary.md) · [ADR-0023](../background/decisions/0023-deception-content-injection.md) · [ADR-0015](../background/decisions/0015-indirect-prompt-injection.md) |

---

## 1. 需求与验收

**要解决什么**（一句话）：用户要评审 AI 能力的设计是否合理、哪些功能点要优化，但**「AI 能力」现在散在三个地方**
（`analysis/llm/` 的纪律层 · `analysis/aicap/` 的出口 · `analysis/intent|chain|strategy/` 的消费者），
没有一份把「我们打算让模型做什么、今天用什么代替、边界在哪、哪里可能是问题」写全的文档。

**做完之后能做什么**：

1. 一页看完 7 个能力各是什么、**模型接没接**、产物去哪、怎么验；
2. 看清「模型后端必须满足什么」（8 条硬约束，每条带规则依据与判据）与「怎么把模型接进来」（只换 `produce`）；
3. 有一份**16 条优化点候选**（分五组，每条给现状 / 问题 / 候选做法 / 代价），可以直接勾选要不要做；
4. 有 **5 条关键发现**（读代码发现的，可能是问题）—— 其中最重的一条：**今天没有任何生产链路在调模型**。

**验收判据**（可验证）：

1. `docs/kb/ai-capabilities.md` 存在，含 §0 速览表（7 个能力 × 8 列）· §2–§5 逐个能力详解 · §6 硬约束表 · §7 接缝 · §8 优化点 · §10 关键发现；
2. **能力总表与代码一致**：逐个能力的位置 / 状态 / 产物在代码里都能核到（每条都有文件链接）；
3. **优化点与关键发现都标了依据**（规则 ID / 命令 / 文件位置），没有「感觉上可能」这类无法核的说法；
4. 与既有三份文档**不重复**：规则在 `design/` · 字节契约在 `spec/ai-contract.md` · 状态证据在 `kb/capabilities.md`
   —— 新文档只在 §0/§9 引用它们；
5. `make gate` 通过（含 `make trace`：新文档被 `docs/kb/README.md` 与 `docs/README.md` 索引，无孤儿、无悬空链接）。

**不做什么**：

- **不做优化本身** —— 16 条只是候选，等用户看过再定（`AGENTS.md` §2.2：不替用户决定）；
- **不改 `docs/design/`** —— `AR-33` 的措辞放宽仍是 🟡 提案（ADR-0025 决定 4，待用户确认）；
- **不引任何依赖、不改任何代码** —— 本轮是文档轮（M 档）；
- **不给动态沙箱立项**（形态待用户裁定）。

---

## 2. 设计逻辑

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 这份文档放哪一层 | `docs/kb/`（**参考层**，不具约束力） | 它要装「我列的优化候选」与「我发现的可疑点」—— 那**不是**已确认规则；`kb/README.md` 明确「不具约束力、允许不确定」 | `docs/kb/ai-capabilities.md` |
| ② | 与三份已有文档怎么分工 | 只**指向**，不复制：规则 → `design/`；字节 → `spec/ai-contract.md`；状态证据 → `kb/capabilities.md` | `MD-3`（契约只在一处）；避免「两处维护、必然漂移」 | 新文档 §0 头部说明 + 各节引用 |

**仍未定**（明确列出，不阻塞本轮）：

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | 16 条优化点里**哪些要做** | 后续排期 | 等用户评审（本文 §8 就是为这个写的） |
| 2 | 「L4 分析接模型」走不走 `aicap` 的 kind 登记（关键发现 1/5） | `AR-33` 措辞、注册表形态 | ADR-0025 决定 4 + 关键发现 1 的三选一 |
| 3 | 动态沙箱的形态 / 层次 / 语言 | 第二个消费方落地 | 用户裁定 |
| 4 | `llm.twophase` 这个骨架**要不要接生产**（关键发现 3） | 模型超时的结果救回能力 | 与优化点 14 一起定 |

**接缝与接口**：本轮**不新增、不修改任何接口**（纯文档）。

**数据流**：无（纯文档）。

---

## 3. 文档对应（追溯矩阵）

| 规则 ID | 文档章节 | 代码 | 测试 / 证据 | 验证命令 |
| --- | --- | --- | --- | --- |
| `AR-33` | §0 速览（唯一出口）· §7 接缝 · §8.1 优化点 1 · §10 发现 4/5 | `analysis/aicap/service.py` · `analysis/aicap/tasks/_registry.py` | `test_aicap_guardrail.py`（未登记必拒）· `make archcheck` 的 `AR-33` 项 | `make archcheck` |
| `AR-15` | §2.2 输出契约 · §6 第 1 条 · §7 | `analysis/llm/contract.py` · `analysis/aicap/guardrail/inspect.py` | `test_llm_contract.py` · `test_aicap_guardrail.py` | `make pytest` |
| `AR-22` / `AR-23` | §2.4 护栏表 · §8.3 优化点 8 | `analysis/llm/blacklist.py` · `analysis/llm/limits.py` | 黑名单三类 + 上限用例 | `make pytest` |
| `AR-24` | §2.3 提示词 · §10 发现 1/2 | `analysis/aicap/guardrail/prompts.py` · `analysis/llm/prompts.py` | `test_aicap_guardrail.py`（启动期断言）· `test_llm_discipline.py` | `make pytest` |
| `AR-30` | §2.5 确定性 · §6 第 4 条 · §8.2 优化点 4/5 | `analysis/aicap/tasks/content.py` | `test_aicap_content.py`（逐字节可复现） | `make ai-check` |
| `AR-31` / `AR-32` | §1.1 三条硬约束 · §6 第 2 条 | `analysis/llm/untrusted.py` · `analysis/llm/client.py` | `test_llm_discipline.py`（`test_ar31_*`） | `make pytest` |
| `AR-12` | §3.1 证据纪律 · §3.2 | `analysis/chain/evidence.py` · `analysis/worker.py` | `test_worker.py` | `make dev` |
| `MD-4` | §10 发现 5 | `scripts/archcheck/main.go` | 构造性反证（上轮 §6.1） | `make archcheck` |
| `MD-3` / `MD-5` | §2 决策 ② | —— | 新文档只引用不复制 | `make trace` |
| `DEV-1` / `DEV-2` | 本文件 · [`../log.md`](../log.md) | —— | `make trace` 的形状检查 | `make trace` |

---

## 4. 代码实现（本轮 = 文档）

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `docs/kb/ai-capabilities.md` | **新增** | 主体：7 个能力详解 + 8 条硬约束 + 16 条优化候选 + 5 条关键发现 |
| `docs/kb/README.md` | 修改 | 文件索引 + 最快路径各加一行（否则新文档是孤儿） |
| `docs/kb/capabilities.md` | 修改 | §1.5 加指向新文档的指针（说明两篇的分工：本文是状态，那篇是详解） |
| `docs/README.md` | 修改 | 文档地图里 `kb/` 的份数 5 → 6，并点名「AI 能力详解」 |
| `docs/modules/ai-capability.md` | 修改 | §1 加指针（读模块文档的人要知道「评审 AI 能力设计」看哪） |
| `docs/plans/2026-09-20-ai-capability-detail.md` | 新增 | 本文件 |
| `docs/log.md` | 修改 | 本轮变更日志条目 |

**关键内容**（新文档的骨架，供快速定位）：

- §0 一页速览：**7 个能力 × 8 列**的表 + 一句结论「今天没有任何一条生产链路在调模型」；
- §1 模型在系统里的位置：三条硬约束 + 三条链路的图（模型出现处用 ★ 标）；
- §2 能力 1 详解（**最详细**，因为它唯一走完了护栏）：输入字段表 / 输出契约 / 提示词 / 五关护栏 / 确定性 / 失败语义 / 怎么验；
- §3 能力 2–4（意图 / 链 / 策略）：形状已是「模型的形状」，填内容的是规则；
- §4 能力 5（双阶段收尾）；§5 能力 6–7（未接通 / 未立项）；
- §6 模型后端 8 条硬约束（每条带规则依据 + 判据）；
- §7 接缝（三步接入，只换 `produce`）；
- §8 优化点候选 16 条（五组：能力覆盖 / 质量与多样性 / 安全与护栏 / 工程与运维 / 契约与可验证）；
- §9 今天怎么验（7 条命令 × 看什么）；
- §10 关键发现 5 条（每条带命令级证据）；
- §11 术语与边界速查（防误解表）。

**必须遵守的上位约束**：`MD-3`（契约不复制）· `P-1`/`P-2`（`design/` 只收已确认内容；本文不往 `design/` 写任何东西）·
`AGENTS.md` §2.2（不替用户决定 —— 16 条全标「候选」）· 技能 `dev-loop` §8（输出形状：结论先行、可核、给量级）。

---

## 5. 测试与场景（文档轮：可核项）

| # | 场景 | 输入 / 前置 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 1 | 能力总表与代码一致 | 逐行核 §0 表里的路径与状态 | 每个路径存在且状态可核（如 `kind=content` 是模板生成器） | ✅ | 表中链接逐条可达；`content.py` 顶部注释即「确定性模板生成器」 |
| 2 | 「没有生产链路调模型」这条结论可核 | `grep -rn "AnalysisClient\|resolve(" analysis/`（非测试、非 `aicap`、非 `llm`） | 只有 `aicap` 出口/接缝引用 | ✅ | 见 §6 证据行 |
| 3 | 「四个提示词只被测试渲染」可核 | `grep -rn "prompts.render\|prompts.load" analysis/` | 生产只有 `aicap/guardrail/prompts.py` | ✅ | 见 §6 证据行 |
| 4 | 「`run_two_phase` 没有生产调用方」可核 | `grep -rn "run_two_phase" analysis/` | 只有 `twophase.py` 与 `test_llm_discipline.py` | ✅ | 见 §6 证据行 |
| 5 | 与既有文档不重复 | 读新文档 §0/§9 与 `design/` `spec/` `kb/capabilities.md` | 新文档只引用、不抄规则正文 | ✅ | 人工核对；`make trace` 的 D-3 项（规则 ID 引用存在性） |
| 6 | 新文档不是孤儿 | 四处索引指针（`kb/README.md` · `docs/README.md` · `kb/capabilities.md` §1.5 · `modules/ai-capability.md` §1） | 每个指针都可达 | ✅ | `grep -rn "ai-capabilities" docs/` → 4 处命中（**注意**：`make trace` 的孤儿检查只覆盖 `docs/modules`、悬空链接检查跳过 `docs/kb`，所以这一项靠人工核，工具帮不上） |

**没有覆盖的**：

- **没有验证 16 条优化点的「值得做」** —— 那是用户要做的判断，本文只负责把候选摆清楚；
- **没有对 L4 分析三件套做效果评估**（规则版准不准）—— 那需要带对照组的基准（技能 `evidence-and-decisions` §3），本轮不做；
- **没有核 `deception/shell/` 等相关模块**（动态沙箱候选形态）—— 本轮只列它作为候选，不下结论。

---

## 6. 验证证据

```console
$ make gate
All checks passed!（ruff）
架构检查通过。
  AI 能力独立性（MD-4：aicap / llm 的依赖白名单）
追溯检查通过。
泄漏检查通过。
许可审计通过：没有传染性或限制性许可。
79 passed in 0.11s
门禁通过。
```

**四条「文档声称」的命令级证据**（评审者要求把它当可核事实）：

```console
# ① 没有别的生产链路碰模型
$ grep -rn "AnalysisClient\|model_seam\|UnconfiguredClient" analysis/ --include=*.py \
    | grep -v "/tests/\|aicap/\|llm/"
(无输出)

# ② 生产里在渲染提示词的只有 aicap；llm/prompts.py 的 assert_startup 只被设了校验、没有调用方
$ grep -rn "prompts.render\|prompts.load\|assert_startup" analysis/ --include=*.py | grep -v "/tests/"
aicap/service.py:135:    prompt = guardrail_prompts.render(
aicap/tasks/_registry.py:151/164/166:     ... assert_startup
aicap/guardrail/prompts.py:85:def assert_startup(
llm/prompts.py:82:def assert_startup(            ← 定义在此，生产无调用方

# ③ 双阶段收尾没有生产调用方
$ grep -rln "run_two_phase" analysis/ --include=*.py
analysis/llm/twophase.py
analysis/tests/test_llm_discipline.py

# ④ aicap 注册表只登记了 content
$ grep -n "return {" -A1 analysis/aicap/tasks/_registry.py
130:    return {CONTENT_TASK.kind: CONTENT_TASK}
```

**独立评审**（冷上下文 `reviewer`，只看产物与 diff，不问作者）：**有异议 5 组，已全部修完** ——

| # | 异议 | 修法 |
| --- | --- | --- |
| ① | **事实错误 4 处**：`CATEGORIES`/`INTENT_SCHEMA` 的「守住闭集」不成立（schema 只查 str+长度，越界值**静默回落**）· chain 产物多写了 `conclusion`（实现没有）· §10 发现 1 的证据行复现不出 · 发现 4 的成因写错（是**事件里没有 `backend`**，不是「诱饵资产未启用」） | 四处全部改正；并把 `CATEGORIES` 那条**升级成新发现 6 + 优化点 17**（它本身是个真缺陷） |
| ② | **P1 未验的声称**：变更包 §6 写着「待填」而 §1 已标「已实现」；`docs/log.md` 无本轮条目 | 本轮补齐（本节 + 日志条目） |
| ③ | §1.1 把 ADR-0025 **待确认**的 `AR-33` 放宽写成既有硬约束；§8.1 有一个**坏交叉引用**（指 §11，实际在别处） | §1.1 加 ⚠️ **口径说明**（现文只覆盖欺骗内容、门禁是全局的、放宽是提案）；交叉引用改指 §10/§11 |
| ④ | **重复维护**：§2.4 与 `spec/ai-contract.md` §1.6 逐行同构（连常量都抄）；§0 状态列与 `kb/capabilities.md` 重复 | §2.4 改为「只列本能力参数 + 指向契约」；§0 声明「快照，权威在 `capabilities.md`」 |
| ⑤ | P2：§5 行 6 把「不是孤儿」的证据挂在 `make trace` 上，而 tracecheck **跳过 `docs/kb`** | 改为人工作业 + 四处指针的 `grep` 证据，并写明工具帮不上 |

> 评审同时确认**未越界**：`docs/design/` 无改动、`AR-33` 原文未改、优化点全部标「候选」、本轮只动 `.md`。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 16 条优化点**全部未定** | 后续排期 | 等用户评审（这是本轮的交付目的） |
| 2 | 关键发现 1/5 的三选一未定（L4 四个模型任务走 `aicap` 登记 / 留 `llm/` / 别的） | `AR-33` 措辞与注册表形态 | ADR-0025 决定 4 |
| 3 | 关键发现 2（`llm.prompts.assert_startup` 不在生产启动路径上） | `AR-24` 对那四个模板只有测试保障 | 若决定接模型，必须在启动路径上补校验 |
| 4 | 关键发现 3（`run_two_phase` 是骨架） | 模型超时的结果救回能力未启用 | 优化点 14 + 决定 4 |
| 5 | 新文档与 `kb/capabilities.md` 存在**内容重叠的风险**（都在讲 AI 能力） | 长期会漂移 | 已划界：本文=详解与评审材料（易变），那篇=状态与证据（稳定）；若后续重合一，应合并并删其一 |
| 6 | Marksman 报 5 条「ambiguous link」（提示词文件与模块文档同名，如 `intent.md`） | 无（链接可达，工具按 basename 去重） | 工具噪音，不修；与上轮记录的 `strategy.md` 同一类 |
| 7 | 独立评审又指出**一处真缺陷**：`intent` 的越界类别静默回落 `reconnaissance`（违背 `AR-15` 精神） | 新增第六类那天会**静默误标** | 已写进新文档 §8.5 优化点 17 与 §10 发现 6；**本轮不动代码**（纯文档轮），建议单开一个 S 档小轮修 |

---

## 7.1 审视记录（M 档：改了对外描述与文档索引 → 必做）

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 新文档若不被索引就是**孤儿** | 孤儿文档 | `grep -rn "ai-capabilities" docs/` 在本轮前为空 | 在 `kb/README.md`（索引 + 最快路径）、`docs/README.md`（地图 + 份数 5→6）、`kb/capabilities.md` §1.5、`modules/ai-capability.md` §1 四处加指针 | 四向可达 |
| 2 | `docs/README.md` 写着 `kb/` 有 **5 份** | 过期计数（`TC-2` 同族） | `docs/README.md` 文档地图行 | 6 份并点名新文档 | 已改 |
| 3 | 新文档会不会**重复** `spec/ai-contract.md` 与 `modules/ai-capability.md`？ | 重复维护风险 | **独立评审指出**：§2.4 的护栏表与 `spec/ai-contract.md` §1.6 **逐行同构**（还抄了常量 `65536`），而 `modules/ai-capability.md` 已有第三份 | §2.4 改为「只列**本能力的参数**（受检字段 / 用途 / 上限 / 术语 / 注入标识）+ 指向契约」，四道关的定义不再复制 | 重复已消除；第三份（模块文档）保持为逐模块视角，与契约各可核 |
| 3b | §0 概览表的「状态」列与 `kb/capabilities.md` 对同一批能力给出同样状态 | 两处维护必然漂移 | 独立评审指出同一状态两处维护 | §0 加一句「**状态列是 2026-09-20 的快照**，权威在 `capabilities.md`」 | 单一权威已声明 |
| 4 | 优化点里有没有「不可核」的说法？ | 悬空断言 | 逐条扫 §8：每条都有现状 + 问题 + 候选 + 代价 | 把「可能/大概」改成可核事实（如「N=8 固定变体」「只查术语命中」） | 16 条全部可核 |
| 5 | 关键发现是否都有**命令级**证据？ | 悬空结论 | §10 每行「证据」列 | 5 条全部给出 `grep` 式证据 | 可复核 |
| 6 | 本轮有没有改到 `docs/design/`？ | 越界核查（`P-1` / §3.1） | `git diff --name-only docs/design/` → 空 | 无动作 | 未越界 |
| 7 | 上一轮记录的工具缓存假失败会不会**再犯**？ | 复发风险 | 上轮审视表第 6 条 | 本轮改的是纯文档，不触发该路径；教训已留在上轮变更包 | 记录在案 |

> **排除项**：`docs/background/` · `docs/plans/` · `docs/log.md` · `docs/kb/` 的历史记录部分不在审视范围内。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-20 | 首版：AI 能力详解文档（7 能力 × 8 列速览 + 逐个详解 + 8 条模型硬约束 + 16 条优化候选 + 5 条关键发现）+ 四处索引 | 用户：「把对应模型能力介绍写详细，后面会进行分析再告诉你是否合理、有哪些功能点需要优化」 |
| 2026-09-20（**修正**） | 本文件 §1/§4/§8 里写的「**5** 条关键发现」在定稿时已变成 **6** 条（独立评审把 `intent` 越界静默回落升级为发现 6）—— 当时数字未同步 | 见 [`2026-09-20-ai-capability-lifecycle.md`](2026-09-20-ai-capability-lifecycle.md)（下一轮的变更包）；正文不改（历史快照），仅在此登记 |
