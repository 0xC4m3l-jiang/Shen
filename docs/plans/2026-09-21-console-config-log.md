# 变更包：控制台「配置」块 + 逐判定日志补全（读面契约收成一处）

| 项 | 值 |
| --- | --- |
| 主题 | 给控制台补两块**今天完全看不到**的东西：**核心当前生效态**（配置页）与**逐判定日志的完整字段 + 链路入口**；同时把「控制台 ↔ 核心」的读面契约收成一份 [`../spec/console-api.md`](../spec/console-api.md) |
| 日期 | 2026-09-21 |
| 状态 | 已实现 |
| 涉及模块 | `console`（[`../design/modules.md`](../design/modules.md) §1.1 第 21 行）· `control`（核心服务面，第 22 行）· `policy`（第 6 行，读快照的来源）· 契约 `api/telemetry/v1` |
| 决策数 | 已答 4 项（用户 Q1-A / Q2-A+B / Q3-A / Q4-A）/ 待定 3 项 |
| 关联 | [ADR-0020](../background/decisions/0020-console-minimal-static-ui.md)（控制台形态与只读边界）· [ADR-0026](../background/decisions/0026-cloud-model-backend.md)（AI 配置项来源）· 上一轮 [`2026-09-21-cloud-model-prereq.md`](2026-09-21-cloud-model-prereq.md) |

---

## 1. 需求与验收

**要解决什么**（一句话）：用户要完善管控平台的展示（日志 / 配置 / AI / 监控 / 告警），
而其中**配置是 0 展示**（接口与页面都没有）、**日志缺关键字段**（`decision_id` 与 `severity` 没在页面上），
且控制台与核心之间的读面契约**从来没写进 `spec/`**。

**做完之后能做什么**：

1. 打开控制台就能看到**核心当前按什么在跑**：策略 id / 版本 / 校验和 / 规则数 / 白名单条数 +
   AI 能力开关 / 任务种类 / 模型 / 清单路径 / 变体数 / 轮换冷却 / **已装载清单的资源与内容条数**；
2. 逐判定日志**字段齐了**（与 `logs.md` §3 的 11 个字段一一对应），并且**每条都能点进四段链路详情**；
3. 「AI 开关开着但没有内容」这种配置错**会被显式标出来**（不是让人从两个字段自己推）；
4. 读面契约有权威处：[`../spec/console-api.md`](../spec/console-api.md) —— 包括**哪些字段允许进快照**的规矩。

**验收判据**（可验证）：

1. `GET /api/config` 返回 `{policy, ai}` 两块、键全为 snake_case，且**校验和与核心启动日志逐字一致**；
2. 核心未装配快照读侧时，接口**报错**且**不回全零配置**（两侧各有一条单测钉住）；
3. 页面出现「配置」块与「逐判定日志」的 12 列表头（含判定 ID / severity / 看链路）；
4. `make gate` 通过（含 `make trace`；`api/` 的生成物与 `.proto` 一致）。

**不做什么**（划界）：

- **AI 生成批次 / 护栏拒绝明细**：要先有适配器（上一轮的下一半），本轮只把**配置态**摆出来；
- **监控（时序图）** 与 **告警（severity 档位 + 去重 + 确认）**：前者需要图表，后者需要先定档位 ——
  **这两块会触发 [ADR-0020](../background/decisions/0020-console-minimal-static-ui.md) 失效条件 1**（见 §7 遗留 1）；
- **控制台的写能力**（改配置 / 编排策略）：`ADR-0020` 决定 2 是「只读」，要动它得先重开；
- **阈值 / 灰度 / 影子模式上页面**：按 §2 决策 ③ 的规矩**不进契约面**；
- **不引任何前端依赖 / 构建步骤**（用户选 Q1-A）：本轮没有引入图表或组件化。

---

## 2. 设计逻辑

