# 目录与模块地图（谁在哪 · 能力是什么 · 怎么接进来）

> **这一页回答三个问题**：① 每个目录是做什么的；② 每个模块在哪个目录、提供什么能力；③ 它怎么和别的模块接起来。
> 规则的**权威**在 [`../design/`](../design/README.md)；**完成度**只在 [`../progress.md`](../progress.md) 维护；本文不重复那两处，只做"索引 + 接线"。

---

## 1. 一分钟看懂：分层 → 目录

```text
接入层   L0  客户自己的 LB / nginx / Envoy（复用现成组件，不自研，AR-3）
          │
数据平面 L1  edge/        适配器：① 旁路镜像 · ② DNS 引流 · ③ 反向代理前置 · ④ Sidecar
          │  「判定与响应生成必须只在核心实现一次」——L1 禁止写判定逻辑（AR-2 / AR-7）
          ▼
核心         core/        判定 judge → 决策 director（三值）→ 会话 · 隔离 · 策略 · 遥测 · 存储 · 服务面
          │
执行平面 L2  deception/   蜜罐协议仿真（框架已就绪，内容待专项调研）
         L3  deception/   网络欺骗：微隔离 · 假拓扑 · 运行时检测（声明式产物）
          │
分析平面 L4  analysis/    意图 intent · 攻击链 chain · 策略生成 strategy · LLM 契约 llm（Python）
          │
控制平面     console/     控制台：只读观测（禁止参与请求级判定，AR-10）
```

**顶层目录一句话**：

| 目录 | 是做什么的 | 语言 | 是否源码 |
| --- | --- | --- | --- |
| [`api/`](../../api) | **契约的唯一事实源**：`.proto` + 生成的 Go 桩（禁止手写客户端，`ST-6`） | Proto / Go | 生成物 |
| [`core/`](../../core) | 核心：判定 · 决策 · 会话 · 隔离 · 策略 · 遥测 · 存储 · 服务面 · 欺骗面 | Go | 是 |
| [`edge/`](../../edge) | L1 数据平面：四个接入形态的适配器 + L1 处置 | Go / 配置 | 是 |
| [`deception/`](../../deception) | L2+L3 执行平面：蜜罐接入架构 · 网络欺骗声明式产物 | Go / 声明式 | 是 |
| [`analysis/`](../../analysis) | L4 分析平面 + 其**私有工具链**（`pyproject.toml` · `requirements*.txt` · `.venv`） | Python | 是 |
| [`console/`](../../console) | 控制平面：只读观测控制台（Web UI + 4 个接口） | Go + 静态页 | 是 |
| [`deploy/`](../../deploy) | 部署物料：Docker（`docker/`）与配置模板（`config/`） | YAML / Dockerfile | 物料 |
| [`scripts/`](../../scripts) | 门禁与运维工具（`archcheck` 等）· 演示环境 · 本地 `bin/` 工具缓存 | Go / sh / Python | 是 |
| [`docs/`](../README.md) | 全部文档（设计 · 模块 · 规格 · 运行 · 背景） | Markdown | 文档 |
| [`vendor/`](../../vendor) | Go 依赖副本（**入库** ⇒ 构建与门禁离线可用，不需要 Go 代理） | Go | 第三方 |

> 顶层目录白名单由 `ST-1` 守着（[`../design/structure.md`](../design/structure.md) §1.1）；`vendor/` 与 `scripts/bin/` 是产物形态，不进白名单。

---

## 2. 模块总表（22 个模块：目录 · 能力 · 对外接口 · 接线）

**读取方式**：一个模块 = 一个目录 + 一份文档（`MD-2`）；`iface.go` 是它**对外承诺**的接口（`ST-4`：一模块一目录，接口单独成文件）。

### 2.1 核心（`core/internal/` · Go · 进程 `shen-core`）

