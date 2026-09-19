# 运行手册：启动 · 检查 · 修复 · 更新

> **本文是运行操作的唯一说明**（启动、验证、排障、升级都在这里）。命令一律在**仓库根**执行。
> 设计层面的依据在 [`../design/`](../design/README.md)；模块细节在 [`../modules/`](../modules/README.md)。

## 0. 一分钟命令表

| 我要… | 命令 | 说明 |
| --- | --- | --- |
| 起全套（**推荐**） | `scripts/shen.sh up`（等价 `make up`） | 只需 Docker；会自动等就绪并打印地址 |
| 看状态 | `scripts/shen.sh status` | 容器 + 控制台概览 |
| 验证链路（造流量看判定） | `scripts/shen.sh smoke` | 经引擎发三条流量并回显分值/信号 |
| 仓库级验证 | `scripts/shen.sh check`（= `make gate` + `make dev`） | 门禁 + 开发循环 |
| 看日志（**不跟随**） | `scripts/shen.sh logs core` | 取尾部 80 行后立即返回 |
| 本地进程起（开发） | `scripts/shen.sh local` | 不用 Docker；Go/Python 直接跑 |
| 停掉 | `scripts/shen.sh down` | 删容器，保留镜像 |

`docker logs -f` 与 `make docker-logs` **会一直挂着**（不返回）：脚本与自动化里用 `scripts/shen.sh logs` 或 `make docker-log`。

## 1. 启动

### 1.1 Docker（默认方式，本地依赖最少）

前置：**Docker 24+**（含 `docker compose`）。本机不需要 Go / Python / 前端工具链。

```sh
scripts/shen.sh up
```

首次会构建镜像（约 1–4 分钟：Go 三入口 + Python 层）；之后是幂等的（改了代码会自动重建对应镜像）。

启动后：

| 地址 | 是什么 |
| --- | --- |
| `http://127.0.0.1:19444/` | **观测控制台** —— 概览 · 告警 · 流量访问与流动 · L4 分析结论 |
| `http://127.0.0.1:18080/` | **业务入口**（经引擎；影子模式：只观测、不处置，`INT-11`） |

起了哪些容器：

| 服务 | 是什么 | 端口 |
| --- | --- | --- |
| `core` | 核心（判定与响应生成的唯一实现，`AR-2`）；**拥有本栈的网络命名空间** | 容器内 9443，**不对外发布** |
| `proxy` | L1 适配器：反向代理前置（接入形态③） | 容器内 8080 → 宿主 18080 |
| `console` | 控制台：只读观测（`AR-10`） | 容器内 9444 → 宿主 19444 |
| `analysis` | L4 近线分析 worker | 无 |
| `business` | 演示用假业务站（**不是产品的一部分**） | 容器内 19080 |

**端口被占用**（最常见：本机 8080/9444 被别的项目占着）：

```sh
SHEN_HTTP_PORT=8080 SHEN_CONSOLE_PORT=9444 scripts/shen.sh up
```

### 1.2 本地进程（开发用）

前置：**Go 1.26+**；跑 L4 与门禁另需 **Python 3.13+**（环境由 `make pyenv` 建在 `analysis/.venv`）。

```sh
make dev                 # 本地一键验证：配置干跑 → 起核心 → 冒烟 → 规则回放 → 跑一轮 L4
scripts/shen.sh local    # 起「本地演示环境」：核心 + 假业务站 + 反向代理 + 控制台
```

### 1.3 「起来了」与「已就绪」的区别

容器 `Up` **不等于**服务可用：核心要先装载策略、控制台要先连上核心。
所以脚本在 `up` 之后会**轮询控制台的可读性**（最多 60 秒），就绪才打印地址；超时会打印容器状态。

## 2. 检查（四层，从外到内）

| 层 | 命令 | 通过判据 |
| --- | --- | --- |
| ① 容器 | `scripts/shen.sh status` | 五个服务都 `Up`；`core` 的 PORTS 显示 `18080->8080` 与 `19444->9444` |
| ② 链路 | `scripts/shen.sh smoke` | 三条流量都 `HTTP 200`；「流量流动」里能看到 `score` 与 `signals` |
| ③ 仓库 | `scripts/shen.sh check` | `make gate` 通过（格式/vet/staticcheck/errcheck/架构/追溯/泄漏/许可/Python 门禁/单测含 -race）+ `make dev` 全绿 |
| ④ 观测 | 控制台四块 / 四个接口 | 见下表 |

