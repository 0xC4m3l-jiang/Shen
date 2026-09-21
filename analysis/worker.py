"""L4 近线分析 worker —— 让 L4 **在运行时真的跑起来**。

链路（`architecture.md` §2 的 L4 段）：

    遥测事件（读 ListEvents） → 态势去重（AR-14） → 意图识别（intent）
      → 攻击链 + 证据校验（chain / AR-12） → 策略数据（strategy） → 结论**上报为事件**

**三步各自的两种产出（`--llm` 控制）**

| 步 | 默认（无 `--llm`） | `--llm` 且模型可用 | 模型被拒 / 不可用 |
| --- | --- | --- | --- |
| `intent` | `recognize()` 规则版 | `aicap` 的 `kind=intent` | 回落规则版 |
| `chain` | `reconstruct()` + `AR-12` | `aicap` 的 `kind=chain` + `AR-12` | 回落确定性版 |
| `strategy` | `generate()` 规则版 | `aicap` 的 `kind=strategy` | 回落规则版 |

模型这一路**必须**走唯一出口（`aicap.service.generate`）—— 那里有两道护栏，
绕不过去（`AR-33`）。回落原因记进 `AnalysisRun.model_rejected`；而
`AnalysisRun.errors` 只装**本轮真的没做成的事**（遥测不可达 / 链作废 / 上报失败）。

三条硬边界：

1. **不在业务路径上**（近线）：worker 挂了不影响任何请求（`NI-1`）；读不到遥测就**降级为不分析**；
2. **不持有执行能力**（`AR-32`）：只读事件 + 上报结构化结论，不下发指令、不生成载荷、不调外部系统；
3. **结论只经 `policy` 间接生效**（`AR-32`）：worker **不直接**改策略、**不直接**回写响应。

用法：

    python -m analysis.worker --core 127.0.0.1:9443 --once        # 跑一轮（人工测试 / CI）
    python -m analysis.worker --core 127.0.0.1:9443 --once --llm  # 启用模型路径
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import Any

from .aicap import service as aicap_service
from .aicap.model import AnalysisClient
from .aicap.model import Unavailable as ModelUnavailable
from .aicap.model import from_environment as model_from_environment
from .chain.broken import BrokenSignal
from .chain.evidence import EvidenceIndex, assert_exists
from .chain.reconstruct import Chain, Stage, reconstruct
from .dedupe import SituationDedupe
from .events import Observation, parse_all
from .intent.recognize import recognize
from .llm.envelope import Envelope, accept
from .llm.schemas import CHAIN_KIND, INTENT_KIND, STRATEGY_KIND
from .strategy.generate import StrategyInput, generate
from .strategy.rotate import decide as decide_rotation
from .telemetry import (
    CONCLUSION_EVENT_TYPE,
    TelemetryPort,
    TelemetryUnavailable,
    WireEvent,
)

MODEL_GENERATOR = "model-v1"
"""结论由**模型路径**产出时的生成器标识（与任务登记项的 `generator` 同值）。"""

RULES_GENERATOR = "rules-v1"
"""结论由**确定性规则**产出时的生成器标识。

