# 变更包：流量调度 DAG 图（意图 vs 实际落点）+ 告警在图中展示

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 控制台新增「流量调度图」：把请求进来后**实际走到哪**（业务源站 / 幻境后端 / 拦截 / 白名单 / 缓存 / 失败放行）画成 DAG，并区分**核心意图**与**适配器实际执行**；告警与高风险在图中标注 |
| 日期 | 2026-09-20 |
| 状态 | 已验证（图接口与链路详情接口**实跑有数据**；拓扑单测通过；适配器落点穷举测试通过；`make gate` 通过） |
| 改动分级 | **L**（跨层契约扩展 + 适配器响应观测 + 控制台新模块与页面） |
| 涉及范围 | `edge/proxy/` · `api/telemetry/v1/testdata/` · `console/internal/topology/`（新增）· `console/cmd/console/` · `console/web/` · `docs/spec/events.md` · `docs/spec/metrics.md` · `docs/modules/adapter-proxy.md` · `docs/modules/console.md` · `docs/integrate/observability.md` · `docs/kb/` |
| 决策数 | 已答 3 项（扩展契约 · 聚合拓扑+下钻 · 两段式告警）/ 未做 1 项（见 §7） |
| 关联 | `INT-11` · `NI-3` · `NI-5` · `NI-1` · `AR-10` · `AR-11` · `ST-7` · `ST-10` · `INT-25` · [`../spec/events.md`](../spec/events.md) §2.2 |

---

## 1. 需求与验收

**用户要什么**：能看到流量进入引擎后的**调度方向** —— 是走到后段业务服务，还是进了我们设的蜜罐/幻境；
用 DAG 展示**链路 + 请求/返回信息**；触发的**监控告警**也要在图中展示；用于功能验证。

**验收判据**：

1. 图上能区分 **意图**（核心 `action`/`score`/`signals`）与 **实际落点**（适配器 `executed`），含 `origin` / `mirage` / `origin_fallback`（`NI-5`）/ `block` / `whitelist` / `cache` / `failopen`；
2. 单请求详情有**四段**：请求 · 判定 · 执行与返回（状态码/字节/耗时）· 告警；
3. 告警在图上可见：真实告警（`block` 或 `severity≠none`）+ 高风险显示标记（分值阈值，仅显示）；
4. 影子模式、未登记幻境后端、缺数据等情形**如实标注**（`notes`），不给假绿；
5. `make gate` 绿。

**不做什么**：不引前端库/构建步骤（`ADR-0020`）· 不做实时推送 · 不改判定与处置语义 · 不把告警判定引入控制面（`AR-10`）。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | 数据从哪来 | **扩展 `request_judged` 契约**：`executed` / `backend` / `status` / `bytes` / `duration_ms` | 原先只有核心"判成什么"，没有"实际走了哪"与返回信息 —— 图会骗人 |
| ② | 落点怎么定 | 纯函数 `executedFor(shadow, action, mirageFound, mirageFellBack)`，七值穷举单测 | 影子模式优先（`INT-11`）、后端不可用回落（`NI-5`）都必须钉死 |
| ③ | 响应观测放哪 | 复用已有 `headerSanitizer`（它已包住整个请求的 `ResponseWriter`）加状态码与字节计数 | 不再加一层包装；不引入额外 I/O，延迟影响可忽略（`AR-29`） |
| ④ | 图怎么组织 | **聚合拓扑 + 下钻**（节点/边带计数，点开看请求列表与详情） | 数十条场景流量下仍可读；逐请求泳道会失去聚合视角 |
| ⑤ | 告警口径 | **两段式**：真实告警沿用现行定义；"高风险"仅显示（默认 0.9，`SHEN_CONSOLE_ALERT_SCORE`） | `severity` 档位未定 ⇒ 真实告警恒 0；但运营需要看到"高分未处置"的流量 |

---

## 3. 追溯矩阵

| 规则 | 落实 | 验证 |
| --- | --- | --- |
| `INT-11` | 影子模式 `executed=origin` + 图顶部提示；`executedFor` 单测 | `TestExecutedFor/影子模式一律记_origin（只观测）` |
| `NI-5` | 回落记为 `origin_fallback`，图上虚线边 | `TestExecutedFor/后端未登记/中途失败 ⇒ 回落源站` · `TestBuildFallbackAndBlock` |
| `NI-3` | 判定失败 `executed=failopen` + 节点附失败原因 | `TestBuildWithoutCoreDecision` |
| `NI-1` | 上报仍异步（响应结束后投递），不阻塞请求 | 代码路径 `reportRoute` |
| `AR-10` | 图与两个接口**只读**；高风险阈值仅显示 | 代码评审 + `docs/modules/console.md` §9.1 |
| `AR-11` | 事件 `event_id` = `decision_id`（幂等） | `enqueueEvent` |
| `ST-10` / `INT-25` | `cache` / `whitelist` 单独成节点，且**没有核心判定**也要画 | `TestBuildWithoutCoreDecision` |
| `ST-7` | 判定细节仍只在观测面（页面与接口），不进响应 | 既有约束未变 |
| 契约纪律 | 契约文档 + 夹具 + Go 契约测试三处同步 | `TestRequestJudgedWireContract` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `edge/proxy/handler.go` | 改 | `headerSanitizer` 加状态码/字节观测；`ServeHTTP` 统一走 `reportRoute`（白名单/缓存也上报）；`dispatch` 返回实际落点；`forwardMirage` 返回是否回落；新增 `executedFor` / `routeInfo` / `judgedEventPayload` |
| `edge/proxy/wire_test.go` | 新增 | 契约测试（载荷键集与夹具逐键一致）+ `executedFor` 七值穷举 |
| `api/telemetry/v1/testdata/request_judged_event.json` | 新增 | 跨语言契约夹具 |
| `console/internal/topology/topology.go` | 新增 | 纯函数聚合：join（`decision_id`）· 落点归并 · 告警分级 · 边计数 · `notes` |
| `console/internal/topology/topology_test.go` | 新增 | 意图/实际配对 · 回落与拦截 · 无核心判定的三种请求 · 空输入 |
| `console/cmd/console/main.go` | 改 | `/api/topology` · `/api/trace` · `alertScore()`（默认 0.9，非法值回落） |
| `console/web/index.html` | 改 | 「流量调度图（DAG）」区块：SVG 分层布局 + 图例 + 过滤 + 下钻列表 + 四段详情面板（全 `textContent`） |

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 |
| --- | --- | --- | --- |
| 1 | `executedFor` 七种取值 | 全覆盖且正确 | ✅ 7/7 通过 |
| 2 | `request_judged` 契约 | 键集与夹具一致 | ✅ 契约测试通过 |
| 3 | 拓扑聚合 | 意图/实际配对、回落虚线、告警分级、缺数据 | ✅ 4 个用例通过 |
| 4 | `/api/topology` 实跑 | 有节点/边/notes | ✅ `节点 4 边 3` · `requests=2 to_origin=2` · notes 含影子模式提示 |
| 5 | `/api/trace` 实跑 | 四段齐全 | ✅ 请求（method/path/ua）· 判定（action/score/signals）· 执行（executed/status/bytes/duration_ms）· 告警 |
| 6 | 页面渲染纪律 | 无 `innerHTML` 拼接 | ✅ `innerHTML` 仅出现在注释里 |
| 7 | `make gate` | 绿 | ✅ |

