#!/usr/bin/env python3
"""向欺骗引擎发送伪造流量，并从**观测面**核对判定结果。

为什么核对走观测面：判定响应**禁止**回显分值 / 规则名 / 枚举（`ST-7`），
所以"引擎判成什么"只能从控制台接口看 —— 这个脚本正是按这条规矩写的。

用法（仓库根执行；默认对着 Docker 栈）：

    scripts/traffic/send.py                      # 全部场景
    scripts/traffic/send.py --group 自动化探针    # 只跑某组
    scripts/traffic/send.py --only probe-git-headless --repeat 3
    scripts/traffic/send.py --json               # 机器可读（自动化用）
    scripts/traffic/send.py --entry http://host:18080 --console http://host:19444

退出码：0 = 全部断言通过（观察类不计入）· 1 = 有断言失败或控制台不可读 · 2 = 用法/场景文件错误。

只用标准库：不需要 venv、不需要装依赖 —— 谁都能跑。
"""

from __future__ import annotations

import argparse
import http.client
import json
import pathlib
import secrets
import sys
import time
import urllib.parse
import urllib.request
from typing import Any

HERE = pathlib.Path(__file__).resolve().parent
SCENARIOS_FILE = HERE / "scenarios.json"

DEFAULT_ENTRY = "http://127.0.0.1:18080"
DEFAULT_CONSOLE = "http://127.0.0.1:19444"

ALLOWED_SCHEMES = ("http", "https")

# 响应头卫生（OH-2）：这两样是"我们自己"的痕迹，出现在响应里即视为泄漏。
# 注意：**不**禁止任何 `Server` 头 —— 上游业务自己会发（例如 Python 的 BaseHTTP），
# 我们禁止的是适配器留下的默认值（Caddy）与 Via。
FORBIDDEN_HEADER_PREFIXES = ("x-shen",)
FORBIDDEN_HEADER_NAMES = ("via",)
FORBIDDEN_HEADER_VALUES = ("caddy",)


class ScenarioError(ValueError):
    """场景文件不合法 —— 直接报错，不猜。"""


class ConsoleError(RuntimeError):
    """控制台不可读或返回了看不懂的东西。"""


def parse_http_url(url: str) -> tuple[str, int, str]:
    """拆出 (host, port, path)；只接受 http/https 且必须有主机名。

    显式校验而不是直接丢给 urlopen：避免 file: 之类非预期 scheme 被放过去。
    """
    parts = urllib.parse.urlsplit(url)
    if parts.scheme not in ALLOWED_SCHEMES:
        raise ValueError(f"只支持 http/https，收到 {url!r}")
    if not parts.hostname:
        raise ValueError(f"URL 里没有主机名：{url!r}")
    port = parts.port or (443 if parts.scheme == "https" else 80)
    return parts.hostname, port, parts.path or "/"


def load_scenarios() -> list[dict[str, Any]]:
    try:
        raw = json.loads(SCENARIOS_FILE.read_text(encoding="utf-8"))
    except OSError as exc:
        raise ScenarioError(f"读不到场景文件 {SCENARIOS_FILE}：{exc}") from exc
    except json.JSONDecodeError as exc:
        raise ScenarioError(f"场景文件不是合法 JSON（{SCENARIOS_FILE}）：{exc}") from exc

    scenarios = raw.get("scenarios")
    if not isinstance(scenarios, list) or not scenarios:
        raise ScenarioError(f"{SCENARIOS_FILE} 里没有 scenarios 列表")
    for item in scenarios:
        for key in ("id", "method", "path", "expect"):
            if key not in item:
                raise ScenarioError(f"场景缺必填字段 {key}：{item}")
    return scenarios


