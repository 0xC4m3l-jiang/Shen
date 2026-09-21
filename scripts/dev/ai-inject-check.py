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
  block: 0.95
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
    weight: 0.9
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
    procs: Procs, ports: dict[str, int], config: Path, *, inject_content: bool
) -> tuple[subprocess.Popen[bytes], subprocess.Popen[bytes]]:
    """起「核心 + 适配器 + 控制台」这一段可重启的栈；两个站点**不在**其中（它们全程不重启）。

    返回（核心, 适配器）两个句柄 —— 阶段⑤ 要单独换掉核心、并断言适配器**没被重启**。
    """
    core_proc = procs.start(
        [str(RUNDIR / "core")],
        {"SHEN_CONFIG": str(config), "SHEN_LISTEN": f"127.0.0.1:{ports['core']}"},
        RUNDIR / "core.log",
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


def generate_manifest(out: Path, *, identifiers: str = "") -> subprocess.CompletedProcess[str]:
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
    return subprocess.run(args, cwd=ROOT, capture_output=True, text=True)


def run_checks(ports: dict[str, int], procs: Procs) -> None:
    manifest = RUNDIR / "manifest.json"
    generated = generate_manifest(manifest)
    print(f"清单：{generated.stdout.strip()}")
    check("清单生成成功", generated.returncode == 0 and manifest.exists(), generated.stdout.strip())

    blocked = RUNDIR / "blocked.json"
    rejected = generate_manifest(blocked, identifiers="service-detail")
    check(
        "关卡：护栏拒绝全部内容 ⇒ 退出码 1 且**不写清单**",
        rejected.returncode == 1 and not blocked.exists(),
        f"exit={rejected.returncode} 清单存在={blocked.exists()}",
    )

    off_config = RUNDIR / "config-off.yaml"
    off_config.write_text(
        CONFIG_TEMPLATE.format(**ports, version=1)
        + AI_SECTION.format(enabled="false", manifest=""),
        encoding="utf-8",
    )
    on_config = RUNDIR / "config-on.yaml"
    on_config.write_text(
        CONFIG_TEMPLATE.format(**ports, version=2)
        + AI_SECTION.format(enabled="true", manifest=str(manifest)),
        encoding="utf-8",
    )
    off_again = RUNDIR / "config-off-2.yaml"
    off_again.write_text(
        CONFIG_TEMPLATE.format(**ports, version=3)
        + AI_SECTION.format(enabled="false", manifest=""),
        encoding="utf-8",
    )

    # ── ① 默认全关 ──────────────────────────────────────────────────────
    print("\n== ① 关闭态（ai.enabled=false + SHEN_PROXY_INJECT_CONTENT=false）==")
    start_stack(procs, ports, off_config, inject_content=False)
    baseline_mirage = http_get(ports["mirage"], RESOURCE)
    baseline_business = http_get(ports["business"], RESOURCE)
    through_off = http_get(ports["proxy"], RESOURCE, cookie="sid=off-1")
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

    # ── ② 打开（核心与适配器各重启一次）─────────────────────────────────
    print("\n== ② 打开态（ai.enabled=true + 清单下发 + SHEN_PROXY_INJECT_CONTENT=true）==")
    procs.stop_all()
    _core_on, proxy_proc = start_stack(procs, ports, on_config, inject_content=True)
    injected = http_get(ports["proxy"], RESOURCE, cookie="sid=on-1")
    check(
        "改道侧响应含注入内容",
        b'<section class="service-detail">' in injected
        and injected.rstrip().endswith(b"</body></html>"),
        f"{injected[:70]!r}…",
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

    # ── ⑤ 秒级关闭（核心切开关；适配器不重启）──────────────────────────
    print("\n== ⑤ 秒级关闭（核心重启 + 适配器不重启）==")
    procs.drop(_core_on)  # 只换核心；适配器保持**同一个进程**
    new_core = procs.start(
        [str(RUNDIR / "core")],
        {"SHEN_CONFIG": str(off_again), "SHEN_LISTEN": f"127.0.0.1:{ports['core']}"},
        RUNDIR / "core-off.log",
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
    check(
        "关闭后：响应体里没有注入片段",
        b'<section class="service-detail">' not in after_off,
        f"字节 {len(baseline_mirage)} → {len(after_off)}",
    )
    graphs = wait_graph(
        ports["console"], lambda g: g.get("executed") == "mirage" and g.get("inject") == "disabled"
    )
    tail = [g for g in graphs if g.get("executed") == "mirage" and g.get("inject") == "disabled"]
    check(
        "逐请求事件回到 inject=disabled（下发级开关生效）", bool(tail), f"{len(tail)} 条 disabled"
    )


def cleanup_dir() -> None:
    """删临时目录；删不掉只提醒，不影响验收结论。"""
    try:
        shutil.rmtree(RUNDIR, ignore_errors=True)
    except OSError as exc:  # pragma: no cover - ignore_errors 之下几乎不可达
        print(f"（临时目录未清干净：{exc}）")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--keep", action="store_true", help="跑完保留临时目录（排查用）")
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
            run_checks(ports, stack)
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
