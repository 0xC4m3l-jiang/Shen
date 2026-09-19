"""三段式 JSON 容错提取（`AR-17`）。

① 整段解析 → ② markdown 代码围栏内提取 → ③ 对每个 `{` 位置尝试解析（**必须**设扫描位置上限）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import json
import re
from typing import Any

DEFAULT_SCAN_CAP = 256
"""第 ③ 段的扫描位置上限；防止超长输出把 CPU 拖死（`AR-17`）。"""

_FENCE = re.compile(r"```(?:json|JSON)?\s*(?P<body>.*?)```", re.DOTALL)


class ExtractionError(ValueError):
    """三段都失败 —— 调用方**必须**走 `envelope.reject`，禁止返回默认值（`AR-15`）。"""


def extract_json(text: str, *, scan_cap: int = DEFAULT_SCAN_CAP) -> dict[str, Any]:
    """按三段式取第一个可解析的 JSON 对象；失败抛 `ExtractionError`。"""
    if scan_cap <= 0:
        raise ValueError("scan_cap 必须为正（AR-17）")

    # ① 整段解析
    whole = _try_parse(text)
    if whole is not None:
        return whole

    # ② markdown 代码围栏
    for match in _FENCE.finditer(text):
        fenced = _try_parse(match.group("body"))
        if fenced is not None:
            return fenced

    # ③ 逐个 '{' 位置尝试（受扫描上限约束）
    scanned = 0
    for index, char in enumerate(text):
        if char != "{":
            continue
        scanned += 1
        if scanned > scan_cap:
            break
        for candidate in (text[index:], _balanced(text, index)):
            if candidate is None:
                continue
            parsed = _try_parse(candidate)
            if parsed is not None:
                return parsed

    raise ExtractionError(f"三段式提取失败（扫描上限 {scan_cap}，文本长度 {len(text)}）")


def _try_parse(raw: str) -> dict[str, Any] | None:
    try:
        value = json.loads(raw.strip())
    except (json.JSONDecodeError, ValueError):
        return None
    return value if isinstance(value, dict) else None


def _balanced(text: str, start: int) -> str | None:
    """从 `start`（指向 `{`）取出括号配对的子串，尊重字符串与转义。"""
    depth = 0
    in_string = False
    escaped = False
    for index in range(start, len(text)):
        char = text[index]
        if in_string:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                in_string = False
            continue
        if char == '"':
            in_string = True
        elif char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return text[start : index + 1]
    return None
