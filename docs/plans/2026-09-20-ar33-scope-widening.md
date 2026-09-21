# 变更包：`AR-33` 口径放宽为「任何 LLM 生成」（用户确认的升格）

| 项 | 值 |
| --- | --- |
| 主题 | 把 `AR-33` 的适用范围从「欺骗内容生成」放宽为「**任何** LLM 生成」，并把交付禁令泛化到「任何消费方」——让规则文字追平**本来就是全局的**门禁检查 |
| 日期 | 2026-09-20 |
| 状态 | 已实现 |
| 涉及模块 | `ai-capability`（[`../design/modules.md`](../design/modules.md) §1.1 第 25 行）· `llm-components`（第 20 行）· 消费者 `intent`（17）· `chain`（18）· `strategy`（19）· 工具 `scripts/archcheck/` |
| 决策数 | 已答 1 项（用户确认）/ 待定 0 项 |
| 关联 | [ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md)（决定 4 由 🟡 转 ✅）· [ADR-0023](../background/decisions/0023-deception-content-injection.md)（原口径来源）· 上轮变更包 [`2026-09-20-ai-capability-lifecycle.md`](2026-09-20-ai-capability-lifecycle.md) |

---

## 1. 需求与验收

**要解决什么**（一句话）：`AR-33` 的**文字**只覆盖「欺骗内容生成」，而 `make archcheck` 的 `AR-33` 项
**本来就是全局的**（`analysis/` 下除出口、接缝、`llm/` 自身与测试外，一律禁止 import 模型客户端）——
于是「规则说一半、门禁管全部」，读者会以为「想接模型就接，只要别 import 客户端就行」。
用户于 2026-09-20 明确确认：**把口径放宽为「任何 LLM 生成」**。

**做完之后能做什么**：

1. `AR-33` 的规则文字与门禁检查**一致** —— L4 的意图 / 攻击链 / 策略在阶段 3 接模型时，
   **必须**登记成 kind 走 `ai-capability` 出口，不能自己在 `llm/` 里另起一条路；
2. 「未经护栏校验的**产物**禁止交付给任何消费方」（入库 / 下发 / 上报）—— 不再只针对内容与边缘下发；
3. 第二个消费方（动态沙箱）接入时，约束是**已确认的规则**，不是待定的提案。

**验收判据**：

1. `docs/design/architecture.md` 的 `AR-33` 行写明「**任何** LLM 生成」并把依据指向 ADR-0025；
2. **全库不再有**任何地方把 `AR-33` 的适用范围说成「欺骗内容」或「待确认」（可核：`grep` 清单见 §5）；
3. ADR-0025 的状态为「已采纳（决定 1–4）」、决定 4 为 ✅、原「未解决 1」标为已解决；
4. `docs/design/README.md` 与 `docs/design/modules.md` 的确认记录同步（升格台账一致）；
5. `make gate` 通过（含 `make trace` 的规则 ID 引用存在性检查）。

**不做什么**：

- **不改任何代码逻辑** —— 门禁本来就拦全局，本轮只改**注释与报错文案**（`scripts/archcheck/main.go` 2 处字符串）；
- **不动 `analysis/`** —— 出口、护栏、注册表一行不改；
- **不引入新规则 ID、不新增模块** —— 放宽的是既有 `AR-33` 的适用范围（ID 保持，`D-6`）。

---

## 2. 设计逻辑

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | `AR-33` 的口径 | 由「欺骗内容生成」放宽为「**任何** LLM 生成」 | **用户 2026-09-20 明确确认**（上一轮的 ADR-0025 决定 4 是 🟡 提案，本轮执行） | [`../design/architecture.md`](../design/architecture.md) 的 `AR-33` |
| ② | 交付禁令的措辞 | 「未经护栏校验的**内容**禁止入库 / 下发到边缘」→「未经护栏校验的**产物**禁止交付给**任何消费方**（入库 / 下发 / 上报）」 | 原措辞绑在内容这一个消费方上；放宽口径后第二批消费方（L4 分析 / 动态沙箱）的交付路径各不相同，禁令必须与消费方无关 | 同一行 |
| ③ | 依据指向 | 追加 ADR-0025（并保留 ADR-0023） | ADR-0025 是本次放宽的决策记录；ADR-0023 是模块与 `AR-33` 的由来 —— 两个都要能追 | 同一行的「依据」列 + 三个 `design/` 文档的确认记录 |
| ④ | 是否需要新 ADR | **不需要** | 它不是替代方案，而是**同一条决定的适用范围**（ADR-0025 已记；扩成两条反而要两处维护） | ADR-0025 决定 4 |

**仍未定**：无（本轮闭合一个待确认项）。

