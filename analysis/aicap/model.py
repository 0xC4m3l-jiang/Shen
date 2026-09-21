"""模型接缝（`AR-32` / `AR-33`）。

本模块只做三件事：

1. **复用**分析层的模型客户端契约（`llm.client.AnalysisClient`）——
   **禁止**在本模块重新定义一个同名类型（`MD-5`）；
2. 把**环境变量**翻译成一个真实客户端（`SHEN_AI_KEY` 齐备 ⇒ `DeepSeekClient`），
   缺配置则返回「未配置即显式失败」的默认实现（`llm.client.UnconfiguredClient`）；
3. 取用时核对**无执行面**（`AR-32`）。

**为什么接缝只有这一处**：`AR-33` 的门禁结构检查（`make archcheck` 的 `MD-4` 项）
规定 `analysis/aicap/**` 只允许依赖 `analysis.llm` 与它自己 —— 想再开一条模型调用路径，
会在那道检查上失败。所以「接模型」是改这里一处，而不是在每个消费方各接一次。

**密钥纪律**（`ST-20` / `ST-21`）：key 只从环境变量读，**禁止**入库；
本模块不把 key 写进任何日志、异常或返回值 —— `DeepSeekClient` 的单测钉住这一条。

决策与失效条件：[ADR-0026](../../docs/background/decisions/0026-cloud-model-backend.md)
（云模型后端）· [ADR-0031](../../docs/background/decisions/0031-analysis-reuse-and-model-backend.md)
（L4 复用与模型后端的具体口径）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import os
from collections.abc import Mapping

from ..llm.client import (
    AnalysisClient,
    Unavailable,
    UnconfiguredClient,
    assert_no_execution_surface,
)
from ..llm.deepseek import DeepSeekClient

__all__ = [
    "BASE_ENV",
    "DEFAULT_BASE",
    "DEFAULT_MODEL",
    "DEFAULT_TIMEOUT_S",
    "KEY_ENV",
    "MODEL_ENV",
    "TIMEOUT_ENV",
    "AnalysisClient",
    "Unavailable",
    "UnconfiguredClient",
    "assert_no_execution_surface",
    "from_environment",
    "resolve",
]

KEY_ENV = "SHEN_AI_KEY"
"""模型密钥的**唯一**来源（环境变量；`ST-20` / `ST-21`：禁止入库、禁止落盘）。"""

BASE_ENV = "SHEN_AI_BASE"
MODEL_ENV = "SHEN_AI_MODEL"
TIMEOUT_ENV = "SHEN_AI_TIMEOUT_S"

DEFAULT_BASE = "https://api.deepseek.com"
DEFAULT_MODEL = "deepseek-flash"
DEFAULT_TIMEOUT_S = 30.0


def from_environment(env: Mapping[str, str] | None = None) -> AnalysisClient:
    """按环境变量取客户端：key 齐备 ⇒ 真实客户端；缺 key ⇒ **显式失败**的实现。

    为什么缺 key 不抛异常：`resolve()` 在 `generate()` 内被调用，而 `kind=content`
    的模板生成器根本不调模型 —— 不传客户端也应当能离线生成内容。
    「没有后端」的失败发生在**真的调 `complete()` 时**（`AR-15`：禁止用模板冒充模型输出）。

    `SHEN_AI_TIMEOUT_S` 不是数字或非正时**抛 `Unavailable`** —— 不悄悄换成默认值
    （配置写错却跑得像没事，比直接失败更坏）。
    """
    values = os.environ if env is None else env
    key = values.get(KEY_ENV, "").strip()
    if not key:
        return UnconfiguredClient()
    return DeepSeekClient(
        key=key,
        model=values.get(MODEL_ENV, "").strip() or DEFAULT_MODEL,
        base=values.get(BASE_ENV, "").strip() or DEFAULT_BASE,
        timeout=_timeout_from(values),
    )


def _timeout_from(values: Mapping[str, str]) -> float:
    """读 `SHEN_AI_TIMEOUT_S`；不是数字即**显式失败**（不悄悄换成默认值）。"""
    raw = values.get(TIMEOUT_ENV, "").strip()
    if not raw:
        return DEFAULT_TIMEOUT_S
    try:
        return float(raw)
    except ValueError as exc:
        raise Unavailable(f"{TIMEOUT_ENV}={raw!r} 不是数字（AR-19：配置错误必须可见）") from exc


def resolve(client: AnalysisClient | None) -> AnalysisClient:
    """取本次生成要用的模型客户端。

    - 显式传入的客户端优先（调用方接线；测试用的替身）；
    - 未传入即按环境变量取：齐备 ⇒ `DeepSeekClient`，缺 key ⇒ `UnconfiguredClient`
      —— 调用它会**显式失败**（`AR-15`：禁止用模板化文本冒充模型输出）；
    - 无论哪一种，都在这里核对**无执行面**
      （`AR-32`：不下发指令 / 不生成载荷 / 不调外部系统）。
    """
    chosen: AnalysisClient = client if client is not None else from_environment()
    assert_no_execution_surface(chosen)
    return chosen
