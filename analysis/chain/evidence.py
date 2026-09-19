"""证据引用校验（`AR-12`）。

L4 生成结论若引用证据 ID，**必须**在写入前校验该 ID **真实存在**于遥测存储中。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from collections.abc import Iterable, Sequence
from typing import Protocol


class EvidenceIndex(Protocol):
    """遥测存储的**只读**证据索引（`store` 是核心唯一 I/O 出口，`MD-20`）。"""

    def exists(self, event_id: str) -> bool: ...


class MissingEvidence(ValueError):
    """引用了不存在的证据 —— 结论**必须**作废，禁止写入（`AR-12`）。"""

    def __init__(self, missing: Sequence[str]) -> None:
        super().__init__(f"引用了不存在的证据 ID：{sorted(missing)}（AR-12）")
        self.missing = tuple(sorted(missing))


def assert_exists(index: EvidenceIndex, evidence_ids: Iterable[str]) -> list[str]:
    """全部存在才返回（去重、保序）；有一个不存在即抛 `MissingEvidence`。"""
    seen: dict[str, None] = {}
    for event_id in evidence_ids:
        if event_id:
            seen.setdefault(event_id, None)
    missing = [event_id for event_id in seen if not index.exists(event_id)]
    if missing:
        raise MissingEvidence(missing)
    return list(seen)


class InMemoryEvidenceIndex:
    """测试与离线用的内存索引。"""

    def __init__(self, event_ids: Iterable[str] = ()) -> None:
        self._ids = set(event_ids)

    def exists(self, event_id: str) -> bool:
        return event_id in self._ids
