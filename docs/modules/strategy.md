# 模块：`strategy`

| 项 | 内容 |
| --- | --- |
| 模块名 | `strategy` |
| 所属层 | `L4`（依据 MD-1） |
| 实现语言 | `Python`（依据 [`../design/language.md`](../design/language.md) §1 与 `TB-2`） |
| 负责人 | — |
| 状态 | ✅ **已实现（双路）**（阶段 3，Python）：策略数据生成（灰度 ≤20%、阈值有下界）+ 诱饵轮换决策；**只出数据**，经 `policy` 下发。**模型路径已接**（`aicap` 的 `kind=strategy`，`--llm` 才走，失败回落规则版） |
| 最后更新 | 2026-09-21 |

---

## 1. 职责

**做什么**：

- **策略生成**：根据意图与攻击链，生成/调整欺骗策略（诱饵选择、灰度、阈值建议）；
- **诱饵再生成决策**（[ADR-0016](../background/decisions/0016-decoy-polymorphism.md)）：收到识破信号时，决定是否轮换变体；
- 产出**策略数据**，经 `policy` 版本化后下发。

**明确不做什么**：

- **不做判定**（`AR-2`）· **不做决策**（`MD-12`）· **不做执行**（`SB-1`）；
- **不直接回写响应** —— 策略只经 `policy` 下发，**禁止**影响对攻击者的响应（`AR-32`，防注入回流）；
- **不直接改适配器配置** —— 经 `policy` 的策略平面（`ST-8`）。

## 2. 输入 / 输出契约

| 方向 | 契约 | 定义位置 |
| --- | --- | --- |
| 输入 | 意图结论（`intent`）+ 攻击链（`chain`）+ 识破信号 | `analysis/` |
| 输出 | 策略（数据） | [`../spec/config.md`](../spec/config.md) 的策略段 |
| 输出字段 | `decoy_selection` · `gray_pct` · `threshold_suggestions` · `rationale` | [`../spec/ai-contract.md`](../spec/ai-contract.md) §7（`STRATEGY_SCHEMA` 与 `MAX_GRAY_PCT` / `THRESHOLD_FLOOR`，定义在 `analysis/llm/schemas.py`） |

## 3. 依赖

| 允许依赖 | 原因 |
| --- | --- |
| `llm-components` 的契约层 | 输出校验 · 三任务契约（`llm/schemas.py`：schema 与 `INT-11` 边界常量） |
| `ai-capability`（`aicap`） | 模型路径**必须**走唯一出口 + 护欏（`AR-33`）；`INT-11` 的数值边界在那边也强制一遍（**两边同规**） |
| `store.PolicyStore`（经接口） | 写版本台账 |

| 禁止依赖 | 原因 |
| --- | --- |
| 任何执行能力 | `SB-1` / `SB-2` / `AR-32` |
| 判定与决策逻辑 | `AR-2` / `MD-12` |
| 响应路径 | `AR-32` |

## 4. 关键规则

| 规则 | 与本模块的关系 |
| --- | --- |
| `ST-24` | 策略**必须**是数据（配置），**禁止**编译进代码 |
| `AR-13` | 策略**必须**版本化 + 灰度 |
| `AR-32` | 结论**禁止**直接回写响应；只经 `policy` 间接生效 |
| `AR-31` | 输入结构化（攻击者内容置于数据区） |
| `AR-15` | 输出契约校验 |

## 5. 状态与生命周期

| 状态 | 存在哪里 | 生命周期 | 多副本一致性 |
| --- | --- | --- | --- |
| 策略版本 | `store.PolicyStore`（PostgreSQL，版本只增） | 长期 | 强一致 |
| 生成中间态 | 无（每次生成独立） | —— | 天然一致 |

## 6. 失败模式与降级

| 失败情形 | 行为 | 是否满足 `NI-1` | 依据 |
| --- | --- | --- | --- |
| 策略生成失败 | **维持当前策略**（不发布新版） | ✅ | 失败不改变现状 |
| 模型给出的灰度 / 阈值**越界** | 拒绝建产物（`StrategyBoundError`）⇒ 由调用方回落规则版；**不夹紧、不改写** | ✅ | `INT-11` / `AR-15` |
| 策略 schema 校验失败 | 拒绝发布，走独立分支 | ✅ | `AR-15` / `AR-16` |
| L4 不可用 | 核心与业务不受影响 | ✅ | `NI-1` |

## 7. 测试

| 类型 | 覆盖什么 | 位置 |
| --- | --- | --- |
| 单元 | 策略 schema 校验 + 灰度/阈值边界 | `analysis/tests/test_l4_modules.py`（`test_strategy_generates_data_only_within_limits`） |
| 单元 | **模型路径的 `INT-11`**：灰度 100% / 阈值低于下界均被拒 | `analysis/tests/test_aicap_l4_tasks.py`（`test_strategy_bounds_*` · `test_worker_falls_back_on_bound_violation_*`） |
| 注入样本回归 | 攻击者内容含指令时策略不变 | 同上 |
| 分支穷尽性 | `accepted` 真假两分支 | `MD-8` |

| 运行时链路 | 近线 worker 调用本模块并把结论上报为事件 | `analysis/tests/test_worker.py` + `make analysis`（[ADR-0022](../background/decisions/0022-l4-near-line-worker.md)） |

## 8. 未决项

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | 再生成的判据（哪些识破信号触发轮换） | 多态触发 | [ADR-0016](../background/decisions/0016-decoy-polymorphism.md)（`rotate.py` 已实现三类信号 + 冷却期，阈值仍可取） |
| 2 | 学习路线（规则 → 超博弈 → 学习，[ADR-0016](../background/decisions/0016-decoy-polymorphism.md) 候选 D） | 演进 | 阶段 3 后 |
| 3 | ~~策略 schema 的完整字段~~ | —— | ✅ **已定**（2026-09-21）：`decoy_selection` / `gray_pct` / `threshold_suggestions` / `rationale`（[`../spec/ai-contract.md`](../spec/ai-contract.md) §7）；`rationale` 于本轮补上，使**两条路产出同一形状**（可比的前提） |
| 4 | 策略数据的**消费方**（谁把结论变成 `policy` 的新版） | 建议→生效的闭环 | 今天只上报为结论事件；接线属后续阶段（与 `ADR-0023` 未解决 4 的轮换接线同一批） |

## 9. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 创建（设计）：策略生成 + 诱饵再生成决策 | [ADR-0016](../background/decisions/0016-decoy-polymorphism.md) |
| 2026-09-21 | **双路**：新增模型路径（`aicap` 的 `kind=strategy`）· schema 与 `INT-11` 边界移到 `analysis/llm/schemas.py`（本模块再导出）· 输出补 `rationale`（两条路同形）· §2/§3/§6/§7/§8 同步 | [`../plans/2026-09-21-l4-model-and-stability.md`](../plans/2026-09-21-l4-model-and-stability.md) · [ADR-0031](../background/decisions/0031-analysis-reuse-and-model-backend.md) |