| # | 模块 | 目录 | 能力 | 对外接口 | 依赖 → 被谁用 |
| --- | --- | --- | --- | --- | --- |
| 1 | `judge` | [`core/internal/judge/`](../../core/internal/judge) | 把观测变成判定：风险分 · 命中信号 · 证据链 | `Judge` · `RuleSource` | `contract` → `director` · `control` |
| 2 | `director` | [`core/internal/director/`](../../core/internal/director) | 决策：阈值 → 三值 + 灰度 + 白名单/诱饵豁免 | `Director` · `ThresholdSource` · `GraySource` | `contract` · `judge` → `control` |
| 3 | `responder` | [`core/internal/responder/`](../../core/internal/responder) | 欺骗响应生成（同会话同资源内容一致，`AR-30`） | `Responder` · `ContentStore` | `contract` → 装配层 |
| 4 | `session` | [`core/internal/session/`](../../core/internal/session) | 会话身份提取（`INT-19` 优先级）与状态抽象 | `Session` | `contract` → `control` |
| 5 | `isolation` | [`core/internal/isolation/`](../../core/internal/isolation) | 隔离记录写入 / TTL 过期 / 查询短路 | `Isolation` · `Store` | `contract` → `control` |
| 6 | `policy` | [`core/internal/policy/`](../../core/internal/policy) | 策略版本 · 灰度 · **下发与回执**（`Pull`/`Ack`）、规则与白名单装载 | `Policy` | `contract` · `store.PolicyStore` → `core/cmd/core`（装配成 gRPC 服务） |
| 7 | `telemetry` | [`core/internal/telemetry/`](../../core/internal/telemetry) | 事件归一化 · 批量异步上报 · 失败缓冲重放 | `Telemetry` · `Sink` · `Result` | `contract` → `control` |
| 8 | `store` | [`core/internal/store/`](../../core/internal/store) | **唯一 I/O 出口**（`MD-20`）：会话 / 隔离 / 判定 / 事件 / 策略 / 诱饵 / 内容 + 读侧查询 | `SessionStore` · `IsolationStore` · `DecisionStore` · `EventStore` · `PolicyStore` · `DecoyStore` · `ContentStore` · `DecisionQuery` · `EventQuery` | `contract` → 几乎全部核心模块（经接口） |
| 9 | `edge-injection` | [`edge/injection/`](../../edge/injection) | 投毒响应改写 · 假路径 · 蜜饵注入（**不独立部署**，`ST-5`） | `Injector` · `Rule` | 纯变换（连 `contract` 都不依赖）→ `edge/proxy` |
| 10 | `adapter-mirror` | [`edge/mirror/`](../../edge/mirror) | ① 旁路镜像接收端：只读采集、**不在请求路径**（`INT-6`） | `Receiver`（HTTP） | `api/judge/v1` · `api/telemetry/v1` |
| 13 | `adapter-dns` | [`edge/dns/`](../../edge/dns) | ② DNS 引流：按来源解析到引擎或真实服务（**纯配置，无源码**，`AR-3`） | `config/Corefile.example` | CoreDNS（复用） |
| 11·12 | `adapter-proxy` | [`edge/proxy/`](../../edge/proxy) | ③ 反向代理前置 + ④ Sidecar（**同一份实现**）：白名单 → 判定缓存 → 调核心 → 三值处置 → 异步上报；转发与 TLS 用内嵌 Caddy（`ADR-0017`） | `JudgeClient` · `TelemetryClient` · `Injector` · `Config` | `api/*` · `edge/injection` |
| 22 | `control` | [`core/internal/control/`](../../core/internal/control) | gRPC 服务面：入参映射 · 会话身份 · **禁止回显的强制点**（`ST-7`）· 观测面记录与读取 · 熔断 | `Decider` · `DecisionRecorder` · `EventLister` | `contract` · `judge` · `session` · `telemetry` · `api/*` |
| 23 | `decoy` | [`core/internal/decoy/`](../../core/internal/decoy) | 诱饵面：功能性伪装诱饵定义 + 多态 | `Surface` · `AssetStore` | `contract` → 装配层 |
| 24 | `honeypot` | [`core/internal/honeypot/`](../../core/internal/honeypot) | 蜜罐入口与**后端池**：类型注册 · 开关 · 生命周期 | `Pool` | `contract` → 装配层 |
| — | `contract` | [`core/internal/contract/`](../../core/internal/contract) | 进程内**共享类型**（叶子，无逻辑） | 类型定义 | 被全部核心模块依赖 |

