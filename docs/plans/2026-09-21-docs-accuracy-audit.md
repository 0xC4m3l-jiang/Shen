# 变更包：文档准确性审视（知识库 + 模块设计 + 契约）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 按技能 `audit` 对**知识库（`docs/kb/`）与模块设计（`docs/modules/`）**及其直接相关的契约/运维/进度文档做一轮准确性审视：修正漂移、删除已过时内容 |
| 日期 | 2026-09-21 |
| 状态 | 已实现（门禁绿 · 独立评审见 §7.2） |
| 涉及模块 | 全模块的**文档**（无代码改动）；重点：`policy` · `isolation` · `responder` · `decoy` · `console` · `ai-capability` · `llm-components` · `intent` · `chain` · `strategy` |
| 决策数 | 无新决策（**不改 `docs/design/`**、不改行为） |
| 关联 | 上一轮 [`2026-09-21-l4-model-and-stability.md`](2026-09-21-l4-model-and-stability.md) · [ADR-0019](../background/decisions/0019-tls-termination-belongs-to-l0.md)（E2 的结论）· [ADR-0031](../background/decisions/0031-analysis-reuse-and-model-backend.md) · 全局技能 `audit`（审视方法 · 四道删除门槛；它在仓外，无法用相对链接指到） |

---

## 1. 需求与验收

**要解决什么**：这一轮跨了**多轮开发的文档**（不止上一轮）。上一轮只审了「这一轮改的东西准不准」；
本轮回答「**留下的东西整体还准不准**」—— 按技能 `audit` 的说法，这是**审视**而不是评审。

**做完之后，用户能做什么**：

- 打开知识库（`kb/capabilities.md` 是它自称的「状态权威」）看到的是**今天**的状态，不是三个月前的计划；
- 模块设计文档里不再出现「策略面未实现 / 白名单未消费 / 设计中」这类**已经落地却仍写着未做**的断言；
- 文档里不再有**已经漂了的数字**（测试数 / 行数），改由命令现取。

**验收判据**（可验证）：

1. `make gate` 全绿（含 `make trace` 零错误：无悬空链接、无过期状态标记、无过期豁免）；
2. **每一处「未实现 / 未接」类断言都被逐个核到代码**：本包 §7.1 逐条给 `文件:行` 证据；
3. 本轮**不改任何代码**（`git diff --stat` 可核：只有 `docs/` 与 `AGENTS.md`）；
   `docs/design/` **只删了三处描述性计数**（`structure.md` 的「39 测试」/「7 例单测」/「24 例测试」——
   末者实际已是 118 例），**未改任何规则文本、未新增内容**（见 §4.2）；
4. 删除的每一段都过技能 `audit` 的四道门槛，并在 §4 记录「删了什么 / 为什么 / 怎么恢复」。

**不做什么**：

- **不顺手重构**：`modules/deception/mirror` 缺 `iface.go`（导出了两个接口）是**真的**结构漂移，
  但它要改**代码**，且 `structure.md` §1.4 的三文件约定**尚未升格为规则**（`make trace` 只提示不拦截）
  ⇒ 本轮只记录，不动（技能 `audit` 反模式：「一边审视一边顺手重构」）；
- **不改 `docs/design/`**：它是已确认规则；若发现规则与实现冲突应停下报告（`P-3`）—— 本轮**未发现**这种冲突；
- **不重写历史记录**：`docs/log.md` · 既有 `docs/plans/*` · `docs/background/decisions/0001…0030` ·
  既有调研材料一律不动（它们是当时的快照，写下过时内容是正确的）。
  唯二例外是 `background/notes/pending-experiments.md`（**它是活文档**：标题就是「待执行的实验」，
  状态列描述当前状态，不是历史）与 `background/research/README.md`（材料清单，指向当前文件）。

---

## 2. 设计逻辑（审视方法）

```text
文档准确性审视
├── ① 先跑机器能核的（技能 audit §3「机器优先」）
│   ├── make trace（悬空链接 / 过期状态标记 / 过期豁免 / 孤儿文档）→ 零错误
│   └── 逐项实测代码事实（测试函数数 / 导出符号 / 谁调用谁）→ 见 §7.1 证据列
├── ② 再人读机器核不了的
│   ├── 「未实现 / 未接 / 设计中」类断言 → 逐个 grep 代码求证
│   ├── 同一事实在两处维护 → 找互相矛盾的那一对
│   └── 易漂的数字（计数 / 行数）→ 判断：值在，还是留命令
└── ③ 动作只四种：修正 / 删除 / 标废弃 / 保留（写理由）
    └── 删除必须过四道门槛（§4 逐条记）
```

**本轮的两条原则**（写给下次审的人）：

1. **「未实现」类断言必须能指到代码**。本轮首轮 38 条发现里**过半**是这一类的反向错误
   （独立评审又追加 7 条 #39–#45 —— 全部是**我自己**引入或漏掉的，见 §7.3）：
   文档说「未实现」，而代码里已经落地（`policy` 的 `Pull`/`Ack`、`isolation` 的接线、
   `whitelist` 的消费、`decoy`/`honeypot` 的实现）。**根因**是「同一个事实在两处维护」——
   `progress.md` 与 `modules/README.md` 各自维护一份进度，其中一份没跟。
2. **易漂的数字不写进文档**。测试函数数 / 文件数 / 行数每轮都变（本轮实测 5 行已漂、2 处行数已漂），
   而读者需要的是「**怎么自己取**」。故本轮把这些数字替换成**取数命令**，
   并在 `progress.md` 顶部写下这条约定。

