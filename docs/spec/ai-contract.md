# 契约：AI 能力服务与欺骗内容（`ai-capability`）

> **状态**：已实现（阶段 A —— 通路 · 开关 · 强制护栏）。
> 本文件是**跨模块与跨语言契约**：Python 侧生成（`analysis/aicap/`）、核心侧装载与投影（`core/internal/policy/`）、
> L1 适配器消费（`deception/proxy/`）三方**必须**同键、同语义。
>
> 规则依据：`AR-33`（生成必须经护栏出口）· `AR-15`（结构由独立代码层校验）· `AR-16`（统一信封）·
> `AR-22`（黑名单三类）· `AR-23`（分用途长度）· `AR-24`（提示词资源化）· `AR-30`（同会话同资源同答案）·
> `AR-31`（不可信数据区）· `AR-32`（分析侧无执行面）· `MD-3`（跨模块契约放 `../spec/`）。
> 决策与失效条件：[ADR-0023](../background/decisions/0023-deception-content-injection.md)。
>
> **本文件不是规则文档** —— 它规定「字节长什么样、什么算非法、谁校验什么」。
> 与 [`../design/`](../design/README.md) 冲突时以 `design/` 为准。

---

## 0. 三个角色与各自的校验责任

```text
① 生成期（Python · analysis/aicap/）      ② 装载期（核心 · policy）            ③ 消费期（适配器 · deception/proxy）
   generate(TaskSpec) → Envelope            读清单文件 → 逐条验校验和            读策略载荷 → 命中 → 再验校验和
   ├─ 前置护栏（提示词三段式）                → 验单条上限 / 总上限                 → 选变体（会话钉定）
   ├─ 生成（模板生成器 / 模型）               → 验 variants 与配置一致              → 构造 per-request 规则
   └─ 后置护栏（schema/黑名单/长度/风格）      → 投影进策略载荷                      → injection.Inject（改道侧）
   未过护栏 ⇒ 不入库 ⇒ 不进清单              任一条坏 ⇒ 丢该条 + warn（不整份作废）   任一不成立 ⇒ 不注入（原样返回）
```

**三处都验**是有意的：`AR-22` 的黑名单挡的是「生成出错」，校验和挡的是「字节在路上被改过或版本错配」，
而适配器的「任何一步不成立就原样返回」挡的是「注入失败变成业务失败」（`NI-1`）。

---

## 1. `TaskSpec` · `Envelope` · 任务注册表

### 1.1 `TaskSpec`（调用方唯一能表达的东西）

| 字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `kind` | string | ✅ | 任务种类；**必须**已在任务注册表登记，未登记即拒绝 |
| `session_id` | string | ✅ | 会话标识；`AR-25` 的一等字段（生成期作为**分类依据**，不进热路径） |
| `deadline_s` | number | ✅ | > 0；生成的时限（`AR-19` 的双阶段收尾用） |
| `payload` | object | ✅ | 任务输入；**攻击者可控的一切**只允许出现在这里（`AR-31` 的数据区来源） |

### 1.2 `Envelope`（输出只允许这一种形状，`AR-16`）

```json
{"accepted": true, "data": {...}}
{"accepted": false, "data": {}, "rejected_reason": "..."}
```

- `accepted=false` **必须**带非空 `rejected_reason`；`accepted=true` **禁止**携带 `rejected_reason`；
- 实现：`analysis/llm/envelope.py`（复用，不重定义）。

### 1.3 任务注册表（`analysis/aicap/tasks/`）

每个 `kind` 一条登记，**必须**声明下表全部项；缺一样即**启动期断言失败**（`AR-33` 的结构闸门）：

