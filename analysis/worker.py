"""L4 近线分析 worker —— 让 L4 **在运行时真的跑起来**。

链路（`architecture.md` §2 的 L4 段）：

    遥测事件（读 ListEvents） → 态势去重（AR-14） → 意图识别（intent）
      → 攻击链 + 证据校验（chain / AR-12） → 策略数据（strategy） → 结论**上报为事件**

三条硬边界：

1. **不在业务路径上**（近线）：worker 挂了不影响任何请求（`NI-1`）；读不到遥测就**降级为不分析**；
2. **不持有执行能力**（`AR-32`）：只读事件 + 上报结构化结论，不下发指令、不生成载荷、不调外部系统；
3. **结论只经 `policy` 间接生效**（`AR-32`）：worker **不直接**改策略、**不直接**回写响应。

用法：

    python -m analysis.worker --core 127.0.0.1:9443 --once     # 跑一轮（人工测试 / CI）
    python -m analysis.worker --core 127.0.0.1:9443            # 常驻（默认 30s 一轮）
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from collections.abc import Sequence
from dataclasses import dataclass, field
from datetime import UTC, datetime

from .chain.reconstruct import Chain, reconstruct
from .dedupe import SituationDedupe
from .events import Observation, parse_all
from .intent.recognize import recognize
from .llm.envelope import Envelope
from .strategy.generate import StrategyInput, generate
from .strategy.rotate import decide as decide_rotation
from .telemetry import (
    CONCLUSION_EVENT_TYPE,
    TelemetryPort,
    TelemetryUnavailable,
    WireEvent,
)

EVIDENCE_CACHE_CAP = 4096
"""证据缓存上限。