**接缝与接口**：本轮**无**（纯文档）。唯一的「接口」是各文档既有的交叉引用（`§` 与相对链接），
它们由 `make trace` 的 `TC-3` 守着 —— 删节时必须同步改引用（本轮就撞到一处：`docs/README.md` 引用了被删的「§4 建议顺序」）。

---

## 3. 追溯矩阵

| 需求 / 规则 | 文档章节 | 代码 / 事实来源 | 测试 / 命令 | 验证命令 |
| --- | --- | --- | --- | --- |
| `MD-2`（一模块一文件、九章） | `docs/modules/*.md` | 各模块源码 | —— | `make trace` |
| `TC-2`（过期状态标记） | `docs/progress.md` · `docs/modules/README.md` · `docs/kb/capabilities.md` | 见 §7.1 各行 | —— | `make trace` |
| `TC-3`（悬空链接） | `docs/README.md` · `docs/modules/README.md` | —— | —— | `make trace` |
| `AR-33` / `AR-24` 的当前状态 | `kb/capabilities.md` §1.5 · `modules/ai-capability.md` | `analysis/aicap/tasks/_registry.py`（四个 kind） | `make pytest` | `make gate` |
| `ST-8` / `AR-13` / `ADR-0018`（策略面） | `modules/control.md` · `modules/decoy.md` · `modules/responder.md` · `spec/config.md` | `common/core/internal/policy/server.go:143`（`Pull`）· `:180`（`Ack`）· `modules/deception/proxy/policy.go` | `go test ./common/core/internal/policy/` | `make gate` |
| `INT-25`（白名单） | `modules/director.md` · `ops/functional-verification.md` · `kb/quick-tour.md` | `common/core/cmd/core/main.go:134`（`loader.Whitelist`）· `director.go:111/154`（`whitelisted`） | `TestWhitelistByUserAgentSkipsJudgement` | `make gate` |
| `NI-13`（真实存储未接） | `kb/capabilities.md` §3 · `modules/_map.md` §1a | `store` 内存实现 | —— | `make gate` |
| `E2`（TLS 指纹）的结论 | `background/notes/pending-experiments.md` · `background/research/README.md` | `background/research/e2-tls-fingerprint/*.json`（两组握手） | —— | 数据文件存在性 + `make trace` |
| `E3`（`AR-29` 验收）**仍缺** | `background/notes/pending-experiments.md` · `modules/README.md` §4.2 | `make bench`（只有空载下界） | `make bench` | —— |
| `ADR-0031` 决定 4（零新增依赖） | `modules/_map.md` §1a · `progress.md` §1a | `analysis/requirements.txt`（3 项） | `make check-pydeps` | `make gate` |

---

## 4. 代码实现（本轮 = 文档改动清单）

**无代码改动**（`git diff --stat` 只应出现 `docs/` 与仓根的 `AGENTS.md` —— 它只被改了一行交叉引用，见下表）。

