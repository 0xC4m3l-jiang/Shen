# 变更包归档（历史留痕）

> **为什么有这个文件**：每轮开发的变更包（计划）在**其设计已实现并验证**后归档为下表一行 —— 保留追溯线索，不再占用独立文件。
> **原文仍可取回**：每份都在 git 历史里（`git log --diff-filter=D -- docs/plans/<文件名>` 或 `git show <提交>:docs/plans/<文件名>`）。
> **本目录只保留**：未实现/待确认的变更包 · [`_change-package.md`](_change-package.md)（模板）· 本文件。

| 日期 | 变更包（原文件） | 主题 | 状态 | 追溯 |
| --- | --- | --- | --- | --- |
| 2026-09-17 | `2026-09-17-director-2a.md` | `director` 阶段 2a 实施计划 | ✅ **已确认并实现** | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-code-review.md` | 变更包 · 2026-09-18 · 全仓代码审查与加固 | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-deception-engine-design.md` | 变更包 · 2026-09-18 · 补欺骗引擎设计（诱饵面 + 蜜罐入口） | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-deception-modules-impl.md` | 变更包 · 2026-09-18 · 欺骗引擎模块实现（isolation / honeypot / decoy / r | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-dev-loop.md` | 开发循环与追溯门禁（`make trace` · 变更包模板 · 变更日志） | ✅ **已实现并验证** | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-director-2a.md` | 变更包 · 2026-09-18 · director 阶段 2a 实现（6 项决策已确认） | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-docs-wiring-cleanup.md` | 变更包 · 2026-09-18 · 代码整理 + 调用链核实 + 文档站完善 | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-judgement-layer-design.md` | 变更包 · 2026-09-18 · 补判别层设计（指纹 + 会话级判别 + 归因令牌） | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-log-rename.md` | 变更包 · 2026-09-18 · 变更日志改名 Log.md → log.md（小写） | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-lua-cleanup-and-cache-cap.md` | 变更包 · 2026-09-18 · 清理 Lua 残留设计 + 判定缓存容量上限（MD-10） | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-mirror-dead-code-and-block-note.md` | 变更包 · 2026-09-18 · 删除 mirror 死代码 + block 可见性注释 | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-module-cleanup.md` | 变更包 · 2026-09-18 · 模块代码整理（简化，无行为变更） | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-module-overview.md` | 变更包 · 2026-09-18 · 模块总览（结构图 + 关系图 + 23 模块解释） | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-policy-2a.md` | `policy` 阶段 2a（策略装载 · 版本 · 校验和 · 规则供给） | ✅ **已实现并验证** —— 9 项决策已由用户确认 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-polymorphism-and-module-docs.md` | 变更包 · 2026-09-18 · 诱饵多态机制 + 补完模块文档 | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-progress-overview.md` | 变更包 · 2026-09-18 · 模块实现方式与进度总览 | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-progress-relocation.md` | 变更包 · 2026-09-18 · 进度表迁至 docs/progress.md（与 Log.md 同目录） | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-18 | `2026-09-18-responder-and-injection-design.md` | 变更包 · 2026-09-18 · 补响应层设计（生成式欺骗响应 + 间接注入防护） | 已实现 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-caddy-coupling-guardrails.md` | 降低 Caddy 耦合的可见性 —— 耦合面自动提取 + 升级兼容锁 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-caddy-l1-base.md` | L1 反向代理底座换成内嵌 Caddy（adapter-proxy 重构） | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-content-path-injects.md` | 2b 内容通路 —— 响应改写规则经策略面下发 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-deception-l2-l3-frameworks.md` | L2 第一批 —— `honeypot-protocol` 框架落地 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-directory-and-module-map.md` | 目录与模块地图（谁在哪 · 能力是什么 · 怎么接进来） | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-docker-and-repo-layout.md` | Docker 一键起全套 + 目录工整化（Python 环境归层、依赖离线化） | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-docs-accuracy-and-readme.md` | 整体文档准确化 + 根 README（项目是什么 / 怎么用 / 效果什么样） | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-docs-audit-and-module-review.md` | 文档审视（删除无用信息 · 事实核验）+ 模块定义/设计一致性核验 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-docs-organization-and-kb.md` | 文档整理（按设计 / 知识库 / 模块 / 运行分类）+ 知识库重做 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-doctor-and-metrics.md` | 接入自检实现（`INT-17`）+ 指标字典（`AR-28`） | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-e2-tls-fingerprint-tool.md` | `E2`（TLS 指纹一致性）从「待执行实验」变成「可复现工具 + 判定口径」+ 主臂初测 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-forwarding-boundary-verification.md` | 转发路径的边界行为实测锁定（升级 · 流式 · 大响应 · 大上传 · 协议版本） | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-forwarding-deception-hardening.md` | 转发/欺骗路径硬化 —— 修掉对外可见面的代理栈指纹（`OH-2`）+ 蜜罐范围裁定 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-git-and-commit-workflow.md` | 引入版本控制（git）+ 把「提交」纳入每轮收尾 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-l4-analysis-python.md` | L4 分析层（Python）—— `llm-components` · `intent` · `chain` · `st | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-l4-runtime-and-event-contract.md` | L4 接入运行时（近线 worker）+ 事件契约对齐（跨语言） | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-local-logs-and-verify.md` | 本地日志（逐判定）+ 一键验证与定位线索 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-ni12-vseries-and-ar29-floor.md` | `NI-12` 的 `V-1…V-4` 自动化 + `AR-29` 空载下界 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-observability-and-console.md` | 观测面读路径 + 控制台（Web UI）+ 一键人工测试环境 + 接入/使用文档 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-oh3-leak-check.md` | `OH-3` 泄漏检查落地 + 设计一致性核查 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-policy-plane.md` | 策略面（S4）落地 —— 核心下发 → 适配器应用 → 真实改道 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-remaining-modules-config-and-declarative.md` | `adapter-dns` 配置收口 · `netpolicy` 声明式产物 · `console` 模块落地 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-revert-python-and-tracecheck-recovery.md` | 回退 Python/TS 工具链引入 + 修复 tracecheck（自伤恢复）+ `.gitignore`（`ST-2 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-rule-clarify-shared-types.md` | 规则措辞修正（`MD-5` / `MD-19` ↔ 进程内共享类型目录） | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-runbook-and-start-script.md` | 运行手册 + 一键启动脚本 + 产物卫生 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-tls-termination-belongs-to-l0.md` | TLS 终结默认交客户 L0（`E2` 实测驱动的重估） | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-traffic-scenarios-functional-verification.md` | 全场景伪造流量 + 整体功能验证（含缺口清单） | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-traffic-verification-script.md` | 伪造流量与判定核对脚本（`scripts/traffic/`） | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-19 | `2026-09-19-verify-opt-logs-docs.md` | 验证 + 代码/日志优化 + 文档整合减量 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-20 | `2026-09-20-dag-completion.md` | DAG 图示收口（逐请求唯一事件 · 真实落点可观测 · 判定标签修正） | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-20 | `2026-09-20-dag-text-fitting.md` | DAG 图上文字完整、不溢出 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-20 | `2026-09-20-logic-map-and-docs-consolidation.md` | 全链路逻辑梳理（蜜罐以外）+ docs 整合 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-20 | `2026-09-20-per-request-dag.md` | DAG 改为「逐请求链路」（动态 · 不聚合） | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-20 | `2026-09-20-step-clickable-dag.md` | DAG 逐步可点（每一步看请求 / 响应 / 为什么执行） | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |
| 2026-09-20 | `2026-09-20-traffic-dag-view.md` | 流量调度 DAG 图（意图 vs 实际落点）+ 告警在图中展示 | 已验证 | 见 [`../log.md`](../log.md) 同日条目 |

**合计归档 53 份**（2026-09-17 → 2026-09-20）。判定依据（可复核）：① 计划里出现的**带目录前缀的仓库路径**全部存在（少数"缺失"是计划正文里的**反例路径**与已改名/已删除文件，见各自正文）；
② 计划都有对应的 `docs/log.md` 条目与门禁通过记录；③ 实现位置在 `docs/modules/`、`docs/spec/` 与代码里可查。
