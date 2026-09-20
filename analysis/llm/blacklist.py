"""生成内容黑名单（`AR-22`）—— 至少覆盖三类。

- **泄露类**：内网地址、真实主机名、真实文件路径、真实业务标识；
- **自曝类**：「我是 AI」「这是蜜罐」「作为语言模型」；
- **超长类**：超过该用途长度上限。

业务专有标识无法凭空得知，**必须**由部署方在启动时经 `identifiers` 注入（`AR-24`：资源随版本分发）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import re
from collections.abc import Iterable
from dataclasses import dataclass
from pathlib import Path

import yaml

from .limits import PURPOSE_LIMITS, LimitError

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

    def check(self, text: str, *, purpose: str, cap: int | None = None) -> list[Finding]:
        """检查一段生成内容；返回全部命中（空列表 = 通过）。用途未登记即失败（`AR-23`）。

        `cap` 是**调用方给的额外收紧上限**（任务级，如 `TaskLimits.max_output`）：
        有效上限取 `min(用途上限, cap)`。`None` = 只用用途上限（默认行为不变）。
        有一个「声明了却不生效」的实例就够危险了 —— 它看起来像保护。
        """
        if purpose not in PURPOSE_LIMITS:
            raise LimitError(f"未知用途 {purpose!r}（AR-23）")
        limit = PURPOSE_LIMITS[purpose]
        if cap is not None:
            if cap <= 0:
                raise LimitError(f"任务级上限必须为正，实际 {cap}（AR-23）")
            limit = min(limit, cap)
        found: list[Finding] = []
        for pattern in self.leak_patterns + self.self_disclosure:
            for match in pattern.finditer(text):
                kind = "self_disclosure" if pattern in self.self_disclosure else "leak"
                found.append(Finding(kind, match.group(0), f"命中规则 {pattern.pattern}"))
        for identifier in self.identifiers:
            if identifier and identifier in text:
                found.append(Finding("leak", identifier, "命中真实业务标识（启动时注入）"))
        if len(text) > limit:
            found.append(
                Finding(
                    "overlength",
                    f"{len(text)} 字符",
                    f"超过上限 {limit}（用途 {purpose}）",
                )
            )
        return found
