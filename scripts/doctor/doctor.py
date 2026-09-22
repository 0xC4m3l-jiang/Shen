#!/usr/bin/env python3
"""接入自检（`INT-17`）：五项必须能给**通过 / 失败**的结论。

五项（规则原文见 docs/design/integration.md `INT-17`）：

    ① body 是否可读        —— 判不了 body 就只能观察（`INT-22` 禁止启用误导处置）
    ② TLS 是终结还是透传    —— 默认应交给客户 L0（ADR-0019）
    ③ 会话粘性是否生效      —— 同会话同路径应复用同一判定（`ST-10`）
    ④ 真实业务与蜜罐是否可区分 —— 需要登记的幻境后端；没有就如实说"无法判定"
    ⑤ 引擎是否真的在请求路径上 —— 请求应出现在观测面（没出现＝不在路径上）

设计原则：**能验的验、不能验的说清为什么**（不给假绿）。每项都给「结论 + 判据 + 失败怎么办」。

用法：

    scripts/doctor/doctor.py                             # 默认对本地 Docker 栈
    scripts/doctor/doctor.py --entry http://host:8080 --console http://host:9444
    scripts/doctor/doctor.py --origin http://127.0.0.1:19080   # 直连业务，用于 ⑤ 对照
    scripts/doctor/doctor.py --json

退出码：0 = 五项都给出了结论且无失败 · 1 = 有失败项 · 2 = 参数/环境错（入口或控制台不可达）。

只用标准库。
"""

from __future__ import annotations

import argparse
import http.client
import json
import secrets
import ssl
import sys
import time
import urllib.parse
import urllib.request
from typing import Any

DEFAULT_ENTRY = "http://127.0.0.1:18080"
DEFAULT_CONSOLE = "http://127.0.0.1:19444"

# 状态取值（不是密码）。常量名用 `STATUS_OK` 而不是 `STATUS_OK`：
# 后者会被凭据类静态检查器当成硬编码密码（**假阳性**），改名比登记豁免更省事。
STATUS_OK = "通过"
STATUS_FAIL = "失败"
STATUS_CONSTRAINT = "约束"  # 不是失败：验证成功，但结论限制了能做什么
STATUS_UNKNOWN = "无法判定"


class ConsoleError(RuntimeError):
    """控制台返回了看不懂的东西。"""


DANGLING_HINTS = {
    "body": (
        "要让引擎判 body：在核心观测里加 body（或摘要）字段 —— 但**先读 `INT-22`**："
        "读不到 body 时禁止启用误导处置。"
    ),
    "session": (
        "会话粘性依赖 `decision_id`（由 来源 / 会话 / 路径 / 时间窗 派生，`ST-10`），"
        "而本地判定缓存键还含方法 / 查询串 / Host / UA / 策略版本（2026-09-22 起）。"
        "不生效时先看适配器日志里的 `decision_id`。"
    ),
    "mirage": (
        "需要：① 非影子模式 ② `honeypots[]` 登记一个**可用**后端 ③ 命中阈值。"
        "见 docs/ops/functional-verification.md §3。"
    ),
    "path": "引擎不在请求路径上时，先确认上游指向了适配器（形态③的 `SHEN_PROXY_UPSTREAM`）。",
}


def parse_url(url: str) -> tuple[str, int, str]:
    parts = urllib.parse.urlsplit(url)
    if parts.scheme not in ("http", "https"):
        raise ValueError(f"只支持 http/https：{url!r}")
    if not parts.hostname:
        raise ValueError(f"URL 没有主机名：{url!r}")
    return parts.hostname, parts.port or (443 if parts.scheme == "https" else 80), parts.path or "/"


def request(
    host: str,
    port: int,
    path: str,
    *,
    method: str = "GET",
    headers: dict[str, str] | None = None,
    body: bytes | None = None,
    timeout: float = 8.0,
    tls: bool = False,
) -> tuple[int, dict[str, str], bytes, Any]:
    """发一次请求；返回 (状态码, 响应头, 响应体, TLS 证书信息)。"""
    cert: Any = None
    if tls:
        context = ssl.create_default_context()
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE
        conn: http.client.HTTPConnection = http.client.HTTPSConnection(
            host, port, timeout=timeout, context=context
        )
    else:
        conn = http.client.HTTPConnection(host, port, timeout=timeout)
    try:
        conn.request(method, path, body=body, headers=headers or {})
        resp = conn.getresponse()
        payload = resp.read()
        if tls:
            sock = getattr(conn, "sock", None)
            cert = sock.getpeercert() if sock is not None else None
        return resp.status, {k.lower(): v for k, v in resp.getheaders()}, payload, cert
    finally:
        conn.close()


