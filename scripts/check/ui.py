#!/usr/bin/env python3
"""管控台 UI 显示验证：HTTP 层 → 浏览器渲染 → 登录 → 逐页截图。

它回答的是「页面**真的显示出来了**吗」，不是「接口通不通」：
容器 Up、/healthz 200 时页面照样可能白屏（入口脚本 404、运行时异常、路由守卫死循环）。

三层，逐层给结论：
  ① HTTP：首页 200 且含挂载点 `#app`；入口脚本可取；`/healthz` 经 UI 的反代可达（API 在线）
  ② 登录（走 API）：账号可用；首登强制改密时，`--change-pass` 才会替你改（只给一次性本地栈用）
  ③ 浏览器（agent-browser）：登录页渲染 → 表单登录，然后**两条路各走一遍**：
     a. 逐页**直开**（整页加载 / 深链，等价于刷新到该页）
     b. 逐页**点侧栏**（客户端路由 —— 用户真实走的路；「点了没反应」这类问题只在点击时暴露）
     两条都核「主区域有内容、页面没跳走、无未捕获异常」并截图

结论状态：✓ 通过 · ✗ 失败 · – 未验证（写明原因；**不算通过**，也不伪装成通过）。
退出码：有 ✗ 即 1；只有 ✓ / – 为 0。

用法：
    python3 scripts/check/ui.py --url http://127.0.0.1:5173     # 本机栈 = dev.sh ui check
    python3 scripts/check/ui.py --url http://127.0.0.1:19444    # Docker 栈 = shen.sh ui-check
    python3 scripts/check/ui.py --url ... --pages overview,deception   # 只看部分页面
    python3 scripts/check/ui.py --url ... --http-only                  # 没有浏览器时只做 ① ②

账号：--user / --pass，缺省取 SHEN_CONSOLE_USER / SHEN_CONSOLE_PASS，再缺省 admin / admin。
截图：默认落在 ${TMPDIR:-/tmp}/shen-<uid>/ui-check/<时间戳>/，仓库里不留文件。
只用标准库；浏览器层依赖 agent-browser CLI（`npm i -g agent-browser && agent-browser install`）。
"""

from __future__ import annotations

import argparse
import http.cookiejar
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
import unicodedata
import urllib.error
import urllib.request
from pathlib import Path

# 侧栏里的全部页面（与 modules/console/ui/src/router/index.ts 对齐）。
# 当前账号无权限的页会被路由守卫跳走，记为「–」。
DEFAULT_PAGES = [
    "overview",
    "services",
    "connectors",
    "deception",
    "honeypot",
    "config",
    "alerts",
    "analysis",
    "system",
]

# 注入到每个页面的错误收集器：未捕获异常 / 未处理的 Promise 拒绝 / console.error。
# 以字符串形式在运行时写进临时文件 —— 仓库不允许 .js 源文件（archcheck TB-21 语言层数）。
INIT_SCRIPT = """(() => {
  const e = (window.__shenErrs = []);
  addEventListener('error', (ev) => e.push('page: ' + (ev.message || ev.type)));
  addEventListener('unhandledrejection', (ev) => e.push('promise: ' + String(ev.reason)));
  const ce = console.error.bind(console);
  console.error = (...a) => { e.push('console: ' + a.map(String).join(' ')); ce(...a); };
})();"""

NAV_LINKS = (
    'JSON.stringify([...document.querySelectorAll("nav a")].map((a) => a.getAttribute("href")))'
)

# 命中测试：在「真人会点的那一点」问浏览器——最上面的是不是这个链接？
# 脚本的 element.click() 会**绕过命中测试**（被遮住也照样触发），所以只用它测，
# 会在「有东西盖住侧栏、真人点不到」的情况下给出假绿。这条要跟真·鼠标点击配对使用。
HIT_TEST = """(() => {{
  const a = document.querySelector('nav a[href="{href}"]')
  if (!a) return 'NO-ITEM'
  const r = a.getBoundingClientRect()
  if (r.width === 0 || r.height === 0) return 'ZERO-SIZE'
  const el = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2)
  if (!el) return 'NOTHING-AT-POINT'
  if (el.closest('nav a') === a) return 'ok'
  return 'COVERED-BY ' + el.tagName + '.' + String(el.className).slice(0, 80)
}})()"""


