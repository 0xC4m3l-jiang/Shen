# docs/background —— 背景材料

> 本目录是**背景材料的统一容器**：一切「不是已确认规则」但需要留档的东西都放这里。
>
> **它没有约束力。** 其中的任何内容都**不得**被当作基线引用（依据 `AGENTS.md` 的 **P-1**）。
> 规则只在 [`../design/`](../design/README.md) 与 [`../modules/`](../modules/) 里。
>
> 导航中枢在 [`../README.md`](../README.md)。

| 子目录 | 是什么 | 效力 | 状态 |
| --- | --- | --- | --- |
| [`decisions/`](decisions/README.md) | **ADR**：候选、理由、**失效条件**、未决项 | 决策过程与理由 —— 解释基线**为什么是这样**、**什么条件下会变错** | ✅ 6 条（4 已采纳 · 1 暂缓 · 1 被取代；共 **15 项未决**） |
| [`notes/`](notes/implementation-discussion.md) | **讨论稿**：尚未成决策的议题、待执行实验、威胁模型草案 | 仅供参考 —— **不得**当作已定事实 | ✅ 3 份 |
| [`research/`](research/README.md) | **调研**：材料、审计、立项依据、来源稿 | 证据材料 —— 引用**必须先核验** | ✅ 6 份 + 1 份原始材料 |

### 三者的分工（容易混）

| | `decisions/` | `notes/` | `research/` |
| --- | --- | --- | --- |
| 回答 | **为什么这么定**、什么时候该改 | **还没定什么** | **凭什么这么定** |
| 内容 | 候选对比、负面后果、失效条件 | 议题、倾向、实验设计、草案 | 文献、逆向、审计、来源稿 |
| 何时写 | 决策**未定**或**刚定**时 | 讨论中 | 找到新材料时 |
| 完成后去哪 | 拍板 → 升格为 `../design/` 的规则 | 拍板 → 变成 ADR | 结论若被确认 → 升格为规则 |

---

## 2. 三个目录各自的入口

| 我要找 | 去哪 |
| --- | --- |
| 为什么选了 Go 而不是 Rust；什么情况下会改回去 | [`decisions/0006-core-language-go.md`](decisions/0006-core-language-go.md) |
| **还剩什么没定**（未决项的唯一权威位置） | 各 ADR 的「未解决」段；索引见 [`decisions/README.md`](decisions/README.md) §2 |
| 尚未成决策的议题（`D0`、`D1`、`D6`…） | [`notes/implementation-discussion.md`](notes/implementation-discussion.md) §6.1 |
| **待执行的实验**（`E1` 语言 spike / `E2` TLS 指纹） | [`notes/pending-experiments.md`](notes/pending-experiments.md) |
| 对手是谁、我们会被怎么识破（威胁模型草案） | [`notes/threat-model.md`](notes/threat-model.md) |
| 调研材料清单与证据分级 | [`research/README.md`](research/README.md) §1 |
| 知识缺口 `G1`–`G12`（含状态） | [`research/knowledge-base-audit.md`](research/knowledge-base-audit.md) §2 |
| 参考实现的源码级拆解 | [`research/agentcapture-deception-report.md`](research/agentcapture-deception-report.md) |
| 基线来源稿（`design/` 的上游，含未收录细节） | [`research/deception-engine-design.md`](research/deception-engine-design.md) |

---

## 3. 使用纪律

| # | 纪律 |
| --- | --- |
| 1 | **禁止**把本目录的内容当作基线。发现它与 [`../design/`](../design/README.md) 冲突时，**以 `design/` 为准**（`P-3`） |
| 2 | **禁止**在本目录新增「规则」—— 规则只能写在 `design/`，且必须经用户确认（`P-1`、升格流程见 `AGENTS.md` §3） |
| 3 | 新调研材料**必须**放 `research/`，并登记到 [`research/README.md`](research/README.md) §1 的材料清单（含行数、性质、**证据级**） |
| 4 | 引用**必须**先核验 ID / 出处；核不到标「待核」，**不得**写入结论 |
| 5 | 已挂 ADR 的未决项**只写在 ADR 的「未解决」段**，**禁止**在 `notes/` 重复列出 |
