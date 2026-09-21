# 2026-09-21 · 顶层目录与「三个大模块」对齐

> 依据：用户指令「按照这三个方向进行目录调整并进行代码优化」·
> [ADR-0029](../../docs/background/decisions/0029-three-module-dirs.md)（新建）·
> [ADR-0028](../../docs/background/decisions/0028-three-module-view.md)（功能视角 ↔ 代码视角的映射，本文执行其目录面）·
> [ADR-0007](../../docs/background/decisions/0007-repo-layout.md)（**部分被取代**）
> 变更分级：**L**（顶层目录 + 已确认基线 `structure.md` §1.1 + 门禁脚本 + 约 330 行文档）

## 1. 需求

用户的三个大模块心智模型，目录名要能对上：

| 用户说的 | 期望的目录 | 改名前的实际叫法 |
| --- | --- | --- |
| ① 欺骗层（反向代理 + AI 欺骗注入） | `deception/` | `edge/`（名字对不上） |
| ② AI 蜜罐层（能启动/关联不同蜜罐） | `honeypot/` | `deception/honeypot/`（`deception/` 这个名被抢用了） |
| ③ 管控平台（看两层流量日志） | `console/` | `console/` ✓ 已对齐 |

**核心痛点**：`deception/` 这个名字当时指「L2+L3 执行平面（蜜罐 + 网络策略）」，而用户口头说的
「欺骗层」在代码里叫 `edge/` —— **口头说法与目录名互为错位**。

## 2. 设计逻辑

### 2.1 为什么只能改两个顶层名（不动 `core/` 与 `analysis/`）

实测破坏面（`grep` 统计 Go import）：

| 包前缀 | import 出现次数 | 文件数 | 改名代价 |
| --- | --- | --- | --- |
| `shen/core` | 81 | 46 | 全部在 `core/` 内部 → **不动就不改** |
| `shen/api` | 35 | 24 | **11 处来自 `deception/` 与 `console/`**，但不改名就不受影响 |
| `shen/edge` | **5** | 5 | 本次要改的就这些 ✓ |
| `shen/console` | 2 | 1 | 不动 |
| `shen/deception`（旧） | 0 | 0 | 旧包里没有跨包引用 → 移动无成本 ✓ |

⇒ 选**方案 A**（只改两名）而非方案 B（连 `core/` → `kernel/` 也改 + 拆 `analysis/`）：
B 要多付 ~120 处 import + Python 包整体重命名（venv / pytest / ruff / Makefile 全动），
换来的只是名字，不动骨架。

### 2.2 新顶层结构

```text
deception/          ① 欺骗层 —— 原 edge/（proxy · mirror · dns · injection）+ 原 deception/netpolicy/
  ├── proxy/        反向代理适配器（含 AI 欺骗内容消费侧，ADR-0023）
  ├── injection/    投毒改写 / 假路径 / 蜜饵注入（被适配器引用，不独立部署 ST-5）
  ├── mirror/       旁路镜像适配器（唯一不在请求路径上的）
  ├── dns/          DNS 引流适配器
  └── netpolicy/    L3 网络欺骗声明式产物
honeypot/           ② AI 蜜罐层 —— 原 deception/honeypot/ 变为 honeypot/protocol/
  ├── protocol/     蜜罐协议仿真入口（框架已建）
  └── shell/        假 shell（**未建**，MD-21 不预建）
console/            ③ 管控平台（未动）
core/               共享内核（**不改名**：判定与响应生成的唯一实现，不属任何单个大模块）
analysis/           L4 分析（**不改名**：跨 ①② —— 意图 / 攻击链 / 策略 + AI 能力服务）
```

### 2.3 一个必须写下来的边界：`deception/` 装了两层

`deception/` 同时装 ① 的 L1 适配器（`proxy` `mirror` `dns` `injection`）与 L3 网络欺骗（`netpolicy`）。
**理由**：两者都是「对外欺骗的执行面」（一个改响应、一个改网络视图），属于同一个大模块。
这与 [ADR-0028](../../docs/background/decisions/0028-three-module-view.md) §1.1 的平面映射**不冲突**：
映射表说明的是「哪个平面 ↔ 哪个目录」，`deception/` 一行覆盖 L1 与 L3 两格。

