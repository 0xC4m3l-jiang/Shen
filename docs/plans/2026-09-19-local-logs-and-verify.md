# 变更包：本地日志（逐判定）+ 一键验证与定位线索

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 让"刚才那条请求为什么被判成这样"能在**本地一条命令内**查清：补逐判定结构化日志 · 补适配器逐请求日志 · 补一键端到端验证与定位线索 · 把日志字典写成规格 |
| 日期 | 2026-09-19 |
| 状态 | 已验证（核心与适配器的逐判定日志**实跑可见**；`scripts/shen.sh verify` 端到端跑通并落报告） |
| 改动分级 | **L**（改核心与适配器的日志面 + 新增日志规格 + 新增校验入口） |
| 涉及范围 | `core/cmd/core/main.go` · `edge/proxy/handler.go` · `edge/proxy/cmd/proxy/main.go` · `edge/proxy/proxy_test.go` · `deploy/docker/compose.yaml` · `scripts/shen.sh` · `scripts/traffic/send.py` · `docs/spec/logs.md`（新增）· `docs/ops/runbook.md` · `scripts/traffic/README.md` · `docs/README.md` |
| 决策数 | 已答 4 项（逐判定日志放记录器 · slog 结构化 + 格式开关 · 适配器逐请求日志默认关、演示栈开 · 日志字典独立成 spec）/ 待定 3 项 |
| 关联 | `ST-7` · `NI-1` · `AR-11` · `AR-13` · `MD-6` · [`../spec/logs.md`](../spec/logs.md) |

---

## 1. 需求与验收

**用户要什么**：① 补充项目**本地日志**；② 完善**测试脚本**，保证后续**验证**与**定位问题**更快更准。

**验收判据**：

1. **每条判定都有日志**：一行结构化记录（`decision_id` / `action` / `score` / `signals` / `method` / `path` / 后端 / 时刻），实跑可见；
2. **"为什么没判"也有日志**：适配器记录白名单命中与判定缓存命中（`ST-10` 复用的直接证据）、判定结果、回落原因；
3. **开关化**：核心 `SHEN_LOG_FORMAT=text` 或 `json`；适配器 `SHEN_PROXY_LOG_REQUESTS`（生产默认关、演示栈默认开）；
4. **不违反两条硬规矩**：日志是内部面（可有判定细节，`ST-7` 只管响应）；日志故障不得影响请求（`NI-1`）；
5. **一键验证**：`scripts/shen.sh verify` = 状态 + 全量伪造流量 + L4 核对 + **报告落临时目录** + 定位线索；
6. **定位脚本化**：`send.py --explain` 打印失败场景的判定原文与"追一条"的命令；`--report` 落盘；
7. **日志字典成文**：`docs/spec/logs.md`（谁记什么 · 字段 · 开关 · 落在哪 · 与事件的区别 · 定位手法）；
8. `make gate` 绿。

**不做什么**：不做集中式日志/采集（`metrics.md` 仍待建）· 不做 trace/span id · 不给适配器加判定逻辑。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | 逐判定日志放哪 | 核心的**观测记录器**（每次判定都经过它） | 一个位置覆盖全部判定；与「判定只在核心实现一次」（`AR-2` / `AR-5`）同向 |
| ② | 用什么格式 | `slog` 结构化，`SHEN_LOG_FORMAT=text` 或 `json` | 字段化才好筛；启动/装载类仍用标准 `log`（稳定可 grep，脚本在依赖） |
| ③ | 适配器要不要也逐请求记 | 要，但**默认关**（`SHEN_PROXY_LOG_REQUESTS`），演示 compose 默认开 | 观测面已记全量判定；生产不需要两份逐请求日志，本地排查却必须有 |
| ④ | 日志字典放哪 | 独立 `docs/spec/logs.md` | 原先挂在"待建"清单里；字段/开关/定位手法需要一个权威处 |
| ⑤ | 一键验证放哪 | `scripts/shen.sh verify`（报告落 `${RUNDIR}`） | 与"仓库里不留产物"一致；同时给出下一步定位命令 |

---

## 3. 追溯矩阵

| 规则 | 落实 |
| --- | --- |
| `ST-7` 响应不回显 | 日志带 `score`/`signals`（内部面）；`docs/spec/logs.md` §1 写明判据"会不会出现在对手的屏幕上" |
| `NI-1` 不影响业务 | `logDecision` 仅在 Archive 成功后 best-effort 打印；日志器为 nil 时直接返回；适配器日志不参与控制流 |
| `AR-11` 事件幂等 | 日志与事件的区别在 spec §6 讲清（日志可丢、事件必须幂等） |
| `AR-13` 版本对账 | 适配器已有的策略应用/回执日志纳入字典 |
| `MD-6` 时钟注入 | 日志里的 `at` 直接用判定记录里的时刻（调用方注入），不读系统时钟 |
| `TB-15` 门禁 | `scripts/` 已在 ruff 覆盖内；`make gate` 绿 |

---

