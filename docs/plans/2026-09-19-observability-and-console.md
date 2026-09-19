# 变更包：观测面读路径 + 控制台（Web UI）+ 一键人工测试环境 + 接入/使用文档

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 让「告警 / 流量访问 / 请求在引擎中怎么流动」**看得见**：补上观测面读路径、判定记录、控制台网页、一键 demo 环境，并产出四份接入与使用文档 |
| 日期 | 2026-09-19 |
| 状态 | 已验证（真进程端到端：经引擎的真实流量 → 控制台显示 `score=0.90 signals=[ua-headless,path-git]`；`make gate` 通过） |
| 改动分级 | **L**（新增对外服务面 RPC 与一个模块的实现（`console`）；改数据侧读契约） |
| 涉及模块 | `store`（读侧）· `telemetry`（读侧 RPC）· `control`（观测面记录与读取）· `console`（**新实现**，[`../design/modules.md`](../design/modules.md) §1.1 第 21 行）· `cmd/core`（装配） |
| 决策数 | 已答 4 项（读侧放遥测面 · 控制台无前端构建 · 判定细节只进观测面 · 示例配置默认给可观察规则）/ 待定 2 项（见 §7） |
| 关联 | `ST-7`（判定细节不回显）· `AR-10`（控制面不参与判定）· `NI-1` · `ST-17`（探针语义）· 实验 `E3` |

---

## 1. 需求与验收（来自本轮目标）

**要解决什么**：目标要求「**产出业务如何使用、用户如何使用、如何查看对应的告警、如何查看对应的流量访问和在欺骗引擎中如何流动**，方便人工测试，并有一个**简单的 Web UI**」。
现状盘点发现**根因**：① `store.EventStore` **只写不读**、`telemetry.proto` **没有查询 RPC** ⇒ 观测面**没有读路径**；
② `DecisionStore.Archive` **从未被调用** ⇒ 判定（分值 / 命中信号 / 去向）根本没落库；
③ 示例配置 `rules: []` ⇒ 所有分数恒为 0，人工测试**什么都看不见**。

**做完之后**：一条命令起「能看」的环境；打开网页就能看到谁访问了什么、被判成什么、分值与命中信号、去了哪个后端。

**验收判据**：

1. `store` 读侧：`EventStore.List` / `DecisionStore.List`（有界缓冲、newest first、按时间与类型过滤）。
2. 契约：`telemetry.v1` 新增 `ListEvents`（`ST-6`：契约从 `.proto` 生成）。
3. 核心：服务面每次判定记一笔（事件 + 判定记录），**记录失败不影响响应**（`NI-1`）。
4. 控制台：`GET /`（网页）· `/api/summary` · `/api/flow` · `/api/events` · `/healthz`；**渲染攻击者可控字符串用 DOM + `textContent`**（不用 `innerHTML`，避免存储型 XSS）。
5. 一键环境：`scripts/demo/run.sh` 起核心 + 假业务站 + 反向代理 + 控制台并打印地址。
6. 文档四份：`docs/integrate/`（总览 · 5 分钟上手 · 业务接入 · 观测台 · 人工测试）。
7. `make gate` 绿；本轮已提交。

**不做什么**：不实现 write 类控制面能力（改策略 / 干预请求，`AR-10`）· 不引入前端工具链（无 Node/npm 构建）· 不改任何规则正文。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | 读侧放哪个契约 | **`telemetry.v1` 加 `ListEvents`** | 告警与流量本质是遥测的读侧；不新开服务面 |
| ② | 判定细节（分值/信号）能不能进事件 | **能，但只进观测面** | `ST-7` 禁止**回显给适配器/客户端**；控制台是内部面，人工测试必须看得到 |
| ③ | 控制台用什么写 | **Go 进程 + 静态 HTML（无构建步骤）** | 用户要求「不用实现的太复杂」且此前裁定不引入前端工具链；单文件二进制最好交付 |
| ④ | 页面渲染纪律 | **DOM + `textContent`，禁用 `innerHTML` 渲染数据** | 页面展示的是**攻击者可控**的 UA/路径 —— 拼字符串就是存储型 XSS |
| ⑤ | 示例配置要不要带规则 | **要（2 条可观察规则）** | 规则为空 ⇒ 分数恒 0 ⇒ 人工测试看不见任何东西（本轮实测踩到） |

