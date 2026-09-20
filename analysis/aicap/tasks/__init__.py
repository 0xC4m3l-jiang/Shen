"""任务子模块：**任务注册表**（`AR-33` 的结构闸门）。

- `_registry`：`kind` → `Task` 的登记与启动期断言（缺护栏档案即**启动期断言失败**）；
- `content`：`kind=content` 的实现（阶段 A：确定性模板生成器）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。
