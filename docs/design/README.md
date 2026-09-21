# docs/design —— 技术基线

> **本目录 = 已确认、无二义、客观的规则。开发必须遵守。**
> 规则总数 **159 条**：`AR` 33 · `INT` 25 · `ST` 24 · `MD` 26 · `NI` 14 · `TB` 12 · `TM` 11 · `SB` 8 · `OH` 5 · `BA` 1；
> 另有本文件的 `D-1…D-8`。（2026-09-20 复核重数：原计数停在 `AR` 29 / `MD` 24，与正文的实际规则条数不符。）
>
> 确认轨迹：
> · **C1–C6 轮**确认 144 条
> · **模块清单轮**追加 `MD-18`（模块必须对应 §1.1 清单的一行）…`MD-22`（每模块有独立可运行的测试套件）
> · **决策取值轮**（[ADR-0002](../background/decisions/0002-decision-model.md) 裁定「三值 + `severity`」）写入 6 条：
> `TM-12`（severity 必须是旁路字段，禁止并入枚举） `TM-13`（severity 档位只能用登记过的值） `MD-23`（severity 的加重必须可归因于环境） `MD-24`（新增）与 `MD-12` `MD-13`（改写）—— ✅ 用户已确认（2026-09-17）
> · **模块确认轮**（用户确认新增 `control` 为第 22 个模块）—— 修 `architecture.md` §10.2 缺口 12 与 `modules.md` §1.1
> · **AI 能力轮**（用户确认新增第 25 个模块 `ai-capability` 与 `AR-33`）—— **任何** LLM 生成必须经护栏出口（口径于 2026-09-20 经用户确认由「欺骗内容」放宽，[ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md)）；见 [`../background/decisions/0023-deception-content-injection.md`](../background/decisions/0023-deception-content-injection.md)
>
> 与代码冲突时以本目录为准；发现冲突时按 [`../../AGENTS.md`](../../AGENTS.md) 规则 **P-3** 停下并报告，
> **不得**修改本目录来迁就实现。

---

## 1. 八份文档（一份一个逻辑）

| # | 文档 | 回答的问题 | 规则前缀 |
| --- | --- | --- | --- |
| 1 | [`architecture.md`](architecture.md) | **架构是什么** —— 分层、核心与适配器、组件、数据流要点、非功能指标；**§7 拓扑视图**（结构：平面 / 接缝 / 闭环 / 数据模型 / 部署）；**§8 请求流视图**（行为：完整旅程 / 四形态差异 / 三条处置分支 / 语言分布 / 模块×步骤 / 时序预算） | `AR` |
| 2 | [`integration.md`](integration.md) | **怎么接进来** —— 接入形态、上线步骤、接入物料、会话身份 | `INT` |
| 3 | [`language.md`](language.md) | **用什么语言** —— 分层语言选型、依据、被否决的候选 | `TB` |
| 4 | [`modules.md`](modules.md) | **有哪些模块、各自能干什么** —— **§1.1 权威模块清单**（25 行 / 24 个有效模块）、职责、边界、依赖方向、阶段、失败模式 | `MD` |
| 5 | [`structure.md`](structure.md) | **代码长什么样** —— 仓库布局、目录结构、命名与分发；**§1.5 已建/未建**；**§1.6 当前实现的包级地图**（进程边界 / 实测依赖 / 接口出口 / 阶段 2 接缝） | `ST` |
| 6 | [`constraints.md`](constraints.md) | **绝对不能违反什么** —— 非侵入性、可观测面卫生、范围边界 | `NI` `OH` `SB` |
| 7 | [`terminology.md`](terminology.md) | **东西叫什么** —— 唯一写法与禁用词 | `TM` |
| 8 | 本文件 | 索引、写作要求、废弃与待定登记 | `D` |

**读的顺序**：先 §2 的写作要求 → 再按 1→7 顺序读。**七份都要读完**才能动手。

---

## 2. 写作要求

- **D-1** 每条规则**必须**用规范性动词：**必须** / **禁止** / **应当**。
- **D-2** 本目录正文**禁止**出现含糊词：「可能 / 大概 / 建议后续 / 考虑 / 或许 / 视情况」。
  **例外**：本文件（规则定义处）或用于**明确禁止**某表述时可引用。
  自查：`grep -rn '可能\|大概\|建议后续\|考虑\|或许\|视情况' docs/design/*.md`（**只允许本文件命中**）
