"""后置护栏：**独立于提示词的代码层校验**（`AR-15` 的核心要求）。

四关，按顺序执行，**任一不过即拒绝**（禁止默认值、禁止静默降级）：

| # | 检查 | 依据 |
| --- | --- | --- |
| 1 | 结构（schema） | `AR-15` |
| 2 | 黑名单三类（泄露 / 自曝 / 超长） | `AR-22` |
| 3 | 长度（分用途上限） | `AR-23` |
| 4 | 风格一致性（至少命中一个画像术语） | `AR-33` |

**故意不在这里写「尝试修复」** —— 修复过的模型输出就不再是模型输出，而是一个没人验证过的新东西。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any

from ...llm.blacklist import Blacklist
from ...llm.contract import ContractError, validate

if TYPE_CHECKING:  # pragma: no cover - 只用于类型标注，避免运行期循环导入
    from ..tasks._registry import Task

CHECK_SCHEMA = "schema"
CHECK_BLACKLIST = "blacklist"
CHECK_LENGTH = "length"
CHECK_STYLE = "style"


def blacklist_of(identifiers: tuple[str, ...] = ()) -> Blacklist:
    """构造黑名单（`AR-22`）；真实业务标识由部署方注入（`AR-24`）。"""
    return Blacklist.from_resource(identifiers=identifiers)


def check(
    candidate: Mapping[str, Any],
    *,
    task: Task,
    blacklist: Blacklist,
) -> tuple[dict[str, Any], list[dict[str, str]]]:
    """执行后置四关。

    返回 `(校验后的输出, 拒绝原因列表)`；原因列表非空即视为**拒绝**（调用方必须丢弃候选输出）。
    每条原因都写清**哪一关**与**命中了什么** —— 拒绝理由要能被审计，不能只说「不合规」。
    """
    reasons: list[dict[str, str]] = []

    try:
        checked = validate(candidate, task.schema)
    except (ContractError, ValueError) as exc:
        reasons.append({"check": CHECK_SCHEMA, "detail": f"{exc}（AR-15）"})
        return {}, reasons

    body = checked.get("body")
    if isinstance(body, str):
        findings = blacklist.check(body, purpose=task.limits.purpose)
        for finding in findings:
            check_id = CHECK_LENGTH if finding.kind == "overlength" else CHECK_BLACKLIST
            reasons.append(
                {
                    "check": check_id,
                    "detail": (
                        f"{finding.kind}: {finding.match} —— {finding.detail}（AR-22 / AR-23）"
                    ),
                }
            )
        if not any(term in body for term in task.guardrail_profile.style_terms):
            reasons.append(
                {
                    "check": CHECK_STYLE,
                    "detail": (
                        f"未命中任何画像术语 {list(task.guardrail_profile.style_terms)}"
                        " —— 不像该文档画像，拒绝（AR-33）"
                    ),
                }
            )

    return checked, reasons
