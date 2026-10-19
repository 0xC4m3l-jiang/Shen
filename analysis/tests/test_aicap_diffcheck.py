"""差异化门禁（`SHEN-M5-03`）的用例：指标算法 · 阈值判定 · CLI 退出码 · 画像卫生。

门禁本身必须**可被证伪**：除了「当前仓库能过」，还要证明「越界时真的会失败」——
否则它和没有一样（调阈值就能过，等于假绿）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import json

import pytest

from analysis.aicap import diffcheck

# ── 指标算法 ─────────────────────────────────────────────────────────────────


def test_jaccard_basics() -> None:
    assert diffcheck._jaccard(set(), set()) == 0.0
    assert diffcheck._jaccard({"a"}, {"a"}) == 1.0
    assert diffcheck._jaccard({"a", "b"}, {"b", "c"}) == pytest.approx(1 / 3)


def test_max_pairwise_takes_worst_pair() -> None:
    groups = {"a": {"x", "y"}, "b": {"x", "y"}, "c": {"z"}}
    assert diffcheck._max_pairwise(groups) == pytest.approx(1.0)
    assert diffcheck._max_pairwise({"only": {"x"}}) == 0.0


# ── 当前仓库：真实画像必须过 ────────────────────────────────────────────────


def test_repo_profiles_pass() -> None:
    report = diffcheck.compute(diffcheck.build_samples(diffcheck._discover_pairs()))
    assert report.violations() == [], f"当前画像越界：{report.violations()}"
    # 三项 Jaccard 为 0 是本轮的设计目标（不是"刚好低于阈值"）。
    assert report.metrics["cross_service_term_jaccard"] == 0.0
    assert report.metrics["intra_service_field_jaccard"] == 0.0


def test_sample_bodies_come_from_profiles() -> None:
    samples = diffcheck.build_samples((("site-a", "/api/orders"), ("site-b", "/api/documents")))
    a, b = samples
    assert "Order" in a.body and "order_id" in a.body
    assert "Article" in b.body or "Document" in b.body


# ── 门禁要能证伪：越界必须失败 ───────────────────────────────────────────────


def test_gate_fails_when_threshold_violated(monkeypatch: pytest.MonkeyPatch) -> None:
    """把阈值压到 0（低于基线的 0.15 痕迹率）⇒ 必须判违界、CLI 退出码 1。"""
    monkeypatch.setitem(diffcheck.THRESHOLDS, "template_trace_rate", 0.0)
    assert diffcheck.main([]) == 1


def test_gate_reports_hygiene_problems(monkeypatch: pytest.MonkeyPatch) -> None:
    """画像术语太少必须被点出来（卫生项与 Jaccard 是两件事）。"""
    monkeypatch.setattr(diffcheck, "MIN_TERMS", 99)
    report = diffcheck.compute(diffcheck.build_samples((("site-a", "/api/orders"),)))
    problems = report.violations()
    assert any("术语只有" in item for item in problems), problems


def test_cli_json_shape(capsys: pytest.CaptureFixture[str]) -> None:
    assert diffcheck.main(["--json"]) == 0
    payload = json.loads(capsys.readouterr().out)
    assert set(payload["metrics"]) == set(diffcheck.THRESHOLDS)
    assert payload["violations"] == []


def test_cli_rejects_malformed_sample(capsys: pytest.CaptureFixture[str]) -> None:
    assert diffcheck.main(["--samples", "site-a"]) == 2
    assert "样本格式错误" in capsys.readouterr().err


def test_custom_samples_are_used() -> None:
    report = diffcheck.compute(diffcheck.build_samples((("site-b", "/api/tags"),)))
    assert {s.profile_id for s in report.samples} == {"site-b"}
    # 只有一个服务 ⇒ 跨服务指标没有可比对象，必须是 0 而不是报错。
    assert report.metrics["cross_service_term_jaccard"] == 0.0
