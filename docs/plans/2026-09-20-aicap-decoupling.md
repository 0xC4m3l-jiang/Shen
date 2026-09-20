# 变更包：`ai-capability` 出口解耦（内核任务无关化 + 消费方插件 + 可核的独立性）

| 项 | 值 |
| --- | --- |
| 主题 | 把 AI 生成出口从「欺骗内容生成器」重构成「**受护栏的结构化生成出口**」：内核不认识「内容」，新消费方靠**登记**接入，且「独立 / 解耦」有**机器判据** |
| 日期 | 2026-09-20 |
| 状态 | 已实现 |
| 涉及模块 | `ai-capability`（[`../design/modules.md`](../design/modules.md) §1.1 第 25 行，阶段 2b/3）· `llm-components`（第 20 行）· 工具 `scripts/archcheck/` |
| 决策数 | 已答 4 项 / 待定 3 项（均不阻塞本轮） |
| 关联 | [ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md)（新）· [ADR-0024](../background/decisions/0024-ai-oss-reuse-boundary.md)（OSS 落地范围）· [ADR-0023](../background/decisions/0023-deception-content-injection.md)（本模块的由来）· 上轮变更包 [`2026-09-20-ai-oss-reuse.md`](2026-09-20-ai-oss-reuse.md) |

---

## 1. 需求与验收

**要解决什么**（一句话）：`ai-capability` 的出口今天**绑死在「欺骗内容」上** ——
`generate()` 内部直接写 `ContentStore`、`task.build` 必须返回 `ContentObject`、风格检查写死读 `body` 字段。
而用户已明确：**L4 的 AI 分析**与**后续的动态沙箱**都要用这个能力。再往下走，要么复制护栏，
要么内核被改得只认识第一种消费方。

**做完之后能做什么**：

1. 接入一个新消费方 = **加一个 `produce`/`build` + 一条登记**；内核（`service.py`）**一行不改**；
2. `generate()` 不再知道「内容库」是什么 —— 它只把产物交给一个 `Sink`（调用方决定去哪）；
3. 「内核不依赖任何消费方」与「纪律层不依赖出口」变成 **`make archcheck` 的机器判据**（不是评审承诺）；
4. 声明的上限、声明的受检字段**真的生效**（今天 `max_output` 与「受检字段」都是空的）。

**验收判据**（每条都能贴证据）：

1. **可用性**：一个**测试内定义的假 kind**（不进生产注册表）走完 `run_task()` 全程 —— 拿到 `Envelope`、
   `Sink` 收到产物、黑名单与风格按**声明的字段**生效（证据：新单测名 + 断言）；
2. **不可绕性**：`make archcheck` 新增的 `MD-4` 项通过，且**构造性反证**能证明它会拦
   （临时在 `analysis/llm/` 里加一句 `from analysis.aicap import service` ⇒ 必须报错）；
3. **零行为变化**：`kind=content` 的产物与清单**逐字节不变**（同一组输入跑前后对比，或既有 6 项端到端验收 `make ai-check` 仍 17/17）；
4. **声明即纪律**：任务声明的受检字段若不在输出里 ⇒ **拒绝**（今天会静默跳过）；`max_output` 真的参与上限计算；
5. `make gate` 通过（含 `make trace`）· `make dev` 通过。

**不做什么**：

- **不动** `docs/design/`：`AR-33` 的措辞目前只覆盖「欺骗内容生成」。放宽成「**任何**生成」是升格，
  须用户明确确认（`AGENTS.md` §3.1）—— 本轮只出 [ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md) 的**提案**部分；
- **不给动态沙箱立项**：用户未回答它是什么形态（模块 15 `honeypot-shell`？`language.md` 的「安全底座」？还是 L4 新模块），
  本轮只保证「它接进来时内核不用改」；
- **不做插件自动发现**（entry-point / 扫描目录）：只有一个消费方，加发现机制就是预置抽象；
- **不引任何第三方依赖**（OSS 落地单开一轮）· **不改** `intent`/`chain`/`strategy`/`worker`（它们今天还没接模型）。

---

## 2. 设计逻辑

**决策树**：

