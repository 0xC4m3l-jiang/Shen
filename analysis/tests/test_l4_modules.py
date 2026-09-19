"""`intent` / `chain` / `strategy` 与 `AR-12` / `AR-14`。"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import pytest

from analysis.chain.evidence import InMemoryEvidenceIndex, MissingEvidence, assert_exists
from analysis.chain.reconstruct import reconstruct
from analysis.dedupe import SituationDedupe
from analysis.events import parse_all
from analysis.intent.recognize import CATEGORIES, recognize
from analysis.strategy.generate import StrategyInput, generate
from analysis.strategy.rotate import decide

RAW = [
    {
        "event_id": "e-1",
        "at": "2026-09-19T10:00:00+08:00",
        "source": "203.0.113.9",
        "session_id": "s-1",
        "method": "GET",
        "path": "/.git/config",
        "signals": ["path-git"],
    },
    {
        "event_id": "e-2",
        "at": "2026-09-19T10:00:05+08:00",
        "source": "203.0.113.9",
        "session_id": "s-1",
        "method": "GET",
        "path": "/admin/login",
    },
    {
        "event_id": "e-3",
        "at": "2026-09-19T10:00:09+08:00",
        "source": "203.0.113.9",
        "session_id": "s-1",
        "method": "GET",
        "path": "/etc/passwd",
    },
]


def test_intent_recognizes_and_cites_evidence() -> None:
    obs = parse_all(RAW)
    out = recognize(obs)
    assert out.accepted
    assert out.data["category"] in CATEGORIES
    assert out.data["evidence_ids"], "结论必须带证据引用"
    assert all(eid.startswith("e-") for eid in out.data["evidence_ids"])


def test_intent_rejects_when_no_evidence() -> None:
    benign = [
        {"event_id": "e-9", "source": "x", "session_id": "s", "path": "/pricing", "method": "GET"}
    ]
    out = recognize(parse_all(benign))
    assert not out.accepted
    assert out.rejected_reason is not None
    assert "证据不足" in out.rejected_reason


def test_ar12_evidence_refs_must_exist() -> None:
    index = InMemoryEvidenceIndex(["e-1", "e-2", "e-3"])
    assert assert_exists(index, ["e-1", "e-1", "e-3"]) == ["e-1", "e-3"]
    with pytest.raises(MissingEvidence) as err:
        assert_exists(index, ["e-1", "e-404"])
    assert "e-404" in str(err.value)

    chain_ok = reconstruct(parse_all(RAW), evidence=index)
    assert chain_ok.stages, "有证据时链应成形"
    with pytest.raises(MissingEvidence):
        reconstruct(parse_all(RAW), evidence=InMemoryEvidenceIndex([]))  # AR-12 拦截


def test_chain_stages_ordered_and_broken_signals_detected() -> None:
    many = RAW + [
        {
            "event_id": f"e-{n}",
            "source": "203.0.113.9",
            "session_id": f"s-{n}",
            "method": "GET",
            "path": "/.git/config",
        }
        for n in range(4, 8)
    ]
    chain = reconstruct(parse_all(many))
    names = [s.name for s in chain.stages]
    assert names == sorted(names, key=lambda n: CATEGORIES.index(n)), "阶段必须按链序"
    assert "multi_session_same_method" in {b.kind for b in chain.broken}


def test_ar14_dedupe_keeps_llm_calls_bounded() -> None:
    dedupe = SituationDedupe(window_seconds=60.0)
    key = dedupe.key(source="a", session_id="s", method="GET", path="/.git/config")
    admitted = sum(1 for step in range(1000) if dedupe.admit(key, at=1000.0 + step * 0.01))
    assert admitted == 1, "同一态势 1000 条事件只应触发 1 次（AR-14）"
    assert dedupe.ratio < 0.01
    assert dedupe.admit(key, at=1100.0) is True, "窗口外出现新态势应再触发"


def test_strategy_generates_data_only_within_limits() -> None:
    out = generate(
        StrategyInput(
            intent_category="reconnaissance",
            available_decoys=["fake-gitlab", "fake-jenkins"],
            broken_signals=["skipped"],
        )
    )
    assert out.accepted
    assert out.data["gray_pct"] <= 20, "策略不得建议一次性全量接管（INT-11）"
    assert out.data["threshold_suggestions"]["block"] >= 0.6
    rejected = generate(StrategyInput(intent_category="reconnaissance", available_decoys=[]))
    assert not rejected.accepted, "无可用诱饵时不得凭空生成策略"


def test_decoy_rotation_decision() -> None:
    assert decide(["skipped"]).rotate is True
    assert decide(["hit_without_followup"]).rotate is False
    assert decide(["skipped"], cooldown_active=True).rotate is False
