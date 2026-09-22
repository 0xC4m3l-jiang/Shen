# 变更包：逐模块功能测试流程 + 架构符合性机器判据 + 分层伪造流量

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | ① 逐模块功能测试流程（24 模块出表 + 真跑单测）② 架构解耦的三条**新机器判据** ③ 伪造流量按层补齐 + 修掉三个让它不稳的坑 |
| 日期 | 2026-09-21 |
| 状态 | 已实现（门禁绿 · 独立评审见 §7.3） |
| 涉及模块 | 全部 24 个（新增的是**验证**而非产品功能）；`scripts/` · `docs/ops/` · `scripts/traffic/` |
| 决策数 | 已答 4 项（§2 的 Q1–Q4）· 无新 ADR（不引入依赖、不改契约、不改判定） |
| 关联 | [`docs/ops/module-test-flow.md`](../ops/module-test-flow.md)（本轮产物）· [`docs/ops/architecture-conformance.md`](../ops/architecture-conformance.md)（本轮产物）· [`docs/kb/known-issues.md`](../kb/known-issues.md) `K-29`（本轮修掉的坑）· 上一轮 [`2026-09-21-ai-injection-verification.md`](2026-09-21-ai-injection-verification.md) |

---

## 1. 需求与验收

**要解决什么**（用户原话拆解）：

1. 实现**完整的「功能模块测试流程」**；
2. 确认**实现是否按架构设计来**（架构符合性）；
3. 保证架构逻辑的**解耦**与功能的**稳定**；
4. 提供**详细的伪造测试流量**，方便测试验证。

**做完之后能做什么**：

- `make verify-modules`：24 个模块逐个列出「文档 / 引了哪些规则 / 测试目标 / **真实测得的用例数** / 对应哪些功能场景」，跑不过就红；
- `make archcheck`：**13 项**解耦判据（新增 3 项），含「决策取值闭集」「响应路径纯度」「L4 不碰写侧」；
- 伪造流量按**形态**分三层跑（影子 / 接管 / 接管+AI），每组都标了它证明哪些模块在工作。

**验收判据**：

1. `make gate` 全绿（含新增的 `verify-evidence`，进 `lint`）；
2. `make verify-modules` **24/24** 模块证据链齐备，且实测用例数与代码相符（豁免 3 个：dns / netpolicy / honeypot-shell）；
3. 新增的 3 条 archcheck 判据**每条都做过反证**（注入违规必然报红），且**没有**加「永远为真」的假判据；
4. 接管形态下新场景真的断言：`takeover-mirage-routed`（`executed=mirage`）与 `takeover-block-403`（403 + `executed=block`）**✓**；
5. 符合性文档引用的 **47 个测试名全部真实存在**（可 grep 复跑，缺失为 0）。

**不做什么**：

- **不给每个模块重写一套功能测试**（既有 240+ 用例就是它们的证据；重写会重复并腐烂）；
- **不改产品代码的行为**（唯一的产品侧改动是**演示物料** `scripts/demo/business.py` 的单线程→多线程，见 §4）；
- **不引入依赖**（全用标准库 + 既有 Go 工具链）；
- **不把要起栈的端到端流量塞进 `make gate`**（gate 保持离线、4 秒级）。

---

## 2. 设计逻辑

**⑧ 项需求 → 四个抉择**（终端非交互，按 `grill_deck` 的降级形态给出「编号 + 推荐答案」，并据此实施）：

| # | 问题 | 选定 | 为什么不选另一条 |
| --- | --- | --- | --- |
| Q1 | 「模块功能测试流程」怎么落地 | **编排既有测试 + 逐模块出表**（`scripts/verify`），只给空缺补测试 | 给 24 个模块各写一套功能测试 = 与既有 240+ 用例重复，且必然腐烂 |
| Q2 | 「是否按架构设计实现」判据放哪 | **两层**：可机器核的进 `archcheck`（`make gate` 硬拦）；语义级进符合性报告（引用可 grep 核） | 只扩 archcheck → 语义级规则永远核不了；只出报告 → 不拦，会腐烂 |
| Q3 | 伪造流量的深度与归属 | **分两层**：离线（逐模块单测，进 gate）与在线（`scenarios.json` 按形态/模块分组） | 只做在线 → 必须起 Docker 才能验；只做离线 → 真实链路没人验 |
| Q4 | 是否进 `make gate` | **结构/证据级进 gate**；端到端单独目标 | 端到端要起栈（1–2 分钟 + Docker）⇒ 塞进 gate 会让门禁变慢变脆 |

