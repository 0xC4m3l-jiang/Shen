"""`llm-components`（L4）：契约 / 超时 / 内容纪律与间接注入防护。

规则落点：`AR-15`…`AR-24` · `AR-31` · `AR-32`（见 `docs/modules/llm-components.md`）。

**本包不在 `__init__` 里做再导出** —— 消费方直接从具体子模块导入，例如：

- 契约与信封：`llm.contract` · `llm.envelope`
- 解析与长度：`llm.extract` · `llm.limits`
- 超时与收尾：`llm.twophase`
- 内容与资源：`llm.blacklist` · `llm.prompts`
- 注入与执行面：`llm.untrusted` · `llm.client`
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。
