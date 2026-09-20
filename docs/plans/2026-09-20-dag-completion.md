# 变更包：DAG 图示收口（逐请求唯一事件 · 真实落点可观测 · 判定标签修正）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 完成 DAG 图示实现：让**每条流量独立成图**（修掉同判定下请求被幂等键折叠）、补上 join 缺失导致"意图整列丢失"的真因、修正决策标签与告警分类（载荷用设计术语而非枚举名），并用真实环境验证到能验的分支 |
| 日期 | 2026-09-20 |
| 状态 | 已验证（`origin` / `cache` / `failopen` / `origin_fallback` 四值实测 + `route_mirage` 意图实测；单测 6 例；`make gate` 通过） |
| 改动分级 | **L**（契约语义 + 适配器 + 控制台 join + 测试） |
| 涉及范围 | `edge/proxy/` · `api/telemetry/v1/testdata/` · `console/internal/topology/` · `console/cmd/console/` · `deploy/` · 文档 4 处 |
| 关联 | `AR-11` · `AR-12` · `NI-3` · `NI-5` · `INT-11` · `ST-10` · `docs/spec/events.md` §2.2 |

---

## 1. 需求与验收

**用户要什么**：完成 DAG 图示（前序要求：动态 · 每条流量单独 · 每步可点看请求/响应/为什么）。

**验收判据**：

1. 同一路径的多条请求**各自成图**（不再被折叠）；
2. 图上"意图"（核心判定）与"实际落点"都能显示（join 成立）；
3. 决策节点显示**真实决策**（改道/放行/拦截），真实告警与高风险分类正确；
4. 能验证到的执行落点都实测：`origin` · `cache` · `failopen` · `origin_fallback`（`NI-5`）；
5. `make gate` 绿。

---

## 2. 三个真缺陷（本轮修掉，均由实跑暴露）

| # | 缺陷 | 证据 | 修法 |
| --- | --- | --- | --- |
| 1 | 同判定下的多条请求共用事件 id ⇒ 被幂等键（`AR-11`）折叠成一条 | 三条同路径探针在图上只剩一条 | 事件 id 改**逐请求唯一**（`judged:<decision_id>:<序>`），`decision_id` 进载荷 |
| 2 | 控制台按总条数取事件 ⇒ "判定+执行"两份事件抢同一窗口，join 落空、**意图整列缺失** | 图上 `intent=(未join)`、分值恒 0 | 按**类型分别取数**再合并（`fetchObservations`） |
| 3 | `JudgedEvent.DecisionID` 标签仍是 `json:"-"` | 单测直接抓到：`decision_id 丢了` | 标签改为 `json:"decision_id"`，并加**真实载荷**用例防回归 |
| 4 | 常量写成枚举名（`ACTION_MIRAGE`），而载荷是设计术语（`route_mirage`）⇒ 决策标签恒"放行"、告警恒 0 | 实测 `intent=route_mirage` 而节点显示"放行" | 常量改为设计术语，并加真实载荷用例 |

---

## 3. 追溯矩阵

| 规则 | 落实 | 验证 |
| --- | --- | --- |
| `AR-11` | 事件 id 逐请求唯一同时仍幂等（同一请求重复上报不重复记） | 事件 id 形态实测 |
| `ST-10` | 缓存命中的请求各自成图（`executed=cache`） | 实测 3 条 |
| `NI-3` | `failopen` 成图并带失败原因 | 实测（重建后首请求 DeadlineExceeded） |
| `NI-5` | `origin_fallback` 成图，链路多一跳"幻境不可用" | 实测（后端 502 dial refused） |
| `INT-11` | 影子模式下意图与实际并列可见 | 实测 |
| 契约纪律 | 契约文档 + 夹具 + 契约测试三处同步 | `TestRequestJudgedWireContract` |

---

