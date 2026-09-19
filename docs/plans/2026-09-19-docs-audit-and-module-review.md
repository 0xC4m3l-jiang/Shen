# 变更包：文档审视（删除无用信息 · 事实核验）+ 模块定义/设计一致性核验

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 对**活文档**做一次审视：机器能核的全部跑一遍，逐条判定；修过期事实、删幽灵配置、清自相矛盾；并核验「模块定义 / 模块设计」是否与代码一致 |
| 日期 | 2026-09-19 |
| 状态 | 已验证 |
| 改动分级 | **M**（只改文档与示例物料；**不改代码、不改契约、不改已确认规则**）—— 降档理由：没有行为/接口变化，故不触发 L 档的访谈与 ADR |
| 涉及文档 | [`../design/structure.md`](../design/structure.md) · [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) · [`../../edge/mirror/README.md`](../../edge/mirror/README.md) · [`../../edge/proxy/config/sidecar.example.yaml`](../../edge/proxy/config/sidecar.example.yaml) · [`../../edge/proxy/config/front-proxy.example.env`](../../edge/proxy/config/front-proxy.example.env) |
| 决策数 | 已答 0 项（无取舍）/ **待用户确认 2 项**（规则措辞冲突，见 §7） |
| 关联 | 上轮 [`2026-09-19-policy-plane.md`](2026-09-19-policy-plane.md) · [`../log.md`](../log.md) · 审视方法见全局技能 `audit` |

---

## 1. 需求与验收

**要解决什么**：文档与示例物料里积累了**过期事实**（说「未实现」其实已实现、计数对不上）、
**幽灵内容**（示例写了代码没有的配置项与端点）、**自相矛盾**（同一份 README 两处结论相反）。
这些都让读者必须自己怀疑文档，等于文档失效。

**做完之后**：活文档里的每条事实断言都能对上代码；示例物料里不存在无人读取的配置项；
模块定义（清单 · 目录 · 文档 · 单测 · 依赖）四方一致，并有机器证据。

**验收判据**：

1. 机器可核的四类（悬空引用 · 孤儿文档 · 过期状态标记 · 过期豁免）零错误（`make trace`）。
2. 模块三方清单（[`../design/modules.md`](../design/modules.md) §1.1 · [`../progress.md`](../progress.md) §1 · [`../modules/README.md`](../modules/README.md) §0）模块名与数量一致。
3. 每个已实现模块的**实际 import** 与其模块文档 §3 的依赖表一致。
4. 示例物料里的每个 env / 端点都有代码读取点；反之，代码读取的每个 env 都有文档去处。
5. 模块文档九章齐全（`MD-2`）；无孤儿导出符号。
6. `make gate` 全绿。

**不做什么**：

- **不改任何已确认规则**（`docs/design/` 只做事实性修正：计数与状态；**发现的两条规则措辞冲突不回改，改为上报**，见 §7）；
- **不改代码**（本轮无 `*.go` 改动）；
- **不删历史记录**（`docs/background/` · `docs/plans/` · `docs/log.md` · `docs/kb/` 按项目规范不在审视范围）。

---

## 2. 设计逻辑

**方法**（技能 `audit` §3：机器优先，人只补机器核不了的）：

```text
文档审视
├── 第 1 步 · 跑机器检查
│   ├── make trace（悬空引用 · 孤儿文档 · 过期状态标记 · 过期豁免）
│   ├── make archcheck（目录 · 依赖方向 · 清单一致 · 语言层数 · CGO）
│   └── make leakcheck（可观测面泄漏）
├── 第 2 步 · 自查脚本（本轮新增的一次性核验）
│   ├── 模块三方清单交叉比对（modules.md §1.1 ⇄ progress.md §1 ⇄ modules/README §0）
│   ├── 模块文档九章齐全性
│   ├── 导出符号孤儿检查（除自身与测试外无引用）
│   ├── env 双向核对（文档声明的 env ⇄ 代码读取的 env）
│   ├── 文档里 `make <目标>` 是否真实存在（幽灵命令）
│   └── design/ 的 P-2 含糊词
└── 第 3 步 · 人读（机器核不了的）
    ├── 语义是否一致（同一份文档两处结论相反？）
    ├── 叙述是否误导（示例声称代码没有的东西？）
    └── 内容是否真的无用（重复段落 · 自述废话 · 流水账）
```

