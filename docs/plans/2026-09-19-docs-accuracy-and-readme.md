# 变更包：整体文档准确化 + 根 README（项目是什么 / 怎么用 / 效果什么样）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 新增根 `README.md`（项目入口）；修正散落在 `docs/` 里的过期事实（配置消费状态 · 模块计数 · 测试计数 · 工具清单） |
| 日期 | 2026-09-19 |
| 状态 | 已验证 |
| 涉及模块 | 无代码模块改动（纯文档轮）；涉及文档：[`../../README.md`](../../README.md)（新增）· [`../README.md`](../README.md) · [`../progress.md`](../progress.md) · [`../spec/config.md`](../spec/config.md) · [`../modules/README.md`](../modules/README.md) · [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) · [`../design/modules.md`](../design/modules.md) · [`../design/structure.md`](../design/structure.md) |
| 决策数 | 已答 3 项 / 待定 0 项 |
| 关联 | 上轮 [`2026-09-19-oh3-leak-check.md`](2026-09-19-oh3-leak-check.md) · [`2026-09-19-caddy-l1-base.md`](2026-09-19-caddy-l1-base.md) · [`../log.md`](../log.md) |

---

## 1. 需求与验收

**要解决什么**：① 仓库**没有根 README** —— 新人无从知道这是什么样的项目、怎么用、效果长什么样；
② `docs/` 里多处**过期事实**（配置段的消费状态、模块计数、测试计数、工具清单）会误导读者。

**做完之后**：从根 `README.md` 五分钟能看懂「项目是什么 · 现在能跑到什么程度 · 怎么跑起来 · 效果长什么样 · 边界在哪」；
`docs/` 里的事实与**当前代码**一致。

**验收判据**：

1. 根 `README.md` 存在，含：一句话定位 · 一页架构图 · 阶段现状 · 5 分钟上手 · **真实输出**的效果示例 · 目录与文档地图 · 门禁说明 · 使用边界 · 已知限制。
2. 文档里的事实断言**逐条对得上代码**（本轮发现的 4 类过期各修一处，见 §5 场景表）。
3. `make gate` 全绿（含 `make trace` 的悬空链接检查与 `make leakcheck`）。
4. 根 README 里出现的每条命令，**都在这条仓库里跑得通**。

**不做什么**：

- **不改任何规则**（`docs/design/` 只做与代码对齐的**事实性**修正，不新增/不降格规则；`docs/design/modules.md` 只加了一句历史澄清）；
- 不新建 `docs/integrate/` · `docs/ops/` · `docs/analytics/`（`docs/README.md` §3 已登记为「待建」，属接入物料轮）；
- 不写营销式介绍（README 只写可验证的事实与真实输出）。

---

## 2. 设计逻辑

**决策树**：

```text
文档准确化 + 根 README
├── 第 1 轮（受众与位置）：README 放哪、给谁看
│   └── 根 README（对外/新人/接手人）vs docs/README.md（开发导航）→ 分工：根 README 讲「是什么/怎么用/效果」，
│       docs/README.md 保持「开发与设计的导航中枢」，两者互相指一下
├── 第 2 轮（准确性）：哪些断言会漂移、怎么核
│   └── 计数（模块/测试）· 配置消费状态 · 工具与目标清单 → 一律用命令核（§5）
└── 第 3 轮（边界）：README 里哪些必须写清楚
    └── 使用边界（SB-7 仅授权环境）· 已知限制（策略面 S4 未实现等）· 未验项（AR-29 未重测）
```

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 根 README 与 `docs/README.md` 的分工 | 根讲**产品视角**（是什么/怎么用/效果），`docs/README.md` 讲**开发视角**（规则/模块/进度在哪） | 两者受众不同；重复会让两处都漂移 | `README.md` §1–§9 · `docs/README.md` 开头加互指 |
| ② | 效果怎么写 | **只贴真实输出**（`make dev` 五步 · 真二进制 + 真 TLS 的 curl 结果），并标注哪些是实跑、哪些是测试覆盖 | 「效果」若靠描述就变成宣传；真实输出可被反驳也可被复现 | `README.md` §4 |
| ③ | 计数类事实怎么办 | 保留具体数字（项目惯例），但**每轮落地必须同步**（本轮把 4 处过期的改成实测值） | 去掉数字会丢掉「现在做到哪」的信息；同步义务已在 `progress.md` 头部写明 | 见 §4 文件清单 |