**已确认的决策**（用户 2026-09-21 四问四答）：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 技术路线 | **保持无构建**：仍单文件静态页，图表（将来）手写 SVG，**不引 npm** | 避开新增前端工具链；本轮也确实不需要图表 | 不动 `console/web/` 的形态，只加块与列 |
| ② | 先做哪些模块 | **配置 + 日志**（AI / 监控 / 告警 下一轮） | 配置今天 0 展示、日志缺字段，两者都**只读且不依赖**任何未落地的东西 | 本文件 §4 文件清单 |
| ③ | 数据从哪来 | **核心加一个只读快照 RPC**；其余沿用现有接口 | 配置与内容清单活在**核心内存**里，控制台拿不到，只能问核心要 | `api/telemetry/v1` 的 `GetCoreSnapshot` |
| ④ | 技能装哪 | 全局装（不进仓库 `.pi/skills/`） | 沿用「第三方技能带可执行脚本」的先例处理 | 见 §7 遗留 4（**本轮未执行**：先做展示，技能按需再装） |

**③ 的关键取舍 —— 字段准入规矩**（写进契约 §1.2，是这份契约最该被复用的部分）：

只允许两类字段进快照：**(a)** 已经允许离开核心的（策略版本 / 校验和 / AI 配置 —— 它们本就在策略载荷或适配器回执里）；
**(b)** 比 (a) 更弱的描述性信息（白名单**条数** —— 载荷里本来就有全量 CIDR 列表）。

因此**显式不含**阈值 / 灰度 / 误调度预算 / 影子模式 —— 它们是**核心运行参数**：
`core/internal/contract/thresholds.go` 明确禁止写进 `api/*.proto` 的对外响应（`ST-23` 只要求它们集中定义、可配置）。
**也不含**进程 `started_at`：核心全库**零** `time.Now()`（`MD-6` 的纪律），不为一个展示字段开这个口子。

**仍未定**（不阻塞本轮）：

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | 阈值 / 灰度 / 影子模式**要不要**上页面 | 「配置」页的完整度 | 需要用户定：按现行规矩它们不进契约面，只能走配置文档或另开一条非契约渠道 |
| 2 | 监控时序图与告警档位的做法 | 展示能力上限 | 先重开 ADR-0020（失效条件 1），再谈堆栈 |
| 3 | AI 生成批次 / 护栏拒绝明细的展示 | 「AI」模块的完整度 | 等适配器落地（ADR-0026 未解决 1） |

**接缝与接口**（依赖方向单向，`MD-4`）：

```text
api/telemetry/v1  ── proto（跨进程契约，唯一事实源）
        ▲
        │ 生成（make generate）
core/internal/contract/snapshot.go   ── 进程内共享类型（MD-5）
        ▲
        │ 转换（toProtoSnapshot）
core/internal/control/telemetry.go   ── TelemetryService：读侧的接缝
        ▲
        │ 装配（WithSnapshotProvider）
core/cmd/core/main.go                ── coreSnapshot 提供方（只有这里同时看得见 loader 与装载结果）
        ▲
        │ gRPC（GetCoreSnapshot）
console/cmd/console/main.go          ── /api/config → configView（snake_case）
        ▲
        │ fetch
console/web/index.html               ── 「配置」块 + 逐判定日志
```

**数据流（含失败路径）**：

```text
浏览器 /api/config
  └─ console.handleConfig(ctx 3s) ──gRPC──▶ core.GetCoreSnapshot
       ├─ 未装配 provider ⇒ Unimplemented ⇒ 502 {"error": …} ⇒ 页面显示「取不到」（**不**显示全零配置）
       ├─ provider 报错    ⇒ Internal      ⇒ 同上
       └─ 成功 ⇒ {policy, ai} ⇒ 卡片 + 明细表；「开了但没内容」⇒ 显式标红提示
```

---

## 3. 文档对应（追溯矩阵）

