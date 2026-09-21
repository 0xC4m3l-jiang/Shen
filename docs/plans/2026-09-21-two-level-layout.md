# 2026-09-21 · 顶层收成两级：`modules/`（三个大模块）+ `common/`（公用代码）

> 依据：用户指令「将我说的三个核心模块的根目录，放到根目录下的一个目录下……公用代码模块也要有相关的目录区分，
> 从而提高项目的结构理解度，并且优化 docs 中的文档内容。先帮我调整整体项目结构」·
> [ADR-0030](../../docs/background/decisions/0030-two-level-layout.md)（新建）·
> [ADR-0029](../../docs/background/decisions/0029-three-module-dirs.md)（决定 1 被本文取代）·
> [ADR-0028](../../docs/background/decisions/0028-three-module-view.md) 决定 1（核心不拆散）
> 变更分级：**L**（顶层目录 + 已确认基线 `structure.md` §1.1 + 两个门禁脚本 + 124 处 import + 约 184 行文档）

## 1. 需求

两条，都来自用户：

1. **三个大模块的根目录收进一个目录** —— 「我知道后续开发从哪个子目录进去，就知道我要做哪个功能模块」；
2. **公用代码也要有目录区分** —— 「提高项目的结构理解度」，即「这是被共用的」不能只写在文档里。

外加一条：**优化 `docs/` 内容**，保证结构标准化。

## 2. 设计逻辑

### 2.1 方案与取舍

见 [ADR-0030](../../docs/background/decisions/0030-two-level-layout.md) 的候选表（A / B / C / D）。
采纳 **A**：

```text
modules/            产品功能模块（**开发从这里进去**）
├── deception/      ① 欺骗层（L1 适配器与处置 + L3 网络欺骗）
├── honeypot/       ② AI 蜜罐层（protocol/ 已建 · shell/ 未建）
└── console/        ③ 管控平台（只读观测台）
common/             公用代码（被 modules/ 共用，**不是**功能模块）
├── core/           共享内核 —— 判定与响应生成的唯一实现
└── api/            跨进程契约 —— .proto + 生成的 Go 桩（叶子）
analysis/           L4 分析层（Python）—— 跨 ①②，故既不在 modules/ 也不在 common/
deploy/ docs/ scripts/ vendor/ .pi/
```

**否掉 B（一个 `src/` 装五个平面）的关键理由**：`src/` **不区分「模块 / 公用」**，
用户提的第 2 个问题（公用代码要有区分）它没解决，却要多付 116 处 import。

**否掉 C（`apps/`）**：`analysis/` 同样是可部署的服务却不在里面 ⇒ 名不副实。

### 2.2 `analysis/` 留顶层的理由（写进 ADR，避免将来反复争）

它**不是**公用库（有自己一条流水线：意图 → 攻击链 → 策略），**也不是**三大模块之一（用户的三模块不含它），
而且是**另一种语言与运行时**（Python + venv）。放进 `common/` 会同时错两件事：把一条流水线叫「公用代码」，
并暗示它和 `core/` 一样是被人 import 的库。

### 2.3 容器名为什么是 `modules/` 与 `common/`

项目名未定（[ADR-0004](../../docs/background/decisions/0004-terminology.md) 卡住建仓库 / 二进制名 / 服务名）
⇒ 不能用产品名做容器名。两个名字都直译用户的说法，且不带技术语义。
**刻意避开 `pkg/`**：Go 里它专指「供外部引用的库」，与这三个（与 `core/`）恰好相反。

## 3. 追溯矩阵

