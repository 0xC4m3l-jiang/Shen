#!/usr/bin/env python3
"""向欺骗引擎发送伪造流量，并从**观测面**核对判定结果。

为什么核对走观测面：判定响应**禁止**回显分值 / 规则名 / 枚举（`ST-7`），
所以"引擎判成什么"只能从控制台接口看 —— 这个脚本正是按这条规矩写的。

用法（仓库根执行；默认对着 Docker 栈）：

    scripts/traffic/send.py                       # 全部场景（按分组打印）
    scripts/traffic/send.py --group 扫描器指纹      # 只跑一组（可重复）
    scripts/traffic/send.py --only probe-git-headless --repeat 3
    scripts/traffic/send.py --check-l4            # 顺带核对 L4 结论与证据引用（AR-12）
    scripts/traffic/send.py --json                # 机器可读（自动化用）
    scripts/traffic/send.py --entry http://host:18080 --console http://host:19444

退出码：0 = 全部断言通过（观察类与已知缺口不计入）·
1 = 有断言失败/泄漏/控制台不可读 · 2 = 用法或场景文件错误。

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
) -> tuple[int, dict[str, str], str]:
    """发一次请求；返回 (状态码, 响应头, 响应体文本)。

    响应体只用于两件事：检查 `ST-7`（业务响应里不得出现判定细节）与展示长度，不做别的解析。
    """
    host, port, _ = parse_http_url(entry)
    # 非 ASCII 路径要百分号编码：HTTP 请求行必须 ASCII（真实客户端也是这么发的）
    path = urllib.parse.quote(scenario["path"], safe="/?=&%+")
    if nonce:
        joiner = "&" if "?" in path else "?"
        path = f"{path}{joiner}r={nonce}"
    method = scenario["method"].upper()
    headers = dict(scenario.get("headers") or {})
    if session_cookie:
        # `decision_id` 由 (来源, 会话, 路径, 时间窗) 派生（ST-10，未变）；而**本地判定缓存键**
        # 2026-09-22 起还包括方法 / 查询串 / Host / UA 与策略版本
        # （见 modules/deception/proxy/cache.go）。所以下面两个作用各归其位：
        # 查询串 nonce 让每次请求**重新判定**（不吃上一条的判定）；
        # 会话 cookie 让每次请求拿到**不同的 decision_id**（判定身份不同，图上看得出是两次）。
        existing = headers.get("Cookie")
        headers["Cookie"] = f"{existing}; {session_cookie}" if existing else session_cookie
    body = scenario.get("body")

    conn = http.client.HTTPConnection(host, port, timeout=timeout)
    try:
        conn.request(method, path, body=body.encode("utf-8") if body else None, headers=headers)
        response = conn.getresponse()
        payload = response.read()
        text = payload.decode("utf-8", errors="replace")
        return response.status, {k.lower(): v for k, v in response.getheaders()}, text
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


def body_findings(body: str, flow: dict[str, Any] | None) -> list[str]:
    """`ST-7`：业务响应里不得出现判定细节（命中信号名、判定 id）。

    这条是被回显禁令的直接检查 —— 泄漏了就等于把我们的判定告诉对手。
    """
    if not body or flow is None:
        return []
    findings = []
    lowered = body.lower()
    for signal in flow.get("signals") or []:
        if str(signal).lower() in lowered:
            findings.append(f"响应体里出现命中信号名 {signal}（ST-7 禁止回显）")
    decision_id = str(flow.get("decision_id") or "")
    if decision_id and decision_id in body:
        findings.append(f"响应体里出现判定 id {decision_id}（内部标识不该外泄）")
    return findings


def fetch_json(path: str, console: str, timeout: float = 5.0) -> Any:
    host, port, _ = parse_http_url(console)
    url = f"http://{host}:{port}{path}"
    try:
        # scheme 与主机名已由 parse_http_url 显式校验（只允许 http/https），故：
        # nosemgrep: python.lang.security.audit.dynamic-urllib-use-detected
        with urllib.request.urlopen(url, timeout=timeout) as response:
            raw = response.read().decode("utf-8")
    except OSError as exc:
        raise ConsoleError(f"控制台不可读（{url}）：{exc}") from exc
    try:
        return json.loads(raw)
    except json.JSONDecodeError as exc:
        raise ConsoleError(f"控制台返回的不是 JSON（{url}）：{exc}") from exc


def fetch_flows(console: str) -> list[dict[str, Any]]:
    flows = fetch_json("/api/flow?limit=500", console)
    if flows is None:
        # 老版本控制台会把空切片序列化成 null（Go 的 nil slice）；控制台侧已改为 []。
        return []
    if not isinstance(flows, list):
        raise ConsoleError(f"控制台 /api/flow 期望列表，实际 {type(flows).__name__}")
    return flows


def wait_for_flow(
    console: str,
    scenario: dict[str, Any],
    *,
    wait: float,
) -> dict[str, Any] | None:
    """在控制台的判定流动里找这条请求的记录（按方法与路径匹配，取最新一条）。

    为什么要等：适配器的上报是**异步**的（`AR-11` 异步批量幂等），写入与控制台可见之间有间隔。

    注意：**`executed` / `inject` 不在这里**（这是判定记录）—— 它们在逐请求链路里，
    由 `wait_for_graph` 单独取（实测：本接口的 `executed` 恒为 None）。
    """
    # 控制台记录的是**解码后**的路径（实测：非 ASCII 路径存成 /搜索/商品），
    # 所以两边都按解码形态比较，编码与非编码的写法都能匹配上。
    wanted_path = urllib.parse.unquote(scenario["path"].split("?")[0])
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
            and urllib.parse.unquote(str(row.get("path", "")).split("?")[0]) == wanted_path
        ]
        if matches:
            return max(matches, key=lambda row: str(row.get("at", "")))
        if time.monotonic() >= deadline:
            return None
        time.sleep(0.5)


def wait_for_graph(console: str, decision_id: str, *, wait: float) -> dict[str, Any] | None:
    """从逐请求链路（`/api/graphs`）里按 `decision_id` 取这条请求的**实际落点**与注入结果。

    为什么要单独取：`executed` / `inject` **只在这条记录里**（`/api/flow` 是判定记录，没有它们 ——
    实测它的这两个字段恒为 None）。判定记录先到、逐请求记录随后到，所以要**等**。
    """
    if not decision_id:
        return None
    deadline = time.monotonic() + wait
    while True:
        try:
            rows = fetch_json("/api/graphs?limit=200", console) or []
        except ConsoleError:
            rows = []
        if isinstance(rows, list):
            for row in rows:
                if isinstance(row, dict) and str(row.get("decision_id") or "") == decision_id:
                    return row
        if time.monotonic() >= deadline:
            return None
        time.sleep(0.5)


def as_float(value: Any) -> float | None:
    """把观测值转成浮点；转不了返回 None（由调用方当作"无法核对"处理，不猜）。"""
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def as_int(value: Any) -> int | None:
    """同上：转不了就返回 None，由调用方当作"配置写错"报出来，不猜。"""
    try:
        return int(value)
    except (TypeError, ValueError):
        return None


def judge(
    scenario: dict[str, Any],
    status: int,
    headers: dict[str, str],
    body: str,
    flow: dict[str, Any] | None,
    *,
    previous_decision: str | None,
    stack_form: str = "unknown",
) -> dict[str, Any]:
    """把一条场景的实测结果与期望比对，返回结果字典。

    `gap` 类场景（已知缺口）只记录事实 + 缺口说明，**不**计入失败 —— 它的用途是把
    "设计有、当前没做到" 的东西显式列出来，而不是制造红灯。
    """
    expect = scenario.get("expect") or {}
    # 形态判定必须在**任何断言之前**：需要接管形态的场景在影子栈里连状态码都不该被断言
    # （`block` 场景在影子栈里必然 200 —— 那是形态，不是失败）。以前这段在状态码检查**之后**，
    # 于是观察行里残留一条失败 ⇒ 整次运行退出码 1。
    needs_takeover = str(scenario.get("stack") or "") == "takeover"
    form_observe = needs_takeover and stack_form == "shadow"
    observe_only = bool(expect.get("observe_only")) or form_observe
    if form_observe:
        scenario["_observe_note"] = (
            "影子栈（/api/topology 的 shadow=true）⇒ 只算不处置，本场景只能观察；"
            "要跑接管形态请加 deploy/docker/compose.verify-mirage.yaml"
        )
    gap = expect.get("gap")
    result: dict[str, Any] = {
        "id": scenario["id"],
        "group": scenario.get("group", ""),
        "why": scenario.get("why", ""),
        "method": scenario["method"].upper(),
        "path": scenario["path"],
        "http_status": status,
        "observe_only": observe_only,
        "observe_note": scenario.get("_observe_note", ""),
        "gap": gap,
        "flow": flow,
        "findings": header_findings(headers),
        "st7_findings": body_findings(body, flow),
        "body_bytes": len(body.encode("utf-8")),
    }

    failures: list[str] = []
    if scenario.get("_unknown_ai"):
        failures.append(
            "读不到核心快照（/api/config）⇒ 无法判定 AI 层是否打开：证据缺失，不当作通过"
            "（先确认控制台可达、再重跑）"
        )
    status_in = expect.get("status_in")
    if observe_only:
        pass  # 观察行不断言状态码（影子栈里 block 场景必然 200 —— 那是形态）
    elif status_in:
        # 显式声明可接受的状态码（例：演示站没实现 POST，501 是**业务自己**的行为，
        # 与 NI-1（引擎不得影响业务）无关 —— 这类场景要靠 status_in 说清楚）。
        allowed = [as_int(code) for code in status_in]
        if any(code is None for code in allowed):
            failures.append(f"场景里的 status_in 含非数字项：{status_in!r}")
        elif status not in allowed:
            failures.append(f"HTTP {status} 不在允许集合 {status_in} 内")
    elif status >= 500:
        failures.append(f"HTTP {status}：业务被影响（NI-1 不允许）")

    if flow is not None:
        score = as_float(flow.get("score"))
        signals = list(flow.get("signals") or [])
        action = str(flow.get("action") or "")
        executed = str(flow.get("executed") or "")
        inject = str(flow.get("inject") or "")
        decision_id = str(flow.get("decision_id") or "")
        result["signals"] = signals
        result["action"] = action
        result["executed"] = executed
        result["inject"] = inject
        result["decision_id"] = decision_id

        # 形态不匹配 ⇒ 记「观察」，**不是失败** —— 但判据必须是**权威信号**，不是结果形状。
        #
        # 为什么：`executed=origin` 有两种成因 —— ① 影子栈（只算不处置，INT-11）；
        # ② 接管栈但改道/拦截**没生效**（真失败）。凭形状降级会把 ② 放行成绿，
        # 而「忘了挂 compose.verify-mirage.yaml」恰好就长成 ②。
        # 权威信号是控制台链路里的 `shadow`（graph 自己算，见 modules/console 的 Graph.Shadow）：
        # 它是 true ⇒ 形态是影子，只能观察；是 false ⇒ 接管形态，落点不对就是失败。
        # 形态的判据与说明在函数开头就定了（见 `form_observe`）：
        #   · shadow=true  ⇒ 形态就是影子，落点必是 origin（观察，不是失败）；
        #   · shadow=false ⇒ 接管形态 ⇒ 落点/决策不对就是**真失败**
        #     （「忘了挂 compose.verify-mirage.yaml」正是这个形状 —— 以前会被形状判据放行）；
        #   · 读不到 ⇒ 不降级、也不额外指控，同样走断言。
        result["stack_form"] = stack_form
        result["observe_only"] = observe_only
        result["observe_note"] = scenario.get("_observe_note", "")
        if score is not None:
            result["score"] = score

        if (
            "same_decision_as_previous" in expect
            and previous_decision is not None
            and decision_id != previous_decision
        ):
            failures.append(f"期望复用同一判定，实际换了：{previous_decision} → {decision_id}")
        if (
            expect.get("distinct_decisions")
            and previous_decision is not None
            and decision_id == previous_decision
        ):
            failures.append(f"期望不同判定，实际复用同一 decision_id：{decision_id}")

        if not (observe_only or gap):
            if score is None:
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
                # 逐场景的落点 / 注入期望（`executed` 允许写一个值或一个集合）：
                # 这两条是「处置真的执行下去了」与「内容真的注进去了」的可断言形式。
                if "executed" in expect:
                    want = expect["executed"]
                    allowed = want if isinstance(want, list) else [want]
                    if executed not in [str(w) for w in allowed]:
                        failures.append(f"落点 {executed or '（空）'} 不在期望 {allowed} 内")
                if "inject" in expect and inject != str(expect["inject"]):
                    failures.append(f"注入结果 {inject or '（空）'} ≠ 期望 {expect['inject']}")
    elif not (observe_only or gap):
        failures.append("控制台里没找到这条判定（适配器上报是否正常？）")

    result["failures"] = failures
    return result


EXECUTED_VALUES = {
    "origin",
    "cache",
    "failopen",
    "origin_fallback",
    "mirage",
    "block",
    "whitelist",
}
"""`executed` 的合法取值（权威表见 docs/spec/events.md §2.2）。多一个少一个都说明契约漂了。"""

INJECT_VALUES = {
    "applied",
    "disabled",
    "no_content",
    "off",
}
"""`inject` 的合法取值（AI 欺骗内容注入的结果；权威表见 docs/spec/events.md §2.2 与 ADR-0023）。"""


def check_graph(console: str) -> dict[str, Any]:
    """核对逐请求链路（DAG）：每条请求一条链路，且每一步的「请求/响应/为什么」都在。

    这条断言守住用户在意的两件事：**每条流量单独成图**（而不是被折叠/聚合）、**每步文字完整**。
    """
    chains = fetch_json("/api/graphs?limit=200", console)
    if chains is None:
        chains = []
    if not isinstance(chains, list):
        raise ConsoleError(f"/api/graphs 期望列表，实际 {type(chains).__name__}")
    problems: list[str] = []
    for ch in chains:
        did = str(ch.get("decision_id") or "?")
        executed = str(ch.get("executed") or "")
        if executed not in EXECUTED_VALUES:
            problems.append(f"{did}: executed={executed!r} 不在合法取值内")
        # `inject` 是**可选**字段（旧的夹具/旧适配器不带它）：带了就必须是登记值。
        inject = str(ch.get("inject") or "")
        if inject and inject not in INJECT_VALUES:
            problems.append(f"{did}: inject={inject!r} 不在合法取值内")
        if inject == "applied" and not ch.get("content_id"):
            problems.append(f"{did}: inject=applied 却没有 content_id（定位不到是哪份内容）")
        hops = ch.get("chain") or []
        if len(hops) < 3:
            problems.append(f"{did}: 链路只有 {len(hops)} 跳（至少应有 客户端 → 适配器 → … ）")
        for hop in hops:
            keys = ("label", "value", "request", "response", "why")
            missing = [k for k in keys if not hop.get(k)]
            if missing:
                problems.append(f"{did}: 第 {hop.get('label', '?')} 跳缺 {missing}")
    return {"chains": len(chains), "problems": problems}


def check_l4(console: str) -> dict[str, Any]:
    """核对 L4 结论：存在性 + `AR-12`（结论引用的证据必须真实存在于遥测里）。

    只读核对，不触发分析：结论由近线 worker 周期性产出。
    """
    conclusions = fetch_json("/api/analysis?limit=200", console)
    if not isinstance(conclusions, list):
        raise ConsoleError("/api/analysis 期望列表")
    flows = fetch_flows(console)
    known_ids = {str(row.get("decision_id") or "") for row in flows if row.get("decision_id")}
    dangling: list[str] = []
    accepted = 0
    for item in conclusions:
        if not item.get("accepted"):
            continue
        accepted += 1
        for evidence in item.get("evidence_ids") or []:
            if str(evidence) not in known_ids:
                dangling.append(f"{item.get('kind')} 引用了遥测里不存在的证据 {evidence}")
    return {
        "conclusions": len(conclusions),
        "accepted": accepted,
        "known_decisions": len(known_ids),
        "dangling_evidence": dangling,
    }


def render(
    results: list[dict[str, Any]],
    l4: dict[str, Any] | None,
    graph: dict[str, Any] | None = None,
) -> None:
    width = max((len(row["id"]) for row in results), default=12)
    group_width = max((len(row["group"]) for row in results), default=6)
    print(
        f"{'场景':<{width}}  {'分组':<{group_width}}  {'HTTP':>4}  {'分数':>5}  {'决策':<12} 信号"
    )
    print("-" * (width + group_width + 46))
    for row in results:
        score = f"{row['score']:.2f}" if "score" in row else "  — "
        mark = (
            "缺口"
            if row["gap"]
            else ("观察" if row["observe_only"] else ("✓" if not row["failures"] else "✗"))
        )
        print(
            f"{row['id']:<{width}}  {row['group']:<{group_width}}  "
            f"{row['http_status']:>4}  {score:>5}  "
            f"{row.get('action', '—'):<12} {','.join(row.get('signals', [])) or '—'}  {mark}"
        )
        if row.get("observe_note"):
            print(f"{'':<{width}}    ↳ {row['observe_note']}")
        for failure in row["failures"]:
            print(f"{'':<{width}}    ↳ {failure}")
        for finding in row["findings"]:
            print(f"{'':<{width}}    ↳ 响应头卫生：{finding}")
        for finding in row["st7_findings"]:
            print(f"{'':<{width}}    ↳ {finding}")

    asserted = [row for row in results if not row["observe_only"] and not row["gap"]]
    failed = [row for row in asserted if row["failures"]]
    gaps = [row for row in results if row["gap"]]
    hygiene = [row for row in results if row["findings"] or row["st7_findings"]]

    print()
    print(
        f"断言 {len(asserted) - len(failed)}/{len(asserted)} 通过 · 观察 "
        f"{len([r for r in results if r['observe_only']])} 条 · 缺口 {len(gaps)} 条 · "
        f"出口卫生问题 {len(hygiene)} 条"
    )
    if failed:
        print("失败场景：" + ", ".join(row["id"] for row in failed))

    if gaps:
        print("\n── 已知缺口（不算失败；这是「设计有、当前没做到」的清单）──")
        for row in gaps:
            gap = row["gap"]
            evidence = row.get("score")
            shown = (
                "未产出判定"
                if evidence is None
                else f"当前分数 {evidence:g}、信号 {row.get('signals', [])}"
            )
            print(
                f"  · [{gap.get('kind', '?')}] {row['id']}（{row['group']}）：{gap.get('why', '')}"
            )
            print(f"      实测：{shown}")
            print(f"      怎么关：{gap.get('close', '')}")

    if graph is not None:
        print("\n── 逐请求链路核对（DAG）──")
        if graph["chains"] == 0:
            print("  · 还没有链路（先造点流量）")
        else:
            print(f"  · 链路 {graph['chains']} 条；落点取值与每步三段均已核对")
            for issue in graph["problems"]:
                print(f"  ✗ {issue}")

    if l4 is not None:
        print("\n── L4 结论核对（AR-12：引用必须真实存在）──")
        if l4["conclusions"] == 0:
            print("  · 还没有结论（近线 worker 默认 20 秒一轮：稍后重跑 --check-l4 即可）")
        else:
            print(
                f"  · 结论 {l4['conclusions']} 条（接受 {l4['accepted']}）· "
                f"遥测里已知判定 {l4['known_decisions']} 个"
            )
            for issue in l4["dangling_evidence"]:
                print(f"  ✗ {issue}")


def make_nonce(attempt: int) -> str:
    """每次请求的唯一值：查询串（拟真）用。

    它**参与本地判定缓存键**（2026-09-22 起）：带 nonce 的两次请求会各自重新判定，
    但 `decision_id` 不受它影响（那按 ST-10 派生）—— 想复现"同输入命中缓存"用 `--no-nonce`。
    """
    return f"{secrets.token_hex(4)}{attempt:02d}"


def make_session(cookie_name: str, attempt: int, *, shared: bool) -> str:
    """会话 cookie。

    `decision_id` 按 (来源, 会话, 路径, 时间窗) 派生（ST-10）—— 换会话才换判定身份；
    要"同身份再判一次"靠 nonce（它进本地缓存键），要"命中缓存"就用 --no-nonce。
    `shared=True` 时同一场景内**复用**同一会话（用于验证 ST-10 的复用行为）。
    """
    suffix = "shared" if shared else f"{attempt:02d}"
    return f"{cookie_name}={secrets.token_hex(8)}{suffix}"


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
        help="会话 cookie 名（默认 sid，见 session.cookie_name）",
    )
    parser.add_argument(
        "--check-l4",
        action="store_true",
        help="跑完顺带核对 L4 结论与证据引用（AR-12）",
    )
    parser.add_argument(
        "--check-graph",
        action="store_true",
        help="跑完核对逐请求链路（DAG）：落点取值合法 + 每步三段齐全（请求/响应/为什么）",
    )
    parser.add_argument("--json", action="store_true", help="输出 JSON（自动化用）")
    parser.add_argument(
        "--report",
        default="",
        help="把完整报告（JSON）写到该路径（自动化/留档用；建议落在临时目录）",
    )
    parser.add_argument(
        "--explain",
        action="store_true",
        help="失败/缺口场景额外打印该请求的判定原文与响应头（定位用）",
    )
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

    # AI 层开没开，由**核心自己的快照**说了算（不猜）。
    # 为什么需要它：声明 `stack: takeover-ai` 的场景在「接管但 AI 关」的栈里，
    # 落点是对的（mirage）而注入结果是 `disabled` —— 那是**形态没打开**，不是被测行为不达标。
    # 形态的**权威信号**：控制台聚合视图里的 `shadow`（`/api/topology` 的 Graph.Shadow）。
    # 为什么不用逐请求链路里的字段：`/api/graphs` 的行里**没有** shadow（实测；
    # 它是聚合视图才有的字段）—— 拿不到就会退化成「凭结果形状猜形态」，那正是要避免的。
    stack_form = "unknown"  # shadow / takeover / unknown
    try:
        topo = fetch_json("/api/topology", args.console) or {}
        if isinstance(topo.get("shadow"), bool):
            stack_form = "shadow" if topo["shadow"] else "takeover"
    except (ConsoleError, AttributeError):
        stack_form = "unknown"

    ai_state = "unknown"  # on / off / unknown（三态：读不到 ≠ 关着）
    try:
        snapshot = fetch_json("/api/config", args.console) or {}
        ai_state = "on" if (snapshot.get("ai") or {}).get("enabled") else "off"
    except (ConsoleError, AttributeError):
        # 读不到就**没有结论**：既不当作「关着」去降级成观察（那会把真失败放行），
        # 也不当作「开着」去报红（那是无证据的控告）—— 交给 judge 报「证据缺失」。
        ai_state = "unknown"

    results: list[dict[str, Any]] = []
    # 热身（K-24）：第一条请求常落在「冷连接 + 适配器还没 Pull 到策略」的窗口里，
    # 结果是 failopen（放行到业务、没有判定记录）—— 那是**通道时序**，不是被测行为。
    # 不热身的话，只跑单条场景时第一条几乎必然假红（实测：takeover-mirage-routed 超时）。
    warm_host, warm_port, _ = parse_http_url(args.entry)
    warm_url = f"http://{warm_host}:{warm_port}/healthz"
    try:
        # scheme 与主机名已由 parse_http_url 显式校验（只允许 http/https），与 fetch_json 同一做法：
        # nosemgrep: python.lang.security.audit.dynamic-urllib-use-detected
        with urllib.request.urlopen(warm_url, timeout=args.timeout) as response:
            response.read()
        time.sleep(1.0)
    except OSError as exc:
        # 不静默：热身失败本身不判失败，但要让人看见（否则第一条场景的红会让人查错方向）
        print(f"（热身请求失败，不影响判定：{exc}）", file=sys.stderr)

    for scenario in scenarios:
        expect = scenario.get("expect") or {}
        if str(scenario.get("stack") or "") == "takeover-ai":
            if ai_state == "off":
                expect["observe_only"] = True
                scenario["_observe_note"] = (
                    "AI 层未打开（核心快照 ai.enabled=false）⇒ 本场景只能观察；"
                    "打开 AI 层（ai.enabled=true + 清单 + SHEN_PROXY_INJECT_CONTENT=true）后再跑"
                )
            elif ai_state == "unknown":
                scenario["_unknown_ai"] = True
        needs_pair = bool(
            expect.get("same_decision_as_previous") or expect.get("distinct_decisions")
        )
        repeat = max(args.repeat, 2) if needs_pair else args.repeat
        shared_session = str(scenario.get("session", "")) == "shared"
        previous_decision: str | None = None
        # 共享会话必须**在场景级生成一次**：每轮都随机的话，判定键每轮都不同，
        # "复用同一判定"这条断言就永远测不到（第一版正是这个 bug）。
        shared_cookie = make_session(args.session_cookie, 0, shared=True) if shared_session else ""
        for attempt in range(repeat):
            nonce = "" if args.no_nonce else make_nonce(attempt)
            session = shared_cookie or make_session(args.session_cookie, attempt, shared=False)
            try:
                status, headers, body = send(args.entry, scenario, nonce, args.timeout, session)
            except (OSError, ValueError) as exc:
                results.append(
                    {
                        "id": scenario["id"],
                        "group": scenario.get("group", ""),
                        "why": scenario.get("why", ""),
                        "method": scenario["method"].upper(),
                        "path": scenario["path"],
                        "http_status": 0,
                        "observe_only": bool(expect.get("observe_only")),
                        "observe_note": scenario.get("_observe_note", ""),
                        "gap": expect.get("gap"),
                        "flow": None,
                        "findings": [],
                        "st7_findings": [],
                        "body_bytes": 0,
                        "failures": [f"请求没发出去：{exc}"],
                        "attempt": attempt + 1,
                    }
                )
                continue
            flow = wait_for_flow(args.console, scenario, wait=args.wait)
            # 落点 / 注入结果在逐请求链路里，不在判定记录里 —— 按 decision_id 取回并合进 flow
            if flow is not None and any(
                name in (scenario.get("expect") or {}) for name in ("executed", "inject")
            ):
                graph_row = wait_for_graph(
                    args.console, str(flow.get("decision_id") or ""), wait=args.wait
                )
                if graph_row is not None:
                    for name in ("executed", "inject", "status", "content_id", "bytes"):
                        if graph_row.get(name) not in (None, ""):
                            flow[name] = graph_row[name]
            row = judge(
                scenario,
                status,
                headers,
                body,
                flow,
                previous_decision=previous_decision,
                stack_form=stack_form,
            )
            row["attempt"] = attempt + 1
            if flow is not None:
                previous_decision = str(flow.get("decision_id") or "") or previous_decision
            results.append(row)
            if args.delay:
                time.sleep(args.delay)

    graph: dict[str, Any] | None = None
    if args.check_graph:
        try:
            graph = check_graph(args.console)
        except (ConsoleError, ValueError) as exc:
            print(f"链路核对失败：{exc}", file=sys.stderr)
            return 1

    l4: dict[str, Any] | None = None
    if args.check_l4:
        try:
            l4 = check_l4(args.console)
        except (ConsoleError, ValueError) as exc:
            print(f"L4 核对失败：{exc}", file=sys.stderr)
            return 1

    asserted = [r for r in results if not r["observe_only"] and not r["gap"]]
    payload = {
        "entry": args.entry,
        "console": args.console,
        "results": results,
        "l4": l4,
        "summary": {
            "asserted": len(asserted),
            "failed": len([r for r in asserted if r["failures"]]),
            "observed": len([r for r in results if r["observe_only"]]),
            "gaps": len([r for r in results if r["gap"]]),
            "hygiene_findings": len([r for r in results if r["findings"] or r["st7_findings"]]),
        },
    }
    if args.report:
        try:
            pathlib.Path(args.report).write_text(
                json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8"
            )
            print(f"报告已写入：{args.report}")
        except OSError as exc:
            print(f"报告写入失败（不影响结论）：{exc}", file=sys.stderr)

    if args.explain:
        print("\n── 定位线索（失败与缺口场景的判定原文；按 decision_id 去日志里追）──")
        for row in results:
            if not (row["failures"] or row["gap"]):
                continue
            print(f"  · {row['id']}  decision_id={row.get('decision_id', '（无判定）')}")
            if row["flow"]:
                print(f"      控制台原文：{json.dumps(row['flow'], ensure_ascii=False)}")
                print(f"      追一条：scripts/shen.sh logs core | grep {row['decision_id']}")
            else:
                print("      控制台里没有这条判定 —— 先看 proxy 是走了白名单还是没到判定")

    if args.json:
        payload = {
            "entry": args.entry,
            "console": args.console,
            "results": results,
            "l4": l4,
            "graph": graph,
            "summary": {
                "asserted": len(asserted),
                "failed": len([row for row in asserted if row["failures"]]),
                "observed": len([row for row in results if row["observe_only"]]),
                "gaps": len([row for row in results if row["gap"]]),
                "hygiene_findings": len(
                    [row for row in results if row["findings"] or row["st7_findings"]]
                ),
            },
        }
        print(json.dumps(payload, ensure_ascii=False, indent=2))
    else:
        render(results, l4, graph)

    # 观察行不算失败（形态没打开不是被测行为的错；它们的失败已在 judge 里跳过）
    failed = any(row["failures"] for row in results if not row["observe_only"])
    hygiene = any(row["findings"] or row["st7_findings"] for row in results)
    dangling = bool(l4 and l4["dangling_evidence"])
    graph_bad = bool(graph and graph["problems"])
    return 1 if (failed or hygiene or dangling or graph_bad) else 0


if __name__ == "__main__":
    raise SystemExit(main())
