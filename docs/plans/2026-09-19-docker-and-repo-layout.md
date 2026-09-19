# 变更包：Docker 一键起全套 + 目录工整化（Python 环境归层、依赖离线化）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 让**任何有 Docker 的机器**一条命令跑起整套；同时把「某一层的私有工具」从仓库根挪回它所属的层，根目录只留必需项 |
| 日期 | 2026-09-19 |
| 状态 | 已验证（5 个容器全部 Up · 容器内真流量 → 控制台可见分值/信号 · L4 结论落入控制台 · `make gate` 通过） |
| 改动分级 | **L**（部署形态 + 仓库布局 + 依赖策略 + 门禁） |
| 涉及范围 | `deploy/docker/`（新增）· `analysis/`（配置内收）· `vendor/`（新增）· `Makefile` · `scripts/`（archcheck · licensecheck）· `.gitignore` · 文档 |
| 决策数 | 已答 3 项（容器网络 = 共享核心命名空间 · 依赖 vendor 入库 · 宿主端口可配）/ 待定 2 项 |
| 关联 | `ST-1`（顶层目录白名单）· `ST-20`（忽略清单）· `TB-15`（Python 门禁）· `NI-1` · [ADR-0019](../background/decisions/0019-tls-termination-belongs-to-l0.md) · [ADR-0021](../background/decisions/0021-l4-python-toolchain.md) · [ADR-0022](../background/decisions/0022-l4-near-line-worker.md) |

---

## 1. 需求与验收

**用户要什么**：① Python 相关的配置文件**不要放仓库根**（那是 L4 的能力，不是全局）；设计一个合理的目录结构；
② 项目整体能**在 Docker 里一条命令启动**，对本地环境依赖最少，别人能直接跑；③ README 与文档保持**准确客观**；
④ 少一些 `.` 开头的隐藏文件、文件架构工整。

**验收判据**：

1. 仓库根只剩必需项：`AGENTS.md` `Makefile` `README.md` `go.mod` `go.sum` + 源码平面 + `.git`/`.gitignore`/`.pi`；**没有** Python/前端/Tool 的配置文件与虚拟环境。
2. `make up` 在有 Docker 的机器上一条命令起全套（核心 + 代理 + 控制台 + L4 + 演示业务站），打印两个可访问地址。
3. 容器内**端到端**：经引擎发流量 → 控制台能看到分值/命中信号 → L4 结论出现在「分析结论」块。
4. 构建**不依赖 Go 代理**（本机访问不了 `proxy.golang.org`）：依赖 vendor 入库，`licensecheck` 改从 vendor 读许可证。
5. 忽略清单准确：源码不被忽略，且新增自检 `make check-ignore` 能**真的**发现「已入库文件被忽略」（正反两向验证）。
6. 文档同步：README · `docs/integrate/quickstart.md` · `docs/design/structure.md` · `docs/kb/dev-workflow.md` · `docs/progress.md` · `deploy/docker/README.md`。

**不做什么**：不改产品行为与判定逻辑 · 不做 mTLS（跨节点仍是设计约束，容器里靠共享命名空间保持"同机"）· 不引入前端工具链。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | Python 配置放哪 | **收进 `analysis/`**（pyproject · requirements · `.venv` · proto 桩 · 生成脚本） | Python 是 L4 的实现选择（`TB-2`/`TB-20`），其私有工具不该占用仓库根 |
| ② | 容器之间怎么通信 | **除核心外都 `network_mode: service:core`**（共享核心的网络命名空间，端口发布写在核心上） | 核心判定面是明文 gRPC 且规则只允许回环（`assertPlaintextListenIsLocal`）；共享命名空间 ⇒「同机」前提在容器里仍成立，规则不必放宽 |
| ③ | Go 依赖怎么办 | **vendor 入库**（63MB / 5661 文件），Dockerfile 去掉 `go mod download` | 本机访问不了 Go 代理；vendor 让构建与门禁**离线可用**，换机器/换人都能起 |
| ④ | 宿主端口 | **可配**，默认 `18080`（业务入口）/ `19444`（控制台） | 本机 8080/9444 常被其它项目占用；容器内端口不变 |
| ⑤ | 生成物导入 | 生成后补 `__init__.py` 并把导入改写成 `analysis.proto.telemetry.v1` | 顶层名 `telemetry` 会与本层 `analysis/telemetry.py` 撞名（曾在容器里真炸） |
| ⑥ | 忽略清单可靠性 | 新增 `make check-ignore`，**必须用 `--no-index`** | 默认行为下 git 认为已入库文件不受忽略影响，检查会永远通过（假的防线，已用反向测试抓到） |

---

## 3. 追溯矩阵

