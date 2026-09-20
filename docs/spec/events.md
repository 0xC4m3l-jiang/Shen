# 契约：遥测事件载荷（`event`）

> **状态**：已实现（阶段 2b）。本文是**跨语言契约**：核心写、控制台与 L4 读，三方必须同键。
> 规则依据：`ST-6`（契约唯一事实源）· `AR-11`（幂等）· `ST-7`（判定细节只进观测面）· `AR-31`（原始观测不得丢弃）。

## 1. 事件信封

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `event_id` | string | **幂等键**（`AR-11`）：同一 ID 重复上报不产生重复记录 |
| `event_type` | string | 事件类型：`decision`（判定）· `analysis`（L4 结论） |
| `session_id` | string | 会话 ID（`AR-25` 的一等字段） |
| `actor_id` | string | 行为主体（当前为空，留待来源身份接入） |
| `payload` | bytes | 事件载荷：UTF-8 的 JSON 对象，形状由 `event_type` 决定 |
| `created_at` | timestamp | 由**调用方**注入（`MD-6`：核心不用系统时钟做判定） |

## 2. `decision` 载荷

由核心的 `control.DecisionRecord` 序列化而来（Go 结构体 → JSON）。

| 键 | 类型 | 说明 |
| --- | --- | --- |
| `decision_id` | string | 判定幂等键（`ST-10`）；在 L4 里就是**证据 ID** |
| `source_ip` | string | 来源 IP（可信层已处理 `XFF`，`INT-23`） |
| `method` / `path` / `user_agent` | string | 请求身份（**攻击者可控**，`AR-31`：原样保留、不做净化） |
| `action` | string | 三值之一：`route_origin` / `route_mirage` / `block`（`terminology.md` §4） |
| `severity` | string | 旁路字段（档位未定，当前恒为 `none`） |
| `backend` | string | 仅改道时非空 |
| `score` | number | 风险分 —— **只进观测面**，禁止回显给客户端（`ST-7`） |
| `signals` | string[] | 命中信号 ID —— 同上只进观测面 |
| `at` | string | RFC 3339 时间（带偏移） |

**夹具（唯一事实源）**：[`../../api/telemetry/v1/testdata/decision_event.json`](../../api/telemetry/v1/testdata/decision_event.json) —— 由 Go 结构体直接生成。

## 3. `analysis` 载荷（L4 结论）

| 键 | 类型 | 说明 |
| --- | --- | --- |
| `kind` | string | 结论类型：`intent` / `strategy` |
| `accepted` | bool | 统一信封（`AR-16`） |
| `data` | object | `accepted=true` 时的结构化结论 |
| `rejected_reason` | string | `accepted=false` 时的拒绝原因（**必须**有） |
| `analyzed` | number | 本轮参与分析的事件数（`AR-14` 去重后） |
| `evidence_ids` | string[] | 引用到的 `decision_id` 集合（`AR-12`：写入前校验存在） |
| `event_id` | string | `analysis:<kind>:<内容摘要>` —— 由结论内容决定，重跑同窗口**不产生重复**（`AR-11`） |

## 4. 改键的规矩（防漂移）

改任何键名都**必须**同时做到三件事，否则门禁会红：

1. 改 `docs/spec/events.md`（本文）；
2. 重新生成夹具 `api/telemetry/v1/testdata/decision_event.json`；
3. 两侧契约测试同步：Go 侧 `core/internal/control/observer_contract_test.go`、Python 侧 `analysis/tests/test_event_contract.py`。

> 为什么这么严：L4 曾按**自造的字段名**解析，测试自洽但运行时一条也解不出（取到事件却 0 条可分析）。
> 契约测试就是那次事故的防线。

## 2.2 `request_judged` 载荷（适配器 → 核心）

适配器在**每次请求处理结束后**上报一条（异步、幂等：`event_id` = `decision_id`，`AR-11`）。

| 键 | 类型 | 说明 |
| --- | --- | --- |
| `decision_id` | string | **判定 id**（与核心 `decision` 事件同键）—— 控制台用它 join；事件信封的 `event_id` 则是**逐请求唯一**（`judged:<decision_id>:<序>`），避免同判定下的多条请求被幂等键折叠 |
| `method` / `path` / `ua` | string | 请求身份（攻击者可控，仅内部可见） |
| `action` | string | **核心判成什么**（`ACTION_ORIGIN` / `ACTION_MIRAGE` / `ACTION_BLOCK`）—— 意图 |
| `shadow` | bool | 是否影子模式（`INT-11`） |
| `executed` | string | **适配器实际走了哪** —— 见下表（图与验证页靠它区分意图/实际） |
| `backend` | string | 实际使用的幻境后端名（仅 `mirage` 时非空） |
| `status` | int | 返回给客户端的状态码（`block`=403） |
| `bytes` | int | 响应体字节数 |
| `duration_ms` | float | 从进入适配器到响应结束（毫秒） |
| `decision_error` | string | 判定失败原因（空 = 判定成功） |
| `inject` | string | **AI 欺骗内容注入的结果** —— 见下表（`ADR-0023` / `AR-33`）；与 `executed` 独立：`executed` 说“去了哪”，`inject` 说“我们改写了多少” |
| `content_id` | string | 实际注入的内容标识（空串 = **未注入**）；定位“这一条上的是哪份内容” |

`inject` 的四个取值（**唯一权威定义**，改它必须同步 `edge/proxy` 常量与本文）：

| 取值 | 何时 |
| --- | --- |
| `applied` | 改道侧响应被内容注入**实际改写**（至少发生一次改写） |
| `disabled` | 走到了改道侧，但注入开关关闭（下发级 `inject_enabled=false` 或本地 `SHEN_PROXY_INJECT_CONTENT=false`） |
| `no_content` | 开关开着、也在改道侧，但没有可用的内容（未命中资源 / 无该变体 / 校验和不符 / 非 HTML / 找不到标记） |
| `off` | **未涉及注入**：不是改道侧（放行 / 白名单 / 缓存 / 判定失败 / 拦截 / 幻境回落） |

`executed` 的七个取值（**唯一权威定义**，改它必须同步 `edge/proxy` 的 `executedFor` 与本文）：

| 取值 | 何时 |
| --- | --- |
| `whitelist` | 白名单命中，未调核心（`INT-25`） |
| `cache` | 本地判定缓存命中，未调核心（`ST-10`） |
| `failopen` | 调核心失败（超时/不可达），按 `NI-3`/`NI-4` 放行 |
| `origin` | 决策 `route_origin`（含影子模式下的一切处置） |
| `origin_fallback` | 决策 `route_mirage` 但后端未登记/不可达 ⇒ 回落源站（`NI-5`） |
| `mirage` | 决策 `route_mirage` 且成功转发到幻境后端 |
| `block` | 决策 `block`，返回 403 |

**夹具**：[`../../api/telemetry/v1/testdata/request_judged_event.json`](../../api/telemetry/v1/testdata/request_judged_event.json)（由 `edge/proxy` 的契约测试逐键比对）。
**消费者**：控制台的流量调度图与链路详情（`/api/topology` · `/api/trace`）—— 注入跳在 DAG 上只用 `inject=applied` + 非空 `content_id` 作为依据。
**L4 worker 不消费本事件**（它只读 `decision`），所以**无需** Python 侧改动。
