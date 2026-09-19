# 变更包：L4 接入运行时（近线 worker）+ 事件契约对齐（跨语言）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 让 L4 **真的在运行时跑起来**，并修掉它运行时**一条也解析不出**的跨语言契约错位 |
| 日期 | 2026-09-19 |
| 状态 | 已验证（真进程端到端：真流量 → 判定事件 → 去重 3 条 → 2 条结论 → 控制台显示；Python 36 例 + Go 契约测试全过；`make gate` 通过） |
| 改动分级 | **L**（新增运行时组件与跨语言契约，改数据侧键名与门禁） |
| 涉及模块 | `llm-components` · `intent` · `chain` · `strategy`（运行时链路）· `control`（载荷键名）· `console`（结论展示） |
| 决策数 | 已答 1 项（[ADR-0022](../background/decisions/0022-l4-near-line-worker.md)）/ 待定 3 项 |
| 关联 | [ADR-0022](../background/decisions/0022-l4-near-line-worker.md) · [ADR-0021](../background/decisions/0021-l4-python-toolchain.md) · [`../spec/events.md`](../spec/events.md) · `AR-11` · `AR-12` · `AR-14` · `AR-15` · `AR-31` · `AR-32` · `ST-6` · `ST-7` · `NI-1` |

---

## 1. 需求与验收

**要解决什么**：上一轮交付的 L4 是**库 + 单测**——设计里它是链路的一环（`architecture.md` §2），实际**运行时无人调用**；更要紧的是，
第一次真接上就发现 **L4 按自造字段名解析事件**（`event_id`/`source`），而核心写出的载荷是 Go 字段名（`DecisionID`/`SourceIP`）：
**取到 26 条事件、0 条可分析**。测试自洽却与真实形状不符 —— 这正是「整体功能不准确」的典型。

**验收判据**：

1. **运行时链路**：`make analysis` 能读核心遥测 → 按 `AR-14` 去重 → 产出 `intent` / `strategy` 结论 → **作为 `analysis` 事件上报**；重跑不重复（`AR-11`）。
2. **退化语义**：遥测不可达 → 本轮不分析并记错误、`--once` 返回非 0（近线，`NI-1`）；没有新态势 → 不产出结论（不臆测，`AR-15`）。
3. **契约唯一**：事件载荷键名统一 **snake_case**，并有**跨语言夹具**（由 Go 结构体生成）+ 两侧契约测试（Go 与 Python 读同一夹具）。
4. **看得见**：控制台新增 `分析结论（L4）` 块与 `/api/analysis`；概览含 L4 结论计数。
5. `make dev` 第 6 步跑一轮 L4（缺 Python 环境即失败，不静默跳过）；`make gate` 绿。

**不做什么**：不把 L4 放进请求路径（`AR-29` 延迟预算）· 不接真实 LLM（`UnconfiguredClient` 显式失败）· 不让 worker 改策略或响应（`AR-32`）。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | L4 怎么接进运行时 | **近线 worker**（读 `ListEvents` → 结论 `Report`） | 合设计（L4 近线）· 读侧契约已存在 · 结论复用观测面、控制台即可见（[ADR-0022](../background/decisions/0022-l4-near-line-worker.md)） |
| ② | 事件载荷键名 | **统一 snake_case**（Go 加 JSON 标签） | 与策略载荷（`policy-payload.md`）、L4 结论、前端惯例一致；避免"载荷一套键、接口另一套键" |
| ③ | 如何防止再次错位 | **夹具 + 两侧契约测试** | 夹具由 Go 结构体**直接生成**，任何一方改键即红 |
| ④ | 去重放哪 | **L4 侧**（worker 内 `SituationDedupe`） | `AR-14` 管的是"触发 L4"，属 L4 的入口纪律 |
| ⑤ | 证据校验怎么才有意义 | worker 维护**有界已见证据缓存** | 否则"引用同一个查询结果里的 ID"永远成立，`AR-12` 形同虚设 |

---

## 3. 追溯矩阵

