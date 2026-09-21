# 配置字典

> 本文件是**配置文档的契约**（接缝 **S4**，见 [`../design/structure.md`](../design/structure.md) §2.2）。
> 权威规则出处：[`../design/structure.md`](../design/structure.md) 的 `ST-20`…`ST-24`、
> [`../design/integration.md`](../design/integration.md) 的 `INT-24`、[`../design/modules.md`](../design/modules.md) 的 `MD-6`。
>
> **本文件不是规则文档** —— 它规定的是「配置文件长什么样、什么算非法」。
> 与 [`../design/`](../design/README.md) 冲突时以 `design/` 为准。
>
> ⚠️ **本文件与实现必须同步**：`common/core/internal/policy/policy.go` 的校验器实现本文件 §2 的全部约束；
> 改一处**必须**改另一处（`ST-13` 精神：新增字段必须同步更新本文件）。

---

## 1. 装载方式

| 项 | 值 |
| --- | --- |
| 格式 | YAML（`ST-24`：策略**必须**是数据，**禁止**编译进代码） |
| 路径 | 环境变量 `SHEN_CONFIG` **必须**显式提供；未设置时进程**必须**启动失败并给出提示 |
| 装载时机 | **仅在进程启动时装载一次**。本期不支持 SIGHUP 与文件监听 —— 运行时变更走策略下发面（`Pull` / `Ack` 已实现，[ADR-0018](../background/decisions/0018-policy-plane-pull-model.md)；**`Watch` 未实现**，故生效延迟 = 一个轮询间隔） |
| 解析严格度 | **未知键一律拒绝**（顶层与嵌套），非法值一律拒绝 |
| 非法配置的后果 | **必须**启动失败（非零退出码）并输出「字段路径 + 违反的约束」 |
| 监听地址 | 进程取环境变量 `SHEN_LISTEN`（缺省 `127.0.0.1:9443`）；`core.listen` 本期**只校验不消费**（见 §2.0） |

**禁止**出现的行为：以空规则集静默启动。`rules` 键缺失**必须**启动失败；
`rules: []` 合法，但启动日志**必须**打印 `WARN 规则集为空`（见 §3.3）。

密钥类配置项（`store.*.password` / `dsn`）**禁止**有默认值；在真实存储驱动接入后，
缺失**必须**导致启动失败（`ST-21`）。

| 开发期入口 | 命令 | 作用 |
| --- | --- | --- |
| 配置干跑 | `make check-config`（或 `core -check-config`） | 只装载 + 校验 + 打印策略摘要，**不开端口** |
| 端到端验证 | `make dev` | 配置干跑（合法 + 非法）→ 起核心 → 在线冒烟 → 规则回放 |
| 校验器实现 | `common/core/internal/policy/policy.go` | 本文件 §2 的可执行版本 |

> 校验失败时进程**必须**输出「字段路径 + 违反的约束」，例如
> `policy: rules[1].weight=1.5 越界（要求 0 < weight ≤ 1）` —— 只说「配置非法」不够，
> 开发者要能直接定位到那一行。

---

## 2. 字段表

### 2.0 本期消费状态

