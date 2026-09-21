"""内容对象与内容清单。

契约：[`../../../docs/spec/ai-contract.md`](../../../docs/spec/ai-contract.md) §2 / §3。

三件事：

1. **内容对象**：`content_id` / `resource` / `profile_id` / `variant` / `body` / `marker` /
   `checksum` / `version` / `generated_at` / `generator`
   —— `content_id` 与 `checksum` **必须**是确定性函数（同输入逐字节同输出）。
   注意这是**产物层**的性质，与「生成期能不能复现」是两件事：接模型后生成不可复现，
   但同一份内容仍必得同一个 id
   （[ADR-0026](../../docs/background/decisions/0026-cloud-model-backend.md) 决定 2）；
2. **内容库**：键 = 一致性键 `content:<profile_id>:<resource>:<variant>:<version>`
   （会话只决定**选哪个变体**，不进键 —— 见 `AR-30` 的划界）；
   阶段 A 是内存实现，生产侧是核心的 `store.ContentStore`；
3. **清单**：把内容库聚合成核心可装载的文件（**只含通过护栏的内容**）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import hashlib
import json
from collections.abc import Iterable, Mapping
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from .ports import Artifact, Sink

CONTENT_KIND = "content"
MANIFEST_SCHEMA_VERSION = 1
SELECTOR_SESSION = "session"

MAX_BODY_BYTES = 65536
"""单条内容体上限（字节，UTF-8）；超限该条**不入清单**（契约 §3）。"""

DEFAULT_MARKER = "</body>"


def as_int(value: object, *, field_name: str) -> int:
    """把值取成整数；不可取值时抛**带字段名**的错误（禁止静默转换）。"""
    if isinstance(value, bool) or not isinstance(value, int | str):
        raise ValueError(f"{field_name} 必须是整数，实际 {type(value).__name__}")
    try:
        return int(value)
    except ValueError as exc:
        raise ValueError(f"{field_name} 不是合法整数：{value!r}") from exc


def body_bytes(body: str) -> bytes:
    return body.encode("utf-8")


def checksum_of(body: str) -> str:
    """`body` 的 SHA-256（UTF-8 字节，小写十六进制）。"""
    return hashlib.sha256(body_bytes(body)).hexdigest()


def content_id_of(
    *,
    kind: str,
    resource: str,
    profile_id: str,
    variant: int,
    version: int,
    body: str,
) -> str:
    """内容标识：**确定性**（同输入必得同 ID）—— 让「同一份内容」可被复算与对账。"""
    raw = "\x00".join(
        [
            kind,
            resource,
            profile_id,
            str(as_int(variant, field_name="variant")),
            str(as_int(version, field_name="version")),
            body,
        ]
    ).encode("utf-8")
    return "c-" + hashlib.sha256(raw).hexdigest()[:16]


def content_key(*, profile_id: str, resource: str, variant: int, version: int) -> str:
    """内容库的键（一致性键；契约 §4）。"""
    resolved_variant = as_int(variant, field_name="variant")
    resolved_version = as_int(version, field_name="version")
    return f"content:{profile_id}:{resource}:{resolved_variant}:{resolved_version}"


@dataclass(frozen=True)
class ContentObject:
    """一条内容（契约 §2）。字段名**必须**与 `docs/spec/ai-contract.md` 一致。"""

    content_id: str
    resource: str
    profile_id: str
    variant: int
    body: str
    checksum: str
    version: int
    generated_at: str
    generator: str
    marker: str = DEFAULT_MARKER
    kind: str = CONTENT_KIND

    @property
    def key(self) -> str:
        return content_key(
            profile_id=self.profile_id,
            resource=self.resource,
            variant=self.variant,
            version=self.version,
        )

    def to_wire(self) -> dict[str, Any]:
        return {
            "content_id": self.content_id,
            "kind": self.kind,
            "resource": self.resource,
            "profile_id": self.profile_id,
            "variant": self.variant,
            "body": self.body,
            "marker": self.marker,
            "checksum": self.checksum,
            "version": self.version,
            "generated_at": self.generated_at,
            "generator": self.generator,
        }


def make_content(
    *,
    resource: str,
    profile_id: str,
    variant: int,
    body: str,
    version: int,
    generated_at: str,
    generator: str,
    marker: str = "",
) -> ContentObject:
    """构造内容对象；`checksum` 与 `content_id` 由内容算出（**不接受**调用方传入）。"""
    if not body:
        raise ValueError("内容体不得为空（docs/spec/ai-contract.md §2）")
    if not resource:
        raise ValueError("resource 不得为空（docs/spec/ai-contract.md §2）")
    resolved_variant = as_int(variant, field_name="variant")
    resolved_version = as_int(version, field_name="version")
    if resolved_variant < 0:
        raise ValueError(f"variant 必须 ≥ 0，实际 {resolved_variant}")
    return ContentObject(
        content_id=content_id_of(
            kind=CONTENT_KIND,
            resource=resource,
            profile_id=profile_id,
            variant=resolved_variant,
            version=resolved_version,
            body=body,
        ),
        resource=resource,
        profile_id=profile_id,
        variant=resolved_variant,
        body=body,
        checksum=checksum_of(body),
        version=resolved_version,
        generated_at=generated_at,
        generator=generator,
        marker=marker or DEFAULT_MARKER,
    )


@dataclass
class ContentStore(Sink):
    """内存内容库（阶段 A）—— **唯一写入者是生成器，且只写通过护栏的内容**。

    生产侧对应核心的 `store.ContentStore`（`MD-20`：核心唯一的 I/O 出口）。

    **显式继承 `Sink`**（而不是靠结构性推断）：一个安全关键缝不该让类型检查器去猜，
    也不该让读者去核对方法签名（ADR-0025 决定 2）。
    """

    items: dict[str, ContentObject] = field(default_factory=dict)

    def put(self, artifact: Artifact) -> str:
        """按一致性键写入；同键覆盖（同输入必得同 ID ⇒ 覆盖是幂等的）。

        参数类型写成 `Sink` 缝的 `Artifact`（而不是 `ContentObject`）：`Sink` 的契约是
        「接受任何产物」，具体实现在这里判断自己认不认得 —— 不认得就**显式报错**，
        而不是等到 `.key` 上报一个 `AttributeError`
        （[ADR-0025](../../docs/background/decisions/0025-generic-guardrailed-outlet.md) 决定 2）。
        """
        if not isinstance(artifact, ContentObject):
            raise TypeError(f"ContentStore 只接受 ContentObject，实际 {type(artifact).__name__}")
        self.items[artifact.key] = artifact
        return artifact.key

    def __len__(self) -> int:
        return len(self.items)

    def entries(self) -> list[ContentObject]:
        """按（资源、变体）排序 —— 清单**必须**是确定性的（同内容同字节）。"""
        return sorted(self.items.values(), key=lambda item: (item.resource, item.variant))


def build_manifest(
    contents: Iterable[ContentObject],
    *,
    version: int,
    variants: int,
    generated_at: str,
    generator: str,
    selector: str = SELECTOR_SESSION,
) -> dict[str, Any]:
    """把内容聚合成清单（契约 §3）。

    **超单条上限的内容在这里被丢弃**（不是截断）并记入 `skipped` —— 截断过的页面会缺 `</body>`，
    注入时根本找不到标记，不如不注入。
    """
    resolved_variants = as_int(variants, field_name="variants")
    if resolved_variants < 1:
        raise ValueError(f"variants 必须 ≥ 1，实际 {resolved_variants}")
    grouped: dict[str, dict[str, Any]] = {}
    skipped: list[dict[str, str]] = []
    for item in sorted(contents, key=lambda c: (c.resource, c.variant)):
        size = len(body_bytes(item.body))
        if size > MAX_BODY_BYTES:
            skipped.append(
                {
                    "resource": item.resource,
                    "variant": str(item.variant),
                    "why": f"内容体 {size} 字节超过上限 {MAX_BODY_BYTES}",
                }
            )
            continue
        if not 0 <= item.variant < resolved_variants:
            skipped.append(
                {
                    "resource": item.resource,
                    "variant": str(item.variant),
                    "why": f"variant 落在 [0, {resolved_variants}) 之外",
                }
            )
            continue
        entry = grouped.setdefault(
            item.resource,
            {"resource": item.resource, "profile_id": item.profile_id, "bodies": []},
        )
        entry["bodies"].append(
            {
                "variant_id": item.variant,
                "content_id": item.content_id,
                "checksum": item.checksum,
                "body": item.body,
                "marker": item.marker,
            }
        )
    return {
        "manifest_version": MANIFEST_SCHEMA_VERSION,
        "version": as_int(version, field_name="version"),
        "selector": selector,
        "variants": resolved_variants,
        "generated_at": generated_at,
        "generator": generator,
        "entries": [grouped[key] for key in sorted(grouped)],
        "skipped": skipped,
    }


def manifest_bytes(manifest: Mapping[str, Any]) -> bytes:
    """清单的字节形式：**确定性**（键序固定 + 紧凑分隔符）—— 同一份清单必得同一串字节。"""
    return json.dumps(manifest, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode(
        "utf-8"
    )


def write_manifest(path: Path, manifest: Mapping[str, Any]) -> int:
    """写出清单文件，返回字节数（**确定性**：同输入同字节）。"""
    payload = manifest_bytes(manifest)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(payload + b"\n")
    return len(payload) + 1
