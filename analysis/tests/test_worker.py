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
from analysis.worker import Checkpoint, EvidenceCache, run_once


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
    # 两个会话 ⇒ 各自一份意图 + 策略（每会话一份结论，不跨会话合并）。
    # 顺序 = **newest first**（与核心 `ListEvents` 同序）：最近活跃的会话先被分析。
    assert [s.session_id for s in run.sessions] == ["s-2", "s-1"]
    kinds = [c["kind"] for c in run.conclusions]
    assert kinds == ["intent", "strategy", "intent", "strategy"], "应逐会话产出意图与策略结论"
    assert run.reported == 4 and run.duplicated == 0
    reported_types = {event.event_type for event in port.reported}
    assert reported_types == {CONCLUSION_EVENT_TYPE}, "结论必须作为 analysis 事件上报"
    intent = run.conclusions[0]
    assert intent["accepted"] is True
    # 关键回归：每个会话的结论**只能**引用本会话的证据（改造前会带上对方的 —— 跨会话串链）。
    assert [c["evidence_ids"] for c in run.conclusions] == [["e-2"], ["e-2"], ["e-1"], ["e-1"]], (
        "结论只引用本会话的证据（AR-12 + 会话分组）"
    )
    # 结论事件的外层 session_id 也要跟着分组，否则控制台/读侧仍看不出归属。
    assert [event.session_id for event in port.reported] == ["s-2", "s-2", "s-1", "s-1"]


def test_worker_groups_by_session_when_same_path_repeats() -> None:
    """同一路径、不同会话 ⇒ **不得**拼成一条结论（FIX-5 的回归）。"""
    port = InMemoryTelemetry(
        [
            decision("e-1", "/.git/config", session="s-1"),
            decision("e-2", "/.git/config", session="s-2", at="2026-09-19T10:00:01+08:00"),
        ]
    )
    run = run_once(port, now="2026-09-19T10:00:30+08:00")
    assert run.admitted == 2, "不同会话的同一路径不是同一态势"
    assert len(run.sessions) == 2
    for session in run.sessions:
        assert {obs.session_id for obs in session.observations} == {session.session_id}
    # newest first ⇒ 先分析 s-2、再 s-1；每个会话的结论只引用自己的证据。
    assert [c["evidence_ids"] for c in run.conclusions] == [["e-2"], ["e-2"], ["e-1"], ["e-1"]]


def test_worker_unknown_identity_is_its_own_group() -> None:
    """没有 session_id 的事件单独成组，**不得**被拱进某个已知会话。"""
    legacy = WireEvent(
        event_id="e-old",
        event_type="decision",
        payload=json.dumps(
            {
                "decision_id": "e-old",
                "at": "2026-09-19T10:00:02+08:00",
                "source_ip": "203.0.113.9",
                "method": "GET",
                "path": "/.env",
                "user_agent": "curl/8",
                "action": "route_origin",
                "severity": "none",
            }
        ).encode("utf-8"),
        created_at="2026-09-19T10:00:02+08:00",
    )
    port = InMemoryTelemetry([decision("e-1", "/.git/config", session="s-1"), legacy])
    run = run_once(port, now="2026-09-19T10:00:30+08:00")
    # newest first ⇒ 未知身份那条（10:00:02）比已知会话（10:00:00）新，因此是第一组。
    assert [s.session_id for s in run.sessions] == ["", "s-1"]
    assert run.sessions[0].unknown_identity is True
    assert run.conclusions[0]["evidence_ids"] == ["e-old"], "未知身份不得混入已知会话的结论"


class RecordingTelemetry(InMemoryTelemetry):
    """记录每次 `list_events` 的 `since`，用来证明「不再每轮重读整个窗口」。"""

    def __init__(self, events: list[WireEvent] | None = None) -> None:
        super().__init__(events or [])
        self.list_calls: list[str | None] = []

    def list_events(self, *, limit: int = 200, since: str | None = None, event_type: str = ""):
        self.list_calls.append(since)
        return super().list_events(limit=limit, since=since, event_type=event_type)


def test_telemetry_double_reads_newest_first() -> None:
    """替身与真服务端**同序**：newest first；同一时间戳按写入序倒排（方案 T23 要求覆盖）。

    为什么单独立一例：这个顺序决定「先分析哪个会话」，早期替身返回 oldest-first，
    于是「测试通过」与「生产行为」是两回事。
    """
    port = InMemoryTelemetry(
        [
            decision("e-old", "/a", session="s-1", at="2026-09-19T10:00:00+08:00"),
            decision("e-new", "/a", session="s-2", at="2026-09-19T10:00:00+08:00"),  # 同时间戳
            decision("e-later", "/a", session="s-3", at="2026-09-19T10:00:05+08:00"),
        ]
    )
    got = [e.event_id for e in port.list_events(limit=10)]
    assert got == ["e-later", "e-new", "e-old"], f"必须 newest first（同时间戳后写入者在前）：{got}"


