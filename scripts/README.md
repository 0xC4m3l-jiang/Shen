# `scripts/` —— 启停、更新、验证

顶层只放**入口**，检查类工具都在 [`check/`](check/)。先按「有没有 Docker」选入口：

```text
scripts/
├── shen.sh      Docker 栈入口（Linux / macOS / WSL）      ← 只需 Docker
├── shen.ps1     同上，Windows PowerShell 版
├── dev.sh       本机开发栈入口（不用 Docker，直接跑进程）  ← 需 Go 1.26+ / Node 20+ / python3
├── lib.sh       上面几个 shell 脚本共用的小函数（被 source，不单独执行）
└── check/       门禁与验证工具（make gate / make check 调用它们，也可以单独跑）
```

所有临时产物（二进制、日志、截图、控制台数据）都落在 `${TMPDIR:-/tmp}/shen-<uid>/`，**仓库里不留文件**。

## 1. 本机开发栈：`dev.sh`

改了 Go 或前端代码，想马上在浏览器里看效果时用它。起的是：假上游 → 影子代理 → 核心，外加管控台 API、连接器网关、Vite 前端。

| 场景 | 命令 | 做了什么 |
| --- | --- | --- |
| 第一次 / 从头起 | `scripts/dev.sh up` | 编译 4 个 Go 服务 → 按依赖顺序启动并逐个等健康 → 装前端依赖（缺的话）→ 起 Vite |
| 改完 Go 代码 | `scripts/dev.sh update` | 重新编译 → 滚动重启核心 / 代理 / 管控台 / 网关（连接器在跑也一起换）。**编译失败时旧进程不动** |
| 看状态 | `scripts/dev.sh status` | 每个服务的 PID / 端口 / 状态 + 4 个健康检查。「端口被占」= 不是本脚本起的残留进程，`up` 会清掉 |
| 看日志 | `scripts/dev.sh logs proxy 100` | 取尾部，不跟随（名：`core proxy console ui upstream gateway connector`） |
| 造数据 | `scripts/dev.sh seed` | 经业务入口发 3 条请求：1 条高分改道、2 条放行，判定流页有数据可点 |
| 隧道端到端 | `scripts/dev.sh tunnel` | 登录管控台签凭证 → 起连接器 → 用标记文件证明请求真穿了隧道（误回落直连会 404） |
| 停 | `scripts/dev.sh stop` | 先 TERM，5 秒不退再 KILL，端口保证释放 |

前端单独管理（**只做 UI 时用这一组**：不需要代理 / 网关 / 连接器）：

| 场景 | 命令 |
| --- | --- |
| 起前端（顺带起最小后端） | `scripts/dev.sh ui up` —— API 不可达时**自动拉起最小后端**（核心 + 管控台 API） |
| **一命令验证显示** | `scripts/dev.sh ui check`（见 §3）—— 后端 / 前端没起就分别拉起来，然后逐页验证 + 截图 |
| 前端依赖变了 | `scripts/dev.sh ui update`（`npm ci` → 重启 Vite → HTTP 层自检） |
| 类型检查 + 单测 | `scripts/dev.sh ui test`（`vue-tsc` + `vitest`；不需要后端） |
| 停 | `scripts/dev.sh ui stop`（停 Vite，并收掉 `ui up` 自动起的最小后端） |

后端在别处（如 Docker 栈）时：`SHEN_CONSOLE_API_URL=http://127.0.0.1:19444 scripts/dev.sh ui up`。
此时脚本**不会**在本机拉起后端；另外要确认那边的 `SHEN_CONSOLE_ALLOWED_ORIGINS` 含 `http://127.0.0.1:5173`，
否则 `GET` 能过、登录 `POST` 返回 `403 origin_rejected`（很容易误判成前端 bug）。
只要前端、连最小后端也不要：`SHEN_UI_NO_BACKEND=1 scripts/dev.sh ui up`。

改前端源码**不需要**任何命令：Vite 热更新。`update` 发现 `package-lock.json` 比上次安装新时会自动 `npm ci`。

默认端口：上游 9000 · 核心 9443 · 业务入口 18080 · 管控台 API 9445 · 前端 5173 · 网关 9446 / 9447，均可用 `SHEN_LOCAL_*_PORT` / `SHEN_LOCAL_*_ADDR` 覆盖（见 `dev.sh` 头部）。

账号：首次 `admin / admin`，首登强制改密。`tunnel` 和 `ui check` 需要登录时会把它改成 `Local-Stack-2026!` 并记在临时目录的 `console-pass` 里 —— 这只针对这个一次性本地栈。

`up` / `ui up` / `ui check` 结束时会把**验证过确实能用**的账号口令直接打出来（口令被人在界面上改过时会明说「口令未知」并给出重置办法）——
所以你不需要去翻 `console-pass`，也不会有「照着记录输却发现登不进去」这种事。

## 2. Docker 栈：`shen.sh` / `shen.ps1`

| 场景 | 命令 | 说明 |
| --- | --- | --- |
| 起栈 | `scripts/shen.sh up` | 首次自动生成 `.env`；等管控台就绪后打印地址。`--profile honeypot` 只起某模块，`--env dev` 开发形态 |
| 改了代码 / 配置 | `scripts/shen.sh update` | 重建镜像 + **整栈**重建容器（`restart` 是它的别名） |
| 只改了 UI | `scripts/shen.sh update ui` | 只重建 `console-ui`，其他容器不动 |
| 看状态 | `scripts/shen.sh status` | 容器状态 + 管控台 `/api/v1/system/status` |
| 看日志 | `scripts/shen.sh logs core` | 取尾部 80 行，不跟随 |
| **验证页面显示** | `scripts/shen.sh ui-check` | 同 §3，对着 `http://127.0.0.1:19444` |
| 链路冒烟 | `scripts/shen.sh smoke` | 发 3 条流量并回显判定 |
| 完整验证 | `scripts/shen.sh traffic` · `verify` · `doctor` | 伪造流量核对 · 端到端报告 · 接入自检 |
| 停 | `scripts/shen.sh down` | 删容器，数据卷保留 |

