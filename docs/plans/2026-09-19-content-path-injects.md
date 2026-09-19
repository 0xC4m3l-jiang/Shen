# 变更包：2b 内容通路 —— 响应改写规则经策略面下发

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 把**响应改写规则**从「每个适配器改 env」变成**核心配置 → 策略面下发**（带版本与校验和），并让注入器支持远端热变更 |
| 日期 | 2026-09-19 |
| 状态 | 已验证（端到端实测：核心下发 → 适配器应用 → 改道侧注入） |
| 涉及模块 | `policy`（[`../design/modules.md`](../design/modules.md) §1.1 第 6 行）· `adapter-proxy`（第 11 行）· `edge-injection`（第 9 行，执行方）· `contract`（共享类型）· `store`（无改动） |
| 决策数 | 已答 9 项（用户 2026-09-19「继续 按照推荐」全部批准）/ 待定 0 项 |
| 关联 | [ADR-0018](../background/decisions/0018-policy-plane-pull-model.md)（本轮的边界与未解决项）· [`../spec/policy-payload.md`](../spec/policy-payload.md) · [`../spec/config.md`](../spec/config.md) §2.12 · 上轮 [`2026-09-19-policy-plane.md`](2026-09-19-policy-plane.md) |

---

## 1. 需求与验收

**要解决什么**：策略面（`S4`）上轮只下发**改道后端表与白名单**，而 `edge-injection.Rule` 的注释早就写着
「**数据**，随配置下发，不是编译进代码的分支（`ST-24`）」—— 现实却是它只能从每个适配器的 `SHEN_PROXY_INJECT` env 来。
运营要把一条钩子铺到 N 个适配器，就得登 N 台机器改 env 并逐个重启；且**没有版本、没有校验和、没有回执**。

**做完之后**：核心配置 `injects` 是唯一来源；适配器按间隔拉到规则并应用；改道侧响应被注入；
`SHEN_PROXY_INJECT` 降级为本地兜底；运营可以用**显式空数组**主动关掉注入。

**验收判据**：

1. 核心配置新增 `injects` 段并通过校验（空片段 / 未登记分类 → **启动失败**）。
2. 载荷新增可选 `inject_rules[]`；**字段缺省**与**显式空数组**语义可区分（前者不下发、后者下发空数组）。
3. 适配器：缺省 → 保留本地规则；空数组 → 关掉注入；非空 → 远端接管（本地规则不再注入）。
4. 注入只作用于**改道侧** HTML 响应；业务侧响应零改写（`INT-8`）。
5. 非法注入规则 → **整份策略不应用**（禁止半应用），并回执 `applied=false` + 原因。
6. **端到端实测**：核心下发 → 适配器应用 → 探针拿到「幻境正文 + 注入片段」，正常会话拿到纯净业务响应。
7. `make gate`（含 `make trace` / `leakcheck`）与 `make dev` 全绿。

**不做什么**（`Q1` / `Q7` / `Q8` 的决定）：

- **不下发诱饵资产与预生成响应正文** —— 假路径自答与「幻境正文由谁产出」属**架构级**取舍（`architecture.md` §10.2 缺口 13），本轮只把现状写清，不改架构；
- **不下发 `decoy` 的投放片段** —— 那是**运维物料**（可直接粘贴到 JS / nginx / SQL），不是运行时数据；
- **注入规则不随会话变化**（`AR-30` 的多态仍由核心在**内容层**负责）；
- **不 bump 载荷 schema 版本**（加可选字段，旧适配器忽略即可；bump 会让未升级的适配器拉不到策略）；
- **不细分回执**（同一份快照 = 同一版本 = 一次回执）。

---

## 2. 设计逻辑

**决策树**（①设计访谈一轮问完，9 项按推荐批准）：

