"""L4 近线 worker：运行时链路 `AR-14` → `AR-12` → `AR-32` → `NI-1`。"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import json

from analysis.telemetry import (
    CONCLUSION_EVENT_TYPE,
    InMemoryTelemetry,
    TelemetryUnavailable,
    WireEvent,
)
from analysis.worker import EvidenceCache, run_once


def decision(
    event_id: str,
    path: str,
    *,
    session: str = "s-1",
    at: str = "2026-09-19T10:00:00+08:00",
    source: str = "203.0.113.9",
    backend: str = "",
) -> WireEvent:
    payload = {
        "event_id": event_id,
        "at": at,
        "source": source,
        "session_id": session,
        "method": "GET",
        "path": path,
        "user_agent": "sqlmap/1.7",
        "action": "route_origin",
        "signals": ["path-probe"],
        "severity": "none",
        "backend": backend,
    }
    return WireEvent(
        event_id=event_id,
        event_type="decision",
        session_id=session,
        payload=json.dumps(payload).encode("utf-8"),
        created_at=at,
    )


def test_worker_produces_and_reports_conclusions() -> None:
    port = InMemoryTelemetry(
        [decision("e-1", "/.git/config"), decision("e-2", "/etc/passwd", session="s-2")]
    )
    run = run_once(port, now="2026-09-19T10:00:30+08:00")
    assert run.fetched == 2 and run.admitted == 2
    kinds = [c["kind"] for c in run.conclusions]
    assert kinds == ["intent", "strategy"], "应产出意图与策略两类结论"
    assert run.reported == 2 and run.duplicated == 0
    reported_types = {event.event_type for event in port.reported}
    assert reported_types == {CONCLUSION_EVENT_TYPE}, "结论必须作为 analysis 事件上报"
    intent = run.conclusions[0]
    assert intent["accepted"] is True
    assert intent["evidence_ids"] == ["e-1", "e-2"], "结论必须带可复核的证据引用（AR-12）"


def test_ar14_worker_dedupes_same_situation_before_analysis() -> None:
    port = InMemoryTelemetry(
        [decision(f"e-{n}", "/.git/config", at=f"2026-09-19T10:00:0{n}+08:00") for n in range(5)]
    )
    run = run_once(port)
    assert run.fetched == 5
    assert run.admitted == 1, "同一态势 5 条事件只应触发 1 次分析（AR-14）"
    assert run.suppressed == 4


def test_worker_is_idempotent_on_rerun() -> None:
    events = [decision("e-1", "/.git/config"), decision("e-2", "/etc/passwd", session="s-2")]
    port = InMemoryTelemetry(events)
    first = run_once(port, now="2026-09-19T10:00:30+08:00")
    assert first.reported == 2
    cache = EvidenceCache()
    second = run_once(port, now="2026-09-19T10:00:30+08:00", cache=cache)
    assert second.reported == 0 and second.duplicated == 2, "同窗口重跑不产生重复结论（AR-11）"


def test_worker_without_events_concludes_nothing() -> None:
    run = run_once(InMemoryTelemetry())
    assert run.fetched == 0 and run.conclusions == [] and run.errors == []


def test_ar12_chain_is_void_when_evidence_missing() -> None:
    events = [decision("e-1", "/.git/config")]
    port = InMemoryTelemetry(events)
    # 故意用一个「没见过任何证据」的缓存：链引用的 ID 都不在遥测里 ⇒ 整条链作废
    run = run_once(port, cache=EvidenceCache())
    assert run.chain is not None and run.chain.stages, "缓存被喂过后证据应齐全"
    from analysis.chain.evidence import InMemoryEvidenceIndex, MissingEvidence
    from analysis.chain.reconstruct import reconstruct
    from analysis.events import parse_all

    obs = parse_all([events[0].json_payload()])
    try:
        reconstruct(obs, evidence=InMemoryEvidenceIndex([]))
    except MissingEvidence as exc:
        assert "e-1" in str(exc)
    else:  # pragma: no cover - 防御性
        raise AssertionError("引用不存在的证据必须失败（AR-12）")


def test_ar32_worker_only_reads_and_reports_structured_conclusions() -> None:
    port = InMemoryTelemetry([decision("e-1", "/.git/config")])
    run_once(port, now="2026-09-19T10:00:30+08:00")
    for event in port.reported:
        payload = event.json_payload()
        assert set(payload) <= {
            "kind",
            "accepted",
            "data",
            "rejected_reason",
            "analyzed",
            "evidence_ids",
            "event_id",
        }, f"结论不得夹带执行类字段：{sorted(payload)}"
        assert not any(key in payload for key in ("command", "payload_b64", "target", "url"))


def test_ni1_worker_degrades_when_telemetry_unreachable() -> None:
    class Dead:
        def list_events(self, **_kwargs: object) -> object:
            raise TelemetryUnavailable("连接被拒")

        def report(self, _event: WireEvent) -> tuple[int, int]:
            raise TelemetryUnavailable("连接被拒")

    run = run_once(Dead())  # type: ignore[arg-type]
    assert run.conclusions == []
    assert run.errors and "遥测不可达" in run.errors[0], "读不到遥测就降级为不分析，不炸（NI-1）"


def test_worker_cli_once_requires_reachable_core() -> None:
    from analysis.worker import main

    # 指向一个没人监听的端口：必须**显式失败**（返回非 0），而不是假装成功
    assert main(["--core", "127.0.0.1:1", "--once"]) != 0
