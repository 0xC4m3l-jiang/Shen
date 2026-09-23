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
from datetime import UTC, datetime, timedelta
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

CURSOR_OVERLAP_S = 1.0
"""增量游标的**回看重叠**（秒）。

为什么要有它：遥测面的游标契约只有 `since`（时间戳），**没有复合位置**
（`created_at` + `event_id`）。只按「最新一条的时间戳」推进时，与它**同一时间戳**、
稍后才写入的事件会被漏掉；回看 1 秒把它们兜回来，重复的部分由既有的幂等机制吞掉
（`AR-11` 事件 ID 去重 + `AR-14` 态势去重）。
代价是每轮少量重算 —— 这是当前服务端契约下的取舍（缺「复合位置」已登记为缺口）。
"""

MAX_SESSIONS_PER_RUN = 8
"""一轮最多分析多少个会话（近线预算：每个会话都跑三步，模型路径下就是 N 次调用）。

为什么要有上限：一轮的分析量必须可预测。超出上限的会话**不静默丢弃** ——
计数进 `AnalysisRun.truncated_sessions` 并写进日志，下一轮（事件仍在窗口内）再分析。
"""

UNKNOWN_SESSION = ""
"""身份未识别的会话键。

为什么要显式写出：旧版事件（v1 夹具）没有 `session_id`。把它们的键当成 `""` 单独一组，
**禁止**把它们拱进某个已知会话 —— 「同一会话」是分析结论的前提，猜错了就是编造证据（`AR-12`）。
"""


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
class Checkpoint:
    """L4 的**增量游标**：记住「已经分析到哪里」，下一轮只取之后的事件。

    为什么需要它（`FIX-6` 的后半）：只按 `limit` 取最近 N 条时，**每一轮都在重算最近的窗口** ——
    结论不重复（`AR-11` 幂等）但白算，窗口一大就白算得更多。

    位置 = `created_at - CURSOR_OVERLAP_S`（回看重叠，见该常量）。**推进只在一轮真正成功之后**：
    游标跳过了失败的那一轮，就再也追不回来了（宁可重算，不可漏算）。
    """

    since: str = ""
    """下一轮的 `since`（RFC3339）。空串 = 还没有游标（从窗口起点取）。"""

    rounds: int = 0
    """已成功推进的轮数（观测用；不参与逻辑）。"""

    dedupe: SituationDedupe | None = None
    """跨轮复用的**态势去重状态**（`N8`）。

    为什么必须跟着游标一起提交：只按 `since` 回看会**重叠**取到上一轮看到的同一条态势；
    去重状态每轮新建 ⇒ 重叠部分被重新判为“新态势”⇒ 重复调模型（白花钱、结论还被幂等吞掉）。
    提交条件与游标**完全相同**（见 `_commit`）：这一轮没处理干净时不提交 ——
    宁可下一轮重算（去重状态仍是上一次成功提交的），也不要让“没上报成功的态势”被永久压掉。
    """

    def advance(self, newest_created_at: str) -> None:
        """把游标推到最新事件的**回看重叠**处。"""
        if not newest_created_at:
            return
        self.since = _shift_iso(newest_created_at, -CURSOR_OVERLAP_S)
        self.rounds += 1


# CURSOR_MAX_LIMIT 是「证明连续性」时允许把单次读取上限放大到的硬顶（`N7`）。
# 为什么需要放大而不是直接接受缺口：`since` 语义是「该时刻之后」且返回 newest-first，
# 只要某次返回**少于**上限，就说明游标与这批之间**没有别的判定事件** —— 位置连续、可提交。
# 顶到上限就加倍重取；顶到硬顶仍取不尽 ⇒ 缺口是**事实**，如实记进 `window_gap` 且不提交游标。
CURSOR_MAX_LIMIT = 5000


def _fetch_contiguous(
    port: TelemetryPort, *, limit: int, since: str
) -> tuple[list[WireEvent], bool, str]:
    """按游标抓取，并**尽量证明**这批事件与游标之间没有缺口（`N7`）。

    返回 `(事件列表, 是否连续, 缺口说明)`。

    - 一次就取不满 ⇒ 连续（最常见路径，代价与从前完全一样）；
    - 取满 ⇒ 用更大上限重取（返回的是超集，最后一次为准），直到取不满或到达 `CURSOR_MAX_LIMIT`；
    - 到硬顶仍取满 ⇒ `连续=False` + 说明（**不提交游标**，见 `_commit`）。
    """
    want = max(1, limit)
    while True:
        fetched = port.list_events(limit=want, since=since or None, event_type="decision")
        if len(fetched) < want:
            return fetched, True, ""
        if want >= CURSOR_MAX_LIMIT:
            return (
                fetched,
                False,
                f"游标 {since or '(起点)'} 之后的事件多于 {CURSOR_MAX_LIMIT} 条："
                "无法证明这一段连续（完整性未知）—— 本轮不提交游标，建议缩短轮询间隔",
            )
        want = min(want * 4, CURSOR_MAX_LIMIT)