PROBE = """JSON.stringify({
  path: location.pathname,
  title: document.title,
  app: (document.querySelector('#app') || {}).childElementCount || 0,
  main: (() => {
    const m = document.querySelector('main');
    return m ? m.innerText.trim().length : -1;
  })(),
  heading: ((document.querySelector('main h1, main h2, h1') || {}).innerText || '')
    .trim().slice(0, 40),
  form: !!document.querySelector('form'),
  errs: window.__shenErrs || [],
})"""


def pad(text: str, width: int) -> str:
    """按显示宽度补空格（中文占两格），让结论列对齐。"""
    shown = sum(2 if unicodedata.east_asian_width(c) in "WF" else 1 for c in text)
    return text + " " * max(0, width - shown)


class Report:
    def __init__(self) -> None:
        self.rows: list[tuple[str, str, str]] = []

    def add(self, mark: str, item: str, detail: str = "") -> None:
        self.rows.append((mark, item, detail))
        print(f"  {mark} {pad(item, 14)} {detail}", flush=True)

    def ok(self, item: str, detail: str = "") -> None:
        self.add("✓", item, detail)

    def fail(self, item: str, detail: str = "") -> None:
        self.add("✗", item, detail)

    def skip(self, item: str, detail: str) -> None:
        self.add("–", item, detail)

    def count(self, mark: str) -> int:
        return sum(1 for r in self.rows if r[0] == mark)


# ── ① HTTP 层 ──────────────────────────────────────────────────────────────


def http_get(url: str, timeout: float = 5) -> tuple[int, str]:
    try:
        with urllib.request.urlopen(url, timeout=timeout) as resp:
            return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as exc:
        return exc.code, ""
    except (urllib.error.URLError, OSError) as exc:
        return 0, str(exc)


def check_http(base: str, rep: Report) -> bool:
    code, html = http_get(base + "/")
    if code != 200 or 'id="app"' not in html:
        rep.fail("首页", f"HTTP {code or '不可达'} {'' if code else html}".strip())
        return False
    rep.ok("首页", "HTTP 200，含挂载点 #app")

    # 开发态 Vite 先注入 /@vite/client；真正的入口在后面（/src/main.ts 或 /assets/*.js）。
    srcs = re.findall(r'<script[^>]+type="module"[^>]+src="([^"]+)"', html)
    srcs = [x for x in srcs if not x.startswith("/@vite/")] or srcs
    if not srcs:
        rep.fail("入口脚本", "首页里找不到 <script type=module src=…>")
        return False
    src = srcs[-1]
    code, body = http_get(base + src if src.startswith("/") else f"{base}/{src}")
    if code == 200 and body:
        rep.ok("入口脚本", f"{src}（{len(body)} 字节）")
    else:
        rep.fail("入口脚本", f"{src} → HTTP {code}（前端没构建好或反代路径不对）")
        return False

    code, _ = http_get(base + "/healthz")
    if code == 200:
        rep.ok("API 可达", "/healthz 经 UI 反代 200")
    else:
        rep.fail(
            "API 可达",
            f"/healthz → HTTP {code or '不可达'}（管控台 API 没起或反代目标不对）",
        )
        return False
    return True


# ── ② 登录（API）─────────────────────────────────────────────────────────────


def api_post(
    opener: urllib.request.OpenerDirector, url: str, body: dict, csrf: str = ""
) -> tuple[int, dict]:
    req = urllib.request.Request(url, data=json.dumps(body).encode(), method="POST")
    req.add_header("Content-Type", "application/json")
    if csrf:
        req.add_header("X-CSRF-Token", csrf)
    try:
        with opener.open(req, timeout=10) as resp:
            return resp.status, json.loads(resp.read() or b"{}")
    except urllib.error.HTTPError as exc:
        try:
            return exc.code, json.loads(exc.read() or b"{}")
        except ValueError:
            return exc.code, {}


