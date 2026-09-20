# 变更包：已实现计划归档删除 + kb 历史文档合并

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 逐份核对 `docs/plans/` 的设计是否已实现 → 已实现的**归档为一行并删除文件**（53 份）；把 kb 的历史文档（已移除技能）并入已知问题；索引同步 |
| 日期 | 2026-09-20 |
| 状态 | 已验证（`make gate` 通过；plans 目录 55 → 3；kb 6 → 5） |
| 改动分级 | **M**（文档结构变更；不改代码） |
| 涉及范围 | docs/plans/（归档+删除）· docs/kb/（合并）· docs/README.md · AGENTS.md |
| 关联 | AGENTS.md §4（每轮变更包）· 本文即为本轮的变更包（未归档） |

---

## 1. 判定方法（可复核，不靠印象）

对每份计划核三件事：

1. **带目录前缀的仓库路径**（`core/…` `edge/…` `docs/…` 等）是否全部存在 —— 少数"缺失"经人工确认为
   **计划正文里的反例路径**（如 `core/internal/xxx/`、`docs/zz_probe.md`，用于演示 archcheck 会拦什么）或**已改名/已删除**文件（`docs/modules/map.md` → `_map.md`；`docs/integrate/manual-test.md` 已并入 ops）；
2. 是否有对应的 `docs/log.md` 条目与当时的门禁通过记录；
3. 实现位置能否在 `docs/modules/`、`docs/spec/` 与代码里查到。

**结果**：53 份全部判定为"已实现"。唯一未建的是设计里明确"待建"的 `analytics/`（不是任何一份计划的核心交付，已在 `docs/README.md` §3 登记）。

## 2. 产物

| 动作 | 结果 |
| --- | --- |
| 新增 `docs/plans/ARCHIVE.md` | 53 行归档表（日期 · 原文件 · 主题 · 状态 · 追溯入口）+ 判定依据 + **原文取回方式**（git show） |
| 删除 53 份计划文件 | 目录只剩 `README.md` · `_change-package.md` · `ARCHIVE.md` |
| `docs/kb/known-issues.md` | 新增 `H-1 · 已移除的技能（历史：为什么删 · 怎么恢复）`（并入原 `removed-skills.md` 全文要点） |
| 删除 `docs/kb/removed-skills.md` | kb 6 → 5 份 |
| `docs/kb/README.md` · `docs/kb/dev-workflow.md` · `AGENTS.md` | 引用改指 `known-issues.md` 的 `H-1` |
| `docs/plans/README.md` · `docs/README.md` | 目录策略与索引改准（归档 53 份 / kb 5 份） |

## 3. 追溯矩阵

| 要求 | 落实 |
| --- | --- |
| AGENTS.md §4「每轮留变更包」 | 本轮变更包即本文件（现行未归档）；历史轮次的变更包**归档为一行**，原文可从 git 取回 |
| 门禁 `TC-3`（悬空链接） | 删除后跑 `make trace`，按结果修引用 |
| `D-3`（规则 ID 唯一） | 未新增/改动任何规则 ID |

## 4. 验证证据

```console
$ python3 审计脚本（带前缀路径 + 日志可查）
✅ 判定「已实现且已入日志」：41 份   ⚠️ 需人工看：12 份（逐条确认：反例路径 / 已改名 / 已删除 / 设计待建）
$ ls docs/plans/*.md | wc -l   → 3
$ grep -rn "removed-skills" docs/ AGENTS.md | grep -v docs/log.md  → 仅剩并入正文里的备份目录名
$ make gate → 门禁通过。
```

## 5. 审视记录

| # | 发现 | 类型 | 证据 | 动作 |
| --- | --- | --- | --- | --- |
| 1 | 用"计划自称的状态"判定实现与否不可靠 | 方法缺陷 | 53/53 都写"已验证" | 改用「带前缀路径存在 + 日志可查」两条可核证据 |
| 2 | 早先我把短相对名（`main.go`）当成缺失路径 | 误报 | 抽查显示是正文简写 | 只统计带目录前缀的路径 |
| 3 | 计划正文含**故意的反例路径**（`core/internal/xxx/`） | 干扰项（已知会误判） | 逐条人工确认 | 在本文档写明，避免下次重复误判 |
