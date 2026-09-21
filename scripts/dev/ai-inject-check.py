#!/usr/bin/env python3
"""AI 欺骗内容注入的端到端验收（阶段 A：通路 · 开关 · 强制护栏）。

它起一套最小的本地环境（业务站 + 幻境站 + 核心 + 适配器 + 控制台），逐条验证：

① 关闭态：改道侧与业务侧响应都与「未注入基线」**逐字节一致**
② 打开态：改道侧含注入内容（`<section class="service-detail">`），业务侧**逐字节不变**
③ AR-30：同会话同资源三次 → 响应 sha256 相同；跨会话 → 落在不同变体（分布可测）
④ 关卡：护栏拒绝的内容**零入库**（CLI 退出码 1 + 不写清单）
⑤ 秒级关闭：核心切回 `ai.enabled=false` 后，**不重启适配器**，下一条请求变 `inject=disabled`
⑥ 观测：DAG 上出现「内容注入」跳（三段文字齐全），逐请求事件带 `inject` / `content_id`

用法：python3 scripts/dev/ai-inject-check.py [--keep]
只用标准库（只有「生成清单」那一步调用 analysis/.venv）。
"""

from __future__ import annotations

import argparse
import hashlib
import http.client
import json
import os
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
RUNDIR = Path(tempfile.gettempdir()) / f"shen-ai-check-{os.getuid()}"

BUSINESS_BODY = b"<html><body>REAL-BUSINESS</body></html>"
MIRAGE_BODY = b"<html><body>MIRAGE-BACKEND</body></html>"
RESOURCE = "/api/users"
ELLIPSIS = "\u2026"  # 省略号（用转义写，避开工具层的字符损坏）

failures: list[str] = []
checks: list[str] = []


# ── 断言与记账 ───────────────────────────────────────────────────────────────


def check(name: str, ok: bool, detail: str = "") -> None:
    checks.append(f"  {'✓' if ok else '✗'} {name}：{detail}")
    if not ok:
        failures.append(f"{name}：{detail}")


def free_port() -> int:
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    if not isinstance(port, int):
        raise RuntimeError(f"取不到空闲端口：{port!r}")
    return port


def _http_get(port: int, path: str, headers: dict[str, str]) -> bytes:
    """向本机发一次 GET。

    用 `http.client` 而不是 `urllib.request`：**scheme 在类型层面只能是 http**
    （没有 `file://` 之类的降级空间），而且这里全都是 `127.0.0.1`。
    非 2xx 一律抛错 —— 与原先 `urllib` 的行为一致（它在 4xx/5xx 时抛异常），
    免得「引擎坏了」被静默当成「拿到了一段错误页」。

    已知差异：`http.client` **不跟随 3xx**（`urlopen` 默认跟随）。本脚本访问的端点不重定向，
    但对齐测试里有一条 302 用例把这个差异钉住（想跟随时得自己写循环）。
    """
    conn = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
    try:
        conn.request("GET", path, headers=headers)
        resp = conn.getresponse()
        body = resp.read()
        if resp.status >= 400:
            raise RuntimeError(f"HTTP {resp.status}：{path}")
        return body
    finally:
        conn.close()


def http_get(
    port: int, path: str, cookie: str | None = None, ua: str = "HeadlessChrome/120"
) -> bytes:
    headers = {"User-Agent": ua}
    if cookie:
        headers["Cookie"] = cookie
    return _http_get(port, path, headers)


def http_status(
    port: int, path: str, *, cookie: str | None = None, ua: str = "HeadlessChrome/120"
) -> tuple[int, bytes]:
    """像 `http_get`，但**不把 4xx 当异常** —— 拦截路径要读的就是 403。"""
    headers = {"User-Agent": ua}
    if cookie:
        headers["Cookie"] = cookie
    conn = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
    try:
        conn.request("GET", path, headers=headers)
        resp = conn.getresponse()
        return resp.status, resp.read()
    finally:
        conn.close()


