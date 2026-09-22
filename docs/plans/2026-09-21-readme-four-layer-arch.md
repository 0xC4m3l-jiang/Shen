# 变更包：README 换成四层架构图（欺骗层 / 蜜罐层 / web 真实业务层 / AI 模型层）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 入口 README 的架构图改为**用户提的四层视图**；顺带清掉 4 处「模型后端（阶段 B）」的过期说法 |
| 日期 | 2026-09-21 |
| 状态 | 已实现（门禁绿 · 独立评审见 §7.3） |
| 涉及模块 | 文档（入口层）：`README.md` · `docs/README.md` · `docs/ops/functional-verification.md` · `docs/modules/ai-capability.md` |
| 决策数 | 无新决策（**不改 `docs/design/`**，只改「怎么把已确认的分层讲给人听」） |
| 关联 | 上一轮 [`2026-09-21-ai-injection-verification.md`](2026-09-21-ai-injection-verification.md)（模型后端接通）· [`design/language.md`](../design/language.md) §1（规范性分层 L0–L4）· [`modules/_map.md`](../modules/_map.md)（24 模块编号） |

---

## 1. 需求与验收

**要解决什么**（用户原话）：

1. 分析是否**所有要求已经完成**；
2. 在 **README** 里更新**最新架构图** —— 「应该是 **欺骗层、蜜罐层、web 真实业务层、AI 模型层**」。

**验收判据**：

1. `README.md` §1 出现四层架构图，且**四层的名字与用户给的一致**；
2. 图里的每一条关系都能指到代码或已确认规则（不是示意图话术）；
3. **不与 `docs/design/` 的分层冲突**：入口 README 用功能四层，同时给出与规范性 `L0–L4` 的映射；
4. `make gate` / `make trace` 绿（无悬空链接、无过期状态标记）；
5. 两处入口（`README.md` 与 `docs/README.md`）讲的是**同一张图**。

**不做什么**：

- **不改 `docs/design/` 的分层定义**（`L0`–`L4` 是已确认基线；四层视图是**讲法**，不是新规则）；
- 不改任何代码与行为；
- 不重画 `docs/modules/README.md` §0.1 与 `modules/README.md` §0.1（它们讲的是**模块编号视角**，
  与功能四层是两种视角，各自成立；本轮只在两个**入口** README 上统一讲法）。

---

## 2. 设计逻辑

**四层 ↔ 系统实况**（图的依据；每行都能指到代码或规则）：

| 用户说的层 | 系统里是什么 | 依据 |
| --- | --- | --- |
| **① 欺骗层** | `modules/deception/`（适配器：proxy ③前置/④边车 · mirror ①旁路 · dns ②纯配置 · injection 处置）+ **核心**（`common/core/internal/`：judge · director · control · session · isolation · policy · telemetry · store · decoy · honeypot · responder） | `AR-2` / `AR-5`：判定与响应生成**只在这里实现一次**；适配器只执行处置（`AR-7`） |
| **② 蜜罐层** | 核心 `internal/honeypot`（类型注册 + 后端池）· `modules/honeypot/protocol`（协议仿真框架）· 真实蜜罐**接第三方** | [`ADR-0011`](../background/decisions/0011-honeypot-entry-external-backends.md)：只做入口与后端池，不实现具体蜜罐 |
| **③ web 真实业务层** | 被保护的真实业务站（上游 `SHEN_PROXY_UPSTREAM`）—— 放行时**原样透传** | `INT-8`（业务侧零改写）· `NI-1`（引擎挂了它照常服务）· `NI-5`（幻境不可用⇒回落业务） |
| **④ AI 模型层** | `analysis/`：`aicap`（唯一生成出口 + 后置检查）· `llm`（契约纪律 + 云模型适配器）· `intent`/`chain`/`strategy`（近线分析）· `worker`；模型后端 = 云模型（DeepSeek） | `AR-33`（唯一出口）· `AR-29`/`AR-30`（热路径零模型）· [`ADR-0026`](../background/decisions/0026-cloud-model-backend.md) |
| （旁路）观测控制台 | `modules/console`（Go + 静态页，只读） | `MD-10` 只读观测 |
| （L0）接入 | 客户侧云 LB / nginx / Envoy —— **复用现成组件，不自研** | `AR-3` |

**为什么「核心」画在①里**：它是 ① 与 ② **共用**的内核（蜜罐入口也在核心）。
图里以「① 欺骗层 = 适配器 + 核心」呈现，并在图注里写明这一点 —— 避免读者以为核心只服务欺骗层。

