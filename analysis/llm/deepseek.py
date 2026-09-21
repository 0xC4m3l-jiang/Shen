"""DeepSeek 适配器（`AR-32`）：把提示词变成文本，**只有这一件事**。

它是 `llm.client.AnalysisClient` 的**唯一真实实现**，受三条纪律约束：

1. **公开成员只有 `complete`** —— `assert_no_execution_surface()` 会核对
   （`AR-32` / `SB-1` / `SB-2`：不下发指令、不生成载荷、不调外部系统）；
2. **只依赖标准库**（`http.client`）—— 不引第三方 SDK，少一个依赖面就少一次许可
   （`TB-16`）与供应链审查（[ADR-0024](../../docs/background/decisions/0024-ai-oss-reuse-boundary.md)）；
3. **失败即 `Unavailable`**：非 2xx / 超时 / 响应不可解析 / 缺 key —— **一律不做自动重试**，
   也不返回空串。重试会把「这一轮没生成出来」掩盖成「生成得慢」
   （[ADR-0024](../../docs/background/decisions/0024-ai-oss-reuse-boundary.md) 决定 1：
   默认行为必须 fail-closed）。

**为什么用 `http.client` 而不是 `urllib.request`**：与实机探针
[`scripts/dev/ai-model-probe.py`](../../scripts/dev/ai-model-probe.py) 用同一套传输，
`https` 在类型层面就是唯一可能（构造 `HTTPSConnection`，没有 scheme 选择的降级空间），
且状态码要自己判 —— 与「非 2xx 即失败」的语义直接对应。
（[ADR-0026](../../docs/background/decisions/0026-cloud-model-backend.md) 决定 3 写的是
`urllib.request`；两者都是标准库，本条在 ADR-0031 里记明。）

**密钥纪律**（`ST-20` / `ST-21`）：key 只经构造参数（环境变量由调用方读取），
**不得**出现在异常、日志或任何返回文本里 —— 单测钉住这一条。

**数据出网面**（ADR-0026 决定 1）：本模块只把**调用方给的那一段提示词**发出去；
它不追加任何字段，也不知道任务是什么。数据区里有什么，取决于 `aicap` 的渲染。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import http.client
import json
import ssl
from collections.abc import Callable, Mapping
from urllib.parse import urlparse

from .client import Unavailable

COMPLETIONS_PATH = "/chat/completions"

ECHO_LIMIT = 200
"""回显外部响应时截断到的字符数（外部端点返回的内容不入日志）。"""

Sender = Callable[[str, str, bytes, str, float], tuple[int, bytes]]
"""传输接缝（**只用于测试注入**）：`(base, path, body, key, timeout) -> (status, raw)`。