def get_graphs(port: int, limit: int = 200) -> list[dict]:
    try:
        raw = json.loads(_http_get(port, f"/api/graphs?limit={limit}", {}))
    except (OSError, RuntimeError, ValueError):
        return []
    if not isinstance(raw, list):
        return []
    return [item for item in raw if isinstance(item, dict)]


def sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


# ── 进程管理 ─────────────────────────────────────────────────────────────────


class Procs:
    def __init__(self) -> None:
        self.items: list[subprocess.Popen[bytes]] = []

    def start(self, args: list[str], env: dict[str, str], log: Path) -> subprocess.Popen[bytes]:
        handle = log.open("wb")
        proc = subprocess.Popen(args, env={**os.environ, **env}, stdout=handle, stderr=handle)
        self.items.append(proc)
        return proc

    def drop(self, proc: subprocess.Popen[bytes]) -> None:
        proc.send_signal(signal.SIGTERM)
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:  # pragma: no cover - 只在进程卡死时走到
            proc.kill()
        if proc in self.items:
            self.items.remove(proc)

    def stop_all(self) -> None:
        for proc in list(self.items):
            if proc.poll() is None:
                proc.send_signal(signal.SIGTERM)
        for proc in list(self.items):
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:  # pragma: no cover
                proc.kill()
        self.items.clear()


def wait_for(port: int, timeout: float = 20.0) -> bool:
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                return True
        except OSError:
            time.sleep(0.1)
    return False


# ── 站点与配置 ───────────────────────────────────────────────────────────────


def write_site(path: Path, body: bytes) -> None:
    path.write_text(
        "import sys\n"
        # ThreadingHTTPServer：单线程 HTTPServer 会在 keep-alive 连接上阻塞 accept，
        # 于是「上游新连接」要等到 5s dial 超时（实测踩过）—— 验收脚本不该被这个坑带偏。
        "from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer\n"
        f"BODY = {body!r}\n"
        "class H(BaseHTTPRequestHandler):\n"
        "    protocol_version = 'HTTP/1.1'\n"
        "    def do_GET(self):\n"
        "        self.send_response(200)\n"
        "        self.send_header('Content-Type', 'text/html')\n"
        "        self.send_header('Content-Length', str(len(BODY)))\n"
        "        self.end_headers()\n"
        "        self.wfile.write(BODY)\n"
        "    def log_message(self, *a):\n"
        "        pass\n"
        "ThreadingHTTPServer(('127.0.0.1', int(sys.argv[1])), H).serve_forever()\n",
        encoding="utf-8",
    )


CONFIG_TEMPLATE = """core:
  listen: "127.0.0.1:{core}"
shadow: false
session:
  cookie_name: "sid"
thresholds:
  route_mirage: 0.70
  block: {block_threshold}
guard:
  false_route_budget: 0.001
store:
  driver: "memory"
  redis: {{addr: "", password: ""}}
  clickhouse: {{addr: "", database: ""}}
  postgres: {{dsn: ""}}
policy:
  policy_id: "ai-check"
  version: {version}
  gray_pct: 100
rules:
  - id: "ua-headless"
    weight: {rule_weight}
    match: {{field: "user_agent", op: "contains", value: "HeadlessChrome"}}
whitelist:
  source_cidrs: []
  user_agents: []
  path_prefixes: []
honeypots:
  - name: "mirage"
    type: "web-clone"
    addr: "http://127.0.0.1:{mirage}"
    enabled: true
"""

AI_SECTION = """ai:
  enabled: {enabled}
  kinds: ["content"]
  model: ""
  manifest: "{manifest}"
  content:
    variants: 8
    rotate_cooldown: "30m"
"""


