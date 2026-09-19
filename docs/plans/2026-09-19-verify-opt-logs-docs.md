# 变更包：验证 + 代码/日志优化 + 文档整合减量

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | ① 跑一遍三层验证取基线 ② 代码优化：gRPC 连接预热（治 `K-24`） ③ 日志优化：判定失败**无条件**告警 ④ 文档整合：删 `manual-test.md` 并入功能验证文档 |
| 日期 | 2026-09-19 |
| 状态 | 已验证（首请求不再超时：重建后**第一条**请求即得判定 `失败=<nil>`；`make gate` 通过） |
| 改动分级 | **M**（一处适配器代码 + 一处日志 + 文档整合；不改判定逻辑） |
| 涉及范围 | `edge/proxy/handler.go` · `docs/ops/functional-verification.md` · `docs/integrate/`（少一份）· `docs/README.md` · `docs/modules/console.md` · `README.md` |
| 决策数 | 已答 3 项（预热不阻断启动 · 判定失败日志不设开关 · 人工测试并入功能验证） |
| 关联 | `AR-29` · `NI-3` · `NI-4` · `INT-17` · `K-24` · [`../kb/known-issues.md`](../kb/known-issues.md) |

---

## 1. 需求与验收

**用户要什么**：先验证，再做代码与日志优化，完成文档优化，并**整合不必要的文档、保证准确、减少数量**。

**验收判据**：

1. **验证有基线**：`make gate` 绿 · `scripts/shen.sh doctor` 五项有结论 · `scripts/shen.sh traffic` 全量断言通过；
2. **代码优化**：`K-24`（重启后首请求判定超时后放行、无观测记录）的**根因**被消除，而不只是记录；
3. **日志优化**：判定失败（放行）**总能**留下线索，不依赖逐请求日志开关；
4. **文档整合**：删掉重复文档并保证引用不悬空、内容不丢；
5. `make gate` 绿。

**不做什么**：不改判定与处置语义 · 不引入新依赖 · 不重写历史留痕（`plans/` · `log.md` 旧条目）。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | 首请求超时怎么治 | **启动时预热 gRPC 连接**（最多等 2s，失败只记日志、**不阻断启动**） | 把建连成本从"请求路径（3ms 预算）"挪到"启动路径"；核心未起也要能启动（模块独立启动） |
| ② | 判定失败日志要不要开关 | **不要**，失败即 warn（带耗时与原因） | 它是"引擎没判定"的唯一线索；默认部署下若被开关挡住，"为什么没判"就无从查起（实测踩过） |
| ③ | 人工测试文档怎么办 | 并入 [`../ops/functional-verification.md`](../ops/functional-verification.md) §7，删除原文件 | 二者都是"怎么验证"，重复度高；保留"人工才做"的两件事（故障注入 · 边界情形） |

---

## 3. 追溯矩阵

| 规则 | 落实 |
| --- | --- |
| `AR-29`（延迟预算 3ms） | 预热说明写明预算前提；`K-24` 同 |
| `NI-3` / `NI-4`（失败放行） | 判定失败仍返回 `route_origin`，只是**多记一条 warn** |
| `INT-17` | 自检五项作为验证基线的一层（另一层是门禁与判定验证） |
| `K-24` | 由"已登记现象"升级为"**已消除**"（预热），文档同步 |
| `TC-3`（悬空链接） | 删除文件后 8 处引用改指向；`make trace` 通过 |

---