### 2.2 执行平面（`deception/`）

| # | 模块 | 目录 | 能力 | 对外接口 | 接线 |
| --- | --- | --- | --- | --- | --- |
| 14 | `honeypot-protocol` | [`deception/honeypot/`](../../deception/honeypot) | 协议仿真**框架**：协议注册表 · 会话工厂 · 运行框架（并发上限 `MD-16` · 对称回收 `MD-15`）· 最小真实适配器 | `Protocol` · `Session` · `SessionFactory` · `Registry` · `Limits` · `Stats` | 独立于核心；真实协议栈内容待专项调研 |
| 15 | `honeypot-shell` | ⏸ **未建**（[`deception/shell/`](../../deception) 不存在） | 命令表分发 · 内存文件系统 · 文件投递 | — | 用户裁定推迟（蜜罐只做接入架构） |
| 16 | `netpolicy` | [`deception/netpolicy/`](../../deception/netpolicy) | 微隔离 · 假拓扑 · 运行时检测（**声明式产物**，复用 Cilium/Tetragon） | `config/*.example.yaml` | 事件应经适配器回流遥测面（文档级约定） |

### 2.3 分析平面（`analysis/` · Python）

| # | 模块 | 目录 | 能力 | 对外接口 | 接线 |
| --- | --- | --- | --- | --- | --- |
| 17 | `intent` | [`analysis/intent/`](../../analysis/intent) | 意图识别：规则命中 → 意图类别 + 置信度 + 证据引用 | `recognize()` → `Envelope` | 被 [`analysis/worker.py`](../../analysis/worker.py) 调用 |
| 18 | `chain` | [`analysis/chain/`](../../analysis/chain) | 攻击链还原 + 识破信号 + **证据引用校验**（`AR-12`） | `reconstruct()` · `EvidenceIndex` | 同上；引用不存在则整链作废 |
| 19 | `strategy` | [`analysis/strategy/`](../../analysis/strategy) | 策略**数据**生成（灰度 ≤20% · 阈值下界）+ 诱饵轮换决策 | `generate()` · `decide()` | 结论经 `policy` 间接生效（禁止直接影响响应，`AR-32`） |
| 20 | `llm-components` | [`analysis/llm/`](../../analysis/llm) | 契约校验（`AR-15`）· 信封（`AR-16`）· 三段式解析（`AR-17`）· 长度纪律（`AR-18`/`AR-23`）· 双阶段收尾（`AR-19`…`AR-21`）· 黑名单（`AR-22`）· 提示词资源化（`AR-24`）· 注入防护（`AR-31`/`AR-32`） | `validate` · `Envelope` · `extract_json` · `truncate_list` · `Blacklist` · `run_two_phase` · `assert_startup` · `assert_no_execution_surface` | 被上三个模块共用 |
| 25 | `ai-capability` | [`analysis/aicap/`](../../analysis/aicap) | **可开关的生成出口**：任务注册表（缺护栏档案即启动失败）· 前置提示词三段式（`AR-31`/`AR-24`）· 后置四关校验（`AR-15`/`AR-22`/`AR-23`/风格）· 内容对象与清单（`content_id` 由内容体算出：**产物层**确定性，不是生成期要求） | `generate(TaskSpec) → Envelope` · `python -m analysis.aicap` | 清单文件 → 核心 [`policy`](../../core/internal/policy) 装载 → 策略载荷 → [适配器](../../edge/proxy) 注入（`AR-33` / [ADR-0023](../background/decisions/0023-deception-content-injection.md)） |
| — | **worker** | [`analysis/worker.py`](../../analysis/worker.py) | 近线链路：读遥测 → 态势去重（`AR-14`）→ 意图/链/策略 → 结论**作为事件**上报 | `run_once()` · `python -m analysis.worker` | 读 `ListEvents`、写 `Report`（[`analysis/telemetry.py`](../../analysis/telemetry.py)） |
| — | 工具链 | [`analysis/tools/genproto.py`](../../analysis/tools/genproto.py) · [`analysis/proto/`](../../analysis/proto) | 生成 gRPC 桩并让其成为 `analysis.proto.*` 普通包 | `make pygen` | 契约仍来自 [`api/`](../../api) |

