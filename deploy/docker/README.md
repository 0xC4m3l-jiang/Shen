# 在 Docker 里跑起整套（推荐入口）

前置：**只需要 Docker**（Compose 随 Docker Desktop / Docker Engine 一起装；要求 **Compose v2.20+**，
用到 `depends_on.required` 与 profiles。旧的 `docker-compose` v1 已停止维护，不保证可用）。
本机不需要 Go、Node、Python。

## 一键启动与按模块启动

```sh
cp .env.example .env          # Windows PowerShell：Copy-Item .env.example .env
docker compose up -d --build  # 仓库根执行；.env 决定文件列表与 profiles
docker compose ps             # 状态
docker compose logs -f core   # 日志（换服务名看其它）
docker compose down           # 停掉
```

按模块单独启动（profiles；`core` 没有 profile，永远随行 —— 它拥有共享网络命名空间）：

```sh
COMPOSE_PROFILES= docker compose --profile frontend up -d   # 管控台：console-ui + console-api
COMPOSE_PROFILES= docker compose --profile honeypot up -d   # 蜜罐：honeypot-web
COMPOSE_PROFILES= docker compose --profile backend  up -d   # 后端：console-api + proxy + analysis
COMPOSE_PROFILES= docker compose --profile demo     up -d   # 演示假业务站
```

⚠️ `.env` 里的 `COMPOSE_PROFILES` 会与命令行 `--profile` **叠加**，所以上面每条都先临时清空它。
也可以用 `make`（`make up`、`make up-frontend`、`make up-honeypot`、`make up-dev`、`make up-prod`、
`make docker-ps`、`make docker-log S=core`、`make down`）；Windows 无 make 时用 `scripts/shen.ps1`。

> ⚠️ core 被重建时，共享其命名空间的容器必须一起重建：对 core 执行 `up --force-recreate` 后，
> 请对整个栈再执行一次 `docker compose up -d --force-recreate`。

启动后：

| 地址 | 是什么 |
| --- | --- |
| http://127.0.0.1:19444/ | **管控台** —— 登录（本地账号 + 四角色）· 总览 · 欺骗层 · 蜜罐层 · 反向链接器 · 分析 · 告警 · 系统 |
| http://127.0.0.1:18080/ | **业务入口**（经引擎；默认影子模式：只观测、不处置，`INT-11`） |

首次登录：默认账号/口令均为 **admin / admin**（便于人工验证；首登强制改密）。
想用随机初始口令：`.env` 里设 `SHEN_CONSOLE_BOOTSTRAP_PASSWORD=random`，口令写在卷内
`/data/bootstrap-admin.txt`（`docker compose exec console-api cat /data/bootstrap-admin.txt`）。
生产叠加强制经 secrets 注入，拿不到默认值。
自动化脚本用只读令牌 `SHEN_CONSOLE_API_TOKEN`（Bearer，viewer 权限，仅限回环/私有网段来源，禁写）。

造点流量再看管控台：

```sh
curl -s -A "HeadlessChrome/120" http://127.0.0.1:18080/.git/config   # 探针：应得高分
curl -s -A "Mozilla/5.0"        http://127.0.0.1:18080/               # 正常浏览器：应得低分
```

## 起了哪些容器

| 服务 | profile | 是什么 | 监听（共享命名空间内） | 宿主端口 |
| --- | --- | --- | --- | --- |
| `core` | 无（永远随行） | 核心：判定与响应生成的**唯一**实现（`AR-2`）；**拥有本栈的网络命名空间** | gRPC 127.0.0.1:9443（不发布） | — |
| `console-api` | frontend + backend | 管控台 API（`/api/v1`，带鉴权） | 127.0.0.1:9445 | 不发布（经 console-ui 同源访问；开发叠加发布 19445） |
| `console-ui` | frontend | 管控台前端（nginx 静态资源 + `/api/` 同源反代） | 0.0.0.0:9444 | `${SHEN_CONSOLE_PORT:-19444}` |
| `proxy` | backend | L1 适配器：反向代理前置（接入形态③） | 0.0.0.0:8080 | `${SHEN_HTTP_PORT:-18080}` |
| `analysis` | backend | L4 近线分析 worker | 无 | — |
| `honeypot-web` | honeypot | 合成 Web 管理台场景包（只经诱饵路由到达） | 127.0.0.1:19090 | 不发布（开发叠加可放开） |
| `business` | demo | 演示用假业务站（**不是**产品的一部分） | 127.0.0.1:19080 | — |

