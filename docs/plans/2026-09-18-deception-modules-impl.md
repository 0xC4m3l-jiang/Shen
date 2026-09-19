# 变更包 · 2026-09-18 · 欺骗引擎模块实现（isolation / honeypot / decoy / responder / edge-injection）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 按模块设计实现 5 个欺骗引擎模块（含单测）+ 数据驱动接线（config）+ 端到端集成测试 |
| 日期 | 2026-09-18 |
| 状态 | 已实现 |
| 涉及模块 | `isolation` · `honeypot` · `decoy` · `responder`（核心，2b）· `edge-injection`（L1，2b）；`store` / `control` / `policy` / `cmd/core` 接线 |
| 决策数 | 已答 0 项（按既有设计实现）/ 待定 4 项（见 §7） |
| 关联 | [ADR-0010](../background/decisions/0010-functional-camouflage.md) · [ADR-0011](../background/decisions/0011-honeypot-entry-external-backends.md) · [ADR-0014](../background/decisions/0014-generative-deceptive-response.md) · [ADR-0016](../background/decisions/0016-decoy-polymorphism.md) · [`../log.md`](../log.md) 同日条目 |

## 1. 需求与验收

**要解决什么**：设计文档已齐，但 `isolation` / `honeypot` / `decoy` / `responder` / `edge-injection` **没有代码**。本轮的产出是「设计 → 代码」这一段。

**做完之后**：欺骗引擎的**全部核心模块可运行、可单测、可组合**；装配层一条命令即可把整条链拼起来。

**验收判据**：

1. 5 个模块各有实现与**独立单测**，测试只用替身（不依赖其他模块真实实例，`MD-22`）。
2. 模块间只经**接口**耦合：每个模块自己定义它依赖的接口（消费方定义），**没有任何模块 import 另一个模块的具体类型**。
3. 诱饵资产与幻境后端池**由 config 驱动**（`ST-24`），且 config 形状有校验。
4. 端到端集成测试跑通：`session → judge → director → honeypot → decoy → responder`。
5. 隔离短路在服务面生效，且存储不可用时 **fail-open**（`NI-10`）。
6. `make gate` 全绿。

**不做什么**：

- 不实现具体蜜罐（`honeypot-protocol` / `honeypot-shell` 仍是可选自研，`ADR-0011`）。
- 不接真实存储（仍用内存实现；接口不变）。
- 不写 L4/L2/L3/控制台代码（阶段 3）。

## 2. 设计逻辑

**接口解耦（本轮的核心纪律）**：

| 模块 | 它依赖的接口 | 由谁定义 | 生产实现 |
| --- | --- | --- | --- |
| `isolation` | `isolation.Store` | 本模块 | `store.IsolationStore` |
| `honeypot` | 无（纯数据 + 内存池） | —— | —— |
| `decoy` | `decoy.AssetStore` | 本模块 | `store.DecoyStore` |
| `responder` | `responder.ContentStore` | 本模块 | `store.ContentStore` |
| `control`（服务面） | `control.IsolationChecker` | 本模块 | `isolation.Engine` |

> 每个消费方定义自己需要的最小接口，因此**没有一个模块 import 另一个模块的具体类型**；
> 装配层（`cmd/core`）是唯一把具体实现拼起来的地方。这正是 `MD-4`（依赖单向）与
> 「接口由消费方定义」在代码上的落地。

**新增的存储实体**（`MD-20`：核心访问存储必经 `store`）：

| 接口 | 承载 | 内存实现 |
| --- | --- | --- |
| `store.DecoyStore` | 诱饵资产（数据） | `DecoyMemory` |
| `store.ContentStore` | 预生成欺骗内容（键 = 一致性键 `AR-30`） | `ContentMemory` |

**数据驱动**（`ST-24`）：`config` 新增 `decoys` / `honeypots` 两段（**可选**，不写即不启用），
由 `policy.Load` 解析校验，经 `Loader.Decoys()` / `Loader.Honeypots()` 供给模块。

**启动期自检**：`cmd/core` 在启动时验证 `AR-30`（同输入两次响应逐字节一致）——
一致性是「幻境不可区分」的前提，坏了就该启动失败。

## 3. 文档对应（追溯矩阵）

