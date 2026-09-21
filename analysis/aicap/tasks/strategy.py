"""`kind=strategy`：产出策略数据（诱饵选择 / 灰度 / 阈值建议，模型路径）。

**只出数据**：不判定（`AR-2`）、不决策（`MD-12`）、不执行（`SB-1`）、不直接回写响应（`AR-32`）。
今天是建议：经结论事件上报，最终怎么生效由核心的 `policy` 决定（`AR-13`）。

`INT-11` 的两条边界（灰度上限、阈值下界）在**本模块的 `build` 里强制** ——
模型这一路与规则这一路受同一条约束，不存在「模型说了就能超」的路径。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any

from ...llm.schemas import MAX_GRAY_PCT, STRATEGY_KIND, STRATEGY_SCHEMA, THRESHOLD_FLOOR
from ..ports import WireArtifact
from ._produce import structured_candidate
from ._registry import GuardrailProfile, Task, TaskLimits

if TYPE_CHECKING:  # pragma: no cover - 只用于类型标注，避免运行期循环导入
    from ..model import AnalysisClient
    from ..service import TaskSpec

GENERATOR = "model-v1"


class StrategyBoundError(ValueError):
    """模型给的策略越过了 `INT-11` 的边界（灰度上限 / 阈值下界）。

    **为什么抛异常，而不是返回 `rejected`**：内核的四个检查点是固定的
    （结构 / 黑名单 / 长度 / 风格），而 `INT-11` 是**数值边界**，不属于任何一关；
    为它扩内核（第五关）的影响面远大于在任务里显式失败。
    调用方按失败路径处理：worker 整步回落确定性版并记原因（近线不得炸，`NI-1`）；
    离线生成器则直接看到一条明确的异常 —— 两者都是 fail-closed（`AR-15`）。
    """


STRATEGY_STYLE_TERMS: tuple[str, ...] = (
    "route_mirage",
    "block",
    "gray_pct",
    "灰度",
    "阈值",
)
"""画像术语：策略理由**必须**点到阈值键或灰度 —— 否则它没说自己建议了什么。"""

STRATEGY_PROFILE = GuardrailProfile(
    name="l4-strategy",
    prompt="strategy",
    style_terms=STRATEGY_STYLE_TERMS,
    checked_fields=("rationale",),
)
"""护栏档案：受检字段只有 `rationale`（本任务唯一的自由文本）。"""

STRATEGY_LIMITS = TaskLimits(purpose="conclusion", max_output=4000)


def produce(prompt: str, spec: TaskSpec, client: AnalysisClient) -> Mapping[str, Any]:
    """让模型产出候选（未过护栏）。"""
    return structured_candidate(prompt, spec, client)


def build(checked: Mapping[str, Any], spec: TaskSpec, generated_at: str) -> WireArtifact:
    """按 `INT-11` 复核数值边界，然后建产物。

    越界**不夹紧、不改写**（那是「修复后放行」，`AR-15` 禁止）—— 直接拒绝建产物。
    """
    del spec, generated_at
    data = dict(checked)

    gray = data.get("gray_pct")
    if isinstance(gray, bool) or not isinstance(gray, int) or gray < 0:
        raise StrategyBoundError(f"灰度必须是非负整数，实际 {gray!r}（INT-11）")
    if gray > MAX_GRAY_PCT:
        raise StrategyBoundError(f"灰度 {gray}% 超过上限 {MAX_GRAY_PCT}%（INT-11 阶梯放开）")

    thresholds = data.get("threshold_suggestions")
    if not isinstance(thresholds, Mapping):
        raise StrategyBoundError(f"阈值建议必须是对象，实际 {type(thresholds).__name__}（INT-11）")
    for key, floor in THRESHOLD_FLOOR.items():
        value = thresholds.get(key)
        if isinstance(value, bool) or not isinstance(value, int | float):
            raise StrategyBoundError(f"阈值建议缺 {key} 或不是数字：{value!r}（INT-11）")
        if value < floor:
            raise StrategyBoundError(f"阈值 {key}={value} 低于下界 {floor}（INT-11）")
    return WireArtifact(data)


STRATEGY_TASK = Task(
    kind=STRATEGY_KIND,
    schema=STRATEGY_SCHEMA,
    guardrail_profile=STRATEGY_PROFILE,
    limits=STRATEGY_LIMITS,
    produce=produce,
    build=build,
    generator=GENERATOR,
    requires_model=True,
)
"""`kind=strategy` 的登记项。"""