## 4. 产物

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `edge/proxy/handler.go` | 改 | 事件 id 逐请求唯一（`eventSeq`）+ 载荷带 `decision_id` |
| `api/telemetry/v1/testdata/request_judged_event.json` · `edge/proxy/wire_test.go` | 改 | 夹具加 `decision_id`；契约测试同步 |
| `console/internal/topology/topology.go` | 改 | `JudgedEvent.DecisionID` 标签修正；`action` 常量改为设计术语（`route_origin`/`route_mirage`/`block`） |
| `console/internal/topology/topology_test.go` | 改 | 新增 join 用例 + **真实载荷**用例（设计术语 action、block 计告警） |
| `console/cmd/console/main.go` | 改 | `fetchObservations`：三类事件分别取数后合并 |
| `deploy/config/config.verify-mirage.yaml` · `deploy/docker/compose.verify-mirage.yaml` | 新增 | 验证改道/回落的配方（非影子 + 灰度 100 + 后端表），供后续复验 |
| 文档 4 处 | 改 | 验证配方与实测结论 · 落点各分支的验证状态 · 事件 id 语义 |

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 |
| --- | --- | --- | --- |
| 1 | 同路径 3 条请求 | 3 条独立链路 | ✅ 3 条（前 2 条 `cache`） |
| 2 | 核心侧改道 | `intent=route_mirage` + 后端名 | ✅ |
| 3 | 幻境不可用回落 | `executed=origin_fallback` + 链路含"幻境不可用" | ✅ |
| 4 | 判定失败放行 | `executed=failopen` + 原因 | ✅ |
| 5 | 决策标签 | 显示改道/放行而非恒放行 | ✅ `决策[改道（route_mirage）]` |
| 6 | 单测 | 6 例通过（含真实载荷） | ✅ `go test ./console/...` |
| 7 | `executed=mirage` | 真正进入幻境后端 | ⚠️ **未观测到**（本机无可达后端：`dial tcp 127.0.0.1:19080: connection refused`） |
| 8 | `make gate` | 绿 | ✅ |

---

## 6. 验证证据

```console
$ curl -s "http://127.0.0.1:19444/api/graphs?limit=4"     # 验证配置（非影子 + 灰度 100）
/.git/HEAD     intent=route_mirage score=0.90 executed=origin_fallback backend=mirage
   客户端 → 适配器 (L1) → 核心判定[分值 0.90 · 信号 ua-headless,path-probe] → 决策[改道（route_mirage）]
     → 幻境不可用[回落业务（NI-5）] → 业务源站[200 · 0 字节 · 1.4ms]
/warmup7       intent=route_origin score=0    executed=origin
   客户端 → 适配器 (L1) → 核心判定[分值 0.00] → 决策[放行（route_origin）] → 业务源站

$ proxy 日志：引流后端不可达，回落业务：HTTP 502: dial tcp 127.0.0.1:19080: connect: connection refused

$ go test ./console/...   → ok（含 TestDecisionPayloadUsesDesignTerms · TestJudgedEventUnmarshalKeepsDecisionID）
$ make gate               → 门禁通过。
```

**关键指标**：修复真缺陷 **4** 个 · 新增单测 **2** 例（真实载荷 + join）· 实测覆盖 `executed` **4/7** 值（`origin`·`cache`·`failopen`·`origin_fallback`）+ 意图 `route_mirage` · 验证配方 **2** 份入库。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | `executed=mirage` 与 `whitelist` 未实测 | 这两条分支只有单测 | 接一个真实可达的蜜罐后端（或让演示站真正监听）后复验 |
| 2 | `--check-graph` 一致性断言仍未实现 | 链路计数缺自动核对 | 下一轮 |
| 3 | 验证配置 `shadow: false` 属危险设置 | 误用会真的执行改道 | 文件头已写醒目警告；只在验证时用 |
| 4 | 事件 id 形态变化对旧数据的兼容 | 旧事件 id 就是 decision_id | 控制台保留兼容路径（回落到 event_id） |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 三条同路径请求被折叠成一条 | 真缺陷（与"每条流量单独"要求冲突） | 图上只有 1 条 | 事件 id 逐请求唯一 | ✅ 3 条 |
| 2 | 意图整列缺失（join 落空） | 真缺陷 | `intent=(未join)` | 按类型取数 + 标签修正 | ✅ |
| 3 | 标签 `json:"-"` 让 decision_id 被丢弃 | 真缺陷（我的补丁曾"报告成功"但没生效） | 单测 `decision_id 丢了` | 改标签 + 用例 | ✅ |
| 4 | 常量用枚举名，载荷用设计术语 | 真缺陷（标签恒"放行"、告警恒 0） | `intent=route_mirage` vs 节点"放行" | 改常量 + 真实载荷用例 | ✅ |
| 5 | 测试自己用枚举名构造输入 ⇒ 自洽但错 | 测试设计缺陷 | 上述 #4 未被测试发现 | 新增**真实载荷**用例 | ✅ |
| 6 | 我此前用 `--build ... >/dev/null` 掩盖了构建是否生效 | 自伤（诊断成本） | 复验时发现跑的是旧镜像 | 改为显式 `build` 并看输出 | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-20 | DAG 收口：事件 id 逐请求唯一 · 控制台按类型取数 · 判定 id 标签修正 · action 常量改设计术语 · 新增 2 例真实载荷单测 · 验证配方入库（改道/回落）· 文档 4 处同步 | 用户要求（完成 DAG 图示）· `AR-11` · `ST-10` · `NI-3` · `NI-5` · `INT-11` |