def send(
    entry: str,
    scenario: dict[str, Any],
    nonce: str,
    timeout: float,
    session_cookie: str = "",
) -> tuple[int, dict[str, str]]:
    """发一次请求；返回 (状态码, 响应头)。不改写任何东西 —— 保持"外部请求"的样子。"""
    host, port, _ = parse_http_url(entry)
    path = scenario["path"]
    if nonce:
        joiner = "&" if "?" in path else "?"
        path = f"{path}{joiner}r={nonce}"
    method = scenario["method"].upper()
    headers = dict(scenario.get("headers") or {})
    if session_cookie:
        # 判定键是 (来源, 会话, 方法, 路径)（ST-10）—— 换查询串**不**改变键，
        # 只有换会话才能让每个场景拿到各自的判定（实测过：不换会话会吃到上一条的分）。
        existing = headers.get("Cookie")
        headers["Cookie"] = f"{existing}; {session_cookie}" if existing else session_cookie
    body = scenario.get("body")

    conn = http.client.HTTPConnection(host, port, timeout=timeout)
    try:
        conn.request(method, path, body=body.encode("utf-8") if body else None, headers=headers)
        response = conn.getresponse()
        _ = response.read()  # 必须读完，否则连接不会干净关闭
        return response.status, {k.lower(): v for k, v in response.getheaders()}
    finally:
        conn.close()


def header_findings(headers: dict[str, str]) -> list[str]:
    findings = []
    for name, value in headers.items():
        if name.startswith(FORBIDDEN_HEADER_PREFIXES) or name in FORBIDDEN_HEADER_NAMES:
            findings.append(f"响应头出现 {name}: {value}")
        if any(bad in value.lower() for bad in FORBIDDEN_HEADER_VALUES):
            findings.append(f"响应头 {name} 暴露了实现痕迹：{value}")
    return findings


def fetch_flows(console: str, timeout: float = 5.0) -> list[dict[str, Any]]:
    host, port, _ = parse_http_url(console)
    url = f"http://{host}:{port}/api/flow?limit=300"
    try:
        # scheme 已由 parse_http_url 显式校验（只允许 http/https），故抑制 S310
        with urllib.request.urlopen(url, timeout=timeout) as response:
            payload = response.read().decode("utf-8")
    except OSError as exc:
        raise ConsoleError(f"控制台不可读（{url}）：{exc}") from exc
    try:
        flows = json.loads(payload)
    except json.JSONDecodeError as exc:
        raise ConsoleError(f"控制台返回的不是 JSON（{url}）：{exc}") from exc
    if flows is None:
        # 老版本控制台在"没有判定"时会把空切片序列化成 null（Go 的 nil slice），
        # 这里当作空列表处理；控制台侧已改为返回 []。
        return []
    if not isinstance(flows, list):
        raise ConsoleError(f"控制台 /api/flow 期望列表，实际 {type(flows).__name__}")
    return flows


def wait_for_flow(console: str, scenario: dict[str, Any], *, wait: float) -> dict[str, Any] | None:
    """在控制台的判定流动里找这条请求的记录（按方法与路径匹配，取最新一条）。

    为什么要等：适配器的上报是**异步**的（`AR-11` 异步批量幂等），写入与控制台可见之间有间隔。
    """
    wanted_path = scenario["path"].split("?")[0]
    wanted_method = scenario["method"].upper()
    deadline = time.monotonic() + wait
    while True:
        try:
            flows = fetch_flows(console)
        except ConsoleError:
            flows = []
        matches = [
            row
            for row in flows
            if str(row.get("method", "")).upper() == wanted_method
            and str(row.get("path", "")).split("?")[0] == wanted_path
        ]
        if matches:
            return max(matches, key=lambda row: str(row.get("at", "")))
        if time.monotonic() >= deadline:
            return None
        time.sleep(0.5)


def as_int(value: Any) -> int | None:
    """同上：转不了就返回 None，由调用方当作"配置写错"报出来，不猜。"""
    try:
        return int(value)
    except (TypeError, ValueError):
        return None