**流程怎么组织的**（`scripts/verify` 的四件事，清单**从文档解析**）：

```text
docs/design/modules.md §1.1   → 24 个模块（名称/层/语言/源码目录/文档/阶段）
   ├── 模块文档 §4 关键规则   → 引了哪些规则 ID（MD-2：至少一条）
   ├── 模块文档 §7 测试       → 测试目标（裸文件名 / 仓库存根相对 / 文档相对，含「同上」继承）
   ├── 模块文档 状态行        → 「推迟 / 未建」⇒ 豁免（不是失败）
   └── scripts/traffic/scenarios.json 的 modules 标签 → 该模块有哪些功能场景
   ↓ 真的执行
go test -count=1 -v ./<pkg>  ·  analysis/.venv/bin/python -m pytest <file> [-k <过滤>]
```

**关键工程决定**（都踩过）：

| 决定 | 为什么 |
| --- | --- |
| 测试目标**去重共享**（同 (命令, 目录) 只跑一次） | 同一份 pytest 被 4 个模块引用 ⇒ 重复启动 4 次，实测 2m35s → **30s** |
| **不**补 `-q` | `pyproject.toml` 已 `-q`，再补一次变 `-qq`，连 `7 passed` 汇总行都没了 ⇒ 用例数恒为 0 |
| 「跑绿但数不出用例数」**判失败** | 那通常意味着文档里的测试名已与代码脱节（或解析规则过期）—— 静默 0 就是假绿 |
| 可执行程序**白名单**（只有 `go` 与 venv python） | 测试目标是从**文档**解析出来的；钉死可执行程序，文档里写了别的也只会成为参数 |
| 形态不匹配记**观察**不记失败 | 影子栈跑 takeover 场景、AI 关闭跑 takeover-ai 场景，是**形态没打开**，不是被测行为不达标 |

---

## 3. 追溯矩阵

| 需求 / 规则 | 文档章节 | 代码 | 测试 / 命令 | 验证命令 |
| --- | --- | --- | --- | --- |
| 逐模块证据链（`MD-2` / `MD-17` / `MD-22`） | [`ops/module-test-flow.md`](../ops/module-test-flow.md) §1 | `scripts/verify/main.go` · `scripts/internal/modules/modules.go` | 24 模块表（含豁免判定） | `make verify-modules` · `make verify-evidence` |
| 决策取值闭集 | [`ops/architecture-conformance.md`](../ops/architecture-conformance.md) §1 | `scripts/archcheck/main.go`（检查 10） | 反证：加 `ActionQuarantine` 必红 | `make archcheck` |
| 响应路径纯度 / `judge` 纯函数（`AR-30` / `MD-6`） | 同上 | `scripts/archcheck/main.go`（检查 11） | 反证：加 `math/rand` / `time` 必红 | `make archcheck` |
| L4 不碰写侧（`AR-32`） | 同上 | `scripts/archcheck/main.go`（检查 12） | 反证：造 `analysis/proto/policy/` 必红 | `make archcheck` |
| 解耦主张 ↔ 实现 ↔ 证据 | 同上 §2 | 全仓（引用表） | **47 个测试名**逐个核存在（缺失 0） | §6 ③ 的 grep 命令 |
| 伪造流量（判定 / 处置 / 注入） | [`ops/module-test-flow.md`](../ops/module-test-flow.md) §3 · [`scripts/traffic/README.md`](../../scripts/traffic/README.md) | `scripts/traffic/scenarios.json`（36 条，含 `modules`/`stack`）· `scripts/traffic/send.py` | 接管形态：2/2 断言 + 1 观察 | `scripts/shen.sh traffic --group …` |
| 演示站并发（`NI-1` 的对照面可信） | [`kb/known-issues.md`](../kb/known-issues.md) `K-29` | `scripts/demo/business.py`（`ThreadingHTTPServer`） | 接管场景由「必超时」变「**✓**」 | 同上 |

