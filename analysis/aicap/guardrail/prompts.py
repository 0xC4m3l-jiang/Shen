"""前置护栏：三段式提示词（**资源文件**，随版本分发）。

`AR-31`：攻击者可控内容**必须**以结构化数据进入**数据区**，**禁止**进入指令区；提示词**必须**显式标注其为不可信数据。
`AR-24`：提示词**必须**是仓库内资源文件，启动期**必须**校验存在与占位符齐全。

三段（顺序固定）：

```text
# 任务        ← 指令区：任务 + 允许/禁止清单 + 输出契约（不含任何攻击者可控内容）
# 画像        ← 去敏的站点术语与模板（术语来自护栏档案的 style_terms）
# 不可信数据   ← 数据区：{{untrusted_data}}（由 llm.untrusted.as_data_block 产出）
```
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import TYPE_CHECKING

from ...llm import prompts as llm_prompts
from ...llm.untrusted import as_data_block, assert_structured

if TYPE_CHECKING:  # pragma: no cover - 只用于类型标注，避免运行期循环导入
    from ..tasks._registry import GuardrailProfile

DEFAULT_DIR = Path(__file__).resolve().parent.parent / "resources" / "prompts"
"""提示词目录：`analysis/aicap/resources/prompts/`（随版本分发，`AR-24`）。"""

REQUIRED_PROMPTS: tuple[str, ...] = ("content", "intent", "chain", "strategy")
"""本模块**必须**存在的提示词模板名（缺任一个 ⇒ 启动期断言失败）。

它们与任务登记表一一对应：每个 `kind` 的护栏档案都指向这里的一个模板（`AR-24`）。
新增 `kind` 时必须同步加进来 —— 否则那个任务能登记、却没有任何人能启动它。
"""

SECTION_TASK = "# 任务"
SECTION_PROFILE = "# 画像"
SECTION_DATA = "# 不可信数据"

REQUIRED_SECTIONS: tuple[str, ...] = (SECTION_TASK, SECTION_PROFILE, SECTION_DATA)
"""三段式的段落标题；缺失或顺序不对**必须**让启动失败（`AR-31` / `AR-24`）。"""

DATA_PLACEHOLDER = "{{untrusted_data}}"


class PromptError(RuntimeError):
    """提示词资源不合规 —— 启动期**必须**让进程起不来（`AR-24`）。"""


class _Template:
    def __init__(self, name: str, text: str) -> None:
        self.name = name
        self.text = text


def _check_sections(name: str, text: str) -> None:
    """校验三段式：三个段落标题**都必须**出现、**必须**按顺序、数据区占位符**必须**存在。"""
    positions = []
    for section in REQUIRED_SECTIONS:
        index = text.find(section)
        if index < 0:
            raise PromptError(f"{name}: 缺段落 {section!r}（三段式，AR-31 / AR-24）")
        positions.append(index)
    if positions != sorted(positions):
        raise PromptError(f"{name}: 三段顺序必须是 {list(REQUIRED_SECTIONS)}（AR-31）")
    if DATA_PLACEHOLDER not in text:
        raise PromptError(f"{name}: 数据区缺 {DATA_PLACEHOLDER} 占位符（AR-31）")


def load(directory: Path = DEFAULT_DIR) -> dict[str, _Template]:
    """加载并校验目录下的模板；**任一不合格即抛异常**（不跳过、不降级）。"""
    if not directory.is_dir():
        raise PromptError(f"提示词目录不存在：{directory}（AR-24）")
    out: dict[str, _Template] = {}
    for path in sorted(directory.glob("*.md")):
        text = path.read_text(encoding="utf-8")
        _check_sections(path.stem, text)
        out[path.stem] = _Template(path.stem, text)
    if not out:
        raise PromptError(f"提示词目录为空：{directory}（AR-24）")
    return out


def assert_startup(directory: Path = DEFAULT_DIR) -> tuple[str, ...]:
    """启动期断言（`AR-24`）：必需模板齐全、三段式合格；返回已加载的模板名。"""
    templates = load(directory)
    missing = [name for name in REQUIRED_PROMPTS if name not in templates]
    if missing:
        raise PromptError(f"缺必需提示词模板：{missing}（AR-24）")
    return tuple(sorted(templates))


def render(
    profile: GuardrailProfile,
    *,
    untrusted_rows: Sequence[Mapping[str, object]],
    directory: Path = DEFAULT_DIR,
) -> str:
    """渲染三段式提示词。

    不可信数据**只能**经 `llm.untrusted.as_data_block` 进入数据区（`AR-31`）；
    渲染后再用 `llm.untrusted.assert_structured` 复核数据区标记与声明。
    """
    templates = load(directory)
    template = templates.get(profile.prompt)
    if template is None:
        raise PromptError(
            f"护栏档案 {profile.name} 引用了未加载的提示词 {profile.prompt!r}（AR-24）"
        )

    text = template.text
    text = text.replace("{{profile_id}}", profile.name)
    text = text.replace("{{style_terms}}", " / ".join(profile.style_terms))
    text = text.replace(DATA_PLACEHOLDER, as_data_block(untrusted_rows))
    if llm_prompts.PLACEHOLDER.search(text):
        raise PromptError(f"{template.name}: 渲染后仍有未替换占位符（AR-24）")
    assert_structured(text)
    return text
