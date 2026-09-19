# ⚠️ 本文件不会被 pi 自动加载 —— 它只是人工入口

**权威规范是 [`../AGENTS.md`](../AGENTS.md)。** 本文件不复制任何规则，只解释一件事：
为什么它放在这一层无效，以及导航该去哪。

---

## 1. 为什么放在 `.pi/` 这一层无效

pi 的上下文文件发现逻辑（`dist/core/resource-loader.js`）：

```js
const candidates = ["AGENTS.override.md", "AGENTS.md", "AGENTS.MD", "CLAUDE.md", "CLAUDE.MD"];
const filePath = join(dir, filename);
```

`dir` 从 **cwd 逐级向上直到文件系统根**，候选文件名里**没有 `.pi`**：

| 路径 | 何时被加载 |
| --- | --- |
| `Shen/AGENTS.md` | cwd 在 `Shen/` 或其子目录时 ✅ |
| `Shen/.pi/AGENTS.md` | **从不** ❌（`.pi` 不在被搜索的目录名里） |
| `code/AGENTS.md` | cwd = `code/` 时 ✅（但那是工作区级，不属于本项目） |

同理，项目级 skill（`.pi/skills/`）与项目级 settings 也只在 **cwd 位于 `Shen/` 内**时生效，
且首次需要 project trust。

> **结论：必须在 `Shen/` 目录下启动 pi**，本项目的规范与 skill 才生效。
> 在父目录 `code/` 启动时，`AGENTS.md` 与全部 skill **都不会生效**。

## 2. 导航（权威在别处，此处只给去向）

| 我要找 | 去哪 |
| --- | --- |
| 强制规范（三个禁止 · 文档分层 · 开发循环 · 约束摘要） | [`../AGENTS.md`](../AGENTS.md) |
| 文档导航中枢（**唯一入口**） | [`../docs/README.md`](../docs/README.md) |
| 技术基线（**八份 / 153 条规则**） | [`../docs/design/`](../docs/design/README.md) |
| 模块：清单（21 个有效）· 完成度 · 下一步做什么 | [`../docs/design/modules.md`](../docs/design/modules.md) §1.1 · [`../docs/progress.md`](../docs/progress.md) §1 · [`../docs/modules/README.md`](../docs/modules/README.md) §4 |
| 背景：决策记录（**8 条 ADR**）· 未决项 · 对手模型 · 调研证据 | [`../docs/background/`](../docs/background/README.md) |

## 3. 本目录放什么

| 路径 | 内容 |
| --- | --- |
| `skills/` | 本项目专用的 Agent 工作流（**3 个目录**；见本节下方） |
| `devloop.md` | **项目适配面**：开发规范读的路径 / 命令 / 依据 |
| `AGENTS.md` | 本文件：人工入口，**不含规则** |

### skill

本项目有 **3 个技能目录**，**全部是自建 Markdown，无可执行代码**。
权威清单是 pi 的扫描结果（`~/.pi/agent/skills/` 与 `.pi/skills/` 合并）；本表只列「谁管什么」，不重复计数。

**自建（3 个，纯 Markdown，无可执行代码）**：

| Skill | 用途 |
| --- | --- |
| `dev-loop-project` | **本项目的开发规范实例化 + 开工纪律**：开工顺序与门禁 · 范围声明与最小实现 · 追溯检查行为 · 验证入口 · 规则 ID 体系 · DoD 与审视 |
| `evidence-and-decisions` | **拿事实下结论的三段：调研 → 决策（ADR）→ 对抗基准**（含证据分级 A/B/C · 失效条件 · 对照组与可复现包） |
| `threat-model` | 威胁建模，含欺骗类系统专用的**「识破路径」**表 |

**全局技能（不在本目录，对所有项目生效）**：`dev-loop`（通用开发规范）· `audit`（审视）。

> ⚠️ 安全提示：`.pi/skills/` 与全局 `~/.pi/agent/skills/` 合并后，**只有全局 `dev-loop` 带一个可执行脚本**
> （`scripts/devloop-init.sh`：只创建文件，不联网）。第三方 `skill-creator`（248K · 10 个 Python 脚本）
> 已于 2026-09-18 移除；另有 4 个技能同月**并入合并后的技能**。原因与恢复方法均见
> [`../docs/kb/removed-skills.md`](../docs/kb/removed-skills.md)。
