# 能力实况（实现了什么 · 怎么验 · 缺口在哪）

> **本文是"能不能用"的实况表**，只写**已核实的事实**（代码位置 + 测试 + 可复跑命令），并**明确标出缺口**。
> 规则权威在 [`../design/`](../design/README.md)；逐模块契约在 [`../modules/`](../modules/_map.md)；操作在 [`../ops/runbook.md`](../ops/runbook.md)。
> 最近一次审计：2026-09-20（逐项取证据，非印象）。

---

## 1. 五个核心维度

### 1.1 反向代理（L1）✅ 已实现

| 项 | 内容 |
| --- | --- |
| 代码 | [`deception/proxy/`](../../deception/proxy)（14 个 Go 文件 · **58 个测试函数**） |
| 技术点 | 内嵌 Caddy 承担转发与 TLS（`ADR-0017`），本模块实现 Caddy 中间件 `http.handlers.shen_proxy`；上游为 `SHEN_PROXY_UPSTREAM` |
| 已实现的行为 | 白名单先于判定（`INT-25`）· 本地判定缓存（`ST-10`）· 调核心判定（gRPC `S1`，deadline 3ms `AR-29`）· 三值处置 · 异步上报（`AR-6`）· 失败放行（`NI-3`/`NI-4`）· 响应头卫生（`OH-2`）· 建连预热（`K-24`） |
| 形态覆盖 | ③ 反向代理前置 与 ④ Sidecar 是**同一份实现**；① 旁路镜像在 [`deception/mirror/`](../../deception/mirror)；② DNS 引流是**纯配置**（[`deception/dns/`](../../deception/dns)） |
| 怎么验 | `scripts/shen.sh up` → 开控制台看 DAG；`make docker-log S=proxy`（看「路由：… 落点=…」）；`go test ./deception/proxy/` |
| 边界已锁 | WebSocket 101 · SSE 不缓冲 · 2 MiB 响应不注入 · 8 MiB 上传不丢字节（见 [`forwarding_test.go`](../../deception/proxy/forwarding_test.go)） |

### 1.2 流量转发 ✅ 已实现

| 项 | 内容 |
| --- | --- |
| 放行转发 | `route_origin` ⇒ 业务源站**原样透传**（`INT-8`：业务侧响应零改写） |
| 改道转发 | `route_mirage` ⇒ 查改道后端表（策略面下发 / 本地兜底）⇒ 转发幻境后端；**注入只在改道侧** |
| 失败回落 | 幻境后端不可用 ⇒ **回落业务**（`NI-5`），图上多一跳"幻境不可用" |
| 拦截 | `block` ⇒ 403；默认关，需 `SHEN_BLOCK_ENABLED=true`（`Q5` · `INT-12` 阶梯放开） |
| 怎么验 | `executed` 七值全部有实测路径：见 [`../ops/functional-verification.md`](../ops/functional-verification.md) §7.2 与 [`../integrate/observability.md`](../integrate/observability.md) §6 的验证配方 |

### 1.3 流量监控 / 观测 ✅ 已实现

