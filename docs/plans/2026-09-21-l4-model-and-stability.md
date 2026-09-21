# 变更包：L4 三任务接模型 + 稳定性三项

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | L4 的 `intent` / `chain` / `strategy` 接模型（经唯一出口 + 护栏、失败回落）；顺带修三项稳定性（适配器落地、锁文件↔venv 门禁、`make pygen` 零 diff） |
| 日期 | 2026-09-21 |
| 状态 | 已实现（门禁绿 · 独立评审见 §7.2） |
| 涉及模块 | `ai-capability`（第 25 行，阶段 3）· `llm-components`（第 20 行）· `intent`（17）· `chain`（18）· `strategy`（19） |
| 决策数 | 已答 6 项 / 待定 4 项（进 ADR-0031 的「未解决」） |
| 关联 | [ADR-0031](../background/decisions/0031-analysis-reuse-and-model-backend.md)（新）· [ADR-0024](../background/decisions/0024-ai-oss-reuse-boundary.md)（四项未解决被收口）· [ADR-0026](../background/decisions/0026-cloud-model-backend.md) · [ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md) · 调研 [`../background/research/l4-oss-reuse.md`](../background/research/l4-oss-reuse.md)（新）· 日志 `docs/log.md` 同日条目 |

---

## 1. 需求与验收（PRD 段）

**要解决什么**：`analysis/llm/resources/prompts/` 下那三份 L4 提示词从写下来那天起就没被生产代码用过
（只在单测里渲染过）—— 而它们**也不在** `aicap` 的任务注册表里，所以不受 `AR-33` 出口约束。
只要有人「先把模型接上」，最自然的写法就是在 `worker.py` 里直接调客户端，从而绕开两道护栏。

**做完之后，用户能做什么**：

- `make analysis-llm` 让 L4 的三步分析**走模型**（`--llm`，默认关闭）；模型不可用时三步各自回落确定性版，整轮仍然成功；
- 读结论的人能看出**这份结论是谁产的**（`generator`）以及**模型那一路为什么没成**（`model_rejected`）；
- 无密钥环境下 `make gate` / `make dev` / CI 的行为**与接模型之前逐字一致**。

**验收判据**（可验证）：

1. `make gate` 全绿（含新增的 `check-pydeps`、**118 例** pytest、`make trace` 零错误）；
2. `make dev` 6 段全过，且 L4 段输出与上一轮**一致**（`strategy: accepted=False` 仍被拒 —— 演示栈无幻境后端）；
3. 连跑两次 `make pygen` **字节零 diff**；
4. `make analysis-llm`（未设 `SHEN_AI_KEY`）→ 打印「模型回落 3 步」+ 每步原因、结论仍产出 2 条、**退出码 0**；
5. `analysis/aicap` 与 `analysis/llm` 的依赖白名单（`MD-4`）仍绿 —— 新增的模型调用**没有**在别处开口子。

**不做什么**：

- **不改 `AR-30` 的适用面**：热路径仍然零模型（本轮接的是近线/离线三步）；
- **不引任何新运行期依赖**（`requirements.txt` 仍三项）—— 不引 `Outlines` / `Instructor` / `Presidio` / `json_repair` / `NetworkX`；
- **不做模型 vs 规则的对照基准**（那是另一轮，按 skill `evidence-and-decisions` §3）；
- **不接 `kind=content` 的模型路径**（阶段 B），也不接内容轮换的消费方；
- **不动 `docs/design/`**（本轮无升格 —— 没有需要新规则的东西）。

---

## 2. 设计逻辑（技术设计段）

**决策树**：

```text
L4 三任务接模型
├── 范围：三个都接？（是 —— 用户确认）
├── 契约放哪（因为 aicap 只能依赖 llm）
│   ├── 各领域模块各写一份 ⇒ 漂移（MD-5 禁止）✗
│   ├── aicap 反向 import 领域模块 ⇒ 门禁 MD-4 红 ✗
│   └── 新 llm/schemas.py，两侧都导入 ⇒ 唯一一份 ✓
├── 提示词放哪
│   ├── 留 llm/ 下 ⇒ 两套系统固化（kb 发现 1 未关）✗
│   └── 迁到 aicap/resources/prompts/ + 三段式改写 + 登记 ⇒ 一套 ✓
├── 闭集与数值边界怎么守
│   ├── 内核加第五关 ⇒ 与 ADR-0025 决定 1 冲突 ✗
│   ├── Field.allowed（新增，作用于字符串字段）⇒ 守 category ✓
│   └── INT-11 数值边界放在策略任务的 build 里 fail-closed ✓
└── 失败语义
    ├── 模型失败 ⇒ 回落确定性版（NI-1：整轮仍成功）✓
    └── 回落原因进 errors 还是独立字段 ⇒ 独立字段 ✓（errors 的语义是「本轮没做成」）
```

