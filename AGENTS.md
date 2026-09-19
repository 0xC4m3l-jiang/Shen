# 项目强制规范

**本文件对本项目内的一切改动具有约束力。** 与 [`docs/design/`](docs/design/README.md) 冲突时，**以 `design/` 为准**。

**动手前做两件事：**

1. 读 [`docs/design/`](docs/design/README.md) **全部八份** —— 技术基线：架构 · 接入 · 语言 · 模块 · 目录 · 硬约束 · 术语。
2. 确认待定项不阻塞你：**项目名未定**（[ADR-0004](docs/background/decisions/0004-terminology.md)），它卡住建仓库、二进制名、服务名。

**现状**：153 条规则已确认 · 阶段 1（MVP）已实现（6 模块 + 门禁）· 开发循环见 §4。

> 本文件只放「每次都必须在场」的规则。它每次会话都进上下文 —— **不得写长**。

---

## 1. 文档分层

**必须遵守**：

| 目录 | 是什么 |
| --- | --- |
| `docs/design/` | **已确认**的规则 —— 开发不得偏离 |
| `docs/modules/` | 模块设计（一模块一文件；清单在 [`design/modules.md`](docs/design/modules.md) §1.1） |
| `docs/spec/` | 契约与字典（[`config.md`](docs/spec/config.md) · `dependencies.md` 已建；`logs` / `metrics` 待写） |

**仅供参考，不具约束力**：

| 目录 | 是什么 |
| --- | --- |
| `docs/kb/` | FAQ · 已知问题 · 踩坑。可随时增改 |
| `docs/plans/` | 变更包（设计访谈 + 追溯矩阵 + 证据）。**待确认提案，不作实现依据** |
| `docs/log.md` | 变更日志：每轮一条（做了什么 / 改了哪些文件 / 验证 / 证据 / 遗留） |
| `docs/background/` | ADR · 讨论稿 · 调研 |
| `.pi/skills/` | 本项目 Agent 工作流（通用规范在全局 `~/.pi/agent/skills/`）—— **开工前必须加载 `dev-loop`**，见 §4 |
| `.pi/devloop.md` | **项目适配面**：文档路径 / 验证命令 / 追溯工具 / 编号体系（技能 `dev-loop` §1 的字段） |

`background/` 三个子目录：`decisions/`（ADR：候选 · 理由 · **失效条件**）· `notes/`（讨论，**不得**当已定事实）· `research/`（证据）。

## 2. 三个禁止

| # | 禁止 |
| --- | --- |
| P-1 | **禁止**把未确认内容写进 `docs/design/` —— 它是「已拍板的事实」，不是「整理好的想法」。讨论与提案留在 `docs/background/notes/` |
| P-2 | **禁止**在 `docs/design/` 用含糊表述 —— 只用**必须 / 禁止 / 应当**，不得出现「可能 / 大概 / 建议后续 / 考虑 / 或许」；每条规则**必须可验证** |
| P-3 | **禁止**实现与 `docs/design/` 冲突的代码 —— 发现冲突时**停下报告**，不得改 `design/` 迁就实现 |

## 3. 规则升格与失效

**升格**（`background/notes/` → `design/`）—— 仅在**用户明确确认**后执行，**Agent 不得自行升格**：

1. 在 `docs/design/` 写规则并分配 ID（如 `AR-1`）
2. 在 `docs/background/notes/` 原处标注「**已升格至** `design/xxx.md#ID`」
3. 更新 [`docs/design/README.md`](docs/design/README.md) §1 索引

**失效**（`design/` → `background/decisions/`）—— 三种，**都必须保留 ID 与正文**：

| 情形 | 做法 |
| --- | --- |
| **重开** —— 规则不再成立、新方案未定 | 开提议 ADR + 标「重开中」，效力暂停 |
| **暂缓** —— 用户要求推迟 | **连原方案也不作依据**，只保留占位 |
| **废弃** —— 被新规则取代 | 登记到 [`design/README.md`](docs/design/README.md) §4.1，**禁止**静默删除 |

详见 [`docs/background/decisions/README.md`](docs/background/decisions/README.md) §3。

## 4. 开发循环（设计 → 开发 → 验证 → 提交）

**每次动手都走这四段，不许跳段。**