为什么结论里必须写清是谁产的：两条路的可复核程度不同（一条能离线逐条重算，一条不能），
读结论的人（控制台 / 审计）必须能分辨 —— 否则「模型说的」会被当成「规则算出来的」。
"""

MODEL_DEADLINE_S = 30.0
"""单步模型调用的时限（`TaskSpec.deadline_s`；`AR-19` 的双阶段收尾用它）。"""

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

    intent_generator: str = RULES_GENERATOR
    chain_generator: str = RULES_GENERATOR
    strategy_generator: str = RULES_GENERATOR
    """每一步实际是谁产的（`RULES_GENERATOR` / `MODEL_GENERATOR`）。

    默认值是规则版：不传 `client` 时整轮与接模型之前**逐字一致**（`make dev` 的回归基线）。
    """

    model_rejected: dict[str, str] = field(default_factory=dict)
    """`kind` → 模型路径被回落的原因（未启用模型 / 该步成功时为**空字典**）。

    为什么与 `errors` 分开：模型不可用是**设计内的降级**（确定性版已经产出结论，
    整轮是成功的），而 `errors` 里的东西是「本轮真的没做成」——
    两者混在一起会让「跑成功了但有回落」看起来像「跑失败了」。
    """

    @property
    def rejected(self) -> int:
        """模型路径被回落的步数（由 `model_rejected` 算出，**不另存一份** 以免两者不同步）。"""
        return len(self.model_rejected)

    def summary(self) -> str:
        return (
            f"取事件 {self.fetched} 条 · 去重后 {self.admitted} 条（抑制 {self.suppressed}）· "
            f"结论 {len(self.conclusions)} 条（新 {self.reported} / 重复 {self.duplicated}）"
            + (f" · 模型回落 {self.rejected} 步" if self.rejected else "")
            + (f" · 错误 {self.errors}" if self.errors else "")
        )


def run_once(
    port: TelemetryPort,
    *,
    limit: int = DEFAULT_LIMIT,
    window_seconds: float = 60.0,
    cache: EvidenceCache | None = None,
    now: str | None = None,
    client: AnalysisClient | None = None,
) -> AnalysisRun:
    """跑一轮：读 → 去重 → 分析 → 上报结论。任何异常都被收住并记进 `errors`（近线，不得炸）。

    `client`：模型客户端。`None`（默认）= 三步全走确定性规则，与接模型之前的行为一致；
    传入时三步各自「模型优先、失败回落」（模型失败**不影响整轮**：结论仍由规则版产出）。
    """
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

    # ① 意图：模型优先，失败/被拒则回落确定性规则。两条路**各自标注**产出者 ——
    #    禁止用规则输出冒充模型输出，反之亦然（AR-15）。
    #    模型给的 `evidence_ids` **也要过 AR-12**：结论里有两处证据引用（顶层的
    #    「本轮参与分析的事件」与 `data` 里「模型引用的证据」），后者是模型自由填的，
    #    不校验就等于让结论携带**编造的证据 ID**（审计/控制台会当成真的）。
    observations_payload = _observations_payload(admitted)
    session_id = admitted[0].session_id
    data, reason = _model_candidate(
        INTENT_KIND, observations_payload, client=client, session_id=session_id
    )
    if data is not None:
        try:
            _assert_evidence_ids(data, evidence)
        except Exception as exc:
            data = None
            reason = f"证据引用校验失败：{type(exc).__name__}: {exc}"
    run.intent = accept(data) if data is not None else recognize(admitted)
    run.intent_generator = MODEL_GENERATOR if data is not None else RULES_GENERATOR
    if reason is not None:
        run.model_rejected[INTENT_KIND] = reason

    # ② 攻击链：模型优先；模型给的证据引用**仍要过 AR-12**（与确定性版同语义）。
    data, chain_reason = _model_candidate(
        CHAIN_KIND, observations_payload, client=client, session_id=session_id
    )
    chain_from_model: Chain | None = None
    if data is not None:
        try:
            chain_from_model = _chain_from_model(data)
            assert_exists(
                evidence,
                [item for stage in chain_from_model.stages for item in stage.evidence_ids],
            )
        except Exception as exc:  # 形状不对或引用不存在 ⇒ 整条链作废（AR-12）
            chain_from_model = None
            chain_reason = f"链不可用：{type(exc).__name__}: {exc}"
    if chain_from_model is not None:
        run.chain = chain_from_model
        run.chain_generator = MODEL_GENERATOR
    else:
        try:
            # AR-12：链里引用的证据 ID **必须**真实存在于遥测（缓存即已取到的证据集合）
            run.chain = reconstruct(admitted, evidence=evidence)
        except Exception as exc:
            run.errors.append(f"攻击链作废：{exc}")
            run.chain = None
        run.chain_generator = RULES_GENERATOR
    if chain_reason is not None:
        run.model_rejected[CHAIN_KIND] = chain_reason

    # ③ 策略：模型优先，失败/被拒则回落确定性版。
    data, reason = _model_candidate(
        STRATEGY_KIND,
        _strategy_payload(run, admitted=admitted, decoys=_decoys(fetched)),
        client=client,
        session_id=session_id,
    )
    run.strategy = (
        accept(data)
        if data is not None
        else generate(
            StrategyInput(
                intent_category=_category_of(run.intent),
                chain_stages=[stage.name for stage in (run.chain.stages if run.chain else ())],
                broken_signals=[sig.kind for sig in (run.chain.broken if run.chain else ())],
                available_decoys=_decoys(fetched),
            )
        )
    )
    run.strategy_generator = MODEL_GENERATOR if data is not None else RULES_GENERATOR
    if reason is not None:
        run.model_rejected[STRATEGY_KIND] = reason

    run.rotation = decide_rotation(
        [sig.kind for sig in (run.chain.broken if run.chain else ())]
    ).to_wire()

    for kind, envelope, generator in (
        (INTENT_KIND, run.intent, run.intent_generator),
        (STRATEGY_KIND, run.strategy, run.strategy_generator),
    ):
        if envelope is None:
            continue
        conclusion = _conclusion(
            kind,
            envelope,
            admitted,
            generator=generator,
            model_rejected=run.model_rejected.get(kind),
        )
        run.conclusions.append(conclusion)
        event = WireEvent(
            event_id=str(conclusion["event_id"]),
            event_type=CONCLUSION_EVENT_TYPE,
            session_id=session_id,
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


def _model_candidate(
    kind: str,
    payload: Mapping[str, Any],
    *,
    client: AnalysisClient | None,
    session_id: str,
) -> tuple[dict[str, Any] | None, str | None]:
    """走一次**唯一出口**（两道护栏在内，绕不过去 —— `AR-33`）。

    返回：

    - `(data, None)`：模型路径可用；
    - `(None, None)`：本轮**没启用**模型（`client is None`）—— 不是回落；
    - `(None, 原因)`：应该回落（被拒 / 不可用 / 任何异常都归一成同一种语义）。

    为什么把异常也收成返回值：调用方只关心「这一步能不能用模型的产出」，
    而模型这一路的失败方式（缺 key / 超时 / 解析失败 / 越界 / 护栏拒绝）对它是同一件事。
    """
    if client is None:
        return None, None
    try:
        envelope = aicap_service.generate(
            aicap_service.TaskSpec(
                kind=kind,
                session_id=session_id,
                deadline_s=MODEL_DEADLINE_S,
                payload=payload,
            ),
            client=client,
        )
    except Exception as exc:  # 近线：模型侧的失败不得冒泡出去（NI-1）
        return None, f"{type(exc).__name__}: {exc}"
    if not envelope.accepted:
        return None, envelope.rejected_reason or "未给出拒绝原因（AR-16）"
    return dict(envelope.data), None


def _observations_payload(admitted: Sequence[Observation]) -> dict[str, Any]:
    """去重后的观测（`AR-31`：**原样**进数据区，不为净化而丢弃字段）。"""
    return {"observations": [obs.to_untrusted() for obs in admitted]}


def _strategy_payload(
    run: AnalysisRun, *, admitted: Sequence[Observation], decoys: Sequence[str]
) -> dict[str, Any]:
    """策略步的输入：意图类别 + 链的阶段 / 识破信号 + 可用诱饵（都来自本轮真实结果）。"""
    return {
        "intent": _category_of(run.intent),
        "stages": [stage.name for stage in (run.chain.stages if run.chain else ())],
        "broken_signals": [sig.kind for sig in (run.chain.broken if run.chain else ())],
        "available_decoys": list(decoys),
        "observations": [obs.to_untrusted() for obs in admitted],
    }


def _category_of(envelope: Envelope | None) -> str:
    """从意图结论里取类别；没产出 / 未接受就是空串（不猜一个默认类别）。"""
    if envelope is None or not envelope.accepted:
        return ""
    return str(envelope.data.get("category", ""))


def _assert_evidence_ids(data: Mapping[str, Any], evidence: EvidenceIndex) -> None:
    """`AR-12`：结论里引用的证据 ID **必须**在遥测里真实存在。

    形状不对也抛（模型输出不可信，不猜）—— 与 `_chain_from_model` 同一种做法：
    任一不满足就整条结论作废、由调用方回落确定性版。
    """
    raw = data.get("evidence_ids")
    if not isinstance(raw, list):
        raise ValueError("evidence_ids 不是列表")
    assert_exists(evidence, [str(item) for item in raw if item])


def _chain_from_model(data: Mapping[str, Any]) -> Chain:
    """把模型给的链数据变成 `Chain`（与确定性版同一个类型，下游不必分叉）。

    **形状不对就抛异常**（由调用方当作「整条链作废」）—— 不猜、不补默认值（`AR-15`）。

    `BrokenSignal.detail` 是**空串**：模型只给信号种类，细节是确定性检测的产物 ——
    不替它编一句没发生过的描述（那会让审计读到假细节）。
    """
    raw_stages = data.get("stages")
    if not isinstance(raw_stages, list):
        raise ValueError("stages 不是列表")
    stages: list[Stage] = []
    for item in raw_stages:
        if not isinstance(item, Mapping):
            raise ValueError(f"阶段项不是对象：{type(item).__name__}")
        raw_ids = item.get("evidence_ids")
        if not isinstance(raw_ids, list):
            raise ValueError(f"阶段 {item.get('name')!r} 缺 evidence_ids 列表")
        stages.append(
            Stage(
                name=str(item.get("name", "")),
                evidence_ids=tuple(str(one) for one in raw_ids if one),
                confidence=_confidence(item.get("confidence")),
            )
        )
    raw_broken = data.get("broken_decoy_signals")
    if not isinstance(raw_broken, list):
        raise ValueError("broken_decoy_signals 不是列表")
    return Chain(
        stages=tuple(stages),
        broken=tuple(BrokenSignal(kind=str(one), detail="", evidence_ids=()) for one in raw_broken),
    )


def _confidence(value: object) -> float:
    """置信度必须是 0–1 的数：越界就当形状不对（**不夹紧**，夹紧是修复后放行）。"""
    try:
        number = float(value)  # type: ignore[arg-type]
    except (TypeError, ValueError) as exc:
        raise ValueError(f"置信度不是数字：{value!r}") from exc
    if not 0.0 <= number <= 1.0:
        raise ValueError(f"置信度越界：{value!r}")
    return number


def _conclusion(
    kind: str,
    envelope: Envelope,
    admitted: Sequence[Observation],
    *,
    generator: str,
    model_rejected: str | None,
) -> dict[str, object]:
    """结论形状：`{accepted,data}` 信封 + 可复算的来源（幂等键由内容决定，`AR-11`）。

    `generator` / `model_rejected` 是**审计字段**：读结论的人要能分辨这份结论是
    规则算的还是模型说的，以及模型那一路为什么没成（可空）。
    """
    payload: dict[str, object] = {
        "kind": kind,
        **envelope.to_wire(),
        "analyzed": len(admitted),
        "evidence_ids": sorted({obs.event_id for obs in admitted}),
        "generator": generator,
        "model_rejected": model_rejected,
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
    parser.add_argument(
        "--llm",
        action="store_true",
        help=(
            "启用模型路径（三步各自模型优先、失败回落；默认关闭）。"
            "需 SHEN_AI_KEY；未设时三步全回落确定性版并记下原因，进程不崩"
        ),
    )
    args = parser.parse_args(argv)

    import time

    from .telemetry import GrpcTelemetryClient

    # `--llm` 只是拿一个客户端：环境变量不齐时拿到的是「调用即显式失败」的实现，
    # 于是三步各自回落确定性版 —— 这与「不传 client」区分得开（后者不算回落）。
    client: AnalysisClient | None = None
    if args.llm:
        try:
            client = model_from_environment()
        except ModelUnavailable as exc:
            print(f"L4 worker：模型配置不合法：{exc}", file=sys.stderr)
            return 2

    try:
        port: TelemetryPort = GrpcTelemetryClient(args.core)
    except TelemetryUnavailable as exc:
        print(f"L4 worker 启动失败：{exc}", file=sys.stderr)
        return 2

    cache = EvidenceCache()
    while True:
        run = run_once(
            port, limit=args.limit, window_seconds=args.window, cache=cache, client=client
        )
        print(f"[L4] {run.summary()}", flush=True)
        for kind, reason in sorted(run.model_rejected.items()):
            print(f"  · 模型回落 {kind}：{reason}", flush=True)
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
