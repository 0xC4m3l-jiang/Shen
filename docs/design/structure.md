# 项目目录结构

> 规则 ID 前缀 `ST`。写作要求见 [`README.md`](README.md) §2。
>
> **已确认**：用户于 2026-09-17 确认 **ST-1…ST-24 全部通过**。
> ⚠️ §1 的布局于同日**追加** `common/core/internal/director/`（依据 [`modules.md`](modules.md) §1.1 的模块清单轮）。
> **来源**：外部设计稿 [`../background/research/deception-engine-design.md`](../background/research/deception-engine-design.md) §8.3 / §8.6。
> ✅ ST-4 / ST-12（依赖方向的强制）**只能靠 lint 或依赖检查实现**；工具已于 2026-09-17 定为 `scripts/archcheck/`，见 §1.7 的「强制机制」。
> **ST-3 例外：已由 Go 的 `internal/` 目录规则在编译期强制**（见 §1.7 的说明）。

---

## 1. 仓库布局

### 1.1 顶层

```text
Shen/
├── AGENTS.md                   # 强制规范（pi 自动加载）
├── Makefile                    # 开发入口：generate / build / test / vet / lint / run
├── go.mod  go.sum              # ★ Go 模块根（module 名 shen）—— 编辑器工作区根即仓库根
├── vendor/                     # Go 依赖副本（入库）：构建与门禁**离线可用**，不依赖 Go 代理
│                               #   （本机访问不了 proxy.golang.org；没有它连依赖都下不动）
├── .pi/                        # Agent 工作流（skills）
├── docs/                       # 设计、决策、背景材料
├── modules/                    # ★ 产品功能模块（**开发从这里进去**：一个子目录 = 一个大模块）
│   ├── deception/              #   ① 欺骗层 —— L1 四个接入适配器与处置 + L3 网络欺骗（Go / 配置 / 声明式）
│   ├── honeypot/               #   ② AI 蜜罐层 —— L2 协议仿真（`protocol/`）+ 假 shell（`shell/`，未建）
│   └── console/                #   ③ 管控平台 —— 只读观测台（Go 进程 + 静态页）
├── common/                     # ★ 公用代码（被 modules/ 共用；**不是**功能模块）
│   ├── core/                   #   共享内核 —— 判定与响应生成的唯一实现（Go）
│   └── api/                    #   **跨**进程契约 —— .proto + 生成的 Go 桩（叶子，无业务逻辑）
├── analysis/                   # L4 分析平面 —— 意图 / 攻击链 / 策略 / LLM 契约 + AI 能力服务 + 近线 worker（Python）
│                               #   **跨** ①②：既不是三大模块之一，也不是公用库；工具链全在本目录（pyproject.toml · requirements*.txt · .venv/）
├── deploy/                     # 部署物料
│   ├── docker/                 #   Dockerfile × 3 · compose.yaml · README（一键起全套：make up）
│   └── config/                 #   配置模板
└── scripts/                    # 自检与运维工具（archcheck / gate / sentinel / doctor / check-leak）
```

> **两级顶层结构**（[ADR-0030](../background/decisions/0030-two-level-layout.md)）：`modules/` 与 `common/` 是**容器**，容器下面才是平面。
>
> | 容器 / 顶层 | 下面有什么 | 是什么 |
> | --- | --- | --- |
> | `modules/` | `deception/`（① 欺骗层）· `honeypot/`（② AI 蜜罐层）· `console/`（③ 管控平台） | **产品功能模块** —— 「去哪个子目录」= 「开发哪个大模块」 |
> | `common/` | `core/`（共享内核）· `api/`（跨进程契约） | **公用代码** —— 被 `modules/` 共用，不单独交付 |
> | `analysis/` | `llm/` · `intent/` · `chain/` · `strategy/` · `aicap/` | **L4 分析层** —— 跨 ①② 的独立语言与进程，故既不在 `modules/` 也不在 `common/` |
>
> 这样「代码在哪」与「它属于哪个大模块 / 还是公用」是同一个答案。
> 容器与平面的**清单以本节为准**：`make archcheck` 从本节解析白名单（`ST-1`）。
> 历史：顶层曾直接是五个平面（`core/` `deception/` `honeypot/` `console/` `analysis/`）——
> 见 [ADR-0029](../background/decisions/0029-three-module-dirs.md)（改名）与 [ADR-0030](../background/decisions/0030-two-level-layout.md)（收进容器）。
>
> **顶层目录之外的形态约定**：`vendor/`（Go 依赖副本，入库以支持离线构建）与 `scripts/bin/`（门禁工具二进制，不入库）
> 都不是源码平面，`make archcheck` 已把它们排除在「顶层目录白名单」之外。
> **阶段未到的目录不预建**（[ADR-0007](../background/decisions/0007-repo-layout.md) 的阶段划分）：目录只在**进入其实施阶段**时才建。
> 当前状态：`modules/`（`deception/` `honeypot/` `console/`）· `common/`（`core/` `api/`）· `analysis/` **均已建**；
> 其中 `honeypot/shell/`（假 shell）与蜜罐协议栈内容按用户裁定**推迟**（`honeypot/protocol/` 是已建的框架）。