| 规则 | 文档 | 代码 / 物料 | 测试 / 门禁 |
| --- | --- | --- | --- |
| `ST-1` 顶层目录白名单 | [`structure.md`](../design/structure.md) §1.1（两级树 + 容器映射表） | 顶层 = `modules/` `common/` `analysis/` + 底座 | ✅ `make archcheck`（从 §1.1 解析白名单） |
| `ST-2` 一个进程只在一个顶层目录内 | `structure.md` §1.7 | `scripts/archcheck/main.go`：`planeOf` 改为**感知容器**的两级解析 | ✅ `make archcheck`（跨平面依赖；实测 0 违规） |
| `ST-3` 适配器禁止 import 核心内部 | `structure.md` §1.7 | 内核移到 `common/core/internal/`，路径守卫同步 | ✅ `make archcheck`（`forbidden = shen/common/core/internal`） |
| 阶段交付范围（[ADR-0007](../../docs/background/decisions/0007-repo-layout.md) 的阶段划分） | `structure.md` §1.5 | `modules/honeypot/shell/` **未创建** | ✅ `make archcheck` 报「尚未实现 1 个」（不算错） |
| `MD-2` / `MD-17` / `MD-22` 模块清单 | `modules.md` §1.1 的「源码目录」列（24 行全部换新路径） | 模块目录 ↔ 清单一一对应 | ✅ `make trace`（清单一致性） |
| `AR-2` / `AR-5` 判定与响应生成只实现一次 | `architecture.md` · `modules/README.md` | `common/core` **一个逻辑文件都没改**（只改 import 前缀） | ✅ `make archcheck` + 单测（含 `-race`） |
| `OH-1` / `OH-4` 泄漏检查 | [`check-leak/README.md`](../../scripts/check-leak/README.md) | `check-leak/main.go` 的前缀表改为容器感知（`modules/` `common/` `analysis/`） | ✅ `make leakcheck` |
| 决策记录 | [ADR-0030](../../docs/background/decisions/0030-two-level-layout.md) 新建 · [ADR-0029](../../docs/background/decisions/0029-three-module-dirs.md) 标注被取代 · [ADR-0028](../../docs/background/decisions/0028-three-module-view.md) 已标注 | — | ✅ `make trace`（规则 ID 引用存在性） |

## 4. 代码与物料改动

### 4.1 目录移动（`git mv`，历史保留）

```bash
mkdir -p modules common
git mv deception modules/deception && git mv honeypot modules/honeypot && git mv console modules/console
git mv core common/core         && git mv api common/api
```

### 4.2 Go import（124 处，全部机械替换）

| 旧前缀 | 新前缀 | 处数 |
| --- | --- | --- |
| `shen/core/` | `shen/common/core/` | 81 |
| `shen/api/` | `shen/common/api/` | 35 |
| `shen/deception/` | `shen/modules/deception/` | 6 |
| `shen/console/` | `shen/modules/console/` | 2 |
| `shen/honeypot/` | `shen/modules/honeypot/` | 0（叶子包，无人 import） |

### 4.3 门禁与物料

| 类别 | 文件 | 内容 |
| --- | --- | --- |
| 门禁 | `scripts/archcheck/main.go` | `planeOf` 改为两级（`modules/` `common/` 是容器）；新增 `planeRoot` 映射表；`structuralDirs` · `forbidden` · `coreInternal` 三处路径前缀；模块目录映射改用 `planeRoot` |
| 门禁 | `scripts/check-leak/main.go` | 清单前缀表 → `modules/` / `common/` / `analysis/`；注释里的 import 示例同步 |
| 构建 | `Makefile` | `PROTOS := find common/api` · `protoc -I common/api` · 4 处 `go run/test ./common/core/...` · 3 处目录参数 · 2 处 bench/grep 目录 |
| 构建 | `deploy/docker/go.Dockerfile` | 4 条 `COPY`（`common/api` `common/core` `modules/deception` `modules/console`）+ 用法注释 |
| 构建 | `deploy/docker/compose.yaml` | 3 个 `SERVICE` 路径 |
| 脚本 | `scripts/demo/run.sh` · `scripts/dev/smoke.sh` · `scripts/dev/ai-inject-check.py` | 构建与运行路径（7 行） |
| 契约 | `analysis/tools/genproto.py` | `PROTO_ROOT` 与 `PROTO_FILES` 指向 `common/api` |
| 忽略 | `.gitignore` | 4 条产物兜底规则 + 2 处注释里的示例路径 |
| 契约 | `common/api/*/v1/*.proto` | `option go_package`：`shen/api/...` → `shen/common/api/...`（不改则 `make generate` 会把生成物写到已不存在的目录） |
| 契约（生成物） | `common/api/*/v1/*.pb.go` | 按新 `.proto` 重新生成：每文件 **2 行**内容差异（内嵌的 go_package 字符串），无其他变化 |
| 门禁豁免 | `scripts/check-leak/allow.txt` | 4 条例外的**文件路径**随搬迁同步（`common/core/internal/...` · `modules/deception/proxy/...`）；字面量与理由未变 |
| Python 夹具 | `analysis/tests/test_event_contract.py` · `analysis/events.py` · `analysis/telemetry.py` | 夹具路径与注释里的契约路径 → `common/api/...` |