**已定决策**（详细理由见 ADR-0031）：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| 1 | 模型后端与结构化输出 | 云模型 + 既有三段式提取 + 独立契约校验；**不引** Outlines/Instructor | 三者的默认行为与本项目 `AR-15` 不同向；框架会进调用图 | `analysis/llm/deepseek.py`（新）· `analysis/aicap/model.py` |
| 2 | 三份契约（schema / 闭集 / 边界）放哪 | 新模块 `analysis/llm/schemas.py`，领域模块再导出 | `aicap` 只能依赖 `llm` 与自己（`MD-4`）；契约只定义一次（`MD-5`） | `analysis/llm/schemas.py`（新） |
| 3 | 三份提示词放哪 | 迁入 `analysis/aicap/resources/prompts/` 并按三段式改写 + 登记 | 它们是**任务**的提示词，任务必须在注册表里（`AR-33`）；合并成一套 | 提示词三个文件 + `guardrail/prompts.py` |
| 4 | 越界类别怎么处理 | `Field.allowed` 守闭集 ⇒ **拒绝**（删掉静默回落） | 回落是默认值，`AR-15` 禁止 | `analysis/llm/contract.py` · `analysis/intent/recognize.py` |
| 5 | 结论里的生成器标识 | `generator` ∈ {`model-v1`, `rules-v1`}（不写模型名） | 适配器**只暴露 `complete`**（`AR-32`），拿模型名要再开一个公开成员 | `worker.py` · `spec/events.md` |
| 6 | 锁文件与 pygen 漂移 | 新增 `check-pydeps` 门禁；`ruff` 加 `force-exclude` | 前者让「环境≠锁」可见；后者是 pygen 永远有 diff 的根因 | `scripts/gate/check-pydeps.sh`（新）· `analysis/pyproject.toml` |

**仍未定**（进 ADR-0031 未解决）：① 模型 vs 规则的质量对照基准；② `chain` 列表字段的闭集守卫；
③ 原始 URI 查询串 / UA 的机器识别；④ 模型成本与配额上限。

**接缝与接口**：

- `TaskSpec.payload` 是攻击者可控内容的**唯一**入口（`AR-31`）：三步分别放
  `observations`（intent / chain）与 `intent`+`stages`+`broken_signals`+`available_decoys`（strategy）；
- `AnalysisClient.complete(prompt, *, session_id, timeout) -> str` —— 适配器的**唯一**公开方法；
- `worker.run_once(..., client=None)`：`None` = 不启用模型（与今天逐字一致）。

**数据流（含失败路径）**：

```text
决策事件 → 去重(AR-14) → ① intent
                         ├─ 模型：aicap.generate(kind=intent) → 护栏 → accept ⇒ data
                         └─ 失败/被拒/未启用 ⇒ recognize() 规则版 + model_rejected 记原因
                       → ② chain
                         ├─ 模型：aicap.generate(kind=chain) → 护栏 → 形状校验 → assert_exists(AR-12)
                         └─ 任一不过 ⇒ reconstruct() 确定性版 + 记原因
                       → ③ strategy
                         ├─ 模型：aicap.generate(kind=strategy) → 护栏 → build 里的 INT-11 边界
                         └─ 越界/失败 ⇒ generate() 规则版 + 记原因
                       → 结论事件（generator + model_rejected）→ 遥测
```

---

## 3. 追溯矩阵

