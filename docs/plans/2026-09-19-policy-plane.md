# 变更包：策略面（S4）落地 —— 核心下发 → 适配器应用 → 真实改道

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 实现 `api/policy/v1` 的两端：核心侧 `Pull` / `Ack` 服务端 + 适配器侧拉取、应用与回执；闭合阶段 2b 的结构性断点 |
| 日期 | 2026-09-19 |
| 状态 | 已验证 |
| 涉及模块 | `policy`（[`../design/modules.md`](../design/modules.md) §1.1 第 6 行，阶段 2a）· `adapter-proxy`（第 11 行，阶段 2a）· `store`（第 8 行：`PolicyStore` 扩回执）· `control`/`cmd/core`（装配）· `cmd/proxy`（装配） |
| 决策数 | 已答 9 项（用户 2026-09-19 全部按推荐批准）/ 待定 0 项 |
| 关联 | [ADR-0018](../background/decisions/0018-policy-plane-pull-model.md) · [`../spec/policy-payload.md`](../spec/policy-payload.md) · [`2026-09-19-caddy-l1-base.md`](2026-09-19-caddy-l1-base.md)（上一轮）· [`../log.md`](../log.md) |

---

## 1. 需求与验收

**要解决什么**：`api/policy/v1` 的契约从阶段 1 就存在，但**两端都没实现** —— 核心算出来的幻境后端池与白名单**传不到边缘**，
`decoy` / `honeypot` / `responder` 因此「装配了却不在链路上」。这是 `docs/design/structure.md` §1.6.4 记录的**唯一结构性断点**。

**做完之后**：核心把当前策略投影成边缘文档下发（带版本 + 校验和），适配器拉取、验完整性、整块应用、回执；
改道后端表与白名单可以**不重启适配器**生效，并能回答「哪个适配器应用了哪个版本」。

**验收判据**：

1. 核心侧实现 `Pull`（投影 + 校验和）与 `Ack`（落账）；`Watch` **显式返回 `Unimplemented`**（不是挂住调用方）。
2. 适配器按间隔拉取：**远端覆盖本地**（后端表按名）· **白名单并集**（护栏只增不减）· 校验和不匹配 / 读不懂的 schema / 非法 JSON → **拒绝应用并回执 `applied=false` + 原因**。
3. 拉取失败**只影响策略**：沿用当前策略、不产生回执风暴、请求路径一切照旧（`NI-1`）。
4. 回执可对账：`(policy_id, version, adapter_id)` 幂等；缺 `adapter_id` 拒收。
5. **端到端实测**：核心下发 → 适配器应用 → 真实改道（探针 UA 拿到幻境响应、正常会话拿到业务响应）。
6. `make gate`（含 `make trace` / `make leakcheck`）与 `make dev` 全绿。

**不做什么**：

- **不做 `Watch`**（[ADR-0018](../background/decisions/0018-policy-plane-pull-model.md) 决定 1）；
- **不下发**规则 / 阈值 / 诱饵资产 / 注入片段（只下发适配器真正消费的两段，见 ADR-0018 理由 3）；
- **不做适配器侧灰度**（灰度仍在核心内逐请求收敛，`INT-12`）；
- **不做回滚触发 UI**（控制台未实现；回滚沿用「发布新版本」语义）；
- **不接真实存储**（`store` 仍是内存实现）。

---

## 2. 设计逻辑

**决策树**（①设计访谈一轮问完，9 项全部按推荐批准）：

```text
策略面 S4
├── 第 1 轮（范围与 RPC）：做到哪一步？Pull+Watch+Ack 全做吗？
│   ├── Q1 → 核心服务端 + edge/proxy 客户端最小闭环（mirror 下轮复用）
│   └── Q2 → 只做 Pull + Ack；Watch 显式 Unimplemented
├── 第 2 轮（契约与消费面）：payload 放什么、下发什么、与本地谁优先？
│   ├── Q3 → JSON + schema_version；checksum 覆盖 payload 字节
│   ├── Q4 → 只下发改道后端表 + 白名单 CIDR
│   └── Q5 → 远端覆盖 + 本地兜底；白名单取并集
└── 第 3 轮（运行与归属）：多久拉、灰度谁消费、回执落哪、服务端放哪？
    ├── Q6 → 启动 + 60s（可配）；失败沿用当前策略
    ├── Q7 → 适配器不消费 gray_pct
    ├── Q8 → 回执经 store.PolicyStore（不够再按 MD-20 扩实体 → 实际扩了接口）
    └── Q9 → 服务端落 policy 模块（不新增模块）；回滚 = 发布新版本
```