### 4.5 生成物与工具链漂移（本轮的处理与取舍）

- **Go 桩**：`.proto` 的 `go_package` **必须**跟着目录改（否则 `make generate` 写出到已不存在的路径）
  ⇒ 改了 3 个 `.proto` 并重新生成；实测每个 `.pb.go` 只有 **2 行**内容差异（内嵌字符串）✓，生成物与本地工具链重新对齐；
- **Python 桩**：`make pygen` 在本机会产生 **287 行**模板抖动（本地 grpcio-tools 的模板用 `grpc.experimental`、引号风格不同），
  而 `analysis/pyproject.toml` 明确把 `proto` 排除在风格检查外。**本轮把 Python 桩回退到 HEAD**：
  `go_package` 对 Python 是惰性元数据（不影响运行期），287 行抖动与本次搬迁无关。
  ⇒ **已知的既存问题**：本机 `make pygen` 不是零 diff；修它需要一次独立的「生成物与工具链对齐」轮次。
| 测试夹具 | `common/core/internal/control/observer_contract_test.go` · `modules/deception/proxy/wire_test.go` · `common/core/internal/policy/policy_test.go` | 相对路径多了一层 |

### 4.4 文档（约 184 行路径 + 链接）

| 类别 | 文件 | 内容 |
| --- | --- | --- |
| 技术基线 | `docs/design/structure.md` | §1.1 两级树 + 容器映射表；§1.2 `common/core/` 内部；§1.3 `modules/` 内部（含新增的 honeypot / console 树）；§1.5 已建表；§1.6 包级地图；§1.7 的 `ST-1`/`ST-2` 枚举 |
| 模块清单 | `docs/design/modules.md` | §1.1 的「源码目录」列 24 行 |
| 索引 | `docs/modules/_map.md` | §1 换成**容器视角**的分层表；§1.1 标题与措辞；§1.2 的「只跑某一层」命令 |
| 其他 | `docs/design/architecture.md` · `docs/spec/*` · `docs/ops/*` · `docs/integrate/*` · `docs/kb/*` · `docs/progress.md` · `docs/README.md` · `README.md` | 路径与链接 |
| **新增** | `docs/background/decisions/0030-two-level-layout.md` | 本文依据的 ADR |
| **新增** | `modules/README.md` | **总入口**：我要做 ①/②/③ 的哪件事 → 进哪个子目录 → 看哪份文档 → 跑什么测试 |
| **新增** | `common/README.md` | 公用代码说明 + 「为什么核心不拆散」+ 契约的规矩 |
| **新增** | `modules/deception/README.md` · `modules/honeypot/README.md` · `modules/console/README.md` | 每个大模块的入口页：有什么 / 怎么跑 / 硬约束 / 边界 |

**被移动树内部的相对链接**（下移一层后要补一个 `../`）：实测 **12 条**（`deception/{dns,mirror,netpolicy,proxy}` 的 README）
+ 本轮新增 README 的 **27 条**深度修正 —— 全部由「补/减一层后确实存在」的确定性脚本修，不靠眼力。

## 5. 场景表

| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 构建全部包 | `go build ./...` | ✓（搬完即通过） |
| 静态检查 | staticcheck / errcheck / ruff / shellcheck / ruff format | ✓ |
| Go 单测（含 `-race`） | 新路径全部通过 | `ok shen/common/core/cmd/core` · `shen/modules/{deception,console,honeypot}` 等 ✓ |
| Python 单测 | 80 例 | ✓ |
| 架构检查 | 顶层白名单 · 跨平面 · 模块清单一致 | ✓「架构检查通过」 |
| 追溯检查 | 无悬空链接 · 最新日志条目路径存在 | ✓（换新日志条目后） |
| 泄漏检查 | 前缀表改了仍逐条生效 | ✓「泄漏检查通过」 |
| 许可审计 | 无新增依赖 | ✓ |
| 端到端 | `make dev` 全 6 段 | ✓ 见 §6 |
| 门禁总门 | `make gate` | ✓ |
| 隐藏目录树遍历 | 新容器不被 archcheck 的遍历规则排除 | ✓（`checkLanguages` 只跳过隐藏目录 / vendor / node_modules） |

## 6. 证据

`make dev` 关键行（注意包路径已经是新布局）：

