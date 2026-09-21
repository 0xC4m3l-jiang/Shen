# 模块：`intent`

| 项 | 内容 |
| --- | --- |
| 模块名 | `intent` |
| 所属层 | `L4`（依据 MD-1） |
| 实现语言 | `Python`（依据 [`../design/language.md`](../design/language.md) §1 与 `TB-2`） |
| 负责人 | — |
| 状态 | ✅ **已实现（双路）**（阶段 3，Python）：确定性规则命中 → 意图类别 + 置信度 + 证据引用（无命中即**拒绝**，不臆测）；**模型路径已接**（`aicap` 的 `kind=intent`，`--llm` 才走，失败回落规则版） |
| 最后更新 | 2026-09-21 |

---

## 1. 职责

**做什么**：

- **意图识别**：把观测与会话行为映射为攻击意图类别（侦察 / 利用 / 横向 / 数据窃取 / 持久化）；
- 产出**结构化结论**（意图 + 置信度 + 证据引用），供 `chain` / `strategy` 消费。

**明确不做什么**：

- **不做判定** —— 判定只回答「风险多大」，在 `judge`（`AR-2`）；本模块回答「想干什么」，是**近线/离线分析**；
- **不做决策**（`MD-12`）与**不做执行**（`SB-1` / `AR-32`）；
- **不直接回写响应**（`AR-32`）。

## 2. 输入 / 输出契约

| 方向 | 契约 | 定义位置 |
| --- | --- | --- |
| 输入 | **结构化数据**（攻击者可控内容置于数据区，`AR-31`） | `analysis/` 的输入契约 |
| 输入 | 会话上下文（会话 ID 一等字段，`AR-25`） | 同上 |
| 输出 | `{"accepted": bool, "data": {...}}`（`AR-16`） | 同上 |
| 输出字段 | `category` · `confidence` · `evidence_ids` · `rationale` | [`../spec/ai-contract.md`](../spec/ai-contract.md) §7（`INTENT_SCHEMA`，**只定义一次**在 `analysis/llm/schemas.py`） |
| 五类闭集 | `CATEGORIES`（也定义在 `analysis/llm/schemas.py`） | 同上；本模块**再导出**，不重定义（`MD-5`） |

## 3. 依赖

| 允许依赖 | 原因 |
| --- | --- |
| `llm-components` 的契约层 | 输出校验与容错解析（`AR-15`…`AR-27`）· 三任务契约（`llm/schemas.py`） |
| `ai-capability`（`aicap`） | 模型路径**必须**走唯一出口 + 护欏（`AR-33`）—— 方向是**消费方 → 能力**，是设计意图不是耦合（ADR-0025） |
| 遥测事件的**只读**视图 | 分析输入 |

| 禁止依赖 | 原因 |
| --- | --- |
| 任何执行能力 | `SB-1` / `SB-2` / `AR-32` |
| `judge` / `director` 的判定与决策 | `AR-2` / `MD-12` |
| 响应路径 | 结论**禁止**直接回写响应（`AR-32`） |

## 4. 关键规则

| 规则 | 与本模块的关系 |
| --- | --- |
| `AR-12` | 结论引用证据 ID **必须**在写入前校验存在 |
| `AR-14` | 触发**必须**经态势去重，**禁止**随事件量线性触发 |
| `AR-15` / `AR-16` | 输出由独立契约层校验；统一信封 |
| `AR-31` / `AR-32` | 数据/指令分离；无执行能力；不回流 |
| `TB-2` | 离线 AI 能力**必须**用 Python |

## 5. 状态与生命周期

| 状态 | 存在哪里 | 生命周期 | 多副本一致性 |
| --- | --- | --- | --- |
| 无（每次分析独立） | —— | —— | 天然一致 |
| 意图结论 | `store`（事件 / 攻击链实体） | 按保留策略 | 强一致（经 `store`） |

## 6. 失败模式与降级