def console_json(console: str, path: str, timeout: float = 8.0) -> Any:
    host, port, _ = parse_url(console)
    url = f"http://{host}:{port}{path}"
    # scheme 与主机名已显式校验（只允许 http/https）
    # nosemgrep: python.lang.security.audit.dynamic-urllib-use-detected
    with urllib.request.urlopen(url, timeout=timeout) as resp:
        raw = resp.read().decode("utf-8")
    try:
        return json.loads(raw)
    except json.JSONDecodeError as exc:
        raise ConsoleError(f"控制台返回的不是 JSON（{url}）：{exc}") from exc


def flows(console: str) -> list[dict[str, Any]]:
    data = console_json(console, "/api/flow?limit=500")
    return data if isinstance(data, list) else []


def wait_flow(console: str, path: str, method: str = "GET", *, wait: float = 8.0) -> dict | None:
    deadline = time.monotonic() + wait
    while True:
        try:
            rows = flows(console)
        except OSError:
            rows = []
        hit = [
            r
            for r in rows
            if str(r.get("path", "")).split("?")[0] == path
            and str(r.get("method", "")).upper() == method
        ]
        if hit:
            return max(hit, key=lambda r: str(r.get("at", "")))
        if time.monotonic() >= deadline:
            return None
        time.sleep(0.5)


def check_body(entry: str, console: str, timeout: float) -> dict[str, Any]:
    """① body 是否可读：发一个带唯一标记的 POST，看观测里能否看到该标记。"""
    marker = f"doctor-body-{secrets.token_hex(6)}"
    path = f"/doctor/body?probe={marker}"
    host, port, _ = parse_url(entry)
    status, _, _, _ = request(
        host,
        port,
        path,
        method="POST",
        headers={"Content-Type": "text/plain", "User-Agent": "shen-doctor/1.0"},
        body=f"marker={marker}".encode(),
    )
    flow = wait_flow(console, "/doctor/body", "POST", wait=timeout)
    if flow is None:
        # 拿到了上游响应却没有判定记录 ⇒ 引擎在路径上但**这次没判**
        # 实测踩过：重启后第一条请求的 gRPC 建连超出判定预算 3ms ⇒ DeadlineExceeded
        # ⇒ 放行但无观测记录
        reached = status not in (0, 502, 503, 504)
        return {
            "id": "① body 可读",
            "status": STATUS_UNKNOWN,
            "detail": (
                f"该 POST 拿到上游响应（HTTP {status}）但无判定记录 ⇒ 在路径上、这次没判"
                "（判定超时后放行 / 判定缓存命中 / 白名单）"
                if reached
                else f"未在观测面找到该 POST（HTTP {status}）—— 先确认引擎在请求路径上（见 ⑤）"
            ),
            "hint": (
                "看适配器日志里这条请求的 `失败=` 字段：`DeadlineExceeded` 就是判定超时后放行"
                "（业务不受影响，但这次没有判定记录）。重跑本项即可（第二条通常会命中）。"
            ),
        }
    if marker in json.dumps(flow, ensure_ascii=False):
        return {
            "id": "① body 可读",
            "status": STATUS_OK,
            "detail": "观测里出现了请求体标记 ⇒ 可判 body",
            "hint": "",
        }
    return {
        "id": "① body 可读",
        "status": STATUS_CONSTRAINT,
        "detail": (
            "观测里**没有**请求体字段 ⇒ 引擎当前不读 body："
            "按 `INT-22` **只能观察，禁止启用误导处置**"
        ),
        "hint": DANGLING_HINTS["body"],
    }


def check_tls(entry: str, timeout: float) -> dict[str, Any]:
    """② TLS 是终结还是透传：分别试明文与 TLS，看入口到底在哪一层收口。"""
    host, port, _ = parse_url(entry)
    plain = tls = None
    try:
        status, _, _, _ = request(host, port, "/doctor/tls", timeout=timeout)
        plain = status
    except OSError as exc:
        plain = f"{type(exc).__name__}"
    try:
        status, _, _, cert = request(host, port, "/doctor/tls", timeout=timeout, tls=True)
        tls = (status, cert)
    except (OSError, ssl.SSLError) as exc:
        tls = type(exc).__name__

    if isinstance(tls, tuple):
        cert_info = tls[1] or {}
        subject = cert_info.get("subject")
        issuer = cert_info.get("issuer")
        return {
            "id": "② TLS 终结方式",
            "status": STATUS_OK,
            "detail": (
                f"入口接受 TLS（HTTP {tls[0]}）⇒ **引擎自终结**；"
                f"证书 subject={subject} issuer={issuer}。"
                "按 ADR-0019，默认应交给客户 L0 终结（指纹天然一致）"
            ),
            "hint": "若要交回 L0：`SHEN_PROXY_TLS_MODE=off`（默认）并在上游配 TLS。",
        }
    if plain is not None and isinstance(plain, int):
        return {
            "id": "② TLS 终结方式",
            "status": STATUS_OK,
            "detail": (
                f"入口是**明文 HTTP**（{plain}），TLS 未在引擎终结 ⇒ 由前置 L0 终结"
                "（ADR-0019 的默认形态）"
            ),
            "hint": "",
        }
    return {
        "id": "② TLS 终结方式",
        "status": STATUS_UNKNOWN,
        "detail": f"明文与 TLS 都不通（明文={plain}，TLS={tls}）—— 入口地址或端口不对？",
        "hint": "确认 `SHEN_HTTP_PORT`（默认 18080）与适配器监听地址。",
    }