**接缝与接口**：无接口变更。

**数据流**：无。

---

## 3. 文档对应（追溯矩阵）

| 规则 ID | 文档章节 | 代码 / 工具 | 证据 | 验证命令 |
| --- | --- | --- | --- | --- |
| `AR-33` | [`../design/architecture.md`](../design/architecture.md) §5（规则表 `AR-33` 行） | `scripts/archcheck/main.go::checkGuardrailIsSoleExit` | 全库 `grep` 清单（§5 场景 1–4） | `make archcheck` |
| `AR-33` | [`../design/README.md`](../design/README.md) §1（AI 能力轮）· [`../design/modules.md`](../design/modules.md) §1.1 上方确认记录 | —— | 升格台账三处一致 | `make trace` |
| `AR-33` | [`../modules/ai-capability.md`](../modules/ai-capability.md) §4 · [`../modules/llm-components.md`](../modules/llm-components.md) §4 | `analysis/aicap/service.py`（唯一出口） | 模块文档与规则的适用范围一致 | `make trace` |
| `AR-33` | [`../kb/ai-capabilities.md`](../kb/ai-capabilities.md) §1.1（硬约束表）· §8.1 优化点 1 | —— | 删掉「待确认」的 ⚠️，改为已生效 | 人工核对 |
| `AR-15` / `AR-31` / `AR-32` | 同 `AR-33` 行（它们一起构成护栏纪律） | `analysis/llm/` · `analysis/aicap/guardrail/` | 既有单测 79 例全绿 | `make pytest` |
| `MD-4` | [`../modules/ai-capability.md`](../modules/ai-capability.md) §7 | `scripts/archcheck/main.go` | 上一轮已落地（依赖白名单） | `make archcheck` |
| `D-6` | 本轮**不**新增 / 不复用规则 ID（只改适用范围） | —— | `make trace` 的 D-3/D-6 检查 | `make trace` |
| `DEV-1` / `DEV-2` | 本文件 · [`../log.md`](../log.md) | —— | 形状检查 | `make trace` |

---

## 4. 代码实现（本轮 = 规则文字 + 文案同步）

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `docs/design/architecture.md` | 修改 | **升格本体**：`AR-33` 口径放宽 + 交付禁令泛化 + 依据追加 ADR-0025 |
| `docs/design/modules.md` | 修改 | §1.1 上方的用户确认记录要写清「口径于同日由欺骗内容放宽」，否则台账与规则对不上 |
| `docs/design/README.md` | 修改 | §1「AI 能力轮」同步（升格台账的第三处） |
| `docs/background/decisions/0025-generic-guardrailed-outlet.md` | 修改 | 状态行 → 已采纳（决定 1–4）；决定 4 → ✅ + 「已落地」表；「未解决 1」→ 已解决 |
| `docs/background/decisions/README.md` | 修改 | ADR 索引行的状态与未决数（5 → 4） |
| `docs/modules/ai-capability.md` | 修改 | §4 的 `AR-33` 行同步口径 |
| `docs/modules/llm-components.md` | 修改 | §4 新增 `AR-33` 行（这是本轮的**实质影响**：本层的消费者从此受该规则约束） |
| `docs/kb/ai-capabilities.md` | 修改 | §1.1 硬约束表（删「待确认」）· §8.1 优化点 1（出口问题已定） |
| `scripts/archcheck/main.go` | 修改 | **仅 2 处注释/文案**由「欺骗内容的生成」改为「任何生成」（逻辑一字不动） |
| `docs/plans/2026-09-20-ar33-scope-widening.md` | 新增 | 本文件 |
| `docs/log.md` | 修改 | 本轮变更日志条目 |

**必须遵守的上位约束**：`D-6`（规则 ID 不得删除 / 复用 —— 本次是**改适用范围**，ID 与位置不变）·
`AGENTS.md` §3（升格须用户确认 —— 已确认）· `MD-3`（不复制契约）· `P-2`（`design/` 只用必须/禁止，可验证）。

---

## 5. 测试与场景