---

## 3. 追溯矩阵

| 规则 ID | 文档 | 代码 | 测试/证据 | 验证命令 |
| --- | --- | --- | --- | --- |
| `ST-7`（禁止回显判定细节） | [`../integrate/observability.md`](../integrate/observability.md) | 观测面记录器（`core/cmd/core/observability`）+ 控制台 | 端到端：分值只在控制台出现 | 实跑 |
| `AR-10`（控制面不参与判定） | [`../modules/console.md`](../modules/console.md) | `console/cmd/console`（只读） | 页面/接口无写操作 | 代码评审 |
| `NI-1`（不影响业务） | [`../integrate/manual-test.md`](../integrate/manual-test.md) §4 | 记录器失败只记日志 | `V-1…V-4`（`make gate` 内） | `make gate` |
| `AR-11` / `ST-9`（遥测幂等） | 同上 | 事件 ID = `decision:<decision_id>` | 重复判定不重复记录 | 实跑 |
| `ST-10`（同窗复用判定） | [`../integrate/manual-test.md`](../integrate/manual-test.md) §3 | 适配器判定缓存（既有） | 端到端：同窗只记一笔 | 实跑 |
| `ST-17`（探针语义） | [`../integrate/observability.md`](../integrate/observability.md) | `/healthz` 只报**本进程**存活 | 页面在核心不可达时显示错误 | 实跑 |
| `ST-6`（契约唯一事实源） | `api/telemetry/v1/telemetry.proto` | `make generate` 重新生成 | 生成物含 `ListEvents` | `make gate` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `core/internal/store/iface.go` · `memory.go` | 改 | 新增 `EventQuery` / `DecisionQuery` / `List`；内存实现保留**有界**最近事件与判定（`DefaultEventBuffer=4096`），并提供 `List` |
| `api/telemetry/v1/telemetry.proto`（+ 生成物） | 改 | 新增 `ListEvents`（读侧契约） |
| `core/internal/control/observer.go` | 新增 | 观测面契约：`DecisionRecord`（含请求身份与判定细节）· `DecisionRecorder`（写侧）· `EventLister`（读侧）——**由消费方定义** |
| `core/internal/control/service.go` | 改 | `WithDecisionRecorder` / `WithClock`；判定后**尽力而为**记录（失败只记日志） |
| `core/internal/control/telemetry.go` | 改 | `WithEventLister` + `ListEvents` 实现（未装配读侧时返回空 ⇒ 页面显示「暂无数据」而非报错） |
| `core/cmd/core/main.go` | 改 | 装配观测面：`decisionRecorder`（事件 + `DecisionStore.Archive`）与 `eventLister` |
| `console/cmd/console/main.go` · `console/web/assets.go` · `console/web/index.html` | 新增 | 控制台：HTTP 服务 + JSON 接口 + 静态页面（embed，无构建步骤） |
| `scripts/demo/run.sh` · `scripts/demo/business.py` | 新增 | 一键环境（核心 + 假业务 + 代理 + 控制台），面向人工测试 |
| `deploy/config/config.example.yaml` | 改 | 启用两条**可观察**示例规则（否则分数恒 0） |
| `Makefile` | 改 | `make console` · `make demo` |
| `docs/integrate/{README,quickstart,business-onboarding,observability,manual-test}.md` | 新增 | 目标明确要求的四类「怎么做」文档 |

---

## 5. 测试与场景

**真进程端到端（权威证据）**：

