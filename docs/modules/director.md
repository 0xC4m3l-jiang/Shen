# 模块：`director`

| 项 | 内容 |
| --- | --- |
| 模块名 | `director` |
| 所属层 | `核心`（依据 MD-1） |
| 实现语言 | `Go`（依据 [`../design/language.md`](../design/language.md) §1） |
| 负责人 | — |
| 状态 | 实现中（阶段 2a） |
| 最后更新 | 2026-09-18 |

---

## 1. 职责

**做什么**：

- 把 `judge` 的风险分映射为**三值决策**（`route_origin` / `route_mirage` / `block`）+ 改道后端名；
- 按 `policy.gray_pct` 做**灰度收敛**：本会改道的请求里只有一定比例真正改道；
- 实现 `control.Decider` 接口，替换影子决策器（`control.ShadowDecider`）。

**明确不做什么**：

- **不做判定** —— 判定只在 `judge` 一处实现（`AR-2`）；本模块只消费 `Verdict.Score`，不参与评分；
- **不做熔断** —— 熔断是「服务整体降级」，落在 `control` 服务面（本模块只做决策）；
- **不选后端池** —— 2a 改道目标是一个固定逻辑名；多后端与健康检查是阶段 3 的事；
- **不读配置** —— 阈值与灰度经接口注入（`ThresholdSource` / `GraySource`）；
- **不依赖系统时钟做判定** —— 灰度只依赖 `decision_id`（`MD-6`）。

## 2. 输入 / 输出契约

跨模块与对外的契约不在这里（规则 `MD-3`），只引用：

| 方向 | 契约 | 定义位置 |
| --- | --- | --- |
| 输入 | `contract.JudgeRequest` | [`../../core/internal/contract/observation.go`](../../core/internal/contract/observation.go) |
| 输入 | `contract.Thresholds` | [`../../core/internal/contract/thresholds.go`](../../core/internal/contract/thresholds.go) |
| 输入 | `contract.PolicySnapshot`（取 `GrayPct`） | [`../../core/internal/contract/policy.go`](../../core/internal/contract/policy.go) |
| 输出 | `contract.Decision` | [`../../core/internal/contract/decision.go`](../../core/internal/contract/decision.go) |

本模块内部的数据结构：`Engine`（`judge.Judge` + `ThresholdSource` + `GraySource` + `Config`）。

> 规则 `MD-5`：跨模块共享类型收敛到 `contract`；本模块不定义与它同名的类型。

## 3. 依赖

| 允许依赖 | 原因 |
| --- | --- |
| `judge.Judge` | 取风险分与信号（判定唯一实现处） |
| `contract` | 进程内共享类型 |
| `ThresholdSource` / `GraySource`（本模块定义） | 阈值与灰度的来源；由 `policy.Loader` 实现 |

| 禁止依赖 | 原因 |
| --- | --- |
| `store` | 本模块不做 I/O（`MD-6`）；核心的 I/O 出口只有一个 |
| `policy` 具体类型 | 只依赖接口，依赖方向单向（消费方定义接口） |
| 适配器（`edge/`） | 依赖方向单向：适配器 → 核心 |

> 规则 `MD-4`：依赖方向必须单向。核心模块禁止外呼、禁止写业务存储、禁止依赖系统时钟做判定。

## 4. 关键规则