def test_worker_checkpoint_stops_rereading_the_whole_window() -> None:
    """游标（`FIX-6`）：第二轮只取游标之后的事件，不再重读最近 N 条。

    断言两件可观测的事：`since` 真的传下去了；**判定事件**的重取量从「整个窗口」降到
    「回看重叠内的一条」。重取到的那条由幂等吞掉（`reported` 不增、`duplicated` 增加）
    —— 是「重算但不重复写」，不是静默丢弃。

    注意：worker 自己上报的结论事件也在同一个窗口里（`event_type` 过滤掉），
    所以「取到的条数」不等于「判定事件数」；游标也**只按判定事件**推进（否则会被自己的写入时间推过头）。
    """
    events = [
        decision(f"e-{n}", "/.git/config", session=f"s-{n}", at=f"2026-09-19T10:{n:02d}:00+08:00")
        for n in range(10)
    ]
    port = RecordingTelemetry(events)
    checkpoint = Checkpoint()

    first = run_once(port, checkpoint=checkpoint, now="2026-09-19T10:10:00+08:00")
    assert first.fetched == 10, "首轮（无游标）看整个窗口的判定事件"
    assert first.cursor == "", "首轮没有游标"
    assert checkpoint.since == "2026-09-19T10:08:59+08:00", "游标 = 最新判定 - 回看重叠（1s）"

    second = run_once(port, checkpoint=checkpoint, now="2026-09-19T10:10:30+08:00")
    assert port.list_calls[1] is not None, "第二轮必须把游标作为 since 传给遥测面"
    assert second.fetched <= 1, f"第二轮只该重取回看重叠内的那条判定，实际 {second.fetched}"
    replayed = [
        e for e in port.list_events(limit=200, since=checkpoint.since) if e.event_type == "decision"
    ]
    ids = [e.event_id for e in replayed]
    assert len(replayed) == 1, f"游标之后只剩回看重叠内的那条判定，实际 {ids}"
    assert second.reported == 0, "重取到的那条由幂等吞掉：不得重复写入结论（AR-11）"
    assert second.duplicated > 0, "重复的结论事件应计为 duplicated（可见，不是静默丢弃）"


def test_worker_checkpoint_does_not_advance_on_failure() -> None:
    """游标**只在一轮没有致命错误之后**推进：跳过失败的一轮就再也追不回来了。"""

    class FailingPort(InMemoryTelemetry):
        def list_events(self, *, limit: int = 200, since: str | None = None, event_type: str = ""):
            # 参数与端口协议一致（调用方全部按关键字传），这里引用一下再抛错。
            raise TelemetryUnavailable(f"模拟不可达（{limit}/{since}/{event_type}）")

    checkpoint = Checkpoint(since="2026-09-19T09:00:00+08:00")
    run = run_once(FailingPort(), checkpoint=checkpoint)
    assert run.errors, "遥测不可达必须如实记录"
    assert checkpoint.since == "2026-09-19T09:00:00+08:00", "失败的一轮不得推进游标"


def test_worker_reports_window_gap() -> None:
    """取满 limit 条且最早一条仍晚于游标 ⇒ 中间有事件没取到，必须可见（丢事件不得静默）。"""
    events = [
        decision(f"e-{n}", "/.git/config", session=f"s-{n}", at=f"2026-09-19T10:{n:02d}:00+08:00")
        for n in range(5)
    ]
    port = RecordingTelemetry(events)
    # 游标故意设得很早（模拟「worker 停了很久」）：本轮取满 limit 条，仍看不到游标与它们之间的那段。
    checkpoint = Checkpoint(since="2020-01-01T00:00:00+08:00")
    run = run_once(port, limit=2, checkpoint=checkpoint, now="2026-09-19T10:10:00+08:00")

    assert run.fetched == 2, "取满 limit"
    assert run.window_gap, "应报告窗口缺口（游标之后的事件多于本轮能取的条数）"
    assert "2020-01-01" in run.window_gap and "limit" in run.window_gap


def test_worker_truncates_sessions_visibly() -> None:
    """会话数超过上限时**不静默丢弃**：计数可见，且只分析前 N 个最近活跃的会话。"""
    from analysis.worker import MAX_SESSIONS_PER_RUN

    events = [
        decision(f"e-{n}", "/.git/config", session=f"s-{n}", at=f"2026-09-19T10:00:0{n}+08:00")
        for n in range(MAX_SESSIONS_PER_RUN + 3)
    ]
    port = InMemoryTelemetry(events)
    run = run_once(port, now="2026-09-19T10:00:30+08:00")
    assert len(run.sessions) == MAX_SESSIONS_PER_RUN
    assert run.truncated_sessions == 3, "被截断的会话数必须可见（不是静默丢弃）"
    assert run.admitted == len(events), "去重计数仍覆盖全部事件"


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
    assert first.reported == 4, "两个会话各产两份结论"
    cache = EvidenceCache()
    second = run_once(port, now="2026-09-19T10:00:30+08:00", cache=cache)
    assert second.reported == 0 and second.duplicated == 4, "同窗口重跑不产生重复结论（AR-11）"


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
            # 审计字段：结论是谁产的 + 模型那一路为何没成（AR-33 / AR-15）
            "generator",
            "model_rejected",
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


def test_ar12_evidence_cache_uses_payload_decision_id() -> None:
    """回归：遥测事件 ID（`decision:<decision_id>`）与载荷里的 `decision_id` 不是同一个 ID。

    链引用的是**载荷里的** `decision_id`；缓存必须装它，
    否则 `AR-12` 会误判「证据不存在」并把整条链作废（`make dev` 第 6 步真抓到过）。
    """
    payload = {
        "decision_id": "devcheck-样本名",
        "at": "2026-09-19T10:00:00+08:00",
        "source_ip": "203.0.113.9",
        "method": "GET",
        "path": "/.git/config",
        "user_agent": "HeadlessChrome/120",
        "action": "route_origin",
        "signals": ["ua-headless"],
    }
    port = InMemoryTelemetry(
        [
            WireEvent(
                event_id="decision:devcheck-样本名",  # 外层幂等键
                event_type="decision",
                payload=json.dumps(payload).encode("utf-8"),
                created_at=payload["at"],
            )
        ]
    )
    run = run_once(port, now="2026-09-19T10:00:30+08:00")
    assert run.errors == [], f"不应因证据 ID 命名空间不一致而作废：{run.errors}"
    assert run.chain is not None and run.chain.stages, "链应成形"
    assert run.chain.stages[0].evidence_ids == ("devcheck-样本名",)
