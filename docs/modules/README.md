# 模块索引

> **权威清单在 [`../design/modules.md`](../design/modules.md) §1.1**（**24 个有效模块**，共 25 行；第 12 行 `adapter-sidecar` 已合并）。
> **实现进度与完成度见 [`../progress.md`](../progress.md)**（进度唯一维护处，与 [`../log.md`](../log.md) 同在 `docs/`，供人工审计对照）。
> 本文件回答：模块文档在哪 · 下一步做什么 · 怎么新增模块。
> **不知道从哪看起？**先看 [`_map.md`](_map.md)：每个模块的目录 · 能力 · 对外接口 · 接线一张表列全。


| 项 | 值 |
| --- | --- |
| 权威清单 | [`../design/modules.md`](../design/modules.md) §1.1 |
| 实现进度 / 完成度 | [`../progress.md`](../progress.md) |
| 代码结构实况（包级依赖 / 进程边界 / 接缝） | [`../design/structure.md`](../design/structure.md) **§1.6** |
| 文档规则 | `MD-2`（一模块一文件）· `MD-17`（实现前必须创建）· `MD-18`（新增模块须先改清单） |
| 模板 | [`_template.md`](_template.md) |

---

## 0. 模块总览（先看这里）

> 本节是给「分析设计是否符合想法」看的入口。权威定义在 [`../design/modules.md`](../design/modules.md) §1.1；
> 代码结构实况（包级依赖 / 进程边界）在 [`../design/structure.md`](../design/structure.md) §1.6。

### 0.1 结构图（分层）

```text
┌──────────────────────────────────────────────────────────────────────────┐
│ L0 接入层 —— 不自研，复用开源（AR-3）                                      │
│   Envoy / Nginx / HAProxy / CoreDNS · TLS 卸载 · 流量镜像 · 路由           │
└───────────────────────────────────┬──────────────────────────────────────┘
                                    │
┌───────────────────────────────────▼──────────────────────────────────────┐
│ L1 边缘欺骗层（Go）—— 只执行处置，不做判定（AR-2 / MD-9）                  │
│   adapter-mirror ①   adapter-dns ②   adapter-proxy ③④                    │
│   edge-injection（投毒改写，被适配器引用，不独立部署 · ST-5）              │
└───────────────────────────────────┬──────────────────────────────────────┘
                                    │ gRPC（接缝 S1）
┌───────────────────────────────────▼──────────────────────────────────────┐
│ 核心（Go）—— 判定与响应生成的唯一实现 · 无状态多副本（AR-9）             │
│   control 服务面 · judge 判定 · director 决策 · responder 响应生成         │
│   session 会话 · isolation 隔离 · policy 策略 · decoy 诱饵面              │
│   honeypot 蜜罐入口 · telemetry 遥测 · store（唯一 I/O 出口）           │
└───┬──────────────────────────┬─────────────────────────┬────────────────┘
    │                          │                         │
    ▼                          ▼                         ▼
┌─────────────────┐  ┌──────────────────┐  ┌──────────────────────────┐
│ L2 蜜罐         │  │ L3 网络层         │  │ L4 分析层（Python）      │
│ honeypot-       │  │ netpolicy        │  │ intent · chain · strategy│
│   protocol/shell│  │ 微隔离 + 假拓扑   │  │ llm-components           │
│ （可选自研）     │  │ （复用 Cilium）  │  │ （LLM 纪律 + 注入防护）  │
└─────────────────┘  └──────────────────┘  └──────────────────────────┘

数据面（开源组件）：Redis（会话/缓存/隔离）· ClickHouse（遥测）· PostgreSQL（策略/资产/链）
控制台：console（TypeScript）
```

### 0.2 关系图（数据流 + 依赖）

