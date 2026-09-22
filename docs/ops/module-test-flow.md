# 功能模块测试流程（24 模块 · 逐模块证据链 · 伪造流量分层）

> **这是什么**：一轮「怎么把 24 个模块逐个验一遍」的**可执行流程** + **伪造流量清单**。
> 命令与场景都是活的（跑得起来、会红）—— 本文只负责把它们组织成一条路。
>
> **两句话用起来**：
>
> ```sh
> make verify-modules     # 逐模块跑单测并出表（约 30s，不需要 Docker）
> make traffic            # 端到端伪造流量（需要起栈；分层见 §3）
> ```

---

## 0. 为什么需要「逐模块」这一层

`make gate` 把 240+ 用例混着跑 —— 绿了，但**看不出哪个模块过没过**。
而每个模块文档里其实早就写好了「它怎么验」（`## 4. 关键规则` 引了哪些规则 / `## 7. 测试` 指向哪些测试）。
本流程把那两节**解析成检查表**，于是三种漏法都会当场报出来：

| 漏法 | 谁会发现 | 判据 |
| --- | --- | --- |
| 文档存在但**没有单测** | `make verify-evidence` | 模块文档 §7 解析不出任何测试目标（`MD-22`） |
| 有测试但**从没跑过** / 跑不过 | `make verify-modules` | 真的执行 `go test` / `pytest`，非零即红 |
| §4 **没引任何规则** | 同上 | 追不回设计依据（`MD-2`） |

> 「明知没有测试却登记在案」的模块（`adapter-dns` 纯配置 · `netpolicy` 声明式 · `honeypot-shell` 状态行写明**推迟**）
> 记**豁免**，不报红 —— 假红比漏报更坏，它会训练人忽略这个检查。

---

## 1. 逐模块流程（`make verify-modules`）

```sh
make verify-modules                    # 全部 24 个模块
make verify-evidence                   # 只核证据是否齐备（纯解析，<1s；已进 make gate）
go run ./scripts/verify -only judge,policy   # 只看几个模块
go run ./scripts/verify -json          # 机器可读（CI / 报告用）
```

它做四件事，逐模块：

| # | 核什么 | 从哪来 | 红了说明 |
| --- | --- | --- | --- |
| 1 | 模块文档存在 | `docs/design/modules.md` §1.1 的清单 | `MD-17`：目录有了、文档没了 |
| 2 | §4 关键规则至少引一条规则 ID | 模块文档 §4 | 追不回设计依据（`MD-2`） |
| 3 | §7 测试能解析出测试目标 | 模块文档 §7 | 模块不可独立测试（`MD-22`） |
| 4 | **真的把测试跑一遍** | `go test` / `pytest` | 测试失败，或**数不出用例数**（文档与代码脱节） |

**实测输出（2026-09-21，终版代码）**。**用例数是该模块 §7 自己列出的目标的实际执行结果**
（`§7` 里的 `（`test_xxx_*`）` 过滤词会真的变成 pytest 的 `-k`；含该模块的「运行时链路」smoke），
**不是**整个测试文件的用例数 —— 所以同一份文件被多个模块引用时，各模块的数字**不相同**是对的：

```text
judge 3 · director 31 · responder 16 · session 4 · isolation 9 · policy 98 · telemetry 7 · store 9
control 18 · decoy 13 · honeypot 15 · edge-injection 100 · adapter-mirror 3 · adapter-proxy 90
honeypot-protocol 7 · intent 12 · chain 11 · strategy 12 · llm-components 52 · ai-capability 81 · console 27
```

