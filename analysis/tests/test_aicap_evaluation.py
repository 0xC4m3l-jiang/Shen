"""`SHEN-M2-03`：五项效果指标的口径与对照报告。

这组用例的核心是**判据本身**：样本不足时不许出比例、没有会话身份的事件不许硬算、
误伤率没有标签就不给数字 —— 否则评估会产出"看起来很精确"的假结论。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import json

import pytest

from analysis.aicap import evaluation as ev


def _ev(session: str, executed: str, path: str, at: str, *, action: str = "", techniques: str = ""):
    payload = {"session_hash": session, "executed": executed, "path": path, "at": at}
    if action:
        payload["action"] = action
    if techniques:
        payload["techniques"] = techniques
    return payload


def _line(ev_obj: dict) -> str:
    return json.dumps(ev_obj, ensure_ascii=False) + "\n"


# ── 五项指标 ─────────────────────────────────────────────────────────────────


def test_counts_rerouted_sessions_and_rates() -> None:
    events = []
    # 10 个被改道会话：前 8 个继续交互（≥2 请求且 ≥3 不同路径），后 2 个只打一枪。
    for i in range(8):
        events.append(_ev(f"s{i}", "mirage", "/index", f"2026-10-14T12:00:0{i}Z"))
        events.append(_ev(f"s{i}", "origin", "/api/a", f"2026-10-14T12:00:1{i}Z"))
        events.append(_ev(f"s{i}", "origin", "/api/b", f"2026-10-14T12:00:2{i}Z"))
    for i in range(8, 10):
        events.append(_ev(f"s{i}", "mirage", "/index", f"2026-10-14T12:01:0{i}Z"))

    m = ev.compute(events)
    assert m.sessions == 10 and m.rerouted_sessions == 10
    assert m.autonomous_entry_rate == pytest.approx(0.8)  # 8/10
    assert m.effective_interaction_rate == pytest.approx(0.8)  # 8/10（≥3 不同路径）


def test_blocked_sessions_are_not_autonomous() -> None:
    events = []
    for i in range(10):
        events.append(_ev(f"s{i}", "mirage", "/index", f"2026-10-14T12:00:{i:02d}Z"))
        events.append(_ev(f"s{i}", "origin", "/api/a", f"2026-10-14T12:01:{i:02d}Z"))
    # 其中 4 个会话被 block 过 ⇒ 不算「自主进入」。
    for i in range(4):
        events.append(_ev(f"s{i}", "block", "/x", f"2026-10-14T12:02:{i:02d}Z", action="block"))

    m = ev.compute(events)
    assert m.autonomous_entry_rate == pytest.approx(0.6)


def test_dwell_and_depth() -> None:
    events = [
        _ev("a", "mirage", "/index", "2026-10-14T12:00:00Z", techniques="T1595"),
        _ev("a", "origin", "/api/a", "2026-10-14T12:00:30Z", techniques="T1595,T1190"),
        _ev("a", "origin", "/api/b", "2026-10-14T12:01:00Z"),
    ]
    m = ev.compute(events)
    assert m.mean_dwell_seconds == pytest.approx(60.0)
    assert m.mean_chain_depth == pytest.approx(2.0)


def test_small_sample_gives_no_ratio() -> None:
    """样本 < 10：比例必须是 None（否则「1/1 = 100%」会被当成结论）。"""
    events = [_ev("a", "mirage", "/index", "2026-10-14T12:00:00Z")]
    m = ev.compute(events)
    assert m.rerouted_sessions == 1
    assert m.autonomous_entry_rate is None
    assert m.effective_interaction_rate is None
    assert m.false_positive_rate is None


def test_events_without_session_are_skipped() -> None:
    events = [
        {"executed": "mirage", "path": "/index"},  # 无 session_hash
        _ev("a", "mirage", "/index", "2026-10-14T12:00:00Z"),
    ]
    m = ev.compute(events)
    assert m.sessions == 1, "没有会话身份的事件不能被算进任何会话"


def test_false_positive_rate_uses_labels() -> None:
    events = []
    for i in range(10):
        events.append(_ev(f"s{i}", "mirage", "/index", f"2026-10-14T12:00:{i:02d}Z"))
    labels = {f"s{i}": i < 2 for i in range(10)}  # 2 个被判为正常用户
    m = ev.compute(events, labels)
    assert m.labelled_sessions == 10
    assert m.false_positive_rate == pytest.approx(0.2)

    # 没标签 ⇒ 不报误伤率（宁可缺，也不要编）。
    assert ev.compute(events).false_positive_rate is None


# ── 对照报告与置信区间 ───────────────────────────────────────────────────────


def test_compare_reports_delta() -> None:
    on = ev.Metrics(
        sessions=20,
        rerouted_sessions=20,
        autonomous_entry_rate=0.8,
        effective_interaction_rate=0.7,
        mean_dwell_seconds=120.0,
        mean_chain_depth=2.0,
        false_positive_rate=0.0,
        labelled_sessions=20,
    )
    off = ev.Metrics(
        sessions=20,
        rerouted_sessions=20,
        autonomous_entry_rate=0.5,
        effective_interaction_rate=0.4,
        mean_dwell_seconds=60.0,
        mean_chain_depth=1.0,
        false_positive_rate=0.0,
        labelled_sessions=20,
    )
    report = ev.compare(on, off)
    assert report["delta"]["autonomous_entry_rate"] == pytest.approx(0.3)
    assert report["delta"]["mean_dwell_seconds"] == pytest.approx(60.0)


def test_wilson_interval() -> None:
    assert ev.wilson(5, 5) is None, "样本不足时不给区间"
    lo, hi = ev.wilson(8, 10) or (0, 0)
    assert 0.0 <= lo < 0.8 < hi <= 1.0


# ── CLI 与容错 ───────────────────────────────────────────────────────────────


def test_load_events_rejects_bad_line(tmp_path) -> None:
    path = tmp_path / "e.jsonl"
    path.write_text('{"executed":"mirage"}\nnot json\n', encoding="utf-8")
    with pytest.raises(ValueError, match="不是合法 JSON"):
        ev.load_events([path])


def test_load_labels_rejects_bad_file(tmp_path) -> None:
    bad = tmp_path / "labels.json"
    bad.write_text("[1,2,3]", encoding="utf-8")
    with pytest.raises(ValueError, match="必须是对象"):
        ev.load_labels(bad)


def test_cli_prints_and_json(tmp_path, capsys) -> None:
    path = tmp_path / "e.jsonl"
    path.write_text(
        "".join(
            _line(_ev(f"s{i}", "mirage", "/index", f"2026-10-14T12:00:{i:02d}Z")) for i in range(12)
        ),
        encoding="utf-8",
    )
    assert ev.main(["--on", str(path)]) == 0
    assert "诱导效果" in capsys.readouterr().out

    assert ev.main(["--on", str(path), "--json"]) == 0
    payload = json.loads(capsys.readouterr().out)
    assert payload["on"]["rerouted_sessions"] == 12