- **D-3** 每条规则**必须**有唯一 ID，格式 `<前缀>-<序号>`，可被代码注释、测试与 ADR 引用。
- **D-4** 每条规则**必须**可验证，并写明验证方式（测试 / lint / 检查项 / 人工核对）。
- **D-5** 本目录**禁止**出现讨论过程与方案对比 —— 那属于 [`../background/notes/`](../background/notes/) 与 [`../background/decisions/`](../background/decisions/)。
- **D-6** 规则 ID **必须**在同一文档内**按序排列**；新增规则**禁止**跳号。
  废弃某条时**必须**保留 ID 并按 §4 登记，**不得**重新编号、**不得**复用该号 ——
  **因此原文档中留空是允许的**（留空 ≠ 缺号）。引用一个已废弃的 ID **必须**同时给出取代者。
- **D-7** **禁止**两条规则使用同一 ID。
- **D-8** 规则 ID 的**定义**用加粗形式；**引用禁止加粗**。同一 ID **必须**只被定义一次 —— 表头小标题也算定义。

---

## 3. 升格与重开

### 3.1 升格（notes → design）

**Agent 不得自行升格。** 仅在用户明确确认后执行：在本目录写入规则并分配 ID → 在 `../background/notes/` 原处标注
「已升格至 `design/<doc>.md#<ID>`」→ 更新本文件 §1 与 §4。

### 3.2 重开（design → decisions）

一条已确认规则**不再成立**时，不能直接改掉：

1. 在 [`../background/decisions/`](../background/decisions/README.md) 开一条**提议**状态的 ADR，把真实候选摆开；
2. 在本目录对应规则后标注 `（重开中 → ADR-NNNN）`，**效力暂停** —— 仍按原文执行，但新增实现**应当**避免依赖它；
3. **保留规则 ID 与正文**（依据 D-6 / D-7）；
4. 用户拍板后：ADR 转「已采纳」→ 更新规则 → 去掉标注；若结论是「维持原判」，删标注并把 ADR 记为「已废弃」并写清为什么不改。

---

## 4. 废弃与待定登记

### 4.1 已废弃的规则（被取代，保留 ID）

| 原 ID | 原内容 | 取代者 | 依据 |
| --- | --- | --- | --- |
| `TB-1` | 全部在线组件**必须**用 Rust 实现 | [`language.md`](language.md) 的分层选型 | [ADR-0005](../background/decisions/0005-layered-languages.md) |
| `TB-3` | **禁止**引入第三种实现语言 | 同上（改为分层多语言，含语言总数上限） | [ADR-0005](../background/decisions/0005-layered-languages.md) |
| `TB-5` | 代码**必须**按 `shen-core` / `shen-control` / `shen-wasm` 三 crate 组织 | [`structure.md`](structure.md) 的仓库布局 | [ADR-0005](../background/decisions/0005-layered-languages.md) |
| `TB-6` | `shen-core` **禁止**依赖 `tokio` / `std::fs` / `std::net` | [`modules.md`](modules.md) 的「核心禁止 I/O」 | 同上 |
| `TB-7` | 判别与调度逻辑**必须**只在 `shen-core` 实现一次 | [`modules.md`](modules.md) 的「核心唯一」 | 同上（保留原则，改换载体） |
| `TB-8` | `shen-control` 与 `shen-wasm` **禁止**互相依赖 | [`modules.md`](modules.md) 的适配器边界 | 同上 |
| `TB-9` | 后端 crate 之间的共享类型**必须**定义在 `shen-core` | [`structure.md`](structure.md) 的契约单一事实源 | 同上 |
| `TB-10` | 插件集合**必须**用 `enum` 分发；**禁止** `dyn Trait` | —— | 同上（Rust 专属约束，随语言变更失效） |
| `TB-11` | 新增插件**必须**更新 `enum` 与全部 `match` 分支 | —— | 同上 |
| `TB-12` | 策略**必须**是数据（配置文件），**禁止**编译进代码 | [`structure.md`](structure.md) 的 `ST-24`（同语义，换到结构文档） | [ADR-0005](../background/decisions/0005-layered-languages.md) |
| `TB-13` | `shen-core` 与 `shen-control` **必须**声明 `#![forbid(unsafe_code)]` | [`language.md`](language.md) 的内存安全规则 | 同上 |
| `TB-17` | 开发内循环**必须**用 `cargo check` | —— | 同上 |
| `TB-18` / `TB-19` | 回退到 Go 的条款与保留约束 | —— | 回退条款**已实现**（就是 ADR-0005），条款本身失去对象 |
| `TM-1` | 术语唯一写法（蜃 / 蜃气 / 海市 / 蜃楼 / 蜃景 / 山市 / 鬼市 / 实景） | [`terminology.md`](terminology.md) 的行业术语体系 | [ADR-0004](../background/decisions/0004-terminology.md) |
| `TM-2` | 三值决策取值**必须**且只能是三个固定值 | [`terminology.md`](terminology.md) §4.1（三值闭集 + `severity` 旁路字段） | [ADR-0002](../background/decisions/0002-decision-model.md) |
| `OH-1` 的部分禁用串 | 针对蜃系术语的 12 个串 | [`constraints.md`](constraints.md) 的 OH-1（已按新术语重算） | [ADR-0004](../background/decisions/0004-terminology.md) |

