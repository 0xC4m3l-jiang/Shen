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
from analysis.worker import RULES_GENERATOR, Checkpoint, EvidenceCache, run_once


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
    # 会话数**不超过** `MAX_SESSIONS_PER_RUN`：截断会让游标停在未分析事件之前（另有专测），
    # 这里要钉的是纯游标机制本身。
    events = [
        decision(f"e-{n}", "/.git/config", session=f"s-{n}", at=f"2026-09-19T10:{n:02d}:00+08:00")
        for n in range(8)
    ]
    port = RecordingTelemetry(events)
    checkpoint = Checkpoint()

    first = run_once(port, checkpoint=checkpoint, now="2026-09-19T10:10:00+08:00")
    assert first.fetched == 8, "首轮（无游标）看整个窗口的判定事件"
    assert first.cursor == "", "首轮没有游标"
    assert first.committed_cursor == "2026-09-19T10:06:59+08:00", "游标 = 最新判定 - 回看重叠（1s）"
    assert checkpoint.since == first.committed_cursor

    second = run_once(port, checkpoint=checkpoint, now="2026-09-19T10:10:30+08:00")
    assert port.list_calls[1] is not None, "第二轮必须把游标作为 since 传给遥测面"
    assert second.fetched <= 1, f"第二轮只该重取回看重叠内的那条判定，实际 {second.fetched}"
    replayed = [
        e for e in port.list_events(limit=200, since=checkpoint.since) if e.event_type == "decision"
    ]
    ids = [e.event_id for e in replayed]
    assert len(replayed) == 1, f"游标之后只剩回看重叠内的那条判定，实际 {ids}"
    assert second.reported == 0, "重取到的那条不得重复写入结论（AR-11 兜底）"
    # `N8` 之后重叠部分在**本地**就被压掉了（去重状态随游标一起提交）：
    # 不再靠“报了再被核心幂等吞”那种白干活的方式去重（也就不再需要模型调用）。
    assert second.suppressed > 0, "重叠部分应由**跨轮复用的去重状态**压掉（N8）"
    assert second.admitted == 0, "没有新态势 ⇒ 不产出结论（AR-15）"


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


class CountingModelClient:
    """最小模型替身：**只数调用**（返回一段固定 JSON，接受与否不影响计数）。

    为什么用它做预算断言：预算关心的是「调用了几次」，不是「模型答得好不好」。
    """

    def __init__(self) -> None:
        self.calls: list[str] = []

    def complete(self, prompt: str, *, session_id: str, timeout: float) -> str:
        del prompt, timeout
        self.calls.append(session_id)
        return json.dumps(
            {"category": "reconnaissance", "confidence": 0.5, "evidence_ids": [], "stages": []}
        )


def test_worker_bounds_model_calls_per_session() -> None:
    """模型调用预算（方案 T23）：每个会话**三步各至多一次**，且调用归到各自会话。

    为什么这条重要：模型路径失败在近线链路里是**设计内的降级**（回落规则版）。
    任何「失败后重试」「为了更准再问一次」都会把一个会话的调用量变成不可预测 ——
    而预算不可预测就意味着成本与延迟不可预测（近线同样是生产资源）。
    """
    port = InMemoryTelemetry(
        [
            decision("e-1", "/.git/config", session="s-1"),
            decision("e-2", "/.git/config", session="s-2", at="2026-09-19T10:00:01+08:00"),
        ]
    )
    client = CountingModelClient()
    run = run_once(port, now="2026-09-19T10:00:30+08:00", client=client)

    assert len(run.sessions) == 2
    assert len(client.calls) == len(run.sessions) * 3, (
        f"每个会话至多三次模型调用（意图/链/策略），实际 {len(client.calls)} 次"
    )
    assert set(client.calls) == {s.session_id for s in run.sessions}, (
        "每次调用都必须归到某个会话（否则无法按会话核算成本）"
    )
    # 结论一定由**某一条路**产出，且如实标注 —— 不管模型那一路是否被接受（AR-15）。
    for session in run.sessions:
        assert session.intent_generator in (RULES_GENERATOR, "model-v1")
        assert session.intent is not None, "意图结论不得为空（回落也必须产出规则版）"


