# L4 分析层的开源复用审计（`intent` / `chain` / `strategy` / `worker`）

> **性质**：开源复用审查（候选 × 许可逐字 × 活跃度 × 规则冲突 × 判定）。**不具约束力** ——
> 结论进 [ADR-0031](../decisions/0031-analysis-reuse-and-model-backend.md)。
> **证据级**：**A** —— 许可正文取自 raw 文件原文（逐字）、活跃度取 GitHub API 快照、许可条款页逐字。
> **审查日期**：2026-09-21。**活跃度是快照**：`pushed_at` 之后可能变化。
> **审查范围**：`analysis/intent/` + `analysis/chain/` + `analysis/strategy/` + `worker.py` +
> `events.py` + `telemetry.py` + `dedupe.py`，合计 **1268 行**（`wc -l` 实测）。
> 不在范围内：`analysis/llm/` 与 `analysis/aicap/`（已由 [`ai-oss-reuse.md`](ai-oss-reuse.md)
> 审过，[ADR-0024](../decisions/0024-ai-oss-reuse-boundary.md)）。

---

## 1. 要回答的问题

[ADR-0024](../decisions/0024-ai-oss-reuse-boundary.md) 的**未解决 4**：
「`analysis/intent` · `chain` · `strategy` · `worker` 的复用问题（本轮未审）」——
消费者侧还有多少是在重造轮子？

三个子问题：

1. **有没有对等的现成实现**能替掉这几百行？
2. 若要借，**许可允不允许**、**默认行为与我们同向吗**（ADR-0024 决定 1 的三条判据）？
3. 借进来要付出什么**不可逆代价**（依赖面、输出侧义务、形态错配）？

---

## 2. 方法与检索记录

| 步 | 做法 | 结果 |
| --- | --- | --- |
| ① 先量 | `wc -l` 量清被审代码的真实规模 | 1268 行（见文件头） |
| ② 按能力找候选 | 对四种能力分别找对等物：意图归类 / 攻击链还原 / 策略参数生成 / 遥测去重 | 五个候选（下表） |
| ③ 许可逐字 | 取候选的 raw `LICENSE` / 仓库 `Licenses` 文件原文；抓不到的用官方条款页 | 5/5 取得（其中 1 项改走官方条款页，见 §3.5 备注） |
| ④ 活跃度 | GitHub API `/repos/{owner}/{repo}`：`pushed_at` · `archived` · `stargazers_count` · `license.spdx_id` | 5/5 取得（2026-09-21 快照） |
| ⑤ 冲突点 | 逐条对着 `AR-15` / `AR-22` / `AR-30` / `AR-33` 与 `INT-11` 判「默认行为是否同向」 | 见 §3 |
| ⑥ 空白 | 记下**没有任何对等物**的能力（那是自研的正当性依据） | 见 §4 |

**未取得的项（显式写出来，不当成核过）**：
`mitre-attack/attack-stix-data` 的 `LICENSE.txt` 原文在本次会话抓取失败
（raw 端点超时）—— 改用 MITRE 官方 **Terms of Use** 页（权威性更高，且那才是许可的出处）。

---

## 3. 逐项材料

### 3.1 MITRE ATT&CK —— 战术枚举（术语与分类的现实来源）

