"""契约与信封纪律：`AR-15` / `AR-16` / `AR-17` / `AR-18` / `AR-23`。"""

# pyright: reportMissingImports=false
# 理由：本仓库静态检查器的导入解析不可靠（绝对/相对导入都误报）；运行时权威判据是 pytest。

from __future__ import annotations

import pytest

from analysis.llm.contract import ContractError, Field, Schema
from analysis.llm.envelope import Envelope, accept, reject
from analysis.llm.extract import ExtractionError, extract_json
from analysis.llm.limits import LimitError, Truncation, cap_text, truncate_list


def test_ar15_bad_output_raises_never_defaults() -> None:
    schema = Schema("intent", (Field("category", (str,)),))
    with pytest.raises(ContractError):
        from analysis.llm.contract import validate

        validate({"category": 123}, schema)
    with pytest.raises(ContractError):
        from analysis.llm.contract import validate

        validate({}, schema)


def test_ar15_extra_field_is_rejected() -> None:
    schema = Schema("s", (Field("a", (str,)),))
    from analysis.llm.contract import validate

    with pytest.raises(ContractError):
        validate({"a": "x", "b": "y"}, schema)


def test_ar16_envelope_shape_and_reason() -> None:
    assert accept({"k": 1}).to_wire() == {"accepted": True, "data": {"k": 1}}
    wire = reject("坏输出").to_wire()
    assert wire["accepted"] is False and wire["rejected_reason"] == "坏输出"
    with pytest.raises(ValueError):
        Envelope(accepted=False, data={})  # 拒绝必须带原因
    with pytest.raises(ValueError):
        Envelope(accepted=True, rejected_reason="x")


def test_ar17_three_stage_extraction() -> None:
    assert extract_json('{"a": 1}') == {"a": 1}  # ① 整段
    assert extract_json('前缀 ```json\n{"b": 2}\n``` 后缀') == {"b": 2}  # ② 围栏
    assert extract_json('噪音 {"c": {"d": 3}} 尾巴') == {"c": {"d": 3}}  # ③ 逐 { 扫描


def test_ar17_scan_cap_bounds_work_on_long_output() -> None:
    long_noise = "x" * 5_000 + " " + "{" * 300 + '{"deep": true}'
    with pytest.raises(ExtractionError):
        extract_json(long_noise, scan_cap=10)  # 上限内找不到 → 报错，不空转
    assert extract_json(long_noise, scan_cap=3000) == {"deep": True}


def test_ar18_list_cap_records_truncation() -> None:
    log: list[Truncation] = []
    kept = truncate_list(list(range(100)), field="items", log=log)
    assert len(kept) == 64
    assert log[0].to_wire() == {"field": "items", "kept": 64, "dropped": 36, "cap": 64}
    with pytest.raises(LimitError):
        truncate_list(list(range(100)), field="items")  # 截断却不给记录容器 → 失败


def test_ar23_per_purpose_limits_are_independent() -> None:
    assert cap_text("a" * 9_999, purpose="session_response") == "a" * 2_000
    assert cap_text("a" * 9_999, purpose="intermediate") == "a" * 9_999
    with pytest.raises(LimitError):
        cap_text("x", purpose="not-registered")
