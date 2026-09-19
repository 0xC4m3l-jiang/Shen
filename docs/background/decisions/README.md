# docs/decisions —— 决策记录（ADR）

> 本目录记录**每个选型的候选、理由、后果与失效条件**。
> 它**不是**基线：基线在 [`../../design/`](../../design/README.md)。本目录解释**基线为什么是现在这样**，以及**什么情况下应该重开**。
>
> 写法见技能 `.pi/skills/evidence-and-decisions/SKILL.md` §2。

---

## 1. 与 `docs/design/` 的分工

| | `docs/decisions/` | `docs/design/` |
| --- | --- | --- |
| 内容 | 候选对比、理由、负面后果、失效条件 | 已确认的规则（带 ID） |
| 表述 | 允许「倾向 / 提议 / 代价」 | 只允许规范性动词 |
| 何时用 | 决策**未定**或**刚定**时 | 决策**已确认**后（升格流程） |
| 编号 | `NNNN-<kebab-slug>`，递增，不删只废弃 | 规则 ID `<前缀>-<序号>` |

**流动方向**：`notes/`（讨论）→ `decisions/`（把候选摆开）→ `design/`（拍板成规则）。

---

## 2. 索引

| # | 决策 | 状态 | 约束的规则 | 未决项 |
| --- | --- | --- | --- | --- |
| [0001](0001-language-rust.md) | 在线组件语言定为 Rust（已否决的候选：Go / Lua / TypeScript） | ⤴ **被 0005 取代** | —— | 已移交（见该文件「未解决」） |
| [0002](0002-decision-model.md) | 决策取值：**三值 + `severity`**（否决五档的挑战页） | ✅ **已采纳** | [`design/modules.md`](../../design/modules.md) MD-12 / MD-13 / MD-23 / MD-24 | 1 项（`severity` 档位） |
| [0003](0003-integration-forms.md) | 接入形态：批准 Mirror / DNS / 反向代理 / Sidecar 四种，SDK 不批准 | ✅ **已采纳** | `INT-1`…`INT-10` | 4 项 |
| [0004](0004-terminology.md) | 术语全部改用行业术语（L0–L4 / 核心 / 适配器 / 蜜饵 / 蜜标 …） | ✅ **已采纳** | `TM-3`…`TM-11` | 3 项 |
| [0005](0005-layered-languages.md) | 语言改为分层多语言（L0–L4），不再是「在线全 Rust」 | ✅ **已采纳** | `TB-2` `TB-4` `TB-20`…`TB-26` | 4 项 |
| [0006](0006-core-language-go.md) | **核心层的语言复查**：Go / Rust 全栈（去 Lua）/ Rust+Lua 三候选 → **维持 Go** | ✅ **已采纳**（补充 0005） | —— | 1 项（`E1` spike） |
| [0008](0008-edge-language-go.md) | **L1 边缘层的语言**：改用 **Go 并移除 Lua**（否决 OpenResty + Lua / Rust Pingora / Envoy ext_proc）；③ 反向代理与 ④ Sidecar 合并为一个模块 | ✅ **已采纳** | 用户 | —— |
| [0007](0007-repo-layout.md) | **仓库布局**：按平面分组的扁平结构（否决 `src/` 单层） | ✅ **已采纳** | [`design/structure.md`](../../design/structure.md) 的 `ST-1`…`ST-5` | 3 项 |
| [0009](0009-policy-version-source.md) | **策略版本的来源**：配置文件显式声明（否决内容推导 / 启动自增） | ✅ **已采纳** | `AR-13` `ST-8` · [`spec/config.md`](../../spec/config.md) §2.7 | 2 项 |
| [0010](0010-functional-camouflage.md) | **欺骗的核心机制**：功能性伪装（否决显式提示注入） | ✅ **已采纳** | [`design/modules.md`](../../design/modules.md) 新增 `decoy` 模块 | 3 项 |
| [0011](0011-honeypot-entry-external-backends.md) | **蜜罐**：只做入口与后端池，具体蜜罐接第三方 | ✅ **已采纳** | [`design/modules.md`](../../design/modules.md) 新增 `honeypot` 模块 | 3 项 |
| [0012](0012-session-level-judgement.md) | **会话级判别的落点**：会话特征注入（`judge` 保持纯函数） | ✅ **已采纳** | `judge` 输入契约 · `session` 新增会话状态 | 2 项 |
| [0013](0013-attribution-token.md) | **归因令牌**：会话蜜标（双角色）+ 凭证水印 | ✅ **已采纳** | `session` 新增归因令牌 · `decoy` 携带令牌 | 3 项 |
| [0014](0014-generative-deceptive-response.md) | **生成式欺骗响应**：快慢两路 + 「同会话同资源同答案」不变量 | ✅ **已采纳** | `responder` 模块 · `AR-30` | 3 项 |
| [0015](0015-indirect-prompt-injection.md) | **间接提示注入防护**：数据与指令分离 | ✅ **已采纳** | `llm-components` 输入契约 · `AR-31` / `AR-32` | 3 项 |
| [0016](0016-decoy-polymorphism.md) | **诱饵多态与再生成**：会话间轮换 + 识破信号触发（与 `AR-30` 划界） | ✅ **已采纳** | `decoy` 变体轮换 · `chain` 识破信号 · `strategy` 再生成 | 4 项 |
| [0017](0017-caddy-l1-base.md) | **L1 反向代理底座**：内嵌 **Caddy**（TLS 终结 + 转发），判定仍是自研 Go 插件（否决维持标准库自研 / xcaddy 外部配置 / 换 Envoy） | ✅ **已采纳** | `AR-3` `AR-4` · `INT-22` · [`design/language.md`](../../design/language.md) §1 L1 行 | 4 项 |
| [0018](0018-policy-plane-pull-model.md) | **策略面（S4）的消费模型**：`Pull` 轮询 + `Ack` 回执 · JSON 载荷 · 远端覆盖本地兜底（白名单并集）；`Watch` 不做（否决流式推送 / 共享配置文件 / 继续只用 env） | ✅ **已采纳** | `ST-8` `ST-24` `AR-13` · [`spec/policy-payload.md`](../../spec/policy-payload.md) · [`design/structure.md`](../../design/structure.md) §1.6.4 / §3 | 5 项 |
| [0022](0022-l4-near-line-worker.md) | **L4 的运行时接缝：近线 worker** —— 读遥测 → 态势去重（`AR-14`）→ 意图/链/策略 → 结论**作为事件**上报；无执行面、不写存储、挂了不影响业务 | ✅ **已采纳** | `AR-12` · `AR-14` · `AR-32` · `NI-1` · `MD-20` | 3 项 |
| [0021](0021-l4-python-toolchain.md) | **L4 引入 Python 工具链**（`ruff` + `pytest`，锁定版本，仓库内 `.venv`）：要 L4 就必须有 Python 门禁（`TB-15`） | ✅ **已采纳** | `TB-2` · `TB-15` · `TB-20` · `TB-14` | 3 项 |
| [0020](0020-console-minimal-static-ui.md) | **控制台的最小实现**：Go 进程 + 静态页（无前端构建），只读；`language.md` 的控制台行**暂缓**（长期仍是 TypeScript，迁移前先引门禁） | ✅ **已采纳** | `AR-10` · `TB-15` · `TB-20` | 3 项 |
| [0019](0019-tls-termination-belongs-to-l0.md) | **TLS 终结归属**：默认**交客户 L0**（与真实站同款栈 ⇒ 指纹构造性一致）；自终结降级为「客户没有 L0」时的备选 + **启动即警告**。依据 `E2` 实测（ServerHello 扩展顺序不可对齐） | ✅ **已采纳** | `A2` · `INT-22` · `AR-4` · 取代 [0017](0017-caddy-l1-base.md) 的 TLS 部分 | 4 项 |

