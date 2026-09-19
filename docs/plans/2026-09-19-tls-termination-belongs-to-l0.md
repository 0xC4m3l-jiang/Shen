# 变更包：TLS 终结默认交客户 L0（`E2` 实测驱动的重估）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 把 `E2` 的实测结论落成决策与代码：**默认交客户 L0 终结 TLS**；自终结降级为备选并**启动即警告** |
| 日期 | 2026-09-19 |
| 状态 | 已验证（告警函数含测试；`make gate` 通过；已提交） |
| 改动分级 | **L**（改对外形态的建议姿态 + 影响 `ADR-0017` 的结论 + 触及 4 份设计文档的表述） |
| 涉及 | [`ADR-0019`](../background/decisions/0019-tls-termination-belongs-to-l0.md)（新增）· `edge/proxy/embed.go` · `edge/proxy/cmd/proxy/main.go` · `edge/proxy/embed_test.go` · [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) · [`../design/architecture.md`](../design/architecture.md) · [`../design/integration.md`](../design/integration.md) · [`../background/decisions/0017-caddy-l1-base.md`](../background/decisions/0017-caddy-l1-base.md) · [`../background/decisions/README.md`](../background/decisions/README.md) · `edge/proxy/README.md` · `edge/proxy/config/front-proxy.example.env` |
| 决策数 | 已答 5 项（`ADR-0019` 的「决定」）/ 待定 4 项（该 ADR 的「未解决」） |
| 关联 | 实验 `E2`（[`../background/notes/pending-experiments.md`](../background/notes/pending-experiments.md)）· 威胁模型 `A2` · `INT-22` · `AR-4` · [`2026-09-19-e2-tls-fingerprint-tool.md`](2026-09-19-e2-tls-fingerprint-tool.md) |

---

## 1. 需求与验收

**要解决什么**：`E2` 主臂实测显示「我方内嵌 Caddy 终结 TLS」与公有站栈在 **ServerHello 扩展顺序**上可区分
（JA3S `43-51` vs `51-43`），按 `E2` 判定标准属**不可对齐** ⇒ 威胁模型 `R-1` 必须重估。
而 `A2`（对手无法察觉被骗）是本项目的**唯一命题** —— 这条实测直接动摇了 `ADR-0017` 的一个核心假设。

**做完之后**：默认姿态改为**交客户 L0 终结**（客户 L0 = 真实站同款栈 ⇒ 指纹**构造性一致**，且我们读明文、`INT-22` 不破）；
自终结保留但**必须知情**（启动日志告警，点名 `JA3S` 差异与 `ADR-0019`）。

**验收判据**：

1. 新 ADR 记录决策、候选、后果与**失效条件**；`ADR-0017` 标注「TLS 部分被 0019 取代」，ID 与正文保留（`D-6`）。
2. 四份设计/模块文档的表述同步（`modules/adapter-proxy.md` §1/§3.1/§8/§9 · `architecture.md` §10.2 缺口 14 · `integration.md` 的 `INT-22` 注记 · 决策索引）。
3. 运营面同步：`edge/proxy/README.md` 与 `edge/proxy/config/front-proxy.example.env` 写明「默认 off = L0 终结」**及其原因**。
4. 代码：`SelfTerminationWarning`（纯函数）+ 启动接线 + 单测（明文空、manual/acme 非空且点名 `JA3S`/`L0`/`ADR-0019`）。
5. `make gate` 绿；本轮已提交。

**不做什么**：**不删**内嵌 Caddy 的 TLS 能力（它是备选路径）· 不改 `SHEN_PROXY_TLS_MODE` 的默认值（本来就是 `off`）· 不改任何规则正文（只改表述与注记）。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | 默认谁终结 TLS | **客户 L0** | 攻击者看到的是**终结者**的栈；唯一能让指纹构造性一致的路 |
| ② | 自终结怎么办 | 保留 + **启动告警** | 客户没有 L0 时仍需要它；但必须知情（警告里点名实测结论与 ADR） |
| ③ | `INT-22` 会破吗 | 不会 | L0 终结后我们收明文，读得到请求体 |
| ④ | `AR-4` 怎么看 | L0 保留时**只做入口路由与 TLS**，不得再做七层路由决策 | 请求路径上做七层决策的仍只有一个组件 |
| ⑤ | 记在哪 | **新 ADR-0019**（不是改 0017） | 改的是默认姿态与结论，按项目规范另立记录并标注取代关系 |

---

## 3. 追溯矩阵