| 规则/约束 | 文档 | 产物 | 验证 |
| --- | --- | --- | --- |
| `ST-1`（顶层目录白名单） | [`../design/structure.md`](../design/structure.md) §1.1 | 顶层目录未新增；`vendor/` 与 `scripts/bin/` 作为非源码形态在文档写明 | `make archcheck` |
| `ST-20`（忽略清单） | [`.gitignore`](../../.gitignore) | 只忽略密钥/真实配置与产物；头部说明已改准 | 正反两向 `make check-ignore` |
| `TB-15`（Python 门禁） | [`analysis/pyproject.toml`](../../analysis/pyproject.toml) | 门禁目标切到 `analysis/` 内运行，缺环境即失败 | `make pylint` · `make pytest` |
| `NI-1`（不影响业务） | [`../../deploy/docker/README.md`](../../deploy/docker/README.md) | 五容器一组；L4/控制台故障不影响请求路径 | 容器内实跑：业务 200 且影子模式只观测 |
| 明文 gRPC 只允许同机 | [`../design/structure.md`](../design/structure.md) §4 | `network_mode: service:core` | 容器内 `127.0.0.1:9443` 可达、宿主不可达 |
| `AR-11`（幂等） | [`../spec/events.md`](../spec/events.md) | L4 结论事件幂等 | 容器内重复跑一轮：`新 0 / 重复 2` |

---

## 4. 代码与产物

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `analysis/pyproject.toml` · `analysis/requirements.txt` · `analysis/requirements-dev.txt` | 新增（从根移入） | Python 工具链与配置归层；依赖**真锁版本**（此前 `grpcio` 只装在 venv、没进锁文件） |
| `analysis/tools/genproto.py` | 新增 | 生成 gRPC 桩并让其成为 `analysis.proto.*` 普通包（消除 `sys.path` 技巧与撞名） |
| `analysis/telemetry.py` | 改 | 按包路径导入生成物；显式加载 well-known types（容器里真炸过） |
| `deploy/docker/go.Dockerfile` · `analysis.Dockerfile` · `business.Dockerfile` | 新增 | 三类镜像：Go 三入口共用一个 Dockerfile（用 `SERVICE` 区分）· L4 worker · 演示业务站 |
| `deploy/docker/compose.yaml` · `deploy/docker/README.md` | 新增 | 一键全套 + 为什么共享命名空间、怎么换成自己的业务 |
| `.dockerignore` | 新增 | 构建上下文瘦身（放根目录才生效） |
| `vendor/` | 新增 | 依赖副本入库，构建离线 |
| `scripts/licensecheck/main.go` | 改 | 优先从 `vendor/` 读许可证 ⇒ 审计离线可用、且审的是**真正参与构建**的副本 |
| `scripts/archcheck/main.go` | 改 | 顶层目录白名单跳过构建产物（`*.egg-info` / `build` / `dist` / `__pycache__`） |
| `Makefile` | 改 | `PY_VENV`→`analysis/.venv`；工具二进制→`scripts/bin`；`pygen` 走生成脚本；新增 `check-ignore`（接进 `lint`）与 `docker-build`/`up`/`down`/`docker-ps`/`docker-logs`/`docker-log` |
| `.gitignore` | 改 | 删掉空转条目、补兜底并写清「不影响源码」；头部过期说明改准 |
| `README.md` · `docs/integrate/quickstart.md` · `docs/design/structure.md` · `docs/kb/dev-workflow.md` · `docs/progress.md` | 改 | 一键 Docker 优先、目录结构、环境位置、端口与日志注意事项 |

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 |
| --- | --- | --- | --- |
| 1 | 镜像构建 | 5 个镜像全成 | ✅ `core` 29.8MB · `proxy` 83.9MB · `console` 31.1MB · `analysis` 250MB · `business` 203MB（0 报错） |
| 2 | 起全套 | 5 容器 Up | ✅ `make docker-ps` 全部 Up |
| 3 | 经引擎访问 | 业务 200（影子模式） | ✅ 三条流量全 200 |
| 4 | 控制台可见判定细节 | 分值 + 命中信号 | ✅ `/api/flow` 有 `score` 与 `signals` |
| 5 | L4 在容器内跑 | 结论进入控制台 | ✅ `/api/analysis` 2 条（`intent` 接受 + `strategy` 按 `AR-15` 拒绝）；概览 `l4_conclusions: 2` |
| 6 | 幂等 | 重复跑不重复计 | ✅ 容器内再跑一轮：`新 0 / 重复 2` |
| 7 | 忽略清单自检 | 正反两向都正确 | ✅ 当前清单通过；故意加 `/core/cmd/core` 时报出 3 个会被排除的文件并退出非 0 |
| 8 | 门禁 | 全绿 | ✅ `make gate` |

**没有覆盖的**：多机部署（跨节点需 mTLS，未实现）· 镜像体积优化（analysis 250MB 可再瘦）· `docker compose` 在 Windows 上的行为。

---

## 6. 验证证据

