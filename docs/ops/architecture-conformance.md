# 架构符合性（实现 ↔ 设计 · 解耦判据 · 怎么核）

> **这是什么**：回答「**实现是否按 `docs/design/` 的架构来的**」。
> 分两层判 —— **能机器核的绝不靠人读**（§2），核不了的写成**可复核的引用**（§3）。
> **它不是规则**：规则在 [`../design/`](../design/README.md)；本文只说「哪条规则由什么保证、怎么核」。
>
> ```sh
> make archcheck          # 结构面：13 项（已含在 make gate）
> make verify-modules     # 逐模块证据链 + 真跑单测
> ```

---

## 0. 一句话结论

| 解耦主张（来自 `design/`） | 由什么保证 | 谁在核 |
| --- | --- | --- |
| **判定与响应生成只实现一次**（`AR-2` / `AR-5`） | `modules/**` 在编译期**不可能** import `common/core/internal/**`（Go 的 `internal/` 规则 + `ST-3`）；判定是纯函数；决策取值是闭集 | `archcheck` 检查 3 / 10 / 11 |
| **唯一出口**（`AR-33`）：任何生成都过 `ai-capability` + 两道检查 | 门禁的结构检查：除出口与接缝外**禁止** import 模型客户端 | `archcheck` 检查 8 |
| **AI 能力独立**（`MD-4`）：可被第二个消费方复用 | `analysis/aicap` 只允许依赖 `analysis.llm` 与自己；`analysis/llm` 禁止反向依赖 | `archcheck` 检查 9 |
| **L4 不碰处置**（`AR-32`）：只上报结论事件 | `analysis/` 不得生成策略面桩、不得出现策略客户端；客户端无执行面成员 | `archcheck` 检查 12 + 单测 |
| **store 是核心唯一 I/O 出口**（`MD-20`） | 除 `store` 外禁止引入存储驱动 | `archcheck` 检查 4 |
| **不影响原始业务**（`NI-1` / `NI-5`） | 故障注入用例：核心被杀 / 变慢 / 返回畸形 / 幻境后端挂 | `go test ./modules/deception/proxy/` |
| **响应路径零非确定性**（`AR-30`） | 热路径禁止伪随机；会话钉定 + 产物冻结 | `archcheck` 检查 11 + proxy 单测 |
| **语言分层上限 5 种**（`TB-20`/`TB-21`）· 禁 CGO（`TB-24`） | 语言清单从文档解析后计数；`CgoFiles` 必须为空 | `archcheck` 检查 6 / 7 |

---

## 1. 结构面（可机器核，进 `make gate`）

`go run ./scripts/archcheck` —— **13 项**（`main()` 里逐个调用），清单**从文档解析**、不硬编码：

| # | 检查 | 规则 | 抓什么 |
| --- | --- | --- | --- |
| 1 | 顶层目录白名单 | `ST-1` | 悄悄多出第五个顶层目录 |
| 2 | 容器子目录白名单 | `ST-2` / `ST-4` | 平面内悄悄多出 / 改名子目录 —— 那是跨平面的另一条通路 |
| 3 | 禁止跨顶层目录 import | `ST-2` / `ST-4` | 平面之间直接互相引用（跨平面只允许 wire format） |
| 4 | 非核心代码禁止 import `core/internal` | `ST-3` | 适配器去拿核心内部状态（**解耦的第一道闸**） |
| 5 | `store` 是核心唯一 I/O 出口 | `MD-20` | 别的模块直接连 Redis / PG / CH |
| 6 | 模块目录 ↔ `modules.md` §1.1 清单一致 | `MD-18` / `MD-19` | 幽灵模块 / 漏登记 |
| 7 | 禁止 CGO 与本地原生库 | `TB-24` | 破坏「单二进制、交叉编译」 |
| 8 | 实现语言必须登记且总数 ≤ 5 | `TB-20` / `TB-21` | 悄悄引入第六种语言 |
| 9 | 模型客户端只许出口与接缝 import | `AR-33` | 绕过护栏的生成路径 |
| 10 | Python 侧依赖方向（`aicap` / `llm`） | `MD-4` | AI 能力反向依赖消费方 |
| **11** | **决策取值闭集** | `MD-12` | 第四个 `Action`（会落到 `String()="unknown"` ⇒ 被当成放行） |
| **12** | **热路径禁伪随机 + `judge` 纯函数（含子包）** | `AR-30` / `MD-6` | 热路径引入 `math/rand`；`judge`（或其子包）引入 `time`/`os`/`net` |
| **13** | **L4 不碰写侧** | `AR-32` | `analysis/proto/` 出现非 telemetry 契约（目录缺失也报）；`analysis/` 出现策略客户端（含 `policy_pb2` / `PolicyService` 等 Python 写法） |