| 需求 / 规则 | 文档章节 | 代码 | 测试 | 验证命令 |
| --- | --- | --- | --- | --- |
| `AR-33`（唯一出口 + 登记表） | [`spec/ai-contract.md`](../spec/ai-contract.md) §1.3 / §7 · [`modules/ai-capability.md`](../modules/ai-capability.md) §1/§4 | `analysis/aicap/tasks/{intent,chain,strategy}.py` · `tasks/_registry.py` | `test_aicap_l4_tasks.py::test_registry_lists_all_four_kinds` · `::test_startup_assert_*` | `make pytest` · `make archcheck`（`MD-4`） |
| `AR-24`（提示词资源化 + 启动期校验） | `spec/ai-contract.md` §1.5 | `analysis/aicap/guardrail/prompts.py`（`REQUIRED_PROMPTS` 四个）· `analysis/aicap/resources/prompts/*.md` | `test_aicap_l4_tasks.py::test_startup_assert_fails_when_a_new_template_is_missing` · `::test_every_kind_points_at_its_own_template` | `make pytest` |
| `AR-15`（越界即拒绝，不回落） | `spec/ai-contract.md` §7 · `modules/intent.md` §6 | `analysis/llm/contract.py`（`Field.allowed`）· `analysis/intent/recognize.py` | `test_llm_contract.py::test_ar15_allowed_*` · `test_aicap_l4_tasks.py::test_intent_out_of_set_category_is_rejected_not_defaulted` | `make pytest` |
| `AR-22`（黑名单三类在受检字段上生效） | `spec/ai-contract.md` §1.6 | `analysis/llm/blacklist.py`（既有）· 三个档案的 `checked_fields=("rationale",)` | `test_aicap_l4_tasks.py::test_guardrail_rejects_leaked_private_address` | `make pytest` |
| `AR-31`（攻击者内容只经数据区） | `spec/ai-contract.md` §1.5 | `analysis/llm/untrusted.py`（既有）· `worker._observations_payload` | `test_aicap_guardrail.py::test_run_task_is_task_agnostic` | `make pytest` |
| `AR-32`（无执行面 / 不持执行能力） | `spec/ai-contract.md` §1.6 · `modules/llm-components.md` §3 | `analysis/llm/deepseek.py`（公开成员只有 `complete`） | `test_llm_deepseek.py::test_ar32_adapter_has_no_execution_surface` | `make pytest` · `make archcheck` |
| `AR-12`（引用证据必须存在） | `spec/events.md` §3 · `modules/chain.md` §6 | `worker._chain_from_model` + `analysis/chain/evidence.assert_exists` | `test_aicap_l4_tasks.py::test_worker_voids_model_chain_when_evidence_does_not_exist` | `make pytest` |
| `AR-16`（统一信封） | `spec/ai-contract.md` §1.2 | `analysis/llm/envelope.py`（既有） | `test_llm_contract.py::test_ar16_*` | `make pytest` |
| `INT-11`（灰度上限 / 阈值下界） | `spec/ai-contract.md` §7 · `modules/strategy.md` §6 | `analysis/aicap/tasks/strategy.py::build`（`StrategyBoundError`）· `analysis/llm/schemas.py`（常量） | `test_aicap_l4_tasks.py::test_strategy_bounds_are_enforced_on_the_model_path` · `::test_worker_falls_back_on_bound_violation_from_the_model` | `make pytest` |
| `NI-1`（不影响业务 / 失败不炸） | `modules/intent.md` §6 · `modules/ai-capability.md` §6 | `worker._model_candidate` 收住所有异常 → 回落 | `test_aicap_l4_tasks.py::test_worker_falls_back_on_every_step_but_still_concludes` | `make pytest` · `make analysis-llm` |
| `ST-20` / `ST-21`（密钥只经环境变量、禁入库） | [`ops/runbook.md`](../ops/runbook.md) §6.1 · ADR-0026 决定 4 | `analysis/aicap/model.py::from_environment` · `analysis/llm/deepseek.py`（不回显 key） | `test_llm_deepseek.py::test_key_never_leaks_into_errors_or_logs` | `make pytest` |
| `TB-16`（依赖须经审计） | [`spec/dependencies.md`](../spec/dependencies.md) §2 | `scripts/gate/check-pydeps.sh`（新） | `test_gate_pydeps.py`（**7 例**，含 fixture 不一致必红） | `make check-pydeps` |
| `AR-33` 实现缺口（`AR-33` 的「任何生成」） | `kb/ai-capabilities.md` §10 发现 1/5 | 提示词迁移 + 三 kind 登记 | 同上两条 | `make pytest` |
| 生成期模板漂移 | [`kb/known-issues.md`](../kb/known-issues.md) `K-28` | `analysis/pyproject.toml`（`force-exclude`） | —— （判据是命令：连跑两次零 diff） | `make pygen` ×2 + `git status` |

---

## 4. 代码实现

**新增**

| 文件 | 为什么 |
| --- | --- |
| `analysis/llm/deepseek.py` | 云模型适配器：唯一公开方法 `complete`，只用标准库，失败即 `Unavailable`（不重试） |
| `analysis/llm/schemas.py` | 三任务的契约**唯一**定义处（`CATEGORIES` · 三份 schema · `MAX_GRAY_PCT` · `THRESHOLD_FLOOR` · 三个 kind 名） |
| `analysis/aicap/tasks/intent.py` · `chain.py` · `strategy.py` | 三个 `kind` 的登记项（schema / 护栏档案 / produce / build） |
| `analysis/aicap/tasks/_produce.py` | 三者共用的 `produce`（提示词 → 模型文本 → JSON 对象） |
| `analysis/aicap/resources/prompts/{intent,chain,strategy}.md` | 三段式提示词（**自 `analysis/llm/resources/prompts/` 迁入并改写**） |
| `scripts/gate/check-pydeps.sh` | 锁文件 ↔ venv 逐条比对（不一致即红，指向 `make pyenv`） |
| `analysis/tests/test_llm_deepseek.py` | 适配器 12 例（无网络） |
| `analysis/tests/test_aicap_l4_tasks.py` | 三 kind + worker 双路 15 例 |
| `analysis/tests/test_gate_pydeps.py` | 门禁脚本本身 7 例（含 fixture 不一致） |
| `docs/background/research/l4-oss-reuse.md` | 复用审计材料（5 候选 × 许可逐字 × 活跃度） |
| `docs/background/decisions/0031-analysis-reuse-and-model-backend.md` | 收口 ADR-0024 的四项未解决 |

**修改**（各自的「为什么必须改」）

