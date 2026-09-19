"""超时双阶段收尾（`AR-19` / `AR-20` / `AR-21`）。

- 阶段 1 执行（超时 T1）→ 阶段 2 **复用同一会话**收尾（超时 T2）；
  **禁止**超时后丢弃整轮结果，**禁止**重新执行完整任务（`AR-19`）；
- 收尾提示词**必须**限定范围，且收尾阶段**必须**用**不同契约**（只允许「事实」类字段）（`AR-20`）；
- 两阶段均失败时**禁止**写入任何中间数据，**必须**释放已占用的标记（认领、租约）（`AR-21`）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import time
from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Any, Protocol

from .contract import Field, Schema, validate
from .envelope import Envelope, accept, reject
from .extract import ExtractionError, extract_json

FINALIZE_SCHEMA = Schema(
    name="finalize",
    fields=(
        # 只允许「事实」类字段：禁止「完成 / 成功」类（AR-20）
        Field("session_id", (str,), True, 128),
        Field("actions_observed", (list,), True, 64),
        Field("evidence_ids", (list,), True, 256),
        Field("blocked_at", (str,), False, 256),
    ),
)


class Session(Protocol):
    """与 LLM 的一次会话（阶段 2 **必须**复用同一会话）。"""

    @property
    def session_id(self) -> str: ...

    def run(self, prompt: str, *, timeout: float) -> str: ...


class Lease(Protocol):
    """占用的标记（认领 / 租约）；两阶段失败**必须**释放（`AR-21`）。"""

    def release(self) -> None: ...


class Clock(Protocol):
    def now(self) -> float: ...


@dataclass
class TwoPhaseResult:
    envelope: Envelope
    phase1: str | None = None
    phase2: str | None = None
    timings: dict[str, float] = field(default_factory=dict)
    truncations: list[Any] = field(default_factory=list)


class PhaseFailure(TimeoutError):
    """阶段失败（含超时）—— 只用于**本模块内部**流转，不改变「不得丢结果」的语义（`AR-19`）。"""

    def __init__(self, phase: str, detail: str) -> None:
        super().__init__(f"{phase}: {detail}")
        self.phase = phase
        self.detail = detail


def run_two_phase(
    session: Session,
    *,
    prompt: str,
    finalize_prompt: str,
    t1: float,
    t2: float,
    lease: Lease | None = None,
    clock: Clock | None = None,
) -> TwoPhaseResult:
    """跑「执行 → 收尾」两阶段；任一步失败都不返回半成品。"""
    now: Callable[[], float] = clock.now if clock else time.monotonic
    timings: dict[str, float] = {}

    started = now()
    try:
        phase1 = session.run(prompt, timeout=t1)
    except Exception as failure:  # 超时或会话错误都按「阶段失败」处理（AR-19）
        timings["phase1"] = now() - started
        _abandon(lease)
        return TwoPhaseResult(envelope=reject(f"阶段 1 失败：{failure}，整轮作废"), timings=timings)
    timings["phase1"] = now() - started

    try:
        phase2 = session.run(finalize_prompt, timeout=t2)
    except Exception as failure:  # 同上：收尾失败 → 不写中间数据 + 释放标记（AR-21）
        timings["phase2"] = now() - started - timings["phase1"]
        _abandon(lease)  # AR-21：两阶段均失败 → 不写中间数据 + 释放标记
        return TwoPhaseResult(
            envelope=reject(f"收尾阶段失败：{failure}（未写入任何中间数据）"),
            phase1=phase1,
            timings=timings,
        )
    timings["phase2"] = now() - started - timings["phase1"]

    try:
        raw = extract_json(phase2)
        facts = validate(raw, FINALIZE_SCHEMA)
    except (ExtractionError, ValueError) as exc:
        _abandon(lease)
        return TwoPhaseResult(
            envelope=reject(f"收尾契约校验失败：{exc}"),
            phase1=phase1,
            phase2=phase2,
            timings=timings,
        )
    if facts.get("session_id") != session.session_id:
        _abandon(lease)
        return TwoPhaseResult(
            envelope=reject("收尾结论的 session_id 与会话不一致（AR-25）"),
            phase1=phase1,
            phase2=phase2,
            timings=timings,
        )
    return TwoPhaseResult(
        envelope=accept(dict(facts)), phase1=phase1, phase2=phase2, timings=timings
    )


def _abandon(lease: Lease | None) -> None:
    if lease is not None:
        lease.release()