### 2.4 机械替换会留语义痕迹（本轮实测 6 处）

`sed 's|edge/|deception/|'` 只改路径字符串，**改不动含义**。本轮实际留下并被人工修掉的痕迹：

| # | 位置 | 痕迹 | 修法 |
| --- | --- | --- | --- |
| 1 | `structure.md` §1.1 目录树 | 两行**都叫** `deception/` | 拆成 `deception/`（①）+ `honeypot/`（②） |
| 2 | `structure.md` §1.1 映射句 | `` `edge` = 数据平面 L1 ``（无斜杠，`sed` 未命中） | 改写成「三模块 + 内核 + 跨层」 |
| 3 | `structure.md` §1.1 当前状态 | `core/ deception/ deception/ analysis/ console/` 重复 | 去重 + 标 `honeypot/shell/` 未建 |
| 4 | `_map.md` §1 目录表 | 同上重复，且第一行链接指向**已删除**的 `../../edge`（`tracecheck` 抓到） | 拆两行 + 改链接 |
| 5 | `docs/kb/quick-tour.md` | `edge-injection` 是**模块名**不是目录，`sed` 不该动它 ✓ | 已确认无需改 |
| 6 | `architecture.md` §7.2 图 | `L1 deception/ 模块` —— 语义恰好仍成立 ✓ | 已确认无需改 |

⇒ **教训**：这类批量改名必须**人工复核**每一条命中，门禁只能抓到第 4 类（悬空链接）。

## 3. 追溯矩阵

| 规则 | 文档 | 代码 | 测试 / 门禁 |
| --- | --- | --- | --- |
| `ST-1` 顶层目录白名单 | [`structure.md`](../design/structure.md) §1.1 · [`modules.md`](../design/modules.md) §1.1 | 顶层目录 = `core/ deception/ honeypot/ console/ analysis/` + 底座 | ✅ `make archcheck`（顶层目录白名单） |
| `MD-21` 阶段未到的目录不预建 | `structure.md` §1.5 | `honeypot/shell/` **未创建** | ✅ `make archcheck` 报「清单里有、代码尚未实现 1 个」（不算错） |
| `AR-2` / `AR-5` 判定与响应生成只实现一次 | `architecture.md` §7.1 | `core/` **一个文件未动** | ✅ `make archcheck`（跨平面依赖 + 核心内部可见性） |
| `MD-2` / `MD-17` / `MD-22` 模块清单 | `modules.md` §1.1 的「源码目录」列 | 24 个模块的目录全部指向新路径 | ✅ `make trace`（模块清单一致性） |
| `OH-1` / `OH-4` 泄漏检查 | [`check-leak/README.md`](../../scripts/check-leak/README.md) 豁免分类表 | `check-leak/main.go`：`skippedLiterals` 新增 **import 路径**一类 | ✅ `make leakcheck`（4 条既有豁免未动） |
| `D-3` / `D-8` 设计索引 | [`design/README.md`](../design/README.md) | 索引未点名目录，无需改 ✓ | ✅ `make trace` |
| 顶层目录决策 | [`ADR-0029`](../background/decisions/0029-three-module-dirs.md) · [`ADR-0007`](../background/decisions/0007-repo-layout.md) 标注被取代 | — | ✅ `make trace`（规则 ID 引用存在性） |

## 4. 代码改动

### 4.1 目录移动（`git mv`，历史保留）

```bash
git mv edge/injection deception/injection && git mv edge/mirror  deception/mirror
git mv edge/dns       deception/dns       && git mv edge/proxy   deception/proxy
git mv deception/honeypot honeypot/protocol && rmdir edge
```

### 4.2 引用与物料