### 1.2 `common/core/` 内部

```text
common/core/
├── cmd/core/               # 进程入口（main.go）
└── internal/               # ★ internal 规则：只有 common/core/ 子树能 import（见 §1.7 的 ST-3）
    ├── contract/           #   进程内共享类型 —— 只放类型，无逻辑
    ├── control/            #   gRPC 服务面（S1 服务端）
    ├── judge/              #   判定
    ├── director/           #   决策与后端选择（阶段 2a）
    ├── decoy/              #   诱饵面：功能性伪装 + 蜜饵（阶段 2b）
    ├── honeypot/           #   蜜罐入口与后端池（阶段 2b，不实现具体蜜罐）
    ├── responder/          #   响应生成（阶段 2b）
    ├── session/            #   会话身份
    ├── isolation/          #   隔离名单（阶段 2）
    ├── policy/             #   策略版本与灰度（阶段 2）
    ├── telemetry/          #   事件归一化与上报
    └── store/              #   唯一 I/O 出口（Redis / ClickHouse / PostgreSQL）
```

### 1.3 `modules/` 内部（三个大模块）

```text
modules/deception/
├── injection/              # L1 处置逻辑（模块 edge-injection）—— Go，被适配器引用，不独立部署（阶段 2b）
├── mirror/                 # ① 旁路镜像（配置 + Go，阶段 1）
├── dns/                    # ② DNS 引流（**纯配置**，无源码，阶段 2a）
└── proxy/                  # ③ 反向代理前置 + ④ Sidecar（同一份实现，Go，阶段 2a）
    ├── iface.go            #   对外契约：JudgeClient / TelemetryClient / Injector / Config
    ├── handler.go          #   判定胶水：Caddy 中间件 `http.handlers.shen_proxy`
    ├── glue.go             #   纯函数：decision_id / 白名单 / 观测 / 注入判据 / 判定缓存
    ├── embed.go            #   进程装配：BuildConfig（http server + TLS）+ TLSConfig 校验
    └── cmd/proxy/          #   进程入口（env → Handler → Caddy）
```

> **转发与 TLS 终结由内嵌 Caddy 承担**（[ADR-0017](../../docs/background/decisions/0017-caddy-l1-base.md)）：
> 本模块**不写转发逻辑**，判定逻辑也**不在这里**（`AR-2` / `AR-5`）。
> 四个 `.go` 文件是**同一个模块**的内部拆分，不跨层、不新增模块（`MD-1` / `MD-18` 不触发）。

> `reverse-proxy/` 与 `sidecar/` **不再存在** —— ③④ 是同一份代码的两种部署形态，已合并为 `proxy/`（[ADR-0008](../../docs/background/decisions/0008-edge-language-go.md)）。

**同一个容器下的另外两个大模块**（`modules/honeypot/` = ② · `modules/console/` = ③）：

```text
modules/honeypot/                 # ② AI 蜜罐层
├── protocol/                     #   协议仿真入口（模块 honeypot-protocol；阶段 3 的**框架**，真实协议栈待调研）
│   ├── iface.go                  #     对外接口
│   ├── banner.go                 #     协议标识
│   ├── runner.go                 #     运行框架
│   └── errors.go                 #     错误类型
└── shell/                        #   ⏸ **未建**（假 shell；用户裁定推迟；阶段未到，不预建 —— [ADR-0007](../background/decisions/0007-repo-layout.md) 的阶段划分）

modules/console/                  # ③ 管控平台
├── cmd/console/                  #   进程入口
├── internal/topology/            #   拓扑图（内存计数 → 视图）
└── web/                          #   静态页（index.html + assets.go 内嵌，**无构建步骤** ADR-0020）
```

> 「我要开发哪个模块 → 进哪个子目录 → 看哪份文档 → 怎么跑测试」：见 [`../../modules/README.md`](../../modules/README.md)。
> 公用代码（内核与契约）在 `common/`，见 [`../../common/README.md`](../../common/README.md)。

### 1.4 模块的文件约定

每个 Go 模块固定三个文件，与 [`modules.md`](modules.md) §1.1 的清单一一对应：