| 规则 / 依据 | 文档 | 代码 | 测试 | 验证命令 |
| --- | --- | --- | --- | --- |
| `A2`（不可区分是唯一命题） | [`../background/notes/threat-model.md`](../background/notes/threat-model.md) · [`ADR-0019`](../background/decisions/0019-tls-termination-belongs-to-l0.md) | `SelfTerminationWarning` | `TestSelfTerminationWarning` | `make gate` |
| `INT-22`（TLS 可读） | [`../design/integration.md`](../design/integration.md) | —— （L0 终结后读明文，前提保持） | 文档评审 | —— |
| `AR-4`（每层一个组件） | [`../design/architecture.md`](../design/architecture.md) | —— （L0 只做入口路由与 TLS） | 文档评审 | —— |
| `D-6`（ID 不复用、旧记录保留） | [`../design/README.md`](../design/README.md) | —— | `make trace` | `make trace` |
| `TB-15` | [`../design/language.md`](../design/language.md) | —— | 上述测试 | `make gate` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `edge/proxy/embed.go` | 改 | 新增 `SelfTerminationWarning(cfg) string`：明文返回空；`manual`/`acme` 返回**基于实测**的风险提示（点名 `JA3S`/`L0`/`ADR-0019`） |
| `edge/proxy/cmd/proxy/main.go` | 改 | 启动日志后接线：有提示就打 `警告：…` |
| `edge/proxy/embed_test.go` | 改 | `TestSelfTerminationWarning`（3 类断言） |
| [`../background/decisions/0019-tls-termination-belongs-to-l0.md`](../background/decisions/0019-tls-termination-belongs-to-l0.md) | **新增** | 决策记录：背景（`E2` 数据）· 候选 A/B/C · 决定 5 条 · 理由 · 对 B 的回应 · 后果 · **失效条件 4 条** · 未解决 4 条 |
| [`../background/decisions/0017-caddy-l1-base.md`](../background/decisions/0017-caddy-l1-base.md) · [`README.md`](../background/decisions/README.md) | 改 | 标注取代关系 + 索引加 0019 行 |
| [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) · [`../design/architecture.md`](../design/architecture.md) · [`../design/integration.md`](../design/integration.md) | 改 | §1/§3.1/§8/§9 与缺口 14、`INT-22` 注记的表述同步 |
| `edge/proxy/README.md` · `edge/proxy/config/front-proxy.example.env` | 改 | 运营面写明「默认 off = L0 终结」及其原因（否则没人知道为什么默认是明文） |

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | `tls_mode=off`（默认） | 无告警 | ✅ | `TestSelfTerminationWarning` |
| 2 | `tls_mode=manual` | 告警且点名 `JA3S` / `L0` / `ADR-0019` | ✅ | 同上 |
| 3 | `tls_mode=acme` | 同上 | ✅ | 同上 |
| 4 | 取代关系 | 0017 标注、索引含 0019、旧 ID 未动 | ✅ | `make trace` |

**没有覆盖的**：**告警是否真的出现在启动日志**（测试覆盖的是纯函数；端到端日志未断言 —— 日志断言脆且收益低，故不做）。

---

## 6. 验证证据

```console
$ go test ./edge/proxy/ -run SelfTermination -count=1
--- PASS: TestSelfTerminationWarning (0.00s)

$ make gate
门禁通过。
```

**关键指标**：新增 ADR **1 份** · 新增测试 **1 例（3 断言）** · 同步文档 **8 份** · 规则正文改动 **0 条**。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | `E2` 仍是**单站单次**初测 | 本决定建立在一份采样上 | 对客户真实站重复 ≥3 次 |
| 2 | L0 终结时的**来源 IP 透传**（`INT-23`）需接入物料写明（XFF / PROXY protocol） | 业务侧可能丢客户端 IP | 接入物料目录 integrate/（未建） |
| 3 | L0↔引擎那一跳明文还是 mTLS | 跨节点部署未定（当前只允许回环明文） | 接入演练 |
| 4 | 自终结 + ACME 仍在但**未被本决定背书** | 客户若无 L0，需自行评估指纹差异 | 接入物料 |
| 5 | ⚠️ **`ST-10` / `K-20` 误调度取舍**仍待裁决 | 关系 `NI-1` | 你裁决后开一轮 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `adapter-proxy.md` §8 未决项 1 写「TLS 由**本进程**终结（已结案）」——与本决定相反 | 过期结论 | 文档比对 | 改为「默认交客户 L0；自终结为备选 + 告警」，并指向 `ADR-0019` | ✅ |
| 2 | `architecture.md` §10.2 缺口 14 与 `integration.md` 的 `INT-22` 注记同样写着「由引擎内嵌 Caddy 终结」 | 过期结论 | 同上 | 两处同步为「默认 L0 终结」 | ✅ |
| 3 | `edge/proxy/README.md` 与 env 模板只讲「三取值」，**没说默认为什么是 off** | 漏写（运营无从理解） | 运营面文档 | 写明依据 `E2` 实测与 `ADR-0019` | ✅ |
| 4 | `ADR-0017` 未标注被取代 | 决策链断裂 | 决策索引对照 | 状态行标注「TLS 部分被 0019 取代」，ID 与正文保留（`D-6`） | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 新增 `ADR-0019`：**TLS 终结默认交客户 L0**（`E2` 实测驱动）；自终结保留 + 启动告警（含测试）；同步 8 份文档；`ADR-0017` 的 TLS 部分标注被取代 | 用户「按照你的推荐来」· 实验 `E2` · `A2` · `INT-22` · `AR-4` |
