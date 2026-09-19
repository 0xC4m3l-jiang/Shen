"""攻击链还原（`chain`）—— 把零散事件串成可追踪、可回放的阶段序列。"""

from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass

from analysis.chain.broken import BrokenSignal, detect
from analysis.chain.evidence import EvidenceIndex, assert_exists
from analysis.events import Observation
from analysis.intent.recognize import CATEGORIES, rule_hits

STAGE_ORDER = CATEGORIES


@dataclass(frozen=True)
class Stage:
    name: str
    evidence_ids: tuple[str, ...]
    confidence: float

    def to_wire(self) -> dict[str, object]:
        return {
            "name": self.name,
            "evidence_ids": list(self.evidence_ids),
            "confidence": round(self.confidence, 3),
        }


@dataclass(frozen=True)
class Chain:
    stages: tuple[Stage, ...]
    broken: tuple[BrokenSignal, ...]

    def to_wire(self) -> dict[str, object]:
        return {
            "stages": [s.to_wire() for s in self.stages],
            "broken_decoy_signals": [b.kind for b in self.broken],
        }


def reconstruct(
    observations: Sequence[Observation],
    *,
    evidence: EvidenceIndex | None = None,
    decoy_paths: Sequence[str] = (),
) -> Chain:
    """按阶段顺序串链；给了证据索引就**先校验引用**（`AR-12`），不通过即抛异常。"""
    hits = rule_hits(observations)
    by_stage: dict[str, list[str]] = {}
    for hit in hits:
        by_stage.setdefault(hit.category, []).append(hit.event_id)

    stages: list[Stage] = []
    for name in STAGE_ORDER:
        ids = by_stage.get(name)
        if not ids:
            continue
        unique = tuple(dict.fromkeys(ids))
        stages.append(Stage(name=name, evidence_ids=unique, confidence=min(1.0, len(unique) / 3)))

    if evidence is not None:
        for stage in stages:
            assert_exists(evidence, stage.evidence_ids)  # AR-12：引用不存在则整条链作废

    return Chain(stages=tuple(stages), broken=tuple(detect(observations, decoy_paths=decoy_paths)))