| 文件 | 内容 | 依据 |
| --- | --- | --- |
| `iface.go` | 该模块**对外的唯一契约**（导出的 interface） | 依赖方只依赖接口，不依赖具体类型 |
| `<模块名>.go` | 实现 | —— |
| `<模块名>_test.go` | 单测 | `MD-7`（判定规则正负样本）· `MD-22`（独立可运行） |

**`store` 例外**：按实体拆成五个类型，故为 `iface.go`（五个接口）+ `memory.go`（五个内存实现）+ `store_test.go`。

> 这条约定**尚未升格为规则**。若要约束必须遵守，加一条 `ST-25`。

### 1.5 已建 / 未建

| 位置 | 状态 |
| --- | --- |
| `common/api/*.proto` + 生成的 Go | ✅ 已建（judge / policy / telemetry 三份契约） |
| `common/core/internal/contract` | ✅ 已建（8 个类型文件；只放类型，无逻辑） |
| `common/core/internal/{judge,session,telemetry,store,control}` | ✅ 已建（含单测） |
| `common/core/internal/policy` | ✅ 已建（阶段 2a：配置装载 · 版本 · 校验和 · 规则供给 · **下发面 `Pull`/`Ack`**，含单测） |
| `common/core/cmd/core` | ✅ 已建 |
| `modules/deception/mirror/` | ✅ 已建（`receiver.go` + 单测） |
| `common/core/internal/director` | ✅ 已建（阶段 2a：阈值→三值 + 灰度，含单测） |
| `common/core/internal/{responder,isolation,decoy,honeypot}` | ✅ 已建（阶段 2b，含单测；**不在判定调用链上**）。⚠️ **处置内容的边缘通路只有一部分接通**：AI 欺骗内容已接通（`ai-capability` → 策略面 `content_manifest` → 适配器，`ADR-0023`）；诱饵资产仍未接 |
| `modules/deception/mirror/` 的 L0 配置模板 | ✅ 已建（`iptables-tee` / `nginx-mirror` / `envoy-mirror`） |
| `modules/deception/proxy/` | ✅ 已建（③前置 + ④边车的同一份实现：内嵌 Caddy 转发 + TLS + 策略面消费；用例数用 `go test ./modules/deception/proxy/ -v` 取） |
| `modules/deception/dns/` | 🟡 [`Corefile.example`](../../modules/deception/dns/config/Corefile.example) 已建（② 纯配置，无源码） |
| `modules/deception/injection/` | ✅ 已建（阶段 2b，含单测；被适配器引用，**不独立部署** `ST-5`） |
| `modules/honeypot/protocol/` | ✅ 已建（阶段 3 **框架**：协议注册表 · 运行框架 · 最小适配器 + 单测；**真实协议栈待设计**） |
| `modules/deception/netpolicy/` | 🟡 阶段 3：三份声明式产物（微隔离 / 假拓扑 / 运行时检测，复用 Cilium / Tetragon，**无源码**） |
| `modules/honeypot/shell/` | ⏸ **推迟**（用户裁定：蜜罐只做接入架构，内容与协议栈待专项调研） |
| `analysis/*` | ✅ 已建（阶段 3，Python：`llm` 契约层 · `intent` · `chain` · `strategy` + 单测；用例数用 `make pytest` 取；工具链见 `requirements-dev.txt`） |
| `analysis/aicap/` | ✅ 已建（阶段 3，模块 25：**可开关的 AI 能力服务**——`service.py` 是唯一出口且**内部强制走护栏**，子包 `tasks/`（任务注册表 + `content`）与 `guardrail/`（前置提示词 + 后置独立校验）；阶段 A 产出欺骗内容 + 清单给核心装载。依据 `AR-33` / [ADR-0023](../background/decisions/0023-deception-content-injection.md)） |
| `modules/console/` | ✅ 已建（阶段 2b：Go 进程 + 静态页，只读观测；语言偏离见 [ADR-0020](../background/decisions/0020-console-minimal-static-ui.md)） |
| `scripts/*` | 🟡 部分已建：`archcheck/` `gate/` `licensecheck/` `tracecheck/` `check-leak/` ✅ 已实现；`devcheck/`（开发期在线冒烟）+ `dev/smoke.sh`（一键验证）✅ 已实现；`sentinel/` `doctor/` 仍只有说明 |
| `deploy/` | 🟡 配置示例已建；helm / compose 未建 |

> **本表只区分「目录在不在」。** 模块级的**实现完成度**以 [`../progress.md`](../progress.md) 为准，只在那处维护。
>
> ⚠️ **§1.2 / §1.3 的目录树是目标布局**（含阶段 2 的目录，标注为「阶段 2」）。
> **当前实况**（哪些包真的存在、谁依赖谁）见 **§1.6**。

### 1.6 当前实现的包级地图（实测）

