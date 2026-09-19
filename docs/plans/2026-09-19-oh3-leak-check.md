# 变更包：`OH-3` 泄漏检查落地 + 设计一致性核查

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 把 `OH-3`「必须提供自动检查」真正做出来（`scripts/check-leak`）并接进门禁；顺带修掉它查出的真实泄漏 |
| 日期 | 2026-09-19 |
| 状态 | 已验证 |
| 涉及模块 | 工具 `scripts/check-leak/`（新建）· `adapter-proxy`（[`../design/modules.md`](../design/modules.md) §1.1 第 11 行）· `decoy`（第 23 行）· `responder`（第 3 行） |
| 决策数 | 已答 5 项 / 待定 2 项 |
| 关联 | [`../design/constraints.md`](../design/constraints.md) 的 `OH-1`…`OH-5` · 上轮 [`2026-09-19-caddy-l1-base.md`](2026-09-19-caddy-l1-base.md) · [`../log.md`](../log.md) |

---

## 1. 需求与验收

**要解决什么**：`OH-3` 规定「工程**必须**提供自动检查，对响应面模块的字符串字面量扫描 `OH-1` 清单」，
但仓库里只有 [`scripts/check-leak/README.md`](../../scripts/check-leak/README.md) 一个占位（状态「⏳ 尚未实现」），
`make gate` 里也没有这一环 —— **已确认的规则没有实现**。

**做完之后**：`make gate`（与 `make check`）里多一道 `leakcheck`；任何人往响应面写「蜜罐 / decoy / mirage / 决策分数」这类字面量，**提交时就会被拦**，并给出两条出路（改代码 / 逐条登记豁免）。

**验收判据**：

1. `make leakcheck` 可运行，且已接进 `make lint` / `make gate` / `make check`（`OH-3`）。
2. 扫描范围从 [`../design/modules.md`](../design/modules.md) §1.1 **解析**，不硬编码；模块改名 → 检查**直接失败**（拒绝少扫）。
3. 豁免分三类且都**不是整目录豁免**（`OH-4`）：规则级（内部日志 / 错误构造 / struct tag）· 文件级（`//check-leak:filter <理由>`）· 逐条（`allow.txt`，精确到 **文件 + 字面量**）。
4. 豁免过期会被报出来（与 `tracecheck` 同一规矩，不留僵尸条目）。
5. **负向验证**：故意写入违规字面量必须被抓到（体 / 响应头名 / Cookie 名 / `OH-5` 四类各验一次）。
6. 现有代码里的真实泄漏修掉；`make gate` 全绿。

**不做什么**：

- 不做**拼接串**检测（从配置读来的值、模板变量）—— `OH-2` 说最终判据是人工核对，检查只覆盖字面量，且这一点写进了 README；
- 不扫**项目名 / 二进制名 / 服务名** —— 项目名尚未定（[ADR-0004](../background/decisions/0004-terminology.md)），README 里标明定名后必须补进清单；
- 不改 `docs/design/` 的任何规则（`OH-3` 本来就要求了这件事，本轮只是把它做出来）。

---

## 2. 设计逻辑

**决策树**：

```text
OH-3 落地
├── 第 1 轮（范围）：扫哪些文件？
│   ├── Q1 全部代码 vs 只扫响应面 → 只扫响应面（OH-3 字面要求），清单从 modules.md 解析
│   └── Q2 测试文件？ → 不扫（夹具不是响应面）
├── 第 2 轮（判据）：什么算违规？
│   ├── Q3 OH-1 词表照抄 constraints.md；大小写不敏感（ACTION_MIRAGE 与 mirage 对攻击者是一回事）
│   └── Q4 OH-5 怎么查 → 响应头名 / Cookie 名里的决策类词
└── 第 3 轮（例外）：怎么允许内部用法？
    ├── Q5 规则级（日志 / 错误串 / struct tag）→ 写进 main.go 包注释，附 OH-2 依据
    ├── Q6 文件级（黑名单表这种「过滤器」）→ 文件自己声明理由，检查打印出来
    └── Q7 逐条 → allow.txt，精确到文件+字面量，理由必填，过期即报
```

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 扫描范围 | 只扫**响应面模块**，清单解析自 `modules.md` §1.1 | `OH-3` 的字面要求；同时避免把配置/存储面的内部术语（`TM-6` 允许）误报 | `scripts/check-leak/main.go` 的 `responseSurfaceModules` + `responseSurfaceDirs` |
| ② | 命中粒度 | 大小写不敏感的子串匹配 | `ACTION_MIRAGE` 与 `mirage` 在攻击者眼里是一回事 | `containsAnyFold` |
| ③ | 三类豁免 | 规则级 · 文件级 · 逐条，**禁止**目录级 | `OH-4`：例外必须逐条声明并附理由；目录豁免是最容易腐烂的形式 | `main.go` 包注释 · `//check-leak:filter` · `allow.txt` |
| ④ | 过期豁免 | 命不中就报 `ALLOW` 问题 | 与 `tracecheck` 同规矩；僵尸豁免会让检查慢慢失效 | `run()` |
| ⑤ | 装配位置 | 接进 `lint` / `check` / `gate`，独立 `make leakcheck` | 与 `archcheck` / `tracecheck` / `licensecheck` 一致的写法 | `Makefile` |

