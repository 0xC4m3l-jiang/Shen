# 已移除的技能（含恢复方法）

> **性质**：知识库（非规范）。用途：回答「这东西以前有、为什么没了、要回来的话怎么做」。
> 依据技能 `audit` 的删除门槛 ④（记录在案：删了什么 · 为什么 · **怎么恢复**）。
> 本文件**只增不删**：移除记录长期保留，即使后来重装。

---

## skill-creator（移除于 2026-09-18）

| 项 | 值 |
| --- | --- |
| 原位置 | `.pi/skills/skill-creator/` |
| 来源 | 第三方（anthropics/skills），许可 Apache-2.0（包内带 `LICENSE.txt`） |
| 体积 | 248K · 18 个文件 · **10 个可执行 Python 脚本**（`scripts/run_eval.py`、`improve_description.py`、`eval-viewer/generate_review.py` 等） |
| 用途 | 新建 / 改良技能、跑评测、比较技能触发准确率 |
| 移除日期 | 2026-09-18 |

### 为什么移除

| # | 理由 | 证据 |
| --- | --- | --- |
| 1 | **体积不成比例**：248K / 18 文件，是其余 6 个项目技能总和的 15 倍 | `du -sh .pi/skills/*/` |
| 2 | **仓库里唯一的第三方可执行代码面**（10 个 Py 脚本 + 一个评测器） | `find .pi/skills -name '*.py'` |
| 3 | **实际未使用**：本项目的 `dev-loop` / `audit` 两个技能都是手写的，没走它 | `docs/plans/2026-09-18-dev-loop.md` §9 / §11 |
| 4 | **只有路由表引用它**：删掉只需改 3 处文字，无任何代码或流程依赖 | `grep -rl skill-creator` → 4 处（含自身） |

移除后：`.pi/skills/` 共 **6 个目录，全部自建 Markdown，可执行代码文件数 = 0**。

### 怎么恢复

**备份仍在**（从仓库移出，未销毁）：

```sh
# 备份目录（248K · 18 文件）
~/.pi/backups/removed-skills/skill-creator-2026-09-18/

# 恢复到项目
cp -R ~/.pi/backups/removed-skills/skill-creator-2026-09-18 .pi/skills/skill-creator

# 或重新安装上游版本（拿最新版）
# 来源：anthropics/skills 的 skill-creator
```

**恢复后必须同步**（否则门禁会报悬空引用或点名缺失）：

1. 在 [`../../AGENTS.md`](../../AGENTS.md) §4 的技能路由注里加回 `skill-creator`；
2. 在全局技能 `~/.pi/agent/skills/dev-loop/SKILL.md` §9 路由表加回一行；
3. 在 [`../kb/dev-workflow.md`](dev-workflow.md) §5 的路由图里加回一个分支；
4. 在 [`.pi/AGENTS.md`](../../.pi/AGENTS.md) 的技能清单里改回数量与安全提示；
5. 跑 `make trace` 确认没有悬空引用。

> ⚠️ 恢复第三方技能前先读它的 `SKILL.md` 与 `scripts/`：它会指示模型执行 Python 脚本，
> 属**首次信任前的必读项**（同 `.pi/AGENTS.md` 的安全提示）。

---

## 并入其他技能的 4 个技能（2026-09-18）

不是「删掉不用」，而是**合并**：内容原封不动地进了更大的技能，只是不再占一个独立入口。
合并的原因：**触发面重叠 + 维护成本**（原文 6 个技能 592 行里有大量重复纪律），
以及 pi 每个技能都要占一条描述（描述越多，触发越模糊）。

| 原技能 | 并入 | 在哪一段 | 备份 |
| --- | --- | --- | --- |
| `spec-first-dev` | [`dev-loop-project`](../../.pi/skills/dev-loop-project/SKILL.md) | §1 开工门禁 · §2 范围纪律 · §8 反模式 | `~/.pi/backups/removed-skills/merged-2026-09-18/spec-first-dev/` |
| `research-survey` | [`evidence-and-decisions`](../../.pi/skills/evidence-and-decisions/SKILL.md) | §1 调研（阶段 0–5 · 证据分级 A/B/C · 输出格式） | `~/.pi/backups/removed-skills/merged-2026-09-18/research-survey/` |
| `design-decision` | 同上 | §2 决策记录（何时写 · 模板 · 纪律） | `~/.pi/backups/removed-skills/merged-2026-09-18/design-decision/` |
| `adversarial-benchmark` | 同上 | §3 对抗性基准（顺序 7 步 · 必答问题 · 报告格式） | `~/.pi/backups/removed-skills/merged-2026-09-18/adversarial-benchmark/` |

**为什么这三个能合**：它们是同一条链的三段 —— 调研给出证据 → 决策把证据变成选择 → 基准证明选择有效，
共用同一批纪律（先定判据 · 引用可核 · 区分自报与实测 · 负面结果要记）。

### 怎么恢复成独立技能

```sh
cp -R ~/.pi/backups/removed-skills/merged-2026-09-18/design-decision .pi/skills/
```

恢复后需要同步（否则门禁会报点名缺失或悬空引用）：

1. [`../../AGENTS.md`](../../AGENTS.md) §4 的技能表加回一行；
2. [`dev-loop-project`](../../.pi/skills/dev-loop-project/SKILL.md) 与 `evidence-and-decisions` 里去掉对应段落的「已并入」说明；
3. 全局技能 `~/.pi/agent/skills/dev-loop/SKILL.md` §9 路由表加回一行；
4. [`dev-workflow.md`](dev-workflow.md) §5 的路由图加回分支；
5. `make trace` 确认无悬空引用。

> **注意**：恢复后不要与 `evidence-and-decisions` 里的同名段落**两处维护** ——
> 要么删掉合并版里的那一段，要么只保留合并版。