| 项 | 值 |
| --- | --- |
| 仓库 | `mitre-attack/attack-stix-data`（STIX 2.1 数据集） |
| 活跃度 | 671★ · 未归档 · `pushed_at` **2026-08-05** |
| 许可（逐字） | 「The MITRE Corporation (MITRE) hereby grants you a non-exclusive, royalty-free license to use ATT&CK® for research, development, and commercial purposes. Any copy you make for such purposes is authorized provided that you reproduce MITRE's copyright designation and this license in any such copy.」 |
| 出处 | <https://attack.mitre.org/resources/legal-and-branding/terms-of-use/>（2026-09-21 抓取） |
| 与本项目的重叠 | **只有分类名**：`intent.recognize.CATEGORIES` 的五类（`reconnaissance` / `exploitation` / `lateral_movement` / `exfiltration` / `persistence`）就是 ATT&CK 战术名 |
| 冲突点 | ① **署名义务**：任何拷贝必须带 MITRE 版权声明与许可；② 战术集合是**演进的**（ATT&CK 会新增/重命名战术），而我们的五类是**闭集**（`INTENT_SCHEMA.category.allowed`）—— 跟着上游走会与「闭集」冲突 |
| 判定 | 🟡 **借知识，不装数据**：术语对齐属事实性使用（五个战术名不构成可主张的独创表达），**不**引入 STIX 数据集、**不**在代码里复制其文本；若将来在文档里引用 ATT&CK 名称，按官方要求加版权声明 |

### 3.2 OWASP CRS —— 攻击特征正则（最接近「意图规则表」的东西）

| 项 | 值 |
| --- | --- |
| 仓库 | `coreruleset/coreruleset`（官方仓库） |
| 活跃度 | 3269★ · 未归档 · `pushed_at` **2026-09-21**（当日仍在提交） |
| 许可（逐字） | `LICENSE` 开头：`Apache License / Version 2.0, January 2004 / http://www.apache.org/licenses/`（全文 11347 字符） |
| 与本项目的重叠 | `intent.recognize._PATTERNS` 那 5 条正则（`.git` / `.env` / `union select` / `/etc/passwd` / `crontab` …）与 CRS 的 `REQUEST-9xx` 系列攻击特征族高度重叠 |
| 冲突点 | ① **形态错配**：CRS 是 ModSecurity/Coraza **规则引擎**的规则集（`SecRule` + 变换链 + 阶段 + 异常评分），复用其规则等于引入一个引擎，而我们要的是一张 Python 正则表（ADR-0024 决定 1 第 2 条：不得接管处置语义）；② **语义不同**：CRS 判「这个请求该不该拦」，我们判「这批观测属于哪类意图」—— 前者是逐请求的判定，后者是跨事件的归类；③ 正则本身可借，但**借来的是知识，不是代码** |
| 判定 | 🟡 **借知识（可），不整库引入（不可）**：要扩特征表时，参考 CRS 的**特征形态**改写为我们自己的正则，并在注释里注明出处与许可；**不**引入 CRS 规则文件、**不**引入规则引擎 |

### 3.3 Sigma / OSSEM —— 检测规则与检测模型

| 项 | 值 |
| --- | --- |
| 仓库 | `SigmaHQ/sigma`（规则库）· `OTRF/OSSEM`（检测模型） |
| 活跃度 | Sigma：11071★ · 未归档 · `pushed_at` **2026-09-21**；OSSEM：1301★ · 未归档 · `pushed_at` **2023-02-27** |
| 许可（逐字） | Sigma 仓库 `Licenses` 文件：「The Sigma specification … and the Sigma logo are public domain / The rules contained in the SigmaHQ repository … are released under the Detection Rule License (DRL) 1.1」。DRL-1.1 正文（SPDX）：MIT 式的授予，**外加输出侧义务** ——「If you share the Rules (including in modified form), you must retain … [author / URI / 许可文本]」与「**If you use the Rules (including in modified form) on data, messages based on matches with the Rules must retain …** identification of the authors(s) ("author" field) of the Rule」。OSSEM：MIT（GitHub API `license.spdx_id = MIT`，`LICENSE` 文件） |
| 与本项目的重叠 | Sigma：SIEM 日志字段上的检测规则（与我们的事件模型不同构）；OSSEM：检测相关的**数据模型**（实体关系），与 `chain` 的阶段/证据模型概念相邻 |
| 冲突点 | ① **DRL-1.1 不是 OSI 许可**，且它把**署名义务压到输出上**：凡由规则命中产生的消息都要带上规则作者 —— 我们的结论会进控制台与遥测（`analysis` 事件），那意味着一条**跨模块契约**（谁生成、谁保署名、谁展示）；② **OSSEM 已停更**：`pushed_at` 停在 2023-02-27（距今约 31 个月）—— 触发 [ADR-0024](../decisions/0024-ai-oss-reuse-boundary.md) **失效条件 2**（任一依赖 12 个月无提交即重开评估），先例是已归档的 `llm-guard` 与 `PyRIT` |
| 判定 | ⛔ **不复用**（两者的原因不同）：Sigma —— 输出侧署名义务与我们的结论载荷契约冲突，且字段模型不同构；OSSEM —— 停更触发 ADR-0024 的失效条件 2 |

