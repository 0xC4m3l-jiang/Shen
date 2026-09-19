# 变更包 · 2026-09-18 · 删除 mirror 死代码 + block 可见性注释

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 闭合两处遗留：删 mirror 读 body 死代码；点明 block 403 是已承认的设计 |
| 日期 | 2026-09-18 |
| 状态 | 已实现 |
| 涉及模块 | `adapter-mirror`（edge/mirror）· `adapter-proxy`（edge/proxy） |
| 决策数 | 已答 1 项（删死代码）/ 待定 0 项 |
| 关联 | [`../background/decisions/0002-decision-model.md`](../background/decisions/0002-decision-model.md) · [`../log.md`](../log.md) 同日条目 |

## 1. 需求与验收

**要解决什么**：闭合上一轮的两处遗留——① `edge/mirror` 把 body 前缀塞进 header（`x-observed-body-prefix`）是契约无 body 字段的权宜；② block 决策返回 403 的可见性权衡。

**做完之后**：mirror 不再读请求体塞死数据；block 的可见性在代码里有据可查。

**验收判据**：

1. `x-observed-body-prefix` 与 `MaxBody` 从代码库消失，无人消费的死代码不再存在。
2. block 分支注释引用 ADR-0002。
3. `make gate` 全绿。

**不做什么**：

- 不给契约加 body 字段（阶段 2b 契约演进时再加，避免预置）。
- 不改变 block 的行为（403 是设计如此）。

## 2. 设计逻辑

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | mirror 读 body 塞 header | 删除 | grep 证实 `x-observed-body-prefix` 在核心/api/scripts 无任何消费，是死数据；契约无 body 字段，塞 header 无收益 | `edge/mirror/receiver.go` |
| ② | block 403 可见性 | 保留行为，加注释点明 | ADR-0002 已裁定：引擎是欺骗调度器不是 WAF，block 只用于明确拒绝已知恶意 | `edge/proxy/proxy.go` |

## 3. 追溯矩阵

| 规则 / 依据 | 位置 | 说明 |
| --- | --- | --- |
| ADR-0002（三值 + severity，block 保留但引擎定位透明误导） | `edge/proxy/proxy.go` dispatch | block 403 注释 |
| 最小实现（不留死代码） | `edge/mirror/receiver.go` | 删 body 读取 |

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `edge/mirror/receiver.go` | 改 | 删 body 读取块 + `MaxBody` 字段 + `maxBody()` + `defaultMaxBody` + `io` import |
| `edge/mirror/receiver_test.go` | 改 | 删只测死数据的 `TestReceiver_BodyIsCapped` + `strings` import |
| `edge/proxy/proxy.go` | 改 | block 分支加注释（ADR-0002 依据） |

## 5. 测试与场景

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | 死代码删除后编译测试 | 全绿 | ✅ | `make gate` 通过（edge/mirror 1.469s · edge/proxy 1.983s） |
| 2 | body 前缀无残留 | grep 为空 | ✅ | §6 |

## 6. 验证证据

```console
$ make gate
门禁通过。  （fmt · vet · staticcheck · errcheck · archcheck · trace · licensecheck · go test -race）
```

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 误导处置读 body（INT-22） | 阶段 2b 需要 | 契约演进时加 body 字段 |
| 2 | block 403 可见性 | 设计如此 | ADR-0002 已裁定，不重开 |

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 删 mirror 死代码 + block 注释 | 最小实现 · ADR-0002 |