def as_float(value: Any) -> float | None:
    """把观测值转成浮点；转不了返回 None（由调用方当作"无法核对"处理，不猜）。"""
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def judge(
    scenario: dict[str, Any], status: int, headers: dict[str, str], flow: dict[str, Any] | None
) -> dict[str, Any]:
    """把一条场景的实测结果与期望比对，返回结果字典。"""
    expect = scenario.get("expect") or {}
    observe_only = bool(expect.get("observe_only"))
    status_in = expect.get("status_in")
    result: dict[str, Any] = {
        "id": scenario["id"],
        "group": scenario.get("group", ""),
        "method": scenario["method"].upper(),
        "path": scenario["path"],
        "http_status": status,
        "observe_only": observe_only,
        "flow": flow,
        "findings": header_findings(headers),
    }

    failures: list[str] = []
    if status_in:
        # 显式声明可接受的状态码（例：演示站没实现 POST，501 是**业务自己**的行为，
        # 与 NI-1（引擎不得影响业务）无关 —— 这类场景要靠 status_in 说清楚）。
        allowed = [as_int(code) for code in status_in]
        if any(code is None for code in allowed):
            failures.append(f"场景里的 status_in 含非数字项：{status_in!r}")
        elif status not in allowed:
            failures.append(f"HTTP {status} 不在允许集合 {status_in} 内")
    elif status >= 500:
        failures.append(f"HTTP {status}：业务被影响（NI-1 不允许）")
    if flow is None and not observe_only:
        failures.append("控制台里没找到这条判定（适配器上报是否正常？）")

    if flow is not None:
        score = as_float(flow.get("score"))
        signals = list(flow.get("signals") or [])
        action = str(flow.get("action") or "")
        result["signals"] = signals
        result["action"] = action
        if score is not None:
            result["score"] = score

        if observe_only:
            pass  # 观察类：只报告，不断言分数/信号
        elif score is None:
            failures.append(f"控制台给的 score 不是数字：{flow.get('score')!r}")
        else:
            for bound, better in (("min_score", "低于下界"), ("max_score", "高于上界")):
                if bound in expect:
                    limit = as_float(expect[bound])
                    if limit is None:
                        failures.append(f"场景里的 {bound} 不是数字：{expect[bound]!r}")
                    elif (score < limit) if bound == "min_score" else (score > limit):
                        failures.append(f"分数 {score} {better} {limit}")
            if "signals_any" in expect and not set(expect["signals_any"]) & set(signals):
                failures.append(f"未命中任一期望信号 {expect['signals_any']}（实际 {signals}）")
            if "signals_all" in expect and not set(expect["signals_all"]) <= set(signals):
                failures.append(f"未命中全部期望信号 {expect['signals_all']}（实际 {signals}）")
            if "action" in expect and action != expect["action"]:
                failures.append(f"决策 {action} ≠ 期望 {expect['action']}")

    result["failures"] = failures
    return result


def render(results: list[dict[str, Any]]) -> None:
    width = max((len(row["id"]) for row in results), default=10)
    print(f"{'场景':<{width}}  {'HTTP':>4}  {'分数':>5}  {'决策':<12} 信号")
    print("-" * (width + 46))
    for row in results:
        score = f"{row['score']:.2f}" if "score" in row else "  — "
        mark = "观察" if row["observe_only"] else ("✓" if not row["failures"] else "✗")
        print(
            f"{row['id']:<{width}}  {row['http_status']:>4}  {score:>5}  "
            f"{row.get('action', '—'):<12} {','.join(row.get('signals', [])) or '—'}  {mark}"
        )
        for failure in row["failures"]:
            print(f"{'':<{width}}    ↳ {failure}")
        for finding in row["findings"]:
            print(f"{'':<{width}}    ↳ 响应头卫生：{finding}")

    asserted = [row for row in results if not row["observe_only"]]
    failed = [row for row in asserted if row["failures"]]
    hygiene = [row for row in results if row["findings"]]
    print()
    print(
        f"断言 {len(asserted) - len(failed)}/{len(asserted)} 通过 · "
        f"观察 {len(results) - len(asserted)} 条 · 响应头卫生问题 {len(hygiene)} 条"
    )
    if failed:
        print("失败场景：" + ", ".join(row["id"] for row in failed))
    if hygiene:
        print("响应头泄漏（OH-2）：" + ", ".join(row["id"] for row in hygiene))


def make_nonce(attempt: int) -> str:
    """每次请求的唯一值：查询串（拟真）用。**它不参与判定键**，所以不能靠它拿新判定。"""
    return f"{secrets.token_hex(4)}{attempt:02d}"


