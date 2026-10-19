"""差异化门禁（`SHEN-M5-03`）：把「内容要贴合业务、彼此独立」变成**可测量的三条硬指标**。

为什么要有它：`SHEN-M2-02` / `SHEN-M5-01` 修掉的是「所有服务共用一套措辞」，
但只要没有 CI 判据，下一次有人图省事把词表塞回代码、或者加一个和 `site-a` 共用术语的画像，
就没人会发现 —— 门禁的作用是**挡住增量**。

三条指标（全部可自动计算，口径固定在 `compute()` 里）：

| 指标 | 定义 | 阈值 |
| --- | --- | --- |
| 跨服务术语重合率 | 两份画像 `domain_terms` 的 Jaccard | ≤ 0.20 |
| 跨服务内容重合率 | 两份**实际产出**的业务词 Jaccard（先减去通用画像的模板骨架） | ≤ 0.20 |
| 服务内跨资源字段重合率 | 同一画像下两个资源的字段名 Jaccard | ≤ 0.30 |
| 模板痕迹率 | 两份产出里**逐行完全相同**的比例（结构性闭合标签无法避免） | ≤ 0.20 |

> 模板痕迹率为什么不是需求文档里的 10%：那条是给「整页模板」写的；这里的产物是
> **可注入的 HTML 片段**，`</ul>` / `</section>` 这类闭合标签天然相同且不含业务信息。
> 实测基线见变更包；阈值取「基线 + 余量」，用途是**防回退**，不是追求 0。

第四类「盲测可归属率 ≥ 80%」需要人工评审，**不在门禁里**（§未做）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import argparse
import json
import re
import sys
from collections.abc import Iterable, Sequence
from dataclasses import dataclass

from .profiles import PROFILE_DIR, load_profile
from .tasks.content import _render_body

# 默认样本：每个画像挑两个语义不同的资源，覆盖「跨服务」与「服务内跨资源」两类比较。
DEFAULT_SAMPLES: tuple[tuple[str, str], ...] = (
    ("site-a", "/api/orders"),
    ("site-a", "/api/users"),
    ("site-b", "/api/articles"),
    ("site-b", "/api/documents"),
)

THRESHOLDS: dict[str, float] = {
    "cross_service_term_jaccard": 0.20,
    "cross_service_content_jaccard": 0.20,
    "intra_service_field_jaccard": 0.30,
    "template_trace_rate": 0.20,
}

# 画像卫生（不是差异化指标，但缺了会让上面的指标失去意义）
MIN_TERMS = 8
MIN_ENTITIES = 2


@dataclass(frozen=True)
class Sample:
    profile_id: str
    resource: str
    body: str


@dataclass(frozen=True)
class Report:
    samples: Sequence[Sample]
    metrics: dict[str, float]
    hygiene: dict[str, list[str]]

    def violations(self) -> list[str]:
        """返回越界项的人话说明（空列表 = 通过）。"""
        out = [
            f"{name}={value:.2f} 超过阈值 {THRESHOLDS[name]:.2f}"
            for name, value in self.metrics.items()
            if value > THRESHOLDS[name]
        ]
        for profile_id, problems in self.hygiene.items():
            out.extend(f"画像 {profile_id}：{p}" for p in problems)
        return out


def _words(text: str) -> set[str]:
    return {w.lower() for w in re.findall(r"[A-Za-z][A-Za-z_\-]{2,}", text)}


def _fields(body: str) -> set[str]:
    return {m.group(1).lower() for m in re.finditer(r"<li>([A-Za-z_][A-Za-z0-9_]*):", body)}


def _jaccard(a: set[str], b: set[str]) -> float:
    if not a and not b:
        return 0.0
    return len(a & b) / len(a | b)


def _max_pairwise(groups: dict[str, set[str]]) -> float:
    keys = sorted(groups)
    worst = 0.0
    for i, left in enumerate(keys):
        for right in keys[i + 1 :]:
            worst = max(worst, _jaccard(groups[left], groups[right]))
    return worst


def _skeleton_words() -> set[str]:
    """模板骨架词：用**通用画像**渲一份，它出现的词就是骨架（不维护魔法停用表）。"""
    return _words(_render_body(resource="/api/x", profile_id="generic", variant=0, version=1))


def build_samples(pairs: Iterable[tuple[str, str]]) -> tuple[Sample, ...]:
    return tuple(
        Sample(
            profile_id=pid,
            resource=res,
            body=_render_body(resource=res, profile_id=pid, variant=0, version=1),
        )
        for pid, res in pairs
    )


def compute(samples: Sequence[Sample]) -> Report:
    skeleton = _skeleton_words()

    # ① 跨服务：画像声明的术语
    terms_by_profile: dict[str, set[str]] = {}
    for s in samples:
        terms = load_profile(s.profile_id).domain_terms
        terms_by_profile[s.profile_id] = {t.lower() for t in terms}

    # ② 跨服务：实际产出的业务词（减去骨架）
    content_by_profile: dict[str, set[str]] = {}
    for s in samples:
        content_by_profile.setdefault(s.profile_id, set())
        content_by_profile[s.profile_id] |= _words(s.body) - skeleton

    # ③ 服务内：跨资源字段
    fields_by_group: dict[str, set[str]] = {}
    for s in samples:
        fields_by_group.setdefault(s.profile_id, set())
        fields_by_group[s.profile_id] |= _fields(s.body)

    # ④ 模板痕迹：逐行完全相同的比例（取最坏的一对）
    trace = 0.0
    bodies = [(s.profile_id, {ln for ln in s.body.split("\n") if ln.strip()}) for s in samples]
    distinct_profiles = sorted({pid for pid, _ in bodies})
    for i, left in enumerate(distinct_profiles):
        for right in distinct_profiles[i + 1 :]:
            la = next(lines for pid, lines in bodies if pid == left)
            lb = next(lines for pid, lines in bodies if pid == right)
            trace = max(trace, len(la & lb) / max(len(la), len(lb)))

    # ⑤ 画像卫生
    hygiene: dict[str, list[str]] = {}
    for pid in sorted(terms_by_profile):
        profile = load_profile(pid)
        problems: list[str] = []
        if len(profile.domain_terms) < MIN_TERMS:
            problems.append(f"术语只有 {len(profile.domain_terms)} 条（至少 {MIN_TERMS}）")
        if len(profile.entity_fields) < MIN_ENTITIES:
            problems.append(f"实体只有 {len(profile.entity_fields)} 个（至少 {MIN_ENTITIES}）")
        if problems:
            hygiene[pid] = problems

    metrics = {
        "cross_service_term_jaccard": _max_pairwise(terms_by_profile),
        "cross_service_content_jaccard": _max_pairwise(content_by_profile),
        "intra_service_field_jaccard": _max_pairwise(fields_by_group),
        "template_trace_rate": trace,
    }
    return Report(samples=samples, metrics=metrics, hygiene=hygiene)


def _discover_pairs() -> tuple[tuple[str, str], ...]:
    """默认样本里只保留**真实存在**的画像文件；没有画像就退回内置样本，不静默空跑。"""
    available = {p.stem for p in PROFILE_DIR.glob("*.json")}
    pairs = tuple((pid, res) for pid, res in DEFAULT_SAMPLES if pid in available)
    return pairs or DEFAULT_SAMPLES


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="差异化门禁（SHEN-M5-03）")
    parser.add_argument("--json", action="store_true", help="机器可读输出")
    parser.add_argument("--samples", help="自定义样本：profile:resource,profile:resource")
    args = parser.parse_args(argv)

    if args.samples:
        pairs = []
        for item in args.samples.split(","):
            pid, _, res = item.partition(":")
            if not pid or not res:
                print(f"样本格式错误：{item!r}（应为 profile:resource）", file=sys.stderr)
                return 2
            pairs.append((pid.strip(), res.strip()))
    else:
        pairs = _discover_pairs()

    report = compute(build_samples(pairs))
    problems = report.violations()

    if args.json:
        print(
            json.dumps(
                {
                    "metrics": report.metrics,
                    "thresholds": THRESHOLDS,
                    "hygiene": report.hygiene,
                    "violations": problems,
                },
                ensure_ascii=False,
                indent=2,
            )
        )
    else:
        print("差异化门禁（SHEN-M5-03）")
        for name, value in report.metrics.items():
            limit = THRESHOLDS[name]
            mark = "✓" if value <= limit else "✗"
            print(f"  {mark} {name:32s} {value:5.2f}  (阈值 ≤ {limit:.2f})")
        if problems:
            print("\n越界项：")
            for item in problems:
                print(f"  · {item}")

    if problems:
        print("\n差异化门禁未通过：先把画像/生成器改对，不要调阈值。", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":  # pragma: no cover
    raise SystemExit(main())