| 文件 | 改什么 |
| --- | --- |
| `analysis/llm/contract.py` | 新增 `Field.allowed`（守闭集；默认 `None` ⇒ 既有 schema 行为不变） |
| `analysis/aicap/model.py` | `resolve()` 增加「按环境变量取真实客户端」一路；`from_environment()` 新公开函数 |
| `analysis/aicap/tasks/_registry.py` | 登记三个新 kind（四条） |
| `analysis/aicap/guardrail/prompts.py` | `REQUIRED_PROMPTS` 从 1 个变 4 个 |
| `analysis/aicap/ports.py` | 新增 `WireArtifact`（三个消费方共用的最小产物，避免三份同样的小类） |
| `analysis/intent/recognize.py` | 类别与 schema 改为从 `analysis/llm/schemas.py` 再导出；**删掉静默回落** |
| `analysis/strategy/generate.py` | schema / 边界常量改为再导出；输出补 `rationale`（两条路同形）；灰度解析失败改为**拒绝**而非抛裸异常 |
| `analysis/worker.py` | 三步「模型优先、失败回落」；`AnalysisRun` 增 `model_rejected` / 各步 `*_generator` / `rejected`；`--llm` 开关；结论增两个审计字段 |
| `analysis/chain/reconstruct.py` | `STAGE_ORDER` 改为从 `analysis/llm/schemas.py` 取（原经 `intent.recognize` 中转） |
| `analysis/pyproject.toml` | `ruff` 加 `force-exclude = true`（pygen 零 diff 的根因修复） |
| `Makefile` | 新增 `check-pydeps` / `analysis-llm`；`lint` 与 `pyenv` 接入检查 |
| `scripts/dev/ai-model-probe.py` | 环境变量统一为 `SHEN_AI_*` |
| `analysis/proto/**` | 接受一次生成器原始形态（此后零 diff） |
| 文档（9 份） | `spec/ai-contract.md` §1.3/§1.5/§7 · `spec/events.md` §3 · `kb/ai-capabilities.md` · `kb/known-issues.md` `K-28` · `modules/{ai-capability,llm-components,intent,chain,strategy}.md` · `modules/_map.md` · `progress.md` · `ops/runbook.md` §4.3/§6.1 · `background/{research,decisions}/README.md` |

**删除 / 迁移**（依据 skill `audit` 门槛 ④：记录了删什么、为什么、怎么恢复）

| 对象 | 为什么它失去了对象 | 怎么恢复 |
| --- | --- | --- |
| `analysis/llm/resources/prompts/{intent,chain,strategy}.md` | **迁移**（不是删除）：它们是任务的提示词，迁到 `analysis/aicap/resources/prompts/` 并按三段式改写 —— 留在 `analysis/llm/` 下等于保留第二套提示词系统 | `git show <本提交前一个>:analysis/llm/resources/prompts/intent.md`；反向迁移同理（但会使 `AR-33` 的登记表无法声明它们） |

**关键类型与函数**（导出契约）：`DeepSeekClient.complete` · `model.from_environment` / `resolve` ·
`llm.schemas` 的 `INTENT_SCHEMA` / `CHAIN_SCHEMA` / `STRATEGY_SCHEMA` / `CATEGORIES` /
`MAX_GRAY_PCT` / `THRESHOLD_FLOOR` / `*_KIND` · `ports.WireArtifact` ·
`strategy.StrategyBoundError` · `worker.run_once(client=…)`。

---

## 5. 测试与场景