## 文件划分：base + override

| 文件 | 用途 |
| --- | --- |
| `compose.yaml`（仓库根） | base：全部服务、profiles、健康检查、卷 |
| `deploy/docker/compose.dev.yaml` | 开发：Vite 热更新容器（挂载 `modules/console/ui` 源码）、发布调试端口 19445/5173、蜜罐直连口 |
| `deploy/docker/compose.prod.yaml` | 生产：镜像 tag（`${SHEN_IMAGE_PREFIX:?}/${SHEN_VERSION:?}`）、secrets 文件、只读根文件系统 + tmpfs、资源限制、日志轮转、无 demo、强制影子首发 |
| `deploy/docker/compose.verify-mirage.yaml` | 验证档：非影子、登记改道与诱饵路由（见根 README §5.2） |

叠加文件的路径一律**相对仓库根**书写（Compose 以第一个文件所在目录为项目目录）：

```sh
docker compose -f compose.yaml -f deploy/docker/compose.dev.yaml   up -d --build    # 开发
docker compose -f compose.yaml -f deploy/docker/compose.prod.yaml  up -d --no-build # 生产
docker compose -f compose.yaml -f deploy/docker/compose.verify-mirage.yaml up -d --build
```

仓库根 `.env`（从 `.env.example` 复制，不入库）用 `COMPOSE_FILE` / `COMPOSE_PATH_SEPARATOR=;`
/ `COMPOSE_PROFILES` 声明默认值，因此日常**零参数** `docker compose up -d --build` 即可。

## 环境变量（优先级：shell > `.env` > compose 默认值）

| 变量 | 默认 | 作用 |
| --- | --- | --- |
| `COMPOSE_PROFILES` | `frontend,backend,honeypot,analysis,demo` | 一键启动全部；只写一个值就是单模块 |
| `SHEN_HTTP_PORT` / `SHEN_CONSOLE_PORT` | 18080 / 19444 | 业务入口与管控台的宿主端口 |
| `SHEN_BIND_ADDR` | 127.0.0.1 | 宿主发布绑定地址；对外服务时改 `0.0.0.0` 并自备 TLS（`ADR-0019`） |
| `SHEN_PROXY_UPSTREAM` | `http://127.0.0.1:19080` | 真实业务地址（或 `host.docker.internal:端口`） |
| `SHEN_PROXY_SHADOW` | true | 影子模式；**生产首次上线必须保持 true**（`INT-11`） |
| `SHEN_CONSOLE_ALLOWED_ORIGINS` | 本机 19444 两个来源 | 浏览器 Origin 白名单（写请求还要过 CSRF 与 Sec-Fetch-Site 校验） |
| `SHEN_CONSOLE_COOKIE_SECURE` | false（生产 true） | 会话 Cookie 的 Secure 属性 |
| `SHEN_CONSOLE_BOOTSTRAP_PASSWORD` | admin（验证缺省） | 初始管理员口令：默认 admin/admin 便于验证（首登强制改密）；`random` = 随机生成写 `/data/bootstrap-admin.txt`；生产用 `*_FILE`/secrets 强制注入 |
| `SHEN_CONSOLE_API_TOKEN` | 空 = 不启用 | 只读自动化令牌（`demo/traffic/send.py` 等脚本用） |
| `SHEN_WEB_SCENARIO` / `SHEN_WEB_SCENARIOS_FILE` | contoso / 示例场景包 | 蜜罐场景 |
| `SHEN_IMAGE_PREFIX` / `SHEN_VERSION` | shen / dev | 镜像命名；生产叠加要求**显式**设置 |