```console
$ docker images --format '{{.Repository}}:{{.Tag}} {{.Size}}' | grep shen
shen-proxy:latest 83.9MB
shen-core:latest 29.8MB
shen-console:latest 31.1MB
shen-analysis:latest 250MB
shen-business:latest 203MB

$ curl -s -o /dev/null -w "%{http_code}\n" -A "HeadlessChrome/120" http://127.0.0.1:18080/.git/config
200

$ curl -s http://127.0.0.1:19444/api/summary
{"total":8,"by_action":{"route_origin":3},"alerts":0,"l4_conclusions":2,…}

$ make gate
门禁通过。
```

**关键指标**：新增镜像定义 **3** 个 · 新增编排 **1** 份 · 依赖副本 **63MB/5661 文件**（换取离线构建）· 根目录隐藏条目从 **8 个减到 3 个**（`.git` `.gitignore` `.pi`）· 新增门禁自检 **1** 项。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 跨节点部署仍不可用（明文 gRPC 只允许同机） | 多机要 mTLS | 设计已记（[`../design/structure.md`](../design/structure.md) §4），实现待排 |
| 2 | 控制台无鉴权 | 仅适合本机/内网 | [ADR-0020](../background/decisions/0020-console-minimal-static-ui.md) |
| 3 | `analysis` 镜像 250MB | 拉取慢 | 可换 slim 基础镜像 + 多阶段（待做） |
| 4 | vendor 使仓库变大 | clone 慢 | 若未来 Go 代理可达，可去掉 vendor 并恢复 `go mod download` |
| 5 | 演示业务站在 compose 内 | 易误当产品组件 | README 已标明「不是产品的一部分」 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `grpcio` **只在 venv 里、没进锁文件** | **缺陷**（换机器必崩） | 重建 venv 后 `make analysis` 报 `缺少 grpc` | 写进 `analysis/requirements.txt`；`protobuf` 对齐真实版本 7.36.2 | ✅ |
| 2 | 生成物的顶层名 `telemetry` 与 `analysis/telemetry.py` 撞名 | **缺陷**（容器内启动即崩） | `ModuleNotFoundError: 'telemetry' is not a package` | 生成脚本补 `__init__.py` 并改写为 `analysis.proto.*` | ✅ |
| 3 | 新版 protobuf 生成物需先导入 well-known types | **缺陷**（容器内 `AddSerializedFile` 报错） | `Depends on file 'google/protobuf/timestamp.proto'` | `telemetry.py` 显式导入 `google.protobuf.timestamp_pb2` | ✅ |
| 4 | Dockerfile 里的 `go mod download` 在**无代理网络**下永远卡住 | **缺陷**（"一直不完成"的真因） | `proxy.golang.org → HTTP 000` | 依赖 vendor 入库并删掉该步骤 | ✅ |
| 5 | `licensecheck` 加 vendor 后对 158 个模块报「未知」 | 工具误判/能力缺口 | `模块未下载到本地缓存，无法判定` | 改为优先从 `vendor/` 读许可证 | ✅ |
| 6 | 我写的 `make check-ignore` **是假的防线** | **缺陷**（自伤） | 反向测试（故意加 `/core/cmd/core`）仍然通过 | 改用 `--no-index`；正反两向重新验证 | ✅ |
| 7 | `.gitignore` 顶部写着「本仓库还不是 git 仓库」 | 过期文档 | 实际已有 14 次提交 | 头部重写，并说明 `check-ignore` 的用法与 `--no-index` 理由 | ✅ |
| 8 | `/core/core` 是空转规则，容易被误读成「忽略源码」 | 误导 | `ls core/core` 不存在；用户据此提问 | 删掉空转项、补齐兜底项、写明「不影响源码」 | ✅ |
| 9 | `.vscode/` 与已被取代的 `pyrightconfig.json` | 无用文件 | 前者仅 2 个编辑器配置；后者已并入 `analysis/pyproject.toml` | 删除 | ✅ |
| 10 | 我用 `docker logs -f` 看日志导致命令**挂住不返回** | 自伤（用法错误） | 命令一直流式输出不退出 | 新增 `make docker-log`（不跟随）并在 README/KB 写明区别 | ✅ |
| 12 | 搬迁 venv 后 `scripts/dev/smoke.sh` 仍查旧路径 `.venv` | **缺陷**（`make dev` 第 6 步直接红） | `❌ 缺 L4 环境：先跑 make pyenv` | 改为查 `analysis/.venv`，并全仓库扫描残留引用 | ✅ `make dev` 全绿 |
| 11 | 宿主 8080 被**别的项目**容器占用，`make up` 失败 | 环境冲突（非本仓库问题） | `Bind for 0.0.0.0:8080 failed: port is already allocated` | 宿主端口改为可配、默认避开（18080/19444）；按用户要求停掉 cairn 容器 | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | Docker 一键起全套（3 个 Dockerfile + compose + `make up/down/...`）· Python 工具链与配置收进 `analysis/` · Go 依赖 vendor 入库（离线构建）· `licensecheck` 支持 vendor · 新增 `make check-ignore` · 目录工整化与文档同步 | 用户要求（Docker 化 · 目录工整 · 少隐藏文件 · 文档准确）· `ST-1` · `ST-20` · `TB-15` · `NI-1` |
