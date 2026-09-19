# 变更包：降低 Caddy 耦合的可见性 —— 耦合面自动提取 + 升级兼容锁

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 让「Caddy 升级」变成**可核、可回退、不会静默坏**的事：耦合面自动提取（`make caddy-surface`）+ 四类假设的兼容锁（`caddy_compat_test.go`）+ 升级检查表（模块文档 §3.1） |
| 日期 | 2026-09-19 |
| 状态 | 已验证（兼容锁 1 例含 4 条断言全绿；`make caddy-surface` 可用；`make gate` 通过；已提交） |
| 改动分级 | **M**（新增测试、Makefile 目标与文档；产品代码零改动） |
| 涉及模块 | `adapter-proxy`（[`../design/modules.md`](../design/modules.md) §1.1 第 11 行） |
| 决策数 | 已答 2 项（耦合面做成**自动提取**而非手写文档；三类静默风险用测试锁）/ **待定 1 项**（是否进一步把 Caddy 绑定收进更小边界，见 §7） |
| 关联 | [ADR-0017](../background/decisions/0017-caddy-l1-base.md)（内嵌 Caddy 作 L1 底座）· [`2026-09-19-forwarding-deception-hardening.md`](2026-09-19-forwarding-deception-hardening.md)（`OH-2` 清洗依赖默认 `Server` 值） |

---

## 1. 需求与验收

**要解决什么**（用户要求）：*「Caddy 的引入不要太耦合，方便将来更快升级，并保证功能支持」*。

**做完之后**：升级前一条命令看清耦合面；升级后 `make gate` 会把**会静默失效的假设**直接报出来（而不是等生产上发现指纹漏了）。

**验收判据**：

1. `make caddy-surface` 从代码**自动提取**：引用的包 · 用到的导出符号 · 以字符串引用的模块 ID（不手写、不会腐烂）。
2. **四类不会编译失败的依赖**被测试锁住：默认 `Server` 头值 · `caddy.Duration` 单位 · 三个模块 ID 仍在注册表中。
3. 编译期断言：`Handler` 继续满足 Caddy 的四类契约（`Module` / `Provisioner` / `Validator` / `CleanerUpper`）+ `caddyhttp.MiddlewareHandler`。
4. 升级流程写成**四条命令**（surface → get → gate → bench → dev）并写明「禁止只跑 go build」的理由。
5. `make gate` 绿；本轮已提交。

**不做什么**：不改产品代码（本轮只加安全网与可见性）· 不做「把 `handler.go` 拆成外壳 + 纯逻辑」的重构（列为可选增强，见 §7）。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | 耦合面写进文档还是做成命令？ | **做成命令**（`make caddy-surface`） | 手写清单一定会腐烂；从代码提取的结果永远是真的 |
| ② | 只加编译期断言够不够？ | **不够**：必须锁「不会编译失败」的三类 | 模块 ID 字符串 · 默认 `Server` 头值 · `Duration` 单位 —— 升级后照样编译、照样启动，**行为却变了** |
| ③ | 为什么不顺手把耦合面拆小？ | 本轮**只加安全网**，拆分另议 | 「降耦」有两种解读：**可见/可控**（本轮）与**物理拆分**（改结构）；后者需要单独一轮并承担回归风险 |

---

## 3. 追溯矩阵

