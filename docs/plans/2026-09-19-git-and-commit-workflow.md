# 变更包：引入版本控制（git）+ 把「提交」纳入每轮收尾

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | ① 初始化 git 仓库并打**基线提交** ② 新增 `make commit` / `make clean-check` / `make done`，把「提交」变成一轮收尾的**固定一步** ③ 把这条纪律写进工作流四份文档 |
| 日期 | 2026-09-19 |
| 状态 | 已验证（`make done` 跑通：门禁 → 提交 → 工作区干净） |
| 改动分级 | **M**（改工程流程与门禁入口；不改模块代码、不改契约、不改规则） |
| 涉及 | `Makefile` · `AGENTS.md` §4 · [`.pi/devloop.md`](../../.pi/devloop.md) · [`../kb/dev-workflow.md`](../kb/dev-workflow.md) · [`.pi/skills/dev-loop-project/SKILL.md`](../../.pi/skills/dev-loop-project/SKILL.md) · `.git/`（新仓库） |
| 决策数 | 已答 3 项（版本控制 / 收尾命令形态 / 身份设定）· 待定 2 项（远端、提交规范，见 §7） |
| 关联 | 上一轮 [`2026-09-19-revert-python-and-tracecheck-recovery.md`](2026-09-19-revert-python-and-tracecheck-recovery.md)（正是那轮的事故说明为什么必须有回退点）· [`../log.md`](../log.md) |

---

## 1. 需求与验收

**要解决什么**：仓库此前**没有版本控制** —— 后果在上一轮已经真实发生：我在回退 Python 工具链时误删了
`scripts/tracecheck` 的十余个函数，**没有任何回退点**，只能从 pi 的会话记录里考古复原。
用户要求：把仓库变成 git 目录、每轮开发完**必须提交**、让自动化链路闭环。

**做完之后**：每次改动都有提交记录可回退；一轮的收尾是**一条命令** `make done MSG="…"`（门禁 → 提交 → 校验工作区干净）；
「本轮改了哪些文件」不再靠人回忆，`git show --stat` 即可回答。

**验收判据**：

1. `git log` 有基线提交；`git status` 干净。
2. `make clean-check`：**脏**工作区必须失败并列出文件；干净时必须通过。
3. `make commit` 缺 `MSG` 必须失败（不允许「无信息提交」）。
4. `make done MSG="…"`：门禁不过就**不提交**（`make` 遇错即停）；门禁过后自动提交并校验干净。
5. 提交信息含「验证：make gate 通过」，使 `git log` 能当索引读。
6. 四份工作流文档都写明了这一步（`AGENTS.md` §4 · `.pi/devloop.md` · `docs/kb/dev-workflow.md` · 项目技能 §7 DoD）。

**不做什么**：

- **不配置远端**（纯本地仓库；推送到哪、用什么托管由你定）；
- **不引入提交信息规范工具**（commitlint 之类），只用「一句话主题 + 正文」的朴素约定；
- **不加 pre-commit hook**（门禁已由 `make done` 串起来；hook 与 `make gate` 会重复跑）；
- **不改历史**（基线就是基线，不假装这个仓库一直有历史）。

---

## 2. 设计逻辑

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 提交身份 | 设**仓库级** `user.name` / `user.email`（`Shen Dev <dev@shen.local>`） | 本机没有全局身份；仓库级不污染其他项目，且一条命令可改 | `.git/config`（本地，不入库） |
| ② | 提交放在流程哪一步 | **收尾的最后一步**（记录与审视之后） | 提交要包含变更包、日志条目与审视结论；先提交再补记录会出现「记录未入库」的悬空状态 | `AGENTS.md` §4 第 ⑤ 段 |
| ③ | 命令形态 | `make done MSG="…"` = `gate` → `commit` → `clean-check` | 三者是**同一个动作的三段**：不绿不提交、提交完必须干净；拆成三条命令会有人少跑一段 | `Makefile` |
| ④ | `.gitignore` | 上一轮已补（`ST-20`），本轮只做首次提交前的核对 | 密钥类配置与工具产物不得入库 | [`.gitignore`](../../.gitignore) |

**命令设计**：

| 目标 | 行为 | 为什么这样设计 |
| --- | --- | --- |
| `make commit MSG="…"` | 缺 `MSG` → 失败；工作区已干净 → 失败（拒绝空提交）；否则 `git add -A` + 提交，正文固定写「验证：make gate 通过」 | 提交信息是给人读的索引；空提交会污染 `git log` |
| `make clean-check` | `git status --porcelain` 非空 → 失败并列出文件与下一步命令 | 「完成」的判据必须是**客观**的：改动已入库、工作区无残留 |
| `make done MSG="…"` | 依次跑 `gate` → `commit` → `clean-check` | 一条命令闭环；`make` 遇错即停保证了「不绿不提交」 |