def save_pass(path: str, password: str) -> None:
    f = Path(path)
    f.write_text(password + "\n", encoding="utf-8")
    f.chmod(0o600)


def must_change_of(session: dict) -> bool:
    user = session.get("user") or {}
    return bool(user.get("must_change") or session.get("must_change"))


def check_login(base: str, args: argparse.Namespace, rep: Report) -> tuple[str, bool] | None:
    """返回（可用口令, 是否仍需改密）；登录不了返回 None。"""
    opener = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
    )
    login = f"{base}/api/v1/auth/login"
    password = args.password
    code, sess = api_post(opener, login, {"username": args.user, "password": password})
    if code == 401 and args.change_pass and password != args.change_pass:
        # 一次性本地栈可能已被改过密（口令文件丢了）：再试一次 --change-pass 的口令。
        code, sess = api_post(opener, login, {"username": args.user, "password": args.change_pass})
        if code == 200:
            password = args.change_pass
            print("  （给定口令不对，已用 --change-pass 的口令登录：这个本地栈之前改过密）")
            if args.pass_file:
                save_pass(args.pass_file, password)
    if code != 200:
        msg = sess.get("error") or sess.get("message") or ""
        rep.fail(
            "登录（API）",
            f"HTTP {code} {msg}（口令改过？用 --pass 或 SHEN_CONSOLE_PASS 指定）".strip(),
        )
        return None
    if must_change_of(sess) and args.change_pass:
        code, _ = api_post(
            opener,
            f"{base}/api/v1/auth/password",
            {"old_password": password, "new_password": args.change_pass},
            csrf=str(sess.get("csrf_token", "")),
        )
        if code >= 300:
            rep.fail("首登改密", f"HTTP {code}（新口令不满足口令策略？）")
            return None
        password = args.change_pass
        if args.pass_file:
            save_pass(args.pass_file, password)
        code, sess = api_post(opener, login, {"username": args.user, "password": password})
        if code != 200:
            rep.fail("登录（API）", f"改密后重新登录失败：HTTP {code}")
            return None
        rep.ok("首登改密", "已按 --change-pass 改掉默认口令（仅限一次性本地栈）")
    must = must_change_of(sess)
    rep.ok("登录（API）", f"账号 {args.user} 可用" + ("（首登须改密）" if must else ""))
    return password, must


# ── ③ 浏览器层 ───────────────────────────────────────────────────────────────


def find_agent_browser() -> str:
    found = shutil.which("agent-browser")
    if found:
        return found
    versions = Path.home() / ".workbuddy/binaries/node/versions"
    for cand in sorted(versions.glob("*/bin/agent-browser"), reverse=True):
        if os.access(cand, os.X_OK):
            return str(cand)
    return ""