```text
出口解耦
├── 1 内核与插件怎么分
│   ├── 内核 = 取任务 → 渲染前置提示词 → 生成 → 后置校验 → 交 sink（不认识「内容」）
│   └── 插件 = kind=content 的 schema / 护栏档案 / produce / build（今天唯一的登记项）
├── 2 产物怎么出去（sink 缝隙）
│   └── sink 是 Protocol（`put(artifact)`）⇒ 内容库只是它的一个实现
└── 3 怎么让「解耦」可核
    └── archcheck 新增 MD-4：aicap 的仓内依赖白名单 + llm 禁止反向依赖
```

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 「这个能力」的边界 | `ai-capability` 的**生成出口**（信封 + 注册表 + 双层护栏）；内容部分下沉为「消费方插件」 | 用户答 Q1-A；`llm-components` 是纪律层，不是出口 | `aicap/service.py` · `aicap/__init__.py` 的边界说明 |
| ② | 跨语言怎么复用 | **离线产物 + 版本化契约**（沿用现有清单通路） | 用户答 Q3-A；沙箱大概率 Rust（`language.md` §1），`TB-24` 禁止 FFI ⇒ 只能走产物或 wire | `spec/ai-contract.md`；本模块只保证产物契约与消费方无关 |
| ③ | 产物怎么交出去 | `Sink` Protocol（`put(artifact) -> object`） | 现有 `ContentStore` 天然满足；有第二个实现可预见的缝隙才抽接口（最小实现 §2.2.2） | `aicap/service.py` |
| ④ | 「解耦 / 独立」怎么判 | `make archcheck` 的 `MD-4` 项（白名单式） | 本项目的口径：**无绕过路径必须是机器判据**，不能是评审承诺 | `scripts/archcheck/main.go` |
| ⑤ | `max_output` 声明了不生效怎么办 | 让它生效：有效上限 = `min(用途上限, 任务上限)` | 「声明了却不生效」比「没有声明」更坏 —— 它看起来像保护 | `guardrail/inspect.py` + `llm/blacklist.py` 的可选 `cap` |
| ⑥ | 受检字段怎么通用化 | `GuardrailProfile.checked_fields`（**必填**、非空）；声明字段缺失或非字符串 ⇒ **拒绝** | 今天 `if isinstance(body, str)` 会**静默跳过** —— 那是 fail-open 洞 | `tasks/_registry.py` + `guardrail/inspect.py` |
| ⑦ | 注册表要不要做自动发现 | **不做**，保持显式两行登记 | 只有一个消费方；加发现机制是预置抽象 | `tasks/_registry.py` 的注释与 `spec` §6 |

**仍未定**：

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | `AR-33` 的措辞是否放宽为「**任何**生成必须经唯一出口」 | `intent`/`chain`/`strategy` 接模型时的合法性 | ADR-0025 的提案段；**须用户确认后才能升格到 `design/`** |
| 2 | 动态沙箱的形态、层次、语言 | 沙箱消费方能否落地 | 用户裁定；本轮只保证内核不用改 |
| 3 | 产物契约要不要引入**跨语言**的 schema 标准（JSON Schema） | 沙箱侧能否独立校验产物 | ADR-0025 未解决 3（需第二个消费方到场才定） |

**接缝与接口**（依赖方向单向）：

```text
analysis/llm/*            纪律层（契约 / 解析 / 长度 / 黑名单 / 数据区）   ← 不依赖 aicap（MD-4 新检查）
        ▲
        │ 只有这一个方向
analysis/aicap/*          出口：service(内核) · model(模型接缝) · tasks(登记) · guardrail(双层护栏)
        ▲                                        │
        │                                        └── 任务私有的东西：content.py / tasks/content.py / __main__.py
（未来）动态沙箱 / L4 分析消费方  ← 通过 kind 登记接入，或读离线产物
```

新增/改动的接口：

