# 契约：控制台读面（控制台 ↔ 核心）

> **状态**：已实现（`ListEvents` 自阶段 1；**`GetCoreSnapshot` 于 2026-09-21 新增**）。
> 本文件是**跨进程契约**：控制台（Go 进程）读核心（Go 进程）的**只读**面。
>
> 规则依据：`AR-10`（控制面**禁止**参与请求级判定）· `AR-13` / `ST-8`（策略版本与对账）·
> `ST-7`（判定细节只进观测面，**禁止**回客户端）· `ST-20` / `ST-21`（密钥与真实配置**禁止**入库）·
> `ST-23`（阈值集中定义、可配置 —— 但**不**等于可以摊到契约面）· `MD-3`（跨模块契约放本目录）·
> `MD-12`（决策取值只在 `director` 定义一次）。
> 决策与失效条件：[ADR-0020](../background/decisions/0020-console-minimal-static-ui.md)（控制台形态与只读边界）。

---

## 0. 边界（先看这个）

| 项 | 约定 |
| --- | --- |
| **只读** | 控制台的任何接口**禁止**改核心状态。`GetCoreSnapshot` 与 `policy.Pull` 的关键差别：它**不**推进游标、**不**写任何东西 —— 刷新页面不会影响适配器的策略对账 |
| **不在请求路径上** | 控制台挂了不影响业务与判定（`AR-10` · `NI-1`） |
| **不参与判定** | 它只**展示**核心已经做完的判定 |
| **可以看见什么** | 判定细节（`score` / `signals` / `severity`）在观测面可见 —— 这是 `ST-7` 允许的：判据是**会不会出现在攻击者的屏幕上** |
| **禁止出现什么** | 契约里**禁止**出现：decision 三值枚举的**第二份定义**（`MD-12`）· 密钥（`ST-20`）· 白名单/隔离名单的**内容**（只给条数） |
| **鉴权** | 当前**无鉴权**、只绑 `127.0.0.1`（单实例）。生产化需定访问控制 —— 未解决项见 ADR-0020 |

---

## 1. 核心 gRPC 读面（`api/telemetry/v1`）

`shen/api/telemetry/v1/telemetry.proto` 的 `DeceptionTelemetry` 服务承载两个读方法：

| 方法 | 回答什么 | 有「时序」吗 |
| --- | --- | --- |
| `ListEvents` | 「刚才**发生了什么**」 | ✅ 事件流，带 `created_at` |
| `GetCoreSnapshot` | 「现在**按什么在跑**」 | ❌ 永远是「现在」，无历史 |

> 两者分工不要混：**要历史读事件，要当前态读快照**。把配置塞进事件流会让它就变成有时序的假象。

### 1.1 `GetCoreSnapshot`

请求无参数（快照就是「现在」，不接受时间窗）。响应 `CoreSnapshot`：

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `policy_id` | string | 策略集标识 |
| `policy_version` | uint64 | 策略版本（单调递增，回滚即发布新版本） |
| `policy_checksum` | string | 策略校验和（与核心启动日志、适配器回执对账用） |
| `rule_count` | int32 | 判定规则条数 |
| `whitelist_count` | int32 | 白名单**条数**（来源网段 + UA + 路径前缀，三者相加） |
| `ai_enabled` | bool | 能力级总开关（配置 `ai.enabled`，投影成载荷的 `inject_enabled`） |
| `ai_kinds` | string[] | 启用的任务种类（配置 `ai.kinds`） |
| `ai_model` | string | 模型后端标识；**空串合法**（阶段 A 的模板生成器不调模型） |
| `ai_manifest_path` | string | 内容清单路径（配置 `ai.manifest`）；空 = 未配 |
| `ai_content_variants` | int32 | 变体数 N（配置 `ai.content.variants`；**必须**与清单一致，装载期已保证） |
| `ai_rotate_cooldown` | string | 轮换冷却（Go duration 字面量，如 `30m0s`） |
| `ai_manifest_loaded` | bool | **是否真的装载出了内容**（`bodies > 0`） |
| `ai_manifest_version` | uint64 | 已装载清单的内容版本 |
| `ai_manifest_resources` | int32 | 已装载清单的资源数（`entries` 条数） |
| `ai_manifest_contents` | int32 | 已装载清单的内容条数（所有 `bodies` 之和） |

**两种容易被混淆的「没有内容」**（控制台据此给不同提示）：

| 情形 | `ai_enabled` | `ai_manifest_loaded` | 说明 |
| --- | --- | --- | --- |
| 未启用 | `false` | `false` | 默认态。资产仍在，但不新增、不下发 |
| **开了但没内容** | `true` | `false` | 配置写错了（没配 `ai.manifest`，或清单一条都没装进来）——**这是最该被看见的一种错** |

### 1.2 字段取舍的规矩（**加字段前必读**）