| 失败情形 | 行为 | 是否满足 `NI-1` | 依据 |
| --- | --- | --- | --- |
| 契约校验失败 | 抛异常，不返回默认值 | ✅ | `AR-15` |
| **模型给出闭集外的类别** | **拒绝**（`ContractError`）—— **不**回落成某个默认类别（曾经回落成 `reconnaissance`，那是默认值） | ✅ | `AR-15` |
| **模型不可用 / 被拒** | 由调用方（`worker`）回落规则版并把原因记进 `model_rejected` | ✅ | `NI-1` / [ADR-0031](../background/decisions/0031-analysis-reuse-and-model-backend.md) 决定 6 |
| 证据 ID 不存在 | 拒绝写入该结论 | ✅ | `AR-12` |
| L4 不可用 | 核心与业务不受影响（分析不在业务路径上） | ✅ | `NI-1` |

## 7. 测试

| 类型 | 覆盖什么 | 位置 |
| --- | --- | --- |
| 单元 | 契约校验（喂坏输出必失败） | `analysis/tests/test_l4_modules.py`（`test_intent_*`） |
| 单元 | **闭集**：越界类别被拒而不是回落（`Field.allowed`） | `analysis/tests/test_aicap_l4_tasks.py`（`test_intent_out_of_set_*`） |
| 单元 | **模型路径**：越界类别 / 泄露类 / 风格不一致均被拒；同一批观测走模型 vs 规则两条路 | 同上 |
| 注入样本回归 | 攻击者内容含指令时结论不变（`AR-31`） | 同上 |
| 引用校验 | 引用不存在的证据 ID 必被拒（`AR-12`） | 同上 |
| 分支穷尽性 | `accepted` 真假两分支 | `MD-8` |

| 运行时链路 | 近线 worker 调用本模块并把结论上报为事件 | `analysis/tests/test_worker.py` + `make analysis`（[ADR-0022](../background/decisions/0022-l4-near-line-worker.md)） |

## 8. 未决项

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | ~~意图分类体系与 TTP 映射（对齐 MITRE ATT&CK / Engage）~~ | —— | ✅ **已定**：五类即 ATT&CK 战术名，且是**闭集**（不跟着上游演进）；ATT&CK 允许商用但要带版权声明 —— 引用时遵守（[`../background/research/l4-oss-reuse.md`](../background/research/l4-oss-reuse.md) §3.1） |
| 2 | 意图结论如何并入攻击链（与 `chain` 的接口） | —— | ✅ **已定**（2026-09-21）：两者共用 `CATEGORIES`（阶段名＝意图类别）与同一份证据引用校验；`chain` 从本模块导入 `rule_hits`，类别常量从 `llm/schemas.py` 取（`MD-5`） |
| 3 | 五条正则的**覆盖率**（今天只有 5 条） | 规则路径的召回 | 若扩表，按 [`../background/research/l4-oss-reuse.md`](../background/research/l4-oss-reuse.md) §3.2 的做法**借 OWASP CRS 的特征形态**改写（不整库引入），并注明出处 |
| 4 | 模型路径与规则路径的**质量对照**未做 | 两条路谁更准 | [ADR-0031](../background/decisions/0031-analysis-reuse-and-model-backend.md) 未解决 1 |

## 9. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 创建（设计）：意图识别 + 契约与注入纪律 | [ADR-0015](../background/decisions/0015-indirect-prompt-injection.md) |
| 2026-09-21 | **双路**：新增模型路径（走 `aicap` 的 `kind=intent` + 护欏）· 越界类别由「静默回落」改为**拒绝**（`Field.allowed` 守闭集）· 类别与 schema 移到 `analysis/llm/schemas.py` 本模块再导出（`MD-5`）· §2/§3/§6/§7/§8 同步 | [`../plans/2026-09-21-l4-model-and-stability.md`](../plans/2026-09-21-l4-model-and-stability.md) · [ADR-0031](../background/decisions/0031-analysis-reuse-and-model-backend.md) |