| 声明 | 类型 | 含义 |
| --- | --- | --- |
| `schema` | `llm.contract.Schema` | 输出的结构契约（后置校验第一关，`AR-15`） |
| `guardrail_profile` | `GuardrailProfile` | 护栏档案（见 §1.4）；**不允许**为空或未定义 |
| `limits.purpose` | string | 长度用途（`AR-23`）；**必须**是 `llm.limits.PURPOSE_LIMITS` 里已登记的用途 |
| `limits.max_output` | integer | **任务级**输出上限，必须 > 0；与用途上限**取较小者**生效（`AR-23`）。声明了就必须真的生效 —— 不生效的上限比没有上限更坏 |
| `produce` | callable | `(prompt, spec, client) → 候选输出`（**未过护栏**） |
| `build` | callable | `(checked, spec, generated_at) → Artifact`；内核只要求产物能 `to_wire()` |
| `generator` | string | 生成器标识（进产物，供审计定位「这份东西是哪个版本产出的」） |

### 1.4 护栏档案 `GuardrailProfile`

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| `name` | string | 非空；档案标识（进日志与拒绝原因） |
| `prompt` | string | 提示词模板名；**必须**能在 `analysis/aicap/resources/prompts/*.md` 里加载到（`AR-24`） |
| `style_terms` | string[] | 非空；风格一致性判据 —— **受检字段**必须至少命中一个（否则判为「不像该画像」） |
| `checked_fields` | string[] | **非空必填**；后置护栏第 2–4 关作用在**哪几个输出字段**上。声明了却不在输出里、或不是字符串 ⇒ **拒绝**（**不得**静默跳过）—— 依据 [ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md) 决定 3 |

> `purpose` **不在**本表里：它是 `TaskLimits` 的字段（见 §1.3）——「长度用途」属于输出纪律，不属于画像。
> （本表在 2026-09-20 之前把 `purpose` 错写在这里，与实现不一致。）

### 1.5 前置护栏提示词的三段式（`AR-31` / `AR-24`）

模板**必须**包含且按顺序包含三段：

```text
# 任务          ← 指令区：任务说明 + 允许/禁止清单 + 输出契约（**不含**任何攻击者可控内容）
# 画像          ← 去敏的站点术语与模板（术语来自 style_terms，不含真实资产标识）
# 不可信数据     ← 数据区：{{untrusted_data}}（由 llm.untrusted.as_data_block() 产出）
```

启动期**必须**校验：模板存在、占位符齐全、数据区标记成对且非空。

### 1.6 后置护栏（`analysis/aicap/guardrail/inspect.py`）

按顺序执行，**任一不过即拒绝**（禁止默认值、禁止静默降级 —— `AR-15`）：

| # | 检查 | 依据 | 失败语义 |
| --- | --- | --- | --- |
| 1 | 结构（schema） | `AR-15` | 拒绝 |
| 2 | 黑名单三类（泄露 / 自曝 / 超长） | `AR-22` | 拒绝 |
| 3 | 长度（有效上限 = `min(用途上限, 任务上限)`） | `AR-23` | 拒绝 |
| 4 | 风格一致性（`style_terms` 至少命中一个） | `AR-33` | 拒绝 |

**第 2–4 关作用在 `checked_fields` 声明的字段上**，且：

- 声明的字段**不在**输出里 ⇒ 拒绝；**不是字符串** ⇒ 拒绝 —— **不得**静默跳过
  （否则新任务接入时那两关会静默失效，而调用方看不见）；
- 多个字段**逐个**检查；拒绝原因里写出是哪一个字段。

拒绝原因**必须**随日志留痕，并写明是哪一关、命中了什么。

> **§1 是任务无关的**（信封 / 注册表 / 护栏 / 收尾）；**§2–§4 是 `kind=content` 专有的**
> （内容对象 / 清单 / 核心侧装载）。接一个非内容消费方时，只需读 §1 与 §6。

---

## 2. 内容对象（`kind=content` 的 `data`）

