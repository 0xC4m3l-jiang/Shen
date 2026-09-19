"""分析用 LLM 客户端（`AR-32` 的边界所在）。

分析用 LLM **禁止**持有执行能力（不下发指令 / 不生成载荷 / 不调外部系统），只输出**结构化结论**。
本模块只提供「把提示词变成文本」这一件事；**不提供**任何工具、函数调用或副作用入口。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from typing import Protocol, runtime_checkable

FORBIDDEN_SURFACE = (
    "tool",
    "function_call",
    "tools",
    "exec",
    "shell",
    "subprocess",
    "request",
    "http",
    "url",
)
"""**禁止**在分析客户端上出现的成员名 —— 以此守住 `AR-32` / `SB-1` / `SB-2`。"""


class Unavailable(RuntimeError):
    """没有可用的模型后端 —— **必须**显式失败，禁止用模板化文本冒充模型输出（`AR-15`）。"""


@runtime_checkable
class AnalysisClient(Protocol):
    """唯一能力：给定提示词，返回文本。"""

    def complete(self, prompt: str, *, session_id: str, timeout: float) -> str: ...


class UnconfiguredClient:
    """未配置模型时的默认实现：**显式失败**，不猜、不降级。"""

    def complete(self, prompt: str, *, session_id: str, timeout: float) -> str:
        # 入参刻意不用：本实现存在的意义就是「没有后端时显式失败」，而不是产出文本。
        del prompt, session_id, timeout
        raise Unavailable(
            "分析用模型未配置（需在部署侧注入 AnalysisClient）；禁止用固定模板冒充模型输出（AR-15）"
        )


def assert_no_execution_surface(client: object) -> None:
    """接口清单核对（`AR-32`）：客户端不得暴露任何执行类成员。

    用**子串**匹配而不是精确匹配 —— 否则 `execute()` 这类命名会从 `exec` 名单下漏过去。
    """
    members = [name for name in dir(client) if not name.startswith("_")]
    leaked = sorted({name for name in members for bad in FORBIDDEN_SURFACE if bad in name.lower()})
    if leaked:
        raise AssertionError(f"分析客户端暴露了执行能力：{leaked}（AR-32 / SB-1 / SB-2）")
