# 文档导航中枢

**唯一入口。** 回答「什么时候看哪个目录下的哪份文档」。

> 🚪 **三个最快入口**：
> ① 想**快速了解项目并定位能力点** → [`kb/quick-tour.md`](kb/quick-tour.md)（速览 · 能力点定位表 · 五分钟跑起来 · 定位三招）
> ② 想**跑起来 / 验证 / 排障 / 升级** → [`ops/runbook.md`](ops/runbook.md)（`scripts/shen.sh up|verify|traffic`）
> ③ 想**知道某能力在哪个模块、怎么接** → [`modules/_map.md`](modules/_map.md)
> ④ 想**顺着一条请求读懂整体逻辑**（判定 / 决策 / 处置 / 观测 / L4 / 策略面） → [`logic.md`](logic.md)
>
> 想先知道「它是什么 / 效果什么样」→ 看根 [`README.md`](../README.md)。
> 本文件是**开发与设计**的导航：规则在哪、模块文档在哪、每轮的改动怎么复核。

**现状**（2026-09-19）：153 条规则已确认 · 阶段 1（MVP）完成 · **阶段 2a 完成**（判定 → 决策 → 引流闭环）·
**策略面（`S4`）已落地**（核心 → 适配器下发改道后端表 / 白名单 / 注入规则）· **L4 已接入近线 worker** · **控制台可用**（只读观测）。
**未接通的**：诱饵资产与预生成响应正文到边缘的通路；蜜罐只做接入架构（内容待专项调研）。
**能力缺口清单**（查询串不参与判定等）见 [`ops/functional-verification.md`](ops/functional-verification.md) §2。

> ✅ **策略面（`api/policy/v1`）已落地（2026-09-19）**：`Pull` 轮询 + `Ack` 回执，下发改道后端表与白名单（[ADR-0018](background/decisions/0018-policy-plane-pull-model.md)）。
> 🟡 **仍未接通的是处置内容**（诱饵资产 / 预生成响应 / 注入片段）—— 需先在核心侧定下它们的来源与归属。
> 详见 [`modules/README.md`](modules/README.md) §0.4 与 [`design/structure.md`](design/structure.md) §1.6.4。

**动手前**：读 [`design/`](design/README.md) 八份 → 确认待定项不阻塞你 → 按 [`../AGENTS.md`](../AGENTS.md) §4 的开发循环走。

> 强制规范（三个禁止 · 文档分层 · 开发循环 · 约束摘要）→ [`../AGENTS.md`](../AGENTS.md)
> 技术基线（8 份 / 153 条规则）→ [`design/README.md`](design/README.md)
> 背景材料（ADR · 讨论稿 · 调研）→ [`background/README.md`](background/README.md)

---

## 0. 项目是什么

面向自主渗透 Agent 的**欺骗引擎**：不拦截，而是把已识别的自动化对手**透明地**送进蜜罐后端，让它以为自己在打真站。

```text
L0 接入层     流量镜像 · TLS 卸载 · 路由        ← 复用现成组件，不自研
L1 边缘欺骗层  投毒 · 假路径 · 引流执行          ← 只执行处置，不做判定
核心          判定与响应生成的唯一实现           ← Go，无状态多副本
L2/L4        协议仿真蜜罐 · LLM 会话 · 意图分析   ← Python 为主的 AI 层
L3 网络层     蜜网编排 · 微隔离 · 假拓扑
```

**硬需求**：**不影响原始业务**（`NI-1`，最高优先级，由架构机制而非语言提供）。

## 1. 文档地图

**必须遵守**：

| 目录 | 是什么 | 状态 |
| --- | --- | --- |
| [`design/`](design/README.md) | **技术基线**：已确认、无二义、客观的规则 | ✅ 8 份 / 153 条（+ `D-1…D-8`） |
| [`modules/`](modules/) | **模块设计**：一模块一文件；清单在 [`design/modules.md`](design/modules.md) §1.1 | ✅ **23 个有效模块**（§1.1 共 24 行）· 全部有九章文档；**§0 总览含结构图 + 关系图 + 运行时调用链**；完成度见 [`progress.md`](progress.md)（本表不重复计数，避免两处漂移） |
| [`spec/`](spec/) | **契约与字典**：对外 / 跨模块的确定性契约 | 🟡 [`dependencies.md`](spec/dependencies.md)（生成物）· [`config.md`](spec/config.md) · [`policy-payload.md`](spec/policy-payload.md)（边缘策略载荷）✅ 已建；`logs` / `metrics` 待写 |

**仅供参考，不具约束力**：

