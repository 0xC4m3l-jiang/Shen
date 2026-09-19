# 变更包 · 2026-09-18 · 代码整理 + 调用链核实 + 文档站完善

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 整理装配代码；核实模块**真实调用链**；把结构文档与文档站入口更新到实况；登记唯一结构性断点 |
| 日期 | 2026-09-18 |
| 状态 | 已实现 |
| 涉及模块 | `cmd/core`（整理）· 文档：`docs/README.md` · `docs/design/structure.md` · `docs/modules/README.md` · `decoy` / `responder` / `honeypot` 模块文档 |
| 决策数 | 已答 0 项 / 待定 1 项（策略面，见 §4） |
| 关联 | [`../log.md`](../log.md) 同日条目 |

## 1. 需求与验收

**要解决什么**：三件事 —— ① 整理装配代码；② **核实模块到底怎么被调用**（装配 ≠ 在调用链上）；③ 文档站（`docs/` 这套 markdown）已失真，需按实况完善。

**验收判据**：

1. 装配代码更易读，且**行为不变**（测试全绿）。
2. 调用链有**以代码为准**的书面描述，且如实区分「已装配」与「在调用链上」。
3. 结构文档（自称「实测」的 §1.6）与文档站入口不再失准。
4. `make gate` + `make dev` 全绿。

## 2. 核实结论：模块**真实**如何调用

> 方法：`go list -f '{{.Imports}}'` 实测依赖 + 读三个 `main.go` + 反查每个模块接口的**消费者**。

### 2.1 在调用链上（运行时真的被调用）

```text
适配器 ──gRPC(S1)──► control.JudgeService
   ① breaker.Allow()        熔断          ← NI-10
   ② session.Key()          会话身份      ← INT-19
   ③ isolation.Check()      隔离短路      ← 命中即回放行，客户端不可见
   ④ decider.Decide()
        ├─ 影子 → ShadowDecider → judge.Judge() → policy.Rules()
        └─ 接管 → director.Decide()
                    ├─ 白名单短路（INT-25）      ← 本轮补齐
                    ├─ judge.Judge() → policy.Rules()
                    ├─ policy.Thresholds() / GrayPct()
                    └─ 灰度收敛 + 诱饵面豁免 block（MD-25）← 本轮补齐
适配器 ──gRPC──► control.TelemetryService → telemetry.Collector → store.EventStore
```

### 2.2 **已装配但不在调用链上**（缺口）

| 模块 | 反查证据 | 影响 |
| --- | --- | --- |
| `decoy` | `Surface.Match/Variant/Placements` 的调用者只有测试 | 诱饵定义到不了边缘 |
| `responder` | `Responder.Respond` 的调用者只有 `main.go` 的启动自检 | 生成能力到不了边缘 |
| `honeypot` | `Pool.Resolve` 的调用者只有测试；适配器用**自己的** `SHEN_PROXY_MIRAGE` | **两份事实源**（核心 config vs 适配器环境变量） |

### 2.3 根因：策略面（S4）未实现

`api/policy/v1` 已定义 `Pull` / `Watch` / `Ack`，但**核心侧没有服务端、边缘侧没有客户端**（`grep DeceptionPolicy core/ edge/` 零命中）。
于是「核心算出的诱饵 / 后端池 / 预生成内容」没有通路到 L1/L2/L3 —— **这是欺骗引擎当前唯一的结构性断点**。

## 3. 处置

### 3.1 代码整理（行为不变）

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `core/cmd/core/main.go` | 提取 `assembleDeception` + `deception` 结构 + `assertConsistency` | `run()` 从 ~200 行降到 **125 行**；装配线一眼可读；「构造 + 自检」从主流程里分离出来，后续接策略面时改动点集中在一处 |

> `deception` 结构只放**之后还会被用到**的东西（`surface` / `pool` / `isolate` / `assets`），
> 四个字段全部有读取点 —— 避免上一轮 review 里 `director.Engine.now` 那种「存了不用」的情况。