### 3.4 NetworkX —— 图算法（攻击链还原最诱人的误配）

| 项 | 值 |
| --- | --- |
| 仓库 | `networkx/networkx` |
| 活跃度 | 17271★ · 未归档 · `pushed_at` **2026-09-18** |
| 许可（逐字） | `LICENSE.txt`：「NetworkX is distributed with the 3-clause BSD license.」（版权行 `Copyright (c) 2004-2026, NetworkX Developers`，全文 1763 字符） |
| 与本项目的重叠 | 名字上有：`chain` 听起来像「图」。**实测代码没有图**：`chain/reconstruct.py` 做的是「按 `STAGE_ORDER` 把 `rule_hits` 分桶 + 去重 + 逐阶段校验证据存在」，即一次 `dict.setdefault` + 一次 `assert_exists`，复杂度 O(命中数) |
| 冲突点 | ① **没有需求**：今天不需要路径搜索/中心性/连通分量（引入图库的唯一理由是算法需求，而这里没有）；② **依赖面是长期成本**（ADR-0024 后果段已记）：BSD-3 许可没问题，但零运行期依赖的好处（离线可跑、无供应链、无升级负担）会立刻消失；③ **可逆性差**：一旦让 `Chain` 依赖 `networkx.DiGraph`，序列化形状（`to_wire()` 的 `stages[]`）与审计脚本都得跟着改 |
| 判定 | ⛔ **不复用** —— 这条是本材料的**主要「不需要」结论**：**「听起来像图」不是图形需求**。若将来真的出现路径/环检测需求（例如跨会话的攻击者图），再按 skill `evidence-and-decisions` §3 单独评估 |

### 3.5 现有确定性实现的替代品（整块替换）

| 找什么 | 找到什么 | 判定 |
| --- | --- | --- |
| 把「一批带 `event_id` 的观测」变成「意图 + 置信度 + 证据引用」的可复用库 | 无。所有对等物都绑定**自己的输入模型**（Sigma 绑 SIEM 字段、CRS 绑 HTTP 请求、OSSEM 绑实体关系） | ⛔ 无可复用 |
| 把「观测 + 证据索引」变成「阶段序列」，且拒绝引用不存在的证据（`AR-12`） | 无。`AR-12` 的「引用必须在遥测里真实存在」是本项目的证据链规则 | ⛔ 无可复用（自研的正当性就在这里） |
| 产出受 `INT-11` 约束的策略参数（灰度上限 / 阈值下界） | 无。上游里没有「策略建议」这种东西的对等物 | ⛔ 无可复用 |
| 态势去重（`AR-14`：按 `(来源,会话,手法)` 时间窗去重） | 无对等物。**也不需要**：`dedupe.py` 是 48 行、一个字典 + 一次时间比较 | ⛔ 无可复用 |

---

## 4. 空白矩阵（没有任何对等实现的能力）