| 目录 | 是什么 | 状态 |
| --- | --- | --- |
| [`plans/`](plans/README.md) | **实施计划 = 变更包**。**已实现的归档**为一行（[`plans/ARCHIVE.md`](plans/ARCHIVE.md)），本目录只留未实现的；模板 [`_change-package.md`](plans/_change-package.md) | ✅ 现行 0 份 · 归档 53 份 |
| [`progress.md`](progress.md) | **模块规划进度**：全部模块清单 · 实现方式 · 阶段状态 —— 完成度的**唯一维护处**，与 [`log.md`](log.md) 同在 `docs/` 供人工审计对照 | ✅ 已建；每轮开发落地后同步更新 |
| [`log.md`](log.md) | **变更日志**：每轮开发追加一条（做了什么 / 改了哪些文件 / 对应文档 / 验证 / 证据 / 遗留） | ✅ 最新在最上面；`make trace` 核最新条目的形状 |
| [`kb/`](kb/README.md) | **功能知识库**：速览与能力定位 · FAQ · 已知问题（含现行/历史 · 已移除技能）· 开发链路 | ✅ 5 份；新人从 [`kb/quick-tour.md`](kb/quick-tour.md) 开始 |
| [`background/`](background/README.md) | **背景材料**：ADR · 讨论稿 · 调研 | ✅ 3 子目录 / 19 份 |
| [`../.pi/skills/`](../.pi/skills/) | 本项目的 Agent 工作流**实例化**（通用规范在全局 `~/.pi/agent/skills/`）—— 开工前必须加载 `dev-loop`（全局通用）+ [`dev-loop-project`](../.pi/skills/dev-loop-project/SKILL.md)（本项目实例化）；另有 [`evidence-and-decisions`](../.pi/skills/evidence-and-decisions/SKILL.md)（调研/决策/基准）与 `threat-model` | ✅ 3 个（全部自建 · 纯 Markdown，**无可执行代码**） |
| [`../.pi/devloop.md`](../.pi/devloop.md) | **项目适配面**：文档路径 / 验证命令 / 追溯工具 / 编号体系（通用技能 `dev-loop` §1 的字段） | ✅ 已建 |

`background/` 三个子目录 —— 依次回答「为什么」「还没定什么」「凭什么」：

| 子目录 | 回答 |
| --- | --- |
| [`decisions/`](background/decisions/README.md) | **为什么这么定** · 什么条件下会变错（**失效条件**）· **未决项的权威位置在各 ADR 的「未解决」段** |
| [`notes/`](background/notes/implementation-discussion.md) | **还没定什么**（含前置未决项 `D0` 与两个待执行实验 `E1` / `E2`） |
| [`research/`](background/research/README.md) | **凭什么这么定**（证据材料；引用**必须先核验**） |

**读的顺序**：`design/`（8 份全读）→ `background/decisions/` → `background/notes/` → `background/research/`。
**开发时看** `design/`（规则）+ [`progress.md`](progress.md)（进度）+ `modules/`（模块文档）；`background/` 是解释性的，**不要**从它开始读。

### 1.1 门禁与工具（[`scripts/`](../scripts/)）

`make gate` = 下面这些检查 + 全部单测（`-race`）。每一项都对应已确认的规则：

| 工具 / 目标 | 核什么 | 依据 |
| --- | --- | --- |
| `make fmt-check` · `vet` · `staticcheck` · `errcheck` | 格式化 · 静态检查 · 未处理错误 | `TB-14` / `TB-15` |
| `make archcheck` | 顶层目录与跨平面 import · 模块清单一致 · `store` 唯一 I/O 出口 · 语言层数 · 禁 CGO | `ST-1`…`ST-4` · `MD-18`…`MD-20` · `TB-20` / `TB-21` / `TB-24` |
| `make trace` | 模块文档 ↔ 代码 ↔ 单测 · 规则 ID 引用存在性 · 变更包与日志形状 · 过期状态标记 · 悬空链接 | `MD-2` / `MD-17` / `MD-22` · `D-3` / `D-8` · `DEV-1`/`DEV-2` · `TC-2` / `TC-3` |
| `make leakcheck` | 响应面字符串字面量 ↔ 禁用清单 · 决策与分数禁止回传响应头 | `OH-1`…`OH-5` |
| `make licensecheck` | 依赖许可审计（拦 AGPL / SSPL / BSL） | `TB-16` |
| `make dev` | 效果验证：配置干跑（合法 + 非法）→ 起核心 → 在线冒烟 → 规则回放 | `NI-12` 的开发期替身 |
| `scripts/doctor` · `scripts/sentinel` | 接入自检（五项）· 差异哨兵（真实站 vs 幻境逐字段 diff） | ⏳ **尚未实现**（接入演练用） |