## 4. 产物

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `edge/proxy/handler.go` | 改 | 新增 `warmUp()`（`conn.Connect()` + 等 `connectivity.Ready`，最多 2s）并在 `Provision` 调用；`decide()` 失败分支**无条件** warn（含 `耗时=` 与 `原因=`） |
| `docs/ops/functional-verification.md` | 改 | 新增 §7 人工测试（15 分钟一轮）：故障注入（`NI-1` 现场验证）· 边界情形快速检查 |
| `docs/integrate/manual-test.md` | **删除** | 内容已并入 §7；本文与 `ops/` 重复 |
| `README.md` · `docs/README.md` · `docs/integrate/README.md` · `docs/integrate/business-onboarding.md` · `docs/modules/console.md` | 改 | 8 处引用改指向 `ops/functional-verification.md` §7；顺带修掉"标签写着 manual-test、目标却是别的文件"的错配 |

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 |
| --- | --- | --- | --- |
| 1 | `make gate` | 绿 | ✅ |
| 2 | `scripts/shen.sh doctor` | 五项有结论 | ✅ 通过 3 · 失败 0 · 约束 1 · 无法判定 1 |
| 3 | `scripts/shen.sh traffic` | 全量断言通过 | ✅ 27/27 · 缺口 8 · 出口卫生 0 |
| 4 | **整栈重建后第一条请求** | 应有判定（不再超时） | ✅ `判定：GET /k24-check … 失败=<nil>`（预热生效） |
| 5 | 失败路径日志 | 无需开关即出现 | ✅ 代码分支（`log.Printf("proxy: 判定失败，按 NI-3 放行…")`） |
| 6 | 文档引用 | 无悬空 | ✅ `make trace` 通过 |
| 7 | 适配器单测 | 全过 | ✅ `go test ./edge/proxy/`（17.9s） |

**没有覆盖的**：预热在"核心永远不可达"下的长尾行为（只记日志，未做专门的超时用例）；失败日志的限流（失败本就罕见，未加）。

---

## 6. 验证证据

```console
$ scripts/shen.sh restart && curl -A "HeadlessChrome/120" http://127.0.0.1:18080/k24-check && make docker-log S=proxy
{"msg":"proxy: 判定：GET /k24-check decision_id=b0977d01… → route_origin（后端 \"\"，失败=<nil>）"}

$ scripts/shen.sh doctor   → 通过 3 · 失败 0 · 约束 1 · 无法判定 1
$ scripts/shen.sh traffic  → 断言 27/27 通过 · 缺口 8 条 · 出口卫生问题 0 条
$ make gate                → 门禁通过。
```

**关键指标**：代码改动 **2 处**（预热 + 失败日志）· 文档数量 **-1**（`integrate/` 5 → 4 份）· 引用修正 **8 处** · 消除已登记缺陷 **1 条**（`K-24`）。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | `docs/log.md` 与 `docs/plans/` 的**历史条目**里有相对链接写法不规范（Marksman 提示） | 不影响门禁 | 低优先，必要时批量修 |
| 2 | 判定失败日志未限流 | 核心长时间不可达时会刷屏 | 需要时加节流 |
| 3 | `docs/plans/` 46 份历史变更包 | 目录偏大，但属追溯留痕 | 若确需减量，可归档为按月汇总（需评估 `make trace` 对最新条目的依赖） |
| 4 | `scripts/sentinel` 仍占位 | 实境/幻境逐字段 diff 无工具 | 接入演练前补 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `K-24` 只被"登记"，失败模式仍在 | 缺陷未修（自欺风险） | 重建后首请求 `失败=DeadlineExceeded` | 加连接预热（根因消除）+ 文档从"已知现象"改为"已消除" | ✅ 首请求即得判定 |
| 2 | 判定失败没有任何**无条件**日志 | 可诊断性缺口 | 默认部署下失败只在逐请求日志里（默认关） | `decide()` 失败分支加 warn（耗时 + 原因） | ✅ |
| 3 | 删除 `manual-test.md` 后标签与目标错配（文本写 manual-test、链接指向 functional-verification） | 文档缺陷 | `grep manual-test` 的 5 处残留 | 逐处改标签与路径 | ✅ |
| 4 | 根 `README.md` 仍指向被删文件 | **悬空链接** | `make trace` 报 `TC-3 README.md:219` | 改为指向 `ops/functional-verification.md` | ✅ 追溯通过 |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 验证基线（gate/doctor/traffic）· gRPC 连接预热（消除 `K-24`）· 判定失败无条件告警 · 删除 `manual-test.md` 并入功能验证 §7 并修 8 处引用 | 用户要求（验证 · 代码与日志优化 · 文档整合减量）· `AR-29` · `NI-3` · `K-24` |