| 段 | 何时进入 | 动作 | 产物落在哪 |
| --- | --- | --- | --- |
| **① 设计访谈** | 需求有歧义、或涉及取舍时 | 用 `grill_deck` 把**本轮能问的全部问题**一次问完（编号 + 给出推荐答案）；**事实自己查，别问用户**；未达成共识前不动手 | 实施计划 → [`docs/plans/`](docs/plans/README.md) |
| **② 设计定稿** | 访谈达成共识后 | 决定经用户确认才升格；有真实取舍的写 ADR | 规则 → `docs/design/` · 取舍 → `docs/background/decisions/` |
| **③ 开发** | 设计定稿后 | 先声明改动范围；只做被要求的最小实现；不替用户决定选型；不加未要求的接口或依赖 | 模块文档 + 代码 |
| **④ 验证** | 开发完成后 | 跑 `make gate`（含 `make trace`）与 `make dev`；效果类或对抗性结论另按技能 `evidence-and-decisions` §3 走 | 门禁输出 · 基准数据 |
| **⑤ 收尾提交** | 验证绿、记录写完之后 | `make done MSG="…"`：**门禁 → 提交 → 校验工作区干净**（一条命令跑完三段） | git 提交记录 |

**四条不能破**：

1. ①→② 的升格**必须经用户确认**（§3）—— **Agent 不得自行升格**
2. ③ 之前先确认待定项不阻塞你（见文首「动手前做两件事」）
3. ④ **不绿就不算完成** —— `make gate` 失败时**禁止**声称完成，也**禁止**绕过检查
4. ⑤ **必须提交** —— 每轮收尾跑 `make done MSG="…"`；**没有提交就没有回退点**（曾发生过工具被误删、只能从会话记录考古的事）

> `grill_deck` 只在交互式终端可用；非交互时退化为 markdown 提问（同样按轮次 + 编号 + 推荐答案）。用 `/grill` 重开上一轮复核或改答案。

**每轮必须留下六样**（= 全局技能 `dev-loop` 的四类产出 + 本条验收命令；形状与逐个动作见 [`.pi/skills/dev-loop-project/SKILL.md`](.pi/skills/dev-loop-project/SKILL.md)）：

| # | 产出 | 要求 |
| --- | --- | --- |
| 1 | **变更包** `docs/plans/YYYY-MM-DD-<主题>.md` | 照 [`_change-package.md`](docs/plans/_change-package.md)：需求 → 设计逻辑 → **追溯矩阵**（规则 ↔ 文档章节 ↔ 代码 ↔ 测试）→ 代码 → 场景表 → 证据 |
| 2 | **变更日志** [`docs/log.md`](docs/log.md) 追加一条 | 做了什么 · 改了哪些文件 · 对应文档 · 验证 · 证据 · 遗留（最新在最上面） |
| 3 | `make gate` | 已含 `make trace`：模块文档↔代码↔单测 · 规则 ID 引用存在性 · 变更包与日志 |
| 4 | `make dev` | 效果验证的关键行贴进变更包 §6（能反驳「跑了吗」即可，不需全文） |
| 5 | **审视**（仅 L 档） | 用全局技能 `audit`（`~/.pi/agent/skills/audit/SKILL.md`）对着本轮 diff 核文档、删无用、标废弃；审视表进变更包 §7.1 |
| 6 | **提交** | `make done MSG="<一句话主题>"`（门禁 → 提交 → 校验工作区干净）；提交信息写清「做了什么」，让 `git log` 能当索引读 |

> 追溯检查发现的**已知缺口**必须写进 [`scripts/tracecheck/allow.txt`](scripts/tracecheck/allow.txt) 并说明理由；
> 豁免过期会被门禁报出来 —— 不允许僵尸条目。

**技能是强制的，不是参考** —— 每轮开发开工前**必须**先加载 [`dev-loop`](~/.pi/agent/skills/dev-loop/SKILL.md)（通用流程）与 [`dev-loop-project`](.pi/skills/dev-loop-project/SKILL.md)（本项目实例化），
并先读 [`.pi/devloop.md`](.pi/devloop.md)（本项目适配面：路径 / 命令 / 依据）；
未加载即视为**流程未执行**，该轮不算完成。动代码前另按 [`dev-loop-project`](.pi/skills/dev-loop-project/SKILL.md) §1/§2 过开工门禁与范围声明；L 档收尾按 [`audit`](~/.pi/agent/skills/audit/SKILL.md) 做审视。

> 项目实例化技能叫 `dev-loop-project`：pi 同名时保留先加载的（全局先于项目），两个都叫 `dev-loop` 会让项目那份永远不会被加载。

**本项目的技能（共 3 个，全部自建 Markdown，无可执行代码）**：

| 技能 | 何时用 | 产物 |
| --- | --- | --- |
| [`dev-loop-project`](.pi/skills/dev-loop-project/SKILL.md) | **每轮开工前**（强制） | 开工门禁四查 · 范围声明 · 追溯与验证入口 · DoD |
| [`evidence-and-decisions`](.pi/skills/evidence-and-decisions/SKILL.md) | 要调研找依据 · 有真实取舍 → ADR · 要声称「有效」→ 基准 | 证据分级材料 · ADR（含失效条件）· 基准包（含对照组） |
| [`threat-model`](.pi/skills/threat-model/SKILL.md) | 改安全相关路径 / 信任边界 / 对手可见面 | 资产 · 对手分档 · 失效路径 · 识破路径表 |