| 规则 | 与本模块的关系 |
| --- | --- |
| `AR-2` | 判定只在 `judge`；本模块不复制任何判定逻辑 |
| `AR-9` | 核心必须无状态多副本；本模块无状态，决策是输入的纯函数 |
| `MD-6` | 禁止依赖系统时钟做判定；灰度只依赖 `decision_id` |
| `MD-8` | 决策分支（三值）必须有穷尽性测试 |
| `MD-12` | 决策取值只在 `director` 定义一次（`contract.Action` 由本模块产出） |
| `MD-13` | `severity` 必须与决策取值并列输出 |
| `MD-22` | 本模块有独立可运行的测试套件，不依赖其他模块真实实例 |
| `MD-24` | `severity` 档位只能用登记过的值 |
| `NI-5` | 未识别 / 未匹配 / 非法输入必须回落 `route_origin` |
| `NI-10` | 熔断（实现落在 `control` 服务面，不在本模块） |
| `ST-10` | 灰度必须确定性：同一 `decision_id` 必得同一结果 |
| `ST-23` | 阈值必须集中定义并可配置覆盖 |
| `INT-12` | 灰度必须支持逐级放开 |
| `INT-24` | 阈值必须集中定义并可配置覆盖 |
| `TM-13` | `severity` 档位未登记前实现只支持 `none` |
| `MD-25` | **诱饵面 observe-only**：诱饵前缀集内的路径**禁止**产出 `block`（实现见 `Config.DecoyPrefixes`） |
| `OH-1`…`OH-5` | 不适用 —— 本模块不产生任何攻击者可见的响应（那是适配器与接入层的事） |

## 5. 状态与生命周期

| 状态 | 存在哪里 | 生命周期 | 多副本一致性 |
| --- | --- | --- | --- |
| 无 | —— | —— | 天然一致：决策是 `(Verdict, Thresholds, GrayPct, decision_id)` 的纯函数 |

> 规则 `AR-9`：核心必须无状态多副本。`Engine` 不持有任何请求级或跨请求状态。

## 6. 失败模式与降级

| 失败情形 | 行为 | 是否满足 `NI-1` | 依据 |
| --- | --- | --- | --- |
| `judge` 返回错误 | 上抛错误 → 服务面 → 适配器 fail-open 放行 | ✅ | `NI-3` / `NI-4` |
| `ThresholdSource` 取阈值失败 | 上抛错误（同上） | ✅ | `NI-3` |
| `GraySource` 取灰度失败 | 上抛错误（同上） | ✅ | `NI-3` |
| 分数越界 / 未识别取值 | 回落 `route_origin` | ✅ | `NI-5` |
| 后端名在适配器侧查不到 | 适配器回落业务（本模块无感知） | ✅ | `NI-5` |

## 7. 测试

| 类型 | 覆盖什么 | 位置 |
| --- | --- | --- |
| 单元 | 三值 × 阈值边界 · 灰度确定性 · `NI-5` 回落 · 参数校验 | [`../../core/internal/director/director_test.go`](../../core/internal/director/director_test.go) |
| 集成 | 端到端改道（配蜜罐后端 + 阈值降至 0，经 `edge/proxy` 观察） | `make dev` |
| 故障注入 | `judge` 出错 / 超时 → 放行 | 单元（替身注入错误） |
| 属性测试 | 不适用（决策是有限分支的纯函数） | —— |
| 分支穷尽性 | 三值分支全覆盖 | `MD-8` |

> 测试只使用替身（`stubJudge` / `stubThresholds` / `stubGray`），不依赖 `judge` / `policy` 真实实例（`MD-22`）。

## 8. 未决项

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | `severity` 档位 | 加重处置能力（当前恒 `none`） | [`../design/terminology.md`](../design/terminology.md) §4.2 · `TM-13` / `MD-24` |
| 2 | 后端池选择与健康检查 | 多后端改道 | 阶段 3（依赖蜜罐池） |
| 3 | `whitelist` 消费（`INT-25`） | 核心侧白名单前置 | 本轮未接（边缘侧已由适配器实现）；另开一条 |
| 4 | `block` 开关进配置 | 运维可编排 | 当前是构造参数；与后端池一起进 `config` |
| 5 | `guard.false_route_budget` 消费 | 误调度率护栏 | 与观测报表一起 |

## 9. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 创建（阶段 2a）：纯阈值三值 + 灰度 + 接缝替换 | 6 项决策经用户确认（[`../plans/2026-09-17-director-2a.md`](../plans/2026-09-17-director-2a.md)） |
| 2026-09-18 | **实现 `MD-25`**：诱饵前缀集豁免 block（诱饵面 observe-only）；前缀集由装配层从诱饵资产汇总 | 本轮开发 |