```text
2b 内容通路
├── 第 1 轮（范围）：本轮送什么到边缘？
│   ├── Q1 → 只送**响应改写规则**（容器 Rule/Injector 已存在）；架构缺口 13 另议
│   └── Q7 → 假路径/诱饵内容/投放片段都**不送**
├── 第 2 轮（契约）：规则的来源与形态
│   ├── Q2 → 核心配置新增 `injects` 段（数据驱动，ST-24）
│   ├── Q3 → 载荷加可选 `inject_rules[]`，schema_version 保持 1
│   └── Q4 → 复用 `Rule` 三字段：kind / snippet / marker
└── 第 3 轮（运行语义）
    ├── Q5 → 字段缺省 = 本地规则；显式空数组 = 明确无规则
    ├── Q6 → 注入规则是静态数据（不随会话变化）
    ├── Q8 → 架构缺口 13 只更新现状，不改架构
    └── Q9 → 随同一版本、一次回执
```

**已确认的决策**（来源：用户「继续 按照推荐」）：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 范围 | 只下发响应改写规则 | 容器已在（`edge/injection.Rule`），改动可验证、可回退 | `docs/spec/policy-payload.md` |
| ② | 来源 | 核心配置 `injects` | `decoy.Placements` 是运维物料（语义不同）；配置是 `ST-24` 的正式通路 | [`../spec/config.md`](../spec/config.md) §2.12 |
| ③ | 载荷版本 | 加可选字段，`schema_version` 保持 1 | 适配器已忽略未知字段；bump 会让未升级的适配器拉不到策略 | `policy/server.go` |
| ④ | 字段 | 复用 `kind` / `snippet` / `marker` | 容器与取值注释都已存在；`kind` 是分类，边缘不该猜 | `contract.InjectRule` |
| ⑤ | 缺省 vs 空 | **缺省 = 本地兜底；显式空数组 = 关掉注入** | 否则「没配」与「配了空」无法区分 ⇒ 运营没有关闭手段 | `Loader.Injects` 的三返回值 |
| ⑥ | 会话维度 | 注入规则不随会话变化 | 边缘引入会话键会碰 `INT-20`；多态在核心内容层 | `applyEdgePolicy` |
| ⑦ | 假路径/内容 | 本轮不下发 | 属架构缺口 13 / 6 的范围 | `architecture.md` §10.2 |
| ⑧ | 架构缺口 13 | 只更新现状描述 | 决定 L1↔核心之间传多少数据，属架构级取舍，不该塞进本轮 | `architecture.md` §10.2 缺口 13 |
| ⑨ | 回执粒度 | 随同一版本、一次回执 | 细分会把版本对账变成按段对账，对运营无增量价值 | `policy.RunPolicyPolling`（未改） |

**接缝与接口**：

| 接缝 | 变化 | 说明 |
| --- | --- | --- |
| `S4` 载荷（`docs/spec/policy-payload.md`） | **新增可选字段** `inject_rules[]` | 两端手工对齐（`ST-3` 禁止适配器 import `core/internal/`） |
| 配置契约（`docs/spec/config.md` §2.12） | **新增段** `injects` | 数据驱动（`ST-24`），未写即不下发 |
| `contract` | 新增 `InjectRule` + `InjectKinds` | 进程内共享类型（`MD-5` 修正后的落点） |
| `edge-injection` | 无接口变化 | 它的 `Rule` 未动（字段一一对应）；变的只是「谁把规则送进来」 |
| 注入器读取点 | `injectingTransport` 改为**持 Handler、按请求取当前注入器** | 否则策略面热变更后，建后端时钉住的注入器会继续用旧规则 |

**数据流（含失败路径）**：

```text
config.yaml ──injects──► policy.Loader ──┐
                                          │ Pull：投影（不排序，顺序即执行顺序）
                                          ▼
                    payload.inject_rules（缺省则**不出现**该字段）
                                          │
                 适配器 applyEdgePolicy ──┤
                                          ├─ 字段缺省 → 保留本地 env 规则
                                          ├─ 显式空数组 → 关掉注入（currentInjector()=nil）
                                          └─ 非空 → injection.New(远端规则) 接管
                                          │
                      injectingTransport（**按请求**读当前注入器）
                                          │
                               改道侧 HTML 响应 ← 注入（业务侧**零改写**）

失败路径：
  规则非法（片段为空）→ **整份策略不应用** + Ack(applied=false, reason) → 继续用旧规则
  拉取失败            → 沿用当前策略（NI-1）
  找不到 marker       → 跳过该条规则（不阻断响应）
```

---

## 3. 追溯矩阵

