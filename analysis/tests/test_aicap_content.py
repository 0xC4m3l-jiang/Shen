"""`ai-capability` 的内容与清单测试（产物层确定性 · 幂等 · 清单形状 · CLI 关卡）。

它要证明的是**内容可复现、清单可信**：同一份输入与内容必得同一个 `content_id` / `checksum`、
同一份内容两次建对象必得相同 `to_wire()`、没过护栏的内容**进不了内容库也进不了清单**。
注：这是**产物层**的确定性；生成期的「同输入同字节」是阶段 A 的工程性质（`AR-30` 只管响应路径）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import json
from pathlib import Path

import pytest

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
    CONTENT_TASK,
    GENERATOR,
    MODEL_GENERATOR,
    PROFILE_VOCAB,
    _render_body,
    produce,
)
from analysis.llm.client import Unavailable

STAMP = "2026-09-20T00:00:00+00:00"


# ── 内容对象：产物层的确定性（`content_id` / `checksum` 由内容算出）─────────────


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
    assert first == second, "模板生成在阶段 A 必须可复现（工程性质；AR-30 只管响应路径）"


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
        sink=store,
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
        sink=store,
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


# ── 模型路径（阶段 B）：模型优先、失败回落，且**逐条标明实际生成器** ──────────────


class _StubClient:
    """满足 `AnalysisClient` 形状的替身：公开成员只有 `complete`（`AR-32`）。"""

    def __init__(self, *, reply: str = "", fail: Exception | None = None) -> None:
        self.reply = reply
        self.fail = fail

    def complete(self, prompt: str, *, session_id: str, timeout: float) -> str:
        del prompt, session_id, timeout
        if self.fail is not None:
            raise self.fail
        return self.reply


def _spec(*, use_model: bool, resource: str = "/api/users", variant: int = 3):
    return service.TaskSpec(
        kind="content",
        session_id="s",
        deadline_s=1.0,
        payload={
            "resource": resource,
            "variant": variant,
            "version": 1,
            "profile_id": "site-a",
            "use_model": use_model,
        },
    )


def test_produce_defaults_to_template_even_when_a_client_is_present() -> None:
    """**不靠环境变量隐式决定**：`use_model` 不为真就该走模板，哪怕给了一个能用的客户端。"""
    candidate = produce("p", _spec(use_model=False), _StubClient(reply='{"body": "<p>x</p>"}'))
    assert candidate["generator"] == GENERATOR
    assert "<section" in str(candidate["body"])


def test_produce_model_path_labels_model_and_takes_identity_from_spec() -> None:
    """模型只负责正文；`resource` / `variant` 由生成器从输入取（模型笔误不得改清单归属）。"""
    reply = json.dumps(
        {"resource": "/WRONG", "variant": 99, "body": "<section>Service status</section>"}
    )
    candidate = produce(
        "p", _spec(use_model=True, resource="/api/users", variant=3), _StubClient(reply=reply)
    )
    assert candidate["generator"] == MODEL_GENERATOR
    assert candidate["resource"] == "/api/users", "身份字段必须来自 spec"
    assert candidate["variant"] == 3, "身份字段必须来自 spec"
    assert candidate["body"] == "<section>Service status</section>", "正文必须是模型写的"


def test_produce_falls_back_to_template_when_model_is_unavailable() -> None:
    """模型不可用 ⇒ 回落模板，且**如实标注** `template-v1`（不冒充模型输出，`AR-15`）。"""
    candidate = produce("p", _spec(use_model=True), _StubClient(fail=Unavailable("没配 key")))
    assert candidate["generator"] == GENERATOR
    assert "<section" in str(candidate["body"])


def test_produce_falls_back_when_extraction_fails() -> None:
    """模型答了但不是 JSON ⇒ 抽取失败，同样回落（与「结构不合契约」不是一回事）。"""
    candidate = produce("p", _spec(use_model=True), _StubClient(reply="抱歉，我不能这样做。"))
    assert candidate["generator"] == GENERATOR


def test_schema_rejects_unknown_generator() -> None:
    """生成器是**闭集**：写别的值会被契约层拒绝（`Field.allowed`）。"""
    from analysis.llm.contract import ContractError, validate

    with pytest.raises(ContractError):
        validate(
            {"resource": "/", "variant": 0, "body": "<p>x</p>", "generator": "evil-v1"},
            CONTENT_TASK.schema,
        )


def test_service_end_to_end_model_path_is_accepted_and_labelled() -> None:
    """走完整出口（两道检查）的模型路径：接受、且产物里写明 `model-v1`。"""
    store = ContentStore()
    envelope = service.generate(
        _spec(use_model=True),
        client=_StubClient(reply=json.dumps({"body": "<section>Service status</section>"})),
        sink=store,
    )
    assert envelope.accepted, envelope.rejected_reason
    assert envelope.data["generator"] == MODEL_GENERATOR
    assert [item.generator for item in store.entries()] == [MODEL_GENERATOR]


def test_service_end_to_end_template_path_still_labelled_template() -> None:
    """回归：默认路径（`use_model` 不为真）仍标 `template-v1`，且形状与阶段 A 一致。"""
    store = ContentStore()
    envelope = service.generate(_spec(use_model=False), sink=store)
    assert envelope.accepted, envelope.rejected_reason
    assert envelope.data["generator"] == GENERATOR


def test_cli_llm_without_key_exits_2(tmp_path: Path, monkeypatch) -> None:
    """`--llm` 但没配 key ⇒ 在**开始生成之前**退出（避免生成一半模板、一半模型）。"""
    from analysis.aicap.__main__ import main

    monkeypatch.delenv("SHEN_AI_KEY", raising=False)
    code = main(["--out", str(tmp_path / "m.json"), "--llm", "--quiet"])
    assert code == 2
    assert not (tmp_path / "m.json").exists()


def test_cli_labels_mixed_when_some_items_fall_back(tmp_path: Path, monkeypatch) -> None:
    """逐条回落时必须如实标 `mixed:…` —— 「不静默」的关键一步，得有人钉住。"""
    import json as _json

    from analysis.aicap.__main__ import main

    calls = {"n": 0}

    class _FlakyOnce:
        """第一条给模型正文，之后抛 `Unavailable`（模拟中途限流/超时）。"""

        def complete(self, prompt: str, *, session_id: str, timeout: float) -> str:
            del prompt, session_id, timeout
            calls["n"] += 1
            if calls["n"] == 1:
                return _json.dumps({"body": "<section>Service status</section>"})
            raise Unavailable("模型中途不可用")

    monkeypatch.setattr("analysis.aicap.__main__.from_environment", lambda: _FlakyOnce())
    out = tmp_path / "m.json"
    code = main(["--out", str(out), "--llm", "--quiet", "--resources", "/", "--variants", "3"])
    assert code == 0, "部分回落不该让整轮失败（清单仍值得写）"
    manifest = _json.loads(out.read_text(encoding="utf-8"))
    assert manifest["generator"] == "mixed:model-v1,template-v1", manifest["generator"]