| 规则 ID | 文档位置 | 代码 | 测试 | 验证命令 |
| --- | --- | --- | --- | --- |
| `AR-3`（L0 复用现成组件） | [`../design/architecture.md`](../design/architecture.md) | 内嵌 Caddy（`embed.go`） | —— | `make gate` |
| `OH-2`（对手可见面不得留栈指纹） | [`../design/constraints.md`](../design/constraints.md) | `handler.go` 的 `headerSanitizer` | `TestCaddyAssumptionsStillHold` 的 ①（默认 `Server` 值） | `make gate` |
| `TB-15`（CI 必须能挡住回归） | [`../design/language.md`](../design/language.md) | `Makefile` 的 `gate` | 上述全部 | `make gate` |
| `TB-16`（依赖经许可审计） | 同上 | `docs/spec/dependencies.md`（生成物） | `make licensecheck` | `make gate` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `edge/proxy/caddy_compat_test.go` | **新增** | 编译期断言（五条契约）+ 四类假设断言（默认 `Server` 值 / `Duration` 单位 / 三个模块 ID） |
| `Makefile` | 改 | 新增 `make caddy-surface`（自动提取耦合面） |
| [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) | 改 | 新增 **§3.1 与 Caddy 的耦合面与升级检查表**（含「禁止只跑 go build」的理由） |
| [`../background/decisions/0017-caddy-l1-base.md`](../background/decisions/0017-caddy-l1-base.md) | 改 | 「未解决」增升级检查表定位 + 「是否进一步收窄绑定」列作可选增强 |

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | 默认 `Server` 头值 | 仍为 `Caddy`（否则 `OH-2` 清洗静默失效） | ✅ | `TestCaddyAssumptionsStillHold` |
| 2 | `caddy.Duration` 单位 | `time.Second` 往返一致 | ✅ | 同上 |
| 3 | 三个模块 ID | `http.handlers.{headers,reverse_proxy,shen_proxy}` 均在注册表 | ✅ | 同上 |
| 4 | Handler 契约 | 五条编译期断言 | ✅ | `edge/proxy/caddy_compat_test.go` 顶部 |
| 5 | 耦合面提取 | 命令列出包/符号/模块 ID | ✅ | `make caddy-surface` |

**没有覆盖的**：Caddy 内部行为（如 `ResponseHeaderTimeout` 的精确语义）· 上游字段顺序 —— 这两类只能靠既有集成测试覆盖（`make gate`）。

---

## 6. 验证证据

```console
$ make caddy-surface
我们对 Caddy 的依赖面（自动从代码提取）：
  · 引用的包： github.com/caddyserver/caddy/v2 · /caddyconfig · /modules/caddyhttp · /modules/caddyhttp/reverseproxy · /modules/caddytls · /modules/standard
  · 用到的导出符号： caddy.AdminConfig · caddy.CleanerUpper · caddy.Config · caddy.ConfigSettings · caddy.Context · caddy.Duration · caddy.GetModule · …
  · 以字符串引用的模块 ID： "http.handlers.headers" · "http.handlers.reverse_proxy" · "http.handlers.shen_proxy"

$ go test ./edge/proxy/ -run CaddyAssumptions -count=1
--- PASS: TestCaddyAssumptionsStillHold (0.00s)

$ make gate
门禁通过。
```

**关键指标**：新增兼容断言 **4 条**（+ 5 条编译期）· 新增 Make 目标 **1 个** · 产品代码改动 **0 行** · 升级流程从「靠人记」变成「四条命令 + 测试兜底」。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | **未做物理降耦**：`handler.go` 仍同时含「Caddy 模块契约」与「判定胶水/路由逻辑」；`policy.go` 仍出现 `caddyhttp.MiddlewareHandler` | 单文件变大，但耦合面**已可枚举且有测试兜底** | 可选增强：拆 `caddy_module.go`（外壳）+ 纯逻辑文件；需单独一轮承担回归风险 |
| 2 | 上游字段顺序 / `ResponseHeaderTimeout` 语义未单独锁 | 少数行为只能靠集成测试 | 需要时补 |
| 3 | ⚠️ **`E2` 的「TLS 终结归属」仍待你裁决** | 关系 `A2` / `ADR-0017` | 你裁决后开 ADR 重估 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 耦合面此前**没有任何清单**，升级只能靠通读代码 | 基建缺口 | `grep -rln caddyserver/caddy` 只有零散结果 | 加 `make caddy-surface`（从代码提取）+ 模块文档 §3.1 | ✅ |
| 2 | 我们对 Caddy 的四类依赖**不会造成编译失败**（模块 ID 字符串 / 默认 `Server` 值 / `Duration` 单位 / —— 前三类） | 静默风险 | 升级场景推演 | 写成断言（失败信息里直接写「该改哪里」） | ✅ |
| 3 | `handler.go` 与 `policy.go` 仍混着 Caddy 类型 | 技术债（已登记） | `grep caddyserver edge/proxy/*.go` | 列为可选增强（§7 第 1 条），本轮不做以免与安全网混在一起 | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 降低 Caddy 耦合的可见性：`make caddy-surface` 自动提取耦合面 · `caddy_compat_test.go` 锁四类静默风险 + 五条编译期断言 · 模块文档新增 §3.1 升级检查表（四条命令，禁止只跑 go build）· ADR-0017 补升级视角 | 用户要求（升级要快、不得静默坏）· `AR-3` · `OH-2` · `TB-15` |