| 类别 | 文件 | 内容 |
| --- | --- | --- |
| Go import | `deception/proxy/{handler,policy,content}.go` · `*/cmd/*/main.go`（5 处） | `shen/edge/` → `shen/deception/` |
| 门禁脚本 | `scripts/archcheck/main.go` | `case "deception", "honeypot", "analysis":` + 注释 |
| 门禁脚本 | `scripts/check-leak/main.go` | 前缀表加 `deception/` `honeypot/`；`skippedLiterals` 新增 import 路径 |
| 门禁豁免 | `scripts/check-leak/allow.txt` | 2 条例外（`SHEN_PROXY_MIRAGE` / `route_mirage`）的**文件路径**随改名同步为 `deception/proxy/...`（字面量与理由未变）；`scripts/tracecheck/allow.txt` 未动 |
| 构建 | `Makefile` · `deploy/docker/go.Dockerfile` · `deploy/docker/compose.yaml` | 路径与构建上下文 |
| 脚本 | `scripts/demo/run.sh` · `scripts/dev/ai-inject-check.py` | 路径常量 |
| 忽略 | `.gitignore` | 忽略项路径 |

### 4.3 顺带修掉的一处真实缺陷（非改名引入）

`scripts/dev/ai-inject-check.py` 的 HTTP 调用原用 `urllib.request.urlopen(<变量>)`，
静态审计长期报「未限制 scheme」；**与 HEAD 逐字相同**（预存在）。本轮改为 `http.client.HTTPConnection`
（scheme 在类型层面只能是 http），并**用本地 HTTP 服务器做行为对齐测试**：

```text
200：新=b'hello-parity' 旧=b'hello-parity' → 一致 ✓
404：新 抛 RuntimeError ✓ / 旧 抛 HTTPError ✓（调用方均不捕获 → 失败同样致命）
get_graphs（核心不在时软失败）：[] ✓
```


**已知的语义差（评审发现）**：`urllib.request.urlopen` 默认**跟随 3xx**，`http.client` **不跟随**。
本脚本访问的端点（`/api/graphs` 在 `console/cmd/console/main.go` 里精确注册）**不重定向**，故当前无影响；
但这是真差异，已在对齐测试里补了 302 用例（断言新实现停在 302、不跟到目标页）。

## 5. 场景表

| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 构建全部包 | `go build ./...` 通过 | ✓ |
| 静态检查 | `staticcheck` / `errcheck` / `ruff` / `shellcheck` 全绿 | ✓ |
| Go 单测（含 `-race`） | 新包路径全部通过 | `shen/deception/{injection,mirror,proxy}` · `shen/honeypot/protocol` ✓ |
| Python 单测 | 80 例通过 | ✓ |
| 架构检查 | 顶层目录 / 跨平面依赖 / 模块清单一致 | ✓「架构检查通过」 |
| 追溯检查 | 无悬空链接 | ✓（修掉 `_map.md` 的 `../../edge` 后） |
| 泄漏检查 | 无未登记泄漏字面量 | ✓「泄漏检查通过」 |
| 许可审计 | 依赖台账无变化（未增依赖） | ✓ |
| 端到端 | `make dev` 全 6 段 | ✓ 见 §6 |
| 门禁总门 | `make gate` | ✓「门禁通过」 |

## 6. 证据

`make gate` 关键行：

```text
✓ Shell clean · ✓ Python 格式（ruff format）· All checks passed!（ruff）
✓ 忽略清单未误伤任何已入库文件
架构检查通过。（顶层目录 · 跨平面依赖 · 核心内部可见性 · store 唯一 I/O 出口 · 模块清单一致性 · …）
ℹ 清单里有、代码尚未实现的模块目录 1 个（阶段 2/3，不算错）：honeypot/shell/（目录尚未创建）
泄漏检查通过。
ok  shen/deception/injection 1.400s   ok  shen/deception/mirror 1.572s
ok  shen/deception/proxy 19.616s      ok  shen/honeypot/protocol 1.423s
门禁通过。
```

`make dev` 关键行：

```text
核心已启动（影子模式（只算判定、不处置，INT-11））：127.0.0.1:19540
== 4/6 在线冒烟 ==  判定面 127.0.0.1:19540：3 个样本 → [通过] ×3
== 5/6 规则回放 ==  无头浏览器探源码 0.9(u a-headless,path-git) · 脚本批量扫路径 0.3 · 正常浏览器 0 · 伪造 UA 但探路径 0.5
== 6/6 L4 近线分析 == 取事件 3 条 → 结论 2 条（intent: reconnaissance 0.333；strategy: accepted=False）
✅ 通过：配置校验 · 非法配置被拒 · 启动 · 判定面 · 冒烟 · 规则回放 · L4 近线分析
```

