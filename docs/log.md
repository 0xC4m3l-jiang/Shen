# 变更日志

> **每轮开发追加一条**，最新在最上面。用途：事后复核「这轮到底改了什么、对应哪份文档、验证过没有」。
> 形状固定（`make trace` 会核对最新条目）：做了什么 · 改了哪些文件 · 对应文档 · 验证 · 证据 · 遗留。
> 写法见 [`.pi/skills/dev-loop/SKILL.md`](../.pi/skills/dev-loop/SKILL.md)；
> 完整变更包在 [`plans/`](plans/README.md)，本文件只留索引与结果。
>
> 读者视角优先：**先给结论与能验证的东西，再给细节**。

---

## 2026-09-20 · AI 能力服务（模块 25）+ 欺骗内容注入通路（阶段 A：通路 · 开关 · 强制护栏）

**做了什么**：新增 L4 模块 **`ai-capability`（第 25 行）**——一个**可开关的共享生成出口**：
唯一入口 `generate(TaskSpec) → Envelope`，**内部强制走护栏**（前置三段式提示词 + 后置四关：结构 / 黑名单 / 长度 / 风格）；
未登记的任务种类必拒、缺护栏档案**启动期就失败**。同时接通**欺骗内容通路**：
离线生成 → 过护栏 → 清单文件 → 核心装载进 `store.ContentStore`（该接口的**首个真实消费方**）→
策略载荷 `content_manifest` 下发 → 适配器按（资源精确匹配 + **会话哈希**）确定性命中、**会话钉定**、注入前再验校验和 →
`edge/injection` 写入**改道侧**响应。三层开关默认**全关**（`ai.enabled` · `inject_enabled` · `SHEN_PROXY_INJECT_CONTENT`），
不打开时行为与以前**逐字节一致**。
新增规则 **`AR-33`**（生成必须经护栏出口，判据 = 门禁结构检查 + 单测）与 [ADR-0023](background/decisions/0023-deception-content-injection.md)。

**改了哪些文件**：新增 `analysis/aicap/`（service / model / content / tasks / guardrail / resources）· `analysis/tests/test_aicap_guardrail.py` · `analysis/tests/test_aicap_content.py` · `core/internal/contract/content.go` · `core/internal/policy/ai.go` · `core/internal/policy/ai_test.go` · `edge/proxy/content.go` · `edge/proxy/content_test.go` · `scripts/dev/ai-inject-check.py` · `docs/spec/ai-contract.md` · `docs/modules/ai-capability.md` · `docs/background/decisions/0023-deception-content-injection.md`；修改 `analysis/llm/limits.py` · `analysis/pyproject.toml` · `core/internal/policy/policy.go` · `core/internal/policy/server.go` · `core/cmd/core/main.go` · `edge/proxy/policy.go` · `edge/proxy/handler.go` · `edge/proxy/cmd/proxy/main.go` · `console/internal/topology/topology.go` · `scripts/archcheck/main.go` · `scripts/traffic/send.py` · `Makefile` · `api/telemetry/v1/testdata/request_judged_event.json` · `deploy/config/config.example.yaml` · `docs/design/`（modules / structure / architecture / README）· `docs/spec/`（policy-payload / events / config / README）· `docs/modules/`（adapter-proxy / policy / store / console / llm-components）· `docs/kb/capabilities.md` · `docs/kb/quick-tour.md` · `docs/progress.md` · `docs/modules/_map.md` · `docs/modules/README.md` · `docs/README.md` · `.pi/devloop.md` · `docs/background/decisions/README.md`

**对应文档**：`docs/plans/2026-09-20-ai-capability-guardrail.md`（含追溯矩阵与审视 15 条）· `docs/spec/ai-contract.md` · `docs/modules/ai-capability.md` · `docs/design/architecture.md` §5 的 `AR-33`

**验证**：`make gate` 通过（含 `make archcheck` 的 `AR-33` 项与 71 例 pytest）· `make dev` 通过 ·
`make ai-check` **17/17 通过**（连续 4 次一致）· `AR-33` 结构检查做了正/负两例（含 `from analysis.llm import client` 这种等价写法）。
**独立评审**（冷上下文 `reviewer`，抽查 24 条 ✅ 断言）：**有异议 5 条，已全部修完** —— 内容库值的契约描述 · 会话钉定语义 · `inject` 四值的失败表 · `ai.kinds` 关闭态漏校 · `AR-33` 正则漏一种等价写法（其中第 4 条修的是**实现**）。

**证据**：

| 判据 | 期望 | 实测 |
| --- | --- | --- |
| ① 关闭态 | 与未注入基线逐字节一致 | ✅ 改道侧 `9eef5471…` == 幻境直连；业务侧 `3e535d75…` == 业务直连 |
| ② 打开态 | 改道侧含注入内容；业务侧不变 | ✅ `<html><body>MIRAGE-BACKEND<section class="service-detail">…`（40 → 374 字节）；业务侧 sha256 不变（`INT-8`） |
| ③ `AR-30` | 同会话一致 + 跨会话分布 | ✅ 同会话三次长度 [374,374,374] 且 sha256 相同；16 会话命中 **8** 个变体（N=8） |
| ④ 关卡 | 未过护栏的内容零入库 | ✅ CLI 退出码 1、**不写清单**；单测覆盖四关各自拒（含注入的真实标识） |
| ⑤ 秒级关闭 | 不重启适配器即停止注入 | ✅ 换核心后适配器 pid 不变，下一条请求 `inject=disabled` 且响应回到 40 字节原样 |
| ⑥ 观测 | DAG 有注入跳 | ✅ 「内容注入 (L1)」五段文字齐全；逐请求事件 `inject=applied` + `content_id`（20 条） |
| ⑦ 门禁 | 绿 | ✅ `make gate` / `make dev` / `make ai-check` 全绿 |
| `AR-33` 结构检查 | 能拦也能放 | ✅ 故意在 `analysis/intent/` 用两种等价写法 import 模型客户端 → 门禁报 `AR-33`；删掉即过 |
| 独立评审 | 异议全修 | ✅ 5 条异议（#29–#33）逐条改完并重跑门禁/验收 |

**没做 / 遗留**：① **真实模型后端**未接（阶段 A 是确定性模板生成器：阶段 B 接模型 + 风格画像 + PII 检测，引入前要过许可证台账 `TB-16`）；
② **诱饵资产到边缘的通路**仍未接（本轮接的是 AI 内容，另一条通路）；③ 识破信号 → 清单 `version` +1 的轮换接线未接；
④ **核**心侧开关变更需重启核心**（适配器侧不重启即生效）—— 配置只在启动装载；⑤ 清单纯量上限与分片拉取（阶段 B）。
去向：[ADR-0023](background/decisions/0023-deception-content-injection.md) 未解决 1–6 · `docs/kb/capabilities.md` §1.5/§3。

---

## 2026-09-20 · 五维能力审计（反代 / 转发 / 监控 / 配置 / AI 注入）+ KB 能力实况

**做了什么**：用户要求 review"设计的功能是否都开发好了"，点名五个维度，并在确认后完善 KB。
① **逐项审计**（代码 / 测试 / 活体三类证据，不靠印象）：反向代理 ✅（edge/proxy 14 文件 · **58 个测试** · 内嵌 Caddy 模块 · 边界用例：WebSocket 101 / SSE 不缓冲 / 2 MiB 响应不注入 / 8 MiB 上传不丢字节）；流量转发 ✅（原样透传 INT-8 · 改道转发且注入只在改道侧 · 回落 NI-5 · 拦截 403 默认关）；流量监控 ✅（控制台 8 个只读接口 · 逐判定日志 · 逐请求 DAG 每步三段 · 两段式告警 · 指标规格 · 接入自检五项）；配置 ✅（示例 153 行 + 规格 429 行 + 漂移守卫单测 + 策略载荷规格 + env 表 + compose + 验证配方）。
② **一处诚实的"部分实现"**：**注入 AI 欺骗信息** —— 注入*机制*完整（edge/injection + 策略面 inject_rules）、诱饵资产定义与多态、响应一致性 AR-30、LLM 契约层（AR-15…AR-24 · AR-31/AR-32）都在；**但没有真实模型后端**（UnconfiguredClient 显式失败），且 **L4 结论未接到 policy/注入** ⇒ 目前注入的是**静态片段**，不是 AI 现场生成的内容。关闭路径两步：部署侧注入真实 AnalysisClient；把 L4 结论经 policy 接成 inject_rules。
③ **KB 完善**：新增 docs/kb/capabilities.md（五维实况：状态 · 代码 · 技术点 · 怎么验；其余能力速查 9 项；**已知缺口 8 条**含 AI 注入的两步关闭路径）；kb/README 与 quick-tour 索引同步。
④ 修掉我插入的两处表格列数不匹配（kb/README、quick-tour）。

**改了哪些文件**：docs/kb/capabilities.md（新增）· docs/kb/README.md · docs/kb/quick-tour.md · 本变更包

**对应文档**：docs/plans/2026-09-20-capability-audit-and-kb.md（含审计方法与审视 4 条）· docs/kb/capabilities.md

**验证**：`make gate` 通过。

**证据**：
| 维度 | 证据 |
| --- | --- |
| 反向代理 | edge/proxy 测试函数 58 · Caddy 模块 http.handlers.shen_proxy |
| 流量转发 | executed 七值全部实测（origin·cache·failopen·origin_fallback·mirage·whitelist·block） |
| 流量监控 | 控制台 8 个只读接口 · DAG 逐请求链路每步三段 |
| 配置 | 示例配置 153 行 · 配置规格 429 行 · TestExampleConfigLoads 漂移守卫 |
| AI 注入 | ⚠️ 部分：机制在（injection + inject_rules）· 内容不在（UnconfiguredClient） |

**没做 / 遗留**：① AI 生成内容未接入（无模型后端）· L4 结论未接 policy/注入 —— 两步关闭路径已写入 KB；② 诱饵资产 / 预生成正文 → 边缘的通路仍未接通；③ 其余缺口（查询串不参与判定 · 编码绕过 · 前缀精度 · severity 档位 · 真实存储）见 capabilities.md §3。

---

## 2026-09-20 · 已实现计划归档删除（53 份）+ kb 历史文档合并

**做了什么**：用户要"检查 plans 里的功能设计是否已实现，已实现的把 plan 删掉"，并整理其他文档（含 kb）。
① **先定判定方法**（不靠计划自称的状态）：**带目录前缀的仓库路径是否全部存在** + **是否有对应日志条目与门禁记录** + 实现位置能否在 modules/spec/代码里查到。
初版审计用"计划状态行"判定（53/53 都写已验证）→ 判为不可靠；换用上面两条可核证据后：41 份直接判定，12 份逐条人工确认（都是**正文里的反例路径**如 core/internal/xxx/、已改名文件（docs/modules/map.md → _map.md）、已删除文件（manual-test.md 已并入 ops）、或设计里明确"待建"的 docs/analytics/）。
② **归档并删除 53 份计划**：新增 docs/plans/ARCHIVE.md（一行一份：日期 · 原文件 · 主题 · 状态 · 追溯入口 + 判定依据 + **原文取回方式** git show），随后 git rm 53 份；目录 55 → 3（README · 模板 · 归档表）。
③ **kb 合并**：`removed-skills.md`（历史：第三方技能为什么移除、怎么恢复）并入 `known-issues.md` 的 `H-1`，删除原文件；引用者（kb/README · dev-workflow · AGENTS.md）改指 `known-issues.md` 的 H-1；kb 6 → 5 份。
④ **索引改准**：docs/plans/README 写明目录策略（已实现即归档删除）；docs/README 的 plans 行（现行 0 份 / 归档 53 份）与 kb 行（5 份）同步。

**改了哪些文件**：docs/plans/ARCHIVE.md（新增）· 删除 53 份历史计划 · docs/plans/2026-09-20-plans-archive-and-kb-merge.md（本轮变更包）· docs/plans/README.md · docs/kb/known-issues.md · 删除 docs/kb/removed-skills.md · docs/kb/README.md · docs/kb/dev-workflow.md · docs/README.md · AGENTS.md

**对应文档**：docs/plans/2026-09-20-plans-archive-and-kb-merge.md（含判定方法与审视 3 条）· docs/plans/ARCHIVE.md

**验证**：`make gate` 通过。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 实现判定 | 可核证据、不靠自称 | ✅ 带前缀路径 + 日志条目 + 实现位置三查；12 份逐条人工确认 |
| 归档留痕 | 不丢追溯 | ✅ ARCHIVE.md 53 行 + git show 取回方式 |
| 目录规模 | 明显减少 | ✅ plans 55 → 3；kb 6 → 5 |
| 引用 | 不悬空 | ✅ 引用改指 known-issues H-1；make trace 通过 |
| 门禁 | 绿 | ✅ |

**补记（同轮修复）**：删除计划后，模块文档与一份 ADR 的「变更记录」里原指向具体计划的链接变成悬空（门禁 TC-3 抓到 5 个文件）——
统一改指 docs/plans/ARCHIVE.md（归档表里保留了每份计划的日期/主题/状态与 git 取回方式），追溯不断。

**没做 / 遗留**：① docs/background/（33 份：ADR + notes + research）未动 —— ADR 与材料各有独立价值、且被多处引用；② docs/analytics/ 仍未建（设计里明确待建）。

---

## 2026-09-20 · 全链路逻辑梳理（蜜罐以外）+ docs 整合

**做了什么**：用户要"把蜜罐以外的所有逻辑梳理出来、连技术点与实现参考补进 docs，并整合 docs 减少不必要的信息与文件"。落地：
① **新增 docs/logic.md**（117 行）：一张全链路图（客户端 → L0 → 适配器四件事 → 核心判定/决策 → 处置执行 → 响应卫生 → 遥测与观测面 → 控制台 → L4 近线 → 策略面），
加**16 步分步表**（每步：逻辑要点 / 技术点 / 实现位置 / 规则与取舍 / 怎么验），再补三条贯穿性原则（NI-1 · AR-2/AR-5 · ST-7）、实现参考索引（按文件找职责）、现象→看哪、以及**明确不在范围**（蜜罐内容与协议栈、L3 声明式细节）。
② **与既有文档分工**：logic.md = 行为逻辑；modules/_map.md = 目录/能力；ops/runbook.md = 操作；design/ = 规则 —— 四者不重复。
③ **docs 整合（只做减法、不动引用图）**：把 kb/dev-workflow 里与 ops/runbook 重复的两节（环境与产物放在哪 · 起环境的两种方式）改为指针表（减 9 行重复内容）；
给 docs/plans/README 加"为什么文件这么多（怎么读）"——当前状态看 progress/log、整体逻辑看 logic.md、旧计划不删的原因（日志历史条目引用它们）。
④ **查清两处"不能合并"**：docs/plans 54 份被 docs/log.md 历史条目引用、kb/removed-skills.md 被 5 处引用（含 AGENTS.md）—— 合并或删除都会造成悬空引用，故保留并在文档里说明读法。
⑤ 修掉一处格式缺陷：logic.md 表格单元里的裸竖线被当成列分隔（MD060）。

**改了哪些文件**：docs/logic.md（新增）· docs/README.md · docs/kb/dev-workflow.md · docs/plans/README.md

**对应文档**：docs/plans/2026-09-20-logic-map-and-docs-consolidation.md（含审视 4 条）· docs/logic.md

**验证**：`make gate` 通过（含悬空链接、过期状态标记、表格列数、追溯检查）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 全链路逻辑 | 每步有技术点/实现/依据/验证 | ✅ 16 步表 + 三条原则 + 实现索引 |
| 表格列数 | 本轮 4 份文档一致 | ✅ 全部 ✅ |
| 引用未断 | 不删被引用文件 | ✅ plans 与 removed-skills 均保留并说明读法 |
| 重复信息 | 减少 | ✅ dev-workflow 减 9 行（改指针） |
| 门禁 | 绿 | ✅ |

**补记（同轮小修）**：核实 `docs/logic.md` 表格里的 `\|` 是**正确转义**（MD060 已不再报，是我的自检脚本按裸竖线计数导致误报）；
另补上本轮文档中缺语言的代码围栏（MD040）。

**没做 / 遗留**：① docs/plans（54 份）与 docs/background（33 份）仍属多文件 —— 分别是逐轮追溯与决策材料，合并会断链或丢理由，需先改 log 历史引用（另估工作量）；② kb/removed-skills.md 作为历史留痕保留。

---

## 2026-09-20 · DAG 图上文字完整、不溢出

**做了什么**：用户反馈"图上文字要完整、不能溢出"。定位到两个原因并修掉：
① **字号随链路长度缩放**：SVG 原用 `width="100%"` + `viewBox`，链路越长整图越缩、字越小；改为**固定像素宽度**，卡片容器横向可滚动（长链路滚动看，而不是把字缩小）。
② **值单行不换行 ⇒ 溢出方框**：新增 `wrapCell()` 按字符预算折行（26 字符/行 × 最多 3 行，超出加省略号），方框高度按行数自适应；每跳再加 SVG 悬停提示（完整原文）。
③ **信息不丢**：冗长的失败原因（gRPC 原文）在方框里压成短标签（如"判定超时（DeadlineExceeded）"），**完整原文拼回该跳的「响应」段** —— 复核时发现短标签替换后原文一度只剩悬停里有，已补回。
④ 文档补"文字不会溢出"的说明（折行 / 横向滚动 / 悬停 / 三段全文）。

**改了哪些文件**：console/web/index.html · console/internal/topology/topology.go · docs/integrate/observability.md

**对应文档**：docs/plans/2026-09-20-dag-text-fitting.md（含审视 3 条）

**验证**：`make gate` 通过；重建控制台镜像后实跑复核。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 最长值是否放得下 | ≤ 三行预算 78 | ✅ 最长 35 |
| 失败跳方框 | 短标签、可读 | ✅ 判定超时（DeadlineExceeded） |
| 完整原文 | 三段里能查到 | ✅ 响应段含 rpc error: code = DeadlineExceeded desc = context deadline exceeded |
| 页面 | 新逻辑已生效、无 innerHTML 拼接 | ✅ wrapCell / overflowX 存在、非注释 innerHTML 0 |
| 门禁 | 绿 | ✅ |

**补记（继续验证）**：`executed=whitelist`（白名单命中、跳过判定）也已**端到端验证**：

```text
客户端[来源未采集] → 适配器 (L1)[GET /.git/config] → 白名单命中[跳过判定（INT-25）] → 业务源站[200 · 39 字节 · 2.6ms]
```

至此 **7 个落点里 6 个已实测**（origin · cache · failopen · origin_fallback · mirage · whitelist），仅 `block` 为单测
（它需要显式启用拦截开关）。验证配方已补进观测文档（`SHEN_VERIFY_WHITELIST=172.21.0.0/16` 放开白名单网段）。

**补记（收口最后一个登记遗留）**：`--check-graph` 一致性断言已实现并接入一键验证 —— 它核对两件用户在意的事：
**每条请求一条链路**（不被折叠/聚合）与**每步三段齐全**（请求 / 响应 / 为什么），并校验 `executed` 取值合法（七值表）。
实跑输出：`链路 1 条；落点取值与每步三段均已核对`。至此本轮的登记遗留只剩"字符宽度按保守值估算"与"真机视觉检查"两项。

**补记（收口验证矩阵）**：`block` 也已**端到端验证** —— 顺带发现并补上一个真缺口：
`director.Config.BlockEnabled` 默认关、但装配处**从未赋值** ⇒ `block` 在运行时**没有任何途径可启用**（既验不了，也无法按 INT-12 阶梯放开）。
装配层补上 `SHEN_BLOCK_ENABLED` 显式开关（默认仍关），并用一条触顶流量实测：

```text
客户端 → 适配器 (L1)[GET /actuator/env] → 核心判定[分值 1.00 · 信号 ua-nuclei,path-actuator]
  → 决策[拦截（block）] → 拦截[403]        🔴 真实告警
```

HTTP 实测 403。**至此 `executed` 七值全部实测**（origin · cache · failopen · origin_fallback · mirage · whitelist · block），
两段式告警（真实告警 + 高风险）也一并在图上验证。验完已恢复默认影子栈。

**没做 / 遗留**：① 字符预算按中英混排保守值估算，emoji 等极宽字符未精确测量（需要时用 getComputedTextLength 精确折行）；② 真机视觉检查仍未做（我无截图能力），请人工打开页面确认窄屏表现。

---

## 2026-09-20 · DAG 图示收口（逐请求唯一事件 · 真实落点可观测 · 判定标签修正）

**做了什么**：把 DAG 图示"完成"到可用，并在实跑中修掉**四个真缺陷**（都属"自洽但错"类，只有真实验证才暴露）：
① **同判定下的多条请求被折叠**：适配器事件的幂等键原为 decision_id，判定缓存命中的请求共享同一 id ⇒ 遥测侧（AR-11）把三条请求合成一条，与"每条流量单独成图"直接冲突。改为事件 id **逐请求唯一**（judged:decision_id:序），decision_id 放进载荷供 join；实测同路径三条 → **三条独立链路**。
② **意图整列缺失（join 落空）**：控制台按总条数取事件，而一次请求产生两份事件（适配器的 request_judged + 核心的 decision），同一窗口放不下 ⇒ 图上"核心判定"分值恒 0、意图空白。改为**按类型分别取数再合并**（fetchObservations）。
③ **判定 id 被 JSON 标签丢弃**：`JudgedEvent.DecisionID` 仍是 `json:"-"`（早期设计：id 取自事件信封）⇒ 载荷里的 decision_id 被忽略、join 必然失败。我上一版补丁"报告成功"其实没匹配上 —— **新加的单测当场抓到**（`decision_id 丢了`）。
④ **常量用枚举名、载荷用设计术语**：常量写成 `ACTION_MIRAGE`，而核心 decision 载荷的 action 是 `route_mirage`/`block`（设计术语）⇒ 决策节点恒显示"放行"、真实告警恒 0、影子判断失效。改为设计术语，并新增**真实载荷**用例（旧测试自己也用枚举名构造输入，所以自洽但错）。
⑤ **验证配方入库**：新增 `deploy/config/config.verify-mirage.yaml` 与 `deploy/docker/compose.verify-mirage.yaml`（非影子 + 灰度 100% + 幻境后端表），用于复验改道/回落分支；文件头有醒目警告（会真的执行改道）。
⑥ **实测结论**（验证配置下）：核心侧改道意图 `route_mirage` + 后端名、以及 `origin_fallback`（NI-5 回落，链路多一跳"幻境不可用"，代理日志 `dial tcp 127.0.0.1:19080: connection refused`）**均端到端验证**；`origin` / `cache` / `failopen` 也已实测。**`executed=mirage` 与 `whitelist` 未观测到**（本机没有可达的蜜罐后端；whitelist 仅单测），已在文档与变更包中如实标注。
⑦ 验完已**恢复默认（影子）栈**，避免危险配置留在运行环境。

**改了哪些文件**：`edge/proxy/handler.go` · `edge/proxy/wire_test.go` · `api/telemetry/v1/testdata/request_judged_event.json` ·
`console/internal/topology/topology.go` · `console/internal/topology/topology_test.go` · `console/cmd/console/main.go` ·
`deploy/config/config.verify-mirage.yaml`（新增）· `deploy/docker/compose.verify-mirage.yaml`（新增）·
`docs/spec/events.md` · `docs/modules/adapter-proxy.md` · `docs/integrate/observability.md` · `docs/ops/functional-verification.md`

**对应文档**：`docs/plans/2026-09-20-dag-completion.md`（含追溯矩阵与审视 6 条）· `docs/integrate/observability.md` §5/§6

**验证**：`make gate` 通过；逐请求接口与控制台链路对 Docker 栈实跑；单测 6 例（含真实载荷与 join）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 同路径 3 条请求 | 3 条独立链路 | ✅ 3 条（含 2 条 executed=cache） |
| 核心侧改道意图 | intent=route_mirage + 后端名 | ✅ |
| 幻境不可用回落 | executed=origin_fallback + 链路含"幻境不可用" | ✅（代理 502 dial refused） |
| 判定失败放行 | executed=failopen + 原因 | ✅ |
| 决策标签 | 显示改道/放行（不再恒放行） | ✅ 决策[改道（route_mirage）] |
| 真进入幻境后端 | executed=mirage | ⚠️ 未观测到（本机无可用后端，如实登记） |
| 门禁 | 绿 | ✅ |

**补记（同轮修复）**：终验时发现一处**跨尝试错误配对** —— 判定失败（failopen）的请求会按 decision_id 配上窗口里的**旧判定**，
于是图上出现"分值 0.90 + 落点 failopen"这种自相矛盾的组合。已改为 **failopen 不参与 join**（它本来就没有判定），
并加用例 `TestFailOpenDoesNotInheritStaleDecision`；实测修复后为 `intent=(未判定) score=0 executed=failopen`。

**没做 / 遗留**：① `executed=mirage` 与 `whitelist` 未实测（需一个真实可达的蜜罐后端）；② `--check-graph` 一致性断言仍未实现；③ 验证配置含 `shadow: false`（危险设置，仅验证用，已加警告）；④ 事件 id 形态变化对旧数据靠控制台兼容路径兜住。

---

## 2026-09-20 · DAG 逐步可点：每一步看「请求 / 响应 / 为什么执行」

**做了什么**：用户要"DAG 图每一步都能点开，看该步对应的请求信息、返回是什么、以及为什么会执行到这一步，方便整理哪儿要优化"。落地：
① **后端逐步生成三段**：`topology.ChainNode` 增加 `request` / `response` / `why`，`BuildRequests` 为每一跳填**这条请求自己的值**与依据 —— 客户端（观测来源）· 适配器（判定来源：白名单/缓存/核心/失败）· 核心判定（命中几条规则、分值如何得出、1.0 截断）· 决策（三值 + 影子模式说明）· 实际落点（放行/改道/拦截/回落各自的原因与约束）· L4 注解（不在请求路径上）。"为什么"里带**规则依据 ID**（AR-2、AR-6、AR-7、NI-1、NI-3、NI-5、INT-8、INT-11、INT-25、ST-10）与已知取舍（K-20 判定缓存"谁先到谁定调"、K-24 首请求判定超时后放行），便于顺着规则去查。
② **页面**：链路里每个方框可点（`stopPropagation`，不与整卡详情冲突）→ 出「第 N 步」面板显示三段；三段随接口一起下发，**点开不再发请求**（一屏几十条逐步排查不会 N+1）。
③ **文档**：观测文档补"逐步可点"的使用说明（含"哪儿需要优化"的定位入口），控制台模块文档补逐步详情一行。

**改了哪些文件**：console/internal/topology/topology.go · console/web/index.html · docs/integrate/observability.md · docs/modules/console.md

**对应文档**：docs/plans/2026-09-20-step-clickable-dag.md（含追溯矩阵与审视 4 条）· docs/integrate/observability.md §5

