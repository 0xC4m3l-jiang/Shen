# 变更包：观测面推送（核心事件流 + SSE）—— 从「轮询」改成「记到即推」

| 项 | 值 |
| --- | --- |
| 主题 | 核心**记录事件的那一刻**把事件推给管控平台：gRPC 服务端流 `WatchEvents` → 控制台 `GET /api/stream`（SSE）→ 页面**增量更新**；**删掉**「每 5 秒整块轮询」 |
| 日期 | 2026-09-21 |
| 状态 | 已实现 |
| 涉及模块 | `telemetry`（核心事件管道，[`../design/modules.md`](../design/modules.md) §1.1 第 7 行）· `control`（第 22 行）· `console`（第 21 行）· 契约 `api/telemetry/v1` |
| 决策数 | 已答 4 项（用户 Q1-A / Q2-A / Q3-A / Q4-A）/ 待定 4 项（见 ADR-0027 未解决） |
| 关联 | [ADR-0027](../background/decisions/0027-observability-push.md)（新）· [ADR-0018](../background/decisions/0018-policy-plane-pull-model.md)（**边界**：策略面仍 Pull）· 上一轮 [`2026-09-21-console-freshness-and-doc-marks.md`](2026-09-21-console-freshness-and-doc-marks.md) |

---

## 1. 需求与验收

**要解决什么**（一句话）：控制台的时效下限就是它的**轮询间隔**（5 秒）+ 还要有人盯着；
用户要的是「**监控到了、并且记录了，就通知或者主动更新到管控平台**」。

**做完之后能做什么**：

1. 事件**落库那一刻**就出现在页面上（实测端到端 **3 ms**，原来最坏 5 秒 + 人工点击）；
2. 告警实时计数并插入告警表；
3. **断线重连会补漏**（带 `since`），不静默丢数据；
4. **丢包可见**：订阅者跟不上时核心丢最旧并计数，页面显示「累计丢弃 N 条」。

**验收判据**（可验证）：

1. 端到端延迟实测 **< 1 秒**（实测 3 ms，回环，含一次 gRPC 往返）；
2. 带 `since` 连上能补到历史（实测补到 3 条）；
3. **慢订阅者拖不死上报路径**：单测证明 `Publish` 在订阅者完全不读时不阻塞，且丢包被计数；
4. **幂等命中不重复推**（同 `event_id` 第二次上报不产生第二条流消息）；
5. 并发 `Publish`×`Close` 在 `-race` 下无 panic（关闭在写锁内 ⇒ 结构上不可能向已关闭 channel 发送）；
6. `make gate` 通过（含 `make trace`）。

**不做什么**：

- **不动策略面**：`policy.Watch` 保持 `Unimplemented`（ADR-0018 的决策不变，理由见 ADR-0027 背景）；
- **不引依赖**：gRPC 流用已有库、SSE 用标准库 `http.Flusher`、页面用原生 `EventSource`；
- **不做外部通知渠道**（桌面通知 / 邮件 / webhook）：本轮只做**页内**高亮与计数（ADR-0027 未解决 4）；
- **不解决多副本聚合**：控制台只连一个核心 ⇒ 只看到那一个副本的存储（**既有**边界，本轮显式写下）。

---

## 2. 设计逻辑

**已确认的决策**（用户 2026-09-21 四问四答）：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 浏览器侧推送方式 | **SSE**（`EventSource`） | 零依赖（浏览器原生 + Go `http.Flusher`）；我们要的只是**单向推**，WebSocket 要握手/心跳/引库 | `console/cmd/console/main.go::handleStream` |
| ② | 推什么 | **全量事件流 + 告警单独高亮** | 只推告警会让表格仍靠轮询；只推全量又没人被「提醒」 | 页面 `onFrame`：事件插入 + `isAlert` 命中则进告警表并计数 |
| ③ | 刷新节奏 | **混合**：事件驱动增量 + **30 秒**整块对账 | 增量只负责及时性，本地计数久了会漂 ⇒ 用整块刷新**纠偏** | 页面 `openStream()` + `setInterval(load, 30000)` |
| ④ | 断线补漏上限 | `since` 补最近 **500** 条（上限 5000）并在页面标注 | 不补会**静默**丢数据；不设上限会一次灌爆客户端 | `WatchEventsRequest.catch_up_limit` |