| # | 能力 | 为什么没有对等物 | 结论 |
| --- | --- | --- | --- |
| C-1 | **证据引用必须在遥测中真实存在**（`AR-12`） | 这是本项目的证据链契约，不是通用问题 | 自研（正当） |
| C-2 | **解释型结论 + 闭集类别**（`AR-16` / `AR-15`） | 上游要的是「检测到没有」，我们要的是「属于哪一类、凭什么」 | 自研（正当） |
| C-3 | **策略参数的建议与边界**（`INT-11` 灰度上限 / 阈值下界） | 上游没有「策略建议」这一形态 | 自研（正当） |
| C-4 | **闭集守卫本身**（`Field.allowed`，本轮新增） | 通用 schema 库（pydantic 等）默认 lax，而 `AR-15` 要 fail-closed | 自研（正当） |
| C-5 | **同会话同资源同答案的钉定**（`AR-30`） | 上游的缓存语义不区分「会话钉定」 | 自研（正当） |

---

## 5. 结论

1. **零新增运行期依赖**。`analysis/requirements.txt` 保持三项（`grpcio` / `protobuf` / `PyYAML`）。
   五个候选里 **3 个 ⛔（Sigma · OSSEM · NetworkX）+ 2 个 🟡（ATT&CK · CRS，只借知识）**。
2. **最值得记的一条是 NetworkX**：它许可干净、极其活跃，唯一缺的是**需求**。
   如果按「有没有对口库」选型，它会被引进来 —— 而它解决的是一个不存在的问题。
3. **许可允许 ≠ 可以引**。Sigma 的 DRL-1.1 允许用，但把署名义务压到**输出消息**上，
   那会变成一条跨模块契约；ATT&CK 允许商用但要带版权声明。
   两条都说明：**引入前要看的是义务落在哪，而不是「能不能用」**。
4. **自研的正当性有据**：C-1…C-5 五条空白说明这几百行不是重造轮子，而是本项目特有的规则
   （证据链、闭集、阶梯放开、会话钉定）—— 这些恰是 ADR-0024 决定 1 判定「不满足第 1 条就不引」的地方。
5. **没有一项建议升格为规则**：本材料不改任何 `design/` 内容（`P-1`）。

---

## 6. 局限与待核

| # | 局限 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 活跃度是 **2026-09-21 的快照**（`pushed_at` / `stars` / `archived`） | 「活跃」的判断会过期 | 引用前重取；ADR-0031 的失效条件已写明 |
| 2 | `attack-stix-data` 的 `LICENSE.txt` 原文**未取到**（改用官方 Terms of Use 页） | 许可结论的依据是条款页而非仓库文件 | 若将来真要**整份**引入 STIX 数据，先补取仓库原文 |
| 3 | CRS 的**特征形态**借用是**人工改写**，不是自动转换 | 改写质量取决于人，可能引入误报 | 每次改写都跑 `make pytest` 的规则样本（`test_l4_modules.py`） |
| 4 | 没有做**性能基准**（替换前后吞吐/延迟） | 「自研够用」是推理，不是实测 | 只有当真出现吞吐瓶颈时才按 skill `evidence-and-decisions` §3 做 |
| 5 | 未审 `analysis/aicap/` 与 `analysis/llm/` 的**模型路径**新增面 | 本轮新增的 `llm/schemas.py` / `llm/deepseek.py` 的复用面未走这份材料 | 它们只用标准库与既有契约；若将来要引 `json_repair` 等，走 ADR-0024 的失效条件 1 |

---

## 7. 与本项目其他材料的关系

| 关系 | 说明 |
| --- | --- |
| 承接 [ADR-0024](../decisions/0024-ai-oss-reuse-boundary.md) | 关闭其**未解决 4**（消费者侧复用问题）；决定 1 的三条判据在本材料里被逐项使用 |
| 依据 [ADR-0016](../decisions/0016-decoy-polymorphism.md) | `chain` 的识破信号与 `strategy` 的轮换都是那一条的落地；本材料没有推翻它 |
| 结论进 [ADR-0031](../decisions/0031-analysis-reuse-and-model-backend.md) | 本材料只给证据与判定；成决策的表述在 ADR-0031 |
| 与 [`ai-oss-reuse.md`](ai-oss-reuse.md) 的分工 | 那份审 `llm/` + `aicap/`（阶段 A 的能力层）；本份审 L4 的消费侧 |