**未经端到端验证的一处**（必须显式写出）：`scripts/dev/ai-inject-check.py` 的 HTTP 层改写
**只做了隔离的行为对齐测试**，**没有**跑 `make ai-check`（需 Docker 起全栈，本轮未跑）。
⇒ 该文件的风险面 = 5 个函数 + 9 个调用点，全部走同一的 `_http_get`；若后续跑 `make ai-check` 失败，
回退点就是本次提交前的版本。

## 7. 审视（L 档 · 对着本轮 diff 核文档与代码）

### 7.1 审视表

| 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- |
| `structure.md` §1.1 目录树两行同名 `deception/` | 机械替换痕迹 | `grep -n 'deception/' structure.md` 相邻两行相同 | 改写为 `deception/` + `honeypot/` | ✓ 已修 |
| `structure.md` §1.1 映射句仍写 `edge` | 机械替换漏网（无斜杠） | 该行含 `` `edge` `` 无 `/` | 改写为「三模块 + 内核 + 跨层」 | ✓ 已修 |
| `structure.md` §1.1 当前状态重复 `deception/` | 同上 | 该行 5 个目录出现 6 个名字 | 去重 + 标注 shell 未建 | ✓ 已修 |
| `_map.md` §1 两行同名 + 链接指向已删目录 | 悬空链接（门禁抓到） | `make trace` 报 `TC-3 "../../edge" 不存在` | 拆两行 + 改链接 | ✓ 已修 |
| `docs/background/decisions/README.md` 的 0028 行仍写「机器判据」 | 与已修正的 ADR 正文不一致 | ADR-0028 正文已改为「结构支撑」 | 同步用词 + 登记 0029 | ✓ 已修 |
| `check-leak` 把 **import 路径**当泄漏字面量（5 处误报） | 检查器精度（**非本轮引入的缺陷**，由新目录名触发） | `make leakcheck` 5 条，全部为 `import "shen/deception/…"` | `skippedLiterals` 新增 import 路径一类 + README 分类表同步 | ✓ 已修，OH-1 规则未改 |
| `ai-inject-check.py` 的 `urlopen` 静态审计（预存在） | 真实但低危：scheme 是硬编码 `http://127.0.0.1:` 字面量 | 与 HEAD 逐字相同（`diff` 证明） | 改为 `http.client` + 行为对齐测试 | ✓ 已修（端到端未验，见 §6） |
| `Makefile` 的 `SC1089/SC2276/SC2157`（预存在） | 工具把 **Makefile 当 shell** 读（文件头第 3 行本已 `disable=SC1089`） | 与 HEAD 逐字相同（`diff` 证明）+ `make help` 解析正常 | 把同族另两条并入那条既有豁免（纯注释） | ✓ 已登记 |
| 历史文档（`plans/` `background/` `log.md`）里的旧路径 | **刻意保留**的快照 | ADR-0029 决定 3 | 不追改 | ✓ 有意为之 |

**独立评审（冷上下文，对着本轮 diff）另抓到 9 条，全部已处理**：

