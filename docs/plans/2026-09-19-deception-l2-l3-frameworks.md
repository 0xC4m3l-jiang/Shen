# 变更包：L2 第一批 —— `honeypot-protocol` 框架落地

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 实现 `honeypot-protocol`（L2 协议仿真）的**契约与运行框架**：注册表 · 连接上限 · 对称回收 · 会话录制口 · 一个最小真实适配器；**真实协议栈留作核心逻辑接缝** |
| 日期 | 2026-09-19 |
| 状态 | 已验证（7 例单测全绿；`make gate` 通过；已提交） |
| 改动分级 | **L**（新增一个包与两个跨进程契约：`Protocol` / `SessionFactory`） |
| 涉及模块 | `honeypot-protocol`（[`../design/modules.md`](../design/modules.md) §1.1 第 14 行，阶段 3） |
| 决策数 | 复用已批准的 `Q2(a)` / `Q4(a)`（框架 + seam；L2/L3 只做契约与注册，不实现协议栈）/ 待定 0 |
| 关联 | [ADR-0011](../background/decisions/0011-honeypot-entry-external-backends.md)（可选自研）· [`../log.md`](../log.md) · 本批剩余三项见 §7 |

---

## 1. 需求与验收

**要解决什么**：`honeypot-protocol` 是 `deception/` 下的第一个模块，此前只有设计文档、没有任何代码；
而它的「框架部分」（注册、并发上限、资源回收、会话录制）是**确定性**的、可以现在就做完，
剩下「真实协议栈」才是需要专门设计的核心逻辑。

**做完之后**：谁要加一个协议（SSH / MySQL / Redis / FTP…），只需实现 `Protocol`（`Name` / `DefaultPort` / `Serve`）、
`Register` 进注册表、用 `Runner.Start` 起监听 —— 并发上限、超限拒绝与计数、资源回收、会话 ID 与转录上行**都是现成的**。

**验收判据**：

1. 契约：`Protocol`（适配器）· `Session`（会话录制口，会话 ID 一等字段 `AR-25`）· `SessionFactory`（消费方提供）· `Registry`。
2. 运行框架：`Runner.Start/Stop/Addr/Stats`；**并发上限必须存在且超限即拒并计数**（`MD-16`）；
   **停止时对称回收**：停收新连接 → 等在途（宽限）→ 强制关闭 → 等退出（`MD-15`）。
3. **不主动连接任何目标**（`SB-6`）：本模块没有任何出站拨号（结构上只有 `net.Listen` 与已接受的 `net.Conn`）。
4. 最小真实适配器：`Banner`（问候 + 双向录制），使框架可跑通、可被测。
5. 「核心逻辑未实现」必须**显式报错**（`NotImplemented`），不静默返回空结果。
6. 7 例单测覆盖：注册表三态 · banner 双向录制 · 超限拒绝与计数 · `Stop` 对称性与监听关闭 · 重复启动/未知协议 · 未实现错误。
7. `make gate` 全绿；本轮已提交。

**不做什么**：

- **不实现真实协议栈**（SSH 密钥交换 / MySQL 握手 / Redis RESP / 凭证捕获与命令解释）—— 那是**核心逻辑接缝**（已批准 `Q4(a)`）；
- **不实现 `honeypot-shell`**（命令表 / 内存 FS / 水印）· **不做 `netpolicy`** · **不动 `adapter-dns`** —— 属本批剩余三项，见 §7；
- **不做 `MD-14` 的进程组回收** —— 那条规则针对**子进程形态**的蜜罐；本框架是进程内适配器，能做到的上限是「关连接 + 等宽限」，已在文档里写明而非假装做到；
- 不改任何已确认规则、不改 `docs/design/` 的规则正文。

---

## 2. 设计逻辑