class Browser:
    def __init__(self, exe: str, session: str, init_script: str) -> None:
        self.exe = exe
        self.base = [exe, "--session", session]
        self.init_script = init_script
        self.started = False
        # agent-browser 会拉起 node 子进程：把它所在目录放进 PATH，免得用到终端里另一个坏掉的 node。
        path = f"{Path(exe).parent}{os.pathsep}{os.environ.get('PATH', '')}"
        self.env = dict(os.environ, PATH=path)

    def run(self, *argv: str, timeout: float = 60) -> tuple[bool, dict]:
        cmd = [*self.base, "--json", *argv]
        if not self.started:
            cmd[1:1] = ["--init-script", self.init_script]
            self.started = True
        try:
            out = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                timeout=timeout,
                env=self.env,
                check=False,
            )
        except subprocess.TimeoutExpired:
            return False, {"error": f"agent-browser {argv[0]} 超时（{timeout:.0f}s）"}
        try:
            data = json.loads(out.stdout.strip().splitlines()[-1]) if out.stdout.strip() else {}
        except ValueError:
            data = {"error": (out.stdout + out.stderr).strip()[-300:]}
        return bool(data.get("success")) and out.returncode == 0, data

    def eval_json(self, expr: str):
        """跑一段 JS 并解析返回值：JSON 串 / 数组 / 对象 → Python 值；裸标量（如 'ok'）原样返回。

        注意别假设「返回值一定是 JSON 文本」：agent-browser 会把 `JSON.stringify(...)` 的**字符串**
        原样给回来（需要再 loads 一次），而 `return 'ok'` 这类标量直接就是 'ok' —— 早先只做前者，
        于是 `'ok'` 解析失败被当成 None，点击阶段全报「找不到侧栏项」。
        """
        ok, data = self.run("eval", expr)
        if not ok:
            return None
        raw = (data.get("data") or {}).get("result")
        if raw is None or isinstance(raw, (list, dict, bool, int, float)):
            return raw
        try:
            return json.loads(raw)
        except ValueError:
            return raw

    def probe(self) -> dict:
        state = self.eval_json(PROBE)
        return state if isinstance(state, dict) else {"error": "probe 结果不是 JSON"}

    def wait_until(self, cond, limit: float = 10) -> dict:
        deadline = time.monotonic() + limit
        state: dict = {}
        while time.monotonic() < deadline:
            state = self.probe()
            if "error" not in state and cond(state):
                return state
            time.sleep(0.5)
        return state

    def close(self) -> None:
        subprocess.run(
            [*self.base, "close"],
            capture_output=True,
            timeout=30,
            env=self.env,
            check=False,
        )


def new_errors(state: dict, seen: int) -> tuple[list[str], int]:
    errs = [e for e in state.get("errs", []) if not e.startswith("console: ")]
    return errs[seen:], len(errs)


def judge_page(rep: Report, label: str, page: str, st: dict, errs: list[str]) -> None:
    """对一个页面的观测下结论：路径是否如期 · 主区域有没有内容 · 有没有未捕获异常。"""
    path = st.get("path") or ""
    if path and path.strip("/") != page:
        rep.skip(label, f"未验证：被路由守卫跳到 {path}（无此页，或当前账号无该页权限）")
    elif st.get("main", -1) <= 20:
        why = f"；异常：{errs[0]}" if errs else ""
        rep.fail(label, f"主区域空白（main 文本 {st.get('main')} 字）{why}")
    elif errs:
        rep.fail(label, f"渲染了但有未捕获异常：{errs[0][:120]}")
    else:
        rep.ok(label, f"「{st.get('heading') or st.get('title', '')}」{st.get('main')} 字")


def check_pages_by_url(
    br: Browser, base: str, args: argparse.Namespace, out: Path, rep: Report, seen: int
) -> None:
    """逐页**整页直开**：验证深链 / 刷新后直接落到某页（生产里用户收藏的 URL 就是这种）。"""
    for page in args.pages:
        ok, data = br.run("open", f"{base}/{page}", timeout=60)
        if not ok:
            rep.fail(page, f"打开失败：{data.get('error')}")
            continue
        st = br.wait_until(lambda s: s.get("main", -1) > 20, limit=args.page_timeout)
        errs, seen = new_errors(st, seen)
        br.run("screenshot", str(out / f"{page}.png"))
        judge_page(rep, page, page, st, errs)


def wait_settled(br: Browser, want: str, limit: float) -> dict:
    """等客户端路由**真正落地**：路径已是目标页，且主区域连续两次探测内容相同。

    为什么不能只看「路径变了 + 主区域有字」：外壳用 `<Transition mode="out-in">`，
    URL 先变、旧组件还在播离场动画（0.28s）—— 此时读到的是**上一页的内容**，
    结论会整排错位一格（实测踩过：点 overview 报「系统」）。
    """
    deadline = time.monotonic() + limit
    state = br.probe()
    prev: tuple | None = None
    while time.monotonic() < deadline:
        state = br.probe()
        settled = (state.get("path") or "").strip("/") == want and state.get("main", -1) > 20
        key = (state.get("heading"), state.get("main"))
        if settled and key == prev:
            return state
        prev = key if settled else None
        time.sleep(0.35)
    return state