| 规则 / 依据 | 模块文档 | 代码 | 测试 | 验证命令 |
| --- | --- | --- | --- | --- |
| `NI-10`（fail-open）、`AR-9`（状态外置） | [`../modules/isolation.md`](../modules/isolation.md) §4/§6 | `core/internal/isolation/isolation.go` | `isolation_test.go` | `make gate` |
| `MD-26`、`MD-14`/`MD-15`/`MD-16`、`ST-24` | [`../modules/honeypot.md`](../modules/honeypot.md) §4 | `core/internal/honeypot/honeypot.go` | `honeypot_test.go` | `make gate` |
| `ADR-0010`（功能性伪装）、`ADR-0016`（多态）、`MD-25` | [`../modules/decoy.md`](../modules/decoy.md) §1/§4 | `core/internal/decoy/decoy.go` | `decoy_test.go` | `make gate` |
| `AR-30`（一致性不变量）、`NI-9`（无新 cookie）、`MD-23` | [`../modules/responder.md`](../modules/responder.md) §4 | `core/internal/responder/responder.go` | `responder_test.go` | `make gate` |
| `INT-8`（只改蜜罐侧）、`AR-7`/`MD-9`（适配器边界） | [`../modules/edge-injection.md`](../modules/edge-injection.md) §4 | `edge/injection/injection.go` | `injection_test.go` | `make gate` |
| 全链路 | —— | `core/cmd/core/{main.go,integration_test.go}` | `integration_test.go`（4 例） | `make gate` · `make dev` |

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `core/internal/contract/deception.go` | 新增 | 跨模块共享类型：`DecoyAsset` / `HoneypotBackend` / `RespondRequest` / `RespondOutput`（`MD-5`） |
| `core/internal/store/iface.go` | 改 | 新增 `DecoyStore` / `ContentStore` |
| `core/internal/store/memory.go` | 改 | 新增 `DecoyMemory` / `ContentMemory`，并进 `MemStores` |
| `core/internal/isolation/{iface,isolation,isolation_test}.go` | 新增 | 隔离记录 / TTL / 查询短路 |
| `core/internal/honeypot/{iface,honeypot,honeypot_test}.go` | 新增 | 幻境后端池：类型注册 / 开关 / 健康 / 解析 |
| `core/internal/decoy/{iface,decoy,decoy_test}.go` | 新增 | 诱饵面：匹配 / 多态 / 投放片段 |
| `core/internal/responder/{iface,responder,responder_test}.go` | 新增 | 欺骗响应生成：确定性模板 + 预生成优先 |
| `core/internal/responder/blacklist.go` | 新增 | **`AR-22` / `AR-23`**：内容黑名单（泄露 / 自曝 / 超长） |
| `edge/injection/{iface,injection,injection_test}.go` | 新增 | L1 注入引擎（非 HTML 原样返回，纯变换） |
| `edge/proxy/{iface,proxy}.go` | 改 | **`Injector` 接口**（本模块定义，避免适配器互依 MD-4）+ 引流侧 HTML 注入（`INT-8`、`ST-5`） |
| `edge/proxy/proxy_test.go` | 改 | 注入 4 例（引流注入 / 业务侧不改 / 非 HTML 不改 / 未配置不改） |
| `edge/proxy/cmd/proxy/main.go` | 改 | `SHEN_PROXY_INJECT` 解析 + 装配 `edge/injection` |
| `core/internal/control/service.go` | 改 | 服务面接隔离短路（`WithIsolation`） |
| `core/internal/director/director.go` | 改 | 实现 `MD-25`（诱饵面 observe-only）：诱饵前缀集在上游豁免 block |
| `core/internal/policy/policy.go` | 改 | 解析 `decoys` / `honeypots`（含校验）+ 暴露 |
| `deploy/config/config.example.yaml` | 改 | 两段示例（`decoys` / `honeypots`） |
| `core/cmd/core/main.go` | 改 | 装配 5 个模块 + 启动期 `AR-30` 自检 + 装配日志 |
| `core/cmd/core/integration_test.go` | 新增 | 端到端链路 + 隔离短路 + fail-open（4 例） |

**必须遵守的上位约束**：`AR-2` · `AR-7`/`AR-9` · `AR-30` · `MD-4`/`MD-5`/`MD-9`/`MD-20`/`MD-22`/`MD-23`/`MD-25`/`MD-26` · `NI-5`/`NI-9`/`NI-10` · `ST-3`/`ST-5`/`ST-24` · `TM-13`/`MD-24`。

## 5. 测试与场景

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | 隔离：命中 / 未命中 / TTL / 参数 / 存储故障 | 短路与上抛语义正确 | ✅ | `isolation_test.go`（11 例） |
| 2 | 后端池：解析需启用且健康 | 未启用/不健康不得解析 | ✅ | `honeypot_test.go`（14 例） |
| 3 | 诱饵：最长前缀匹配 / 多态确定性 / 投放片段 | 按设计 | ✅ | `decoy_test.go`（14 例） |
| 4 | 响应：同 `(会话,资源)` 逐字节一致 | `AR-30` | ✅ | `responder_test.go`（10 例） |
| 5 | 注入：非 HTML 原样返回 / 纯变换 | 绝不阻断、无副作用 | ✅ | `injection_test.go`（10 例） |
| 6 | 端到端：Agent → route_mirage → 后端解析 → 诱饵 → 响应 | 全链通 | ✅ | `integration_test.go::TestDeceptionChainEndToEnd` |
| 7 | 误调度：正常用户放行 | `NI-1` 首要约束 | ✅ | `TestLowScoreAgentStaysOnOrigin` |
| 8 | 隔离短路不调决策层 | 客户端不可见 | ✅ | `TestIsolationShortCircuitsWithoutCallingCore` |
| 9 | 隔离存储故障 fail-open | 回退调核心 | ✅ | `TestIsolationStoreFailureFailsOpen` |
| 10 | `MD-25`：诱饵面上禁止 block | 满分 + block 开启仍不 block；非诱饵路径仍可 block | ✅ | `director_test.go`（4 例）· `TestConfiguredDecoyPathIsNeverBlocked` |
| 11 | 适配器注入：只改引流侧 HTML；业务侧不改写 | `INT-8` / `ST-5` | ✅ | `edge/proxy` 4 例 |
| 12 | 内容黑名单：泄露 / 自曝 / 超长均被拒 | `AR-22` / `AR-23` | ✅ | `responder` 6 例（含「模板自身必须合规」） |

