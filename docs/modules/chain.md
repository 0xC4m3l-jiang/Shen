# 模块：`chain`

| 项 | 内容 |
| --- | --- |
| 模块名 | `chain` |
| 所属层 | `L4`（依据 MD-1） |
| 实现语言 | `Python`（依据 [`../design/language.md`](../design/language.md) §1 与 `TB-2`） |
| 负责人 | — |
| 状态 | ✅ **已实现（双路）**（阶段 3，Python）：攻击链还原（阶段有序）+ 四类识破信号（ADR-0016）+ 证据引用校验（`AR-12`）；**模型路径已接**（`aicap` 的 `kind=chain`，`--llm` 才走，失败回落确定性版） |
| 最后更新 | 2026-09-21 |

---

## 1. 职责

**做什么**：

- **攻击链还原**：把零散事件串成可追踪、可回放的攻击链（阶段 + 置信度 + 结论）；
- **识破信号识别**（[ADR-0016](../background/decisions/0016-decoy-polymorphism.md)）：识别「诱饵已被识破」的信号
  （诱饵被直接跳过 / 命中后无后续 / 显式比对 / 多会话同法访问），供 `strategy` 触发再生成。

**明确不做什么**：

- **不做判定**（`AR-2`）· **不做决策**（`MD-12`）· **不做执行**（`SB-1`）；
- **不直接回写响应或策略** —— 识破信号经 `strategy` → `policy` 间接生效（`AR-32`）。

## 2. 输入 / 输出契约

| 方向 | 契约 | 定义位置 |
| --- | --- | --- |
| 输入 | 遥测事件（只读） | `store` 的事件实体 |
| 输入 | 意图结论（来自 `intent`） | `analysis/` |
| 输出 | 攻击链（`chain` + `chain_node`） | [`../design/structure.md`](../design/structure.md) §3 |
| 输出 | 识破信号 | 同上 |
| 输出字段（模型路径） | `stages` · `broken_decoy_signals` · `rationale` | [`../spec/ai-contract.md`](../spec/ai-contract.md) §7（`CHAIN_SCHEMA`，定义在 `analysis/llm/schemas.py`） |

## 3. 依赖

| 允许依赖 | 原因 |
| --- | --- |
| `store` 的**只读**接口 | 读事件与链 |
| `llm-components` 的契约层 | 输出校验 · 三任务契约（`llm/schemas.py`）· 证据校验（`chain/evidence.py`） |
| `ai-capability`（`aicap`） | 模型路径**必须**走唯一出口 + 护欏（`AR-33`）—— 方向是**消费方 → 能力**（ADR-0025） |

| 禁止依赖 | 原因 |
| --- | --- |
| 任何执行能力 | `SB-1` / `SB-2` / `AR-32` |
| 判定与决策 | `AR-2` / `MD-12` |
| 响应路径 | `AR-32` |

## 4. 关键规则

| 规则 | 与本模块的关系 |
| --- | --- |
| `AR-12` | **引用证据 ID 必须在写入前校验存在** —— 本模块的核心约束（链的每条边都要有真实证据） |
| `AR-14` | 触发必须经态势去重 |
| `AR-31` / `AR-32` | 数据/指令分离；无执行能力 |
| `NI-13` | 取证数据按类别分别配置保留天数 |

## 5. 状态与生命周期

| 状态 | 存在哪里 | 生命周期 | 多副本一致性 |
| --- | --- | --- | --- |
| 攻击链 | `store`（`chain` + `chain_node`，PostgreSQL） | 按保留策略 | 强一致 |
| 只增不改 | `chain_node` 的引用关系 | 永久（按保留策略） | 强一致 |

## 6. 失败模式与降级

| 失败情形 | 行为 | 是否满足 `NI-1` | 依据 |
| --- | --- | --- | --- |
| 引用证据不存在 | **拒绝写入**该结论（模型路径同样：链整体作废、回落确定性版） | ✅ | `AR-12` |
| 契约校验失败 | 抛异常 | ✅ | `AR-15` |
| 模型给出的链**形状不对**（阶段项不是对象 / 缺 `evidence_ids` / 置信度越界） | 抛异常 ⇒ 整条链作废、回落确定性版（**不猜、不补默认值**） | ✅ | `AR-15` / `NI-1` |
| L4 不可用 | 核心与业务不受影响 | ✅ | `NI-1` |

## 7. 测试

| 类型 | 覆盖什么 | 位置 |
| --- | --- | --- |
| 单元 | **引用校验**：注入不存在的证据 ID 必被拒 | `analysis/tests/test_l4_modules.py`（`test_ar12_evidence_refs_must_exist`） |
| 注入样本回归 | 攻击者内容含指令时结论不变 | 同上 |
| 识破信号 | 四类信号的判据各一例 | 同上 |
| 单元 | **模型路径的 `AR-12`**：引用不存在的证据 ⇒ 整条链作废并回落（不得连带拖垮其他步） | `analysis/tests/test_aicap_l4_tasks.py`（`test_worker_voids_model_chain_*`） |
| 分支穷尽性 | 链阶段的全分支 | `MD-8` |

| 运行时链路 | 近线 worker 调用本模块并把结论上报为事件 | `analysis/tests/test_worker.py` + `make analysis`（[ADR-0022](../background/decisions/0022-l4-near-line-worker.md)） |

## 8. 未决项

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | 攻击链的判据（哪些事件构成一条链） | 实现 | ✅ **已定**：阶段名 = 五类意图类别（`CATEGORIES`），按 `STAGE_ORDER` 排序；未命中任何规则的阶段不出现（**不编造阶段**） |
| 2 | 识破信号的判据与阈值（误报/漏报权衡） | 再生成触发 | [ADR-0016](../background/decisions/0016-decoy-polymorphism.md) 未解决（本模块实现四类判据，阈值仍可取） |
| 3 | ~~链与 `intent` / `strategy` 的接口~~ | —— | ✅ **已定**（2026-09-21）：`intent` → `stages[]`（共用类别闭集）→ `strategy` 的输入（阶段名 + 识破信号种类）；契约在 [`../spec/ai-contract.md`](../spec/ai-contract.md) §7 |
| 4 | **模型路径的图算法需求**（路径搜索 / 环检测 / 跨会话攻击者图） | 是否该引入图库 | **今天没有这种需求**（链还原是分组 + 证据校验，O(命中数)）；若真出现，按 [`../background/research/l4-oss-reuse.md`](../background/research/l4-oss-reuse.md) §3.4 重开 `NetworkX` 评估（[ADR-0031](../background/decisions/0031-analysis-reuse-and-model-backend.md) 失效条件 3） |
| 5 | `stages[].name` / `broken_decoy_signals[]` 的**闭集无机器守卫** | 模型可给越界值 | [ADR-0031](../background/decisions/0031-analysis-reuse-and-model-backend.md) 未解决 2 |

## 9. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 创建（设计）：攻击链还原 + 识破信号识别 | [ADR-0016](../background/decisions/0016-decoy-polymorphism.md) |
| 2026-09-21 | **双路**：新增模型路径（`aicap` 的 `kind=chain`，`AR-12` 同样强制）· 阶段名与 `intent` 共用闭集（定义移到 `analysis/llm/schemas.py`）· §2/§3/§6/§7/§8 同步 | [`../plans/2026-09-21-l4-model-and-stability.md`](../plans/2026-09-21-l4-model-and-stability.md) · [ADR-0031](../background/decisions/0031-analysis-reuse-and-model-backend.md) |