`AR-12` 要判「引用的证据 ID 是否真的在遥测里」；缓存让该检查对**跨轮引用**也成立。
"""

DEFAULT_LIMIT = 200


@dataclass
class EvidenceCache:
    """已见过的证据 ID（有界）。实现 `chain.evidence.EvidenceIndex`。"""

    capacity: int = EVIDENCE_CACHE_CAP
    _seen: dict[str, None] = field(default_factory=dict)

    def feed(self, event_ids: Sequence[str]) -> None:
        for event_id in event_ids:
            if not event_id:
                continue
            if len(self._seen) >= self.capacity:
                self._seen.pop(next(iter(self._seen)), None)
            self._seen[event_id] = None

    def exists(self, event_id: str) -> bool:
        return event_id in self._seen

    def __len__(self) -> int:
        return len(self._seen)


@dataclass
class AnalysisRun:
    """一轮分析的结果（供日志、测试与人工核对）。"""

    fetched: int = 0
    admitted: int = 0
    suppressed: int = 0
    reported: int = 0
    duplicated: int = 0
    conclusions: list[dict[str, object]] = field(default_factory=list)
    errors: list[str] = field(default_factory=list)
    intent: Envelope | None = None
    chain: Chain | None = None
    strategy: Envelope | None = None
    rotation: dict[str, object] | None = None

    def summary(self) -> str:
        return (
            f"取事件 {self.fetched} 条 · 去重后 {self.admitted} 条（抑制 {self.suppressed}）· "
            f"结论 {len(self.conclusions)} 条（新 {self.reported} / 重复 {self.duplicated}）"
            + (f" · 错误 {self.errors}" if self.errors else "")
        )


def run_once(
    port: TelemetryPort,
    *,
    limit: int = DEFAULT_LIMIT,
    window_seconds: float = 60.0,
    cache: EvidenceCache | None = None,
    now: str | None = None,
) -> AnalysisRun:
    """跑一轮：读 → 去重 → 分析 → 上报结论。任何异常都被收住并记进 `errors`（近线，不得炸）。"""
    run = AnalysisRun()
    evidence = cache if cache is not None else EvidenceCache()
    try:
        fetched = port.list_events(limit=limit)
    except TelemetryUnavailable as exc:
        run.errors.append(f"遥测不可达：{exc}")
        return run

    run.fetched = len(fetched)
    # 证据缓存要装「L4 会引用的 ID」：判定事件的**载荷里**是 `decision_id`（与契约、控制台一致），
    # 而遥测事件 ID 是 `decision:<decision_id>`（外层幂等键）。两者都装 ——
    # 只装后者曾导致 AR-12 误判「引用的证据不存在」，整条攻击链被作废（make dev 抓到的真 bug）。
    for event in fetched:
        evidence.feed([event.event_id, str(event.json_payload().get("decision_id", ""))])

    decisions = [event for event in fetched if event.event_type == "decision"]
    observations = parse_all([event.json_payload() for event in decisions])
    observations = [obs for obs in observations if obs.event_id]
    dedupe = SituationDedupe(window_seconds=window_seconds)
    admitted: list[Observation] = []
    for obs in observations:
        key = dedupe.key(
            source=obs.source, session_id=obs.session_id, method=obs.method, path=obs.path
        )
        if dedupe.admit(key, at=_epoch(obs.at)):
            admitted.append(obs)
    run.admitted = len(admitted)
    run.suppressed = dedupe.suppressed
    if not admitted:
        return run  # 无新态势：不猜、不产出结论（AR-15）

    intent = recognize(admitted)
    run.intent = intent
    try:
        # AR-12：链里引用的证据 ID **必须**真实存在于遥测（缓存即已取到的证据集合）
        run.chain = reconstruct(admitted, evidence=evidence)
    except Exception as exc:
        run.errors.append(f"攻击链作废：{exc}")
        run.chain = None
    run.strategy = generate(
        StrategyInput(
            intent_category=str(intent.data.get("category", "")) if intent.accepted else "",
            chain_stages=[stage.name for stage in (run.chain.stages if run.chain else ())],
            broken_signals=[sig.kind for sig in (run.chain.broken if run.chain else ())],
            available_decoys=_decoys(fetched),
        )
    )
    run.rotation = decide_rotation(
        [sig.kind for sig in (run.chain.broken if run.chain else ())]
    ).to_wire()

    for kind, envelope in (("intent", run.intent), ("strategy", run.strategy)):
        if envelope is None:
            continue
        conclusion = _conclusion(kind, envelope, admitted)
        run.conclusions.append(conclusion)
        event = WireEvent(
            event_id=str(conclusion["event_id"]),
            event_type=CONCLUSION_EVENT_TYPE,
            session_id=admitted[0].session_id,
            payload=json.dumps(conclusion, ensure_ascii=False, sort_keys=True).encode("utf-8"),
            created_at=now or _now_iso(),
        )
        try:
            accepted, duplicated = port.report(event)
        except TelemetryUnavailable as exc:
            run.errors.append(f"结论上报失败：{exc}")
            continue
        run.reported += accepted
        run.duplicated += duplicated
    return run


def _conclusion(
    kind: str, envelope: Envelope, admitted: Sequence[Observation]
) -> dict[str, object]:
    """结论形状：`{accepted,data}` 信封 + 可复算的来源（幂等键由内容决定，`AR-11`）。"""
    payload: dict[str, object] = {
        "kind": kind,
        **envelope.to_wire(),
        "analyzed": len(admitted),
        "evidence_ids": sorted({obs.event_id for obs in admitted}),
    }
    digest = hashlib.sha256(
        json.dumps(payload, ensure_ascii=False, sort_keys=True).encode("utf-8")
    ).hexdigest()[:16]
    payload["event_id"] = f"analysis:{kind}:{digest}"
    return payload


def _decoys(events: Sequence[WireEvent]) -> list[str]:
    """诱饵名从**已登记的后端名**里取（核心启动时会打印后端池；这里只认事件里出现过的后端）。"""
    names: dict[str, None] = {}
    for event in events:
        backend = event.json_payload().get("backend")
        if isinstance(backend, str) and backend:
            names.setdefault(backend, None)
    return list(names)


def _epoch(stamp: str) -> float:
    if not stamp:
        return 0.0
    try:
        return datetime.fromisoformat(stamp).timestamp()
    except ValueError:
        return 0.0


def _now_iso() -> str:
    return datetime.now(UTC).isoformat()


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="analysis.worker", description="L4 近线分析 worker")
    parser.add_argument(
        "--core", default="127.0.0.1:9443", help="核心遥测面地址（明文 gRPC，loopback）"
    )
    parser.add_argument("--once", action="store_true", help="只跑一轮（人工测试 / CI 用）")
    parser.add_argument("--interval", type=float, default=30.0, help="常驻模式的轮询间隔秒数")
    parser.add_argument("--limit", type=int, default=DEFAULT_LIMIT, help="每轮取事件条数上限")
    parser.add_argument("--window", type=float, default=60.0, help="态势去重时间窗（秒，AR-14）")
    args = parser.parse_args(argv)

    import time

    from .telemetry import GrpcTelemetryClient

    try:
        port: TelemetryPort = GrpcTelemetryClient(args.core)
    except TelemetryUnavailable as exc:
        print(f"L4 worker 启动失败：{exc}", file=sys.stderr)
        return 2

    cache = EvidenceCache()
    while True:
        run = run_once(port, limit=args.limit, window_seconds=args.window, cache=cache)
        print(f"[L4] {run.summary()}", flush=True)
        for conclusion in run.conclusions:
            print(
                f"  · {conclusion['kind']}: accepted={conclusion['accepted']} "
                f"data={json.dumps(conclusion.get('data', {}), ensure_ascii=False)}",
                flush=True,
            )
        if args.once:
            return 0 if not run.errors else 1
        time.sleep(max(1.0, args.interval))


if __name__ == "__main__":
    raise SystemExit(main())