| 段 | 本期（阶段 2a） | 消费方 |
| --- | --- | --- |
| `policy` · `rules` | ✅ **已消费** | `policy` 模块（→ `judge` 的规则来源；版本与校验和进台账） |
| `thresholds` | ✅ **已消费** | `director`（阈值 → 三值，`ST-23` / `INT-24`） |
| `policy.gray_pct` | ✅ **已消费** | `director`（灰度收敛，`INT-12`） |
| `shadow` | ✅ **已消费** | `cmd/core` 装配（影子 → `ShadowDecider`；关闭 → `director`，`INT-11`） |
| `whitelist` | ✅ **已消费** | `director`（改道判定前置，`INT-25`）—— 关闭影子模式时经 `loader.Whitelist()` 注入决策层 |
| `guard` | ⏳ 只解析与校验 | 观测报表（误调度率护栏，阶段 2b+） |
| `decoys` | ✅ **已消费（核心内）** | `cmd/core` 装配时经 `loader.Decoys()` 写入 `store` → `decoy` 诱饵面 + `MD-25` 前缀集；⚠️ **边缘还取不到** —— 卡的不是策略面（已实现），而是**诱饵资产的来源与归属未定** —— 见 §2.8 |
| `honeypots` | ✅ **已消费（核心内）** | `cmd/core` 装配时经 `loader.Honeypots()` → `honeypot` 类型注册与后端池；⚠️ 同上，边缘不可达 —— 见 §2.9 |
| `injects` | ✅ **已消费并下发** | `policy` 装载（`loader.Injects()`）→ 策略面 `Pull` 下发（`inject_rules`）→ 适配器应用到改道侧响应；**未写该段**则不下发（适配器用本地 env）—— 见 §2.12 |
| `ai` | ✅ **部分已消费并下发** | `policy` 装载与校验；`ai.enabled` → 策略载荷 `inject_enabled`，`ai.manifest` → 装载内容清单 → 投影 `content_manifest` → 适配器注入到改道侧。`kinds` / `model` / `content.rotate_cooldown` 阶段 A **只解析与校验**（阶段 B 的生成与轮换消费）—— 见 §2.13 |
| `fingerprints` | ⏳ 未接入 | `judge`（指纹签名库，阶段 2b）—— 见 §2.10 |
| `attribution` | ⏳ 未接入 | `session`（归因令牌，阶段 2b）—— 见 §2.11 |
| `core` · `session` | ⏳ 只解析与校验 | 启动参数（本期端口仍取 `SHEN_LISTEN`，cookie 名仍为 `sid`） |
| `store` | ⏳ 只解析与校验 | 本期进程固定用内存实现；真实存储轮消费 |

> 未消费的段**不是**死配置：它们被严格校验，写错就启动失败 —— 这正是「静默忽略等于配置失效」的反面。

### 2.1 顶层

| 键 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `core` | object | ✅ | 见 §2.2 |
| `shadow` | bool | ✅ | `INT-11`：首次上线**必须**为 `true`（默认）；`director` 交付后允许关闭（运维动作），关闭时启动日志点名 |
| `session` | object | ✅ | 见 §2.2 |
| `thresholds` | object | ✅ | 见 §2.3 |
| `guard` | object | ✅ | 见 §2.3 |
| `store` | object | ✅ | 见 §2.6 |
| `policy` | object | ✅ | 见 §2.7 |
| `rules` | array | ✅ | 键**必须**存在；可为 `[]`；元素见 §2.4 |
| `whitelist` | object | ✅ | 见 §2.5（**本期只解析与校验，不消费**） |
| `ai` | object | ⚪ **可选** | AI 能力服务开关与内容清单；**整段不写 = 全关**（`enabled=false`），行为与今天逐字节一致 —— 见 §2.13 |

顶层**禁止**出现上述之外的键。

### 2.2 `core` · `session`

| 键 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `core.listen` | string | ✅ | 必须可被 `net.SplitHostPort` 解析；本期只校验不消费（监听地址取 `SHEN_LISTEN`） |
| `session.cookie_name` | string | ✅ | 非空；会话身份三级优先级的第 ① 级（`INT-19`） |

### 2.3 `thresholds` · `guard`

| 键 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `thresholds.route_mirage` | number | ✅ | `[0,1]`，且 `≤ thresholds.block` |
| `thresholds.block` | number | ✅ | `[0,1]` |
| `guard.false_route_budget` | number | ✅ | `[0,1]` |

> 阈值**必须**集中定义并可配置覆盖（`ST-23` / `INT-24`）。
> 阶段 2a 起由 `director` 消费（阈值 → 三值）；启动时校验，错值让进程启动失败。

### 2.4 `rules[]`

