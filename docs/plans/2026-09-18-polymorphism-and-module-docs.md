# 变更包 · 2026-09-18 · 诱饵多态机制 + 补完模块文档

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | ① 诱饵多态与再生成机制（[ADR-0016](../background/decisions/0016-decoy-polymorphism.md)，与 `AR-30` 划界）；② 补完剩余 8 个模块文档（清单 §1.1 全部有文档） |
| 日期 | 2026-09-18 |
| 状态 | 已实现（**设计轮**，不含代码） |
| 涉及模块 | `decoy`（补充）· `edge-injection` · `honeypot-protocol` · `honeypot-shell` · `netpolicy` · `intent` · `chain` · `strategy` · `console` |
| 决策数 | 已答 1 项（多态机制）/ 待定 4 项（见 §7） |
| 关联 | [ADR-0016](../background/decisions/0016-decoy-polymorphism.md) · [`../log.md`](../log.md) 同日条目 |

## 1. 需求与验收

**要解决什么**：① 静态诱饵会被多 Agent 共享记忆**众包识破**（综述空白 F，评级 ⭐⭐⭐）；② 权威清单里仍有 8 个模块**没有模块文档**（`MD-2` / `MD-17` 要求一模块一文件、实现前创建）。

**做完之后，用户能做什么 / 看到什么**：诱饵有明确的多态机制且与 `AR-30` 不冲突；`docs/modules/` 覆盖清单全部 24 行。

**验收判据**：

1. [ADR-0016](../background/decisions/0016-decoy-polymorphism.md) 记录候选、理由、后果、**失效条件**，并给出与 `AR-30` 的**划界**。
2. 清单 §1.1 的每个有效模块都有对应模块文档（九章）。
3. `make gate` 全绿。

**不做什么**：

- 不写代码（设计轮）。
- 不设计学习型多态（[ADR-0016](../background/decisions/0016-decoy-polymorphism.md) 候选 D，留作演进）。

## 2. 设计逻辑

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地 |
| --- | --- | --- | --- | --- |
| ① | 怎么对抗众包识破 | **会话间轮换 + 识破信号触发**；学习留作演进 | 成本极低（哈希纯函数）；打破长期不变的指纹 | [ADR-0016](../background/decisions/0016-decoy-polymorphism.md) |

**关键划界**（本决策的核心）：

| 维度 | 规则 |
| --- | --- |
| 会话内（同一 `(会话, 资源)`） | **必须**一致（`AR-30`）—— 重试 / 翻页 / 重放同答案 |
| 会话间（不同会话） | **允许**不同 —— 多态粒度是会话 / 时间窗 |
| 时间上（跨轮换窗口） | **必须**粘住建立时的变体 |

> 一句话：**多态在会话边界上选择，在会话内部冻结。** 这使 `AR-30`（一致性）与多态（差异）**同时成立**。

**仍未定**：

| # | 未决 | 去向 |
| --- | --- | --- |
| 1 | 变体集合的规模与生成方式 | [ADR-0016](../background/decisions/0016-decoy-polymorphism.md) |
| 2 | 识破信号的判据与阈值（属实测） | 同上 |
| 3 | 「会话结束」的判据（变体冻结到何时） | 同上 |
| 4 | `intent` 的意图分类体系 / `chain` 的链判据 | 阶段 3 设计 |

## 3. 文档对应（追溯矩阵）

| 决策 / 规则 | 模块文档 | 验证命令 |
| --- | --- | --- |
| [ADR-0016](../background/decisions/0016-decoy-polymorphism.md) | [`../modules/decoy.md`](../modules/decoy.md) §1/§4 · [`../modules/chain.md`](../modules/chain.md) §1 · [`../modules/strategy.md`](../modules/strategy.md) §1 | `make trace` |
| `AR-30`（一致性） | `decoy.md` §4（划界）· [`../modules/responder.md`](../modules/responder.md) §4 | `make trace` |
| `MD-2` / `MD-17`（一模块一文档） | 全部 9 份新文档 | `make trace`（模块文档 ↔ 代码） |
| `AR-12` | `chain.md` §4 | `make trace` |
| `MD-9` / `ST-5` | `edge-injection.md` §4 | `make trace` |

## 4. 文档实现（本轮文件清单）

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `docs/background/decisions/0016-decoy-polymorphism.md` | 新增 | 诱饵多态机制 + 与 `AR-30` 划界 |
| `docs/background/decisions/README.md` | 改 | 登记 0016 |
| `docs/modules/decoy.md` | 改 | §1 多态职责 · §4 `AR-30` 划界 · §8 未决 |
| `docs/modules/edge-injection.md` | 新增 | L1 注入执行（九章） |
| `docs/modules/honeypot-protocol.md` | 新增 | L2 协议仿真（九章，**可选自研**） |
| `docs/modules/honeypot-shell.md` | 新增 | L2 命令表 / 假文件系统（九章，**可选自研**） |
| `docs/modules/netpolicy.md` | 新增 | L3 微隔离（九章） |
| `docs/modules/intent.md` | 新增 | L4 意图识别（九章） |
| `docs/modules/chain.md` | 新增 | L4 攻击链 + 识破信号（九章） |
| `docs/modules/strategy.md` | 新增 | L4 策略生成 + 再生成决策（九章） |
| `docs/modules/console.md` | 新增 | 控制台（九章） |
| `docs/progress.md` | 改 | 8 个模块文档状态 → ✅（设计） |

## 5. 场景（设计覆盖度）

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | 多态与一致性共存 | 有明确划界 | ✅ | ADR-0016 §划界 |
| 2 | 识破信号触发再生成 | 四类信号 + 经 `strategy`/`policy` 间接生效 | ✅ | `chain.md` §1 · `strategy.md` §1 |
| 3 | 模块文档全覆盖 | 清单 24 行均有文档 | ✅ | `docs/modules/` 24 份 |
| 4 | 蜜罐为可选自研 | `honeypot-protocol` / `-shell` 明确标注 | ✅ | 两文档 §1 |

## 6. 验证证据

```console
$ ls docs/modules/*.md | wc -l   # 覆盖清单全部模块
$ make trace
追溯检查通过。

$ make gate
门禁通过。
```

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 变体集合规模 / 识破阈值 / 会话结束判据 | 多态实现 | [ADR-0016](../background/decisions/0016-decoy-polymorphism.md) |
| 2 | `intent` 意图分类体系（TTP 映射）· `chain` 链判据 | 阶段 3 实现 | 两文档 §8 |
| 3 | `netpolicy` 是否进 MVP（既有未决） | 模块阶段 | [`../design/modules.md`](../design/modules.md) §7 |
| 4 | 学习型多态（候选 D） | 演进 | [ADR-0016](../background/decisions/0016-decoy-polymorphism.md) |

## 7.1 审视记录（L 档）

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 多态（同资源不同答案）与 `AR-30`（同资源同答案）表面冲突 | 冲突识别 | 两规则文本 | 用**作用域划界**（会话内 vs 会话间）解决 | ✅ |
| 2 | 8 个模块有清单行但无文档 | 缺口（`MD-17`） | `docs/modules/` 列表 | 全部补齐九章 | ✅ |
| 3 | `honeypot-protocol` / `-shell` 与 `honeypot` 的职责重叠风险 | 边界 | ADR-0011 | 前者标注**可选自研**，后者是入口 | ✅ |

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 诱饵多态机制 + 补完 8 个模块文档 | [ADR-0016](../background/decisions/0016-decoy-polymorphism.md) · `MD-2` / `MD-17` |