```json
{
  "content_id": "c-<16 位十六进制>",
  "kind": "content",
  "resource": "/api/users",
  "profile_id": "site-a",
  "variant": 3,
  "body": "<html>…</html>",
  "marker": "</body>",
  "checksum": "<sha256(body) 的小写十六进制>",
  "version": 1,
  "generated_at": "2026-09-20T12:00:00+00:00",
  "generator": "template-v1"
}
```

| 字段 | 约束 |
| --- | --- |
| `content_id` | **确定性**：`c-` + `sha256(kind \| resource \| profile_id \| variant \| version \| body)` 的前 16 位十六进制。同输入必得同 ID（幂等，可复算） |
| `resource` | 请求路径的**精确值**（区分大小写）；阶段 A **不做**前缀匹配 |
| `variant` | `[0, N)` 的整数；同一 `(resource, version)` 下唯一 |
| `body` | **注入片段**（HTML 片段，不是完整文档）：它被插到改道侧响应的 `marker` **之前** —— 与 `edge-injection` 的既有语义一致（`ST-5`：复用纯改写，不新增替换语义）。长度 ≤ 65536 字节（UTF-8 计） |
| `marker` | 插入位置标记；空串 = 执行方用 `</body>`（`deception/injection` 的既有语义） |
| `checksum` | 对 `body` 的 **UTF-8 字节**取 SHA-256，小写十六进制 |
| `version` | 内容版本（清单 `version`）；轮换时递增 |
| `generator` | `template-v1` = 确定性模板生成器；阶段 B 接模型后写 `prompt-<模板版本>` |

> **阶段 A 的生成器是确定性的**：同输入（同一 resource / variant / version / profile）必得**逐字节相同**的 `body`
> —— 这是**阶段 A 的工程性质**（让生成结果可回归、清单可复现），**不是** `AR-30` 的要求：
> `AR-30` 管的是**响应路径**（同会话同资源同答案）。
>
> **接模型后这条性质不成立**（实测 `temperature=0` 也不可复现，见
> [`../background/research/ai-live-probe/README.md`](../background/research/ai-live-probe/README.md) §1.2）。
> 热路径字节一致改由三条保证：产物**冻结进清单** + `content_id` 由**内容体**算出 + **会话钉定**
> （[ADR-0026](../background/decisions/0026-cloud-model-backend.md) 决定 2）。

---

## 3. 内容清单文件（生成期产物 → 核心装载）

`python -m analysis.aicap --out <file>` 产出的 JSON（UTF-8，无 BOM）：

```json
{
  "manifest_version": 1,
  "version": 1,
  "selector": "session",
  "variants": 8,
  "generated_at": "2026-09-20T12:00:00+00:00",
  "generator": "template-v1",
  "entries": [
    {
      "resource": "/api/users",
      "profile_id": "site-a",
      "bodies": [
        {"variant_id": 0, "content_id": "c-1a2b…", "checksum": "9f3c…", "body": "…", "marker": "</body>"}
      ]
    }
  ]
}
```

| 项 | 约束 | 谁校验 |
| --- | --- | --- |
| `manifest_version` | 清单**格式**版本；当前唯一合法值 `1`。读不懂即**拒绝装载**（启动失败，不猜） | 核心 |
| `version` | **内容**版本；单调递增；轮换即 +1（进策略载荷，参与适配器的钉定判断） | 核心 / 适配器 |
| `selector` | 变体选择器；当前唯一合法值 `session` | 核心 |
| `variants` | N ≥ 1；**必须**与核心配置 `ai.content.variants` 一致，不一致即拒绝装载 | 核心 |
| `generated_at` | RFC 3339（带偏移）；仅作审计（生成时间不参与任何判定） | —— |
| `generator` | 生成器标识；与内容对象同义 | —— |
| `entries[].resource` | 精确路径；同一份清单内**禁止**重复 | 核心（重复即拒绝装载） |
| `entries[].profile_id` | 非空；画像标识 | 核心 |
| `entries[].bodies[].variant_id` | `[0, variants)`；同一资源内**禁止**重复 | 核心 |
| `entries[].bodies[].body` | ≤ 65536 字节；超限 ⇒ **丢弃该条**（不整份作废）+ warn | 核心 |
| `entries[].bodies[].checksum` | `sha256(body)`；不符 ⇒ **丢弃该条** + warn（宁可漏注入，不可注入错内容） | 核心 + 适配器 |
| 清单总字节 | ≤ 1048576（1 MiB）；超限 ⇒ **拒绝装载**（启动失败，提示做分片 —— 阶段 B） | 核心 |