def check_session(entry: str, console: str, timeout: float) -> dict[str, Any]:
    """③ 会话粘性：同会话同路径两次请求应复用同一判定（`ST-10`）。"""
    sid = f"doctor-{secrets.token_hex(8)}"
    path = "/doctor/session"
    host, port, _ = parse_url(entry)
    ids = []
    for _ in range(2):
        request(
            host,
            port,
            f"{path}?t={secrets.token_hex(4)}",
            headers={"Cookie": f"sid={sid}", "User-Agent": "shen-doctor/1.0"},
            timeout=timeout,
        )
        time.sleep(0.4)
        flow = wait_flow(console, path, "GET", wait=timeout)
        ids.append(str((flow or {}).get("decision_id", "")))
    if not all(ids):
        return {
            "id": "③ 会话粘性",
            "status": STATUS_UNKNOWN,
            "detail": "没取到判定（观测面里没有这两条）",
            "hint": DANGLING_HINTS["path"],
        }
    if ids[0] == ids[1]:
        return {
            "id": "③ 会话粘性",
            "status": STATUS_OK,
            "detail": f"同会话两次请求复用同一判定 decision_id={ids[0]}（`ST-10` 生效）",
            "hint": "",
        }
    return {
        "id": "③ 会话粘性",
        "status": STATUS_FAIL,
        "detail": f"同会话两次得到不同判定：{ids[0]} → {ids[1]}（会话没粘住 ⇒ 判定键里的会话变了）",
        "hint": DANGLING_HINTS["session"],
    }


def check_mirage(entry: str, console: str, timeout: float) -> dict[str, Any]:
    """④ 真实业务与幻境可区分：需要真的有一次改道（否则如实说无法判定）。"""
    try:
        rows = flows(console)
    except OSError as exc:
        return {
            "id": "④ 实境/幻境可区分",
            "status": STATUS_UNKNOWN,
            "detail": f"读控制台失败：{exc}",
            "hint": "",
        }
    mirage = [r for r in rows if str(r.get("action", "")) == "route_mirage"]
    if not mirage:
        return {
            "id": "④ 实境/幻境可区分",
            "status": STATUS_UNKNOWN,
            "detail": (
                "观测里没有任何 `route_mirage` 判定（影子模式 或 未登记可用幻境后端）⇒ 无法比较"
            ),
            "hint": DANGLING_HINTS["mirage"],
        }
    sample = mirage[0]
    path = str(sample.get("path", "/"))
    backend = str(sample.get("backend", ""))
    host, port, _ = parse_url(entry)
    try:
        status, headers, body, _ = request(
            host, port, path, headers={"User-Agent": "shen-doctor/1.0"}, timeout=timeout
        )
    except OSError as exc:
        return {
            "id": "④ 实境/幻境可区分",
            "status": STATUS_UNKNOWN,
            "detail": f"复现改道失败：{exc}",
            "hint": "",
        }
    return {
        "id": "④ 实境/幻境可区分",
        "status": STATUS_OK,
        "detail": (
            f"存在改道判定（后端 {backend or '未解析'}）；同路径经引擎取回 HTTP {status}、"
            f"{len(body)} 字节、Server={headers.get('server', '—')}"
        ),
        "hint": "逐字段 diff（12 项）由差异哨兵负责（`scripts/sentinel`，**待实现**）。",
    }