**已确认的判定**（动作只有四种：删除 / 修正 / 标废弃 / 保留并写理由）：

| # | 判定 | 依据 |
| --- | --- | --- |
| ① | 过期事实 → **修正** | 证据是命令输出（见 §5） |
| ② | 幽灵配置项 / 幽灵端点 → **删除** | 门槛①「证明无引用」：全仓检索结果为 0 命中 |
| ③ | 重复的门禁链（4 处）→ **保留** | 它们是**分级**的：入口摘要（`README.md` §7、`docs/README.md` §1.1）vs 详细链路（[`../kb/dev-workflow.md`](../kb/dev-workflow.md) §3）vs 适配面配置（[`.pi/devloop.md`](../../.pi/devloop.md)）—— 合并反而会让入口文档失去可读性 |
| ④ | 未实现的规则能力（`ST-17` 探针）→ **删示例 + 登记未决**，不改规则 | 规则没错，是没实现；删掉「模板声称有探针」的假象，把缺口登记进模块文档 §8 |

---

## 3. 追溯矩阵

| 规则 / 依据 | 文档位置 | 代码 / 事实来源 | 测试 / 核验 | 验证命令 |
| --- | --- | --- | --- | --- |
| `MD-2`（一模块一文档，九章） | `docs/modules/*.md`（23 份） | —— | 九章齐全性自查：缺章 0 | 自查脚本（§5 行 6） |
| `MD-18` / `MD-19`（清单 ↔ 目录 ↔ 文档） | [`../design/modules.md`](../design/modules.md) §1.1 | `core/internal/*` · `edge/*` 目录 | 三方清单交叉比对一致（23 个有效模块） | `make archcheck` + 自查（§5 行 5） |
| `MD-22`（模块独立可测） | 各模块文档 §7 | `*_test.go` | `make trace`（模块文档↔代码↔单测） | `make trace` |
| `ST-17`（探针语义） | [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) §8 未决 12 | 无实现（`grep healthz --include=*.go` 只命中测试夹具） | —— | 自查（§5 行 9） |
| `MD-10`（缓存必须有容量上限） | [`../../edge/proxy/config/front-proxy.example.env`](../../edge/proxy/config/front-proxy.example.env) | `SHEN_PROXY_CACHE_MAX`（`cmd/proxy/main.go`） | env 双向核对 | 自查（§5 行 10） |
| `INT-8`（只改改道侧响应） | 同上（注入片段说明） | `SHEN_PROXY_INJECT`（`handler.go` 的注入 transport） | `edge/proxy` 注入类单测 | `make gate` |
| `INT-6`（形态①不在请求路径上） | [`../../edge/mirror/README.md`](../../edge/mirror/README.md) | `edge/mirror/receiver.go` | `receiver_test.go`（3 例） | `make gate` |
| `OH-2`（攻击者可见面判据） | 同上 · 未决项 12 的落点选择 | —— | —— | `make leakcheck` |

---