> 第 **11/12/13** 项是本轮新增的（此前这三条只有文字、没有机器判据）。
> 反证记录见 §4 —— 每一条都做过「注入违规必然报红」。

---

## 2. 语义面（核不了机器，但引用必须真实存在）

下表每条都给了**实现位置**与**证据**（真实的测试函数名 / 文档章节）。
引用可核：把下面的命令跑一遍，缺失的名字会被列出来。

```sh
# 核验本文引用的测试函数真实存在（引用核验，不是印象）
# 两条腿都要走：Go 用 `func Name(`，Python 用 `def name(` —— 只查 Go 会漏掉表里全部 Python 引用。
grep -oE '`(Test[A-Za-z0-9_]+|test_[a-z0-9_]+)`' docs/ops/architecture-conformance.md \
  | tr -d '`' | sort -u | tee /tmp/cited.txt | wc -l    # 去重后的引用总数（46）
while read -r t; do
  grep -rq "func $t(" --include='*_test.go' . \
    || grep -rq "^def $t(" --include='*.py' analysis/tests/ \
    || echo "缺失：$t"
done < /tmp/cited.txt
```

| 设计主张 | 实现位置 | 证据（测试 / 检查） |
| --- | --- | --- |
| 判定是纯函数：同样观测得同样分（`AR-2` + `MD-6`） | `common/core/internal/judge/judge.go` | `TestJudge_PositiveAndNegative` · `TestJudge_ScoreCappedAtOne` · `TestJudge_NilRuleSourcePanics` · `archcheck` 检查 11 |
| 三值决策 + 灰度确定性收敛（`MD-12` / `INT-12`） | `common/core/internal/director/director.go` | `TestClassifyBoundaries` · `TestGrayDeterministic` · `TestGrayZeroFallsBackToOrigin` · `TestGrayHundredAlwaysDiverts` · `TestNegativeScoreFallsBackToOrigin` |
| `block` 默认关（`Q5`）· 诱饵面禁止 block（`MD-25`） | 同上 | `TestBlockDefaultOff` · `TestBlockBackendEmpty` · `TestDecoyPathsNeverBlocked` · `TestNonDecoyPathStillBlocks` · `TestDecoyPathLowScoreStaysOrigin` |
| 白名单先于引流判定（`INT-25`） | `director.whitelisted` + `proxy.whitelisted` | `TestWhitelistByUserAgentSkipsJudgement` · `TestWhitelistSkipsCore` · `TestRemoteWhitelistIsAdditive` |
| 影子模式只算不处置（`INT-11`） | `director` + `proxy` | `TestShadowNeverDiverts` · `TestShadowAlsoSuppressesBlock` |
| **不影响原始业务**：核心挂了 / 慢了 / 答畸形都放行（`NI-1`） | `modules/deception/proxy` | `TestV1_KilledCoreKeepsBusinessAlive` · `TestV2_SlowCoreKeepsBusinessAlive` · `TestV3_MalformedCoreResponseKeepsBusinessAlive` · `TestCoreErrorFallsBackToOrigin` · `TestV4_IllegalDecisionValueFallsBackToOrigin` |
| 幻境后端不可用回落业务（`NI-5`） | `proxy.forwardMirage` | `TestMirageBackendDownFallsBackToOrigin` · `TestUnknownBackendFallsBackToOrigin` |
| 同会话同资源同答案（`AR-30`）：会话钉定 + 变体分布 | `proxy` 的变体选择与钉定 | `TestVariantOfSessionPinsFirstResult` · `TestVariantPinsHonorTTLAndVersion` · `TestVariantForDistributesAcrossSessions` |
| 注入是**插入**语义，且只改改道侧（`ST-5` / `INT-8`） | `modules/deception/injection` | `TestInjectContentInsertsBeforeMarker` · `TestInjectContentLeavesNonHTMLAlone` · `TestInjectContentSkippedWhenMarkerMissing` · `TestApplyEdgePolicyInjectSemantics` · `TestLargeResponseIsNotInjectedOrTruncated` |
| 唯一出口 + 检查不可绕过（`AR-33`） | `analysis/aicap/service.py` | `test_run_task_is_task_agnostic` · `test_registry_lists_all_four_kinds` · `test_startup_assert_fails_when_a_new_template_is_missing` · `archcheck` 检查 8 |
| 越界即拒绝、不回落默认值（`AR-15`） | `analysis/llm/contract.py`（`Field.allowed`） | `test_ar15_allowed_is_a_closed_set` · `test_intent_out_of_set_category_is_rejected_not_defaulted` · `archcheck` 检查 10 |
| 攻击者可控内容只经数据区（`AR-31`） | `analysis/llm/untrusted.py` + `aicap/guardrail/prompts.py` | `test_ar31_untrusted_content_is_structured_and_labeled` · `test_run_task_is_task_agnostic` |
| 模型客户端无执行面（`AR-32`） | `analysis/llm/client.py`（成员名子串检查） | `test_ar32_adapter_has_no_execution_surface` · `test_ar32_worker_only_reads_and_reports_structured_conclusions` |
| 证据引用必须真实存在（`AR-12`） | `analysis/chain/evidence.py` + `analysis/worker.py` | `test_ar12_evidence_refs_must_exist` · `test_ar12_evidence_cache_uses_payload_decision_id` · `test_worker_voids_model_chain_when_evidence_does_not_exist` |
| 触发分析必经态势去重（`AR-14`） | `analysis/dedupe.py` | `test_ar14_worker_dedupes_same_situation_before_analysis` |
| 统一信封（`AR-16`）· 提示词资源化（`AR-24`） | `analysis/llm/envelope.py` · `analysis/llm/prompts.py` | `test_ar16_envelope_shape_and_reason` · `test_ar24_prompt_resources_validated` |
| 模块 ↔ 文档 ↔ 单测（`MD-2` / `MD-17` / `MD-22`） | 全 24 个模块 | `make verify-modules`（逐模块表 · 见 [`module-test-flow.md`](module-test-flow.md)） |

---

## 3. 本轮新增的三条机器判据（附反证）

**只有「注入违规会报红」的检查才算检查** —— 三条都实测过：

| 新增检查 | 反证（临时注入 → 期望报红） | 结果 |
| --- | --- | --- |
| `MD-12` 决策取值闭集 | 在 `contract/decision.go` 的 iota 块里加 `ActionQuarantine` | ✅ 报「决策取值必须是闭集（恰好 3 个），实际 4 个」 |
| `AR-30` / `MD-6` 响应路径纯度 | 给 `director` 加 `import "math/rand"`；给 `judge` 加 `import "time"` | ✅ 报「响应路径不得引入伪随机」/「judge 必须保持纯函数」 |
| `AR-32` L4 不碰写侧 | 新建 `analysis/proto/policy/policy_pb2.py` | ✅ 报「analysis/proto/ 下只允许 telemetry 契约」 |

同时**没有**加的那条：适配器「不得构造 `contract.Decision`」—— 它已被 `ST-3` 覆盖
（适配器**只能** import `common/api/*`，根本没有 `contract` 可引用），加了就是一条**永远为真的假防线**。

---

## 4. 已知不符合 / 缺口（诚实清单）

| # | 项目 | 现状 | 去向 |
| --- | --- | --- | --- |
| 1 | `modules/deception/mirror` 缺 `iface.go`（导出了 `JudgeClient` / `TelemetryClient`） | `structure.md` §1.4 的三文件约定被破坏（`make trace` 只提示 `TC-1`，该约定尚未升格为规则） | 单开一轮（S 档）改代码 |
| 2 | 诱饵资产 → 边缘未接通 | 诱饵面 observe-only（`MD-25`） | 设计登记的未接通项 |
| 3 | 真实蜜罐协议栈未实现 | 只有框架 + 契约（`ADR-0011`：具体蜜罐接第三方） | 阶段 3 / 接第三方 |
| 4 | 内容轮换消费方未接 | `strategy` 出信号，没人 +1 清单版本 | `ADR-0023` 未解决 4 |
| 5 | L3 网络欺骗（`netpolicy`）只有声明式产物 | 需集群侧加载 | 阶段 3 |
| 6 | 判定缺失用例偏少（`judge` 仅 3 例） | 判定是**核心价值**，用例数却是 24 个模块里最少的之一 | 建议随 `judge` 规则扩充补齐（不阻塞） |

---

## 5. 怎么复查（三条命令）

```sh
make gate                # 结构 + 逐模块证据 + 追溯 + 泄漏 + 许可 + 全部单测
make verify-modules      # 逐模块「文档 / 规则依据 / 测试 / 实测」表（30s）
docs/ops/architecture-conformance.md   # 本文：§0 主张表 · §2 引用表（可 grep 核）
```