> 依赖关系来自 `go list -f '{{.Imports}}' ./...` 的**实测输出**，不是设计意图。
> 各模块的**完成度**见 [`../progress.md`](../progress.md)（完成度只在那一处维护）。

#### 1.6.1 进程边界

全仓当前产出**三个二进制**（`shen-core` / `shen-mirror` / `shen-proxy`）；它们之间**唯一**的连接是 `common/api/` 的 gRPC 契约。

```text
┌─ 进程 1 · shen-core ── 判定与决策的唯一实现（无状态多副本）────────────────
│  cmd/core/main.go      装配处（唯一允许依赖具体实现的地方）
│
│  接线板     control      gRPC 服务面：JudgeService · TelemetryService
│  判定/决策  judge        规则求值            director   三值决策 + 灰度 + 白名单/诱饵豁免
│  会话/隔离  session      身份提取 + 会话特征  isolation  隔离短路（命中不再调核心）
│  欺骗面     decoy        诱饵定义 + 多态      responder  欺骗响应（一致性 AR-30）
│             honeypot     幻境后端池（不实现具体蜜罐）
│  支撑       policy       策略/阈值/白名单     telemetry  事件上报     store  唯一 I/O 出口
│  共享类型   contract     只放类型（叶子）
└──────────────────────────────────────────────────────────────────────────────
   ⚠ 实况：decoy / responder / honeypot 已装配但**不在调用链上** —— 缺策略平面（见 §2.1）
```

核心是**服务端**、适配器是**客户端**，接缝 **S1**（`common/api/judge/v1` · `common/api/telemetry/v1`）：

```text
┌─ 进程 2 · shen-mirror ── ① 旁路镜像接收端（不在请求路径上）────────────────
│  cmd/mirror/main.go      HTTP 服务入口
│
│  modules/deception/mirror/            Receiver.ServeHTTP:
│      1. 取观测（IP / UA / 方法 / 路径 / 头）
│      2. 算 decision_id（来源 + 会话 + 路径 + 60s 时间窗）
│      3. 请核心判定 → 4. 记事件 → 5. 恒回 202
└──────────────────────────────────────────────────────────────────────────────
```

```text
┌─ 进程 3 · shen-proxy ── ③ 反向代理前置 + ④ Sidecar（同一份实现）──────────
│  cmd/proxy/main.go      env 装配 → proxy.BuildConfig → caddy.Run
│                         （内嵌 Caddy：TLS 终结 + 转发，ADR-0017）
│
│  modules/deception/proxy/            Handler = Caddy 中间件 `http.handlers.shen_proxy`；无状态，可随时重启
│    ServeHTTP:  白名单 → 本地判定缓存 → 请核心判定 → 按三值处置 → 异步上报
│    dispatch:   route_origin  → 业务 upstream（原样透传）
│                route_mirage  → 查**本进程的**引流表 → 注入诱饵 → 后端
│                block         → 403（默认不产出，Q5）
│    转发层:     Caddy 的 reverse_proxy（本模块只提供决定与后端地址）
│  modules/deception/injection/        注入引擎（被 proxy 通过 Injector 接口引用，ST-5）
└──────────────────────────────────────────────────────────────────────────────
```

> **两个适配器进程都 import 不了 `common/core/internal/`**（`ST-3`）—— 由 Go 的 `internal/` 规则在**编译期**强制，
> 它们只能走 `common/api/` 生成的 stub。


#### 1.6.2 依赖方向（实测）

```text
叶子（谁都不依赖）
  common/core/internal/contract                 ← 被全部核心模块依赖 —— 进程内的「词汇表」
  modules/deception/injection                         ← 纯变换；连 contract 都不依赖（ST-3 的自然结果）

只依赖 contract（8 个）
  judge · session · telemetry · store · decoy · honeypot · isolation · responder

接缝层（contract + 另一个模块的接口）
  policy      →  contract · store.PolicyStore
  director    →  contract · judge（阈值/灰度/白名单由 policy.Loader 供给）

聚合层（唯一同时依赖多个同级模块的包）
  control     →  contract · judge · session · telemetry · common/api/judge/v1 · common/api/telemetry/v1

适配器（对 common/core/ 零依赖 —— 只能走 common/api/ 的 stub）
  modules/deception/mirror · modules/deception/proxy   →  common/api/judge/v1 · common/api/telemetry/v1

装配层（只有 main 允许依赖具体实现）
  common/core/cmd/core          →  control · decoy · director · honeypot · isolation · judge
                            · policy · responder · session · store · telemetry · common/api/*
  modules/deception/mirror/cmd/mirror →  modules/deception/mirror · common/api/*
  modules/deception/proxy/cmd/proxy   →  modules/deception/proxy · modules/deception/injection · common/api/*
```

