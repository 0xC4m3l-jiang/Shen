"""`ai-capability` 的内容与清单测试（`AR-30` 的确定性 · 幂等 · 清单形状 · CLI 关卡）。

它要证明的是**内容可复现、清单可信**：同输入逐字节同输出（否则热路径的字节一致无从谈起）、
`content_id` / `checksum` 由内容算出、没过护栏的内容**进不了内容库也进不了清单**。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import json
from pathlib import Path

from analysis.aicap import service
from analysis.aicap.content import (
    DEFAULT_MARKER,
    MAX_BODY_BYTES,
    ContentStore,
    build_manifest,
    checksum_of,
    content_id_of,
    content_key,
    make_content,
    manifest_bytes,
)
from analysis.aicap.tasks.content import (
    GENERATOR,
    PROFILE_VOCAB,
    _render_body,
    produce,
)

STAMP = "2026-09-20T00:00:00+00:00"


# ── 内容对象：确定性（AR-30 的前提）──────────────────────────────────────────


def test_checksum_and_id_are_deterministic() -> None:
    body = "<html>body</html>"
    assert checksum_of(body) == checksum_of(body)
    args = {
        "kind": "content",
        "resource": "/",
        "profile_id": "site-a",
        "variant": 1,
        "version": 2,
        "body": body,
    }
    assert content_id_of(**args) == content_id_of(**args)
    assert content_id_of(**{**args, "variant": 2}) != content_id_of(**args)


def test_make_content_fields_and_key() -> None:
    item = make_content(
        resource="/api/users",
        profile_id="site-a",
        variant=3,
        body="<html>x</html>",
        version=1,
        generated_at=STAMP,
        generator=GENERATOR,
    )
    assert item.marker == DEFAULT_MARKER
    assert item.checksum == checksum_of(item.body)
    assert item.key == content_key(profile_id="site-a", resource="/api/users", variant=3, version=1)
    wire = item.to_wire()
    assert set(wire) == {
        "content_id",
        "kind",
        "resource",
        "profile_id",
        "variant",
        "body",
        "marker",
        "checksum",
        "version",
        "generated_at",
        "generator",
    }


def test_make_content_rejects_empty_body() -> None:
    try:
        make_content(
            resource="/",
            profile_id="site-a",
            variant=0,
            body="",
            version=1,
            generated_at=STAMP,
            generator=GENERATOR,
        )
    except ValueError as exc:
        assert "内容体不得为空" in str(exc)
    else:  # pragma: no cover - 只有在实现坏掉时才走到
        raise AssertionError("空内容体竟然被接受")


# ── 模板生成器：同输入同输出 + 变体互不相同 ──────────────────────────────────


def test_template_generator_is_reproducible() -> None:
    first = _render_body(resource="/", profile_id="site-a", variant=2, version=1)
    second = _render_body(resource="/", profile_id="site-a", variant=2, version=1)
    assert first == second, "模板生成必须确定性（AR-30）"


def test_template_generator_changes_with_variant_and_version() -> None:
    bodies = {
        v: _render_body(resource="/", profile_id="site-a", variant=v, version=1) for v in range(8)
    }
    assert len(set(bodies.values())) == 8, "N 个变体必须互不相同（ADR-0023 的多态）"
    assert _render_body(resource="/", profile_id="site-a", variant=0, version=2) != bodies[0], (
        "版本递增必须换内容（否则轮换是空操作）"
    )


def test_produce_reads_payload() -> None:
    spec = service.TaskSpec(
        kind="content",
        session_id="s",
        deadline_s=1.0,
        payload={"resource": "/x", "variant": 1, "version": 1, "profile_id": "site-b"},
    )
    candidate = produce("prompt", spec, None)  # type: ignore[arg-type] - 模板生成器不用 client
    assert candidate["resource"] == "/x"
    assert PROFILE_VOCAB[1 % len(PROFILE_VOCAB)] in str(candidate["body"])


# ── 清单：形状、上限、确定性 ─────────────────────────────────────────────────


def _item(resource: str, variant: int, body: str = "", version: int = 1):
    return make_content(
        resource=resource,
        profile_id="site-a",
        variant=variant,
        body=body or f"<html>{PROFILE_VOCAB[variant % len(PROFILE_VOCAB)]}</html>",
        version=version,
        generated_at=STAMP,
        generator=GENERATOR,
    )


def test_manifest_groups_by_resource() -> None:
    manifest = build_manifest(
        [_item("/a", 0), _item("/a", 1), _item("/b", 0)],
        version=1,
        variants=2,
        generated_at=STAMP,
        generator=GENERATOR,
    )
    assert [entry["resource"] for entry in manifest["entries"]] == ["/a", "/b"]
    bodies = manifest["entries"][0]["bodies"]
    assert [body["variant_id"] for body in bodies] == [0, 1]
    assert bodies[0]["checksum"] == checksum_of(_item("/a", 0).body)
    assert manifest["selector"] == "session"
    assert manifest["skipped"] == []


def test_manifest_skips_overlong_and_out_of_range() -> None:
    manifest = build_manifest(
        [_item("/a", 0, body="x" * (MAX_BODY_BYTES + 1)), _item("/a", 9)],
        version=1,
        variants=2,
        generated_at=STAMP,
        generator=GENERATOR,
    )
    assert manifest["entries"] == []
    whys = " ".join(item["why"] for item in manifest["skipped"])
    assert "超过上限" in whys
    assert "之外" in whys


def test_manifest_bytes_deterministic() -> None:
    items = [_item("/a", 0), _item("/a", 1)]
    first = manifest_bytes(
        build_manifest(items, version=1, variants=2, generated_at=STAMP, generator=GENERATOR)
    )
    second = manifest_bytes(
        build_manifest(
            list(reversed(items)), version=1, variants=2, generated_at=STAMP, generator=GENERATOR
        )
    )
    assert first == second, "清单必须与输入顺序无关（否则策略载荷每轮都变）"


# ── service.generate：过护栏才入库 ──────────────────────────────────────────


def test_generate_puts_content_in_store() -> None:
    store = ContentStore()
    envelope = service.generate(
        service.TaskSpec(
            kind="content",
            session_id="s",
            deadline_s=5.0,
            payload={"resource": "/", "variant": 0, "version": 1, "profile_id": "site-a"},
        ),
        store=store,
        generated_at=STAMP,
    )
    assert envelope.accepted
    assert len(store) == 1
    assert envelope.data["content_id"] == store.entries()[0].content_id


def test_generate_rejects_and_keeps_store_empty() -> None:
    """护栏拒绝 ⇒ **不入库**（`AR-33`），拒绝原因可查。"""
    store = ContentStore()
    envelope = service.generate(
        service.TaskSpec(
            kind="content",
            session_id="s",
            deadline_s=5.0,
            payload={"resource": "/", "variant": 0, "version": 1, "profile_id": "site-a"},
        ),
        store=store,
        identifiers=("Service status",),  # 命中真实业务标识 → 泄露类（AR-22）
        generated_at=STAMP,
    )
    assert not envelope.accepted
    assert len(store) == 0
    assert "护栏拒绝" in str(envelope.rejected_reason)


# ── CLI：清单文件 + 关卡 ────────────────────────────────────────────────────


def test_cli_writes_deterministic_manifest(tmp_path: Path) -> None:
    from analysis.aicap.__main__ import main

    out = tmp_path / "manifest.json"
    argv = [
        "--out",
        str(out),
        "--resources",
        "/,/api/users",
        "--variants",
        "4",
        "--version",
        "2",
        "--now",
        STAMP,
        "--quiet",
    ]
    assert main(argv) == 0
    first = out.read_bytes()
    assert main(argv) == 0
    assert out.read_bytes() == first, "同一份输入两次生成必须逐字节相同"

    manifest = json.loads(first)
    assert manifest["version"] == 2
    assert manifest["variants"] == 4
    assert manifest["manifest_version"] == 1
    assert {entry["resource"] for entry in manifest["entries"]} == {"/", "/api/users"}
    for entry in manifest["entries"]:
        for body in entry["bodies"]:
            assert body["checksum"] == checksum_of(body["body"])
            assert 0 <= body["variant_id"] < 4


def test_cli_refuses_to_write_when_everything_is_rejected(tmp_path: Path) -> None:
    """一条都没过护栏 ⇒ 不写清单 + 退出码 1（关卡在 CLI 上可见）。"""
    from analysis.aicap.__main__ import main

    out = tmp_path / "manifest.json"
    code = main(
        [
            "--out",
            str(out),
            "--resources",
            "/",
            "--variants",
            "2",
            "--identifiers",
            PROFILE_VOCAB[0][:8],  # 模板里必然出现的片段 → 全部被护栏拒（AR-22）
            "--quiet",
        ]
    )
    assert code == 1
    assert not out.exists()


def test_cli_rejects_unregistered_kind(tmp_path: Path) -> None:
    from analysis.aicap.__main__ import main

    code = main(["--out", str(tmp_path / "m.json"), "--kind", "没有这个种类"])
    assert code == 2
