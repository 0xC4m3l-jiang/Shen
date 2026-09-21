# 模块：`console`

| 项 | 内容 |
| --- | --- |
| 模块名 | `console` |
| 所属层 | `控制台`（依据 MD-1） |
| 实现语言 | `TypeScript`（依据 [`../design/language.md`](../design/language.md) §1） |
| 负责人 | — |
| 状态 | ✅ **已实现（最小可用）**（阶段 2b）：Go 进程 + 静态页（`go:embed`，**无前端构建步骤**）；只读观测 · `docs/integrate/observability.md` 是使用说明 |
| 最后更新 | 2026-09-21 |

---

## 1. 职责

**做什么**：

- **策略编排**：查看 / 编辑 / 发布策略（经策略平面，`ST-8`）；
- **诱饵资产管理**：查看 / 投放 / 轮换 `decoy` 的诱饵资产；
- **蜜罐管理**：查看 `honeypot` 的后端池与类型开关（`config` 驱动的只读视图 + 开关）；
- **人工干预**：白名单、隔离名单的查看与解除；
- **审计查询**：攻击流量、会话、凭证、攻击链、执行历史。

**明确不做什么**：

- **不做判定 / 决策**（`AR-2` / `MD-12`）—— 只展示与管理；
- **不直接改核心状态** —— 写操作**必须**走策略平面并留审计；
- **不把重逻辑塞进控制台**（`ST-16`）。

## 2. 输入 / 输出契约

**权威在 [`../spec/console-api.md`](../spec/console-api.md)，本节不复制**（`MD-3`）：

| 方向 | 契约 | 状态 |
| --- | --- | --- |
| **输入**（读核心） | 核心 gRPC 读面：`ListEvents`（历史）· `GetCoreSnapshot`（当前态） | ✅ 已实现（快照 2026-09-21 新增） |
| **输出**（页 / HTTP） | 控制台 10 个**只读** HTTP 接口（含 `/api/config`） | ✅ 已实现 |
| 输入 / 输出（**设计意图**） | 策略平面（`api/policy/v1`）的 `Pull` / `Ack` —— 策略编排所需的**写能力** | ❌ **未实现**：控制台当前只读（[ADR-0020](../background/decisions/0020-console-minimal-static-ui.md) 决定 2）；要做得先重开它 |

## 3. 依赖

| 允许依赖 | 原因 |
| --- | --- |
| 策略平面（`api/policy/v1`） | 唯一的写入通道 |
| 只读查询接口 | 展示 |

| 禁止依赖 | 原因 |
| --- | --- |
| 直接写数据库 | 写操作**必须**经策略平面并留审计 |
| 判定 / 决策逻辑 | `AR-2` / `MD-12` |

## 4. 关键规则

| 规则 | 与本模块的关系 |
| --- | --- |
| `ST-8` | 策略**必须**版本化 + 回滚；控制台是发布入口 |
| `ST-16` | **禁止**把重逻辑（LLM 推理、蜜罐仿真）塞进控制台 |
| `NI-13` | 取证数据的保留天数按类别配置 |
| `BA-1` | 在法务结论完成前，**禁止**把取证型组件投入生产 |
| `OH-1` | 控制台可见面**禁止**含禁用串（若控制台会被攻击者看到） |

## 5. 状态与生命周期

| 状态 | 存在哪里 | 生命周期 | 多副本一致性 |
| --- | --- | --- | --- |
| 无（前端状态） | 浏览器会话 | 会话期 | —— |
| 策略发布记录 | `store.PolicyStore` | 版本只增 | 强一致 |

## 6. 失败模式与降级

| 失败情形 | 行为 | 是否满足 `NI-1` | 依据 |
| --- | --- | --- | --- |
| 策略发布失败 | 保留旧版本，报错 | ✅ | `ST-8`（回滚） |
| 后端不可达 | 只读视图显示陈旧数据并标注 | ✅ | —— |

## 7. 测试

