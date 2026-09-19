# 变更包 · 2026-09-18 · 进度表迁至 docs/progress.md（与 Log.md 同目录）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 模块规划进度迁出为 `docs/progress.md`，与 `docs/Log.md` 同目录供人工审计 |
| 日期 | 2026-09-18 |
| 状态 | 已实现 |
| 涉及模块 | 全部 21 个有效模块（进度表所在文档迁移，无模块内容变更） |
| 决策数 | 已答 2 项（结构 + 范围，用户确认）/ 待定 0 项 |
| 关联 | `docs/Log.md` 同日条目 · 上一轮 `2026-09-18-progress-overview.md`（§1a 实现方式表随迁） |

## 1. 需求与验收

**要解决什么**：模块规划进度（`docs/modules/README.md`）与变更日志（`docs/Log.md`）分处两个目录，人工审计要跨目录对照。用户要求两者同目录、开发后及时更新。

**做完之后，用户能做什么 / 看到什么**：在 `docs/` 下同时看到 [`progress.md`](../progress.md)（进度）与 [`Log.md`](../Log.md)（日志），一处完成审计对照。

**验收判据**：

1. `docs/progress.md` 承载全部进度内容（模块清单 · 实现方式 · 阶段状态），是完成度**唯一维护处**。
2. `docs/modules/README.md` 瘦身为模块索引，不再保留进度副本（避免两处漂移）。
3. 所有指向「modules/README.md 完成度」的现行文档引用更新为 `progress.md`。
4. `make trace` 零错误（含 `TC-3` 悬空链接、`D-3` 规则 ID）。

**不做什么**：

- 不改任何代码 / 契约 / 模块边界。
- 不动历史快照（`docs/plans/*` 旧变更包 · `docs/Log.md` 旧条目 · `docs/kb/*`）—— audit 门槛 ③。
- 不修 `modules/README.md` §4.2 的 Lua/Rust 过时表述（既有漂移，另开一条）。

## 2. 设计逻辑

**已确认的决策**（来源：用户 2026-09-18 通过结构选择题确认）：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 进度与日志怎么同目录 | 进度独立成 `docs/progress.md`，`Log.md` 不动 | 日志是全局的（不只模块）；进度独立成文件可单一维护、不动 tracecheck 的 Log 路径 | [`../progress.md`](../progress.md) |
| ② | 进度包含哪些 | 全部（模块清单 §1 + 实现方式 §1a + 阶段状态 §2） | 用户确认「全部进度内容」 | 同上 |

**接缝与接口**：无代码接缝。文档接缝 = 「完成度唯一维护处」从 `docs/modules/README.md` 迁到 `docs/progress.md`，引用它的现行文档共 6 处（见 §3）。

**数据流**：审计者 → `docs/` 目录 → `progress.md`（当前进度）+ `Log.md`（每轮做了什么）对照。

## 3. 文档对应（追溯矩阵）

本轮无代码变更，矩阵对齐「约定 ↔ 文档」：

| 约定 / 规则 | 文档章节 | 说明 |
| --- | --- | --- |
| 完成度唯一维护处 | [`../progress.md`](../progress.md) §1 | 从 `modules/README.md` 迁出，避免两处漂移 |
| `MD-2` / `MD-17` / `MD-18` | [`../modules/README.md`](../modules/README.md) §3 | 模块文档规则引用保持 |
| `D-6`（编号不重排） | [`../progress.md`](../progress.md) §1 | 22 行 / 21 有效模块，编号保持 |
| 复用三原则（无 ID） | [`../progress.md`](../progress.md) §1a | 实现方式总览 |
| `AR-3` / `TB-22` / `TB-2` | 同上 | 实现方式依据 |

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `docs/progress.md` | 新增 | 进度唯一维护处，与 `Log.md` 同目录 |
| `docs/modules/README.md` | 改写（瘦身） | 移除进度副本，保留模块索引 · 文档状态 · 下一步 · 新增指南 |
| `docs/README.md` | 改 | 注册 `progress.md`；「完成度见 progress.md」；导航更新 |
| `AGENTS.md` | 改 | §7 导航「挑模块开发」指向 progress.md + modules/README.md §4 |
| `.pi/AGENTS.md` | 改 | 导航补 progress.md |
| `docs/design/structure.md` | 改 | 两处「完成度见 modules/README.md」→ progress.md |
| `.pi/devloop.md` | 改 | `doc_layers` 注册 `docs/progress.md`（spec） |

## 5. 测试与场景

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | 进度表链接不悬空 | `make trace` 无 `TC-3` | ✅ | 见 §6 |
| 2 | 旧「完成度」引用全部更新 | 无现行文档仍指向 `modules/README.md` 完成度 | ✅ | `grep modules/README.md` 仅剩历史快照与 §4/§5 引用 |
| 3 | 规则 ID 引用真实 | 无 `D-3` | ✅ | 见 §6 |

## 6. 验证证据

```console
$ make trace
追溯检查通过。

$ make gate
门禁通过。
```

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | `docs/modules/README.md` §4.2 的 `adapter-reverse-proxy` Lua / `adapter-sidecar` Rust 表述与 ADR-0008（L1 改 Go、③④ 合并）漂移 | 下一步模块建议的表述过时 | 另开一轮修 |

## 7.1 审视记录（L 档）

| # | 发现 | 类型 | 证据（位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- |
| 1 | `modules/README.md` §4.2 Lua/Rust 过时 | 漂移（代码/决策已变，文档未跟） | ADR-0008 + `design/language.md` §1（L1 = Go） | 标遗留，本轮不修（范围外） | 记录在 §7 |
| 2 | `docs/plans/2026-09-18-progress-overview.md`（上轮变更包）写「modules/README.md 新增 §1a」 | 历史快照 | 该文件 §1 | 不动（audit 门槛 ③：记录当时事实，准确） | 保留 |
| 3 | `docs/Log.md` 上轮条目写「modules/README.md（新增 §1a）」 | 历史快照 | Log.md | 不动（同上） | 保留 |

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 进度表迁至 `docs/progress.md`，与 `Log.md` 同目录 | 用户确认（结构 + 范围两项） |