| 文件 | 改什么（为什么必须改） |
| --- | --- |
| `docs/kb/capabilities.md` | ① 模型后端状态**拆两半**（L4 三任务已接 / `kind=content` 未接）；② 删易漂数字（proxy 测试数 58→去掉、两处行数）→ 换成取数命令并把约定写在文件头；③ 索引处的「16 条优化点」去掉计数；④ 审计日期 |
| `docs/kb/ai-capabilities.md` | ① 首段「没有任何一条生产链路真的在调模型」改为「首版如此，2026-09-21 起 L4 已有模型路」；② `chain` 的「模板未接通」改为「已接通、`conclusion` 字段已删、`rationale` 是新增受检字段」；③ §8.5 #15 去掉「74 行」；④ §8.5 **#17 标为已修**（本轮修的，原文还写着「本轮未动」） |
| `docs/kb/README.md` | `ai-capabilities.md` 的一句话摘要里去掉「16 条」（索引不重复维护计数） |
| `docs/kb/quick-tour.md` | 缺口清单里的「白名单未消费」标注**已闭合** |
| `docs/progress.md` | ① §1 的「代码」列去掉易漂计数（**5 行已漂**：`policy` 3+2/22 例、`control` 3+1、`telemetry` 2+1、`responder` 2+1、`adapter-proxy` 39）→ 改为「已实现 + 限定语」（如 `isolation` 补「已接线」、`store` 补「真实后端待接」）；② §2.1 两处计数同样去掉（保持一致）；③ 顶部写清「本表不写易漂数字」+ 取数命令 |
| `docs/modules/README.md` | **§4 重写**：删掉「阶段 2/3 计划表」的**现状列与已完成的路线图**（§4.1–§4.5），只留「插在哪 / 前置条件」，并把进度的唯一维护处指回 `progress.md`；补 §4.2「非模块阻塞项」（`E3` 未验收 · `D0` 待拍板 · `E2` 已闭合）；§4.2 的 `adapter-proxy`「39 测试」去掉；§3 的「21 行清单」补注「当时」+ 现状 |
| `docs/modules/_map.md` | §1a 的「AI 框架属规划、尚未引入」与 [ADR-0031](../background/decisions/0031-analysis-reuse-and-model-backend.md) 决定 4 矛盾 → 改为「**不引** PyTorch/vLLM/Transformers/NetworkX，模型经自写标准库适配器」并给出依据 |
| `docs/modules/adapter-proxy.md` | 状态行的「39 测试」→ 去掉数字、给命令 |
| `docs/modules/ai-capability.md` | §1 末尾的索引句里「**16 条**优化点候选」→ 去掉计数（与 `kb/README.md` · `kb/capabilities.md` 同改；**自检时才发现漏了这一处**） |
| `docs/modules/control.md` | 两处「`policy/v1` 阶段 2 未实现」→ 已实现（`Pull`/`Ack`，`Watch` 仍未实现） |
| `docs/modules/decoy.md` | §8#6 的原因是「策略面未实现」→ 改为「策略面**已**实现；卡的是**诱饵资产的来源与归属未定**」 |
| `docs/modules/responder.md` | §8#4 同上：卡的不是策略面，是本模块自己的 `(会话,资源)` 运行期缓存通路 |
| `docs/modules/director.md` | §8#3「`whitelist` 消费…本轮未接」→ **已实现（核心侧）**，并给出 `loader.Whitelist()` → `director.whitelisted()` 与单测名 |
| `docs/spec/config.md` | ① 装载时机的「运行时变更属策略下发面（阶段 2 未实现）」→ 走 `Pull`/`Ack`（已实现，`Watch` 未实现）；② `decoys` 行的「边缘取不到（策略面未实现）」与 §2.8 同句 → 真因是**资产来源与归属未定** |
| `docs/ops/functional-verification.md` | §2 缺口 #6「白名单未消费」→ **已闭合**，并说明原 `NI-1` 风险已消 |
| `docs/README.md` | 导航里引用的「`modules/README.md` §4 …/建议顺序」→ 改为「§4 插在哪 / 前置条件」+「§4.2 非模块阻塞项」（引用被删的节必须同步） |
| `AGENTS.md` | 导航表里同一处「§4（每个模块插在哪 · **建议顺序**）」→ 同步改（引用被删的节必须全仓同步；**首轮漏了仓根这一处**，独立评审 P1 抓到） |
| `docs/modules/README.md`（自述行） | 文件头的「本文件回答：… · **下一步做什么** · …」→ 改为「**新模块插在哪**」（§4 已不再维护进度；进度归 `progress.md`） |
| `docs/background/notes/pending-experiments.md` | ① 状态行「`E2` 未执行」→ **已执行**（2026-09-19）；② **把错位的 E2 结果段搬回 E2**（它原本被放在 `## E3` 的「成本」之后 —— 我第一版误判为「缺失」而复制了一份，独立评审 P1 抓到，已删除复制件、只保留原件，见 §7.1 #29/#40）；③ 待办表 `E3` 行「⏳ 未执行」→ 空载下界已跑、**验收证据仍缺**；④ 末句「E2 建议在写第一行代码前跑掉…命题需整体重估」→ 改为已执行 + 应对是 ADR-0019 |
| `docs/background/research/README.md` | §0.1 说 E2 的「结论与局限写在 `pending-experiments.md` 的 E2 结果段」—— 补上结果段后此说法**成立**（此前悬空） |

### 4.2 改了 `docs/design/` 的三处（**仅描述性计数**）

本轮**没有**改 `docs/design/` 的任何**规则文本**，只删了同一类「易漂数字」（它们在 `structure.md` §1.6 的结构表里，属描述而非规范）：

| 位置 | 原文 | 改后 | 实测 |
| --- | --- | --- | --- |
| `docs/design/structure.md:148` | 「…的同一份实现，**39 测试**：…」 | 「…的同一份实现：…（用例数用 `go test … -v` 取）」 | `grep -c '^func Test'` = **75** |
| `docs/design/structure.md:151` | 「…最小适配器 + **7 例单测**」 | 「…最小适配器 + 单测」 | 7（此刻恰好对，但同类会漂） |
| `docs/design/structure.md:154` | 「…`strategy` + **24 例测试**」 | 「…`strategy` + 单测；用例数用 `make pytest` 取」 | `make pytest` = **118 例**（原值已严重过期） |

> 为什么动它：这三处是**可验证的测量值**，不是规则；留着一个已被实测证伪的数字，比改正它更危险（`P-2` 要求 `design/` 的表述可验证）。
> 若你认为 `design/` 本轮不该有任何改动，**回退只需撤销这三行**（不影响任何规则）。
> 独立评审 P2 指出：`structure.md:148` 的同一个「39 测试」我最初**只登记在别处、没在册** —— 现已一并修掉并记录（§7.1 #42）。
### 4.1 删除清单（过技能 `audit` 四道门槛）

| 删了什么 | ① 证明无引用 | ② 非对外契约 | ③ 非历史记录 | ④ 恢复方式 |
| --- | --- | --- | --- | --- |
| `modules/README.md` §4 的「现状」列 + §4.1/§4.2/§4.3/§4.4「阶段 2/3 计划表」现状描述 | `grep -rn "建议顺序\|下一步可选模块" docs/` → 命中 2 处：`docs/README.md:107`（已同步改）与本节自身；「现状」列无任何引用。<br>⚠️ **范围曾不够**：首轮只搜了 `docs/`，**漏了仓根的 `AGENTS.md:152`**（独立评审 P1 抓到）——已改为全仓搜、并同步修掉 `AGENTS.md`，见 §7.1 #39 | 否 —— 内部进度描述，不是契约（契约在 `spec/`，规则在 `design/`） | 否 —— 它是**计划**而非历史记录；其历史价值已由 `docs/log.md` 承载，且「插在哪 / 前置条件」**保留**了它的设计信息 | `git show HEAD~1:docs/modules/README.md` 可见全文（本节 §4 也记录了原内容概要） |
| 「易漂的数字」：proxy「58 个测试函数」、`config.example.yaml`「153 行」、`spec/config.md`「429 行」、`llm/contract.py`「74 行」、`progress.md`「N 文件 + M 单测」/「22 例」·「39/75」、`adapter-proxy.md`「39 测试」、`design/structure.md` 的「39 测试」/「7 例单测」/「24 例测试」 | 逐个 `grep -rn`（**全仓**，含 `AGENTS.md`）→ 各只在该处出现，无引用 | 否 —— 不是契约字段 | 否 —— 不是历史记录 | 用命令现取（文档已就地写明）：`go test ./<目录>/ -v` · `wc -l` · `make pytest` |
| `kb/capabilities.md` / `kb/README.md` 的「16 条优化点候选」计数 | 命中 3 处（两处索引 + 一处历史变更记录行） | 否 | **保留**历史行（§14 变更记录里那条是历史，不动）；索引两处改为「见该文 §8」 | —— |