**已确认的决策**（来源：本轮之前的 ① 访谈，用户「按推荐」全部批准）：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | L2/L3 的实现边界（`Q4(a)`） | **只做契约与注册**，不实现协议栈 / 内核策略 | `ADR-0011`：默认接第三方蜜罐；自研协议栈属另一项决策 | `deception/honeypot/iface.go` |
| ② | 「完整」的判定（`Q2(a)`） | 契约 + 确定性框架 + 单测 + **核心逻辑留 seam** | 正是「只留下后续要设计的核心逻辑」；避免把「能力判断核心」（用户已推迟的 `D8`）提前定死 | 同上 + `errors.go` 的 `NotImplemented` |
| ③ | seam 的失败姿态（`Q5(a)`） | **fail-loud**：显式返回「尚未实现」错误 | 静默返回空结果会让人以为「在跑、只是没抓到东西」；蜜罐面在请求路径之外，失败不影响业务（`NI-1`） | `errors.go` |
| ④ | 会话身份怎么表达（`AR-25`） | `Session.ID()` 是**一等字段**，且由 `SessionFactory` 在连接建立时创建 | 转录若没有稳定的会话 ID 会散成一堆互不相干的行，归因与攻击链无从谈起 | `iface.go` |
| ⑤ | 事件怎么上行 | 由消费方实现 `Session`（生产实现经 `api/telemetry/v1`） | 本模块**禁止** import 核心内部包（`ST-3`/`ST-4`），也不该绑定某一种上行方式；单测可用替身（`MD-22`） | `iface.go` |
| ⑥ | 并发无上限怎么办 | **必须有默认上限**（`DefaultMaxConns=64`），超限拒绝并计数 | `MD-16`；没有上限的蜜罐等于给对手一个免费的资源耗尽入口 | `runner.go` |

**接缝与接口**：

| 契约 | 定义方 | 生产实现 | 单测替身 |
| --- | --- | --- | --- |
| `Protocol` | 本模块（消费方=运行框架） | 待实现（真实协议栈）· 现有 `Banner` | `holdProtocol`（占连接） |
| `Session` / `SessionFactory` | 本模块 | 待接入 `api/telemetry/v1` 的上行实现 | `recordingSession` / `recordingFactory` |
| `Registry` | 本模块 | 本模块 | —— |

**数据流（含失败路径）**：

```text
幻境路由来的 TCP 连接
        │
        ▼  Runner.acceptLoop
   ① 超限？ ── 是 ──► 立即关闭 + rejected++（MD-16：拒绝并记录）
        │ 否
        ▼
   ② active++ · 登记在途连接 · NewSession(协议, 对端)（会话 ID 一等字段，AR-25）
        │
        ▼
   ③ Protocol.Serve(ctx, conn, sess)  ← 真实协议栈的接缝在这里
        │   ├─ 正常结束（对手断开 / 读超时）→ 安静收尾
        │   └─ 出错 → 记日志（一个坏会话不拖垮监听）
        ▼
   ④ 会话内容经 Session 录制（转录 / 凭证 / 文件投递）→ 由消费方上行

Runner.Stop()（MD-15 对称回收）：
   停收新连接 → 等宽限期 → 强制关闭在途连接 → 等 goroutine 退出
```

---

## 3. 追溯矩阵

| 规则 ID | 模块文档 | 代码 | 测试 | 验证命令 |
| --- | --- | --- | --- | --- |
| `MD-16`（并发上限，超限即拒并记录） | [`../modules/honeypot-protocol.md`](../modules/honeypot-protocol.md) §4 | `runner.go` 的 `acceptLoop` + `Stats.Rejected` | `TestRunnerRejectsConnectionsOverLimit` | `make gate` |
| `MD-15`（资源创建与释放对称） | 同上 §4 | `runner.go` 的 `Stop`（停收 → 宽限 → 强制 → 等退出） | `TestRunnerStopIsSymmetricAndClosesListener` | `make gate` |
| `AR-25`（会话 ID 一等字段） | 同上 §4 | `iface.go` 的 `Session.ID`；`acceptLoop` 建会话时生成 | `TestBannerServesAndRecordsBothDirections`（断言 ID 非空） | `make gate` |
| `SB-6`（禁止主动连接目标） | 同上 §4 | 全模块只有 `net.Listen` 与已接受连接，**无任何拨号** | 代码评审 + `grep -n "Dial" deception/honeypot/*.go` → 空 | `make gate` |
| `NI-1`（不影响原始业务） | 同上 §6 | 蜜罐面在请求路径之外；seam 失败显式报错而非静默 | `TestNotImplementedIsExplicit` | `make gate` |
| `MD-14`（进程组回收 / 双层超时） | 同上 §4 | **部分适用**：宽限期即内层超时；进程组回收只对子进程形态适用（已在文档写明） | —— （见 §7 遗留 3） | 文档评审 |
| `MD-22`（测试独立可运行） | 同上 §7 | `protocol_test.go` 全部用替身，不依赖核心与真实存储 | 7 例全绿 | `make gate` |
| `ST-3` / `ST-4`（分层 import 纪律） | [`../design/structure.md`](../design/structure.md) §1.7 | 本模块**不** import `core/internal` 或 `api/` | `make archcheck` | `make archcheck` |
| `ADR-0011`（可选自研） | 同上 §1 | 协议栈刻意不实现，留作接缝与第三方路径 | —— | 文档评审 |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `deception/honeypot/iface.go` | 新增 | 契约：`Protocol`（适配器）· `Session`（录制口，`AR-25`）· `SessionFactory`（消费方提供）· `Registry`（名字唯一）· `Limits` / `Stats` |
| `deception/honeypot/errors.go` | 新增 | 错误值集中处：nil/空名/重名（`DuplicateError`）· `NotImplemented(what)`（**故意失败**的接缝） |
| `deception/honeypot/runner.go` | 新增 | 运行框架：监听 · 并发上限与拒绝计数（`MD-16`）· 对称回收（`MD-15`）· `UnknownProtocolError` |
| `deception/honeypot/banner.go` | 新增 | 最小真实适配器（问候 + 双向录制），示范框架用法，并就是可用的协议门牌仿真 |
| `deception/honeypot/protocol_test.go` | 新增 | 7 例：注册表三态 · banner 双向录制 · 超限拒绝计数 · 未知协议/重复启动 · `Stop` 对称性 · 未实现错误 |

