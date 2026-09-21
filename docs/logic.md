# 全链路逻辑（蜜罐内容除外）

> **本文回答**：一个请求从进入引擎到业务回包，中间**每一步的逻辑、技术点、实现位置、验证方式**是什么。
> **范围**：接入 / 判定 / 决策 / 处置 / 观观测与遥测 / L4 近线 / 策略面 / 失败放行 —— **不含**蜜罐内部内容与协议栈
> （本项目按用户裁定只做蜜罐**接入架构**，见 [`design/modules.md`](design/modules.md) §1.1 第 14/15 行）。
> 规则权威在 [`design/`](design/README.md)；目录/能力总表在 [`modules/_map.md`](modules/_map.md)；怎么跑在 [`ops/runbook.md`](ops/runbook.md)。

---

## 1. 一张图：请求在引擎里怎么走

```text
客户端
  │  ① L0（客户自己的 LB / nginx / Envoy）：TLS 终结 · 路由        ← 复用现成组件（AR-3），TLS 默认交 L0（ADR-0019）
  ▼
适配器 L1（deception/proxy；③ 前置 / ④ 边车同一实现，① ② 形态见下文）
  │  ② 白名单？ ──命中──▶ 直接透传业务（不判定，INT-25）
  │  ③ 判定缓存？ ─命中─▶ 复用同窗判定（不调核心，ST-10）
  │  ④ 派生 decision_id（来源+会话+方法+路径+时间窗；幂等键）
  │  ⑤ 调核心判定（gRPC S1；deadline 3ms，AR-29）
  │        └─失败/超时─▶ 折叠为放行（NI-3/NI-4），落点记 failopen
  ▼
核心（core；判定与响应生成的唯一实现，AR-2 / AR-5）
  │  ⑥ judge：按配置规则逐条匹配 → 分值（权重求和，1.0 截断）+ 命中信号
  │  ⑦ director：阈值 → 三值（route_origin / route_mirage / block）+ 灰度 + 白名单/诱饵豁免，severity 旁路字段
  ▼
处置执行（回到适配器；影子模式下只观测不执行，INT-11）
  │  ⑧ route_origin → 业务源站**原样透传**（INT-8，业务响应零改写）
  │     route_mirage → 查改道后端表 → 转发幻境后端（+注入仅在改道侧）；后端不可用 → **回落业务**（NI-5）
  │     block        → 403（可见拦截；ADR-0002：block 只用于明确拒绝已知恶意）
  │  ⑨ 响应卫生：删 `Via`、删我们自己的默认 `Server`（OH-2；错误路径也删）
  ▼
遥测与观测面（异步，不阻塞请求）
  │  ⑩ 适配器上报 request_judged（executed / status / bytes / duration_ms / decision_error）
  │  ⑪ 核心记录判定：**事件**（供读侧）+ **判定归档**（store 是唯一 I/O 出口，MD-20）
  ▼
读侧与分析
  │  ⑫ 控制台（只读，AR-10）：概览 / 告警 / 流量与流动 / **逐请求链路（DAG）** / L4 结论
  │  ⑬ L4 近线 worker：读事件 → 态势去重（AR-14）→ 意图 → 攻击链（引用校验 AR-12）→ 策略数据 → 结论事件
  ▼
策略面（S4，核心 → 适配器）
     ⑭ Pull 轮询 + Ack 回执：改道后端表 / 白名单 / 注入规则（版本 + 校验和 + 回执，ST-8 / AR-13）
```

---

## 2. 分步逻辑：技术点 · 实现 · 验证