```text
ok  shen/common/core/cmd/core 0.475s
== 5/6 规则回放 ==  无头浏览器探源码 0.9(ua-headless,path-git) · 脚本批量扫路径 0.3 · 正常浏览器 0 · 伪造 UA 但探路径 0.5
== 6/6 L4 近线分析 == 取事件 3 条 → 结论 2 条（intent: reconnaissance 0.333；strategy: accepted=False）
✅ 通过：配置校验 · 非法配置被拒 · 启动 · 判定面 · 冒烟 · 规则回放 · L4 近线分析
```

`make gate` 关键行：

```text
架构检查通过。（顶层目录 · 跨平面依赖 · 核心内部可见性 · store 唯一 I/O 出口 · 模块清单一致性 · …）
ℹ 清单里有、代码尚未实现的模块目录 1 个（阶段 2/3，不算错）：modules/honeypot/shell/（目录尚未创建）
泄漏检查通过。
门禁通过。
```

## 7. 审视（L 档 · 对着本轮 diff 核文档与代码）

### 7.1 审视表

| 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- |
| **`sed` 把 `common/core/internal/` 里的 `honeypot` 树行改成了 `modules/honeypot/`** | 机械替换只改路径、不改语义（**本轮真实事故**） | `structure.md` §1.2 的树里出现 `├── modules/honeypot/` | 改回 `honeypot/` | ✓ 已修 |
| `_map.md` 的链接目标 `(../../core)` 等指向已移动的目录 | 悬空链接（`make trace` 抓到 30+ 条） | `TC-3 悬空链接：目标 "../../deception/dns/..." 不存在` | 用「给 `../` 后再插容器名」的规则批量修 | ✓ 已修 |
| 被移动树内部的相对链接少一层 | 位置下移一层的必然结果 | `modules/deception/proxy/README.md` 的 11 条 `../../docs/...` | 确定性脚本：仅在「补一层后确实存在」时改 | ✓ 已修 12 条 |
| 本轮新增 README 的相对链接深度算错 | 我自己的错误（`modules/README.md` 只该用 `../docs/`） | `make trace` 报 20+ 条新悬空链接 | 同一个脚本支持「减一层」后修 | ✓ 已修 27 条 |
| `structure.md` §1.3 标题从 `deception/` 内部变成 `modules/` 内部后，另两个模块的树缺失 | 文档不完整 | 读者在 §1.3 只看得到 `deception/` | 补 `modules/honeypot/` 与 `modules/console/` 两棵树 + 入口指针 | ✓ 已修 |
| `common/core/cmd/core/main.go` 的 `nosemgrep` 注释离被抑制行 3 行 | 抑制不生效（**预存在**，非本轮引入） | `git show HEAD:core/cmd/core/main.go` 同位置 | 把注释挪到紧邻行（纯注释，零行为变化） | ✓ 已修 |
| `analysis/aicap/tasks/content.py` 的注释因路径变长超行宽 | 我自己的 sed 引入 | 门禁 E501（104 > 100） | 折行 | ✓ 已修 |
| `docs/log.md` 里历史条目的旧路径 | **刻意保留**的快照 | [ADR-0030](../../docs/background/decisions/0030-two-level-layout.md) 决定 4 | 不追改 | ✓ 有意为之 |
| 仓库根的 `licensecheck` 二进制（2.9 MB，`d76d236` 入库） | 卫生问题，**非本轮引入** | 上一轮已核实 | 不在本轮修（超出用户要求） | ⏸ 待办 |
| `check-leak` 的「import 路径」豁免类在 `OH-2` 位置表里没有落点 | 基线与检查器口径的差（上一轮遗留） | 上一轮独立评审指出 | 记为 [ADR-0029](../../docs/background/decisions/0029-three-module-dirs.md) 未解决 5，待用户确认 | ⏸ 待办 |
| **`allow.txt` 的 4 条豁免路径随搬迁失配** | 门禁的「豁免会腐烂」规则（`OH-4`）**真的抓到了** | `make leakcheck` 报「豁免已过期：该字面量已不存在」 | 逐条同步文件路径（字面量与理由未变） | ✓ 已修 |
| `.proto` 的 `go_package` 仍指旧路径 | 不改则 `make generate` 把生成物写到已不存在的目录 | `grep go_package` 三条都是 `shen/api/...` | 改为 `shen/common/api/...` + 重新生成 Go 桩（每文件 2 行） | ✓ 已修 |
| `analysis/tests/test_event_contract.py` 的夹具路径 | 搬迁后指向不存在的文件 | pytest 4 例 `FileNotFoundError` | 路径改 `common/api/...`；该行被 ruff format 重排 | ✓ 已修 |
| `common/core/internal/policy/policy_test.go` 的配置夹具路径 | 同上（少一层 `../`） | `TestExampleConfigLoads` FAIL | 加一层 `../` | ✓ 已修 |
| Python 桩被重生成冲掉了格式化（287 行抖动） | 工具链模板漂移，**与本次搬迁无关** | `git diff` 全是引号风格与 `grpc.experimental` | 回退到 HEAD + 记为既存问题（见 §4.5） | ✓ 已回退 |
| `Makefile` 的 `PROTOS := find api` 与 `protoc -I api` | 不改则 `make generate` 扫不到任何 `.proto` | 该行在 `sed` 之外（变量定义在文件头） | 改为 `find common/api` / `-I common/api` | ✓ 已修 |
| **容器子目录脱出了 `ST-1` 的覆盖**（`modules/foo/` 不会被拦） | **既有约束被收缩**（本项目引入） | 评审指出：`checkTopLevel` 只看仓库根一层 | `scripts/archcheck/main.go` 新增 `parseContainerChildren` + `checkContainerChildren`（从 §1.1 解析容器下的平面并逐一核对）；**用探针验证**：造 `modules/probe-tmp/` 与 `common/probe-tmp/` → 各被拦下，移走 → 恢复通过 | ✓ 已修 + 已验 |
| `planeOf` 丢弃容器名 ⇒ `modules/core/` 会与 `common/core` 混成同一平面 | 静默失效面 | 评审指出（`plane, _, _ := strings.Cut(...)`） | 上一条的白名单同时堵住它（`core` 不是 `modules/` 的合法子目录）；并写进函数注释 | ✓ 已防 |
| `modulePath` 硬编码 `shen`：项目名一变，ST-2/ST-3/MD-20 会**静默全过** | 静默失效面（既存） | 评审指出（`planeOf` 返回 `""` 即 `continue`） | `main()` 新增自检：一个平面都解析不出时 `fatal` 退出（拒绝静默放行） | ✓ 已修 |
| **活文档的代码块里仍有失效命令**（`go run ./deception/proxy/...` · `go test ./console/...` · `go list ./core/...`） | 我上一道 `sed` 的盲区：`(\?<![\/\w])` 前瞻把 `./core/` 排除了 | 评审指出 ≥10 个文件（`README.md` · `docs/integrate/business-onboarding.md` · `docs/kb/capabilities.md` · `docs/spec/*` · `structure.md` 的三条复核命令 · `modules/deception/proxy/README.md`） | 补一道针对 `./X/` 形式的改写，并扫到 `scripts/*/README.md` · `deploy/config/*.yaml` · `analysis/requirements.txt` · `.proto` 注释 | ✓ 已修（并重生成 Go 桩） |
| **§1.1 的容器子目录行被并行 `sed` 又改回 `modules/deception/` 形式** | 编辑与 bash 同时跑同一文件造成的竞态；后果是「白名单解析为空 → 全部子目录误报」 | `-dump` 输出：`容器 modules/ 下的平面：modules honeypot modules` | 重修行 + 让解析器取**最后一段**（两种写法都认） | ✓ 已修 |
| `structure.md` §1.1 的**历史行**被 sed 写成新路径 | **事实错误**（历史应该是旧路径） | 「顶层曾直接是五个平面（`common/core/` `modules/deception/` …）」 | 改回 `core/` `deception/` `honeypot/` `console/` `analysis/` | ✓ 已修 |
| `MD-21` 被当成「目录不预建」引用（**不支持该语义**） | 引证与原文不符 | `MD-21` 原文：阶段 1 交付只含标记 1 的模块；与「目录建不建」无关 | 4 处改引 [ADR-0007](../../docs/background/decisions/0007-repo-layout.md) 的阶段划分（`structure.md` · `modules/honeypot/README.md` · 本包 2 处） | ✓ 已修 |
| `modules/console/README.md` 说 `internal/` 含「核心读面客户端 · HTTP 面与 SSE 流」 | 写了但不存在（且与本行上方自相矛盾） | 实际 `internal/` 只有 `topology/`；其余在 `cmd/console/main.go` | 改述 + 指向真实文件 | ✓ 已修 |
| `structure.md` §1.6.2 的「`common/api/policy/v1` 当前无人引用」 | 过期事实（与 §1.6.4 的「✅ 已接 Pull + Ack」矛盾） | `docs/modules/policy.md` 写「下发与回执（`Pull` / `Ack`）」（2026-09-19） | 改述为「只差 `Watch`」并去掉「到不了边缘」的过期因果 | ✓ 已修 |
| `structure.md` §1.6.4 末尾两句「仍未接通…诱饵资产」几乎逐字重复 | 重复内容（既存） | 两句同义 | 合并为一句 | ✓ 已修 |