| 键 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `rules[].id` | string | ✅ | 非空；**全文档唯一** |
| `rules[].weight` | number | ✅ | `(0,1]` —— 判定分截顶在 1（`judge.Engine`），权重 >1 无意义 |
| `rules[].match` | object | ✅ | 见下 |
| `rules[].match.field` | enum | ✅ | `user_agent` · `path` · `method` · `source_ip` · `tls_fingerprint` |
| `rules[].match.op` | enum | ✅ | `equals` · `prefix` · `contains` |

> **匹配语义的实测行为（写规则前必须知道；证据见 [`../ops/functional-verification.md`](../ops/functional-verification.md)）**：
>
> | 项 | 实测行为 | 后果 |
> | --- | --- | --- |
> | `prefix` | **纯字符串前缀**，不做路径段归一化 | `/.git` 会命中合法的 `/.gitignore`（误伤）；`/static/../.git/config` 不以 `/.git` 开头 ⇒ 不命中（可被前缀绕过） |
> | `contains` | 子串包含，**按原始字符串** | `%2e%2e%2f` 不会被 `contains "../"` 命中（一次 URL 编码即绕过） |
> | `path` 字段取值 | **不含查询串**（`/search?q=union+select` 的 `path` 是 `/search`） | 载荷放在查询参数里的注入/穿越**不参与判定**（当前最大的一处能力缺口） |
> | 分数 | 命中权重求和后**在 1.0 处截断** | `ua-nuclei` 0.6 + `path-actuator` 0.4 = 1.00（触顶，不再累加） |
| `rules[].match.value` | string | ✅ | 非空 |

元素的**禁止**出现上述之外的键。

> 字段与算符**必须**与 `contract.Match` / `Observation.Field` 一致 —— 类型定义在
> [`common/core/internal/contract/verdict.go`](../../common/core/internal/contract/verdict.go) 与
> [`observation.go`](../../common/core/internal/contract/observation.go)，本表是它的**契约化描述**。
> 新增字段或算符**必须**同时改实现与本节（`MD-5`：跨模块共享类型收敛到一处）。

### 2.5 `whitelist`（本期预留）

| 键 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `whitelist.source_cidrs` | string[] | ✅ | 每项可被 `netip.ParsePrefix` 解析 |
| `whitelist.user_agents` | string[] | ✅ | 每项非空 |
| `whitelist.path_prefixes` | string[] | ✅ | 每项以 `/` 开头 |

> **本期只解析与校验、不消费。** 消费方是 `director` 的引流判定前置（`INT-25`：内部 IP / 健康检查 / 监控探针
> **必须**在引流判定前生效）。预留的理由：配置面先就位，避免「做完了引流才想起白名单」。
> 三个列表均允许为空数组（= 未启用）。

### 2.6 `store`

| 键 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `store.driver` | enum | ✅ | **本期只接受 `memory`**；`redis` / `clickhouse` / `postgres` 等其他值报「阶段 2 未实现」 |
| `store.redis.addr` | string | ✅ | 键必须存在，值可为空串 |
| `store.redis.password` | string | ✅ | 同上；**禁止**写入日志或策略载荷（`ST-20`） |
| `store.clickhouse.addr` | string | ✅ | 同上 |
| `store.clickhouse.database` | string | ✅ | 同上 |
| `store.postgres.dsn` | string | ✅ | 同上 |

> 核心访问外部存储**必须**经 `store` 模块（`MD-20`）。本期 `driver: memory` 时这些值不被消费，
> 但键**必须**存在 —— 缺键说明配置文件不完整，**禁止**静默补默认值。

### 2.7 `policy`

| 键 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `policy.policy_id` | string | ✅ | 非空；策略标识，用于台账与对账（`AR-13`） |
| `policy.version` | integer | ✅ | `≥1`；**必须**严格大于版本台账中的当前版本（`store.PolicyStore` 侧强制） |
| `policy.gray_pct` | integer | ✅ | `[0,100]` 的整数百分比 |