| 项 | 内容 |
| --- | --- |
| 逐判定日志 | `make docker-log S=core` 看 `msg=decision`（分值 · 命中信号 · 决策 · 后端 · 时刻）；字段表 [`../spec/logs.md`](../spec/logs.md) §3 |
| 逐请求日志 | `SHEN_PROXY_LOG_REQUESTS=1` 时看 `路由：… 落点=…`（演示栈默认开） |
| 控制台接口 | **11 个只读接口**（权威表：[`../spec/console-api.md`](../spec/console-api.md) §2）：`/`（页面）· `/api/summary` · **`/api/config`（核心只读快照）** · **`/api/stream`（SSE 实时流）** · `/api/flow`（逐判定）· `/api/events` · `/api/analysis` · `/api/graphs`（逐请求链路）· `/api/trace`（单请求四段）· `/api/topology`（聚合）· `/healthz` |
| **DAG 逐请求链路** | 每条请求一条链路，**每步可点**看「请求 / 响应 / 为什么执行」；5 秒刷新；文字折行不溢出 |
| **配置快照** | 核心**当前生效态**（策略 id/版本/校验和/规则数/白名单条数 · AI 开关/种类/模型/清单路径/变体/已装载条数），2026-09-21 新增；**阈值/灰度/影子模式不在其中**（核心运行参数，不进契约面） |
| **实时流（推）** | 核心**记录事件的那一刻**推给页面（`ADR-0027`）：gRPC 服务端流 `WatchEvents` → 控制台 `GET /api/stream`（SSE）→ 页面增量插入。实测端到端延迟 **3 ms**（回环）。三条纪律：**旁路**（不影响上报）· **无背压且丢包可见**（缓冲满丢最旧并计数）· **可补漏**（`since` 重连先补历史）。**仍保留 30 秒整块对账**纠偏本地计数 |
| **观测新鲜度** | 概览第一眼显示**最后一条事件距今多久** + **观测窗口**；超过 120 秒（≈24 个刷新周期）标色提醒「观测可能已停」。为什么需要它：控制台最易出的错不是「显示错」而是「**数据停了**」—— 页面看上去正常，只是不再变 |
| 告警 | 两段式：真实告警（`block` 或 `severity≠none`）+ 高风险显示标记（`score ≥ SHEN_CONSOLE_ALERT_SCORE`，**仅显示**）。⚠️ 影子模式下真实告警**恒为 0**（`severity` 档位未定，见 [`../spec/metrics.md`](../spec/metrics.md) §2） |
| 指标 | [`../spec/metrics.md`](../spec/metrics.md)：**已采集 10 项** / 未采集 6 项（各带关法） |
| 接入自检 | `scripts/shen.sh doctor`（`INT-17` 五项：body 可读 / TLS 方式 / 会话粘性 / 实境与幻境 / 是否在路径上） |
| 怎么验 | `scripts/shen.sh verify`（状态 + 全量伪造流量 + L4 核对 + DAG 一致性 + 报告） |

### 1.4 配置 ✅ 已实现

| 项 | 内容 |
| --- | --- |
| 核心配置 | [`deploy/config/config.example.yaml`](../../deploy/config/config.example.yaml)（153 行）+ 字段全表与约束 [`../spec/config.md`](../spec/config.md)（429 行） |
| 校验 | **严格拒绝未知键**；示例配置有**漂移守卫单测**（`core/internal/policy` 的 `TestExampleConfigLoads`）；`make check-config` 可干跑 |
| 策略面（S4） | 改道后端表 / 白名单 / 注入规则经 `Pull` + `Ack` 下发（`ADR-0018`），载荷契约 [`../spec/policy-payload.md`](../spec/policy-payload.md)；远端优先、本地兜底 |
| 适配器/控制台/L4 | 适配器的全量环境变量示例：[`../../deception/proxy/config/front-proxy.example.env`](../../deception/proxy/config/front-proxy.example.env)（含 `SHEN_PROXY_INJECT_CONTENT`）；部署入口的变量在 [`../../deploy/docker/README.md`](../../deploy/docker/README.md) 与 [`deploy/docker/compose.yaml`](../../deploy/docker/compose.yaml)；本地进程起法见 [`../ops/runbook.md`](../ops/runbook.md) §1.2 |
| 部署 | `deploy/docker/`（3 镜像 + compose + README）；验证配方 `deploy/config/config.verify-mirage.yaml` + `deploy/docker/compose.verify-mirage.yaml` |
| 怎么验 | `make check-config` · `scripts/shen.sh status` · `scripts/shen.sh doctor` |

### 1.5 注入 AI 欺骗信息 🟡 **分阶段**（阶段 A 通路已通：生成 → 护栏 → 下发 → 注入；模型后端待接）

