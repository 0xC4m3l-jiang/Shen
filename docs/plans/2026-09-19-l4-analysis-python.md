# 变更包：L4 分析层（Python）—— `llm-components` · `intent` · `chain` · `strategy`

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 按设计完成最后一块模块组：L4 分析决策层（Python），并把 Python 门禁接入 `make gate` |
| 日期 | 2026-09-19 |
| 状态 | 已验证（24 例 pytest 全过 · `ruff check` / `ruff format --check` 全绿 · `make gate` 通过） |
| 改动分级 | **L**（新增一层实现与一门语言的工具链，改门禁） |
| 涉及模块 | `llm-components`（§1.1 第 20 行）· `intent`（17）· `chain`（18）· `strategy`（19） |
| 决策数 | 已答 1 项（Python 工具链 → [ADR-0021](../background/decisions/0021-l4-python-toolchain.md)）/ 待定 3 项（见 §7） |
| 关联 | [ADR-0021](../background/decisions/0021-l4-python-toolchain.md) · `AR-12` · `AR-14` · `AR-15`…`AR-27` · `AR-31`/`AR-32` · `ADR-0016` · `TB-2`/`TB-14`/`TB-15`/`TB-20` |

---

## 1. 需求与验收

**要解决什么**：目标要求「按设计完成所有模块（除细节蜜罐）」。L4 是最后一块，设计指定 **Python**
（`language.md` §1 · `TB-2` · `TB-20`），而 `TB-15` 要求 Python 必须过 `ruff` ⇒ 引入 L4 就必须引入 Python 门禁。

**验收判据**：

1. `analysis/llm/` 覆盖 `AR-15`…`AR-24`：契约失败**抛异常**（不填默认值）· 统一信封 `{accepted,data}` + 拒绝原因 ·
   三段式 JSON 提取（含扫描上限）· 列表硬截断并**记录截断数** · 双阶段收尾（复用同一会话、收尾契约只允许事实类字段）·
   两阶段失败**不写中间数据 + 释放租约** · 黑名单三类 · 分用途长度上限 · 提示词资源化并在启动期校验。
2. `analysis/llm/untrusted.py` 落实 `AR-31`：攻击者可控内容以**结构化数据区**传入并显式标注不可信，原始观测**原样保留**。
3. `analysis/llm/client.py` 落实 `AR-32`：客户端**只**能把提示词变成文本；`assert_no_execution_surface` 拦截任何执行类成员。
4. `intent` / `chain` / `strategy` 产出**结构化结论**；`chain` 在写入前校验证据引用（`AR-12`）；`strategy` 只出数据（灰度 ≤ 20%、阈值有下界）。
5. `AR-14` 去重：同一态势 N 条事件只触发 1 次 L4。
6. `make gate` 含 Python 三项（`pyfmt-check` / `pylint` / `pytest`），且**缺环境时报错而非跳过**。

**不做什么**：不接真实 LLM（`UnconfiguredClient` 显式失败）· 不把 L4 接进运行时触发链路（见 §7）· 不做蜜罐内容。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | L4 用什么语言 | **Python**（[ADR-0021](../background/decisions/0021-l4-python-toolchain.md)） | `TB-2` / `TB-20`；AI 生态在 Python |
| ② | 门禁怎么放 | 仓库内 `.venv` + 锁版本 + `make gate` 三项 | `TB-15`；不污染系统 Python；升级可审计 |
| ③ | 没有模型后端时怎么办 | `UnconfiguredClient` **显式抛错** | `AR-15`：禁止默认值、禁止静默降级 |
| ④ | 结论怎么防注入 | 攻击者内容进**数据区**并标注不可信；LLM 无执行面 | `AR-31` / `AR-32`（[ADR-0015](../background/decisions/0015-indirect-prompt-injection.md)） |

---

## 3. 追溯矩阵