**三条不可破的纪律**（写进契约 §1.5 与 ADR-0027 决定 2）：

1. **旁路** —— 推送在落库**之后**；投递失败**不得**影响写入路径（`NI-1` / `AR-6`）。实现：`Collector.ReportBatch` 只推**真正写入**的事件；`Hub.Publish` 不返回错误。
2. **无背压 + 丢包可见** —— 每订阅者有界 channel（默认 256 / 上限 4096），满则**丢最旧**并计数；`WatchStatus` 只在变化时发。**不可见的丢包会让页面把「漏了」读成「没发生」**。
3. **可补漏** —— `since` 非空先补历史再转流。**顺序必须是「先订阅、再补漏」**：反了的话，补漏那段时间新产生的事件会永久丢失（读历史读不到未来的）。代价是历史与缓冲可能重叠 ⇒ 用 `event_id` 去重。

**仍未定**：见 ADR-0027 未解决 1–4（多副本聚合 · 控制台鉴权 · 缺口大小上报 · 外部通知渠道）。

**接缝与接口**：

```text
api/telemetry/v1: WatchEvents(WatchEventsRequest) → stream WatchEvent{event|status}
        ▲
core/internal/telemetry.Hub          ── 有界广播（丢最旧 + 计数 + 永不阻塞）
        ▲ Collector.ReportBatch 落库后 Publish(只推写入成功的)
core/internal/control.WatchEvents    ── 先订阅 → 补漏 → 转流（含状态帧）
        ▲ gRPC 流
console GET /api/stream              ── SSE 转发（复用 viewOf 解码，帧形状见契约 §2.1）
        ▲ EventSource
console/web/index.html               ── 增量插入 + 告警高亮 + 30 秒对账 + DAG 去抖 1 秒重取
```

---

## 3. 文档对应（追溯矩阵）