只有两类字段允许进快照：

1. **已经允许离开核心的** —— 策略版本 / 校验和 / AI 配置，它们本就在 `api/policy/v1` 的策略载荷或适配器回执里；
2. **比第一类更弱的描述性信息** —— 例如白名单**条数**（载荷里本来就有全量 CIDR 列表）。

因此**显式不含**：

| 不含 | 为什么 |
| --- | --- |
| 判定阈值（`thresholds.*`） | 核心运行参数。`core/internal/contract/thresholds.go` 明确禁止写进 `api/*.proto` 的对外响应；要看它们读 [`config.md`](config.md) |
| 灰度比例 / 误调度预算 | 同上（运行参数，且不在策略载荷里） |
| 影子模式标志 | 同上（它是模式，不是配置路径；但它决定了「现在有没有真的在处置」—— 见 §1.4 未解决） |
| 白名单 / 隔离名单的**内容** | 接入方的资产面；观测只需知道「有几条」 |
| 任何密钥 | `ST-20` / `ST-21` |
| 进程 `started_at` | 核心全库**零** `time.Now()`（`MD-6` 的纪律）—— 不为一个展示字段开这个口子。「是不是刚重启」由 `/api/summary` 的事件时间范围回答 |

### 1.3 失败语义

| 情形 | 核心返回 | 控制台显示 |
| --- | --- | --- |
| 未装配快照读侧 | `Unimplemented` + **不回快照** | 错误提示（**不是**全零配置） |
| 提供方内部错误 | `Internal` | 错误提示 |

> **为什么不回一份全零快照**：零值看起来完全正常（`version=0`、`variants=0`），
> 运维会以为引擎真的这么配的 —— **一份看起来正常的错数据比一个错误危险得多**（`AR-15` 的精神）。

### 1.4 `ListEvents`（简述）

参数：`limit`（0 = 服务端默认 200）· `since`（只返回该时刻之后）· `event_type`（只看某类，如 `decision`）。
事件载荷的字段字典在 [`events.md`](events.md) —— 本节不复制。

**未装配事件读侧时返回空列表**（不是错误）：控制台要能显示「暂无数据」，
而不是因为核心没接存储就整页报错。

---

## 2. 控制台 HTTP 只读面（`/api/*`）

全部 `GET`，全部只读。返回 JSON 的键一律 **snake_case**（与项目其余载荷一致）。

| 接口 | 参数 | 返回什么 | 数据来源 |
| --- | --- | --- | --- |
| `GET /` | —— | 静态页（`go:embed`，无构建步骤） | 二进制内嵌 |
| `GET /healthz` | —— | `ok`。**只反映本进程存活** —— 核心是否可达由页面上的错误提示体现（`ST-17` 的语义区分） | —— |
| `GET /api/summary` | —— | 概览：`total` · `by_action` · `alerts` · `l4_conclusions` · `first_seen` · `last_seen` | `ListEvents` |
| `GET /api/config` | —— | **核心只读快照**（§1.1 的字段，包在 `{policy, ai}` 两层里） | `GetCoreSnapshot` |
| `GET /api/events` | `limit` · `type` | 最近事件（原始视图） | `ListEvents` |
| `GET /api/flow` | `limit` | **逐判定**记录：`decision_id` / `source_ip` / `method` / `path` / `user_agent` / `action` / `severity` / `backend` / `score` / `signals` / `at`（与 `logs.md` §3 的字段表一一对应） | `ListEvents`（`type=decision`） |
| `GET /api/analysis` | `limit` | L4 结论事件（解成结构化字段） | `ListEvents`（`type=analysis`） |
| `GET /api/topology` | —— | 聚合的流量调度图（节点 / 边计数） | `ListEvents`（全部类型聚合） |
| `GET /api/graphs` | `limit` | **逐请求链路**：每请求一条独立 DAG（不聚合），最新在前 | `ListEvents`（`decision` + `request_judged` 按 `decision_id` join） |
| `GET /api/trace` | `decision_id` | 单请求**四段**：① 请求 ② 判定（核心）③ 执行与返回（适配器）④ 告警 | 同上 |

错误响应统一形状：`{"error": "<原因>"}` + HTTP `502`（核心不可达/报错时）。

---

## 3. 改键的规矩

改任何字段名都**必须**同时做到这三处，否则页面会静默显示 `—`（编译期看不出 JSON 键漂移）：

1. 改本文件；
2. 改提供方（核心 `core/internal/control/telemetry.go` 的映射 / `core/internal/contract/snapshot.go`）；
3. 改消费方（控制台 `console/cmd/console/main.go` 的 `*View` 结构 + `console/web/index.html` 的取键），
   并跑 `go test ./console/cmd/console/`（那里有一条键名测试钉住 snake_case）。

加**快照字段**前先过 §1.2 的两条规矩；过不了就不加，改由配置文档承载。