def start_stack(
    procs: Procs,
    ports: dict[str, int],
    config: Path,
    *,
    inject_content: bool,
    block_enabled: bool = False,
    tag: str = "core",
) -> tuple[subprocess.Popen[bytes], subprocess.Popen[bytes]]:
    """起「核心 + 适配器 + 控制台」这一段可重启的栈；两个站点**不在**其中（它们全程不重启）。

    返回（核心, 适配器）两个句柄 —— 阶段⑤ 要单独换掉核心、并断言适配器**没被重启**。
    """
    core_proc = procs.start(
        [str(RUNDIR / "core")],
        {
            "SHEN_CONFIG": str(config),
            "SHEN_LISTEN": f"127.0.0.1:{ports['core']}",
            # block 默认关（INT-12 阶梯放开）；覆盖验证时才显式打开
            "SHEN_BLOCK_ENABLED": "true" if block_enabled else "false",
        },
        RUNDIR / f"core-{tag}.log",  # 每阶段一个名字：同路径覆盖会让「哪个阶段的日志」说不清
    )
    if not wait_for(ports["core"]):
        raise RuntimeError("核心没起来")
    proxy_proc = procs.start(
        [str(RUNDIR / "proxy")],
        {
            "SHEN_PROXY_UPSTREAM": f"http://127.0.0.1:{ports['business']}",
            "SHEN_CORE_ADDR": f"127.0.0.1:{ports['core']}",
            "SHEN_PROXY_LISTEN": f"127.0.0.1:{ports['proxy']}",
            "SHEN_PROXY_SHADOW": "false",
            "SHEN_PROXY_POLICY_INTERVAL": "1s",
            "SHEN_PROXY_INJECT_CONTENT": "true" if inject_content else "false",
            "SHEN_PROXY_LOG_REQUESTS": "1",
        },
        RUNDIR / ("proxy-on.log" if inject_content else "proxy-off.log"),
    )
    if not wait_for(ports["proxy"]):
        raise RuntimeError("适配器没起来")
    procs.start(
        [str(RUNDIR / "console")],
        {
            "SHEN_CORE_ADDR": f"127.0.0.1:{ports['core']}",
            "SHEN_CONSOLE_LISTEN": f"127.0.0.1:{ports['console']}",
        },
        RUNDIR / "console.log",
    )
    if not wait_for(ports["console"]):
        raise RuntimeError("控制台没起来")
    time.sleep(1.5)  # 让适配器完成第一次 Pull（否则第一条请求落在「还没策略」的窗口里）
    return core_proc, proxy_proc


def wait_graph(port: int, predicate) -> list[dict]:
    """等控制台出现满足条件的链路（遥测是异步的，最多等 5 秒）。"""
    deadline = time.time() + 5
    graphs: list[dict] = []
    while time.time() < deadline:
        graphs = get_graphs(port)
        if any(predicate(graph) for graph in graphs):
            return graphs
        time.sleep(0.3)
    return graphs


# ── 主流程 ───────────────────────────────────────────────────────────────────


def generate_manifest(
    out: Path, *, identifiers: str = "", llm: bool = False
) -> subprocess.CompletedProcess[str]:
    args = [
        str(ROOT / "analysis/.venv/bin/python"),
        "-m",
        "analysis.aicap",
        "--out",
        str(out),
        "--profile",
        "site-a",
        "--resources",
        f"/,{RESOURCE}",
        "--variants",
        "8",
        "--version",
        "1",
        "--now",
        "2026-09-20T00:00:00+00:00",
        "--quiet",
    ]
    if identifiers:
        args += ["--identifiers", identifiers]
    if llm:
        # 走模型（需 SHEN_AI_KEY；缺失时 CLI 退出码 2，不静默回落）
        args += ["--llm"]
    return subprocess.run(args, cwd=ROOT, capture_output=True, text=True)


