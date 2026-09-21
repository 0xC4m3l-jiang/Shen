"""意图识别（`intent`）—— 把观测映射为攻击意图类别。

判定只回答「风险多大」（`judge`，`AR-2`）；本模块回答「**想干什么**」，属近线/离线分析。
输出是**结构化结论**（意图 + 置信度 + 证据引用），供 `chain` / `strategy` 消费。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import re
from collections.abc import Iterable, Sequence
from dataclasses import dataclass

from ..events import Observation
from ..llm.contract import validate
from ..llm.envelope import Envelope, accept, reject
from ..llm.limits import Truncation, truncate_list
from ..llm.schemas import CATEGORIES, INTENT_SCHEMA

__all__ = [
    "CATEGORIES",
    "INTENT_SCHEMA",
    "IntentRuleHit",
    "recognize",
    "rule_hits",
]
"""`CATEGORIES` / `INTENT_SCHEMA` 在这里是**再导出**：契约只在 `llm.schemas` 定义一次（`MD-5`）。

为什么定义不在本模块：任务登记项**必须**声明 schema（`Task.schema`），而 `analysis/aicap/**`
只允许依赖 `analysis.llm` 与它自己（`MD-4`）。两边都要用的契约因此只能住在那一层；
本模块与 `chain` 都从那里导入，不各写一份（两处各写一份而不同步的教训见 `spec/events.md` §4）。
"""

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
                # 不再回落：越界由 `INTENT_SCHEMA.category.allowed` 拒绝（`AR-15`）。
                # 被删掉的写法：`category if category in CATEGORIES else "reconnaissance"` ——
                # 那是默认值；今天不可达（`_PATTERNS` 只用五类），接模型后可达。
                "category": category,
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