| # | 场景 | 输入 / 前置 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 1 | 规则文字已是新口径 | `grep -n "AR-33" docs/design/architecture.md` | 命中「**任何** LLM 生成」 | ✅ | §6 证据行 |
| 2 | 全库无残留旧口径 | `grep -rn "欺骗内容生成必须经" docs/ scripts/` | **只剩历史快照**（`docs/log.md` / 旧变更包 / 旧 ADR 的原文引用） | ✅ | §6 证据行 |
| 3 | ADR 状态已闭合 | `grep -n "状态：\|决定 4" docs/background/decisions/0025-*.md` | 含「已采纳（决定 1–4）」与「✅ **已确认」 | ✅ | §6 证据行 |
| 4 | 升格台账三处一致 | `grep -rn "任何.*LLM 生成必须经" docs/design/` | `architecture.md` · `modules.md` · `README.md` 各一处 | ✅ | §6 证据行 |
| 5 | 门禁与追溯仍绿 | `make gate` | 含 `AR-33` 项与 `D-3` 规则 ID 引用检查 | ✅ | §6 证据行 |
| 6 | 代码逻辑未被误改 | `git diff --stat scripts/archcheck/main.go` | 只有注释/字符串行变化（2 处） | ✅ | §6 证据行 |
| 7 | `analysis/` 一行未动 | `git diff --stat analysis/` | 空 | ✅ | §6 证据行 |

**没有覆盖的**：

- **没有新单测** —— 门禁检查的逻辑没变（本来就全局），本轮改的是规则文字与注释；第 6/7 项用 `git diff --stat` 证明「没动逻辑」比加测试更直接；
- **没有验证「放宽后 L4 真的接得上」** —— 那要等阶段 3 真的接模型（本轮只解锁合法性）。

---

## 6. 验证证据

```console
$ make gate
架构检查通过。            ← 含 AR-33 项（模型客户端唯一出口）与 MD-4 项（依赖白名单）
追溯检查通过。            ← 含 D-3 规则 ID 引用存在性
79 passed · L4 单测（pytest）
门禁通过。
```

**七条场景证据**（§5 逐项）：

```console
# ① 新口径已入规则
$ grep -c "任何 LLM 生成必须经" docs/design/architecture.md
1

# ② 旧口径在活跃文档里零残留（唯一命中是本次变更包自己的检查描述）
$ grep -rn "欺骗内容生成必须经" docs/ scripts/ | grep -v .venv
docs/plans/2026-09-20-ar33-scope-widening.md:104:  ← 那是本文件 §5 写的「怎么核」那一行，不是文档结论

# ③ ADR 已闭合
$ sed -n '3p' docs/background/decisions/0025-generic-guardrailed-outlet.md
- 状态：✅ **已采纳**（决定 1–4；其中决定 4 的 `AR-33` 措辞放宽于 **2026-09-20 经用户确认**）
$ grep -n '^### 决定 4' docs/background/decisions/0025-generic-guardrailed-outlet.md
75:### 决定 4 · ✅ **已确认（2026-09-20）**：`AR-33` 的措辞放宽为「任何生成」

# ④ 升格台账三处一致
$ grep -rn "任何.*LLM 生成" docs/design/
docs/design/architecture.md:149 · docs/design/modules.md:14 · docs/design/README.md:13

# ⑥ 只改了 2 行注释/字符串
$ git diff --stat scripts/archcheck/main.go
 scripts/archcheck/main.go | 4 ++--      1 file changed, 2 insertions(+), 2 deletions(-)

# ⑦ analysis/ 一行未动
$ git diff --stat analysis/
(空输出)
```

**独立评审**（冷上下文 `reviewer`，只看产物与 diff）：**有异议 P1 ×5 + P2 ×6，已全部修完** ——

| # | 异议（评审给的一手证据） | 修法 |
| --- | --- | --- |
| ① P1 | `docs/modules/ai-capability.md` §8 未决 7 仍写「只覆盖欺骗内容 … 🟡 提案，待用户确认」——与本文件 §4 和规则本体**正面矛盾** | 标为 ✅ 已解决（连同本条，落地清单从 8 处补齐到 9 处） |
| ② P1 | `docs/kb/ai-capabilities.md` §10 发现 1 / 5 用**现在时**描述已解决的口径问题 | §10 加快照声明：口径那一半已解决，**另一半**（两套提示词未合并 / 四个任务未登记）仍未 |
| ③ P1 | `docs/log.md` **无本轮条目**（计划 §4 却声明改了它） | 本轮补齐 |
| ④ P1 | 变更包 §6 仍是「（待填）」，§5 七条全 ✅ 无证据 | 本节即填空，证据逐条在上一段 |
| ⑤ P1 | `git diff` 类声称被记为已核而无可复现证据 | 本节把 `git diff --stat` 的**真实输出**贴上（⑥⑦） |
| ⑥ P2 | `architecture.md` §5 的「适用范围」仍只列 L2 / L4 三项，与同一文件 §5 规则表的「任何」自相矛盾 | §5 适用范围补上「未来任何要调模型的生成能力」并点明「凡生成必经出口」 |
| ⑦ P2 | 规则的「能力之外一律禁止」与同一行**验证方式**（豁免 `llm/` 自身）不一致 | 改为「能力自身与客户端所在层（`analysis/llm/`）之外一律禁止，判据 = 门禁结构检查」 |
| ⑧ P2 | 「上报均禁止」与 `AR-16` / `AR-21` 要求的「记录拒绝原因」字面重叠 | 补「校验**拒绝记录**不属「产物」，按 `AR-16` 必须记录原因」 |
| ⑨ P2 | ADR 失效条件 1 仍允许**无条件**回退到候选 A，而放宽后的 `AR-33` 已排除 A 的护栏复制形态 | 限定为「**仅限内核形态**：护栏仍只许一处、仍禁止绕过路径」 |
| ⑩ P2 | ADR 未解决 4（「分析类结论要不要登记成 kind」）触发条件已发生但未闭合，与状态行「决定 1–4 已采纳」不自洽 | 标为 ✅ 已由口径放宽答定（**必须**登记成 kind） |
| ⑪ P2 | `docs/modules/llm-components.md` 新行有错字「本体 不自己实现出口」 | 改为「本层不自己实现出口」 |