| 规则 ID | 文档 | 代码 | 测试 |
| --- | --- | --- | --- |
| `AR-15` | [`../modules/llm-components.md`](../modules/llm-components.md) | `analysis/llm/contract.py` | `test_llm_contract.py::test_ar15_*`（坏输出必失败、契约外字段必失败） |
| `AR-16` | 同上 | `analysis/llm/envelope.py` | `test_ar16_envelope_shape_and_reason` |
| `AR-17` | 同上 | `analysis/llm/extract.py` | `test_ar17_three_stage_extraction` · `test_ar17_scan_cap_bounds_work_on_long_output` |
| `AR-18` | 同上 | `analysis/llm/limits.py` | `test_ar18_list_cap_records_truncation` |
| `AR-19` | 同上 | `analysis/llm/twophase.py` | `test_ar19_and_ar20_two_phase_reuses_session_with_facts_only_contract` · `test_ar19_phase1_failure_does_not_rerun_whole_task` |
| `AR-20` | 同上 | 同上（`FINALIZE_SCHEMA`） | `test_ar20_finalize_rejects_non_fact_field` · `test_ar20_finalize_requires_session_id_match` |
| `AR-21` | 同上 | 同上（`_abandon`） | `test_ar21_both_phase_failure_writes_nothing_and_releases_lease` |
| `AR-22` | 同上 | `analysis/llm/blacklist.py` + `resources/blacklist.yaml` | `test_ar22_blacklist_three_classes` |
| `AR-23` | 同上 | `analysis/llm/limits.py`（`PURPOSE_LIMITS`） | `test_ar23_per_purpose_limits_are_independent` |
| `AR-24` | 同上 | `analysis/llm/prompts.py` + `resources/prompts/*.md` | `test_ar24_prompt_resources_validated` · `test_ar24_missing_template_fails_loudly` |
| `AR-25` | 同上 | `analysis/llm/twophase.py`（`session_id` 一等字段 + 一致性校验） | `test_ar20_finalize_requires_session_id_match` |
| `AR-12` | [`../modules/chain.md`](../modules/chain.md) | `analysis/chain/evidence.py` | `test_ar12_evidence_refs_must_exist`（注入不存在的 ID 必失败） |
| `AR-14` | [`../modules/strategy.md`](../modules/strategy.md) | `analysis/dedupe.py` | `test_ar14_dedupe_keeps_llm_calls_bounded`（1000 条事件 → 1 次触发） |
| `AR-31` | [`../modules/llm-components.md`](../modules/llm-components.md) | `analysis/llm/untrusted.py` | `test_ar31_untrusted_content_is_structured_and_labeled`（注入样本不得改变结论、原文保留） |
| `AR-32` | 同上 | `analysis/llm/client.py` | `test_ar32_analysis_client_has_no_execution_surface` |
| `TB-14` | [`../design/language.md`](../design/language.md) | `pyproject.toml`（`ruff` 选 `E722`） | `make pylint` |
| `TB-15` | 同上 | `Makefile`（`pyfmt-check` / `pylint` / `pytest`） | `make gate` |
| `ADR-0016` | [`../background/decisions/0016-decoy-polymorphism.md`](../background/decisions/0016-decoy-polymorphism.md) | `analysis/chain/broken.py` · `analysis/strategy/rotate.py` | `test_chain_stages_ordered_and_broken_signals_detected` · `test_decoy_rotation_decision` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `analysis/__init__.py` · `analysis/events.py` | 新增 | L4 的只读观测模型（`AR-31`：原样携带全部字段） |
| `analysis/llm/{envelope,extract,contract,limits,blacklist,twophase,prompts,untrusted,client}.py` | 新增 | 契约层：`AR-15`…`AR-24` · `AR-31` · `AR-32` |
| `analysis/llm/resources/blacklist.yaml` · `resources/prompts/{intent,chain,strategy,finalize}.md` | 新增 | 资源随版本分发（`AR-24`） |
| `analysis/intent/recognize.py` | 新增 | 意图识别：确定性规则 + 证据引用；无命中即拒绝 |
| `analysis/chain/{evidence,reconstruct,broken}.py` | 新增 | 攻击链 + 识破信号 + 证据校验（`AR-12`） |
| `analysis/strategy/{generate,rotate}.py` | 新增 | 策略数据（灰度 ≤20%、阈值下界）+ 轮换决策 |
| `analysis/dedupe.py` | 新增 | 态势去重（`AR-14`） |
| `analysis/tests/*` | 新增 | 24 例 pytest |
| `pyproject.toml` · `requirements.txt` · `requirements-dev.txt` · `pyrightconfig.json` | 新增 | 工具配置与**锁定**依赖 |
| `Makefile` | 改 | `pyenv` / `pyfmt-check` / `pylint` / `pytest`，并把三者接进 `lint` / `test` |
| `.gitignore` | 改 | 排除 `.venv/` 与 Python 缓存 |
| `docs/background/decisions/0021-l4-python-toolchain.md` 等 | 改/新增 | 决策登记 · 四份模块文档状态 · 结构与进度同步 |

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 |
| --- | --- | --- | --- |
| 1 | L4 单测 | 全过 | ✅ `24 passed` |
| 2 | 坏输出（类型错 / 缺字段 / 契约外字段） | **抛异常**，绝不填默认值 | ✅ `ContractError` |
| 3 | 超长输出（5000 噪声 + 300 个 `{`） | 扫描上限内失败、放宽后成功 | ✅ |
| 4 | 收尾阶段塞「成功」字段 | 拒绝 | ✅ `ContractError` |
| 5 | 两阶段都失败 | 不返回中间数据 + 释放租约 | ✅ `data == {}`、`released == 1` |
| 6 | 引用不存在的证据 ID | 整条链作废 | ✅ `MissingEvidence` |
| 7 | 同一态势 1000 条事件 | 只触发 1 次 L4 | ✅ `admitted == 1` |
| 8 | 提示词注入样本 | 内容原样保留、结论不因注入改变 | ✅ 数据区标注 + 原文保留 |
| 9 | 客户端暴露 `execute()` | 被拦下 | ✅ `AssertionError` |
| 10 | 门禁 | Python 三项进 `make gate` | ✅ `make gate` 通过 |