实测得出的**四条结构性质**：

| 性质 | 复核命令 |
| --- | --- |
| `contract` 是唯一被全部核心模块依赖的包 —— 进程内的「词汇表」 | `go list -deps ./common/core/... \| grep contract` |
| `modules/deception/injection` 的 shen 内部依赖数为 **0** —— 它连 `contract` 都不依赖 | `go list -f '{{.Imports}}' ./modules/deception/injection` |
| 两个适配器对 `common/core/` 依赖数为 **0** —— `ST-3` 由**编译器**强制 | `go list -f '{{.Imports}}' ./modules/deception/proxy` |
| `control` 是唯一同时依赖多个同级模块的包 —— 它是**接线板**，不是业务逻辑 | 见上方依赖图 |

> ⚠️ **策略面只差 `Watch`**：`Pull` 轮询 + `Ack` 回执**已落地**（2026-09-19，见 §1.6.4）；
> `Watch`（服务端流）仍是 `Unimplemented`（[ADR-0018](../../docs/background/decisions/0018-policy-plane-pull-model.md)）——
> 所以改道后端表 / 白名单 / 响应改写规则 / AI 欺骗内容现在**都能到边缘**，靠的是 Pull；
> 仍未接通的只剩**诱饵资产**（`decoy`）到边缘的通路。

#### 1.6.3 各模块对外的唯一出口

依赖方**只依赖 interface**。本项目约定**接口由消费方定义**，故下表同时给出接口所在处：

| 包 | 导出的接口 | 方法签名 |
| --- | --- | --- |
| `control` | `Decider` · `IsolationChecker` | `Decide(ctx, JudgeRequest) (Decision, error)` · `Check(ctx, SessionKey) (IsolationHit, error)` |
| `judge` | `Judge` · `RuleSource` | `Judge(ctx, JudgeRequest) (Verdict, error)` · `Rules(ctx) ([]Rule, error)` |
| `director` | `Director` · `ThresholdSource` · `GraySource` | `Decide(...)` · `Thresholds(ctx) (Thresholds, error)` · `GrayPct(ctx) (uint8, error)` |
| `session` | `Session` | `Key(ctx, Observation) (SessionKey, error)` |
| `decoy` | `Surface` · `AssetStore` | `Enabled` · `Match` · `Variant` · `Placements` |
| `honeypot` | `Pool` | `Resolve` · `List` · `Register` · `SetEnabled` · `SetHealthy` · `Types` |
| `responder` | `Responder` · `ContentStore` | `Respond(ctx, RespondRequest) (RespondOutput, error)` |
| `isolation` | `Isolation` · `Store` | `Check` · `Isolate` |
| `telemetry` | `Telemetry` · `Sink` | `Report` / `ReportBatch` · `Write` |
| `policy` | `Policy` | `Rules` · `Snapshot` · `Thresholds` · `GrayPct` · `Decoys` · `Honeypots` · `Whitelist` |
| `store` | 7 个实体接口 | `SessionStore` · `IsolationStore` · `DecisionStore` · `EventStore` · `PolicyStore` · `DecoyStore` · `ContentStore` |
| `modules/deception/injection` | `Injector` | `Inject(contentType string, body []byte) ([]byte, bool)` |
| `modules/deception/proxy` | `JudgeClient` · `TelemetryClient` · `Injector` | 见 [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) |

> `Store`（isolation）· `AssetStore`（decoy）· `ContentStore`（responder）· `IsolationChecker`（control）· `Injector`（proxy）
> 都是**消费方自定**的最小接口 —— 因此**没有任何模块 import 另一个模块的具体类型**。

#### 1.6.4 模块接入点（接缝现状）

| 接缝 | 实现方 | 现状 |
| --- | --- | --- |
| `control.Decider` | `control.ShadowDecider`（影子，恒放行）/ `director`（接管） | ✅ **已接**（`config.shadow` 决定装配哪个） |
| `judge.RuleSource` | `policy`（配置文件装载） | ✅ **已接** |
| `control.IsolationChecker` | `isolation` | ✅ **已接**（`WithIsolation`，命中即短路） |
| `store.EventStore` / `PolicyStore` / `DecisionStore` / `SessionStore` / `IsolationStore` / `DecoyStore` / `ContentStore` | 内存实现（生产应换 ClickHouse / PostgreSQL / Redis） | 🟡 已接内存实现；**`ContentStore` 自 2026-09-20 起有真实消费方**（`policy` 装载期 `Put` / 投影期 `Get`，见 [ADR-0023](../../docs/background/decisions/0023-deception-content-injection.md)） |
| **`common/api/policy/v1`（策略面 S4）** | `policy.Server`（核心侧）+ `modules/deception/proxy` 的策略客户端 | ✅ **已接**：`Pull` 轮询 + `Ack` 回执（`ST-8` / `AR-13`）· `Watch` 未实现（见 [ADR-0018](../../docs/background/decisions/0018-policy-plane-pull-model.md)） |
| `decoy` / `responder` / `honeypot` 到边缘的通路 | 经策略面下发 | 🟡 **部分接通**：改道后端表 · 白名单 · **响应改写规则（`injects` → `inject_rules`）** · **AI 欺骗内容（`ai-capability` → `content_manifest`）** 已能下发；**诱饵资产**仍未接通路（核心侧尚无「谁产出、存在哪」的定义） |