| 规则 ID | 文档章节 | 代码 | 测试 / 证据 | 验证命令 |
| --- | --- | --- | --- | --- |
| `AR-6` | [`../spec/console-api.md`](../spec/console-api.md) §1.5（纪律①旁路） | `core/internal/telemetry/telemetry.go`（`Publish` 在选写成功后） | `TestCollector_ReportsWithoutPublisher`（无推送也照常上报） | `go test ./core/internal/telemetry/` |
| `NI-1` | `console-api.md` §0（控制台可挂）· ADR-0027 纪律① | 同上；`Hub.Publish` 不返错 | 单测 + 端到端 | `make gate` |
| `MD-10`（缓存必须上限与失效） | `console-api.md` §1.5 · ADR-0027 纪律② | `core/internal/telemetry/hub.go`（容量上限 + 丢最旧 + 计数） | `TestHub_PublishNeverBlocksAndDropsOldest` | `go test -race ./core/internal/telemetry/` |
| `AR-10` | `console-api.md` §0 · 页面 `isAlert` | `console/web/index.html`（只显示，不改处置） | 代码审查 | 人工 |
| `AR-11`（幂等） | `console-api.md` §1.5（幂等命中不推） | `Collector.ReportBatch` | `TestCollector_DoesNotPublishDuplicates` | 同上 |
| `MD-3` | `console-api.md` §2.1 / §3（帧形状与改键规矩） | —— | `make trace` | `make trace` |
| `MD-18` | 不新增模块 | 广播落在已有 `telemetry` 模块 | `make archcheck` | `make archcheck` |
| `DEV-1` / `DEV-2` | 本文件 · [`../log.md`](../log.md) | —— | 形状检查 | `make trace` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `api/telemetry/v1/telemetry.proto` | 修改 | 加 `WatchEvents` 流 + `WatchEventsRequest` / `WatchEvent`（`oneof event/status`）/ `WatchStatus` |
| `api/telemetry/v1/*.pb.go` | **重新生成** | 契约唯一事实源是 `.proto`（`make generate`） |
| `core/internal/telemetry/hub.go` | **新增** | 有界广播：`Hub` + `Subscription`；**丢最旧 + 计数 + 永不阻塞**；`Close` 在写锁内（正确性条件） |
| `core/internal/telemetry/iface.go` | 修改 | 新增 `Publisher` 端口（依赖方定义接口） |
| `core/internal/telemetry/telemetry.go` | 修改 | `New(sink, pub)`；`ReportBatch` 落库后推**真正写入**的事件 |
| `core/internal/telemetry/hub_test.go` | **新增** | 三条纪律 + 幂等不重推 + 并发 Publish×Close（`-race`） |
| `core/internal/control/telemetry.go` | 修改 | `WithEventHub` + `WatchEvents`（**先订阅→补漏→转流**）+ `catchUp` + `wantedType` |
| `core/cmd/core/main.go` | 修改 | 建 `eventHub` 并同时交给采集器与服务面 |
| `console/cmd/console/main.go` | 修改 | `GET /api/stream`（SSE）+ `viewOf`（拉/推共用解码）+ `sseFrame` + `streamEndReason` |
| `console/web/index.html` | 修改 | `EventSource` + 增量插入 + 告警高亮 + **30 秒对账** + DAG **去抖 1 秒**重取；**删除** 5 秒轮询与开关；列定义抽成三处共用函数 |
| `docs/spec/console-api.md` | 修改 | §1.5 `WatchEvents` · §2 加 `/api/stream` · **§2.1 页面如何消费流** · §3 改键规矩补帧形状 |
| `docs/background/decisions/0027-observability-push.md` | **新增** | 决策记录（含与 ADR-0018 的边界、三条纪律、失效条件） |
| `docs/background/decisions/README.md` · `docs/modules/console.md` · `docs/kb/capabilities.md` | 修改 | 索引 / 接口数 10→**11** / 测试表 / 能力表 |
| `docs/plans/2026-09-21-observability-push.md` | 新增 | 本文件 |
| `docs/log.md` | 修改 | 本轮变更日志条目 |

**关键类型与函数**：

- **导出契约**（跨进程）：`telemetry.v1.WatchEvents` / `WatchEvent` / `WatchStatus` · 控制台 `GET /api/stream`（帧形状以契约 §2.1 为准）；
- **进程内**：`telemetry.Hub` / `Subscription` / `Publisher`（端口）· `control.WithEventHub`；
- **内部细节**：`Subscription.offer`（丢最旧的那段 `select`）· `catchUp` · `viewOf` · 页面 `eventCells`/`flowCells`/`alertCells`。

**必须遵守的上位约束**：`AR-6` · `AR-10` · `AR-11` · `NI-1` · `MD-10` · `MD-18` · `TB-14`（错误显式返回）。

---

## 5. 测试与场景

| # | 场景 | 输入 / 前置 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 1 | **时效**：核心记录 → 控制台收到 | 起核心 + 控制台；连上 SSE 后注入 3 条**唯一 id** 事件 | 秒级以内 | ✅ **3 ms**（min=中位=max） | §6 |
| 2 | **补漏**：带 `since` 重连 | 先注入事件，再带 `since=now-60s` 连 | 补到历史 | ✅ 补到 **3 条** + 1 状态帧 | §6 |
| 3 | **不阻塞上报路径** | 订阅者完全不读；`Publish` 1000 次（容量 2） | `Publish` 立即返回；丢包计数 > 0；留下的是**最后两条** | ✅ | `TestHub_PublishNeverBlocksAndDropsOldest` |
| 4 | 无订阅者零成本 | 无订阅者时 `Publish` | 不 panic、不阻塞 | ✅ | `TestHub_PublishWithoutSubscribersIsNoop` |
| 5 | 并发 Publish×Close 不 panic | 4 个发布者 vs 100 次订阅/关闭 | `-race` 下干净 | ✅ | `TestHub_ConcurrentPublishAndClose`（`-race`，3 次） |
| 6 | **幂等命中不重推** | 同一 `event_id` 上报两次 | 流上只出现一次 | ✅ | `TestCollector_DoesNotPublishDuplicates` |
| 7 | 无推送时上报照常 | `pub = nil` | `Accepted=1` | ✅ | `TestCollector_ReportsWithoutPublisher` |
| 8 | 未装配推送 ⇒ 不挂住 | 核心无 Hub | `Unimplemented` | ✅ | 与 `policy.Watch` 同一约定（代码审查） |
| 9 | 页面删除旧机制 | `grep` 5 秒轮询与开关 | 0 命中 | ✅ | §6 |
| 10 | 门禁 | `make gate` | 全绿 | ✅ | §6 |