| 接口 | 谁实现 | 谁消费 | 变化 |
| --- | --- | --- | --- |
| `Sink`（`put(artifact) -> object`） | 调用方（内容库、未来的规格库） | 内核 `run_task()` | **新增**（替换原来的 `store: ContentStore`），住在 `aicap/ports.py` |
| `Artifact`（`to_wire() -> dict`） | 每个 `kind` 的 `build` | 内核（`Envelope.data`）+ `Sink` | **新增**（原来是硬编码 `ContentObject`），同样在 `aicap/ports.py` |
| `run_task(task, spec, *, client, sink, identifiers, generated_at)` | 内核 | `generate()` + 测试 | **新增**（内核从注册表里解出来，于是测试能塞假 kind） |
| `GuardrailProfile.checked_fields` | 每个 `kind` | 后置护栏第 2/4 关 | **新增必填字段** |
| `Blacklist.check(..., cap=None)` | 纪律层 | 后置护栏 | **新增可选参数**（默认行为不变） |

**数据流**（含失败路径）：

```text
generate(spec, sink=…)                run_task(task, spec, …)
  └─ task_for(spec.kind)                ├─ task.assert_declared()     缺声明 ⇒ 抛异常（启动期就该发现）
     未登记 ⇒ 抛 UnregisteredKind       ├─ 前置护栏：三段式提示词（数据区，AR-31）⇒ 渲染失败即抛（AR-24）
                                        ├─ produce(prompt, spec, client)  未配置模型 ⇒ Unavailable（AR-15，不降级）
                                        ├─ 后置护栏 4 关：
                                        │   ① schema（AR-15）
                                        │   ② 黑名单三类（AR-22）—— 逐**声明字段**，有效上限 = min(用途, 任务)
                                        │   ③ 长度（AR-23）—— 同上
                                        │   ④ 风格术语（AR-33）—— 逐**声明字段**
                                        │   声明字段缺失 / 非字符串 ⇒ 拒绝（原来会静默跳过）
                                        │   任一不过 ⇒ reject(带原因)，**不建产物、不调 sink**
                                        ├─ build(checked, spec, generated_at) → artifact
                                        ├─ sink.put(artifact)          只有过了护栏才会到这里
                                        └─ accept(artifact.to_wire())
```

---

## 3. 文档对应（追溯矩阵）

| 规则 ID | 模块文档 / 契约 | 代码 | 测试 / 证据 | 验证命令 |
| --- | --- | --- | --- | --- |
| `AR-33` | [`docs/modules/ai-capability.md`](../modules/ai-capability.md) §1 · [`spec/ai-contract.md`](../spec/ai-contract.md) §1.3 | `aicap/service.py`（唯一出口）· `aicap/tasks/_registry.py`（登记表） | 既有 `test_aicap_guardrail.py::test_*未登记*` + 新的假 kind 用例 | `make archcheck`（AR-33 项）· `make pytest` |
| `AR-15` | [`spec/ai-contract.md`](../spec/ai-contract.md) §1.3 / §1.6 | `aicap/guardrail/inspect.py`（第 1 关） | 缺声明字段 ⇒ 拒绝的新用例 | `make pytest` |
| `AR-22` / `AR-23` | [`spec/ai-contract.md`](../spec/ai-contract.md) §1.3 / §1.4 / §1.6 | `aicap/guardrail/inspect.py` · `llm/blacklist.py`（`cap`） | 黑名单三类 + 任务上限生效的用例 | `make pytest` |
| `AR-24` | [`spec/ai-contract.md`](../spec/ai-contract.md) §1.5 · [`modules/ai-capability.md`](../modules/ai-capability.md) §6 | `aicap/guardrail/prompts.py` · `aicap/resources/prompts/` | 既有启动期断言用例 | `make pytest` |
| `AR-30` | [`modules/ai-capability.md`](../modules/ai-capability.md) §4 | `aicap/tasks/content.py`（确定性生成器） | 既有 `test_aicap_content.py`（逐字节可复现） + **端到端 17/17** | `make ai-check` |
| `AR-31` / `AR-32` | [`modules/llm-components.md`](../modules/llm-components.md) §4 | `llm/untrusted.py` · `llm/client.py` | 既有注入样本回归 | `make pytest` |
| `MD-4`（新检查项） | [`scripts/archcheck/README.md`](../../scripts/archcheck/README.md) 检查项表 · `design/modules.md` §3 | `scripts/archcheck/main.go`（新增 `checkPythonDependencyDirection`） | 构造性反证（在 `llm/` 里加一行反向 import ⇒ 必拦） | `make archcheck` |
| `MD-3` / `MD-5` | `spec/ai-contract.md`（跨模块契约唯一处） | —— | `make trace`（文档 ↔ 代码 ↔ 单测） | `make trace` |
| `DEV-1` / `DEV-2` | 本文件 · [`docs/log.md`](../log.md) | —— | `make trace` 的形状检查 | `make trace` |

