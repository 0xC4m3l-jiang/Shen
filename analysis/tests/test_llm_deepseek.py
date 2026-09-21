"""DeepSeek 适配器与模型接缝：`AR-32`（无执行面）· `AR-15`（失败即显式）· `ST-20`（密钥不入日志）。

**全部不打网络**：传输经构造参数注入替身（生产路径永远是模块里的 `_https_send`）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import logging

import pytest

from analysis.aicap import model as model_seam
from analysis.llm.client import UnconfiguredClient, assert_no_execution_surface
from analysis.llm.deepseek import DeepSeekClient, Unavailable

KEY = "sk-不应出现在任何异常或日志里"


def _client(sender: object) -> DeepSeekClient:
    return DeepSeekClient(key=KEY, model="deepseek-flash", sender=sender)  # type: ignore[arg-type]


def _reply(status: int, body: bytes) -> object:
    """造一个只回固定应答的发送替身。"""

    def send(base: str, path: str, payload: bytes, key: str, timeout: float) -> tuple[int, bytes]:
        del base, payload, key, timeout
        assert path == "/chat/completions", "协议路径由适配器决定，不由调用方传"
        return status, body

    return send


_OK = b'{"choices":[{"message":{"content":"{\\"ok\\":true}"}}]}'


def test_happy_path_returns_content() -> None:
    content = _client(_reply(200, _OK)).complete("提示词", session_id="s", timeout=1.0)
    assert content == '{"ok":true}'


def test_non_2xx_is_unavailable() -> None:
    with pytest.raises(Unavailable) as excinfo:
        _client(_reply(429, b"rate limited")).complete("提示词", session_id="s", timeout=1.0)
    assert "429" in str(excinfo.value)


def test_unparseable_body_is_unavailable() -> None:
    with pytest.raises(Unavailable):
        _client(_reply(200, b"<html>not json</html>")).complete("p", session_id="s", timeout=1.0)


def test_malformed_shape_is_unavailable() -> None:
    for body in (b"{}", b'{"choices":[]}', b'{"choices":[{"message":{}}]}'):
        with pytest.raises(Unavailable):
            _client(_reply(200, body)).complete("p", session_id="s", timeout=1.0)


def test_empty_content_is_unavailable() -> None:
    """空 content 也是「没产出」—— 放行空结论等于让下游拿空数据当结论（AR-15）。"""
    with pytest.raises(Unavailable):
        _client(_reply(200, b'{"choices":[{"message":{"content":""}}]}')).complete(
            "p", session_id="s", timeout=1.0
        )


def test_transport_error_is_normalized_to_unavailable() -> None:
    """传输层的任何异常都归一成 `Unavailable`（否则每个调用点都要重写一遍失败语义）。"""

    def boom(base: str, path: str, payload: bytes, key: str, timeout: float) -> tuple[int, bytes]:
        del base, path, payload, key, timeout
        raise TimeoutError("读超时")

    with pytest.raises(Unavailable) as excinfo:
        _client(boom).complete("p", session_id="s", timeout=1.0)
    assert "TimeoutError" in str(excinfo.value)


def test_key_never_leaks_into_errors_or_logs(caplog: pytest.LogCaptureFixture) -> None:
    """`ST-20` / `ST-21`：密钥只经环境变量，**禁止**出现在异常、日志与 repr 里。

    这是准入类检查：它失败意味着密钥会随日志/上报被带出去，属于必须当场修的那一类。
    """

    def boom(base: str, path: str, payload: bytes, key: str, timeout: float) -> tuple[int, bytes]:
        del base, path, payload, key, timeout
        raise OSError("连接被拒")

    client = _client(boom)
    with caplog.at_level(logging.DEBUG):
        for attempt in (
            lambda: client.complete("p", session_id="s", timeout=1.0),
            lambda: client.complete("", session_id="s", timeout=1.0),
        ):
            with pytest.raises(Unavailable):
                attempt()
    assert KEY not in caplog.text, "密钥不得进日志"
    assert KEY not in repr(client), "密钥不得进 repr（会被随手打进日志）"
    with pytest.raises(Unavailable) as excinfo:
        _client(_reply(200, b"not json at all")).complete("p", session_id="s", timeout=1.0)
    assert KEY not in str(excinfo.value), "密钥不得进异常信息"


def test_ar32_adapter_has_no_execution_surface() -> None:
    """公开成员只有 `complete`（`AR-32` / `SB-1` / `SB-2`）。

    两道检查，缺一不可：

    1. `assert_no_execution_surface` 查**子串黑名单**（`execute` / `request` / `http` …）；
    2. 本用例另查**白名单**（公开成员集合 == `{complete}`）—— 因为黑名单拦不住
       一个叫 `fetch()` / `get()` 的新公开方法（独立评审 P2 指出）。
    """
    client = _client(_reply(200, _OK))
    assert_no_execution_surface(client)
    public = {name for name in dir(client) if not name.startswith("_")}
    assert public == {"complete"}, f"适配器不得新增公开成员：{sorted(public - {'complete'})}"


def test_config_errors_fail_loudly() -> None:
    """端点 / 密钥 / 超时写错一律**显式失败**，不悄悄换默认值。"""
    for kwargs in (
        {"key": "", "model": "m"},
        {"key": "k", "model": ""},
        {"key": "k", "model": "m", "base": "http://api.deepseek.com"},
        {"key": "k", "model": "m", "base": "https://api.deepseek.com/v1"},
        {"key": "k", "model": "m", "timeout": 0},
        {"key": "k", "model": "m", "timeout": "很久"},
    ):
        with pytest.raises(Unavailable):
            DeepSeekClient(**kwargs)  # type: ignore[arg-type]


# ── 模型接缝：`resolve()` 的三个分支（AR-15 / AR-32）─────────────────────────────


def test_resolve_without_environment_is_unconfigured(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv(model_seam.KEY_ENV, raising=False)
    chosen = model_seam.resolve(None)
    assert isinstance(chosen, UnconfiguredClient)
    with pytest.raises(Unavailable):
        chosen.complete("p", session_id="s", timeout=1.0)


def test_resolve_with_environment_builds_real_client(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv(model_seam.KEY_ENV, "k")
    monkeypatch.setenv(model_seam.MODEL_ENV, "deepseek-v4-pro")
    monkeypatch.setenv(model_seam.BASE_ENV, "https://api.deepseek.com")
    chosen = model_seam.resolve(None)
    assert isinstance(chosen, DeepSeekClient)
    assert_no_execution_surface(chosen)  # 两条路都必须过同一道核对（AR-32）


def test_resolve_prefers_explicit_client_and_bad_timeout_fails(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    explicit = _client(_reply(200, _OK))
    assert model_seam.resolve(explicit) is explicit

    monkeypatch.setenv(model_seam.KEY_ENV, "k")
    monkeypatch.setenv(model_seam.TIMEOUT_ENV, "不是数字")
    with pytest.raises(Unavailable):
        model_seam.resolve(None)
