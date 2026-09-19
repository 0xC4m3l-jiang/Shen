# 变更包 · 2026-09-18 · 变更日志改名 Log.md → log.md（小写）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 变更日志文件 `docs/Log.md` 改名 `docs/log.md`，同步全部活文档引用与 tracecheck 硬编码 |
| 日期 | 2026-09-18 |
| 状态 | 已实现 |
| 涉及模块 | 无（文档与工具命名） |
| 决策数 | 已答 1 项（全小写，历史快照保留原写法）/ 待定 0 项 |
| 关联 | [`../log.md`](../log.md) 同日条目 |

## 1. 需求与验收

**要解决什么**：用户要求变更日志不叫 `Log.md`，改成小写 `log.md`。

**做完之后，用户能做什么 / 看到什么**：`docs/log.md` 是变更日志，全仓库活文档统一引用小写名。

**验收判据**：

1. `docs/log.md` 存在，`docs/Log.md` 不存在。
2. tracecheck、`.pi/devloop.md`、全部活文档引用新名。
3. 历史快照（`docs/plans/2026-09-18-*.md` 具体变更包、`log.md` 旧条目）保留原写法。
4. `make gate` 全绿。

**不做什么**：

- 不改历史变更包与 log.md 旧条目里的「Log.md」（audit 门槛③：历史快照不动）。
- 不改全局技能 `~/.pi/agent/skills/dev-loop/SKILL.md` 里的 `docs/Log.md` 默认值示例（那是跨项目默认，非本项目路径）。

## 2. 设计逻辑

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 文件名大小写 | 全小写 `log.md` | 用户明确要求 | `docs/log.md` |
| ② | 历史快照里的旧名 | 保留「Log.md」原写法 | audit 门槛③：历史记录是当时快照 | `docs/plans/2026-09-18-*.md` · `log.md` 旧条目 |

**接缝与接口**：tracecheck 硬编码 `docs/Log.md`（5 处，含 `isHistorical` 判断）→ `docs/log.md`；`.pi/devloop.md` 的 `change_log` 字段同步。

## 3. 追溯矩阵

| 规则 / 约定 | 位置 | 说明 |
| --- | --- | --- |
| `change_log` 字段（dev-loop §1） | [`.pi/devloop.md`](../../.pi/devloop.md) | 适配面指向新路径 |
| `isHistorical`（audit 排除项） | [`scripts/tracecheck/main.go`](../../scripts/tracecheck/main.go) | log.md 仍视为历史文件，TC-3 不查它的链接 |

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `docs/Log.md` → `docs/log.md` | 改名 | 用户要求 |
| `scripts/tracecheck/main.go` | 改 | 5 处硬编码路径与 `isHistorical` 判断 |
| `.pi/devloop.md` | 改 | `change_log: docs/log.md` |
| `AGENTS.md` · `docs/README.md` · `docs/progress.md` · `docs/modules/README.md` · `.pi/skills/dev-loop-project/SKILL.md` · `docs/plans/_change-package.md` · `docs/plans/README.md` · `docs/kb/dev-workflow.md` | 改 | 活文档引用与链接 |

## 5. 测试与场景

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | 新文件存在 | `docs/log.md` 存在、`docs/Log.md` 不存在 | ✅ | `ls docs/log.md` |
| 2 | 活文档无旧名 | 活文档不再写 `Log.md` | ✅ | grep 仅剩历史快照 |
| 3 | 门禁认新路径 | `make trace` 通过 | ✅ | 见 §6 |

## 6. 验证证据

```console
$ make gate
门禁通过。
```

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 历史变更包与 log.md 旧条目仍写「Log.md」 | 无（历史快照，TC-3 不查） | 保留 |

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 变更日志改名 log.md | 用户要求 |