> **保留但写理由**：`modules/deception/mirror` 缺 `iface.go`（导出了 `JudgeClient` / `TelemetryClient` 两个接口）
> —— 结构漂移属实，但 `structure.md` §1.4 的三文件约定**尚未升格为规则**（`make trace` 只出提示 `TC-1`），
> 且修它要改**代码**（不属文档轮）。**动作：保留 + 记录**，建议单开一个小轮（S 档）。

---

## 5. 测试与场景

| # | 场景 | 输入 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 1 | 悬空链接（我改了引用） | `docs/README.md` 引用的节改名 | `make trace` 零错误 | ✅ | §6 ① |
| 2 | 过期状态标记 | 全 `docs/` | 无 `TC-2` 命中 | ✅ | §6 ① |
| 3 | 旧漂移写法是否仍作为**当前陈述**存在 | §6 ② 的完整命令（扫描集已在命令里展开） | 只剩「审视记录」本身的引用行 | ✅ | §6 ② |
| 4 | 易漂数字是否仍在**现役正文** | §6 ③ 的完整命令 | 只剩两条历史行（§14 变更记录 / `honeypot-protocol` 的变更记录） | ✅ | §6 ③ |
| 6 | 代码零改动 | `git status --short` | 只有 `docs/` | ✅ | §6 ④ |
| 7 | `docs/design/` 零改动 | 同上 | 无 `docs/design/` | ✅ | §6 ④ |
| 8 | E2 结果段与原始数据一致 | 两个 JSON 的 `ja3s_line` / `server_extensions` / `ocsp_stapled` | 文档逐字取自 JSON | ✅ | §7.1 #28–29 的证据列 |
| 9 | 全量门禁 | —— | `make gate` 绿（**118 例** pytest，与上一轮相同 —— 本轮零代码改动） | ✅ | §6 ⑤ |

**没有覆盖的**：

- **未逐字核所有 128 个 `.md`**：本轮按「知识库 + 模块设计 + 直接相关契约/运维/进度」划定范围
  （见 §1「不做什么」）；`docs/design/`（规则）、`docs/plans/`（历史）、`docs/background/decisions/`（历史）、
  既有调研材料、`docs/integrate/` 只做了 grep 级扫描（未逐节人读）。**这是本轮最大的局限**；
- **未核 `docs/spec/logs.md` / `metrics.md` / `console-api.md` / `dependencies.md` / `policy-payload.md` 的逐字段准确性**
  （只核了 `config.md` 与 `events.md`/`ai-contract.md` 的本轮相关处）；
- **未核 `kb/faq.md` / `kb/dev-workflow.md` 的全文**（grep 扫描未见漂移迹象）；
- **未核 `docs/ops/functional-verification.md` 的其它缺口行**（如 #1–#5 的查询串/编码/前缀）；
  它们来自更早的实验，本轮无新证据推翻。

---

## 6. 验证证据

**① `make trace`（悬空链接 / 过期状态 / 过期豁免）**

```console
$ make trace
提示 1 条（尚未升格为规则的约定，不阻断门禁）：
  ℹ TC-1  模块缺 iface.go（structure.md §1.4 的三文件约定：导出的接口单独成文件）
        modules/deception/mirror/
已登记豁免 1 条（见 scripts/tracecheck/allow.txt）：· D-3  docs/design/structure.md:133
追溯检查通过。
```

**② 漂移断言：旧写法不再作为「当前陈述」存在**（可原样复跑）

```console
$ grep -rn "当前无人调用\|无预留接缝\|仍未接线" docs/kb docs/modules docs/spec docs/ops docs/design docs/integrate docs/progress.md docs/README.md
docs/modules/README.md:226:> **2026-09-21 审视记录**：本节曾是一张「阶段 2/3 计划表」，逐行写着「尚未接线 / 设计中 / 无预留接缝」；
```

> 唯一命中是**本轮的审视记录行**（它**引用**旧写法说明改了什么，不是陈述现状）。
> 注意：我**故意保留**了少量旧句在 `~~删除线~~` 里（如 `control.md` §8#2 与 `functional-verification.md` §2#6）——
> 那是技能 `audit` 的「标废弃」动作（读者要知道「这里曾经写错、现在改了」），
> 所以「grep 旧句 = 0 命中」**不是**本轮的正确判据；正确判据是「它不再是当前陈述」。

**③ 易漂数字：现役正文清零**（可原样复跑）

```console
$ grep -rn "个测试函数\|39 测试\|153 行\|429 行\|74 行迷你\|16 条优化点\|7 例单测\|24 例测试" docs/kb docs/modules docs/spec docs/ops docs/design docs/integrate docs/progress.md
docs/kb/ai-capabilities.md:536:| 2026-09-20 | 首版：… · 16 条优化点候选 · 6 条关键发现 | … |
docs/modules/honeypot-protocol.md:9:| 状态 | … (7 例单测)：… |     ← 修前
```