> 一图读懂 `make gate` 的链路 → [`kb/dev-workflow.md`](kb/dev-workflow.md) §3。
> 工具的已知缺口一律写在该工具的 `allow.txt` 里并附理由；**豁免过期会被报出来**，不允许僵尸条目。

## 2. 我要做什么 → 去哪

**产研**：

| 我现在的任务 | 先看 | 再看 |
| --- | --- | --- |
| **开始写代码** | [`design/`](design/README.md) 八份全部 | 对应 [`modules/<模块>.md`](modules/)；**先确认待定项不阻塞你**；流程 = 全局技能 `dev-loop` + 适配面 [`../.pi/devloop.md`](../.pi/devloop.md)，产出照 [`plans/_change-package.md`](plans/_change-package.md) |
| **想知道开发规范怎么跑起来** | [`kb/dev-workflow.md`](kb/dev-workflow.md)（三层结构 · 一轮时序 · 门禁与追溯链路图 · 技能路由） | [`../AGENTS.md`](../AGENTS.md) §4 + [`.pi/devloop.md`](../.pi/devloop.md) |
| **复核某轮改了什么** | [`log.md`](log.md)（一轮一条） | [`plans/`](plans/README.md) 的变更包（含追溯矩阵与证据） |
| **挑一个模块来开发** | [`progress.md`](progress.md) §1 完成度 + [`modules/README.md`](modules/README.md) §4 下一步（插在哪 / 前置 / 建议顺序） | [`design/structure.md`](design/structure.md) **§1.6 包级地图 + 接缝** |
| **想知道模块**怎么调用 / 怎么装配 | [`modules/README.md`](modules/README.md) **§0.4 运行时调用链**（谁真的在调用链上） | [`design/structure.md`](design/structure.md) §1.6.1 进程边界 · §1.6.4 接缝现状 |
| **看模块总览（结构 / 关系图）** | [`modules/README.md`](modules/README.md) **§0**（结构图 · 关系图 · 23 模块一句话解释） | [`design/modules.md`](design/modules.md) §1.1（职责权威） |
| **改决策路径** | [`design/constraints.md`](design/constraints.md) 的 `NI-12`（**必须**重跑 `V-1…V-5`） | [`design/architecture.md`](design/architecture.md) §9 未决项 |
| **新增 / 改字段或事件** | [`design/constraints.md`](design/constraints.md) 的 `OH` 段 | [`design/structure.md`](design/structure.md) §3 数据模型 |
| **做技术选型** | [`background/decisions/`](background/decisions/README.md) 有无现成 ADR | [`background/notes/implementation-discussion.md`](background/notes/implementation-discussion.md) §6 议题清单 |

**运维 · 运营 · 接入方**：

| 角色 | 去哪 | 规则源头 |
| --- | --- | --- |
| 运维（部署 / 升级 / 故障 / 容量） | [`ops/runbook.md`](ops/runbook.md)（启动 · 检查 · 修复表 · 更新 · 产物落在哪） | `NI-1`…`NI-14` · `V-1…V-5` |
| 要验证"整体功能有没有问题" | [`ops/functional-verification.md`](ops/functional-verification.md)（跑法 · **缺口清单** · 环境验不了什么） | `NI-1` · `ST-7` · `AR-12` |
| 接入方（站点接进来 / 接完验证） | [`integrate/business-onboarding.md`](integrate/business-onboarding.md)（形态选择 + 最小步骤）· [`integrate/quickstart.md`](integrate/quickstart.md) | `INT-1`…`INT-25` |
| 看告警 / 流量 / 流动 / L4 结论 | [`integrate/observability.md`](integrate/observability.md) | `ST-7` · `AR-10` |
| 人工测试（一轮 15 分钟） | [`ops/functional-verification.md`](ops/functional-verification.md) §7 + [`../scripts/traffic/README.md`](../scripts/traffic/README.md) | — |
| 运营（看识别量 / 判断能否放开误导） | 指标与看板**待建**（`analytics/`）；当前用控制台 + [`spec/logs.md`](spec/logs.md) 的定位手法 | `INT-12` · `SB-1` / `SB-2` |

**任何人**：

| 我现在的任务 | 去哪 |
| --- | --- |
| 想知道**对手是谁 · 我们怎么被识破** | [`background/notes/threat-model.md`](background/notes/threat-model.md)（草案）→ [`background/research/knowledge-base-audit.md`](background/research/knowledge-base-audit.md) 的 G3 |
| 找某条规则 ID 是什么 | [`design/README.md`](design/README.md) §1 的前缀表 → 对应文档的规则表 |