> **未决项的权威位置就是各 ADR 的「未解决」段**（依据用户的裁定）。
> 尚未成决策的议题在 [`../notes/implementation-discussion.md`](../notes/implementation-discussion.md) §6.1，
> 调研待办在 [`../research/knowledge-base-audit.md`](../research/knowledge-base-audit.md) §2。

### 状态图例

| 标记 | 含义 |
| --- | --- |
| 🟡 **提议** | 候选已摆开，**未拍板**。提议状态的记录可直接改写，不需要新开编号 |
| 🟡 **暂缓** | 用户明确要求推迟；**连原规则也不作为实现依据**，只保留占位。当前无暂缓项 |
| ✅ **已采纳** | 已拍板，改动需新理由或触发失效条件 |
| ❌ **已废弃** | 明确排除，保留记录避免重复讨论 |
| ⤴ **被 NNNN 取代** | 新记录生效，旧记录停止约束 |

---

## 3. 重开与暂缓

### 3.1 重开（规则不再成立，但尚未拍板新方案）

1. 在本目录新开一条**提议**状态的 ADR，把真实候选摆开（**禁止**稻草人候选）；
2. 在 `../../design/` 对应规则后标注 `（重开中 → ADR-NNNN）`，**效力暂停** —— 仍按原文执行，但新增实现应避免依赖；
3. **保留规则 ID 与文本**，不得删除、不得重新编号（依据 `D-6` / `D-7`）；
4. 用户拍板后：ADR 转「已采纳」→ 按升格流程更新 `../../design/` → 去掉标注；
5. 若结论是「维持原判」：删除标注，ADR 转「已废弃（维持原规则）」并写清为什么不改。

### 3.2 暂缓（用户明确要求推迟）

与重开不同：**连原规则也不作为实现依据**，只保留占位。
`../../design/` 中**禁止**把暂缓项当既定事实陈述，必须显式标「待定」。
当前暂缓项：**无**（`0002` 决策取值已于 2026-09-17 结案）。

### 3.3 写入顺序

- `../../design/` 的规则**必须**先在 ADR 中定案后才写入；
- 已废除的旧规则**必须**登记在 [`../../design/README.md`](../../design/README.md) §4.1，**禁止**静默删除。
