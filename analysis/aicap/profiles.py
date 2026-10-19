"""服务画像：内容生成的**输入**，而不是写死在生成器里的词表。

为什么把画像做成数据（JSON）：

- 写死词表意味着所有接入服务共用同一套措辞 —— 这正是 `SHEN-M5-01` / `SHEN-M2-02`
  指出的问题（「同一模板的不同排列」，服务之间可以互相串号）；
- 画像进数据后可以版本化、可以评审、可以不改代码就调整某个服务的口径；
- 生成器于是变成「画像 → 内容」的纯函数，测试可以直接喂两份画像比较差异。

**本模块只负责加载与校验，不臆造业务语义。** 画像内容来自人工整理或后续的提取步骤
（`SHEN-M5-02`）；这里没有对应文件时退回**结构性**通用画像，绝不按路径猜业务含义。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import json
import re
from collections.abc import Mapping
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path

PROFILE_DIR = Path(__file__).parent / "resources" / "profiles"
"""画像目录：一个服务一个 JSON。新增服务 = 加一个文件，不改代码。"""

GENERIC_PROFILE_ID = "generic"

# 画像 id 的合法形态：只允许字母数字与 `._-`，且不得以点开头/结尾。
#
# 为什么必须校验：画像 id 来自策略载荷 / 命令行，会被拼进文件路径
# （`PROFILE_DIR / f"{id}.json"`）。不校验就等于把 `../` 交给调用方 —— 那属于
# 「本来只该读一个目录，结果能读全盘」。宁可报错，也不要读一个猜出来的文件。
_PROFILE_ID_RE = re.compile(r"^[A-Za-z0-9_-][A-Za-z0-9._-]{0,63}$")


def valid_profile_id(profile_id: str) -> bool:
    """画像 id 是否可安全地拼进文件路径（读与写共用同一判据）。"""
    return bool(_PROFILE_ID_RE.match(profile_id)) and ".." not in profile_id


# 兼容旧名（包内私有用法的历史拼写）。
_valid_id = valid_profile_id


GENERIC_TERMS: tuple[str, ...] = ("Records", "Status", "Revision", "Summary")
"""通用画像的**结构性**词汇（不含任何业务语义），仅用于「没有为该服务准备画像」的场合。

它同时承担两个职责：`style_terms` 的非空来源（`AR-33` 要求），以及模板生成器的措辞层扰动来源。
"""

GENERIC_FIELDS: tuple[str, ...] = ("record_id", "state", "updated_at")
"""通用字段名：结构性的，不暗示任何具体业务。"""


@dataclass(frozen=True)
class ServiceProfile:
    """一个服务的生成画像。

    `entity_fields` 的键是**资源实体名**（`/api/orders` → `orders`）；`"*"` 是兜底。
    实体名到字段的映射是「同一服务下不同资源要有不同字段」的唯一依据
    （`SHEN-M5-01` 验收：`/api/orders` 与 `/api/users` 的字段名重合率 ≤ 30%）。
    """

    profile_id: str
    domain_terms: tuple[str, ...]
    entity_fields: Mapping[str, tuple[str, ...]]
    status_values: tuple[str, ...] = ()
    roles: tuple[str, ...] = ()
    source: str = "builtin"

    def fields_for(self, entity: str) -> tuple[str, ...]:
        """取该实体的字段名；未登记的实体用画像兜底（再不行用通用字段）。"""
        if entity and entity in self.entity_fields:
            return self.entity_fields[entity]
        if "*" in self.entity_fields:
            return self.entity_fields["*"]
        return GENERIC_FIELDS


GENERIC_PROFILE = ServiceProfile(
    profile_id=GENERIC_PROFILE_ID,
    domain_terms=GENERIC_TERMS,
    entity_fields={"*": GENERIC_FIELDS},
    source="generated",
)


class ProfileError(ValueError):
    """画像文件存在但不可用 —— 拒绝猜着用（宁可报错，也不要产出错口径的内容）。"""


def _as_terms(value: object, field: str, *, profile_id: str) -> tuple[str, ...]:
    if value is None:
        return ()
    if not isinstance(value, list) or not all(isinstance(x, str) and x.strip() for x in value):
        raise ProfileError(f"画像 {profile_id!r} 的 {field} 必须是字符串数组")
    return tuple(x.strip() for x in value)


def _parse(raw: object, *, profile_id: str, source: str) -> ServiceProfile:
    if not isinstance(raw, dict):
        raise ProfileError(f"画像 {profile_id!r} 必须是 JSON 对象")
    terms = _as_terms(raw.get("domain_terms"), "domain_terms", profile_id=profile_id)
    if not terms:
        # `style_terms` 为空会让风格检查静默失效（AR-33 的启动期断言也会挂）—— 这里直接拒绝。
        raise ProfileError(f"画像 {profile_id!r} 缺 domain_terms（风格检查需要非空术语）")

    fields_raw = raw.get("entity_fields", {})
    if not isinstance(fields_raw, dict):
        raise ProfileError(f"画像 {profile_id!r} 的 entity_fields 必须是对象")
    entity_fields: dict[str, tuple[str, ...]] = {}
    for entity, names in fields_raw.items():
        entity_fields[str(entity)] = _as_terms(
            names, f"entity_fields[{entity}]", profile_id=profile_id
        )

    return ServiceProfile(
        profile_id=profile_id,
        domain_terms=terms,
        entity_fields=entity_fields,
        status_values=_as_terms(raw.get("status_values"), "status_values", profile_id=profile_id),
        roles=_as_terms(raw.get("roles"), "roles", profile_id=profile_id),
        source=source,
    )


@lru_cache(maxsize=64)
def load_profile(profile_id: str) -> ServiceProfile:
    """按 id 加载画像；没有对应文件时退回通用画像（**不报错** —— 未知服务仍要能产出内容）。

    文件存在但格式坏了则抛 `ProfileError`：那是配置错误，猜着用会产出错口径的内容。
    """
    pid = (profile_id or "").strip()
    if not pid or pid == GENERIC_PROFILE_ID:
        return GENERIC_PROFILE
    if not _valid_id(pid):
        raise ProfileError(f"画像 id 非法：{pid!r}（只允许字母数字与 ._-，且不得含 ..）")

    path = PROFILE_DIR / f"{pid}.json"
    if not path.is_file():
        return ServiceProfile(
            profile_id=pid,
            domain_terms=GENERIC_TERMS,
            entity_fields=GENERIC_PROFILE.entity_fields,
            source="missing",
        )

    try:
        raw = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise ProfileError(f"画像 {pid!r} 不是合法 JSON：{exc}") from exc
    return _parse(raw, profile_id=pid, source=str(path.name))