| 类型 | 覆盖什么 | 位置 |
| --- | --- | --- |
| 集成（人工） | 起核心 + 控制台 → 经引擎发流量 → 页面/`api` 能看到**分值 + 命中信号** | [`../ops/functional-verification.md`](../ops/functional-verification.md) §7 |
| 接口（形状） | `/api/summary` · `/api/flow` · `/api/events` · `healthz` 的返回形状 | 实跑（`scripts/demo/run.sh`） |
| 单元 | **`/api/config` 的键名与失败语义**：snake_case 键齐备；核心未装配快照时**返回错误而不是全零配置** | [`../../console/cmd/console/config_test.go`](../../console/cmd/console/config_test.go) |
| 单元 | 核心侧快照 RPC：未装配 ⇒ `Unimplemented` · 逐字段映射 · 提供方报错 ⇒ `Internal` | [`../../core/internal/control/telemetry_test.go`](../../core/internal/control/telemetry_test.go)（`TestTelemetryService_Snapshot*`） |
| 安全 | 页面渲染攻击者可控字符串用 DOM + `textContent`（**禁止** `innerHTML` 拼接） | [`../../console/web/index.html`](../../console/web/index.html) 顶部注释 |

> ⚠️ **语言偏离设计**：设计写 TypeScript，本轮实现为 Go + 静态页（按用户裁定不引入前端工具链）——
> 见 [`../background/decisions/0020-console-minimal-static-ui.md`](../background/decisions/0020-console-minimal-static-ui.md)。


| 类型 | 覆盖什么 | 位置 |
| --- | --- | --- |
| 实跑 | 页面与全部只读接口（个数与参数以 [`../spec/console-api.md`](../spec/console-api.md) §2 为准，本节不重复计数） | `console/web/index.html` + `scripts/demo/run.sh`（见 [`../ops/functional-verification.md`](../ops/functional-verification.md) §7） |
| 集成 | 策略发布 → 回执对账 | 同上 |
| 契约 | 与 `api/policy/v1` 的一致性测试（`ST-6`：文档由 proto 生成） | 同上 |

## 8. 未决项

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | 前端框架与组件库选型 | 实现 | 实现期定（用户决定） |
| 2 | 策略编辑界面与 `policy` schema 的耦合方式 | 实现 | 与 `policy` 实现一起定 |

## 9. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 创建（设计）：策略编排 + 资产管理 + 审计查询 | 既有清单（`modules.md` §1.1） |
| 2026-09-21 | **新增「配置」块**（读核心只读快照 `GetCoreSnapshot`）+ **逐判定日志补全字段**（`decision_id` / `severity`）与「看链路」入口；新增 `../spec/console-api.md` 把读面契约收成一处 | [`../plans/2026-09-21-console-config-log.md`](../plans/2026-09-21-console-config-log.md) · [`../spec/console-api.md`](../spec/console-api.md) |

## 9.1 流量调度图（DAG）与链路详情（2026-09-20 新增）

| 项 | 内容 |
| --- | --- |
| 接口 | **`GET /api/graphs?limit=N`（逐请求链路，页面主视图）** · `GET /api/topology?limit=N`（聚合视图，程序化用）· `GET /api/trace?decision_id=...`（单请求四段详情） |
| 聚合逻辑 | [`../../console/internal/topology/`](../../console/internal/topology/)（纯函数 + 单测；join 键 = `decision_id`） |
| 逐步详情 | 链路里**每一步可点**：显示该步的「请求 / 响应 / 为什么执行」三段（数据随 `/api/graphs` 一起返回，点开无需再请求） |
| 页面 | **每条请求一张横向小图**（客户端 → 适配器 → 分支/核心判定 → 决策 → 实际落点 → **内容注入**），每跳写该请求自己的值；5 秒刷新（新流量在最上）；筛选：告警/高风险/幻境/源站；点卡片看四段详情；**禁止 `innerHTML`** |
| 内容注入跳 | 仅当逐请求事件的 `inject=applied` 且 `content_id` 非空时出现（`ADR-0023`）；没注入时**不编出一跳** —— 实情留在 `inject` 字段（`disabled` / `no_content` / `off`）里（单测：`TestInjectionHopAppearsOnlyWhenApplied`） |
| 告警口径 | 真实告警 = `block` 或 `severity≠none`（不变）；「高风险」= `score ≥ SHEN_CONSOLE_ALERT_SCORE`（默认 0.9）**仅显示**、不改变处置（`AR-10` 边界未变） |
| 已知限制 | 影子模式下真实告警恒为 0（`severity` 档位未定）；没登记幻境后端时图上不会出现幻影分支 —— 页面用 `notes` 明确写出这些事实 |
