"""`kind=intent`：把去重后的观测归类为攻击意图（`intent`，模型路径）。

**它只是「模型那条路」** —— 今天真正跑在默认路径上的是确定性规则
（`analysis/intent/recognize.py` 的 `rule_hits`）。worker 的语义是
**模型优先、失败回落**：模型这一路被拒 / 不可用 / 超时，就回落规则版，
并把原因记进 `AnalysisRun.model_rejected`（近线不得炸，`NI-1`）。

阶段 A 的确定性版**保留**，因为它是：
① 离线可复核的对照物（模型给了什么、规则给了什么，能并排看）；
② 模型不可用时的兜底（不产结论比产一个没依据的结论更坏 —— `AR-15`）。

边界（三条都不许破）：

- **这里不判定**：输出只是结论数据，判定与处置在核心（`AR-2`）；
- **这里不查证据是否存在**：`evidence_ids` 的引用校验在遥测侧（`AR-12`）；
- **这里不碰策略**：策略只由 `kind=strategy` 产出（`MD-12`）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any

from ...llm.schemas import INTENT_KIND, INTENT_SCHEMA
from ..ports import WireArtifact
from ._produce import structured_candidate
from ._registry import GuardrailProfile, Task, TaskLimits

if TYPE_CHECKING:  # pragma: no cover - 只用于类型标注，避免运行期循环导入
    from ..model import AnalysisClient
    from ..service import TaskSpec

GENERATOR = "model-v1"
"""生成器标识：**模型路径**的产物都写这个（规则路径写 `rules-v1`，见 `worker.py`）。"""

INTENT_STYLE_TERMS: tuple[str, ...] = (
    *("reconnaissance", "exploitation", "lateral_movement", "exfiltration", "persistence"),
    "侦察",
    "利用",
    "横向移动",
    "外传",
    "持久化",
)
"""画像术语（`style_terms`）：`rationale` 必须至少命中一个。

为什么把它当判据：结论**必须自述类别**。若一段置信理由里连类别名都不出现，
它大概率是「不懂装懂」的泛泛之谈 —— 那正是这条风格检查要拦的东西（`AR-33`）。
"""

INTENT_PROFILE = GuardrailProfile(
    name="l4-intent",
    prompt="intent",
    style_terms=INTENT_STYLE_TERMS,
    checked_fields=("rationale",),
)
"""护栏档案：受检字段只有 `rationale` —— 它是本任务唯一的自由文本。

为什么不是 `category`：闭集由 `INTENT_SCHEMA.category.allowed` 守（越界即拒），
而黑名单 / 长度 / 风格这三关要的是**可长可短的文本**：`category` 是枚举值，
拿它去查「至少命中一个画像术语」是永真或永假的假检查。
"""

INTENT_LIMITS = TaskLimits(purpose="conclusion", max_output=4000)
"""长度用途 `conclusion`（面向存储的结论）；任务级上限 4000 字符，与用途上限取较小者。"""


def produce(prompt: str, spec: TaskSpec, client: AnalysisClient) -> Mapping[str, Any]:
    """让模型产出候选（未过护栏）。"""
    return structured_candidate(prompt, spec, client)


def build(checked: Mapping[str, Any], spec: TaskSpec, generated_at: str) -> WireArtifact:
    """把过完护栏的输出变成产物：结论本身就是 wire 形状，没有额外对象语义。"""
    del spec, generated_at  # 结论不含「生成时刻」：它不参与任何判定（MD-6 的精神）
    return WireArtifact(dict(checked))


INTENT_TASK = Task(
    kind=INTENT_KIND,
    schema=INTENT_SCHEMA,
    guardrail_profile=INTENT_PROFILE,
    limits=INTENT_LIMITS,
    produce=produce,
    build=build,
    generator=GENERATOR,
    requires_model=True,
)
"""`kind=intent` 的登记项（`AR-33`：缺任何一样都让启动期断言失败）。"""
