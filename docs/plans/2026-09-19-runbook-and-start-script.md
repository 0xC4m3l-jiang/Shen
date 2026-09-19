# 变更包：运行手册 + 一键启动脚本 + 产物卫生

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 把「怎么启动、怎么检查、怎么修、怎么更新」写成**唯一一份**运行说明，并提供一个**会等就绪、不落仓库文件**的启动脚本 |
| 日期 | 2026-09-19 |
| 状态 | 已验证（`up` / `status` / `smoke` / `logs` / `check` 五个子命令实测；`make gate` + `make dev` 全绿；启动后工作区保持干净） |
| 改动分级 | **M**（新增脚本与文档 + 一处控制台接口语义修复） |
| 涉及范围 | `scripts/shen.sh`（新增）· `docs/ops/runbook.md`（新增）· `scripts/demo/run.sh` · `Makefile` · `console/cmd/console/main.go` · `README.md` · `docs/README.md` |
| 决策数 | 已答 3 项（脚本为统一入口 · 临时产物统一落一个临时目录 · 日志默认不跟随）/ 待定 2 项 |
| 关联 | `NI-1` · `ST-7` · `ST-17` · `AR-10` · `INT-11` · `TB-15` · `ST-20` · [`../ops/runbook.md`](../ops/runbook.md) |

---

## 1. 需求与验收

**用户要什么**：① 把**如何启动 / 如何检查 / 如何修复**记录下来，保证后续 Shen 的启动、更新、验证准确；
② 给出一个**启动脚本**；③ 注意本地目录产物的位置 —— 该进临时目录的进临时目录，**别让无用文件影响项目结构**。

**验收判据**：

1. 存在**唯一**运行说明 `docs/ops/runbook.md`：启动（Docker / 本地）· 检查（四层）· 修复（症状 → 原因 → 处理表）· 更新（代码 / Go 依赖 / Python 依赖 / 基础镜像）· 产物卫生 · 端口变量。
2. 存在一键脚本 `scripts/shen.sh`：`up`（**等就绪**后打印地址）· `status` · `smoke`（造流量并回显判定）· `check`（`make gate` + `make dev`）· `logs`（**不跟随**）· `local` · `down`；并接进 `make start/status/app-smoke/verify`。
3. 临时产物**只落一个目录**：`${TMPDIR:-/tmp}/shen-<uid>/`；仓库里不留二进制、日志、pid。
4. 启动 / 验证后 `git status --short` 为**空**。
5. 文档接线：README §3 指向运行手册；`docs/README.md` 导航收录；`make help` 能看到新目标。

**不做什么**：不改产品行为 · 不引入 Kubernetes/Helm（compose 够用）· 不替用户决定生产部署形态。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | 启动入口放哪 | **`scripts/shen.sh` 作为统一入口**，Makefile 保留 `make up` 并加薄别名 | 脚本能做「等就绪 / 打印地址 / 非跟随日志 / 临时目录」这些 Makefile 不擅长的事 |
| ② | 「起来了」怎么判 | `up` 之后**轮询控制台可读性**（最多 60 秒） | 容器 `Up` ≠ 服务可用；核心要先装策略、控制台要先连上核心（`ST-17` 的语义区分） |
| ③ | 临时产物放哪 | 统一 `${TMPDIR:-/tmp}/shen-<uid>/`（`RUNDIR`），并去掉 `TMPDIR` 可能带的尾斜杠 | 用户要求"减少不必要的文件影响项目结构"；一个目录便于清理与排查 |
| ④ | 日志怎么给 | 默认**不跟随**（`--tail` 即返回）；要跟随用 `make docker-logs` | `docker logs -f` 会挂住 —— 本仓库真的踩过（人写的命令与自动化都不友好） |
| ⑤ | 运行说明写哪 | `docs/ops/runbook.md`，并在 README/docs 导航建指针 | 之前「怎么跑」散落在 README、`docs/integrate/`、`deploy/docker/README.md`，排障时要翻三处 |

---

## 3. 追溯矩阵

| 规则/约束 | 文档 | 产物 | 验证 |
| --- | --- | --- | --- |
| `NI-1`（不影响业务） | [`../ops/runbook.md`](../ops/runbook.md) §2 | `smoke` 三条流量均 200；影子模式只观测（`INT-11`） | `scripts/shen.sh smoke` |
| `ST-7`（判定细节只进观测面） | 同上 §2 | 脚本明确提示「分值/信号只在控制台可见」 | 实测输出 |
| `AR-10`（控制台只读） | 同上 §2 | 控制台四个接口全为只读 | 代码评审 |
| `TB-15`（门禁） | 同上 §2 §4 | `scripts/shen.sh check` = `make gate` + `make dev` | `scripts/shen.sh check` |
| `ST-20` / 产物卫生 | 同上 §5 | 临时产物只落 `RUNDIR`；`make check-ignore` | `git status --short` 为空 |

---