## 4. 产物

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `core/cmd/core/main.go` | 改 | 新增 `newDecisionLogger()`（slog，格式开关）+ 记录器里 `logDecision()`（逐判定 11 个字段） |
| `edge/proxy/handler.go` | 改 | 白名单命中 / 判定缓存命中 / 判定结果三处日志（开关控制）+ `actionName()`（日志用设计三值术语） |
| `edge/proxy/cmd/proxy/main.go` | 改 | `SHEN_PROXY_LOG_REQUESTS` 接线（默认 false） |
| `edge/proxy/proxy_test.go` | 改 | `TestActionName` 守住"日志用设计术语" |
| `deploy/docker/compose.yaml` | 改 | 演示栈 `SHEN_PROXY_LOG_REQUESTS: "true"` |
| `scripts/shen.sh` | 改 | 新增 `verify`；`restart` 改带 `--build`（真缺陷，见 §7.1 #1） |
| `scripts/traffic/send.py` | 改 | `--explain`（失败/缺口场景打印判定原文 + 追查命令）· `--report`（报告落盘） |
| `docs/spec/logs.md` | 新增 | 日志字典（原则 · 组件日志点 · 字段表 · 开关 · 落点 · 与事件区别 · 定位手法 · 未决） |
| `docs/ops/runbook.md` · `scripts/traffic/README.md` · `docs/README.md` | 改 | verify 命令 · 日志与定位两行故障表 · logs.md 状态从"待建"改准 |

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 |
| --- | --- | --- | --- |
| 1 | 造一条探针流量后看核心日志 | 出现 `msg=decision` 且字段齐全 | ✅ `msg=decision decision_id=b1fd473f… action=route_origin score=0.6 signals=[ua-headless] method=GET path=/probe` |
| 2 | 同一条看适配器日志 | 出现判定行且 id 与核心一致 | ✅ `proxy: 判定：GET /probe decision_id=b1fd473f… → route_origin（后端 ""，失败=<nil>）` |
| 3 | 日志用设计术语 | 三值而非枚举名 | ✅ `TestActionName` 通过（含 `ACTION_UNSPECIFIED → route_origin`） |
| 4 | 一键验证 | 报告落临时目录 + 给线索 | ✅ 报告写入临时目录；打印逐判定/适配器两条追查命令 |
| 5 | `--explain` | 失败/缺口场景打印判定原文 | ✅ |
| 6 | 门禁 | 绿 | ✅ `make gate` |

**没有覆盖的**：日志轮转与保留 · 高并发下的日志开销 · `metrics.md`（仍未建）。

---

## 6. 验证证据

```console
$ scripts/shen.sh logs core | grep msg=decision | tail -1
time=2026-09-19T15:34:49.677Z level=INFO msg=decision decision_id=b1fd473f8d5a76b49724db4fd8e76a35 \
  action=route_origin severity=none backend="" score=0.6 signals=[ua-headless] method=GET path=/probe \
  source_ip=172.21.0.1 user_agent=HeadlessChrome/120 at=2026-09-19T15:34:49.676Z

$ scripts/shen.sh logs proxy | grep 'proxy: 判定' | tail -1
proxy: 判定：GET /probe decision_id=b1fd473f8d5a76b49724db4fd8e76a35 → route_origin（后端 ""，失败=<nil>）

$ scripts/shen.sh verify
== 3/3 报告与定位线索 ==
  报告：…/T/shen-501/verify-20260919-233558.json
  逐判定日志：scripts/shen.sh logs core | grep msg=decision
```

**关键指标**：新增日志点 **4** 处（核心逐判定 1 + 适配器 3）· 新增字段 **11** 个 · 新增开关 **2** 个 · 新增规格文档 **1** 份 · 新增一键入口 **1** 个（`verify`）。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 无日志轮转/保留 | 生产需配 log driver | 部署侧配置 |
| 2 | 无 trace/span id | 一次请求多判定时无法成树 | 跨进程关联暂靠 `decision_id` |
| 3 | 适配器 `request_judged` 事件字段未入字典 | 字段分散 | 补 [`../spec/events.md`](../spec/events.md) 后合并 |
| 4 | 日志级别不可调（只有逐请求开关） | debug 细粒度受限 | 需要时加 `SHEN_LOG_LEVEL` |
| 5 | `metrics.md` 仍未建 | 指标口径无权威处（`AR-28`） | 下一轮 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | **`restart` 只重建容器、不重建镜像** | **真缺陷**（改了代码看不到任何日志/行为变化，极易误判为"日志没生效"） | 加完日志后 `verify` 无一行 `msg=decision`；`docker compose` 未 rebuild | `cmd_restart` 改为 `up -d --build --force-recreate`，并在 spec/runbook 写明 | ✅ 重建后日志立刻出现 |
| 2 | `logs.md` 在 `docs/README.md` 里仍标"待建" | 过期状态（门禁的 TC-2 会拦） | `docs/README.md` §3 两行 | 改为 ✅ 并写清它现在覆盖什么、`metrics.md` 仍待建 | ✅ |
| 3 | 我在适配器日志里用了不存在的 `actionName` | 自伤（编译红） | LSP + 编译器双报 | 新增纯函数 `actionName`（设计三值术语）+ 单测 | ✅ |
| 4 | 该函数放在 `glue.go` 时，静态检查器反复报"未定义" | 工具陈旧索引（已用三法证伪：grep 定义 · 全新空缓存编译 exit=0 · 包测试 ok） | 多次红灯 | 把定义**挪到使用它的同一文件**（彻底不依赖跨文件索引） | ✅ 不再报 |
| 5 | 适配器逐请求日志若默认开会很吵 | 取舍 | — | 默认关，演示 compose 开，并在 spec §4 说明理由 | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 核心逐判定结构化日志（`slog` + `SHEN_LOG_FORMAT`）· 适配器逐请求日志（白名单/缓存/判定，`SHEN_PROXY_LOG_REQUESTS`）· 新增 [`../spec/logs.md`](../spec/logs.md) · `scripts/shen.sh verify`（一键验证 + 报告）· `send.py --explain/--report` · `restart` 带 `--build` | 用户要求（补本地日志 + 完善测试脚本，让验证与定位更快更准）· `ST-7` · `NI-1` · `AR-11` · `MD-6` |