| # | 逻辑要点 | 技术点 | 实现位置 | 规则 / 取舍 | 怎么验 |
| --- | --- | --- | --- | --- | --- |
| ① | 接入与 TLS | 四种形态：① 旁路镜像（不在请求路径）② DNS 引流 ③ 反代前置 ④ 边车；**TLS 默认由客户 L0 终结**（指纹天然一致） | `deception/proxy` · `deception/mirror` · `deception/dns` · `deploy/docker/` | `AR-3`（不自研 L0）· `ADR-0019` · `INT-1…INT-10` | `scripts/shen.sh doctor`（② TLS 终结方式） |
| ② | 白名单先于判定 | 本地（env）与远端（策略面）白名单取**并集**；命中即不调核心 | `deception/proxy/handler.go`（`ServeHTTP` ⓪–①） | `INT-25`（护栏只增不减） | 链路里出现「白名单命中」跳（`SHEN_VERIFY_WHITELIST` 配方见 [`integrate/observability.md`](integrate/observability.md) §6） |
| ③ | 判定缓存 | 键 = `(来源, 会话, 方法, 路径)` + 时间窗；**同窗谁先到谁定调** | `deception/proxy/glue.go`（`decisionCache` / `decisionID`） | `ST-10` · 取舍 `K-20` | 同路径连发两条 → 第二条 `executed=cache` |
| ④ | 幂等键 | `decision_id` 由适配器派生、核心原样回显；事件 id 逐请求唯一（`judged:<id>:<序>`） | `deception/proxy/handler.go` · `docs/spec/events.md` §2.2 | `AR-11`（幂等） | `scripts/shen.sh traffic --check-graph` |
| ⑤ | 调核心判定 | gRPC 明文**仅回环**；判定预算 3 ms；建连在启动时**预热**（否则首请求超时后放行） | `deception/proxy/handler.go`（`decide` / `warmUp`） | `AR-29` · `NI-3`/`NI-4` · 取舍 `K-24` | 重建后首请求应 `失败=nil`；`doctor` ⑤ |
| ⑥ | 规则求值 | 字段 `user_agent`/`path`/`method`/`source_ip`/`tls_fingerprint`；算子 `equals`/`prefix`/`contains`；分值**1.0 截断**；⚠️ `path` **不含查询串**、按原始字符串匹配 | `core/internal/judge/` · `core/internal/policy/policy.go` | `ST-24`（规则是数据）· 缺口见 [`ops/functional-verification.md`](ops/functional-verification.md) §2 | `make replay`（离线回放）· DAG 第三步「响应」列 |
| ⑦ | 三值决策 | 阈值 `thresholds.route_mirage` / `thresholds.block`；**灰度**按 decision_id 哈希（0% 时改道会被降级为放行）；诱饵面**禁止 block**；`severity` 档位未定恒 `none` | `core/internal/director/director.go`（`classify` / `grayAllows`） | `MD-12` · `MD-25` · `INT-12` · 未决项见 [`spec/metrics.md`](spec/metrics.md) §2 | 验证覆盖（`SHEN_BLOCK_ENABLED=true` + `gray_pct: 100`）下看 DAG「决策」跳 |
| ⑧ | 处置执行 | 放行=原样透传；改道=查表→转发（可注入）；后端不可用**回落业务**；拦截=403 | `deception/proxy/handler.go`（`dispatch` / `forwardMirage`）· `deception/injection/` | `INT-8` · `NI-5` · `ADR-0002` | `executed` 七值实测（见 DAG 与 [`ops/functional-verification.md`](ops/functional-verification.md) §7.2） |
| ⑨ | 响应卫生 | 删 `Via`、删 Caddy 默认 `Server`；错误路由同样删 | `deception/proxy/handler.go`（`headerSanitizer`）· `embed.go`（`server.Errors`） | `OH-2` / `OH-5` | `scripts/traffic/send.py` 的响应头/响应体卫生检查（每场景都查） |
| ⑩ | 遥测上报 | 异步、批量、幂等；载荷带**实际落点与返回信息** | `deception/proxy/handler.go`（`reportRoute`）· [`spec/events.md`](spec/events.md) §2.2 | `AR-11` · `AR-6` | `make docker-log S=proxy`，看「路由：… 落点=…」 |
| ⑪ | 观测面记录 | 判定同时落成**事件**与**判定归档**；`store` 是核心唯一 I/O 出口 | `core/cmd/core/main.go`（观测面适配器）· `core/internal/store/` · `core/internal/control/` | `MD-20` · `ST-7`（细节只进观测面） | `make docker-log S=core`（`msg=decision` 逐判定） |
| ⑫ | 控制台读侧 | 只读接口：概览/流动/结论/**逐请求链路**/单请求详情；页面全 `textContent` | `console/cmd/console/` · `console/internal/topology/` · `console/web/` | `AR-10`（不参与判定） | `scripts/shen.sh verify` |
| ⑬ | L4 近线分析 | 读事件 → 去重（`AR-14`）→ 意图 → 攻击链（引用校验 `AR-12`）→ 策略数据 → 结论事件；**无执行能力** | `analysis/worker.py` · `analysis/{intent,chain,strategy,llm}/` | `ADR-0022` · `AR-31`/`AR-32` | `scripts/shen.sh traffic --check-l4` |
| ⑭ | 策略面下发 | 拉取式 `Pull` + 回执 `Ack`；载荷 = 改道后端表 / 白名单 / 注入规则；**远端优先、本地兜底** | `core/internal/policy/server.go` · `deception/proxy/policy.go` | `ADR-0018` · `ST-8` · `AR-13` | `make docker-log S=proxy`（`已应用策略 …`） |
| ⑮ | 会话与隔离 | 会话身份三级优先级（cookie > …）；隔离命中**短路**不再调核心（TTL） | `core/internal/session/` · `core/internal/isolation/` | `INT-19` · `INT-20` | 单测；运行时接缝状态见 [`modules/_map.md`](modules/_map.md) §3 |
| ⑯ | 失败与熔断 | 任何未识别状态**放行到真实业务**；服务面熔断（错误率超阈值 → 纯放行） | `core/internal/control/`（breaker）· 各适配器 fail-open 分支 | `NI-1`（最高优先级）· `NI-10` | `make gate` 内的 `V-1…V-4` 故障注入 |