| 规则 ID | 文档 | 代码 | 测试 |
| --- | --- | --- | --- |
| `AR-11`（幂等） | [`../spec/events.md`](../spec/events.md) §3 | `analysis/worker.py`（结论 `event_id` 由内容摘要决定） | `test_worker_is_idempotent_on_rerun` |
| `AR-12`（证据引用校验） | [`../modules/chain.md`](../modules/chain.md) | `analysis/chain/evidence.py` + worker 的 `EvidenceCache` | `test_ar12_chain_is_void_when_evidence_missing` · `test_ar12_evidence_refs_must_exist` |
| `AR-14`（态势去重） | [`../modules/strategy.md`](../modules/strategy.md) | `analysis/dedupe.py` + worker | `test_ar14_worker_dedupes_same_situation_before_analysis` |
| `AR-15`（禁止默认值） | [`../modules/llm-components.md`](../modules/llm-components.md) | worker：无新态势即不产结论；`strategy` 无诱饵即拒绝 | `test_worker_without_events_concludes_nothing` |
| `AR-31`（不可信数据） | 同上 | `analysis/events.py`（`to_untrusted` 原样保留） | `test_untrusted_block_keeps_raw_fields` |
| `AR-32`（无执行面） | [ADR-0022](../background/decisions/0022-l4-near-line-worker.md) | worker 只调 `list_events` / `report` | `test_ar32_worker_only_reads_and_reports_structured_conclusions` |
| `ST-6`（契约唯一事实源） | [`../spec/events.md`](../spec/events.md) | 夹具 `api/telemetry/v1/testdata/decision_event.json` + 两侧测试 | `observer_contract_test.go` · `test_event_contract.py` |
| `ST-7`（判定细节只进观测面） | [`../integrate/observability.md`](../integrate/observability.md) | 结论块只读展示 | 实跑 |
| `NI-1`（不影响业务） | [ADR-0022](../background/decisions/0022-l4-near-line-worker.md) | worker 是独立进程、失败只记错误 | `test_ni1_worker_degrades_when_telemetry_unreachable` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `analysis/telemetry.py` | 新增 | 遥测端口（`ListEvents` / `Report`）+ gRPC 适配器 + 内存替身；`ensure_proto_path()` 解决生成桩的绝对导入 |
| `analysis/worker.py` | 新增 | 近线链路：取事件 → `AR-14` 去重 → 意图/链/策略 → 结论上报；`--once` 供门禁与人工测试 |
| `analysis/events.py` | 改 | 按**真实载荷键名**解析（`decision_id` / `source_ip`）—— 本次事故的修复点 |
| `core/internal/control/observer.go` | 改 | `DecisionRecord` 加 snake_case JSON 标签（事件载荷的线上形状） |
| `console/cmd/console/main.go` · `console/web/index.html` | 改 | 新增 `/api/analysis` 与「分析结论（L4）」块；`flowRecord` 键名对齐；修掉页面重复闭合标签 |
| `api/telemetry/v1/testdata/decision_event.json` | 新增 | 跨语言契约夹具（由 Go 结构体生成） |
| `core/internal/control/observer_contract_test.go` | 新增 | Go 侧契约测试（与夹具逐字段对照） |
| `analysis/tests/test_event_contract.py` · `test_worker.py` | 新增 | Python 侧契约测试 + 运行时链路测试 |
| `analysis/proto/**` | 新增 | 由 `api/telemetry/v1/telemetry.proto` 生成的 Python gRPC 桩（`make pygen`） |
| `pyproject.toml` · `requirements.txt` · `requirements-dev.txt` · `pyrightconfig.json` | 改/新增 | 可编辑安装为包 · 资源随包分发 · 锁定依赖 · 静态检查器导入噪音的显式说明 |
| `Makefile` · `scripts/dev/smoke.sh` | 改 | `pygen` / `analysis` 目标（并修 `.PHONY`：同名目录曾让目标被当成"已最新"）；`make dev` 第 6 步跑一轮 L4 |
| `docs/spec/events.md` · [ADR-0022](../background/decisions/0022-l4-near-line-worker.md) · `docs/integrate/*` · `docs/modules/*` | 新增/改 | 契约正文 · 运行时接缝决策 · 使用与人工测试 · 模块测试表 |

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 |
| --- | --- | --- | --- |
| 1 | Python 单测 | 全过 | ✅ `36 passed` |
| 2 | Go 契约测试 | 夹具与结构体一致 | ✅ `TestDecisionRecordWireContract PASS` |
| 3 | 真核心 + 真流量 + `make analysis` | 解析成功、去重生效、出结论 | ✅ `取事件 6 条 · 去重后 3 条 · 结论 2 条（新 2）` |
| 4 | 结论内容 | `intent` 接受并带证据；`strategy` 无诱饵即拒绝 | ✅ `intent accepted=True category=exfiltration evidence_ids=[…]` · `strategy accepted=False（没有可用诱饵，AR-15）` |
| 5 | 控制台 | `/api/analysis` 返回 2 条；页面含新区块 | ✅ HTTP 200 · `l4_conclusions: 2` · 页面 10149 字节含「分析结论（L4）」 |
| 6 | 遥测不可达 | 降级不分析、退出码非 0 | ✅ `test_ni1_*` + CLI 用例 |
| 7 | 页面渲染纪律 | 数据一律走 `textContent` | ✅ 16 处 `textContent`，0 处 `innerHTML` 拼接 |

**没有覆盖的**：真实 LLM（未接）· 常驻模式长跑 · 跨节点遥测（当前 loopback）· worker 重启后的窗口回补。

---

## 6. 验证证据