> `version` **必须**由配置文件显式声明，理由是版本号是**运维语义**（可编排、可对账、可回滚）；
> 候选对比与失效条件见 [ADR-0009](../background/decisions/0009-policy-version-source.md)。
> **回滚不是回退版本号**，而是发布一个内容为旧版、版本号更高的版本。
> `gray_pct` 由 `director` 消费（灰度收敛，`INT-12`）；0 = 全放行（影子等价），100 = 全改道。

### 2.8 `decoys`（阶段 2b 接入）

| 键 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `decoys.enabled` | bool | ✅ | 诱饵面总开关；默认 `false` |
| `decoys.developer_api` | bool | ✅ | Developer API 诱饵（[ADR-0010](../background/decisions/0010-functional-camouflage.md) 形态 1） |
| `decoys.instruction_files` | bool | ✅ | 指令文件蜜饵（形态 2：`AGENTS.md` / `CLAUDE.md` / `.cursorrules`） |
| `decoys.mcp` | bool | ✅ | MCP 诱饵（形态 3） |
| `decoys.dataset` | bool | ✅ | 消耗战数据集（形态 4） |
| `decoys.baits` | bool | ✅ | 三类蜜饵 + SSRF 蜜饵（形态 5） |

> 诱饵面**必须** observe-only（`MD-25`）。本段**已进** [`../../deploy/config/config.example.yaml`](../../deploy/config/config.example.yaml) 与校验器（`policy.go`），
> 且 `cmd/core` 装配时已消费（`loader.Decoys()` → `store` → 诱饵面）；**未接的只是边缘那一跳** ——
> 而卡住它的**不是策略面**（`Pull` / `Ack` 已实现），是**诱饵资产「谁产出、存在哪」没有定义**（见 [`../modules/README.md`](../modules/README.md) §0.4 与 [`../modules/decoy.md`](../modules/decoy.md) §8）。

### 2.9 `honeypots`（阶段 2b 接入）

| 键 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `honeypots.<type>.enabled` | bool | ✅ | 该类型是否启用；默认 `false` |
| `honeypots.<type>.backend` | string | ✅ | 后端地址或逻辑名 |

> `<type>` 取蜜罐类型清单（`ssh` / `mysql` / `redis` / `ftp` / `elasticsearch` / `nginx-admin` / `web-clone` / `internal-wiki` / `database`），
> 见 [`../modules/honeypot.md`](../modules/honeypot.md) §1。默认全部 off（`INT-11`）。
> 本段**已进** [`../../deploy/config/config.example.yaml`](../../deploy/config/config.example.yaml) 与校验器，且 `cmd/core` 已消费（`loader.Honeypots()` → 类型注册与后端池）。

### 2.10 `fingerprints`（阶段 2b 接入）

| 键 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `fingerprints` | array | ✅ | 指纹签名；可为 `[]` |
| `fingerprints[].key` | string | ✅ | 非空；**全文档唯一** |
| `fingerprints[].label` / `.vendor` / `.kind` | string | ✅ | 产品名 / 厂商 / 类型 |
| `fingerprints[].ua_markers` | string[] | ✅ | 强 UA 标记（置信度 0.9） |
| `fingerprints[].ua_weak_markers` | string[] | ✅ | 弱 UA 标记（置信度 0.6） |
| `fingerprints[].ua_prefixes` | string[] | ✅ | UA 前缀（置信度 0.75） |
| `fingerprints[].header_markers` | string[] | ✅ | 特征头（每命中 +0.03） |
| `fingerprints[].header_pairs` | string[] | ✅ | 头对（命中 +0.05） |

> 依据 [ADR-0010](../background/decisions/0010-functional-camouflage.md)。消费者是 `judge`（见 [`../modules/judge.md`](../modules/judge.md) §4.1）。
> 指纹库**自行整理**，不照搬 AGPL 实现的签名表（许可风险）。**本段在阶段 2b 实现时加入 `config.example.yaml` 与 JSON Schema**。