## 4. 代码实现（本轮 = 文档与示例物料）

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| [`../design/structure.md`](../design/structure.md) §1.5 | 改（6 处） | 过期事实：`contract` 文件数 6→**8**；`responder`/`isolation`/`edge-injection` 标「⏳ 阶段 2b」但它们**已实现并含单测**；`scripts/*` 行把 `check-leak` 写成「仍只有说明」（**已实现**）；`edge/proxy` 测试数 30→**37**；补 `decoy`/`honeypot` 两行（已实现却无行） |
| [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) §8 | 改 | 新增未决项 12：`ST-17` 探针**未实现**（本轮删掉了示例里假装有的探针） |
| [`../../edge/mirror/README.md`](../../edge/mirror/README.md) | 改 | **自相矛盾**：目录表写「`receiver.go` ✅ 含单测」，下方又写「接收端尚未实现」→ 改为准确的现状；补 `SHEN_MIRROR_LISTEN` 配置段（此前**全仓零文档**）；把「接收端形态取决于镜像方式」这一真实前提留下并写清 |
| [`../../edge/proxy/config/sidecar.example.yaml`](../../edge/proxy/config/sidecar.example.yaml) | 改 | **删幽灵配置** `SHEN_CORE_TLS`（全仓无引用）；**删幽灵探针** `livenessProbe:/__shen/healthz` 与 `readinessProbe:/__shen/readyz`（代码无该端点）；补策略面两个 env；把「明文 gRPC 跨节点」这条**真实**约束保留成注释（mTLS 未实现） |
| [`../../edge/proxy/config/front-proxy.example.env`](../../edge/proxy/config/front-proxy.example.env) | 改 | 补 `SHEN_PROXY_CACHE_MAX`（`MD-10` 的容量上限）与 `SHEN_PROXY_INJECT`（`INT-8` 的注入入口）—— 两者代码都读，模板此前都没有 |

**关键「类型」**：本轮没有代码类型；改的是**示例物料与事实陈述**。

**必须遵守的上位约束**：`MD-2` · `MD-18` / `MD-19` · `MD-22` · `MD-10` · `INT-6` / `INT-8` · `ST-17` · `OH-2` · 技能 `audit` 的四道删除门槛。

---