## 4. 代码与文档

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `scripts/shen.sh` | 新增 | 统一入口：`up`（等就绪）· `status` · `smoke` · `check` · `logs`（不跟随）· `local` · `down`；临时产物落 `RUNDIR` |
| `docs/ops/runbook.md` | 新增 | 运行操作唯一说明：启动 · 检查（四层）· 修复（故障表）· 更新 · 产物卫生 · 端口变量 |
| `scripts/demo/run.sh` | 改 | 二进制与日志从硬编码 `/tmp/shen-demo-*` 改为统一 `RUNDIR`（并处理 `TMPDIR` 尾斜杠） |
| `Makefile` | 改 | 新增 `start` / `status` / `app-smoke` / `verify` 四个薄别名（都指向脚本） |
| `console/cmd/console/main.go` | 改 | `/api/flow` 改为**只取 decision 事件**再套 limit —— 修复「页面行数远少于 limit」的语义缺陷 |
| `README.md` · `docs/README.md` | 改 | §3 指向运行手册与脚本；导航收录运行手册 |

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 |
| --- | --- | --- | --- |
| 1 | `scripts/shen.sh up` | 幂等启动 + 等就绪 + 打印地址 | ✅ 打印两个地址与 `RUNDIR` |
| 2 | `scripts/shen.sh status` | 容器状态 + 控制台概览 | ✅ 五服务 `Up`；概览 JSON 可读 |
| 3 | `scripts/shen.sh smoke` | 三条流量 200 + 回显分值/信号 | ✅ 5 行，探针 `score=0.9 signals=['ua-headless','path-probe']` |
| 4 | `scripts/shen.sh logs console` | 取尾部后**立即返回** | ✅ 返回日志尾部（不挂住） |
| 5 | `scripts/shen.sh check` | `make gate` + `make dev` 全绿 | ✅ 全绿（含第 6 步 L4） |
| 6 | `/api/flow` 语义 | limit 作用于**判定**事件 | ✅ 修复前 1 行 → 修复后 5 行 |
| 7 | 启动后仓库干净 | `git status --short` 为空 | ✅ 无产物落进仓库 |
| 8 | `TMPDIR` 带尾斜杠 | 路径不出现 `//` | ✅ 已规范化 |

**没有覆盖的**：Windows/WSL 上的脚本行为 · 断网环境下 `up`（镜像已缓存时应可离线起）· 长时间运行的日志轮转。

---

## 6. 验证证据

```console
$ scripts/shen.sh up
  ✅ 已就绪
     观测控制台   http://127.0.0.1:19444/
     业务入口     http://127.0.0.1:18080/
  临时产物目录（不在仓库里）：/var/folders/.../T/shen-501

$ scripts/shen.sh smoke
  HTTP 200  HeadlessChrome/120  /.git/config
  5 行
    10:38:13 route_origin GET  /.git/config     score=0.8999999999999999 signals=['ua-headless', 'path-probe']

$ scripts/shen.sh check
✅ 通过：配置校验 · 非法配置被拒 · 启动 · 判定面 · 冒烟 · 规则回放 · L4 近线分析
```

**关键指标**：新增脚本 **1 个** · 新增运行手册 **1 份**（约 130 行）· Makefile 薄别名 **4 个** · 修复接口语义缺陷 **1 处**。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 跨节点部署仍需 mTLS | 多机不可用 | 设计约束（[`../design/structure.md`](../design/structure.md) §4） |
| 2 | 控制台无鉴权 | 只适合本机/内网 | [ADR-0020](../background/decisions/0020-console-minimal-static-ui.md) |
| 3 | `analysis` 镜像 250MB | 拉取慢 | 可多阶段瘦身 |
| 4 | 未在 CI 里跑 `scripts/shen.sh up`（需 Docker-in-Docker） | 脚本回归靠人工 | 若要接 CI，用专用 runner |
| 5 | hadolint 未能本地运行（无该工具） | Dockerfile 少了独立 lint | 需要时用 `docker run --rm -i hadolint/hadolint < deploy/docker/go.Dockerfile` |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `/api/flow?limit=N` 的 limit 作用在**全部事件**上，过滤后行数远少于 N | **缺陷**（页面"记录变少"的假象） | `smoke` 只显示 1 行，而刚发了 3 条流量 | `handleFlow` 改为只取 decision 事件后再套 limit | ✅ 修复后 5 行 |
| 2 | `smoke` 里内联 python 用 `\"` 转义，被 Python 当成字面反斜杠 | **缺陷**（脚本自伤） | 输出「读取失败」 | 改用 here-doc 传参（`python3 - "$CONSOLE" <<'PY'`） | ✅ 正常回显 |
| 3 | `TMPDIR` 带尾斜杠时打印 `.../T//shen-501` | 瑕疵 | 实测输出 | 脚本与 `demo/run.sh` 都做 `${_TMPBASE%/}` 规范化 | ✅ 路径干净 |
| 4 | `scripts/dev/smoke.sh` 仍查旧 `.venv`（上一轮遗漏） | 缺陷 | `make dev` 报「缺 L4 环境」 | 已改为 `analysis/.venv`（上一提交修复） | ✅ |
| 5 | 运行说明此前散落三处（README / integrate / deploy/docker） | 结构问题 | 排障要翻三处 | 收敛到 `docs/ops/runbook.md`，别处只留指针 | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 新增运行手册 `docs/ops/runbook.md` 与一键入口 `scripts/shen.sh`（等就绪 / 不跟随日志 / 统一临时目录）· Makefile 薄别名 `start`/`status`/`app-smoke`/`verify` · 修复 `/api/flow` 的 limit 语义 · 临时产物统一 `RUNDIR` | 用户要求（记录启动/检查/修复 · 给启动脚本 · 少留文件）· `NI-1` · `ST-7` · `ST-17` · `TB-15` · `ST-20` |
