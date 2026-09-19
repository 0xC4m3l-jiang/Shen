# 变更包：转发路径的边界行为实测锁定（升级 · 流式 · 大响应 · 大上传 · 协议版本）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 把上一轮列出的「转发未覆盖项」全部实测一遍，并把**实测到的行为**锁成断言（含一处被误判为 bug 的默认行为） |
| 日期 | 2026-09-19 |
| 状态 | 已验证（6 例集成测试全绿；`make gate` 通过；已提交） |
| 改动分级 | **M**（新增测试与文档；产品代码**零改动**） |
| 涉及模块 | `adapter-proxy`（[`../design/modules.md`](../design/modules.md) §1.1 第 11 行） |
| 决策数 | 已答 1 项（上游协议默认值：保持 `reverse_proxy` 默认，不加开关）/ 待定 0 |
| 关联 | [`2026-09-19-forwarding-deception-hardening.md`](2026-09-19-forwarding-deception-hardening.md)（上一轮：可见面卫生）· [`../log.md`](../log.md) |

---

## 1. 需求与验收

**要解决什么**：上一轮修掉响应头指纹后，明确列了「转发路径尚未复测」的四类边界：协议升级 · 流式 · 大响应 · 大上传，
以及 HTTP/3 未验证。这些正对应**「不影响原始业务」（`NI-1`）在最苛刻业务形态下是否成立** ——
一条坏了，业务就会出问题（而不是「看起来不对」）。

**做完之后**：这四类边界各有**实测断言**；h2/h1.1 的协议行为也写成断言（含一处曾被误判为 bug 的默认行为）。

**验收判据**：

1. **协议升级**：客户端拿到 `101`，且升级后仍能双向收发（说明 `Hijack` 在我们这层没被破坏）。
2. **流式**：首块到达时间显著早于「全部产完」——证明**没有全量缓冲**。
3. **大响应**（2 MiB HTML，跨过注入缓冲上限）：**不注入、不截断**，长度逐字节一致。
4. **大上传**（8 MiB）：上游收到完整字节数（证明我们**不读请求体**）。
5. **观测构造**：不含 body 内容（`INT-22` 的边界，也是「读了就会吃掉上游 body」的防线）。
6. **协议版本**：客户端↔我们 = HTTP/2；我们↔上游 = HTTP/1.1（默认行为，见 §2 决策）。
7. `make gate` 绿；本轮已提交。

**不做什么**：不加任何产品代码（本轮是**验证轮**）· 不为「上游 h2」加开关（见 §2）· HTTP/3 仍未实测（见 §7）。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | 上游协议默认 HTTP/1.1（实测发现）算 bug 吗？ | **不算，保持默认，不加开关** | 上游是业务或幻境后端，**服务端之间的协议版本对攻击者不可见**；`reverse_proxy` 默认 h1.1 与 nginx / Envoy 同类。加开关属「没有第二调用方的抽象」，还可能被误用 |
| ② | 流式怎么证「不是全量缓冲」？ | 用**首块到达时间**对比服务端产生间隔 | 「内容全对」不能证明没缓冲；只有时间能 |
| ③ | 大响应如何证「没被注入」？ | 配了注入规则 + 响应 2 MiB（> 1 MiB 缓冲上限），断言长度与片段 | 注入只改小 HTML 是**设计**（大响应流式透传），必须被锁住 |
| ④ | 大上传怎么证「没读 body」？ | 上游计数收到的字节数 | 我们在请求路径上，读 body 会把上游的 body 吃掉 |

---

## 3. 追溯矩阵

| 规则 ID | 模块文档 | 代码（测试） | 覆盖什么 | 验证命令 |
| --- | --- | --- | --- | --- |
| `NI-1`（不影响原始业务） | [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) §6 | `forwarding_test.go` 全 6 例 | 升级 / 流式 / 大响应 / 大上传 四条业务形态 | `make gate` |
| `INT-8`（只改改道侧响应） | 同上 §1 | `TestLargeResponseIsNotInjectedOrTruncated` | 大响应不注入；注入只作用于小 HTML | `make gate` |
| `INT-22`（TLS 可读才允许误导处置）· 边界 | 同上 §1 | `TestObservationDoesNotCarryBody` | 观测**不含** body（我们不读它） | `make gate` |
| `AR-29`（延迟预算） | 同上 §4 | `TestStreamingResponseIsNotBuffered` | 流式不被缓冲（延迟的直接体现） | `make gate` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `edge/proxy/forwarding_test.go` | **新增** | 6 例集成测试：WebSocket 升级 · SSE 流式 · 2 MiB 响应 · 8 MiB 上传 · 观测无 body · h2/h1.1 |
| [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) | 改 | §1 增「协议与流式的边界行为」（**实测锁定**的措辞）· §7 增测试行 · §9 增变更记录 |