**为什么必须给映射**：`docs/design/language.md` §1 定了 `L0`–`L4`，`docs/design/modules.md` §1.1 定了 24 个模块编号；
入口 README 若只讲四层而不给映射，读者按「`L2` 蜜罐」去搜代码会对不上（`L2` 是「协议仿真蜜罐」，
而「蜜罐层」还包括核心的幻境入口与后端池）。故图下附一行映射 + 指到 `design/` 与 `_map.md`。

**顺手修掉的过期说法**（本轮核对发现，四处）：

| # | 位置 | 过期内容 | 为什么过期 |
| --- | --- | --- | --- |
| 1 | `docs/README.md` §0 未接通清单 | 「真实模型后端（阶段 B）」 | 上一轮已接通：L4 三任务与 `kind=content` 都走 `--llm` |
| 2 | `docs/ops/functional-verification.md` §5 | 「未覆盖：真实模型后端（阶段 B）」 | 同上；报告已把它覆盖（仍未做的只是「模型 vs 模板」的质量对照） |
| 3 | `docs/modules/ai-capability.md` §3 | 「Faker/Mimesis 落地属阶段 B（与真实模型后端一起做）」 | 模型后端已接通 ⇒ 两件事不再必须一起做（改为「假数据生成仍属阶段 B」） |
| 4 | `docs/README.md` §0 分层列表 | 只讲 `L0–L4`，与入口 README 的四层讲法不同源 | 补四层视图 + 映射，两处入口同图 |

---

## 3. 追溯矩阵

| 需求 / 规则 | 文档章节 | 代码 / 事实来源 | 测试 / 命令 | 验证命令 |
| --- | --- | --- | --- | --- |
| ① 欺骗层（判定与响应只实现一次） | `README.md` §1 · `docs/README.md` §0 | `modules/deception/proxy` · `common/core/internal/{judge,director,control}` | —— | `make gate` |
| ② 蜜罐层（接入架构 + 第三方） | 同上 | `common/core/internal/honeypot` · `modules/honeypot/protocol` | `TestResolve*/TestNew*` | `make gate` |
| ③ web 真实业务层（放行目标） | 同上 | `SHEN_PROXY_UPSTREAM` 与 proxy 转发 | —— | `make ai-check`（业务侧逐字节不变） |
| ④ AI 模型层（L4 + 云模型） | 同上 | `analysis/aicap` · `analysis/llm/deepseek.py` | `analysis/tests/*` | `make gate` · `make ai-check-llm` |
| 与规范性分层不冲突 | `README.md` §1 图注 · `docs/README.md` §0 | `docs/design/language.md` §1 · `docs/design/modules.md` §1.1 | —— | `make trace` |
| 模型后端不再「待接」 | `docs/README.md` §0 · `docs/ops/functional-verification.md` §5 | `analysis/aicap/tasks/content.py` · `analysis/aicap/model.py` | 内容两条路用例 | `make gate` |

---

## 4. 代码实现（本轮 = 文档改动）

**无代码改动**（`git diff --stat` 只有 `*.md`）。

| 文件 | 改什么 |
| --- | --- |
| `README.md` §1 | ASCII 分层图 → **mermaid 四层图**（① 欺骗层 / ② 蜜罐层 / ③ web 真实业务层 / ④ AI 模型层）+ 三行文字速览 + 与 `L0–L4`、24 模块编号的映射注；保留「三条不可妥协」表 |
| `docs/README.md` §0 | 补**同一张四层视图**（文字版）+ 映射；修掉「真实模型后端（阶段 B）」的过期说法 |
| `docs/ops/functional-verification.md` **§1.1 的「未覆盖」段**（不是 §5；§5 是「方法论边界」，未动） | 去掉「未覆盖：真实模型后端（阶段 B）」，改为指向 §1.0 的报告（仍未做的写明是质量对照）；§1.0 的项数改为分模式口径「模板 21 / 模型 23」 |
| `docs/modules/ai-capability.md` §3 | 「与真实模型后端一起做」→「模型后端已接通，两件事不再必须一起做」 |

---

## 5. 测试与场景

| # | 场景 | 输入 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 1 | mermaid 结构自检 | README 的 mermaid 块 | `flowchart` 起头 · `subgraph`/`end` 配对 · 引号/方括号成对 · 边只引用已声明节点 | ✅ 2/2 配对 · 11 节点 · 13 边 · 未声明 0（仅边标签 `HTTPS`，合法） | §6 ② |
| 2 | 四层名字与用户给的一致 | 图 | ① 欺骗层 · ② 蜜罐层 · ③ web 真实业务层 · ④ AI 模型层 | ✅ | `README.md` §1 |
| 3 | 两处入口同图 | `README.md` 与 `docs/README.md` | 讲法一致（四层 + 映射） | ✅ | 两文件 §0/§1 |
| 4 | 无悬空链接 / 过期状态标记 | 全 docs | `make trace` 零错误 | ✅ | §6 ① |
| 5 | 门禁 | —— | 全绿（127 例 + 结构/追溯/泄漏/许可） | ✅ | §6 ① |

