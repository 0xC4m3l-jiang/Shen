"""L4 分析决策层（Python）—— `intent` / `chain` / `strategy` / `llm-components`。

依据 `docs/design/language.md` §1（L4 = Python）与 `TB-2` / `TB-20`。
本层**只产出结构化结论**：不做判定（`AR-2`）、不做决策（`MD-12`）、不做执行（`SB-1` / `AR-32`）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。