> 本轮**没有**在 `docs/design/` 加规则：`AR-33` 的措辞仍只覆盖「欺骗内容生成」，代码的通用化**不与之冲突**
> （唯一出口没变、护栏没变、只多支持了几种任务）。把它写成「任何生成」是**升格**，见 ADR-0025 提案段。

---

## 4. 代码实现

**文件清单**：

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `analysis/aicap/ports.py` | **新增** | 产出缝隙（`Artifact` / `Sink`）单独成文件：它**不 import 本包任何模块**，于是消费方接入时不必把内核依赖一起拖进来；两个 Protocol 也因此不会有循环 |
| `analysis/aicap/service.py` | 修改 | 引入 `run_task()`；**不再 import `content.py`**（内核不认识插件）；`store=` → `sink=` |
| `analysis/aicap/tasks/_registry.py` | 修改 | `GuardrailProfile.checked_fields`（必填）+ 登记/装配的类型收紧 |
| `analysis/aicap/guardrail/inspect.py` | 修改 | 4 关按**声明字段**执行；声明字段缺失/非字符串 ⇒ 拒绝；有效上限 = min(用途, 任务) |
| `analysis/aicap/__init__.py` | 修改 | 把「内核 / 插件」边界写成模块地图（读者第一眼要知道哪几个文件是内核） |
| `analysis/aicap/tasks/content.py` | 修改 | `CONTENT_PROFILE` 补 `checked_fields=("body",)`（**行为不变**） |
| `analysis/aicap/__main__.py` | 修改 | `store=` → `sink=`（一处） |
| `analysis/llm/blacklist.py` | 修改 | `check(..., cap=None)`：让任务声明的上限参与判定（默认行为不变） |
| `analysis/tests/test_aicap_guardrail.py` | 修改 | 新签名 + 新的拒绝语义 + **假 kind 走完整内核**（解耦的可执行证明） |
| `analysis/tests/test_aicap_content.py` | 修改 | `store=` → `sink=`（三处）；其余不动（它是 `kind=content` 的回归） |
| `scripts/archcheck/main.go` | 修改 | 新增检查：`analysis/aicap/**` 的仓内依赖白名单 + `analysis/llm/**` 禁止反向依赖 |
| `scripts/archcheck/README.md` | 修改 | 检查项表 + 依据列补 `MD-4` |
| `docs/spec/ai-contract.md` | 修改 | 修 `purpose` 漂移 · `max_output` 入表 · 新增「接入一个新 kind 的步骤」· 标注内核 vs 插件 |
| `docs/modules/ai-capability.md` | 修改 | §1 职责（内核 vs 插件）· §3 依赖 · §4 规则 · §7 测试 · §8 未决 · §9 变更记录 |
| `docs/background/decisions/0025-generic-guardrailed-outlet.md` | 新增 | 决策记录（候选 / 理由 / 后果 / 失效条件）+ `AR-33` 放宽的**提案** |
| `docs/background/decisions/README.md` | 修改 | ADR 索引登记一行 |
| `docs/plans/2026-09-20-aicap-decoupling.md` | 新增 | 本文件 |
| `docs/log.md` | 修改 | 本轮变更日志条目 |

**关键类型与函数**（哪些是导出契约、哪些是内部细节）：

- **导出契约**（跨模块）：`service.generate` · `service.TaskSpec` · `service.Envelope`（复用 `llm.envelope`）· `service.run_task`（内核，测试与未来的批量调用方用）；
- **缝隙**：`service.Artifact` · `service.Sink`（Protocol，结构性满足，不强制继承）；
- **每个 kind 必须声明的**：`schema` · `guardrail_profile`（含 `checked_fields`）· `limits`（`purpose` + `max_output`）· `produce` · `build` · `generator`；
- **内部细节**：`guardrail/inspect.py` 的四关顺序 · `tasks/_registry.py` 的 `_REGISTRY` 常量 · `model.py` 的 `resolve()`。

