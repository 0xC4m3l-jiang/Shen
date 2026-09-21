# 模块：`llm-components`

| 项 | 内容 |
| --- | --- |
| 模块名 | `llm-components` |
| 所属层 | `L4`（依据 MD-1） |
| 实现语言 | `Python`（依据 [`../design/language.md`](../design/language.md) §1 与 `TB-2`） |
| 负责人 | — |
| 状态 | ✅ **已实现（框架 + 确定性部分）**（阶段 3，Python）：契约（`AR-15`/`AR-16`/`AR-17`）· 长度（`AR-18`/`AR-23`）· 超时（`AR-19`…`AR-21`）· 内容（`AR-22`/`AR-24`）· 注入防护（`AR-31`/`AR-32`） |
| 最后更新 | 2026-09-20 |

---

## 1. 职责

**做什么**：

- **契约纪律**：LLM 输出的独立校验层（`AR-15`）、统一信封 `{accepted, data}`（`AR-16`）、三段式 JSON 容错提取（`AR-17`）、
  列表硬截断（`AR-18`）；
- **超时纪律**：双阶段收尾（`AR-19` / `AR-20` / `AR-21`）；
- **内容纪律**：生成内容黑名单（`AR-22`）、分用途长度上限（`AR-23`）、话术与提示词资源化（`AR-24`）；
  已登记用途：`session_response` 2000 · `conclusion` 8000 · `intermediate` 20000 · `deception_content` 65536
  （后者供 `ai-capability` 的欺骗内容使用，见 [ADR-0023](../background/decisions/0023-deception-content-injection.md)）；
- **间接注入防护**（[ADR-0015](../background/decisions/0015-indirect-prompt-injection.md)）：
  攻击者可控内容**必须**以结构化数据传入（`AR-31`）；分析 LLM **禁止**持有执行能力（`AR-32`）；
- **高保真内容预生成**：离线/近线生成诱饵内容（假用户 / 假订单 / 假日志），落库供 `responder` 消费。

**明确不做什么**：

- **不持有执行能力** —— 不下发指令、不生成载荷、不调用外部系统（`SB-1` / `SB-2` / `AR-32`）；
- **不做判定**（`AR-2`）与**不做决策**（`MD-12`）；
- **不直接回写响应** —— 分析结论只经 `policy` 间接生效，**禁止**影响对攻击者的响应（`AR-32`，防注入回流）；
- **不做协议仿真** —— 那是 L2 蜜罐。

## 2. 输入 / 输出契约

跨模块与对外的契约不在这里（`MD-3`），只引用：

| 方向 | 契约 | 定义位置 |
| --- | --- | --- |
| 输入 | **结构化数据**（攻击者可控内容置于数据区，`AR-31`） | `analysis/llm/` 的输入契约 |
| 输入 | 任务上下文（会话 ID 一等字段，`AR-25`） | 同上 |
| 输出 | `{"accepted": bool, "data": {...}}`（`AR-16`） | 同上 |
| 输出 | 预生成的诱饵内容 | `store` 的内容缓存实体 |

本模块内部数据结构：`Envelope`（统一信封）、`Contract`（校验层）、`Template`（提示词资源）。

## 3. 依赖

| 允许依赖 | 原因 |
| --- | --- |
| LLM 运行时（vLLM / Transformers 等） | 推理 |
| 标准库（`json` / `re`） | 容错解析 |

| 禁止依赖 | 原因 |
| --- | --- |
| **任何执行能力**（命令执行 / 网络外呼 / 载荷生成） | `SB-1` / `SB-2` / `AR-32` |
| `judge` / `director` 的判定与决策逻辑 | `AR-2` / `MD-12`；依赖方向单向 |
| 判定缓存 / 响应路径 | 分析结论**禁止**直接回写响应（`AR-32`） |

**复用候选（尚未引入 —— 不是已批准的依赖）**：本模块的 6 项手写能力已逐项对过开源实现，
详见 [`../background/research/ai-oss-reuse.md`](../background/research/ai-oss-reuse.md) §3.1。
判据与边界在 [ADR-0024](../background/decisions/0024-ai-oss-reuse-boundary.md)：
**只看默认行为是否 fail-closed，不看功能表**。据此：

