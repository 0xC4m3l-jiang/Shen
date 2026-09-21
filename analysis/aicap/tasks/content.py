"""`kind=content`：产出「资源 × 变体」的欺骗内容（阶段 A：**确定性模板生成器**）。

**为什么是确定性的**：这是**阶段 A 的工程性质**，**不是** `AR-30` 的要求 ——
`AR-30` 管的是**响应路径**（「禁止在响应路径上引入非确定性」，见 `docs/design/architecture.md`）。
同一 `(resource, profile_id, variant, version)` 逐字节可复现，让内容可回归、清单可复现。

**接模型后这条性质不成立**（实测：`temperature=0` 也 3 次 3 个结果，见
`docs/background/research/ai-live-probe/README.md` §1.2）。热路径的一致改由三条保证：
产物**冻结进清单** + `content_id` 由**内容体**算出 + **会话钉定**
（[ADR-0026](../../../docs/background/decisions/0026-cloud-model-backend.md) 决定 2）。

阶段 B 接模型（**2026-09-21 落地**）：`produce` 分两条路（模型 / 模板），
按 `TaskSpec.payload["use_model"]` 选（由 CLI 的 `--llm` 置位）。
两条路都走同一个出口与同一套护欏，**并且各自在产物里如实标注 `generator`**：
模型路 `model-v1`、模板路 `template-v1`（`AR-15`：不得用一者的输出冒充另一者）。

**身份字段不从模型取**：`resource` / `variant` 由生成器从 `spec` 取值
—— 模型只负责 `body`（与可选的 `marker`）。
理由：这两个字段是**清单的键**，模型一次笔误就会把内容挂到错资源上，而那属「修复不了只能拒绝」的错；
内容本身（`body`）仍然是模型产出的。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import logging
from collections.abc import Mapping
from typing import TYPE_CHECKING, Any

from ...llm.contract import Field, Schema
from ...llm.extract import ExtractionError
from ..content import CONTENT_KIND, ContentObject, make_content
from ..model import Unavailable
from ._produce import structured_candidate
from ._registry import GuardrailProfile, Task, TaskLimits

if TYPE_CHECKING:  # pragma: no cover - 只用于类型标注，避免运行期循环导入
    from ..model import AnalysisClient
    from ..service import TaskSpec

GENERATOR = "template-v1"
"""生成器标识（进内容对象，供审计定位「这份内容是哪个版本产出的」）。"""

logger = logging.getLogger(__name__)

MODEL_GENERATOR = "model-v1"
"""模型路径的生成器标识（与 L4 三任务同一口径）。"""

GENERATORS: tuple[str, ...] = (GENERATOR, MODEL_GENERATOR)
"""生成器闭集（由 `Field.allowed` 守住）：写别的值会被后置检查拒绝。"""

CONTENT_SCHEMA = Schema(
    name="content",
    fields=(
        Field("resource", (str,), True, 256),
        Field("variant", (int,), True),
        Field("body", (str,), True),
        Field("marker", (str,), False, 64),
        Field("generator", (str,), True, 32, allowed=GENERATORS),
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

    为什么是片段：注入复用 `deception/injection` 的**插入**语义
    （插在标记之前，`ST-5` 不新增替换语义），
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
    """产出候选输出（**未过检查**）：模型优先、失败回落模板。

    走哪条路看 `spec.payload["use_model"]`（CLI 的 `--llm` 置位）——**不靠环境变量隐式决定**：
    同一个命令在任何环境里都该产出同一类东西，否则「这次是模型还是模板」无法复现。

    回落**不静默**：产物写明 `generator=template-v1`，日志里有 `WARNING`（含原因）。
    抽取失败（模型不是 JSON）算回落；**结构不合契约**则是后置检查拒绝 —— 两者不是一回事。
    """
    if _wants_model(spec):
        candidate = _model_candidate(prompt, spec, client)
        if candidate is not None:
            return {**candidate, "generator": MODEL_GENERATOR}
    return {**_template_candidate(spec), "generator": GENERATOR}


def _wants_model(spec: TaskSpec) -> bool:
    """`payload["use_model"]` 为真才走模型（默认模板 —— 与阶段 A 的行为逐字一致）。"""
    return bool(spec.payload.get("use_model", False))


def _model_candidate(
    prompt: str, spec: TaskSpec, client: AnalysisClient
) -> Mapping[str, Any] | None:
    """让模型产出 body；不可用/抽取失败返回 `None`（由调用方回落模板并记日志）。

    **身份字段覆盖为 spec 的值**（`resource` / `variant`）：它们决定内容进哪个清单条目，
    不能由模型的一次笔误决定（与 `AR-12` 同一纪律：可核的字段不由模型自述）。
    """
    try:
        raw = structured_candidate(prompt, spec, client)
    except (Unavailable, ExtractionError) as exc:
        logger.warning("aicap：模型路径不可用，回落模板生成器：%s", exc)
        return None
    candidate: dict[str, Any] = {
        "resource": str(spec.payload.get("resource", "")),
        "variant": _as_variant(spec.payload.get("variant")),
        "body": raw.get("body"),
    }
    marker = raw.get("marker")
    if isinstance(marker, str) and marker:
        candidate["marker"] = marker
    return candidate


def _template_candidate(spec: TaskSpec) -> Mapping[str, Any]:
    """确定性模板生成器（阶段 A 的实现；仍是默认路径与落底面）。"""
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
    """把过完检查的输出变成**内容对象**（契约 §2）。

    `generator` 取**候选自报的那一个**（由 `produce` 按实际路径填），不是任务默认值 ——
    否则模型产出的内容会被记成模板产出，审计上就是假的（`AR-15`）。
    """
    profile_id = str(spec.payload.get("profile_id", ""))
    return make_content(
        resource=str(checked["resource"]),
        profile_id=profile_id,
        variant=_as_variant(checked["variant"]),
        body=str(checked["body"]),
        version=_as_variant(spec.payload.get("version")),
        generated_at=generated_at,
        generator=str(checked["generator"]),
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