| # | 场景 | 输入 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 1 | 适配器正常应答 | 2xx + 合法 JSON 体 | 返回 `content` | ✅ | `test_llm_deepseek.py::test_happy_path_returns_content` |
| 2 | 适配器非 2xx / 不可解析 / 形状不合 / 空 content | 429 · HTML · `{}` · `content:""` | 一律 `Unavailable`，**不重试** | ✅ | 同文件 4 例 |
| 3 | 传输超时 | sender 抛 `TimeoutError` | 归一为 `Unavailable`（不裸抛） | ✅ | `::test_transport_error_is_normalized_to_unavailable` |
| 4 | 密钥泄露 | 失败调用 + DEBUG 日志 | key 不在异常 / repr / 日志里 | ✅ | `::test_key_never_leaks_into_errors_or_logs` |
| 5 | 配置写错 | `http://` 端点 / 带路径端点 / 超时 `0` / 超时非数字 | 构造即 `Unavailable`（不换默认值） | ✅ | `::test_config_errors_fail_loudly` |
| 6 | 接缝三分支 | 无环境变量 / 齐备 / 显式传入 | `UnconfiguredClient` / `DeepSeekClient` / 显式者优先；两者都过无执行面 | ✅ | `::test_resolve_*` 3 例 |
| 7 | 越界类别 | 模型给 `category="第六类"` | **拒绝**，`data=={}`，原因含「闭集」 | ✅ | `test_aicap_l4_tasks.py::test_intent_out_of_set_category_is_rejected_not_defaulted` |
| 8 | 泄露类 | 理由含 `10.1.2.3` | 拒绝（`AR-22`） | ✅ | `::test_guardrail_rejects_leaked_private_address` |
| 9 | 风格不一致 | 理由里无任何画像术语 | 拒绝（`AR-33`） | ✅ | `::test_guardrail_rejects_off_profile_rationale` |
| 10 | 三条禁令在模板里 | 读三个模板 | 都含「回显原始 URI 查询串」禁令 | ✅ | `::test_all_three_templates_ban_echoing_raw_attacker_fields` |
| 11 | `INT-11` 对模型路径生效 | 模型给 `gray_pct=100` / `block=0.1` | 抛 `StrategyBoundError`（不夹紧）；合法值通过 | ✅ | `::test_strategy_bounds_are_enforced_on_the_model_path` |
| 12 | 缺新模板即启动失败 | 只放 `content.md` 的目录 | `PromptError` | ✅ | `::test_startup_assert_fails_when_a_new_template_is_missing` |
| 13 | **回归**：不启用模型 | `client=None` | 三步都是 `rules-v1`，`model_rejected=={}`，`rejected==0` | ✅ | `::test_worker_without_client_is_unchanged_and_not_a_fallback` |
| 14 | 模型全成功 | 替身按 kind 答 | 三步 `model-v1`，`errors==[]` | ✅ | `::test_worker_uses_model_when_the_model_path_works` |
| 15 | 模型全失败 | 替身抛 `Unavailable` | 回落 3 步、**仍然产出 2 条结论**、`errors==[]`（降级不是失败） | ✅ | `::test_worker_falls_back_on_every_step_but_still_concludes` |
| 16 | `AR-12` 在模型路径 | 模型链引用不存在的证据 | 链作废 + 回落确定性版；**不拖垮** intent/strategy | ✅ | `::test_worker_voids_model_chain_when_evidence_does_not_exist` |
| 17 | 门禁脚本：不一致必红 | fixture：锁 `protobuf==7.36.2` / 环境 `7.35.1` | 非零退出 + 两个版本都打印 + 指向 `make pyenv` | ✅ | `test_gate_pydeps.py::test_version_mismatch_fails` |
| 18 | 门禁脚本：其余分支 | 一致 / 未安装 / 非严格定版 / 名称规范化 / 缺 venv | 各如预期 | ✅ | 同文件 5 例 |
| 19 | `make analysis-llm`（无 key，真实核心） | 起核心 + 造 3 条判定事件 | 回落 3 步 + 每步原因；结论 2 条；**退出码 0** | ✅ | §6 命令 ③ |
| 20 | `make pygen` 幂等 | 连跑两次 | 两份文件 `md5` 相同 | ✅ | §6 命令 ④ |
| 21 | **`AR-12` 对意图模型路径生效** | 模型给 `evidence_ids:["根本没这条证据"]` | 该步作废 + 回落规则版（原因含 `AR-12`）；**不拖垮**其他步 | ✅ | `test_aicap_l4_tasks.py::test_worker_voids_model_intent_when_evidence_does_not_exist` |
| 22 | 同上的**正对照** | 模型引用真实存在的证据 | 放行（`model-v1`）—— 证明上一例不是「什么都拦」 | ✅ | 同文件 `::test_worker_accepts_model_intent_when_evidence_exists` |
| 23 | 适配器公开成员面 | `dir(client)` 去下划线 | 恰好 `{complete}`（不只看子串黑名单） | ✅ | `test_llm_deepseek.py::test_ar32_adapter_has_no_execution_surface` |

**没有覆盖的**：

- **真实模型的端到端**（真的调 `api.deepseek.com`）：本轮无密钥，未跑。适配器的传输层只经替身验证
  （`_https_send` 未被单测覆盖 —— 它与实机探针 `scripts/dev/ai-model-probe.py` 用同一套写法，而那份**有**实机记录）；
- **模型 vs 规则的质量对照**：未做（ADR-0031 未解决 1）；
- **`analysis/proto/` 之外的容器/镜像路径**：本轮不动容器；
- **`--llm` 在常驻模式下的长期行为**（配额、成本）：未验。

---

## 6. 验证证据

**① `make gate`（关键行，**原样摘录**：行序与真实输出一致）**

```console
$ make gate
✓ Python 格式（ruff format）
✓ Python 依赖版本一致（6 条 pin：analysis/requirements.txt + requirements-dev.txt）
All checks passed!
✓ Python 静态检查（ruff）
✓ 忽略清单未误伤任何已入库文件
118 passed in 2.30s
✓ L4 单测（pytest）
门禁通过。
```

> 行序不是随便排的：`lint` 的依赖顺序是 `pyfmt-check → check-pydeps → pylint`（`Makefile`），
> 所以「依赖版本一致」必然打在「静态检查（ruff）」**之前**。
> 中间还有 `archcheck` / `trace` / `leakcheck` / `licensecheck` 的输出与一行 trace 提示，此处略。

**② `make dev`（6/6，L4 段与上一轮一致）**

