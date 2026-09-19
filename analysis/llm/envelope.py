"""统一信封 `{accepted, data}`（`AR-16`）。

规则：所有 LLM 输出**必须**统一为 `{"accepted": bool, "data": {...}}`；
`accepted=false` **必须**走独立分支并**记录拒绝原因**。
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True)
class Envelope:
    """LLM 输出的唯一对外形状。"""

    accepted: bool
    data: dict[str, Any] = field(default_factory=dict)
    rejected_reason: str | None = None

    def __post_init__(self) -> None:
        if self.accepted and self.rejected_reason is not None:
            raise ValueError("accepted=true 时禁止携带 rejected_reason（AR-16）")
        if not self.accepted and not self.rejected_reason:
            raise ValueError("accepted=false 必须记录拒绝原因（AR-16）")

    def to_wire(self) -> dict[str, Any]:
        out: dict[str, Any] = {"accepted": self.accepted, "data": dict(self.data)}
        if not self.accepted:
            out["rejected_reason"] = self.rejected_reason
        return out


def accept(data: dict[str, Any]) -> Envelope:
    return Envelope(accepted=True, data=dict(data))


def reject(reason: str) -> Envelope:
    if not reason:
        raise ValueError("拒绝原因不得为空（AR-16）")
    return Envelope(accepted=False, data={}, rejected_reason=reason)
