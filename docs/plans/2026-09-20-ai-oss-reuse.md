# 变更包：AI 能力层的开源复用审查 + Python 依赖许可审计面

| 项 | 值 |
| --- | --- |
| 主题 | 把 `analysis/llm` + `analysis/aicap` 的手写能力逐项对到开源实现上，给出「复用 / 不复用」的判据；并把 `TB-16` 缺失的 Python 审计面补上 |
| 日期 | 2026-09-20 |
| 状态 | 已实现 |
| 涉及模块 | `llm-components`（[`../design/modules.md`](../design/modules.md) §1.1 第 21 行，阶段 3）· `ai-capability`（第 25 行，阶段 2b/3）· 工具 `scripts/licensecheck/`（`TB-16` 的实现） |
| 决策数 | 已答 4 项 / 待定 4 项（全部不阻塞本轮） |
| 关联 | [ADR-0024](../background/decisions/0024-ai-oss-reuse-boundary.md)（新）· [ADR-0023](../background/decisions/0023-deception-content-injection.md)（未解决 1/2 的前置条件）· 调研材料 [`../background/research/ai-oss-reuse.md`](../background/research/ai-oss-reuse.md)（新）· 上轮变更包 [`2026-09-20-ai-capability-guardrail.md`](2026-09-20-ai-capability-guardrail.md) |

---

## 1. 需求与验收

**要解决什么**：阶段 A 的 AI 能力代码（18 个模块）除 `PyYAML` 外零第三方依赖全部自研，
但**没人核对过**哪些能力本可以直接复用开源实现；同时 `TB-16`（依赖必须经许可审计）
在 Python 侧**没有实现**，导致 ADR-0023 未解决 1/2（模型后端、PII 检测）卡在「先过许可台账」上。

**做完之后能做什么**：

1. 阶段 B 立项时不必从零查证候选 —— 逐项判定表已给出许可（逐字核过）、活跃度、规则冲突点；
2. 「该不该引某个库」有一条**可判的判据**（默认行为是否 fail-closed），不再靠偏好；
3. `make licensecheck` 同时审 Go 与 Python 运行期依赖，台账里两类都在；
4. 知道哪些能力在开源侧**没有**对等物（自研是正确的），避免下一轮重复检索。

**验收判据**：

1. `docs/background/research/ai-oss-reuse.md` 存在，含「结论先行 / 检索记录 / 逐项对比表 / 空白矩阵 / 待核清单 / 局限」六段，且每条结论带证据级；
2. 15 个候选的许可**全部来自仓库 `LICENSE` 正文**（不是二手文章），归档项被显式标出并禁止引入；
3. `make licensecheck` 的输出里同时出现 Go 模块数与 Python 运行期依赖数，且 Python 项逐条带许可与判定；
4. **构造性反证**：篡改一条锁文件版本（或删掉一个已装发行版）后，`make licensecheck` **必须失败**并指向 `make pyenv`；
5. `make gate` 通过（含 `make trace`），且 `docs/spec/dependencies.md` 由 `make license-ledger` 重新生成后与门禁输出一致。

**不做什么**：

- **不改** `analysis/llm/` 与 `analysis/aicap/` 的任何一行代码 —— 复用是**阶段 B** 的动作（ADR-0024 决定 3）；
- **不装**任何候选库，因此**不声称**「换库更快 / 更准」（那需要对照组基准，是另一轮）；
- 不审 `intent/` `chain/` `strategy/` `worker/`（AI 能力的消费者）与跨语言侧（`core/internal/policy/ai.go` · `edge/injection/`）；
- 不把开发期依赖（`ruff` / `pytest` / `grpcio-tools`）纳入台账（与 Go 侧「只审参与构建的模块」同口径）。

---

## 2. 设计逻辑

**决策树**：