| 规则 ID | 模块文档 | 代码 | 测试 | 验证命令 |
| --- | --- | --- | --- | --- |
| `ST-24`（策略是数据） | [`../spec/config.md`](../spec/config.md) §2.12 · [`../modules/policy.md`](../modules/policy.md) §1 | `policy.go` 的 `injectsDoc` / `validateInjects` / `buildInjects` | `TestInjectsSectionValidation`（5 例） | `make gate` |
| `ST-8`（版本 + 校验和） | [`../spec/policy-payload.md`](../spec/policy-payload.md) §1 | `policy/server.go` 的 `edgePayload` | `TestPullCarriesInjectRules` · `TestPullIsDeterministic` | `make gate` |
| `AR-13`（回执对账） | [`../modules/policy.md`](../modules/policy.md) §1 | `Server.Ack` · `store.RecordAck` | `TestAckRecordsAndIsIdempotent` · `TestPolicyAppliedAndAcked` | `make gate` |
| `INT-8`（只改改道侧） | [`../modules/edge-injection.md`](../modules/edge-injection.md) §1 · [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) §6 | `injectingTransport` 只挂在改道后端上 | `TestRemoteInjectRuleRewritesDivertedResponse` · 端到端实测（业务侧零注入） | `make gate` + 实跑 |
| `NI-1`（不影响业务） | [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) §6 | `runPolicyPolling` 失败分支 | `TestPullFailureKeepsCurrentPolicy` | `make gate` |
| `NI-5`（未识别回落） | 同上 | `mirageHandler` 兜底 | `TestRemoteBackendIsUsedByRequestPath` | `make gate` |
| `AR-30`（同会话同资源同答案） | [`../modules/responder.md`](../modules/responder.md) §1 | 注入规则为**静态数据**（不引入会话维度） | `TestApplyEdgePolicyInjectSemantics`（三态） | `make gate` |
| `MD-5`（共享类型收敛） | [`../design/modules.md`](../design/modules.md) §1.7 | `contract.InjectRule`（进程内共享类型 → `contract/`） | `make archcheck` | `make archcheck` |
| `ST-3`（适配器禁 import 核心内部） | [`../design/structure.md`](../design/structure.md) §1.7 | `edge/proxy/policy.go` 自持载荷结构 | `make archcheck` | `make archcheck` |
| `MD-22`（测试用替身） | [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) §7 | `stubPolicy` · `stubInjector` · `roundTripFunc` | 策略类全部用例 | `make gate` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `core/internal/contract/deception.go` | 改 | 新增 `InjectRule` + `InjectKinds`（进程内共享类型，按修正后的 `MD-5` 落 `contract/`） |
| `core/internal/policy/policy.go` | 改 | `injects` 段：`injectDoc` · `validateInjects`（空片段/未登记分类即报错）· `buildInjects`（**不排序**）· `Loader.Injects` 返回 `(rules, provided, err)` —— `provided` 就是「缺省 vs 空数组」的区分 |
| `core/internal/policy/server.go` | 改 | `edgeDoc.InjectRules *[]edgeInjectRule`：`nil` → `omitempty` 省略字段；非 `nil`（含空数组）→ 显式下发 |
| `core/internal/policy/server_test.go` | 改 | 新增 5 例：校验 5 子例 · provided 标志 · 载荷携带且保序 · 未配置则省略 · 空数组原样下发 |
| `edge/proxy/policy.go` | 改 | 载荷 `InjectRules *[]policyInjectRule`；`remoteState` 加 `injectRulesProvided` / `injector`；`applyEdgePolicy` 构造远端注入器（**非法规则 → 整份不应用**）；新增 `currentInjector()` |
| `edge/proxy/handler.go` | 改 | `injectingTransport` 改持 `handler` 并**按请求**取注入器（支持远端热变更）；挂载条件改为「本地有规则**或**策略面可能下发」 |
| `edge/proxy/policy_test.go` | 改（含一处修复） | 新增夹具 `policyFixtureWithInjects`（缺省/空数组/非空三态）与 2 个测试；把 4 处旧 `injectingTransport{injector: ...}` 用例改到新形态（`injectingTransportFor`） |
| `deploy/config/config.example.yaml` | 改 | 新增 `injects: []` 示例（含注释说明缺省 vs 空数组、kind 登记值、顺序即执行顺序） |