```text
   Agent / 爬虫 / 正常用户
              │
              ▼
   ┌──────────────────────┐
   │ adapter-mirror/dns/  │  1. 拦截请求  2. 查本地缓存  3. 调核心  4. 异步上报
   │ proxy（L1）         │     白名单先于引流判定（INT-25）
   └──────────┬───────────┘
              │ gRPC（S1）deadline ≤ 3ms → 超时 fail-open（NI-4）
              ▼
   ┌──────────────────────┐
   │ control（服务面）     │     入参映射 · 会话身份提取 · 熔断（NI-10）
   └──────────┬───────────┘
              ▼
   ┌──────────────────────────────────────────────┐
   │ 判定链（不写响应，只产出结论）                   │
   │  session（身份+状态）→ judge（规则+指纹）       │
   │  isolation（隔离短路，命中则不调核心）            │
   └──────────┬───────────────────────────────────┘
              ▼
   ┌──────────────────────┐
   │ director              │     三值：route_origin / route_mirage / block
   │                      │     + severity（旁路）+ 灰度
   └────┬─────────────┬────┘
        │             │ route_mirage
        │ route_origin│
        ▼             ▼
   ┌─────────┐   ┌──────────┐        ┌────────────┐
   │ 真实业务 │   │ honeypot │◄─────│ decoy      │
   │ （不经核心）│  │ 后端池    │  诱饵  │ 诱饵面     │
   └─────────┘   └────┬─────┘  定义 └────────────┘
                        │                    │
                        ▼                    ▼
                   L2 蜜罐 / 第三方     responder（生成欺骗响应）
                                              │
                                              ▼
                                        edge-injection（改写，仅蜜罐侧 INT-8）

   旁路（不在请求路径上）：
     telemetry（事件归一化 → 上报）· store（唯一 I/O）· policy（策略下发）
   分析闭环（近线/离线，不回流响应 AR-32）：
     telemetry → llm-components → intent → chain → strategy → policy → 下发
   运营：console（策略编排 · 资产管理 · 审计查询）
```

### 0.3 全部模块一览（24 个有效模块）

#### 核心（11）—— 判定与响应生成的唯一实现

| # | 模块 | 一句话解释 | 阶段 |
| --- | --- | --- | --- |
| 1 | `judge` | **判定**：把观测变成风险分 + 信号 + 证据；含 Agent 指纹（置信度/证据链）与消费会话特征 | 1 |
| 2 | `director` | **决策**：把风险分映射为三值（放行 / 改道 / 拦截）+ 后端名，按灰度确定性收敛 | 2a |
| 3 | `responder` | **响应生成**：产出「看起来像真实业务」的响应（快路径模板 + 慢路径预生成）；同会话同资源同答案（AR-30） | 2b |
| 4 | `session` | **会话**：三级优先级提取身份 + 会话状态（速度/挑战逃逸）+ 归因令牌（蜜标/水印） | 1 |
| 5 | `isolation` | **隔离**：隔离记录 / TTL 过期 / 查询短路（命中则不调核心，客户端不可见） | 2b |
| 6 | `policy` | **策略**：规则与阈值的版本化装载 + 灰度比例 + 下发与回执 | 2a |
| 7 | `telemetry` | **遥测**：事件归一化 + 批量异步上报 + 失败缓冲重放 | 1 |
| 8 | `store` | **存储**：Redis / ClickHouse / PostgreSQL 访问层 —— **核心唯一的 I/O 出口** | 1 |
| 22 | `control` | **服务面**：gRPC 入参映射 · 会话身份提取 · **禁止回显的强制点** · 熔断（NI-10） | 1 |
| 23 | `decoy` | **诱饵面**：五类（Developer API / 指令文件 / MCP / 数据集 / 蜜饵）+ 多态轮换 | 2b |
| 24 | `honeypot` | **蜜罐入口**：类型注册 + config 开关 + 后端池解析；**不实现具体蜜罐**（接第三方） | 2b |

#### L1 边缘（4）—— 只执行处置