```text
AI 代码要不要复用开源实现
├── 第 1 轮（范围与交付）
│   ├── Q1 审哪些代码        → 只审 llm/ + aicap/
│   ├── Q2 交到哪一步        → 调研 + 判定表 + 文档，代码不动
│   ├── Q3 依赖门槛          → 宽松许可 + 离线无外呼；本轮补 Python 审计面
│   └── Q4 动哪些文档        → 材料 + 登记 + 两个模块文档 + 修重复段 + 记缺口
└── 第 2 轮（判据）
    └── 复用判据 = 默认行为是否 fail-closed（不看功能表）  ← 决定 1
        └── 前提：Python 依赖必须可审计（TB-16 的实现缺口）← 决定 2
```

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 审查范围 | 只审 `analysis/llm` + `analysis/aicap` | 这两块是 AI 能力本体；消费者与跨语言侧混进来会让判定表失焦 | 调研材料 §0 范围边界 |
| ② | 交付深度 | 调研 + 判定表 + 文档；**AI 代码不动** | `AR-15`/`AR-21` 约束的是默认行为，多数框架默认行为相反；引依赖还要过 ADR 与台账 | 本文件 §4「不改」+ ADR-0024 决定 3 |
| ③ | 依赖门槛 | 宽松许可（MIT/Apache-2.0/BSD/ISC）+ **可离线、无外呼**；弱传染逐案审 | 与 Go 侧既有白名单式判定同口径（`scripts/licensecheck/README.md`） | ADR-0024 决定 1 |
| ④ | 审计面缺口 | **本轮补上** Python 运行期依赖的许可审计 | 不补则 `TB-16` 只成立一半，「依赖都审过」这句是错的 | ADR-0024 决定 2 + 本文件 §4 |
| ⑤ | Python 审计的数据源 | 依赖集合取**锁文件**（`analysis/requirements.txt`），许可声明取**已安装发行版元数据**（`dist-info/METADATA`，离线） | 锁文件是声明、环境是事实；两者不一致时**必须失败** —— 宁可门禁红，不可拿 A 版本的元数据审定 B 版本的许可 | ADR-0024 决定 2 |
| ⑥ | 认不出许可怎么办 | 「需人工判定」+ 门禁失败（白名单式，不改成黑名单式） | 许可识别错的代价是法律风险，比构建失败严重 —— 沿用 Go 侧既有口径 | `scripts/licensecheck/main.go` 既有设计 2 |

**仍未定**（不阻塞本轮）：

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | 模型后端与结构化输出的**具体**选型（Outlines / Instructor / 自研） | 阶段 B 生成质量 | ADR-0024 未解决 1 |
| 2 | PII 检测用 Presidio 还是 detect-secrets | `AR-22` 泄露类覆盖度 | ADR-0024 未解决 2 |
| 3 | `Faker` vs `Mimesis` | 生成吞吐 | ADR-0024 未解决 3（需要基准才下结论） |
| 4 | `json_repair(strict=True)` 是否真能覆盖 markdown 围栏与非 JSON 前置文本 | 该条复用能否落地 | 调研材料 §5 待核 T-1 |

**接缝与接口**：

- 新增接口只有一处，且**不在产品代码里**：`scripts/licensecheck` 的审计入口不变（`make licensecheck` / `make license-ledger`），
  内部多一类依赖来源（Python 运行期）。依赖方向：工具 → 锁文件 + venv（只读）。
- **AI 能力层零接口变更**：`generate(TaskSpec) → Envelope`（`AR-33`）不动，`AnalysisClient` 契约（`AR-32`）不动。

**数据流**（含失败路径）：

```text
make licensecheck
├── Go 侧（既有）：go list -deps → vendor/ 或模块缓存 → LICENSE 文本 → sig 表判定
└── Python 侧（新）：analysis/requirements.txt（集合 + 版本）
                     ↓ 比对
                    analysis/.venv 的 *.dist-info/METADATA（Name / Version / License-Expression / Classifier / License）
                     ↓
                    SPDX 表（表达式按 OR/AND 取最严格）或 自由文本别名表
                     ↓
        ┌── 对得上 + 许可宽松 ──────► ✓ 允许
        ├── 对得上 + 许可限制性 ────► ✗ 禁止
        ├── 对得上 + 认不出 ────────► ✗ 需人工判定
        ├── 环境缺该发行版 ─────────► ✗ 环境缺 X（跑 make pyenv）
        └── 版本与锁文件不一致 ─────► ✗ 锁文件 X vs 环境 Y（跑 make pyenv）
```

---

## 3. 文档对应（追溯矩阵）