**已确认的决策**（来源：用户 2026-09-19「按照推荐」）：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 范围 | 核心服务端 + `edge/proxy` 客户端 | 先让一条真实链路带证据跑通；mirror 下轮复用同一客户端 | `core/internal/policy/server.go` · `edge/proxy/policy.go` |
| ② | RPC | `Pull` + `Ack`；`Watch` 返回 `Unimplemented` | `AR-13` 要回执；`Watch` 要处理长连接与重连，等真需求再加 | `server.go` 的 `Watch` |
| ③ | 载荷 | JSON + `schema_version`；`checksum = sha256(payload)` | 人可读、可回滚；适配器能验完整性 | [`../spec/policy-payload.md`](../spec/policy-payload.md) |
| ④ | 下发面 | 改道后端表 + 白名单 CIDR | 只下发边缘真正消费的（发不用的 = 多一份会过期的复制） | `server.go` 的 `edgePayload` |
| ⑤ | 合并 | 远端覆盖本地；白名单并集 | 策略面是 `ST-24` 正式通路；白名单是护栏，只增不减（`INT-25`） | `edge/proxy/policy.go` 的 `applyEdgePolicy` |
| ⑥ | 节奏 | 启动 + 60s（`SHEN_PROXY_POLICY_INTERVAL`）；失败沿用 | 改配置无需重启适配器；失败不得进请求路径（`NI-1`） | `runPolicyPolling` |
| ⑦ | 灰度 | 适配器不消费 `gray_pct` | 避免与核心内逐请求灰度（`INT-12`）两处定义打架 | ADR-0018 §决定 9 |
| ⑧ | 回执 | 经 `store.PolicyStore`（扩 `RecordAck` / `Acks`）；幂等；缺标识拒收 | `MD-20`：核心唯一 I/O 出口；适配器重启会重报 | `store/iface.go` · `store/memory.go` |
| ⑨ | 服务端归属 | 落在 `policy` 模块；`cmd/core` 注册；回滚 = 发布新版本 | 快照与校验和本来就在 `policy`；不新增模块（`MD-18` 不触发） | `core/cmd/core/main.go` |

**仍未定**：无（ADR-0018 登记了 5 项「未解决」，均不阻塞本轮；其中「注入片段/诱饵资产的边缘通路」是下一轮的设计议题）。

**接缝与接口**：

| 接缝 | 定义 | 实现 | 消费 |
| --- | --- | --- | --- |
| `api/policy/v1`（S4） | `Pull` / `Watch` / `Ack`（契约早已存在，本轮扩 `PolicyAck.adapter_id` / `reason`） | 核心：`policy.Server` | 适配器：`edge/proxy` 的 `PolicyClient`（由**消费方**定义的最小接口） |
| `store.PolicyStore` | `Current` / `Publish` / **`RecordAck` / `Acks`** | `store.PolicyMemory` | `policy.Server` |
| 载荷 | 边缘策略文档（JSON，schema v1） | 核心 `policy.edgeDoc` ⇄ 适配器 `edge.proxyPolicy`（**两处手工对齐**，因 `ST-3` 禁止适配器 import `core/internal/`） | 两端 |

**数据流（含失败路径）**：

```text
配置(YAML) ──装载──► policy.Loader ──► Snapshot{policy_id, version, checksum, gray_pct}
                              │
                  Pull ──────►│ 投影：backends + whitelist.source_cidrs（按名排序 ⇒ 字节确定）
                              ▼
                    payload(JSON) + checksum=sha256(payload)
                              │
                 适配器轮询（默认 60s）
                              ▼
        ┌──────────── 校验 ────────────┐
        │ sha256(payload)==checksum ?   │ 否 → 拒绝 + Ack(applied=false, reason)
        │ schema_version==1 ?           │ 否 → 拒绝 + Ack(applied=false, reason)
        └───────────────┬──────────────┘
                        ▼ 是
        整块原子替换 remoteState（backends 远端优先本地兜底 · cidrs 并集）
                        ▼
                Ack(applied=true, adapter_id) ──► store.PolicyStore（幂等）

失败路径（全部不影响请求）：
  核心不可达 / 超时 → 沿用当前策略 + 限速日志（同一失败只报一次）
  个别后端地址非法   → 只丢那一条并记账
  版本与校验和未变   → 不重复应用、不重复回执
```

---

## 3. 追溯矩阵

