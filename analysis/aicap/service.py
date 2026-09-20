"""**唯一出口**：`generate(TaskSpec) → Envelope`（`AR-33`）。

本文件是**内核**：它只做五件事，且**不认识**任何具体任务 ——

1. **取任务**（`_registry.task_for`）—— 未登记即拒绝（`AR-33`）；
2. **前置护栏**：渲染三段式提示词（含不可信数据区，`AR-31` / `AR-24`）；
3. **生成**：模板生成器或模型客户端（未配置模型即**显式失败**，`AR-15`）；
4. **后置护栏**：结构 / 黑名单 / 长度 / 风格（`AR-15` / `AR-22` / `AR-23` / `AR-33`）；
5. **交 `Sink`**：只有过完护栏才会调 `sink.put`。

**「内核不认识内容」的判据**：把 `kind=content` 整块删掉，本文件与 `guardrail/` 仍应原样可用
（[ADR-0025](../../docs/background/decisions/0025-generic-guardrailed-outlet.md) 决定 1）。
因此本文件**禁止** import 任何具体任务模块（如 `tasks.content`），
也**禁止** import 任何具体存储（如 `content.ContentStore`）。

四条不可绕过的纪律都在本文件里：

1. **只有一个入口** —— 消费方拿到的是 `generate`，不是任务实现，也不是模型客户端；
2. **内部强制走护栏** —— 前置（三段式提示词 + 不可信数据区）→ 生成 →
   后置（结构 / 黑名单 / 长度 / 风格）；调用方**无法**选择跳过；
3. **模型客户端只在本文件被 import** —— `AR-33` 的门禁结构检查就核这一条
   （`scripts/archcheck`：除本文件与测试外，`analysis/` 下禁止 import 模型客户端）；
4. **未过护栏的内容不入库** —— `store.put` 只在护栏通过之后发生。

失败语义（与 `AR-16` 一致）：

- **内容不合规**（结构 / 黑名单 / 长度 / 风格）⇒ 返回 `Envelope{accepted:false, rejected_reason}`，
  并**不进** `store`、**不进**清单；
- **程序性错误**（未登记的 kind、缺护栏档案、提示词资源坏了）⇒ **抛异常**（启动期就该校验出来，
  不允许在运行期悄悄降级）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import logging
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any

from ..llm.envelope import Envelope, accept, reject
from . import model as model_seam
from .guardrail import inspect as guardrail_inspect
from .guardrail import prompts as guardrail_prompts
from .ports import Artifact, Sink
from .tasks import _registry

if TYPE_CHECKING:  # pragma: no cover - 只用于类型标注，避免运行期循环导入
    from .tasks._registry import Task

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class TaskSpec:
    """调用方**唯一**能表达的东西（契约 §1.1）。

    `payload` 是攻击者可控内容的**唯一**入口：它会整体进入提示词的数据区（`AR-31`），
    **禁止**把 payload 的内容拼进指令区。
    """

    kind: str
    session_id: str
    deadline_s: float
    payload: Mapping[str, Any] = field(default_factory=dict)

    def untrusted_rows(self) -> list[dict[str, Any]]:
        """进入提示词数据区的行（结构化 + 原样保留，**不做净化** —— `AR-31`）。"""
        row: dict[str, Any] = {"kind": self.kind, "session_id": self.session_id}
        row.update({str(key): value for key, value in self.payload.items()})
        return [row]


def generate(
    spec: TaskSpec,
    *,
    client: model_seam.AnalysisClient | None = None,
    sink: Sink | None = None,
    identifiers: Sequence[str] = (),
    generated_at: str | None = None,
) -> Envelope:
    """按 `spec.kind` 取任务并执行（登记表是**唯一**的判断点）。

    - `client`：阶段 B 的模型客户端；阶段 A 不传（模板生成器不调模型）；
    - `sink`：产物出口（内容库 / 规格库 / 测试替身）；**只有过护栏的内容**会写进去；
    - `identifiers`：部署方注入的真实业务标识（`AR-22` 泄露类）；
    - `generated_at`：生成时刻（只作审计，不参与任何判定 —— `MD-6` 的精神）。
    """
    task = _registry.task_for(spec.kind)  # 未登记 → 抛 UnregisteredKind（AR-33）
    return run_task(
        task,
        spec,
        client=client,
        sink=sink,
        identifiers=identifiers,
        generated_at=generated_at,
    )


def run_task(
    task: Task,
    spec: TaskSpec,
    *,
    client: model_seam.AnalysisClient | None = None,
    sink: Sink | None = None,
    identifiers: Sequence[str] = (),
    generated_at: str | None = None,
) -> Envelope:
    """**内核**：执行一个任务，两道护栏都在本函数内部（`AR-33`）。

    为什么从 `generate()` 里解出来：`generate` 是「查表 + 执行」，本函数是执行本身。
    分开之后，（①） 内核不再认识登记表，（②）测试能直接塞一个假任务验证内核**真的**任务无关
    （[ADR-0025](../../docs/background/decisions/0025-generic-guardrailed-outlet.md) 决定 1）。

    失败语义（与 `AR-16` 一致）：

    - **内容不合规**（结构 / 黑名单 / 长度 / 风格）⇒ 返回
      `Envelope{accepted:false, rejected_reason}`，并**不建产物**、**不调 sink**；
    - **程序性错误**（未登记的 kind、缺护栏档案、提示词资源坏了）⇒ **抛异常**（启动期就该校验出来，
      不允许在运行期悄悄降级）。
    """
    if spec.kind != task.kind:
        # 不匹配就是调用方接错了线：数据区会带上错误的 kind，产物的归属也就错了（AR-31）
        raise ValueError(
            f"TaskSpec.kind={spec.kind!r} 与任务的 kind={task.kind!r} 不一致"
            "（AR-31：数据区不得错位）"
        )
    task.assert_declared()

    # ① 前置护栏：三段式提示词（含数据区），渲染本身就会校验（AR-24 / AR-31）
    prompt = guardrail_prompts.render(
        task.guardrail_profile,
        untrusted_rows=spec.untrusted_rows(),
    )

    # ② 生成：模板生成器或模型客户端。模型未配置时**显式失败**（AR-15），不允许降级成模板。
    chosen = model_seam.resolve(client)
    candidate = task.produce(prompt, spec, chosen)

    # ③ 后置护栏：结构 → 黑名单 → 长度 → 风格；任一不过即拒绝（不建产物、不写 sink）
    checked, reasons = guardrail_inspect.check(
        candidate,
        task=task,
        blacklist=guardrail_inspect.blacklist_of(tuple(identifiers)),
    )
    if reasons:
        detail = "；".join(f"[{item['check']}] {item['detail']}" for item in reasons)
        logger.warning("aicap: 护栏拒绝 kind=%s：%s", task.kind, detail)
        return reject(f"护栏拒绝：{detail}")

    # ④ 建产物并交给 sink：**只**在护栏通过之后（AR-33）
    stamp = generated_at or datetime.now(UTC).isoformat()
    artifact: Artifact = task.build(checked, spec, stamp)
    if sink is not None:
        sink.put(artifact)
    return accept(artifact.to_wire())


def startup_assert() -> tuple[str, ...]:
    """启动期断言（`AR-33` / `AR-24`）：进程/任务起不来**好过**带病生成。

    生成器（`python -m analysis.aicap`）与任何常驻消费方在启动时**必须**调用它。
    """
    return _registry.assert_startup()


def message_of(envelope: Envelope) -> str:
    """把信封压成一行摘要（日志与 CLI 共用）。

    只读 `data` 里**任务无关**的两个字段（`content_id` / `variant` 是 `kind=content` 的字段，
    缺失时退化成「已接受」）—— 内核不假设产物类型。
    """
    if envelope.accepted:
        content = envelope.data
        ident = content.get("content_id") or content.get("id") or "-"
        return f"accepted id={ident}"
    return f"rejected：{envelope.rejected_reason}"
