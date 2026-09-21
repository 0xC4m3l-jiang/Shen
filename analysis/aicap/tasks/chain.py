"""`kind=chain`：把零散观测串成攻击链（`chain`，模型路径）。

与 `intent` 同构：**模型优先、失败回落**，确定性版是
`analysis/chain/reconstruct.py` 的 `reconstruct()`（阶段排序 + 证据校验）。

**模型这一路额外背一条硬约束**：它给的 `evidence_ids` 仍要过证据校验（`AR-12`）——
校验在 worker 里做（那里才有 `EvidenceIndex`），本模块只声明形状与护栏。
不通过就整条链作废、回落确定性版 —— 与今天同语义。

`rationale` 是本任务唯一的自由文本，所以它是**受检字段**（黑名单 / 长度 / 风格）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any

from ...llm.schemas import CHAIN_KIND, CHAIN_SCHEMA
from ..ports import WireArtifact
from ._produce import structured_candidate
from ._registry import GuardrailProfile, Task, TaskLimits

if TYPE_CHECKING:  # pragma: no cover - 只用于类型标注，避免运行期循环导入
    from ..model import AnalysisClient
    from ..service import TaskSpec

GENERATOR = "model-v1"

CHAIN_STYLE_TERMS: tuple[str, ...] = (
    *("reconnaissance", "exploitation", "lateral_movement", "exfiltration", "persistence"),
    "阶段",
    "证据",
    "缺失",
    "侦察",
    "利用",
    "横向移动",
    "外传",
    "持久化",
)
"""画像术语：链的置信理由**必须**说到阶段、证据或缺失中的至少一个。

为什么这三类词：链的价值就在于「哪些阶段成立了、拿什么证据、哪里是缺口」。
一段不点这三样的理由，等于没说这条链为什么成立（`AR-33` 的风格判据）。
"""

CHAIN_PROFILE = GuardrailProfile(
    name="l4-chain",
    prompt="chain",
    style_terms=CHAIN_STYLE_TERMS,
    checked_fields=("rationale",),
)
"""护栏档案：受检字段只有 `rationale`。

**已知缺口**：`stages[].name` 与 `broken_decoy_signals[]` 的**闭集**没有被机器守住 ——
`Field.allowed` 只作用于字符串字段，管不到列表元素。worker 只对它们做**形状**校验
（每一项必须是带 `name` / `evidence_ids` 的对象），取值越界目前靠提示词约束。
这一条记在变更包 §7，不假装它已被守住。
"""

CHAIN_LIMITS = TaskLimits(purpose="conclusion", max_output=4000)


def produce(prompt: str, spec: TaskSpec, client: AnalysisClient) -> Mapping[str, Any]:
    """让模型产出候选（未过护栏）。"""
    return structured_candidate(prompt, spec, client)


def build(checked: Mapping[str, Any], spec: TaskSpec, generated_at: str) -> WireArtifact:
    """把过完护栏的输出变成产物（链数据的 wire 形状与 `Chain.to_wire()` 同键）。"""
    del spec, generated_at
    return WireArtifact(dict(checked))


CHAIN_TASK = Task(
    kind=CHAIN_KIND,
    schema=CHAIN_SCHEMA,
    guardrail_profile=CHAIN_PROFILE,
    limits=CHAIN_LIMITS,
    produce=produce,
    build=build,
    generator=GENERATOR,
    requires_model=True,
)
"""`kind=chain` 的登记项。"""
