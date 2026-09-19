# 变更包：`NI-12` 的 `V-1…V-4` 自动化 + `AR-29` 空载下界

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 把 `NI-1` 的强制测试 `V-1…V-4` 做成 CI 内自动执行的用例（真 Caddy + 真 gRPC/裸 TCP 故障注入）；给出 `AR-29` 的**空载下界**基准 |
| 日期 | 2026-09-19 |
| 状态 | 已验证（4 例故障注入全绿 + 基准出数；`make gate` 通过；已提交） |
| 改动分级 | **M**（新增测试与基准 + 文档；产品代码零改动） |
| 涉及模块 | `adapter-proxy`（[`../design/modules.md`](../design/modules.md) §1.1 第 11 行） |
| 决策数 | 已答 2 项（V-5 归接入演练；基准只给下界）/ 待定 0 |
| 关联 | `NI-1` / `NI-12`（[`../design/constraints.md`](../design/constraints.md)）· `AR-29` · 实验 `E3`（[`../background/notes/pending-experiments.md`](../background/notes/pending-experiments.md)） |

---

## 1. 需求与验收

**要解决什么**：`NI-1` 是**最高优先级**约束，`NI-12` 明确要求它的强制测试 `V-1…V-5` **必须纳入 CI**。
而此前 `docs/modules/adapter-proxy.md` §7 的「故障注入」一行还写着「待补」—— 即：**最高优先级的约束没有自动化证据**。

**做完之后**：`V-1…V-4` 在 `make gate` 里自动执行；`AR-29` 有了可复现的空载下界（`make bench`）。

**验收判据**：

1. `V-1`（杀死引擎）· `V-2`（决策延迟 > 预算）· `V-3`（malformed protobuf）· `V-4`（非法决策值）各有专门用例，**走真进程路径**。
2. 每条都连打 **20 次**并要求 **100% 正常** —— 用「100%」而不是比例，因为 `NI-1` 的原话是「不受影响」。
3. `V-2` 额外断言总耗时**不接近**核心延迟之和（证明走的是超时放行，不是等核心）。
4. `AR-29` 空载下界可用 `make bench` 复现并登记进 `E3`。
5. `make gate` 绿；本轮已提交。

**不做什么**：**不做 `V-5`**（CPU 饱和下的 P99 对比需要基线 P99 与真实负载 → 属接入演练 `E3`）· 不改产品代码 · 不改规则。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | 用替身还是真进程？ | **真进程**（真 Caddy + 真 gRPC 客户端 + 真业务后端） | 这几条断言的正是「适配器与核心之间的真实链路在故障下会不会把业务带下水」；替身会绕开最可能出问题的那一段（连接、编解码、超时传播） |
| ② | 判定用「全部成功」还是「比例」？ | **100%** | `NI-1` 说「可用性与正确性不受影响」—— 一次失败就是一次业务故障 |
| ③ | `V-3`（malformed protobuf）怎么造？ | **裸 TCP 服务回垃圾字节** | 真 gRPC 服务端在 wire 层就拒绝非法 protobuf，造不出来；裸 TCP 才是忠实的「线上出现畸形响应」 |
| ④ | `V-5` 怎么办？ | **归入接入演练**（`E3`） | 它要「业务 P99 不劣化」，需要基线 P99 与真实负载；用单机微基准伪造一个"P99"是假证据 |
| ⑤ | 空载下界怎么表达？ | 直连 vs 经引擎的 `ns/op` 差值，并写明**这只是下界** | 避免把下界当成 `AR-29` 的验收结论 |

---

## 3. 追溯矩阵