**关键类型**（导出 = 契约）：`Protocol` · `Session` · `SessionFactory` · `Registry` · `Runner` · `Banner` · `Limits` · `Stats` ·
`DuplicateError` · `UnknownProtocolError` · `NotImplemented`。

**必须遵守的上位约束**：`MD-14` / `MD-15` / `MD-16` · `AR-25` · `SB-6` · `NI-1` · `MD-22` · `ST-3` / `ST-4` · `ADR-0011`。

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | 注册 nil / 空名 / 重名 | 三者都报错，重名带 `DuplicateError` 与协议名 | ✅ | `TestRegistryRejectsNilEmptyAndDuplicate` |
| 2 | 查表与枚举 | 已注册可查、未注册不可查、`Names()` 计数正确 | ✅ | `TestRegistryLookupAndNames` |
| 3 | banner 会话 | 先收到问候；我方发的与对手写的**都被录制**；会话 ID 非空 | ✅ | `TestBannerServesAndRecordsBothDirections` |
| 4 | 并发上限（`MD-16`） | 第二条连接被立即关闭，`Rejected=1`，`Accepted` 含被拒者 | ✅ | `TestRunnerRejectsConnectionsOverLimit` |
| 5 | 未知协议 / 重复启动 | 分别报 `UnknownProtocolError` 与错误（不允许两个监听抢同一协议） | ✅ | `TestRunnerUnknownProtocolAndDoubleStart` |
| 6 | `Stop` 对称回收（`MD-15`） | 宽限期内返回；监听关闭（再拨号失败）；不再报告监听地址 | ✅ | `TestRunnerStopIsSymmetricAndClosesListener` |
| 7 | 接缝错误 | 「尚未实现」必须显式报错且信息可读 | ✅ | `TestNotImplementedIsExplicit` |
| 8 | 分层 import 纪律 | 不 import `core/internal` 或 `api/` | ✅ | `make archcheck` |
| 9 | 无出站拨号（`SB-6`） | 代码里没有 `net.Dial` | ✅ | `grep -n "Dial" deception/honeypot/*.go` → 空 |

**没有覆盖的**：

- **真实协议栈**（SSH/MySQL/Redis…）：刻意不做（接缝）；
- **`MD-14` 的进程组回收**：需要子进程形态才能真正验证（本轮无子进程蜜罐）；
- **长时压力测试**：只验证了「超限即拒」这一条，未做长时间稳定性压测；
- **事件真正上行的端到端**：`Session` 的生产实现（接 `api/telemetry/v1`）尚未接，单测用的是替身。

---

## 6. 验证证据

```console
$ make gate
门禁通过。      # fmt · vet · staticcheck · errcheck · archcheck · trace · leakcheck · license · test -race

$ go test ./deception/... -count=1
ok  shen/deception/honeypot   0.209s      # 7 例

$ go build ./...
BUILD OK        # 全仓
```