**空清单合法**：`entries: []` ⇒ 核心投影出 `inject_enabled` 与**无** `content_manifest`，
适配器一律报 `inject=no_content`（不是错误）。

---

## 4. 内容在核心侧的位置（`store.ContentStore`）

- 键 = **一致性键**：`content:<profile_id>:<resource>:<variant_id>:<content_version>`
  （会话不参与键 —— 会话只决定**选哪个变体**，见 `AR-30` 的划界与 [ADR-0023](../background/decisions/0023-deception-content-injection.md) 决定 2）；
- 值 = **内容体原文**（`body` 的 UTF-8 字节）；元数据（`content_id` / `checksum` / `marker` / `variant`）留在**清单结构**里，不进内容库；
- 装载期 `Put`、投影期 `Get`（内存实现；生产实现换 PostgreSQL / Redis 时不改调用方）。

> 这是 `store.ContentStore` 的**第一个真实消费方**：它此前只有接口与内存实现，没有写入者。

---

## 5. 改键的规矩（防漂移）

改任何字段名都**必须**同时做到四处，否则门禁会红：

1. 改本文件；
2. 改生成侧 `analysis/aicap/`（内容对象 / 清单的构造）；
3. 改核心侧 `core/internal/policy/`（装载与投影）与适配器侧 `deception/proxy/`（消费）——两侧**不能共享 Go 类型**
   （`ST-3` / `TB-24`：跨平面只允许 wire format）；
4. 同步 `docs/spec/policy-payload.md`（载荷里的 `content_manifest` 是本文件 §3 的**投影**）与两侧单测。

> 依据 [`events.md`](events.md) §4 的同一教训：契约两边各写一份而不同步时，
> 测试自洽、运行时不符 —— 契约测试就是那道防线。

---

## 6. 接入一个新 `kind`（新增一个消费方）

**内核不需要改**（[ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md) 决定 1）——
接一个新消费方是三件事，全部加在**消费方自己**那一边：

| # | 做什么 | 写在哪 | 缺了会怎样 |
| --- | --- | --- | --- |
| 1 | 写 `produce` / `build` + 一个任务登记项（含 §1.3 的全部声明） | 新增 `analysis/aicap/tasks/<你的任务>.py` | 启动期断言失败（缺声明） |
| 2 | 在登记表里加一行 import + 一条登记 | `analysis/aicap/tasks/_registry.py` 的 `_registry()` | 未登记的 `kind` ⇒ `UnregisteredKind`（拒绝） |
| 3 | 决定产物去哪：实现一个 `Sink`，或直接不传（只用返回值） | 消费方自己的代码 | 不传也能拿到 `Envelope`，只是产物不落在任何地方 |

**产物契约**（`Artifact`）只有一条要求：`to_wire() -> dict`。具体字段由消费方与其下游约定 ——
内容那一路在 §2 / §3，别的消费方另定并登记进本目录（`MD-3`）。

**产物怎么跨语言**：经**文件 + 版本化契约**（§3 是内容那一路的样本），**不是**在线调用 ——
热路径永不调模型（`AR-30` / `AR-29`）。其它语言侧**禁止**重复实现护栏：只校验产物字节，不生成（`AR-33`）。

**由结构检查保证的部分**：`analysis/aicap/**` 的仓内依赖只允许 `analysis.llm` 与它自己
（`make archcheck` 的 `MD-4` 项）—— 想顺手引用别的模块，会在门禁处失败。