```text

```text
模块                   层      文档   规则   测试     实测           功能场景
--------------------------------------------------------------------------------
judge                核心     ✅    7    ✅      ✅ 3 例        probe-git-headless · … · normal-chrome-css
director             核心     ✅    18   ✅      ✅ 31 例       method-delete · whitelist-monitoring · takeover-mirage-routed（需 takeover 形态起栈）…
responder            核心     ✅    9    ✅      ✅ 16 例
session              核心     ✅    5    ✅      ✅ 4 例        session-reuse · session-distinct · boundary-*
isolation            核心     ✅    6    ✅      ✅ 9 例        session-reuse · session-distinct
policy               核心     ✅    10   ✅      ✅ 98 例       probe-gitignore · endpoint-* · injection-* · method-put
telemetry            核心     ✅    3    ✅      ✅ 7 例
store                核心     ✅    4    ✅      ✅ 9 例
control              核心     ✅    4    ✅      ✅ 18 例       probe-* · scanner-* · boundary-* · takeover-block-403
decoy                核心     ✅    16   ✅      ✅ 13 例
honeypot             核心     ✅    10   ✅      ✅ 15 例       takeover-mirage-routed
edge-injection       L1     ✅    11   ✅      ✅ 100 例      takeover-mirage-injected
adapter-mirror       L1     ✅    7    ✅      ✅ 3 例
adapter-proxy        L1     ✅    30   ✅      ✅ 90 例       whitelist-monitoring · normal-* · takeover-*
adapter-dns          L1     ✅    10   豁免     —            （无源码：验证靠两条 dig）
honeypot-protocol    L2     ✅    7    ✅      ✅ 7 例
honeypot-shell       L2     ✅    5    豁免     —            （状态行：推迟，用户裁定）
netpolicy            L3     ✅    6    豁免     —            （无源码：声明式产物）
intent               L4     ✅    7    ✅      ✅ 12 例
chain                L4     ✅    5    ✅      ✅ 11 例
strategy             L4     ✅    5    ✅      ✅ 12 例
llm-components       L4     ✅    14   ✅      ✅ 52 例
ai-capability        L4     ✅    14   ✅      ✅ 81 例       takeover-mirage-injected（需 takeover-ai 形态起栈）
console              控制台    ✅    5    ✅      ✅ 27 例
--------------------------------------------------------------------------------
✅ 全部模块的证据链齐备（文档 · 规则依据 · 测试目标 · 功能场景）
```

**两条工程细节**（都是踩出来的，写在这里免得重犯）：

1. **同一份测试只跑一次**：`test_aicap_l4_tasks.py` 被 intent / chain / strategy / ai-capability 共同引用 ——
   不共享结果时同一份 pytest 会启动 4 次，总时长从 **2m35s** 涨到没必要的地步（现在 **30s**）；
2. **「跑绿了但数不出用例数」不算通过**：那通常意味着文档里的测试名已经与代码脱节，
   或者解析规则过期（实测：多带一个 `-q` 会把 `7 passed` 汇总行也抑制掉，于是 7 个用例数成 0）；
3. **同一个文件的多个过滤词是两个目标**：按「文件 + 参数」分槽记结果，否则后一次会覆盖前一次
   （用例数少算、失败也会被盖掉）；文档里路径外面那层反引号要先吃掉，否则 `（`test_xxx_*`）` 全部解析失败、
   静默退化成「跑整个文件」—— 数字看着更大，其实已经不属于这个模块。

---

## 2. 架构符合性（`make archcheck` + 符合性报告）

「实现是否按架构设计来」分两层判，**能机器核的绝不靠人读**：

| 层 | 谁在核 | 在哪 |
| --- | --- | --- |
| **结构面**（可机器核） | `make archcheck`（13 项，进 `make gate`） | §[`architecture-conformance.md`](architecture-conformance.md) §2 |
| **语义面**（核不了） | 符合性报告：规则 → 实现 → 证据 → 命令 | 同上 §3 |

本轮**新增**三条可机器核的解耦判据（都做过反证：注入违规必然报红）：

| 新增检查 | 规则 | 抓什么 |
| --- | --- | --- |
| 决策取值**闭集** | `MD-12` | 有人顺手 `iota` 出第四个 Action —— 它会落到 `String()="unknown"`，下游按字符串比较就**静默当放行** |
| 响应路径**纯度** + `judge` **纯函数** | `AR-30` / `MD-6` | 响应路径引入 `math/rand`（同会话同资源不同答案）；`judge` 引入 `time`/`os`/`net`（判定不再可复现） |
| L4 **不碰写侧** | `AR-32` | `analysis/` 生成策略面桩或出现策略客户端 —— 那是第二条「分析 → 处置」通路 |

---

## 3. 伪造流量（分层）

现有 **36 条**场景（`scripts/traffic/scenarios.json`），每条都带 `modules`（这条流量证明哪些模块在工作）。
按**形态**分三层跑，别混：

| 形态 | 怎么起栈 | 能验什么 | 命令 |
| --- | --- | --- | --- |
| **shadow**（默认） | `make up`（或 `make demo`） | 判定与规则精度：分值 · 信号 · 三值决策 · 会话与缓存 · 边界 —— **只算不处置** | `make traffic` |
| **takeover** | `docker compose -f deploy/docker/compose.yaml -f deploy/docker/compose.verify-mirage.yaml up -d` | **真的处置**：改道到幻境后端（`executed=mirage`）· 拦截（403）· 白名单跳判 | `make traffic ARGS='--group "接管 · 改道"'` |
| **takeover-ai** | 同 takeover + `ai.enabled=true` + `ai.manifest` + `SHEN_PROXY_INJECT_CONTENT=true` | AI 生成内容真的注进去（`inject=applied` + `content_id`） | 见 [`ai-injection-2026-09-21/README.md`](ai-injection-2026-09-21/README.md) §7 |

**分组一览**（36 条）：

| 分组 | 条数 | 验什么 | 归属模块 |
| --- | --- | --- | --- |
| 自动化探针 / 扫描器指纹 | 4 + 5 | UA 指纹类规则命中与分值 | `judge` · `control` |
| 规则精度 | 7 | 前缀匹配的误伤与绕过（**含 8 条已知缺口**） | `judge` · `policy` |
| 敏感端点 / 破坏性方法 | 5 + 1 | 路径类规则 · DELETE（PUT/PATCH 未建模，登记为缺口） | `judge` · `director` |
| 会话与缓存 | 2 | 判定缓存复用 / 跨会话新判定（`ST-10`） | `session` · `isolation` |
| 白名单 | 1 | 免判定来源（`INT-25`） | `director` · `adapter-proxy` |
| 正常对照 | 4 | 正常用户**不得**被改道（`NI-1` 的对照面） | `judge` · `director` · `adapter-proxy` |
| 边界 | 4 | 超长 UA / 超长路径 / 空 UA / 非 ASCII 路径 | `control` · `session` |
| **接管 · 改道** | 1 | `route_mirage` ⇒ **`executed=mirage`** | `director` · `adapter-proxy` · `honeypot` |
| **接管 · 拦截** | 1 | `block` ⇒ **403** + `executed=block` + 不注入 | `director` · `adapter-proxy` · `control` |
| **接管 · 注入** | 1 | `inject=applied` + `content_id`（需 AI 层打开） | `ai-capability` · `adapter-proxy` · `edge-injection` |

**三个已经帮你处理掉的坑**（都是实测撞出来的）：

1. **第一条请求要热身**：冷连接 + 适配器还没 Pull 到策略的窗口里会 failopen（`K-24`），
   只跑单条场景时第一条几乎必然假红。`send.py` 现在**自动热身**，失败时打一行提示（不静默）；
2. **`executed` / `inject` 只在逐请求链路里**（`/api/graphs`），判定记录（`/api/flow`）没有这两个字段 ——
   断言它们时要按 `decision_id` 去链路里取，并**等**（异步上报 `AR-11`）；
3. **形态不匹配记「观察」，不记失败**：影子栈里跑 `stack: takeover` 的场景、或 AI 关闭时跑
   `stack: takeover-ai` 的场景，会打印一行原因（例：`AI 层未打开（核心快照 ai.enabled=false）`）——
   报成失败只会训练人忽略这个检查。

**仿真的边界**（别当成真实攻击）：路径与 UA 是**规则能识别的那一类**，不是完整攻击工具；
载荷只覆盖判定字段（`path` / `query` / `method` / `UA`），没有真实的漏洞利用流量。

---

## 4. 一条命令跑完全部（人工验收）

```sh
make pyenv                        # 首次：L4 环境
make gate                         # 结构 + 逐模块证据 + 追溯 + 单测（离线）
make verify-modules               # 逐模块跑单测（30s）
make up                           # 起栈（Docker）
make traffic ARGS='--check-graph --check-l4'   # 端到端 + 链路 + L4 结论
make down
```

> **不在这一轮里的**：`scripts/shen.sh verify`（状态 + 全量流量 + L4 + DAG 一致性 + 报告）
> 需要 Docker，属仓库级验证；`make ai-check` / `make ai-check-llm` 是 AI 注入的专项验收。