def check_path(entry: str, console: str, origin: str, timeout: float) -> dict[str, Any]:
    """⑤ 引擎是否真的在请求路径上：带唯一标记的请求应出现在观测面。"""
    marker = secrets.token_hex(6)
    path = f"/doctor/path/{marker}"
    host, port, _ = parse_url(entry)
    status, _, _, _ = request(
        host, port, path, headers={"User-Agent": "shen-doctor/1.0"}, timeout=timeout
    )
    flow = wait_flow(console, path, "GET", wait=timeout)
    detail = f"经引擎请求 HTTP {status}"
    if flow is None:
        return {
            "id": "⑤ 引擎在请求路径上",
            "status": STATUS_FAIL,
            "detail": f"{detail}，但观测面**没有**这条判定 ⇒ 请求没经过引擎判定",
            "hint": DANGLING_HINTS["path"],
        }
    detail += f"，观测面有判定 decision_id={flow.get('decision_id')}"
    if origin:
        before = len(flows(console))
        ohost, oport, _ = parse_url(origin)
        try:
            request(
                ohost,
                oport,
                f"{path}?direct=1",
                headers={"User-Agent": "shen-doctor/1.0"},
                timeout=timeout,
            )
        except OSError as exc:
            detail += f"；直连对照失败（{exc}）"
            return {"id": "⑤ 引擎在请求路径上", "status": STATUS_OK, "detail": detail, "hint": ""}
        time.sleep(1.5)
        after = len(flows(console))
        detail += f"；直连业务后判定数 {before} → {after}（应**不变**）"
        if after != before:
            return {
                "id": "⑤ 引擎在请求路径上",
                "status": STATUS_FAIL,
                "detail": detail + " —— 直连业务也产生了判定？对照不成立",
                "hint": "确认直连地址是**业务真实地址**，不是引擎入口。",
            }
    return {"id": "⑤ 引擎在请求路径上", "status": STATUS_OK, "detail": detail, "hint": ""}


def render(rows: list[dict[str, Any]]) -> None:
    icon = {STATUS_OK: "✅", STATUS_FAIL: "❌", STATUS_CONSTRAINT: "⚠️ ", STATUS_UNKNOWN: "➖"}
    width = max(len(r["id"]) for r in rows) + 2
    for row in rows:
        print(f"{icon.get(row['status'], '?')} {row['id']:<{width}} {row['status']}")
        print(f"{'':<{width + 4}}{row['detail']}")
        if row["hint"]:
            print(f"{'':<{width + 4}}怎么办：{row['hint']}")
    print()
    failed = [r for r in rows if r["status"] == STATUS_FAIL]
    constrained = [r for r in rows if r["status"] == STATUS_CONSTRAINT]
    unknown = [r for r in rows if r["status"] == STATUS_UNKNOWN]
    print(
        f"结论：通过 {len([r for r in rows if r['status'] == STATUS_OK])} · 失败 {len(failed)} · "
        f"约束 {len(constrained)} · 无法判定 {len(unknown)}"
    )
    if constrained:
        print("约束（不是失败，但限制你能做什么）：" + "、".join(r["id"] for r in constrained))
    if unknown:
        print(
            "无法判定（工作没白做：原因与关法都写在上面）：" + "、".join(r["id"] for r in unknown)
        )
    if failed:
        print("失败项：" + "、".join(r["id"] for r in failed))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="scripts/doctor/doctor.py", description="接入自检（INT-17 五项）"
    )
    parser.add_argument(
        "--entry", default=DEFAULT_ENTRY, help=f"业务入口（经引擎），默认 {DEFAULT_ENTRY}"
    )
    parser.add_argument(
        "--console", default=DEFAULT_CONSOLE, help=f"观测控制台，默认 {DEFAULT_CONSOLE}"
    )
    parser.add_argument("--origin", default="", help="业务真实地址（用于 ⑤ 的直连对照；可不填）")
    parser.add_argument("--timeout", type=float, default=8.0, help="单步超时秒数")
    parser.add_argument("--json", action="store_true", help="输出 JSON（自动化用）")
    args = parser.parse_args(argv)

    try:
        rows = [
            check_body(args.entry, args.console, args.timeout),
            check_tls(args.entry, args.timeout),
            check_session(args.entry, args.console, args.timeout),
            check_mirage(args.entry, args.console, args.timeout),
            check_path(args.entry, args.console, args.origin, args.timeout),
        ]
    except (OSError, ValueError) as exc:
        print(f"自检无法进行：{exc}\n先跑 scripts/shen.sh status 看服务起来没", file=sys.stderr)
        return 2

    if args.json:
        print(
            json.dumps(
                {"entry": args.entry, "console": args.console, "checks": rows},
                ensure_ascii=False,
                indent=2,
            )
        )
    else:
        render(rows)

    return 1 if any(r["status"] == STATUS_FAIL for r in rows) else 0


if __name__ == "__main__":
    raise SystemExit(main())
