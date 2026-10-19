"""诱导效果评估口径（`SHEN-M2-03`）：把「有没有用」变成可复算的五个数。

输入是**逐请求事件**的 JSONL（`request_judged` 的载荷，可带 `at` 时间戳），
输出是五项指标 + 一份「开/关」对照报告。

| 指标 | 定义 | 为什么是它 |
| --- | --- | --- |
| 自主进入率 | 被改道会话中未被 block、且继续交互（≥2 请求）的比例 | 价值是「让它自己走下去」 |
| 有效交互率 | 被改道会话中，产生 **≥ 3 个不同请求** 的比例 | 只看进入次数会被「进一次就走」刷高 |
| 平均驻留时长 | 会话从首次改道到最后一次请求的秒数 | 停留越久，我们拿到的情报越多 |
| 攻击链深度 | 会话内出现过的 ATT&CK 技术数（`techniques`，可缺） | 深度比次数更能说明暴露 |
| 误伤率 | 被改道会话中事后被判为**正常用户**的比例（需标签） | 没有它，前四项都可能靠误伤刷出来 |

**口径纪律**：所有比例都给 `n`（分母）；`n` 太小时不报比例（返回 `None`），
避免「1/1 = 100%」这种看上去很美的假结论。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import argparse
import json
import math
import sys
from collections.abc import Iterable, Mapping, Sequence
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path

# 比例的最小分母：低于它不报比例（返回 None），只报计数。
MIN_DENOMINATOR = 10

# 有效交互的门槛：不同请求数 ≥ 3。
EFFECTIVE_REQUESTS = 3


@dataclass
class sessionState:
    session: str
    first_mirage_at: datetime | None = None
    last_at: datetime | None = None
    requests: int = 0
    paths: set[str] = field(default_factory=set)
    blocked: bool = False
    techniques: set[str] = field(default_factory=set)


@dataclass(frozen=True)
class Metrics:
    sessions: int
    rerouted_sessions: int
    autonomous_entry_rate: float | None
    effective_interaction_rate: float | None
    mean_dwell_seconds: float | None
    mean_chain_depth: float | None
    false_positive_rate: float | None
    labelled_sessions: int


def _parse_at(raw: object) -> datetime | None:
    if isinstance(raw, str) and raw:
        try:
            return datetime.fromisoformat(raw.replace("Z", "+00:00"))
        except ValueError:
            return None
    return None


def _techniques(ev: Mapping[str, object]) -> set[str]:
    raw = ev.get("techniques")
    if isinstance(raw, list):
        return {str(x) for x in raw if isinstance(x, str) and x}
    if isinstance(raw, str) and raw:
        return {part.strip() for part in raw.split(",") if part.strip()}
    return set()


def summarise(events: Iterable[Mapping[str, object]]) -> list[sessionState]:
    """把逐请求事件按会话聚合。只保留**出现过改道**的会话（其余与会话目标无关）。

    事件里没有 `session_hash` 时**跳过该条**：没有会话身份就算不出会话级指标，
    猜一个（例如按 IP）会得出看起来精确、其实错的数字。
    """
    by_session: dict[str, sessionState] = {}
    for ev in events:
        sid = str(ev.get("session_hash") or "")
        if not sid:
            continue
        st = by_session.setdefault(sid, sessionState(session=sid))
        at = _parse_at(ev.get("at"))
        st.requests += 1
        if at is not None and (st.last_at is None or at > st.last_at):
            st.last_at = at
        path = ev.get("path")
        if isinstance(path, str) and path:
            st.paths.add(path)
        executed = str(ev.get("executed") or "")
        if executed in ("mirage", "decoy") and st.first_mirage_at is None:
            st.first_mirage_at = at
        if str(ev.get("action") or "") == "block" or executed == "block":
            st.blocked = True
        st.techniques |= _techniques(ev)

    return list(by_session.values())


def _rerouted(states: Iterable[sessionState]) -> list[sessionState]:
    """被改道会话 = 至少有一条 `executed` 落在幻境的会话。"""
    return [st for st in states if st.first_mirage_at is not None]


def _rate(num: int, den: int) -> float | None:
    if den < MIN_DENOMINATOR:
        return None
    return num / den


def compute(
    events: Iterable[Mapping[str, object]], labels: Mapping[str, bool] | None = None
) -> Metrics:
    """算五项指标。`labels` 是 `session_hash → 是否为正常用户`（误伤率需要）。"""
    states = summarise(events)
    rerouted = _rerouted(states)

    autonomous = sum(1 for st in rerouted if not st.blocked and st.requests >= 2)
    effective = sum(1 for st in rerouted if len(st.paths) >= EFFECTIVE_REQUESTS)

    dwells = [
        (st.last_at - st.first_mirage_at).total_seconds()
        for st in rerouted
        if st.first_mirage_at is not None and st.last_at is not None
    ]
    depths = [len(st.techniques) for st in rerouted]

    labelled = [st for st in rerouted if labels is not None and st.session in labels]
    false_positives = sum(1 for st in labelled if labels and labels.get(st.session))

    return Metrics(
        sessions=len(states),
        rerouted_sessions=len(rerouted),
        autonomous_entry_rate=_rate(autonomous, len(rerouted)),
        effective_interaction_rate=_rate(effective, len(rerouted)),
        mean_dwell_seconds=(sum(dwells) / len(dwells)) if dwells else None,
        mean_chain_depth=(sum(depths) / len(depths)) if depths else None,
        false_positive_rate=_rate(false_positives, len(labelled)) if labelled else None,
        labelled_sessions=len(labelled),
    )


def wilson(successes: int, total: int, z: float = 1.96) -> tuple[float, float] | None:
    """比例的 Wilson 置信区间（小样本下比正态近似可靠）。

    `total < MIN_DENOMINATOR` 时返回 `None`：样本太小，区间宽得没有意义。
    """
    if total < MIN_DENOMINATOR:
        return None
    phat = successes / total
    denom = 1 + z * z / total
    centre = phat + z * z / (2 * total)
    margin = z * math.sqrt(phat * (1 - phat) / total + z * z / (4 * total * total))
    return ((centre - margin) / denom, (centre + margin) / denom)


def compare(on: Metrics, off: Metrics) -> dict[str, object]:
    """开/关对照：给出两组数字与差值（差值只在两边都算得出比例时给）。"""

    def delta(a: float | None, b: float | None) -> float | None:
        if a is None or b is None:
            return None
        return a - b

    return {
        "on": on.__dict__,
        "off": off.__dict__,
        "delta": {
            "autonomous_entry_rate": delta(on.autonomous_entry_rate, off.autonomous_entry_rate),
            "effective_interaction_rate": delta(
                on.effective_interaction_rate, off.effective_interaction_rate
            ),
            "mean_dwell_seconds": delta(on.mean_dwell_seconds, off.mean_dwell_seconds),
            "mean_chain_depth": delta(on.mean_chain_depth, off.mean_chain_depth),
            "false_positive_rate": delta(on.false_positive_rate, off.false_positive_rate),
        },
    }


def load_events(paths: Sequence[Path]) -> list[Mapping[str, object]]:
    """读 JSONL 事件（一行一条；空行忽略）。坏行**直接报错** —— 评估不该悄悄跳过数据。"""
    out: list[Mapping[str, object]] = []
    for path in paths:
        with path.open(encoding="utf-8") as fh:
            for lineno, line in enumerate(fh, 1):
                raw = line.strip()
                if not raw:
                    continue
                try:
                    obj = json.loads(raw)
                except json.JSONDecodeError as exc:
                    raise ValueError(f"{path}:{lineno} 不是合法 JSON：{exc}") from exc
                if not isinstance(obj, dict):
                    raise ValueError(f"{path}:{lineno} 不是 JSON 对象")
                out.append(obj)
    return out


def load_labels(path: Path | None) -> dict[str, bool]:
    """标签文件：`{"<session_hash>": true}`，true = 事后判定为**正常用户**（误伤）。"""
    if path is None:
        return {}
    try:
        raw = json.loads(path.read_text(encoding="utf-8"))
    except OSError as exc:
        raise ValueError(f"读标签文件失败：{exc}") from exc
    except json.JSONDecodeError as exc:
        raise ValueError(f"标签文件不是合法 JSON：{exc}") from exc
    if not isinstance(raw, dict):
        raise ValueError("标签文件必须是对象：{session_hash: true/false}")
    return {str(k): bool(v) for k, v in raw.items()}


def _fmt(value: float | None, *, pct: bool = True) -> str:
    if value is None:
        return "样本不足"
    return f"{value * 100:.1f}%" if pct else f"{value:.1f}"


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="诱导效果评估（SHEN-M2-03）")
    parser.add_argument("--on", action="append", required=True, help="开诱导的事件 JSONL（可多次）")
    parser.add_argument("--off", action="append", default=[], help="关诱导的事件 JSONL（可多次）")
    parser.add_argument("--labels", help="误伤标签文件：{session_hash: true}")
    parser.add_argument("--json", action="store_true", help="机器可读输出")
    args = parser.parse_args(argv)

    labels = load_labels(Path(args.labels) if args.labels else None)
    on = compute(load_events([Path(p) for p in args.on]), labels)
    report: dict[str, object] = {"on": on.__dict__}
    if args.off:
        off = compute(load_events([Path(p) for p in args.off]), labels)
        report = compare(on, off)
    if args.json:
        print(json.dumps(report, ensure_ascii=False, indent=2))
        return 0

    print("诱导效果（SHEN-M2-03）")
    print(f"  会话 {on.sessions} · 其中被改道 {on.rerouted_sessions}")
    print(f"  自主进入率      {_fmt(on.autonomous_entry_rate)}")
    print(f"  有效交互率      {_fmt(on.effective_interaction_rate)}")
    print(f"  平均驻留时长    {_fmt(on.mean_dwell_seconds, pct=False)} 秒")
    print(f"  攻击链深度      {_fmt(on.mean_chain_depth, pct=False)} 个技术")
    fp = _fmt(on.false_positive_rate)
    print(f"  误伤率          {fp}（已标注 {on.labelled_sessions} 个会话）")
    if on.rerouted_sessions < MIN_DENOMINATOR:
        note = f"注意：被改道会话只有 {on.rerouted_sessions} 个"
        print(f"\n{note}（< {MIN_DENOMINATOR}），比例列不给结论。")
    return 0


if __name__ == "__main__":  # pragma: no cover
    try:
        raise SystemExit(main())
    except ValueError as exc:
        print(f"评估失败：{exc}", file=sys.stderr)
        raise SystemExit(1) from exc