> 评审同时确认：与 `AR-15` / `AR-24` / `AR-30` / `AR-31` / `AR-32` / `MD-3` / `TB-16` **无硬冲突**；
> 与 `NI-1` / `NI-5` **不打架**（对象不同：NI 管请求路径的失败放行，`AR-33` 禁的是「交付未过护栏的产物」，
> 而回退动作仍是「原样返回业务响应」）；升格程序（ID 保持 · 正文保留 · 三处台账 · ADR 闭合）**已走完**。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | L4 的四个模型任务（intent / chain / strategy / finalize）**还没登记成 kind** | 它们今天不调模型，所以不阻塞；阶段 3 接模型时**必须**先登记 | [`../kb/ai-capabilities.md`](../kb/ai-capabilities.md) §10 发现 1/5 |
| 2 | 两套提示词系统（`llm/resources/prompts/` 与 `aicap/resources/prompts/`）尚未合并 | 放宽口径后，前者的**位置**仍尴尬（在 `llm/` 下，不在出口的注册表里） | 阶段 3 立项时一并定 |
| 3 | 用户本轮同时提出「保证生成信息的安全与合规」+「护栏可在控制平台完善」 | **与 `ADR-0020` 决定 2（控制台只读）与 `AR-24`（提示词与资源必须随版本分发、禁止运行期可变存储）存在冲突** | 已按 `P-3` **停下报告**并出访谈题（见下方「下一步」），**本轮不实现** |

---

## 7.1 审视记录（本轮改了规则本体 → 必做）

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 规则改了，**升格台账三处**要不要同步？ | 台账一致性 | `docs/design/README.md` §1 · `docs/design/modules.md` §1.1 上方 · ADR 索引 | 三处全部同步（否则读者按旧口径理解） | 一致 |
| 2 | 有没有别处引用 `AR-33` 时**说了旧口径**？ | 表述漂移 | `grep -rln "AR-33" docs/ \| sort` → 23 份 | 逐份看：**活跃文档**全部改；**历史快照**（`docs/log.md` · 已归档变更包 · 旧 ADR 的原文）不改 | 见场景 2 |
| 3 | `AR-24`（提示词必须随版本分发）会不会被本轮影响？ | 交叉影响核查 | `AR-24` 原文只管「话术与提示词」 | 不受影响；但**用户本轮提的「控制台完善护栏」**会碰它 —— 已单列为遗留 3 并停下报告 | 无违规 |
| 4 | `scripts/archcheck/main.go` 改的是不是只有文案？ | 越界核查 | `git diff scripts/archcheck/main.go` | 只有 2 行注释/字符串；`gofmt` 无变化 | 已核 |
| 5 | 「交付禁令泛化」会不会比用户确认的范围**更宽**？ | 超范围核查 | 用户确认的是「适用范围放宽为任何生成」 | 泛化的是**同一句**的宾语（内容 → 产物、边缘 → 任何消费方），语义是「别绕护栏」，没有新增义务 | 在授权范围内 |
| 6 | 本轮有没有动代码逻辑 / `docs/design/` 的其它规则？ | 越界核查 | `git diff --stat` | 只改 `AR-33` 一行 + 三处台账 + 文档 + 2 处注释 | 未越界 |

> **排除项**：`docs/background/` · `docs/plans/` · `docs/log.md` · `docs/kb/` 的历史记录部分不在审视范围内。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-20 | `AR-33` 口径放宽为「任何 LLM 生成」+ 交付禁令泛化到「任何消费方」+ 三处升格台账与 ADR 同步 | **用户 2026-09-20 确认**（ADR-0025 决定 4 由 🟡 转 ✅） |