| 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- |
| **改名树内 16 处旧路径未清**（含 4 条会失效的命令） | 机械替换范围不足：§7.2 的核查命令只覆盖 `docs/` | 评审指出后全仓扫：`deception/proxy/README.md` 三处 `go run ./deception/proxy/cmd/proxy`（原写 edge）· `deception/dns/README.md` 的 `cp` 命令 | 逐处改 | ✓ 已修（含 4 条命令） |
| `structure.md` §1.7 的 `ST-2` 枚举漏 `honeypot/`、重 `deception/` | 机械替换痕迹（在**已确认基线**的表格里） | §1.1 与 §1.5 都已有 `honeypot/` | 改为五目录枚举 | ✓ 已修 |
| `_map.md` §1 分层图 L2 写 `deception/`；模块 15 行链接标签与目标不符 | 同页自相矛盾 + 链接标签不实（`../../deception` 存在 → 门禁抓不到） | 评审指出 | 改 `honeypot/` · 目标改 `../../honeypot` | ✓ 已修 |
| 本变更包与 ADR-0029 说「`api/` 的 import 全在自身内部」 | **事实错误** | 评审实测：24 个文件里 11 个在 `core/` 之外 | 改述为「不改名就不受影响」 | ✓ 已修 |
| 两条豁免的文件路径随改名同步，却未列入「改了哪些文件」 | 变更包遗漏 | 评审指出；「4 条豁免未动」易被读成文件没动 | 补进 §4.2 文件清单 + 改措辞 | ✓ 已修 |
| `core/internal/contract/deception.go` · `core/internal/policy/server.go` 的注释仍写 `edge.proxy.*` | 改名树**之外**的旧路径 | 全仓 `\bedge\b` 扫描 | 改为 `deception.proxy.*` | ✓ 已修 |
| `ADR-0028` 决定 3 / 决定 6 被 ADR-0029 反转而未标注 | 决策记录口径不一致 | 评审指出 | 状态行标注「已被 0029 取代」 | ✓ 已修 |
| HTTP 改写未覆盖 3xx | 语义差（见 §4.3） | 评审指出 | §4.3 写明差异 + 对齐测试补 302 用例 | ✓ 已修 |
| `check-leak` 的 import 路径豁免类在 `OH-2` 位置表里没有落点 | 设计基线与检查器口径的差 | 评审指出 | 记为 ADR-0029 未解决 5（需用户确认才能改基线） | ✓ 已登记 |
| 仓库根有**已入库的 Go 二进制** `licensecheck`（2.9 MB） | 卫生问题，**非本轮引入** | `git log --diff-filter=A -- licensecheck` → `d76d236`（早于本轮） | 不在本轮修（超出用户要求），记为发现 | ⏸ 待办 |
| `analysis/aicap/tasks/content.py` 的注释因改名超长 4 字符 | 我自己的 `sed` 引入 | 门禁 E501（104 > 100） | 折行 | ✓ 已修 |

### 7.2 悬空 / 僵尸内容检查

- `deception/shell/`（旧 `_map.md` 曾提到）：**未创建**，且已从活文档的「均已建」表述中移出 ✓；
- `edge/` 这个目录：`grep -rn "edge/" docs/ --include=*.md` 仅剩**历史目录**（`plans/` `background/` `log.md`）与**模块名**
  `edge-injection`（模块名不在本次改名范围，`modules.md` §1.1 仍叫它）✓；
- 门禁豁免：`scripts/tracecheck/allow.txt` 的 `D-3` 与 `scripts/check-leak/allow.txt` 的 4 条**均未过期**（未新增、未删除）✓。

## 8. 未做与未验

**未做**（有意推迟，均有去处）：

1. `honeypot/shell/` —— 假 shell 内容按用户裁定推迟（`MD-21` 不预建），`archcheck` 已把它列为「尚未实现」；
2. `analysis/` 的拆分（是否分成 ①/② 两半）—— ADR-0029 未解决 1，需单独 ADR；
3. 蜜罐**编排**的实现面（真起容器/进程）—— 能力归属已在 `core/internal/honeypot`（ADR-0011），实现面另开 ADR；
4. 历史文档里的旧路径不加「已迁移」横幅 —— ADR-0029 决定 3（刻意保留快照）；
5. 控制台展示生成批次 / 护栏拒绝明细 —— 与本轮无关的既有待办。

**未验**：

1. `make ai-check`（端到端 17 项）—— 需 Docker 起全栈，本轮未跑；受影响的只有 `ai-inject-check.py` 的 HTTP 层（已做隔离对齐测试，见 §6）；
2. 控制台在浏览器里的实际观感 —— 本轮未动 `console/`（除 2 处 import 路径），未跑浏览器；
3. `docker compose` 构建 —— 未跑 `make up`（改了 `go.Dockerfile` 与 `compose.yaml` 的路径，由 `make gate` 的静态检查覆盖，未做真实构建）；
4. **`make demo`**（`scripts/demo/run.sh` 的路径常量本轮改了，门禁**不跑**它）—— 未跑；
5. **`make leakcheck` 的完整回归** —— 跑过且绿，但 import 路径豁免类是**新加的分支**，只由本次实跑覆盖，无单测（`scripts/check-leak` 下没有测试文件，预存在）；
6. 仓库根那个**已入库的 `licensecheck` 二进制**（非本轮引入）—— 未处置，见 §7.1。