**仍未定**：

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | 项目名 / 二进制名 / 服务名的禁用扫描 | `OH-1` 明写禁止这三类；未定名就无从扫描 | 定名后（ADR-0004）补进 `banned` |
| 2 | 拼接串与模板变量的泄漏 | 只写字面量扫不到 | 依赖接入演练 + `OH-2` 人工核对（README 已标明） |

**接缝与接口**：本轮不新增任何对外接口。新增的两个「接口」都是**约定**：`//check-leak:filter <理由>`（文件级声明）与 `allow.txt` 的 `<ID> <文件> <字面量> # 理由`（逐条登记）。

**数据流（含失败路径）**：

```text
modules.md §1.1 ──解析──► 响应面模块目录 ──► go/ast 解析每个非测试 .go
                                              │
                            ┌─────────────────┼──────────────────┐
                            ▼                 ▼                  ▼
                      跳过日志/错误/tag   收集字面量         识别 Header/Cookie 名
                            │                 │                  │
                            └─────────► 对照 OH-1 词表 ──► 命中？
                                                            ├─ 已登记豁免 → 标记 used
                                                            └─ 未登记 → 报 OH-1 / OH-5
解析结果 < 下限或模块名找不到 → 直接失败（拒绝少扫，不静默放行）
```

---

## 3. 追溯矩阵

| 规则 ID | 模块文档 | 代码 | 测试 | 验证命令 |
| --- | --- | --- | --- | --- |
| `OH-1`（禁用清单） | [`../design/constraints.md`](../design/constraints.md) §OH-1 | `scripts/check-leak/main.go` 的 `banned` | 负向验证（体/头/Cookie 三类探针） | `make leakcheck` |
| `OH-2`（唯一判据） | 同上 §OH-2 适用位置表 | 三类豁免的依据写在 `main.go` 包注释 | —— | `make leakcheck`（豁免打印） |
| `OH-3`（必须自动检查） | 同上 §OH-3 | `scripts/check-leak/main.go` · `Makefile` 的 `leakcheck` | 本条即本轮验收判据 1 | `make gate` |
| `OH-4`（例外逐条 + 禁止目录豁免） | 同上 §OH-4 | `allow.txt` 解析（拒绝目录形式 / 拒绝缺理由）· `//check-leak:filter` | —— | `make leakcheck` |
| `OH-5`（决策与分数禁止回传响应头） | 同上 §OH-5 | `headerNameArg` + `decisionTokens` | 负向验证（`X-Agent-Capture-Decision` 探针） | `make leakcheck` |
| `AR-22`（生成内容黑名单） | [`../modules/responder.md`](../modules/responder.md) §4 | `core/internal/responder/blacklist.go`（含 `//check-leak:filter` 声明） | 模块既有单测 | `make gate` |
| `INT-8`（只改蜜罐侧响应） | [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) §1 | `edge/proxy/handler.go`（未动；本轮只改其日志措辞） | `edge/proxy` 既有 29 例 | `make gate` |
| `TM-6` / `TM-7`（术语只用于内部） | [`../design/terminology.md`](../design/terminology.md) §6 | 同上（豁免的依据） | —— | `make leakcheck` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `scripts/check-leak/main.go` | 新增 | `OH-3` 要求的自动检查本体（标准库 + `go/ast`，零新依赖） |
| `scripts/check-leak/allow.txt` | 新增 | 逐条例外登记处（`OH-4`）；当前 2 条 |
| `scripts/check-leak/README.md` | 重写 | 从「⏳ 尚未实现」改为已实现：范围 / 三类豁免 / 局限（它**不**保证什么） |
| `Makefile` | 改 | 新增 `leakcheck` 目标，接进 `lint` / `check` / `gate` 与 `.PHONY` |
| `core/internal/responder/blacklist.go` | 改 | 加 `//check-leak:filter <理由>` 文件级声明（该文件**就是** `AR-22` 的过滤器） |
| `core/internal/decoy/decoy.go` | 改 | **修真实泄漏**：投放片段里的「指令文件蜜饵」「凭证蜜饵」与 `decoy_accounts` 表名 → 中性措辞（这些片段会被投放进客户环境，对手可能读到） |
| `edge/proxy/cmd/proxy/main.go` | 改 | 启动日志措辞「可按决策引流」→「可按决策改道」（与 `terminology.md` §4.1 的三值命名对齐，同时减少泄漏面） |
| `.pi/devloop.md` | 改 | 适配面补 `leak_cmd: make leakcheck`；`make check` 注释加上泄漏检查 |
| `docs/kb/dev-workflow.md` | 改 | §3「`make gate` 替你核了什么」补上 `leakcheck` 一栏 |