| 规则 ID | 模块文档 / 契约 | 代码 | 测试 / 证据 | 验证命令 |
| --- | --- | --- | --- | --- |
| `TB-16` | [`docs/design/language.md`](../design/language.md) §3（依赖必须经许可审计）· [`docs/spec/dependencies.md`](../spec/dependencies.md) | `scripts/licensecheck/main.go` | 场景 §5 #1–#4（含两条构造性反证） | `make licensecheck` |
| `TB-16` | [`docs/spec/README.md`](../spec/README.md) §1（台账为生成物） | `scripts/licensecheck/main.go::printLedger` | `make license-ledger` 后 `git diff` 与门禁输出一致 | `make license-ledger` |
| `AR-15` `AR-21` | [`docs/modules/llm-components.md`](../modules/llm-components.md) §3 依赖表 · §4 规则表 | **本轮不改**（判据落在 ADR-0024 决定 1） | 调研材料 §3.1 的「冲突点」列 | —— |
| `AR-22` | [`docs/modules/llm-components.md`](../modules/llm-components.md) §8 未决项 | **本轮不改**（阶段 B 复用 Presidio 的识别器） | 调研材料 §3.2 与 §5 待核 T-2 | —— |
| `AR-30` | [`docs/modules/ai-capability.md`](../modules/ai-capability.md) §3 依赖表 · §8 未决项 1 | **本轮不改**（阶段 B 复用 Faker 的 `seed()`） | 调研材料 §3.3 与 §5 待核（`seed()` 跨版本稳定性 → ADR-0024 失效条件 5） | —— |
| `AR-33` | [`docs/modules/ai-capability.md`](../modules/ai-capability.md) §4 | **本轮不改**；判据仍是 `make archcheck` 的结构检查 | `make archcheck` 的 `AR-33` 项（既有） | `make archcheck` |
| `MD-17` | 本文件 + [`docs/modules/ai-capability.md`](../modules/ai-capability.md) | ——（文档轮） | `make trace` 的模块文档 ↔ 代码 ↔ 单测检查 | `make trace` |
| `DEV-1` `DEV-2` | 本文件 · [`docs/log.md`](../log.md) | —— | `make trace` 的变更包与变更日志形状检查 | `make trace` |

> **本轮没有新增规则 ID**，也没有改 `docs/design/`（`AGENTS.md` §3.1：升格须经用户确认）。
> 复用判据落在 ADR-0024（`background/decisions/`，参考层）—— 它约束阶段 B 的引入项，
> 但**尚未**升格为 `design/` 的规则（若要升格，需用户明确确认后写成一条新的 `TB` 类规则，编号由用户定 ——
> 本轮**不**预占编号，否则这里会变成对一条不存在的规则的引用）。

---

## 4. 代码实现

**文件清单**：

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `docs/background/research/ai-oss-reuse.md` | 新增 | 本轮的主产物：逐项对比表 + 空白矩阵 + 待核清单 |
| `docs/background/decisions/0024-ai-oss-reuse-boundary.md` | 新增 | 选型决策（候选 A/B/C + 失效条件 + 复查日期）—— 没有它，判据只是文档里的一段话 |
| `docs/background/research/README.md` | 修改 | 材料清单登记一行（`§5 如何登记新材料` 的强制要求） |
| `docs/background/decisions/README.md` | 修改 | ADR 索引登记一行（`§2 索引`） |
| `scripts/licensecheck/main.go` | 修改 | 补 Python 审计面（`TB-16` 的实现缺口）；复用既有 `verdict` / `row` / 台账机制 |
| `scripts/licensecheck/README.md` | 修改 | 工具自述必须与实现一致（它写「读 go.mod 的依赖」，现在不止） |
| `docs/spec/dependencies.md` | 重新生成 | 台账是生成物（`docs/spec/README.md` §1），**禁止手改** |
| `docs/modules/llm-components.md` | 修改 | §3 依赖表（复用候选）· §8 未决项 · **修 §1 的两行重复段** |
| `docs/modules/ai-capability.md` | 修改 | §3 依赖表 · §8 未决项 1/2 指向调研材料 |
| `docs/kb/known-issues.md` | 修改 | 记「Python 依赖无审计面」与「改锁文件后必须重跑 `make pyenv`」 |
| `docs/log.md` | 修改 | 本轮变更日志条目（最新在最上面） |
| `Makefile` | **不改** | `licensecheck` 已在 `lint` 里；不改触发条件、不加新目标 |
| `.gitignore` | 修改 | `.vscode/` 是**个人**编辑器配置（本轮开始前就已存在且未跟踪），加进忽略清单 —— `make done` 的 `git add -A` 否则会把它提交进仓库（收尾时发现的，经用户确认后处理） |
| `analysis/requirements.txt` | **不改** | 它是锁文件，本轮不引依赖 |

**关键类型与函数**（`scripts/licensecheck/main.go`，全部为工具内部细节，无对外契约）：

