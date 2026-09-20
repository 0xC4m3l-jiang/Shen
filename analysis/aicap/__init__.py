"""`ai-capability`（L4）：**可开关的共享 AI 能力服务** —— 唯一出口 + 强制护栏。

规则落点：`AR-33`（本模块存在的理由）·
`AR-15` / `AR-22` / `AR-23` / `AR-24` / `AR-31`（护栏与提示词纪律）·
`AR-30`（生成必须确定性）· `AR-32`（无执行面）·
`MD-3` / `MD-5`（契约只在 `../../docs/spec/ai-contract.md`）。
决策与失效条件见 `../../docs/background/decisions/0023-deception-content-injection.md`。

**本包不在 `__init__` 里做再导出** —— 消费方直接从具体子模块导入，例如：

- 唯一出口：`aicap.service`（`generate(TaskSpec) → Envelope`；
  **能力内只有出口与 `aicap.model` 接缝可以 import 模型客户端**，见 `AR-33`）
- 任务注册表：`aicap.tasks._registry`（缺护栏档案即**启动期断言失败**）
- 护栏：`aicap.guardrail.prompts`（前置三段式）· `aicap.guardrail.inspect`（后置四关）
- 内容对象与清单：`aicap.content`
- 离线生成器：`python -m analysis.aicap`
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。