def test_worker_widens_fetch_until_contiguity_is_proven() -> None:
    """`limit` 只是**起点**：取满就加倍重取，直到取不满。

    ⇒ 这就**证明**了游标与这批之间没有别的判定事件（`N7`）。

    为什么不是「取满就报缺口」：`since` 语义是「该时刻之后」，一次取不满就等价于「取尽了」。
    取满时**不放大**会把本可拿到的事件报成缺口（旧行为），放大后这一段可以被完整分析（新行为）。
    """
    events = [
        decision(f"e-{n}", "/.git/config", session=f"s-{n}", at=f"2026-09-19T10:{n:02d}:00+08:00")
        for n in range(5)
    ]
    port = RecordingTelemetry(events)
    checkpoint = Checkpoint(since="2020-01-01T00:00:00+08:00")
    run = run_once(port, limit=2, checkpoint=checkpoint, now="2026-09-19T10:10:00+08:00")

    assert run.fetched == 5, "取满 limit=2 后应放大重取，最终拿到全部 5 条"
    assert run.contiguous is True and not run.window_gap, "取不满 ⇒ 连续性被证明，没有缺口"
    assert run.completeness == "complete"
    assert checkpoint.since == "2026-09-19T10:03:59+08:00", "证明连续后游标必须前进"


def test_worker_reports_gap_when_contiguity_cannot_be_proven() -> None:
    """证明不了连续（顶到硬顶仍取满）⇒ 如实报「完整性未知」且**不提交游标**（`N7`）。"""

    class AlwaysFullTelemetry(InMemoryTelemetry):
        """永远返回**刚好** limit 条：模拟「窗口里的事件比硬顶还多」。"""

        def list_events(self, *, limit: int = 200, since: str | None = None, event_type: str = ""):
            events = super().list_events(limit=limit, since=since, event_type=event_type)
            return list(events)[:limit]

    from analysis.worker import CURSOR_MAX_LIMIT

    events = [
        decision(
            f"e-{n}", "/.git/config", session=f"s-{n}", at=f"2026-09-19T10:{n % 60:02d}:00+08:00"
        )
        for n in range(CURSOR_MAX_LIMIT)
    ]
    port = AlwaysFullTelemetry(events)
    checkpoint = Checkpoint(since="2020-01-01T00:00:00+08:00")
    run = run_once(port, limit=8, checkpoint=checkpoint, now="2026-09-19T10:10:00+08:00")

    assert run.contiguous is False
    assert run.window_gap, "证明不了连续必须可见"
    assert run.completeness == "unknown", "完整性未知时不得声称“不丢事件”"
    assert run.advance_blocked, "取不尽时**不得提交游标**（否则缺口会被永久跨过）"
    assert checkpoint.since == "2020-01-01T00:00:00+08:00", "游标必须原样不动"


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


# ── N3 / N7 / N8：游标与恢复契约 ────────────────────────────────────────────


def test_n3_cursor_is_committed_only_after_reporting() -> None:
    """上报失败 ⇒ **不提交游标**（`N3`：原缺陷是“最早推进、最晚失败”= 永久丢事件）。"""

    class ReportFailsTelemetry(InMemoryTelemetry):
        def report(self, _event: WireEvent) -> tuple[int, int]:
            raise TelemetryUnavailable("模拟核心写侧不可用")

    events = [decision("e-1", "/.git/config")]
    port = ReportFailsTelemetry(events)
    checkpoint = Checkpoint(since="2026-09-19T09:00:00+08:00")
    run = run_once(port, checkpoint=checkpoint, now="2026-09-19T10:00:30+08:00")

    assert run.errors, "上报失败必须如实记录"
    assert checkpoint.since == "2026-09-19T09:00:00+08:00", "没上报成功就不得推进游标"
    assert run.advance_blocked, "为什么没提交必须可见（不是静默不动）"
    assert not run.committed_cursor


