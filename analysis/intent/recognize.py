"""意图识别（`intent`）—— 把观测映射为攻击意图类别。

判定只回答「风险多大」（`judge`，`AR-2`）；本模块回答「**想干什么**」，属近线/离线分析。
输出是**结构化结论**（意图 + 置信度 + 证据引用），供 `chain` / `strategy` 消费。
"""

from __future__ import annotations

import re
from collections.abc import Iterable, Sequence
from dataclasses import dataclass

from analysis.events import Observation
from analysis.llm.contract import Field, Schema, validate
from analysis.llm.envelope import Envelope, accept, reject
from analysis.llm.limits import Truncation, truncate_list

CATEGORIES = (
    "reconnaissance",
    "exploitation",
    "lateral_movement",
    "exfiltration",
    "persistence",
)
"""五类意图；**禁止**新增第六类（`AR-16` 的契约由 `INTENT_SCHEMA` 守住）。"""

INTENT_SCHEMA = Schema(
    name="intent",
    fields=(
        Field("category", (str,), True, 32),
        Field("confidence", (float, int), True),
        Field("evidence_ids", (list,), True, 256),
        Field("rationale", (str,), True, 500),
    ),
)

_PATTERNS: tuple[tuple[str, re.Pattern[str]], ...] = (
    (
        "reconnaissance",
        re.compile(r"\.git|\.env|\.svn|/admin|/wp-login|/phpmyadmin|/actuator", re.I),
    ),
    ("exploitation", re.compile(r"union\s+select|\.\./|/shell|/exec|<script|/upload", re.I)),
    ("lateral_movement", re.compile(r"/ssh|/rdp|/smb|/docker|/k8s|/jenkins|/gitlab", re.I)),
    ("exfiltration", re.compile(r"/etc/passwd|/shadow|\.sql|dump|/backup|\.zip$", re.I)),
    ("persistence", re.compile(r"crontab|/authorized_keys|/startup|/systemd|\.bashrc", re.I)),
)


@dataclass(frozen=True)
class IntentRuleHit:
    category: str
    event_id: str
    pattern: str


def rule_hits(observations: Iterable[Observation]) -> list[IntentRuleHit]:
    """确定性规则命中（不依赖模型，可离线复核）。"""
    hits: list[IntentRuleHit] = []
    for obs in observations:
        haystack = f"{obs.path} {obs.method} {obs.user_agent} {' '.join(obs.signals)}"
        for category, pattern in _PATTERNS:
            if pattern.search(haystack):
                hits.append(
                    IntentRuleHit(category=category, event_id=obs.event_id, pattern=pattern.pattern)
                )
    return hits


def recognize(observations: Sequence[Observation]) -> Envelope:
    """按规则给出意图；证据不足时**拒绝**（不猜，`AR-15`）。"""
    hits = rule_hits(observations)
    if not hits:
        return reject("无任何意图规则命中：证据不足，不臆测意图（AR-15）")

    tally: dict[str, list[str]] = {}
    for hit in hits:
        tally.setdefault(hit.category, []).append(hit.event_id)

    category = max(tally, key=lambda name: len(tally[name]))
    evidence = tally[category]
    confidence = min(1.0, len(evidence) / max(1, len(observations)))
    truncations: list[Truncation] = []
    try:
        payload = validate(
            {
                "category": category if category in CATEGORIES else "reconnaissance",
                "confidence": round(float(confidence), 3),
                "evidence_ids": truncate_list(evidence, field="evidence_ids", log=truncations),
                "rationale": f"命中 {len(evidence)} 条规则（{category}）",
            },
            INTENT_SCHEMA,
        )
    except ValueError as exc:
        return reject(f"意图结论不合契约：{exc}")
    payload["truncations"] = [item.to_wire() for item in truncations]
    return accept(payload)
