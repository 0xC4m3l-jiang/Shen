"""模型路径的**候选产出**：让模型把提示词变成文本，再三段式取出 JSON 对象（`AR-17`）。

三个 L4 任务（`intent` / `chain` / `strategy`）的 `produce` 做的事**完全一样** ——
差别只在 schema / 护栏档案 / 输入载荷，而那些都在各自的登记项里声明。

**本模块不做任何校验**：结构由后置护栏的第一关校验（`AR-15`）。
这里只负责「拿到一个对象」；提取失败就抛 `ExtractionError`，
由调用方按失败路径处理 —— **不返回默认值、不返回空对象**。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any

from ...llm.extract import extract_json

if TYPE_CHECKING:  # pragma: no cover - 只用于类型标注，避免运行期循环导入
    from ..model import AnalysisClient
    from ..service import TaskSpec


def structured_candidate(prompt: str, spec: TaskSpec, client: AnalysisClient) -> Mapping[str, Any]:
    """`提示词 → 模型文本 → JSON 对象`（未过护栏的候选）。

    `session_id` 与 `deadline_s` 原样交给客户端：会话标识进数据区（`AR-31`），
    时限用于两阶段收尾（`AR-19`）。
    """
    text = client.complete(prompt, session_id=spec.session_id, timeout=spec.deadline_s)
    return extract_json(text)