**产品代码改动 0 行** —— 本轮只把「已经成立的行为」变成「被断言的行为」。

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | WebSocket 升级（真 raw TCP） | 101 + 升级后双向收发 | ✅ | `TestWebSocketUpgradePassesThrough` |
| 2 | SSE 流式（3 块 × 150ms 间隔） | 首块 < 2×间隔（证明未缓冲） | ✅ 0.45s 全程 | `TestStreamingResponseIsNotBuffered` |
| 3 | 2 MiB HTML（配了注入规则） | 长度一致且**无注入片段** | ✅ | `TestLargeResponseIsNotInjectedOrTruncated` |
| 4 | 8 MiB 上传 | 上游收到 8 MiB | ✅ | `TestLargeUploadReachesUpstreamIntact` |
| 5 | 观测构造 | 不含 body 内容 | ✅ | `TestObservationDoesNotCarryBody` |
| 6 | 协议版本 | 客户端 h2 / 上游 h1.1 | ✅ | `TestClientUsesHTTP2UpstreamUsesHTTP11` |

**没有覆盖的**：

- **HTTP/3（QUIC）**：需要引入 QUIC 客户端依赖才能测；本轮未做（已登记 §7）。风险低：h3 只是同一套路由的另一个监听协议，且 Caddy 默认启用；
- **WebSocket over HTTP/2（RFC 8441）**：测的是经典 h1.1 升级路径（覆盖绝大多数真实场景）；
- **上游要求 h2 的罕见后端**：不加开关（见 §2 ①），需要时再加。

---

## 6. 验证证据

```console
$ go test ./edge/proxy/ -run 'WebSocket|Streaming|Large|Observation|ClientUsesHTTP2' -count=1 -v
--- PASS: TestWebSocketUpgradePassesThrough (0.00s)
--- PASS: TestStreamingResponseIsNotBuffered (0.45s)
--- PASS: TestLargeResponseIsNotInjectedOrTruncated (0.00s)
--- PASS: TestLargeUploadReachesUpstreamIntact (0.01s)
--- PASS: TestObservationDoesNotCarryBody (0.00s)
--- PASS: TestClientUsesHTTP2UpstreamUsesHTTP11 (0.08s)
ok  shen/edge/proxy

$ make gate
门禁通过。
```

**关键指标**：新增集成测试 **6 例**（`edge/proxy` 43 → 49）· 产品代码改动 **0 行** · 修掉我自己造的两处占位/类型错误（`judgev1OriginResponse` 占位、`%d` 传字符串）。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | HTTP/3 未实测 | h3 客户端路径未验证 | 需要时引入 QUIC 客户端测；风险低（同一路由的另一协议） |
| 2 | WebSocket over h2（RFC 8441）未测 | 少见 | 同上 |
| 3 | 上游要求 h2 的后端 | 目前无法表达 | 需要时给传输层加显式开关 |
| 4 | ⚠️ **`E2` 的「TLS 终结归属」仍待你裁决**（上一轮提出） | 关系 `A2` / `ADR-0017` | 你裁决后开 ADR 重估 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 新测试文件里留了一个占位函数 `judgev1OriginResponse`（编译红） | 自伤 | `go vet` 报 undefined | 删占位、测试改为纯函数断言 | ✅ |
| 2 | 用 `%d` 格式化 `freePort` 返回的字符串（编译红） | 自伤 | `go vet` 报 format 类型不符 | 改为字符串拼接 | ✅ |
| 3 | 初版测试把「上游看到 h1.1」当成失败 | 误判 | 测试失败输出 `proto=HTTP/1.1` | 核实为 `reverse_proxy` 默认行为 → 改为**断言并文档化**，不加无谓开关 | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 转发边界实测锁定：WebSocket 升级 · SSE 流式不被缓冲 · 2 MiB 响应不注入不截断 · 8 MiB 上传完整送达 · 观测不含 body · h2 下行 + h1.1 上行；新增 6 例集成测试，产品代码零改动 | 用户「继续开发」· 上轮遗留清单 · `NI-1` · `INT-8` · `INT-22` · `AR-29` |
