"""生成内容黑名单（`AR-22`）—— 至少覆盖三类。

- **泄露类**：内网地址、真实主机名、真实文件路径、真实业务标识；
- **自曝类**：「我是 AI」「这是蜜罐」「作为语言模型」；
- **超长类**：超过该用途长度上限。

业务专有标识无法凭空得知，**必须**由部署方在启动时经 `identifiers` 注入（`AR-24`：资源随版本分发）。
"""

from __future__ import annotations

import re
from collections.abc import Iterable
from dataclasses import dataclass
from pathlib import Path

import yaml

from analysis.llm.limits import PURPOSE_LIMITS, LimitError

DEFAULT_RESOURCE = Path(__file__).with_name("resources") / "blacklist.yaml"

_PRIVATE_IP = re.compile(
    r"\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}"
    r"|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}"
    r"|192\.168\.\d{1,3}\.\d{1,3})\b"
)


@dataclass(frozen=True)
class Finding:
    """一条命中；`kind` ∈ {leak, self_disclosure, overlength}。"""

    kind: str
    match: str
    detail: str

    def to_wire(self) -> dict[str, str]:
        return {"kind": self.kind, "match": self.match, "detail": self.detail}


@dataclass(frozen=True)
class Blacklist:
    self_disclosure: tuple[re.Pattern[str], ...]
    identifiers: tuple[str, ...] = ()
    leak_patterns: tuple[re.Pattern[str], ...] = (_PRIVATE_IP,)

    @classmethod
    def from_resource(
        cls, path: Path = DEFAULT_RESOURCE, *, identifiers: Iterable[str] = ()
    ) -> Blacklist:
        raw = yaml.safe_load(path.read_text(encoding="utf-8"))
        if not isinstance(raw, dict) or "self_disclosure" not in raw:
            raise ValueError(f"黑名单资源缺 self_disclosure：{path}")
        patterns = tuple(re.compile(p, re.IGNORECASE) for p in raw["self_disclosure"])
        extra = (*tuple(re.compile(item) for item in raw.get("leak_patterns", [])), _PRIVATE_IP)
        return cls(
            self_disclosure=patterns,
            identifiers=tuple(identifiers),
            leak_patterns=extra,
        )

    def check(self, text: str, *, purpose: str) -> list[Finding]:
        """检查一段生成内容；返回全部命中（空列表 = 通过）。用途未登记即失败（`AR-23`）。"""
        if purpose not in PURPOSE_LIMITS:
            raise LimitError(f"未知用途 {purpose!r}（AR-23）")
        found: list[Finding] = []
        for pattern in self.leak_patterns + self.self_disclosure:
            for match in pattern.finditer(text):
                kind = "self_disclosure" if pattern in self.self_disclosure else "leak"
                found.append(Finding(kind, match.group(0), f"命中规则 {pattern.pattern}"))
        for identifier in self.identifiers:
            if identifier and identifier in text:
                found.append(Finding("leak", identifier, "命中真实业务标识（启动时注入）"))
        if len(text) > PURPOSE_LIMITS[purpose]:
            found.append(
                Finding(
                    "overlength",
                    f"{len(text)} 字符",
                    f"超过用途 {purpose} 上限 {PURPOSE_LIMITS[purpose]}",
                )
            )
        return found
