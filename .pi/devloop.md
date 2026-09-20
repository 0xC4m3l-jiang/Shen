# 项目适配面 —— 开发规范（技能 `dev-loop`）读取本文件

> 通用规范在 `~/.pi/agent/skills/dev-loop/SKILL.md`（全局）+ `~/.pi/agent/AGENTS.md`（全局强制三条）。
> 本文件只写**本项目的取值**：路径、命令、依据在哪。改路径或命令时**同一轮内**更新本文件。
> 与 [`../AGENTS.md`](../AGENTS.md) 冲突时以 `AGENTS.md` 为准（项目规范优先于全局规范）。

```yaml
enabled: true
language: 中文

# 依据的权威位置：项目强制规范 + 已确认的技术基线
spec_sources:
  - AGENTS.md
  - docs/design/

# 文档分层
doc_layers:
  - path: docs/design/        # 已确认的规则 —— 开发不得偏离
    role: spec
  - path: docs/modules/       # 模块设计（一模块一文件，固定九章）
    role: spec
  - path: docs/progress.md    # 模块规划进度（完成度唯一维护处，与 log.md 供人工审计）
    role: spec
  - path: docs/spec/          # 契约与字典（config / dependencies …）
    role: spec
  - path: docs/plans/         # 变更包（待确认提案）
    role: reference
  - path: docs/log.md         # 变更日志
    role: reference
  - path: docs/kb/            # FAQ / 已踩的坑
    role: reference
  - path: docs/background/    # ADR / 讨论稿 / 调研
    role: reference

# 每轮的变更包与变更日志
change_package_dir: docs/plans/
change_log: docs/log.md

# 一键验证命令（含静态检查 + 架构检查 + 追溯检查 + 许可审计 + 单测 -race）
verify_cmd: make gate

# 一轮的收尾命令：门禁 → 提交 → 校验工作区干净（每轮**必须**跑，见 AGENTS.md §4）
commit_cmd: make done MSG="<一句话主题>"

# 只提交（不重跑门禁）；缺 MSG 直接失败
commit_only: make commit MSG="<一句话主题>"

# 追溯检查命令（本项目的实现：scripts/tracecheck/）
# 它覆盖技能 `audit` §6 的四类：悬空引用（D-3）· 孤儿文档（MD-2 反向检查）· 过期状态标记（TC-2）· 过期豁免（ALLOW）
trace_cmd: make trace

# 泄漏检查（本项目的实现：scripts/check-leak/）
# 它覆盖 OH-1 / OH-3 / OH-4 / OH-5：响应面字符串字面量 ↔ 禁用清单 · 响应头不回传决策信息
leak_cmd: make leakcheck

# 迭代期快速验证命令
iteration_cmds:
  - make check          # 构建 + 格式化 + vet + 架构 + 追溯 + 泄漏（不需下载工具）
  - make dev            # 效果验证：配置干跑 → 起核心 → 冒烟 → 规则回放
  - make ai-check       # AI 欺骗内容注入端到端验收（阶段 A 六项，scripts/dev/ai-inject-check.py）
  - make replay         # 规则回放：样本 → 分数 → 命中信号
  - make smoke          # 在线冒烟：判定面形状 + decision_id 幂等

# 编号体系：规则 ID（AR / INT / ST / MD / NI / TB / TM / SB / OH / BA / D），
# 定义与登记处见 docs/design/README.md；引用必须真实存在（D-3 / D-8）
numbering: "docs/design/README.md（前缀表 + §4 废弃登记）"

# 本项目特有的例外
notes: |
  · 改动分级按技能 §2 走；L 档必填追溯矩阵（docs/plans/_change-package.md §3）与**审视记录**（§7.1）。
  · L 档收尾加载全局技能 `audit`（~/.pi/agent/skills/audit/SKILL.md）做一次审视：对着 diff 核文档、删无用、标废弃。
  · 未经用户确认，禁止把内容升格进 docs/design/（AGENTS.md §3.1）。
  · 判定与响应生成只在核心实现一次（AR-2 / AR-5）；适配器禁止实现判定逻辑。
  · 追溯检查的已知缺口必须登记在 scripts/tracecheck/allow.txt 并写理由；过期即报。
  · 历史记录类文件（docs/background/ · docs/plans/ · docs/log.md · docs/kb/）不在过期状态标记的检查范围内。
```