| # | 场景 | 期望 | 实测 |
| --- | --- | --- | --- |
| 1 | 起核心（示例配置含 2 规则）+ 控制台 | 均启动、打印地址 | ✅ `策略已装载 … 规则=2 条` · `console 已启动：监听 127.0.0.1:19461` |
| 2 | 经引擎发探针流量（`HeadlessChrome` + `/.git/config`） | 产生判定记录 | ✅ 3 条决策事件 |
| 3 | 控制台读取流动 | 显示**分值 + 命中信号** | ✅ `/.git/config score=0.90 signals=[ua-headless,path-git]`；`/x score=0.60 signals=[ua-headless]`；正常 UA `score=0.00` |
| 4 | 概览接口 | 计数与告警数 | ✅ `{"total":6,"by_action":{"route_origin":3},"alerts":0,…}` |
| 5 | 页面可取 | 200 + HTML | ✅ `code=200 bytes=8394 type=text/html` |
| 6 | 事件与流动条数一致 | 一致（幂等 + 解析正确） | ✅ 3 决策事件 ↔ 3 行 flow |

**没有覆盖的**：`make demo` 脚本本身的自动化测试（靠人工按 quickstart 走一遍）· 大量事件下的分页/性能 · 控制台的多副本（当前单实例）。

---

## 6. 验证证据

```console
$ curl -s -A "HeadlessChrome/120" http://127.0.0.1:18082/.git/config      # 经引擎
$ curl -s "http://127.0.0.1:19461/api/flow?limit=20"
/.git/config  score=0.8999999999999999  signals=[ua-headless, path-git]
/x            score=0.6                 signals=[ua-headless]
/y            score=0                    signals=[]

$ make gate
门禁通过。
```

**关键指标**：新增 RPC **1 个** · 新增模块实现 **1 个**（`console`）· 新增文档 **5 份** · 新增 Make 目标 **2 个** · 端到端可见「分值 + 命中信号」。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 控制台**语言偏离设计**：设计写 TypeScript，本轮实现为 Go + 静态页面（无构建步骤） | 需登记（`TB-20` / `TB-4`） | 拟开 ADR-0020，说明「按用户裁定不引入前端工具链」 |
| 2 | 观测面**没有保留期与分页**（内存实现有界缓冲） | 只能看「最近」 | 接真实存储（ClickHouse）时按 `NI-13` 定 |
| 3 | 判定记录的时间基准：事件 `created_at` 为 UTC、载荷 `At` 带本地偏移 | 页面已统一按本地显示，JSON 消费者需注意 | 在 `spec/logs.md` 落地时统一 |
| 4 | 剩余模块：`adapter-dns`（配置收口）· `netpolicy`（声明式）· `analysis/*`（4 个 Python 模块） | 目标要求「按设计完成所有模块」 | 下一轮继续；Python 侧需先定门禁（此前用户裁定不引入） |
| 5 | `honeypot-shell` / 蜜罐内容 | **目标明确排除** | 保持 ⏸ 推迟 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `DecisionStore.Archive` **从未被调用**，判定结果没有落库 | 漏接（观测面全瞎） | `grep -rn "Archive(" core/` 仅实现处 | 在服务面接上记录器（事件 + 归档） | ✅ |
| 2 | 示例配置 `rules: []` ⇒ 分数恒 0 | 叙述误导（人工测试看不见东西） | 实跑：`score=0.00 signals=[]` | 启用两条可观察规则 + 文档说明「规则为空则分数为 0」 | ✅ |
| 3 | 页面初版用 `innerHTML` 渲染 UA/路径 | **安全缺陷**（存储型 XSS，数据是攻击者可控） | 静态检查告警 12 处 | 改为 DOM + `textContent`，并写进页面注释与变更包 | ✅ |
| 4 | `go:embed web/index.html` 路径相对包目录且不许 `..` | 编译红 | `pattern web/index.html: no matching files found` | 资源移到 `console/web/` 独立包 embed | ✅ |
| 5 | 我的补丁里用了「嵌套 heredoc」导致 shell 解析错乱 | 自伤（可能误改文件） | 命令输出大量 `command not found` | 立刻核实 git 状态与文件完整性，改用 `write` 工具落盘 | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 观测面读路径（`List` + `ListEvents` RPC）· 判定记录（事件 + 归档）· `console` 实现（Go + 静态页，无构建）· 一键 demo（核心+业务+代理+控制台）· 示例配置启用可观察规则 · `docs/integrate/` 五份文档 | 本轮目标（看告警/看流量/看流动 + 简单 Web UI + 方便人工测试）· `ST-7` · `AR-10` · `NI-1` · `ST-17` |
