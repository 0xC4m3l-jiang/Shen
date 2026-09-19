"""策略生成（`strategy`）—— 产出**策略数据**，经 `policy` 版本化后下发。

本模块**不做**判定（`AR-2`）、**不做**决策（`MD-12`）、**不做**执行（`SB-1`）、
**不直接回写响应**（`AR-32`）。输出只是数据，灰度受 `policy` 的既有语义约束（`AR-13`）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field

from ..llm.contract import Field, Schema, validate
from ..llm.envelope import Envelope, accept, reject
from ..llm.limits import Truncation, truncate_list

STRATEGY_SCHEMA = Schema(
    name="strategy",
    fields=(
        Field("decoy_selection", (list,), True, 64),
        Field("gray_pct", (int,), True),
        Field("threshold_suggestions", (dict,), True),
    ),
)

MAX_GRAY_PCT = 20
"""灰度上限：策略永远**不得**建议一次性全量接管（`INT-11` 的阶梯放开）。"""

THRESHOLD_FLOOR = {"route_mirage": 0.3, "block": 0.6}
"""阈值下界：建议值**禁止**低于它（否则影子期就会开始改道/拦截）。"""


@dataclass
class StrategyInput:
    intent_category: str
    chain_stages: Sequence[str] = ()
    broken_signals: Sequence[str] = ()
    available_decoys: Sequence[str] = ()
    current_gray_pct: int = 0
    notes: list[str] = field(default_factory=list)


def generate(data: StrategyInput) -> Envelope:
    """生成策略建议；任何不合规（超上限 / 低阈值）都**拒绝**而不是悄悄夹紧（`AR-15`）。"""
    decoys = [name for name in data.available_decoys if name]
    if not decoys:
        return reject("没有可用诱饵：不生成无依据的策略（AR-15）")

    gray = min(MAX_GRAY_PCT, max(data.current_gray_pct, 5 if data.intent_category else 0))
    thresholds = {
        "route_mirage": THRESHOLD_FLOOR["route_mirage"],
        "block": THRESHOLD_FLOOR["block"],
    }
    if data.broken_signals:
        thresholds["route_mirage"] = round(min(0.9, THRESHOLD_FLOOR["route_mirage"] + 0.1), 2)

    truncations: list[Truncation] = []
    payload = {
        "decoy_selection": truncate_list(decoys, field="decoy_selection", log=truncations),
        "gray_pct": int(gray),
        "threshold_suggestions": thresholds,
    }
    try:
        checked = validate(payload, STRATEGY_SCHEMA)
    except ValueError as exc:
        return reject(f"策略数据不合契约：{exc}")
    if checked["gray_pct"] > MAX_GRAY_PCT:
        return reject(f"灰度 {checked['gray_pct']}% 超过上限 {MAX_GRAY_PCT}%（INT-11）")
    for key, floor in THRESHOLD_FLOOR.items():
        value = checked["threshold_suggestions"].get(key)
        if isinstance(value, int | float) and value < floor:
            return reject(f"阈值 {key}={value} 低于下界 {floor}（INT-11）")
    checked["truncations"] = [item.to_wire() for item in truncations]
    return accept(checked)


def summarize_chain(chain_wire: Mapping[str, object]) -> list[str]:
    """把链的对外形状压成阶段名列表（便于策略输入）。"""
    stages = chain_wire.get("stages", [])
    if not isinstance(stages, list):
        return []
    return [str(stage.get("name", "")) for stage in stages if isinstance(stage, Mapping)]