| 规则 ID | 定义位置 | 测试 | 覆盖什么 | 验证命令 |
| --- | --- | --- | --- | --- |
| `NI-1` / `NI-12`（`V-1…V-4` 必须纳入 CI） | [`../design/constraints.md`](../design/constraints.md) 的 `NI-1` 表与 `NI-12` | `edge/proxy/failopen_test.go`（4 例） | 核心被杀 / 慢 / 畸形 / 非法值四种故障 | `make gate` |
| `NI-3`（失败放行） | 同上 | `TestV1_*` | 核心不可达 → 业务 100% 正常 | `make gate` |
| `NI-4`（硬超时后放行） | 同上 | `TestV2_*`（并断言总耗时不接近 20×延迟） | 超时预算生效 | `make gate` |
| `NI-5`（未识别回落） | 同上 | `TestV4_*`（UNSPECIFIED 与越界 99） | 非法决策值 → 放行 | `make gate` |
| `AR-29`（额外延迟预算 5 ms） | [`../design/architecture.md`](../design/architecture.md) | `edge/proxy/latency_test.go`（`make bench`） | 空载下界 ≈ **+48 µs** | `make bench` |
| `E3`（换底座后的 AR-29 实测） | [`../background/notes/pending-experiments.md`](../background/notes/pending-experiments.md) | 同上 | 下界已测；**P99 验收待演练** | 接入演练 |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `edge/proxy/failopen_test.go` | **新增** | `V-1…V-4`：# 走真进程路径；含三种「坏核心」实现（可停的真 gRPC 服务端 / 延迟判定面 / 裸 TCP 回垃圾字节） |
| `edge/proxy/latency_test.go` | **新增** | `BenchmarkAddedLatencyDirect` 与 `BenchmarkAddedLatencyThroughProxy`（空载下界） |
| `Makefile` | 改 | 新增 `make bench` |
| [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) | 改 | §7「故障注入」行由「待补」改为**已实现**（并单列 `V-5` 的去向）· 增基准行 · §9 变更记录 |
| [`../background/notes/pending-experiments.md`](../background/notes/pending-experiments.md) | 改 | `E3` 增「空载下界」结果段（含两臂数值与「这只是下界」的限定） |

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | `V-1` 杀死引擎（真 gRPC 服务端 `Stop()`） | 业务 20/20 正常 | ✅ | `TestV1_KilledCoreKeepsBusinessAlive` |
| 2 | `V-2` 判定延迟 400 ms（预算 50 ms） | 业务 20/20 正常且总耗时不接近 8 s | ✅ 0.05 s | `TestV2_SlowCoreKeepsBusinessAlive` |
| 3 | `V-3` 裸 TCP 回垃圾字节 | 业务 20/20 正常 | ✅ | `TestV3_MalformedCoreResponseKeepsBusinessAlive` |
| 4 | `V-4` UNSPECIFIED 与越界 99 | 业务 20/20 正常（回落放行） | ✅ | `TestV4_IllegalDecisionValueFallsBackToOrigin` |
| 5 | `AR-29` 空载下界 | 给出两臂数值 | ✅ 直连 37.7 µs · 经引擎 85.7 µs · **额外 ≈48 µs** | `make bench` |

**没有覆盖的**：`V-5`（CPU 饱和下的 P99 对比 —— 需基线 P99 与真实负载，归 `E3`）· 并发下的故障注入（当前是串行 20 次）。

---

## 6. 验证证据

```console
$ go test ./edge/proxy/ -run 'TestV[1-4]_' -count=1 -v
--- PASS: TestV1_KilledCoreKeepsBusinessAlive (0.01s)
--- PASS: TestV2_SlowCoreKeepsBusinessAlive (0.05s)
--- PASS: TestV3_MalformedCoreResponseKeepsBusinessAlive (0.00s)
--- PASS: TestV4_IllegalDecisionValueFallsBackToOrigin (0.01s)

$ make bench
BenchmarkAddedLatencyDirect-10           500   37670 ns/op
BenchmarkAddedLatencyThroughProxy-10     500   85668 ns/op      # 额外 ≈ 48 µs

$ make gate
门禁通过。
```

**关键指标**：新增故障注入用例 **4 例**（`V-1…V-4`）· 新增基准 **2 个** · `AR-29` 空载额外延迟 **≈48 µs**（预算 5 ms 的 1%）· 产品代码改动 **0 行**。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | `V-5`（CPU 饱和下 P99 不劣化）未做 | `NI-1` 的「资源被压满」场景缺自动化证据 | 接入演练（`E3`），需真实负载与基线 P99 |
| 2 | 故障注入目前是**串行** 20 次 | 未覆盖并发下的超时/回落 | 需要时加压测形态 |
| 3 | `AR-29` 只有下界 | 真实 P99 未知（大响应 / 流式 / TLS / 注入均会抬高） | `E3` |
| 4 | ⚠️ **`E2` 的「TLS 终结归属」仍待你裁决** | 关系 `A2` 与 `ADR-0017` | 你裁决后开 ADR 重估 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 初版 `V-1` 想复用 `startJudgeServer` 的地址再「杀死」它，但那个辅助**不交出服务端句柄**（会永远找不到） | 自伤 | 写测试时的自查 | 改为 `startStoppableJudgeServer` 自建并交出句柄；删掉注册表 hack | ✅ |
| 2 | `latency_test.go` 漏了 `net` 导入 | 自伤 | `go vet` 报 undefined | 补导入 | ✅ |
| 3 | 模块文档 §7「故障注入」行仍写「待补（`NI-12` 的 `V-1…V-5`）」 | 过期状态 | 本文件与模块文档对照 | 改为「`V-1…V-4` 已实现」+ 单列 `V-5` 去向 + 基准行 | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | `NI-12` 的 `V-1…V-4` 自动化（真 Caddy + 真 gRPC/裸 TCP 故障注入，每条 20 次要求 100% 正常）· 新增 `AR-29` 空载下界基准与 `make bench`（额外 ≈48 µs）· `E3` 登记下界 · `V-5` 明确归入接入演练 | `NI-1` / `NI-12` · `AR-29` · 实验 `E3` |