链路检查的**期望输出**（实测）：

```text
经引擎发三条流量（http://127.0.0.1:18080）：
  HTTP 200  HeadlessChrome/120  /.git/config
  HTTP 200  sqlmap/1.7  /etc/passwd
  HTTP 200  Mozilla/5.0  /

控制台看到的流动（分值 / 命中信号只在这里可见，ST-7）：
  5 行
    10:38:13 route_origin GET  /.git/config     score=0.8999999999999999 signals=['ua-headless', 'path-probe']
    10:38:13 route_origin GET  /etc/passwd      score=0     signals=[]
```

控制台的四个只读接口（脚本化时直接用）：

| 接口 | 用途 |
| --- | --- |
| `/api/summary` | 概览：事件总数 · 按决策分布 · 告警数 · L4 结论数 · 时间范围 |
| `/api/flow?limit=300` | 判定流动：时间 · 决策 · 来源 · 方法 · 路径 · UA · **分值** · **命中信号** · 后端 |
| `/api/analysis` | **L4 结论**（意图 / 策略；含拒绝原因与证据引用数） |
| `/api/events?limit=100&type=decision` | 原始事件（`type` 可空；`type=analysis` 只看结论） |

> **每次启动/更新后请确认 `git status --short` 为空** —— 启动**不应该**改动仓库（见 §5）。

L4 结论需要**新态势**才产生：同一 `(来源, 会话, 方法, 路径)` 在 60 秒窗口内只分析一次（`AR-14`）。
所以「造一批新流量 → 等一个轮询周期（默认 20 秒）→ 看结论」。想立刻跑一轮：

```sh
docker compose -f deploy/docker/compose.yaml exec -T analysis python -m analysis.worker --core 127.0.0.1:9443 --once
```

## 3. 修复（症状 → 原因 → 处理）

| 症状 | 最可能的原因 | 处理 |
| --- | --- | --- |
| `make up` 报 `Bind for 0.0.0.0:8080 failed: port is already allocated` | 宿主端口被别的进程/容器占用 | `lsof -nP -iTCP:8080 -sTCP:LISTEN` 看是谁；或换端口：`SHEN_HTTP_PORT=18080 SHEN_CONSOLE_PORT=19444 scripts/shen.sh up` |
| 容器 `Up`，控制台打不开 | 控制台没连上核心（或宿主端口不是默认值） | `scripts/shen.sh logs console`；确认地址用的是实际端口 |
| 控制台顶部显示「读取核心失败」 | 核心没起 / 名字不对 / 不在同一网络命名空间 | `scripts/shen.sh logs core`；确认 `core` 服务在跑 |
| `analysis` 反复 `Restarting` | L4 侧启动失败（依赖缺失 / 导入错误） | `scripts/shen.sh logs analysis`（**不跟随**）。历史上踩过三种：依赖没锁进 `analysis/requirements.txt`；生成桩顶层名与本层模块撞名；新版 protobuf 需先导入 well-known types |
| 业务入口返回 502 | 上游不可达（`SHEN_PROXY_UPSTREAM` 指错了） | `scripts/shen.sh logs proxy`；确认 `business` 在跑，或把上游改成你的服务 |
| 控制台分数全是 0 | 配置里的 `rules` 为空 | 检查 `deploy/config/config.example.yaml` 的 `rules`：没有规则就恒为 0 |
| `make gate` 红在 `fmt-check` | 有 Go 文件没格式化（**不会**是 vendor —— 已排除第三方） | `make fmt` 后重跑 |
| `make dev` 报「缺 L4 环境」 | 没建 Python 环境（或被移动） | `make pyenv`（建在 `analysis/.venv`） |
| 构建卡在 `go mod download` | 网络访问不了 Go 模块代理 | 本仓库依赖已 **vendor 入库**，正常构建**不需要网络**；若仍卡，见 §4.2 |
| 容器起不来且提示 `no space left` | Docker 磁盘满 | `docker system prune`（会删未使用镜像/缓存） |