**关键类型与函数**（导出 = 契约）：`contract.InjectRule` · `contract.InjectKinds` · `policy.Loader.Injects`（三返回值）；
内部：`injectDoc` / `edgeInjectRule` / `policyInjectRule` / `remoteState.injectRulesProvided` / `Handler.currentInjector`。

**必须遵守的上位约束**：`ST-3` · `ST-8` · `ST-24` · `AR-13` · `AR-30` · `MD-5` · `MD-20` · `MD-22` · `NI-1` · `NI-5` · `INT-8`。

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | `injects` 校验（5 子例） | 空片段 / 只有空白 / 未登记分类 → 报错；带分类与标记 / 省略两者 → 通过 | ✅ | `TestInjectsSectionValidation` |
| 2 | 「未配置」与「空数组」可区分 | `provided=false` vs `provided=true` 且零规则 | ✅ | `TestInjectsProvidedFlag` |
| 3 | 载荷携带规则且**保序** | 两条按配置顺序、字段原样 | ✅ | `TestPullCarriesInjectRules` |
| 4 | 未配置 → **省略**字段 | payload 里没有 `inject_rules` | ✅ | `TestPullOmitsInjectRulesWhenUnconfigured` |
| 5 | 空数组 → **显式下发** | payload 含 `"inject_rules":[]` | ✅ | `TestPullEmitsExplicitEmptyInjectRules` |
| 6 | 适配器三态合并 | 缺省 → 保留本地；空数组 → 关掉；非空 → 远端接管 | ✅ | `TestApplyEdgePolicyInjectSemantics` |
| 7 | 远端规则**真的改写改道侧响应**，且远端接管后本地规则不再注入 | 三态在响应体上可区分 | ✅ | `TestRemoteInjectRuleRewritesDivertedResponse` |
| 8 | 业务侧响应零改写 | 非改道请求不带任何注入片段 | ✅ | 端到端实测 ② |
| 9 | **端到端**（真进程 · 真 gRPC） | 核心下发 → 适配器应用 → 探针拿到「幻境正文 + 注入片段」 | ✅ | 见 §6 |
| 10 | 策略面不可达 / 校验和不匹配 / schema 读不懂 | 沿用当前策略并回执 `applied=false` | ✅ | 上轮既有用例（本轮回归） |

**没有覆盖的**：

- **注入规则的多态**（本轮明确不做，见 `Q6`）；
- **`marker` 指向不存在位置**时该条规则被跳过的**端到端**验证（单测层由 `edge-injection` 既有用例覆盖）；
- **多适配器同时应用不同版本**的注入对账（只有单适配器端到端）；
- **诱饵资产 / 预生成正文的下发**（本轮范围外）。

---

## 6. 验证证据

```console
$ make gate
门禁通过。          # fmt · vet · staticcheck · errcheck · archcheck · trace · leakcheck · license · test -race

$ go test ./core/internal/policy/ ./edge/proxy/ -count=1
ok  shen/core/internal/policy   0.258s     # 22 例（新增 5 例：injects 校验与载荷投影）
ok  shen/edge/proxy             0.899s     # 39 例（新增 2 例：注入三态与真改写）
```

**端到端实测（真进程 · 真 gRPC · 真注入）**：

```console
# 核心：shadow=false · gray_pct=100 · 规则(UA 含 HeadlessChrome → 0.9)
#       honeypots[0]={name:mirage, addr:http://127.0.0.1:19201}
#       injects: [{kind: developer_api, snippet: "<!--INJECTED-BY-POLICY-->"}]
$ ./shen-core                     → 幻境后端池：1 个登记 / 1 个可用 → 核心已启动（接管模式）

# 适配器：**没有**设 SHEN_PROXY_INJECT（本地零规则），注入只能来自策略面
$ SHEN_PROXY_UPSTREAM=http://127.0.0.1:19200 SHEN_PROXY_SHADOW=false \
  SHEN_PROXY_POLICY_INTERVAL=2s SHEN_PROXY_ADAPTER_ID=proxy@inject ./shen-proxy
{"msg":"策略面：每 2s 拉取一次（适配器标识 proxy@inject）"}
{"msg":"proxy: 已应用策略 e2e-inject v1（1 个改道后端）"}

$ curl -s -A "HeadlessChrome/120" -H "Cookie: sid=probe-1" http://127.0.0.1:18100/
<html><body>MIRAGE-PAGE<!--INJECTED-BY-POLICY--></body></html>        ← 改道 + 策略面注入 ✅

$ curl -s -A "Mozilla/5.0" -H "Cookie: sid=normal-1" http://127.0.0.1:18100/?v=1
<html><body>REAL-BUSINESS</body></html>                                ← 业务侧零注入 ✅（INT-8）
```