**验证**：`make gate` 通过；逐请求链表接口对 Docker 栈实跑，五步三段全有真实数据。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 探针请求五步 | 每步三段齐全 | ✅ 客户端 → 适配器 → 核心判定[0.90 · ua-headless,path-probe] → 决策[放行] → 业务源站[200 · 39 字节 · 3.4ms] |
| 判定步的为什么 | 写明规则匹配与截断 | ✅ "命中 2 条规则（ua-headless,path-probe）" |
| 源站步的为什么 | 区分放行/影子/未判定 | ✅ "也可能是改道/拦截但影子模式不执行（INT-11）" |
| 交互 | 步点击不触发整卡详情 | ✅ 已用 stopPropagation |
| 既有单测 | 不回归 | ✅ go test ./console/... |

**没做 / 遗留**：① `BuildRequests` 仍无单测（含新增三段字段）—— 下一轮补；② 阈值没进"为什么"（控制台拿不到配置阈值），所以"为什么改道/没改道"缺一个精确数字；③ `--check-graph` 一致性断言仍未实现；④ 未在真实改道下核对过幻境与回落两步的文案。

---

## 2026-09-20 · DAG 改为逐请求链路（动态 · 不聚合）

**做了什么**：用户指出"DAG 图要动态、**每个流量单独**的，而不是存（聚合）在一起"，于是把上一轮的聚合拓扑改为**逐请求链路**：
① **新增模型与接口**：`topology.RequestGraph` / `ChainNode` / `BuildRequests`（按 `decision_id` 把核心判定与适配器执行 join 后，**每个请求产出一条链路**，最新在前）；控制台新增只读接口 GET /api/graphs?limit=N。
② **链路上每跳都写该请求自己的值**：`客户端[来源 IP]` → `适配器[方法 路径]` → （`白名单命中` / `判定缓存命中` / `判定失败[原因]` / `核心判定[分值 · 命中信号]`）→ 决策[放行/改道/拦截] → **实际落点**（`业务源站[状态 · 字节 · 耗时]` / `幻境后端[后端名]` / `拦截[403]`，回落时多一跳 `幻境不可用 → 回落业务（NI-5）`），有 L4 结论时再加一跳。
③ **动态**：页面每 5 秒刷新、后端倒序返回 ⇒ 新流量出现在最上面（不引 SSE/WebSocket）。
④ **页面**：区块改为逐请求卡片列表（每张卡片一张横向链路小图），加筛选（全部 / 只看告警 / 只看高风险 / 只看幻境与回落 / 只看源站），点卡片看四段详情；渲染仍全程 `textContent`（路径与 UA 是攻击者可控字符串，非注释 `innerHTML` 使用为 0）。
⑤ **聚合视图**：接口 GET /api/topology 保留（程序化统计仍可用），页面不再用它。
⑥ 顺手把 `joinSignals`/`joinNonEmpty` 改用 `strings.Builder`（静态检查提示）。

**改了哪些文件**：`console/internal/topology/topology.go` · `console/cmd/console/main.go` · `console/web/index.html` · `docs/integrate/observability.md` · `docs/modules/console.md`

**对应文档**：[`docs/plans/2026-09-20-per-request-dag.md`](docs/plans/2026-09-20-per-request-dag.md)（含追溯矩阵与审视 3 条）· [`docs/integrate/observability.md`](docs/integrate/observability.md) §5

**验证**：`make gate` 通过；/api/graphs 对 Docker 栈**实跑**返回逐请求链路。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 逐请求链路 | 每请求一条、最新在前 | ✅ 2 条（`/` 与 `/.git/config`） |
| 链路取值 | 每跳带该请求的值 | ✅ `核心判定[分值 0.90 · 信号 ua-headless,path-probe] → 决策[放行（route_origin）] → 业务源站[200 · 39 字节 · 2.8ms]` |
| 渲染纪律 | 无 innerHTML 拼接 | ✅ 非注释使用 0 处 |
| 门禁 | 绿 | ✅ |

**没做 / 遗留**：① **`BuildRequests` 尚无单测**（`Build` 有 4 个用例）—— 需补探针链路 / 回落链路 / 未判定链路三个用例；② 上一轮的 `--check-graph` 一致性断言仍未实现；③ 未在真实改道（非影子 + 登记可用幻境后端）下验证过 `mirage` 那张图；④ 页面一屏最多 50 张卡片。

---

## 2026-09-20 · 流量调度 DAG 图（意图 vs 实际落点）+ 告警入图

**做了什么**：用户要看清"流量进来后实际走到哪"——是后段业务服务，还是进了我们设的幻境/蜜罐，并要求图上带请求与返回信息、告警也在图里。落地：
① **契约扩展**（用户选定）：适配器的 `request_judged` 事件新增 `executed`（**实际落点**）+ `backend` + `status` + `bytes` + `duration_ms`。`executed` 七值有**唯一权威定义**：`whitelist` / `cache` / `failopen` / `origin` / `origin_fallback`（NI-5 回落）/ `mirage` / `block`。文档 + 夹具 + Go 契约测试三处同步。
② **适配器**：`headerSanitizer` 顺带观测状态码与字节数（不新增包装层，不影响延迟预算 AR-29）；`ServeHTTP` 统一收尾到 `reportRoute`（**白名单命中与缓存命中此前根本不上报事件**，图上会缺分支 —— 一并修掉）；纯函数 `executedFor(shadow, action, mirageFound, mirageFellBack)` 决定落点，**7 值穷举单测**。
③ **控制台聚合**（新增纯函数包 `console/internal/topology`）：按 `decision_id` join「核心判定（意图）」与「适配器执行（实际）」与 L4 结论；输出节点/边计数 + 汇总 + `notes`；**告警两段式**——真实告警（`block` 或 `severity≠none`，口径不变）+「高风险」显示标记（`score ≥ SHEN_CONSOLE_ALERT_SCORE`，默认 0.9，**仅显示**）。
④ **两个只读接口**（`AR-10`）：`GET /api/topology`（聚合图）· `GET /api/trace?decision_id=…`（单请求四段：请求 / 判定 / 执行与返回 / 告警）。
⑤ **页面**：新增「流量调度图（DAG）」区块 —— 内联 SVG 分层布局（固定列、边宽∝计数、回落画虚线）、图例、点击节点/边过滤请求、点请求看四段详情；全页仍是 DOM + `textContent`（路径与 UA 是攻击者可控字符串，`innerHTML` 只出现在注释里）。
⑥ **如实标注**：影子模式提示（意图已判、执行仍在源站）、无幻境后端提示、判定失败计数与原因，都写进图的 `notes`，避免把缺口看成没问题。

**改了哪些文件**：`edge/proxy/handler.go` · `edge/proxy/wire_test.go`（新增）· `api/telemetry/v1/testdata/request_judged_event.json`（新增）·
`console/internal/topology/topology.go`（新增）· `console/internal/topology/topology_test.go`（新增）· `console/cmd/console/main.go` · `console/web/index.html` ·
`docs/spec/events.md` · `docs/spec/metrics.md` · `docs/modules/adapter-proxy.md` · `docs/modules/console.md` · `docs/integrate/observability.md` · `docs/kb/quick-tour.md` · `docs/kb/faq.md`

**对应文档**：[`docs/plans/2026-09-20-traffic-dag-view.md`](docs/plans/2026-09-20-traffic-dag-view.md)（含追溯矩阵与审视 5 条）· [`docs/spec/events.md`](docs/spec/events.md) §2.2 · [`docs/integrate/observability.md`](docs/integrate/observability.md) §5

**验证**：`make gate` 通过；拓扑单测与落点穷举测试通过；两个新接口对 Docker 栈**实跑有数据**。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 落点穷举 | 七值全覆盖 | ✅ `TestExecutedFor` 7/7 |
| 契约 | 键集与夹具一致 | ✅ `TestRequestJudgedWireContract` |
| 拓扑聚合 | 意图/实际配对 · 回落 · 告警分级 · 缺数据 | ✅ 4 用例（含空输入） |
| /api/topology | 有节点/边/说明 | ✅ 节点 4 · 边 3 · `to_origin=2` · 含影子模式提示 |
| /api/trace | 四段齐全 | ✅ 请求/判定/执行（executed=origin status=200 bytes=39 44.47ms）/告警 |
| 页面渲染纪律 | 无 innerHTML 拼接 | ✅ 仅注释出现 |

**没做 / 遗留**：① **`--check-graph` 一致性断言未实现**（原计划：拓扑计数 ↔ 判定条数，纳入 `verify`）；② 未在**真实改道**（非影子 + 登记可用幻境后端）下验证过图，`mirage` 分支目前只有单测；③ 页面布局未做真机视觉检查；④ 事件量大时只取最近 `limit` 条。

---

## 2026-09-19 · 验证 + 代码/日志优化 + 文档整合减量

**做了什么**：按用户要求"先验证，再优化代码与日志，再优化并整合文档"。
① **验证基线（三层）**：`make gate`（格式 · vet · staticcheck · errcheck · 架构 · 追溯 · 泄漏 · 许可 · Python 门禁 · 单测含 -race）· scripts/shen.sh doctor（接入自检五项）· scripts/shen.sh traffic（33 场景判定验证）。基线结论：自检 **通过 3 · 失败 0 · 约束 1 · 无法判定 1**；判定 **断言 27/27 · 缺口 8 · 出口卫生 0**。
② **代码优化（消除已登记缺陷 `K-24`）**：适配器启动时**预热 gRPC 连接**（`conn.Connect()` + 等 `connectivity.Ready`，最多 2s；失败只记日志、**不阻断启动**）。根因是"惰性建连的成本落在第一条请求上，而判定预算只有 3ms（`AR-29`）"。**实测**：整栈重建后第一条请求即得判定（`失败=nil（空）`，此前是 `DeadlineExceeded`）。
③ **日志优化**：`decide()` 失败分支加**无条件** warn（`判定失败，按 NI-3 放行到业务` + `decision_id` + `耗时` + `原因`），**不受逐请求日志开关控制** —— 默认部署下这是"引擎为什么没判"的唯一线索（此前只有开关打开的部署才看得到）。
④ **文档整合减量**：删除 docs/integrate/manual-test.md（本轮已删除），其"人工才做"的内容（故障注入 `NI-1` 现场验证 · 边界情形快速检查）并入 `docs/ops/functional-verification.md` **§7 人工测试（15 分钟一轮）**；integrate/ 从 5 份减到 4 份。同时修正 8 处引用（含"标签写 manual-test、链接指向别的文件"的错配），并修掉根 `README.md` 的 1 处悬空链接。

**改了哪些文件**：`edge/proxy/handler.go` · `docs/ops/functional-verification.md` · docs/integrate/manual-test.md（本轮已删除）（**删除**）· `README.md` · `docs/README.md` · `docs/integrate/README.md` · `docs/integrate/business-onboarding.md` · `docs/modules/console.md`

**对应文档**：[`docs/plans/2026-09-19-verify-opt-logs-docs.md`](docs/plans/2026-09-19-verify-opt-logs-docs.md)（含追溯矩阵与审视 4 条）

**验证**：`make gate` 通过；自检与判定验证实跑；**整栈重建后首请求**实测有判定（预热生效）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 门禁 | 绿 | ✅ 门禁通过 |
| 接入自检 | 五项有结论 | ✅ 通过 3 · 失败 0 · 约束 1 · 无法判定 1 |
| 判定验证 | 全量断言通过 | ✅ 27/27 · 缺口 8 · 出口卫生 0 |
| 重建后首请求 | 应有判定（不再超时） | ✅ 判定：GET /k24-check … 失败=nil（空） |
| 适配器单测 | 全过 | ✅ go test ./edge/proxy/（17.9s） |
| 文档引用 | 无悬空 | ✅ 追溯检查通过 |

**没做 / 遗留**：① `docs/log.md` 与 `docs/plans/` 的历史条目里有相对链接写法不规范（Marksman 提示，不影响门禁）；② 判定失败日志未限流（核心长时间不可达会刷屏）；③ `docs/plans/` 46 份历史变更包目录偏大（属追溯留痕，减量需评估对门禁最新条目的依赖）；④ `scripts/sentinel`（差异哨兵 12 项）仍占位。

---

## 2026-09-19 · 接入自检实现（INT-17 五项）+ 指标字典（AR-28）

**做了什么**：把两个"设计已要求、当前还是占位"的东西做实。
① **接入自检五项落地**（新增 `scripts/doctor/doctor.py`，接线 `scripts/shen.sh doctor` 与 `make doctor`）：① body 是否可读 ② TLS 是终结还是透传 ③ 会话粘性 ④ 实境与幻境可区分 ⑤ 引擎是否真的在请求路径上。**不给假绿**：结果分四态 —— 通过 / 失败 / **约束**（验到了但限制你能做什么）/ **无法判定**（验不了，写明原因与关法），各自带"怎么办"。只用标准库。
② **实跑结论**：`通过 3 · 失败 0 · 约束 1 · 无法判定 1`。其中 ① 是**真发现**：观测里没有请求体字段 ⇒ **引擎当前不读 body**，按 `INT-22` **只能观察、禁止启用误导处置**；④ 因当前环境没有可用幻境后端而如实"无法判定"（并在输出里写清前置：非影子 + 登记可用后端）。
③ **指标字典**（新增 `docs/spec/metrics.md`）：按 `AR-28`（"§6 指标必须全有采集点与告警阈值，禁止只定义不采集"）**如实分两段** —— **已采集 10 项**（判定分布 · 风险分 · 命中信号 · 告警 · 误调度预算 · 事件幂等 · L4 去重率 · L4 证据完整性 · 延迟下界 · 引擎故障对业务的影响；每项给口径 + 采集点 + 阈值）；**未采集 6 项**（运行时延迟分位 · 线上误调度率 · `severity` 分档告警 · 资源容量 · L1 分支计数 · `INT-18` 运营报表；每项给关法）。并立三条规矩（新增指标先登记口径再写代码 · 不要用日志当指标 · 阈值权威在配置）。
④ **文档同步**：新增 `docs/integrate/doctor.md`（五项判据 · 四态怎么读 · 实测例子 · 与 `traffic`/哨兵的分工）；`scripts/doctor/README.md` 从占位改成真实；`docs/README.md` §3 把 doctor 从"未建"改 ✅（`scripts/sentinel` 仍为占位）；`docs/kb/quick-tour.md` 速览加一行。

**改了哪些文件**：`scripts/doctor/doctor.py`（新增）· `scripts/doctor/README.md` · `scripts/shen.sh` · `Makefile` ·
`docs/integrate/doctor.md`（新增）· `docs/spec/metrics.md`（新增）· `docs/README.md` · `docs/kb/known-issues.md` · `docs/kb/quick-tour.md`

**对应文档**：[`docs/plans/2026-09-19-doctor-and-metrics.md`](docs/plans/2026-09-19-doctor-and-metrics.md)（含追溯矩阵与审视 5 条）· [`docs/integrate/doctor.md`](docs/integrate/doctor.md) · [`docs/spec/metrics.md`](docs/spec/metrics.md)

**验证**：`make gate` 通过；自检对 Docker 栈**实跑**（结论见下）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 自检五项 | 各有结论、无假绿 | ✅ 通过 3 · 失败 0 · 约束 1（body 不可读）· 无法判定 1（无幻境后端） |
| body 检查 | 观测无标记 ⇒ 约束 + 关法 | ✅ 输出 `INT-22` 的限制与关闭路径 |
| TLS 检查 | 明文入口 ⇒ L0 终结 | ✅ "入口是明文 HTTP ⇒ 由前置 L0 终结（ADR-0019 默认）" |
| 会话粘性 | 同会话同一判定 | ✅ 两次同一 decision_id（`ST-10` 生效） |
| ⑤ 路径检查 | 观测面有该判定 | ✅ 经引擎 200 且观测面有记录 |
| 指标登记 | 已采集/未采集两段 + 关法 | ✅ 10 已采集 / 6 未采集 |

**没做 / 遗留**：① **引擎不读请求体**（自检给出的是"约束"：按 `INT-22` 只能观察，不得启用误导处置）；② ④ 在当前环境无法验证（需非影子 + 登记可用幻境后端）；③ `scripts/sentinel`（差异哨兵 12 项）仍是占位；④ 运行时指标（延迟分位 / 分支计数 / 容量）未采集，关法已列在 metrics 规格 §2；⑤ 重启后第一条请求判定超时（业务不受影响、该条无观测记录）已登记为 `K-24`。

---

## 2026-09-19 · 文档整理 + 知识库重做（新人入口 / 能力定位 / 减冗余）

**做了什么**：用户要「按设计 / 知识库 / 模块 / 运行分类整理 docs，减少不必要内容，主要更新 kb，方便快速学习、运行、开发定位」。落地：
① **知识库新增新人入口 [docs/kb/quick-tour.md](docs/kb/quick-tour.md)**：三句话讲清项目 · 五层→目录一张图 · **能力点定位表（24 行：我想改 X → 模块 → 代码目录 → 模块文档）** · 五分钟跑起来（`up`/`traffic`/`verify`）· 开发定位三招（一条请求三处对齐 / 控制台 / 回放）· 缺口去哪看 · 文档地图分类。
② **docs/kb/README.md 重写**：三条最快路径（了解项目 / 查已知问题 / 查高频疑问）· 文件索引（标现行/历史）· 与相邻目录的区别（规则→`design`、契约→`spec`、操作→`ops`）· 怎么写一条。
③ **`known-issues.md` 加"现行 / 历史"分类**：`K-9`/`K-10`/`K-11` 属 **ext_proc 时代**（代码里已无 ext_proc，`ADR-0017` 换成内嵌 Caddy），标为历史并写明"不要按它们改今天的代码"；新增 `K-21`（单独重启 core 会整栈端口不通）· `K-22`（`--force-recreate` 不重建镜像）· `K-23`（查询串不被判是已知缺口，别当 bug 查）。
④ **`faq.md` 补 6 条高频"像 bug"的现象**：分数恒 0 · 告警恒 0（`severity` 未定档）· 改了代码没生效 · 参数里的攻击为何不判 · 怎么加验证场景 · 缺口去哪看。
⑤ **新增 [docs/spec/README.md](docs/spec/README.md)**：规格索引 + **"改了要同步哪几处"**（config ↔ 示例配置与单测 · events ↔ 夹具与两侧契约测试 · logs ↔ 打日志代码）—— 同时修掉 docs/kb/README.md 的两处坏链。
⑥ **`docs/README.md` 分类与过期状态修准**：顶部加"三个最快入口"；§2 运维/运营/接入方改指向**已建**的 docs/integrate 与 docs/ops；§3 把"待建"改成"已建并列出内容"（docs/analytics 与 docs/integrate/doctor.md 保留为待建）；现状行重写（策略面/注入/L4/控制台均已落地，未接通项与缺口指针写明）；删掉文末 3 行游离表格行。

**改了哪些文件**：docs/kb/quick-tour.md（新增）· docs/kb/README.md · `docs/kb/known-issues.md` · `docs/kb/faq.md` ·
docs/spec/README.md（新增）· `docs/README.md`

**对应文档**：[`docs/plans/2026-09-19-docs-organization-and-kb.md`](docs/plans/2026-09-19-docs-organization-and-kb.md)（含追溯矩阵与审视 6 条）

**验证**：`make gate` 通过（含 `make trace`：悬空链接 · 过期状态标记 · 文档↔代码↔单测）；自建脚本复核 6 份改动文档的表格列数全部一致。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 追溯与链接 | 无悬空、无过期标记 | ✅ 追溯检查通过 |
| 表格列数 | 6 份文档一致 | ✅ 无 ✗ |
| 新人路径 | 三步内拿到"是什么/能力在哪/怎么跑/怎么定位" | ✅ 顶部三入口 + kb 三条路径 + quick-tour 六节 |
| 能力定位表 | 目录与文档真实存在 | ✅ 与 `_map.md` 同源（实测清单） |

**没做 / 遗留**：① analytics/（运营看板与放开判据）仍待建；② integrate/doctor.md 与 `scripts/doctor` 仍是占位；③ docs/spec/metrics.md 仍待建（`AR-28` 指标口径无权威处）；④ kb 的历史条目会累积，需定期用 `audit` 审视。

---

## 2026-09-19 · 本地日志（逐判定）+ 一键验证与定位线索

**做了什么**：用户要「补项目本地日志」并「完善测试脚本，保证后续验证与定位更快更准」。逐条落地：
① **核心逐判定日志**：在观测记录器（每次判定都经过它）里加一行结构化日志 —— `msg=decision` + `decision_id` / `action` / `severity` / `backend` / `score` / `signals` / `method` / `path` / `source_ip` / `user_agent` / `at`。用 `slog`，开关 `SHEN_LOG_FORMAT=text|json`（默认 text 给人读，json 给 jq）。启动与装载类日志保持原来的标准 `log`（稳定可 grep，脚本在依赖它）。
② **适配器逐请求日志**：白名单命中（跳过判定）· 判定缓存命中（`ST-10` 复用的直接证据）· 判定结果（id → 决策 + 后端 + 失败原因），开关 `SHEN_PROXY_LOG_REQUESTS`（**默认关**：观测面已记全量判定，生产不需要两份；演示 compose 默认开，本地排查必须有）。日志里的决策用**设计三值术语**（新增纯函数 `actionName` + 单测守住），与控制台、核心日志、文档用同一套词。
③ **一键验证**：新增 `scripts/shen.sh verify` —— 状态 → 全量伪造流量（含 `--check-l4`）→ **报告落临时目录** → 打印定位线索（逐判定日志怎么 grep、适配器侧怎么看）。
④ **定位脚本化**：`send.py --explain`（失败/缺口场景打印判定原文 + "追一条"的命令）与 `--report`（JSON 落盘，自动化/留档）。
⑤ **日志字典成文**：新增 `docs/spec/logs.md`（三条原则 · 组件→日志点表 · 逐判定字段表 · 开关 · 落在哪 · **与事件的区别** · 定位手法 · 未决），并把 `docs/README.md` 里仍标"待建"的两行改准。
⑥ **真缺陷（本轮最有价值）**：`scripts/shen.sh restart` 原先只 `--force-recreate` —— **它不重建镜像**。加完日志跑验证时一行 `msg=decision` 都没有，正是这个原因（差点误判成"日志没生效"）。已改为 `up -d --build --force-recreate`，并写进日志规格与运行手册：**改了代码必须重建镜像**。

**改了哪些文件**：`core/cmd/core/main.go` · `edge/proxy/handler.go` · `edge/proxy/cmd/proxy/main.go` · `edge/proxy/proxy_test.go` ·
`deploy/docker/compose.yaml` · `scripts/shen.sh` · `scripts/traffic/send.py` · `scripts/traffic/README.md` ·
`docs/spec/logs.md`（新增）· `docs/ops/runbook.md` · `docs/README.md`

**对应文档**：[`docs/plans/2026-09-19-local-logs-and-verify.md`](docs/plans/2026-09-19-local-logs-and-verify.md)（含追溯矩阵与审视 5 条）· [`docs/spec/logs.md`](docs/spec/logs.md)

**验证**：`make gate` 通过；日志**实跑可见**（核心与适配器两侧，`decision_id` 一致）；`scripts/shen.sh verify` 端到端跑通并落报告。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 核心逐判定日志 | 一行结构化、字段齐全 | ✅ `msg=decision decision_id=b1fd473f… action=route_origin score=0.6 signals=[ua-headless] method=GET path=/probe` |
| 适配器逐请求日志 | 同一 `decision_id` | ✅ `proxy: 判定：GET /probe decision_id=b1fd473f… → route_origin（后端 ""，失败=nil（空））` |
| 日志用设计术语 | 三值而非枚举名 | ✅ `TestActionName` 通过（含 `ACTION_UNSPECIFIED → route_origin`） |
| 一键验证 | 报告 + 线索 | ✅ 报告写入临时目录；打印两条追查命令 |
| 门禁 | 全绿 | ✅ `make gate` |

**没做 / 遗留**：① 无日志轮转与保留（生产需配日志驱动）；② 无 trace/span id（跨进程只靠 `decision_id`）；③ 适配器 `request_judged` 事件字段未并入日志字典；④ 日志级别不可调（只有逐请求开关）；⑤ metrics 规格（docs/spec/metrics.md）仍未建 —— AR-28 的指标口径暂无权威处。

---

## 2026-09-19 · 全场景伪造流量 + 整体功能验证（含缺口清单）

**做了什么**：用户要「不同场景的伪造流量做整体功能验证」，并「跑一次测试脚本确认整体逻辑、帮我找到功能欠缺」。逐条落地：
① **场景集 10 → 33 条**（九个分组）：自动化探针 · 扫描器指纹 · 敏感端点 · 注入与穿越 · 破坏性方法 · 会话与缓存 · 白名单 · 正常对照（含静态资源）· 边界（8KB UA / 2KB 路径 / 空 UA / 非 ASCII 路径）。
② **示例规则 2 → 16 条**（扫描器指纹 sqlmap/Nuclei/Nikto/masscan/脚本客户端 · 敏感路径 .env/.svn/.aws/credentials/admin/actuator/wp-login · 注入标记 · 方法维度）—— 规则太少时绝大多数流量只能"观察"，验证是浅的；示例规则是可替换的样例（数据，`ST-24`）。
③ **脚本新增四类语义**：`gap`（已知缺口：分类 + 实测证据 + 怎么关，**只打印不算失败**）· 场景级 `session: shared/unique` + `same_decision_as_previous`/`distinct_decisions`（验证 `ST-10` 判定复用）· 响应**体**也查 `ST-7`（不得出现命中信号名与判定 id）· `--check-l4`（结论存在性 + 证据引用必须真实存在，`AR-12`）。
④ **跑通一次全量验证**：断言 **27/27** 通过 · 缺口 **8** 条 · 出口卫生 **0** 问题；L4 核对：结论 2 条（接受 1）· 遥测已知判定 34 个 · **无悬空证据引用**（`AR-12` 成立）；`ST-10` 两个方向都验到（同会话复用同一 `decision_id`、换会话得不同 `decision_id`）；正常对照与边界全 0 分且业务 200。
⑤ **发现 2 处真实能力缺口**（本轮只登记不修）：**查询串不参与判定** —— 判定用的 `path` 字段不含 query，所以 /download?file=../../etc/passwd、/search?q=union+select 都是 0 分 0 信号（SQLi/穿越这类最主流的入口默认不判）；**一次 URL 编码即绕过**字符串规则（`%2e%2e%2f`、`union%20select`）。另有 4 条精度/未覆盖缺口（前缀误伤 `/.gitignore`、前缀可被 /static/../.git/config 绕过、PUT/PATCH 未建模、`severity` 恒为 none 导致告警永远 0）与 1 条未实现（白名单 `INT-25` 未消费）。
⑥ **发现并修掉一个运维陷阱**：`core` 是这套 compose 的**网络命名空间持有者**，单独 `docker compose restart core` 会让兄弟服务留在旧命名空间 ⇒ 容器全 `Up` 但宿主端口 **HTTP 000**。新增 `scripts/shen.sh restart`（整栈重建），并写进运行手册故障表与验证文档。
⑦ **跑测过程中修掉 3 个脚本自身 bug**：共享会话每轮都随机（复用断言永远测不到）· 第一轮就断言"与上一条比较" · 非 ASCII 路径直接把请求发崩（改按 percent-encoding 发送、按解码形态匹配）。
⑧ **文档**：新增 `docs/ops/functional-verification.md`（验到了什么 · 缺口清单含怎么关 · **当前环境验不了什么**及原因 · 运维陷阱 · 方法论边界）；`docs/spec/config.md` 写出**匹配语义的实测行为**（`prefix` 纯字符串、`path` 不含查询串、分数 1.0 截断）；`docs/ops/runbook.md` 与 `scripts/traffic/README.md` 同步。

