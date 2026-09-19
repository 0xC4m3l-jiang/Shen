"""跨语言事件契约：Python 侧必须能解析**核心真实写出**的判定事件载荷。

契约正文：`docs/spec/events.md`。
夹具：`api/telemetry/v1/testdata/decision_event.json`（由 Go 结构体生成）。
本用例的存在理由：L4 曾按自造字段名解析，导致运行时取到事件却一条也解析不出（自洽但错）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false

from __future__ import annotations

import json
import pathlib

from analysis.events import parse_all
from analysis.telemetry import InMemoryTelemetry, WireEvent
from analysis.worker import run_once

FIXTURE = (
    pathlib.Path(__file__).resolve().parents[2]
    / "api"
    / "telemetry"
    / "v1"
    / "testdata"
    / "decision_event.json"
)


def load_fixture() -> dict:
    return json.loads(FIXTURE.read_text(encoding="utf-8"))


def test_fixture_exists_and_is_the_go_shape() -> None:
    payload = load_fixture()
    assert set(payload) == {
        "decision_id",
        "source_ip",
        "method",
        "path",
        "user_agent",
        "action",
        "severity",
        "backend",
        "score",
        "signals",
        "at",
    }, "夹具键集变了：Go 侧结构体与本文档/Python 解析需同步（docs/spec/events.md）"


def test_observation_parses_real_payload() -> None:
    obs = parse_all([load_fixture()])[0]
    assert obs.event_id == "d-9f3c1a2b", "decision_id 必须映射为证据 ID"
    assert obs.source == "203.0.113.9"
    assert obs.method == "GET"
    assert obs.path == "/.git/config"
    assert obs.user_agent == "HeadlessChrome/120"
    assert obs.action == "route_origin"
    assert obs.signals == ("ua-headless", "path-probe")
    assert obs.at == "2026-09-19T10:00:00+08:00"


def test_worker_analyzes_real_payload() -> None:
    """端到端（进程内）：真实载荷 → 去重通过 → 产出结论 → 上报。"""
    payload = load_fixture()
    port = InMemoryTelemetry(
        [
            WireEvent(
                event_id=payload["decision_id"],
                event_type="decision",
                payload=json.dumps(payload).encode("utf-8"),
                created_at=payload["at"],
            )
        ]
    )
    run = run_once(port, now="2026-09-19T10:00:30+08:00")
    assert run.fetched == 1 and run.admitted == 1, (
        "真实载荷必须能被解析并进入分析（曾在此处全落空）"
    )
    assert [c["kind"] for c in run.conclusions] == ["intent", "strategy"]
    assert run.conclusions[0]["evidence_ids"] == ["d-9f3c1a2b"]
    assert run.reported == 2


def test_untrusted_block_keeps_raw_fields() -> None:
    """`AR-31`：送模型的原始观测**原样保留**（不做净化丢弃）。"""
    obs = parse_all([load_fixture()])[0]
    wire = obs.to_untrusted()
    assert wire["decision_id"] == "d-9f3c1a2b"
    assert wire["path"] == "/.git/config"
    assert wire["signals"] == ["ua-headless", "path-probe"]