- `pyDep{Name, Version}` —— 锁文件里的一条运行期依赖；
- `pyLockfile(path)` —— 解析 `name==version` 行；**不猜、不规范化版本**；
- `pyInstalled(venv)` —— 扫 `<venv>/lib/python3*/site-packages/*.dist-info/METADATA`，
  按 `Name`（PEP 503 规范化）索引 `{Version, License-Expression, Classifier, License}`；
- `spdxVerdict(expr)` —— SPDX 表达式判定；` OR ` / ` AND ` / 括号按**最严格**取（与 Go 侧「多份许可取最严格」同规则）；
- `declaredLicense(meta)` —— 取值优先级 `License-Expression` > `Classifier: License :: OSI Approved ::` > `License:`；
- `pyRows(lock, venv)` —— 组装 `row`（`Lang` 区分两类），缺发行版 / 版本不一致一律 `unknown`。

**必须遵守的上位约束**：`TB-16`（依赖必须经许可审计）· `TB-14`（禁止未处理的错误返回值 —— 工具侧对应「解析失败即失败，不静默放行」）·
`MD-5`（不重复定义跨模块类型 —— 台账是生成物，不在别处手写第二份）· `AR-15` 的**精神**（fail-closed：认不出即失败）。

---

## 5. 测试与场景

`scripts/licensecheck` 是 Go 工具，本仓库的既有惯例是**用命令的实际输出当证据**（它没有 Go 单测文件；
`make gate` 每次跑它）。本轮沿用，并补两条**构造性反证**（篡改输入后必须失败）：

| # | 场景 | 输入 / 前置 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 1 | 正向：审计通过 | 锁文件 3 条运行期依赖全部装好且许可宽松 | 输出含两类依赖计数，退出码 0 | ✅ | `make licensecheck` 输出行（§6） |
| 2 | 反向：许可限制性 | 把某条依赖的元数据许可改成 `AGPL-3.0-only`（临时改副本） | 报「禁止」而非「允许」 | ✅ | §6 反证 A |
| 3 | 反向：环境缺发行版 | 锁文件里加一条未安装的依赖 | 报「环境缺 X → 跑 make pyenv」并失败 | ✅ | §6 反证 B |
| 4 | 反向：版本不一致 | 锁文件里把 `protobuf` 写成别的版本 | 报「锁文件 X vs 环境 Y」并失败 | ✅ | §6 反证 C |
| 5 | 正向：台账生成 | `make license-ledger` | `docs/spec/dependencies.md` 含两节（Go / Python），且与门禁输出一致 | ✅ | `git diff` + §6 |
| 6 | 回归：Go 侧不变 | 既有 176 个 Go 模块行 | 判定与说明逐行不变（只有章节标题变） | ✅ | `git diff docs/spec/dependencies.md` |

**没有覆盖的**：

- **没有 Go 单测**（`scripts/licensecheck/` 从来只有 `main.go` + `README.md`）。本轮不新增测试文件 ——
  加一个只测内部函数的单测壳子会让工具目录的既有形状变复杂，收益低于「构造性反证」（场景 2/3/4），
  后者证明的是**真的会失败**，比单测更直接。这是**取舍**，不是遗漏。
- **没有覆盖「许可表达式为 `LicenseRef-*` / 无任何许可字段」的情形**（代码里走「需人工判定」，未构造用例）。
- **没有覆盖无 venv 的干净克隆**（那会走 `require_pyenv` 之外的路径：工具报「读不到 site-packages」并失败）——
  只做了代码审查，未实测。

---

## 6. 验证证据

```console
$ make gate
fmt-check 通过 · go vet · staticcheck · errcheck（TB-14）· ruff format（61 files already formatted）· ruff check（All checks passed!）
✓ 忽略清单未误伤任何已入库文件
架构检查通过。
  顶层目录 · 跨平面依赖 · 核心内部可见性 · store 唯一 I/O 出口
  模块清单一致性 · CGO 与本地库 · 语言层数 · 护栏为唯一出口（AR-33）
追溯检查通过。
  模块文档↔代码↔单测 · 规则 ID 引用存在性 · 变更包与变更日志 · AGENTS.md 点名的技能 · 过期状态标记 · 悬空链接
泄漏检查通过。
依赖许可审计：Go 模块 158 个（参与构建）· Python 运行期依赖 4 个（含传递闭包）

Python 运行期依赖（4）
  ✓ PyYAML                    MIT           允许
  ✓ grpcio                    Apache-2.0    允许
  ✓ protobuf                  BSD-3-Clause  允许
  ✓ typing_extensions         PSF-2.0       允许

许可审计通过：没有传染性或限制性许可。
71 passed in 0.32s
✓ L4 单测（pytest）
ok  shen/edge/proxy（… 全部 Go 包 ok）
门禁通过。

$ make dev
✅ 通过：配置校验 · 非法配置被拒 · 启动 · 判定面 · 冒烟 · 规则回放 · L4 近线分析

$ make license-ledger
go run ./scripts/licensecheck -ledger > docs/spec/dependencies.md
已写入 docs/spec/dependencies.md
```