**没有覆盖的**：图在**真实改道**下的样子（需要非影子 + 登记可用幻境后端）· 页面的人工视觉检查（未截图）· `--check-graph` 一致性断言（见 §7）。

---

## 6. 验证证据

```console
$ go test ./console/... ./edge/proxy/ -run "Topology|ExecutedFor|WireContract"
ok  shen/console/internal/topology   0.170s
--- PASS: TestExecutedFor (7/7)

$ curl -s "http://127.0.0.1:19444/api/topology?limit=200"
节点 4 边 3 | totals: {'requests': 2, 'to_origin': 2}
   adapter → judge   n=2 kind=normal
   client  → adapter n=2 kind=normal
   judge   → origin  n=2 kind=normal
   注: 当前影子模式（INT-11）：意图已判，但执行仍在业务源站
   注: 没有任何请求进入幻境后端（未登记可用幻境后端，或处于影子模式）

$ curl -s "http://127.0.0.1:19444/api/trace?decision_id=c4a3…"
① 请求  : {'method': 'GET', 'path': '/', 'ua': 'Mozilla/5.0'}
② 判定  : {'action': 'route_origin', 'score': 0, 'signals': []}
③ 执行  : {'executed': 'origin', 'status': 200, 'bytes': 39, 'duration_ms': 44.47}
④ 告警  : {'real': False, 'high_risk': False}
```

**关键指标**：新增契约字段 **5** 个 · 新增纯函数包 **1** 个（含 4 用例）· 新增接口 **2** 个 · 新增页面区块 **1** 个 · 新增夹具 **1** 份 · 落点穷举覆盖 **7** 值。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | **`--check-graph` 一致性断言未实现**（原计划：拓扑计数 ↔ `/api/flow` 条数） | 图的计数缺少自动化一致性校验 | 下一轮补在 [`../../scripts/traffic/send.py`](../../scripts/traffic/send.py) 并纳入 `scripts/shen.sh verify` |
| 2 | 未在**真实改道**下验证过图（无可用幻境后端） | `mirage` 分支只有单测覆盖 | 配一个后端 + 关影子后手工核对 |
| 3 | 图未做真机视觉检查（无截图能力） | 布局在窄屏/深色主题下未评估 | 人工打开页面确认 |
| 4 | `docs/progress.md` 的 `console` 行未改（内容已是最新） | 无 | — |
| 5 | 事件量大时拓扑只取最近 `limit` 条 | 更早流量不入图 | 接真实存储（`NI-13`）后再谈时间窗 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 只画核心判定会导致"看着已改道、其实仍走源站"的**错误结论** | 设计缺陷（会误导验证） | 影子模式下 `action=route_mirage`、执行仍是 origin | 扩展契约 + 图上区分意图/实际 + 页面显式提示 | ✅ |
| 2 | `Node.add` 缺失（方法定义在 `counter` 上） | 编译错误（自伤） | `go build` 报 11 处 | 补 `Node.add` | ✅ |
| 3 | `buildNotes(in Input, …)` 未用参数 | 静态检查 | 工具报 unused parameter | 去掉参数 | ✅ |
| 4 | console 的 `topology` import 未加上（补丁守卫条件写反） | 编译错误（自伤） | `go build` 报 undefined: topology | 用 edit 精确补 import | ✅ |
| 5 | 白名单/缓存请求此前**根本不上报事件** | 缺口（图上会缺整条分支） | 读 `ServeHTTP` 原实现：白名单分支直接 return | 统一走 `reportRoute`（三条分支都上报） | ✅ 单测覆盖 |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-20 | `request_judged` 契约扩展（落点 + 返回信息）· 适配器响应观测与 `executedFor` · 控制台拓扑聚合包与两个只读接口 · 页面新增流量调度 DAG（含下钻与四段详情）· 文档同步 7 处 | 用户要求（流量调度显示 · DAG 展示链路与请求返回 · 告警入图）· `INT-11` · `NI-3` · `NI-5` · `AR-10` · `AR-11` · `ST-10` |