| 规则 ID | 文档章节 | 代码 | 测试 / 证据 | 验证命令 |
| --- | --- | --- | --- | --- |
| `AR-10` | [`../spec/console-api.md`](../spec/console-api.md) §0 · [`../modules/console.md`](../modules/console.md) §1 | `console/cmd/console/main.go`（全部只读） | 页面与接口均无写路径 | `make gate` |
| `ST-7` | `console-api.md` §0（可见面判据） | `console/web/index.html`（分值/信号只在本地页面） | 上轮 `make leakcheck`（响应面 ↔ 禁用清单） | `make leakcheck` |
| `MD-12` | `console-api.md` §1.2（**不含**阈值等运行参数） | `core/internal/contract/snapshot.go` · `core/cmd/core/main.go` 的 `coreSnapshot` | 单测 + 契约字段表 | `make test` |
| `ST-23` | `console-api.md` §1.2 | 同上 | 同上 | `make test` |
| `MD-6` | `console-api.md` §1.2（**不含** `started_at`） | 核心仍零 `time.Now()` | `grep -rn "time\.Now()" core/ --include=*.go \| grep -v _test` → 空 | 人工 |
| `MD-3` | `console-api.md`（读面契约唯一处）· `../modules/console.md` §2（只指向） | —— | `make trace` 的 D-3 引用检查 | `make trace` |
| `MD-5` | `console-api.md` §1.1 | `core/internal/contract/snapshot.go`（进程内类型）· `api/telemetry/v1`（跨进程） | 生成物与 `.proto` 一致 | `make generate` + `make gate` |
| `MD-20` | —— | 快照**不碰** store（只从 `loader` 与装载结果读） | 代码审查 | 人工 |
| `ST-20` / `ST-21` | `console-api.md` §0（禁止密钥） | 快照无密钥字段；`ai.model` 是**标识**不是密钥 | 字段表逐项看 | 人工 |
| `DEV-1` / `DEV-2` | 本文件 · [`../log.md`](../log.md) | —— | 形状检查 | `make trace` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `api/telemetry/v1/telemetry.proto` | 修改 | 加 `GetCoreSnapshot` RPC + `CoreSnapshot`（21 字段压到 16：去掉阈值/灰度/影子/started_at） |
| `api/telemetry/v1/telemetry.pb.go` · `telemetry_grpc.pb.go` | **重新生成** | 契约的唯一事实源是 `.proto`，**禁止手写客户端**（`make generate`，实测零 diff 于改前状态） |
| `core/internal/contract/snapshot.go` | 新增 | 进程内共享类型（`MD-5`）：`CoreSnapshot` / `PolicyState` / `AIState` |
| `core/internal/control/observer.go` | 修改 | 加 `SnapshotProvider` 端口（与 `EventLister` 并列，说明两者分工） |
| `core/internal/control/telemetry.go` | 修改 | `WithSnapshotProvider` + `GetCoreSnapshot` 实现 + `toProtoSnapshot`；**未装配返回 `Unimplemented`，不回全零** |
| `core/cmd/core/main.go` | 修改 | `coreSnapshot` 提供方（从 `loader` + 装载结果组装）；**内容只装载一次**供策略面与快照共用；`WithSnapshotProvider` 接线 |
| `console/cmd/console/main.go` | 修改 | `/api/config` 路由 + `handleConfig` + `configView`（snake_case；这层把「哪些字段允许出观测面」显式写下） |
| `console/web/index.html` | 修改 | 新增「配置」块（6 张卡 + 明细表）；逐判定日志补 `判定 ID` / `severity` / `看链路` 三列；`idCell` / `traceCell` / `renderConfig` / `loadConfig` |
| `console/cmd/console/config_test.go` | 新增 | 键名（snake_case 齐备）+ 失败语义（取不到时**不得**回 200/全零） |
| `core/internal/control/telemetry_test.go` | 修改 | 快照三条：未装配 ⇒ `Unimplemented` · 逐字段映射 · 提供方报错 ⇒ `Internal` |
| `docs/spec/console-api.md` | **新增** | 读面契约收成一处：两个 gRPC 读方法 + 10 个 HTTP 接口 + **字段准入规矩** + 改键规矩 |
| `docs/spec/README.md` | 修改 | 加 `console-api.md` + `metrics.md` 两行；删掉过期的「`metrics.md` 待建」 |
| `docs/README.md` | 修改 | `spec/` 那行的「`logs` / `metrics` 待写」是**过期标记**（两者都已建）→ 改为 8 份清单 |
| `docs/modules/console.md` | 修改 | §2 改为指向契约（并标出「策略编排」是**未实现的设计意图**）；§7 加四条测试；§9 加一行 |
| `docs/kb/capabilities.md` | 修改 | 控制台接口数 7 → **10**（含 `/api/config`）；补「配置块」一句 |
| `docs/plans/2026-09-21-console-config-log.md` | 新增 | 本文件 |
| `docs/log.md` | 修改 | 本轮变更日志条目 |