def test_n3_truncation_stops_cursor_before_unanalyzed_events() -> None:
    """会话上限截断 ⇒ 游标停在**未分析事件的最早一条之前**（不越过、也不停摆）。"""
    from analysis.worker import MAX_SESSIONS_PER_RUN

    total = MAX_SESSIONS_PER_RUN + 2
    events = [
        decision(f"e-{n}", "/.git/config", session=f"s-{n}", at=f"2026-09-19T10:{n:02d}:00+08:00")
        for n in range(total)
    ]
    port = InMemoryTelemetry(events)
    checkpoint = Checkpoint()
    run = run_once(port, checkpoint=checkpoint, now="2026-09-19T10:30:00+08:00")

    assert run.truncated_sessions == 2
    # 新到旧排序 ⇒ 被留下的是**最新**的 8 个会话（s-9 … s-2），被截断的是最旧的 s-1、s-0 ——
    # 其中最早的事件是 10:00:00；游标只能停在它之前（09:59:59），否则那两条永远不会被分析。
    assert checkpoint.since == "2026-09-19T09:59:59+08:00", (
        f"游标必须停在最早未分析事件之前，实际 {checkpoint.since}"
    )
    assert not run.known_loss, "能部分推进时不算“已知丢弃”"
    assert run.completeness == "complete", "提交的那一段确实处理干净了"


def test_n3_known_loss_is_recorded_when_progress_is_impossible() -> None:
    """截断且天花板给不出前进量 ⇒ 只能跨过，但**必须显式记为已知丢弃**（不是静默丢）。"""
    from analysis.worker import MAX_SESSIONS_PER_RUN

    total = MAX_SESSIONS_PER_RUN + 2
    events = [
        decision(f"e-{n}", "/.git/config", session=f"s-{n}", at=f"2026-09-19T10:{n:02d}:00+08:00")
        for n in range(total)
    ]
    port = InMemoryTelemetry(events)
    # 游标已经停在 09:59:59（上一轮按天花板停下的位置）⇒ 天花板（10:00:00）给不出前进量。
    checkpoint = Checkpoint(since="2026-09-19T09:59:59+08:00")
    run = run_once(port, checkpoint=checkpoint, now="2026-09-19T10:30:00+08:00")

    assert run.truncated_sessions == 2
    assert run.known_loss, "跨过未分析事件必须留痕（已知丢弃）"
    assert run.completeness == "unknown", "已知丢弃 ⇒ 完整性未知"
    assert checkpoint.since > "2026-09-19T09:59:59+08:00", "但不能因此永远停摆"


def test_n8_dedupe_state_is_reused_across_rounds() -> None:
    """去重状态随游标提交（`N8`）：重叠窗口不再重新调模型、也不再重复产出结论。"""

    class CountingClient:
        def __init__(self) -> None:
            self.calls: list[str] = []

        def complete(self, _prompt: str, *, session_id: str, timeout: float) -> str:
            del timeout
            self.calls.append(session_id)
            return json.dumps(
                {"category": "reconnaissance", "confidence": 0.5, "evidence_ids": [], "stages": []}
            )

    events = [decision("e-1", "/.git/config", session="s-1", at="2026-09-19T10:00:00+08:00")]
    port = InMemoryTelemetry(events)
    checkpoint = Checkpoint()
    client = CountingClient()

    first = run_once(port, checkpoint=checkpoint, client=client, now="2026-09-19T10:00:30+08:00")
    calls_after_first = len(client.calls)
    assert calls_after_first > 0 and first.admitted == 1

    # 第二轮：游标回看会**重叠**取到同一条态势。
    second = run_once(port, checkpoint=checkpoint, client=client, now="2026-09-19T10:00:40+08:00")
    assert second.suppressed > 0, "重叠部分应由跨轮复用的去重状态压掉"
    assert second.admitted == 0 and not second.conclusions
    assert len(client.calls) == calls_after_first, "被压掉的态势不得再调模型（N8）"


def test_n7_bad_events_are_counted_not_silently_dropped() -> None:
    """解析不出 `event_id` 的坏事件要计数（`N7`：坏事件与“没事件”不是一回事）。"""
    good = decision("e-1", "/.git/config")
    bad = WireEvent(
        event_id="e-bad",
        event_type="decision",
        session_id="s-1",
        payload=json.dumps({"path": "/admin"}).encode("utf-8"),  # 没有 event_id 字段
        created_at="2026-09-19T10:00:01+08:00",
    )
    run = run_once(InMemoryTelemetry([good, bad]), now="2026-09-19T10:00:30+08:00")
    assert run.fetched == 2
    assert run.dropped_events == 1, "坏事件必须可见"