## 数据卷与挂载

| 卷 / 挂载 | 位置 | 内容 |
| --- | --- | --- |
| 命名卷 `console-data` | console-api `/data` | 账号（argon2id 哈希）、反向链接器登记、审计 JSONL、用户偏好 —— 原子写，跨平台无权限问题 |
| 只读绑定 | core `/etc/shen/config.yaml` | 核心配置（`SHEN_CORE_CONFIG` 可换） |
| 只读绑定 | honeypot-web `/etc/shen/scenarios.json` | 蜜罐场景包（`SHEN_WEB_SCENARIOS_FILE` 可换） |
| 开发叠加 | console-ui `/ui`（源码）+ 匿名卷 `/ui/node_modules` | Vite 热更新；bind 挂载下自动开启轮询监听（WSL2/macOS 兼容） |

Windows / macOS **不要**把 `/data` 改成绑定挂载（权限与性能问题）；可写状态一律走命名卷。

## 开发与生产差异

| 项 | 开发（dev 叠加） | 生产（prod 叠加） |
| --- | --- | --- |
| 前端 | Vite 热更新容器（5173） | nginx 提供静态文件（9444） |
| 调试端口 | 发布 19445（API 直连）、5173（Vite） | 不发布，只暴露 9444 |
| 密钥 | `.env` 明文（仅限本机） | `secrets` 文件 + `*_FILE`，必填缺失即报错 |
| 影子模式 | 可关（验证档） | 首次上线强制 true |
| 容器加固 | 无 | 只读根文件系统 + tmpfs、CPU/内存限制、日志轮转、无 demo |
| Cookie | Secure 可关 | 强制 Secure；TLS 由客户侧 L0 终结（`ADR-0019`） |

## 跨系统兼容

| 系统 | 注意点 |
| --- | --- |
| Linux | `extra_hosts: host.docker.internal:host-gateway` 已写在 core 上（共享命名空间的服务不能各自设置） |
| macOS / Windows | Docker Desktop 自带 `host.docker.internal`；Apple Silicon 基础镜像均多架构 |
| Windows | 建议仓库放在 WSL2 文件系统；无 make 用 `scripts/shen.ps1`；`.gitattributes` 已强制脚本/YAML 的 LF，避免 CRLF 破坏容器内脚本 |

## 为什么除 core 外都 `network_mode: service:core`

核心的判定面是**明文 gRPC**，而设计规则只允许它监听**回环地址**
（`assertPlaintextListenIsLocal` 兜底；跨节点部署必须换 mTLS）。

让其它容器**加入 core 的网络命名空间**后，它们与核心共享同一个回环 —— 「同机部署」这个前提
在容器里依然成立，规则不必放宽。端口发布写在 `core` 上，发布的就是这个共享命名空间里的监听。

> 想让引擎直接对外服务（而不是只绑本机）？把 `compose.yaml` 里的
> `127.0.0.1:${SHEN_HTTP_PORT:-18080}:8080` 改成 `8080:8080`，并自行在其前面加一层客户侧 L0
> （TLS 终结默认交给 L0，见 `ADR-0019`）。

> 历史注记：旧版控制台「无鉴权、绑定非回环前必须先解决鉴权」的限制**已解除** —— `/api/v1`
> 全量鉴权（会话 + CSRF + Origin + RBAC，SSE 实时流同样先过认证，有未登录 401 测试）。
> 仍存的约束是**单实例**：会话在内存、状态文件单写者，console-api 不能水平扩展；
> 对外部署用单副本 + L0 TLS 即可，多副本需先外置会话存储。

## 密钥管理与文档脱敏规范

密钥**永远不进版本库**。全链路设计：