> **保留有效的旧规则**（均在 `design/` 内有定义处）：`TB-2`（离线必须 Python）、`TB-4`（新增语言层必须先有 ADR）、
> `TB-14`（禁止未处理的错误返回值） / `TB-15` / `TB-16`（质量与安全纪律）、全部 `NI-*` / `OH-*` / `SB-*` / `INT-*` / `TM-3`（禁止为决策值 / 指标名起文学化名称）…`TM-11`（分层名必须写成 L0–L4）。
> 各规则的新归属见对应文档。

#### 4.1.1 `INT-*` 编号重排映射表（⚠️ 必读）

2026-09-17 重构 [`integration.md`](integration.md) 时，`INT-` 前缀被**整套重排**：
接入形态从两种扩为四种，编号随之改变，但**当时未登记映射** —— 导致旧文档里的 `INT-x` 会指向错误的规则。

**读 2026-09-17 之前写的内容时，按下表换算：**

| 旧 ID | 旧含义 | 新 ID | 新含义 |
| --- | --- | --- | --- |
| `INT-1` | ① Envoy ext_proc 过滤器 | `INT-3` / `INT-4` | ③ 反向代理 / ④ Sidecar（Envoy 集成现属此二形态） |
| `INT-2` | ③ 反向代理前置 | `INT-3` | ③ 反向代理前置 |
| `INT-3` | 两形态必须独立工作 | `INT-5` | 四种形态必须各自独立工作 |
| `INT-4` | 未批准形态 ②④⑤ | `INT-10` | ⑤ SDK 未批准（②DNS 与镜像已批准） |
| `INT-5` | 首次上线影子模式 | `INT-11` | 首次上线必须影子模式 |
| `INT-6` | 渐进放开阶梯 | `INT-12` | 渐进放开阶梯 |
| `INT-7` | 越界自动回退 | `INT-13` | 越界自动回退 |
| `INT-8` | 配置生成器 | `INT-16` | 配置生成器 |
| `INT-9` | `shen doctor` 自检 | `INT-17` | `shen doctor` 自检 |
| `INT-10` | 观测报表 | `INT-18` | 观测报表 |
| `INT-11` | 会话身份优先级 | `INT-19` | 会话身份优先级 |
| `INT-12` | 会话身份禁止上行 / 回传 | `INT-20` | 会话身份禁止上行 / 回传 |
| `INT-13` | 同会话幂等 | `INT-21` | 同会话幂等 |
| `INT-14` | 禁止要求客户改业务代码 | `INT-7` | 禁止要求客户改业务代码 |
| `INT-15` | 禁止改写业务侧响应 | `INT-8` | 禁止改写业务侧响应 |
| `INT-16` | 交付物必须是独立服务 | `INT-9` | 交付物必须是独立服务 |
| `INT-17` | 禁止做成 SDK | `INT-10` | ⑤ SDK 未批准 |

> 旧编号不再作为引用形式使用；发现旧编号时按本表换算为现行编号。

### 4.2 待定（占位，后续讨论）

| 项 | 状态 | 位置 |
| --- | --- | --- |
| `severity` 的具体档位 | ⏳ 属**实测问题**（「多狠算不明显」无已知答案） | [`terminology.md`](terminology.md) §4.2 · [ADR-0002](../background/decisions/0002-decision-model.md) |

> ⚠️ 实测确定并登记之前，实现**必须**只支持 `none`（`TM-13` / `MD-24`）。

**已结案**：决策取值（三值 + `severity`）于 2026-09-17 拍板，见 [ADR-0002](../background/decisions/0002-decision-model.md)。