## 5. 测试与场景（审视表 · 技能 `audit` §5 要求的产出）

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | [`../design/structure.md`](../design/structure.md) §1.5 写 `contract` 是「6 个类型文件」 | 过期计数 | `ls core/internal/contract/*.go \| wc -l` → **8** | 修正 | ✅ 改为 8（并补「只放类型，无逻辑」） |
| 2 | 同上：`core/internal/{responder,isolation}` 标「⏳ 阶段 2b」 | 过期状态 | 两目录各有 `*.go` + `*_test.go`（`progress.md` §1 标 ✅） | 修正 | ✅ 改为已建（并注明「未接到请求路径」） |
| 3 | 同上：`edge/injection/` 标「⏳ 阶段 2b」 | 过期状态 | `ls edge/injection/` → `iface.go` `injection.go` `injection_test.go`（10 例） | 修正 | ✅ 改为已建（含 `ST-5` 说明） |
| 4 | 同上：`scripts/*` 行把 `check-leak/` 写成「仍只有说明」 | 过期状态 | `scripts/check-leak/main.go` 已实现并接进 `make gate` | 修正 | ✅ 移入已实现组 |
| 5 | 同上：`edge/proxy/` 测试数写 30 | 过期计数 | `cat edge/proxy/*_test.go \| grep -c '^func Test'` → **37** | 修正 | ✅ 改为 37 |
| 6 | 同上：无 `core/internal/{decoy,honeypot}` 行（两者已实现） | 漏写 | `progress.md` §1 两行均为 ✅ | 修正 | ✅ 与 responder/isolation 合并成一行 |
| 7 | [`../../edge/mirror/README.md`](../../edge/mirror/README.md)：目录表「`receiver.go` ✅ 含单测」与下方「⚠️ 接收端**尚未实现**」**结论相反** | 自相矛盾 | 同文件两处；`ls edge/mirror/` → `receiver.go` + `receiver_test.go`（3 例） | 修正 | ✅ 删除过期警示，改写为准确现状 + 保留真实前提（TEE/eBPF 需先重组 TCP 流） |
| 8 | `SHEN_MIRROR_LISTEN`（接收端监听地址）**全仓零文档** | 漏写 | `grep -rn SHEN_MIRROR_LISTEN .` → 仅 `edge/mirror/cmd/mirror/main.go` | 修正 | ✅ 在 mirror README 新增配置段（含 `SHEN_CORE_ADDR`） |
| 9 | [`../../edge/proxy/config/sidecar.example.yaml`](../../edge/proxy/config/sidecar.example.yaml) 设 `SHEN_CORE_TLS=required`，但**没有任何代码读它** | **幽灵配置** | `grep -rn SHEN_CORE_TLS .`（排除 `.bin`）→ 仅该模板本身 | 删除（门槛①通过） | ✅ 已删；改成说明跨节点必须 mTLS 的注释（不假装有开关） |
| 10 | 同上：`livenessProbe: /__shen/healthz` · `readinessProbe: /__shen/readyz` —— **代码里没有这两个端点** | **幽灵内容** | `grep -rn 'healthz\|readyz' --include='*.go'` → 仅 4 处**测试夹具**（白名单路径前缀），无实现 | 删除 + 登记 | ✅ 已删；`ST-17` 缺口登记进 [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) §8 未决 12 |
| 11 | `SHEN_PROXY_CACHE_MAX`（代码读）与 `SHEN_PROXY_INJECT`（代码读）**模板都没写** | 漏写 | `grep -rhoE '"SHEN_[A-Z_]+"' --include='*.go' edge/ core/` 与模板集合求差 | 修正 | ✅ 两个 env 补进 [`../../edge/proxy/config/front-proxy.example.env`](../../edge/proxy/config/front-proxy.example.env) |
| 12 | 门禁链在 4 处出现（根 `README.md` §7 · [`../README.md`](../README.md) §1.1 · [`../kb/dev-workflow.md`](../kb/dev-workflow.md) §3 · [`.pi/devloop.md`](../../.pi/devloop.md)） | 重复内容（疑似） | 逐处比对：四处**当前一致**，且粒度不同（入口摘要 / 表格 / 详细链路 / 适配面配置） | **保留**（写理由） | ✅ 保留：合并会让入口文档失去可读性；一致性由 `make trace` 与人工在改门禁时同步负责 |
| 13 | `docs/design/` 是否含 P-2 禁止的含糊表述 | 合规核验 | `grep -rn '可能\|大概\|建议后续\|考虑\|或许' docs/design/*.md` → 仅 `README.md`（**规则定义自身**，规范明确「只允许本文件命中」） | 保留 | ✅ 零违规 |
| 14 | 23 份模块文档是否九章齐全（`MD-2`） | 合规核验 | 自查脚本：缺章模块数 **0** | 保留 | ✅ |
| 15 | 是否存在「导出但无人引用」的符号（死代码） | 僵尸核验 | 自查脚本：`core/internal/*` + `edge/*` 的导出 func/type，除自身与测试外零引用 → **0 命中** | 保留 | ✅ |
| 16 | 文档里提到的 `make <目标>` 是否都存在 | 幽灵命令 | 提取全部 `make xxx`（非 `docs/plans`/`docs/background`）→ 与 `Makefile` 目标集合求差 → **0 命中** | 保留 | ✅ |
| 17 | 模块三方清单（[`../design/modules.md`](../design/modules.md) §1.1 · [`../progress.md`](../progress.md) §1 · [`../modules/README.md`](../modules/README.md) §0）是否一致 | 定义准确性 | 交叉比对：23 个有效模块**三方一致**（`modules.md` 多出的 `adapter-sidecar` 是已合并行的墓碑、`severity` 来自另一张表） | 保留 | ✅ |
| 18 | 已实现模块的**实际 import** 与模块文档 §3 依赖表是否一致 | 定义准确性 | `go list -f '{{join .Imports " "}}'` 逐模块：均只依赖 `contract`（+ 文档已写的消费者定义接口：`director→judge` · `control→judge/session/telemetry` · `policy→store`） | 保留 | ✅ 与 [`../design/structure.md`](../design/structure.md) §1.6.2 一致 |

---

## 6. 验证证据