```console
$ make dev
✅ 通过：配置校验 · 非法配置被拒 · 启动 · 判定面 · 冒烟 · 规则回放 · L4 近线分析

== 6/6 L4 近线分析（读遥测事件 → 意图/链/策略 → 结论事件） ==
[L4] 取事件 3 条 · 去重后 3 条（抑制 0）· 结论 2 条（新 2 / 重复 0）
  · intent: accepted=True data={"category": "reconnaissance", "confidence": 0.333, …}
  · strategy: accepted=False data={}
```

**③ `make analysis-llm`（未设 `SHEN_AI_KEY`，起真实核心）—— 判据 4**

```console
$ SHEN_CONFIG=deploy/config/config.example.yaml SHEN_LISTEN=127.0.0.1:19999 go run ./common/core/cmd/core &
$ go run ./scripts/devcheck -addr 127.0.0.1:19999
冒烟通过。
$ env -u SHEN_AI_KEY analysis/.venv/bin/python -m analysis.worker --core 127.0.0.1:19999 --once --llm
[L4] 取事件 3 条 · 去重后 3 条（抑制 0）· 结论 2 条（新 2 / 重复 0） · 模型回落 3 步
  · 模型回落 chain：Unavailable: 分析用模型未配置（需在部署侧注入 AnalysisClient）；禁止用固定模板冒充模型输出（AR-15）
  · 模型回落 intent：Unavailable: 分析用模型未配置（…）
  · 模型回落 strategy：Unavailable: 分析用模型未配置（…）
  · intent: accepted=True data={"category": "reconnaissance", …}
  · strategy: accepted=False data={}
worker exit=0
```

**④ `make pygen` 幂等（判据 3）**

```console
$ md5 -q analysis/proto/telemetry/v1/telemetry_pb2*.py > /tmp/a.txt
$ make pygen >/dev/null && md5 -q analysis/proto/telemetry/v1/telemetry_pb2*.py > /tmp/b.txt
$ diff /tmp/a.txt /tmp/b.txt && echo "✓ make pygen 幂等（零 diff）"
✓ make pygen 幂等（零 diff）
```

**⑤ `make check-pydeps`**

```console
$ make check-pydeps
✓ Python 依赖版本一致（6 条 pin：analysis/requirements.txt + requirements-dev.txt）
```

**⑥ `make trace`**

```console
$ make trace
提示 1 条（尚未升格为规则的约定，不阻断门禁）：ℹ TC-1 modules/deception/mirror/（缺 iface.go）
已登记豁免 1 条：· D-3 docs/design/structure.md:133
追溯检查通过。
```

**未跑 / 需环境**：`make ai-check`（需 Docker 全套；本轮未动通路，未跑 —— 见「没有覆盖的」）。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | **§2 里「新增受检项」的原始意图未能照字面实现**：计划想让三个新档案「受检「不得回显原始 URI 查询串 / UA / 来源 IP」」，但内核的四个检查点是固定的（ADR-0025 决定 1），加第五关会破坏「内核任务无关」。实际落实＝① 三个模板写死该禁令（有单测钉住文本）+ ② `rationale` 作为受检字段走既有黑名单/长度/风格。**原始 URI 查询串与 UA 的机器识别仍缺** | 泄露类的机器覆盖度低于计划字面 | [ADR-0031](../background/decisions/0031-analysis-reuse-and-model-backend.md) 未解决 3；触发条件见其失效条件 2 |
| 2 | `chain` 的 `broken_decoy_signals[]` 与 `stages[].name` 闭集**无机器守卫** | 模型可给越界阶段名/信号种类 | ADR-0031 未解决 2 |
| 3 | `generator` 写 `model-v1` 而非**模型名**（计划表里写的是「模型名或 `rules-v1`」） | 知道「是模型产的」但不知「哪个模型」 | 要写模型名就得给客户端再加一个公开成员（与 `AR-32` 的单成员面冲突）—— 记在这里；若将来必须，另开评审 |
| 4 | 未接 `kind=content` 的模型路径（阶段 B） | 内容多样性仍靠模板 | ADR-0023 未解决 1 的阶段 B 部分 |
| 5 | `twophase` 仍无生产调用方（旧发现 3） | 超时收尾是骨架 | 本轮未动；`deadline_s` 已传进适配器 |
| 6 | 未跑 `make ai-check`（需 Docker） | 通路回归未在本轮验证 | 下轮或 CI 跑；本轮动的是 L4，通路未改 |
| 7 | **`make pygen` 的漂移根因是 `ruff` 缺 `force-exclude`**（而不是「某次手工格式化」） | 已修且写进 `K-28`；但需要意识到：任何「排除规则」都要问它对**显式传路径**的调用是否有效 | 已在 `K-28` 记；无遗留 |
| 8 | `analysis/proto/**` 接受了一次生成器原始形态（含 `import warnings` 这类「看起来没用」的行） | 生成物风格与仓库其他 Python 不一致（这是**刻意**的：生成物不守本仓库风格，proto 目录本来就被 ruff 排除） | 无遗留；`K-28` 有记 |