| 能力 | 候选 | 为什么现在**不**引 |
| --- | --- | --- |
| `AR-17` 三段式 JSON 容错提取（`llm/extract.py`） | `json_repair`（MIT，5.1k★） | 它有 `strict=True`（遇结构问题抛错，与 `AR-15`/`AR-21` 同向）⇒ **唯一「默认行为就能对齐」的候选**；但要先做行为对齐测试（markdown 围栏样本），见 ADR-0024 失效条件 1 |
| `AR-15` 契约校验（`llm/contract.py`） | `pydantic` / `jsonschema` / `msgspec` | `pydantic` 默认 lax 模式**强制转换**（`'123'` → `123`），与 `AR-15`「禁止修正、禁止默认值」正面冲突 |
| `AR-22` 黑名单三类（`llm/blacklist.py`） | Presidio（MIT）· detect-secrets（Apache-2.0） | 二者都只是**识别器**（要额外模型与依赖面）；`AR-22` 的处置语义仍须在本模块做 ⇒ 阶段 B 再引 |
| `AR-24` 提示词资源化（`llm/prompts.py`） | `jinja2`（BSD-3） | 模板里可写表达式与过滤器，与 `AR-31`（指令区 / 数据区严格分离）的精神相反 |
| `AR-19`…`AR-21` 双阶段收尾（`llm/twophase.py`） | `tenacity`（Apache-2.0） | 它解决的是**重试**；本项目要的是「超时后**复用同一会话**收尾，两阶段皆失败则整体作废」—— 语义相反 |
| `AR-31` / `AR-32` 注入防护（`untrusted.py` / `client.py`） | **无对口实现** | 见过的「提示注入检测器」都在做**分类**（像不像攻击），而 `AR-31` 要的是**结构断言**（数据有没有只在数据区）；本项的价值是「**不用**执行能力」，不是「用了谁」 |

> 规则 `TB-24`：跨语言**禁止** FFI / CGO / 共享内存；**必须**经 wire format（gRPC / Protobuf）通信。

## 4. 关键规则

| 规则 | 与本模块的关系 |
| --- | --- |
| `AR-15` | 输出结构由**独立代码层**校验；失败**必须**抛异常，**禁止**默认值 / 静默降级 |
| `AR-16` | 输出统一为 `{accepted, data}`；`accepted=false` 走独立分支 |
| `AR-17` | JSON 三段式容错提取，第三段**必须**有扫描上限 |
| `AR-19` / `AR-20` / `AR-21` | 双阶段超时收尾；两阶段皆失败**禁止**写中间数据 |
| `AR-22` | 内容黑名单：泄露类 / 自曝类 / 超长类 |
| `AR-24` | 话术与提示词**必须**仓库内资源，启动时校验 |
| `AR-25` | 会话 ID 一等字段，三阶段一致 |
| `AR-31` | **数据与指令分离**：攻击者内容进数据区，**禁止**进指令区；原始观测不得为净化而丢弃 |
| `AR-32` | **最小权限**：不持有执行能力；结论**禁止**直接回写响应 |
| `AR-33` | **唯一出口**：**任何** LLM 生成（不只欺骗内容）**必须**经 `ai-capability` 的 `generate()` 并过护栏；本层不自己实现出口，也不 import 模型客户端（口径 2026-09-20 由「欺骗内容」放宽，[ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md)） |
| `TB-2` | 离线 AI 能力**必须**用 Python |
| `AR-14` | 触发 L4 **必须**经态势去重，**禁止**随事件量线性触发 |

## 5. 状态与生命周期

| 状态 | 存在哪里 | 生命周期 | 多副本一致性 |
| --- | --- | --- | --- |
| 调用状态 | 无（每次调用独立，`AR-9`） | —— | 天然一致 |
| 分析结论 | `store`（遥测 / 攻击链实体） | 按保留策略 | 强一致（经 `store`） |
| 预生成内容 | `store` 的内容缓存实体 | 按保留策略 | 强一致 |

> 规则 `AR-9`：核心无状态多副本 —— 本模块是独立进程（L4），状态同样外置。

## 6. 失败模式与降级