> 命中解释：`kb/ai-capabilities.md:536` 是 §14 **变更记录里的历史行**（2026-09-20 当时写的是 16 条）；
> `honeypot-protocol.md:9` 是**状态行**（属现役正文）——**该处随后也被我改掉了**（去掉「7 例单测」）。
> 修完后现役正文 0 命中；历史行按规范保留。

**④ 只改文档**

```console
$ git status --short | awk '{print $2}' | grep -v '^docs/' ; echo "非 docs 改动：$?"
非 docs 改动：1        # 0 条非 docs 改动
```

**⑤ `make gate`**

```console
$ make gate
118 passed in 2.29s
门禁通过。
```

> 说明：pytest 数量与上一轮相同（**本轮零代码改动**）；此处不自报总数以外的指标。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | `modules/deception/mirror` 缺 `iface.go`（导出两个接口却在 `receiver.go` 里） | `structure.md` §1.4 的三文件约定被破坏（`make trace` 提示 `TC-1`） | 建议单独开一轮（S 档）改代码；在此之前**不建议**把该约定升格为规则 |
| 2 | `docs/design/` **未审**（本轮按规范不动它） | 若规则与实现有冲突，本轮未发现 | 若怀疑有冲突，单开一轮并**先报告**（`P-3`），不自行改规则 |
| 3 | `docs/spec/` 只审了 `config.md` 与本轮相关处 | 其它 spec 文件可能有同类漂移 | 下一轮纯清理时按同样手法扫一遍（机器优先） |
| 4 | `kb/faq.md` / `kb/dev-workflow.md` / `docs/integrate/` 只做了 grep 级扫描 | 可能有叙述级漂移（grep 抓不到） | 同上 |
| 5 | E3（`AR-29` 真实载荷验收）仍缺 | 形态 ③④ 上线前的硬阻塞 | 已登记在 `pending-experiments.md` 与 `modules/README.md` §4.2 |
| 6 | `docs/plans/ARCHIVE.md` 曾被 `modules/policy.md` 引用（本轮看到 `../plans/ARCHIVE.md`） | 若该文件不存在就是悬空链接 | `make trace` 零错误 ⇒ 存在；无需动作 |

### 7.1 审视记录（技能 `audit` §5 的审视表）