**改了哪些文件**：`scripts/traffic/scenarios.json` · `scripts/traffic/send.py` · `scripts/traffic/README.md` · `deploy/config/config.example.yaml` ·
`scripts/shen.sh` · `docs/ops/functional-verification.md`（新增）· `docs/ops/runbook.md` · `docs/spec/config.md`

**对应文档**：[`docs/plans/2026-09-19-traffic-scenarios-functional-verification.md`](docs/plans/2026-09-19-traffic-scenarios-functional-verification.md)（含追溯矩阵与审视 7 条）· [`docs/ops/functional-verification.md`](docs/ops/functional-verification.md)

**验证**：`make gate` 通过；`scripts/shen.sh traffic --check-l4` 全量实跑。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 全量断言 | 全过 | ✅ 断言 27/27 通过 · 缺口 8 条 · 出口卫生 0 问题 |
| 规则叠加与截断 | 触顶 1.0 | ✅ ua-nuclei 0.6 + path-actuator 0.4 = 1.00 |
| 判定复用 ST-10 | 同会话同一判定 | ✅ session-reuse 两次同 decision_id（均 0.90） |
| 换会话换判定 | 不同判定 | ✅ session-distinct 两个不同 decision_id |
| 出口卫生 ST-7 | 不泄漏 | ✅ 响应头与响应体 0 命中 |
| L4 一致性 AR-12 | 引用真实存在 | ✅ 结论 2 条（接受 1）· 已知判定 34 个 · 无悬空引用 |
| 运维陷阱 | 修复后可用 | ✅ 单独重启 core 曾 HTTP 000；scripts/shen.sh restart 后 200 |

**没做 / 遗留**：① **查询串不可见**（最高优先，需给观测加 query/raw_uri 字段）；② 一次 URL 编码即绕过（需匹配前归一化）；③ 前缀语义误伤与绕过；④ PUT/PATCH 未建模；⑤ 白名单 INT-25 未消费；⑥ `severity` 恒为 none ⇒ 告警永远 0（档位未定，设计已登记）；⑦ 期望值与示例规则手工同步；⑧ 改道/拦截/注入/诱饵/蜜罐/隔离/DNS/镜像/netpolicy 在当前栈**验不了**（原因与关法见功能验证文档 §3）。

---

## 2026-09-19 · 目录与模块地图（README 与 docs 完整化）

**做了什么**：用户要求「让我知道每个目录是做什么的、每个模块在哪个目录、对应什么能力、怎么和其他模块接入」。
① **新增索引文档 `docs/modules/_map.md`**（六节）：**分层 → 顶层目录**（十个目录逐个一句话 + 语言 + 是否源码，并指出白名单与产物形态）· **模块总表**（22 个模块按平面分四张表：目录 / 能力一句话 / **对外接口名** / 依赖 → 被谁用）· **进程与接缝**（3 个 Go 二进制 + L4 worker + 控制台；接缝表 7 条含"不存在的接缝"；请求时序文字图）· **数据与配置落在哪**（配置 · 事件契约 · 策略载荷 · 提示词资源 · 适配器变量）· **一个模块的标准形状**（七条要件，照它加新模块）· **不知道看哪**。
② **事实一律取自实测**：清单取 `docs/design/modules.md` §1.1；接口名由脚本从各模块 `iface.go` 抽取（不凭记忆）；进程边界与依赖方向引 `docs/design/structure.md` §1.6 的实测结论；rpc 取自 `api/` 下的 `.proto`。
③ **不重复权威**：完成度不写进地图（`docs/progress.md` 是唯一维护处），只给"源码/配置/声明式/第三方"的产物形态；规则正文仍只在 `docs/design/`。
④ **导航接通**：`README.md` §5、`docs/README.md` 导航、`docs/modules/README.md` 顶部三处都能点到地图。

**改了哪些文件**：`docs/modules/_map.md`（新增）· `README.md` · `docs/README.md` · `docs/modules/README.md`

**对应文档**：[`docs/plans/2026-09-19-directory-and-module-map.md`](docs/plans/2026-09-19-directory-and-module-map.md)（含追溯矩阵与审视 5 条）

**验证**：`make trace` 通过（模块↔文档↔代码↔单测 · 规则 ID 引用 · 悬空链接）· `make gate` 通过。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 地图覆盖模块 | 22 个模块都有目录与接口 | ✅ 目录取自实测清单、接口名取自 `iface.go` |
| 追溯 | 全过 | ✅ 追溯检查通过（含悬空链接检查） |
| 导航 | 三处可点 | ✅ README §5 · docs/README · docs/modules/README |
| 门禁 | 全绿 | ✅ `make gate` |

**没做 / 遗留**：① 地图与代码**手工同步**（加模块后要更新；可在未来加"脚本比对 iface.go 与地图"的自动检查）；② `honeypot-shell` 无目录（用户裁定推迟），地图中标为 ⏸ 并写明；③ 地图不画模块级依赖图（权威图在 `docs/design/structure.md` §1.6.2，不重复）。

---

## 2026-09-19 · 伪造流量验证脚本（`scripts/traffic/`）

**做了什么**：给人工测试一套**声明式**的伪造流量工具 —— 发请求 + **从观测面核对判定** + 断言 + 退出码，能直接进自动化。
① **场景声明式**（`scripts/traffic/scenarios.json`，10 条）：自动化探针（无头浏览器 + 源码目录探测）· 敏感文件（`.env`）· 凭证爆破（`wp-login.php` POST）· 注入（SQLi）· 路径穿越 · Actuator 扫描 · 正常用户对照（首页 / 接口）。每条 = 一次请求 + 期望（分数上下界 / 命中信号 / 决策）。
② **核对走观测面**：判定响应禁止回显分值/信号（`ST-7`），所以脚本只看响应状态码，**判定去控制台读** —— 这也是本项目最容易讲错的一条规矩，脚本按它写。
③ **每请求唯一会话**：第一版只换查询串，结果第二个场景吃到了第一个场景的判定（`probe-git-normal-ua` 拿到 0.90 与 `ua-headless`）。读代码确认判定按 `decision_id`（键为 `(来源, 会话, 方法, 路径)`，`ST-10`）复用 ⇒ 改为**每条请求带唯一 `sid` cookie**；修正后 0.30 / 仅 `path-probe`。想复现"同窗复用"用 `--no-session-nonce`。
④ **观察类不下断言**：示例配置只有两条规则（`ua-headless` / `path-probe`），/.env、/wp-login.php、SQLi、Actuator 这类流量标 `observe_only` —— 只报告，不替用户编期望值。
⑤ **额外两道检查**（所有场景都做）：业务未被影响（状态码默认 < 500，可用 `status_in` 声明例外，`NI-1`）与响应头卫生（不得出现 `x-shen*` / `Via` / `Caddy`，`OH-2`）。
⑥ **入口与门禁**：`scripts/shen.sh traffic` · `make traffic`；顺手补掉一个门禁漏洞 —— `scripts/*.py` 此前从未进 Python 静态检查，现已纳入 ruff（并修掉暴露出的 7 条）。
⑦ **修掉一个控制台缺陷**：空列表被 Go 序列化成 `null`（`var out []T` 是 nil 切片），客户端得同时处理 `null` 与 `[]`；改为 `make([]T, 0)`，/api/flow 与 /api/analysis 都返回 `[]`。
⑧ 文档同步：运行手册新增 §2.1（伪造流量验证 + 两个必须知道的语义）与故障表两条；人工测试文档改为指向脚本。

**改了哪些文件**：`scripts/traffic/scenarios.json`（新增）· `scripts/traffic/send.py`（新增）· `scripts/traffic/README.md`（新增）·
`scripts/shen.sh` · `Makefile` · `console/cmd/console/main.go` · `docs/ops/runbook.md` · `docs/integrate/manual-test.md`

**对应文档**：[`docs/plans/2026-09-19-traffic-verification-script.md`](docs/plans/2026-09-19-traffic-verification-script.md)（含追溯矩阵与审视 5 条）· [`scripts/traffic/README.md`](scripts/traffic/README.md)

**验证**：`make gate` 通过；对 Docker 栈**实跑**全部场景。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 全场景 | 断言全过 | ✅ 断言 5/5 通过 · 观察 5 条 · 响应头卫生问题 0 条 |
| 探针（UA+路径） | ≥0.8 且命中两类信号 | ✅ 0.90 ua-headless,path-probe |
| 仅路径 / 仅 UA | 0.2–0.5 / 0.5–0.7 | ✅ 0.30 path-probe · 0.60 ua-headless |
| 正常对照 | ≤0.1 | ✅ 0.00（首页与接口） |
| 观察类 | 只报告 | ✅ 5 条（`.env` · 爆破 501 · SQLi · 穿越 · Actuator） |
| 退出码 | 全过 → 0 | ✅ 0 |

**没做 / 遗留**：① 5 条观察类场景尚无断言（等加了规则再收紧）；② 无并发/速率控制（不做压测）；③ 期望值与示例配置**手工同步**，改规则忘改场景会假红；④ 会话 nonce 默认 cookie 名 `sid`，换配置需 `--session-cookie`；⑤ 未覆盖 TLS 入口与跨节点形态。

---

## 2026-09-19 · 运行手册 + 一键启动脚本 + 产物卫生

**做了什么**：用户要三件事 —— ① 把「如何启动 / 如何检查 / 如何修复」记录清楚，保证后续启动、更新、验证准确；② 给一个启动脚本；③ 注意本地产物位置，别让无用文件影响项目结构。逐条落地：
① **运行手册**（新增 `docs/ops/runbook.md`，唯一一份运行说明）：一分钟命令表 · 启动（Docker / 本地 / 「Up ≠ 就绪」）· 检查（四层：容器 → 链路 → 仓库门禁 → 观测接口）· **修复表**（症状 → 原因 → 处理，10 条，含本会话真实踩过的坑）· 更新（代码 / Go 依赖 / Python 依赖 / 基础镜像）· 产物卫生 · 端口变量一览。README 与 docs 导航已指向它。
② **一键启动脚本**（新增 `scripts/shen.sh`）：`up`（**等就绪后再打印地址**）· `status`（容器 + 控制台概览）· `smoke`（经引擎造三条流量并回显分值/命中信号）· `check`（`make gate` + `make dev`）· `logs`（**不跟随**，取尾部即返回）· `local`（本地进程）· `down`；Makefile 加四个薄别名 `start` / `status` / `app-smoke` / `verify`。
③ **产物卫生**：临时产物统一落在 `${TMPDIR:-/tmp}/shen-<uid>/`（脚本的 `RUNDIR`），仓库里**不留**二进制/日志/pid；`scripts/demo/run.sh` 也从硬编码 /tmp/shen-demo-* 改到同一目录（并处理 `TMPDIR` 尾斜杠）。启动与验证后 `git status --short` 保持为空。
④ **顺手修掉两个真缺陷**：控制台 /api/flow?limit=N 原先的 limit 作用在**全部事件**上（含 `request_judged`），过滤后行数远少于 N —— 页面像是"记录变少"，实为被别的类型挤掉；脚本 `smoke` 里内联 python 用 `\"` 转义被当成字面反斜杠（改用 here-doc 传参）。

**改了哪些文件**：`scripts/shen.sh`（新增）· `docs/ops/runbook.md`（新增）· `scripts/demo/run.sh` · `Makefile` · `console/cmd/console/main.go` · `README.md` · `docs/README.md`

**对应文档**：[`docs/plans/2026-09-19-runbook-and-start-script.md`](docs/plans/2026-09-19-runbook-and-start-script.md)（含追溯矩阵与审视 5 条）· [`docs/ops/runbook.md`](docs/ops/runbook.md)

**验证**：`make gate` 通过；`scripts/shen.sh check`（即 gate + dev）全绿；五个子命令逐一实测。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| `scripts/shen.sh up` | 幂等 + 等就绪 + 打印地址 | ✅ 打印控制台/业务入口地址与临时目录 |
| `scripts/shen.sh smoke` | 三条流量 200 + 回显判定 | ✅ 5 行；探针 `score=0.9 signals=['ua-headless', 'path-probe']` |
| `scripts/shen.sh logs console` | 取尾部后立即返回 | ✅ 不挂住 |
| `scripts/shen.sh check` | gate + dev 全绿 | ✅ 含第 6 步 L4 |
| /api/flow limit 语义 | limit 作用于判定事件 | ✅ 修复前 1 行 → 修复后 5 行 |
| 启动后仓库状态 | `git status --short` 为空 | ✅ 无产物落进仓库 |

**没做 / 遗留**：① 跨节点部署仍需 mTLS（设计约束）；② 控制台无鉴权（仅本机/内网）；③ `analysis` 镜像 250MB 可再瘦；
④ 脚本本身未进 CI（需 Docker-in-Docker，靠人工按手册走）；⑤ hadolint 本机没有，Dockerfile 少了独立 lint（需要时可用容器跑）。

---

## 2026-09-19 · Docker 一键起全套 + 目录工整化（Python 环境归层、依赖离线化）

**做了什么**：用户提出三点要求 —— ① Python 相关配置**不要放仓库根**；② 整个项目要能**在 Docker 里一条命令启动**、对本地依赖最少；③ 文档要准确客观、目录要工整、少一些 `.` 开头的隐藏文件。逐条落地：
① **Python 工具链归层**：`pyproject.toml` · `requirements*.txt` · `.venv` · proto 桩 · 生成脚本**全部收进 `analysis/`**（Python 是 L4 的实现选择，不该占用仓库根）；根目录只剩 `AGENTS.md` `Makefile` `README.md` `go.mod` `go.sum` 与源码平面，隐藏条目从 8 个减到 3 个（`.git` `.gitignore` `.pi`）。合并掉 `pyrightconfig.json`（并入 `analysis/pyproject.toml`），删掉 `.vscode/`，门禁工具二进制从 `.bin/` 挪到 `scripts/bin/`。
② **Docker 一键起**（`make up`）：三个镜像定义（Go 三入口共用一个 `go.Dockerfile`、L4 的 `analysis.Dockerfile`、演示业务站）+ `deploy/docker/compose.yaml`：核心 + 反向代理前置 + 观测控制台 + L4 近线分析 + 演示业务站。**关键设计**：除核心外都 `network_mode: service:core` —— 核心判定面是明文 gRPC 且规则只允许回环（`assertPlaintextListenIsLocal`），共享核心的网络命名空间后「同机」前提在容器里依然成立，规则不必放宽；宿主端口可配（默认 18080 / 19444，避开常被占用的 8080/9444）。
③ **依赖离线化**：本机**访问不了 `proxy.golang.org`**（HTTP 000），容器里 `go mod download` 会永远卡住 —— 这正是此前"构建一直不完成"的真因。改为 **vendor 入库**（63MB / 5661 文件）并删掉该步骤；`licensecheck` 相应改为**优先从 `vendor/` 读许可证**（审计离线可用，且审的是真正参与构建的副本）。
④ **修掉四个真缺陷**：`grpcio` 只装在 venv、**没进锁文件**（换机器必崩）；生成桩的顶层名 `telemetry` 与本层 `analysis/telemetry.py` **撞名**（容器内启动即崩）；新版 protobuf 的生成物**需先导入 well-known types**（`AddSerializedFile` 报错）；Dockerfile 的 `go mod download` 在无代理网络下卡死。
⑤ **忽略清单**：删掉空转的 /core/core、补齐二进制兜底条目并写明「不影响源码」；顶部过期的「本仓库还不是 git 仓库」改准。新增门禁自检 `make check-ignore`（已入库文件不得被忽略规则命中）—— 注意**必须用 `--no-index`**：默认行为下 git 认为已入库文件不受忽略影响，检查会永远通过（我第一版就是**假的防线**，被反向测试抓到并修正）。
⑥ **文档**：`README.md`（一键 Docker 优先 + 目录结构 + 规模数字）· `docs/integrate/quickstart.md` · `docs/design/structure.md` §1.1（`vendor/` 与各层配置位置）· `docs/kb/dev-workflow.md`（环境放哪 + Docker 命令）· `docs/progress.md` · `deploy/docker/README.md`。

**改了哪些文件**：`analysis/pyproject.toml` · `analysis/requirements.txt` · `analysis/requirements-dev.txt` · `analysis/tools/genproto.py` · `analysis/telemetry.py` · `analysis/proto/`（重新生成为普通包） ·
`deploy/docker/go.Dockerfile` · `deploy/docker/analysis.Dockerfile` · `deploy/docker/business.Dockerfile` · `deploy/docker/compose.yaml` · `deploy/docker/README.md` · `.dockerignore` · `vendor/` ·
`scripts/licensecheck/main.go` · `scripts/archcheck/main.go` · `Makefile` · `.gitignore` ·
`README.md` · `docs/integrate/quickstart.md` · `docs/design/structure.md` · `docs/kb/dev-workflow.md` · `docs/progress.md`

**对应文档**：[`docs/plans/2026-09-19-docker-and-repo-layout.md`](docs/plans/2026-09-19-docker-and-repo-layout.md)（含追溯矩阵与审视 11 条）· [`deploy/docker/README.md`](deploy/docker/README.md)

**验证**：`make gate` 通过；**容器内端到端**实测（5 容器 · 真流量 · 控制台可见分值/信号 · L4 结论落入控制台）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 镜像构建 | 5 个全成、0 报错 | ✅ core 29.8MB · proxy 83.9MB · console 31.1MB · analysis 250MB · business 203MB |
| 起全套 | 5 容器 Up | ✅ `make docker-ps` |
| 经引擎访问 | 业务 200（影子模式） | ✅ 三条流量全 200 |
| L4 在容器内 | 结论进控制台 | ✅ /api/analysis 2 条；概览 `l4_conclusions: 2`；再跑一轮 `新 0 / 重复 2`（`AR-11` 幂等） |
| 忽略清单自检 | 正反两向 | ✅ 当前通过；故意加 `/core/cmd/core` 时精确报出 3 个文件并退出非 0 |
| 门禁 | 全绿 | ✅ `make gate` |

**补记（同轮修复）**：搬迁 venv 后 `scripts/dev/smoke.sh` 仍查旧路径 `.venv`，导致 `make dev` 第 6 步直接报「缺 L4 环境」。
已改为查 `analysis/.venv`，并全仓库扫描残留引用（`Makefile` 的 `cd analysis && .venv/...` 是正确的，文档均已写成 `analysis/.venv`）。

**没做 / 遗留**：① 跨节点部署仍需 mTLS（设计约束，未实现）；② 控制台无鉴权（`ADR-0020`）；③ `analysis` 镜像 250MB 可再瘦；
④ vendor 让仓库变大（若代理可达可去掉）；⑤ 按用户要求**已停掉本机 cairn 项目的容器**（stop，未删除）以释放 8080。

---

## 2026-09-19 · 修复：L4 证据缓存命名空间错位（`make dev` 第 6 步抓到）+ archcheck 放过构建产物

**做了什么**：给 `make dev` 加了第 6 步「跑一轮 L4」之后，它**立刻红了**，并抓到一个上一轮没暴露的**真 bug**——
① **证据缓存装错命名空间**：判定事件有**两个 ID** —— 外层幂等键是 `decision:<decision_id>`，载荷里才是 `decision_id`；
而 L4 的链引用的是**载荷里的**那个。缓存只装了外层键，于是 `AR-12`（引用必须存在于遥测）把**每一条链都判成引用不存在**、整链作废。
修复：缓存同时装两者；并加回归用例 `test_ar12_evidence_cache_uses_payload_decision_id`（构造 `event_id != decision_id` 的场景）。
② **`archcheck` 把构建产物当源码布局**：`pip install -e .` 在仓库根生成 `shen_analysis.egg-info/`，`ST-1`（顶层目录白名单）因此拦下门禁。
修复：把 `*.egg-info` / `build` / `dist` / `__pycache__` 归入工具目录跳过，并同步 `.gitignore`（它们是产物，列进 `structure.md` 反而让那份布局失真）。

**改了哪些文件**：`analysis/worker.py` · `analysis/tests/test_worker.py` · `scripts/archcheck/main.go` · `.gitignore` ·
`docs/plans/2026-09-19-l4-runtime-and-event-contract.md`

**对应文档**：[`docs/plans/2026-09-19-l4-runtime-and-event-contract.md`](docs/plans/2026-09-19-l4-runtime-and-event-contract.md)（审视记录第 7、8 条）

**验证**：`make gate` 通过；`make dev` 通过（含第 6 步 L4 近线分析）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| `make dev` 第 6 步 | 跑通并打印结论 | ✅ `取事件 3 条 · 去重后 3 条 · 结论 2 条（新 2）`，无 `AR-12` 报错 |
| 回归用例 | 事件 ID 与载荷 ID 不同也不误判 | ✅ `37 passed` |
| 顶层目录检查 | 构建产物不再算源码布局 | ✅ `make gate` 的 `archcheck` 通过 |

**没做 / 遗留**：① L4 **无断点续读**；② 未接真实 LLM；③ `strategy` 结论未回写 `policy`；④ 事件类型有两套载荷形状（核心的 `decision` 与控制台/适配器的 `request_judged`），
只有前者进了 `docs/spec/events.md` —— 后者的字典待补齐；⑤ `honeypot-shell` 与蜜罐协议栈内容按目标排除。

---

## 2026-09-19 · L4 接入运行时（近线 worker）+ 事件契约对齐（跨语言夹具）

**做了什么**：上一轮交付的 L4 只有库与单测 —— 设计里它是链路一环，运行时却**无人调用**。本轮把它**真接上**，并在接的过程中抓到一处**真缺陷**。
① **真缺陷（最重要）**：L4 按**自造字段名**解析事件（`event_id` / `source`），而核心写出的载荷是 Go 字段名（`DecisionID` / `SourceIP`）—— 第一次真接就出现「取到 26 条事件、**0 条可分析**」。测试自洽、真实形状不符，属"自洽但错"的典型。
② **统一契约**：判定事件的 JSON 键名统一为 **snake_case**（`core/internal/control/observer.go` 加 JSON 标签），Python 侧 L4 与控制台同键；契约正文写入 `docs/spec/events.md`。
③ **防复发**：新增**跨语言契约夹具** `api/telemetry/v1/testdata/decision_event.json`（由 Go 结构体**直接生成**），Go 侧 `observer_contract_test.go` 与 Python 侧 `test_event_contract.py` **读同一夹具** —— 任何一方改键即红。
④ **运行时链路（近线 worker，[ADR-0022](docs/background/decisions/0022-l4-near-line-worker.md)）**：`analysis/telemetry.py`（遥测端口 + gRPC 适配器 + 内存替身）与 `analysis/worker.py`（取事件 → `AR-14` 态势去重 → 意图 → 攻击链（`AR-12` 证据校验）→ 策略 → 结论**作为事件**上报）。三条硬边界：**不在业务路径**（近线，挂了不影响请求，`NI-1`）· **无执行能力**（`AR-32`）· **不写存储**（`MD-20`）。
⑤ **结论可见**：控制台新增 `分析结论（L4）` 块与 /api/analysis，概览加 `l4_conclusions` 计数；页面渲染仍**一律 `textContent`**（攻击者可控字符串，16 处，无 `innerHTML` 拼接）。
⑥ **门禁与开发循环**：`make pygen`（生成 Python gRPC 桩）· `make analysis`（跑一轮 L4）· `make dev` 第 6 步跑一轮并打印结论；修掉一处 Make 陷阱 —— 目标名 `analysis` 与同名目录冲突，Make 认为"已是最新"（`.PHONY` 里原先那条是多行续行，早先的替换没生效）。
⑦ **工程化**：`analysis` 成为**可编辑安装的包**（资源随包分发，`AR-24`），导入不再依赖 cwd；Python 依赖锁定（`grpcio` / `grpcio-tools` / `pyyaml` / `ruff` / `pytest`）；静态检查器在本仓库的导入解析误报，用**文件级指令**显式关闭并写明理由（运行时权威判据是 `pytest`）。

**改了哪些文件**：`analysis/telemetry.py` · `analysis/worker.py` · `analysis/events.py` · `analysis/proto/` ·
`analysis/llm/__init__.py` · `analysis/llm/*.py` · `analysis/chain/*.py` · `analysis/intent/recognize.py` · `analysis/strategy/*.py` ·
`analysis/tests/test_worker.py` · `analysis/tests/test_event_contract.py` · `analysis/tests/test_llm_contract.py` · `analysis/tests/test_llm_discipline.py` · `analysis/tests/test_l4_modules.py` ·
`core/internal/control/observer.go` · `core/internal/control/observer_contract_test.go` · `core/cmd/core/main.go` ·
`api/telemetry/v1/testdata/decision_event.json` · `console/cmd/console/main.go` · `console/web/index.html` ·
`pyproject.toml` · `requirements.txt` · `requirements-dev.txt` · `pyrightconfig.json` · `Makefile` · `scripts/dev/smoke.sh` · `scripts/demo/business.py` ·
`docs/spec/events.md` · `docs/background/decisions/0022-l4-near-line-worker.md` · `docs/background/decisions/README.md` ·
`docs/integrate/observability.md` · `docs/integrate/manual-test.md` · `docs/modules/llm-components.md` · `docs/modules/intent.md` · `docs/modules/chain.md` · `docs/modules/strategy.md`

**对应文档**：[`docs/plans/2026-09-19-l4-runtime-and-event-contract.md`](docs/plans/2026-09-19-l4-runtime-and-event-contract.md)（含追溯矩阵与审视 6 条）· [`docs/spec/events.md`](docs/spec/events.md) · [`docs/background/decisions/0022-l4-near-line-worker.md`](docs/background/decisions/0022-l4-near-line-worker.md)

**验证**：`make gate` 通过；`make dev` 通过（新增第 6 步 L4 近线分析）；**真进程端到端**实测（真核心 + 真流量 + 真 worker + 控制台）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 真流量经引擎 → 判定事件 | 事件可被 L4 解析 | ✅ `取事件 6 条 · 去重后 3 条 · 结论 2 条（新 2）` |
| 结论内容 | 意图带证据；策略无诱饵即拒绝 | ✅ `intent accepted=True category=exfiltration` · `strategy accepted=False（没有可用诱饵，AR-15）` |
| 控制台 | 结论可见 | ✅ /api/analysis HTTP 200，2 条；`l4_conclusions: 2`；页面 10149 字节含「分析结论（L4）」 |
| 跨语言契约 | 两侧读同一夹具 | ✅ Go `TestDecisionRecordWireContract` PASS · Python `test_event_contract.py` 4 例通过 |
| Python 单测 | 全过 | ✅ `36 passed` |
| 幂等 | 同窗口重跑不重复 | ✅ `test_worker_is_idempotent_on_rerun`（新 0 / 重复 2） |

