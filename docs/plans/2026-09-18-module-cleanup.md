# 变更包 · 2026-09-18 · 模块代码整理（简化，无行为变更）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 整理本轮开发的模块代码：消除死代码与重复逻辑，为后续按架构重组模块做准备 |
| 日期 | 2026-09-18 |
| 状态 | 已实现 |
| 涉及模块 | `honeypot` · `store` · `responder` · `edge/proxy`（4 处简化） |
| 决策数 | 已答 0 项（纯整理，不改行为）/ 待定 1 项（见 §5） |
| 关联 | [`2026-09-18-code-review.md`](2026-09-18-code-review.md) · [`../log.md`](../log.md) 同日条目 |

## 1. 需求与验收

**要解决什么**：本轮快速开发的模块里积累了若干**冗余结构与重复逻辑**，会妨碍后续「按架构重组模块」时的阅读与搬动。

**验收判据**：

1. 不改变任何行为 —— 全部既有测试继续通过（含 `-race`）。
2. 不新增功能、不改动对外契约。
3. `make gate` + `make dev` 全绿。

## 2. 设计逻辑（4 处简化 + 1 处刻意保留）

| # | 位置 | 简化前 | 简化后 | 理由 |
| --- | --- | --- | --- | --- |
| 1 | `honeypot` | `known map[string]bool` + `knownTypes()`（每次调用重建切片并排序）+ `typeOrder()`（线性查设计清单序号） | `known []string`（**按给定顺序**保存）+ `slices.Contains` 做成员判断 | 成员判断与顺序输出由同一份数据满足；**删掉两个函数与一次排序**（-15 行）。原实现为了「输出稳定」每次都重建+排序，属于用复杂换稳定 |
| 2 | `store/memory.go` | 三个带 TTL 的存储各写一份「超限则清理过期」循环（其中一份还提取成了方法，另两份内联） | 一个泛型 `sweepExpiredIfFull[V]`，三处调用 | 同一策略三份实现 → 一处定义；也消除了「一份提方法、两份内联」的不一致 |
| 3 | `responder` | `seedOf` 分三次 `Write`（含一次写单字节分隔符） | 一次 `Write` 拼好的字符串 | 三段拼接的意图（会话+资源+资产）本可以一行表达 |
| 4 | `edge/proxy` | `injectResponse` 里三条「不注入」的提前返回散在函数开头 | 提取 `injectable(resp) (contentType, ok)` | 这三条本是**同一个概念**（这份响应能不能注入），抽出来后可读性提升，且函数体只剩「读→注入→回写」三步 |

**刻意保留（未简化）**：

- `director.Config.Now` / `Engine.now`：`Engine.now` 当前**没有任何读取点**，`Config.Now` 也无人设置 —— 看似死代码。但字段注释写明它是「**仅为将来的可观测留口**」，属于**有意的预留 API**；且删它属于改动导出类型形状。按「不改变对外契约」的要求**保留**，仅在此登记（见 §5）。

## 3. 文档对应（追溯矩阵）

| 依据 | 位置 | 验证 |
| --- | --- | --- |
| `MD-26`（蜜罐经 `honeypot` 管理，类型由配置决定） | `core/internal/honeypot/honeypot.go` | `honeypot_test.go`（15 例） |
| `AR-30`（一致性不变量） | `core/internal/responder/responder.go` | `responder_test.go`（16 例） |
| `INT-8`（只改蜜罐侧）· `NI-9` | `edge/proxy/proxy.go` | `proxy_test.go`（24+ 例） |
| `MD-20`（核心唯一 I/O 出口） | `core/internal/store/memory.go` | `store_test.go`（9 例） |

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `core/internal/honeypot/honeypot.go` | 改 | 删 `knownTypes()` / `typeOrder()`，`known` 改为有序切片 |
| `core/internal/store/memory.go` | 改 | 抽 `sweepExpiredIfFull`，三处调用统一 |
| `core/internal/responder/responder.go` | 改 | `seedOf` 一次写入 |
| `edge/proxy/proxy.go` | 改 | 抽 `injectable()`，收敛三条「不注入」判据 |

## 5. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | `director.Config.Now` 与 `Engine.now` 当前无读取点（有意预留） | 读者会疑惑「now 用在哪」 | 若确认不需要，删 5 行（字段 + `New` 里的默认值处理）；需先确认「将来可观测留口」是否仍要保留 |

## 6. 验证证据

```console
$ make gate
门禁通过。  （fmt · vet · staticcheck · errcheck · archcheck · trace · licensecheck · go test -race）

$ go test -count=1 ./...
15 个包 ok          ← 与整理前完全一致，无行为变更

$ make dev
✅ 通过：配置校验 · 非法配置被拒 · 启动 · 判定面 · 冒烟 · 规则回放
```

**净变化**：`honeypot.go` 170 → 155 行；`memory.go` 三份清理循环 → 一处。

## 7. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 模块代码整理：4 处简化，无行为变更 | 用户「整理优化模块代码」要求 |