**必须遵守的上位约束**：`AR-33`（唯一出口 + 启动期断言）· `AR-15`（结构由独立代码层校验，失败即抛/拒）·
`AR-2`/`AR-5`（判定只在核心一处 —— 本模块**不做判定**）· `AR-30`（`kind=content` 必须确定性）·
`AR-31`/`AR-32`（数据区 / 无执行面）· `MD-3`（契约放 `spec/`）· `MD-4`（依赖方向单向）· `MD-5`（不重复定义类型）·
`TB-14`（禁止未处理的错误返回值）· `TB-15`（Python 过 ruff + pytest）。

---

## 5. 测试与场景

| # | 场景 | 输入 / 前置 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 1 | **假 kind 走完整内核**（解耦的可执行证明） | 测试内定义 `Task(kind="demo", …)`，`checked_fields=("text",)`，`build` 返回简单 Artifact | `run_task` 返回 `accepted=True`；`Sink` 收到产物；产物 `to_wire()` 进 `Envelope.data` | ✅ | `test_run_task_is_task_agnostic` |
| 2 | 缺 `checked_fields` 声明 ⇒ 启动期断言失败 | `GuardrailProfile(name, prompt, style_terms)`（不传 checked_fields） | 构造即 `TypeError`；`assert_declared()` 亦失败 | ✅ | `test_profile_without_checked_fields_fails` |
| 3 | **声明的受检字段不在输出里 ⇒ 拒绝**（修 fail-open） | schema 不含该字段 / produce 不给它 | `rejected_reason` 含「声明的受检字段」 | ✅ | `test_declared_field_missing_is_rejected` |
| 4 | 受检字段非字符串 ⇒ 拒绝 | produce 给 `text=123` | 拒绝 | ✅ | 同上（参数化） |
| 5 | 任务上限生效（`max_output` 不是摆设） | 任务 `max_output=10`，输出 20 字符 | 黑名单判 `overlength` ⇒ 拒绝 | ✅ | `test_task_output_cap_is_enforced` |
| 6 | 多个受检字段都被检查 | `checked_fields=("a","b")`，`b` 命中自曝黑名单 | 拒绝，且原因指向 `b` | ✅ | `test_all_checked_fields_are_scanned` |
| 7 | `kind=content` **零行为变化** | 同一组输入（资源 × 变体） | 产物与清单逐字节不变；`make ai-check` 仍 17/17 | ✅ | `make ai-check` + 既有 `test_aicap_content.py` |
| 8 | 未登记 kind 仍被拒 | `generate(TaskSpec(kind="nope"))` | 抛 `UnregisteredKind` | ✅ | 既有用例（回归） |
| 9 | `MD-4` 结构检查通过 | 现状代码 | 通过 | ✅ | `make archcheck` |
| 10 | `MD-4` 构造性反证 | 临时在 `analysis/llm/envelope.py` 加 `from analysis.aicap import service` | **必须报错** | ✅ | §6 反证输出 |

**没有覆盖的**：

- **没有第二个真实消费方**（不加未要求的层）。场景 1 的假 kind 只活在测试里；
- **没有跨语言验证**：产物契约的「沙箱侧能否独立校验」要等消费方到场（ADR-0025 未解决 3）；
- **没做性能基准**：本轮是结构与语义，没有「更快」的声称。

---

## 6. 验证证据

```console
$ make gate
✓ Python 格式（ruff format）· All checks passed!（ruff check）
架构检查通过。
  顶层目录 · 跨平面依赖 · 核心内部可见性 · store 唯一 I/O 出口
  模块清单一致性 · CGO 与本地库 · 语言层数 · 护栏为唯一出口（AR-33）
  AI 能力独立性（MD-4：aicap / llm 的依赖白名单）
追溯检查通过。
泄漏检查通过。
许可审计通过：没有传染性或限制性许可。
79 passed in 0.18s            ← 本轮前是 71（新增 8 例）
✓ L4 单测（pytest）
门禁通过。

$ make ai-check
—— 验收结果 ——
  ✓ 改道侧与「未注入基线」逐字节一致：9eef5471e0ca88c0 vs 9eef5471e0ca88c0
  ✓ 业务侧与业务基线逐字节一致：3e535d75f9418bcc vs 3e535d75f9418bcc
  ✓ 同会话同资源三次 → 响应 sha256 相同：sha256=744905218c9cee91，三次长度 [374, 374, 374]
  ✓ 16 个会话落在 ≥4 个不同变体上：命中 8 个变体（N=8）
  ✓ DAG 出现「内容注入」跳且三段文字齐全
  ✓ 关闭后：改道侧响应回到原样：9eef5471e0ca88c0 == 9eef5471e0ca88c0
✅ 全部通过（17 项）
```