**先看日志再猜**：`scripts/shen.sh logs <服务>`（`core` / `proxy` / `console` / `analysis` / `business`）。

## 4. 更新

### 4.1 改代码之后

```sh
scripts/shen.sh up        # 自动重建有变化的镜像并滚动重启
```

只改一个服务时更快：

```sh
docker compose -f deploy/docker/compose.yaml build console && \
docker compose -f deploy/docker/compose.yaml up -d console
```

### 4.2 Go 依赖变化

```sh
go mod tidy && go mod vendor      # 必须重新 vendor 并**一起提交** vendor/
go build ./...                    # 验证 vendor/modules.txt 与 go.mod 一致
```

> 为什么 vendor 入库：本机与不少环境访问不了 `proxy.golang.org`，容器里 `go mod download` 会永久卡住。
> vendor 让构建与门禁**离线可用**。若你的网络能访问代理，也可以删掉 `vendor/` 并恢复 `go mod download`。

### 4.3 Python（L4）依赖或契约变化

```sh
# 依赖：改 analysis/requirements*.txt 后
make pyenv                        # 重建 analysis/.venv（锁定版本）
# 契约：改了 api/ 下的 .proto 后
make pygen                        # 重新生成 analysis/proto（并一起提交）
make pytest                       # 37 例
```

### 4.4 升级基础镜像 / 工具链

1. 改 `deploy/docker/*.Dockerfile` 里的基础镜像 tag（或 `analysis/requirements-dev.txt` 里的 `ruff`/`pytest`）；
2. `make docker-build` 重建；
3. `scripts/shen.sh check` 跑完整验证；
4. 在 [`../log.md`](../log.md) 记一条（含旧版本 → 新版本）。

## 5. 产物卫生（仓库里不留东西）

| 东西 | 落在哪 | 入库？ |
| --- | --- | --- |
| 容器镜像 / 网络 / 卷 | Docker 自己管 | 否 |
| 本地跑起来的二进制与日志 | `${TMPDIR:-/tmp}/shen-<uid>/`（脚本的 `RUNDIR`） | 否 |
| Go 依赖副本 | `vendor/` | **是**（为了离线构建） |
| Python 环境与缓存 | `analysis/.venv` · `analysis/__pycache__` · `analysis/.pytest_cache` | 否 |
| 门禁工具二进制 | `scripts/bin/` | 否 |
| 生成的 gRPC 桩 | `analysis/proto/` | **是**（契约生成物，与 Go 侧一致） |

**规矩**：任何脚本都**不许**往仓库里写临时文件；要落盘就落 `RUNDIR`。
每轮开发收尾时：

```sh
git status --short          # 应为空 —— 启动与验证都不该改仓库
make check-ignore           # 忽略清单不能误伤已入库文件
```

## 6. 端口与地址一览

| 变量 | 默认 | 作用 |
| --- | --- | --- |
| `SHEN_HTTP_PORT` | `18080` | 宿主侧业务入口（映射容器内 `proxy` 的 8080） |
| `SHEN_CONSOLE_PORT` | `19444` | 宿主侧控制台（映射容器内 `console` 的 9444） |
| `SHEN_RUNDIR` | `${TMPDIR:-/tmp}/shen-<uid>` | 临时产物目录（二进制 / 日志 / 启动日志） |
| `SHEN_CORE_ADDR` | `127.0.0.1:9443` | 适配器 / 控制台 / L4 访问核心判定面的地址 |
| `SHEN_CONFIG` | `/etc/shen/config.yaml`（容器内） | 核心配置（挂载自 `deploy/config/config.example.yaml`） |

> 核心判定面是**明文 gRPC**，只允许回环地址（`assertPlaintextListenIsLocal` 兜底）。
> 容器里靠「除核心外都共享核心的网络命名空间」维持「同机」前提 —— 详见 [`../../deploy/docker/README.md`](../../deploy/docker/README.md)。
> 跨节点部署必须换 mTLS（未实现，见 [`../design/structure.md`](../design/structure.md) §4）。