def _at_of(obs: Observation) -> str:
    """取观测的时间戳（空串 = 没有）—— 只用于「位置天花板」比较。"""
    return str(getattr(obs, "at", "") or "")


def _commit(
    checkpoint: Checkpoint | None,
    run: AnalysisRun,
    decisions: Sequence[WireEvent],
    dedupe: SituationDedupe,
    ceiling: str = "",
) -> None:
    """提交位置与去重状态 —— **只有整批被完整处理时**才允许（`N3` / `N8`）。

    `N3` 的原始缺陷：游标在**分析/上报之前**就推进了。只要随后任何一步失败，
    那段事件就再也不会被读回来（“最早推进，最晚失败”= 永久丢事件）。

    提交条件（任一不满足 ⇒ 不提交，并把原因写进 `run.advance_blocked`）：

    | 条件 | 为什么 |
    | --- | --- |
    | 本轮无 `errors` | 结论可能没上报成功；重算比漏算便宜（幂等吞重复） |
    | 抓取证明连续（`contiguous`） | 取不尽时游标与这批之间可能有缺口，位置不敢说“到这儿” |
    | 每条判定事件都有 `created_at` | 缺时间戳就没有可比较的位置，游标无法表达“处理到哪” |
    | 批次非空 | 空批次没有可推进的位置 |

    `ceiling`（非空）是本轮**未分析事件的最早时间**（会话上限截断时才有）：

    - 提交位置取 `min(最新已处理事件, ceiling)` ⇒ **不越过**未分析事件（不丢）；
    - 于是下一轮的 `since` 落在它们之前，它们会被重新读到并分析（不停摆）；
    - 若连天花板也给不出前进量（`≤ 当前游标`）⇒ 不提交并记原因（此时确实只能重算）。

    提交时**同时**写回去重状态（`N8`）：否则下一轮的重叠部分会被当成新态势再调一次模型。
    """
    if checkpoint is None:
        return
    if not decisions:
        return
    reasons: list[str] = []
    if run.errors:
        reasons.append(f"本轮有 {len(run.errors)} 条错误（结论可能未上报）")
    if not run.contiguous:
        run.completeness = "unknown"
        reasons.append("未能证明抓取连续（游标与这批之间可能有缺口）")
    if any(not event.created_at for event in decisions):
        run.completeness = "unknown"
        reasons.append("批次里有判定事件缺 created_at（无法表达位置）")
    if reasons:
        run.advance_blocked = "；".join(reasons)
        return
    newest = max(event.created_at for event in decisions)
    if ceiling:
        newest = min(newest, ceiling)
    if _shift_iso(newest, -CURSOR_OVERLAP_S) <= (checkpoint.since or ""):
        if not ceiling:
            # 没有截断，只是位置没前进（游标已经落在最新事件之后）⇒ 下一轮重算同一段。
            run.advance_blocked = "位置没有前进量（游标已在最新事件之后）"
            return
        # 截断 + 天花板给不出前进量：只能跨过未分析的事件 —— 但**必须留痕**：
        # 这不是静默丢弃（`N3` 的原始缺陷），而是「已知丢了哪一类东西」。
        run.completeness = "unknown"
        run.known_loss = (
            f"会话上限截断（{run.truncated_sessions} 个会话本轮未分析）"
            "且游标无法停在它们之前：这些会话的事件**被跳过**（已知丢弃）"
        )
        newest = max(event.created_at for event in decisions)
    checkpoint.advance(newest)
    checkpoint.dedupe = dedupe
    run.committed_cursor = checkpoint.since


def _shift_iso(stamp: str, delta_s: float) -> str:
    """把 RFC3339 时间戳平移若干秒（保持原偏移量）；解析不了就原样返回（宁可重算）。"""
    try:
        parsed = datetime.fromisoformat(stamp)
    except ValueError:
        return stamp
    return (parsed + timedelta(seconds=delta_s)).isoformat()


