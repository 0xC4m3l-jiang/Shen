"""任务注册表：每个 `kind` **必须**声明 `schema` + `guardrail_profile` + `limits`。

`AR-33` 的结构闸门：

- 缺声明 ⇒ **启动期断言失败**（fail-closed）—— 不是「运行时才发现」，也不是「跳过护栏继续生成」；
- 未登记的 `kind` ⇒ 调用被**拒绝**（`UnregisteredKind`），禁止猜着生成；
- 护栏档案引用的提示词**必须**能加载（`AR-24`），长度用途**必须**已登记（`AR-23`）。

设计上刻意「注册表是数据、生成器是函数」：加一个任务种类 = 加一条登记 + 一个 `produce`，
护栏不需要重写，也无法绕开（两道闸门在 `service.generate()` 内部）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from collections.abc import Callable, Mapping
from dataclasses import dataclass
from types import MappingProxyType
from typing import TYPE_CHECKING, Any

from ...llm.contract import Schema
from ...llm.limits import PURPOSE_LIMITS
from ..guardrail import prompts as guardrail_prompts

if TYPE_CHECKING:  # pragma: no cover - 只用于类型标注，避免运行期循环导入
    from ..model import AnalysisClient
    from ..ports import Artifact
    from ..service import TaskSpec

# produce：把（已渲染的提示词, 任务输入, 模型客户端）变成**候选输出**（还没过护栏）。
Produce = Callable[[str, "TaskSpec", "AnalysisClient"], Mapping[str, Any]]
# build：把（过完护栏的输出, 任务输入, 生成时刻）变成**产物**（内核只认 `Artifact` 这个缝）。
Build = Callable[[Mapping[str, Any], "TaskSpec", str], "Artifact"]


@dataclass(frozen=True)
class GuardrailProfile:
    """护栏档案（契约 §1.4）：提示词模板 + **受检字段** + 风格一致性判据。

    `checked_fields` 回答「后置护栏的黑名单与风格检查作用在输出的**哪几个字段**上」。
    它是**必填**且**必须非空** —— 缺了它，第二个消费方接入时那两关会**静默失效**
    （[ADR-0025](../../../docs/background/decisions/0025-generic-guardrailed-outlet.md)
    决定 3）。
    """

    name: str
    prompt: str
    style_terms: tuple[str, ...]
    checked_fields: tuple[str, ...]

    def assert_declared(self, *, kind: str) -> None:
        if not self.name:
            raise AssertionError(f"任务 {kind} 的护栏档案缺 name（AR-33）")
        if not self.prompt:
            raise AssertionError(f"任务 {kind} 的护栏档案缺 prompt（AR-33）")
        if not self.style_terms:
            raise AssertionError(f"任务 {kind} 的护栏档案缺 style_terms（AR-33）")
        if not self.checked_fields:
            raise AssertionError(
                f"任务 {kind} 的护栏档案缺 checked_fields —— "
                "缺了它黑名单与风格检查会静默失效（AR-33 / ADR-0025 决定 3）"
            )


@dataclass(frozen=True)
class TaskLimits:
    """长度与体积纪律（`AR-23`）：用途**必须**已登记，输出上限**必须**为正。

    `max_output` 是**任务级**上限，与用途上限**取较小者**生效（ADR-0025 决定 3）。
    不要把它当文档：它真的参与判定（在 `guardrail/inspect.py` 里交给 `blacklist.check` 的 `cap`）。
    """

    purpose: str
    max_output: int

    def assert_declared(self, *, kind: str) -> None:
        if self.purpose not in PURPOSE_LIMITS:
            raise AssertionError(
                f"任务 {kind} 的长度用途 {self.purpose!r} 未登记（AR-23）；"
                f"已登记：{sorted(PURPOSE_LIMITS)}"
            )
        if self.max_output <= 0:
            raise AssertionError(f"任务 {kind} 的 max_output 必须为正（AR-23）")


@dataclass(frozen=True)
class Task:
    """一个任务种类（登记项）。"""

    kind: str
    schema: Schema
    guardrail_profile: GuardrailProfile
    limits: TaskLimits
    produce: Produce
    build: Build
    generator: str
    requires_model: bool = False

    def assert_declared(self) -> None:
        """声明逐条核对；缺一样即**抛异常**（启动期断言，`AR-33`）。"""
        if not self.kind:
            raise AssertionError("任务 kind 不得为空（AR-33）")
        if self.schema is None or not self.schema.fields:
            raise AssertionError(f"任务 {self.kind} 缺 schema（AR-33）")
        if self.guardrail_profile is None:
            raise AssertionError(f"任务 {self.kind} 缺 guardrail_profile（AR-33）")
        if self.limits is None:
            raise AssertionError(f"任务 {self.kind} 缺 limits（AR-33）")
        if not callable(self.produce):
            raise AssertionError(f"任务 {self.kind} 缺 produce（AR-33）")
        if not callable(self.build):
            raise AssertionError(f"任务 {self.kind} 缺 build（AR-33）")
        if not self.generator:
            raise AssertionError(f"任务 {self.kind} 缺 generator 标识（AR-33）")
        self.guardrail_profile.assert_declared(kind=self.kind)
        self.limits.assert_declared(kind=self.kind)


class UnregisteredKind(LookupError):
    """`kind` 未登记 —— **必须**拒绝，禁止猜着生成（`AR-33`）。"""


def _registry() -> dict[str, Task]:
    # 延迟导入：任务实现需要 service 的 TaskSpec（类型），反过来 service 只需要本函数。
    from .content import CONTENT_TASK

    return {CONTENT_TASK.kind: CONTENT_TASK}


_REGISTRY: Mapping[str, Task] = MappingProxyType(_registry())
"""已登记的任务（只读映射 —— 登记表是常量，不是运行期可变的容器）。"""


def kinds() -> tuple[str, ...]:
    return tuple(sorted(_REGISTRY))


def task_for(kind: str) -> Task:
    """取任务定义；未登记**必须**拒绝（`AR-33`）。"""
    task = _REGISTRY.get(kind)
    if task is None:
        raise UnregisteredKind(
            f"未登记的任务种类 {kind!r}；已登记：{list(kinds())}（AR-33：禁止绕过护栏的生成路径）"
        )
    return task


def assert_startup(*, prompt_dir: Any = None) -> tuple[str, ...]:
    """启动期断言（`AR-33` / `AR-24` / `AR-23`）：任一不满足即让进程起不来。

    检查四件事：

    1. 每个任务三样声明齐全（`schema` / `guardrail_profile` / `limits`）；
    2. 护栏档案引用的提示词模板**真实存在**且三段式合格；
    3. 长度用途已登记；
    4. 注册表非空（空注册表说明导入路径坏了 —— 静默通过等于放行一切）。
    """
    if not _REGISTRY:
        raise AssertionError("任务注册表为空 —— 注册路径可能坏了（AR-33）")
    names = (
        guardrail_prompts.assert_startup()
        if prompt_dir is None
        else guardrail_prompts.assert_startup(prompt_dir)
    )
    for kind in kinds():
        task = task_for(kind)
        task.assert_declared()
        if task.guardrail_profile.prompt not in names:
            raise AssertionError(
                f"任务 {kind} 的提示词 {task.guardrail_profile.prompt!r}"
                f" 不在已加载模板 {list(names)}（AR-24）"
            )
    return names