**没做 / 遗留**：① **无断点续读**（每轮只取最近若干条，重启不回补更早事件）；② L4 **未接真实 LLM**（`UnconfiguredClient` 显式失败；`AR-19`…`AR-21` 的双阶段收尾尚未被真实走到）；
③ `strategy` 结论**未回写 `policy`**（只是数据，接策略面是下一步）；④ 事件类型字典待 `spec` 下 logs.md 补齐；⑤ `honeypot-shell` 与蜜罐协议栈内容 —— **目标明确排除**，保持推迟。

---

## 2026-09-19 · L4 分析层（Python）：`llm-components` / `intent` / `chain` / `strategy` + Python 门禁

**做了什么**：完成目标里最后一块模块组 —— **L4 分析决策层**，设计指定 **Python**（`language.md` §1 · `TB-2` · `TB-20`），
而 `TB-15` 要求 Python 必须过 `ruff`，所以同时引入**仓库内 `.venv` + 锁定版本**的 Python 门禁（[ADR-0021](docs/background/decisions/0021-l4-python-toolchain.md)）。
① **契约层 `analysis/llm/`**：`AR-15`（独立校验、坏输出**抛异常**，禁止默认值/静默降级）· `AR-16`（统一信封 `{accepted,data}` + 拒绝原因）·
`AR-17`（三段式 JSON 提取 + **扫描位置上限**）· `AR-18`/`AR-23`（列表硬截断并**记录截断数**、分用途长度上限）·
`AR-19`…`AR-21`（双阶段收尾：复用同一会话、收尾契约**只允许事实类字段**、两阶段失败**不写中间数据 + 释放租约**）·
`AR-22`（黑名单三类：泄露 / 自曝 / 超长，业务标识由部署侧注入）· `AR-24`（提示词**资源化** + 启动期校验占位符齐全）·
`AR-25`（`session_id` 一等字段并校验一致）。
② **注入与执行面**：`AR-31`（攻击者可控内容以**结构化数据区**传入并显式标注不可信，原始观测**原样保留**）·
`AR-32`（分析客户端**只**能把提示词变文本；`assert_no_execution_surface` 按**子串**拦截任何执行类成员）。
③ **三个分析模块**：`intent`（确定性规则命中 → 意图类别 + 置信度 + 证据引用；无命中即**拒绝**，不臆测）·
`chain`（阶段有序还原 + 四类识破信号 + **写入前校验证据引用** `AR-12`）· `strategy`（策略**数据**：灰度 ≤20%、阈值有下界；诱饵轮换决策）。
④ **去重**：`AR-14` 态势去重 —— 同一态势 1000 条事件只触发 **1 次** L4（防 LLM 调用量随事件量线性增长）。
⑤ **门禁**：新增 `pyenv` / `pyfmt-check` / `pylint` / `pytest` 四个目标并接进 `make lint` 与 `make test`；**缺环境时报错退出**，不静默跳过。
⑥ 同步：`ADR-0021` · 四份 L4 模块文档状态 · `docs/design/structure.md`（原来写着 `analysis/`/`console/` 「当前不存在」，已改准）· `docs/progress.md` 第 17–20 行 · 决策索引。

**改了哪些文件**：`analysis/__init__.py` · `analysis/events.py` · `analysis/dedupe.py` ·
`analysis/llm/__init__.py` · `analysis/llm/envelope.py` · `analysis/llm/extract.py` · `analysis/llm/contract.py` · `analysis/llm/limits.py` ·
`analysis/llm/blacklist.py` · `analysis/llm/twophase.py` · `analysis/llm/prompts.py` · `analysis/llm/untrusted.py` · `analysis/llm/client.py` ·
`analysis/llm/resources/blacklist.yaml` · `analysis/llm/resources/prompts/intent.md` · `analysis/llm/resources/prompts/chain.md` ·
`analysis/llm/resources/prompts/strategy.md` · `analysis/llm/resources/prompts/finalize.md` ·
`analysis/intent/recognize.py` · `analysis/chain/evidence.py` · `analysis/chain/reconstruct.py` · `analysis/chain/broken.py` ·
`analysis/strategy/generate.py` · `analysis/strategy/rotate.py` ·
`analysis/tests/test_llm_contract.py` · `analysis/tests/test_llm_discipline.py` · `analysis/tests/test_l4_modules.py` ·
`pyproject.toml` · `requirements.txt` · `requirements-dev.txt` · `pyrightconfig.json` · `Makefile` · `.gitignore` ·
`docs/background/decisions/0021-l4-python-toolchain.md` · `docs/background/decisions/README.md` · `docs/design/structure.md` ·
`docs/modules/intent.md` · `docs/modules/chain.md` · `docs/modules/strategy.md` · `docs/modules/llm-components.md` · `docs/progress.md`

**对应文档**：[`docs/plans/2026-09-19-l4-analysis-python.md`](docs/plans/2026-09-19-l4-analysis-python.md)（含追溯矩阵 17 条与审视 5 条）· [`docs/background/decisions/0021-l4-python-toolchain.md`](docs/background/decisions/0021-l4-python-toolchain.md)

**验证**：`make gate` 通过（新增 Python 三项：`ruff format --check` · `ruff check` · `pytest`）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| L4 单测 | 全过 | ✅ `24 passed` |
| 坏输出 | **抛异常**、绝不填默认值 | ✅ `ContractError` |
| 超长输出（5000 噪声 + 300 个花括号） | 扫描上限内失败、放宽后成功 | ✅ |
| 收尾塞「成功」类字段 | 拒绝 | ✅ `ContractError` |
| 两阶段均失败 | 不返回中间数据 + 释放租约 | ✅ `data == {}`、`released == 1` |
| 引用不存在的证据 ID | 整条链作废 | ✅ `MissingEvidence` |
| 同态势 1000 条事件 | 只触发 1 次 L4 | ✅ `admitted == 1` |
| 客户端暴露 `execute()` | 被拦下 | ✅ `AssertionError` |
| 门禁 | Python 三项进 `make gate` | ✅ 门禁通过 |

**没做 / 遗留**：① ⚠️ **L4 尚未接入运行时触发链路**（遥测事件 → `AR-14` 去重 → L4 → 结论落库）—— 目前是**可调用库 + 单测**，线上不会自动跑；
② L4 未接真实 LLM（`UnconfiguredClient` 显式失败）；③ `chain` 的链序与置信度是启发式初值，待真实数据校正；④ `strategy` 输出未经策略面端到端验证；
⑤ `honeypot-shell` 与蜜罐协议栈内容 —— **目标明确排除**，保持推迟。

---

## 2026-09-19 · 剩余模块（非代码类）：`adapter-dns` 配置收口 · `netpolicy` 声明式产物 · `console` 文档与偏离登记

**做了什么**：目标要求「按设计完成所有模块（除细节蜜罐）」。本轮补齐**不需要写数据面代码**的三块：
① **`adapter-dns`（纯配置，`AR-3` 禁自研 DNS）**：重写 README —— 怎么用 · **怎么确认分流生效（两条 `dig`：可疑来源得引擎 IP、其他来源得业务 IP）** · 怎么回退（`NI-11`：改回业务 IP，TTL 压到 30s 就是为了回退快）· **配错影响面**（本形态 `NI-1` 风险最高：解析被接管后引擎即业务可达性前置）；模块文档状态改准并写明「无源码 ⇒ 验证靠 dig」。
② **`netpolicy`（声明式 + 复用 Cilium/Tetragon）**：新增三份可直接 `kubectl apply` 的产物 —— 微隔离（**默认拒绝东西向**，只放行幻网内既定关系；出站禁止到业务网段，`SB-5`）· 假拓扑（用 `gitlab` / `jenkins` / `db-01` 等**行业惯例命名**，不出现自曝名 `OH-1`）· 运行时检测（Tetragon `TracingPolicy`，`action: Post` **只观察不阻断** —— 与 `MD-25`「诱饵面 observe-only」同精神）；另写明事件应经适配器回流遥测面。
③ **`console`**：模块文档补状态/测试方式/偏离登记；新增 [ADR-0020](docs/background/decisions/0020-console-minimal-static-ui.md) 记录「Go 进程 + 静态页（无前端构建）」的取舍与**失效条件**（页面复杂化 / 需要写能力 / 前端门禁被引入时重开），并写明 `language.md` 的控制台行在本记录生效期内**暂缓**（长期仍是 TypeScript）。
④ 同步：决策索引 · `docs/progress.md` 三行（13/16/21）· 根 `README.md` 增四个入口（起环境 / 看观测 / 接入 / 人工测试）。

**改了哪些文件**：`edge/dns/README.md` · `deception/netpolicy/README.md`（新增）· `deception/netpolicy/config/microsegmentation.example.yaml`（新增）·
`deception/netpolicy/config/fake-topology.example.yaml`（新增）· `deception/netpolicy/config/runtime-detect.example.yaml`（新增）·
`docs/modules/adapter-dns.md` · `docs/modules/netpolicy.md` · `docs/modules/console.md` ·
`docs/background/decisions/0020-console-minimal-static-ui.md`（新增）· `docs/background/decisions/README.md` · `docs/progress.md` · `README.md`

**对应文档**：[`docs/plans/2026-09-19-remaining-modules-config-and-declarative.md`](docs/plans/2026-09-19-remaining-modules-config-and-declarative.md)（含追溯矩阵与审视 4 条）

**验证**：`make gate` 通过（含 `make trace` / `make leakcheck`）；三份 YAML 解析验证通过。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| netpolicy 三份产物 | 可 YAML 解析、kind 正确 | ✅ `CiliumNetworkPolicy`×2 · `Service`×3 · `TracingPolicy`×1 |
| 模板不含自曝名 | 行业惯例命名 | ✅ `make leakcheck` 通过 |
| 文档与状态 | 三份模块文档状态与产物一致 | ✅ `docs/progress.md` 行 13/16/21 已同步 |

**没做 / 遗留**：① ⚠️ **`analysis/*` 四个模块（`intent` / `chain` / `strategy` / `llm-components`）仍未实现** —— 设计为 **Python**，而此前用户裁定**不引入 Python 工具链**，两者冲突，需先裁（这是目标「完成所有模块」的最后一块）；
② netpolicy 未接真实 Cilium/Tetragon；③ 控制台无鉴权、单实例；④ 蜜罐细节（`honeypot-shell` / 协议栈内容）**目标明确排除**，保持推迟。

---

## 2026-09-19 · 观测面读路径 + 控制台（Web UI）+ 一键人工测试环境 + 接入/使用文档

**做了什么**：先把「看不见」的**根因**查清并修掉，再交付「能看」的环境与文档。
① **根因三处**：`store.EventStore` **只写不读**、`telemetry.proto` **没有查询 RPC** ⇒ 观测面没读路径；
`DecisionStore.Archive` **从未被调用** ⇒ 判定（分值/命中信号/去向）没落库；示例配置 `rules: []` ⇒ **分数恒 0**，人工测试什么都看不见。
② **读路径**：`store` 新增 `EventQuery首页 /DecisionQuery首页 /List`（有界缓冲、newest first、按时间与类型过滤，内存实现就位）；`api/telemetry/v1` 新增 **`ListEvents`** 读侧 RPC 并重新生成。
③ **记录**：`control` 新增观测面契约（`DecisionRecord首页 /DecisionRecorder首页 /EventLister`，**接口由消费方定义**），服务面每次判定记一笔（事件 + `DecisionStore.Archive`），**失败只记日志**（`NI-1`）；判定细节**只进观测面**（`ST-7` 仍禁止回显给客户端）。
④ **控制台**（`console/`，**新实现**）：Go 进程 + 静态页（`go:embed`，**无前端构建步骤**），提供 首页 /、/api/summary、/api/flow、/api/events、/healthz；页面四块＝概览 / 告警 / 流量访问与流动（含**分值**与**命中信号**）/ 原始事件。渲染**一律用 DOM + `textContent`** —— 页面显示的是攻击者可控的 UA/路径，拼 `innerHTML` 就是存储型 XSS（初版被静态检查抓到 12 处，已全改）。
⑤ **一键环境**：`scripts/demo/run.sh` 起核心 + 假业务站 + 反向代理 + 控制台并打印地址，`make demo` / `make console` 两个入口。
⑥ **示例配置**启用两条**可观察**规则（`ua-headless` 0.6 / `path-probe` 0.3）——否则分数恒 0。
⑦ **文档五份**（目标明确要求）：`docs/integrate/` 的 README · quickstart（5 分钟上手）· business-onboarding（业务怎么接）· observability（怎么看告警/流量/流动）· manual-test（人工测试步骤，含故障注入）。

**改了哪些文件**：`core/internal/store/iface.go` · `core/internal/store/memory.go` · `api/telemetry/v1/telemetry.proto`（+ 生成物）·
`core/internal/control/observer.go`（新增）· `core/internal/control/service.go` · `core/internal/control/telemetry.go` · `core/cmd/core/main.go` ·
`console/cmd/console/main.go`（新增）· `console/web/assets.go`（新增）· `console/web/index.html`（新增）·
`scripts/demo/run.sh` · `scripts/demo/business.py`（新增）· `deploy/config/config.example.yaml` · `Makefile` ·
`docs/integrate/README.md` · `docs/integrate/quickstart.md` · `docs/integrate/business-onboarding.md` · `docs/integrate/observability.md` · `docs/integrate/manual-test.md`

**对应文档**：[`docs/plans/2026-09-19-observability-and-console.md`](docs/plans/2026-09-19-observability-and-console.md)（含追溯矩阵与审视 5 条）· [`docs/integrate/`](docs/integrate/README.md)

**验证**：`make gate` 通过；**真进程端到端**（核心 + 假业务 + 反向代理 + 控制台）实测。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 经引擎发探针流量 | 产生判定记录 | ✅ 3 条决策事件 |
| 控制台读流动 | 看到**分值 + 命中信号** | ✅ `/.git/config score=0.90 signals=[ua-headless,path-git]` · `/x score=0.60 signals=[ua-headless]` · 正常 UA `score=0.00` |
| 概览接口 | 计数与告警数 | ✅ `{"total":6,"by_action":{"route_origin":3},"alerts":0}` |
| 页面可取 | 200 + HTML | ✅ `code=200 bytes=8394 text/html` |
| 事件 ↔ 流动条数 | 一致（幂等 + 解析正确） | ✅ 3 ↔ 3 |

**没做 / 遗留**：① 控制台**语言偏离设计**（设计 TypeScript，本轮 Go + 静态页，按用户裁定不引前端工具链）—— 拟开 ADR-0020 登记；
② 观测面无保留期/分页（内存有界缓冲，只能看最近）；③ 时间基准（事件 UTC / 载荷带本地偏移）待日志字典（spec 下 logs.md，尚未落地）统一；
④ 剩余模块：`adapter-dns` 收口 · `netpolicy` 声明式 · `analysis/*` 四个 Python 模块（需先定门禁）；⑤ `honeypot-shell` 与蜜罐内容——**目标明确排除**，保持推迟。

---

## 2026-09-19 · `ADR-0019`：TLS 终结默认交客户 L0（`E2` 实测驱动的重估）

**做了什么**：把 `E2` 的实测结论落成**决策 + 代码 + 同步**。
① 新增 [`ADR-0019`](docs/background/decisions/0019-tls-termination-belongs-to-l0.md)：由于 `E2` 实测到「我方栈与公有站栈在 **ServerHello 扩展顺序**上可区分」
（JA3S `43-51` vs `51-43`，属不可对齐），**默认姿态改为交客户 L0 终结 TLS** —— 客户 L0 就是真实站同款栈 ⇒ 指纹**构造性一致**，
且我们收到明文 ⇒ `INT-22`（TLS 可读才允许误导处置）仍成立；自终结（内嵌 Caddy 的 `manual`/`acme`）**保留为备选**；
② 代码：新增 `SelfTerminationWarning`（纯函数，点名 `JA3S` 差异与 `ADR-0019`）+ 启动日志接线 + 单测（明文无告警、manual/acme 有且点名关键项）；
③ 同步 **8 份文档**：`ADR-0017` 标注「TLS 部分被 0019 取代」（ID 与正文保留，`D-6`）· 决策索引加 0019 行 ·
`docs/modules/adapter-proxy.md` §1/§3.1/§8/§9 · `docs/design/architecture.md` §10.2 缺口 14 · `docs/design/integration.md` 的 `INT-22` 注记 ·
`edge/proxy/README.md` 与 `edge/proxy/config/front-proxy.example.env`（写明「默认 off = L0 终结」**及其原因**）。
`SHEN_PROXY_TLS_MODE` 的默认值本来就是 `off` —— 本轮改的是**它为什么是默认**以及**自终结必须知情**。

**改了哪些文件**：`edge/proxy/embed.go` · `edge/proxy/cmd/proxy/main.go` · `edge/proxy/embed_test.go` ·
`docs/background/decisions/0019-tls-termination-belongs-to-l0.md`（新增）· `docs/background/decisions/0017-caddy-l1-base.md` ·
`docs/background/decisions/README.md` · `docs/modules/adapter-proxy.md` · `docs/design/architecture.md` · `docs/design/integration.md` ·
`edge/proxy/README.md` · `edge/proxy/config/front-proxy.example.env`

**对应文档**：[`docs/plans/2026-09-19-tls-termination-belongs-to-l0.md`](docs/plans/2026-09-19-tls-termination-belongs-to-l0.md)（含追溯矩阵与审视 4 条）

**验证**：`make gate` 通过（含 `make trace` 的规则 ID 与链接检查）；`go test ./edge/proxy/ -run SelfTermination` 通过。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| `tls_mode=off`（默认） | 无告警 | ✅ `TestSelfTerminationWarning` |
| `tls_mode=manual` / `acme` | 告警且点名 `JA3S` / `L0` / `ADR-0019` | ✅ 同上 |
| 取代关系 | 0017 标注、索引含 0019、旧 ID 未动 | ✅ `make trace` |

**没做 / 遗留**：① `E2` 仍是**单站单次**初测（本决定建立在一份采样上，须对客户真实站重复 ≥3 次）；
② L0 终结时的**来源 IP 透传**（`INT-23`：XFF / PROXY protocol）需写进接入物料（integrate/ 目录未建）；
③ L0↔引擎那一跳明文还是 mTLS 未定（跨节点部署）；④ 自终结 + ACME 仍保留但**未被本决定背书**；
⑤ ⚠️ **`ST-10` / `K-20` 误调度取舍**仍待你裁决。

---

## 2026-09-19 · 降低 Caddy 耦合的可见性：耦合面自动提取 + 升级兼容锁

**做了什么**（用户要求：*Caddy 不要耦合太深，升级要快且保证功能支持*）：
① **耦合面实测**：4 个非测试文件依赖 Caddy，用到的导出符号与模块 ID 逐条列出；
② **`make caddy-surface`**：把耦合面做成**从代码自动提取**的命令（包 / 导出符号 / 字符串引用的模块 ID）—— 不手写，故不会腐烂；
③ **升级兼容锁**（`edge/proxy/caddy_compat_test.go`）：五条编译期契约断言 + **四类「不会编译失败」的假设**断言 ——
默认 `Server` 头值（我们的 `OH-2` 清洗按值判断才删）· `caddy.Duration` 单位 · 三个模块 ID（`headers` / `reverse_proxy` / 我们自己的）；
失败信息里**直接写「该改哪里」**，升级后一红就知道动哪一行；
④ **模块文档新增 §3.1 升级检查表**：耦合面风险分级（编译可见 vs 静默失效）+ **四条命令的升级流程**，并写明
「**禁止**只跑 go build 就宣布升级完成」的理由（带「不会编译失败」的三类正是最容易静默坏的地方，其中默认 `Server` 头直接关系 `OH-2`）；
⑤ ADR-0017 的「未解决」补上升级视角，并把「是否进一步收窄 Caddy 绑定」列作**可选增强**（见下）。

**改了哪些文件**：`edge/proxy/caddy_compat_test.go`（新增）· `Makefile` · `docs/modules/adapter-proxy.md` ·
`docs/background/decisions/0017-caddy-l1-base.md`

**对应文档**：[`docs/plans/2026-09-19-caddy-coupling-guardrails.md`](docs/plans/2026-09-19-caddy-coupling-guardrails.md)（含追溯矩阵与审视 3 条）

**验证**：`make gate` 通过；`go test ./edge/proxy/ -run CaddyAssumptions` 通过；`make caddy-surface` 可用。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 默认 `Server` 头值 | 仍为 `Caddy` | ✅ `TestCaddyAssumptionsStillHold` |
| `caddy.Duration` 单位 | 纳秒往返一致 | ✅ 同上 |
| 三个模块 ID 注册 | 都在 | ✅ 同上 |
| Handler 五条契约 | 编译期断言 | ✅ `caddy_compat_test.go` |
| 耦合面提取 | 一条命令列出 | ✅ `make caddy-surface` |

**没做 / 遗留**：① **未做物理降耦**（`handler.go` 仍含「Caddy 模块契约 + 判定胶水」、`policy.go` 仍出现 `caddyhttp.MiddlewareHandler`）——
属**可选增强**，需单独一轮承担回归风险，已登记在变更包 §7；② 上游字段顺序 / `ResponseHeaderTimeout` 语义未单独锁；
③ ⚠️ **`E2` 的「TLS 终结归属」仍待你裁决**。

---

## 2026-09-19 · `NI-12` 的 `V-1…V-4` 自动化 + `AR-29` 空载下界（把 NI-1 从承诺变成证据）

**做了什么**：`NI-1` 是**最高优先级**约束、`NI-12` 要求它的强制测试 `V-1…V-5` **必须纳入 CI** —— 而此前只有「待补」两个字。
本轮把可自动化的四条做实（**产品代码零改动**）：
① **`V-1` 杀死引擎**：起真 gRPC 判定面再 `Stop()`（真「进程被杀」，不是指一个从未存在的端口），业务 20/20 正常；
② **`V-2` 决策延迟超预算**：判定延迟 400ms（预算 50ms），业务 20/20 正常，并断言总耗时不接近 20×400ms（证明走的是超时放行 `NI-4`）；
③ **`V-3` malformed protobuf**：用**裸 TCP 服务回垃圾字节**造畸形响应（真 gRPC 服务端在 wire 层就拒绝非法 protobuf，造不出来），业务 20/20 正常；
④ **`V-4` 非法决策值**：UNSPECIFIED 与**越界值 99** 两种，都回落放行（`NI-5`）。
⑤ 新增 `AR-29` **空载下界**基准（`make bench`）：直连 37.7 µs / 经引擎 85.7 µs ⇒ **额外 ≈48 µs**（5 ms 预算的 1%），并登记进实验 `E3`（写明**这只是下界**）。
⑥ `V-5`（CPU 饱和下 P99 不劣化）**明确不做**：它要基线 P99 与真实负载，用单机微基准伪造「P99」就是假证据 → 归接入演练。
期间修掉我自造的两处错（`startJudgeServer` 不交出句柄导致「杀不死」、`latency_test.go` 漏 `net` 导入），并把模块文档里过期的「待补」改为已实现 + 单列 `V-5` 去向。

**改了哪些文件**：`edge/proxy/failopen_test.go`（新增，4 例）· `edge/proxy/latency_test.go`（新增，2 基准）· `Makefile` ·
`docs/modules/adapter-proxy.md` · `docs/background/notes/pending-experiments.md`

**对应文档**：[`docs/plans/2026-09-19-ni12-vseries-and-ar29-floor.md`](docs/plans/2026-09-19-ni12-vseries-and-ar29-floor.md)（含追溯矩阵与审视 3 条）

**验证**：`make gate` 通过；`go test ./edge/proxy/ -run 'TestV[1-4]_' -count=1` 4 例全绿；`make bench` 出数。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| `V-1` 引擎被杀 | 业务 20/20 正常 | ✅ `TestV1_KilledCoreKeepsBusinessAlive` |
| `V-2` 延迟 400ms（预算 50ms） | 20/20 正常且不等核心 | ✅ 0.05s（若等核心需 8s） |
| `V-3` 畸形响应 | 20/20 正常 | ✅ `TestV3_MalformedCoreResponseKeepsBusinessAlive` |
| `V-4` UNSPECIFIED / 越界 99 | 回落放行 | ✅ `TestV4_IllegalDecisionValueFallsBackToOrigin` |
| `AR-29` 空载下界 | 出两臂数值 | ✅ 直连 37.7µs · 经引擎 85.7µs · **额外 ≈48µs** |

**没做 / 遗留**：① `V-5`（CPU 饱和下 P99）归接入演练 `E3`；② 故障注入目前是**串行** 20 次（未覆盖并发下的超时/回落）；
③ `AR-29` 只有下界（大响应/流式/TLS/注入都会抬高）；④ ⚠️ **`E2` 的「TLS 终结归属」仍待你裁决**。

---

## 2026-09-19 · 转发路径边界行为实测锁定（升级 · 流式 · 大响应 · 大上传 · 协议版本）

**做了什么**：把上一轮列的「转发未覆盖项」全部**实测**并锁成断言（本轮**产品代码零改动**，只把已成立的行为变成被断言的行为）：
① **协议升级（WebSocket）**：真 raw TCP 打 `101`，并验证升级后仍能双向收发（说明我们这层的 `Hijack` 没被破坏）；
② **流式（SSE）**：用**首块到达时间**证明没有被全量缓冲（内容全对不能证明这一点，只有时间能）；
③ **大响应**（2 MiB HTML，跨过注入缓冲上限）：**不注入、不截断**，长度逐字节一致；
④ **大上传**（8 MiB）：上游收到完整字节数 —— 证明我们**不读请求体**；
⑤ **观测构造**不含 body 内容；
⑥ **协议版本**：客户端↔我们 = HTTP/2，我们↔上游 = **HTTP/1.1**（实测发现，核实为 `reverse_proxy` 默认行为，与 nginx/Envoy 同类 —— **不是缺陷**，改为断言并写进模块文档，不加无谓开关）。
期间修掉我自己造的两处错（占位的 `judgev1OriginResponse`、`%d` 格式化字符串），并把一次**误判**（把上游 h1.1 当 bug）纠正为文档化行为。

**改了哪些文件**：`edge/proxy/forwarding_test.go`（新增，6 例）· `docs/modules/adapter-proxy.md`

**对应文档**：[`docs/plans/2026-09-19-forwarding-boundary-verification.md`](docs/plans/2026-09-19-forwarding-boundary-verification.md)（含追溯矩阵与审视 3 条）