def make_session(cookie_name: str, attempt: int) -> str:
    """每次请求的唯一会话 cookie。

    判定键是 (来源, 会话, 方法, 路径)（ST-10）—— 换会话才改变键，
    否则第二次请求会复用第一次的判定（控制台里只看到旧的分数）。
    """
    return f"{cookie_name}={secrets.token_hex(8)}{attempt:02d}"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="scripts/traffic/send.py", description="向欺骗引擎发送伪造流量并核对判定"
    )
    parser.add_argument(
        "--entry", default=DEFAULT_ENTRY, help=f"业务入口（经引擎），默认 {DEFAULT_ENTRY}"
    )
    parser.add_argument(
        "--console", default=DEFAULT_CONSOLE, help=f"观测控制台，默认 {DEFAULT_CONSOLE}"
    )
    parser.add_argument("--only", action="append", default=[], help="只跑指定场景 id（可重复）")
    parser.add_argument("--group", action="append", default=[], help="只跑指定分组（可重复）")
    parser.add_argument("--repeat", type=int, default=1, help="每个场景重复次数（默认 1）")
    parser.add_argument("--delay", type=float, default=0.0, help="每次之间的间隔秒数")
    parser.add_argument("--wait", type=float, default=8.0, help="等待判定在控制台出现的最长秒数")
    parser.add_argument("--timeout", type=float, default=10.0, help="单次请求超时秒数")
    parser.add_argument("--no-nonce", action="store_true", help="不追加拟真用的查询串")
    parser.add_argument(
        "--session-cookie",
        default="sid",
        help="会话 cookie 名（默认 sid，见 session.cookie_name）；每条请求唯一值→独立判定",
    )
    parser.add_argument(
        "--no-session-nonce", action="store_true", help="复用同一会话（复现 ST-10 判定复用）"
    )
    parser.add_argument("--json", action="store_true", help="输出 JSON（自动化用）")
    args = parser.parse_args(argv)

    try:
        scenarios = load_scenarios()
    except ScenarioError as exc:
        print(f"场景文件有问题：{exc}", file=sys.stderr)
        return 2

    if args.only:
        scenarios = [row for row in scenarios if row["id"] in set(args.only)]
    if args.group:
        scenarios = [row for row in scenarios if row.get("group") in set(args.group)]
    if not scenarios:
        print("没有匹配的场景（检查 --only / --group）", file=sys.stderr)
        return 2

    try:
        fetch_flows(args.console)
    except (ConsoleError, ValueError) as exc:
        print(f"{exc}\n先跑 scripts/shen.sh status 看看服务起来没", file=sys.stderr)
        return 1

    results: list[dict[str, Any]] = []
    for scenario in scenarios:
        for attempt in range(args.repeat):
            nonce = "" if args.no_nonce else make_nonce(attempt)
            session = "" if args.no_session_nonce else make_session(args.session_cookie, attempt)
            try:
                status, headers = send(args.entry, scenario, nonce, args.timeout, session)
            except (OSError, ValueError) as exc:
                results.append(
                    {
                        "id": scenario["id"],
                        "group": scenario.get("group", ""),
                        "method": scenario["method"].upper(),
                        "path": scenario["path"],
                        "http_status": 0,
                        "observe_only": False,
                        "flow": None,
                        "findings": [],
                        "failures": [f"请求没发出去：{exc}"],
                        "attempt": attempt + 1,
                    }
                )
                continue
            row = judge(
                scenario, status, headers, wait_for_flow(args.console, scenario, wait=args.wait)
            )
            row["attempt"] = attempt + 1
            results.append(row)
            if args.delay:
                time.sleep(args.delay)

    if args.json:
        asserted = [row for row in results if not row["observe_only"]]
        payload = {
            "entry": args.entry,
            "console": args.console,
            "results": results,
            "summary": {
                "asserted": len(asserted),
                "failed": len([row for row in asserted if row["failures"]]),
                "observed": len(results) - len(asserted),
                "header_findings": len([row for row in results if row["findings"]]),
            },
        }
        print(json.dumps(payload, ensure_ascii=False, indent=2))
    else:
        render(results)

    failed = any(row["failures"] for row in results)
    hygiene = any(row["findings"] for row in results)
    return 1 if (failed or hygiene) else 0


if __name__ == "__main__":
    raise SystemExit(main())