## 3. 面向「跑与用」的目录（⚠️ 原「待建」已在 2026-09-19 建成）

| 目录 | 受众 | 现在有什么 |
| --- | --- | --- |
| [`integrate/`](integrate/README.md) ✅ | 接入方 + 运维 + 做人工测试的人 | `README`（总览）· `quickstart`（5 分钟）· `business-onboarding`（怎么把业务接进来）· `observability`（看告警/流量/流动）|
| [`ops/`](ops/runbook.md) ✅ | 运维 + 任何要验证的人 | `runbook.md`（启动/检查/**修复表**/更新/产物卫生）· `functional-verification.md`（整体功能验证 + **缺口清单** + 环境验不了什么） |
| `analytics/` ⏳ 未建 | 运营 | 待建：`dashboards.md`（每个指标在哪看 + 怎么读 + 异常怎么办）· `playbook.md`（影子 → 放开的晋升判据）；规则来源 `INT-12` · `INT-18` |
| [`integrate/doctor.md`](integrate/doctor.md) ✅ | 接入方 | 接入自检五项（`INT-17`）的用法 · 四种结果状态怎么读 · 与 `traffic`/哨兵的分工（**工具已实现**：`scripts/shen.sh doctor`） |
| `scripts/sentinel` ⏳ 占位 | 接入方 | 差异哨兵（真实业务 vs 幻境逐字段 diff 12 项） |

**✅ 已创建，但文件待补**：

| 目录 | 现状 | 待补 |
| --- | --- | --- |
| [`spec/`](spec/) | [`dependencies.md`](spec/dependencies.md)（生成物）· [`config.md`](spec/config.md) ✅ · [`events.md`](spec/events.md) ✅ · [`policy-payload.md`](spec/policy-payload.md) ✅ · [`logs.md`](spec/logs.md) ✅ | `metrics.md`（每指标口径**含分母** / 单位 / 采集点 / 正常范围 / 越界动作） |
| `kb/` · `plans/` | 已建 | —— |

## 4. 三个容易混的概念

| 概念 | 位置 | 是什么 | 与谁的区别 |
| --- | --- | --- | --- |
| **功能知识库** | [`kb/`](kb/README.md) | **非规范**的沉淀：FAQ · 已知问题 · 踩坑 | 与 `design/` 的区别：**不具约束力**、可随时增改、允许不确定 |
| **日志字典** | [`spec/logs.md`](spec/logs.md) ✅ | 组件 → 日志点 · 逐判定字段表 · 开关（`SHEN_LOG_FORMAT` / `SHEN_PROXY_LOG_REQUESTS`）· 落在哪 · 与事件的区别 · **定位手法** | 日志是内部面（可含判定细节），响应禁止回显（`ST-7`） |
| **运营数据查看方式** | `analytics/`（待建） | 每个指标：口径 → 在哪看 → 正常范围 → 越界找谁 | 含**「怎么看」和「看了怎么办」**，不只是指标定义 |

## 5. 新人上手顺序

```text
1. 本文件 §0 + §1          —— 这是什么、文档在哪、现状到哪
2. design/README.md        —— 规则体系怎么读、哪些已废弃、哪些待定
3. design/ 八份（1→7）      —— 技术基线（必须全部读完）
4. design/modules.md §1.1  —— 权威模块清单：我在写哪个模块、目录在哪、属于哪个阶段
5. background/             —— 为什么这么定 · 还没定什么 · 结论从哪来
6. kb/dev-workflow.md      —— 开发规范与链路图（三层结构 · 一轮时序 · 门禁 · 追溯 · 技能路由）
7. modules/README.md §0    —— 模块总览：结构图 · 关系图 · 运行时调用链（谁真的被调用）
8. design/structure.md §1.6 —— 包级地图：三个进程 · 实测依赖 · 每个模块的对外接口 · 接缝现状
```

## 6. 产品形态（由规则导出）

交付物 = **一个独立服务 + 接入物料**。权威表述在 [`design/integration.md`](design/integration.md)，此处只列否证：

| 候选形态 | 判定 | 依据 |
| --- | --- | --- |
| 应用内 SDK / middleware | ❌ **不是** | `INT-10`（未批准）· `INT-7`（禁止要求客户改业务代码） |
| 业务系统的核心模块 | ❌ **不是** | `NI-7`（必须独立进程运行），不嵌入业务进程 |
| **独立前置服务 / 旁路组件** | ✅ **是** | `INT-1`…`INT-4` · `INT-9` |