### 7.1 审视记录（L 档，技能 `audit`）

对着本轮 diff 核文档与代码，逐条给证据与动作：

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `kb/ai-capabilities.md` 说「今天没有任何一条生产链路在调模型」，与新的 `--llm` 路径不符 | 文档 vs 实现漂移 | 该文件 §0 首段 | 改为「默认路径无模型；L4 三步已有模型路径（`--llm`，默认关）」并补 `generator`/`model_rejected` | ✅ 已改 |
| 2 | 三份 L4 提示词在 `analysis/llm/resources/prompts/` 下，而 `aicap` 的渲染器只读自己的目录 ⇒ 它们**从来没被生产渲染过**（旧发现 1 的另一半） | 漂移（两套系统） | `grep -rn "prompts.render" analysis/`（排除 tests）→ 只有 `analysis/aicap/service.py` | 迁入 `analysis/aicap/resources/prompts/` 并改写为三段式 + 登记；`analysis/llm/` 下只剩 `finalize.md` | ✅ 已合并为一套 |
| 3 | `analysis/intent/recognize.py` 的 `category if … else "reconnaissance"` 是**默认值**（`AR-15` 明令禁止） | 规则违反（当时不可达） | 该行代码 + `AR-15` 原文 | 改为 `Field.allowed` 守闭集，越界即 `ContractError`；单测钉住 | ✅ 已删 |
| 4 | `test_llm_discipline.py::test_ar24_prompt_resources_validated` 断言 `analysis/llm/` 下有四个模板 —— 迁移后它守的对象变了 | 过期断言（会掩盖漂移） | 该用例 | 改为只要求 `finalize`（并把「为什么只有它」写进 docstring） | ✅ 已改 |
| 5 | `test_worker.py` 的「结论不得夹带执行类字段」允许集不含新字段 ⇒ 会红 | 过期断言 | `test_ar32_worker_only_reads_and_reports_structured_conclusions` | 加 `generator` / `model_rejected` 两键（它们不是执行字段） | ✅ 已改 |
| 6 | `kb/known-issues.md` 没有 pygen 漂移这一条（它是本轮**真撞到**的） | 缺记录 | `make pygen` 两次都有 diff | 新增 `K-28`（症状 / 原因 / 判据 / 同类陷阱） | ✅ 已增 |
| 7 | `docs/modules/_map.md` 把「话术与提示词」整块算在 `llm-components` 名下 —— 迁移后各 kind 的提示词归 `ai-capability` | 文档口径过期 | 该行 | 拆成「各 kind 的（`aicap`）/ 收尾的（`llm`）」并补 `SHEN_AI_*` 行 | ✅ 已改 |
| 8 | `docs/ops/runbook.md` §4.3 写 `make pytest`「37 例」—— 实际早已不是 | 过期数字 | 实跑 **118 例**（`make gate`） | 改为 118 例并补 `check-pydeps` / pygen 的注意 | ✅ 已改 |
| 9 | `progress.md` 四个 L4 模块都写「框架 + 确定性部分」与「24 项测试」 | 过期状态标记（`TC-2` 类） | 实跑 / 代码 | 改为「双路」并按实际测试总数写 | ✅ 已改 |
| 10 | 检查**有没有留下僵尸内容**：`analysis/llm/resources/prompts/` 迁走后目录里仍能加载（只剩 `finalize.md`）；`analysis/aicap/tasks/__init__.py` 的清单指向新文件 | 僵尸/悬空 | `ls` + `pytest`（`test_ar24_*` 仍绿） | 无需动作（目录非空、`assert_startup()` 仍过）；`analysis/aicap/tasks/__init__.py` 已补三个新文件说明 | ✅ 核过 |
| 11 | 检查**规则 ID 引用是否真实存在**：本轮文档新增引用 `AR-12`/`AR-15`/`AR-16`/`AR-22`/`AR-24`/`AR-30`/`AR-31`/`AR-32`/`AR-33`/`INT-11`/`NI-1`/`ST-20`/`ST-21`/`TB-16`/`MD-4`/`MD-5` | 悬空引用（`D-3`） | `make trace` | 通过（零错误） | ✅ 零错误 |
| 12 | 检查**是否有「代码做了文档没写」**：`WireArtifact` · `_produce.py` · `Field.allowed` · `worker.rejected` 属性 | 漂移（代码→文档） | 逐个核 | 分别写进 `spec/ai-contract.md` §7 · `modules/ai-capability.md` §2 · `modules/llm-components.md` §1 · `spec/events.md` §3 | ✅ 已写 |

> 历史记录类文件（`docs/log.md` · 旧 `docs/plans/*` · `docs/background/notes|research` 的既有材料）
> **不在审视范围内**：它们是当时的快照，写下过时内容是正确的（`dev-loop-project` §7.1）。
> 唯一例外是本轮**新增**的材料（`l4-oss-reuse.md` · ADR-0031）—— 它们按最新状态写。