**接缝与接口**：`.pi/devloop.md` 新增 `commit_cmd: make done MSG="…"` 与 `commit_only:`，使技能 `dev-loop` 的
「⑤ 记录」段在本项目**落到具体命令**上（该文件是技能的适配面）。

---

## 3. 追溯矩阵

| 规则 / 依据 | 文档位置 | 代码 / 配置 | 验证 | 命令 |
| --- | --- | --- | --- | --- |
| `ST-20`（含密钥配置进忽略清单） | [`../design/structure.md`](../design/structure.md) §1.7 | [`.gitignore`](../../.gitignore) | 首次提交前逐类核对 | `git check-ignore -v deploy/config/config.yaml` |
| 技能 `dev-loop` 的「记录」段（每轮留痕） | 全局技能 §4.2 / §3⑤ | `Makefile` 的 `commit` / `done` | 本轮自身就是证据（先记录后提交） | `make done MSG="…"` |
| `DEV-1` / `DEV-2`（变更包与日志形状） | 技能 `dev-loop` | `scripts/tracecheck` 的 `checkChangePackage` | `make trace` | `make trace` |
| 项目技能 §7 DoD（完成定义） | [`.pi/skills/dev-loop-project/SKILL.md`](../../.pi/skills/dev-loop-project/SKILL.md) §7 | 新增「已提交且工作区干净」勾选项 | `make clean-check` | `make done` |
| `AGENTS.md` §4（开发循环不许跳段） | [`../../AGENTS.md`](../../AGENTS.md) §4 | 新增第 ⑤ 段「收尾提交」+ 四条不能破的第 4 条 | 文档互指一致 | 人工核对 |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `.git/`（`git init -b main`） | 新增 | 提供回退点；分支名用 `main` |
| — | 基线提交 `abf9a34` | 220 个文件：全部文档 · 代码 · 契约 · 部署模板 · 门禁工具 · `.pi/` 项目技能。**基线提交信息里写清了「此前无版本控制」**，不假装有历史 |
| `Makefile` | 改 | 新增 `commit` / `clean-check` / `done` 三个目标（含 `.PHONY`）；**顺带修正**：把四处跨行 `if … then \` 守卫改为单行 —— shellcheck 报 `SC1089` 解析失败（真告警，会让门禁红） |
| [`../../AGENTS.md`](../../AGENTS.md) §4 | 改 | 标题加「→ 提交」；表格加第 ⑤ 段；「三条不能破」→「**四条**不能破」（新增「必须提交」）；「每轮必须留下五样」→「**六样**」（新增第 6 项：提交） |
| [`.pi/devloop.md`](../../.pi/devloop.md) | 改 | 新增 `commit_cmd: make done MSG="…"` 与 `commit_only: make commit MSG="…"`（技能的适配面要落到具体命令） |
| [`../kb/dev-workflow.md`](../kb/dev-workflow.md) | 改 | §2 时序图新增第 ⑦ 步「提交」并说明「没有提交就没有回退点」；§5 入口表补 `make done` / `make clean-check` |
| [`.pi/skills/dev-loop-project/SKILL.md`](../../.pi/skills/dev-loop-project/SKILL.md) §5/§7 | 改 | 验证入口表补两条命令；DoD 增加「本轮改动已提交且工作区干净」勾选项 |

**必须遵守的上位约束**：`ST-20` · `DEV-1` / `DEV-2` · 技能 `dev-loop` §3⑤/§4.2 与 §10 DoD。

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | 首次提交前核对忽略清单 | 密钥类文件被忽略 | ✅ | `git add -A --dry-run` 不含 `*.pem` / `*.key` / `.env` / `deploy/config/config.yaml`；`.bin/` 亦被忽略 |
| 2 | 基线提交 | 有提交、工作区干净 | ✅ | `abf9a34 baseline: 阶段 1 + 2a 完成，2b 策略面与内容通路已落地`；`git status --porcelain` 为空 |
| 3 | `make clean-check`（脏工作区） | **失败**并列出文件 | ✅ | 改 `Makefile` 后：`工作区不干净 —— 这一轮的改动还没提交：` + `M Makefile` |
| 4 | `make clean-check`（干净） | 通过 | ✅ | 基线提交后：`工作区干净：本轮改动都已提交。` |
| 5 | `make commit` 缺 `MSG` | **失败**且不提交 | ✅ | 见 §6 输出（`MSG 是必需的…`） |
| 6 | `make commit` 工作区已干净 | **失败**（拒绝空提交） | ✅ | `没有可提交的改动（工作区已干净）。` |
| 7 | `make done MSG="…"`（本轮全链路） | 门禁 → 提交 → 干净 | ✅ | 见 §6；产生本轮提交 |
| 8 | 提交信息可当索引读 | 含主题 + 「验证：make gate 通过」 | ✅ | `git --no-pager log --oneline` 与 `git show --stat HEAD` |

**没有覆盖的**：

- **远端与推送**：本轮不配远端（纯本地）；
- **多人协作/分支策略**：当前单分支 `main`，未定 PR / 评审流程；
- **提交信息规范强校验**：只要求 `MSG` 非空，不校验格式（如 `type: subject`）；
- **`git bisect` 之类的高级回退手段**：未演练（等真需要时再说）。

---

## 6. 验证证据

```console
$ make commit          # 缺 MSG
用法：make commit MSG="<一句话主题>"
MSG 是必需的：提交信息要让半年后的人看懂这轮干了什么。
make: *** [commit] Error 1