### 2.11 `attribution`（阶段 2b 接入）

| 键 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `attribution.enabled` | bool | ✅ | 归因令牌总开关；默认 `false` |
| `attribution.secret` | string | ✅ | HMAC 密钥；**禁止默认值**（`ST-21`），缺失即启动失败；**禁止**写入日志或策略载荷（`ST-20`） |
| `attribution.token_ttl` | duration | ✅ | 令牌有效期；**必须** > 0 |
| `attribution.watermark_prefix` | string | ✅ | 凭证水印前缀 |

> 依据 [ADR-0013](../background/decisions/0013-attribution-token.md)。消费者是 `session`（见 [`../modules/session.md`](../modules/session.md)）。
> **本段在阶段 2b 实现时加入 `config.example.yaml` 与 JSON Schema**。

### 2.12 `injects`（响应改写规则）

| 键 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `injects[]` | array | ⚪ 可选 | 响应改写规则列表。**整个段不写** = 不下发给适配器（适配器继续用本地 env 规则）；**写空数组** = 显式下发「无规则」（运营籍此主动关掉注入） |
| `injects[].kind` | string | ⚪ 可选 | 分类：`developer_api` / `instruction_file` / `hidden_link` / `dataset`（或空串 = 未分类）。**只用于组织与审计，不改变注入行为** |
| `injects[].snippet` | string | ✅ | 注入片段，**非空** |
| `injects[].marker` | string | ⚪ 可选 | 插入位置标记；空 = 执行方用 `</body>` |

> **数组顺序即执行顺序**（不得重排）。它只注入**改道侧**的 HTML 响应（`INT-8`；业务侧响应一律原样透传）。
> 消费者链路：`policy` 装载 → 策略面 `Pull` 下发 → 适配器应用 → `edge-injection` 执行；
> 载荷字段定义见 [`policy-payload.md`](policy-payload.md)。
> 与 `decoys` 的区别：诱饵资产定义「投放什么钩子」，本段定义「往改道侧响应里插什么」。

### 2.13 `ai`（AI 能力服务与欺骗内容）

| 键 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `ai.enabled` | bool | ⚪ 可选（默认 `false`） | 能力级总开关。**必须**为 `false` 时保持「不生成、不新增内容、不下发内容」（存量内容仍在库）；它**只**影响内容注入，不影响判定与改道（`ADR-0023` 决定 4） |
| `ai.kinds` | string[] | ⚪ 可选（默认 `["content"]`） | 启用的任务种类（kind）。每项**必须**非空且**必须**互不相同；未登记于任务注册表的 `kind` 由生成侧拒绝（`AR-33`），核心只校验形状 |
| `ai.model` | string | ⚪ 可选（默认 `""`） | 模型后端标识。**空串是阶段 A 的合法取值**（确定性模板生成器不调模型）；阶段 B 起，声明 `requires_model` 的任务在空值时**必须**显式失败（禁止用模板冒充模型输出，`AR-15`） |
| `ai.manifest` | string | ⚪ 可选（默认 `""`） | 内容清单文件路径（[`ai-contract.md`](ai-contract.md) §3）。空串 = 无清单。非空时**必须**能被读取、解析、校验；**任一失败即启动失败**（与配置文件同样的严格度） |
| `ai.content.variants` | integer | ⚪ 可选（默认 `8`） | 变体数 N。**必须** ≥ 1；**必须**与清单文件的 `variants` 一致，不一致 ⇒ **拒绝装载**（启动失败） |
| `ai.content.rotate_cooldown` | duration | ⚪ 可选（默认 `"30m"`） | 轮换冷却。**必须** > 0；可被 `time.ParseDuration` 解析。阶段 A **只解析与校验**（轮换接线在阶段 B） |