> ✅ **2026-09-19：策略面已落地** —— 核心不再只能靠「改 env + 重启适配器」传递改道后端表：
> 版本 + 校验和 + 回执三条都有了（`ST-8` / `AR-13` 首次真正落地）。
> ✅ **2026-09-20：AI 欺骗内容也已落地**（阶段 A）—— `ai-capability` 离线生成 → 护栏 → 清单 → 核心装载 →
> 策略面 `content_manifest` → 适配器注入改道侧（[ADR-0023](../../docs/background/decisions/0023-deception-content-injection.md)）。
> 仍未接通的只剩**诱饵资产**（`decoy`）到边缘的通路：需要先在核心侧定下「谁产出、存在哪」，
> 见 [ADR-0018](../../docs/background/decisions/0018-policy-plane-pull-model.md) 的「未解决」。


### 1.7 规则

| ID | 规则 | 验证方式 |
| --- | --- | --- |
| **ST-1** | 代码**必须**按 §1.1 的顶层目录组织；**禁止**在顶层新增目录而不更新本节。 | `make archcheck` + 评审 |
| **ST-2** | 一个进程的源码**必须**只在一个顶层目录内（`modules/` / `common/` / `analysis/` 各自独立）；**禁止**跨目录 import 源码。 | `make archcheck` |
| **ST-3** | 适配器**禁止** import 核心内部包（`common/core/internal/`），**只能**经 `common/api/` 生成的 stub 调用。 | **编译期强制** —— 见下方说明 |
| **ST-4** | 各层**禁止**跨层 import。 | `make archcheck` |
| **ST-5** | L1 边缘处置模块（`modules/deception/injection/`）**禁止**独立部署；它**必须**作为适配器内的模块被引用。 | 部署清单评审 |

> ⭐ **强制机制（2026-09-17 定）**
>
> | 规则 | 谁来强制 |
> | --- | --- |
> | `ST-1` 顶层目录组织 | `make archcheck` |
> | `ST-2` 禁止跨顶层目录 import | `make archcheck` |
> | `ST-3` 禁止 import 核心内部包 | **编译器**（Go 的 `internal/` 目录规则） |
> | `ST-4` 禁止跨层 import | `make archcheck` |
>
> `scripts/archcheck/` 的清单**从本文档与 [`modules.md`](modules.md) 解析，不硬编码** ——
> 改文档即改检查，不会两边漂移；解析结果不合预期时它**直接报错退出**，不静默放行。
>
> 门禁全套是 `make gate`（格式化 / `go vet` / `staticcheck` / `errcheck` / 架构检查 / 许可审计 / 单测含 `-race`）。
> **工具缺失时门禁失败，不跳过** —— 静默跳过等于假绿。
>
> ⭐ **`ST-3` 由编译器强制，不靠人工检查。**
> 把进程内共享类型放在 `common/core/internal/contract/`，使 Go 的 `internal/` 目录规则生效：
> **`common/core/internal/` 下的任何包，只有 `common/core/` 子树内的代码能 import**。
> 适配器（`modules/deception/`）因此在**编译期就无法**拿到核心内部类型 —— 只能走 `common/api/` 的 proto stub。

---

## 2. 契约与接缝

### 2.1 三个平面

| 平面 | 服务 | 方法 | 幂等键 | 超时与降级 |
| --- | --- | --- | --- | --- |
| **判定面** | `DeceptionJudge` | `Judge` | `decision_id` | deadline ≤ 3 ms；超时 fail-open = 放行 |
| **遥测面** | `DeceptionTelemetry` | `Report` / `ReportBatch` | `event_id` | 适配器侧仅做受限长度的尽力转发，不阻塞请求路径；可靠重放由核心侧接收缓冲 + 落库补偿承担 |
| **策略面** | `DeceptionPolicy` | `Pull` / `Watch` / `Ack` | `(policy_id, version)` | 拉取失败沿用本地缓存版本 |

### 2.2 接缝清单