$ make commit          # 工作区已干净
没有可提交的改动（工作区已干净）。
make: *** [commit] Error 1

$ make done MSG="流程：引入 git 版本控制，把提交纳入每轮收尾"
…（fmt · vet · staticcheck · errcheck · archcheck · trace · leakcheck · license · test -race）
门禁通过。
已提交： <sha> 流程：引入 git 版本控制，把提交纳入每轮收尾
工作区干净：本轮改动都已提交。

本轮收尾完成：门禁绿 · 已提交 · 工作区干净。
```

**关键指标**：无版本控制 → **有基线提交 + 每轮提交**；`make done` 一条命令覆盖三段（门禁 / 提交 / 洁净校验）；
工作流四份文档同步（`AGENTS.md` · `.pi/devloop.md` · `docs/kb/dev-workflow.md` · 项目技能）；模块代码改动 **0 行**。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | **未配置远端**（纯本地仓库） | 机器损坏 = 历史丢失；无法协作 | 你定托管与地址后：`git remote add origin <url> && git push -u origin main` |
| 2 | **提交信息无格式规范**（只要求非空） | 长历史后检索不便 | 若要：约定 `type(scope): subject`（`feat/fix/docs/chore`），或引入 commitlint + hook |
| 3 | **未加 pre-commit hook** | 有人可能绕过 `make done` 直接 `git commit` | 可选：hook 里跑 `make clean-check`（但会与 `make gate` 重复跑，需权衡） |
| 4 | **分支/评审策略未定** | 单人开发够用；多人需要 PR 流程 | 到有第二个开发者时定 |
| 5 | 仓库 24 MB（含 `docs/background/research/` 材料） | 首次 clone 稍慢 | 可接受；若将来变大再评估 LFS 或拆分材料目录 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 仓库没有版本控制，而上游文档/技能都假定「改动可追溯」 | **基建缺口** | `git status` → `fatal: not a git repository` | `git init -b main` + 基线提交 | ✅ |
| 2 | `Makefile` 里四处跨行 `if … then \` 守卫被 shellcheck 判为解析失败（`SC1089`） | **门禁红（真告警）** | 编辑 `Makefile` 时的 shellcheck 告警 L41 | 改为单行 `@if … then … fi` | ✅ 门禁绿 |
| 3 | 工作流文档里「记录」段没有**提交**这一步 | 流程缺口 | `AGENTS.md` §4 表格只有 ①…④ 段 | 四份文档同步补上（含 DoD 勾选项） | ✅ |
| 4 | 上一轮的事故（tracecheck 被误删、无从恢复）没有被流程吸收 | 教训未固化 | [`2026-09-19-revert-python-and-tracecheck-recovery.md`](2026-09-19-revert-python-and-tracecheck-recovery.md) §7.1 行 2 | 在 `Makefile` 的目标注释与 `dev-workflow.md` 时序图里写明「没有提交就没有回退点」 | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 引入 git（基线提交 `abf9a34`）· 新增 `make commit` / `make clean-check` / `make done`（门禁 → 提交 → 校验）· 工作流四份文档写入「每轮必须提交」· 修正 `Makefile` 四处跨行 `if` 守卫（shellcheck `SC1089`） | 用户要求（便于管理与回退、每轮提交、链路闭环）· `ST-20` |