**关键指标**：测试函数全仓 **189 → 196**（`policy` 17→22 · `edge/proxy` 37→39）· `edge/proxy` 39 例 · 新增依赖 **0** · 新增模块 **0** · 载荷 schema 版本 **未变**（1）。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | **诱饵资产与预生成响应正文仍不下发** | 假路径自答、幻境正文由引擎自己产出这两件事做不了 | [`../design/architecture.md`](../design/architecture.md) §10.2 缺口 13 / 6 |
| 2 | **`Watch` 未实现** | 注入规则生效延迟上限 = 一个轮询间隔（默认 60s） | ADR-0018 失效条件 1 |
| 3 | 注入规则的**多态**（按会话轮换钩子）未定 | 需要时会碰 `INT-20`（会话身份不上行/不回传） | 需单独一轮设计 |
| 4 | `policy_ack` 无保留期；明文 gRPC 仍是同主机假设 | 接真库 / 跨节点部署 | `NI-13` · mTLS |

---

## 7.1 审视记录（L 档，做法见全局技能 `audit`）

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `docs/modules/edge-injection.md` 头部写「状态：设计（阶段 2b）」，但 `edge/injection/` **早已实现并含单测**（10 例） | **过期状态** | `ls edge/injection/` · `grep -c '^func Test'` | 修正为「✅ 已实现」，并写明规则来源 | ✅ |
| 2 | 同上 §2 写「输入：诱饵定义（来自 `decoy`）」，而实现消费的是**注入规则**（`edge/injection.Rule`） | 漂移（文档 ↔ 代码） | `edge/injection/iface.go` 的 `Rule` / `Injector` | 改为「注入规则（核心配置 → 策略面）」，保留 `decoy` 作为假路径内容来源 | ✅ |
| 3 | `edge/proxy/README.md` 的目录表缺 `policy.go` / `policy_test.go`（上轮新增） | 漏写 | `ls edge/proxy/*.go` | 补两行 | ✅ |
| 4 | `edge/proxy/config/front-proxy.example.env` 把 `SHEN_PROXY_INJECT` 写成主入口 | 叙述误导 | 本轮起正式通路是核心 `injects` | 改写为「本地兜底」，并注明「策略面显式下发后以它为准（空数组即关掉）」 | ✅ |
| 5 | 上轮写的 `42 测试`（我自己在文档里的预估）与实际 **39** 不符 | 计数错 | `cat edge/proxy/*_test.go \| grep -c '^func Test'` → 39 | 4 处一并改准（39 / 22 / 196） | ✅ |
| 6 | `store.PolicyStore` 无新增实体（回执复用上轮接口） | 无影响 | `make archcheck` | 保留 | ✅ |
| 7 | 本轮测试一度 import 了核心包？ | 规则核查 | `grep -n "core/internal" edge/proxy/*.go` → 无（`policy_test.go` 只 import `api/policy/v1` 与本包） | 保留 | ✅ |

> 历史记录类文件（[`../log.md`](../log.md) · `docs/plans/` · `docs/background/` · `docs/kb/`）不在审视范围。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 2b 内容通路：核心配置新增 `injects` 段（校验 + 装载）· 载荷新增可选 `inject_rules[]`（缺省 = 本地兜底 / 空数组 = 关掉注入）· 适配器三态合并 + 注入器按请求读取（支持热变更）· `contract.InjectRule` · 端到端实测「核心下发 → 改道侧注入」；测试 189 → 196 | 用户「继续 按照推荐」（9 项）· `ST-24` / `ST-8` / `AR-13` / `INT-8` · [ADR-0018](../background/decisions/0018-policy-plane-pull-model.md) |
