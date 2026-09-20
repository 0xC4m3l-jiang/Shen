"""护栏子模块（`ai-capability` 的内部子模块，**不单独登记为模块**）。

- `prompts`：**前置** —— 三段式提示词（指令区 / 画像区 / **不可信数据区**，`AR-31`）
  + 资源化与启动期断言（`AR-24`）；
- `inspect`：**后置** —— 结构（`AR-15`）→ 黑名单三类（`AR-22`）→
  长度（`AR-23`）→ 风格一致性（`AR-33`）。

两道闸门都在 `service.generate()` 的**内部**执行，调用方无法跳过（`AR-33`）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。