---

## 4. 代码实现

**新增**

| 文件 | 为什么 |
| --- | --- |
| `scripts/internal/modules/modules.go` | **共享**的 `modules.md §1.1` 解析器（`archcheck` 与 `verify` 两个调用方 —— 各写一份会漂移） |
| `scripts/verify/main.go` | 逐模块功能测试流程：解析文档 → 收集证据 → 真跑测试 → 出表 / JSON |
| `docs/ops/module-test-flow.md` | 流程说明 + 24 模块实测表 + 伪造流量分层 + 三个坑 |
| `docs/ops/architecture-conformance.md` | 架构符合性：13 项结构检查 + 语义面引用表 + 本轮新增判据的反证 + 诚实缺口 |

**修改**

| 文件 | 改什么 |
| --- | --- |
| `scripts/archcheck/main.go` | 新增检查 10/11/12（决策闭集 · 热路径纯度 · L4 写侧）；改用共享模块解析器；自述与汇总行同步 |
| `Makefile` | `verify-modules`（跑测试）· `verify-evidence`（只核证据，**进 `lint`**） |
| `scripts/traffic/scenarios.json` | 36 条（+3 接管场景）· 每条加 `modules` · 新增 `stack` 字段 · 自述补 `executed`/`inject`/形态说明 |
| `scripts/traffic/send.py` | 逐场景 `executed`/`inject` 断言 · **自动热身**（去 `K-24` 假红）· 落点/注入从 `/api/graphs` 取 · 读核心快照判定 AI 是否打开 · 形态不匹配记观察 |
| `scripts/demo/business.py` | 单线程 `HTTPServer` → `ThreadingHTTPServer`（**演示物料**；见 `K-29`） |
| `docs/README.md` · `docs/ops/runbook.md` · `docs/ops/functional-verification.md` | 指到两条新流程与新目标 |
| `scripts/traffic/README.md` | 补上本轮的字段（`modules` / `stack` / `executed` / `inject`）· 形态分层 · 两个新坑（热身、落点要去链路里取）· 场景条数 33→36 |
| `docs/kb/known-issues.md` | 新增 `K-29`（单线程测试替身会把并发测试变成随机红灯） |

**没有改的**：产品代码（`common/` · `modules/deception|honeypot|console`）与 `docs/design/` 一行未动
—— 本轮只加**验证**与**演示物料**；`git diff --stat` 可核。

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | 24 模块逐个体检 | 每个都有文档 + 规则引用 + 测试目标 | ✅ 24/24（3 个豁免：dns / netpolicy / honeypot-shell） | `make verify-modules` |
| 2 | 真跑每个模块的单测 | 全绿，且用例数 > 0 | ✅ judge 3 · policy 98 · adapter-proxy 90 · intent 12 / chain 11 / strategy 12 · …（**按该模块 §7 的过滤词实际执行**，不是整个文件） | 同上（§6 ②） |
| 3 | 文档里写「没有单测」的模块 | 记豁免而非失败 | ✅ adapter-dns / netpolicy | 同上 |
| 4 | 文档状态行写「推迟」的模块 | 记豁免而非失败 | ✅ honeypot-shell | 同上 |
| 5 | 测试名与代码脱节（模拟） | 判失败（不许静默 0 例） | ✅ 规则已实现（`Cases==0` ⇒ 失败） | `scripts/verify/main.go` |
| 6 | 决策取值闭集 | 第四个取值必报红 | ✅ 注入 `ActionQuarantine` ⇒ MD-12 报「实际 4 个」 | §6 ④ |
| 7 | 热路径纯度 | `math/rand` 必报红 | ✅ 注入后 AR-30 报「不得引入伪随机」 | §6 ④ |
| 8 | `judge` 纯函数 | `time` 必报红 | ✅ 注入后 MD-6 报「judge 必须保持纯函数」 | §6 ④ |
| 9 | L4 不碰写侧 | 策略桩必报红 | ✅ 注入 `analysis/proto/policy/` ⇒ AR-32 报错 | §6 ④ |
| 10 | 符合性文档的引用 | 47 个测试名全存在 | ✅ 缺失 **0** | §6 ③ |
| 11 | 接管形态：改道真的执行 | 200 + `route_mirage` + `executed=mirage` | ✅ ✓ | §6 ⑤ |
| 12 | 接管形态：拦截真的 403 | 403 + `block` + `executed=block` + `inject=off` | ✅ ✓ | §6 ⑤ |
| 13 | 接管- AI 关闭时的注入场景 | 记**观察**并打印原因（不是失败） | ✅ 「AI 层未打开（核心快照 ai.enabled=false）」 | §6 ⑤ |

