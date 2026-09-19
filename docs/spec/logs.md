# 规格：日志（`logs.md`）

> **状态**：已实现（阶段 1–2b 的日志点已就位）。
> 本文是**日志字典**：谁记什么、字段叫什么、开关在哪、落在哪、怎么用它定位问题。
> 规则依据：`ST-7`（响应禁止回显判定细节）· `NI-1`（日志故障不得影响请求）· `AR-11`（事件幂等，与日志不同）· `MD-6`（时钟由调用方注入）。

---

## 1. 三条原则

1. **日志是内部面**：可以记分值 / 信号 / 判定 id（**响应**不可以 —— `ST-7`）。判据是"会不会出现在对手的屏幕上"；
2. **日志失败不得影响请求**（`NI-1`）：只记，不因日志失败改行为；**只写 stdout**（12-factor，容器/编排友好，不在容器里写文件）；
3. **优先结构化字段**：逐判定行带 `msg=decision` 与具名字段（`decision_id` / `action` / `score` / `signals`），不要靠拼接字符串再正则。

---

## 2. 组件 → 日志点

| 组件 | 日志点 | 级别 | 说明 |
| --- | --- | --- | --- |
| `core` | 启动 / 影子模式 | info | 监听地址 + 当前模式（`INT-11`） |
| `core` | 策略装载 | info | `policy_id` / `version` / `checksum` / 规则条数 / 灰度 |
| `core` | 规则集为空 | **warn** | 判定恒零分（联调用；正式环境不该出现） |
| `core` | 诱饵面 / 幻境后端池摘要 | info · warn | 启用数量、可用后端数；读取失败给 warn |
| `core` | **逐判定** | info | `msg=decision` + 见 §3 字段表 —— **本地排查第一入口** |
| `proxy` | 启动 / 策略面 | info | 监听、上游、核心地址、影子模式、策略拉取间隔 |
| `proxy` | 策略应用 / 校验和 / 回执 | info · warn | 版本对账（`AR-13`）；失败继续用当前策略 |
| `proxy` | **白名单命中**（跳过判定） | info（需开关） | 记方法、路径、来源 —— 排查"为什么没判" |
| `proxy` | **判定缓存命中** | info（需开关） | 记 `decision_id` → 决策与后端（`ST-10` 复用的直接证据） |
| `proxy` | **判定结果** | info（需开关） | 记方法、路径、`decision_id`、决策、后端、失败原因 |
| `proxy` | 引流后端不可达 / 中途失败（回落业务） | warn | `NI-5`：宁可漏改道，不可断业务 |
| `proxy` | 遥测上报失败 | warn | 异步面，不影响请求（`AR-11`） |
| `console` | 启动 / 写响应失败 | info · warn | 只读面 |
| `analysis`（L4 worker） | 每轮摘要 + 结论 | info | `取事件 N 条 · 去重后 M 条 · 结论 K 条`（`AR-14` 去重的可见证据） |

---

## 3. 逐判定日志字段（`core`，`msg=decision`）

| 字段 | 说明 |
| --- | --- |
| `decision_id` | 判定幂等键（`ST-10`）；**跨组件关联全靠它**（proxy 日志、控制台、L4 证据引用都用它） |
| `action` | 三值：`route_origin` / `route_mirage` / `block`（`terminology.md` §4） |
| `severity` | 旁路字段（档位未定，当前恒为 `none`） |
| `backend` | 仅改道时非空 |
| `score` | 风险分（命中权重求和，1.0 截断） |
| `signals` | 命中信号 ID（= 配置里规则 `id`） |
| `method` / `path` | 请求身份（`path` **不含查询串**，见 [`config.md`](config.md) §2.4） |
| `source_ip` / `user_agent` | 请求身份（攻击者可控，仅内部可见） |
| `at` | 判定时刻（由调用方注入，`MD-6`） |

**proxy 的逐请求行**用同一套词（`route_origin` / `route_mirage` / `block`），便于与控制台、核心日志直接对照。

---

## 4. 格式与开关