> 证据列只写**命令或 `文件:行`**；没有证据的发现不算发现。

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `kb/capabilities.md:74` 写「真实模型后端 ❌ **未接入（阶段 B）**」 | 状态过期 | `analysis/worker.py` 的 `--llm` 路径 + 实跑 `模型回落 3 步`；[ADR-0031](../background/decisions/0031-analysis-reuse-and-model-backend.md) | 修正 | ✅ 拆成「L4 三任务已接 / `kind=content` 未接」 |
| 2 | `kb/capabilities.md:59` 标题「模型后端待接」 | 状态过期 | 同上 | 修正 | ✅ |
| 3 | `kb/capabilities.md:110` 缺口 4「真实模型后端未接入」 | 状态过期 | 同上 | 修正 | ✅ 限定为 `kind=content` |
| 4 | `kb/capabilities.md:15` proxy「**58 个测试函数**」 | 数字过期 | `grep -c '^func Test' modules/deception/proxy/*_test.go` → **75** | 修正（去数字给命令） | ✅ |
| 5 | `kb/capabilities.md:52` `config.example.yaml`「153 行」· `spec/config.md`「429 行」 | 数字过期 | `wc -l` → **176** / **448** | 修正 | ✅ |
| 6 | `kb/ai-capabilities.md:9` 「其中**没有任何一条生产链路真的在调模型**」 | 语义过期 | 同上 #1 | 修正 | ✅ 改为「首版如此…2026-09-21 起 L4 已有模型路」 |
| 7 | `kb/ai-capabilities.md:157` chain「只有提示词模板里写了它，而**那份模板未接通**」 | 状态过期 | 三个模板已迁入 `analysis/aicap/resources/prompts/` 并登记（上一轮） | 修正 | ✅ 并补「`conclusion` 已删、`rationale` 是新增受检字段」 |
| 8 | `kb/ai-capabilities.md` §8.5 **#17** 仍写「本轮未动（纯文档轮）；建议单开一个小轮」 | 状态过期 | `analysis/llm/contract.py` 的 `Field.allowed` + `test_llm_contract.py::test_ar15_allowed_*` | 修正 | ✅ 标为已修（2026-09-21），并保留「列表元素闭集仍无守卫」这条真缺口 |
| 9 | `kb/ai-capabilities.md` §8.5 #15「schema 是自研的 **74 行**迷你校验器」 | 数字过期 | `wc -l analysis/llm/contract.py` → **88** | 修正 | ✅ |
| 10 | `kb/README.md:21` · `kb/capabilities.md:62` 「**16 条**优化点候选」 | 数字过期 | `sed -n '/^## 8/,/^## 9/p' docs/kb/ai-capabilities.md \| grep -cE '^\| [0-9]+ '` → **17** | 修正（索引处去计数） | ✅ |
| 11 | `modules/_map.md:78` 「AI 框架（PyTorch / vLLM / Transformers / **NetworkX**）属**规划**，**尚未引入**」 | 与决策矛盾 | [ADR-0031](../background/decisions/0031-analysis-reuse-and-model-backend.md) 决定 4（NetworkX ⛔、零新增依赖）；`analysis/requirements.txt` 仍 3 项 | 修正 | ✅ 改为「**不引**…模型经自写标准库适配器」 |
| 12 | `modules/README.md` §4 整节：「`policy` 下发面**仍未接线**」·「`isolation` …**当前无人调用**」·「`responder` …**无预留接缝**」·「`decoy` / `honeypot` **设计中**」·「`console` TypeScript 需要 `policy` 下发契约」·§4.4「阶段 3…依赖阶段 2 完成」·§4.5 建议顺序列 2–5 待做 | 状态过期 **+ 自相矛盾** | `policy/server.go:143/180`（`Pull`/`Ack`）· `common/core/cmd/core/main.go:179`（`control.WithIsolation`）· `responder/iface.go:29`（`Responder` 接口）· `decoy` 13 个测试函数 · `honeypot` 15 个 · `console` 11 个接口注册；**且同一文件 §0.4 写着「策略面（`S4`）已落地」** | 修正 + **删除**重复的现状列与已完成路线图 | ✅ 改为「插在哪 / 前置条件」，进度指回 `progress.md` |
| 13 | `modules/README.md:245` `adapter-proxy`「**39 测试**」 | 数字过期 | `grep -c '^func Test'` → **75** | 修正 | ✅ |
| 14 | `modules/README.md:216` 「尚未并入 … §1.1 的 **21 行**清单」 | 历史陈述易误读 | `docs/design/modules.md` §1.1 现为 25 行 / 24 有效模块 | 修正 | ✅ 加「（当时 21 行）」+ 现状 |
| 15 | `modules/adapter-proxy.md:9` 「**39 测试**」 | 数字过期 | 同上 #13 | 修正 | ✅ |
| 16 | `modules/control.md:40` 「`common/api/policy/v1`（**阶段 2 未实现**）」 | 状态过期 | `policy/server.go:143/180`；`policy.proto:9-11` 已有 `Pull`/`Watch`/`Ack` | 修正 | ✅ |
| 17 | `modules/control.md:96` §8#2「策略面（`Pull`/`Watch`/`Ack`）未实现」 | 状态过期 | 同上（`Watch` 确实未实现，其余已实现） | 修正 | ✅ |
| 18 | `modules/decoy.md:131` §8#6「**策略面（`common/api/policy/v1`）未实现**」 | 状态过期（真因判错） | 同上 + `modules/README.md` §0.4（真因＝资产来源与归属未定） | 修正 | ✅ |
| 19 | `modules/responder.md:107` §8#4「而**该面未实现**」 | 状态过期（真因判错） | 同上；真因＝本模块 `(会话,资源)` 运行期缓存通路 | 修正 | ✅ |
| 20 | `modules/director.md:119` §8#3「`whitelist` 消费（`INT-25`）…**本轮未接**」 | 状态过期 | `common/core/cmd/core/main.go:134/140`（`loader.Whitelist()` → `Whitelist:`）· `director.go:111`（`whitelisted` 先于引流判定）· `:154` 实现 · 单测 `TestWhitelistByUserAgentSkipsJudgement` | 修正 | ✅ 标为已实现并给证据链 |
| 21 | `spec/config.md:21` 「运行时变更属策略下发面（**阶段 2 未实现**）」 | 状态过期 | 同上 #16 | 修正 | ✅ |
| 22 | `spec/config.md:56` `decoys` 行「⚠️ **边缘还取不到**（策略面 `S4` 未实现）」 | 状态过期（真因判错） | 同上 #18 | 修正 | ✅ |
| 23 | `spec/config.md:180` 「策略面 `S4` 未实现前，适配器拿不到诱饵」 | 状态过期（真因判错） | 同上 #18 | 修正 | ✅ |
| 24 | `ops/functional-verification.md:89` 缺口 #6「**白名单（`INT-25`）未消费**：配置里 `whitelist` 段只解析与校验」 | 状态过期（缺口已闭） | 同上 #20 | 修正为**已闭合** | ✅ 并写明原 `NI-1` 风险已消 |
| 25 | `kb/quick-tour.md:107` 缺口清单含「白名单未消费」 | 状态过期 | 同上 | 修正 | ✅ |
| 26 | `progress.md` §1 五行易漂计数（`policy`「3 文件 + 2 单测（22 例）」· `control`「3 文件 + 1 单测」· `telemetry`「2 文件 + 1 单测」· `responder`「2 文件 + 1 单测」· `adapter-proxy`「39 测试」） | 数字过期 | 实测：`policy` 4 源码 + 3 测试文件 / 36 测试函数；`control` 6 + 4；`telemetry` 3 + 2；`responder` 3 + 1；`adapter-proxy` 75 测试函数 | 修正（统一去数字 + 写约定） | ✅ |
| 27 | `progress.md` §2.1 两处计数（`policy` 36 · `adapter-proxy` 75）**当前正确**但同类易漂 | 数字易漂（预防） | 同上实测（此刻相符） | 修正（去数字，保持一致） | ✅ |
| 28 | `background/notes/pending-experiments.md:6` 状态行「`E2` **未执行**」 | 状态过期 | `background/research/e2-tls-fingerprint/` 两组 JSON（2026-09-19）· [ADR-0019](../background/decisions/0019-tls-termination-belongs-to-l0.md) 第 1 行「由 `E2` 实测驱动的重估」· 同文件「待办状态」表 `E2` 行已写「主臂初测已跑」 | 修正 | ✅ |
| 29 | 同文件的 **E2 结果段错位**：它被放在 `## E3` 的「成本」之后（`### 结果 · 2026-09-19（**主臂初测**，非最终结论）`），而 [ADR-0019](../background/decisions/0019-tls-termination-belongs-to-l0.md) 第 9 行与 `docs/background/research/README.md` §0.1 都指向「E2 的结果段」 | 位置错（读起来像 E3 的第二次结果、且让 E2 看上去没有结果） | `grep -n "^## \|^### " docs/background/notes/pending-experiments.md` → 该「主臂初测」结果落在 `## E3` 之后 | **改正为「搬迁」**：把那一段原样移到 E2 的「成本」之后（**不新增第二份**） | ✅ |
| 30 | 同文件「待办状态」表 `E3` 行「⏳ **未执行**（2026-09-19 登记）」 | 状态过期 | 同文件 E3「结果 · 2026-09-19（空载下界）」段 | 修正 | ✅ 改为「空载下界已跑 · 验收证据仍缺」 |
| 31 | 同文件末注「⚠️ **E2 建议在写第一行业务代码之前跑掉** —— 如果结论是「无法对齐」，本项目的欺骗命题需要整体重新评估」 | 状态过期 | E2 已执行；实际应对是 [ADR-0019](../background/decisions/0019-tls-termination-belongs-to-l0.md)（TLS 交 L0），不是整体重估 | 修正 | ✅ |
| 32 | 我在改 §4 时**一并删掉了「非模块阻塞项」注**（`E2` / `D0`） | 漏写（本轮自己引入） | 原文末注；`D0` 在 `implementation-discussion.md:225` 仍为「⏳ 待拍板」 | 修正 | ✅ 补回为 §4.2，按最新状态重写（`E2` 已闭合 · `E3` 未验收 · `D0` 待拍板） |
| 33 | `docs/README.md:107` 引用「`modules/README.md` §4 …**建议顺序**」 | 悬空引用（被删的节） | `grep -rn "建议顺序" docs/` → 命中该行 | 修正 | ✅ 改为「§4 插在哪 / 前置条件」+ §4.2 |
| 34 | `modules/deception/mirror` 导出 `JudgeClient` / `TelemetryClient` 但无 `iface.go` | 结构漂移（提示级） | `grep -n '^type [A-Z]' modules/deception/mirror/receiver.go` → 两个接口；`make trace` 出 `TC-1` 提示 | **保留**（要改代码；且该约定未升格为规则） | 已记录 → §7 遗留 1 |
| 35 | 本轮我又踩了一次 `K-27`（把 `llm/client.py` 写成非仓库存根路径） | —— | `make trace` 当场报 `DEV-2 llm/client.py` | 修正（改为 `analysis/llm/client.py`） | ✅ 已改；**`K-27` 这条记录有效**，无需修改 |
| 36 | `modules/ai-capability.md:50` 也写着「**16 条**优化点候选」 | 数字过期（漏改） | `grep -rn "16 条优化点" docs/kb docs/modules` → 命中该行（本轮**自检阶段**发现：它不在我最初的改动清单里） | 修正 | ✅ 去掉计数；并列入 §4 改动清单 |
| 37 | `modules/control.md:96` 我改时写成「✅ **已实立**」 | 笔误 | 该行文本 | 修正 | ✅ 改为「已实现」 |
| 38 | **我自己的证据行曾经不复现**：§6 原写「`grep "策略面.*未实现" docs/` → 0 命中」，实跑却命中（历史日志 + 本变更包自引 + 我故意保留的 `~~删除线~~` 旧句） | 证据不实（自评） | 实跑该 grep 的输出 | 修正 | ✅ 重新限定扫描集并把**真实输出**贴回 §6；§5/§6 的判据改成「不再是当前陈述」而非「grep 为 0」 |