**关键类型与函数**：

- **导出契约**（跨进程）：`telemetry.v1.CoreSnapshot` · `telemetry.v1.DeceptionTelemetry.GetCoreSnapshot` · 控制台 `GET /api/config`（键名以 `console-api.md` 为准）；
- **进程内**：`contract.CoreSnapshot` / `PolicyState` / `AIState`（`MD-5`）、`control.SnapshotProvider`（端口）；
- **内部细节**：`coreSnapshot`（提供方实现）· `toProtoSnapshot`（映射）· `configView`（页面形状）。

**必须遵守的上位约束**：`AR-10` · `ST-7` · `MD-3` · `MD-5` · `MD-6` · `MD-12` · `ST-20` / `ST-21` · `ST-23` · `TB-14`（错误一律显式返回，不吞）。

---

## 5. 测试与场景

| # | 场景 | 输入 / 前置 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 1 | 端到端取配置 | 起核心（临时配置：policy v3 · 2 条规则 · 白名单 3 条）+ 起控制台 | `/api/config` 返回两部分，值与核心一致 | ✅ | §6 的 JSON 输出 |
| 2 | **校验和交叉核对** | 同上 | 接口里的 `checksum` == 核心启动日志里的 `checksum` | ✅ | 两处都是 `8a0913a613594bb2db72f6428a385524359d638441ab359ebe6f5d6353948771` |
| 3 | 白名单**只给条数** | 配置 2 个 CIDR + 1 个 UA | `whitelist_count=3`，且响应里**没有**任何 CIDR 字符串 | ✅ | §6 |
| 4 | 页面含新块 | `curl /` | 命中 `configCards` / `configDetail` / `逐判定日志` / `看链路` | ✅ | §6（6 处命中） |
| 5 | 未装配快照读侧 | 核心侧单测：不传 `WithSnapshotProvider` | `Unimplemented` 且**不回快照** | ✅ | `TestTelemetryService_SnapshotWithoutProviderIsUnimplemented` |
| 6 | 逐字段映射 | 核心侧单测：给定快照 | 16 个字段逐个对上 | ✅ | `TestTelemetryService_SnapshotMapsFields` |
| 7 | 提供方报错 | 核心侧单测：provider 返回 error | `Internal`（不吞成成功） | ✅ | `TestTelemetryService_SnapshotProviderErrorIsInternal` |
| 8 | 键名钉住 | 控制台单测：假客户端返回快照 | `policy`/`ai` 两块的 snake_case 键齐备、值正确 | ✅ | `TestHandleConfig_EmitsSnakeCaseShape` |
| 9 | **取不到时不得伪造** | 控制台单测：假客户端返回 error | 非 200 且响应带 `error` | ✅ | `TestHandleConfig_CoreUnimplementedIsAnError` |
| 10 | 生成物与 `.proto` 一致 | `make generate` | 零 diff（改 proto 后重新生成，改前状态可复现） | ✅ | `git status`（生成后仅 3 个 api 文件变更） |
| 11 | 门禁 | `make gate` | 全绿 | ✅ | §6 |