| 开关 | 默认 | 作用 |
| --- | --- | --- |
| `SHEN_LOG_FORMAT` | `text` | `core` 逐判定行格式：`text`（人读）· `json`（`jq` / 日志管道） |
| `SHEN_PROXY_LOG_REQUESTS` | `false` | `proxy` 逐请求行（白名单命中 / 缓存命中 / 判定结果）。**演示 compose 默认开**；生产按需，量大时再关 |
| `SHEN_PROXY_POLICY_INTERVAL` | 见适配器文档 | 策略面拉取间隔（`0` = 不拉取） |

启动与装载类日志固定走标准 `log`（稳定、可 grep —— 脚本在依赖它）；逐判定走结构化日志（`slog`）。

---

## 5. 落在哪

| 运行方式 | 日志去哪 | 怎么看 |
| --- | --- | --- |
| Docker（推荐） | 容器 **stdout**（compose 的 json-file 驱动） | `scripts/shen.sh logs core`（**不跟随**，取尾部）· `make docker-logs S=proxy`（跟随） |
| 本地进程（`scripts/shen.sh local`） | `${TMPDIR:-/tmp}/shen-<uid>/*.log` | `tail -f $RUNDIR/core.log`（脚本启动时会打印该目录） |

> **仓库里不写日志**：所有临时产物统一落在 `RUNDIR`（见 [`../ops/runbook.md`](../ops/runbook.md) §5）。

---

## 6. 日志 vs 事件（别混）

| 维度 | 日志（本文） | 事件（观测面，[`events.md`](events.md)） |
| --- | --- | --- |
| 用途 | 运维排查 | 观测/审计/驱动 L4 |
| 落点 | stdout（可丢、可滚） | 存储（内存实现，`store` 是唯一 I/O 出口，`MD-20`） |
| 幂等 | 不保证（重启即丢历史） | **必须**幂等（`event_id`，`AR-11`） |
| 消费方 | 人、`grep`/`jq` | 控制台、L4 worker、离线分析 |

**要去查历史**用控制台接口；**要看"刚才发生了什么"**用日志。

---

## 7. 定位手法（照着做）

```sh
# ① 一条请求为什么被判成这样：按 decision_id 串起来
scripts/shen.sh logs core | grep msg=decision | tail -20          # 全部逐判定
scripts/shen.sh logs core | grep <decision_id>                    # 指定一条
scripts/shen.sh logs proxy | grep <decision_id>                   # 适配器侧同一 id
curl -s http://127.0.0.1:19444/api/flow?limit=20 | jq '.[] | select(.decision_id=="<decision_id>")'

# ② 哪些信号在起作用
scripts/shen.sh logs core | grep msg=decision | grep -o 'signals=\[[^]]*\]' | sort | uniq -c | sort -rn

# ③ 为什么"没判"（走了白名单 / 吃了缓存）
scripts/shen.sh logs proxy | grep -E '白名单|缓存命中'

# ④ 引擎有没有影响业务（回落 / 失败）
scripts/shen.sh logs proxy | grep -E '回落|失败'

# ⑤ 机器可读：把逐判定切成 JSON 行再筛
SHEN_LOG_FORMAT=json ./scripts/shen.sh local        # 然后 jq：
tail -f "$RUNDIR/core.log" | jq -c 'select(.msg=="decision") | {decision_id, action, score, signals}'
```

> 改了代码/配置之后记得**重建镜像**：`scripts/shen.sh restart`（带 `--build`）。
> 只 `--force-recreate` 会在旧镜像上重建容器 —— 新日志一行都不会出现（实测踩过）。

---

## 8. 未决

| # | 未决 | 影响 |
| --- | --- | --- |
| 1 | 没有**轮转与保留**策略 | 生产需配日志驱动/轮转（Docker `max-size` 等） |
| 2 | 没有 trace / span id | 跨进程关联只靠 `decision_id`；一次请求多判定时无法成树 |
| 3 | 适配器侧 `request_judged` 事件的字段字典未写入本文 | 目前见 [`events.md`](events.md)；补全后再合并 |
| 4 | 日志级别不可调（除逐请求开关） | 需要 debug 级细粒度时可能过吵 |