| # | 接缝 | 格式 | 稳定性 |
| --- | --- | --- | --- |
| **S1** | 接入层 ↔ 核心 | gRPC / Protobuf（`common/api/`） | ✅ 我们控制，版本化 |
| **S2** | 适配器 ↔ 核心 | gRPC over `common/api/*.proto`（`TB-26` 版本号在包路径） | ✅ 随 proto `vN` |
| **S3** | 核心 ↔ L4 分析（Python） | gRPC / Protobuf，**异步非阻塞** | 我们控制，**必须**带版本号 |
| **S4** | 策略 | 控制面 → 核心：YAML + JSON Schema（[`../spec/config.md`](../spec/config.md)）；核心 → 适配器：gRPC `common/api/policy/v1` + **JSON 载荷**（[`../spec/policy-payload.md`](../spec/policy-payload.md)） | 我们控制，只读 |
| **S5** | 模型产物 | ONNX / 特征表 | **必须**带版本号，只读 |
| ❌ | FFI · CGO · 共享内存 · 嵌入式 Python | — | **禁止**（见 [`language.md`](language.md) 的 TB-24） |

| ID | 规则 | 验证方式 |
| --- | --- | --- |
| **ST-6** | `common/api/` **必须**是契约的唯一事实源；各语言客户端**必须**由 `.proto` 生成，**禁止**手写。对外文档**必须**由该定义生成。 | 代码生成检查 + 文档一致性测试 |
| **ST-7** | 判定面**禁止**在对外响应中回显分值、规则名或决策枚举。 | 自动检查 + 集成测试 |
| **ST-8** | 策略面**必须**携带版本号与校验和，并支持回滚。 | 契约评审 |
| **ST-9** | 遥测面**必须**幂等：同一 `event_id` 重复上报不产生重复记录。 | 重放测试 |
| **ST-10** | `decision_id` **必须**由适配器按（来源标识, 会话, 路径, 时间窗）派生并随请求传入；核心**必须**在响应中原样回传，重试复用同一 ID。 | 集成测试 |
| **ST-11** | 判定面未命中缓存时才调用；适配器侧**必须**实现超时降级。 | 集成测试 |
| **ST-12** | 核心与适配器**必须**分目录，编译产物与发布节奏独立。 | 发布流程评审 |

---

## 3. 数据模型

| 实体 | 存储 | 主键 | 关键字段 | 写入语义 |
| --- | --- | --- | --- | --- |
| `decision` | Redis（缓存）+ ClickHouse（归档） | `decision_id` | source_ip、session_id、score、signals、action、created_at | 两级去重：Redis 按 `decision_id` 在 TTL 窗口内去重；ClickHouse 按 `decision_id` 最终去重 |
| `event` | ClickHouse | `event_id` | event_type、actor_id、session_id、payload、created_at | 只增不改，批量写入；去重同上 |
| `session` | Redis（TTL） | `session_id` | actor_id、first_seen、last_seen、watermark_token、state | 有 TTL，过期自动回收 |
| `actor` | PostgreSQL | `actor_id` | 指纹、置信度、来源、画像标签、首次与最近出现时间 | 可更新 |
| `decoy` | PostgreSQL | `decoy_id` | 类型（API 路由 / 文件 / 凭证）、投放位置、绑定关系 | 可更新 |
| `honeypot` | PostgreSQL | `honeypot_id` | 协议、端口、所在节点、运行状态 | 状态与实况对账 |
| `isolation` | Redis（生效）+ PostgreSQL（审计） | `(kind, value)` | reason、expires_at | TTL 生效，到期自动解除 |
| `policy` | PostgreSQL | `(policy_id, version)` | payload、checksum、灰度比例、生效范围、回滚指针 | 版本只增，回滚即发布新版本 |
| `policy_ack` | PostgreSQL | `(policy_id, version, adapter_id)` | `applied`、`reason`、`received_at` | **幂等**：同键覆盖（适配器重启会重报）；`applied=false` 必须带原因（`AR-13` 版本对账） |
| `chain` | PostgreSQL | `chain_id` | 阶段、置信度、结论 | 结论写入前**必须**校验引用证据存在 |
| `chain_node` | PostgreSQL | `(chain_id, event_id)` | 引用关系 | 只增不改，构成证据链 |

关系：

```text
actor 1 ──< session 1 ──< event >── 1 decision
  │                          ▲
  └──< chain 1 ──< chain_node ┘        （证据引用，多对多）
decoy 1 ──< honeypot                   （蜜饵与蜜罐绑定）
policy ──► 下发至 L1 / L2 / L3          （按版本与灰度比例）
isolation ──► L1 短路判定               （命中即不再调用核心）
```