### 3.2 文档站完善（按实况）

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `docs/README.md`（站点入口） | 更新「现状」（阶段 2a 完成 / 2b 已实现未接线 / 断点提示）；`modules/` 行 21→**23** 个模块；新增两条导航（**调用链** / **模块总览**）；上手顺序加第 7、8 步 | 入口是文档站第一屏，计数与状态过期会误导所有读者 |
| `docs/design/structure.md` §1.6 | **重写** §1.6.1（三进程 + 每进程职责）· §1.6.2（实测依赖，含 4 条结构性质）· §1.6.3（各模块对外接口 + 消费者自定接口清单）· §1.6.4（接缝现状表） | 该节自称「实测」，但缺 5 个包、说「只产出两个二进制」（实为三个）、说 isolation「无人调用」（已接）—— 全部失真 |
| `docs/modules/README.md` §0.4 | **新增**「运行时调用链（哪些模块真的被调用）」 | 直接回答「模块如何调用」，并如实标出三个未接线模块 |
| `docs/modules/decoy.md` · `responder.md` · `honeypot.md` §8 | 各增一行「⚠️ 未接到请求路径」 | 让每个模块**自述**其缺口，读者不必回到总览才知道 |

## 4. 设计是否合理（评估）

**结论：分层与契约合理，但闭环缺一环。**

| 维度 | 判断 | 依据 |
| --- | --- | --- |
| 分层（L0 复用 / L1 执行 / 核心判定 / L2-L4 后端与分析） | ✅ 合理 | 与 `AR-2`/`AR-3`/`MD-4` 一致；`go list` 可证依赖单向、无环 |
| 「接口由消费方定义」 | ✅ 合理 | 5 个新模块只依赖 `contract`；`control`/`proxy` 各自定义所需接口，互不 import |
| 判定与执行分离 | ✅ 合理 | 判定只在 `judge`；适配器 `ST-3` 编译期读不到核心内部类型 |
| 降级（`NI-1` 优先） | ✅ 合理 | 超时 / 非法值 / 后端不可达 / 存储故障全部回落放行，均有测试 |
| **内容与配置的「下发通路」** | ❌ **缺** | 策略面 S4 未实现 → `decoy`/`responder`/`honeypot` 到不了边缘；后端池存在**两份事实源** |
| 后端池的唯一性 | ❌ **待统一** | 核心 `honeypot` 池 vs 适配器 `SHEN_PROXY_MIRAGE` —— 应统一到策略面下发 |

**唯一的 P0 建议**：**实现策略面（`api/policy/v1`）** —— 服务端在 `control`（或 `policy` 模块旁）、客户端在适配器；
它一旦落地，诱饵定义 / 后端池 / 预生成内容就能按版本下发（`ST-8`），欺骗引擎的闭环才真正闭合。

## 5. 验证证据

```console
$ make gate
门禁通过。  （fmt · vet · staticcheck · errcheck · archcheck · trace · licensecheck · go test -race）

$ go test -count=1 ./...
15 个包 ok                     ← 整理前后逐包一致（行为不变）

$ make dev
✅ 通过：配置校验 · 非法配置被拒 · 启动 · 判定面 · 冒烟 · 规则回放

$ 启动日志（证明装配与自检仍生效）
策略已装载 policy_id=core-rules version=1 …
诱饵面：0 个资产启用（observe-only，MD-25）
幻境后端池：0 个登记 / 0 个可用（不实现具体蜜罐，ADR-0011）
核心已启动（影子模式（只算判定、不处置，INT-11））
```

**净变化**：`run()` 200 → 125 行；`structure.md §1.6` 从「缺 5 包 + 计数错」更新为实测三进程地图。

## 6. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 整理装配代码 + 核实调用链 + 文档站按实况完善 + 登记策略面断点 | 用户「整理代码 / 确认调用 / 是否合理 / 完善文档」要求 |