**仍未定**：无（本轮为文档事实对齐，无取舍）。

**接缝与接口**：本轮不涉及任何代码接口。

**数据流（事实 → 文档）**：

```text
make help / go test / grep 代码  ──►  实测事实  ──►  写进文档
                                                        │
                                      逐条回核（§5 场景表）│
                                                        ▼
                                        make trace（链接存在性）+ 人工读一遍
```

---

## 3. 追溯矩阵

| 规则 / 依据 | 文档位置 | 代码 / 事实来源 | 测试 / 核验 | 验证命令 |
| --- | --- | --- | --- | --- |
| `NI-1`（不影响业务） | `README.md` §1 / §4.2 失败行为行 | `edge/proxy/handler.go` 的 `dispatch` / `forwardMirage` | `edge/proxy` 单测 + 真二进制断核心实跑 | `make gate` · 实跑 |
| `MD-12`（三值闭集） | `README.md` §1 三条不可妥协 | `api/judge/v1/judge.proto` 的 `Action` 枚举 | `edge/proxy` / `director` 单测 | `make gate` |
| `INT-8`（业务侧零改写） | `README.md` §4.2 | `edge/proxy/handler.go`（注入只挂引流侧） | `edge/proxy` 的注入类单测 + 端到端 | `make gate` |
| `INT-11`（影子模式先行） | `README.md` §3 / §4.2 | `edge/proxy/cmd/proxy/main.go` 默认 `SHEN_PROXY_SHADOW=true`；核心 `loader.Shadow()` | `edge/proxy` 单测 | `make gate` |
| `INT-23`（来源 IP 透传） | `README.md` §4.2 | Caddy `reverse_proxy` 的 `X-Forwarded-*` | `TestForwardingSemanticsWithTLS` | `make gate` |
| `ST-24`（策略是数据） | `docs/spec/config.md` §2.0 / §2.8 / §2.9 | `core/internal/policy/policy.go` 的 `Decoys` / `Honeypots` / `Whitelist`；`core/cmd/core/main.go` 的 `assembleDeception` | `policy` 单测（12 例） | `make gate` |
| `OH-1`…`OH-5` | `README.md` §8 | `scripts/check-leak` | 上轮负向探针 | `make leakcheck` |
| `SB-1` / `SB-2` / `SB-6` / `SB-7` | `README.md` §8 | 无控制面代码；`.proto` 只有判定与遥测两个面 | 人工核对 + 契约清单 | `make trace` |
| `MD-18` / `MD-19` | `README.md` §5 · `docs/README.md` §1.1 | `docs/design/modules.md` §1.1（解析源） | `archcheck` 模块清单一致性 | `make archcheck` |
| `DEV-1` / `DEV-2` | 本变更包 + [`../log.md`](../log.md) 新条目 | —— | `tracecheck` 形状与路径检查 | `make trace` |

---

## 4. 代码实现（本轮 = 文档 + 事实核对）

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `README.md`（根） | **新增** | 仓库此前没有入口文档：讲清「是什么 / 现在到哪 / 怎么跑 / 效果什么样 / 边界与限制」，并指向 `docs/` |
| `docs/README.md` | 改 | ① 开头加「想看项目全貌 → 根 README」的互指；② 新增 **§1.1 门禁与工具**（`scripts/` 七项 + 各自依据的规则），此前文档完全没有工具清单 |
| `docs/spec/config.md` | 改 | **过期事实**：`whitelist` / `decoys` / `honeypots` 三段早已被 `cmd/core` 消费，原文写「阶段 2a 未接 / 未接入」；§2.8 / §2.9 的「阶段 2b 实现时才进示例配置」也已不成立（`deploy/config/config.example.yaml` 已有 `decoys:` 与 `honeypots:`） |
| `docs/progress.md` | 改 | ① 头部「21 个有效模块」→ **23 个有效模块（共 24 行）**；② 第 11 行与 §2.1 的测试计数 29 → **30** |
| `docs/modules/README.md` | 改 | ① 头部「（21 个模块）」→ **23 个有效模块（共 24 行）**；② `adapter-proxy` 行计数 29 → **30** |
| `docs/modules/adapter-proxy.md` | 改 | 头部状态与变更记录的测试计数 29 → **30** |
| `docs/design/structure.md` | 改 | §1.5 的 `edge/proxy/` 行计数 29 → **30** |
| `docs/design/modules.md` | 改 | 头部历史注记补一句澄清：「§1.1（**当时** 21 个模块）」，并指明此后新增 `control` / `decoy` / `honeypot` 现为 23 个 —— **不改规则，只消除歧义** |