| 39 | **`AGENTS.md:152`** 仍写「`docs/modules/README.md` §4（每个模块插在哪 · **建议顺序**）」，而该节已被本轮删除 —— 它在仓根、**每次会话都进上下文** | 删除门槛①**未真正满足**（悬空引用 + 漏引） | 独立评审 P1；`grep -rn "建议顺序\|下一步可选模块" .`（**首轮只搜了 `docs/`**，漏仓根） | 修正（全仓重搜 + 改 `AGENTS.md` + 在 §4.1 记明首轮范围不足） | ✅ |
| 40 | 我「补写」的 E2 结果段与**已存在**的那段（错位在 `## E3` 之后）**重复**，且对同一数据给出**相反判定**：我把 OCSP `True/False` 当成「第二处独立信号」，而原件基于实测把它标为「⚠️ 可配置项」——B 组 `false` 是**自签测试证书**所致（无 OCSP responder URL），我自己的局限 3 已承认证书差异是测试环境产物 | **我引入的不准确**（过度声称）+ 重复 | 独立评审 P1；`ocsp_stapled` 在两组 JSON 里的值与「自签测试证书」这一事实 | 修正（删掉我的复制件，只保留原件；OCSP 不作为栈级证据） | ✅ |
| 41 | `docs/modules/README.md:5` 仍自称「本文件回答：… · **下一步做什么** · …」，而 §4 已改写为「不维护进度」、路线图已删 | 自述过期（本轮自己造成的） | 该行文本 vs 新 §4 | 修正（改为「新模块插在哪」） | ✅ |
| 42 | `docs/design/structure.md:148` 的「**39 测试**」（同一数字我在别处修了、却没在册）；同文件 `:154` 的「**24 例测试**」实际已是 **118 例** | 数字过期 + 门槛①自述「只在本行出现」不成立 | 独立评审 P2；`grep -c '^func Test'` = 75 · `make pytest` = 118 | 修正（三处描述性计数一并去掉、改用命令取数）+ §4.2 单独记录 | ✅ |
| 43 | §6 原把判据写成 `grep … $IN` / `<现行文档集>` 占位 —— 读者**无法原样复跑**；且我首版写「0 命中」，实跑会命中（历史行 + 我的审视记录行 + 故意保留的 `~~删除线~~` 旧句） | 证据不可复跑（自评） | 独立评审 P2；实跑输出 | 修正（写死扫描集与完整命令 + 贴真实输出 + 判据改为「不再是当前陈述」） | ✅ |
| 44 | 本包内部数字互斥：§2 写「33 条发现」、§8 写「35 条」、§7.1 实为 38 行；§6 曾写「116 passed in 2.30s」而实测是 **118** | 记录不一致 | 独立评审 P2（它认为 116 才对 —— **这一点它错了**：`analysis/.venv/bin/pytest --collect-only -q` → `118 tests collected`、`make pytest` → `118 passed`；上一轮的 P1 修复新增了 2 个回归用例） | 修正（统一为 38 条 / 118 例，并贴真实输出行） | ✅ |
| 45 | `docs/modules/honeypot-protocol.md:9` 状态行「**7 例单测**」（现役正文里的易漂计数） | 数字易漂（预防） | `grep -rn "7 例单测"` 全扫 | 修正（去掉计数） | ✅ |