**关键指标**：新增包 **1 个** · 新增测试 **7 例**（全仓 196 → **203**）· 已实现包 14 → **15** ·
导出的跨模块契约 **2 个**（`Protocol` / `SessionFactory`）· 新增依赖 **0**。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | ⚠️ **真实协议栈未实现**（SSH 密钥交换 / MySQL 握手 / Redis RESP / 凭证捕获 / 命令解释） | 蜜罐「像不像真的」全部取决于它 | 阶段 3 设计，或接第三方（`ADR-0011`） |
| 2 | **`honeypot-shell` 未实现**（命令表 / 内存文件系统 / 会话水印） | 协议仿真之后没有「命令响应」这一层 | 本批下一项 |
| 3 | **`netpolicy` 未实现**（声明式微隔离 / 假拓扑 / 运行时检测，复用 Cilium / Tetragon） | L3 网络欺骗层为空 | 本批下一项 |
| 4 | **`adapter-dns` 配置未收口**（生效/回退步骤、校验方法、配错影响面） | 形态 ② 只有 Corefile 草稿 | 本批下一项 |
| 5 | `MD-14`（进程组回收）**只对子进程形态适用** —— 本框架不是子进程 | 接第三方可执行蜜罐时需要进程包装层 | 接第三方时定 |
| 6 | `Session` 的生产实现（接 `api/telemetry/v1`、保留期按 `NI-13`）未做 | 转录目前只在内存里（测试） | 与「真实存储」那轮一起 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `docs/design/structure.md` §1.5 把 `deception/*` 整行标为「⏳ 阶段 2 / 3」，而 `deception/honeypot/` 已建 | 过期状态 | `ls deception/honeypot/` | 拆行：`deception/honeypot/` ✅ 已建（框架）；其余仍 ⏳ | ✅ |
| 2 | `docs/progress.md` 第 14 行「代码」列写 ⏳、模块文档状态写「设计」 | 过期状态 | 同上 | 两处改准（框架 + 7 例；协议栈待设计） | ✅ |
| 3 | 根 `README.md` 把「阶段 3」整行写「未开始」，且规模计数为 14 包 / 196 测试 | 过期计数 | `cat core/internal/*/*_test.go edge/*/*_test.go core/deception 2>/dev/null` → 203 | 改为「已起步」并更新计数 | ✅ |
| 4 | 模块文档 §7 的测试表把 `MD-14` 的专项测试写成「待建」，未区分**子进程形态**与进程内框架 | 叙述误导 | `docs/modules/honeypot-protocol.md` §7 | 拆成「已实现 5 类（框架）」+「待补 2 类（子进程/压力）」并注明适用条件 | ✅ |
| 6 | 测试替身 `recordingSession` 没加锁：`Serve` 在服务端 goroutine 里写转录、测试 goroutine 读它会**数据竞争** | **缺陷（-race 抓到）** | `make gate` → `WARNING: DATA RACE` + `--- FAIL: TestBannerServesAndRecordsBothDirections` | 替身加 `sync.Mutex` + `recorded()` 快照读；`go test -race ./deception/...` 通过 | ✅ |
| 7 | 日志条目里写了 `Start/Stop/Addr/Stats`（带斜杠）—— `DEV-2` 把它当路径核，报「引用的路径不存在」 | 记录形状（自查工具做对了事） | `make trace` → `✗ DEV-2 … Start/Stop/Addr/Stats` | 改为 `Start` · `Stop` · `Addr` · `Stats`；门禁绿 | ✅ |
| 5 | 首次写 `iface.go` 时引用了未定义符号（`errNilProtocol` 等），且留下一行 `ErrNotImplemented` 变量与后来的函数重复 | 编译红（自伤） | `go build ./deception/...` 报 `undefined` | 补 `errors.go` 集中错误值；删重复变量；`go build` + `go vet` 干净 | ✅ |

> 历史记录类文件（`docs/background/` · `docs/plans/` · `docs/log.md` · `docs/kb/`）不在审视范围。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | `honeypot-protocol` **框架落地**：`Protocol` / `Session` / `SessionFactory` / `Registry` 契约 · `Runner`（`MD-16` 并发上限 + `MD-15` 对称回收）· `Banner` 最小真实适配器 · `NotImplemented` 显式接缝；新增 7 例单测（全仓 203）；同步模块文档 / 结构文档 / 进度 / 根 README | 用户「继续」· 已批准的 `Q2(a)` / `Q4(a)` / `Q5(a)` · `MD-14`…`MD-16` · `AR-25` · `SB-6` |