def manifest_facts(path: Path) -> tuple[str, list[str]]:
    """清单的 (generator, 该资源下全部 body)：核「注入的字节是不是清单里那一份」。

    读不了就报一句人话：这是验收脚本，不该因为一个坏 JSON 抛一串 traceback。
    """
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"清单读不了（{path}）：{exc}") from exc
    bodies: list[str] = []
    for entry in data.get("entries", []):
        if entry.get("resource") == RESOURCE:
            bodies = [str(b.get("body", "")) for b in entry.get("bodies", [])]
    return str(data.get("generator", "")), bodies


def dump_dag(ports: dict[str, int], out: Path) -> None:
    """把控制台的 DAG / 拓扑原始 JSON 落盘（报告与图示都从它生成，**不手绘**）。

    为什么落盘而不是直接画图：图是「从原始数据渲染出来的」才算证据 ——
    留下 JSON，别人可以自己重画、也可以对着它核每个节点。
    """
    out.mkdir(parents=True, exist_ok=True)
    written: list[str] = []
    for name, path in (
        ("topology", "/api/topology"),
        ("graphs", "/api/graphs?limit=200"),
        ("flow", "/api/flow?limit=200"),
        ("analysis", "/api/analysis"),
    ):
        try:
            raw = http_get(ports["console"], path)
            (out / f"{name}.json").write_bytes(raw)
            written.append(f"{name}.json({len(raw)}B)")
        except Exception as exc:  # 落盘失败不该让验收变红：它只是证据附件
            written.append(f"{name}.json(失败:{type(exc).__name__})")
    print(f"DAG 原始数据：{out} · {', '.join(written)}")


def maybe_dump(ports: dict[str, int], dag_out: Path | None, name: str) -> None:
    """按阶段落盘 DAG 原始数据（`--dag-out` 才做）——报告里的图从它渲染，不手绘。"""
    if dag_out is None:
        return
    print(f"\n[阶段 {name}] ", end="")
    dump_dag(ports, dag_out / name)