### 2.4 控制平面（`console/`）

| # | 模块 | 目录 | 能力 | 对外接口 | 接线 |
| --- | --- | --- | --- | --- | --- |
| 21 | `console` | [`console/cmd/console/`](../../console/cmd/console) · [`console/web/`](../../console/web) | 只读观测台：概览 · 告警 · 流量访问与流动（含**分值/命中信号**）· **L4 分析结论** | HTTP：`/` · `/api/summary` · `/api/flow` · `/api/analysis` · `/api/events` | 只读 `ListEvents`（`AR-10`：禁止参与请求级判定） |

---

## 3. 进程与接缝（怎么接起来的）

全仓产出 **3 个 Go 二进制** + **2 个非 Go 组件**：

| 进程 / 组件 | 入口 | 角色 |
| --- | --- | --- |
| `shen-core` | [`core/cmd/core/main.go`](../../core/cmd/core/main.go) | 服务端：判定与决策的唯一实现；**唯一允许依赖具体实现**的装配处 |
| `shen-proxy` | [`edge/proxy/cmd/proxy/main.go`](../../edge/proxy/cmd/proxy/main.go) | ③ 前置 + ④ 边车（内嵌 Caddy 处理转发与 TLS） |
| `shen-mirror` | [`edge/mirror/cmd/mirror/main.go`](../../edge/mirror/cmd/mirror/main.go) | ① 旁路镜像接收端（恒回 202，不在请求路径） |
| L4 worker | [`analysis/worker.py`](../../analysis/worker.py) | 近线分析（Python）；**不在业务路径** |
| 控制台 | [`console/cmd/console/`](../../console/cmd/console) | 只读观测台 |

**接缝表**（跨进程/跨层的契约点）：

| 接缝 | 从 → 到 | 契约 | 说明 |
| --- | --- | --- | --- |
| **S1 判定** | `edge/*` → `core` | [`api/judge/v1`](../../api/judge/v1) `DeceptionJudge.Judge` | 适配器把观测发给核心，拿回三值决策；deadline 受延迟预算约束（`AR-29`） |
| **遥测上报** | `edge/*` · L4 → `core` | [`api/telemetry/v1`](../../api/telemetry/v1) `Report` / `ReportBatch` | 异步、批量、幂等（`AR-11`，幂等键 `event_id`） |
| **观测读取** | 控制台 · L4 → `core` | `api/telemetry/v1` `ListEvents` | 读侧：控制台看告警/流量，L4 拿输入（[`../spec/events.md`](../spec/events.md)） |
| **S4 策略面** | `core` → `edge/*` | [`api/policy/v1`](../../api/policy/v1) `Pull` / `Ack`（`Watch` 未实现） | 拉取式：后端表 · 白名单 · 注入规则；带版本 + 校验和 + 回执（`ST-8` · `AR-13` · [`../spec/policy-payload.md`](../spec/policy-payload.md)） |
| **注入** | `edge/proxy` → `edge/injection` | Go 接口 `Injector` | 进程内引用，**不独立部署**（`ST-5`） |
| **存储** | 核心各模块 → `store` | Go 接口（7 个 Store） | `store` 是核心**唯一** I/O 出口（`MD-20`），其它模块不碰外部系统 |
| **不存在的接缝** | 适配器 → `core/internal/*` | — | **编译期禁止**：`internal/` 规则挡住（`ST-3`）——适配器只能走 `api/` 的 stub |

**请求怎么走**（影子模式下的实测路径）：

```text
客户端 → L0（客户 LB，终结 TLS）→ edge/proxy
  1. 白名单命中？→ 直接放行到业务（INT-25）
  2. 本地判定缓存命中？→ 复用（ST-10；键 = 来源 + 会话 + 方法 + 路径 + 时间窗）
  3. 调核心 Judge（S1）→ judge 打分 → director 出三值
  4. 按三值处置：route_origin → 业务原样透传 · route_mirage → 引流 + 注入 · block → 403
  5. 异步上报事件（遥测）
→ core 把每次判定同时落成：事件（控制台读）+ 判定记录（store）
→ analysis/worker 读事件 → 态势去重 → 意图/链/策略 → 结论作为事件回写
→ console 读事件：告警 · 流量访问与流动 · 分析结论
```

