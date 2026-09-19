"""话术与提示词资源化（`AR-24`）。

提示词**必须**作为仓库内资源文件、随版本分发；**禁止**存放于运行期可变存储。
启动时**必须**校验模板存在与占位符齐全。
"""

from __future__ import annotations

import re
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path

PLACEHOLDER = re.compile(r"\{\{(\w+)\}\}")

DEFAULT_DIR = Path(__file__).with_name("resources") / "prompts"
"""提示词目录；随版本分发（`AR-24`）。"""


class PromptResourceError(RuntimeError):
    """资源缺失或占位符不齐 —— 启动期**必须**让进程起不来（`AR-24`）。"""


@dataclass(frozen=True)
class PromptTemplate:
    name: str
    path: Path
    text: str
    placeholders: frozenset[str]


class PromptStore:
    """已加载并校验过的提示词集合。"""

    def __init__(self, templates: Mapping[str, PromptTemplate]) -> None:
        self._templates = dict(templates)

    @property
    def names(self) -> tuple[str, ...]:
        return tuple(sorted(self._templates))

    def render(self, name: str, **values: str) -> str:
        """渲染模板；缺值或多值都**抛异常**（禁止静默降级，`AR-15` / `AR-24`）。"""
        template = self._templates.get(name)
        if template is None:
            raise PromptResourceError(f"未加载的提示词 {name!r}；已有 {self.names}")
        missing = template.placeholders - set(values)
        extra = set(values) - template.placeholders
        if missing or extra:
            raise PromptResourceError(f"{name}: 缺 {sorted(missing)}，多 {sorted(extra)}（AR-24）")
        text = template.text
        for key, value in values.items():
            text = text.replace("{{" + key + "}}", value)
        if PLACEHOLDER.search(text):
            raise PromptResourceError(f"{name}: 渲染后仍有未替换占位符（AR-24）")
        return text


def load(directory: Path = DEFAULT_DIR, *, required: Sequence[str] = ()) -> PromptStore:
    """加载目录下全部 `.md` 模板；`required` 里的模板缺一个就抛异常。"""
    if not directory.is_dir():
        raise PromptResourceError(f"提示词目录不存在：{directory}（AR-24）")
    templates: dict[str, PromptTemplate] = {}
    for path in sorted(directory.glob("*.md")):
        text = path.read_text(encoding="utf-8")
        templates[path.stem] = PromptTemplate(
            name=path.stem,
            path=path,
            text=text,
            placeholders=frozenset(PLACEHOLDER.findall(text)),
        )
    missing = [name for name in required if name not in templates]
    if missing:
        raise PromptResourceError(f"缺必需提示词模板：{missing}（AR-24）")
    return PromptStore(templates)


def assert_startup(directory: Path = DEFAULT_DIR, *, required: Sequence[str] = ()) -> PromptStore:
    """启动期断言（`AR-24`）：不满足即让启动失败，而不是运行到一半才发现。"""
    store = load(directory, required=required)
    if not store.names:
        raise PromptResourceError(f"提示词目录为空：{directory}（AR-24）")
    return store