| 层 | 机制 |
| --- | --- |
| 应用读取 | DeepSeek 等模型密钥走环境变量 `SHEN_AI_KEY`（`analysis/aicap/model.py`），管控台密钥支持 `*_FILE`（Compose secrets / K8s Secret 挂载）；代码与配置样例里只有变量名 |
| 落盘隔离 | `.env` 已被 `.gitignore` 挡住；容器可写状态只在 `console-data` 卷（存 argon2id 哈希，不存明文口令） |
| 日志纪律 | 密钥不进日志/异常/返回值（`ST-20`，`analysis/tests/test_llm_deepseek.py` 钉住） |
| 入库门禁 | `make secrets-check`（已并入 `make gate`）：拦截 `sk-` 真值与密钥类变量的非占位赋值进入版本库 |

**文档里如何引用密钥（脱敏格式）**：写占位符或脱敏形态 —— `sk-****<末4位>`（如 `sk-****a1b2`）、`<YOUR_API_KEY>`、`${SHEN_AI_KEY}`；**绝不**出现完整值。已验证真实的 key 在任何文档/截图/issue 里都只允许脱敏形态。

**泄漏处置 SOP**（`secrets-check` 报错时同样提示）：
1. 视为已泄漏：立即去服务商控制台**吊销并签发新 key**（从仓库删掉文件不解决已发生的泄漏）；
2. 用占位符或环境变量引用替换该值后重新提交；
3. 若已推送远端：轮换密钥后评估 `git filter-repo` 清历史（需负责人审批）。

## 跨模块通信

| 链路 | 协议 | 说明 |
| --- | --- | --- |
| 浏览器 → console-ui → console-api | HTTP 同源（REST + SSE） | 会话 Cookie（HttpOnly）+ CSRF 头；不输出 CORS 允许头 |
| console-api → core | 明文 gRPC，仅回环 | 只读：遥测拉取与 `WatchEvents` 实时流 |
| proxy → core | gRPC | 判定（预算 ≤3ms，失败放行）、策略 Pull/Ack、异步遥测上报 |
| proxy → business / honeypot-web | HTTP | 评分改道失败回落业务；专属诱饵失败固定 502，绝不回源 |
| analysis → core | gRPC | 按游标增量读取，结论回写 |
| honeypot-web → core | 暂无 | 合成交互事件只进本地日志（README §6.2 #4），页面如实标注「未接入」 |

## 换成你自己的业务

1. `compose.yaml` 里把 `proxy` 的 `SHEN_PROXY_UPSTREAM` 指向你的服务地址；
2. 不启用 `demo` profile（假业务站只是演示用）；
3. 把 core 挂载的配置换成你的：`SHEN_CORE_CONFIG` 指向的 YAML（模板见 [`../config/config.example.yaml`](../config/config.example.yaml)）；
4. 在管控台「反向链接器」登记你的服务（域名 / 上游 / 负责人），即可按服务观测流量与流入蜃楼比例。

## 排障顺序

`docker compose ps`（健康检查状态）→ `docker compose logs core`（策略是否下发）→
`docker compose logs proxy`（改道 / 回落）→ `docker compose logs console-api`（鉴权 / 读面）→
管控台顶栏实时指示灯（「重连中」= 核心流中断）与页面错误提示。

常见问题：

- **端口被占用**：`SHEN_HTTP_PORT=8080 SHEN_CONSOLE_PORT=9444 make up`（或改 `.env`）。
- **core 重启后其它容器 502/连接失败**：共享命名空间随 core 重建，整栈 `up -d --force-recreate`。
- **`--profile` 报服务不存在**：`.env` 的 `COMPOSE_PROFILES` 在叠加，先临时清空（见上文）。
- **管控台 401 循环**：会话在内存，console-api 重启后需重新登录（会话令牌从不落盘，属设计取舍）。
- **Vite 热更新不生效**（bind 挂载）：dev 叠加已开轮询；确认没改坏 `CHOKIDAR_USEPOLLING`。