| 存储 | 承载 | 选择理由 | 代价 |
| --- | --- | --- | --- |
| **Redis** | 会话、判定缓存、限流、幂等去重 | 低延迟读、原生 TTL | 容量受内存限制；**必须**定义持久化与丢失后的降级行为 |
| **ClickHouse** | 遥测事件 | 写入吞吐高、聚合查询快 | 主键去重非强一致；不适合频繁更新；**必须**按时间分区与保留策略 |
| **PostgreSQL** | 策略、资产、画像、攻击链 | 事务与关系完整性 | 写入吞吐低，不适合承载逐请求数据 |

| ID | 规则 | 验证方式 |
| --- | --- | --- |
| **ST-13** | 数据模型**必须**与 §3 一致；新增实体或字段**必须**同步更新本节与 `../spec/logs.md`。 | 评审 |
| **ST-14** | ClickHouse 的最终去重**必须**在查询期显式处理（不能假设写入即去重）。 | 集成测试 |
| **ST-15** | Redis 不可用时的降级行为**必须**显式定义并测试（fail-open，不阻断业务）。 | 故障注入 |

---

## 4. 部署拓扑与进程边界

### 4.1 传统虚机形态

```text
用户 → LB → Go 代理（L1）─┬─► 真实业务集群（不经核心）
                                     └─► 蜜罐集群（引流目标）
        核心集群（无状态 × N） ← gRPC ─ L1 插件 / 适配器
        Redis · ClickHouse · PostgreSQL
```

### 4.2 K8s 形态

```text
用户 → Gateway / Ingress（Envoy）
         ├─► 业务 Pod（含 Sidecar 适配器）→ 真实业务容器
         └─► 蜜罐 Deployment / StatefulSet
        核心 Deployment（无状态 × N）
        Cilium（微隔离）· Tetragon（运行时检测）
        Redis · ClickHouse · PostgreSQL（Operator 管理）
```

| 组件 | 进程形态 | 边界约束 |
| --- | --- | --- |
| L1 适配器 | 独立进程（Go） | **禁止**阻塞调用；**禁止**持有跨请求业务状态 |
| 核心 | 独立进程，无状态多副本 | 进程内**禁止**模块级可变容器（AR-9） |
| L4 分析 | 独立进程（Python） | 按任务队列并发，**禁止**直接写判定缓存 |
| 蜜罐 | 每协议独立进程 / 容器 | 连接数与线程数**必须**有上限；启停资源**必须**对称回收 |
| 控制台 | 独立进程 | 以只读为主，写操作**必须**走策略平面并留审计 |

| ID | 规则 | 验证方式 |
| --- | --- | --- |
| **ST-16** | 部署**必须**满足 §4 的进程边界；**禁止**把重逻辑（LLM 推理、蜜罐仿真）塞进边车。 | 部署清单评审 |
| **ST-17** | 存活探针**必须**只反映进程存活；就绪探针**必须**反映依赖可用。 | 部署评审 + 故障注入 |
| **ST-18** | 高风险能力（容器运行时套接字挂载、宿主网络模式、免密提权、无沙箱执行）**禁止**在同一部署单元叠加；CI **必须**做组合检查。 | CI 组合检查 |
| **ST-19** | 以宿主权限运行的模式**必须**在配置项、启动日志与文档中显式标注。 | 部署评审 |

---

## 5. 配置与密钥

| ID | 规则 | 验证方式 |
| --- | --- | --- |
| **ST-20** | 含密钥的配置文件**必须**加入忽略清单；配置模板**必须**只保留占位符。 | CI 密钥扫描 |
| **ST-21** | 密钥、口令类配置项**禁止**有默认值；缺失时进程**必须**启动失败并给出明确错误。 | 单元测试（不配置必失败） |
| **ST-22** | 话术与提示词模板**必须**随代码分发（存于各模块内资源目录），由配置项选择模板组；**禁止**存放于运行期可变存储。 | 部署评审 + 启动期校验 |
| **ST-23** | 判定阈值、隔离有效期、去重窗口**必须**集中在一处定义，并可由配置覆盖。 | 代码评审 |
| **ST-24** | 策略**必须**是数据（配置文件），**禁止**编译进代码。（原 `TB-12`，随结构文档迁移而改号；语义不变） | 评审 + 配置 schema 测试 |

---

## 6. 未决项

| # | 未决 | 去向 |
| --- | --- | --- |
| 1 | 仓库名与二进制名（原 `shen` 的命名体系随术语变更待复算） | [`terminology.md`](terminology.md) · [ADR-0004](../background/decisions/0004-terminology.md) |
| 2 | `common/api/` 的语言绑定生成工具链 | 实现期决定 |
| 3 | eBPF 数据面是否进 MVP（影响 `ST-1` 的 `modules/deception/netpolicy/`） | [`../background/notes/implementation-discussion.md`](../background/notes/implementation-discussion.md) |