@dataclass
class SessionAnalysis:
    """**一个会话**的三步结果。

    为什么按会话存而不是合成一份：意图、攻击链与策略都是**会话级**结论 ——
    把多个会话的观测拼在一起产出的结论，其证据引用会跨会话（`AR-12` 要求引用真实存在，
    但「真实存在」不等于「属于同一个主体」）。改造前 `run_once` 正是这么做的：
    它取 `admitted[0].session_id` 当整批的会话，其余会话的观测混进同一份结论。
    """

    session_id: str
    observations: list[Observation] = field(default_factory=list)
    intent: Envelope | None = None
    chain: Chain | None = None
    strategy: Envelope | None = None
    rotation: dict[str, object] | None = None
    intent_generator: str = RULES_GENERATOR
    chain_generator: str = RULES_GENERATOR
    strategy_generator: str = RULES_GENERATOR
    model_rejected: dict[str, str] = field(default_factory=dict)
    errors: list[str] = field(default_factory=list)

    @property
    def unknown_identity(self) -> bool:
        """身份未识别（事件没带 `session_id`）—— 结论可以产，但必须能被看出来。"""
        return self.session_id == UNKNOWN_SESSION


@dataclass
class AnalysisRun:
    """一轮分析的结果（供日志、测试与人工核对）。"""

    fetched: int = 0
    admitted: int = 0
    suppressed: int = 0
    reported: int = 0
    duplicated: int = 0
    sessions: list[SessionAnalysis] = field(default_factory=list)
    truncated_sessions: int = 0
    """因超出 `MAX_SESSIONS_PER_RUN` 而**本轮未分析**的会话数（不是错误，但必须可见）。"""
    window_gap: str = ""
    """窗口缺口提示：非空表示本轮**没能取尽**游标之后的事件 ⇒ 中间可能有一段没被取到。

    为什么要显式写出来：丢事件会让效果口径（进入率等）**偏低**而看不出来 ——
    与「过载丢包不可隐藏成低攻击率」同一条纪律。
    """
    contiguous: bool = True
    """本轮抓取是否**证明**了连续性（不足上限 ⇒ 游标与这批之间没有别的判定事件）。"""
    completeness: str = "complete"
    """本轮的完整性口径（`N7`）：`complete` / `unknown`。
    `unknown` 时**不得**声称“不丢事件”——缺口是事实，只是我们还不知道它有多大。"""
    advance_blocked: str = ""
    """本轮**为什么没有提交游标**（`N3`）。空 = 已提交。下一轮会重算这一段（幂等吞掉重复）。"""
    dropped_events: int = 0
    """解析出来**没有 `event_id`** 因而被丢弃的判定事件数（`N7` 的“坏事件”）。"""
    known_loss: str = ""
    """**已知丢弃**（`N3`）：非空 = 本轮确实跨过了未分析的事件。区分两种情形：
    「游标停住重算」（`advance_blocked`，没有丢）与「只能前进、但已知丢了哪些」（这里）。
    两者都必须可见 —— 静默丢弃等于让效果口径偏低还看不出来。"""
    cursor: str = ""
    """本轮实际使用的 `since`（空串 = 没有游标）。留档用：能回答「这一轮看了哪一段」。"""
    committed_cursor: str = ""
    """提交后的游标（空 = 本轮**未提交**，见 `advance_blocked`）。"""
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

    多会话时这三个字段是**第一个会话**的值（兼容字段）；要逐会话看请读 `sessions`。
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
            f"会话 {len(self.sessions)} 个"
            + (f"（另有 {self.truncated_sessions} 个未分析）" if self.truncated_sessions else "")
            + f" · 结论 {len(self.conclusions)} 条（新 {self.reported} / 重复 {self.duplicated}）"
            + (f" · 游标 {self.cursor}" if self.cursor else "")
            + (f" · 游标已提交至 {self.committed_cursor}" if self.committed_cursor else "")
            + (f" · 游标未提交：{self.advance_blocked}" if self.advance_blocked else "")
            + (f" · 完整性 {self.completeness}" if self.completeness != "complete" else "")
            + (f" · 已知丢弃：{self.known_loss}" if self.known_loss else "")
            + (f" · 丢弃坏事件 {self.dropped_events} 条" if self.dropped_events else "")
            + (f" · 窗口缺口：{self.window_gap}" if self.window_gap else "")
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
    checkpoint: Checkpoint | None = None,
) -> AnalysisRun:
    """跑一轮：读（按游标增量）→ 去重 → 分析 → 上报结论。

    任何异常都被收住并记进 `errors`（近线，不得炸）。

    `client`：模型客户端。`None`（默认）= 三步全走确定性规则，与接模型之前的行为一致；
    传入时三步各自「模型优先、失败回落」（模型失败**不影响整轮**：结论仍由规则版产出）。

    `checkpoint`：增量游标。传同一个对象跨轮复用 ⇒ 只取游标之后的事件（`FIX-6`）：
    **整批处理完（分析 + 上报 + 连续抓取）之后才提交**（`N3`），提交时**同时**写回去重状态（`N8`）；
    提交不了就把原因写进 `run.advance_blocked`，并把完整性标成 `unknown`（`N7`）。

    **仍不承诺 exactly-once**（如实写在这里，别让人以为它可靠）：
      ① 进程重启 ⇒ 游标丢失，重读一个窗口（结论靠 `AR-11` 幂等，代价是少量重算）；
      ② 上报成功但提交前崩溃 ⇒ 下一轮重复上报（幂等吞掉）；
      ③ `created_at` 早于游标的迟到事件（时钟偏差）不会被再读 —— 这是时间戳游标的固有窗口；
      ④ 抓取取不尽时缺口大小**未知**，只能标 `completeness=unknown`。
    """
    run = AnalysisRun()
    evidence = cache if cache is not None else EvidenceCache()
    cursor = checkpoint.since if checkpoint is not None else ""
    run.cursor = cursor
    try:
        # 只拉**判定事件**（`event_type`）：本链路只分析判定，而 worker 自己上报的结论事件
        # 也在同一个窗口里、时间戳是本机当前时间 —— 不筛掉的话它们会挤占读取配额，
        # 还会让「取到多少条」与「要看多少判定」不再是同一个数（缺口判断会失真）。
        #
        # 连续抓取（`N7`）：能取尽就**证明**游标与这批之间没有别的判定事件
        # （见 `_fetch_contiguous`）。
        fetched, contiguous, fetch_note = _fetch_contiguous(port, limit=limit, since=cursor)
    except TelemetryUnavailable as exc:
        run.errors.append(f"遥测不可达：{exc}")
        return run

    run.fetched = len(fetched)
    run.contiguous = contiguous
    run.window_gap = fetch_note
    # 去重状态跨轮复用（`N8`）：**只有提交成功时**才会被写回 checkpoint（见下面的 `_commit`）。
    dedupe = (
        checkpoint.dedupe
        if checkpoint is not None and checkpoint.dedupe is not None
        else SituationDedupe(window_seconds=window_seconds)
    )
    suppressed_before = dedupe.suppressed
    # 位置的**天花板**（`N3`）：非空 = 本轮有「已取到但没分析」的事件，游标不得越过其中最早一条。
    # 默认空（没有截断就没有上限）。
    ceiling = ""
    # 推进**不在**这里做：只有整批被完整处理（分析 + 上报）之后才提交游标（`N3`，见 `_commit`）。
    # 证据缓存要装「L4 会引用的 ID」：判定事件的**载荷里**是 `decision_id`（与契约、控制台一致），
    # 而遥测事件 ID 是 `decision:<decision_id>`（外层幂等键）。两者都装 ——
    # 只装后者曾导致 AR-12 误判「引用的证据不存在」，整条攻击链被作废（make dev 抓到的真 bug）。
    for event in fetched:
        evidence.feed([event.event_id, str(event.json_payload().get("decision_id", ""))])

    # 读取侧已按 `event_type=decision` 收窄；这里保留同一过滤（防御：换实现时语义不悄悄变宽）。
    decisions = [event for event in fetched if event.event_type == "decision"]
    observations = parse_all([event.json_payload() for event in decisions])
    # 坏事件（解析出来没有 event_id）不能静默消失：计数进 `run.dropped_events`（`N7`）。
    parsed = len(observations)
    observations = [obs for obs in observations if obs.event_id]
    run.dropped_events = parsed - len(observations)
    admitted: list[Observation] = []
    for obs in observations:
        key = dedupe.key(
            source=obs.source, session_id=obs.session_id, method=obs.method, path=obs.path
        )
        if dedupe.admit(key, at=_epoch(obs.at)):
            admitted.append(obs)
    run.admitted = len(admitted)
    # 去重状态跨轮复用时 `suppressed` 是累计值 ⇒ 只报**本轮增量**（否则数字会一直涨）。
    run.suppressed = dedupe.suppressed - suppressed_before
    if not admitted:
        # 没有新态势也要提交位置（这一段确实处理完了），否则每轮都重读同一批。
        _commit(checkpoint, run, decisions, dedupe, ceiling)
        return run  # 无新态势：不猜、不产出结论（AR-15）

    # ── 按会话分组（FIX-5/FIX-6）────────────────────────────────────────────
    # 结论是**会话级**的：意图、攻击链与策略描述的都是「一个主体在做什么」。
    # 改造前这里取 `admitted[0].session_id` 当整批的会话，其余会话的观测混进同一份结论 ——
    # 跨会话串链，引用看似真实、语义却是错的。现在每个会话各产一份。
    groups, truncated = _group_by_session(admitted, MAX_SESSIONS_PER_RUN)
    run.truncated_sessions = truncated
    # 位置的**天花板**（`N3`）：若本轮因会话上限少分析了若干会话，游标就不能越过这些事件里
    # **最早**的那条 —— 越过它 = 那些事件永远不会被分析（原缺陷正是如此）。
    # 取最早 ⇒ 下一轮的 `since` 一定落在它们之前，于是它们会被重新读到并分析；最终收敛。
    if truncated:
        analyzed = {group[0].session_id for group in groups if group}
        dropped_stamps = [
            at for obs in admitted if obs.session_id not in analyzed and (at := _at_of(obs))
        ]
        if dropped_stamps:
            ceiling = min(dropped_stamps)

    for index, group in enumerate(groups):
        session = _analyze_session(
            group,
            evidence=evidence,
            decoys=_decoys(fetched),
            client=client,
        )
        run.sessions.append(session)
        run.errors.extend(session.errors)
        run.model_rejected.update(session.model_rejected)
        if index == 0:
            # 兼容字段：既有读侧（日志 / 测试 / 控制台脚本）看第一组。
            run.intent, run.chain, run.strategy = session.intent, session.chain, session.strategy
            run.rotation = session.rotation
            run.intent_generator = session.intent_generator
            run.chain_generator = session.chain_generator
            run.strategy_generator = session.strategy_generator

        for kind, envelope, generator in (
            (INTENT_KIND, session.intent, session.intent_generator),
            (STRATEGY_KIND, session.strategy, session.strategy_generator),
        ):
            if envelope is None:
                continue
            conclusion = _conclusion(
                kind,
                envelope,
                group,
                generator=generator,
                model_rejected=session.model_rejected.get(kind),
            )
            run.conclusions.append(conclusion)
            event = WireEvent(
                event_id=str(conclusion["event_id"]),
                event_type=CONCLUSION_EVENT_TYPE,
                session_id=session.session_id,
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

    # 整批处理完才提交位置与去重状态（`N3` / `N8`）。
    _commit(checkpoint, run, decisions, dedupe, ceiling)
    return run


def _group_by_session(
    admitted: Sequence[Observation], max_sessions: int
) -> tuple[list[list[Observation]], int]:
    """把去重后的观测按 `session_id` 分组（保持首次出现顺序），并限制组数。

    返回 `(组列表, 被截断的会话数)`。

    两条约定：

    - **保持首次出现顺序**（事件是 newest-first）⇒ 最近活跃的会话先被分析；
    - 构造顺序（`dict` 保留插入序）而不是排序：排序会让「哪一组先分析」变成一个额外决定。

    被截断的会话**必须**能被调用方看见（返回值不是 None，也不静默丢弃）：
    它们下一轮仍在事件窗口里，会重新进入分组。
    """
    buckets: dict[str, list[Observation]] = {}
    for obs in admitted:
        buckets.setdefault(obs.session_id, []).append(obs)
    groups = list(buckets.values())
    if max_sessions <= 0 or len(groups) <= max_sessions:
        return groups, 0
    return groups[:max_sessions], len(groups) - max_sessions


def _analyze_session(
    observations: Sequence[Observation],
    *,
    evidence: EvidenceIndex,
    decoys: Sequence[str],
    client: AnalysisClient | None,
) -> SessionAnalysis:
    """跑完**一个会话**的三步：意图 → 攻击链 → 策略。

    三步的顺序与回落语义与改造前逐字一致（模型优先、失败回落、各自标注产出者）——
    变的只是「一次只处理一个会话」，因此 `AR-12` 的证据引用不再跨会话。
    """
    session = SessionAnalysis(
        session_id=observations[0].session_id, observations=list(observations)
    )
    admitted = session.observations
    observations_payload = _observations_payload(admitted)

    # ① 意图：模型优先，失败/被拒则回落确定性规则。两条路**各自标注**产出者 ——
    #    禁止用规则输出冒充模型输出，反之亦然（AR-15）。
    #    模型给的 `evidence_ids` **也要过 AR-12**：结论里有两处证据引用（顶层的
    #    「本轮参与分析的事件」与 `data` 里「模型引用的证据」），后者是模型自由填的，
    #    不校验就等于让结论携带**编造的证据 ID**（审计/控制台会当成真的）。
    data, reason = _model_candidate(
        INTENT_KIND, observations_payload, client=client, session_id=session.session_id
    )
    if data is not None:
        try:
            _assert_evidence_ids(data, evidence)
        except Exception as exc:
            data = None
            reason = f"证据引用校验失败：{type(exc).__name__}: {exc}"
    session.intent = accept(data) if data is not None else recognize(admitted)
    session.intent_generator = MODEL_GENERATOR if data is not None else RULES_GENERATOR
    if reason is not None:
        session.model_rejected[INTENT_KIND] = reason

    # ② 攻击链：模型优先；模型给的证据引用**仍要过 AR-12**（与确定性版同语义）。
    data, chain_reason = _model_candidate(
        CHAIN_KIND, observations_payload, client=client, session_id=session.session_id
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
        session.chain = chain_from_model
        session.chain_generator = MODEL_GENERATOR
    else:
        try:
            # AR-12：链里引用的证据 ID **必须**真实存在于遥测（缓存即已取到的证据集合）
            session.chain = reconstruct(admitted, evidence=evidence)
        except Exception as exc:
            session.errors.append(f"攻击链作废：{exc}")
            session.chain = None
        session.chain_generator = RULES_GENERATOR
    if chain_reason is not None:
        session.model_rejected[CHAIN_KIND] = chain_reason

    # ③ 策略：模型优先，失败/被拒则回落确定性版。
    data, reason = _model_candidate(
        STRATEGY_KIND,
        _strategy_payload(session, admitted=admitted, decoys=decoys),
        client=client,
        session_id=session.session_id,
    )
    session.strategy = (
        accept(data)
        if data is not None
        else generate(
            StrategyInput(
                intent_category=_category_of(session.intent),
                chain_stages=[
                    stage.name for stage in (session.chain.stages if session.chain else ())
                ],
                broken_signals=[
                    sig.kind for sig in (session.chain.broken if session.chain else ())
                ],
                available_decoys=decoys,
            )
        )
    )
    session.strategy_generator = MODEL_GENERATOR if data is not None else RULES_GENERATOR
    if reason is not None:
        session.model_rejected[STRATEGY_KIND] = reason

    session.rotation = decide_rotation(
        [sig.kind for sig in (session.chain.broken if session.chain else ())]
    ).to_wire()
    return session


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
    session: SessionAnalysis, *, admitted: Sequence[Observation], decoys: Sequence[str]
) -> dict[str, Any]:
    """策略步的输入：意图类别 + 链的阶段 / 识破信号 + 可用诱饵（都来自**本会话**的真实结果）。"""
    return {
        "intent": _category_of(session.intent),
        "stages": [stage.name for stage in (session.chain.stages if session.chain else ())],
        "broken_signals": [sig.kind for sig in (session.chain.broken if session.chain else ())],
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
    # 游标跨轮复用（进程内）：只取上一次之后的事件，不再每轮重读最近 limit 条（FIX-6）。
    # ⚠️ 进程重启后游标丢失 ⇒ 重新读一个窗口；结论靠 AR-11 幂等不会重复写，代价是少量重算。
    checkpoint = Checkpoint()
    while True:
        run = run_once(
            port,
            limit=args.limit,
            window_seconds=args.window,
            cache=cache,
            client=client,
            checkpoint=checkpoint,
        )
        print(f"[L4] {run.summary()}", flush=True)
        if run.window_gap:
            # 丢事件必须可见：否则效果口径会偏低而看不出来。
            print(f"  · ⚠ 窗口缺口：{run.window_gap}", flush=True)
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