| # | 模块 | 一句话解释 | 阶段 |
| --- | --- | --- | --- |
| 10 | `adapter-mirror` | **① 旁路镜像**接收端：只读采集，**不处置**（不在请求路径上） | 1 |
| 11 | `adapter-proxy` | **③ 前置 + ④ 边车**同一实现：拦截 → 调核心 → 按决策选 upstream → 异步上报 | 2a |
| 13 | `adapter-dns` | **② DNS 引流**：按来源解析到引擎或真实服务（**纯配置，无源码**） | 2a |
| 9 | `edge-injection` | **投毒改写 / 假路径 / 蜜饵注入**；被适配器引用，不独立部署 | 2b |

#### L2 蜜罐 / L3 网络 / L4 分析 / 控制台（9）

| # | 模块 | 一句话解释 | 阶段 |
| --- | --- | --- | --- |
| 14 | `honeypot-protocol` | 协议仿真（SSH / MySQL / Redis / FTP…）+ 交互捕获；**可选自研** | 3 |
| 15 | `honeypot-shell` | 命令表分发 + 内存文件系统 + 文件投递（含会话水印）；**可选自研** | 3 |
| 16 | `netpolicy` | 微隔离 + 假拓扑 + 运行时检测（复用 Cilium / Tetragon） | 3 |
| 17 | `intent` | 意图识别（侦察 / 利用 / 横向 / 窃取） | 3 |
| 18 | `chain` | 攻击链还原 + **识破信号识别**（触发诱饵再生成） | 3 |
| 19 | `strategy` | 策略生成 + 诱饵再生成决策（经 policy 下发） | 3 |
| 20 | `llm-components` | LLM 契约纪律 + **间接注入防护** + 内容预生成 | 3 |
| 25 | `ai-capability` | **AI 能力服务**：唯一生成出口 + **强制护栏**（缺护栏档案即启动失败）+ 欺骗内容与清单 | 3 |
| 21 | `console` | 控制台：策略编排 · 资产管理 · 人工干预 · 审计查询 | 2b |

> 编号不按位置递增（`control` 编号 22、`decoy` 23、`honeypot` 24、`ai-capability` 25）—— 编号是引用句柄，保持稳定（`MD-18` / `D-6` 精神）。
> 第 12 行 `adapter-sidecar` **已合并入** `adapter-proxy`，不构成独立模块。

### 0.4 运行时调用链（实测：哪些模块**真的被调用**）

> 依据装配层代码与 `go list` 实测。**「已装配」≠「在调用链上」** —— 本表如实区分两者。

**进程 1 · `shen-core`** —— 判定与决策的唯一实现

```text
适配器 ──gRPC（接缝 S1）──► control.JudgeService
                              │
                              ├─ ① 熔断检查      breaker.Allow()        ← NI-10
                              ├─ ② 会话身份      session.Key()          ← INT-19
                              ├─ ③ 隔离短路      isolation.Check()      ← 命中即回放行，客户端不可见
                              └─ ④ 决策          control.Decider
                                                   ├─ 影子模式 → ShadowDecider → judge.Judge()
                                                   └─ 接管模式 → director.Decide()
                                                          ├─ 白名单短路（INT-25）
                                                          ├─ judge.Judge() → policy.Rules()      （判定）
                                                          ├─ 阈值/灰度 ← policy.Thresholds()/GrayPct()
                                                          └─ 灰度收敛 + 诱饵面豁免 block（MD-25）

适配器 ──gRPC──► control.TelemetryService → telemetry.Collector → store.EventStore
```

**进程 2 · `shen-mirror`（① 旁路镜像，不在请求路径上）**

```text
请求副本 → 取观测 → 算 decision_id → core.Judge → core.Report → 恒回 202
```

**进程 3 · `shen-proxy`（③ 前置 / ④ 边车，在请求路径上）**

```text
请求 → 白名单（INT-25）→ 本地判定缓存（AR-6）→ core.Judge → 按三值处置
         ├─ route_origin → 业务 upstream（原样透传）
         ├─ route_mirage → 查**适配器自己的**引流表（策略面下发优先 + 本地 env 兜底）
         │                  → 静态规则 / AI 内容注入（只改改道侧，INT-8）→ 后端
         └─ block        → 403（默认不产出，Q5）
```