| 规则 ID | 模块文档 | 代码 | 测试 | 验证命令 |
| --- | --- | --- | --- | --- |
| `ST-8`（版本号 + 校验和 + 可回滚） | [`../modules/policy.md`](../modules/policy.md) §1/§2 | `policy/server.go` 的 `Pull`（`sha256(payload)`）· `store.Publish` 版本单调 | `server_test.go::TestPullProjectsEdgePayload` / `TestPullIsDeterministic` | `make gate` |
| `AR-13`（版本化 + 灰度 + 回滚 + 回执） | [`../modules/policy.md`](../modules/policy.md) §1 · [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) §5 | `policy/server.go` 的 `Ack` · `store.RecordAck` · `edge/proxy/policy.go` 的 `ack` | `TestAckRecordsAndIsIdempotent` · `TestPolicyAppliedAndAcked` | `make gate` |
| `ST-24`（策略是数据） | [`../spec/config.md`](../spec/config.md) §2.8/§2.9 · [`../spec/policy-payload.md`](../spec/policy-payload.md) | `edgePayload`（从配置投影） | `TestPullProjectsEdgePayload` | `make gate` |
| `MD-20`（核心唯一 I/O 出口是 `store`） | [`../design/structure.md`](../design/structure.md) §3 | `store.PolicyMemory` 的 `RecordAck` / `Acks` | `store_test.go` | `make archcheck` |
| `MD-18`（不新增模块） | [`../design/modules.md`](../design/modules.md) §1.1 | 服务端落在既有 `policy` 模块 | `make archcheck` 模块清单一致性 | `make archcheck` |
| `ST-3`（适配器禁止 import 核心内部） | [`../design/structure.md`](../design/structure.md) §1.7 | `edge/proxy/policy.go` 自持载荷结构，**不** import `core/internal/contract` | `make archcheck`（编译期 + 检查器） | `make archcheck` |
| `NI-1`（不影响原始业务） | [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) §6 | `runPolicyPolling` 的失败分支（沿用当前策略） | `TestPullFailureKeepsCurrentPolicy` | `make gate` |
| `INT-25`（白名单先于改道判定） | [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) §1/§6 | `Handler.ServeHTTP` ① + `remoteWhitelisted` · `applyEdgePolicy` 的并集语义 | `TestRemoteWhitelistIsAdditive` · `TestWhitelistSkipsCore` | `make gate` |
| `NI-5`（未识别状态回落放行） | [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) §6 | `dispatch` 兜底 | `TestRemoteBackendIsUsedByRequestPath`（应用前回落 origin） | `make gate` |
| `MD-22`（测试用替身） | [`../modules/policy.md`](../modules/policy.md) §7 | `stubPolicy` / `fakePolicyStore` | 全部策略类测试 | `make gate` |
| `TB-26`（跨语言接缝带版本号） | [`../spec/policy-payload.md`](../spec/policy-payload.md) §1/§2 | `schema_version` + proto 包路径 `policy.v1` | `TestApplyEdgePolicyRejectsBadInput`（读不懂即拒） | `make gate` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `api/policy/v1/policy.proto` | 改 | `PolicyAck` 加 `adapter_id` + `reason` —— 没有标识就只知道「有人应用了」，不知道「谁应用了」，`AR-13` 的对账无法成立 |
| `api/policy/v1/policy.pb.go` · `policy_grpc.pb.go` | 重新生成 | `make generate`（`ST-6`：契约的唯一事实源是 `.proto`） |
| `core/internal/contract/policy.go` | 改 | 新增内部类型 `PolicyAck` |
| `core/internal/store/iface.go` | 改 | `PolicyStore` 扩 `RecordAck` / `Acks`（回执也是 I/O，必经 `store`，`MD-20`） |
| `core/internal/store/memory.go` | 改 | 内存实现：按 `(policy_id, version, adapter_id)` 幂等存储 |
| `core/internal/policy/server.go` | 新增 | 策略面服务端：`Pull`（投影 + 校验和）· `Watch`（`Unimplemented`）· `Ack`（落账） |
| `core/internal/policy/server_test.go` | 新增 | 投影 / 确定性 / 未知 policy_id / Watch 未实现 / 回执幂等（5 例） |
| `core/internal/policy/policy_test.go` | 改 | 测试替身 `fakePolicyStore` 跟上接口 |
| `core/cmd/core/main.go` | 改 | 注册策略面服务端；**加明文监听闸门** `assertPlaintextListenIsLocal`（非回环地址 + 明文 gRPC → 启动失败） |
| `edge/proxy/policy.go` | 新增 | 策略客户端：拉取 → 验校验和/schema → 合并应用（远端覆盖 + 白名单并集）→ 回执；失败沿用当前策略 |
| `edge/proxy/policy_test.go` | 新增 | 应用语义与失败路径（7 例，含「远端后端真被请求路径用上」） |
| `edge/proxy/handler.go` | 改 | 新增配置字段（`PolicyID` / `AdapterID` / `PolicyInterval`）与运行态（`remote` 原子快照、`policy` 客户端、`buildRemote`）；`Provision` 启动轮询；`dispatch` / `forward` / `forwardMirage` 改走 `mirageHandler`（远端优先、本地兜底）；白名单检查加远端 |
| `edge/proxy/cmd/proxy/main.go` | 改 | 新增 `SHEN_PROXY_POLICY_INTERVAL` / `SHEN_PROXY_POLICY_ID` / `SHEN_PROXY_ADAPTER_ID`；启动日志说明策略面状态 |
| `docs/spec/policy-payload.md` | 新增 | 边缘策略载荷契约（字段表 / 合并语义 / 不变量 / 两处实现必须同时改） |
| `docs/background/decisions/0018-policy-plane-pull-model.md` | 新增 | 决策记录（候选 · 理由 · 后果 · **失效条件 4 条** · 未解决 5 条） |

