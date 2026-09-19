# 变更包 · 2026-09-18 · 补响应层设计（生成式欺骗响应 + 间接注入防护）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 补「改道之后返回什么」与「分析链路怎么不被反注入」：`responder`（生成式欺骗响应）+ `llm-components`（间接注入防护）+ 2 条 ADR + 3 条规则 |
| 日期 | 2026-09-18 |
| 状态 | 已实现（**设计轮**，不含代码） |
| 涉及模块 | `responder`（设计，2b）· `llm-components`（设计，3） |
| 决策数 | 已答 2 项 / 待定 6 项（见 §7） |
| 关联 | [ADR-0014](../background/decisions/0014-generative-deceptive-response.md) · [ADR-0015](../background/decisions/0015-indirect-prompt-injection.md) · [`../log.md`](../log.md) 同日条目 |

## 1. 需求与验收

**要解决什么**：两轮之前补齐了诱饵面与判别层，但**「返回什么」**与**「分析链路的安全」**仍缺：
① 改道后返回静态内容会被众包识破，返回 LLM 随机内容会破坏「同资源同答案」；
② 把攻击者输入喂 LLM 分析 = 主动开一条间接提示注入通道。

**做完之后，用户能做什么 / 看到什么**：响应生成有「快慢两路 + 一致性不变量」的设计；分析链路有「数据/指令分离 + 最小权限」的防护。

**验收判据**：

1. `responder` 有九章模块文档，含一致性不变量（`AR-30`）。
2. `llm-components` 有九章模块文档，含间接注入防护（`AR-31` / `AR-32`）。
3. 2 条 ADR 记录候选、理由、后果、**失效条件**。
4. `make gate` 全绿。

**不做什么**：

- 不写代码（设计轮）。
- 不给 `responder` 加 LLM 调用（核心禁止外呼，`MD-6`）。
- 不改 `config.example.yaml`（响应段实现时定）。

## 2. 设计逻辑

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地 |
| --- | --- | --- | --- | --- |
| ① | 响应怎么生成 | **快慢两路**：核心确定性模板 + L4 预生成落库；**一致性不变量** | 热路径不放 LLM（`AR-29`）；LLM 随机性破坏一致性；调用量与请求量解耦（`AR-14`） | [ADR-0014](../background/decisions/0014-generative-deceptive-response.md) |
| ② | 分析链路怎么防反注入 | **数据/指令分离**（结构性）+ 输出契约 + 最小权限 + 单向通道 | 模式净化不可靠；结构性分离不依赖攻击者长什么样 | [ADR-0015](../background/decisions/0015-indirect-prompt-injection.md) |

**关键约束**：

> **`AR-30`（一致性不变量）**：任何伪造响应**必须**按 `(会话, 资源)` 命中同一内容。
> 理由：Agent 必然重复请求同一资源；同 URL 两个答案 = 一眼假（`D0` + [deception-engine-design](../research/deception-engine-design.md) §2.4）。
> **LLM 的随机性恰恰最容易破坏这条** —— 所以 LLM 被移出响应路径。

**仍未定**（不阻塞本轮）：

| # | 未决 | 去向 |
| --- | --- | --- |
| 1 | 一致性键的粒度（含 query / 方法？） | [ADR-0014](../background/decisions/0014-generative-deceptive-response.md) |
| 2 | 预生成的触发时机 | 同上 |
| 3 | 与 `severity` 的交互 | 同上 |
| 4 | 多源交叉校验的具体做法 | [ADR-0015](../background/decisions/0015-indirect-prompt-injection.md) |
| 5 | 注入样本测试集的构建 | 同上 |
| 6 | `strategy` 链路的注入面 | `strategy.md`（待建） |

## 3. 文档对应（追溯矩阵）

| 决策 / 规则 | 模块文档 | 契约 | 验证命令 |
| --- | --- | --- | --- |
| [ADR-0014](../background/decisions/0014-generative-deceptive-response.md) · `AR-30` | [`../modules/responder.md`](../modules/responder.md) §1/§4 | `store` 内容缓存实体 | `make trace` |
| [ADR-0015](../background/decisions/0015-indirect-prompt-injection.md) · `AR-31` / `AR-32` | [`../modules/llm-components.md`](../modules/llm-components.md) §1/§4 | `analysis/llm/` 输入契约 | `make trace` |
| `AR-15`…`AR-27`（既有） | `llm-components.md` §4 | —— | `make trace`（ID 存在性） |
| `INT-8` · `AR-22` | `responder.md` §4 | —— | `make trace` |

## 4. 文档实现（本轮文件清单）

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `docs/background/decisions/0014-generative-deceptive-response.md` | 新增 | 响应生成机制 + 一致性不变量 |
| `docs/background/decisions/0015-indirect-prompt-injection.md` | 新增 | 间接注入防护 |
| `docs/background/decisions/README.md` | 改 | 登记 0014 / 0015 |
| `docs/design/architecture.md` | 改 | §5 新增 `AR-30` / `AR-31` / `AR-32` |
| `docs/modules/responder.md` | 新增 | 响应生成模块（九章） |
| `docs/modules/llm-components.md` | 新增 | LLM 组件模块（九章，含注入防护） |
| `docs/progress.md` | 改 | 两模块文档状态 → ✅（设计） |

## 5. 场景（设计覆盖度）

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | 一致性不变量 | 同 `(会话,资源)` 同答案，有规则 ID | ✅ | `AR-30` |
| 2 | 核心不调 LLM | `responder` 禁止外呼 | ✅ | `responder.md` §3 |
| 3 | LLM 调用与请求量解耦 | 预生成 + 按会话缓存 | ✅ | ADR-0014 §3 |
| 4 | 注入防护是结构性的 | 数据/指令分离（非模式匹配） | ✅ | `AR-31` |
| 5 | 分析无执行能力 | 最小权限 + 不回流响应 | ✅ | `AR-32` |
| 6 | 原始证据不丢弃 | 净化只作用于 LLM 视图 | ✅ | ADR-0015 §决定 5 |

## 6. 验证证据

```console
$ make trace
追溯检查通过。

$ make gate
门禁通过。
```

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 诱饵多态与再生成机制 | 对抗众包识破 | [ADR-0010](../background/decisions/0010-functional-camouflage.md) 未解决 |
| 2 | 一致性键粒度 / 预生成时机 / `severity` 交互 | 实现细节 | [ADR-0014](../background/decisions/0014-generative-deceptive-response.md) |
| 3 | 多源交叉校验 / 注入测试集 | 结论可信度 | [ADR-0015](../background/decisions/0015-indirect-prompt-injection.md) |
| 4 | `strategy` / `intent` / `chain` 的模块文档（含注入面） | 阶段 3 设计 | 下轮或实现期 |

## 7.1 审视记录（L 档）

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 架构 §5 的 LLM 规范只管输出侧，输入侧（间接注入）无覆盖 | 缺口 | `architecture.md` §5 grep | 新增 `AR-31` / `AR-32` | ✅ |
| 2 | 「同资源同答案」此前只在 notes/research 提过，未成为规则 | 漂移（未升格） | `deception-engine-design.md` §2.4 | 升格为 `AR-30` | ✅ |
| 3 | 核心禁止外呼（`MD-6`）与「用 LLM 生成响应」冲突 | 冲突识别 | `MD-6` | 设计为「L4 预生成 + 核心读库」，不改 `MD-6` | ✅ |

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 补响应层设计：`responder` + `llm-components` + 2 ADR + 3 规则 | 用户指示 + 调研 |