**没有覆盖的**：`takeover-ai` 形态下的 `inject=applied` 断言（要起「接管 + AI 打开」的栈；
本轮用本地进程栈的 [`ai-injection-2026-09-21`](../ops/ai-injection-2026-09-21/README.md) 报告覆盖同一行为）；
`make verify-modules` 不做跨模块的**语义**判据（那是 `archcheck` 与符合性报告的事）。

---

## 6. 验证证据

**① `make gate`**

```console
$ make gate
127 passed in 3.29s
门禁通过。        # 含新增的 verify-evidence（逐模块证据链）
```

**② `make verify-modules`（节选，全文见 [`module-test-flow.md`](../ops/module-test-flow.md) §1）**

```console
模块                   层      文档   规则   测试     实测
judge                核心     ✅    7    ✅      ✅ 3 例
policy               核心     ✅    10   ✅      ✅ 98 例
adapter-proxy        L1     ✅    30   ✅      ✅ 90 例
intent               L4     ✅    7    ✅      ✅ 12 例
adapter-dns          L1     ✅    10   豁免     —
honeypot-shell       L2     ✅    5    豁免     —
✅ 全部模块的证据链齐备（文档 · 规则依据 · 测试目标 · 功能场景）
real 0m30.5s        # 去重前 2m35s
```

**③ 符合性文档的引用核验**

```console
$ grep -oE '`Test[A-Za-z0-9_]+`|`test_[a-z0-9_]+`' docs/ops/architecture-conformance.md | tr -d '`' | sort -u | while read -r t; do
    grep -rq "func $t(" --include='*_test.go' . || grep -rq "^def $t(" --include='*.py' analysis/tests/ || echo "缺失：$t"; done
引用总数: 47
缺失总数: 0
```

**④ 三条新判据的反证（注入违规 → 必红 → 还原 → 绿）**

```console
✗ MD-12  决策取值必须是闭集（恰好 3 个），实际 4 个：ActionOrigin / ActionMirage / ActionBlock / ActionQuarantine
✗ AR-30  响应路径不得引入伪随机（同会话同资源必须同答案）：import math/rand
✗ MD-6   judge 必须保持纯函数（不用时钟/随机/网络/文件），但它 import 了 time
✗ AR-32  analysis/proto/ 下只允许 telemetry 契约（L4 与核心的唯一通道），出现了 policy
（还原后）架构检查通过。
```

**⑤ 伪造流量：与形态无关的红绿判据（P1 修复后复测）**

形态的**权威信号**取控制台聚合视图 `/api/topology` 的 `shadow`（实测：逐请求链路 `/api/graphs` 的行里
**没有** shadow —— 那是聚合视图的字段）。两种形态各跑一遍，行为相反且都正确：

```console
# 形态一：影子栈（make up 默认；/api/topology 的 shadow=true）⇒ 三条都只能观察，退出码 0
$ make up && scripts/shen.sh traffic --group "接管 · 改道" --group "接管 · 拦截" --group "接管 · 注入"
takeover-mirage-routed    接管 · 改道   200   0.90  route_origin ua-headless,path-probe  观察
                            ↳ 影子栈（/api/topology 的 shadow=true）⇒ 只算不处置，本场景只能观察；…
takeover-mirage-injected  接管 · 注入   200   0.90  route_origin ua-headless,path-probe  观察
                            ↳ AI 层未打开（核心快照 ai.enabled=false）⇒ 本场景只能观察；…
takeover-block-403        接管 · 拦截   200   1.00  route_origin ua-sqlmap,path-probe   观察
断言 0/0 通过 · 观察 3 条 · 缺口 0 条 · 出口卫生问题 0 条        # exit=0