def run_checks(
    ports: dict[str, int],
    procs: Procs,
    *,
    llm: bool = False,
    dag_out: Path | None = None,
) -> None:
    manifest = RUNDIR / "manifest.json"
    generated = generate_manifest(manifest, llm=llm)
    print(f"清单：{generated.stdout.strip()}")
    check("清单生成成功", generated.returncode == 0 and manifest.exists(), generated.stdout.strip())
    generator, bodies = manifest_facts(manifest)
    if llm:
        check(
            "清单由模型产出（generator=model-v1，不是模板）",
            generator == "model-v1",
            f"generator={generator}；该资源 {len(bodies)} 个 body 由 AI 生成",
        )

    blocked = RUNDIR / "blocked.json"
    rejected = generate_manifest(blocked, identifiers="service-detail")
    check(
        "关卡：护栏拒绝全部内容 ⇒ 退出码 1 且**不写清单**",
        rejected.returncode == 1 and not blocked.exists(),
        f"exit={rejected.returncode} 清单存在={blocked.exists()}",
    )

    off_config = RUNDIR / "config-off.yaml"
    off_config.write_text(
        CONFIG_TEMPLATE.format(**ports, version=1, block_threshold=0.95, rule_weight=0.9)
        + AI_SECTION.format(enabled="false", manifest=""),
        encoding="utf-8",
    )
    on_config = RUNDIR / "config-on.yaml"
    on_config.write_text(
        CONFIG_TEMPLATE.format(**ports, version=2, block_threshold=0.95, rule_weight=0.9)
        + AI_SECTION.format(enabled="true", manifest=str(manifest)),
        encoding="utf-8",
    )
    off_again = RUNDIR / "config-off-2.yaml"
    off_again.write_text(
        CONFIG_TEMPLATE.format(**ports, version=3, block_threshold=0.95, rule_weight=0.9)
        + AI_SECTION.format(enabled="false", manifest=""),
        encoding="utf-8",
    )

    # ── ① 默认全关 ──────────────────────────────────────────────────────
    print("\n== ① 关闭态（ai.enabled=false + SHEN_PROXY_INJECT_CONTENT=false）==")
    start_stack(procs, ports, off_config, inject_content=False, tag="stage1")
    baseline_mirage = http_get(ports["mirage"], RESOURCE)
    baseline_business = http_get(ports["business"], RESOURCE)
    # 与阶段② 同样的时序问题：第一条请求可能落在启动窗口里（failopen 到业务侧）。
    # 重试到**确实走了改道侧**为止 —— 断言的「不注入」是在改道侧上比，不是在「有没有到改道侧」上比。
    through_off = b""
    for attempt in range(10):
        candidate = http_get(ports["proxy"], RESOURCE, cookie=f"sid=off-1-{attempt}")
        if b"MIRAGE-BACKEND" in candidate:
            through_off = candidate
            break
        time.sleep(0.5)
    check(
        "关闭态：确实到达改道侧（否则下面的字节比较没有意义）",
        bool(through_off),
        f"{len(through_off)} 字节",
    )
    check(
        "改道侧与「未注入基线」逐字节一致",
        sha(through_off) == sha(baseline_mirage),
        f"经引擎 {sha(through_off)[:16]} vs 幻境直连 {sha(baseline_mirage)[:16]}",
    )
    origin_off = http_get(ports["proxy"], "/healthz", cookie="sid=off-2", ua="Mozilla/5.0")
    check(
        "业务侧与业务基线逐字节一致",
        sha(origin_off) == sha(baseline_business),
        f"经引擎 {sha(origin_off)[:16]} vs 业务直连 {sha(baseline_business)[:16]}",
    )
    graphs = wait_graph(ports["console"], lambda g: g.get("executed") == "mirage")
    disabled = [g for g in graphs if g.get("executed") == "mirage"]
    values = sorted({str(g.get("inject")) for g in disabled})
    check(
        "逐请求事件：改道侧上报 inject=disabled",
        bool(disabled) and all(g.get("inject") == "disabled" for g in disabled),
        f"{len(disabled)} 条改道侧请求，取值 {values}",
    )

    maybe_dump(ports, dag_out, "1-closed")

    # ── ② 打开（核心与适配器各重启一次）─────────────────────────────────
    print("\n== ② 打开态（ai.enabled=true + 清单下发 + SHEN_PROXY_INJECT_CONTENT=true）==")
    procs.stop_all()
    _core_on, proxy_proc = start_stack(procs, ports, on_config, inject_content=True, tag="stage2")
    # 第一条请求可能落在「适配器还没 Pull 到后端表」那个窗口里（同 K-24），
    # 于是它会回落业务侧。这里重试到**确实走了改道侧**：
    # 断言的是注入，不是通道时序。
    # 注意：重试条件只看「到没到改道侧」—— 注入坏了不会被掩盖
    # （那时它到了改道侧，只是没有注入片段）。
    injected = b""
    for attempt in range(10):
        # 每次换一个会话：避开判定缓存（ST-10）把上一次的结果重放回来
        candidate = http_get(ports["proxy"], RESOURCE, cookie=f"sid=on-1-{attempt}")
        if b"MIRAGE-BACKEND" in candidate:
            injected = candidate
            break
        time.sleep(0.5)
    if llm:
        # 模型路的内容体没有固定标记（它是 AI 写的），所以直接核「注入的字节是不是清单里那一份」。
        hit = next((b for b in bodies if b.encode("utf-8") in injected), "")
        # 直接判别式：同一参数再生成一份**模板**清单，断言注入体**不在**模板产物里 ——
        # 这样「注入的是 AI 内容」不依赖 `generator` 字段这一个间接证据。
        tpl_manifest = RUNDIR / "manifest-template.json"
        tpl = generate_manifest(tpl_manifest, llm=False)
        _, tpl_bodies = manifest_facts(tpl_manifest)
        in_template = next((b for b in tpl_bodies if b.encode("utf-8") in injected), "")
        check(
            "注入的内容体逐字节来自清单（即 AI 生成的那一份）",
            bool(hit) and injected.rstrip().endswith(b"</body></html>"),
            f"命中清单 body（{len(hit)} 字符）" if hit else "清单里没有任何 body 出现在响应中",
        )
        tpl_note = "模板清单里也有这段（可疑）" if in_template else "模板清单里没有这一段 ✓"
        check(
            "注入体**不在**模板产物里（与模板清单直接对比）",
            tpl.returncode == 0 and not in_template,
            tpl_note,
        )
    else:
        check(
            "改道侧响应含注入内容",
            b'<section class="service-detail">' in injected
            and injected.rstrip().endswith(b"</body></html>"),
            f"{injected[:70]!r}" + ELLIPSIS,
        )
    check(
        "注入是**插入**：幻境自己的正文仍在",
        b"MIRAGE-BACKEND" in injected,
        f"字节 {len(baseline_mirage)} → {len(injected)}",
    )
    origin_on = http_get(ports["proxy"], "/healthz", cookie="sid=on-2", ua="Mozilla/5.0")
    check(
        "业务侧响应**逐字节不变**（INT-8）",
        sha(origin_on) == sha(baseline_business),
        f"{sha(origin_on)[:16]} == {sha(baseline_business)[:16]}",
    )

    # ── ③ AR-30 与跨会话分布 ───────────────────────────────────────────
    print("\n== ③ AR-30（同会话一致 · 跨会话分布）==")
    same = [http_get(ports["proxy"], RESOURCE, cookie="sid=sticky") for _ in range(3)]
    check(
        "同会话同资源三次 → 响应 sha256 相同",
        len({sha(item) for item in same}) == 1,
        f"sha256={sha(same[0])[:16]}，三次长度 {[len(x) for x in same]}",
    )
    variants = set()
    for i in range(16):
        body = http_get(ports["proxy"], RESOURCE, cookie=f"sid=dist-{i}")
        variants.add(body[body.index(b"<section") : body.index(b"</section>")])
    check(
        "16 个会话落在 ≥4 个不同变体上（多态生效）",
        len(variants) >= 4,
        f"命中 {len(variants)} 个变体（N=8）",
    )
    graphs = wait_graph(ports["console"], lambda g: g.get("inject") == "applied")
    applied = [g for g in graphs if g.get("inject") == "applied"]
    check(
        "逐请求事件：inject=applied 且带 content_id",
        bool(applied) and all(g.get("content_id") for g in applied),
        f"{len(applied)} 条，例：{applied[0].get('content_id') if applied else '—'}",
    )

    # ── ⑥ DAG 注入跳 ───────────────────────────────────────────────────
    print("\n== ⑥ DAG 注入跳 ==")
    hop: dict | None = None
    for graph in applied:
        for node in graph.get("chain") or []:
            if isinstance(node, dict) and node.get("id") == "inject":
                hop = node
    check(
        "DAG 出现「内容注入」跳且三段文字齐全",
        hop is not None
        and all(hop.get(key) for key in ("label", "value", "request", "response", "why")),
        f"{hop.get('label') if hop else '（没有这一跳）'} ⇒ {hop.get('value') if hop else ''}",
    )

    maybe_dump(ports, dag_out, "2-inject")

    # ── ⑤ 秒级关闭（核心切开关；适配器不重启）──────────────────────────
    print("\n== ⑤ 秒级关闭（核心重启 + 适配器不重启）==")
    procs.drop(_core_on)  # 只换核心；适配器保持**同一个进程**
    new_core = procs.start(
        [str(RUNDIR / "core")],
        {"SHEN_CONFIG": str(off_again), "SHEN_LISTEN": f"127.0.0.1:{ports['core']}"},
        RUNDIR / "core-stage3.log",
    )
    check("核心（关闭态 v3）已重启", wait_for(ports["core"]), f"pid={new_core.pid}")
    time.sleep(3.0)  # 等适配器下一次 Pull（SHEN_PROXY_POLICY_INTERVAL=1s）
    check("适配器进程未被重启", proxy_proc.poll() is None, f"pid={proxy_proc.pid}")
    # 换核心后的第一条请求可能因通道重建而 fail-open（K-24 那一类，与注入无关）；
    # 因此重试到「确实走了改道侧」再断言 —— 断言的是**注入开关**，不是通道状态。
    after_off = b""
    for attempt in range(8):
        body = http_get(ports["proxy"], RESOURCE, cookie=f"sid=after-off-{attempt}")
        if b"MIRAGE-BACKEND" in body:
            after_off = body
            break
        time.sleep(0.5)
    actual = sha(after_off)[:16] if after_off else "（没走成改道侧）"
    check(
        "关闭后：改道侧响应回到原样（不再注入）",
        bool(after_off) and sha(after_off) == sha(baseline_mirage),
        f"{actual} == {sha(baseline_mirage)[:16]}",
    )
    if llm:
        check(
            "关闭后：响应体里没有清单里的任何内容体",
            not any(b.encode("utf-8") in after_off for b in bodies),
            f"字节 {len(baseline_mirage)} 到 {len(after_off)}",
        )
    else:
        check(
            "关闭后：响应体里没有注入片段",
            b'<section class="service-detail">' not in after_off,
            f"字节 {len(baseline_mirage)} 到 {len(after_off)}",
        )
    graphs = wait_graph(
        ports["console"], lambda g: g.get("executed") == "mirage" and g.get("inject") == "disabled"
    )
    tail = [g for g in graphs if g.get("executed") == "mirage" and g.get("inject") == "disabled"]
    check(
        "逐请求事件回到 inject=disabled（下发级开关生效）", bool(tail), f"{len(tail)} 条 disabled"
    )

    maybe_dump(ports, dag_out, "3-killed")


