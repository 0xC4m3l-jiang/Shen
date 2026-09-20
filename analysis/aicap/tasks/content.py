"""`kind=content`：产出「资源 × 变体」的欺骗内容（阶段 A：**确定性模板生成器**）。

**为什么是确定性的**：热路径永不调模型（`AR-30` / `AR-29`），因此同一
`(resource, profile_id, variant, version)` 的生成结果**必须**逐字节可复现 —— 这既是
`AR-30`（同会话同资源同答案）的前提，也让内容可回归（同一份输入两次生成必须完全一致）。

阶段 B 接模型时，**只换 `produce`**：提示词已在 `service.generate()` 内渲染并校验过
（三段式 + 数据区），后置四关也照旧执行。本任务的 `requires_model=False` 就是这个意思：
模板生成器不需要模型。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any

from ...llm.contract import Field, Schema
from ..content import CONTENT_KIND, ContentObject, make_content
from ._registry import GuardrailProfile, Task, TaskLimits

if TYPE_CHECKING:  # pragma: no cover - 只用于类型标注，避免运行期循环导入
    from ..model import AnalysisClient
    from ..service import TaskSpec

GENERATOR = "template-v1"
"""生成器标识（进内容对象，供审计定位「这份内容是哪个版本产出的」）。"""

CONTENT_SCHEMA = Schema(
    name="content",
    fields=(
        Field("resource", (str,), True, 256),
        Field("variant", (int,), True),
        Field("body", (str,), True),
        Field("marker", (str,), False, 64),
    ),
)
"""候选输出的结构契约（后置护栏第一关，`AR-15`）。"""

PROFILE_VOCAB: tuple[str, ...] = (
    "Service status",
    "Service health",
    "Service summary",
    "Service overview",
    "Service report",
    "Service inventory",
    "Service catalog",
    "Service registry",
)
"""画像术语（`style_terms`）：内容**必须**至少命中一个，否则判为「不像该画像」（`AR-33`）。

同时也是模板生成器的**措辞层扰动来源**（`variant` 决定用哪一条）—— 一处定义，两处使用。
"""

FIELD_ORDERS: tuple[tuple[str, ...], ...] = (
    ("id", "name", "status"),
    ("id", "region", "status"),
    ("id", "owner", "status"),
    ("id", "tier", "status"),
    ("id", "name", "revision"),
    ("id", "region", "revision"),
    ("id", "owner", "revision"),
    ("id", "tier", "revision"),
)
"""字段层扰动：`variant` 决定字段组合（同一槽位里 `variant` 固定 ⇒ 输出稳定）。"""

CONTENT_PROFILE = GuardrailProfile(
    name="site-content",
    prompt="content",
    style_terms=PROFILE_VOCAB,
    checked_fields=("body",),
)
"""内容任务的护栏档案；受检字段只有 `body`（黑名单与风格只作用在正文上，`AR-22` / `AR-33`）。"""

CONTENT_LIMITS = TaskLimits(purpose="deception_content", max_output=65536)


def _record_id(*, variant: int, version: int) -> str:
    """确定性的假记录号（**不是**随机数 —— `AR-30` 禁止响应路径上的非确定性）。"""
    return f"{4_100_000 + version * 100_003 + variant * 977}"


def _render_body(*, resource: str, profile_id: str, variant: int, version: int) -> str:
    """生成一个 **HTML 片段**（不是完整文档）。

    为什么是片段：注入复用 `edge/injection` 的**插入**语义（插在标记之前，`ST-5` 不新增替换语义），
    所以内容得是「能插进去的一段」，而不是另一个 `<html>` 文档。
    """
    headline = PROFILE_VOCAB[variant % len(PROFILE_VOCAB)]
    fields = FIELD_ORDERS[variant % len(FIELD_ORDERS)]
    record = _record_id(variant=variant, version=version)
    rows = "\n".join(
        f"    <li>{name}: {record if name == 'id' else f'value-{idx}-{variant}'}</li>"
        for idx, name in enumerate(fields)
    )
    return (
        '<section class="service-detail">\n'
        f"  <h2>{headline}</h2>\n"
        f'  <p class="source">{profile_id} API response for <code>{resource}</code>.</p>\n'
        '  <ul class="records">\n'
        f"{rows}\n"
        "  </ul>\n"
        f'  <p class="revision">revision {version} · variant {variant} · record {record}</p>\n'
        "</section>\n"
    )


def produce(prompt: str, spec: TaskSpec, client: AnalysisClient) -> Mapping[str, Any]:
    """产出候选输出（**未过护栏**）。

    `prompt` 与 `client` 在本实现里**刻意不用**：阶段 A 是模板生成器，不调模型。
    提示词仍由 `service.generate()` 渲染并校验（三段式 + 数据区，`AR-31` / `AR-24`），
    所以阶段 B 换成模型实现时，这条路径的其余部分**一行都不用改**。
    """
    del prompt, client
    resource = str(spec.payload.get("resource", ""))
    profile_id = str(spec.payload.get("profile_id", ""))
    variant = _as_variant(spec.payload.get("variant"))
    version = _as_variant(spec.payload.get("version"))
    return {
        "resource": resource,
        "variant": variant,
        "body": _render_body(
            resource=resource, profile_id=profile_id, variant=variant, version=version
        ),
    }


def build(checked: Mapping[str, Any], spec: TaskSpec, generated_at: str) -> ContentObject:
    """把过完护栏的输出变成**内容对象**（契约 §2）。"""
    profile_id = str(spec.payload.get("profile_id", ""))
    return make_content(
        resource=str(checked["resource"]),
        profile_id=profile_id,
        variant=_as_variant(checked["variant"]),
        body=str(checked["body"]),
        version=_as_variant(spec.payload.get("version")),
        generated_at=generated_at,
        generator=GENERATOR,
        marker=str(checked.get("marker", "")),
    )


def _as_variant(value: object) -> int:
    if isinstance(value, bool) or not isinstance(value, int | str):
        raise ValueError(f"变体/版本号必须是整数，实际 {type(value).__name__}")
    try:
        return int(value)
    except ValueError as exc:
        raise ValueError(f"变体/版本号不是合法整数：{value!r}") from exc


CONTENT_TASK = Task(
    kind=CONTENT_KIND,
    schema=CONTENT_SCHEMA,
    guardrail_profile=CONTENT_PROFILE,
    limits=CONTENT_LIMITS,
    produce=produce,
    build=build,
    generator=GENERATOR,
    requires_model=False,
)