**没有覆盖的**：mermaid 的**渲染**效果（本机无渲染器）—— 只做了结构自检；GitHub/编辑器按标准语法渲染。
若你在某处看不到图，把它当「渲染器不支持」处理，文字速览块是同等内容的降级形态。

---

## 6. 验证证据

**① `make trace` + `make gate`**

```console
$ make trace
追溯检查通过。（无悬空链接 / 无过期状态标记 / 无过期豁免）
$ make gate
127 passed in 3.29s
门禁通过。
```

**② mermaid 结构自检（脚本）**

```console
首行: flowchart TB
subgraph=2 end=2 → 配对
节点定义: 11 · 边引用里未定义的节点: 无（唯一命中是未加引号的边标签 HTTPS）
边数: 13
```

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 四层视图**只在两个入口 README** 用了；`docs/modules/README.md` 与 `modules/README.md` 仍是模块编号视角 | 两种视角并存（各有用途：功能定位 vs 查代码） | 本轮刻意如此；若你希望全仓统一讲法，说一声即可（那要连 `design/` 一起定，属规则级改动） |
| 2 | 未做 mermaid 渲染验证 | 极端情况下某渲染器不认某语法 | 文字速览块是等价降级；§5 场景 1 已做结构自检 |
| 3 | 「web 真实业务层」不是软件层而是**被保护对象** | 读者可能以为是我们要开发的模块 | 图注与 `docs/README.md` 都写明「**不是**我们的层」 |

### 7.1 审视记录（M 档）

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `docs/README.md` 把「真实模型后端（阶段 B）」列为未接通 | 状态过期 | 上一轮 `generator=model-v1` 实测 + `--llm` 路径 | 修正 | ✅ 改为「已接通」并链到报告 |
| 2 | `docs/ops/functional-verification.md` 同款过期 | 状态过期 | 同上 | 修正 | ✅ |
| 3 | `docs/modules/ai-capability.md` 的「与真实模型后端一起做」 | 表述过期 | 同上 | 修正 | ✅ |
| 4 | 入口 README 的分层讲法与 `design/` 的 `L0–L4` 不同名 | 潜在冲突 | 用户给的四层名 vs `design/language.md` §1 | 修正 | ✅ 图下加映射，指明规范分层在哪 |
| 5 | `docs/README.md` 5 条 markdown 告警（ambiguous link ×4 · MD060 ×1） | 既有（非本轮引入） | 对 HEAD 版本复核：同款告警已存在 | 保留 | 写在这里；修它属另一轮（与本次诉求无关） |
| 6 | 我给的 `①②③④` 号数与仓库**已确认的编号撞了**：`docs/modules/_map.md` §1.1 与 `design/structure.md` 用 `① 欺骗层 / ② AI 蜜罐层 / ③ 管控平台` 表示**三个大模块** | 编号冲突（我引入；违反本轮验收 3「不与 design/ 冲突」） | 独立评审 P1：按「③」去查会查到 `console` | 修正 | ✅ **把号数全删掉**（用户要的是四个**名字**，号数是我多加的）；并在图注写明「那套 ① ② ③ 是三个大模块的编号，与本图不是同一套」 |
| 7 | 映射注漏了 **`L3`**（`AR-1` 要求按 `L0–L4` 五层划分，而 `L3` 有代码：`modules/deception/netpolicy`） | 映射不完整（评审 P1） | 评审指出「映射注是防冲突的唯一手段，却对 L3 沉默」 | 修正 | ✅ 图注补「另有两块不在这张四层图里：`L3` 网络欺骗 + 管控平台」，并加 `analysis/aicap` 两处都沾的说明 |
| 8 | 图里「适配器 L1（内嵌 Caddy 承担转发与 TLS）」只对 `proxy` 成立；`mirror` 恒回 202 且不在请求路径、`dns` 是纯配置 | 表述过宽（评审 P2） | `modules/deception/mirror/receiver.go` | 修正 | ✅ 改为「这里画的是 ③ 前置 / ④ 边车」，并点出另两种形态的位置特性 |
| 9 | 箭头把「aicap 写文件 → 核心装载 → 适配器 Pull」压成一条且起点不是发送方；缺 `CO→AD`（策略面）与 `NEAR→CO`（结论回写） | 语义不准（评审 P2） | 评审逐条核了 `handler.go` / `policy.go` / `worker.py` | 修正 | ✅ 拆成 `GEN→CO`（离线产出文件）+ `CO→AD`（策略面 Pull：后端表/白名单/注入规则/内容清单）+ `NEAR -.-> CO`（结论事件回写） |
| 10 | `docs/modules/ai-capability.md:9` 状态行自相矛盾（同格既写「模型后端属阶段 B」又写「模型后端已接」） | 自相矛盾（评审 P2；我只改了 §3 漏了状态行） | 该行原文 | 修正 | ✅ 删掉「模型后端与」 |
| 11 | `docs/ops/functional-verification.md` 同文件两个数（§1.0「21 项」vs §1.1「23 项」） | 数字口径不一（评审 P2） | 报告与 `check-log.txt` 均为 23（模型模式）；模板模式 21 | 修正 | ✅ §1.0 改为「模板模式 21 项 / 模型模式 23 项」 |
| 12 | 变更包 §4 把功能验证的改动位置写成「§5」 | 指针错（评审 P2） | 实际改在 §1.1 的「未覆盖」段；§5 是「方法论边界」，未动 | 修正 | ✅ 指针与范围写清 |