def run_block_stage(ports: dict[str, int], procs: Procs, *, dag_out: Path | None = None) -> None:
    """③ 拦截（`block`）—— 三值里的第三个。

    拦截**默认关**（`Q5` · `INT-12` 阶梯放开：先影子 → 只对高置信误导 → 再放开拦截），
    所以这里是**显式打开的覆盖验证**：`SHEN_BLOCK_ENABLED=true` + 把 `block` 阈值降到 0.5，
    让那条 0.9 分的请求落到拦截侧。断言三件事：403 · `executed=block` · **拦截侧不注入**。
    """
    print("\n== ③ 模拟高分请求 \u2192 拦截\uff08SHEN_BLOCK_ENABLED=true\uff09==")
    procs.stop_all()
    cfg = RUNDIR / "config-block.yaml"
    cfg.write_text(
        # 阈值必须满足 route_mirage <= block（校验会拒乱序），所以**不改阈值、改权重**：
        # 规则权重 1.0 ⇒ 分值 1.0 ≥ block 0.95 ⇒ 落到拦截侧。
        CONFIG_TEMPLATE.format(**ports, version=4, block_threshold=0.95, rule_weight=1.0)
        + AI_SECTION.format(enabled="false", manifest=""),
        encoding="utf-8",
    )
    start_stack(procs, ports, cfg, inject_content=False, block_enabled=True, tag="block")
    status, body = http_status(ports["proxy"], RESOURCE, cookie="sid=block-1")
    check(
        "\u62e6\u622a\u8def\u5f84\uff1a403\uff08\u5bf9\u624b\u53ef\u89c1\u7684\u5904\u7f6e\uff09",
        status == 403,
        f"status={status} body={body[:40]!r}",
    )
    graphs = wait_graph(ports["console"], lambda g: g.get("executed") == "block")
    blocked = [g for g in graphs if g.get("executed") == "block"]
    check(
        "\u9010\u8bf7\u6c42\u4e8b\u4ef6\uff1aexecuted=block \u4e14 action=block",
        bool(blocked) and all(g.get("action") == "block" for g in blocked),
        f"{len(blocked)} \u6761\u62e6\u622a\u8bf7\u6c42",
    )
    check(
        "\u62e6\u622a\u4fa7\u4e0d\u6ce8\u5165\uff08inject=off\uff09",
        bool(blocked) and all(g.get("inject") == "off" for g in blocked),
        f"\u53d6\u503c {sorted({str(g.get('inject')) for g in blocked})}",
    )
    maybe_dump(ports, dag_out, "4-block")