**已装配、但当前不在调用链上的模块**（**缺口，不是缺陷**）：

| 模块 | 现状 | 为什么到不了边缘 |
| --- | --- | --- |
| `decoy` | 仅启动装配 + 日志 | **诱饵资产**的边缘通路仍未接（策略面已实现，只差它的来源与归属） |
| `responder` | 仅启动一致性子检 + 装配 | 它自己的 `(会话,资源)` 缓存通路仍未接；但 **L4 离线预生成的 AI 欺骗内容已接通**（`ai-capability` → `content_manifest` → 适配器，[ADR-0023](../background/decisions/0023-deception-content-injection.md)） |
| `honeypot` | 启动装配 + 日志 | 后端池**已能经策略面下发**（`backends`）；适配器本地 `SHEN_PROXY_MIRAGE` 保留为拉不到时的兜底 |

> ⭐ **当前欺骗引擎的断点**：策略面（`S4`）**已落地**（`Pull` 轮询 + `Ack` 回执，[ADR-0018](../background/decisions/0018-policy-plane-pull-model.md)），
> 改道后端表 · 白名单 · 静态注入规则 · **AI 欺骗内容清单** 四条已能到边缘。
> 还差的一条是**诱饵资产**（`decoy`）—— 它需要先在核心侧定下「谁产出、存在哪」。

---

## 1. 进度与完成度 → 已迁出

模块清单 · 实现方式 · 阶段状态已整体迁至 [`../progress.md`](../progress.md)，
与 [`../log.md`](../log.md)（变更日志）同在 `docs/`，供人工审计对照。
**每轮开发落地后必须同步更新 `progress.md`，并在 `log.md` 追加一条。**

---

## 3. 文档状态

阶段 1 的 6 个模块**文档与代码都已就位**（`MD-2` / `MD-17`）。

| 偏差记录 | 说明 |
| --- | --- |
| `MD-17` | 实现顺序上先写了代码、后补文档，与该条要求的顺序相反。已于 2026-09-17 补齐，**留此记录不再重复** |
| `MD-18` | `control` 已在代码中实现，但尚未并入 [`../design/modules.md`](../design/modules.md) §1.1 的 21 行清单 —— ✅ **已闭合**（2026-09-17 确认后并入，见该文件 §1.1 第 22 行） |
| 开发期工具 | `scripts/devcheck/`（在线冒烟）与 `scripts/dev/smoke.sh`（一键验证）**不是模块**，属 `scripts/` 的工具（`MD-19` 的豁免范围）；`Makefile` 新增 `check-config` / `replay` / `smoke` / `dev` 四个开发期目标 |

---

## 4. 下一步可选模块

阶段 1 已落地。下面每一行都标出**它插进现有代码的位置** ——
骨架预留的接口及其「阶段 1 由谁顶」见 [`../design/structure.md`](../design/structure.md) **§1.6.4**。

### 4.1 阶段 2 · 核心（插入现有进程，不新增二进制）

| 模块 | 插在哪 | 现状 | 前置条件 |
| --- | --- | --- | --- |
| `policy` | 实现 `judge.RuleSource` | ✅ **已实现**（配置装载 + 版本台账 + 规则供给）；下发面（`api/policy/v1`）仍未接线 | **无** |
| `director` | 实现 `control.Decider` | ✅ **已实现**（阈值→三值 + 灰度 + 后端名，[`../modules/director.md`](../modules/director.md)）；`severity` 仍固定 `none` | **无**（`policy` 已就绪） |
| `isolation` | 调用 `store.IsolationStore` | 接口已实现但**当前无人调用** | `session` 已就绪（无阻塞） |
| `responder` | 被 `director` 调用 | **无预留接缝**（要新定接口） | 需要 `director` 先定后端选择契约 |
| `store` 真实后端 | 实现 5 个 store 接口 | 内存实现顶着 | 需选定 Redis / PostgreSQL / ClickHouse 方案 |
| `telemetry` 真实落库 | `store.EventStore` 换成真实实现 | 内存实现顶着 | 同上（事件表按天分区） |
| **`decoy`**（诱饵面） | 新模块（阶段 2b） | 设计中（[`../modules/decoy.md`](../modules/decoy.md)） | **无**（与 `director` 的 `route_mirage` 配套）—— 依据 [ADR-0010](../background/decisions/0010-functional-camouflage.md) |
| **`honeypot`**（蜜罐入口） | 新模块（阶段 2b） | 设计中（[`../modules/honeypot.md`](../modules/honeypot.md)） | 需定义蜜罐接入契约；具体蜜罐接第三方 —— 依据 [ADR-0011](../background/decisions/0011-honeypot-entry-external-backends.md) |

