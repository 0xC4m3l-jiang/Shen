"""`ai-capability`（L4）：**可开关的共享 AI 生成出口** —— 唯一出口 + 强制护栏。

规则落点：`AR-33`（本模块存在的理由）·
`AR-15` / `AR-22` / `AR-23` / `AR-24` / `AR-31`（护栏与提示词纪律）·
`AR-30`（生成必须确定性）· `AR-32`（无执行面）·
`MD-3` / `MD-5`（契约只在 `../../docs/spec/ai-contract.md`）。
决策与失效条件见 `../../docs/background/decisions/0023-deception-content-injection.md`
与 `../../docs/background/decisions/0025-generic-guardrailed-outlet.md`。

**本包不在 `__init__` 里做再导出** —— 消费方直接从具体子模块导入。

## 内核 / 插件的边界（`ADR-0025` 决定 1）

**内核**（不认识任何具体任务 —— 加一个消费方时这些文件**一行都不该改**）：

| 文件 | 是什么 |
| --- | --- |
| `aicap/service.py` | 唯一出口 `generate()` + 内核 `run_task()` + `Artifact` / `Sink` 两个缝 |
| `aicap/model.py` | 模型接缝（取客户端 + 核对无执行面，`AR-32`） |
| `aicap/tasks/_registry.py` | 任务登记表（每个 `kind` 的必填声明见契约 §1.3） |
| `aicap/guardrail/prompts.py` | 前置护栏：三段式提示词（指令区 / 画像区 / **不可信数据区**） |
| `aicap/guardrail/inspect.py` | 后置护栏：结构 → 黑名单 → 长度 → 风格 |

**插件**（`kind=content` 这一种消费方的私有实现）：

| 文件 | 是什么 |
| --- | --- |
| `aicap/content.py` | 内容对象 + 一致性键 + 内容清单（契约 §2 / §3） |
| `aicap/tasks/content.py` | `kind=content` 的 schema / 护栏档案 / produce / build |
| `aicap/__main__.py` | 离线生成器（内容专用 CLI：`python -m analysis.aicap`） |
| `aicap/resources/prompts/*.md` | 提示词资源（随版本分发，`AR-24`） |

**判据**：把插件那一列整块删掉，内核这一列仍应原样可用（只有「注册表为空」会让启动期断言失败 ——
那正是它该失败的地方）。**接入一个新消费方 = 加 `produce`/`build` + 一条登记**，内核不改。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。