### 7.2 悬空 / 僵尸内容检查

- `docs/design/structure.md` §1.1 的树是 archcheck 解析白名单的**唯一来源** —— 两级树里的嵌套行（缩进）不会被
  `^[├└]── ` 正则命中，故白名单只认 `modules/` `common/` `analysis/` + 底座 ✓（实测「架构检查通过」）；
- `staleMarkers` / 链接检查在 `docs/plans` `docs/background` `docs/kb` 跳过（历史与沉淀），活文档已全部同步 ✓；
- 门禁豁免未新增、未过期：`scripts/tracecheck/allow.txt` 仍是 1 条（`D-3`），`scripts/check-leak/allow.txt` 仍是 4 条 ✓。

## 8. 未做与未验

**未做**（有意推迟，均有去处）：

1. `modules/honeypot/shell/`（假 shell）—— 按用户裁定推迟（阶段未到，不预建），archcheck 已把它列为「尚未实现」；
2. `analysis/` 是否拆成 ①/② 两半 —— [ADR-0030](../../docs/background/decisions/0030-two-level-layout.md) 未解决 1，需单独 ADR；
3. 蜜罐**编排**的实现面（真起容器 / 进程）—— 归属已在 `common/core/internal/honeypot`，实现面另开 ADR；
4. `analysis/` 收进 `common/` 的可能性 —— 已否，理由写进 ADR 决定 2；若情况变化走 ADR 失效条件 1；
5. 历史文档里的旧路径不加「已迁移」横幅 —— ADR-0030 决定 4（刻意保留快照）；
6. 仓库根的 `licensecheck` 二进制 —— 未处置（见 §7.1）。

