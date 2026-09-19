"""识破信号识别（[ADR-0016]）—— 判断「诱饵已被识破」。

四类信号：
- `skipped`：诱饵被直接跳过（路过却没碰）；
- `hit_without_followup`：命中后没有后续动作（看了一眼就走）；
- `explicit_compare`：显式比对（同一资源反复取、比对内容差异）；
- `multi_session_same_method`：多会话同法访问（同一手法在多个会话里重复）。

本模块只**识别**；是否轮换变体由 `strategy.rotate` 决定，最终经 `policy` 下发（`AR-32`）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass

from ..events import Observation

MIN_SESSIONS_FOR_SAME_METHOD = 3


@dataclass(frozen=True)
class BrokenSignal:
    kind: str
    detail: str
    evidence_ids: tuple[str, ...]

    def to_wire(self) -> dict[str, object]:
        return {"kind": self.kind, "detail": self.detail, "evidence_ids": list(self.evidence_ids)}


def detect(
    observations: Sequence[Observation], *, decoy_paths: Sequence[str] = ()
) -> list[BrokenSignal]:
    """在观测里找识破信号；输入为空即返回空（不猜）。"""
    if not observations:
        return []
    decoys = tuple(p for p in decoy_paths if p)
    found: list[BrokenSignal] = []

    if decoys:
        probes = [o for o in observations if any(p in o.path for p in decoys)]
        skipped = [o for o in observations if not any(p in o.path for p in decoys)]
        if probes and len(skipped) >= 2 * len(probes):
            found.append(
                BrokenSignal(
                    "skipped",
                    f"诱饵仅被访问 {len(probes)} 次，同期其他路径 {len(skipped)} 次",
                    tuple(o.event_id for o in probes),
                )
            )

    hits = [o for o in observations if "mirage" in o.action or "decoy" in o.action]
    followed = {o.session_id for o in observations if o.action == "route_origin"} & {
        o.session_id for o in hits
    }
    lonely = [o for o in hits if o.session_id not in followed]
    if len(lonely) >= 2:
        found.append(
            BrokenSignal(
                "hit_without_followup",
                f"{len(lonely)} 次命中诱饵后无后续动作",
                tuple(o.event_id for o in lonely),
            )
        )

    repeat: dict[str, list[Observation]] = {}
    for obs in observations:
        repeat.setdefault(f"{obs.session_id}|{obs.path}", []).append(obs)
    compared = [group for group in repeat.values() if len(group) >= 3]
    if compared:
        found.append(
            BrokenSignal(
                "explicit_compare",
                f"{len(compared)} 组同会话同资源被重复取用（疑似比对）",
                tuple(o.event_id for group in compared for o in group),
            )
        )

    by_method: dict[str, set[str]] = {}
    for obs in observations:
        by_method.setdefault(f"{obs.method} {obs.path}", set()).add(obs.session_id)
    same_method = {
        key: sessions
        for key, sessions in by_method.items()
        if len(sessions) >= MIN_SESSIONS_FOR_SAME_METHOD
    }
    if same_method:
        found.append(
            BrokenSignal(
                "multi_session_same_method",
                f"{len(same_method)} 种手法在 ≥{MIN_SESSIONS_FOR_SAME_METHOD} 个会话中重复",
                tuple(o.event_id for o in observations),
            )
        )
    return found