### 4.2 阶段 2 · 数据平面

③④ 已合并为 `adapter-proxy`（Go）并实现。数据平面剩下的工作：

| 模块 | 语言 | 现状 |
| --- | --- | --- |
| `adapter-proxy`（③前置 + ④边车） | Go | ✅ 已实现（39 测试，含内嵌 Caddy 端到端与策略面消费）；**需要 `director` 能产出真实决策后才有处置可做** |
| `adapter-dns`（② DNS 引流） | 配置 | 🟡 `Corefile.example` 就绪；同上依赖 `director` |
| `edge-injection` | Go | 被适配器引用，**不独立部署**（`ST-5`）；阶段 2b |

### 4.3 阶段 2 · 控制台

| 模块 | 语言 | 前置条件 |
| --- | --- | --- |
| `console` | TypeScript | 需要 `policy` 的下发契约与回执对账 |

### 4.4 阶段 3

L2 / L3 / L4（`honeypot-protocol` · `honeypot-shell` · `netpolicy` · `intent` · `chain` · `strategy` · `llm-components`）
—— 依赖阶段 2 完成。

### 4.5 建议顺序

1. ~~**`policy`** —— 唯一「零前置」的阶段 2 模块，且 `api/policy/v1` 已在等它；做它能同时闭合阶段 1 的空规则集~~ ✅ **已完成（2026-09-18）**
2. **`director`** —— 让引擎从「只算分」变成「能处置」，**项目的核心价值在此**
3. **真实存储**（`store` / `telemetry` 后端）—— 影子模式跑两周就需要真实落库
4. **`isolation`** —— 与 `director` 配套（判定前短路）
5. **适配器** —— `director` 就绪后才能验证处置

> ⚠️ **两个非模块的阻塞项**（见 [`../background/notes/pending-experiments.md`](../background/notes/pending-experiments.md)）：
> `E2`（TLS 指纹一致性）是 **P0**，其结论可能推翻整个欺骗命题；`D0`（架构律）尚未拍板。

---

## 5. 如何新增一个模块

1. 先在 [`../design/modules.md`](../design/modules.md) §1.1 **加一行**（`MD-18`），并同步 [`../design/structure.md`](../design/structure.md) §1 的目录树
2. 用 [`_template.md`](_template.md) 创建 `docs/modules/<模块名>.md`（`MD-2` / `MD-17`，**一节都不许删**）
3. 建源码目录（`MD-19`：清单外的目录**禁止**承载业务逻辑）：
   - 核心模块 → `core/internal/<模块名>/`
   - L1 模块 → `deception/<模块名去 adapter- 前缀>/`
   - L2/L3 → `deception/<模块名>/` · L4 → `analysis/<模块名>/` · 控制台 → `console/`
4. Go 模块三个文件：`iface.go`（导出的 interface）+ `<模块名>.go` + `<模块名>_test.go`（`MD-22`）
5. 若属阶段 2/3：**禁止**现在实现（`MD-21`），除非先取得用户确认并更新阶段标记
6. **落地后更新**：本文件 §3 与 [`../progress.md`](../progress.md) 的状态列 → [`../design/structure.md`](../design/structure.md) §1.5 与 **§1.6**（包级地图）