**验证**：`make gate` 通过（含 `make trace` / `make leakcheck` / `make archcheck` / 单测 `-race`）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| WebSocket 升级 | 101 + 升级后双向收发 | ✅ `TestWebSocketUpgradePassesThrough` |
| SSE 流式 | 首块早于「全部产完」 | ✅ 首块 < 2×间隔（全程 0.45s） |
| 2 MiB HTML（配了注入规则） | 不注入、不截断 | ✅ 长度一致且无注入片段 |
| 8 MiB 上传 | 上游收满 8 MiB | ✅ `TestLargeUploadReachesUpstreamIntact` |
| 观测构造 | 不含 body 内容 | ✅ `TestObservationDoesNotCarryBody` |
| 协议版本 | 客户端 h2 / 上游 h1.1 | ✅ `TestClientUsesHTTP2UpstreamUsesHTTP11` |

**没做 / 遗留**：① **HTTP/3（QUIC）未实测**（要引 QUIC 客户端依赖；风险低：同一路由的另一协议）；② WebSocket over h2（RFC 8441）未测；
③ 上游要求 h2 的后端目前无法表达（需要时再加开关）；④ ⚠️ **`E2` 的「TLS 终结归属」仍待你裁决**（上一轮提出，关系 `A2` 与 `ADR-0017`）。

---

## 2026-09-19 · `E2`（TLS 指纹一致性）工具落地 + 主臂初测：**不可对齐**

**做了什么**：把 P0 实验 `E2` 从「设计」推进到「可复现工具 + 有结论」。新增 `scripts/fingerprint`（只依赖 Go 标准库 + 自写握手解析）：
① `capture` 采集协商版本 · 套件 · ALPN · 会话恢复 · OCSP · **ServerHello 扩展序列** · ClientHello 形状 · 证书链形状；
② `diff` 由代码判定三种结论（不可区分 / 可区分可调齐 / 不可对齐），**判定标准写死在 `diff.go`**（E2 要求不可事后调整），
最坏结论用**退出码 2** 拦住后续动作；③ `Makefile` 加 `fp-capture` / `fp-diff` 两个入口；④ 10 例单测（解析 · 截断容错 · JA3/JA3S 口径 · GREASE 过滤 · 三种判定）。
**主臂初测（真跑）**：A 组 = `example.com:443`（公有站 OpenSSL 系栈），B 组 = 本机内嵌 Caddy（Go 标准库 crypto/tls）。
一致：TLS 1.3 · 套件 · ALPN · 会话恢复；**本质差异：ServerHello 扩展序列 `51-43` vs `43-51`**（JA3S `772,4865,51-43` vs `772,4865,43-51`）；
OCSP 装订与证书链形状另有差异。**结论：不可对齐 ⇒ 按 E2 判定标准，威胁模型 `R-1` 必须重估**。
原始数据入仓 `docs/background/research/e2-tls-fingerprint/`，结论与**局限**写进 `docs/background/notes/pending-experiments.md`。
工具开发中修掉两处自己造的问题：解析器遇截断字节直接放弃（测试暴露）、初版用 MD5 算 JA3 哈希（改为输出规范原文，比对更实用）。

**改了哪些文件**：`scripts/fingerprint/main.go` · `scripts/fingerprint/tls.go` · `scripts/fingerprint/diff.go` ·
`scripts/fingerprint/fingerprint_test.go` · `scripts/fingerprint/README.md`（均新增）· `Makefile` ·
`docs/background/notes/pending-experiments.md` · `docs/background/research/README.md` ·
`docs/background/research/e2-tls-fingerprint/2026-09-19-example.com.json` · `docs/background/research/e2-tls-fingerprint/2026-09-19-ours-caddy.json`

**对应文档**：[`docs/plans/2026-09-19-e2-tls-fingerprint-tool.md`](docs/plans/2026-09-19-e2-tls-fingerprint-tool.md)（含追溯矩阵与审视 4 条）·
[`docs/background/notes/pending-experiments.md`](docs/background/notes/pending-experiments.md) 的 `E2` 结果段

**验证**：`make gate` 通过；`go test ./scripts/fingerprint/ -count=1` → 10 例全绿；真跑两次采集 + 一次对比（退出码 2）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 采真实站 | 出 JSON + JA3S | ✅ `TLS 1.3` · `JA3S: 772,4865,51-43` |
| 采我方栈 | 同上 | ✅ `TLS 1.3` · `JA3S: 772,4865,43-51` |
| 对比判定 | 出结论 + 后续动作 | ✅ `不可对齐`（本质差异：服务端扩展序列），退出码 `2` |
| 解析与判定单测 | 10 例全绿 | ✅ `ok shen/scripts/fingerprint` |
| 截断容错 | 半条记录也能解析 | ✅（该测试首次失败 → 修好） |

**没做 / 遗留**：① ⚠️ **「TLS 终结归属」待你裁决** —— 候选：交客户 L0 终结（与真实站同款栈 ⇒ 指纹天然一致，且仍满足 `INT-22`）/ 保持自终结接受可区分 / 换同款 TLS 实现；
② A 组只采 1 站 1 次（结论对「客户站栈是否与我方同款」高度敏感，**不得当最终结论**）；③ JA4 未做；④ 「我方→上游」方向未做真实对比。

---

## 2026-09-19 · 转发/欺骗路径硬化：修掉对外可见面的代理栈指纹（`OH-2`）+ 蜜罐范围裁定

**做了什么**：① 按用户裁定把**蜜罐范围收窄为「接入架构」**（入口 · 后端池 · 协议契约 · 生命周期），
命令表 / 内存文件系统 / 会话水印 / 真实协议栈等内容层**推迟到专项调研**（同步了模块文档状态、进度表与根 README）。
② 按同一句话的要求**实测式复查前置的流量转发**（不看代码猜，直接起真进程看客户端收到什么），查出并修掉两处 **`OH-2` 违规**：
`Via: 1.1 Caddy` 会被透给对手、上游不可达的 502 带 `Server: Caddy`。
修法分两处（缺一不可）：中间件里新增 `headerSanitizer`（**一律**删 `Via`；`Server` **仅当**等于 Caddy 默认值时删，
上游给的值原样保留），并在 `BuildConfig` 的 `errors` 路由里删头 —— 因为实测证明**错误响应由 Caddy 服务器层写出、不经过中间件**；
请求侧也删 `Via`，免得幻境后端回显请求头时把我们的栈指纹带上屏。
③ 顺带查清一个坑并记录：`caddyhttp.ServerHeader` 是**包级变量且在 init 时已拷贝**，运行时改它无效（避免后人重复尝试）。
④ 新增 4 个测试（5 子例的清洗器语义 + 错误路径端到端 + 转发语义补 `Via` 断言）。

**改了哪些文件**：`edge/proxy/handler.go` · `edge/proxy/embed.go` · `edge/proxy/proxy_test.go` · `edge/proxy/embed_test.go` ·
`docs/modules/adapter-proxy.md` · `docs/modules/honeypot-shell.md` · `docs/progress.md` · `README.md`

**对应文档**：[`docs/plans/2026-09-19-forwarding-deception-hardening.md`](docs/plans/2026-09-19-forwarding-deception-hardening.md)（含追溯矩阵与审视 5 条）

**验证**：`make gate` 通过（含 `make trace` / `make leakcheck` / `make archcheck` / 单测 `-race`）；真进程实测三种情形。

**证据**：
| 场景 | 修复前（实测） | 修复后（实测） |
| --- | --- | --- |
| 正常转发（上游带 `Server`） | 有 `Via: 1.1 Caddy` | ✅ 无 `Via`；上游 `Server` 原样透传 |
| 上游不带 `Server` | `Via` + `Server: Caddy` | ✅ 无 `Via`、无 `Server: Caddy` |
| 上游不可达（502） | `Via` + `Server: Caddy` | ✅ 502 · 无 `Server` · 无 `Via` |
| 业务响应其他头与状态码 | —— | ✅ 一个字不改（`TestHeaderSanitizerKeepsOtherHeaders`） |
| 清洗器语义（5 子例） | —— | ✅ `TestHeaderSanitizer*` |
| 错误路径端到端 | —— | ✅ `TestNoProxyFingerprintOnErrorPath` |

**没做 / 遗留**：① **蜜罐内容层推迟**（按你的裁定）；② HTTP/3 与 WebSocket 未实测（清洗器透传 `Hijack`/`Flush`，但没跑过）；
③ `Alt-Svc`（h3 广播）保持默认 —— 真实站点启用 h3 时也有，暂判正常；④ 流式（SSE/分块）与大文件传输未在本轮复测；
⑤ 上游发多行 `Server` 时原样透传（未去重）。

---

## 2026-09-19 · L2 第一批：`honeypot-protocol` 框架落地

**做了什么**：实现 `deception/` 下的第一个模块（此前只有设计文档），交付**契约 + 确定性框架**，
把「真实协议栈」留作**核心逻辑接缝**（这正是你说的「只留下后续要设计的核心逻辑」）：
① **契约**（`deception/honeypot/iface.go`）：`Protocol`（适配器）· `Session`（会话录制口，会话 ID **一等字段**，`AR-25`）·
`SessionFactory`（由消费方提供，生产实现将来接 `api/telemetry/v1`）· `Registry`（名字唯一：重名必须报错，
否则「哪个实现生效」取决于注册顺序）。
② **运行框架**（`runner.go`）：`Start` · `Stop` · `Addr` · `Stats`；**并发上限必须存在且超限即拒并计数**（`MD-16`）；
**停止时对称回收**（`MD-15`）：停收新连接 → 等宽限期 → 强制关闭在途连接 → 等 goroutine 退出。
③ **最小真实适配器**（`banner.go`）：问候 + 双向录制 —— 它既让框架可跑可测，本身就是可用的协议门牌仿真。
④ **显式接缝**（`errors.go`）：`NotImplemented(what)` 返回「尚未实现」错误 —— **故意失败**，不静默返回空结果。
⑤ **7 例单测**：注册表三态 · banner 双向录制 · 超限拒绝与计数 · 未知协议/重复启动 · `Stop` 对称性与监听关闭 · 未接缝错误。
⑥ 同步文档：模块文档（状态/§7 测试拆分/§8 未决 3·4/§9）· `docs/design/structure.md` §1.5（`deception/honeypot/` 拆成 ✅）·
`docs/progress.md` 第 14 行 · 根 `README.md`（阶段 3 「已起步」+ 计数 14→15 包 / 196→203 测试）。

**改了哪些文件**：`deception/honeypot/iface.go` · `deception/honeypot/errors.go` · `deception/honeypot/runner.go` ·
`deception/honeypot/banner.go` · `deception/honeypot/protocol_test.go`（均为新增）· `docs/modules/honeypot-protocol.md` ·
`docs/design/structure.md` · `docs/progress.md` · `README.md`

**对应文档**：[`docs/plans/2026-09-19-deception-l2-l3-frameworks.md`](docs/plans/2026-09-19-deception-l2-l3-frameworks.md)
（含追溯矩阵与审视 5 条）· [`docs/modules/honeypot-protocol.md`](docs/modules/honeypot-protocol.md)

**验证**：`make gate` 通过（含 `make trace` / `make leakcheck` / `make archcheck` / 单测 `-race`）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 注册表 nil/空名/重名 | 三者都报错，重名带协议名 | ✅ `TestRegistryRejectsNilEmptyAndDuplicate` |
| banner 会话 | 问候 + 我方发与对手写的**都被录制**，会话 ID 非空 | ✅ `TestBannerServesAndRecordsBothDirections` |
| 并发上限（`MD-16`） | 超限连接被立即关闭且计数 | ✅ `TestRunnerRejectsConnectionsOverLimit`（`Rejected=1`） |
| 对称回收（`MD-15`） | 宽限期内返回、监听关闭 | ✅ `TestRunnerStopIsSymmetricAndClosesListener` |
| 无出站拨号（`SB-6`） | 代码里没有 `net.Dial` | ✅ `grep -n "Dial" deception/honeypot/*.go` → 空 |
| 分层 import 纪律 | 不 import `core/internal` 与 `api/` | ✅ `make archcheck` |
| 测试与包数 | 196 → 203 · 14 → 15 包 | ✅ `ok shen/deception/honeypot` |

**没做 / 遗留**：① ⚠️ **真实协议栈未实现**（SSH 密钥交换 / MySQL 握手 / Redis RESP / 凭证捕获 / 命令解释）—— 即本模块的核心逻辑接缝；
② `honeypot-shell`（命令表 / 内存 FS / 水印）· `netpolicy`（声明式）· `adapter-dns`（配置收口）**属本批剩余三项**，下一轮继续；
③ `MD-14` 的**进程组回收**只适用于子进程形态，本框架不是子进程（已在文档写明，不假装做到）；
④ `Session` 的生产实现（接 `api/telemetry/v1` + 保留期 `NI-13`）未做，单测用的是替身。

---