为什么留这条缝：真实传输要打网络，而单测**禁止**打网络（`make gate` 必须离线可跑）。
生产路径永远走 `_https_send`（模块默认），构造参数只在测试里被替换。
"""


class DeepSeekClient:
    """OpenAI 兼容的对话端点（DeepSeek）—— `AnalysisClient` 的实现。"""

    def __init__(
        self,
        *,
        key: str,
        model: str,
        base: str = "https://api.deepseek.com",
        timeout: float = 30.0,
        sender: Sender | None = None,
    ) -> None:
        if not key:
            raise Unavailable(
                "模型 key 为空 —— 模型后端不可用（AR-15：禁止用模板化文本冒充模型输出）"
            )
        if not model:
            raise Unavailable("模型名不得为空（AR-24：配置随版本分发，不猜默认模型）")
        self._base = _assert_endpoint(base)
        self._key = key
        self._model = model
        self._timeout = _positive_seconds(timeout, field="构造时的 timeout")
        self._sender: Sender = sender or _https_send

    def complete(self, prompt: str, *, session_id: str, timeout: float) -> str:
        """发一次 `POST /chat/completions`，取 `choices[0].message.content`。

        `session_id` 在本适配器里**不参与传输**：会话与提示词的关系由调用方在数据区里表达
        （`AR-31`）。所以这里显式丢弃它，而不是让读者以为「传了就会带上会话」
        （`UnconfiguredClient` 用的是同一种写法）。

        `timeout` 覆盖构造时的默认值 —— 例如 `llm.twophase` 的两阶段超时 T1 / T2 各自独立。
        """
        del session_id
        if not prompt:
            raise Unavailable("提示词为空 —— 不发无内容的请求（AR-31：数据区由调用方渲染）")
        budget = _positive_seconds(timeout, field="调用时的 timeout")
        body = json.dumps(
            {
                "model": self._model,
                "messages": [{"role": "user", "content": prompt}],
                # 结构化输出：**降低**解析失败率的手段，不是替代 `AR-15` 的理由（ADR-0026 决定 3）
                "response_format": {"type": "json_object"},
                "temperature": 0.0,
                "stream": False,
            },
            ensure_ascii=False,
        ).encode("utf-8")

        status, raw = self._send(body, budget)
        if status != 200:
            raise Unavailable(f"模型端点返回 HTTP {status}：{_echo(raw)}")

        return _content_of(_decode(raw), raw=raw)

    def _send(self, body: bytes, timeout: float) -> tuple[int, bytes]:
        """传输调用；**任何**异常都归一成 `Unavailable`，绝不冒泡成别的类型。

        为什么要归一：调用方（`worker` / 生成器）只认 `Unavailable` 这一种「模型不可用」，
        否则「近线不得炸」（`NI-1`）就得在每个调用点重写一遍。
        """
        try:
            return self._sender(self._base, COMPLETIONS_PATH, body, self._key, timeout)
        except Unavailable:
            raise
        except (OSError, TimeoutError, ValueError, http.client.HTTPException) as exc:
            # `HTTPSException` 要一并接住（BadStatusLine 之类）：否则会裸 traceback
            raise Unavailable(f"模型端点不可达：{type(exc).__name__}") from exc


def _assert_endpoint(base: str) -> str:
    """端点必须是 `https://host[:port]`，**不得**带路径（带了就是配置错了，不猜）。"""
    parsed = urlparse(base.strip())
    if parsed.scheme != "https" or not parsed.hostname:
        raise Unavailable(f"模型端点必须是形如 https://host 的地址，实际 {base!r}")
    if parsed.path not in ("", "/") or parsed.query or parsed.fragment:
        raise Unavailable(f"模型端点不得带路径或查询串（协议路径由适配器定），实际 {base!r}")
    return f"https://{parsed.hostname}:{parsed.port or 443}"


def _https_send(base: str, path: str, body: bytes, key: str, timeout: float) -> tuple[int, bytes]:
    """真实传输（生产路径）：TLS **必须**校验证书（显式给默认上下文，不靠「默认就会校验」）。

    这是模块级函数而不是实例成员：实例成员名进 `dir()`，会被 `AR-32` 的成员名检查看见。
    """
    parsed = urlparse(base)
    if not parsed.hostname:  # `_assert_endpoint` 已核过；这里是给类型检查器的显式保证
        raise Unavailable(f"模型端点缺主机名：{base!r}")
    conn = http.client.HTTPSConnection(
        parsed.hostname,
        parsed.port or 443,
        timeout=timeout,
        context=ssl.create_default_context(),
    )
    try:
        conn.request(
            "POST",
            path,
            body=body,
            headers={
                "Authorization": f"Bearer {key}",
                "Content-Type": "application/json",
                "Accept": "application/json",
            },
        )
        response = conn.getresponse()
        return response.status, response.read()
    finally:
        conn.close()


def _positive_seconds(value: float, *, field: str) -> float:
    """超时必须是正数；解析不出或非正即 `Unavailable`（**禁止**悄悄换成默认值）。"""
    try:
        seconds = float(value)
    except (TypeError, ValueError) as exc:
        raise Unavailable(f"{field} 不是数字：{value!r}（AR-19）") from exc
    if not seconds > 0:
        raise Unavailable(f"{field} 必须为正，实际 {value!r}（AR-19）")
    return seconds


def _decode(raw: bytes) -> Mapping[str, object]:
    try:
        decoded = json.loads(raw.decode("utf-8", errors="replace"))
    except (ValueError, UnicodeDecodeError) as exc:
        raise Unavailable(f"模型端点返回的不是 JSON：{_echo(raw)}") from exc
    if not isinstance(decoded, Mapping):
        raise Unavailable(f"模型端点返回的不是 JSON 对象：{_echo(raw)}")
    return decoded


def _content_of(data: Mapping[str, object], *, raw: bytes) -> str:
    """按 OpenAI 兼容形状逐层取 `choices[0].message.content`；任一层不合形状即 `Unavailable`。

    逐层判类型而不是一串下标：下标链在畸形响应上会抛 `TypeError` / `IndexError`，
    而调用方只认「模型不可用」这一种语义（见 `_send` 的理由）。
    """
    choices = data.get("choices")
    if not isinstance(choices, list) or not choices:
        raise Unavailable(f"模型响应没有 choices：{_echo(raw)}")
    message = choices[0].get("message") if isinstance(choices[0], Mapping) else None
    content = message.get("content") if isinstance(message, Mapping) else None
    if not isinstance(content, str) or not content:
        raise Unavailable(f"模型响应没有内容（content 为空）：{_echo(raw)}")
    return content


def _echo(raw: bytes) -> str:
    """回显外部响应片段：只留可打印字符、截断，且**不含 key**（key 从不进 body 的回应）。"""
    return "".join(ch if ch.isprintable() else "?" for ch in raw.decode("utf-8", "replace"))[
        :ECHO_LIMIT
    ]