> 📖 **要看「AI 能力到底有哪些、模型接没接、哪里要优化」** → 本文姊妹篇 [`ai-capabilities.md`](ai-capabilities.md)
> （7 个能力逐个详解 · 模型后端 8 条硬约束 · 16 条优化点候选 · 6 条关键发现 · **§12 模块生命周期 · §13 接入与使用**）。本节只讲**状态与怎么验**。

| 层 | 状态 | 说明 |
| --- | --- | --- |
| **注入机制** | ✅ 已实现 | [`deception/injection/`](../../deception/injection)：只改**改道侧** HTML 响应（`INT-8`），按 `marker` 定位、找不到就跳过（不阻断响应）；规则由策略面 `inject_rules` 下发（`injects[]` 为 `[]` 表示显式关闭） |
| **AI 能力服务**（生成出口） | ✅ **已实现（阶段 A）** | [`analysis/aicap/`](../../analysis/aicap)：唯一出口 `generate(TaskSpec) → Envelope`，**内部强制走护栏**（前置三段式提示词 + 后置四关）——未登记的 kind 必拒、缺护栏档案**启动期就失败**（`AR-33` / [ADR-0023](../background/decisions/0023-deception-content-injection.md)）。**已解耦**（[ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md)）：内核不认识「内容」，接一个新消费方 = 加 `produce`/`build` + 一条登记（[`../spec/ai-contract.md`](../spec/ai-contract.md) §6），内核零改动；「独立」由 `make archcheck` 的 `MD-4` 项保证 |
| **欺骗内容通路** | ✅ **已实现（阶段 A）** | 离线生成 → 过护栏 → 清单（[`../spec/ai-contract.md`](../spec/ai-contract.md)）→ 核心装载进 `store.ContentStore` → 策略面 `content_manifest` 下发 → 适配器按（资源 + 会话哈希）**确定性命中**并注入改道侧；DAG 上多一跳「内容注入」 |
| **三层开关** | ✅ 已实现（默认全关） | `ai.enabled`（能力）· `inject_enabled`（下发）· `SHEN_PROXY_INJECT_CONTENT`（适配器兜底，默认 `false`）——取**与**；不打开时行为与以前逐字节一致 |
| 诱饵资产 | 🟡 定义与多态已实现 | [`core/internal/decoy/`](../../core/internal/decoy)（`MD-25` observe-only）；**资产内容 → 边缘的执行通路未接通**（设计登记的未接通项） |
| 欺骗响应一致性 | ✅ 已实现 | [`core/internal/responder/`](../../core/internal/responder)：同 `(会话, 资源)` 命中同一内容（`AR-30`，禁非确定性） |
| LLM 契约纪律 | ✅ 已实现 | [`analysis/llm/`](../../analysis/llm)：契约校验（`AR-15`）· 信封（`AR-16`）· 三段式解析（`AR-17`）· 双阶段收尾（`AR-19`…`AR-21`）· 黑名单（`AR-22`）· 提示词资源化（`AR-24`）· 注入防护（`AR-31`/`AR-32`） |
| L4 分析链路 | ✅ 已实现（近线） | [`analysis/worker.py`](../../analysis/worker.py)：读事件 → 去重（`AR-14`）→ 意图/攻击链/策略 → 结论事件；**不持有执行能力**（`AR-32`） |
| **真实模型后端** | ❌ **未接入（阶段 B）** | 阶段 A 的生成器是**确定性模板生成器**（证明通路 + 保证**生成期**可复现 —— 那是阶段 A 的工程性质，**不是** `AR-30` 的要求）；接模型时 `UnconfiguredClient` **显式失败**（`AR-15`：禁止用模板冒充模型输出） |
| L4 结论 → 内容轮换 | ❌ **未接通（阶段 B）** | `strategy` 已产出识破信号，但「谁消费它把清单 `version` +1」未接（[ADR-0023](../background/decisions/0023-deception-content-injection.md) 未解决 4） |