# 形态二：接管覆盖栈（shadow=false）⇒ 真的断言，处置确实发生
$ docker compose -f deploy/docker/compose.yaml -f deploy/docker/compose.verify-mirage.yaml up -d
$ scripts/shen.sh traffic --group "接管 · 改道" --group "接管 · 拦截" --group "接管 · 注入"
takeover-mirage-routed    接管 · 改道   200   0.90  route_mirage ua-headless,path-probe  ✓
takeover-mirage-injected  接管 · 注入   200   0.90  route_mirage ua-headless,path-probe  观察
                            ↳ AI 层未打开（核心快照 ai.enabled=false）⇒ …
takeover-block-403        接管 · 拦截   403   1.00  block        ua-sqlmap,path-probe  ✓
断言 2/2 通过 · 观察 1 条 · 缺口 0 条 · 出口卫生问题 0 条        # exit=0
```

**P1 的核心：接管栈里改道没生效必须报红**（这是修复前会被「形状判据」放行的情形）。
直接对 `judge()` 喂合成输入，三种形态一次看清（`stack: takeover` + 决策 `route_mirage` + 落点 `origin`）：

```console
$ python3 - <<'EOF'   # 只调 scripts/traffic/send.py 的 judge()，不发请求
stack_form=shadow    ⇒ observe_only=True  失败数=0  ✓（形态没打开 ⇒ 观察）
stack_form=takeover  ⇒ observe_only=False 失败数=1  ✓（接管形态落点不对 ⇒ **报红**）
stack_form=unknown   ⇒ observe_only=False 失败数=1  ✓（形态读不到 ⇒ 不收宽，照样报红）
block 场景 影子栈 ⇒ 失败数=0（状态码 200 不再残留失败）· 接管栈 ⇒ 失败数=4
EOF
```

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | `judge` 只有 3 个用例（24 个模块里最少之一），而判定是核心价值 | 判定行为的回归网偏薄 | 建议随 `judge` 规则扩充补齐（不阻塞本轮） |
| 2 | `takeover-ai` 的在线注入断言未在 Docker 形态跑 | 该形态缺一条端到端 | 需「接管 + ai.enabled + 清单 + 注入开关」的覆盖文件（可参照 `compose.verify-mirage.yaml` 加一份） |
| 3 | `make verify-modules` 只读**模块文档**的 §7；模块文档若漏写某个测试文件，就不在流程里 | 证据链的准确性依赖文档同步 | `make trace`（`MD-22`）与符合性报告 §2 的引用表是第二道网 |
| 4 | `adapter-dns` / `netpolicy` 的验证仍是**人工两条 dig / 集群侧加载** | 这两个模块没有机器验收 | 已登记豁免；`INT-17` 自检覆盖一部分 |
| 5 | 跨语言「wire format 一致性」靠既有契约测试，不属本流程 | —— | 已有（`test_event_contract.py` 等） |
| 6 | 91 条既有场景里 8 条是**已知缺口**（规则精度类） | 端到端不是全绿 | 有意如此：缺口显式列出，不制造假绿 |
| 7 | 三条新判据**没有自动化回归守着**（本轮的反证是手工注入后还原） | 有人改 `contract/decision.go` 的书写形式时，`MD-12` 的正则会以假红形式失效 | 可加一个「故意违规的测试夹具」（评审提出）—— 单独一轮 |

### 7.1 审视记录（L 档）

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `archcheck` 与新的 `verify` 都要解析 `modules.md §1.1` | 重复维护（将要漂移） | 两处各有一份解析器 | 修正 | ✅ 抽成 `scripts/internal/modules`，两个调用方共用 |
| 2 | 我的第一条新判据把 `analysis/proto/__pycache__` 当成「非 telemetry 契约」 | 假红（我引入） | 实跑报 `出现了 __pycache__` | 修正 | ✅ 与别的检查同口径跳过工具目录 |
| 3 | 我最初想加「适配器不得构造 `contract.Decision`」 | **假防线**（永远为真） | `ST-3` 已禁止适配器 import `core/internal`，故那条永远为真 | 删除 | ✅ 没加；记在符合性文档 §3 |
| 4 | `verify` 第一版所有 pytest 都「失败」 | 我引入的缺陷 | 相对路径 + `cmd.Dir` ⇒ 找不到解释器 | 修正 | ✅ 用绝对路径 + 存在性检查 |
| 5 | `verify` 把「跑绿但数不出用例数」记成 ✅ | 假绿（我引入） | 多带一个 `-q` 导致 `N passed` 被抑制 | 修正 | ✅ 去掉多余 `-q`，且 `Cases==0` 直接判失败 |
| 6 | `send.py` 的 `-k` 过滤词把同行的 Markdown 链接吞了进去 | 假红（我引入） | `Wrong expression passed to '-k': [ADR-0022](…)` | 修正 | ✅ 只接受紧跟路径的 `（…）`，且每段必须像测试名 |
| 7 | 新场景第一条总超时 | 工具不稳（实测） | `takeover-mirage-routed timed out` | 修正 | ✅ 自动热身（`K-24` 那类窗口），失败有提示 |
| 8 | 落点/注入断言恒为空 | 断言取错接口（我引入） | 实测 `/api/flow` 无 `executed`/`inject`；`/api/graphs` 有 | 修正 | ✅ 按 `decision_id` 从链路取并等 |
| 9 | **演示业务站单线程 + HTTP/1.1 长连接 ⇒ 幻境那一跳 10~20s 无响应** | 真实缺陷（测试替身不可信） | 适配器日志 `落点=mirage … 字节=0 耗时=10112.1ms`；`/proc/net/tcp` 显示 LISTEN 而 `rx_queue=0` | 修正 | ✅ 改 `ThreadingHTTPServer`；新增 `K-29` 记录同类陷阱 |
| 10 | AI 未打开时 `takeover-ai` 场景报「注入结果 disabled ≠ applied」 | 假红（形态问题） | 核心快照 `ai.enabled=false` | 修正 | ✅ 读快照判定 ⇒ 记「观察」并打印原因 |
| 11 | 观察行里仍残留 `status_in` 断言失败（`block` 场景在影子栈必然 200）⇒ 整次运行**退出码 1** | 真缺陷（我引入的假红） | 修 P1 后实测 `exit=1`；`status_in` 判定在形态判定**之前** | 修正 | ✅ 形态判定提前到任何断言之前；退出码排除观察行 ⇒ `exit=0` |
| 12 | 观察说明打印在**行上方**，看起来像上一条场景的注解 | 可读性（会指错方向） | 对比 `tail` 输出与实际场景标签 | 修正 | ✅ 说明移到该行下方 |
| 13 | 我原以为「链路的 `shadow` 字段」是权威信号 —— 实测 `/api/graphs` 的**行里没有**该字段 | 判断错误（会退化成凭形状猜） | `curl /api/graphs` 的键集里无 shadow；`/api/topology` 有（`shadow=true` + 人话注解） | 修正 | ✅ 改用 `/api/topology` 的 `shadow`，并在代码里写明为什么不取链路字段 |

### 7.2 独立评审（L 档）

评审者：冷上下文子代理（只读文件与代码）。结论与逐条处置见 §7.3。

### 7.3 评审结论

冷上下文独立评审（只读文件与代码）结论：**有异议** —— 1 条 P1（观察态会吞掉真实失败）+ 7 条 P2；
评审同时核实：三条新判据**都真的接进 `main()` 且形状可触发**、`AR-30` 无假阳性风险、
「删掉那条永远为真的判据」的推理成立、流量期望与 `config.verify-mirage.yaml` 的阈值自洽（0.6+0.3=0.90 改道 /
0.7+0.3=1.00 拦截）、`ThreadingHTTPServer` 足以修掉挂死、抽查 22/47 个引用全部真实存在。

| # | 异议 | 级别 | 处置 |
| --- | --- | --- | --- |
| 1 | 「形态不匹配 ⇒ 观察」只看**结果形状**（`executed=origin` + `action≠origin`），分不出「影子栈（形态未开）」与「接管栈但改道没生效（真失败）」⇒ 忘挂接管覆盖文件也能绿着退出 | **P1** | ✅ 改用**权威信号** `graph.Shadow`（控制台自己算）：`shadow=true` 才降级为观察；`shadow=false/读不到` 一律报失败（文案指向「忘了挂接管覆盖文件？」） |
| 2 | `AR-32` 检查①：`os.ReadDir` 出错静默跳过、非目录项放行 ⇒ 目录被改名/删除时零发现 | P2 | ✅ 目录读不到即出 finding；非目录项也报（例外 `__init__.py`，Python 包脚手架）；两条分支都做了反证（造 `policy.proto` ✅ / 改名目录 ✅） |
| 3 | `AR-32` 禁词表漏 Python 惯例写法（`policy_pb2` / `PolicyService` / `common.api.policy` / `policy/v1`）⇒ 可绕过 | P2 | ✅ 补齐四个词（`PolicyService` 同时覆盖 `PolicyServiceStub`） |
| 4 | `AR-30` 只禁伪随机、`MD-6` 只精确匹配 `judge` 包 ⇒ 「响应路径纯度」这个说法**比检查做得到的事更大**；`judge` 一旦拆子包即漏 | P2 | ✅ `MD-6` 改为**前缀匹配**（含子包）；`AR-30` 的文案与符合性文档收窄为「热路径禁伪随机」并写明边界（抓不到 `time.Now()` 写进响应内容这类） |
| 5 | 目标去重只按文件 ⇒ 同一文件的不同 `-k` 目标**丢第二个**（静默少测）；且 `（` 紧邻规则让**所有**过滤词解析失败 ⇒ 表里的数字其实是整个文件的 | **P2（实际最重）** | ✅ 去重键改「文件 + 参数」；结果按同键分槽（不再互相覆盖）；**吃掉路径外面的收尾反引号**；支持 `test_xxx_*` 前缀写法。修后 intent 33→**12** · chain 33→**11** · strategy 33→**12** · llm-components 61→**52**（数字变小是对的：以前数的是整个文件） |
| 6 | 豁免判据是「§7 整节子串命中」⇒ 有测试的模块只要 §7 里出现某句话就整段跳过 | P2 | ✅ 改成**只有解析不出任何测试目标时才豁免**（有目标就照常跑） |
| 7 | 符合性文档写「13 项」但表里只有 12 行（漏了容器子目录那条） | P2 | ✅ 补第 2 行「容器子目录白名单（ST-2/ST-4）」并重新编号 1–13 |
| 8 | 文档里的引用核验命令只覆盖 Go（表里 14 个 Python 引用一个都不核）；贴的输出与该命令不符；引用去重后是 46 不是 47 | P2 | ✅ 命令补 Python 腿（`def name(`）+ `wc -l`；本文与 `module-test-flow.md` 的数字全部改成修后的实测值 |
| 9 | `scripts/traffic/README.md` 仍写「33 条」（实际 36）· 该文件不在变更包 §4 的清单里 · `send.py` 里 `if args.check_graph:` 整块重复 · `/api/config` 拉不到时静默把 `takeover-ai` 场景降级为观察 | 附 | ✅ 条数改 36 · 补进 §4 · 删重复块 · AI 开关改**三态**（`on`/`off`/`unknown`）：`unknown` 报「证据缺失，不当作通过」 |

**评审未能核实的**（环境限制，已如实记录）：无 shell ⇒ 未复现 `make gate` / `make verify-modules` 的输出、
未跑 Docker 接管栈；另指出**新判据没有自动化回归守着**（本轮的反证是手工注入后还原）——
这条已在 §7 遗留第 7 项登记。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-21 | 首版：逐模块功能测试流程（`scripts/verify` + 共享模块解析器 + `make verify-modules`/`verify-evidence`）· `archcheck` 新增 3 条解耦判据（决策闭集 / 热路径纯度 / L4 写侧，均带反证）· 伪造流量按形态分层（+3 接管场景 + `modules`/`stack` 标签）· 修 4 个工具坑（热身 / 落点接口 / `-k` 解析 / 演示站单线程）· 两份新文档 | 用户要求：完整的功能模块测试流程 · 确认实现符合架构设计 · 保证解耦与稳定 · 提供详细的伪造测试流量 |
