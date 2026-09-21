"""L4 的观测模型（只读输入）。

L4 拿到的**唯一**输入是遥测事件（`common/api/telemetry/v1` 的形状，经适配器回流）。
攻击者可控字段（`path` / `user_agent` 等）遵循 `AR-31`：以**结构化数据**传入，原样保留、不做净化。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from collections.abc import Iterable, Mapping, Sequence
from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class Observation:
    """一条遥测事件的最小只读视图。"""

    event_id: str
    at: str
    source: str
    session_id: str
    method: str = ""
    path: str = ""
    user_agent: str = ""
    action: str = ""
    signals: tuple[str, ...] = ()
    severity: str = "none"

    @classmethod
    def from_wire(cls, raw: Mapping[str, Any]) -> Observation:
        """按**跨语言事件契约**解析（见 `docs/spec/events.md`）。

        判定事件由核心的 `control.DecisionRecord` 序列化而来：`decision_id` 是幂等键，
        在本模块里就是 `event_id`（证据引用以它为准）。
        """
        return cls(
            event_id=str(raw.get("decision_id", raw.get("event_id", ""))),
            at=str(raw.get("at", "")),
            source=str(raw.get("source_ip", raw.get("source", ""))),
            session_id=str(raw.get("session_id", "")),
            method=str(raw.get("method", "")),
            path=str(raw.get("path", "")),
            user_agent=str(raw.get("user_agent", "")),
            action=str(raw.get("action", "")),
            signals=tuple(str(s) for s in raw.get("signals", ())),
            severity=str(raw.get("severity", "none")),
        )

    def to_untrusted(self) -> dict[str, Any]:
        """送给 LLM 的形状：**原样**携带全部字段（`AR-31`：禁止为净化而丢弃）。"""
        return {
            "decision_id": self.event_id,
            "at": self.at,
            "source": self.source,
            "session_id": self.session_id,
            "method": self.method,
            "path": self.path,
            "user_agent": self.user_agent,
            "action": self.action,
            "signals": list(self.signals),
            "severity": self.severity,
        }


def parse_all(raws: Iterable[Mapping[str, Any]]) -> list[Observation]:
    return [Observation.from_wire(raw) for raw in raws]


def index_by_id(observations: Sequence[Observation]) -> dict[str, Observation]:
    return {obs.event_id: obs for obs in observations if obs.event_id}
