"""长度与列表纪律（`AR-18` / `AR-23`）。

- 列表类字段**必须**有硬截断上限，并**显式记录截断数量**（`AR-18`）；
- 输出**必须**按用途设不同长度上限，各自独立（`AR-23`）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from collections.abc import Iterable, Sequence
from dataclasses import dataclass
from typing import Any

LIST_CAP = 64
"""列表字段硬上限（`AR-18`）。"""

PURPOSE_LIMITS: dict[str, int] = {
    "session_response": 2_000,  # 面向攻击者的会话响应：最短，避免长篇破绽
    "conclusion": 8_000,  # 面向存储的结论
    "intermediate": 20_000,  # 面向分析的中间产物：最长
    "deception_content": 65_536,  # 欺骗内容体（`ai-capability`）：按单条 64 KiB 的清单上限
}
"""分用途长度上限，四个用途**各自独立**（`AR-23`）。"""


@dataclass(frozen=True)
class Truncation:
    """一次截断的**显式**记录（`AR-18`：必须记录数量，禁止静默丢）。"""

    field: str
    kept: int
    dropped: int
    cap: int

    def to_wire(self) -> dict[str, Any]:
        return {"field": self.field, "kept": self.kept, "dropped": self.dropped, "cap": self.cap}


class LimitError(ValueError):
    """用途未知或上限非法 —— 禁止悄悄放行（`AR-23`）。"""


def truncate_list(
    items: Sequence[Any],
    *,
    field: str,
    cap: int = LIST_CAP,
    log: list[Truncation] | None = None,
) -> list[Any]:
    """截断列表；被截掉时**必须**写入 `log`（调用方把 `log` 一起落到结论里）。"""
    if cap <= 0:
        raise LimitError("cap 必须为正（AR-18）")
    kept = list(items[:cap])
    dropped = len(items) - len(kept)
    if dropped > 0:
        if log is None:
            raise LimitError(f"{field}: 发生截断（丢弃 {dropped}）但未提供记录容器（AR-18）")
        log.append(Truncation(field=field, kept=len(kept), dropped=dropped, cap=cap))
    return kept


def cap_text(text: str, *, purpose: str) -> str:
    """按用途截断文本到该用途上限（`AR-23`）；用途必须已登记。"""
    if purpose not in PURPOSE_LIMITS:
        raise LimitError(f"未知用途 {purpose!r}；已登记：{sorted(PURPOSE_LIMITS)}（AR-23）")
    return text[: PURPOSE_LIMITS[purpose]]


def purposes() -> Iterable[str]:
    return tuple(PURPOSE_LIMITS)