**关键数字**：

| 指标 | 值 |
| --- | --- |
| 新增单测 | **8** 例（71 → **79** 全绿） |
| `kind=content` 行为变化 | **0**（`make ai-check` 17/17；三组基线 **sha256 逐个相等**） |
| 新增门禁检查项 | **1**（`MD-4`：Python 侧依赖白名单） |
| 新增文件 | **1**（`analysis/aicap/ports.py`，30 行） |
| 内核不再 import | **`content.py`**（插件）—— 解耦的核心指标 |
| 修的 fail-open | **2**（受检字段静默跳过 · `max_output` 声明不生效） |

### 6.1 构造性反证：`MD-4` 真的会拦

在 `analysis/llm/envelope.py` 末尾临时加一行反向依赖（马上还原）：

```console
$ printf '\nfrom analysis.aicap import service  # 反证用，马上删\n' >> analysis/llm/envelope.py
$ go run ./scripts/archcheck
  ✗ MD-4   禁止依赖 analysis.aicap —— analysis/llm 只允许依赖：llm（AI 能力必须独立、依赖方向单向）
        analysis/llm/envelope.py
```

还原后 `git diff --stat analysis/llm/envelope.py` 为空，`make archcheck` 恢复通过。

### 6.2 解耦的可执行证明（新单测）

```console
$ analysis/.venv/bin/pytest tests/test_aicap_guardrail.py -k run_task -q
........
$ analysis/.venv/bin/pytest -k "run_task or checked_fields" -q
9 passed
```

假 kind（`kind="demo"`，字段叫 `text`、产物叫 `_DemoArtifact`、出口叫 `_MemorySink`）
**只在测试里存在**，却完整走完了内核：前置护栏（数据区标记齐全）→ 生成 → 后置四关 → 产物进 sink → 进 `Envelope`。
内核如果还绑在「内容」上，这一组会全红。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | `AR-33` 措辞仍只覆盖「欺骗内容生成」 | `intent`/`chain`/`strategy` 接模型时的合法性未定 | ADR-0025 提案段；**须用户确认**才能升格进 `design/` |
| 2 | 动态沙箱未立项（形态 / 层次 / 语言未知） | 沙箱消费方落地时间 | 用户裁定 |
| 3 | 产物契约未用跨语言标准（JSON Schema） | 沙箱侧要自己写校验器 | ADR-0025 未解决 3 |
| 4 | 注册表仍是显式两行登记（无自动发现） | 接入新 kind 要改一个文件 | 有意为之；消费方 ≥3 个时再评估 |
| 5 | 三项 OSS 复用候选仍未落地 | 自研维护成本 | ADR-0024 决定 3（单开一轮，先做行为对齐测试） |
| 6 | `ports.py` 的两个 Protocol 是**在当前只有一个消费方时**抽出来的缝 | 若第二个消费方一直不来，它们就是预置抽象 | 已在 ADR-0025 决定 2 里写了例外理由；**失效条件 1**：2026-12-20 前第二个消费方仍未出现 ⇒ 重新审视这两个缝 |
| 7 | `analysis/aicap/**` 里其余文件的 docstring 链接深度仍是旧的（`../../../docs/`，超出仓库根） | 人读文档时点击不到 | 本轮只修改到的文件；其余属重构性清理，另开一轮 |

---

## 7.1 审视记录（L 档必填）