**关键「类型」**：本轮没有代码类型；新增的是**两份导航契约** —— 根 `README.md`（产品视角入口）与 `docs/README.md` §1.1（工具与门禁清单）。

**必须遵守的上位约束**：`NI-1` · `MD-12` · `INT-8` / `INT-11` / `INT-23` · `ST-24` · `OH` · `SB` 段 · `DEV-1`/`DEV-2`。

---

## 5. 测试与场景（事实核对表）

文档轮没有单测，「测试」= **逐条拿命令核事实**：

| # | 事实断言 | 怎么核（命令） | 核之前（文档） | 实测 | 动作 |
| --- | --- | --- | --- | --- | --- |
| 1 | `whitelist` 段已被消费 | `grep -rn "loader.Whitelist(" core/ \| grep -v _test` → 1 处 | ⏳ 只解析与校验 | **1 处调用**（非影子路径） | 改为 ✅ 已消费 |
| 2 | `decoys` 段已被消费 | `grep -n "loader.Decoys(ctx)" core/cmd/core/main.go` | ⏳ 未接入 | **已装配**（写入 store → 诱饵面） | 改为 ✅ 已消费（核心内，边缘待 `S4`） |
| 3 | `honeypots` 段已被消费 | `grep -n "loader.Honeypots(ctx)" core/cmd/core/main.go` | ⏳ 未接入 | **已装配**（类型注册 + 后端池） | 同上 |
| 4 | `decoys` / `honeypots` 已进示例配置 | `grep -n "^decoys:\|^honeypots:" deploy/config/config.example.yaml` | 文档称「阶段 2b 才进」 | **已存在**（第 71 / 86 行） | 改掉那句 |
| 5 | 有效模块数 | `grep -c '^| [0-9]' docs/design/modules.md`（§1.1 行数）+ 第 12 行已合并 | 「21 个」 | **24 行 / 23 个有效** | 改 2 处（`progress.md` · `modules/README.md`）+ 澄清 1 处（`design/modules.md` 历史注记） |
| 6 | `edge/proxy` 测试数 | `cat edge/proxy/*_test.go \| grep -c '^func Test'` | 「29」 | **30** | 改 5 处 |
| 7 | 全仓测试数（README 新写） | `cat core/internal/*/*_test.go edge/*/*_test.go core/cmd/core/*_test.go \| grep -c '^func Test'` | —— | **177** | 写进 README §2 |
| 8 | 已实现包数（README 新写） | `ls -d core/internal/*/ edge/*/` 对照 `docs/design/modules.md` §1.1 | —— | **14 个包有实现** | 写进 README §2 |
| 9 | `make help` 的目标清单（README 命令表 · `docs/README.md` §1.1） | `make help` | `docs/` 无工具清单 | 23 个目标 · 7 个 `scripts/` 工具 | 写进两处 |
| 10 | README §4 的效果输出 | 实跑 `make dev` / 真二进制 + TLS + `curl` | —— | 与贴出的输出一致 | 贴真实输出 |

**没有覆盖的**：

- 文档轮**没有代码改动**，因此没有新增测试；`make gate` 的作用是「**没有把东西弄坏**」+ 链接与形状检查；
- README §4 里「改道到幻境后端」的**端到端**效果目前只能在单测与集成测试里看到（策略面 `S4` 未实现，见 §7 遗留 1）—— README §9 已显式写明这条限制，**没有**把它写成现状；
- 中文措辞与可读性属人读范畴，机器核不了（只做了链接与路径核）。

---

## 6. 验证证据