**没有覆盖的**：

- **没有浏览器内的渲染验证**：本轮只核了「页面含这些 id 与表头」+ 键名单测，**没跑真实浏览器**（本轮无 JS 测试基建）；
- **没验证「开了但没内容」的页面提示**：需要一份 `ai.enabled=true` 且清单为空的部署（单测覆盖了数据结构，UI 分支靠代码审查）；
- **没测多副本 / 鉴权**：控制台仍单实例无鉴权（ADR-0020 未解决项，未变）。

---

## 6. 验证证据

**端到端**（临时配置起核心 + 控制台，`/tmp/ai-probe/console-check.sh`）：

```console
$ curl -s http://127.0.0.1:<port>/api/config | python3 -m json.tool
{
    "policy": {
        "policy_id": "console-check",
        "version": 3,
        "checksum": "8a0913a613594bb2db72f6428a385524359d638441ab359ebe6f5d6353948771",
        "rule_count": 2,
        "whitelist_count": 3
    },
    "ai": {
        "enabled": false,
        "kinds": ["content"],
        "model": "",
        "manifest_path": "",
        "variants": 8,
        "rotate_cooldown": "30m0s",
        "manifest_loaded": false,
        "manifest_version": 0,
        "manifest_resources": 0,
        "manifest_contents": 0
    }
}

$ curl -s http://127.0.0.1:<port>/ | grep -cE 'id="configCards"|id="configDetail"|逐判定日志|看链路'
6

$ grep 策略已装载 <核心日志>
策略已装载 policy_id=console-check version=3 checksum=8a0913a613594bb2db72f6428a385524359d638441ab359ebe6f5d6353948771 规则=2 条 灰度=0%
```

> 校验和**两处逐字一致** —— 这是「控制台看到的配置 == 核心实际在跑的配置」的直接证据。
> 白名单 3 = 2 个 CIDR + 1 个 UA（**条数对了，且响应里没有任何 CIDR 字符串**）。

**单测与门禁**：

```console
$ go test ./core/internal/control/ -run Snapshot -v
--- PASS: TestTelemetryService_SnapshotWithoutProviderIsUnimplemented
--- PASS: TestTelemetryService_SnapshotMapsFields
--- PASS: TestTelemetryService_SnapshotProviderErrorIsInternal

$ go test ./console/cmd/console/ -run Config -v
--- PASS: TestHandleConfig_EmitsSnakeCaseShape
--- PASS: TestHandleConfig_CoreUnimplementedIsAnError

$ make gate
All checks passed!（ruff）
架构检查通过。
  模块清单一致性 · CGO 与本地库 · 语言层数 · 护栏为唯一出口（AR-33）
  AI 能力独立性（MD-4：aicap / llm 的依赖白名单）
追溯检查通过。            ← 含悬空链接／规则 ID 引用／变更包与日志形状
泄漏检查通过。
许可审计通过：没有传染性或限制性许可。
80 passed in 0.43s        ← L4 单测（pytest）
门禁通过。
```

> `make gate` 的 Go 侧（含本轮新增的 5 条）由 `go test -race ./...` 跑，输出为各包含 `ok`。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | **`ADR-0020` 失效条件 1 的边界要盯住** | 本轮**未触发**（只加了一张表 + 卡片，**没有**图表/组件化）；但**监控时序图**与**告警的多维筛选**会触发它 —— 那时必须先重开 ADR-0020、**先引入前端门禁再迁移** | 下一轮（监控/告警）开工前先判 |
| 2 | 阈值 / 灰度 / 影子模式**上不了页面** | 「配置」页看不到「现在是不是真的在处置」 | 需用户定（进契约面 / 另开非契约渠道 / 就留在配置文档） |
| 3 | AI 生成批次、护栏拒绝明细未展示 | 「AI」模块只到配置态 | 等适配器（ADR-0026 未解决 1） |
| 4 | 前端技能未安装 | 展示质量靠手写 | 用户选「按你设计来，后续调整」；需要时按 §2 决策 ④ 装 `design-playbook`（全局） |
| 5 | 控制台仍**无鉴权**、单实例 | 生产化阻塞 | ADR-0020 未解决项（未变）；若哪天绑到非回环地址，**必须先解决** —— 配置快照会暴露策略版本与清单路径 |
| 6 | 页面无 JS 测试 | 渲染分支（如「开了但没内容」的提示）没被自动验证 | 若前端继续变复杂，与失效条件 1 一起评估 |