**没有覆盖的**：真实存储后端（Redis/CH/PG）· 具体蜜罐 · 多副本一致性（需真实存储）· `severity` 加重（档位未定）。

## 6. 验证证据

```console
$ make gate
门禁通过。  （fmt · vet · staticcheck · errcheck · archcheck · trace · licensecheck · go test -race）

$ go test ./...
ok  	shen/core/internal/{isolation,honeypot,decoy,responder} · shen/edge/injection · shen/core/cmd/core …
```

**关键指标**：新增单测 **60+ 例**（isolation 9 · honeypot 15 · decoy 13 · responder 16 · injection 10 · proxy 新增 4），
另加集成 5 例；全仓 `-race` 通过。

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | `store` 真实后端（Redis / ClickHouse / PostgreSQL） | 多副本一致性、持久化 | 下一步（接口已就位，换实现即可） |
| 2 | `responder` 的**服务面**未接 —— 生成能力已就绪，但尚无 gRPC/HTTP 暴露给边缘 | 端到端只能进程内验证 | 需要 `api/` 新契约或由适配器侧注入 |
| 3 | `decoy` 资产目前由 config 提供，尚无管理面（console CRUD） | 运营改资产要改配置 | `console`（阶段 2b） |
| 4 | `honeypot` 健康检查是静态的（配置即健康） | 死后端会先收到一次流量 | 健康探针（阶段 3） |
| 5 | `spec/decoy.md` / `spec/honeypot.md` 字段契约未写 | 对外契约不完整 | 实现期补 |
| 6 | 诱饵多态只到「确定性哈希」，识破信号触发未实现 | 对抗众包识破只有一半 | `chain` / `strategy`（阶段 3） |
| 7 | **`console`（阶段 2b，TypeScript）未实现** | 控制平面（策略编排 / 资产管理 / 审计）缺失 | 需 `npm install typescript` —— **项目规则禁止未授权安装**（`dev-loop-project` §2.3），故本轮不做；且它服务的是控制面、非欺骗引擎本体 |
| 8 | `netpolicy` / `intent` / `chain` / `strategy` / `llm-components`（阶段 3）未实现 | 分析层与网络层 | 设计明确为**阶段 3**（依赖阶段 2 完成）；`honeypot-protocol` / `honeypot-shell` 按 [ADR-0011](../background/decisions/0011-honeypot-entry-external-backends.md) **只做预留、接第三方** |

## 7.1 审视记录（L 档）

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 5 个模块文档写「`core/internal/xxx/`（待建）」但代码已存在 | 漂移（`TC-2`） | `make trace` 报 5 条 | 更新测试表 + 追加 §9 变更记录 | ✅ 归零 |
| 2 | `honeypot` 错误串首字母大写 | 静态检查（`ST1005`） | `make gate` | 改小写 | ✅ |
| 3 | `responder_test.go` 比较恒等的表达式 | 静态检查（`SA4000`） | `make gate` | 改用变量比较 | ✅ |
| 4 | `edge/injection/iface.go` 有无用 import | 编译 | `go build` | 删除 | ✅ |
| 5 | `decoy_test.go` 调用 `Match` 少接一个返回值 | 编译 | `go test` | 补 `_` | ✅ |
| 6 | 蜜罐类型成员资格由谁校验 | 依赖方向风险 | 设计推演 | 放在 `honeypot` 模块（避免 `policy` 反向依赖业务模块） | ✅ |
| 7 | **`MD-25`（诱饵面 observe-only）此前只有规则、没有实现** | 缺口（设计与实现不一致） | 逐条比对 design/ 规则与代码 | 在 `director` 实现：诱饵前缀集豁免 block，并由装配层从诱饵资产汇总（单一事实源） | ✅ 已实现 + 5 例测试 |

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 实现 5 个欺骗引擎模块 + 数据驱动接线 + 端到端集成测试 | 模块设计（ADR-0010/0011/0014/0016） |