def check_pages_by_click(br: Browser, args: argparse.Namespace, out: Path, rep: Report) -> None:
    """**点击侧栏**逐页切换：这是用户真实走的路，也是直开测不到的那一半。

    页面直开是整页加载，客户端路由完全没被走过 —— 「点了没反应」这类问题
    （懒加载 chunk 失败 ⇒ vue-router 静默取消导航）只会在点击时暴露。
    """
    links = br.eval_json(NAV_LINKS)
    if not links:
        rep.fail(
            "侧栏点击",
            "侧栏没有任何导航项（nav a 为空）：菜单没渲染，或当前账号没有任何页面权限",
        )
        return
    seen = 0
    for href in links:
        name = (href or "").strip("/") or "overview"
        if name not in args.pages:
            continue
        before = br.probe().get("path")
        # ① 先问浏览器：真人会点的那一点，最上面的是不是这个链接？（脚本 click() 会绕过这一步）
        hit = br.eval_json(HIT_TEST.format(href=href))
        if hit == "NO-ITEM":
            rep.fail(f"点击 {name}", f"侧栏里找不到 {href}")
            continue
        if hit != "ok":
            rep.fail(f"点击 {name}", f"侧栏这一项真人点不到：{hit}")
            continue
        # ② 再用**真·鼠标点击**（按坐标打，跟人手一样），不是 JS 的 element.click()
        ok, data = br.run("click", f'nav a[href="{href}"]')
        if not ok:
            rep.fail(f"点击 {name}", f"鼠标点击没打成：{data.get('error')}")
            continue
        st = wait_settled(br, name, args.page_timeout)
        errs, seen = new_errors(st, seen)
        if (st.get("path") or "") == before:
            br.run("screenshot", str(out / f"nav-{name}.png"))
            why = f"；异常：{errs[0][:160]}" if errs else "（浏览器控制台可能有 chunk 加载错误）"
            rep.fail(f"点击 {name}", f"点击后没有跳转，仍停在 {before}{why}")
            continue
        br.run("screenshot", str(out / f"nav-{name}.png"))
        judge_page(rep, f"点击 {name}", name, st, errs)


def check_browser(
    base: str,
    args: argparse.Namespace,
    password: str,
    must_change: bool,
    out: Path,
    rep: Report,
) -> None:
    exe = find_agent_browser()
    if not exe:
        rep.fail(
            "浏览器",
            "找不到 agent-browser（npm i -g agent-browser && agent-browser install；"
            "或加 --http-only）",
        )
        return
    tmp = tempfile.mkdtemp(prefix="shen-ui-check-")
    init = Path(tmp) / "collect-errors.js"
    init.write_text(INIT_SCRIPT, encoding="utf-8")
    br = Browser(exe, f"shen-ui-check-{os.getpid()}", str(init))
    try:
        ok, data = br.run("open", f"{base}/login", timeout=90)
        if not ok:
            rep.fail("浏览器", f"打开登录页失败：{data.get('error')}")
            return
        br.run("set", "viewport", str(args.viewport[0]), str(args.viewport[1]))

        # 登录页：挂载点有内容、表单在、无未捕获异常
        st = br.wait_until(lambda s: s.get("app", 0) > 0 and s.get("form"), limit=15)
        errs, seen = new_errors(st, 0)
        br.run("screenshot", str(out / "login.png"))
        if st.get("app", 0) > 0 and st.get("form") and not errs:
            rep.ok("登录页", f"「{st.get('title', '')}」渲染完成")
        else:
            why = f"；异常：{errs[0]}" if errs else ""
            rep.fail(
                "登录页",
                f"白屏或缺表单（app={st.get('app')} form={st.get('form')}）{why}",
            )
            return

        # 表单登录（走真实 UI，而不是塞 cookie）
        br.run("fill", 'input[autocomplete="username"]', args.user)
        br.run("fill", 'input[autocomplete="current-password"]', password)
        br.run("click", 'button[type="submit"]')
        st = br.wait_until(lambda s: s.get("path") not in ("/login", None), limit=15)
        if st.get("path") in ("/login", None):
            br.run("screenshot", str(out / "login-failed.png"))
            rep.fail("表单登录", "提交后仍停在 /login（见 login-failed.png）")
            return
        rep.ok("表单登录", f"跳转到 {st.get('path')}")

        if must_change:
            st = br.wait_until(lambda s: s.get("form"), limit=10)
            br.run("screenshot", str(out / "password.png"))
            if st.get("path") == "/password" and st.get("form"):
                rep.ok("改密页", "首登强制改密页渲染完成")
            else:
                rep.fail("改密页", f"应停在 /password，实际 {st.get('path')}")
            for page in args.pages:
                rep.skip(page, "未验证：首登须改密，其余页面被路由守卫拦住（改密后重跑）")
            return

        print("== ③a 逐页直开（整页加载 / 深链）==")
        check_pages_by_url(br, base, args, out, rep, seen)
        print("== ③b 点击侧栏（客户端路由）==")
        check_pages_by_click(br, args, out, rep)
    finally:
        br.close()
        shutil.rmtree(tmp, ignore_errors=True)