**关键类型与函数**（全部内部，无导出契约）：`scanFile`（AST 扫描）· `skippedLiterals`（三类规则级豁免）· `headerNameArg` / `cookieNameArg`（`OH-5` 与 Cookie 名）· `responseSurfaceDirs`（从文档解析范围）· `parseAllow`（逐条例外，拒绝目录形式与空理由）。

**必须遵守的上位约束**：`OH-1`…`OH-5`（本轮的依据）· `AR-22` · `TM-6` / `TM-7` · `TB-15`（静态检查与门禁）。

---

## 5. 测试与场景

**负向验证**（检查类工具的证据必须是「它真能抓到」，而不是「它绿了」）：

| # | 场景 | 输入 / 前置 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 1 | 响应体字面量 | 临时文件里写 `<html>powered by honeypot</html>` | 报 `OH-1` | ✅ | `edge/proxy/zz_leak_probe.go:4` |
| 2 | 响应头名 | `w.Header().Set("X-Decoy-Marker", ...)` | 报 `OH-1` | ✅ | 同上 `:6` |
| 3 | Cookie 名 | 字面量 `"canary"` | 报 `OH-1` | ✅ | 同上 `:8` |
| 4 | **决策信息回传** | `w.Header().Set("X-Agent-Capture-Decision", "block")` | 报 `OH-5` | ✅ | 探针 `zz_oh5_probe.go:7` |
| 5 | 同上（分数） | `w.Header().Set("X-Risk-Score", "95")` | 报 `OH-5` | ✅ | 同上 `:8` |
| 6 | 文件级过滤器声明 | `responder/blacklist.go` 带 `//check-leak:filter` | 跳过该文件并**打印**声明与理由 | ✅ | `已声明过滤器文件：…blacklist.go —— …` |
| 7 | 逐条例外 | `allow.txt` 两条（内部后端名 / 环境变量名） | 命中且不报错，并打印理由 | ✅ | `已登记豁免 2 条` |
| 8 | 过期豁免 | （构造：删掉被豁免的字面量） | 报 `ALLOW 豁免已过期` | ✅ | 本轮修 `used` 标记时实测到该路径 |
| 9 | 解析结果骤降 | 把响应面模块改名 | **直接失败**，不静默少扫 | ✅ | 实测：`modules.md 里找不到这些响应面模块（改名了？）：adapter-proxy` |
| 10 | 撤掉探针 | 删除临时文件 | 检查回到通过 | ✅ | `泄漏检查通过。` |

**没有覆盖的（重要）**：

- **`OH-5` 的响应头检测目前没有任何真实数据**：`grep -rn "Header().Set(" edge/ core/`（非测试）为空 —— 仓库当前**不设置任何自定义响应头**。该检测是给后续代码用的护栏，本轮用探针证明其有效；
- 拼接串、模板变量、从配置读入的值（见 §2 未决 2）；
- 项目名 / 二进制名 / 服务名（未定名）；
- `scripts/check-leak` 自身**没有单测**（与 `archcheck` / `tracecheck` / `licensecheck` 一致：项目未给工具类脚本建测试约定）。因此本轮用**负向探针**作为证据，而不是单测。

---

## 6. 验证证据

```console
$ make leakcheck
已声明过滤器文件：core/internal/responder/blacklist.go —— 本文件就是过滤器：…
已登记豁免 2 条（见 scripts/check-leak/allow.txt）：
  · OH-1 core/internal/director/director.go  "mirage" —— 内部逻辑后端名…
  · OH-1 edge/proxy/cmd/proxy/main.go  "SHEN_PROXY_MIRAGE" —— 环境变量名…
泄漏检查通过。
  字符串字面量 ↔ OH-1 禁用清单 · 响应头 ↔ OH-5（决策与分数不回传）· 豁免逐条登记（OH-4）

$ make gate
fmt-check 通过
go vet ./...
staticcheck ./...
errcheck ./...
archcheck … 架构检查通过。
trace … 追溯检查通过。
leakcheck … 泄漏检查通过。
licensecheck … 许可审计通过。
go test -race ./...  → ok shen/edge/proxy 1.895s（等）
门禁通过。
```