> 机器能核的四类（悬空引用 · 孤儿文档 · 过期状态标记 · 过期豁免）由 `make trace` 覆盖，本轮零错误。
> 历史记录类文件按规范**排除在审视范围外**（见 §1「不做什么」）。

### 7.2 独立评审（L 档）

评审者：冷上下文子代理（`reviewer`，只读文件与证据，不看作者推理）。
结论与逐条处置见 §7.3。

### 7.3 评审结论

评审者：冷上下文子代理（`reviewer`，只读文件与代码，**无 shell**，因此它自己声明「未能跑 `make gate` / `make trace` / `git diff`」）。
**结论：有异议 —— 2 条 P1 + 5 条 P2，全部已处置。**

| # | 级别 | 评审意见 | 处置 |
| --- | --- | --- | --- |
| 1 | **P1** | `AGENTS.md:152` 仍引用被删的「建议顺序」；删除门槛① 只搜了 `docs/`，**漏了仓根** ⇒ 门槛①实际未满足 | ✅ 全仓重搜（含 `AGENTS.md`/`*.go`/`*.sh`/`Makefile`）→ 已改 `AGENTS.md` + §4.1 记明首轮范围不足（§7.1 #39） |
| 2 | **P1** | 我「补写」的 E2 结果段与**已存在**的那段（错位在 `## E3` 后）重复；且我把 OCSP 装订当成「第二处独立信号」，而原件基于实测标为「⚠️ 可配置项」（B 组 `false` 系自签测试证书所致）——**过度声称** | ✅ 删掉我的复制件、只保留原件（并把它搬到 E2 段下）；OCSP 不作为栈级证据（§7.1 #29/#40） |
| 3 | P2 | §7.1 #29 声称结果段「目标不存在」→ 事实是**存在但错位**，「补写」实为复制 | ✅ 该行事实与动作均已改正（§7.1 #29） |
| 4 | P2 | `docs/modules/README.md:5` 自述仍写「下一步做什么」 | ✅ 改为「新模块插在哪」（§7.1 #41） |
| 5 | P2 | 本包数字互斥（33/35/38 · 118/116） | ✅ 统一为 **38 条 / 118 例**；并记下评审在 116 上判断有误（实测 118）（§7.1 #44） |
| 6 | P2 | `docs/design/structure.md:148` 的「39 测试」未在册（我别处修了、这里没登记） | ✅ 一并修掉 `:148`/`:151`/`:154` 三处描述性计数，并在 §4.2 单独记录（§7.1 #42） |
| 7 | P2 | §6 的命令是占位写法（`$IN`），无法原样复跑 | ✅ 写死扫描集与完整命令 + 贴真实输出（§7.1 #43） |

**评审也逐条核实了不少事**（防止「只报不利消息」）：`policy` 的 `Pull`/`Ack` 确已实现（`Watch` 仍 `Unimplemented`）·
适配器确实消费策略面（`modules/deception/proxy/policy.go` + `TestPolicyAppliedAndAcked`）·
`isolation` 确已接线 · 白名单**确已消费且先于引流判定**（含单测）· `responder` 有 `iface.go` ·
`decoy`/`honeypot` 确非「设计中」· 控制台 11 个只读接口 · worker 有 `--llm` 且注册表确为 4 个 kind ·
**E2 的三组数字与两个 JSON 逐字一致** · 易漂数字实测与 §7.1 #4/#26 相符 · 无残留「下一步可选模块」引用 ·
§3 的规则 ID 在 `docs/design/` 中确实存在。

> **它未能核实的**：门禁是否绿、是否零代码改动（它没有 shell）—— 这两项由本轮 §6 的可复跑命令与 `make gate` 承担。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-21 | 首版：文档准确性审视 —— **45 条记录**（首轮 38 条 + 独立评审追加 7 条）·（状态过期 / 数字过期 / 漏写 / 错位 / 悬空引用 / 我自己的过度声称）；删除 `modules/README.md` §4 的重复现状列与已完成路线图；把错位的 E2 结果段搬回 E2 | 用户要求「检查相关文档内容确保准确、删除已过时信息」；技能 `audit`（四道删除门槛 · 机器优先） |