**关键类型与函数**（导出 = 契约）：

- 核心：`policy.Server`（`NewServer` / `Pull` / `Watch` / `Ack`）· `policy.EdgePayloadSchemaVersion`；
- 适配器：`proxy.PolicyClient`（**消费方定义**的最小接口）· `Handler` 的三个新 JSON 字段；
- 边界：`applyEdgePolicy`（合并语义的唯一实现）· `runPolicyPolling`（失败语义的唯一实现）。

**必须遵守的上位约束**：`ST-3`（不 import `core/internal/`）· `ST-6`（`.proto` 是唯一事实源）· `ST-8` · `ST-24` · `AR-13` · `MD-20` · `MD-22` · `NI-1` · `NI-5` · `INT-25` · `TB-26`。

---

## 5. 测试与场景

**单元与集成（`make gate` 覆盖）**：

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | `Pull` 投影 | schema=1 · policy_id/version 一致 · 后端按名排序且与配置池一致 · 白名单 CIDR 带出 · `checksum == sha256(payload)` | ✅ | `TestPullProjectsEdgePayload` |
| 2 | 同内容两次 `Pull` | 逐字节一致、校验和一致 | ✅ | `TestPullIsDeterministic` |
| 3 | 请求别的 `policy_id` | `NotFound`（禁止回一份「别的策略」） | ✅ | `TestPullRejectsUnknownPolicyID` |
| 4 | `Watch` | `Unimplemented`（不是空流挂住） | ✅ | `TestWatchIsUnimplemented` |
| 5 | 回执落账 + 幂等 + 缺标识 | 2 条（每适配器一条）· 重报覆盖 · 缺 `adapter_id` → `InvalidArgument` · `applied=false` 必带原因 | ✅ | `TestAckRecordsAndIsIdempotent` |
| 6 | 远端后端按名覆盖 + 本地保留 | `hp-a` 可用 · `enabled=false` 不入表 · `local-only` 保留 | ✅ | `TestApplyEdgePolicyOverlaysBackends` |
| 7 | 非法 JSON / 读不懂的 schema | 整份拒绝且**不改变已生效策略** | ✅ | `TestApplyEdgePolicyRejectsBadInput` |
| 8 | 坏后端地址 | 只丢那一条，其余生效 | ✅ | 同上 |
| 9 | 白名单并集 | 远端可增 · 本地不丢 · 名单外不命中 | ✅ | `TestRemoteWhitelistIsAdditive` |
| 10 | 请求路径使用远端后端 | 应用前回落 origin（`NI-5`）；应用后改道到远端后端 | ✅ | `TestRemoteBackendIsUsedByRequestPath` |
| 11 | 策略面不可达 | 沿用当前策略 · 本地后端仍可用 · **不产生回执** | ✅ | `TestPullFailureKeepsCurrentPolicy` |
| 12 | 应用成功 | 回执 `applied=true` + 适配器标识；同版本不重复回执 | ✅ | `TestPolicyAppliedAndAcked` |
| 13 | 校验和不匹配 | 拒绝应用 + 回执 `applied=false` + 原因（且**只回执一次**） | ✅ | `TestPolicyChecksumMismatchIsRejected` |