---

## 4. 数据与配置落在哪

| 东西 | 位置 | 谁读 |
| --- | --- | --- |
| 核心配置（规则 · 阈值 · 白名单 · 诱饵 · 蜜罐 · 注入） | [`deploy/config/config.example.yaml`](../../deploy/config/config.example.yaml) → 运行时经 `SHEN_CONFIG` | `core/cmd/core` |
| 事件载荷契约（`decision` / `analysis`） | [`../spec/events.md`](../spec/events.md) + 夹具 [`api/telemetry/v1/testdata/decision_event.json`](../../api/telemetry/v1/testdata/decision_event.json) | 核心写 · 控制台与 L4 读 |
| 策略载荷契约（后端表 / 白名单 / 注入规则 / AI 内容清单） | [`../spec/policy-payload.md`](../spec/policy-payload.md) | `policy` 写 · 适配器读 |
| AI 能力契约（`TaskSpec` / `Envelope` / 护栏 / 内容对象 / 清单文件） | [`../spec/ai-contract.md`](../spec/ai-contract.md) | `ai-capability` 写 · `policy` 装载与投影 · 适配器消费 |
| 内容清单文件（生成期产物） | `ai.manifest` 指向的文件（`python -m analysis.aicap --out …`） | `core/cmd/core` 启动装载 → `store.ContentStore` |
| 话术与提示词（随版本分发，`AR-24`） | [`analysis/llm/resources/prompts/`](../../analysis/llm/resources/prompts) · [`analysis/llm/resources/blacklist.yaml`](../../analysis/llm/resources/blacklist.yaml) | `llm-components` |
| 适配器环境变量 | [`deploy/docker/README.md`](../../deploy/docker/README.md) · [`edge/proxy/config/front-proxy.example.env`](../../edge/proxy/config/front-proxy.example.env) | 各适配器进程 |
| 代理侧默认配置模板 | [`edge/proxy/config/sidecar.example.yaml`](../../edge/proxy/config/sidecar.example.yaml) | 形态 ④ 部署时 |

---

## 5. 一个模块的"标准形状"（照这个加新模块）

| 要件 | 说明 | 规则 |
| --- | --- | --- |
| 目录 | `core/internal/<模块>/`（核心）· `edge/<模块>/`（适配器）· `analysis/<模块>/`（L4） | `MD-1` |
| `iface.go` | **对外接口单独成文件**（导出的 `interface` / 关键类型） | `ST-4` |
| 实现文件 | 一个模块的实现在同一目录内；禁止跨顶层目录 import 源码 | `ST-2` |
| 单测 | 同目录 `*_test.go`（Python：`analysis/tests/`）；用替身，不连真核心/真存储 | `MD-22` |
| 模块文档 | `docs/modules/<模块>.md`（九章：职责 / 契约 / 依赖 / 数据 / 失败路径 / 测试 / 未决 / 变更） | `MD-2` · `MD-17` |
| 清单登记 | 先加进 [`../design/modules.md`](../design/modules.md) §1.1 再实现 | `MD-18` |
| 进度登记 | 落地后更新 [`../progress.md`](../progress.md) 与 [`../log.md`](../log.md) | — |

---

## 6. 我不知道该看哪

| 我要做的事 | 去哪 |
| --- | --- |
| 看懂整体架构与规则 | [`../design/README.md`](../design/README.md)（八份） |
| 知道某模块的细节（契约 · 失败路径 · 测试） | `docs/modules/<模块>.md`（本目录） |
| 知道哪些模块做完了 | [`../progress.md`](../progress.md) |
| 起环境 / 验证 / 排障 | [`../ops/runbook.md`](../ops/runbook.md) |
| 接入真实业务 | [`../integrate/business-onboarding.md`](../integrate/business-onboarding.md) |
| 看告警 / 看流量与流动 | [`../integrate/observability.md`](../integrate/observability.md) |
| 发伪造流量做测试 | [`../../scripts/traffic/README.md`](../../scripts/traffic/README.md) |