### 7.2 独立评审（M 档）

评审者：冷上下文子代理（只读文件；不看作者推理）。结论见 §7.3。

### 7.3 评审结论

评审者：冷上下文子代理（只读文件与代码）。**结论：有异议 —— 2 条 P1 + 5 条 P2，全部已处置。**

| # | 级别 | 评审意见 | 处置 |
| --- | --- | --- | --- |
| 1 | **P1** | 我的 `①②③④` 与**已确认的模块编号**撞号（`_map.md` §1.1 的 `① 欺骗层 / ② AI 蜜罐层 / ③ 管控平台`）⇒ 按「③」查会查到 `console` | ✅ 号数全删（§7.1 #6） |
| 2 | **P1** | 映射注漏 `L3`，而 `AR-1` 要求按 `L0–L4` 五层划分，`L3` 有代码（`netpolicy`） | ✅ 图注补 L3 与管控平台，并写明 `aicap` 两处都沾（§7.1 #7） |
| 3 | P2 | 「适配器 L1 + 内嵌 Caddy」只对 `proxy` 成立（`mirror` 不在请求路径、`dns` 是配置） | ✅ 标签改准（§7.1 #8） |
| 4 | P2 | 箭头把三步压成一条且起点错；缺策略面与结论回写两条 | ✅ 拆成三条真箭头（§7.1 #9） |
| 5 | P2 | `ai-capability.md:9` 状态行自相矛盾 | ✅ 修正（§7.1 #10） |
| 6 | P2 | `functional-verification.md` 同文件 21 vs 23 | ✅ 改为分模式口径（§7.1 #11） |
| 7 | P2 | 变更包 §4 指针错 | ✅ 修正（§7.1 #12） |

**评审核实为真的**（逐条给了代码位置）：适配器五步顺序（`handler.go:341/348/decision/543`）·
核心 12 个包中图里点名的 11 个都在（未画的 `contract` 是共享类型、非模块）· 蜜罐层（`honeypot.go` 的 `KnownTypes`/`Resolve` + `ADR-0011`）·
web 真实业务层（`INT-8`/`NI-1`/`NI-5`）· AI 模型层（`--llm` 缺 key 退出 2 · 回落标注 · `deepseek.py` 标准库）·
**七条箭头全部能指到代码** · mermaid 可解析（`subgraph`/`end` 配对、11 节点先定义后引用、13 边、引号成对）。

**它未能核实的**（我补核）：① 「无代码改动」——`git status --short` 只有 `*.md`（本包 §6 已列）；
② `docs/README.md` 的 5 条告警是**既有**——对 `HEAD` 版本复核过，同款告警已在；
③ mermaid 的**实际渲染**（本机无渲染器）—— 只做结构自检，图下有等价文字速览。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-21 | **评审后修正（7 条全处置）**：号数全删（不与模块编号撞）· 图注补 `L3`/管控平台/`aicap` 重叠 · 适配器标签改准 · 箭头拆成三条真关系 · 修 `ai-capability` 状态行自相矛盾 · 项数改分模式口径 · 变更包指针修正 | 独立评审 2 P1 + 5 P2（§7.3）· `AR-1`（五层划分）· `_map.md` §1.1（模块编号） |
| 2026-09-21 | 入口 README 架构图改为四层视图（欺骗层/蜜罐层/web 真实业务层/AI 模型层）+ 与 `L0–L4` 的映射；`docs/README.md` 同步；修掉 4 处「模型后端（阶段 B）」过期说法 | 用户要求「在 readme 中更新最新架构图，应该是欺骗层、蜜罐层、web真实业务层、AI模型层」 |
