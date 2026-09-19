"""诱饵再生成决策（`strategy`，[ADR-0016]）。

收到识破信号时决定是否轮换变体。**只产出建议**；轮换本身经 `policy` 下发（`AR-32`）。
"""

from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass

ROTATE_SIGNALS = frozenset({"explicit_compare", "multi_session_same_method", "skipped"})
"""触发轮换的信号：这三类说明诱饵**已经被看穿**，继续用同一变体没有情报价值。"""


@dataclass(frozen=True)
class RotationDecision:
    rotate: bool
    reason: str
    signals: tuple[str, ...]

    def to_wire(self) -> dict[str, object]:
        return {"rotate": self.rotate, "reason": self.reason, "signals": list(self.signals)}


def decide(signals: Sequence[str], *, cooldown_active: bool = False) -> RotationDecision:
    """`cooldown_active=true` 时**不轮换**（避免抖动式反复换皮）。"""
    triggered = tuple(sorted(set(signals) & ROTATE_SIGNALS))
    if not triggered:
        return RotationDecision(False, "无识破信号，保持当前变体", ())
    if cooldown_active:
        return RotationDecision(False, "命中信号但处于冷却期，暂不轮换", triggered)
    return RotationDecision(True, f"命中识破信号 {triggered}，建议轮换变体", triggered)