Windows 用 `./scripts/shen.ps1 <同名子命令>`（`-Profile` / `-Env` 代替 `--profile` / `--env`）。
常用的几个也有 make 别名：`make start` · `make status` · `make update` · `make ui-check` · `make down`。

**为什么 `update` 必须整栈重建、而 `update ui` 可以只动一个**：所有服务都挂在 `core` 的网络命名空间上。
单独重建 `core` 会让兄弟容器留在旧命名空间（全都 Up 但端口不通，实测踩过）；
反过来单独重建 `console-ui` 只是加入**当前**命名空间，是安全的。

## 3. UI 显示验证：`check/ui.py`

「容器 Up、`/healthz` 200」不等于页面显示出来了 —— 入口脚本 404、运行时异常、路由守卫死循环都会白屏。
`dev.sh ui check` / `shen.sh ui-check` 调的是同一个脚本，分四段给结论：

1. **HTTP**：首页 200 且有挂载点 `#app`；入口脚本能取到；`/healthz` 经 UI 的反代可达。
2. **登录（API）**：账号可用；首登要改密时，只有本地栈会替你改（Docker 栈不动你的账号）。
3. **浏览器（直开）**：用 [agent-browser](https://github.com/vercel-labs/agent-browser) 打开登录页 → 真实填表登录 →
   逐页**整页直开**（等价于刷新到该页 / 深链），核「主区域有内容、没有未捕获异常」。
4. **浏览器（点侧栏）**：逐页**点击左侧导航**（客户端路由 —— 用户真实走的路）。
   这一段是必需的：直开走的是整页加载，**测不到客户端路由**；「点了没反应」这类问题
   （懒加载 chunk 失败 ⇒ vue-router 静默取消导航）只在点击时暴露。

每页截图。点侧栏那段用的是**真·鼠标点击**（按坐标打）而不是 JS 的 `element.click()` —— 后者会绕过命中测试，
「有东西盖住侧栏、真人点不到」时它会给出假绿；所以点击前先做一次命中测试（那一点最上面的是不是这个链接）。
另外要等视图**稳定**再判（外壳用 `<Transition mode="out-in">`，URL 先变、旧组件还在播离场动画）。

```text
== ③a 逐页直开（整页加载 / 深链）==
  ✓ deception      「欺骗层」105 字
== ③b 点击侧栏（客户端路由）==
  ✓ 点击 deception 「欺骗层」105 字
  – 点击 nosuch     未验证：被路由守卫跳到 /overview（无此页，或当前账号无该页权限）
结论：通过 24 · 失败 0 · 未验证 0
截图：$TMPDIR/shen-501/ui-check/20260930-171229
```

`–` 是**未验证**，不算通过，原因写在后面。有 `✗` 才非零退出。
常用参数：`--pages overview,deception`（只看部分页）· `--http-only`（没装浏览器时只做前两层）· `--pass`（口令改过）·
`--viewport 1280x720`（照你自己的窗口大小跑 —— 布局与「点得到点不到」跟窗口尺寸有关）。
装浏览器：`npm i -g agent-browser && agent-browser install`。

## 4. 检查与门禁：`check/`

平时直接用 make，不用记脚本路径：

```sh
make check    # 快速内循环（不下载工具）：构建 · 格式 · vet · 架构 · 追溯 · 泄漏
make gate     # 完整门禁：再加 staticcheck · errcheck · ruff · 许可审计 · go test -race · pytest
make dev      # 开发验证：配置干跑 → 非法配置被拒 → 起核心 → 冒烟 → 规则回放 → L4
```

每个工具做什么、怎么单独跑，见 [`check/README.md`](check/README.md)。

## 5. 写新脚本时的约定

- **入口只放顶层，检查放 `check/`**。不要再在 `scripts/` 下开新的顶层目录。
- shell 脚本 source `lib.sh` 拿 `die` / `ensure_go` / `ensure_node` / `wait_for` / `http_ok`，不要各写一份。
- 后台进程**先 `go build` 再直跑**，不要 `go run`（PID 是包装器的，kill 会留孤儿，实测泄漏过 51 个核心进程）。
- 工具或环境缺失时**失败**，不要静默跳过 —— 静默跳过等于假绿。
- 日志默认取尾部、不跟随；`-f` 会把自动化挂住。
- **停进程后要确认端口真的释放**：`vite` / `npm run dev` 收到 TERM 后会拖一会儿才退出，端口在期间还开着。
  `kill_one` 因此会循环确认到端口释放为止（实测：不确认的话紧接着的 `ui up` / `ui check` 会撞上半死的进程）。
- **健康探测别只探一次**：一个正在退出的进程可能还会应答一次。判断「前端可用」用的是连续两次都通（`ui_healthy`）。
- 仓库不允许 `.js` 源文件（archcheck TB-21）；浏览器注入脚本之类在运行时写进临时目录。
- Python 脚本受 ruff 约束（`make pyfmt-check pylint` 覆盖 `scripts/`），只用标准库。
