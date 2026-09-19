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