对着本轮 diff 逐项核（依据：全局技能 `audit` 的四道删除门槛）。

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `spec/ai-contract.md` §1.4 把 `purpose` 写在 `GuardrailProfile` 里，代码里它在 `TaskLimits` | 文档与实现不一致（漂移） | `grep -n purpose docs/spec/ai-contract.md` vs `analysis/aicap/tasks/_registry.py` | 修**文档**（代码的划分是对的：长度用途属输出纪律，不属画像） | §1.4 删 `purpose` 行并写明它在哪 |
| 2 | `TaskLimits.max_output` **声明了却从不生效**（真上限只来自 `PURPOSE_LIMITS`） | 空声明（fail-open） | `grep -rn max_output analysis/` → 只有定义与「必须为正」断言 | 修**实现**：有效上限 = `min(用途上限, 任务上限)` | 内容任务行为不变（两者都是 65536）；新单测证明它能拦 |
| 3 | `guardrail/inspect.py` 用 `if isinstance(body, str)` —— 字段缺失/类型不对就**静默跳过**两关 | 空声明（fail-open） | 旧 `inspect.py` 的 `body = checked.get("body")` | 修**实现**：声明的受检字段缺失或非字符串 ⇒ **拒绝** | 新单测 `test_run_task_rejects_missing_checked_field` 等的 `assert` 即证据 |
| 4 | `service.py`（内核）直接 `from .content import ContentStore` —— 内核认识插件 | 耦合 | `git show HEAD:analysis/aicap/service.py` 的 import 块 | 抽 `Sink` 缝 + `run_task()`；**删掉**那行 import | 内核不再 import 任何插件（这也是「解耦」的唯一硬指标） |
| 5 | `aicap/__init__.py` 只列了模块名，没告诉读者**哪些是内核、哪些是插件** | 导航缺失 | 旧 `__init__.py`（21 行） | 补两张表（内核 / 插件）+ 一条「把插件删掉内核仍可用」的判据 | 已补 |
| 6 | 分析器反复回放一条**引用旧签名**的诊断（`put(item: ContentObject) -> str`），而磁盘上只有一个 `ContentStore` 且签名已是 `put(artifact: Artifact) -> str` | 工具缓存陈旧（假阳性） | ① 全仓 `grep -rn "class ContentStore"` → 仅一处；② `inspect.signature(ContentStore.put)` → `(self, artifact: 'Artifact') -> 'str'`；③ 刷新后的 LSP 探针 → 0 诊断 | 不改逻辑；顺手把 `ContentStore` 改成**显式继承** `Sink`（安全关键缝不该让类型检查器去猜） | 诊断消失（再探针 0 错） |
| 7 | `analysis/aicap/**` 的 docstring 链接深度不一致（`../../../docs/` 与 `../../docs/` 混用，前者超出仓库根） | 悬空引用（轻） | `grep -rn "\.\./\.\./\.\./docs" analysis/aicap/` | 只修本轮改到的文件（`service.py` · `content.py` · `inspect.py` · `ports.py` 用正确深度） | 其余文件（`content.py` 顶部等）不动 —— 属另一轮；已记 §7 遗产 |
| 8 | `analysis/` 侧**没有**依赖方向检查（只有 `AR-33` 的模型客户端检查），于是「解耦」只能靠评审承诺 | 缺机器判据 | `grep -n "checkPythonDependencyDirection" scripts/archcheck/main.go` → 本轮前不存在 | 新增 `MD-4` 检查（白名单式 + 扫不到文件即报错） + 构造性反证 | §6.1 |
| 9 | 新增文件 `docs/background/decisions/0025-*.md` 是否已被索引？ | 孤儿文档 | `grep -n 0025 docs/background/decisions/README.md` | 已登记 | 通过 |
| 10 | 本轮有没有改到 `docs/design/`？（`P-1` / `AGENTS.md` §3.1） | 越界核查 | `git diff --name-only docs/design/` → 空 | 无动作（`AR-33` 的放宽只是**提案**） | 未越界 |

> **排除项**：`docs/background/` · `docs/plans/` · `docs/log.md` · `docs/kb/` 是历史记录类文件，不在审视范围内。
> 第 6 条是唯一「不改代码只记录」的项 —— 它的价值是让下一轮不再为同一条假诊断考古。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-20 | 首版：内核任务无关化 + sink 缝隙 + 声明化受检字段 + `MD-4` 独立性检查 | 用户答 Q1-A / Q3-A / Q4-A（2026-09-20）+ [ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md) |