> **默认全关**是刻意的：不写 `ai` 段 ⇒ `inject_enabled=false` ⇒ 适配器不注入、上报 `inject=disabled`，
> 其余行为与今天逐字节一致（验收判据 ①）。
> 消费链路：`policy` 装载 → `ai.manifest` 载入 `store.ContentStore` → 投影进策略载荷（`inject_enabled` + `content_manifest`）
> → 适配器按 `(resource, variant)` 命中并注入改道侧。
> 非法清单的**每一类失败**都有单测守着（见 `common/core/internal/policy/ai_test.go`）。

---

## 3. 策略载荷（`contract.PolicySnapshot`）

| 字段 | 来源 |
| --- | --- |
| `PolicyID` | `policy.policy_id` |
| `Version` | `policy.version` |
| `GrayPct` | `policy.gray_pct`（`0..100` → `uint8`） |
| `Rules` | `rules`（按文件顺序） |
| `Payload` | 见 §3.1 |
| `Checksum` | 见 §3.1 |

### 3.1 载荷与校验和

`Payload` = **规范化 JSON**，只含三段，键序固定为 `policy` → `rules` → `whitelist`：

```json
{"policy":{"policy_id":"core-rules","version":1,"gray_pct":0},
 "rules":[{"id":"ua-headless","weight":0.6,"match":{"field":"user_agent","op":"contains","value":"HeadlessChrome"}}],
 "whitelist":{"source_cidrs":[],"user_agents":[],"path_prefixes":[]}}
```

`Checksum` = `sha256(Payload)` 的小写十六进制（64 字符）。

> 上例为便于阅读做了换行；`Payload` 本身是**紧凑 JSON**（无多余空白、键序固定），
> 因此同一份内容**必须**得到同一个校验和。

**禁止**进入 `Payload` 的内容：

| 排除项 | 原因 |
| --- | --- |
| 整个 `store` 段 | 含口令与 DSN；策略载荷会下发给 L1/L2/L3（`ST-20`） |
| `core` / `shadow` / `session` / `thresholds` / `guard` | 核心自身的运行参数，不是策略（`MD-12`：决策取值与阈值**禁止**写进对外契约） |

### 3.2 校验和的可复现性

同一份配置内容**必须**产出同一个 `Checksum` —— 键序固定、无多余空白、无 map 迭代顺序依赖。
检验方式：同一文档装载两次，`Checksum` 必须相等；改动任一 `weight` 必须使其改变。

### 3.3 启动日志（可观测证据）

判定响应**禁止**回显分值、规则名与决策枚举（`ST-7`），因此「策略是否真的生效」只能由启动日志证明。
进程**必须**打印一行：

```text
策略已装载 policy_id=<id> version=<n> checksum=<hex> 规则=<m> 条 灰度=<gray>%
```

`rules: []` 时**必须**额外打印：

```text
WARN 规则集为空：判定恒零分、无信号（仅用于链路联调）
```

---

## 4. JSON Schema（规范性）