> 全局另有 `dev-loop`（通用流程）与 `audit`（审视）—— 见 `~/.pi/agent/skills/`。
> 项目 `.pi/skills/` 里**只有纯 Markdown 技能，无可执行代码**（第三方 `skill-creator` 已移除，见 [`docs/kb/removed-skills.md`](docs/kb/removed-skills.md)）。
> 技能**只在 cwd 位于 `Shen/` 或其子目录时生效**（见 §6）；从父目录启动 pi 时，本文与项目技能都不加载。
> `make trace` 会核对：本文点名的每个技能文件**必须**存在且头部有 `name` / `description`（`DEV-3`）。

## 5. 已确认约束摘要

**绝不能违反的**：

| 摘要 | 详见 |
| --- | --- |
| **不影响原始业务** —— `NI-1` 优先级高于本项目其他一切目标；任何未识别状态**必须**回落**放行到真实业务** | [`design/constraints.md`](docs/design/constraints.md) |
| **禁止**实现控制面（C2 / 载荷 / 木马 / 向第三方下发指令） | 同上，SB-1 / SB-2 |
| 对外可见面**禁止**出现 `honeypot` / `蜜罐` / `蜜饵` / `投毒` 等词 —— 判据：**会不会出现在攻击者的屏幕上** | 同上，OH-1 / OH-2 |
| **判定与响应生成必须只在核心实现一次**；L1 与适配器**禁止**实现判定逻辑 | [`design/architecture.md`](docs/design/architecture.md) 的 AR-2 / AR-5 |

**形态与选型**：

| 摘要 | 详见 |
| --- | --- |
| 产品 = **一个独立服务 + 接入物料**。四种接入形态：旁路镜像（唯一不在请求路径上的）· DNS 引流 · 反向代理前置 · Sidecar。**不是 SDK**，不是应用内中间件 | [`design/integration.md`](docs/design/integration.md) |
| 任何接入的**首次上线必须影子模式**（只观测、不处置） | 同上，INT-11 |
| **决策取值 = 三值**（`route_origin` / `route_mirage` / `block`），**禁止**第四值；加重用 `severity` **旁路字段**，**禁止**弹挑战页 | [`design/terminology.md`](docs/design/terminology.md) §4 |
| 分层多语言：L0 不自研 / **L1 与核心 Go** / L2·L4 **Python** / 控制台 TypeScript。上限 **5 种**（Lua 已由 [ADR-0008](docs/background/decisions/0008-edge-language-go.md) 移除，实际 4 种） | [`design/language.md`](docs/design/language.md) |

## 6. 位置约束（勿重复踩坑）

pi 只检查 **cwd 及其祖先目录**下的 `AGENTS.override.md` / `AGENTS.md` / `CLAUDE.md`。

- ✅ `Shen/AGENTS.md` —— cwd 在 `Shen/` 或其子目录时**会被加载**
- ❌ `Shen/.pi/AGENTS.md` —— **从不**被加载（`.pi` 不在搜索路径内），仅作人工入口
- 在父目录 `code/` 启动 pi 时，**本规范与 `.pi/skills/` 都不生效**

## 7. 文档导航（只放指针）

| 我要做的事 | 去哪 |
| --- | --- |
| 写 / 改代码（动手前必读） | [`docs/design/`](docs/design/README.md) 八份全部 → `docs/modules/<模块>.md` |
| 挑一个模块来开发 | [`docs/progress.md`](docs/progress.md) §1（完成度）· [`docs/modules/README.md`](docs/modules/README.md) §4（每个模块插在哪 · 建议顺序） |
| 查背景：为什么这么定 / 对手是谁 / 找证据 | [`background/decisions/`](docs/background/decisions/README.md) · [`notes/threat-model.md`](docs/background/notes/threat-model.md) · [`research/`](docs/background/research/README.md)（引用**必须先核验**） |
| 遇到没见过的问题 | [`docs/kb/known-issues.md`](docs/kb/known-issues.md) |
| 不知道看哪 | [`docs/README.md`](docs/README.md) |

**改文档时会碰到的三条**：

1. **`docs/design/` 只收已确认内容**（P-1）—— 升格必须经用户确认，**Agent 不得自行升格**
2. **规则失效不能直接改掉** —— 先开 ADR，标「重开中」或「暂缓」，**保留 ID 与正文**（§3）
3. **新调研一律进 `docs/background/research/`**，登记到材料清单（行数 · 性质 · **证据级**）；未核验的引用**不得**写入结论