---

## 7.1 审视记录（L 档：改了跨进程契约与对外描述 → 必做）

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `docs/README.md` 写着「`logs` / `metrics` 待写」 | **过期状态标记**（两者都已建） | `grep -n "logs\` / \`metrics\` 待写" docs/README.md` | 改为 8 份契约的清单 | 已修 |
| 2 | `docs/spec/README.md` 写着「**待建**：`metrics.md`」 | **过期状态标记** | `grep -n "待建.*metrics" docs/spec/README.md` | 删掉该行、把 `metrics.md` 与 `console-api.md` 加进表格 | 已修 |
| 3 | 控制台接口数在 `kb/capabilities.md` 写的是 **7** | 计数漂移 | `grep -n "个只读接口" docs/kb/capabilities.md` | 改为 **10**（含 `/api/config`） | 已修 |
| 4 | 读面契约（`ListEvents` + 10 个 HTTP 接口）**从来没进 `spec/`** | 契约缺位（`MD-3`） | `grep -rln "ListEvents" docs/spec/` → 仅 `_map.md` 提及 | 新增 `console-api.md` 并登记 | 已修 |
| 5 | 阈值等运行参数**该不该上页面** | 规则边界 | `core/internal/contract/thresholds.go` 的注释（禁止进 `api/*.proto` 对外响应） | **不进快照**，并在契约 §1.2 写下准入规矩与理由 | 守住了，且写成可复用规矩 |
| 6 | 进程 `started_at` 会引入核心第一处 `time.Now()` | 纪律边界（`MD-6`） | `grep -rn "time.Now()" core/ --include=*.go \| grep -v _test` → 空 | **去掉该字段**（「是不是刚重启」由 `/api/summary` 的事件时间范围回答） | 未开这个口子 |
| 7 | 惰性登记表 + 快照这类改动会不会让「声明了不生效」重现 | 空声明复发核查 | 新字段**全部**在页面或接口上有消费者 | 无空字段 | 已核 |
| 8 | 生成物（`.pb.go`）与 `.proto` 是否一致 | 生成物漂移 | `make generate` → 仅 3 个 api 文件变更（就是我改的） | 已重新生成 | 一致 |
| 9 | 本轮有没有改到 `docs/design/`？ | 越界核查 | `git diff --name-only docs/design/` | **无** | 未越界 |
| 10 | 静态分析器报 `scripts/tracecheck/main.go:906` 的「三层嵌套」 | **预存在项**（与本轮无关） | `git diff --name-only \| grep tracecheck` → 空；且该文件共 **899 行**，报告行号越界 ⇒ 是分析器的旧快照/近似位置 | 不改（本轮没碰它）；已记录，供后续清理工具时一并看 | 记录在案 |

> **排除项**：`docs/background/` · `docs/plans/` · `docs/log.md` · `docs/kb/` 的历史记录部分不在审视范围内。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-21 | 首版：控制台「配置」块（核心只读快照 RPC + `/api/config`）· 逐判定日志补全字段与链路入口 · 读面契约收成 `spec/console-api.md` · 修两处过期状态标记 | 用户 Q1-A / Q2-A+B / Q3-A / Q4-A（2026-09-21）+ `AR-10` / `MD-3` / `MD-6` / `MD-12` / `ST-23` |