def main() -> int:
    ap = argparse.ArgumentParser(prog="scripts/check/ui.py", description="管控台 UI 显示验证")
    ap.add_argument(
        "--url",
        required=True,
        help="UI 地址，如 http://127.0.0.1:5173（Vite）或 :19444（Docker）",
    )
    ap.add_argument("--user", default=os.environ.get("SHEN_CONSOLE_USER", "admin"))
    ap.add_argument("--pass", dest="password", default=os.environ.get("SHEN_CONSOLE_PASS", "admin"))
    ap.add_argument("--pass-file", help="口令文件：存在则优先读取；--change-pass 改密后写回")
    ap.add_argument("--change-pass", help="首登强制改密时改成这个口令（只给一次性本地栈用）")
    ap.add_argument("--pages", default=",".join(DEFAULT_PAGES), help="逗号分隔的页面路由名")
    ap.add_argument("--page-timeout", type=float, default=12, help="每页等主区域出内容的秒数")
    ap.add_argument("--out", help="截图目录（默认临时目录）")
    ap.add_argument("--viewport", default="1440x900", help="浏览器视口 WxH（默认 1440x900）")
    ap.add_argument(
        "--http-only",
        action="store_true",
        help="只做 HTTP 与 API 登录两层（无浏览器环境）",
    )
    args = ap.parse_args()
    args.pages = [p.strip() for p in args.pages.split(",") if p.strip()]
    args.viewport = (
        [int(v) for v in args.viewport.lower().split("x")]
        if "x" in args.viewport.lower()
        else [1440, 900]
    )
    if args.pass_file and Path(args.pass_file).is_file():
        args.password = Path(args.pass_file).read_text(encoding="utf-8").strip() or args.password

    base = args.url.rstrip("/")
    stamp = time.strftime("%Y%m%d-%H%M%S")
    out = Path(args.out or Path(tempfile.gettempdir()) / f"shen-{os.getuid()}" / "ui-check" / stamp)
    out.mkdir(parents=True, exist_ok=True)

    rep = Report()
    print(f"UI 显示验证：{base}")
    print("== ① HTTP ==")
    if check_http(base, rep):
        print("== ② 登录（API）==")
        got = check_login(base, args, rep)
        if got:
            print("== ③ 浏览器渲染 ==")
            if args.http_only:
                rep.skip("浏览器渲染", "未验证：--http-only")
            else:
                check_browser(base, args, got[0], got[1], out, rep)

    fails, skips = rep.count("✗"), rep.count("–")
    print()
    print(f"结论：通过 {rep.count('✓')} · 失败 {fails} · 未验证 {skips}")
    if any(out.iterdir()):
        print(f"截图：{out}")
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