机器可读的同一份约束。校验器**必须**与之等价；两者漂移属缺陷。

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://shen/spec/config.json",
  "title": "核心配置",
  "type": "object",
  "additionalProperties": false,
  "required": ["core", "shadow", "session", "thresholds", "guard", "store", "policy", "rules", "whitelist"],
  "properties": {
    "core": {
      "type": "object",
      "additionalProperties": false,
      "required": ["listen"],
      "properties": { "listen": { "type": "string", "minLength": 1 } }
    },
    "shadow": { "const": true },
    "session": {
      "type": "object",
      "additionalProperties": false,
      "required": ["cookie_name"],
      "properties": { "cookie_name": { "type": "string", "minLength": 1 } }
    },
    "thresholds": {
      "type": "object",
      "additionalProperties": false,
      "required": ["route_mirage", "block"],
      "properties": {
        "route_mirage": { "type": "number", "minimum": 0, "maximum": 1 },
        "block": { "type": "number", "minimum": 0, "maximum": 1 }
      }
    },
    "guard": {
      "type": "object",
      "additionalProperties": false,
      "required": ["false_route_budget"],
      "properties": { "false_route_budget": { "type": "number", "minimum": 0, "maximum": 1 } }
    },
    "store": {
      "type": "object",
      "additionalProperties": false,
      "required": ["driver", "redis", "clickhouse", "postgres"],
      "properties": {
        "driver": { "const": "memory" },
        "redis": {
          "type": "object",
          "additionalProperties": false,
          "required": ["addr", "password"],
          "properties": { "addr": { "type": "string" }, "password": { "type": "string" } }
        },
        "clickhouse": {
          "type": "object",
          "additionalProperties": false,
          "required": ["addr", "database"],
          "properties": { "addr": { "type": "string" }, "database": { "type": "string" } }
        },
        "postgres": {
          "type": "object",
          "additionalProperties": false,
          "required": ["dsn"],
          "properties": { "dsn": { "type": "string" } }
        }
      }
    },
    "policy": {
      "type": "object",
      "additionalProperties": false,
      "required": ["policy_id", "version", "gray_pct"],
      "properties": {
        "policy_id": { "type": "string", "minLength": 1 },
        "version": { "type": "integer", "minimum": 1 },
        "gray_pct": { "type": "integer", "minimum": 0, "maximum": 100 }
      }
    },
    "rules": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "weight", "match"],
        "properties": {
          "id": { "type": "string", "minLength": 1 },
          "weight": { "type": "number", "exclusiveMinimum": 0, "maximum": 1 },
          "match": {
            "type": "object",
            "additionalProperties": false,
            "required": ["field", "op", "value"],
            "properties": {
              "field": { "enum": ["user_agent", "path", "method", "source_ip", "tls_fingerprint"] },
              "op": { "enum": ["equals", "prefix", "contains"] },
              "value": { "type": "string", "minLength": 1 }
            }
          }
        }
      }
    },
    "whitelist": {
      "type": "object",
      "additionalProperties": false,
      "required": ["source_cidrs", "user_agents", "path_prefixes"],
      "properties": {
        "source_cidrs": { "type": "array", "items": { "type": "string", "minLength": 1 } },
        "user_agents": { "type": "array", "items": { "type": "string", "minLength": 1 } },
        "path_prefixes": { "type": "array", "items": { "type": "string", "pattern": "^/" } }
      }
    }
  }
}
```

**Schema 未覆盖、由校验器补充的三条**（JSON Schema 表达力之外）：

| # | 约束 | 依据 |
| --- | --- | --- |
| 1 | `core.listen` **必须**可被 `net.SplitHostPort` 解析 | §2.2 |
| 2 | `thresholds.route_mirage ≤ thresholds.block` | §2.3 |
| 3 | `rules[].id` **必须**全文档唯一 | §2.4 |
| 4 | `whitelist.source_cidrs` 每项**必须**可被 `netip.ParsePrefix` 解析 | §2.5 |

---

## 5. 示例

见 [`deploy/config/config.example.yaml`](../../deploy/config/config.example.yaml) ——
它**必须**始终能通过本文件的全部约束（有单测守着，防止样例与 schema 分叉）。
真实配置文件**禁止**入库（`ST-20`），由 `SHEN_CONFIG` 指向。

---

## 6. 未决项

| # | 未决 | 去向 |
| --- | --- | --- |
| 1 | 新增真实存储驱动（`redis` / `clickhouse` / `postgres`）时的字段与校验 | 真实存储轮 |
| 2 | 策略下发面已实现 `Pull` / `Ack`（[ADR-0018](../background/decisions/0018-policy-plane-pull-model.md)）；**`Watch` 未实现**；`fingerprints` / `attribution` 两段仍只解析不消费 | 下发面后续轮 / 阶段 2b |
| 3 | ✅ **已消费（2026-09-19）**：`whitelist` 的消费方是 `director`（核心内判定前置）与适配器（经策略面下发，`INT-25`） | —— |
| 4 | 环境变量覆盖范围是否扩展到其他键 | 部署形态轮 |