def cleanup_dir() -> None:
    """删临时目录；删不掉只提醒，不影响验收结论。"""
    try:
        shutil.rmtree(RUNDIR, ignore_errors=True)
    except OSError as exc:  # pragma: no cover - ignore_errors 之下几乎不可达
        print(f"（临时目录未清干净：{exc}）")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--keep", action="store_true", help="跑完保留临时目录（排查用）")
    parser.add_argument(
        "--llm",
        action="store_true",
        help="清单用**模型**生成（需 SHEN_AI_KEY）；默认用确定性模板生成器",
    )
    parser.add_argument(
        "--block",
        action="store_true",
        help="额外跑一段**拦截（block）**覆盖验证（默认关；INT-12 阶梯放开）",
    )
    parser.add_argument(
        "--dag-out",
        default="",
        help="把 DAG/拓扑原始 JSON 写到这个目录（拟报告与图示用）",
    )
    args = parser.parse_args()

    ports = {name: free_port() for name in ("core", "proxy", "console", "business", "mirage")}
    if RUNDIR.exists():
        cleanup_dir()
    RUNDIR.mkdir(parents=True, exist_ok=True)
    print(f"临时目录：{RUNDIR}（端口 {ports}）")

    print("构建（core / proxy / console）…")
    for binary, pkg in (
        ("core", "./common/core/cmd/core"),
        ("proxy", "./modules/deception/proxy/cmd/proxy"),
        ("console", "./modules/console/cmd/console"),
    ):
        subprocess.run(
            ["go", "build", "-o", str(RUNDIR / binary), pkg],
            cwd=ROOT,
            check=True,
            capture_output=True,
        )

    write_site(RUNDIR / "business.py", BUSINESS_BODY)
    write_site(RUNDIR / "mirage.py", MIRAGE_BODY)

    sites, stack = Procs(), Procs()
    error = ""
    try:
        sites.start(
            [sys.executable, str(RUNDIR / "business.py"), str(ports["business"])],
            {},
            RUNDIR / "business.log",
        )
        sites.start(
            [sys.executable, str(RUNDIR / "mirage.py"), str(ports["mirage"])],
            {},
            RUNDIR / "mirage.log",
        )
        if not wait_for(ports["business"]) or not wait_for(ports["mirage"]):
            check("站点就绪", False, "业务站或幻境站没起来")
        else:
            dag_out = Path(args.dag_out) if args.dag_out else None
            run_checks(ports, stack, llm=args.llm, dag_out=dag_out)
            if args.block:
                run_block_stage(ports, stack, dag_out=dag_out)
    except Exception as exc:
        error = f"{type(exc).__name__}: {exc}"
    finally:
        stack.stop_all()
        sites.stop_all()

    print("\n──────────────── 验收结果 ────────────────")
    for line in checks:
        print(line)
    if error:
        print(f"  ✗ 异常中断：{error}")
        failures.append(error)
    if args.keep:
        print(f"\n临时目录保留：{RUNDIR}")
    else:
        cleanup_dir()
    if failures:
        print(f"\n✗ 失败 {len(failures)} 项：")
        for item in failures:
            print(f"   · {item}")
        return 1
    print(f"\n✅ 全部通过（{len(checks)} 项）")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