**未验**：

1. **`make up` 的真实 Docker 构建** —— 改了 `go.Dockerfile` 的 4 条 `COPY`、`SERVICE` 与 `compose.yaml` 的 3 个路径，
   但这些**只被静态检查覆盖**，本轮**没跑** Docker（`make docker-build` / `make up` 均未执行）。
   ⚠️ 这是本轮最值得先验的一项：`COPY` 漏一个路径，构建期才会炸；
2. `make ai-check`（端到端 17 项，需 Docker）—— 未跑；受影响的只有 `scripts/dev/ai-inject-check.py` 的构建路径常量（3 行）；
3. `make demo` —— 未跑（`scripts/demo/run.sh` 的 3 条构建路径已改）；
4. `make traffic` / `make doctor` / `make smoke` —— 未跑（`scripts/shen.sh` 只涉及容器服务名，未改）；
5. `analysis/tools/genproto.py` 的 `make pygen` —— 未跑（改了 `PROTO_ROOT` / `PROTO_FILES`；Python 单测不覆盖它）；
6. 控制台页面在浏览器里的观感 —— 未动 `modules/console/web/`；
7. **`make pygen` 的零 diff 不变量** —— 本机生成器模板与入库版本不一致（既存漂移，见 §4.5）；本轮已回退 Python 桩，**未修漂移本身**。
   ⚠️ 后果：仓库里那份 `analysis/proto/telemetry/v1/telemetry_pb2.py` 仍内嵌**旧**的 `go_package` 字符串（`shen/api/…`），
   与新 `.proto` 不一致 —— 对 Python **无影响**（惰性元数据），但下次 `make pygen` 会同时带出模板漂移与这个字符串变化；
8. **`make bench` 与 `make caddy-surface`** —— `Makefile` 里这两处的目录参数本轮被改（`modules/deception/` `common/core/`），
   既未跑也未列入前面的清单；
9. **文档里的命令块没有机械检查**（`tracecheck` 只查 markdown 链接）—— 本轮靠评审才发现 `./core/…` 这类失效命令；
   该类问题容易复发，目前**只能靠人工清单**（见 §7.1 对应行）。