```console
$ make gate
门禁通过。          # fmt · vet · staticcheck · errcheck · archcheck · trace · leakcheck · license · test -race

$ go run ./scripts/tracecheck
追溯检查通过。       # 悬空引用 · 孤儿文档 · 过期状态标记 · 过期豁免 · AGENTS.md 点名的技能

$ cat core/internal/*/*_test.go edge/*/*_test.go core/cmd/core/*_test.go | grep -c '^func Test'
189
```

**关键指标**：修正过期事实 **6 处** · 删除幽灵配置 **1 项** + 幽灵端点 **2 个** · 补漏写 **3 项**（2 个 env + 1 段配置文档）·
保留并写理由 **7 项** · 代码改动 **0 行** · 规则改动 **0 条**（两条冲突改为上报，见 §7）。

---

## 7. 遗留与未决（含**待用户确认**的规则冲突）

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | ⚠️ **`MD-5` 与进程内共享类型目录的矛盾（待确认）**：`MD-5` 规定「跨模块共享的**类型定义必须**收敛到 `structure.md` 的 `api/`」；而现实是 `api/` 只放**跨进程**契约（proto 生成物），**进程内**共享类型集中在 `core/internal/contract/`（8 个文件），[`../design/structure.md`](../design/structure.md) §1.2/§1.6.2 明确把 `contract` 定为「只放类型、无逻辑」的词汇表叶子 | 读者按 `MD-5` 字面执行会得出「`contract` 违规」的错误结论；规则与设计文档自相矛盾 | **不动手**（`P-3`：发现冲突停下报告，不得改 `design/` 迁就实现）。建议：开一轮把 `MD-5` 措辞改成「跨**进程**契约 → `api/`；进程**内**共享类型 → `core/internal/contract/`」，或在 `modules.md` 登记一条明确例外 |
| 2 | ⚠️ **`MD-19` 例外清单不完整（待确认）**：`MD-19` 只列了 `api/`（生成物）· `scripts/`（工具）· `deploy/`（配置）三类例外，而 `core/internal/contract/` 同样是「清单外但必须存在」的目录 | 同上：按字面读，`contract` 属「清单外目录承载业务逻辑」 | 与第 1 条同轮处理（同一条改动可以同时修 `MD-5` 与 `MD-19`） |
| 3 | `ST-17`（存活/就绪探针）未实现 | k8s 部署只能用 TCP 探活；示例里的探针配置本轮已删 | [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) §8 未决 12 |
| 4 | `docs/spec/logs.md` · `docs/spec/metrics.md` 待写 | 日志字段与指标口径无权威字典 | [`../README.md`](../README.md) §3 |
| 5 | 接入面文档（`integrate/` · `ops/` · `analytics/`）待建 | 接入方与运维仍只能读规则原文 | 同上 |

---

## 7.1 审视记录

> 本轮**本身**就是审视轮，§5 的表即审视表（18 条）。
> 补充说明删除的**可恢复性**（技能 `audit` 门槛④）：本轮删除的都是**示例物料内的幽灵条目**，
> 内容为「代码不读的 env」与「代码没有的端点」，恢复方式 = 从本变更包的 §5 行 9/10 抄回，
> 或 `grep -rn SHEN_CORE_TLS docs/log.md`（日志里保留了当时的形态）。
> 未删除任何历史记录文件（`docs/background/` · `docs/plans/` · `docs/log.md` · `docs/kb/` 的历史条目）。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 文档审视：修正 `structure.md` §1.5 六处过期事实 · 修 `edge/mirror/README.md` 自相矛盾并补 `SHEN_MIRROR_LISTEN` · 删 `sidecar.example.yaml` 的幽灵 env 与幽灵探针 · 补两个漏写的 env · 上报两条规则措辞冲突（`MD-5` / `MD-19` ↔ `core/internal/contract/`） | 用户要求（删无用信息 · 确保客观 · 核模块定义）· 技能 `audit` |
