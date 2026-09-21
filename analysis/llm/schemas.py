"""L4 三个模型任务（`intent` / `chain` / `strategy`）的**输出契约**（`AR-15` / `AR-16`）。

**为什么这些 schema 放在纪律层而不是各自的领域模块**：`AR-33` 的结构检查
（`make archcheck` 的 `MD-4` 项）规定 `analysis/aicap/**` 只允许依赖 `analysis.llm` 与它自己 ——
而任务登记项**必须**声明 schema（`Task.schema`），所以声明它的那个文件不能 import
`analysis.intent` / `analysis.chain` / `analysis.strategy`。本模块是两侧**都能**依赖的那一层
（既有先例：`twophase.FINALIZE_SCHEMA` 也在纪律层）。
契约**只定义一次**（`MD-5`）：领域模块从这里导入，不各写一份 ——
两边各写一份的后果见 [`docs/spec/events.md`](../../docs/spec/events.md) §4 的教训。

**这里只有结构，没有判定**：`intent` 的五类闭集、`CHAIN_SCHEMA` 的阶段列表、
`strategy` 的阈值键与灰度上限 —— 都是「模型必须产出什么形状」，
判定与处置仍只在核心与 `aicap/service.py` 里发生（`AR-2` / `AR-33`）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from .contract import Field, Schema

INTENT_KIND = "intent"
"""`intent` 的 `kind` 字串：**只在这里写一次**（`MD-5`）。

它同时是：任务登记表的主键（`AR-33`）、调用方 `TaskSpec.kind` 的取值、
结论事件里的 `kind` 字段。三处各写一个字面量就会漂移，所以定义在本模块，两边导入。
"""

CHAIN_KIND = "chain"
STRATEGY_KIND = "strategy"

CATEGORIES: tuple[str, ...] = (
    "reconnaissance",
    "exploitation",
    "lateral_movement",
    "exfiltration",
    "persistence",
)
"""五类攻击意图 / 五类攻击链阶段；**禁止**新增第六类（闭集由两个 schema 的 `allowed` 守住）。"""

BROKEN_SIGNAL_KINDS: tuple[str, ...] = (
    "skipped",
    "hit_without_followup",
    "explicit_compare",
    "multi_session_same_method",
)
"""四类识破信号（[ADR-0016](../../docs/background/decisions/0016-decoy-polymorphism.md)）。

它是**提示词里**的闭集（模型被要求只从这四类里选）；
`Field.allowed` 只作用于字符串字段，管不到列表元素，所以列表侧靠提示词 + 后置人工审计 ——
这是一处**已知缺口**，记在变更包 §7 而不是假装它被机器守住了。
"""

INTENT_SCHEMA = Schema(
    name="intent",
    fields=(
        Field("category", (str,), True, 32, allowed=CATEGORIES),
        Field("confidence", (float, int), True),
        Field("evidence_ids", (list,), True, 256),
        Field("rationale", (str,), True, 500),
    ),
)
"""`intent` 的输出契约。

`category` 用 `allowed=CATEGORIES` 守**闭集**：越界即 `ContractError`
（`AR-15`：**禁止**静默回落成某一个默认类别）。实现见 `contract.Field.allowed`。
"""

CHAIN_SCHEMA = Schema(
    name="chain",
    fields=(
        Field("stages", (list,), True, 64),
        Field("broken_decoy_signals", (list,), True, 64),
        Field("rationale", (str,), True, 500),
    ),
)
"""`chain` 的输出契约。

`rationale` 是**后置护栏的受检字段**（黑名单 / 长度 / 风格作用在它上面）——
列表类字段没有文本可检，所以每个模型任务都必须有一个文本字段承担这项检查。
"""

STRATEGY_SCHEMA = Schema(
    name="strategy",
    fields=(
        Field("decoy_selection", (list,), True, 64),
        Field("gray_pct", (int,), True),
        Field("threshold_suggestions", (dict,), True),
        Field("rationale", (str,), True, 500),
    ),
)
"""`strategy` 的输出契约（`rationale` 同 `CHAIN_SCHEMA`，是受检文本字段）。"""

MAX_GRAY_PCT = 20
"""灰度上限：策略永远**不得**建议一次性全量接管（`INT-11` 的阶梯放开）。"""

THRESHOLD_FLOOR: dict[str, float] = {"route_mirage": 0.3, "block": 0.6}
"""阈值下界：建议值**禁止**低于它（否则影子期就会开始改道/拦截）。"""