**做了什么**：① **初始化 git 仓库**（`git init -b main`）并打**基线提交**（220 个文件：全部文档 · 代码 · 契约 · 部署模板 ·
门禁工具 · `.pi/` 项目技能）；提交信息里写明了「此前无版本控制」，**不假装有历史**。
仓库级提交身份设为 `Shen Dev <dev@shen.local>`（不动全局配置；本机原本没有任何 git 身份）。
② **新增三个目标，把提交变成流程的一部分**：`make commit MSG="…"`（缺 MSG 失败、工作区干净时拒绝空提交、
正文固定写「验证：make gate 通过」） · `make clean-check`（工作区必须干净，否则列出文件与下一步命令） ·
`make done MSG="…"`（**一条命令跑完三段：门禁 → 提交 → 校验干净**；`make` 遇错即停，所以门禁不过就不会提交）。
③ **四份工作流文档同步这条纪律**：`AGENTS.md` §4（循环标题加「→ 提交」、表格新增第 ⑤ 段、
「三条不能破」→「四条不能破」、「每轮留下五样」→「六样」） · [`.pi/devloop.md`](../../.pi/devloop.md)（新增 `commit_cmd` / `commit_only`） ·
[`docs/kb/dev-workflow.md`](docs/kb/dev-workflow.md)（时序图新增第 ⑦ 步「提交」，并写明「没有提交就没有回退点」） ·
[`.pi/skills/dev-loop-project/SKILL.md`](.pi/skills/dev-loop-project/SKILL.md)（入口表与 DoD 各补一条）。
④ 顺手修正 `Makefile` 里四处**跨行 `if … then \` 守卫** —— shellcheck 报 `SC1089` 解析失败（真告警，会让门禁红），改为单行形式。

> 为什么做这件事：上一轮我把 `scripts/tracecheck` 的十余个函数误删了，**没有任何回退点**，
> 只能从 pi 会话记录里考古复原。有了提交，那只是一条 `git checkout` 的事。

**改了哪些文件**：`Makefile` · `AGENTS.md` · `.pi/devloop.md` · `docs/kb/dev-workflow.md` · `.pi/skills/dev-loop-project/SKILL.md`
（新增版本库 `.git/`；基线提交 `abf9a34`）

**对应文档**：[`docs/plans/2026-09-19-git-and-commit-workflow.md`](docs/plans/2026-09-19-git-and-commit-workflow.md)（含追溯矩阵与审视 4 条）

**验证**：`make gate` 通过；`make done MSG="…"` 跑通（门禁 → 提交 → 工作区干净）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 首次提交前核对忽略清单 | 密钥类文件与 `.bin/` 不入库 | ✅ 核对通过（`.gitignore` 生效） |
| 基线提交 | 有提交且工作区干净 | ✅ `abf9a34`；`git status --porcelain` 为空 |
| `make clean-check`（脏） | 失败并列文件 | ✅ `工作区不干净 —— 这一轮的改动还没提交：` + `M Makefile` |
| `make clean-check`（净） | 通过 | ✅ `工作区干净：本轮改动都已提交。` |
| `make commit` 缺 `MSG` | 失败且不提交 | ✅ `MSG 是必需的…` |
| `make commit` 无可提交内容 | 失败（拒绝空提交） | ✅ `没有可提交的改动（工作区已干净）。` |
| `make done` | 门禁 → 提交 → 干净 | ✅ 见变更包 §6 |
| `Makefile` 跨行 if | shellcheck 不再报 | ✅ `SC1089` 消失，门禁绿 |

**没做 / 遗留**：① **未配远端**（纯本地仓库）—— 机器损坏即历史丢失；你定托管后两行命令即可推上去；
② 提交信息只要求非空，**无格式规范**（长历史后检索不便）；③ **未加 pre-commit hook**（有人可绕过 `make done` 直接提交）；
④ 分支/评审策略未定（当前单分支 `main`）；⑤ 仓库 24 MB（含调研材料，目前可接受）。

---

**做了什么**：① 按用户裁定，**撒销我自行引入的 Python/TS 门禁工具链**（删 `pyproject.toml`、`requirements-dev.txt`、`.venv/`，
以及 `Makefile` 里的 `PY`/`RUFF`/`NPMBIN` 与 `py-tools`/`pyfmt-check`/`pystatic`/`pytest`/`ts-tools`/`ts-check` 六个目标，`lint` 链还原为 Go-only）。
② **修复误伤**：我在撒回时用「起止位置切片」还原代码，把 `scripts/tracecheck/main.go` 中间的十余个函数一起切掉了（375 行，原约 1000 行）。
仓库无版本控制，源文件不可从磁盘找回；我从会话记录取回 2026-09-18 的完整副本（27 个函数），
再按「考古表」逐项补齐后续改动：`docs/log.md` 改名（5 处硬编码）· **裸前缀**解析（`AR` 而非 `AR-1`）·
`backtickPaths` 的白名单过滤（裸目录名不当路径核）· `iface.go` 检查改用 `TC-1` + 只提示（原来错误地用了**真规则 ID** `ST-14`）·
补 `finding.Subject`/`Note` · `pathExists`/`isLineRef` · `report()` 的提示与错误分区。
③ **重建三项缺失检查**（按技能 `audit` §6 与 `.pi/devloop.md` §4 的文档化行为）：
`checkSkills`（`DEV-3`：AGENTS.md 点名的技能必须存在且带 `name`/`description`）·
`checkStaleMarkers`（`TC-2`：同一行既写「待建」又点名一个**已存在**的路径）·
`checkDanglingLinks`（`TC-3`：markdown 链接的悬空目标，含 `../` 与 `~/`）。
④ 新增 [`.gitignore`](../../.gitignore)（`ST-20` 此前**未满足**：仓库没有任何忽略清单）：密钥类配置、`.bin/`、构建产物。
⑤ 清理两处死代码（`splitID` / `prefixOf`，`parsePrefixes` 改写后不再使用）；
`Makefile` 的 `@test -x …` 全改为 `@if [ ! -x … ]; then … fi`（前者被 shellcheck 误判为 bats 注解，是真实告警）。

**改了哪些文件**：`Makefile` · `scripts/tracecheck/main.go` · `.gitignore`（新增）
（删除：`pyproject.toml` · `requirements-dev.txt` · `.venv/`）

**对应文档**：[`docs/plans/2026-09-19-revert-python-and-tracecheck-recovery.md`](docs/plans/2026-09-19-revert-python-and-tracecheck-recovery.md)（含追溯矩阵 · 考古表 · 审视 7 条）

**验证**：`make gate` 通过（含 `make trace` / `make archcheck` / `make leakcheck` / 单测 `-race`）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 恢复后 tracecheck 输出 | 与误伤前逐字一致 | ✅ 提示 1 条（`TC-1` edge/mirror）+ 豁免 3 条 + 追溯检查通过 + 六类检查行 |
| `TC-3` 能真报错 | 悬空链接命中 | ✅ 探针（一条指向不存在文件的 markdown 链接）→ 报 TC-3「悬空链接：目标不存在」 |
| `TC-2` 能真报错 | 过期状态标记命中 | ✅ 探针（已存在路径 + 「待建」写在同一行）→ 报 TC-2 过期状态标记 |
| 相对上级链接 | `../` 也核 | ✅ `docs/design` 下指向上级目录不存在文件的链接 → 报 TC-3 |
| `DEV-3` 能真报错 | 不存在的技能路径命中 | ✅ 首次运行报出了正则误抓出的不存在路径；修正则后无报 |
| `TC-3` 口径未被改弱 | 与误伤前一致（只查 markdown 链接） | ✅ 活文档里存在 `` `docs/spec/logs.md` `` 等**不存在**的反引号路径，而误伤前后**都是绿的** |
| Go 静态检查 | 无死代码 | ✅ 删 `splitID`/`prefixOf` 后 `staticcheck` 干净 |
| Python/TS 残留 | 零 | ✅ `grep` 关键词计数 0 |

**没做 / 遗留**：① **`TB-15` 对 Python / 前端的门禁仍未满足**（而 `language.md` §1 把 L4 定为 Python、控制台定为 TypeScript）——
要不要引入那两套门禁是**决策项，待你拍板**；② L4 四个模块与 `console` **仍未实现**（取决于①）；
③ `adapter-dns` / `honeypot-protocol` / `honeypot-shell` / `netpolicy` 四项**不依赖新语言**，可立即开工；
④ 恢复版的内部实现不可能与误伤前逐行相同，我用「输出逐字比对 + 三项检查反向探针」证明行为等价；
⑤ `TC-3` 是否收紧到反引号路径，另立一轮。

---

## 2026-09-19 · 2b 内容通路（响应改写规则经策略面下发）

**做了什么**：把**响应改写规则**从「每个适配器改 env」并成为**核心配置 → 策略面下发**。
① **核心配置新增 `injects` 段**（数据驱动，`ST-24`）：`kind`（可选，登记值 `developer_api` / `instruction_file` / `hidden_link` / `dataset`）/ `snippet`（必填）/ `marker`（可选，空 = `</body>`）；
空片段或未登记分类 → **启动失败**。
② **载荷新增可选 `inject_rules[]`**（[`docs/spec/policy-payload.md`](docs/spec/policy-payload.md)）；`schema_version` **保持 1**（旧适配器忽略未知字段，bump 反而会让未升级的适配器拉不到策略）。
③ **「缺省」与「空数组」必须可区分**（这是运营的**关闭手段**）：配置里**不写**该段 → 载荷**不出现**该字段，适配器继续用本地 env；写 `injects: []` → **显式下发空数组**，适配器关掉注入。适配器侧对应三态：缺省 → 保留本地；空数组 → 关掉；非空 → 远端接管（本地不再注入）。
④ **注入器按请求读取**：`injectingTransport` 改为持 Handler 并每请求取 `currentInjector()` —— 否则建后端时钉住的注入器会继续用旧规则，远端热变更不生效。
⑤ **非法规则 → 整份策略不应用**（禁止半应用）+ 回执 `applied=false` + 原因。
⑥ **共享类型**：新增 `contract.InjectRule` / `InjectKinds`（按上一轮修正后的 `MD-5`，进程内共享类型落 `contract/`）。
⑦ 文档同步：配置契约新增 §2.12、模块文档（`policy` / `adapter-proxy` / `edge-injection`）、`docs/design/structure.md` §1.6.4、
`docs/design/architecture.md` §10.2 缺口 13（按决策只更新现状：**注入规则的归属已定**，仍未定的是「幻境正文由谁产出」）、进度与 README 计数。

**改了哪些文件**：`core/internal/contract/deception.go` · `core/internal/policy/policy.go` · `core/internal/policy/server.go` ·
`core/internal/policy/server_test.go` · `edge/proxy/policy.go` · `edge/proxy/handler.go` · `edge/proxy/policy_test.go` ·
`edge/proxy/proxy_test.go`（旧注入 transport 用例改到新形态）· `deploy/config/config.example.yaml` ·
`docs/spec/policy-payload.md` · `docs/spec/config.md` · `docs/modules/policy.md` · `docs/modules/adapter-proxy.md` ·
`docs/modules/edge-injection.md` · `docs/design/structure.md` · `docs/design/architecture.md` · `docs/progress.md` ·
`docs/modules/README.md` · `README.md` · `edge/proxy/README.md` · `edge/proxy/config/front-proxy.example.env`

**对应文档**：[`docs/plans/2026-09-19-content-path-injects.md`](docs/plans/2026-09-19-content-path-injects.md)（含追溯矩阵与审视记录 7 条）·
[`docs/spec/policy-payload.md`](docs/spec/policy-payload.md) · [`docs/spec/config.md`](docs/spec/config.md) §2.12

**验证**：`make gate` 通过（含 `make trace` / `make leakcheck` / `make archcheck` / 单测 `-race`）；`make dev` 通过；
**端到端实测**（真进程 · 真 gRPC）：核心下发 `injects` → 适配器应用（它**没有**设本地 `SHEN_PROXY_INJECT`）→ 探针拿到
「幻境正文 + 注入片段」，正常会话拿到纯净业务响应。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 探针 UA（改道 + 策略面注入） | 幻境正文含策略面下发的片段 | ✅ `<html><body>MIRAGE-PAGE<!--INJECTED-BY-POLICY--></body></html>` |
| 正常会话（业务侧） | 零注入（`INT-8`） | ✅ `<html><body>REAL-BUSINESS</body></html>` |
| 适配器日志 | 拉到并应用策略 | ✅ `已应用策略 e2e-inject v1（1 个改道后端）` |
| `injects` 校验 | 空片段 / 未登记分类 → 启动失败 | ✅ `TestInjectsSectionValidation`（5 子例） |
| 缺省 vs 空数组 | 可区分 | ✅ `TestInjectsProvidedFlag` · `TestPullOmitsInjectRulesWhenUnconfigured` · `TestPullEmitsExplicitEmptyInjectRules` |
| 适配器三态 + 真改写 | 缺省保留 / 空关掉 / 非空接管 | ✅ `TestApplyEdgePolicyInjectSemantics` · `TestRemoteInjectRuleRewritesDivertedResponse` |
| 测试总数 | 189 → 196 | ✅ `policy` 22 · `edge/proxy` 39 |

**没做 / 遗留**：① **诱饵资产与预生成响应正文仍不下发** —— 假路径自答与「幻境正文由谁产出」属**架构级**取舍
（本轮只更新现状描述，未改架构）；② 注入规则的**多态**（按会话轮换）未定 —— 要做会碰 `INT-20`；
③ `Watch` 未实现（生效延迟上限 = 一个轮询间隔）；④ `policy_ack` 无保留期、明文 gRPC 仍是同主机假设。

---

**做了什么**：把上一轮审视上报的两条规则冲突改准（**经用户确认**）。
`MD-5` 原文说「跨模块共享的类型定义必须收敛到 `api/`」，而现实与设计文档都是：
**跨进程**契约走 `spec/` + `api/`（proto 生成物），**进程内**共享类型集中在 `core/internal/contract/`（8 个文件，只放类型无逻辑，
见 `docs/design/structure.md` §1.2 / §1.6.2）；`scripts/archcheck` 早已把它列入结构性目录白名单并注释「依据 structure.md §1.2」。
改法：`MD-5` 拆成两类落点；`MD-19` 例外清单加上 `core/internal/contract/`；规则表下方加带日期的措辞修正注记（写明原文与改动，可回退）。
**规则效力不变**：仍然禁止各自定义同名类型、禁止清单外目录承载业务逻辑。同步了 2 处会「复制粘贴」到新模块文档的引用
（`docs/modules/_template.md` 与 `docs/modules/policy.md`）；已按现实写作的三份（`docs/modules/decoy.md` / `docs/modules/director.md` / `docs/spec/config.md`）保持不动。
本轮**代码改动 0 行**。

**改了哪些文件**：`docs/design/modules.md` · `docs/modules/_template.md` · `docs/modules/policy.md`

**对应文档**：[`docs/plans/2026-09-19-rule-clarify-shared-types.md`](docs/plans/2026-09-19-rule-clarify-shared-types.md) ·
[`docs/plans/2026-09-19-docs-audit-and-module-review.md`](docs/plans/2026-09-19-docs-audit-and-module-review.md)（发现处）

**验证**：`make gate` 通过（含 `make trace` 的规则 ID 引用存在性检查）；`make archcheck` 通过。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 规则 ID 引用未悬空 | `make trace` 通过 | ✅ 追溯检查通过 |
| 模块清单 ↔ 目录一致 | `make archcheck` 通过 | ✅ 门禁通过 |
| 规则效力 | 两条禁止仍在 | ✅ `MD-5` / `MD-19` 两行均保留 |
| 旧表述残留 | 活性文档零残留 | ✅ 仅历史条目与注记中引用的原文 |

**没做 / 遗留**：① 未新开 ADR（不是取舍，是消除歧义）；② 未来若出现「proto 表达不了的跨进程共享类型」，
`api/` 与 `spec/` 的边界需再定义（无当前影响）；③ 本轮无新增测试（只改规则文本）。

---

**做了什么**：对**活文档与示例物料**做一轮审视（方法 = 技能 `audit`：机器优先，人只补机器核不了的）。
① **机器可核的四类全部跑过**：`make trace`（悬空引用 · 孤儿文档 · 过期状态标记 · 过期豁免）与 `make archcheck` 均无错误。
② **自查六类**（新写的一次性核验）：模块三方清单交叉比对（`docs/design/modules.md` §1.1 ⇄ `docs/progress.md` §1 ⇄ `docs/modules/README.md` §0）
→ **23 个有效模块三方一致**；模块文档九章齐全（缺章 0）；导出符号无孤儿（0 命中）；
**env 双向核对**；文档里 `make <目标>` 全部存在（幽灵命令 0）；`docs/design/` 无 `P-2` 含糊词。
③ **修掉 6 处过期事实**（`docs/design/structure.md` §1.5）：`contract` 文件数 6→8；
`core/internal/responder` / `core/internal/isolation` / `edge/injection` 标「⏳ 阶段 2b」但它们**已实现并含单测**；
`scripts/*` 行把已实现的 `scripts/check-leak` 写成「仍只有说明」；`edge/proxy` 测试数 30→37；补回已实现却无行的 `decoy` / `honeypot`。
④ **删幽灵内容**（四道门槛①：全仓检索 0 引用）：`SHEN_CORE_TLS`（无任何代码读它）与
`sidecar.example.yaml` 里的存活/就绪探针端点（代码里没有该端点，`grep healthz` 只命中测试夹具）；
并把 `ST-17` 未实现登记为 `docs/modules/adapter-proxy.md` §8 未决 12。
⑤ **补漏写**：`SHEN_MIRROR_LISTEN`（此前**全仓零文档**）进 `edge/mirror/README.md`；
`SHEN_PROXY_CACHE_MAX` 与 `SHEN_PROXY_INJECT`（代码读、模板没写）进 `edge/proxy/config/front-proxy.example.env`。
⑥ **修自相矛盾**：`edge/mirror/README.md` 目录表说「接收端 ✅ 含单测」、下方警示又说「尚未实现」——已统一为「已实现」。
⑦ **发现并上报两条规则措辞冲突**（**未擅改** `docs/design/`，`P-3` 要求停下报告）：
`MD-5` 说「跨模块共享类型必须收敛到 `api/`」，而进程内共享类型实际集中在 `core/internal/contract/`（8 个文件）；
`MD-19` 的例外清单也未包含该目录。两条均不阻塞开发，但按字面读会得出错误结论（详见变更包 §7）。

**改了哪些文件**：`docs/design/structure.md` · `docs/modules/adapter-proxy.md` · `edge/mirror/README.md` ·
`edge/proxy/config/sidecar.example.yaml` · `edge/proxy/config/front-proxy.example.env`

**对应文档**：[`docs/plans/2026-09-19-docs-audit-and-module-review.md`](docs/plans/2026-09-19-docs-audit-and-module-review.md)（含 18 条审视表与追溯矩阵）

**验证**：`make gate` 通过（含 `make trace` / `make archcheck` / `make leakcheck` / 单测 `-race`）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 模块三方清单 | 一致 | ✅ 23 个有效模块三方一致 |
| 模块文档九章（`MD-2`） | 无缺章 | ✅ 缺章数 0（23 份） |
| 导出符号孤儿 | 无 | ✅ 0 命中 |
| 幽灵 `make` 目标 | 无 | ✅ 0 命中 |
| 幽灵配置项 / 端点 | 无 | ✅ 删 `SHEN_CORE_TLS` 与两个探针端点 |
| env 双向核对 | 代码读的都有人写、文档写的都有人读 | ✅ 模板 14 → 16；mirror 的监听地址补进文档 |
| `docs/design/` 含糊表述（`P-2`） | 零违规 | ✅ 仅规则定义自身命中（规范允许） |
| 已实现模块的实际 import ↔ 文档依赖表 | 一致 | ✅ 均只依赖 `contract` + 文档已写的接口 |

**没做 / 遗留**：① **待你确认的两条规则冲突**（`MD-5` / `MD-19` ↔ `core/internal/contract/`）—— 改规则需你拍板，
本轮只上报、不动 `docs/design/`；② `ST-17` 探针未实现（已登记，落地前需先定「探针端点是否开在对外监听上」）；
③ `docs/spec/` 的 logs / metrics 两份字典与三个接入面目录（integrate / ops / analytics）仍待建；
④ 本轮**代码改动 0 行**，因此无新增测试。

---

**做了什么**：把 `api/policy/v1` 的**两端**都实现出来，闭合阶段 2b 的结构性断点（此前契约存在但无人实现，
核心算出的幻境后端池与白名单传不到边缘）：
① **核心侧服务端**（`core/internal/policy/server.go`）：`Pull` 把当前策略**投影**成边缘文档（改道后端表 + 白名单 CIDR，
按名排序 ⇒ 字节确定）、`checksum = sha256(payload)`；`Watch` **显式返回 `Unimplemented`**（不挂住调用方）；
`Ack` 把回执写入 `store.PolicyStore`（按 `(policy_id, version, adapter_id)` 幂等，缺标识拒收）。
② **适配器侧客户端**（`edge/proxy/policy.go`）：启动 + 每 60s 拉取（`SHEN_PROXY_POLICY_INTERVAL` 可配，0 = 不拉）；
先验校验和与 `schema_version` 再应用；合并语义 = **远端覆盖本地（后端表按名）+ 白名单并集**（护栏只增不减，`INT-25`）；
失败一律**沿用当前策略**（`NI-1`），拉不到/校验不过都回执 `applied=false` + 原因。
③ **契约**：`PolicyAck` 扩 `adapter_id` / `reason`（没有标识就无法做 `AR-13` 的版本对账）；
新增载荷契约 [`docs/spec/policy-payload.md`](docs/spec/policy-payload.md)（两处实现必须同时改，因 `ST-3` 禁止适配器 import 核心内部）。
④ **决策记录** [`docs/background/decisions/0018-policy-plane-pull-model.md`](docs/background/decisions/0018-policy-plane-pull-model.md)（含 4 条失效条件）—— 9 项推荐经用户全部批准。
⑤ 开发中修的两个**真缺陷**（测试捕到）：校验和不匹配时**每轮重复回执行**；应用成功日志把 `len(payload)` 字节数当「N 个改道后端」。
⑥ 安全硬化：核心明文 gRPC 监听加 fail-closed 闸门（非回环地址启动即失败，不提供开关）。
⑦ 新发现：**判定缓存的键不含 UA** ⇒ 同一 `(IP, 会话, 路径, 60s)` 共享一个决策，
实测可复现「探针先到 → 真实用户被改道」与「真实用户先到 → 探针漏改道」—— 登记为 `K-20`，**未擅自动 `ST-10`**。

**改了哪些文件**：`api/policy/v1/policy.proto`（+ 重新生成的 `api/policy/v1/policy.pb.go` / `api/policy/v1/policy_grpc.pb.go`）·
`core/internal/contract/policy.go` · `core/internal/store/iface.go` · `core/internal/store/memory.go` ·
`core/internal/policy/server.go`（新增）· `core/internal/policy/server_test.go`（新增）· `core/internal/policy/policy_test.go` ·
`core/cmd/core/main.go` · `edge/proxy/policy.go`（新增）· `edge/proxy/policy_test.go`（新增）· `edge/proxy/handler.go` ·
`edge/proxy/cmd/proxy/main.go` · `edge/proxy/README.md` · `edge/proxy/config/front-proxy.example.env` ·
`docs/spec/policy-payload.md`（新增）· `docs/background/decisions/0018-policy-plane-pull-model.md`（新增）·
`docs/background/decisions/README.md` · `docs/design/structure.md` · `docs/design/modules.md` ·
`docs/modules/policy.md` · `docs/modules/adapter-proxy.md` · `docs/progress.md` · `docs/kb/known-issues.md` ·
`README.md` · `docs/README.md`

**对应文档**：[`docs/plans/2026-09-19-policy-plane.md`](docs/plans/2026-09-19-policy-plane.md)（含追溯矩阵与审视记录 10 条）·
[`docs/spec/policy-payload.md`](docs/spec/policy-payload.md) · [`docs/background/decisions/0018-policy-plane-pull-model.md`](docs/background/decisions/0018-policy-plane-pull-model.md)

**验证**：`make gate` 通过（含 `make trace` / `make leakcheck` / `make archcheck` / 单测 `-race`）；`make dev` 通过；
**端到端实测**（真进程 · 真 gRPC · 真改道）通过。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 适配器拉取并应用策略 | 日志显示已应用版本与后端数 | ✅ `proxy: 已应用策略 e2e v1（1 个改道后端）` |
| 探针 UA | 改道到幻境 | ✅ `curl -A "HeadlessChrome/120"` → `MIRAGE-BODY` |
| 正常会话（独立 Cookie） | 到真实业务，不受影响 | ✅ → `REAL-BUSINESS` |
| 校验和不匹配 | 拒绝应用 + 回执 `applied=false` + 原因，**只回执一次** | ✅ `TestPolicyChecksumMismatchIsRejected` |
| 策略面不可达 | 沿用当前策略、本地后端仍可用、不产生回执 | ✅ `TestPullFailureKeepsCurrentPolicy` |
| 回执对账 | 幂等（每适配器一条）+ 能回答「谁应用了哪个版本」 | ✅ `TestAckRecordsAndIsIdempotent` |
| 测试总数 | 177 → 189 | ✅ `ok shen/edge/proxy`（37）· `ok shen/core/internal/policy`（17） |
| 端到端契约纪律 | 适配器不得 import 核心内部 | ✅ `make archcheck`（新测试一度违反而自查修掉） |

**没做 / 遗留**：① **注入片段 / 诱饵资产 / 预生成内容仍不经策略面下发**（核心侧尚无它们的来源与归属）——
阶段 2b 的**内容类**能力在请求路径上还看不到；② `Watch` 未实现（生效延迟上限 = 一个轮询间隔）；
③ 回执只落账不告警（「哪个适配器停在旧版本」要人查）；④ `K-20` 的缓存键问题须单独一轮讨论 `ST-10`；
⑤ `policy_ack` 无保留期配置（接真库时按 `NI-13` 定）；⑥ 明文 gRPC 仍是同主机假设（已加非回环启动闸门，mTLS 未做）。

---

**做了什么**：① **新增根 `README.md`** —— 仓库此前没有任何入口文档；新 README 回答四件事：
项目是什么（一页架构图 + 三条不可妥协）· 现在跑到什么程度（阶段表 + 23 模块 / 14 包 / 177 个测试函数）·
五分钟怎么跑起来（`make check` / `make gate` / `make dev` / `make run`）· **效果长什么样**（贴 `make dev` 五步真实输出、
真二进制 + 真 TLS 的 `curl` 结果、以及七个行为场景：原样透传 / 补 `X-Forwarded-*` / 只改引流侧 / 核心挂了仍 200 / 白名单不调核心 / 影子模式 / 判定缓存），
再加目录结构 · 文档地图 · 门禁说明 · 使用边界（`SB-7` 仅授权环境、`SB-1`/`SB-2` 不做控制面）· 已知限制。
② **修正 docs/ 里的过期事实**（逐条用命令核过）：
`docs/spec/config.md` §2.0 的 `whitelist` / `decoys` / `honeypots` 三行写「未接入」，实证各已有 1 处消费（`loader.Whitelist` / `Decoys` / `Honeypots`）；
§2.8 / §2.9 称「阶段 2b 才进示例配置」，实证 `deploy/config/config.example.yaml` 已有该两段；
`docs/progress.md` 与 `docs/modules/README.md` 的「21 个模块」→ **23 个有效模块（共 24 行）**；
`edge/proxy` 测试计数 5 处 29 → **30**；`docs/design/modules.md` 头部历史注记补「当时」二字澄清（**规则与 ID 未动**）。
③ **补门禁与工具清单**：`docs/README.md` 新增 §1.1（`scripts/` 七项工具 + 各自依据的规则 + 两个未实现工具的显式标注），
并在开头加「想看项目全貌 → 根 README」的互指。
④ 顺手把「策略面未实现」改成精确表述 —— `api/policy/v1` **只有契约**（proto + 生成物），**服务端与消费方都未实现**。

**改了哪些文件**：`README.md`（新增，根目录）· `docs/README.md` · `docs/spec/config.md` · `docs/progress.md` ·
`docs/modules/README.md` · `docs/modules/adapter-proxy.md` · `docs/design/structure.md` · `docs/design/modules.md`

**对应文档**：[`docs/plans/2026-09-19-docs-accuracy-and-readme.md`](docs/plans/2026-09-19-docs-accuracy-and-readme.md)（含追溯矩阵与审视记录）

**验证**：`make gate` 通过（含 `make trace` 的悬空链接检查、`make leakcheck`、单测 `-race`）；`make dev` 通过。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| `whitelist` / `decoys` / `honeypots` 是否被消费 | 文档与代码一致 | ✅ 各 1 处调用（`core/cmd/core/main.go` 与 `director`） |
| 示例配置是否已有 `decoys` / `honeypots` | 一致 | ✅ `deploy/config/config.example.yaml` 第 71 / 86 行 |
| 有效模块数 | 一致 | ✅ `docs/design/modules.md` §1.1 共 24 行、23 个有效 |
| `edge/proxy` 测试数 | 一致 | ✅ 30（`grep -c '^func Test' edge/proxy/*_test.go`） |
| 全仓测试函数数 | 与 README 一致 | ✅ 177 |
| 根 README 里的命令能跑 | 全部可跑 | ✅ `make check` / `gate` / `dev` / `help` 均实跑 |

**没做 / 遗留**：① 策略面仍只有契约（2b 能力在边缘看不到）—— README §2/§9 已显式写明，**没有**写成现状；
② 待建的目录（integrate / ops / analytics 三个，见 docs/README.md §3）与 spec 的两份字典（logs / metrics）仍缺；
③ 计数类事实（模块数 / 测试数）会继续漂移，`make trace` **不核计数**，靠每轮同步（`docs/progress.md` 头部已有此义务）；
④ `scripts/doctor` / `scripts/sentinel` 仍是占位；⑤ `AR-29` 换底座后仍未实测（`E3`）。

---

**做了什么**：把 `OH-3`（「工程**必须**提供自动检查，扫描响应面模块的字符串字面量」）从占位变成真东西：
新增 `scripts/check-leak`（标准库 + go/ast，**零新依赖**）—— 扫 `OH-1` 禁用清单（大小写不敏感）、
查响应头与 Cookie 名里的决策类词（`OH-5`），豁免分**三类**且都不是整目录豁免（`OH-4`）：
规则级（内部日志 / 错误构造 / struct tag，写在 `scripts/check-leak/main.go` 包注释并附 `OH-2` 依据）、
文件级（`core/internal/responder/blacklist.go` 自己声明 check-leak:filter 标记）、逐条（`scripts/check-leak/allow.txt`，
精确到**文件 + 字面量**，理由必填，**过期即报**）。扫描范围从 `docs/design/modules.md` §1.1 **解析**，
模块改名就**直接失败**（拒绝少扫）。同时修掉它查出的真泄漏：`core/internal/decoy/decoy.go` 的投放片段
含「…蜜饵」与 `decoy_accounts` 表名（片段会被投放进客户环境，对手可能读到）、
`edge/proxy/cmd/proxy/main.go` 启动日志的「引流」措辞（与 `docs/design/terminology.md` §4.1 的三值命名对齐）。
另登记一致性核查发现的既有缺口（`NI-12` 的 V 系测试、`AR-26` 的两条周期断言、`NI-7` / `ST-17` 的部署物料、
`scripts/doctor` / `scripts/sentinel` 仍为占位）—— 见变更包 §7，本轮不动它们。

**改了哪些文件**：`scripts/check-leak/main.go`（新增）· `scripts/check-leak/allow.txt`（新增）·
`scripts/check-leak/README.md`（重写：从「尚未实现」改为已实现，含范围与局限）· `Makefile`（新增 `leakcheck`，接进 `lint` / `check` / `gate`）·
`core/internal/responder/blacklist.go`（文件级过滤器声明）· `core/internal/decoy/decoy.go`（修真实泄漏）·
`edge/proxy/cmd/proxy/main.go`（日志措辞）· `.pi/devloop.md`（适配面补 `leak_cmd`）·
`docs/kb/dev-workflow.md`（§3 门禁链路补 `leakcheck`）

**对应文档**：[`docs/plans/2026-09-19-oh3-leak-check.md`](docs/plans/2026-09-19-oh3-leak-check.md)（含追溯矩阵与审视记录）·
[`docs/design/constraints.md`](docs/design/constraints.md) 的 `OH-1`…`OH-5` · `scripts/check-leak/README.md`

**验证**：`make gate` 通过（含 `make trace`、`make leakcheck`、`make licensecheck`、单测 `-race`）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 响应体字面量写 honeypot | 报 `OH-1` | ✅ 探针文件第 4 行被命中 |
| 响应头名 `X-Decoy-Marker` | 报 `OH-1` | ✅ 探针第 6 行被命中 |
| 响应头名 `X-Agent-Capture-Decision` | 报 `OH-5` | ✅ 探针第 7 行被命中 |
| 响应头名 `X-Risk-Score` | 报 `OH-5` | ✅ 探针第 8 行被命中 |
| 撤掉探针 | 回到通过 | ✅ `泄漏检查通过。` |
| 白名单表（AR-22） | 文件级声明后不报错、并打印理由 | ✅ `已声明过滤器文件：…blacklist.go` |
| 逐条例外 | 2 条命中且打印理由 | ✅ `已登记豁免 2 条` |
| 豁免过期 | 报警 | ✅ 修 `used` 标记时实测 |
| 模块改名 | 直接失败不静默少扫 | ✅ 实测报「找不到这些响应面模块（改名了？）」 |

**没做 / 遗留**：① 项目名 / 二进制名 / 服务名未定 ⇒ `OH-1` 那三类无从扫描（待 ADR-0004 定名）；
② 拼接串与模板变量扫不到（检查只覆盖字面量，`OH-2` 的最终判据仍是人工）；
③ `scripts/check-leak` 自身无单测（工具类脚本无测试约定，本轮用**负向探针**作证据）；
④ `OH-5` 目前**没有真实数据** —— 全仓（非测试）当前不设任何自定义响应头，该检测是给后续代码用的护栏；
⑤ 既有缺口 `NI-12` / `AR-26` / `NI-7` / `ST-17` / `scripts/doctor` / `scripts/sentinel` 已登记未做。

---

**做了什么**：把 `edge/proxy/`（模块 `adapter-proxy`，③前置 + ④边车）的**转发层**从 Go 标准库
net/http/httputil.ReverseProxy 换成**内嵌 Caddy**（github.com/caddyserver/caddy/v2，Apache-2.0）；
判定胶水（白名单 → `decision_id` → 判定缓存 → gRPC 判定 → 三值路由 → 异步上报）保持自研 Go，
改为 Caddy 中间件 `http.handlers.shen_proxy`。**TLS 由本进程终结**
（`SHEN_PROXY_TLS_MODE=off|manual|acme`），未决项「TLS 终结归谁」结案
（[ADR-0017](docs/background/decisions/0017-caddy-l1-base.md)）。
另做一条**部署面硬化**：关闭 Caddy 的**配置自动保存**（`Admin.Config.Persist=false`）——
默认它会把整份配置写进 `$XDG_DATA_HOME` 下的 caddy/autosave.json，请求路径上的边车/前置不该有这种写入。
模块身份（§1.1 第 11 行）与三个对外接口（`JudgeClient` / `TelemetryClient` / `Injector`）**不变**；
范围仅形态 ③④，形态 ①（mirror）与 ②（DNS）未动。

**改了哪些文件**：`edge/proxy/handler.go` · `edge/proxy/glue.go` · `edge/proxy/embed.go` · `edge/proxy/embed_test.go`（新增）·
`edge/proxy/proxy.go`（**删除**：旧转发层被 Caddy 取代）· `edge/proxy/iface.go` · `edge/proxy/proxy_test.go` ·
`edge/proxy/cmd/proxy/main.go` · `edge/proxy/README.md` · `edge/proxy/config/front-proxy.example.env` ·
`go.mod` · `go.sum` · `docs/spec/dependencies.md`（重新生成）·
`docs/background/decisions/0017-caddy-l1-base.md`（新增）· `docs/background/decisions/0008-edge-language-go.md`（标注底座部分被取代）·
`docs/background/decisions/README.md` · `docs/design/language.md` · `docs/design/architecture.md` · `docs/design/integration.md` ·
`docs/design/modules.md` · `docs/design/structure.md` ·
`docs/modules/adapter-proxy.md` · `docs/modules/README.md` · `docs/progress.md` ·
`docs/background/notes/pending-experiments.md`（新增 `E3`）

**对应文档**：[`docs/plans/2026-09-19-caddy-l1-base.md`](docs/plans/2026-09-19-caddy-l1-base.md)（变更包，含追溯矩阵与审视记录）·
[`docs/modules/adapter-proxy.md`](docs/modules/adapter-proxy.md) §1/§3/§8 ·
[`docs/design/architecture.md`](docs/design/architecture.md) §10.2 缺口 14（TLS 结案）

**验证**：`make gate` 通过（含 `make trace` 与 `make licensecheck` —— Caddy 全链许可为
Apache-2.0 / MIT / BSD-3-Clause / CC0，无 AGPL/SSPL/BSL）；`make dev` 通过。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 内嵌 Caddy 端到端（真 Caddy + 真 gRPC + 自签 TLS） | 三值路由 + 引流侧注入 + 引流失败回落 + TLS 握手 + block 403 | ✅ `TestEmbeddedCaddyEndToEnd` PASS |
| `tls_mode` 三取值 + 非法值 | 缺字段/非法值启动前报错 | ✅ `TestTLSConfigValidate` PASS（7 例） |
| 测试总数 | 19 → 29（`-race`） | ✅ `ok shen/edge/proxy` |
| **真二进制 + 真 TLS + 断核心** | TLS 握手成功；核心不可达时**放行到业务**（`NI-3`）；同端口明文被拒 | ✅ `curl -sk https://127.0.0.1:18443/` → `code=200 http=2`，体为业务页 |
| 配置自动保存 | 不得写 autosave.json | ✅ 修复后重启：日志无 `autosaved config`，文件不存在 |
| 许可审计 | 无传染性许可 | ✅ `许可审计通过：没有传染性或限制性许可` |

**没做 / 遗留**：① `tls_mode=acme` 只做了装配与校验，**未在真实公网域名验证签发/续期**；
② `AR-29`（P99 ≤ 5 ms）**换底座后未重测** —— 已登记为实验 `E3`（[`pending-experiments.md`](background/notes/pending-experiments.md)），
未测前不得声称形态 ③④ 可上线；③ Caddy admin API 已禁用（缩攻击面）⇒ 本进程**没有**运行时改配置的入口，
后端表热更新留待策略面 `S4`；④ 二进制体积控制未测。

---

## 2026-09-18 · 产品定位与接入边界讨论（CLB / Caddy / 识别判定）

**做了什么**：记录一轮产研讨论的结论与未决项，不改代码、不改规则：
① **定位确认** —— 产品 = 「服务端前置的欺骗引擎」：反向代理前置（L1）→ 判定核心（judge/director）→ 路由真实业务（内网/公网）或蜜罐层；蜜罐层启动与管理各类蜜罐。与 [`docs/README.md`](docs/README.md) §0 及 [`docs/design/integration.md`](docs/design/integration.md) §1 一致，**无需改动**；
② **CLB / L0 不开发** —— 复用云 / 客户现成组件（`AR-3` 已定），**无需改动**；
③ **识别/判定逻辑推迟** —— 用户明确「能力判断核心，暂不确定，后续详细设计」，在讨论稿 D8 登记；
④ **反向代理底座（Caddy）未拍板** —— 讨论提出「Caddy 核心 + 只写 Go 插件」替代自研 `adapter-proxy`，登记为未决项 D13。

**改了哪些文件**：`docs/background/notes/implementation-discussion.md` · `docs/log.md`

**对应文档**：[`docs/background/notes/implementation-discussion.md`](docs/background/notes/implementation-discussion.md) §6.1（D8 更新 · 新增 D13）

**验证**：`make gate` 通过（含 `make trace`）

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 讨论稿未决项 | D8 注明用户推迟、D13 新增 Caddy 议题 | ✅ §6.1 表已更新 |

**没做 / 遗留**：① 识别/判定的详细设计（规则 → ML、首跳定终局 D0 等）留待后续专门讨论；② Caddy vs Go 自研未拍板（D13 待定）。

---

## 2026-09-18 · 代码整理 + 调用链核实 + 文档站完善

**做了什么**：① **整理装配代码** —— `core/cmd/core/main.go` 提取 `assembleDeception` / `deception` / `assertConsistency`，`run()` 从 ~200 行降到 **125 行**，行为不变；
② **核实模块真实调用链**（`go list` 实测依赖 + 反查每个接口的消费者）：**发现 `decoy` / `responder` / `honeypot` 已装配但不在调用链上**（`Resolve`/`Match`/`Respond` 的调用者只有测试与启动自检），根因是**策略面 S4（`api/policy/v1`）未实现**；
③ **完善文档站**：`docs/README.md` 更新现状与计数（21→23 模块）+ 新增调用链导航 + 上手顺序；`docs/design/structure.md` §1.6 **重写**为实测三进程地图（原缺 5 个包、误称「两个二进制」、误称 isolation 无人调用）；`docs/modules/README.md` 新增 **§0.4 运行时调用链**；`decoy`/`responder`/`honeypot` 模块文档各登记一行「未接到请求路径」。

**改了哪些文件**：`core/cmd/core/main.go` · `docs/README.md` · `docs/design/structure.md` · `docs/modules/README.md` · `docs/modules/decoy.md` · `docs/modules/responder.md` · `docs/modules/honeypot.md` · `docs/plans/2026-09-18-docs-wiring-cleanup.md` · `docs/log.md`

**对应文档**：[`docs/plans/2026-09-18-docs-wiring-cleanup.md`](docs/plans/2026-09-18-docs-wiring-cleanup.md)（含调用链核实与设计评估）· [`docs/modules/README.md`](docs/modules/README.md) §0.4 · [`docs/design/structure.md`](docs/design/structure.md) §1.6

**验证**：`make gate` 通过（含 `-race`）· `make dev` 通过 · 启动日志确认装配与 `AR-30` 自检仍生效

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 行为不变 | 15 个包测试与整理前一致 | ✅ `go test -count=1 ./...` 15 个 ok |
| 调用链核实 | 区分「在链上」与「仅装配」 | ✅ `go list` + 反查消费者；见变更包 §2 |
| 结构文档准确 | 三进程 + 实测依赖 + 接口清单 | ✅ `structure.md` §1.6.1–§1.6.4 已重写 |
| 文档站入口 | 计数与状态不再过期 | ✅ `docs/README.md`（21→23 模块 · 断点提示） |
| 启动日志 | 装配摘要与自检仍生效 | ✅ 「诱饵面：0 个资产启用 / 幻境后端池：0 个登记」 |

**没做 / 遗留**：⭐ **策略面（`api/policy/v1`）未实现** —— 当前唯一结构性断点，补齐它才能让 `decoy`/`responder`/`honeypot` 到达边缘、消除后端池的两份事实源；本轮只核实与登记，未实现（属新功能，需单独一轮）。

---

## 2026-09-18 · 模块代码整理（4 处简化，无行为变更）

**做了什么**：整理本轮开发的模块代码，消除冗余结构与重复逻辑，为后续「按架构重组模块」做准备：
① `honeypot` —— 删掉 `knownTypes()`（每次调用重建切片并排序）与 `typeOrder()`（线性查设计清单序号）两个函数，`known` 改为**按给定顺序**的有序切片 + `slices.Contains` 做成员判断（-15 行）；
② `core/internal/store/memory.go` —— 三份「超限则清理过期」循环（一份提成方法、两份内联）收敛为一个泛型 `sweepExpiredIfFull`；
③ `responder` —— `seedOf` 由三次 `Write` 改为一次拼好再写；
④ `edge/proxy` —— `injectResponse` 里三条「不注入」判据抽成 `injectable(resp)`（它们本是同一个概念）。
**刻意保留**：`director.Config.Now` 与 `Engine.now` 当前无读取点，但注释写明是「仅为将来的可观测留口」的**有意预留**，且删它属于改动导出类型形状 —— 按「不改对外契约」保留并登记。

**改了哪些文件**：`core/internal/honeypot/honeypot.go` · `core/internal/store/memory.go` · `core/internal/responder/responder.go` · `edge/proxy/proxy.go` · `docs/plans/2026-09-18-module-cleanup.md` · `docs/log.md`

**对应文档**：[`docs/plans/2026-09-18-module-cleanup.md`](docs/plans/2026-09-18-module-cleanup.md) · [`docs/modules/honeypot.md`](docs/modules/honeypot.md) · [`docs/modules/responder.md`](docs/modules/responder.md)

**验证**：`make gate` 通过（含 `-race`）· `make dev` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 行为不变 | 15 个包全部测试通过（与整理前一致） | ✅ `go test -count=1 ./...` |
| 蜜罐类型顺序 | 仍按登记顺序输出 | ✅ `honeypot_test.go` TestTypesDedupAndOrder |
| 注入判据 | 压缩/非 HTML/超大仍不注入 | ✅ `proxy_test.go` 24+ 例 |
| 一致性 | 同会话语资源仍逐字节一致（AR-30） | ✅ `responder_test.go` 16 例 |
| 代码量 | honeypot 170 → 155 行；清理循环 3 份 → 1 份 | ✅ `wc -l` |

**没做 / 遗留**：`director.Config.Now` 与 `Engine.now` 无读取点（有意预留）—— 若确认不需要，删 5 行即可，但需先确认「将来可观测留口」是否仍要保留；本轮为整理轮，**不改任何行为与对外契约**。

---

## 2026-09-18 · 全仓代码审查与加固（8 处修复 + 开源替换与稳定性评估）

**做了什么**：对全仓非测试代码（6834 行）做整体审查（静态分析器已零告警，故全部发现来自人工核对与定向审查），**修复 8 处缺陷**：
① 引流侧上游**无超时** → 蜜罐挂死会拖住客户端（违 `NI-1`）→ 加响应头超时 + 连接层超时（超时回落业务）；
② **压缩响应被注入** → 损坏 gzip → 加 `Content-Encoding` 检查；
③ `trackingWriter` **未实现 `Hijacker`** → WebSocket 升级一律 502 → 透传 `Hijack`；
④ 注入**读失败返回 nil** → 截断响应 → 改为返回错误（回落业务）；
⑤ **`INT-25` 核心侧白名单未消费** → `director` 实现前置白名单（命中即放行、连判定都不做）；
⑥ `policy` **报错指向错误字段** → `ratio()` 带字段路径；
⑦ **内存存储无界增长**（TTL 条目只在被读到才删）→ 超限时清理已过期项；
⑧ **mirror 调核心无超时** → 加 3s 上限。
另输出**开源替换评估**（12 项组件，结论：该复用的都已复用，自研的正是设计指定的智能决策层）与**架构稳定性评估**（6 强项 / 6 风险点）。

**改了哪些文件**：`edge/proxy/proxy.go` · `edge/proxy/iface.go` · `edge/proxy/proxy_test.go` · `edge/proxy/cmd/proxy/main.go` · `edge/mirror/receiver.go` · `core/internal/contract/policy.go` · `core/internal/policy/policy.go` · `core/internal/policy/policy_test.go` · `core/internal/director/director.go` · `core/internal/director/director_test.go` · `core/internal/store/memory.go` · `core/cmd/core/main.go` · `docs/plans/2026-09-18-code-review.md` · `docs/log.md`

**对应文档**：[`docs/plans/2026-09-18-code-review.md`](docs/plans/2026-09-18-code-review.md)（审查报告）· [`docs/design/constraints.md`](docs/design/constraints.md)（`NI-1`）· [`docs/design/modules.md`](docs/design/modules.md)（`INT-25` / `MD-25`）

**验证**：`make gate` 通过（含 `-race`）· `make dev` 通过 · `go vet` / `staticcheck` / `errcheck` 零告警

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 蜜罐挂死 | 超时后回落业务（NI-1） | ✅ `TestMirageTimeoutFallsBackToOrigin` |
| 压缩响应 | 不注入（否则损坏编码） | ✅ `TestCompressedResponseIsNotInjected` |
| WebSocket 升级 | 透传 101（裸 TCP 验证） | ✅ `TestProtocolUpgradePassesThroughMirage` |
| 白名单前置 | 命中即放行且不做判定 | ✅ `director` 5 例 |
| 配错 guard | 报错指向真实字段 | ✅ `guard 缺键须报对字段` |
| 依赖解耦 | 新模块仅依赖 `contract` | ✅ `go list -f '{{.Imports}}'` |

**没做 / 遗留**（已入报告 §5，按优先级）：① `ADR-0012` 会话级判别未实现（判别缺一半，**P0**）② 真实存储未接（内存是占位，**P0**）③ `responder` 无对外服务面 ④ `telemetry` 的接收缓冲/落库补偿未实现 ⑤ `TrustXFF=false` 时应剥离入站 XFF ⑥ 差异哨兵 `sentinel` 未实现 ⑦ 熔断器是否换 sony/gobreaker（可换非必换）。

---

## 2026-09-18 · 欺骗引擎模块实现（isolation / honeypot / decoy / responder / edge-injection）

**做了什么**：把设计变成代码 —— 实现 5 个欺骗引擎模块（各含独立单测），并用**接口**把它们解耦接起来：
① `isolation`（隔离记录 / TTL / 查询短路，`NI-10` fail-open）；② `honeypot`（幻境后端池：类型注册 + config 开关 + 健康 + 解析，**不实现具体蜜罐** ADR-0011）；
③ `decoy`（诱饵面：最长前缀匹配 + 多态 + 投放片段，ADR-0010/0016）；④ `responder`（欺骗响应：确定性模板 + 预生成优先，`AR-30` 一致性不变量）；
⑤ `edge-injection`（L1 注入：非 HTML 原样返回，纯变换）；⑥ 补上 `MD-25`（诱饵面 observe-only）的**实现** —— 诱饵前缀集在决策层豁免 block，前缀集由装配层从诱饵资产汇总（单一事实源）。另：`store` 新增 `DecoyStore`/`ContentStore`；`config` 新增 `decoys`/`honeypots` 两段（数据驱动，`ST-24`）；`control` 服务面接隔离短路；`main.go` 装配 + 启动期 `AR-30` 自检；新增**端到端集成测试**。⑦ 把 `edge-injection` **接进适配器**（`ST-5`：被适配器引用，不独立部署）：引流侧的 HTML 响应注入诱饵，业务侧一律不改写（`INT-8`）；⑧ 实现 `AR-22` / `AR-23`（内容黑名单 + 长度上限）：生成内容过三类校验，不合格回落安全模板。

**改了哪些文件**：`core/internal/contract/deception.go` · `core/internal/store/iface.go` · `core/internal/store/memory.go` · `core/internal/isolation/` · `core/internal/honeypot/` · `core/internal/decoy/` · `core/internal/responder/` · `edge/injection/` · `core/internal/control/service.go` · `core/internal/director/director.go` · `core/internal/policy/policy.go` · `core/cmd/core/main.go` · `core/cmd/core/integration_test.go` · `deploy/config/config.example.yaml` · `docs/modules/isolation.md` · `docs/modules/honeypot.md` · `docs/modules/decoy.md` · `docs/modules/responder.md` · `docs/modules/edge-injection.md` · `docs/progress.md` · `docs/plans/2026-09-18-deception-modules-impl.md` · `docs/log.md`

**对应文档**：[`docs/modules/decoy.md`](docs/modules/decoy.md) · [`docs/modules/honeypot.md`](docs/modules/honeypot.md) · [`docs/modules/responder.md`](docs/modules/responder.md) · [`docs/modules/isolation.md`](docs/modules/isolation.md) · [`docs/modules/edge-injection.md`](docs/modules/edge-injection.md) · [`docs/plans/2026-09-18-deception-modules-impl.md`](docs/plans/2026-09-18-deception-modules-impl.md)

**验证**：`make gate` 通过（含 `-race`）· `make dev` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 隔离命中 | 不再调决策层，客户端不可见 | ✅ `TestIsolationShortCircuitsWithoutCallingCore` |
| 隔离存储故障 | fail-open（回退调核心，NI-10） | ✅ `TestIsolationStoreFailureFailsOpen` |
| 后端池解析 | 未启用 / 不健康不得解析（NI-5） | ✅ `honeypot_test.go` 14 例 |
| 响应一致性 | 同 `(会话,资源)` 逐字节一致（AR-30） | ✅ `responder_test.go` 10 例 |
| 端到端链路 | session→judge→director→honeypot→decoy→responder | ✅ `TestDeceptionChainEndToEnd` |
| 诱饵面 observe-only | 满分 + block 开启仍不 block（MD-25） | ✅ `TestConfiguredDecoyPathIsNeverBlocked` + director 4 例 |
| 适配器注入 | 只改引流侧 HTML；业务侧一律不改写（INT-8 / ST-5） | ✅ `edge/proxy` 4 例 |
| 内容黑名单 | 泄露 / 自曝 / 超长三类均被拒；模板自身合规（AR-22 / AR-23） | ✅ `responder` 6 例 |
| 误调度 | 正常用户放行 | ✅ `TestLowScoreAgentStaysOnOrigin` |

**没做 / 遗留**：真实存储后端（接口已就位）· `responder` 的对外服务面（生成能力已就绪，尚无 gRPC/HTTP 暴露给边缘）· `decoy` 管理面（console）· 蜜罐健康探针 · spec 下 decoy / honeypot 的字段契约 · 诱饵多态的识破信号触发（`chain`/`strategy`，阶段 3）。

---

## 2026-09-18 · 进度表加「模块介绍」列

**做了什么**：在 [`docs/progress.md`](docs/progress.md) §1「全部模块」表加一列「介绍（做什么）」，逐模块一句话说明职责；同时修正表后过期的「21 个有效模块」为 23。

**改了哪些文件**：`docs/progress.md` · `docs/log.md`

**对应文档**：[`docs/progress.md`](docs/progress.md) §1 · [`docs/design/modules.md`](docs/design/modules.md) §1.1（权威职责）

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 24 行均有介绍 | 逐行一句话 | ✅ |
| 规则 ID 引用 | 无 `D-3` | ✅ `make trace` 通过 |
| 计数正确 | 24 行 / 23 有效 | ✅ |

**没做 / 遗留**：介绍是摘要；职责权威仍在 `docs/design/modules.md` §1.1。

---

## 2026-09-18 · 模块总览：结构图 + 关系图 + 23 模块解释

**做了什么**：在模块索引顶部加「模块总览」—— §0.1 结构图（L0–L4 分层）· §0.2 关系图（数据流 + 依赖）· §0.3 全部 23 个模块的一句话解释，供人工分析设计是否符合想法。同时修正标题里过期的「21 个模块」。

**改了哪些文件**：`docs/modules/README.md` · `docs/plans/2026-09-18-module-overview.md` · `docs/log.md`

**对应文档**：[`docs/modules/README.md`](docs/modules/README.md) §0 · [`docs/design/modules.md`](docs/design/modules.md) §1.1（权威）· [`docs/design/structure.md`](docs/design/structure.md) §1.6

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 23 模块全覆盖 | 每个一句话解释 | ✅ §0.3 三张表 |
| 结构与关系图 | 分层 + 数据流可追 | ✅ §0.1 / §0.2 |
| 链接不悬空 | 无 `TC-3` | ✅ `make trace` 通过 |

**没做 / 遗留**：§0 是总览；职责的**权威**仍在 `docs/design/modules.md` §1.1，修改时以那里为准（避免两处漂移）。

---

## 2026-09-18 · 诱饵多态机制 + 补完模块文档

**做了什么**：① 新增 [ADR-0016](docs/background/decisions/0016-decoy-polymorphism.md)「诱饵多态与再生成」—— **会话间轮换 + 识破信号触发**，并给出与 `AR-30` 的**划界**（多态在会话边界选择、会话内冻结，两条规则同时成立）；② 补完剩余 **8 个模块文档**（`edge-injection` · `honeypot-protocol` · `honeypot-shell` · `netpolicy` · `intent` · `chain` · `strategy` · `console`），使清单 §1.1 的 24 行全部有九章文档（`MD-2` / `MD-17`）。

**改了哪些文件**：`docs/background/decisions/0016-decoy-polymorphism.md` · `docs/background/decisions/README.md` · `docs/modules/decoy.md` · `docs/modules/edge-injection.md` · `docs/modules/honeypot-protocol.md` · `docs/modules/honeypot-shell.md` · `docs/modules/netpolicy.md` · `docs/modules/intent.md` · `docs/modules/chain.md` · `docs/modules/strategy.md` · `docs/modules/console.md` · `docs/progress.md` · `docs/plans/2026-09-18-polymorphism-and-module-docs.md` · `docs/log.md`

**对应文档**：[`docs/background/decisions/0016-decoy-polymorphism.md`](docs/background/decisions/0016-decoy-polymorphism.md) · [`docs/modules/decoy.md`](docs/modules/decoy.md) · [`docs/modules/chain.md`](docs/modules/chain.md) · [`docs/modules/strategy.md`](docs/modules/strategy.md)

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 多态与一致性共存 | 作用域划界（会话内/会话间） | ✅ ADR-0016 |
| 模块文档全覆盖 | 清单 24 行均有九章文档 | ✅ `docs/modules/` 已齐 |
| 蜜罐为可选自研 | 两文档 §1 标注 | ✅ |

**没做 / 遗留**：变体集合规模 / 识破阈值 / 会话结束判据 · `intent` 意图分类体系 · `chain` 链判据 · `netpolicy` 是否进 MVP · 学习型多态；本轮为**设计轮**，不含代码。

---

## 2026-09-18 · 补响应层设计：生成式欺骗响应 + 间接注入防护

**做了什么**：补齐「改道之后返回什么」与「分析链路怎么不被反注入」—— ① `responder`（设计）：**快慢两路**（核心确定性模板 + L4 预生成落库）与**一致性不变量 `AR-30`**（同 `(会话,资源)` 同答案，禁止 LLM 采样等非确定性）；② `llm-components`（设计）：LLM 契约纪律（`AR-15`…`AR-27`）+ **间接注入防护**（`AR-31` 数据/指令分离 · `AR-32` 最小权限与不回流）；③ 两条 ADR（0014 / 0015）；④ `architecture.md` §5 新增 `AR-30` / `AR-31` / `AR-32`。

**改了哪些文件**：`docs/background/decisions/0014-generative-deceptive-response.md` · `docs/background/decisions/0015-indirect-prompt-injection.md` · `docs/background/decisions/README.md` · `docs/design/architecture.md` · `docs/modules/responder.md` · `docs/modules/llm-components.md` · `docs/progress.md` · `docs/plans/2026-09-18-responder-and-injection-design.md` · `docs/log.md`

**对应文档**：[`docs/modules/responder.md`](docs/modules/responder.md) · [`docs/modules/llm-components.md`](docs/modules/llm-components.md) · [`docs/background/decisions/0014-generative-deceptive-response.md`](docs/background/decisions/0014-generative-deceptive-response.md) · [`docs/background/decisions/0015-indirect-prompt-injection.md`](docs/background/decisions/0015-indirect-prompt-injection.md)

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 一致性不变量 | 同 `(会话,资源)` 同答案 | ✅ `AR-30` |
| 核心不调 LLM | `responder` 禁外呼，L4 预生成 | ✅ `responder.md` §3 |
| 注入防护结构性 | 数据/指令分离 | ✅ `AR-31` |
| 分析无执行能力 | 最小权限 + 不回流 | ✅ `AR-32` |

**没做 / 遗留**：诱饵多态与再生成 · 一致性键粒度/预生成时机/`severity` 交互 · 多源交叉校验与注入测试集 · `strategy` / `intent` / `chain` 模块文档；本轮为**设计轮**，不含代码。

---

## 2026-09-18 · 补判别层设计：指纹 + 会话级判别 + 归因令牌

**做了什么**：补判别层的三项设计 —— ① `judge` 增加 Agent 产品级指纹（UA/头签名 + 置信度阶梯 + 证据链）与**消费会话特征**（保持纯函数）；② `session` 增加**会话状态抽象**（速度/挑战逃逸，经 `StateSource`）与**归因令牌**（蜜标 HMAC 双角色 + 凭证水印）；③ 两条 ADR（0012 会话级判别的落点、0013 归因令牌）；④ `config` 契约加 `fingerprints` / `attribution` 段。

**改了哪些文件**：`docs/background/decisions/0012-session-level-judgement.md` · `docs/background/decisions/0013-attribution-token.md` · `docs/background/decisions/README.md` · `docs/modules/judge.md` · `docs/modules/session.md` · `docs/spec/config.md` · `docs/plans/2026-09-18-judgement-layer-design.md` · `docs/log.md`

**对应文档**：[`docs/modules/judge.md`](docs/modules/judge.md) · [`docs/modules/session.md`](docs/modules/session.md) · [`docs/background/decisions/0012-session-level-judgement.md`](docs/background/decisions/0012-session-level-judgement.md) · [`docs/background/decisions/0013-attribution-token.md`](docs/background/decisions/0013-attribution-token.md)

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| Agent 指纹 | 签名 + 置信度阶梯 + 证据链 | ✅ `judge.md` §4.1 |
| 会话级判别 | 特征注入，`judge` 仍纯函数 | ✅ [ADR-0012](docs/background/decisions/0012-session-level-judgement.md) |
| 归因令牌 | 双角色 + 水印 + TTL + 闭环校验 | ✅ [ADR-0013](docs/background/decisions/0013-attribution-token.md) |
| 不照搬 AGPL 签名表 | 明确「自行整理」 | ✅ `judge.md` §4.1 |

**没做 / 遗留**：间接提示注入风险（采集链路喂 LLM 被反注入）· 诱饵多态与再生成 · `responder` 的生成式欺骗响应 —— 均为下轮；spec/fingerprints.md 与 spec/attribution.md 的字段实现时写；本轮为**设计轮**，不含代码。

---

## 2026-09-18 · 补欺骗引擎设计：诱饵面（decoy）+ 蜜罐入口（honeypot）

**做了什么**：补上「改道之后拿什么骗」的设计 —— ① 新增 `decoy` 模块（诱饵面：Developer API / 指令文件 / MCP / 消耗战数据集 / 三类蜜饵 + SSRF，依据 ADR-0010「功能性伪装」）；② 新增 `honeypot` 模块（蜜罐入口与后端池：类型注册 + `config` 开关 + 生命周期 + 后端解析，**不实现具体蜜罐**，依据 ADR-0011）；③ 两条 ADR；④ `config` 契约加 `decoys` / `honeypots` 段；⑤ 新规则 `MD-25`（诱饵面 observe-only）/ `MD-26`（蜜罐经 `honeypot` 管理）。

**改了哪些文件**：`docs/background/decisions/0010-functional-camouflage.md` · `docs/background/decisions/0011-honeypot-entry-external-backends.md` · `docs/background/decisions/README.md` · `docs/design/modules.md` · `docs/modules/decoy.md` · `docs/modules/honeypot.md` · `docs/spec/config.md` · `docs/progress.md` · `docs/modules/README.md` · `docs/design/structure.md` · `docs/plans/2026-09-18-deception-engine-design.md` · `docs/log.md`

**对应文档**：[`docs/modules/decoy.md`](docs/modules/decoy.md) · [`docs/modules/honeypot.md`](docs/modules/honeypot.md) · [`docs/background/decisions/0010-functional-camouflage.md`](docs/background/decisions/0010-functional-camouflage.md) · [`docs/background/decisions/0011-honeypot-entry-external-backends.md`](docs/background/decisions/0011-honeypot-entry-external-backends.md)

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 诱饵面五类形态 | 有设计 | ✅ `decoy.md` §1 |
| 蜜罐只做入口 | 无具体实现，类型 `config` 开关 | ✅ `honeypot.md` §1 |
| 模块清单 | 24 行 / 23 有效 | ✅ `modules.md` §1.1 |
| 悬空链接 | `TC-3` 归零 | ✅ `make trace` 通过 |

**没做 / 遗留**：`judge` 指纹与置信度、会话级有状态判别、归因令牌（蜜标/水印）、间接提示注入风险 —— 均为下轮；spec/decoy.md 与 spec/honeypot.md 的字段实现时写；本轮为**设计轮**，不含代码。

---

## 2026-09-18 · director 阶段 2a 实现（6 项决策已确认）

**做了什么**：让引流真正生效 —— 新增 `director` 模块，把 judge 的风险分映射为三值（`route_origin` / `route_mirage` / `block`）+ 后端名，并按 `policy.gray_pct` 做确定性灰度收敛（同 `decision_id` 同结果，`ST-10`）。`block` 默认不产出（Q5）。熔断（`NI-10`）落在 `control` 服务面。`policy` 暴露阈值/灰度/影子开关。装配层按 `shadow` 选决策器：影子用 `ShadowDecider`（恒放行，回归保持），关闭后用 `director`。

**改了哪些文件**：`core/internal/contract/thresholds.go` · `core/internal/director/`（新：iface.go · director.go · director_test.go）· `core/internal/control/breaker.go` + `breaker_test.go` · `core/internal/control/service.go` · `core/internal/policy/policy.go` + `policy_test.go` · `core/cmd/core/main.go` + `main_test.go` · `docs/modules/director.md` · `docs/spec/config.md` · `docs/progress.md` · `docs/modules/README.md` · `docs/design/structure.md` · `docs/plans/2026-09-17-director-2a.md` · `docs/plans/2026-09-18-director-2a.md` · `docs/log.md`

**对应文档**：[`docs/modules/director.md`](docs/modules/director.md) · [`docs/plans/2026-09-18-director-2a.md`](docs/plans/2026-09-18-director-2a.md) · [`docs/spec/config.md`](docs/spec/config.md) §2.0

**验证**：`make gate` 通过（含 `-race`）· `make dev` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 三值 × 阈值边界 | 按阈值映射 | ✅ `TestClassifyBoundaries` 7 子例 PASS |
| 灰度确定性 | 同 decision_id 同结果 | ✅ `TestGrayDeterministic` PASS |
| 端到端改道 | 接管模式高分 → route_mirage | ✅ `TestDirectorDivertsHighScore` PASS |
| 影子回归 | 影子模式恒 origin | ✅ `make dev` 冒烟 3 样本全 origin |
| 熔断 | 错误率超阈值跳闸 | ✅ `breaker_test.go` 5 例 PASS |

**没做 / 遗留**：`whitelist` 消费（INT-25）未接 · 后端名固定 `mirage`（池选择是阶段 3）· `severity` 仍固定 none · `block` 开关走构造参数（未进 config）· `guard.false_route_budget` 未接。

---

## 2026-09-18 · 删除 mirror 死代码 + block 可见性注释

**做了什么**：① 删除 `edge/mirror` 读请求体塞 header 的死代码（`x-observed-body-prefix` 无人消费，契约无 body 字段）——连同 `MaxBody` 字段、`maxBody()` 方法与只测死数据的 `TestReceiver_BodyIsCapped`；② 在 `edge/proxy` 的 block 分支加注释，点明 403 可见处置是 ADR-0002 已承认的设计。

**改了哪些文件**：`edge/mirror/receiver.go` · `edge/mirror/receiver_test.go` · `edge/proxy/proxy.go` · `docs/plans/2026-09-18-mirror-dead-code-and-block-note.md` · `docs/log.md`

**对应文档**：[`docs/background/decisions/0002-decision-model.md`](docs/background/decisions/0002-decision-model.md)（block 可见性依据）

**验证**：`make gate` 通过（含 `-race`）

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| body 前缀无消费 | grep 核心/api 为空 | ✅ 死数据确认 |
| 删除后编译测试 | 全绿 | ✅ `make gate` 通过（edge/mirror 1.469s · edge/proxy 1.983s） |

**没做 / 遗留**：误导处置读 body（INT-22）留到阶段 2b 契约演进；block 403 可见性不变（设计如此）。

---

## 2026-09-18 · 变更日志改名：Log.md → log.md（小写）

**做了什么**：按用户要求把 `docs/Log.md` 改名为 `docs/log.md`。同步改 `scripts/tracecheck/main.go` 的 5 处硬编码、`.pi/devloop.md` 的 `change_log`、以及 9 个活文档的引用与链接；历史变更包与 `docs/log.md` 旧条目按 audit 门槛③保留原写法。

**改了哪些文件**：`docs/log.md`（由 Log.md 改名）· `scripts/tracecheck/main.go` · `.pi/devloop.md` · `AGENTS.md` · `docs/README.md` · `docs/progress.md` · `docs/modules/README.md` · `.pi/skills/dev-loop-project/SKILL.md` · `docs/plans/_change-package.md` · `docs/plans/README.md` · `docs/kb/dev-workflow.md` · `docs/plans/2026-09-18-log-rename.md`

**对应文档**：[`docs/log.md`](docs/log.md)（变更日志）

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 文件名 | `docs/log.md` 存在，`docs/Log.md` 不存在 | ✅ mv 两步完成 |
| 活文档引用 | 无活文档再写 Log.md | ✅ 仅剩历史快照 |
| 门禁 | tracecheck 认新路径 | ✅ `make trace` 通过 |

**没做 / 遗留**：历史变更包（`docs/plans/2026-09-18-*.md`）与 `docs/log.md` 旧条目里的「Log.md」保留原写法（audit 门槛③历史快照不动）。

---

## 2026-09-18 · 清理 Lua 残留设计 + 判定缓存容量上限（MD-10）

**做了什么**：① 按 ADR-0008（Lua 已移除、L1 改 Go）清理 6 处未跟上的文档残留（`docs/modules/README.md` §4.2 的 adapter-reverse-proxy/sidecar 与 Lua、`docs/modules/_template.md` 语言列表、`docs/design/architecture.md` §7.5/§8.2 的 OpenResty、`docs/design/structure.md` §4.1 的 OpenResty）；② 修 `edge/proxy` 判定缓存无容量上限的 MD-10 缺口 —— 加 `CacheMaxEntries`（默认 65536），满则整体清空。

**改了哪些文件**：`edge/proxy/iface.go` · `edge/proxy/proxy.go` · `edge/proxy/cmd/proxy/main.go` · `edge/proxy/proxy_test.go` · `docs/modules/README.md` · `docs/modules/_template.md` · `docs/design/architecture.md` · `docs/design/structure.md` · `docs/plans/2026-09-18-lua-cleanup-and-cache-cap.md` · `docs/log.md`

**对应文档**：[`docs/background/decisions/0008-edge-language-go.md`](docs/background/decisions/0008-edge-language-go.md)（Lua 移除依据）· [`docs/design/modules.md`](docs/design/modules.md) 的 MD-10（缓存容量上限）

**验证**：`make gate` 通过（含 `-race` 与追溯检查）

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 缓存容量上限 | cap=2 时条目数不超 2 | ✅ `TestCacheRespectsCapacityCap` 通过 |
| Lua 残留归零 | 现行文档无「Lua 是实现语言」漂移 | ✅ 仅剩 language.md 否决记录 + archcheck 门禁（均应保留） |
| 门禁整体 | 全绿 | ✅ fmt · vet · staticcheck · errcheck · archcheck · trace · 许可 · 单测 -race |

**没做 / 遗留**：`language.md` 的 Lua 否决记录与 `archcheck` 的 `.lua` 门禁**保留**（决策依据与防回归，非漂移）；`edge/mirror` 把 body 前缀塞进 header（契约无 body 字段，留待契约演进）；block 决策的 403 可见性（设计议题，非本轮）。

---

## 2026-09-18 · 进度表迁至 docs/progress.md（与 Log.md 同目录）

**做了什么**：把模块实现进度（清单 · 实现方式 · 阶段状态）从 `docs/modules/README.md` 整体迁出，独立成 `docs/progress.md`，与 `docs/Log.md` 同在 `docs/` 根，供人工审计对照；`docs/modules/README.md` 瘦身为模块索引（文档状态 · 下一步 · 新增指南）。同步更新 6 处现行文档的引用。

**改了哪些文件**：`docs/progress.md`（新建）· `docs/modules/README.md`（瘦身）· `docs/README.md` · `AGENTS.md` · `.pi/AGENTS.md` · `docs/design/structure.md` · `.pi/devloop.md`（`doc_layers` 注册 progress.md）· `docs/plans/2026-09-18-progress-relocation.md` · `docs/Log.md`

**对应文档**：[`docs/progress.md`](docs/progress.md)（进度唯一维护处）· [`docs/Log.md`](docs/Log.md)（变更日志）

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 进度表链接不悬空 | `make trace` 无 `TC-3` | ✅ 追溯检查通过 |
| 旧「完成度」引用更新 | 现行文档不再指向 `docs/modules/README.md` 完成度 | ✅ 仅剩历史快照 + §4/§5 引用 |
| 规则 ID 引用真实 | 无 `D-3` | ✅ `AR-3` / `TB-22` / `TB-2` 均命中 |

**没做 / 遗留**：`docs/modules/README.md` §4.2 的 Lua/Rust 表述与 ADR-0008（L1 改 Go、③④ 合并）漂移，属既有问题，本轮未修（另开一条）；历史变更包与旧日志条目按 audit 门槛③不动。

---

## 2026-09-18 · 模块实现方式总览（自研 / 复用开源）

**做了什么**：在模块索引新增 §1a「实现方式总览」，把散落在 `architecture.md`（复用三原则）· `language.md`（分层选型）· `structure.md` §3（存储分工）里的「开源 vs 自研」结论汇总成一张表；并把与实现方式相关的 4 个未决项（D1 / D6 / D7 / L3 是否进 MVP）集中标注去向。

**改了哪些文件**：`docs/modules/README.md`（新增 §1a）· `docs/plans/2026-09-18-progress-overview.md`（本轮变更包）· `docs/Log.md`

**对应文档**：[`docs/design/architecture.md`](docs/design/architecture.md) §1（复用三原则）· [`docs/design/language.md`](docs/design/language.md) §1 · [`docs/design/structure.md`](docs/design/structure.md) §3 · [`docs/background/notes/implementation-discussion.md`](docs/background/notes/implementation-discussion.md) §6.1

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 新增规则 ID 引用真实存在 | 无 `D-3` 悬空 | ✅ `AR-3` / `TB-22` / `TB-2` / `ADR-0008` 均命中 |
| 未决项不当作决策写死 | 标「未决」并给去向 | ✅ 4 项均标 ⏳ + 指向 notes / modules.md §7 |

**没做 / 遗留**：未替用户拍板任何未决项（D1 / D6 / D7 / L3 是否进 MVP 仍待用户决策）；本表是既有结论的汇总，无新决策，故无 ADR。

---

## 2026-09-18 · 技能合并：8 个 → 5 个（项目 6 → 3）

**做了什么**：把触发面重叠、纪律重复的技能合并 —— `spec-first-dev` 并入 [`dev-loop-project`](../../.pi/skills/dev-loop-project/SKILL.md)（开工门禁 + 范围纪律）；`research-survey` + `design-decision` + `adversarial-benchmark` 合并为 [`evidence-and-decisions`](../../.pi/skills/evidence-and-decisions/SKILL.md)（调研 → 决策 → 基准）。加上全局 `dev-loop` / `audit`，**全机共 5 个技能**；项目 `.pi/skills/` 从 6 个降到 3 个。

**改了哪些文件**：`.pi/skills/dev-loop-project/SKILL.md`（重写，吸收开工纪律）· `.pi/skills/evidence-and-decisions/SKILL.md`（新增，合并三份）· `.pi/AGENTS.md` · `AGENTS.md` · `docs/README.md` · `docs/kb/dev-workflow.md` · `docs/kb/removed-skills.md`（追加合并记录）· `docs/background/decisions/README.md` · `docs/background/research/README.md` · `scripts/tracecheck/main.go`（强制名单去掉 `spec-first-dev`）· `~/.pi/agent/skills/dev-loop/SKILL.md` · `~/.pi/agent/skills/audit/SKILL.md` · `docs/plans/2026-09-18-dev-loop.md`（追加第 14 节）· `docs/Log.md`

**对应文档**：[`docs/kb/removed-skills.md`](../kb/removed-skills.md)（合并清单 + 恢复方法 + 恢复后要同步哪 5 处）· [`docs/kb/dev-workflow.md`](../kb/dev-workflow.md) §5（路由图已更新）· [`docs/plans/2026-09-18-dev-loop.md`](2026-09-18-dev-loop.md) §14

**验证**：`make trace` 追溯检查通过 · `make gate` 通过 · `make dev` PASS

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 合并后技能数 | 项目 3 · 全机 5 | ✅ `.pi/skills/` = dev-loop-project · evidence-and-decisions · threat-model |
| 内容不丢 | 原 4 份的实质内容均可定位 | ✅ dev-loop-project §1/§2（原 spec-first-dev）· evidence-and-decisions §1/§2/§3（原三份） |
| 行数收敛 | 少于合并前 | ✅ 592 行 → 492 行（三份，重复纪律已删） |
| 引用不悬空 | 无旧技能名指向已删路径 | ✅ `make trace` 通过（含 `DEV-3` 点名与 `TC-3` 链接） |
| 可恢复 | 原件备份 + 恢复步骤 | ✅ `~/.pi/backups/removed-skills/merged-2026-09-18/`（4 份原件） |

**没做 / 遗留**：`docs/background/research/knowledge-base-audit.md` 仍写旧技能名（属**历史快照**，按 `audit` 门槛 ③ 不改）；合并版技能较长（`evidence-and-decisions` 252 行），若以后只做 ADR 也要整份加载 —— 目前可接受，若变碍事再拆回。

**修正（同日）**：图示对齐 —— 自动补齐右边框后，`§2 一轮时序` 与 `§3 门禁链路` 出现残留的连接符／多余空行；前者重写为无边框的竖向流程，后者重写为缩进树，`§6 失败与返工` 重写为三个等宽并排框（每行 81 列，已核）。

---

## 2026-09-18 · 移除第三方技能 skill-creator（248K · 10 个可执行脚本）

**做了什么**：把第三方技能 `skill-creator` 从 `.pi/skills/` 移出（备份在 `~/.pi/backups/removed-skills/skill-creator-2026-09-18/`），并同步 4 处引用。移除后项目技能库为 **6 个目录，全部自建 Markdown，可执行代码文件数 = 0**。

**改了哪些文件**：`AGENTS.md` · `.pi/AGENTS.md` · `docs/README.md` · `docs/kb/README.md` · `docs/kb/removed-skills.md` · `docs/kb/dev-workflow.md`（路由图）· `~/.pi/agent/skills/dev-loop/SKILL.md`（全局路由表）· `docs/plans/2026-09-18-dev-loop.md`（追加第 13 节）· `docs/Log.md`

**对应文档**：[`docs/kb/removed-skills.md`](../kb/removed-skills.md)（为什么删 + 怎么恢复 + 恢复后要同步哪 5 处）· [`docs/plans/2026-09-18-dev-loop.md`](2026-09-18-dev-loop.md) §13（删除记录）

**验证**：`make trace` 追溯检查通过 · `make gate` 通过 · `make dev` PASS

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 删除前依赖检查（门槛 ①） | 除自身与路由表外无依赖 | ✅ 只有 4 处文字引用（`AGENTS.md` · `.pi/AGENTS.md` · `docs/kb/dev-workflow.md` · 全局路由表），无代码/流程依赖 |
| 可恢复性（门槛 ④） | 有备份且路径可查 | ✅ 248K · 18 文件移至仓库外；路径与重装方法写入 kb |
| 删除后可达性 | 无悬空引用、无点名缺失 | ✅ `make trace` 通过（含 `DEV-3` 技能点名与 `TC-3` 链接） |
| 安全面 | 项目技能里可执行代码归零 | ✅ `find .pi/skills -name '*.py' -o -name '*.sh'` → 0 |

**没做 / 遗留**：`spec-first-dev` 与 `dev-loop` §3③ 仍有部分重叠（未合并，其「开工 4 项检查」清单仍有独立价值）；若要技能评测能力，需按 kb 记录重装。

---

## 2026-09-18 · 规范链路体检 + 学习用图

**做了什么**：把整套开发规范当成被审对象查了一遍 —— 找出 **6 处不合理**（其中 1 处致命：项目实例化技能因同名冲突从未被加载），全部修正；并新增学习用图 [`docs/kb/dev-workflow.md`](../kb/dev-workflow.md)（三层结构 · 一轮时序 · 门禁链路 · 追溯链路 · 技能路由 · 返工路径 · 常见误解）。

**改了哪些文件**：`AGENTS.md` · `.pi/AGENTS.md` · `.pi/skills/dev-loop-project/SKILL.md`（由 `dev-loop` 改名）· `scripts/tracecheck/main.go` · `~/.pi/agent/skills/dev-loop/SKILL.md` · `docs/README.md` · `docs/plans/_change-package.md` · `docs/kb/dev-workflow.md` · `docs/plans/2026-09-18-dev-loop.md`（追加第 12 节）· `docs/Log.md`

**对应文档**：[`docs/kb/dev-workflow.md`](../kb/dev-workflow.md)（图示）· [`docs/plans/2026-09-18-dev-loop.md`](2026-09-18-dev-loop.md) §12（体检结论 + 审视记录）

**验证**：`make trace` 追溯检查通过 · `make gate` 通过 · `make dev` PASS

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 同名技能冲突 | 确认哪个生效 | ✅ pi 源码 `skills.js`（第 329–351 行）：保留**先加载**的；全局先于项目 → 项目那份被丢弃 |
| 引用全局技能的链接 | 应被门禁核 | ✅ 扩展 `DEV-3` 支持 `~/.pi/agent/skills/...` 后，能核到全局 `dev-loop` / `audit` |
| 强制技能名单 | 应含实例化与审视技能 | ✅ 改为 `dev-loop` · `dev-loop-project` · `spec-first-dev` · `audit` |
| `.pi/AGENTS.md` 技能清单 | 与实际一致 | ✅ 原先写「6 个 / 自建 5 个」，实际 7 个目录；已改写并拆出全局层 |
| 项目技能悬空链接 | 应报 `TC-3` | ✅ 改名后依次报出 3 处陈旧链接，全部修正 |

**没做 / 遗留**：`iteration_cmds` 仍只在技能表里说明用途（无工具读它，属人工约定）；图示放在 `kb/`（非规范），若日后规范变动需靠人同步（无机器校验）。

---

## 2026-09-18 · 审视（audit）：查「留下的东西还准不准」

**做了什么**：新增全局技能 `audit`（查漂移 / 无用信息 / 僵尸内容，**四道删除门槛**，产出审视表），接进流程（L 档必做、AGENTS.md 与 dev-loop 同步），并把可机器核的两类接进本项目的追溯检查（新增 `TC-2` 过期状态标记 · `TC-3` 悬空链接）。首次审视即查出 6 条真问题并全部修正。

**改了哪些文件**：`~/.pi/agent/skills/audit/SKILL.md` · `~/.pi/agent/AGENTS.md` · `~/.pi/agent/skills/dev-loop/SKILL.md` · `~/.pi/agent/skills/dev-loop/assets/change-package.template.md` · `~/.pi/agent/skills/dev-loop/assets/devloop.template.md` · `~/.pi/agent/skills/dev-loop/assets/agents-snippet.md` · `~/.pi/agent/skills/dev-loop/scripts/devloop-init.sh` · `scripts/tracecheck/main.go` · `AGENTS.md` · `.pi/devloop.md` · `.pi/skills/dev-loop/SKILL.md` · `docs/plans/_change-package.md` · `docs/README.md` · `docs/modules/judge.md` · `docs/modules/session.md` · `docs/plans/2026-09-18-dev-loop.md`（追加第 11 节）· `docs/Log.md`

**对应文档**：`~/.pi/agent/skills/audit/SKILL.md`（审视做法与删除门槛）· `docs/plans/2026-09-18-dev-loop.md` §11（本节含审视记录）· `.pi/skills/dev-loop/SKILL.md` §5.1（本项目落地）

**验证**：`make trace` 追溯检查通过（新增两类检查后归零）· `make gate` 通过 · 两项注入实验命中预期

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 注入「已存在路径 + 待创建」 | 报 `TC-2` | ✅ 命中 `docs/modules/judge.md:89` |
| 首次扫描本项目技能链接 | 报 `TC-3` | ✅ 命中 4 处层级写错的链接，修正后归零 |
| 「A 已建；B 待写」同一行 | 不误报 | ✅ 0 条 |
| 真实历史缺口 | 首次运行即查出 | ✅ `judge.md` / `session.md` 的「待创建」已过期；`docs/README.md` 的计数已过期 |
| 门禁整体 | 全绿 | ✅ `make gate` 通过 |

**没做 / 遗留**：语义一致性、误导性叙述、内容是否真无用仍靠人读（机器只能核形状）；审视只在 L 档强制，M 档靠判断；本轮无删除动作（六条均为修正）。

---

## 2026-09-18 · 开发规范泛化为跨项目通用（全局技能 + 适配面 + 初始化脚本）

**做了什么**：把只在本仓库成立的开发规范提为**跨项目通用规范** —— 全局三条铁律（`~/.pi/agent/AGENTS.md`）、全局技能 `dev-loop`（S/M/L 分级 · 五步 · 产物形状 · 追溯接口 · 评审 · 返工路径 · 模板与初始化脚本），本项目改为只写差异的实例化。

**改了哪些文件**：`~/.pi/agent/AGENTS.md` · `~/.pi/agent/skills/dev-loop/SKILL.md` · `~/.pi/agent/skills/dev-loop/assets/devloop.template.md` · `~/.pi/agent/skills/dev-loop/assets/change-package.template.md` · `~/.pi/agent/skills/dev-loop/assets/change-log.template.md` · `~/.pi/agent/skills/dev-loop/assets/agents-snippet.md` · `~/.pi/agent/skills/dev-loop/scripts/devloop-init.sh` · `.pi/devloop.md` · `.pi/skills/dev-loop/SKILL.md` · `AGENTS.md` · `scripts/tracecheck/main.go`（日志路径检查支持 `~/` 与绝对路径）· `docs/README.md` · `docs/plans/2026-09-18-dev-loop.md`（追加第 10 节）· `docs/Log.md`

**对应文档**：`docs/plans/2026-09-18-dev-loop.md` §10（泛化的决定与三层结构）· `AGENTS.md` §1 / §4（本项目只写差异）· `.pi/devloop.md`（适配面）

**验证**：`make trace` 追溯检查通过 · `make gate` 通过 · `make dev` PASS · 初始化脚本在空项目 / Python 项目上自测通过且幂等 · 全局技能头部合法（`name` 合规 · `description` 170 字符）

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 空项目初始化 | 生成 4 项骨架并探测验证命令 | ✅ 无标记时写入占位符 `verify_cmd: <本项目的一键验证命令>` |
| Makefile / Python 项目 | 探测到对应验证命令 | ✅ 分别写入 `make gate` 与 `pytest -q` |
| 重复执行初始化 | 不覆盖已有文件 | ✅ 「新增 0 项，跳过 4 项」 |
| 适配面可解析 | YAML 合法且字段齐全 | ✅ 10 个字段全部读出 |
| 本项目追溯 | 零错误 | ✅ 追溯检查通过（1 提示 + 3 登记豁免） |

**没做 / 遗留**：追溯检查工具**不上提**为通用工具（先跑通两三个项目再抽象）；通用技能里的「一键验证命令」仍靠项目自建（脚本只探测并提示）；全局规范对不适用的项目需在该项目 `.pi/devloop.md` 写 `enabled: false` 手动豁免。

---

## 2026-09-18 · 技能强制写进 `AGENTS.md`（+ 门禁核对 `DEV-3`）

**做了什么**：把「开工前先加载 `dev-loop` 技能」从建议变成规范条款 —— `AGENTS.md` §1 / §4 写入强制条款与技能路由表（强制 2 个 · 按需 5 个），并新增门禁检查 `DEV-3`：本文点名的技能文件必须真实存在且头部有 `name` / `description`。

**改了哪些文件**：`AGENTS.md` · `scripts/tracecheck/main.go` · `.pi/skills/dev-loop/SKILL.md` · `docs/README.md` · `docs/plans/2026-09-18-dev-loop.md`（追加第 9 节）· `docs/Log.md`

**对应文档**：`AGENTS.md` §4（技能路由表 + 强制条款）· `.pi/skills/dev-loop/SKILL.md`（配套件表新增「强制条款」一行）· `docs/plans/2026-09-18-dev-loop.md` §9

**验证**：`make gate` 通过 · `make trace` 零错误 · 两项新注入实验按预期报错

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| `AGENTS.md` 点名不存在的技能 | 报 `DEV-3` | ✅ 报「技能文件不存在」+「没有点名强制技能」 |
| 技能文件缺 `name` 头 | 报 `DEV-3` | ✅ 报「缺 YAML 头部（name / description）」 |
| 正常仓库 | 零错误 | ✅ 追溯检查通过（1 提示 + 2 登记豁免） |

**没做 / 遗留**：技能路由表靠人工维护（`DEV-3` 只核「点名的存在 + 强制的被点名」）；技能的具体执行质量（有没有真按五步走）机器核不了，靠变更包与 Log 留痕复查。

---

## 2026-09-18 · 开发循环与追溯门禁（tracecheck · Log · 变更包模板）

**做了什么**：把「设计 → 文档 → 代码 → 测试 → 证据」这条链变成可机器核的固定流程 —— 新增追溯门禁 `make trace`（已进 `make gate`）、变更日志（本文件）、变更包模板与每轮固定动作（技能），并顺手查出两处真实的文档悬空引用。

**改了哪些文件**：`scripts/tracecheck/main.go` · `scripts/tracecheck/allow.txt` · `.pi/skills/dev-loop/SKILL.md` · `docs/plans/_change-package.md` · `docs/plans/2026-09-18-dev-loop.md` · `docs/plans/2026-09-18-policy-2a.md`（按模板重排）· `docs/Log.md` · `Makefile` · `AGENTS.md` · `docs/README.md` · `docs/plans/README.md`

**对应文档**：`AGENTS.md` §4（每轮必须留下四样）· `.pi/skills/dev-loop/SKILL.md`（五步 + 输出形状）· `docs/plans/_change-package.md`（模板）· `docs/design/modules.md` 的 `MD-2` / `MD-17` / `MD-22`（门禁强制的三条）

**验证**：`make gate` 通过（`gate` 现在含 `trace`）· `make trace` 零错误（1 条提示 + 2 条登记豁免）· 八项注入实验均按预期报错 · `make dev` PASS

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 正常仓库 | `make trace` 零错误 | ✅ 「追溯检查通过」 |
| 悬空规则 ID | 报 `D-3` + `文件:行` | ✅ 查出真实历史缺口：`docs/README.md` 的 NI 范围多写了一条（已修正） |
| 模块有代码无文档 | 报 `MD-17` | ✅ 移除 `docs/modules/policy.md` 后即报 |
| 模块有代码无单测 | 报 `MD-22` | ✅ 移除 `policy_test.go` 后即报 |
| 日志缺段 / 引用死路径 | 报 `DEV-2` | ✅ 缺 `**证据**` 段与写假路径均报出 |
| 豁免过期 | 报 `ALLOW` | ✅ 注入匹配不上的豁免后报「豁免已过期」 |
| 文档格式变了 | 拒绝静默放行 | ✅ 前缀解析到 0 个即报错退出 |
| 门禁整体 | 全绿 | ✅ 格式化 · vet · staticcheck · errcheck · archcheck · trace · 许可 · 单测(-race) |

**没做 / 遗留**：规则 ID 未做「逐条映射到代码与测试」（153 条全量映射成本高，本轮只做模块级 + 引用存在性）；门禁查不了「测试覆盖得好不好」与「设计逻辑写得对不对」（那靠评审与场景表）。

---

## 2026-09-18 · `policy` 阶段 2a（策略装载 · 版本 · 校验和 · 规则供给）

**做了什么**：判定引擎第一次有了真实规则来源 —— 从配置文件装载策略、严格校验、产出带版本号与校验和的不可变快照、写入版本台账、供 `judge.RuleSource`。同时闭合阶段 1 的「空规则集静默启动」缺口：配置缺失或非法**必须**启动失败。另配一套开发期验证入口（`make check-config` / `replay` / `smoke` / `dev`）。

**改了哪些文件**：`core/internal/policy/`（新增 `iface.go` · `policy.go` · `policy_test.go`）· `core/cmd/core/main.go` · `core/cmd/core/main_test.go` · `core/internal/control/rules.go`（删除，占位规则源被 `policy` 接管）· `deploy/config/config.example.yaml` · `Makefile` · `scripts/devcheck/main.go` · `scripts/dev/smoke.sh` · `docs/spec/config.md` · `docs/modules/policy.md` · `docs/background/decisions/0009-policy-version-source.md` · `docs/plans/2026-09-18-policy-2a.md`

**对应文档**：`docs/spec/config.md`（配置全表 + JSON Schema）· `docs/modules/policy.md`（九章）· ADR-0009（版本来源）· `docs/design/structure.md` §1.6（接缝已接）

**验证**：`make gate` 通过（含 `-race`）· `make dev` 全绿 · 单测 37 个（`policy`）+ 6 个（装配层）

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 合法配置 | 装载成功并打印摘要 | ✅ `policy_id=dev-smoke version=1 checksum=2cd6a7d1… 规则=2 条` |
| 权重越界 | 启动失败并指出字段 | ✅ `rules[1].weight=1.5 越界（要求 0 < weight ≤ 1）` |
| 规则回放 | 分数与命中信号正确 | ✅ `0.9（ua-headless,path-git）· 0.3 · 0 · 0.5` |
| 在线冒烟 | 判定面应答且 `decision_id` 幂等 | ✅ 3 样本全通过，`action=origin severity=none` |
| 影子模式回归 | 分数拉满也不处置 | ✅ 恒 `route_origin` |

**没做 / 遗留**：下发面 `Pull` / `Watch` / `Ack` 未实现；`store` / `telemetry` 真实后端未接；`whitelist` 段只解析不消费（消费方是 `director`）；`severity` 档位与 `E2`（TLS 指纹一致性，项目级 P0）仍待。**补记（后续验证）**：`executed=mirage`（**真正进入幻境后端**）已**端到端验证**通过：

```
客户端[172.21.0.1] → 适配器 (L1)[GET /.git/config] → 核心判定[分值 0.90 · 信号 ua-headless,path-probe]
  → 决策[改道（route_mirage）] → 幻境后端[mirage]
```

此前"验不到"的真因不是功能缺失，而是**部分重建**：compose 的 `up -d --build` 只重建**有变化**的服务，
core 换了新网络命名空间而兄弟服务留在旧的里面 ⇒ 幻境/业务地址全部 `connection refused`（business 容器自连同地址是通的，可见问题在命名空间不在服务）。
已把这一精确成因写进 `docs/kb/known-issues.md` K-21 与观测文档，并确认失败时应整栈重建（scripts/shen.sh restart）。验完已恢复默认影子栈。