**关键指标**：违规 **0** 条；逐条例外 **2** 条；文件级过滤器声明 **1** 个；扫描模块 **10** 个（清单解析自 `modules.md`）；新增依赖 **0**。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 项目名 / 二进制名 / 服务名未定 ⇒ `OH-1` 这三类无从扫描 | 定名后可能出现泄漏 | ADR-0004 定名后补进 `banned` |
| 2 | 拼接串与模板变量扫不到 | 检查不完备（README 已写明） | 接入演练 + `OH-2` 人工核对 |
| 3 | **`NI-12`（`V-1…V-5` 故障注入测试必须纳入 CI）未落地** —— 全仓只有 `tracecheck` 的注释提到 V-1 | 决策路径改动后没有回归防线 | 另开一轮（属 `NI-12`，与 `NI-10` 熔断、`NI-3/4/5` 降级路径同批） |
| 4 | **`AR-26` 的两条启动期断言只做了一半** —— `core/cmd/core` 有 `AR-30` 一致性断言，但「判定超时 ≤ 延迟预算」「心跳/租约周期 > 上报周期」未断言 | 配置不当时不会启动即失败 | 与心跳/租约（阶段 2b）一并落地 |
| 5 | **`NI-7`（cgroup 上限）与 `ST-17`（存活/就绪探针）没有部署物料** —— `deploy/` 只有 `config.example.yaml` | 交付形态缺少运行时约束与探针 | 接入物料（`docs/integrate/` 或 `deploy/`）另行落地 |
| 6 | `scripts/doctor`（接入自检）与 `scripts/sentinel`（差异哨兵）仍是占位 | 接入验收与「不可区分」验证无工具 | 分别对应接入演练与威胁模型 R-1 |
| 7 | `edge/mirror` 缺 `iface.go`（`tracecheck` 的 `TC-1` 提示） | 三条文件约定未满足（尚未升格为规则） | 与 `edge/mirror` 下一轮一起处理 |

> 第 3–6 条是**本轮一致性核查的产出**：它们不是本轮引入的问题，而是「已确认规则 ↔ 代码」的既有缺口，登记在此以免丢失。

---

## 7.1 审视记录（L 档，做法见全局技能 `audit`）

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `scripts/check-leak/` 只有 README（状态「⏳ 尚未实现」），规则 `OH-3` 却要求「必须提供」 | **规则未实现** | `ls scripts/check-leak/` 只有 `README.md`；`grep -n leakcheck Makefile` 空 | 实现检查本体 + `allow.txt` + 接进门禁；README 改状态 | ✅ |
| 2 | `decoy` 的**投放片段**含「指令文件蜜饵」「凭证蜜饵」与表名 `decoy_accounts` | **真实泄漏**（片段会被投放进客户环境，对手可能读到） | 新检查首次运行命中 `core/internal/decoy/decoy.go:98,111` | 改中性措辞（`# 投放位置…` / `app_accounts`） | ✅ |
| 3 | `adapter-proxy` 启动日志用「引流」而 `terminology.md` §4.1 的三值是「改道」 | 漂移（术语）+ 泄漏面 | 检查命中 `edge/proxy/cmd/proxy/main.go:123` | 改「可按决策改道」 | ✅ |
| 4 | `OH-5` 的响应头识别只认 `X.Header().Set(...)` 链式写法 | **检查缺陷**（漏报） | 负向探针 `h.Set("X-…-Decision", …)` 未被抓到 | 放宽为「接收者名里含 `Header`」，探针复测通过 | ✅ |
| 5 | 检查器首版把「豁免」判成了「未使用」—— 被抑制的命中没回填 `used` | 检查缺陷 | 首次运行报 2 条 `ALLOW 豁免已过期`（实际刚登记） | `allowEntry` 改指针，抑制时标记 `used` | ✅ |
| 6 | 检查器首版对 `**\`adapter-proxy\`**`（加粗 + 行内代码）解析失败 | 检查缺陷 | 运行报 `modules.md 里找不到这些响应面模块（改名了？）：adapter-proxy` | 剥离加粗标记后重解析 | ✅ |
| 7 | `staticcheck` 报 `S1011`（可用 `append(...)` 展开） | 门禁红 | `make gate` → `scripts/check-leak/main.go:269` | 改写为 `append(out, scanFile(...)...)` | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | `OH-3` 落地：新增 `scripts/check-leak`（`OH-1` / `OH-5` 检查 + 三类豁免 + 过期即报），接进 `make lint` / `gate` / `check`；修 `decoy` 投放片段的真实泄漏与 `adapter-proxy` 的日志措辞 | `OH-1`…`OH-5` · 本轮开发 |