**关键数字**：

| 指标 | 值 |
| --- | --- |
| 门禁项 | 全部绿（含 `-race` 全量 Go 单测 + 71 例 pytest） |
| 台账新增 | Go 侧**零变化**（仅加小节标题）；Python 侧新增 **4** 行（`PyYAML` · `grpcio` · `protobuf` · `typing_extensions`） |
| Python 闭包 | 直接依赖 3 条 → 运行期闭包 **4** 条（多出的 `typing_extensions` 是 `grpcio` 的传递依赖 —— 只审直接依赖会漏掉它） |
| 候选审查 | **15** 个候选仓库：许可全部逐字核（MIT/Apache-2.0/BSD-3）· **2 个已归档**（[`protectai/llm-guard`](https://github.com/protectai/llm-guard) · [`Azure/PyRIT`](https://github.com/Azure/PyRIT)） |
| 逐项判定 | 功能重叠 **6** 处（`✅ 可复用` 3 · `🟡 有条件` 2 · `⛔` 1）· 无对等物 **12** 处 |
| 代码变更 | `analysis/` **0 行**（本轮不改 AI 代码） |

### 6.1 构造性反证（篡改输入后工具**必须**失败）

固定夹具在 `/tmp/liccheck-fixtures`（临时目录，不入仓；夹具形状：`lib/python3.14/site-packages/<name>-<ver>.dist-info/METADATA` + 一个锁文件）：

```console
# A 限制性许可：License-Expression: AGPL-3.0-only
  ✗ foo                                     AGPL-3.0-only    禁止

# B 表达式取最严：`MIT AND GPL-3.0` → 拦；`Apache-2.0 OR BSD-2-Clause` → 放
  ✗ baz                                     GPL-3.0          禁止
  ✓ qux                                     Apache-2.0       允许

# C 环境缺发行版 → 失败并指向 make pyenv
  ✗ absent                                  未知              需人工判定

# D 版本与锁文件不一致 → 失败（本轮在真环境上也自然撞到一次：protobuf 7.36.2 vs 7.35.1）
  ✗ bar                                     未审              需人工判定
      锁文件 9.9 vs 环境 2.0 不一致 —— 先跑 make pyenv（禁止用 A 版本的元数据审定 B 版本的许可）

# E 传递依赖缺席 → 必须被走进去（orphan → unlisted-dep）
  ✗ unlisted-dep                            未知              需人工判定

# F 认不出的标识符 → 需人工判定（不猜）
  ✗ weird                                    未识别             需人工判定
      许可标识符 "Frobnicate-1.0" 未登记 —— 需人工判定后加入 spdxVerdicts（取自 License-Expression）

# G 锁文件里的非 `==` 行 → 报错，不静默跳过
licensecheck: Python 依赖审计未完成：/tmp/liccheck-fixtures/lock_g.txt:1 不是 `name==version` 形式的锁定行："foo>=1.0" —— 审计范围不能被静默缩小
```

**反证的退出码**：A–G 全部非零（`go run` 的 `exit status 1`）。
**自然发生的反证**：修 Python 审计面的当天，它就报出了真实存在的环境漂移（场景 D），并推动了 `make pyenv` 同步。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 三项 `✅ 可复用` 都**没有实测**（只核了许可与文档） | 「可复用」≠「已验证可替换」 | 落地前做行为对齐测试（`json_repair`）或阶段 B 立项（Presidio / Faker） |
| 2 | `json_repair(strict=True)` 对 markdown 围栏的覆盖未知 | 决定 `AR-17` 那 91 行能否删 | 调研材料 §5 待核 T-1 |
| 3 | `Faker` vs `Mimesis` 未做基准 | 吞吐 | ADR-0024 未解决 3 |
| 4 | 消费者侧（`intent`/`chain`/`strategy`/`worker`）未审 | 那部分的自研量未知 | 另开一份材料 |
| 5 | 开发期依赖（`ruff`/`pytest`/`grpcio-tools`）不在台账里 | 有意为之（同 Go 侧口径），但**未被任何检查强制**：若将来有人把工具类依赖写进 `requirements.txt`，它会被审；写进 `requirements-dev.txt` 则不会 | 已写进 `scripts/licensecheck/README.md` 的「边界」段 |
| 6 | 「复用判据」尚未升格为 `design/` 规则 | 阶段 B 的约束力来自 ADR（参考层） | 用户确认后可升格（`AGENTS.md` §3.1）—— 本轮**不**预占编号 |
| 7 | `scripts/licensecheck/` 无 Go 单测 | 工具的回归保护靠门禁每次实跑 + 构造性反证（已做，但**不随代码入库**） | 若将来该工具长到 >500 行，把夹具入仓成 `python_test.go`；当前不值得 |
| 8 | Marksman 报 `docs/modules/llm-components.md` 的 `[strategy.md](strategy.md)` 为「ambiguous link」 | 无（`find docs -name strategy.md` 确认只有一份，链接可达） | 工具误报，不修 |

---

## 7.1 审视记录（L 档必填）

对着本轮 diff 逐项核（依据：全局技能 `audit` 的四道删除门槛）。

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `docs/modules/llm-components.md` §1 有**两行重复**（「已登记用途…」写了两遍） | 冗余（漂移） | `grep -n "已登记用途：\`session_response\`" docs/modules/llm-components.md` → 命中 L22 与 L24 | 删后一次（四行 → 两行） | 只命中一次 |
| 2 | `docs/spec/dependencies.md` 台账**不含 Python 依赖**，但 `TB-16` 写着「依赖必须经许可审计」 | 文档与实现不一致（实现缺口） | 台账 158 行全是 Go 模块；`main.go::buildModules` 用 `go list -deps` | 补实现（不改 `TB-16` 的措辞 —— 它的措辞本来就是对的） | 台账 §2 新增 4 个发行版 |
| 3 | `scripts/licensecheck/README.md` 写「它读 `go.mod` 的依赖在本地模块缓存里的 LICENSE」 | 漂移（文档落后于实现） | README「它是什么」段 | 改写为「两类依赖各自的集合与许可来源」对照表 | 已改；并补上「没做的事」边界段 |
| 4 | `analysis/requirements.txt` 锁 `protobuf==7.36.2`，环境实际 `7.35.1` | 环境漂移 | `analysis/.venv/bin/pip freeze` | 跑 `make pyenv` 同步；把规则记进 KB | `K-26`；审计转绿 |
| 5 | 本轮的变更包里出现了对**一条不存在的规则 ID** 的引用 | 悬空引用 | `make trace` 报 `D-3`（引用了未在 `design/` 定义、也未登记为已废弃的规则 ID），位置在变更包与变更日志里 | 改写为「一条新的 `TB` 类规则，编号由用户定」（**不**写出那个编号） | `make trace` 通过 |
| 6 | 调研材料里的 15 个候选中有 **2 个已归档**（[`protectai/llm-guard`](https://github.com/protectai/llm-guard) · [`Azure/PyRIT`](https://github.com/Azure/PyRIT)）—— 对「该引哪个」而言是**无用信息**吗？ | 判断：**不是** | 二者恰是二手文章最常推荐的选型（检索记录 R5） | 保留，并在 ADR-0024 失效条件 2 里当成先例引用 | 保留（删掉会让下一轮重复踩） |
| 7 | `docs/design/` 有没有被本轮改到？（`P-1` / `AGENTS.md` §3.1） | 越界核查 | `git diff --name-only docs/design/` → 空 | 无动作（本轮未升格任何规则） | 未越界 |
| 8 | 有没有悬空链接指向新文件？ | 悬空引用 | `make trace` 的悬空链接项（`TC-3`） | 无（新文件的引用都被对方登记了） | 通过 |

> **排除项**：`docs/background/` · `docs/plans/` · `docs/log.md` · `docs/kb/` 是历史记录类文件，按技能
> `dev-loop-project` §7.1 不在审视范围内 —— 本轮的 §7 第 8 条（工具误报）是唯一发现的、**有意不修**的项。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-20 | 首版：调研 + 判定表 + ADR-0024 + Python 许可审计面 | 用户四问四答（2026-09-20）+ 调研材料 [`../background/research/ai-oss-reuse.md`](../background/research/ai-oss-reuse.md) |