**端到端实跑（真进程 · 真 gRPC · 真改道）**：

```console
# 核心：shadow=false · gray_pct=100 · 规则(UA 含 HeadlessChrome → 0.9) · honeypots[0]={name:mirage, addr:http://127.0.0.1:19101}
$ SHEN_CONFIG=/tmp/e2e-core.yaml SHEN_LISTEN=127.0.0.1:19445 ./shen-core
2026/09/19 15:26:46 幻境后端池：1 个登记 / 1 个可用（不实现具体蜜罐，ADR-0011）
2026/09/19 15:26:46 核心已启动（接管模式（产出真实三值决策））：127.0.0.1:19445

# 适配器：改道后端表**留空**，完全靠策略面拿
$ SHEN_PROXY_UPSTREAM=http://127.0.0.1:19100 SHEN_CORE_ADDR=127.0.0.1:19445 \
  SHEN_PROXY_SHADOW=false SHEN_PROXY_POLICY_INTERVAL=2s SHEN_PROXY_ADAPTER_ID=proxy@e2e ./shen-proxy
{"msg":"策略面：每 2s 拉取一次（适配器标识 proxy@e2e）"}
{"msg":"proxy: 已应用策略 e2e v1（1 个改道后端）"}        ← 策略面确实下发到了适配器

$ curl -A "HeadlessChrome/120" http://127.0.0.1:18081/          → MIRAGE-BODY     ✅ 改道成功
$ curl -A "Mozilla/5.0" -H "Cookie: sid=normal-user-1" \
       http://127.0.0.1:18081/?u=1                              → REAL-BUSINESS   ✅ 正常会话不受影响
```

**没有覆盖的**：

- **`Watch`**：本轮不实现，只有「返回 `Unimplemented`」的测试；
- **真实 PostgreSQL 的 `policy_ack` 保留期**（`NI-13`）：内存实现无所谓，接真库时要定；
- **多适配器同时拉起回执对账的集成测试**：只有单适配器的端到端；
- **策略版本切换（v1 → v2）的端到端**：单测覆盖了合并语义，未在真进程上跑版本跃迁；
- **注入片段 / 诱饵资产的边缘通路**：本轮不下发（见 §7）。

---

## 6. 验证证据

```console
$ make gate
门禁通过。          # fmt · vet · staticcheck · errcheck · archcheck · trace · leakcheck · license · test -race

$ go test ./edge/proxy/ ./core/internal/policy/ ./core/internal/store/ -count=1
ok  shen/edge/proxy      1.092s      # 37 例（新增 7 例策略应用语义）
ok  shen/core/internal/policy   0.719s   # 17 例（新增 5 例下发面）
ok  shen/core/internal/store    0.158s

$ make dev
✅ 通过：配置校验 · 非法配置被拒 · 启动 · 判定面 · 冒烟 · 规则回放
```

**关键指标**：测试函数全仓 **177 → 189**；策略面端到端「下发 → 应用 → 改道」**实测通过**；
新增依赖 **0**（复用已有 grpc）；新增模块 **0**（服务端落在 `policy`）。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | **注入片段 / 诱饵资产 / 预生成内容不经策略面下发** | 阶段 2b 的内容类能力在请求路径上还看不到 | [ADR-0018](../background/decisions/0018-policy-plane-pull-model.md)「未解决」；下一轮设计议题 |
| 2 | `Watch` 未实现 | 策略生效延迟上限 = 一个轮询间隔（默认 60s） | ADR-0018 失效条件 1 |
| 3 | 回执只落账、**不告警** | 「哪个适配器停在旧版本」只能被人查 | 运营面（`analytics/` 未建） |
| 4 | **决策缓存的键不含 UA** ⇒ 同一 `(IP, 会话, 路径, 60s)` 共享一个决策（实测可复现误调度/漏调度） | 误调度率（`guard.false_route_budget`） | 改它要动 `ST-10` → **单独一轮**；现状登记在 [`../kb/known-issues.md`](../kb/known-issues.md) `K-20` |
| 5 | `policy_ack` 无保留期配置；明文 gRPC 仍是同主机假设（已加非回环启动闸门） | 接真库 / 跨节点部署 | `NI-13` · mTLS（均未做） |

---