```console
$ SHEN_CORE_ADDR=127.0.0.1:19460 make analysis
[L4] 取事件 6 条 · 去重后 3 条（抑制 0）· 结论 2 条（新 2 / 重复 0）
  · intent: accepted=True data={"category": "exfiltration", "confidence": 0.333, "evidence_ids": ["4fbafbce…"]}
  · strategy: accepted=False data={}

$ curl -s http://127.0.0.1:19461/api/summary
{"total": 8, "by_action": {"route_origin": 3}, "alerts": 0, "l4_conclusions": 2, …}

$ .venv/bin/pytest analysis/tests     → 36 passed
$ make gate                            → 门禁通过。
```

**关键指标**：新增运行时组件 **1 个**（近线 worker）· 新增跨语言契约测试 **2 套**（Go + Python，同一夹具）· 新增接口 **1 个**（`/api/analysis`）· 修复的真实缺陷 **1 个**（载荷键名错位，此前运行时 0 条可分析）。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | **无断点续读**：每轮只取最近 `--limit` 条 | 进程重启不回补更早事件（幂等保证不重复，但可能漏窗口） | 接 `NI-13` 存储时改为持久化队列 |
| 2 | L4 未接真实 LLM | 只走确定性规则 | 部署侧注入 `AnalysisClient`（`AR-19`…`AR-21` 才会被真正走到） |
| 3 | `strategy` 结论未回写 `policy` | 结论只是数据 | 接策略面（`AR-13` 版本化 + 灰度）是下一步 |
| 4 | 事件类型字典不完整 | `actor_id` 等字段暂无消费方 | `docs/spec/logs.md` 落地时补齐 |
| 5 | `honeypot-shell` 与蜜罐协议栈内容 | **目标明确排除** | 保持推迟 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | L4 解析不了真实载荷（自造字段名） | **缺陷**（运行时 0 条可分析） | 实跑 `取事件 26 条 · 去重后 0 条` | 统一 snake_case（Go 加标签 + Python 对齐）+ 夹具与两侧契约测试 | ✅ 实测 3 条入分析、2 条结论 |
| 2 | `make analysis` 报「已是最新」 | **缺陷**（同名目录 `analysis/` 让 make 认为目标已存在） | `make: 'analysis' is up to date` | 把新目标全部加进 `.PHONY`（原 `.PHONY` 是多行续行，早先的替换没生效） | ✅ 目标真实执行 |
| 3 | gRPC 生成桩用绝对导入，`python -m analysis.worker` 崩 | **缺陷**（只有 `pytest` 下能跑） | `ModuleNotFoundError: No module named 'telemetry'` | `ensure_proto_path()` 把桩目录入 `sys.path` | ✅ 两种调用方式都可用 |
| 4 | 页面尾部重复 `</body></html>` 且 `</script>` 位置错 | **缺陷**（HTML 结构损坏） | 静态检查 `Tag must be paired` | 修正为 `</script></body></html>` | ✅ 结构检查通过 |
| 5 | 旧 console 进程占着端口，新二进制没接上（一度误判为接口未实现） | 环境陷阱 | `lsof` 显示旧 PID 仍 LISTEN | 按端口杀 PID 后重启；并把「进程名不匹配 `pkill -f`」记入经验 | ✅ `/api/analysis` 200 |
| 7 | **证据缓存装错命名空间**：装的是遥测事件 ID（`decision:<id>`），而链引用的是载荷里的 `decision_id` | **缺陷**（`AR-12` 误判、整链作废） | `make dev` 第 6 步报 `攻击链作废：引用了不存在的证据 ID（AR-12）` | 缓存同时装两者，并加回归用例 `test_ar12_evidence_cache_uses_payload_decision_id` | ✅ `make dev` 全绿 |
| 8 | `pip install -e .` 在仓库根生成 `shen_analysis.egg-info/`，被 `archcheck` 按「新增顶层目录」拦下 | 工具误判（产物当源码布局） | `make gate` 报 `ST-1 ... shen_analysis.egg-info/` | `archcheck` 增加产物目录跳过（`*.egg-info` / `build` / `dist` / `__pycache__`）+ `.gitignore` | ✅ `make gate` 通过 |
| 6 | 静态检查器报包内相对/绝对导入都"无法解析" | 工具误判 | 两种 cwd 导入成功 + 36 项测试全过 + 可编辑安装 | 显式关闭该噪音规则并写明理由（运行时权威） | ✅ 噪音消除 |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | L4 接入近线 worker（[ADR-0022](../background/decisions/0022-l4-near-line-worker.md)）· 事件载荷统一 snake_case + 跨语言夹具与双侧契约测试 · 控制台「分析结论（L4）」块与 `/api/analysis` · `make pygen` / `make analysis` / `make dev` 第 6 步 | 目标「按设计完成所有模块，保证整体功能的准确」· `AR-11`/`AR-12`/`AR-14`/`AR-15`/`AR-31`/`AR-32` · `ST-6`/`ST-7` · `NI-1` |