```console
$ make gate
fmt-check … vet … staticcheck … errcheck …
archcheck … 架构检查通过。
trace … 追溯检查通过。
leakcheck … 泄漏检查通过。
licensecheck … 许可审计通过。
go test -race ./...   → 全 ok
门禁通过。

$ make dev
✅ 通过：配置校验 · 非法配置被拒 · 启动 · 判定面 · 冒烟 · 规则回放

$ grep -c '^func Test' <全仓测试文件>
177
```

**关键指标**：新增根 README **1 份**（约 200 行）· 修正过期事实 **4 类 / 10 处** · `docs/README.md` 新增工具与门禁小节 **1 个** · 代码改动 **0 行**。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 策略面（`api/policy/v1`）未实现 | 2b 能力（诱饵/后端池/预生成内容）在**边缘**看不到；README §2 / §9 已写明 | 阶段 2b 的下一轮（最高优先级的结构性断点） |
| 2 | `docs/integrate/` · `docs/ops/` · `docs/analytics/` 仍待建 | 接入方 / 运维 / 运营没有专用文档（README §8 只给了规则源头） | [`../README.md`](../README.md) §3 已列「首份文件必须包含什么」 |
| 3 | `docs/spec/logs.md` · `metrics.md` 待写 | 日志字段与指标口径没有权威字典 | [`../README.md`](../README.md) §3 |
| 4 | 计数类事实会继续漂移（模块数 / 测试数） | 文档与代码可能再次不一致 | **每轮落地同步**（`docs/progress.md` 头部已有此义务）；`tracecheck` 目前**不**核计数 |
| 5 | `AR-29` 换底座后未实测（实验 `E3`） | README §9 已标明 | 接入演练 |

---

## 7.1 审视记录

本轮为**文档轮**，审视对象就是被改的文档；未删除任何内容，只改事实与补入口。

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 仓库**没有根 README** | 缺失 | `ls README*` → `No such file or directory` | 新增根 `README.md`（产品视角入口） | ✅ |
| 2 | `docs/spec/config.md` §2.0 三行写「未接入」，实际已消费 | **过期状态** | `grep -rn "loader.Decoys(\|loader.Honeypots(\|loader.Whitelist(" core/` → 各 1 处 | 改为 ✅ 已消费 + 注明「边缘待 `S4`」 | ✅ |
| 3 | §2.8 / §2.9 称「阶段 2b 实现时才进示例配置」 | **过期状态** | `grep -n "^decoys:\|^honeypots:" deploy/config/config.example.yaml` → 71 / 86 行 | 改为「已进示例配置与校验器」 | ✅ |
| 4 | 「21 个模块」两处（`progress.md` · `modules/README.md`） | **过期计数** | `docs/design/modules.md` §1.1 共 24 行、23 个有效 | 改为 23 / 24 行 | ✅ |
| 5 | `docs/design/modules.md` 头部「§1.1（21 个模块）」易读成现状 | 歧义（**非**规则错误） | 同文件 §1.1 表 | 补「当时 21 个」+ 现为 23 个；**规则与 ID 未动** | ✅ |
| 6 | `edge/proxy` 测试计数散落 5 处且已过期 | 过期计数 | `grep -c '^func Test' edge/proxy/*_test.go` → 30 | 5 处改为 30 | ✅ |
| 7 | `docs/` 没有任何**工具清单**（新人有 `scripts/` 但不知道谁在门禁里） | 缺失 | `grep -n "scripts/" docs/README.md` 原为空 | `docs/README.md` 新增 §1.1（7 项工具 + 依据规则 + 两个未实现工具的显式标注） | ✅ |

> 历史记录类文件（[`../log.md`](../log.md) · `docs/plans/` · `docs/background/` · `docs/kb/`）**不在审视范围**：它们是当时的快照，写下过时内容是正确的。
> 本轮**未删任何历史内容**；`docs/plans/2026-09-19-caddy-l1-base.md` 里「29 测试」的说法属历史快照，保留。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 新增根 `README.md`（是什么 / 现状 / 上手 / 真实效果 / 目录与文档地图 / 门禁 / 边界 / 限制）；修正 `config.md` 的配置消费状态、`progress.md` 与 `modules/README.md` 的模块计数、5 处测试计数；`docs/README.md` 新增门禁工具小节 | 用户要求（文档准确 + 根 README） |