**没有覆盖的**：真实模型调用（无后端）· 运行时触发链路（未接通）· L4 在压力下的吞吐。

---

## 6. 验证证据

```console
$ .venv/bin/pytest analysis/tests
24 passed

$ make gate
门禁通过。
```

**关键指标**：L4 代码 **13 个模块文件** + **24 例测试** · 门禁新增 **3 项** · 覆盖规则 **17 条**（`AR-12`/`AR-14`/`AR-15`…`AR-25`/`AR-31`/`AR-32` + `TB-14`/`TB-15`）。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | **L4 未接运行时触发链路**（遥测事件 → `AR-14` 去重 → L4 → 结论落库） | L4 目前是**可调用库 + 单测**，线上不会自动跑 | 下一步接入（需定触发点与写入路径） |
| 2 | L4 未接真实 LLM | 只跑确定性规则 | 部署侧注入 `AnalysisClient` |
| 3 | `chain` 的链序、置信度为**启发式**初值 | 结论精度有限 | 有真实数据后按 `E3` 类实验校正 |
| 4 | `strategy` 输出未经 `policy` 端到端验证 | 灰度/阈值只是**数据** | 接策略面时验（`AR-13`） |
| 5 | `honeypot-shell` 与蜜罐协议栈内容 | **目标明确排除** | 推迟 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `assert_no_execution_surface` 只精确匹配 `exec`，漏掉 `execute()` | **缺陷**（守卫形同虚设） | 自带测试 `DID NOT RAISE AssertionError` | 改为子串匹配并保留该测试 | ✅ 测试转绿 |
| 2 | `UnconfiguredClient.complete` 为过 lint 改了参数名（`_session_id`），破坏协议一致性 | 自伤 | `TypeError: unexpected keyword argument 'session_id'` | 恢复真实参数名，用 `del` 消费入参 | ✅ |
| 3 | `twophase` 只捕获自定义 `PhaseFailure`，会话抛原生 `TimeoutError` 时逃逸 | **缺陷**（超时路径没兜住） | 测试 `test_ar19_phase1_failure_...` 失败 | 改为捕获异常并按阶段失败处理 | ✅ |
| 4 | 测试里写了 `"证据不足" in (out.envelope if False else out.rejected_reason)` | 死代码 + 可空比较 | 评审自查 | 改成显式断言并检查 `is not None` | ✅ |
| 5 | `structure.md` 仍写「`analysis/` `console/` 当前**不存在**」 | 状态过期（`TC-2` 类） | 本轮已建 | 改为「均已建」并列明各目录状态 | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | L4 分析层按设计以 **Python** 实现（`llm-components` / `intent` / `chain` / `strategy` + 24 例测试）· 引入仓库内 `.venv` 与锁定门禁（`ruff` / `pytest`）· [ADR-0021](../background/decisions/0021-l4-python-toolchain.md) · 结构与进度同步 | 目标「按设计完成所有模块」· `TB-2` · `TB-15` · `TB-20` · `TB-14` · `AR-12` · `AR-14` · `AR-15`…`AR-25` · `AR-31`/`AR-32` |