## 7.1 审视记录（L 档，做法见全局技能 `audit`）

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `docs/modules/policy.md` §8 未决 1/2/5 写「下发面未实现」「`Ack` 需要新实体」「`whitelist` 只解析不消费」 | **过期状态**（三条都已落地） | `grep -n "loader.Whitelist(" core/` · `store.RecordAck` · `policy.Server` | 三条改为 ✅ 已落地并写明新形态；新增未决 7（注入片段通路） | ✅ |
| 2 | `docs/design/structure.md` §1.6.4 写「策略面 ❌ 未实现」并在下面标注「唯一的结构性断点」 | **过期状态** | 本轮 `policy.Server` + `edge/proxy` 客户端 | 改为 ✅ 已接，并把「断点」改写为「处置内容的边缘通路仍未接通」 | ✅ |
| 3 | `docs/design/structure.md` §2.2 的 `S4` 行只写了「YAML + JSON Schema」 | 不完整（缺核心→适配器那一半） | 同上 | 拆成「控制面→核心」与「核心→适配器」两段并指向新契约 | ✅ |
| 4 | `docs/spec/config.md` §2.0 的 `decoys`/`honeypots`/`whitelist` 与 §2.8/§2.9 曾写「未接入 / 阶段 2b 才进示例配置」 | **过期状态**（上一轮已修，本轮复核） | `grep -n "loader.Decoys(" core/cmd/core/main.go` | 复核通过，无需改动 | ✅ |
| 5 | `docs/background/decisions/README.md` 有**两条 0017 索引行**（并行写入留下的重复） | 重复内容 | `grep -n "^| \[0017\]" docs/background/decisions/README.md` | 删掉较旧的一条，并补 0018 索引 | ✅ |
| 6 | 新写的 `edge/proxy/policy_test.go` 一度 import 了 `shen/core/internal/contract` | **规则违规**（`ST-3`：适配器禁止 import 核心内部；`archcheck` 会拦） | 自查 + `make archcheck` | 删除该 import，改用本包自持结构；并把这条纪律写进测试文件头注释 | ✅ |
| 7 | 回执在「校验和不匹配」时**每轮都重发**（测试暴露） | 缺陷（回执风暴） | `TestPolicyChecksumMismatchIsRejected` 首次运行失败：回执 4 条而非 1 条 | 加 `lastRejected` 去重：同一坏载荷只回执一次 | ✅ |
| 8 | 应用成功日志把 `len(payload)`（字节数）当日志输出成「N 个改道后端」 | 缺陷（日志误导） | 端到端日志出现 `168 个改道后端` | 改为读实际生效表长度 | ✅ |
| 9 | 核心明文 gRPC 监听无 fail-closed 保护（pi-lens 安全规则在改动文件上报出） | 安全缺口 | `core/cmd/core/main.go` 的 `net.Listen` | 新增 `assertPlaintextListenIsLocal`：非回环地址启动即失败（**不提供**开关），并加测试 | ✅ |
| 10 | `store.PolicyStore` 接口扩了两个方法后，`policy` 模块的测试替身未同步（编译失败） | 编译红 | `go test ./core/internal/policy/` 报 `*fakePolicyStore does not implement` | 替身补 `RecordAck` / `Acks` | ✅ |
| 11 | 策略替身 `stubPolicy` 没加锁：轮询 goroutine 写回执、测试 goroutine 读回执 | **数据竞争** | `make gate` → `--- FAIL: TestPolicyChecksumMismatchIsRejected … race detected`（普通 `go test` 不报，`-race` 才抓到） | 替身加 `sync.Mutex` + `ackList()` 快照；`go test -race -count=1 ./edge/proxy/` 通过 | ✅ |
| 12 | 变更包里的「测试 177 → 189」需可核 | 证据 | `cat core/internal/*/*_test.go edge/*/*_test.go core/cmd/core/*_test.go \| grep -c '^func Test'` → 189 | 已在 §6 写实测值 | ✅ |

> 历史记录类文件（[`../log.md`](../log.md) · `docs/plans/` · `docs/background/notes/` · `docs/kb/` 的历史条目）**不在审视范围**。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 策略面（S4）落地：`policy.Server`（`Pull` 投影 + `Watch` 未实现 + `Ack` 落账）· `PolicyStore` 扩回执（幂等）· 适配器拉取与合并应用（远端覆盖 + 白名单并集）· 载荷契约 `spec/policy-payload.md` · 明文监听闸门；测试 177 → 189；端到端「下发 → 应用 → 改道」实测通过 | 用户批准 9 项推荐 · [ADR-0018](../background/decisions/0018-policy-plane-pull-model.md) · `ST-8` / `AR-13` / `ST-24` |