### 7.2 独立评审（L 档）

评审者：冷上下文子代理（`reviewer`，只读产物与源码，不看作者推理）。
**该子会话没有 shell 工具**，所以它只做了静态核对，没跑任何命令 —— 这也意味着它**看不到 diff**，
「有没有越界改动」一项它自认未能判定（那项由人的 `git diff` 与门禁负责）。
它自己声明的未能验证项：越界改动 · 各类命令实跑 · `analysis/proto/**` 的字节级再生 · 调研材料的外部事实。

**它的结论：有异议 1 条 P1 + 6 条 P2。其中 P1 是本轮 diff 才可达的真缺陷。**

### 7.3 评审意见与处置

| # | 级别 | 评审意见 | 处置 |
| --- | --- | --- | --- |
| 1 | **P1** | 模型产出的 `intent` 结论里 `data.evidence_ids` **没过 `AR-12`**（全文件只有 chain 步调 `assert_exists`）⇒ 模型可携带**编造的证据 ID**，而结论里另有一份永远为真的顶层 `evidence_ids`，读的人无法分辨 | ✅ **已修**：`worker._assert_evidence_ids()` + 意图步同样校验（失败即回落规则版、原因进 `model_rejected`）；**新增回归用例并实证它真能拦住**（把校验临时关掉 ⇒ 用例失败 `rules-v1` vs `model-v1`）；补正对照用例（引用真实证据时放行） |
| 2 | P2 | `aicap/tasks/intent.py` 的 docstring 写成 `AnalysisRun.model_fallbacks`，实际字段是 `model_rejected` | ✅ **已修**（全仓库仅此一处） |
| 3 | P2 | 变更包 §3 写 `test_gate_pydeps.py`（6 例），实为 7 例 | ✅ **已修**为 7 |
| 4 | P2 | §6 引文是手工重排、不是原样粘贴（顺序与真实输出相反；trace 块漏行）⇒ 不能当「跑过」的原始证据 | ✅ **已修**：§6 ① 改为按真实行序（`pyfmt-check → check-pydeps → pylint`）的原样摘录并标注「原样摘录」；同时**发现并修正一处更严重的**：我引的测试数（123/116）与实际不符，实为 118（详见 §7.1 第 8 条与 §6） |
| 5 | P2 | `progress.md` §1a 仍把 L4 写成「复用 AI 框架 PyTorch / vLLM / Transformers / **NetworkX**」，与本轮 ADR-0031 决定 4 矛盾 | ✅ **已修**为「调用云模型 + 自写标准库适配器，**不引** NetworkX 等」并指向 ADR-0031 |
| 6 | P2 | 「公开成员只有 `complete`」只有人工保证：`assert_no_execution_surface` 只查**子串黑名单**，拦不住新加的 `fetch()` / `get()` | ✅ **已修（在适配器侧）**：`test_llm_deepseek.py` 增加**白名单**断言（公开成员集合 == `{complete}`）。内核的黑名单实现本身**未改**（改它会扩大本轮范围）；该结构性建议归入本节第 7 条遗留 |
| 7 | P2 | 两条路 `data` 并不完全同形（规则路多一个可选 `truncations`），而 ADR-0031 说的是「同一形状」 | ✅ **已修**：ADR-0031 改为「同一 **schema** 形状」并写明 `truncations` 这层差异；[`spec/events.md`](events.md) §3 的 `data` 行补「可带可选审计键，消费者不得假定键集相等」 |

**评审结论**：**通过（P1 已修且带可证伪的回归用例；6 条 P2 全部处置）**。
评审也确认了几件事（防止「只报不利消息」）：登记表四条 kind 齐 · 提示词迁移到位（`llm/` 只剩 `finalize.md`）·
闭集真拒绝 · `INT-11` 对模型路生效 · `generator`/`model_rejected` 与 `events.md` 逐字一致 ·
模型调用无旁路（`archcheck` 前缀表含出口与接缝）· `requirements*.txt` 仍 6 条 pin（零新增依赖）·
`docs/design/` 未被本轮概念污染（无 `model-v1` / `SHEN_AI` / `deepseek` / `generator`）。

> 遗留（由评审第 6 条引出）：`assert_no_execution_surface` 的**子串黑名单**可以换成白名单式结构检查
> （`public ⊆ {complete}`），但那会改到 `llm/client.py` 的公共纪律层与所有调用方 —— 属另一轮，记在 §7。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-21 | 首版：L4 三任务接模型（4 个 kind 的注册表 + 唯一出口 + 护栏 + 回落）· `Field.allowed` 闭集 · 云模型适配器 · 三个提示词迁移并回归三段式 · worker 双路 · 锁文件门禁 · pygen 零 diff · 复用审计材料与 ADR-0031 | 用户 2026-09-21 确认本轮范围（含「L4 三个都接」）· [ADR-0031](../background/decisions/0031-analysis-reuse-and-model-backend.md) |
