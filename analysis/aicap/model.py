"""模型接缝（`AR-32` / `AR-33`）。

本模块只做两件事：

1. **复用**分析层的模型客户端契约（`llm.client.AnalysisClient`）——
   **禁止**在本模块重新定义一个同名类型（`MD-5`）；
2. 提供「未配置即显式失败」的默认实现（`llm.client.UnconfiguredClient`），
   并在取用时核对**无执行面**（`AR-32`）。

阶段 A 的生成器是**确定性模板生成器**，不调模型；本接缝是为了让阶段 B 接模型时**只改一处**：
把 `resolve()` 的结果换成真实客户端。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from ..llm.client import (
    AnalysisClient,
    Unavailable,
    UnconfiguredClient,
    assert_no_execution_surface,
)

__all__ = [
    "AnalysisClient",
    "Unavailable",
    "UnconfiguredClient",
    "assert_no_execution_surface",
    "resolve",
]


def resolve(client: AnalysisClient | None) -> AnalysisClient:
    """取本次生成要用的模型客户端。

    - 显式传入的客户端优先（阶段 B 的真实实现；测试用的替身）；
    - 未传入即用 `UnconfiguredClient` —— 调用它会**显式失败**
      （`AR-15`：禁止用模板化文本冒充模型输出）；
    - 无论哪一种，都在这里核对**无执行面**
      （`AR-32`：不下发指令 / 不生成载荷 / 不调外部系统）。
    """
    chosen: AnalysisClient = client if client is not None else UnconfiguredClient()
    assert_no_execution_surface(chosen)
    return chosen