---

## 3. 三条贯穿性原则（读任何一段逻辑前先记住）

| 原则 | 含义 | 反例（会被门禁或评审打回） |
| --- | --- | --- |
| **不影响原始业务**（`NI-1`） | 未识别 / 引擎故障 / 后端不可用 ⇒ **一律放行**；宁可漏判不可断业务 | 判定超时返回 5xx；幻境不可用返回 502 |
| **判定只在一处**（`AR-2` / `AR-5`） | L1 适配器只执行处置，**禁止**实现判定逻辑 | 适配器里写"如果是 scanner 就改道" |
| **判定细节只进观测面**（`ST-7`） | 分值 / 规则名 / 决策枚举**禁止**回给客户端 | 响应体或响应头带 score / 命中信号 |

---

## 4. 实现参考索引（按文件找职责）

| 位置 | 职责 |
| --- | --- |
| `core/cmd/core/main.go` | 装配：配置装载 → judge/director → 欺骗面 → 服务面 → 观测面（唯一允许依赖具体实现的地方） |
| `core/internal/control/` | gRPC 服务面：入参映射 · **禁止回显的强制点** · 熔断 · 观测面记录与读取 |
| `core/internal/judge/` · `director/` | 规则求值 → 分值/信号；阈值+灰度 → 三值 |
| `core/internal/policy/` | 策略装载 · 下发投影（Pull）· 回执账本（Ack） |
| `core/internal/{session,isolation,telemetry,store}/` | 会话身份 · 隔离短路 · 事件上报 · **唯一 I/O 出口** |
| `core/internal/{decoy,responder,honeypot}/` | 欺骗面（诱饵资产 · 欺骗响应一致性 · 幻境后端池）—— 内容层不在本文范围 |
| `deception/proxy/` | ③④ 适配器：白名单 → 缓存 → 判定 → 处置 → 上报；转发与 TLS 用内嵌 Caddy（`ADR-0017`） |
| `deception/{injection,mirror,dns}/` | 注入引擎 · ① 旁路镜像接收端 · ② DNS 引流配置 |
| `analysis/`（Python） | L4：llm 契约层 · intent · chain · strategy · 近线 worker |
| `console/` | 只读观测台 + 拓扑聚合（纯函数包 `console/internal/topology/`） |
| `api/` | 契约唯一事实源（judge / policy / telemetry 三个 `.proto`） |
| `scripts/` | 门禁（archcheck/tracecheck/check-leak/licensecheck）· 演示环境 · 伪造流量 · 接入自检（doctor） |

---

## 5. 现象 → 看哪（排障入口）

| 现象 | 先看 |
| --- | --- |
| 某条请求为什么被判成这样 | 控制台 DAG 点「核心判定」那步（分值 + 命中信号），或 `make docker-log S=core \| grep msg=decision` |
| 判成改道却仍在业务源站 | DAG「决策 / 幻境后端」两跳：影子模式（`INT-11`）或回落（`NI-5`） |
| 请求没被判定 | DAG「适配器」跳：白名单 / 缓存命中 / 判定失败（带原因） |
| 引擎有没有影响业务 | `scripts/shen.sh traffic`（每场景断言状态码）· `make gate` 的 `V-1…V-4` |
| 规则没命中 / 命中不对 | `make replay` 回放；规则字段与算子语义见 [`spec/config.md`](spec/config.md) §2.4 |
| L4 没出结论 | 得先有新态势（`AR-14` 去重，默认 60s 窗）；`scripts/shen.sh traffic --check-l4` |

---

## 6. 明确不在本文范围

1. **蜜罐内容与协议栈**（`honeypot/protocol/` 只做协议注册表 / 运行框架 / 最小适配器；真实协议库待专项调研）；
2. **L3 网络欺骗细节**（微隔离 / 假拓扑 / 运行时检测是**声明式产物**，复用 Cilium/Tetragon —— 见 [`deception/netpolicy/README.md`](../deception/netpolicy/README.md)）；
3. 各模块的契约、失败路径与测试（在 [`modules/<模块>.md`](modules/) 的九章文档里）；
4. 每一条规则正文（在 [`design/`](design/README.md)，本文只引用 ID）。
