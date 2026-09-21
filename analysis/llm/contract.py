"""独立契约校验层（`AR-15`）。

提示词只描述任务与上下文；**输出结构由本层校验**。
校验失败**必须抛异常** —— 禁止返回默认值、禁止静默降级。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from collections.abc import Mapping, Sized
from dataclasses import dataclass, field
from typing import Any


class ContractError(ValueError):
    """结构不合契约 —— 调用方**必须**放行整轮作废（不得填默认值，`AR-15` / `AR-21`）。"""


@dataclass(frozen=True)
class Field:
    name: str
    types: tuple[type, ...]
    required: bool = True
    max_len: int | None = None
    """字符串/列表的长度上限；超限即失败（长度纪律另见 `limits.py` 的分用途上限）。"""
    allowed: tuple[str, ...] | None = None
    """闭集白名单（可选，作用于字符串字段）：值**必须**在其中，越界即失败。

    为什么需要它：`types=(str,)` 只挡「不是字符串」，挡不住「是字符串但不在闭集里」。
    越界值在消费方很容易变成 `x if x in SET else DEFAULT` 这种回落 ——
    那就是默认值，`AR-15` 明确禁止（实证：`intent.recognize` 曾把越界类别静默回落成
    `reconnaissance`；今天不可达，接模型后可达）。
    """


@dataclass(frozen=True)
class Schema:
    name: str
    fields: tuple[Field, ...] = field(default_factory=tuple)
    allow_extra: bool = False

    def by_name(self) -> dict[str, Field]:
        return {f.name: f for f in self.fields}


def validate(payload: Mapping[str, Any], schema: Schema) -> dict[str, Any]:
    """校验并返回规范化副本；任何不合规**抛异常**（不修正、不补默认值）。"""
    if not isinstance(payload, Mapping):
        raise ContractError(f"{schema.name}: 顶层必须是对象，实际 {type(payload).__name__}")

    known = schema.by_name()
    if not schema.allow_extra:
        extra = set(payload) - set(known)
        if extra:
            raise ContractError(f"{schema.name}: 出现契约外字段 {sorted(extra)}")

    out: dict[str, Any] = {}
    for field_def in schema.fields:
        if field_def.name not in payload:
            if field_def.required:
                raise ContractError(f"{schema.name}: 缺必填字段 {field_def.name}")
            continue
        value = payload[field_def.name]
        if isinstance(value, bool) and bool not in field_def.types:
            raise ContractError(f"{schema.name}.{field_def.name}: 布尔不是合法类型")
        if not isinstance(value, field_def.types):
            expected = "/".join(t.__name__ for t in field_def.types)
            raise ContractError(
                f"{schema.name}.{field_def.name}: 期望 {expected}，实际 {type(value).__name__}"
            )
        if field_def.allowed is not None and value not in field_def.allowed:
            # 越界**不回落、不修正**：回落是默认值，而 AR-15 禁止默认值（也禁止修复后放行）
            raise ContractError(
                f"{schema.name}.{field_def.name}: 必须是闭集之一 "
                f"{list(field_def.allowed)}，实际 {value!r}"
            )
        if field_def.max_len is not None:
            if not isinstance(value, Sized):
                raise ContractError(f"{schema.name}.{field_def.name}: 设了长度上限但值不可度量长度")
            if len(value) > field_def.max_len:
                raise ContractError(
                    f"{schema.name}.{field_def.name}: 超长 {len(value)} > {field_def.max_len}"
                )
        out[field_def.name] = value
    return out