**没有覆盖的**：

- **没有浏览器自动化**：SSE 的浏览器侧（`EventSource` 回调、增量插入、去抖）**没有真实浏览器验证**，靠代码审查 + 契约对齐；
- **没有测半开连接**（拔网线那种）：`since` 补漏能兜住「重连后」，但「连接假装还活着」的场景未构造；
- **没有压测 fan-out**：订阅者数、事件速率的上限未测（ADR-0027 失效条件 1 已登记）；
- **多副本未验**（只有单核心）。

---

## 6. 验证证据

**时效实测**（唯一 id 事件，避开幂等命中）：

```console
$ python3 /tmp/ai-probe/stream-check2.py <console> <core>
① 补漏：事件 3 条 · 状态帧 1
② 连上后注入唯一事件 → 量「记录 → 看见」延迟
  收到 3 条 · 延迟 最小 3ms · 中位 3ms · 最大 3ms
  样例：[('3ms', 'decision'), ('3ms', 'decision'), ('3ms', 'decision')]

$ grep 观测面订阅 <控制台日志>
console: 观测面订阅已建立（since="2026-09-21T05:02:47.013626Z"）
console: 观测面订阅已建立（since=""）
```

> **第 ① 条的数字说明**：延迟 = 收到时刻 − 事件的 `created_at`，**含**一次 gRPC 往返（注入 → 核心落库）
> 与一次 SSE 转发。即「核心记录到页面看见」的端到端口径。对照：原来的轮询是 **≤5 秒 + 人工点击**。

**一次「失败」的实测记录**（值得留档）：第一次用 `devcheck` 造事件时**一条都没收到** ——
原因是它三次请求完全相同 ⇒ 决策 id 相同 ⇒ **幂等命中**（`AR-11`）⇒ 没写库 ⇒ **按设计也不推**。
改用唯一 id 后立刻收到。这恰好从反面证明了纪律①（只推真正写入的）。

**单测与门禁**：

```console
$ go test -race ./core/internal/telemetry/ -v | grep -E "^--- "
--- PASS: TestHub_PublishNeverBlocksAndDropsOldest
--- PASS: TestHub_PublishWithoutSubscribersIsNoop
--- PASS: TestHub_ConcurrentPublishAndClose
--- PASS: TestCollector_DoesNotPublishDuplicates
--- PASS: TestCollector_ReportsWithoutPublisher
（-count=3 亦通过）

$ grep -nE 'id="auto"|setInterval\(load, 5000\)' console/web/index.html
（0 命中 —— 旧的 5 秒轮询与开关已删净）

$ make gate
All checks passed!（ruff）
架构检查通过。
追溯检查通过。
泄漏检查通过。
许可审计通过：没有传染性或限制性许可。
80 passed in 0.49s
门禁通过。
```