**结论（诚实版）**：现在的注入是**预生成、过护栏、确定性命中**的内容（不是现场生成）——这正是设计要的形状：
响应路径**永不调模型**（`AR-30` / `AR-29`）。
阶段 A 证明的是**通路与护栏**（生成 → 校验 → 下发 → 注入 → 观测都能跑通）；
内容像不像、多样性够不够，要等阶段 B 接真实模型与画像（那时的引入项要先过许可证台账 `TB-16`）。
怎么验：`python -m analysis.aicap --out …` 生成 → 配置 `ai.enabled=true` + `ai.manifest` →
适配器 `SHEN_PROXY_INJECT_CONTENT=true` → 改道侧响应多出 `<section class="service-detail">`，
且 `inject=applied` + `content_id` 进逐请求事件（控制台 DAG 可见「内容注入」跳）。

---

## 2. 其余已实现能力（速查）

| 能力 | 状态 | 代码 | 怎么验 |
| --- | --- | --- | --- |
| 判定打分（规则求值） | ✅ | [`core/internal/judge/`](../../core/internal/judge) | `make replay` |
| 三值决策 + 灰度 | ✅ | [`core/internal/director/`](../../core/internal/director) | DAG「决策」跳 · 验证配方 |
| 会话身份 / 隔离短路 | ✅ | [`core/internal/session/`](../../core/internal/session) · [`isolation/`](../../core/internal/isolation) | 单测 + `doctor` ③ |
| 策略装载 / 下发 / 回执 | ✅ | [`core/internal/policy/`](../../core/internal/policy) | `make docker-log S=proxy`（已应用策略 …） |
| 事件上报 / 读侧 | ✅ | [`core/internal/telemetry/`](../../core/internal/telemetry) · `/api/events` | `scripts/shen.sh verify` |
| 存储（内存实现） | 🟡 | [`core/internal/store/`](../../core/internal/store) | 重启丢数据 —— 真实存储待接（`NI-13`） |
| 蜜罐**接入架构** | 🟡 | [`honeypot/protocol/`](../../honeypot/protocol) · [`core/internal/honeypot/`](../../core/internal/honeypot) | 协议注册/运行框架/最小适配器已就绪；**协议栈内容待专项调研**（用户裁定） |
| L3 网络欺骗（声明式） | 🟡 | [`deception/netpolicy/`](../../deception/netpolicy) | 三份 Cilium/Tetragon 模板；需集群侧加载 |
| 伪造流量与验证 | ✅ | [`scripts/traffic/`](../../scripts/traffic) | `scripts/shen.sh traffic --check-graph --check-l4` |

---

## 3. 已知缺口（别重复发现）

| # | 缺口 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | **查询串不参与判定** | 参数型攻击（SQLi/穿越）默认不判 | [`../ops/functional-verification.md`](../ops/functional-verification.md) §2 #1 |
| 2 | **一次 URL 编码即绕过**规则匹配 | 同上 | 同上 #2 |
| 3 | 前缀规则误伤 / 可绕过 | `/.gitignore` 误伤；加前缀可绕过 | 同上 #3/#4 |
| 4 | **真实模型后端未接入** | 阶段 A 的内容由确定性模板生成器产出（像不像另说） | 阶段 B（ADR-0023 未解决 1/2/3，含许可评估） |
| 5 | **诱饵资产 → 边缘**未接通 | 诱饵面内容到不了响应（与 AI 内容是两条通路） | 见本文 §1.5 |
| 6 | `severity` 档位未定 | 控制台"告警"恒为 0 | [`../spec/metrics.md`](../spec/metrics.md) §2 |
| 7 | 真实存储未接 | 重启丢观测数据 | `NI-13` |
| 8 | 蜜罐协议栈内容 | 只有接入架构 | 用户裁定的专项调研 |
