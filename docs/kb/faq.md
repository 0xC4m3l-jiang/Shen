# 高频问题

> 不限于确定性结论 —— 这里记录「现在是怎么做的」。
> 需要**必须遵守**的规则时，去 [`../design/`](../design/README.md)。

---

## Q1 · Go 代码为什么不在 `src/` 里？

顶层按**架构平面**分组，而不是塞进一个 `src/` 桶：

```text
core/        核心 —— 判定与响应生成的唯一实现
edge/        L1 数据平面 —— 四个接入适配器 + L1 处置
deception/   L2+L3 执行平面 —— 蜜罐、假 shell、网络策略（阶段 2/3）
analysis/    L4 分析平面（阶段 3）
console/     控制平面（阶段 2）
api/         跨语言契约（.proto + 生成的 stub）
```

好处：`go.mod` 在仓库根 → `go build ./...` / `go test ./...` / 编辑器全部零摩擦；目录名直接对应架构图上的哪一块。

为什么否决 `src/` 方案：见 [`known-issues.md`](known-issues.md) 的 K-2，以及 [`../background/decisions/0007-repo-layout.md`](../background/decisions/0007-repo-layout.md)。

---

## Q2 · 怎么保证适配器不会偷偷 import 核心内部代码？

**靠编译器，不靠人眼。** 进程内共享类型放在 `core/internal/contract/`，于是 Go 的 `internal` 目录规则生效：

> `core/internal/` 下的任何包，只有 `core/` 子树内的代码能 import。

`edge/mirror` 在 `core/` 之外，**编译期就无法**拿到核心内部类型 —— 它只能走 `api/` 的 proto stub。这条规则就是 `ST-3`，强制方式是编译器而不是 lint。

---

## Q3 · 怎么新增一个模块？

1. 先在 [`../design/modules.md`](../design/modules.md) §1.1 **加一行**（清单是权威）
2. 用 [`../modules/_template.md`](../modules/_template.md) 建 `docs/modules/<模块名>.md`（九章一节都不许删）
3. 建源码目录。Go 模块三个文件：`iface.go`（导出的 interface）+ `<模块名>.go` + `<模块名>_test.go`
4. 若属阶段 2/3：**先别写**，要先取得用户确认并更新阶段标记

---

## Q4 · 阶段 1 / 2 / 3 是什么意思？

| 阶段 | 范围 | 包含模块 |
| --- | --- | --- |
| **1（MVP）** | 只观察，不处置 | `judge` `session` `telemetry` `store` `control` `adapter-mirror` |
| 2 | 接管与处置 | `director` `responder` `isolation` `policy` `edge-injection` 四个适配器 `console` |
| 3 | 高交互与智能 | 蜜罐、假 shell、网络策略、L4 分析 |

阶段 1 的交付**只包含标记为 1 的模块**。提前实现阶段 2/3 要先改清单并确认。

---

## Q5 · 影子模式下为什么判定结果永远是「放行」？

因为**影子模式就是「只观测、不处置」**（首次上线必须如此）。`control.ShadowDecider` 照算判定 —— 分数用于后续校准阈值 —— 但输出恒为 `route_origin`。

阶段 2 由 `director` 替换它，才有真实的三值决策。

---

## Q6 · `store` 为什么拆成五个接口？

两个原因：

1. **Go 不支持方法重载** —— 一个结构体无法同时实现两个都有 `Get`/`Put` 的接口（见 K-4）
2. **单测替身要小** —— 拆开后，`judge` 的测试只需实现它用的那一两个方法；若是一个大接口，每个替身都得实现全部

---

## Q7 · 代码里为什么没有 `MD-20` 这种 ID 了？

ID 现在只出现在 **`docs/design/`** —— 它们是规则的引用句柄（要能被测试、ADR、CI 检查引用）。

代码与叙事性文档里改成**直接说做什么**：

```go
// 改掉：// store 是核心唯一的 I/O 出口（MD-20）。
// 保留：// store 是核心唯一的 I/O 出口：其他模块禁止直连 Redis / ClickHouse / PostgreSQL。
```

看文档时如果遇到 ID 看不懂：`AR` 在 `architecture.md`、`INT` 在 `integration.md`、`MD` 在 `modules.md`、`ST` 在 `structure.md`、`NI`/`OH`/`SB` 在 `constraints.md`、`TM` 在 `terminology.md`、`TB` 在 `language.md`。

---

## Q8 · 常用的开发命令？

```bash
make help       # 列出全部
make build      # 编译全部
make test       # 跑单测（含数据竞争检测）
make vet        # 静态检查
make generate   # 由 api/*.proto 生成 Go 代码
make run        # 本地起核心（影子模式）
```

## Q9 · 为什么所有请求的分数都是 0？

配置里的 `rules` 为空（或规则都没命中）。分数 = 命中规则权重之和，没有规则就恒为 0。
先看 `deploy/config/config.example.yaml` 的 `rules`；改完配置要 `scripts/shen.sh restart`（带 `--build` 不是必需，但配置是挂载进去的，需重启进程）。

## Q10 · 为什么控制台的「告警」永远是 0？

`severity` 档位**尚未定义**（设计里的未决项），核心当前一律输出 `none`；影子模式下也不会产出 `block`，
而控制台的告警口径是「`block` **或** `severity` 非 `none`」。所以告警恒为 0 是**当前设计状态**，不是漏报。
要看引擎判了什么，看「流量访问与流动」的**分值**与**命中信号**。

## Q11 · 为什么我改了代码，跑起来却没变化？

见 [`known-issues.md`](known-issues.md) `K-22`：`--force-recreate` 不重建镜像。用 `scripts/shen.sh restart`（带 `--build`）。

## Q12 · 为什么我把 SQLi / 穿越放在 URL 参数里，引擎完全不判？

查询串**不参与判定**（`path` 字段不含 query），而且规则按原始字符串匹配 ⇒ 编码一次也能绕过。
这是已登记的能力缺口（不是 bug）：见 [`known-issues.md`](known-issues.md) `K-23` 与
[`../ops/functional-verification.md`](../ops/functional-verification.md) §2。

## Q13 · 怎么加一条自己的验证场景？

在 [`../../scripts/traffic/scenarios.json`](../../scripts/traffic/scenarios.json) 里照格式加一条（`id` / `group` / `method` / `path` / `headers` / `expect`）。
先写成 `observe_only` 看引擎实际怎么判，再收紧成断言（`min_score` / `signals_all` / `action`）。
字段含义见 [`../../scripts/traffic/README.md`](../../scripts/traffic/README.md)。

## Q14 · 想看「还有哪些能力没做 / 做不到」，去哪？

[`../ops/functional-verification.md`](../ops/functional-verification.md)：§2 缺口清单（含怎么关）· §3 当前环境验不了的能力 ·
§4 运维陷阱 · §5 方法论边界。完成度看 [`../progress.md`](../progress.md)。

## Q15 · 流量调度图上为什么"判定=改道、实际落点=源站"？

因为**影子模式**（`INT-11`）：核心照算判定（意图可能是 `route_mirage`），但适配器**不执行**改道，仍把请求送到业务源站。
这是首次上线必须的状态。要看到真正的改道，需要：关掉影子 + 在 `honeypots[]` 里登记一个可用后端（否则执行会记为 `origin_fallback`，即 `NI-5` 回落，图上画成虚线）。
