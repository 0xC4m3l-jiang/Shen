"""`SHEN-M5-01` / `SHEN-M2-02` 的验收：内容由**画像**驱动，而不是硬编码词表。

四条判据（需求文档 §6 的验收要点）：

1. 删除硬编码词表后仍可产出内容（生成器不依赖 `PROFILE_VOCAB`）；
2. 两个服务的生成内容，业务术语重合率（Jaccard）≤ 20%；
3. 同一服务下 `/api/orders` 与 `/api/users` 的字段名重合率 ≤ 30%；
4. 护栏拒绝率不因放宽多样性而下降（风格关用的就是**该画像**的术语）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import re

import pytest

from analysis.aicap import service
from analysis.aicap.guardrail import inspect as guardrail_inspect
from analysis.aicap.profiles import GENERIC_TERMS, load_profile
from analysis.aicap.tasks import content as content_task
from analysis.aicap.tasks.content import _render_body, profile_for


def _words(text: str) -> set[str]:
    """粗分词：英文单词（含下划线/连字符）与 ASCII 片段。"""
    return {w.lower() for w in re.findall(r"[A-Za-z][A-Za-z_\-]{2,}", text)}


def _jaccard(a: set[str], b: set[str]) -> float:
    if not a and not b:
        return 1.0
    return len(a & b) / len(a | b)


def _fields_of(body: str) -> set[str]:
    """取 `<li>字段名: 值</li>` 里的字段名。"""
    return {m.group(1).lower() for m in re.finditer(r"<li>([A-Za-z_][A-Za-z0-9_]*):", body)}


# ── 1. 不依赖硬编码词表 ────────────────────────────────────────────────────────


def test_no_hardcoded_vocab_symbol() -> None:
    """`PROFILE_VOCAB` 必须**不存在** —— 它的存在就是「所有服务共用一套措辞」的证据。"""
    assert not hasattr(content_task, "PROFILE_VOCAB"), "内容生成器仍带着全局硬编码词表"


def test_can_render_without_vocab() -> None:
    """删掉词表后仍能产出内容：术语来自画像，而不是常量。"""
    body = _render_body(resource="/api/orders", profile_id="site-a", variant=0, version=1)
    assert body.strip(), "内容为空"
    terms = load_profile("site-a").domain_terms
    assert any(term in body for term in terms), "正文里应出现该画像的术语"


def test_unknown_profile_falls_back_to_generic() -> None:
    """没有画像文件的服务也要能产出（用通用画像），且**不猜业务语义**。"""
    body = _render_body(
        resource="/api/whatever", profile_id="no-such-profile", variant=0, version=1
    )
    assert any(term in body for term in GENERIC_TERMS)


# ── 2. 跨服务术语不重合 ───────────────────────────────────────────────────────


def test_two_profiles_share_few_terms() -> None:
    """两份画像的术语 Jaccard ≤ 20%：不同服务不该说同一套话。"""
    a = {t.lower() for t in load_profile("site-a").domain_terms}
    b = {t.lower() for t in load_profile("site-b").domain_terms}
    assert _jaccard(a, b) <= 0.2, f"site-a 与 site-b 的术语重合度过高：{_jaccard(a, b):.2f}"


def test_two_profiles_produce_disjoint_content_vocabulary() -> None:
    """实际产出的**业务词**之间，Jaccard ≤ 20%（不只看画像声明）。

    怎么把「模板结构词」排除掉：用**通用画像**渲一份正文，它出现的词就是模板骨架
    （`class` / `code` / `revision` 这类）；从两份产出里各减去这份骨架，剩下的才是业务词。
    这样不必维护一张魔法停用词表 —— 骨架变了，口径跟着变。
    """
    structural = _words(_render_body(resource="/api/x", profile_id="generic", variant=0, version=1))
    a = (
        _words(_render_body(resource="/api/orders", profile_id="site-a", variant=0, version=1))
        - structural
    )
    b = (
        _words(_render_body(resource="/api/documents", profile_id="site-b", variant=0, version=1))
        - structural
    )
    assert a and b, "两份产出都应有业务词（否则测试没测到东西）"
    overlap = _jaccard(a, b)
    assert overlap <= 0.2, (
        f"两份产出正文的业务词重合度过高：{overlap:.2f}（a={sorted(a)} b={sorted(b)}）"
    )


# ── 3. 同服务内不同资源的字段不重合 ──────────────────────────────────────────


def test_same_profile_different_resources_have_different_fields() -> None:
    """`/api/orders` 与 `/api/users` 的字段名 Jaccard ≤ 30%（同一服务、不同资源）。"""
    orders = _fields_of(
        _render_body(resource="/api/orders", profile_id="site-a", variant=0, version=1)
    )
    users = _fields_of(
        _render_body(resource="/api/users", profile_id="site-a", variant=0, version=1)
    )
    assert orders and users
    overlap = _jaccard(orders, users)
    assert overlap <= 0.3, f"同服务跨资源字段重合度过高：{overlap:.2f}"


# ── 4. 护栏跟着画像走（不是放宽，而是对齐）───────────────────────────────────


def test_guardrail_profile_follows_requested_profile() -> None:
    """`profile_for(spec)` 的术语取自该请求的画像 —— 换了画像，判据跟着换。"""
    spec_a = service.TaskSpec(
        kind="content", session_id="s", deadline_s=1.0, payload={"profile_id": "site-a"}
    )
    spec_b = service.TaskSpec(
        kind="content", session_id="s", deadline_s=1.0, payload={"profile_id": "site-b"}
    )
    a, b = profile_for(spec_a), profile_for(spec_b)
    assert a.style_terms == load_profile("site-a").domain_terms
    assert b.style_terms == load_profile("site-b").domain_terms
    assert a.style_terms != b.style_terms
    assert a.name != b.name, "档案名要能区分画像（拒绝原因里能看出是哪份画像判的）"


def test_style_gate_uses_active_profile() -> None:
    """site-b 的正文用 site-a 的术语判 ⇒ 被判「不像」；用 site-b 自己的术语判 ⇒ 通过。

    这条断言的是**判据对齐**，不是放宽：护栏没有变松，只是看的是对的画像。
    """
    body = _render_body(resource="/api/articles", profile_id="site-b", variant=0, version=1)
    candidate = {
        "resource": "/api/articles",
        "variant": 0,
        "body": body,
        "generator": "template-v1",
    }

    spec_b = service.TaskSpec(
        kind="content", session_id="s", deadline_s=1.0, payload={"profile_id": "site-b"}
    )
    _, ok_reasons = guardrail_inspect.check(
        candidate,
        task=content_task.CONTENT_TASK,
        profile=profile_for(spec_b),
        blacklist=guardrail_inspect.blacklist_of(()),
    )
    assert ok_reasons == [], f"按自己的画像判不该被拒：{ok_reasons}"

    spec_a = service.TaskSpec(
        kind="content", session_id="s", deadline_s=1.0, payload={"profile_id": "site-a"}
    )
    _, bad_reasons = guardrail_inspect.check(
        candidate,
        task=content_task.CONTENT_TASK,
        profile=profile_for(spec_a),
        blacklist=guardrail_inspect.blacklist_of(()),
    )
    assert [r["check"] for r in bad_reasons] == ["style"], "用别的画像术语判，应当判为不像"


@pytest.mark.parametrize("profile_id", ["site-a", "site-b"])
def test_generate_accepts_its_own_profile(profile_id: str) -> None:
    """端到端：按画像生成的内容必须过得了**同一画像**的护栏。"""
    envelope = service.generate(
        service.TaskSpec(
            kind="content",
            session_id="s",
            deadline_s=5.0,
            payload={
                "resource": "/api/orders",
                "variant": 0,
                "version": 1,
                "profile_id": profile_id,
            },
        ),
        generated_at="2026-10-14T00:00:00+00:00",
    )
    assert envelope.accepted, envelope.rejected_reason
