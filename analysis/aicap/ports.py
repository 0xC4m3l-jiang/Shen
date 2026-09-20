"""产出缝隙（ports）：内核与消费方之间的**唯一接口面**。

[ADR-0025](../../docs/background/decisions/0025-generic-guardrailed-outlet.md) 决定 2。

本文件**不 import 本包的任何其他模块** —— 消费方（内容库、未来的规格库）与内核都只依赖它，
于是「接入一个新消费方」不必把内核的依赖（护栏、登记表、提示词资源）一起拖进来。

两个 Protocol 都是**结构性**的：满足方法签名即可。`ContentStore` 显式继承 `Sink`
只是为了让「它就是 Sink」在类型检查器那里没有歧义 —— 一个安全关键缝不该依赖推断。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from typing import Any, Protocol


class Artifact(Protocol):
    """任务产物的**唯一**要求：能把自己变成 wire 形状（进 `Envelope.data`）。

    内核只认这个缝 —— 它不知道产物是内容对象、规格文件，还是别的什么。
    具体产物类型由 `spec/` 定义（`MD-5`：跨模块类型不得各自定义）。
    """

    def to_wire(self) -> dict[str, Any]: ...


class Sink(Protocol):
    """产物出口：**只有过完护栏**才会被调用（`AR-33`）。

    内核不知道它是不是内容库 —— 那正是本缝存在的理由。
    返回类型写 `object` 而不是 `None`：结构性满足即可（现有实现返回写入键）。
    """

    def put(self, artifact: Artifact) -> object: ...