**一次真实的失败并修复**：`make trace` 报出变更包里引用了**一个不存在的规则 ID**（我写错了一位数字）——
已改正为 `MD-10`；这也正是门禁存在的意义。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | **多副本聚合**（控制台只连一个核心） | 多副本时视图不完整 | ADR-0027 未解决 1（与既有 `ListEvents` 同一边界） |
| 2 | **控制台无鉴权** | 流会把全部事件持续推给任何连上的人 | **绑非回环前必须先解决**（ADR-0020 未解决项 + ADR-0027 未解决 2） |
| 3 | 断线很久后「缺口多大」未单独上报 | 只显示「累计丢弃」，不显示「补漏可能不全」 | ADR-0027 未解决 3 |
| 4 | 外部通知渠道（桌面/邮件/webhook）未做 | 「通知」只到页内 | ADR-0027 未解决 4（要另定渠道与隐私口径） |
| 5 | 页面无浏览器自动化 | 增量插入/去抖/重连的用户侧行为靠审查 | 与 ADR-0020 失效条件 1 一起评估 |
| 6 | fan-out 未压测 | 订阅者多时成本未知 | ADR-0027 失效条件 1 |

---

## 7.1 审视记录（本轮含**删除**动作 → 逐条走 `audit` 的四道门槛）

四道门槛：证明无引用 · 非对外契约 · 非历史记录 · 记录在案。

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 页面的「每 5 秒自动刷新」开关 + `setInterval(load, 5000)` + 其 handler | **冗余**（被更快的机制取代） | 新增 SSE 后它只是「更慢且更耗」的第二条路径 | **删除**（三处：checkbox 的 HTML、`onchange` handler、interval） | `grep` 0 命中 |
| 2 | `Hub.remove`（独立的注销函数） | **不必要的间接** | 关闭 channel 必须与投递互斥（写锁/读锁）——拆成两个函数反而更容易被误用 | **删除**，合并进 `Close`（并在注释里写明这是**正确性条件**而非防御） | 并发测试 `-race` 通过 |
| 3 | `load()` 里三处**内联**的表格行构造 | **重复**（实时插入要再写一份） | 列定义写两份必然漂（改一列只改一处） | 抽成 `eventCells` / `flowCells` / `alertCells`，整块刷新与实时插入**共用** | 单一列定义 |
| 4 | `fetchType` 里内联的事件解码 | **重复**（SSE 要再写一份） | 同一条事件在两处解成不同字段是必然结局 | 抽成 `viewOf`，拉/推共用 | 单一解码 |
| 5 | DAG 段注释「每 5 秒随页面刷新 → 动态」 | **注释过期** | 新机制下不再有 5 秒轮询 | 改为「新事件到达 → 去抖 1 秒重取」 | 已修 |
| 6 | **DAG 的实效倒退**：改 30 秒对账后，图从「5 秒」变成「30 秒」 | 新引入的退化（自己引入自己抓） | 全页只有 DAG 需要 join，单条事件拼不出来 | 加 `scheduleDagReload()`：事件驱动 + **去抖 1 秒** | 比原来更快 |
| 7 | `telemetry.New` 的签名 | **不加第二套 API** | 若保留 `New(sink)` 再加 `NewWithPublisher` 就是两个构造器一个用途 | 直接改签名（调用点只有 1 个生产 + 2 个测试） | 单一构造器 |
| 8 | 本轮有没有改到 `docs/design/`？ | 越界核查 | `git diff --name-only docs/design/` | **无** | 未越界 |
| 9 | 策略面的 `Watch` 是否被顺手改了？ | 越界核查（ADR-0018） | `git diff core/internal/policy/server.go` | **无** —— 它仍返回 `Unimplemented` | 边界守住 |

> **排除项**：`docs/background/` · `docs/plans/` · `docs/log.md` · `docs/kb/` 的历史记录部分不在审视范围内。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-21 | 首版：观测面推送（`WatchEvents` 流 + SSE + 页面增量 + 30 秒对账）· 三条纪律（旁路 / 无背压且丢包可见 / 可补漏）· 删除 5 秒轮询与开关 | 用户「不是轮询刷新，而是记到就通知/主动更新」；Q1-A / Q2-A / Q3-A / Q4-A |