| 失败情形 | 行为 | 是否满足 `NI-1` | 依据 |
| --- | --- | --- | --- |
| 输出契约校验失败 | **必须**抛异常，不返回默认值 | ✅（不影响业务路径） | `AR-15` |
| 两阶段收尾均失败 | **必须**整体作废，释放已占用的标记 | ✅ | `AR-21` |
| 内容黑名单命中 | 丢弃该内容并记录 | ✅ | `AR-22` |
| 检测到疑似注入 | 按数据处理（不服从数据区里的"指令"），记录为可疑信号 | ✅ | `AR-31` |
| L4 整体不可用 | 核心照常工作（分析是近线/离线，**不在业务路径上**） | ✅ | `NI-1` |

## 7. 测试

| 类型 | 覆盖什么 | 位置 |
| --- | --- | --- |
| 单元 | 契约校验（喂坏输出**必须**失败） | `analysis/tests/test_llm_contract.py` |
| 单元 | JSON 三段式解析 + 超长输出（含扫描上限） | 同上（`test_ar17_*`） |
| 单元 | 黑名单三类各一例 | `analysis/tests/test_llm_discipline.py`（`test_ar22_*`） |
| **注入样本回归** | 攻击者内容含指令时，**结论不得改变**（`AR-31`） | 同上（`test_ar31_*`，与 `MD-7` 正负样本同构） |
| 超时注入 | 双阶段收尾 + 整体作废 | 同上（`test_ar19_*` / `test_ar21_*`） |
| **跨语言契约** | 真实事件载荷可被本层解析（键名漂移即红） | `analysis/tests/test_event_contract.py`（夹具由 Go 结构体生成，见 [`../spec/events.md`](../spec/events.md)） |
| 运行时链路 | 近线 worker 读事件 → 去重 → 结论上报 | `analysis/tests/test_worker.py` + `make analysis`（[ADR-0022](../background/decisions/0022-l4-near-line-worker.md)） |
| 分支穷尽性 | `accepted` 真假两分支 | `analysis/llm/envelope.py` 的 `__post_init__` + 上述用例 |

## 8. 未决项

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | 多源交叉校验的具体做法（多数表决？独立模型？规则复核？） | 结论可信度 | [ADR-0015](../background/decisions/0015-indirect-prompt-injection.md) 未解决 |
| 2 | 注入样本测试集的构建 | 回归保障 | 同上 |
| 3 | `strategy` 生成策略时的注入面 | 策略链路安全 | [`strategy.md`](strategy.md)（已实现：只出数据、经 `policy` 下发） |
| 4 | 预生成内容的触发时机与规模 | LLM 成本 | [ADR-0014](../background/decisions/0014-generative-deceptive-response.md) |
| 5 | 6 项手写能力能否换成开源实现（`json_repair` 等） | 本模块的自研维护成本 | 调研已做：[`../background/research/ai-oss-reuse.md`](../background/research/ai-oss-reuse.md)；判据与失效条件在 [ADR-0024](../background/decisions/0024-ai-oss-reuse-boundary.md)。**落地要等行为对齐测试**（本轮不改代码） |
| 6 | `AR-22` 泄露类拟改用 Presidio 的识别器（本模块只保留处置语义） | 泄露类覆盖度 | 同上 §3.2；阶段 B，需先量离线模型体积 |

## 9. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 创建（设计）：LLM 契约纪律 + 间接注入防护 + 内容预生成 | [ADR-0015](../background/decisions/0015-indirect-prompt-injection.md) · [`../design/architecture.md`](../design/architecture.md) §5（`AR-15`…`AR-27`） |
| 2026-09-20 | 新增长度用途 `deception_content`（65536），供 `ai-capability` 的欺骗内容使用；本层增加第二个消费者（仍**只能**经 `ai-capability` 的护栏出口，`AR-33`） | [`../plans/2026-09-20-ai-capability-guardrail.md`](../plans/2026-09-20-ai-capability-guardrail.md) · [ADR-0023](../background/decisions/0023-deception-content-injection.md) |
| 2026-09-20 | §3 增「复用候选」表（逐项写明为什么不引）· §8 增未决项 5/6 · 删 §1 中重复的一段「已登记用途」（同一段写了两次） | [`../plans/2026-09-20-ai-oss-reuse.md`](../plans/2026-09-20-ai-oss-reuse.md) · [ADR-0024](../background/decisions/0024-ai-oss-reuse-boundary.md) |
