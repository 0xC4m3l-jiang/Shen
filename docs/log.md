# 变更日志

> **每轮开发追加一条**，最新在最上面。用途：事后复核「这轮到底改了什么、对应哪份文档、验证过没有」。
> 形状固定（`make trace` 会核对最新条目）：做了什么 · 改了哪些文件 · 对应文档 · 验证 · 证据 · 遗留。
> 写法见 [`.pi/skills/dev-loop/SKILL.md`](../.pi/skills/dev-loop/SKILL.md)；
> 完整变更包在 [`plans/`](plans/README.md)，本文件只留索引与结果。
>
> 读者视角优先：**先给结论与能验证的东西，再给细节**。

---

## 2026-09-19 · 引入 git 版本控制，把「提交」纳入每轮收尾

**做了什么**：① **初始化 git 仓库**（`git init -b main`）并打**基线提交**（220 个文件：全部文档 · 代码 · 契约 · 部署模板 ·
门禁工具 · `.pi/` 项目技能）；提交信息里写明了「此前无版本控制」，**不假装有历史**。
仓库级提交身份设为 `Shen Dev <dev@shen.local>`（不动全局配置；本机原本没有任何 git 身份）。
② **新增三个目标，把提交变成流程的一部分**：`make commit MSG="…"`（缺 MSG 失败、工作区干净时拒绝空提交、
正文固定写「验证：make gate 通过」） · `make clean-check`（工作区必须干净，否则列出文件与下一步命令） ·
`make done MSG="…"`（**一条命令跑完三段：门禁 → 提交 → 校验干净**；`make` 遇错即停，所以门禁不过就不会提交）。
③ **四份工作流文档同步这条纪律**：`AGENTS.md` §4（循环标题加「→ 提交」、表格新增第 ⑤ 段、
「三条不能破」→「四条不能破」、「每轮留下五样」→「六样」） · [`.pi/devloop.md`](../../.pi/devloop.md)（新增 `commit_cmd` / `commit_only`） ·
[`docs/kb/dev-workflow.md`](docs/kb/dev-workflow.md)（时序图新增第 ⑦ 步「提交」，并写明「没有提交就没有回退点」） ·
[`.pi/skills/dev-loop-project/SKILL.md`](.pi/skills/dev-loop-project/SKILL.md)（入口表与 DoD 各补一条）。
④ 顺手修正 `Makefile` 里四处**跨行 `if … then \` 守卫** —— shellcheck 报 `SC1089` 解析失败（真告警，会让门禁红），改为单行形式。

> 为什么做这件事：上一轮我把 `scripts/tracecheck` 的十余个函数误删了，**没有任何回退点**，
> 只能从 pi 会话记录里考古复原。有了提交，那只是一条 `git checkout` 的事。

**改了哪些文件**：`Makefile` · `AGENTS.md` · `.pi/devloop.md` · `docs/kb/dev-workflow.md` · `.pi/skills/dev-loop-project/SKILL.md`
（新增版本库 `.git/`；基线提交 `abf9a34`）

**对应文档**：[`docs/plans/2026-09-19-git-and-commit-workflow.md`](docs/plans/2026-09-19-git-and-commit-workflow.md)（含追溯矩阵与审视 4 条）

**验证**：`make gate` 通过；`make done MSG="…"` 跑通（门禁 → 提交 → 工作区干净）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 首次提交前核对忽略清单 | 密钥类文件与 `.bin/` 不入库 | ✅ 核对通过（`.gitignore` 生效） |
| 基线提交 | 有提交且工作区干净 | ✅ `abf9a34`；`git status --porcelain` 为空 |
| `make clean-check`（脏） | 失败并列文件 | ✅ `工作区不干净 —— 这一轮的改动还没提交：` + `M Makefile` |
| `make clean-check`（净） | 通过 | ✅ `工作区干净：本轮改动都已提交。` |
| `make commit` 缺 `MSG` | 失败且不提交 | ✅ `MSG 是必需的…` |
| `make commit` 无可提交内容 | 失败（拒绝空提交） | ✅ `没有可提交的改动（工作区已干净）。` |
| `make done` | 门禁 → 提交 → 干净 | ✅ 见变更包 §6 |
| `Makefile` 跨行 if | shellcheck 不再报 | ✅ `SC1089` 消失，门禁绿 |

**没做 / 遗留**：① **未配远端**（纯本地仓库）—— 机器损坏即历史丢失；你定托管后两行命令即可推上去；
② 提交信息只要求非空，**无格式规范**（长历史后检索不便）；③ **未加 pre-commit hook**（有人可绕过 `make done` 直接提交）；
④ 分支/评审策略未定（当前单分支 `main`）；⑤ 仓库 24 MB（含调研材料，目前可接受）。

---

**做了什么**：① 按用户裁定，**撒销我自行引入的 Python/TS 门禁工具链**（删 `pyproject.toml`、`requirements-dev.txt`、`.venv/`，
以及 `Makefile` 里的 `PY`/`RUFF`/`NPMBIN` 与 `py-tools`/`pyfmt-check`/`pystatic`/`pytest`/`ts-tools`/`ts-check` 六个目标，`lint` 链还原为 Go-only）。
② **修复误伤**：我在撒回时用「起止位置切片」还原代码，把 `scripts/tracecheck/main.go` 中间的十余个函数一起切掉了（375 行，原约 1000 行）。
仓库无版本控制，源文件不可从磁盘找回；我从会话记录取回 2026-09-18 的完整副本（27 个函数），
再按「考古表」逐项补齐后续改动：`docs/log.md` 改名（5 处硬编码）· **裸前缀**解析（`AR` 而非 `AR-1`）·
`backtickPaths` 的白名单过滤（裸目录名不当路径核）· `iface.go` 检查改用 `TC-1` + 只提示（原来错误地用了**真规则 ID** `ST-14`）·
补 `finding.Subject`/`Note` · `pathExists`/`isLineRef` · `report()` 的提示与错误分区。
③ **重建三项缺失检查**（按技能 `audit` §6 与 `.pi/devloop.md` §4 的文档化行为）：
`checkSkills`（`DEV-3`：AGENTS.md 点名的技能必须存在且带 `name`/`description`）·
`checkStaleMarkers`（`TC-2`：同一行既写「待建」又点名一个**已存在**的路径）·
`checkDanglingLinks`（`TC-3`：markdown 链接的悬空目标，含 `../` 与 `~/`）。
④ 新增 [`.gitignore`](../../.gitignore)（`ST-20` 此前**未满足**：仓库没有任何忽略清单）：密钥类配置、`.bin/`、构建产物。
⑤ 清理两处死代码（`splitID` / `prefixOf`，`parsePrefixes` 改写后不再使用）；
`Makefile` 的 `@test -x …` 全改为 `@if [ ! -x … ]; then … fi`（前者被 shellcheck 误判为 bats 注解，是真实告警）。

**改了哪些文件**：`Makefile` · `scripts/tracecheck/main.go` · `.gitignore`（新增）
（删除：`pyproject.toml` · `requirements-dev.txt` · `.venv/`）

**对应文档**：[`docs/plans/2026-09-19-revert-python-and-tracecheck-recovery.md`](docs/plans/2026-09-19-revert-python-and-tracecheck-recovery.md)（含追溯矩阵 · 考古表 · 审视 7 条）

**验证**：`make gate` 通过（含 `make trace` / `make archcheck` / `make leakcheck` / 单测 `-race`）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 恢复后 tracecheck 输出 | 与误伤前逐字一致 | ✅ 提示 1 条（`TC-1` edge/mirror）+ 豁免 3 条 + 追溯检查通过 + 六类检查行 |
| `TC-3` 能真报错 | 悬空链接命中 | ✅ 探针（一条指向不存在文件的 markdown 链接）→ 报 TC-3「悬空链接：目标不存在」 |
| `TC-2` 能真报错 | 过期状态标记命中 | ✅ 探针（已存在路径 + 「待建」写在同一行）→ 报 TC-2 过期状态标记 |
| 相对上级链接 | `../` 也核 | ✅ `docs/design` 下指向上级目录不存在文件的链接 → 报 TC-3 |
| `DEV-3` 能真报错 | 不存在的技能路径命中 | ✅ 首次运行报出了正则误抓出的不存在路径；修正则后无报 |
| `TC-3` 口径未被改弱 | 与误伤前一致（只查 markdown 链接） | ✅ 活文档里存在 `` `docs/spec/logs.md` `` 等**不存在**的反引号路径，而误伤前后**都是绿的** |
| Go 静态检查 | 无死代码 | ✅ 删 `splitID`/`prefixOf` 后 `staticcheck` 干净 |
| Python/TS 残留 | 零 | ✅ `grep` 关键词计数 0 |

**没做 / 遗留**：① **`TB-15` 对 Python / 前端的门禁仍未满足**（而 `language.md` §1 把 L4 定为 Python、控制台定为 TypeScript）——
要不要引入那两套门禁是**决策项，待你拍板**；② L4 四个模块与 `console` **仍未实现**（取决于①）；
③ `adapter-dns` / `honeypot-protocol` / `honeypot-shell` / `netpolicy` 四项**不依赖新语言**，可立即开工；
④ 恢复版的内部实现不可能与误伤前逐行相同，我用「输出逐字比对 + 三项检查反向探针」证明行为等价；
⑤ `TC-3` 是否收紧到反引号路径，另立一轮。

---

## 2026-09-19 · 2b 内容通路（响应改写规则经策略面下发）

**做了什么**：把**响应改写规则**从「每个适配器改 env」并成为**核心配置 → 策略面下发**。
① **核心配置新增 `injects` 段**（数据驱动，`ST-24`）：`kind`（可选，登记值 `developer_api` / `instruction_file` / `hidden_link` / `dataset`）/ `snippet`（必填）/ `marker`（可选，空 = `</body>`）；
空片段或未登记分类 → **启动失败**。
② **载荷新增可选 `inject_rules[]`**（[`docs/spec/policy-payload.md`](docs/spec/policy-payload.md)）；`schema_version` **保持 1**（旧适配器忽略未知字段，bump 反而会让未升级的适配器拉不到策略）。
③ **「缺省」与「空数组」必须可区分**（这是运营的**关闭手段**）：配置里**不写**该段 → 载荷**不出现**该字段，适配器继续用本地 env；写 `injects: []` → **显式下发空数组**，适配器关掉注入。适配器侧对应三态：缺省 → 保留本地；空数组 → 关掉；非空 → 远端接管（本地不再注入）。
④ **注入器按请求读取**：`injectingTransport` 改为持 Handler 并每请求取 `currentInjector()` —— 否则建后端时钉住的注入器会继续用旧规则，远端热变更不生效。
⑤ **非法规则 → 整份策略不应用**（禁止半应用）+ 回执 `applied=false` + 原因。
⑥ **共享类型**：新增 `contract.InjectRule` / `InjectKinds`（按上一轮修正后的 `MD-5`，进程内共享类型落 `contract/`）。
⑦ 文档同步：配置契约新增 §2.12、模块文档（`policy` / `adapter-proxy` / `edge-injection`）、`docs/design/structure.md` §1.6.4、
`docs/design/architecture.md` §10.2 缺口 13（按决策只更新现状：**注入规则的归属已定**，仍未定的是「幻境正文由谁产出」）、进度与 README 计数。

**改了哪些文件**：`core/internal/contract/deception.go` · `core/internal/policy/policy.go` · `core/internal/policy/server.go` ·
`core/internal/policy/server_test.go` · `edge/proxy/policy.go` · `edge/proxy/handler.go` · `edge/proxy/policy_test.go` ·
`edge/proxy/proxy_test.go`（旧注入 transport 用例改到新形态）· `deploy/config/config.example.yaml` ·
`docs/spec/policy-payload.md` · `docs/spec/config.md` · `docs/modules/policy.md` · `docs/modules/adapter-proxy.md` ·
`docs/modules/edge-injection.md` · `docs/design/structure.md` · `docs/design/architecture.md` · `docs/progress.md` ·
`docs/modules/README.md` · `README.md` · `edge/proxy/README.md` · `edge/proxy/config/front-proxy.example.env`

**对应文档**：[`docs/plans/2026-09-19-content-path-injects.md`](docs/plans/2026-09-19-content-path-injects.md)（含追溯矩阵与审视记录 7 条）·
[`docs/spec/policy-payload.md`](docs/spec/policy-payload.md) · [`docs/spec/config.md`](docs/spec/config.md) §2.12

**验证**：`make gate` 通过（含 `make trace` / `make leakcheck` / `make archcheck` / 单测 `-race`）；`make dev` 通过；
**端到端实测**（真进程 · 真 gRPC）：核心下发 `injects` → 适配器应用（它**没有**设本地 `SHEN_PROXY_INJECT`）→ 探针拿到
「幻境正文 + 注入片段」，正常会话拿到纯净业务响应。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 探针 UA（改道 + 策略面注入） | 幻境正文含策略面下发的片段 | ✅ `<html><body>MIRAGE-PAGE<!--INJECTED-BY-POLICY--></body></html>` |
| 正常会话（业务侧） | 零注入（`INT-8`） | ✅ `<html><body>REAL-BUSINESS</body></html>` |
| 适配器日志 | 拉到并应用策略 | ✅ `已应用策略 e2e-inject v1（1 个改道后端）` |
| `injects` 校验 | 空片段 / 未登记分类 → 启动失败 | ✅ `TestInjectsSectionValidation`（5 子例） |
| 缺省 vs 空数组 | 可区分 | ✅ `TestInjectsProvidedFlag` · `TestPullOmitsInjectRulesWhenUnconfigured` · `TestPullEmitsExplicitEmptyInjectRules` |
| 适配器三态 + 真改写 | 缺省保留 / 空关掉 / 非空接管 | ✅ `TestApplyEdgePolicyInjectSemantics` · `TestRemoteInjectRuleRewritesDivertedResponse` |
| 测试总数 | 189 → 196 | ✅ `policy` 22 · `edge/proxy` 39 |

**没做 / 遗留**：① **诱饵资产与预生成响应正文仍不下发** —— 假路径自答与「幻境正文由谁产出」属**架构级**取舍
（本轮只更新现状描述，未改架构）；② 注入规则的**多态**（按会话轮换）未定 —— 要做会碰 `INT-20`；
③ `Watch` 未实现（生效延迟上限 = 一个轮询间隔）；④ `policy_ack` 无保留期、明文 gRPC 仍是同主机假设。

---

**做了什么**：把上一轮审视上报的两条规则冲突改准（**经用户确认**）。
`MD-5` 原文说「跨模块共享的类型定义必须收敛到 `api/`」，而现实与设计文档都是：
**跨进程**契约走 `spec/` + `api/`（proto 生成物），**进程内**共享类型集中在 `core/internal/contract/`（8 个文件，只放类型无逻辑，
见 `docs/design/structure.md` §1.2 / §1.6.2）；`scripts/archcheck` 早已把它列入结构性目录白名单并注释「依据 structure.md §1.2」。
改法：`MD-5` 拆成两类落点；`MD-19` 例外清单加上 `core/internal/contract/`；规则表下方加带日期的措辞修正注记（写明原文与改动，可回退）。
**规则效力不变**：仍然禁止各自定义同名类型、禁止清单外目录承载业务逻辑。同步了 2 处会「复制粘贴」到新模块文档的引用
（`docs/modules/_template.md` 与 `docs/modules/policy.md`）；已按现实写作的三份（`docs/modules/decoy.md` / `docs/modules/director.md` / `docs/spec/config.md`）保持不动。
本轮**代码改动 0 行**。

**改了哪些文件**：`docs/design/modules.md` · `docs/modules/_template.md` · `docs/modules/policy.md`

**对应文档**：[`docs/plans/2026-09-19-rule-clarify-shared-types.md`](docs/plans/2026-09-19-rule-clarify-shared-types.md) ·
[`docs/plans/2026-09-19-docs-audit-and-module-review.md`](docs/plans/2026-09-19-docs-audit-and-module-review.md)（发现处）

**验证**：`make gate` 通过（含 `make trace` 的规则 ID 引用存在性检查）；`make archcheck` 通过。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 规则 ID 引用未悬空 | `make trace` 通过 | ✅ 追溯检查通过 |
| 模块清单 ↔ 目录一致 | `make archcheck` 通过 | ✅ 门禁通过 |
| 规则效力 | 两条禁止仍在 | ✅ `MD-5` / `MD-19` 两行均保留 |
| 旧表述残留 | 活性文档零残留 | ✅ 仅历史条目与注记中引用的原文 |

**没做 / 遗留**：① 未新开 ADR（不是取舍，是消除歧义）；② 未来若出现「proto 表达不了的跨进程共享类型」，
`api/` 与 `spec/` 的边界需再定义（无当前影响）；③ 本轮无新增测试（只改规则文本）。

---

**做了什么**：对**活文档与示例物料**做一轮审视（方法 = 技能 `audit`：机器优先，人只补机器核不了的）。
① **机器可核的四类全部跑过**：`make trace`（悬空引用 · 孤儿文档 · 过期状态标记 · 过期豁免）与 `make archcheck` 均无错误。
② **自查六类**（新写的一次性核验）：模块三方清单交叉比对（`docs/design/modules.md` §1.1 ⇄ `docs/progress.md` §1 ⇄ `docs/modules/README.md` §0）
→ **23 个有效模块三方一致**；模块文档九章齐全（缺章 0）；导出符号无孤儿（0 命中）；
**env 双向核对**；文档里 `make <目标>` 全部存在（幽灵命令 0）；`docs/design/` 无 `P-2` 含糊词。
③ **修掉 6 处过期事实**（`docs/design/structure.md` §1.5）：`contract` 文件数 6→8；
`core/internal/responder` / `core/internal/isolation` / `edge/injection` 标「⏳ 阶段 2b」但它们**已实现并含单测**；
`scripts/*` 行把已实现的 `scripts/check-leak` 写成「仍只有说明」；`edge/proxy` 测试数 30→37；补回已实现却无行的 `decoy` / `honeypot`。
④ **删幽灵内容**（四道门槛①：全仓检索 0 引用）：`SHEN_CORE_TLS`（无任何代码读它）与
`sidecar.example.yaml` 里的存活/就绪探针端点（代码里没有该端点，`grep healthz` 只命中测试夹具）；
并把 `ST-17` 未实现登记为 `docs/modules/adapter-proxy.md` §8 未决 12。
⑤ **补漏写**：`SHEN_MIRROR_LISTEN`（此前**全仓零文档**）进 `edge/mirror/README.md`；
`SHEN_PROXY_CACHE_MAX` 与 `SHEN_PROXY_INJECT`（代码读、模板没写）进 `edge/proxy/config/front-proxy.example.env`。
⑥ **修自相矛盾**：`edge/mirror/README.md` 目录表说「接收端 ✅ 含单测」、下方警示又说「尚未实现」——已统一为「已实现」。
⑦ **发现并上报两条规则措辞冲突**（**未擅改** `docs/design/`，`P-3` 要求停下报告）：
`MD-5` 说「跨模块共享类型必须收敛到 `api/`」，而进程内共享类型实际集中在 `core/internal/contract/`（8 个文件）；
`MD-19` 的例外清单也未包含该目录。两条均不阻塞开发，但按字面读会得出错误结论（详见变更包 §7）。

**改了哪些文件**：`docs/design/structure.md` · `docs/modules/adapter-proxy.md` · `edge/mirror/README.md` ·
`edge/proxy/config/sidecar.example.yaml` · `edge/proxy/config/front-proxy.example.env`

**对应文档**：[`docs/plans/2026-09-19-docs-audit-and-module-review.md`](docs/plans/2026-09-19-docs-audit-and-module-review.md)（含 18 条审视表与追溯矩阵）

**验证**：`make gate` 通过（含 `make trace` / `make archcheck` / `make leakcheck` / 单测 `-race`）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 模块三方清单 | 一致 | ✅ 23 个有效模块三方一致 |
| 模块文档九章（`MD-2`） | 无缺章 | ✅ 缺章数 0（23 份） |
| 导出符号孤儿 | 无 | ✅ 0 命中 |
| 幽灵 `make` 目标 | 无 | ✅ 0 命中 |
| 幽灵配置项 / 端点 | 无 | ✅ 删 `SHEN_CORE_TLS` 与两个探针端点 |
| env 双向核对 | 代码读的都有人写、文档写的都有人读 | ✅ 模板 14 → 16；mirror 的监听地址补进文档 |
| `docs/design/` 含糊表述（`P-2`） | 零违规 | ✅ 仅规则定义自身命中（规范允许） |
| 已实现模块的实际 import ↔ 文档依赖表 | 一致 | ✅ 均只依赖 `contract` + 文档已写的接口 |

**没做 / 遗留**：① **待你确认的两条规则冲突**（`MD-5` / `MD-19` ↔ `core/internal/contract/`）—— 改规则需你拍板，
本轮只上报、不动 `docs/design/`；② `ST-17` 探针未实现（已登记，落地前需先定「探针端点是否开在对外监听上」）；
③ `docs/spec/` 的 logs / metrics 两份字典与三个接入面目录（integrate / ops / analytics）仍待建；
④ 本轮**代码改动 0 行**，因此无新增测试。

---

**做了什么**：把 `api/policy/v1` 的**两端**都实现出来，闭合阶段 2b 的结构性断点（此前契约存在但无人实现，
核心算出的幻境后端池与白名单传不到边缘）：
① **核心侧服务端**（`core/internal/policy/server.go`）：`Pull` 把当前策略**投影**成边缘文档（改道后端表 + 白名单 CIDR，
按名排序 ⇒ 字节确定）、`checksum = sha256(payload)`；`Watch` **显式返回 `Unimplemented`**（不挂住调用方）；
`Ack` 把回执写入 `store.PolicyStore`（按 `(policy_id, version, adapter_id)` 幂等，缺标识拒收）。
② **适配器侧客户端**（`edge/proxy/policy.go`）：启动 + 每 60s 拉取（`SHEN_PROXY_POLICY_INTERVAL` 可配，0 = 不拉）；
先验校验和与 `schema_version` 再应用；合并语义 = **远端覆盖本地（后端表按名）+ 白名单并集**（护栏只增不减，`INT-25`）；
失败一律**沿用当前策略**（`NI-1`），拉不到/校验不过都回执 `applied=false` + 原因。
③ **契约**：`PolicyAck` 扩 `adapter_id` / `reason`（没有标识就无法做 `AR-13` 的版本对账）；
新增载荷契约 [`docs/spec/policy-payload.md`](docs/spec/policy-payload.md)（两处实现必须同时改，因 `ST-3` 禁止适配器 import 核心内部）。
④ **决策记录** [`docs/background/decisions/0018-policy-plane-pull-model.md`](docs/background/decisions/0018-policy-plane-pull-model.md)（含 4 条失效条件）—— 9 项推荐经用户全部批准。
⑤ 开发中修的两个**真缺陷**（测试捕到）：校验和不匹配时**每轮重复回执行**；应用成功日志把 `len(payload)` 字节数当「N 个改道后端」。
⑥ 安全硬化：核心明文 gRPC 监听加 fail-closed 闸门（非回环地址启动即失败，不提供开关）。
⑦ 新发现：**判定缓存的键不含 UA** ⇒ 同一 `(IP, 会话, 路径, 60s)` 共享一个决策，
实测可复现「探针先到 → 真实用户被改道」与「真实用户先到 → 探针漏改道」—— 登记为 `K-20`，**未擅自动 `ST-10`**。

**改了哪些文件**：`api/policy/v1/policy.proto`（+ 重新生成的 `api/policy/v1/policy.pb.go` / `api/policy/v1/policy_grpc.pb.go`）·
`core/internal/contract/policy.go` · `core/internal/store/iface.go` · `core/internal/store/memory.go` ·
`core/internal/policy/server.go`（新增）· `core/internal/policy/server_test.go`（新增）· `core/internal/policy/policy_test.go` ·
`core/cmd/core/main.go` · `edge/proxy/policy.go`（新增）· `edge/proxy/policy_test.go`（新增）· `edge/proxy/handler.go` ·
`edge/proxy/cmd/proxy/main.go` · `edge/proxy/README.md` · `edge/proxy/config/front-proxy.example.env` ·
`docs/spec/policy-payload.md`（新增）· `docs/background/decisions/0018-policy-plane-pull-model.md`（新增）·
`docs/background/decisions/README.md` · `docs/design/structure.md` · `docs/design/modules.md` ·
`docs/modules/policy.md` · `docs/modules/adapter-proxy.md` · `docs/progress.md` · `docs/kb/known-issues.md` ·
`README.md` · `docs/README.md`

**对应文档**：[`docs/plans/2026-09-19-policy-plane.md`](docs/plans/2026-09-19-policy-plane.md)（含追溯矩阵与审视记录 10 条）·
[`docs/spec/policy-payload.md`](docs/spec/policy-payload.md) · [`docs/background/decisions/0018-policy-plane-pull-model.md`](docs/background/decisions/0018-policy-plane-pull-model.md)

**验证**：`make gate` 通过（含 `make trace` / `make leakcheck` / `make archcheck` / 单测 `-race`）；`make dev` 通过；
**端到端实测**（真进程 · 真 gRPC · 真改道）通过。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 适配器拉取并应用策略 | 日志显示已应用版本与后端数 | ✅ `proxy: 已应用策略 e2e v1（1 个改道后端）` |
| 探针 UA | 改道到幻境 | ✅ `curl -A "HeadlessChrome/120"` → `MIRAGE-BODY` |
| 正常会话（独立 Cookie） | 到真实业务，不受影响 | ✅ → `REAL-BUSINESS` |
| 校验和不匹配 | 拒绝应用 + 回执 `applied=false` + 原因，**只回执一次** | ✅ `TestPolicyChecksumMismatchIsRejected` |
| 策略面不可达 | 沿用当前策略、本地后端仍可用、不产生回执 | ✅ `TestPullFailureKeepsCurrentPolicy` |
| 回执对账 | 幂等（每适配器一条）+ 能回答「谁应用了哪个版本」 | ✅ `TestAckRecordsAndIsIdempotent` |
| 测试总数 | 177 → 189 | ✅ `ok shen/edge/proxy`（37）· `ok shen/core/internal/policy`（17） |
| 端到端契约纪律 | 适配器不得 import 核心内部 | ✅ `make archcheck`（新测试一度违反而自查修掉） |

**没做 / 遗留**：① **注入片段 / 诱饵资产 / 预生成内容仍不经策略面下发**（核心侧尚无它们的来源与归属）——
阶段 2b 的**内容类**能力在请求路径上还看不到；② `Watch` 未实现（生效延迟上限 = 一个轮询间隔）；
③ 回执只落账不告警（「哪个适配器停在旧版本」要人查）；④ `K-20` 的缓存键问题须单独一轮讨论 `ST-10`；
⑤ `policy_ack` 无保留期配置（接真库时按 `NI-13` 定）；⑥ 明文 gRPC 仍是同主机假设（已加非回环启动闸门，mTLS 未做）。

---

**做了什么**：① **新增根 `README.md`** —— 仓库此前没有任何入口文档；新 README 回答四件事：
项目是什么（一页架构图 + 三条不可妥协）· 现在跑到什么程度（阶段表 + 23 模块 / 14 包 / 177 个测试函数）·
五分钟怎么跑起来（`make check` / `make gate` / `make dev` / `make run`）· **效果长什么样**（贴 `make dev` 五步真实输出、
真二进制 + 真 TLS 的 `curl` 结果、以及七个行为场景：原样透传 / 补 `X-Forwarded-*` / 只改引流侧 / 核心挂了仍 200 / 白名单不调核心 / 影子模式 / 判定缓存），
再加目录结构 · 文档地图 · 门禁说明 · 使用边界（`SB-7` 仅授权环境、`SB-1`/`SB-2` 不做控制面）· 已知限制。
② **修正 docs/ 里的过期事实**（逐条用命令核过）：
`docs/spec/config.md` §2.0 的 `whitelist` / `decoys` / `honeypots` 三行写「未接入」，实证各已有 1 处消费（`loader.Whitelist` / `Decoys` / `Honeypots`）；
§2.8 / §2.9 称「阶段 2b 才进示例配置」，实证 `deploy/config/config.example.yaml` 已有该两段；
`docs/progress.md` 与 `docs/modules/README.md` 的「21 个模块」→ **23 个有效模块（共 24 行）**；
`edge/proxy` 测试计数 5 处 29 → **30**；`docs/design/modules.md` 头部历史注记补「当时」二字澄清（**规则与 ID 未动**）。
③ **补门禁与工具清单**：`docs/README.md` 新增 §1.1（`scripts/` 七项工具 + 各自依据的规则 + 两个未实现工具的显式标注），
并在开头加「想看项目全貌 → 根 README」的互指。
④ 顺手把「策略面未实现」改成精确表述 —— `api/policy/v1` **只有契约**（proto + 生成物），**服务端与消费方都未实现**。

**改了哪些文件**：`README.md`（新增，根目录）· `docs/README.md` · `docs/spec/config.md` · `docs/progress.md` ·
`docs/modules/README.md` · `docs/modules/adapter-proxy.md` · `docs/design/structure.md` · `docs/design/modules.md`

**对应文档**：[`docs/plans/2026-09-19-docs-accuracy-and-readme.md`](docs/plans/2026-09-19-docs-accuracy-and-readme.md)（含追溯矩阵与审视记录）

**验证**：`make gate` 通过（含 `make trace` 的悬空链接检查、`make leakcheck`、单测 `-race`）；`make dev` 通过。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| `whitelist` / `decoys` / `honeypots` 是否被消费 | 文档与代码一致 | ✅ 各 1 处调用（`core/cmd/core/main.go` 与 `director`） |
| 示例配置是否已有 `decoys` / `honeypots` | 一致 | ✅ `deploy/config/config.example.yaml` 第 71 / 86 行 |
| 有效模块数 | 一致 | ✅ `docs/design/modules.md` §1.1 共 24 行、23 个有效 |
| `edge/proxy` 测试数 | 一致 | ✅ 30（`grep -c '^func Test' edge/proxy/*_test.go`） |
| 全仓测试函数数 | 与 README 一致 | ✅ 177 |
| 根 README 里的命令能跑 | 全部可跑 | ✅ `make check` / `gate` / `dev` / `help` 均实跑 |

**没做 / 遗留**：① 策略面仍只有契约（2b 能力在边缘看不到）—— README §2/§9 已显式写明，**没有**写成现状；
② 待建的目录（integrate / ops / analytics 三个，见 docs/README.md §3）与 spec 的两份字典（logs / metrics）仍缺；
③ 计数类事实（模块数 / 测试数）会继续漂移，`make trace` **不核计数**，靠每轮同步（`docs/progress.md` 头部已有此义务）；
④ `scripts/doctor` / `scripts/sentinel` 仍是占位；⑤ `AR-29` 换底座后仍未实测（`E3`）。

---

**做了什么**：把 `OH-3`（「工程**必须**提供自动检查，扫描响应面模块的字符串字面量」）从占位变成真东西：
新增 `scripts/check-leak`（标准库 + go/ast，**零新依赖**）—— 扫 `OH-1` 禁用清单（大小写不敏感）、
查响应头与 Cookie 名里的决策类词（`OH-5`），豁免分**三类**且都不是整目录豁免（`OH-4`）：
规则级（内部日志 / 错误构造 / struct tag，写在 `scripts/check-leak/main.go` 包注释并附 `OH-2` 依据）、
文件级（`core/internal/responder/blacklist.go` 自己声明 check-leak:filter 标记）、逐条（`scripts/check-leak/allow.txt`，
精确到**文件 + 字面量**，理由必填，**过期即报**）。扫描范围从 `docs/design/modules.md` §1.1 **解析**，
模块改名就**直接失败**（拒绝少扫）。同时修掉它查出的真泄漏：`core/internal/decoy/decoy.go` 的投放片段
含「…蜜饵」与 `decoy_accounts` 表名（片段会被投放进客户环境，对手可能读到）、
`edge/proxy/cmd/proxy/main.go` 启动日志的「引流」措辞（与 `docs/design/terminology.md` §4.1 的三值命名对齐）。
另登记一致性核查发现的既有缺口（`NI-12` 的 V 系测试、`AR-26` 的两条周期断言、`NI-7` / `ST-17` 的部署物料、
`scripts/doctor` / `scripts/sentinel` 仍为占位）—— 见变更包 §7，本轮不动它们。

**改了哪些文件**：`scripts/check-leak/main.go`（新增）· `scripts/check-leak/allow.txt`（新增）·
`scripts/check-leak/README.md`（重写：从「尚未实现」改为已实现，含范围与局限）· `Makefile`（新增 `leakcheck`，接进 `lint` / `check` / `gate`）·
`core/internal/responder/blacklist.go`（文件级过滤器声明）· `core/internal/decoy/decoy.go`（修真实泄漏）·
`edge/proxy/cmd/proxy/main.go`（日志措辞）· `.pi/devloop.md`（适配面补 `leak_cmd`）·
`docs/kb/dev-workflow.md`（§3 门禁链路补 `leakcheck`）

**对应文档**：[`docs/plans/2026-09-19-oh3-leak-check.md`](docs/plans/2026-09-19-oh3-leak-check.md)（含追溯矩阵与审视记录）·
[`docs/design/constraints.md`](docs/design/constraints.md) 的 `OH-1`…`OH-5` · `scripts/check-leak/README.md`

**验证**：`make gate` 通过（含 `make trace`、`make leakcheck`、`make licensecheck`、单测 `-race`）。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 响应体字面量写 honeypot | 报 `OH-1` | ✅ 探针文件第 4 行被命中 |
| 响应头名 `X-Decoy-Marker` | 报 `OH-1` | ✅ 探针第 6 行被命中 |
| 响应头名 `X-Agent-Capture-Decision` | 报 `OH-5` | ✅ 探针第 7 行被命中 |
| 响应头名 `X-Risk-Score` | 报 `OH-5` | ✅ 探针第 8 行被命中 |
| 撤掉探针 | 回到通过 | ✅ `泄漏检查通过。` |
| 白名单表（AR-22） | 文件级声明后不报错、并打印理由 | ✅ `已声明过滤器文件：…blacklist.go` |
| 逐条例外 | 2 条命中且打印理由 | ✅ `已登记豁免 2 条` |
| 豁免过期 | 报警 | ✅ 修 `used` 标记时实测 |
| 模块改名 | 直接失败不静默少扫 | ✅ 实测报「找不到这些响应面模块（改名了？）」 |

**没做 / 遗留**：① 项目名 / 二进制名 / 服务名未定 ⇒ `OH-1` 那三类无从扫描（待 ADR-0004 定名）；
② 拼接串与模板变量扫不到（检查只覆盖字面量，`OH-2` 的最终判据仍是人工）；
③ `scripts/check-leak` 自身无单测（工具类脚本无测试约定，本轮用**负向探针**作证据）；
④ `OH-5` 目前**没有真实数据** —— 全仓（非测试）当前不设任何自定义响应头，该检测是给后续代码用的护栏；
⑤ 既有缺口 `NI-12` / `AR-26` / `NI-7` / `ST-17` / `scripts/doctor` / `scripts/sentinel` 已登记未做。

---

**做了什么**：把 `edge/proxy/`（模块 `adapter-proxy`，③前置 + ④边车）的**转发层**从 Go 标准库
net/http/httputil.ReverseProxy 换成**内嵌 Caddy**（github.com/caddyserver/caddy/v2，Apache-2.0）；
判定胶水（白名单 → `decision_id` → 判定缓存 → gRPC 判定 → 三值路由 → 异步上报）保持自研 Go，
改为 Caddy 中间件 `http.handlers.shen_proxy`。**TLS 由本进程终结**
（`SHEN_PROXY_TLS_MODE=off|manual|acme`），未决项「TLS 终结归谁」结案
（[ADR-0017](docs/background/decisions/0017-caddy-l1-base.md)）。
另做一条**部署面硬化**：关闭 Caddy 的**配置自动保存**（`Admin.Config.Persist=false`）——
默认它会把整份配置写进 `$XDG_DATA_HOME` 下的 caddy/autosave.json，请求路径上的边车/前置不该有这种写入。
模块身份（§1.1 第 11 行）与三个对外接口（`JudgeClient` / `TelemetryClient` / `Injector`）**不变**；
范围仅形态 ③④，形态 ①（mirror）与 ②（DNS）未动。

**改了哪些文件**：`edge/proxy/handler.go` · `edge/proxy/glue.go` · `edge/proxy/embed.go` · `edge/proxy/embed_test.go`（新增）·
`edge/proxy/proxy.go`（**删除**：旧转发层被 Caddy 取代）· `edge/proxy/iface.go` · `edge/proxy/proxy_test.go` ·
`edge/proxy/cmd/proxy/main.go` · `edge/proxy/README.md` · `edge/proxy/config/front-proxy.example.env` ·
`go.mod` · `go.sum` · `docs/spec/dependencies.md`（重新生成）·
`docs/background/decisions/0017-caddy-l1-base.md`（新增）· `docs/background/decisions/0008-edge-language-go.md`（标注底座部分被取代）·
`docs/background/decisions/README.md` · `docs/design/language.md` · `docs/design/architecture.md` · `docs/design/integration.md` ·
`docs/design/modules.md` · `docs/design/structure.md` ·
`docs/modules/adapter-proxy.md` · `docs/modules/README.md` · `docs/progress.md` ·
`docs/background/notes/pending-experiments.md`（新增 `E3`）

**对应文档**：[`docs/plans/2026-09-19-caddy-l1-base.md`](docs/plans/2026-09-19-caddy-l1-base.md)（变更包，含追溯矩阵与审视记录）·
[`docs/modules/adapter-proxy.md`](docs/modules/adapter-proxy.md) §1/§3/§8 ·
[`docs/design/architecture.md`](docs/design/architecture.md) §10.2 缺口 14（TLS 结案）

**验证**：`make gate` 通过（含 `make trace` 与 `make licensecheck` —— Caddy 全链许可为
Apache-2.0 / MIT / BSD-3-Clause / CC0，无 AGPL/SSPL/BSL）；`make dev` 通过。

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 内嵌 Caddy 端到端（真 Caddy + 真 gRPC + 自签 TLS） | 三值路由 + 引流侧注入 + 引流失败回落 + TLS 握手 + block 403 | ✅ `TestEmbeddedCaddyEndToEnd` PASS |
| `tls_mode` 三取值 + 非法值 | 缺字段/非法值启动前报错 | ✅ `TestTLSConfigValidate` PASS（7 例） |
| 测试总数 | 19 → 29（`-race`） | ✅ `ok shen/edge/proxy` |
| **真二进制 + 真 TLS + 断核心** | TLS 握手成功；核心不可达时**放行到业务**（`NI-3`）；同端口明文被拒 | ✅ `curl -sk https://127.0.0.1:18443/` → `code=200 http=2`，体为业务页 |
| 配置自动保存 | 不得写 autosave.json | ✅ 修复后重启：日志无 `autosaved config`，文件不存在 |
| 许可审计 | 无传染性许可 | ✅ `许可审计通过：没有传染性或限制性许可` |

**没做 / 遗留**：① `tls_mode=acme` 只做了装配与校验，**未在真实公网域名验证签发/续期**；
② `AR-29`（P99 ≤ 5 ms）**换底座后未重测** —— 已登记为实验 `E3`（[`pending-experiments.md`](background/notes/pending-experiments.md)），
未测前不得声称形态 ③④ 可上线；③ Caddy admin API 已禁用（缩攻击面）⇒ 本进程**没有**运行时改配置的入口，
后端表热更新留待策略面 `S4`；④ 二进制体积控制未测。

---

## 2026-09-18 · 产品定位与接入边界讨论（CLB / Caddy / 识别判定）

**做了什么**：记录一轮产研讨论的结论与未决项，不改代码、不改规则：
① **定位确认** —— 产品 = 「服务端前置的欺骗引擎」：反向代理前置（L1）→ 判定核心（judge/director）→ 路由真实业务（内网/公网）或蜜罐层；蜜罐层启动与管理各类蜜罐。与 [`docs/README.md`](docs/README.md) §0 及 [`docs/design/integration.md`](docs/design/integration.md) §1 一致，**无需改动**；
② **CLB / L0 不开发** —— 复用云 / 客户现成组件（`AR-3` 已定），**无需改动**；
③ **识别/判定逻辑推迟** —— 用户明确「能力判断核心，暂不确定，后续详细设计」，在讨论稿 D8 登记；
④ **反向代理底座（Caddy）未拍板** —— 讨论提出「Caddy 核心 + 只写 Go 插件」替代自研 `adapter-proxy`，登记为未决项 D13。

**改了哪些文件**：`docs/background/notes/implementation-discussion.md` · `docs/log.md`

**对应文档**：[`docs/background/notes/implementation-discussion.md`](docs/background/notes/implementation-discussion.md) §6.1（D8 更新 · 新增 D13）

**验证**：`make gate` 通过（含 `make trace`）

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 讨论稿未决项 | D8 注明用户推迟、D13 新增 Caddy 议题 | ✅ §6.1 表已更新 |

**没做 / 遗留**：① 识别/判定的详细设计（规则 → ML、首跳定终局 D0 等）留待后续专门讨论；② Caddy vs Go 自研未拍板（D13 待定）。

---

## 2026-09-18 · 代码整理 + 调用链核实 + 文档站完善

**做了什么**：① **整理装配代码** —— `core/cmd/core/main.go` 提取 `assembleDeception` / `deception` / `assertConsistency`，`run()` 从 ~200 行降到 **125 行**，行为不变；
② **核实模块真实调用链**（`go list` 实测依赖 + 反查每个接口的消费者）：**发现 `decoy` / `responder` / `honeypot` 已装配但不在调用链上**（`Resolve`/`Match`/`Respond` 的调用者只有测试与启动自检），根因是**策略面 S4（`api/policy/v1`）未实现**；
③ **完善文档站**：`docs/README.md` 更新现状与计数（21→23 模块）+ 新增调用链导航 + 上手顺序；`docs/design/structure.md` §1.6 **重写**为实测三进程地图（原缺 5 个包、误称「两个二进制」、误称 isolation 无人调用）；`docs/modules/README.md` 新增 **§0.4 运行时调用链**；`decoy`/`responder`/`honeypot` 模块文档各登记一行「未接到请求路径」。

**改了哪些文件**：`core/cmd/core/main.go` · `docs/README.md` · `docs/design/structure.md` · `docs/modules/README.md` · `docs/modules/decoy.md` · `docs/modules/responder.md` · `docs/modules/honeypot.md` · `docs/plans/2026-09-18-docs-wiring-cleanup.md` · `docs/log.md`

**对应文档**：[`docs/plans/2026-09-18-docs-wiring-cleanup.md`](docs/plans/2026-09-18-docs-wiring-cleanup.md)（含调用链核实与设计评估）· [`docs/modules/README.md`](docs/modules/README.md) §0.4 · [`docs/design/structure.md`](docs/design/structure.md) §1.6

**验证**：`make gate` 通过（含 `-race`）· `make dev` 通过 · 启动日志确认装配与 `AR-30` 自检仍生效

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 行为不变 | 15 个包测试与整理前一致 | ✅ `go test -count=1 ./...` 15 个 ok |
| 调用链核实 | 区分「在链上」与「仅装配」 | ✅ `go list` + 反查消费者；见变更包 §2 |
| 结构文档准确 | 三进程 + 实测依赖 + 接口清单 | ✅ `structure.md` §1.6.1–§1.6.4 已重写 |
| 文档站入口 | 计数与状态不再过期 | ✅ `docs/README.md`（21→23 模块 · 断点提示） |
| 启动日志 | 装配摘要与自检仍生效 | ✅ 「诱饵面：0 个资产启用 / 幻境后端池：0 个登记」 |

**没做 / 遗留**：⭐ **策略面（`api/policy/v1`）未实现** —— 当前唯一结构性断点，补齐它才能让 `decoy`/`responder`/`honeypot` 到达边缘、消除后端池的两份事实源；本轮只核实与登记，未实现（属新功能，需单独一轮）。

---

## 2026-09-18 · 模块代码整理（4 处简化，无行为变更）

**做了什么**：整理本轮开发的模块代码，消除冗余结构与重复逻辑，为后续「按架构重组模块」做准备：
① `honeypot` —— 删掉 `knownTypes()`（每次调用重建切片并排序）与 `typeOrder()`（线性查设计清单序号）两个函数，`known` 改为**按给定顺序**的有序切片 + `slices.Contains` 做成员判断（-15 行）；
② `core/internal/store/memory.go` —— 三份「超限则清理过期」循环（一份提成方法、两份内联）收敛为一个泛型 `sweepExpiredIfFull`；
③ `responder` —— `seedOf` 由三次 `Write` 改为一次拼好再写；
④ `edge/proxy` —— `injectResponse` 里三条「不注入」判据抽成 `injectable(resp)`（它们本是同一个概念）。
**刻意保留**：`director.Config.Now` 与 `Engine.now` 当前无读取点，但注释写明是「仅为将来的可观测留口」的**有意预留**，且删它属于改动导出类型形状 —— 按「不改对外契约」保留并登记。

**改了哪些文件**：`core/internal/honeypot/honeypot.go` · `core/internal/store/memory.go` · `core/internal/responder/responder.go` · `edge/proxy/proxy.go` · `docs/plans/2026-09-18-module-cleanup.md` · `docs/log.md`

**对应文档**：[`docs/plans/2026-09-18-module-cleanup.md`](docs/plans/2026-09-18-module-cleanup.md) · [`docs/modules/honeypot.md`](docs/modules/honeypot.md) · [`docs/modules/responder.md`](docs/modules/responder.md)

**验证**：`make gate` 通过（含 `-race`）· `make dev` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 行为不变 | 15 个包全部测试通过（与整理前一致） | ✅ `go test -count=1 ./...` |
| 蜜罐类型顺序 | 仍按登记顺序输出 | ✅ `honeypot_test.go` TestTypesDedupAndOrder |
| 注入判据 | 压缩/非 HTML/超大仍不注入 | ✅ `proxy_test.go` 24+ 例 |
| 一致性 | 同会话语资源仍逐字节一致（AR-30） | ✅ `responder_test.go` 16 例 |
| 代码量 | honeypot 170 → 155 行；清理循环 3 份 → 1 份 | ✅ `wc -l` |

**没做 / 遗留**：`director.Config.Now` 与 `Engine.now` 无读取点（有意预留）—— 若确认不需要，删 5 行即可，但需先确认「将来可观测留口」是否仍要保留；本轮为整理轮，**不改任何行为与对外契约**。

---

## 2026-09-18 · 全仓代码审查与加固（8 处修复 + 开源替换与稳定性评估）

**做了什么**：对全仓非测试代码（6834 行）做整体审查（静态分析器已零告警，故全部发现来自人工核对与定向审查），**修复 8 处缺陷**：
① 引流侧上游**无超时** → 蜜罐挂死会拖住客户端（违 `NI-1`）→ 加响应头超时 + 连接层超时（超时回落业务）；
② **压缩响应被注入** → 损坏 gzip → 加 `Content-Encoding` 检查；
③ `trackingWriter` **未实现 `Hijacker`** → WebSocket 升级一律 502 → 透传 `Hijack`；
④ 注入**读失败返回 nil** → 截断响应 → 改为返回错误（回落业务）；
⑤ **`INT-25` 核心侧白名单未消费** → `director` 实现前置白名单（命中即放行、连判定都不做）；
⑥ `policy` **报错指向错误字段** → `ratio()` 带字段路径；
⑦ **内存存储无界增长**（TTL 条目只在被读到才删）→ 超限时清理已过期项；
⑧ **mirror 调核心无超时** → 加 3s 上限。
另输出**开源替换评估**（12 项组件，结论：该复用的都已复用，自研的正是设计指定的智能决策层）与**架构稳定性评估**（6 强项 / 6 风险点）。

**改了哪些文件**：`edge/proxy/proxy.go` · `edge/proxy/iface.go` · `edge/proxy/proxy_test.go` · `edge/proxy/cmd/proxy/main.go` · `edge/mirror/receiver.go` · `core/internal/contract/policy.go` · `core/internal/policy/policy.go` · `core/internal/policy/policy_test.go` · `core/internal/director/director.go` · `core/internal/director/director_test.go` · `core/internal/store/memory.go` · `core/cmd/core/main.go` · `docs/plans/2026-09-18-code-review.md` · `docs/log.md`

**对应文档**：[`docs/plans/2026-09-18-code-review.md`](docs/plans/2026-09-18-code-review.md)（审查报告）· [`docs/design/constraints.md`](docs/design/constraints.md)（`NI-1`）· [`docs/design/modules.md`](docs/design/modules.md)（`INT-25` / `MD-25`）

**验证**：`make gate` 通过（含 `-race`）· `make dev` 通过 · `go vet` / `staticcheck` / `errcheck` 零告警

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 蜜罐挂死 | 超时后回落业务（NI-1） | ✅ `TestMirageTimeoutFallsBackToOrigin` |
| 压缩响应 | 不注入（否则损坏编码） | ✅ `TestCompressedResponseIsNotInjected` |
| WebSocket 升级 | 透传 101（裸 TCP 验证） | ✅ `TestProtocolUpgradePassesThroughMirage` |
| 白名单前置 | 命中即放行且不做判定 | ✅ `director` 5 例 |
| 配错 guard | 报错指向真实字段 | ✅ `guard 缺键须报对字段` |
| 依赖解耦 | 新模块仅依赖 `contract` | ✅ `go list -f '{{.Imports}}'` |

**没做 / 遗留**（已入报告 §5，按优先级）：① `ADR-0012` 会话级判别未实现（判别缺一半，**P0**）② 真实存储未接（内存是占位，**P0**）③ `responder` 无对外服务面 ④ `telemetry` 的接收缓冲/落库补偿未实现 ⑤ `TrustXFF=false` 时应剥离入站 XFF ⑥ 差异哨兵 `sentinel` 未实现 ⑦ 熔断器是否换 sony/gobreaker（可换非必换）。

---

## 2026-09-18 · 欺骗引擎模块实现（isolation / honeypot / decoy / responder / edge-injection）

**做了什么**：把设计变成代码 —— 实现 5 个欺骗引擎模块（各含独立单测），并用**接口**把它们解耦接起来：
① `isolation`（隔离记录 / TTL / 查询短路，`NI-10` fail-open）；② `honeypot`（幻境后端池：类型注册 + config 开关 + 健康 + 解析，**不实现具体蜜罐** ADR-0011）；
③ `decoy`（诱饵面：最长前缀匹配 + 多态 + 投放片段，ADR-0010/0016）；④ `responder`（欺骗响应：确定性模板 + 预生成优先，`AR-30` 一致性不变量）；
⑤ `edge-injection`（L1 注入：非 HTML 原样返回，纯变换）；⑥ 补上 `MD-25`（诱饵面 observe-only）的**实现** —— 诱饵前缀集在决策层豁免 block，前缀集由装配层从诱饵资产汇总（单一事实源）。另：`store` 新增 `DecoyStore`/`ContentStore`；`config` 新增 `decoys`/`honeypots` 两段（数据驱动，`ST-24`）；`control` 服务面接隔离短路；`main.go` 装配 + 启动期 `AR-30` 自检；新增**端到端集成测试**。⑦ 把 `edge-injection` **接进适配器**（`ST-5`：被适配器引用，不独立部署）：引流侧的 HTML 响应注入诱饵，业务侧一律不改写（`INT-8`）；⑧ 实现 `AR-22` / `AR-23`（内容黑名单 + 长度上限）：生成内容过三类校验，不合格回落安全模板。

**改了哪些文件**：`core/internal/contract/deception.go` · `core/internal/store/iface.go` · `core/internal/store/memory.go` · `core/internal/isolation/` · `core/internal/honeypot/` · `core/internal/decoy/` · `core/internal/responder/` · `edge/injection/` · `core/internal/control/service.go` · `core/internal/director/director.go` · `core/internal/policy/policy.go` · `core/cmd/core/main.go` · `core/cmd/core/integration_test.go` · `deploy/config/config.example.yaml` · `docs/modules/isolation.md` · `docs/modules/honeypot.md` · `docs/modules/decoy.md` · `docs/modules/responder.md` · `docs/modules/edge-injection.md` · `docs/progress.md` · `docs/plans/2026-09-18-deception-modules-impl.md` · `docs/log.md`

**对应文档**：[`docs/modules/decoy.md`](docs/modules/decoy.md) · [`docs/modules/honeypot.md`](docs/modules/honeypot.md) · [`docs/modules/responder.md`](docs/modules/responder.md) · [`docs/modules/isolation.md`](docs/modules/isolation.md) · [`docs/modules/edge-injection.md`](docs/modules/edge-injection.md) · [`docs/plans/2026-09-18-deception-modules-impl.md`](docs/plans/2026-09-18-deception-modules-impl.md)

**验证**：`make gate` 通过（含 `-race`）· `make dev` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 隔离命中 | 不再调决策层，客户端不可见 | ✅ `TestIsolationShortCircuitsWithoutCallingCore` |
| 隔离存储故障 | fail-open（回退调核心，NI-10） | ✅ `TestIsolationStoreFailureFailsOpen` |
| 后端池解析 | 未启用 / 不健康不得解析（NI-5） | ✅ `honeypot_test.go` 14 例 |
| 响应一致性 | 同 `(会话,资源)` 逐字节一致（AR-30） | ✅ `responder_test.go` 10 例 |
| 端到端链路 | session→judge→director→honeypot→decoy→responder | ✅ `TestDeceptionChainEndToEnd` |
| 诱饵面 observe-only | 满分 + block 开启仍不 block（MD-25） | ✅ `TestConfiguredDecoyPathIsNeverBlocked` + director 4 例 |
| 适配器注入 | 只改引流侧 HTML；业务侧一律不改写（INT-8 / ST-5） | ✅ `edge/proxy` 4 例 |
| 内容黑名单 | 泄露 / 自曝 / 超长三类均被拒；模板自身合规（AR-22 / AR-23） | ✅ `responder` 6 例 |
| 误调度 | 正常用户放行 | ✅ `TestLowScoreAgentStaysOnOrigin` |

**没做 / 遗留**：真实存储后端（接口已就位）· `responder` 的对外服务面（生成能力已就绪，尚无 gRPC/HTTP 暴露给边缘）· `decoy` 管理面（console）· 蜜罐健康探针 · spec 下 decoy / honeypot 的字段契约 · 诱饵多态的识破信号触发（`chain`/`strategy`，阶段 3）。

---

## 2026-09-18 · 进度表加「模块介绍」列

**做了什么**：在 [`docs/progress.md`](docs/progress.md) §1「全部模块」表加一列「介绍（做什么）」，逐模块一句话说明职责；同时修正表后过期的「21 个有效模块」为 23。

**改了哪些文件**：`docs/progress.md` · `docs/log.md`

**对应文档**：[`docs/progress.md`](docs/progress.md) §1 · [`docs/design/modules.md`](docs/design/modules.md) §1.1（权威职责）

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 24 行均有介绍 | 逐行一句话 | ✅ |
| 规则 ID 引用 | 无 `D-3` | ✅ `make trace` 通过 |
| 计数正确 | 24 行 / 23 有效 | ✅ |

**没做 / 遗留**：介绍是摘要；职责权威仍在 `docs/design/modules.md` §1.1。

---

## 2026-09-18 · 模块总览：结构图 + 关系图 + 23 模块解释

**做了什么**：在模块索引顶部加「模块总览」—— §0.1 结构图（L0–L4 分层）· §0.2 关系图（数据流 + 依赖）· §0.3 全部 23 个模块的一句话解释，供人工分析设计是否符合想法。同时修正标题里过期的「21 个模块」。

**改了哪些文件**：`docs/modules/README.md` · `docs/plans/2026-09-18-module-overview.md` · `docs/log.md`

**对应文档**：[`docs/modules/README.md`](docs/modules/README.md) §0 · [`docs/design/modules.md`](docs/design/modules.md) §1.1（权威）· [`docs/design/structure.md`](docs/design/structure.md) §1.6

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 23 模块全覆盖 | 每个一句话解释 | ✅ §0.3 三张表 |
| 结构与关系图 | 分层 + 数据流可追 | ✅ §0.1 / §0.2 |
| 链接不悬空 | 无 `TC-3` | ✅ `make trace` 通过 |

**没做 / 遗留**：§0 是总览；职责的**权威**仍在 `docs/design/modules.md` §1.1，修改时以那里为准（避免两处漂移）。

---

## 2026-09-18 · 诱饵多态机制 + 补完模块文档

**做了什么**：① 新增 [ADR-0016](docs/background/decisions/0016-decoy-polymorphism.md)「诱饵多态与再生成」—— **会话间轮换 + 识破信号触发**，并给出与 `AR-30` 的**划界**（多态在会话边界选择、会话内冻结，两条规则同时成立）；② 补完剩余 **8 个模块文档**（`edge-injection` · `honeypot-protocol` · `honeypot-shell` · `netpolicy` · `intent` · `chain` · `strategy` · `console`），使清单 §1.1 的 24 行全部有九章文档（`MD-2` / `MD-17`）。

**改了哪些文件**：`docs/background/decisions/0016-decoy-polymorphism.md` · `docs/background/decisions/README.md` · `docs/modules/decoy.md` · `docs/modules/edge-injection.md` · `docs/modules/honeypot-protocol.md` · `docs/modules/honeypot-shell.md` · `docs/modules/netpolicy.md` · `docs/modules/intent.md` · `docs/modules/chain.md` · `docs/modules/strategy.md` · `docs/modules/console.md` · `docs/progress.md` · `docs/plans/2026-09-18-polymorphism-and-module-docs.md` · `docs/log.md`

**对应文档**：[`docs/background/decisions/0016-decoy-polymorphism.md`](docs/background/decisions/0016-decoy-polymorphism.md) · [`docs/modules/decoy.md`](docs/modules/decoy.md) · [`docs/modules/chain.md`](docs/modules/chain.md) · [`docs/modules/strategy.md`](docs/modules/strategy.md)

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 多态与一致性共存 | 作用域划界（会话内/会话间） | ✅ ADR-0016 |
| 模块文档全覆盖 | 清单 24 行均有九章文档 | ✅ `docs/modules/` 已齐 |
| 蜜罐为可选自研 | 两文档 §1 标注 | ✅ |

**没做 / 遗留**：变体集合规模 / 识破阈值 / 会话结束判据 · `intent` 意图分类体系 · `chain` 链判据 · `netpolicy` 是否进 MVP · 学习型多态；本轮为**设计轮**，不含代码。

---

## 2026-09-18 · 补响应层设计：生成式欺骗响应 + 间接注入防护

**做了什么**：补齐「改道之后返回什么」与「分析链路怎么不被反注入」—— ① `responder`（设计）：**快慢两路**（核心确定性模板 + L4 预生成落库）与**一致性不变量 `AR-30`**（同 `(会话,资源)` 同答案，禁止 LLM 采样等非确定性）；② `llm-components`（设计）：LLM 契约纪律（`AR-15`…`AR-27`）+ **间接注入防护**（`AR-31` 数据/指令分离 · `AR-32` 最小权限与不回流）；③ 两条 ADR（0014 / 0015）；④ `architecture.md` §5 新增 `AR-30` / `AR-31` / `AR-32`。

**改了哪些文件**：`docs/background/decisions/0014-generative-deceptive-response.md` · `docs/background/decisions/0015-indirect-prompt-injection.md` · `docs/background/decisions/README.md` · `docs/design/architecture.md` · `docs/modules/responder.md` · `docs/modules/llm-components.md` · `docs/progress.md` · `docs/plans/2026-09-18-responder-and-injection-design.md` · `docs/log.md`

**对应文档**：[`docs/modules/responder.md`](docs/modules/responder.md) · [`docs/modules/llm-components.md`](docs/modules/llm-components.md) · [`docs/background/decisions/0014-generative-deceptive-response.md`](docs/background/decisions/0014-generative-deceptive-response.md) · [`docs/background/decisions/0015-indirect-prompt-injection.md`](docs/background/decisions/0015-indirect-prompt-injection.md)

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 一致性不变量 | 同 `(会话,资源)` 同答案 | ✅ `AR-30` |
| 核心不调 LLM | `responder` 禁外呼，L4 预生成 | ✅ `responder.md` §3 |
| 注入防护结构性 | 数据/指令分离 | ✅ `AR-31` |
| 分析无执行能力 | 最小权限 + 不回流 | ✅ `AR-32` |

**没做 / 遗留**：诱饵多态与再生成 · 一致性键粒度/预生成时机/`severity` 交互 · 多源交叉校验与注入测试集 · `strategy` / `intent` / `chain` 模块文档；本轮为**设计轮**，不含代码。

---

## 2026-09-18 · 补判别层设计：指纹 + 会话级判别 + 归因令牌

**做了什么**：补判别层的三项设计 —— ① `judge` 增加 Agent 产品级指纹（UA/头签名 + 置信度阶梯 + 证据链）与**消费会话特征**（保持纯函数）；② `session` 增加**会话状态抽象**（速度/挑战逃逸，经 `StateSource`）与**归因令牌**（蜜标 HMAC 双角色 + 凭证水印）；③ 两条 ADR（0012 会话级判别的落点、0013 归因令牌）；④ `config` 契约加 `fingerprints` / `attribution` 段。

**改了哪些文件**：`docs/background/decisions/0012-session-level-judgement.md` · `docs/background/decisions/0013-attribution-token.md` · `docs/background/decisions/README.md` · `docs/modules/judge.md` · `docs/modules/session.md` · `docs/spec/config.md` · `docs/plans/2026-09-18-judgement-layer-design.md` · `docs/log.md`

**对应文档**：[`docs/modules/judge.md`](docs/modules/judge.md) · [`docs/modules/session.md`](docs/modules/session.md) · [`docs/background/decisions/0012-session-level-judgement.md`](docs/background/decisions/0012-session-level-judgement.md) · [`docs/background/decisions/0013-attribution-token.md`](docs/background/decisions/0013-attribution-token.md)

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| Agent 指纹 | 签名 + 置信度阶梯 + 证据链 | ✅ `judge.md` §4.1 |
| 会话级判别 | 特征注入，`judge` 仍纯函数 | ✅ [ADR-0012](docs/background/decisions/0012-session-level-judgement.md) |
| 归因令牌 | 双角色 + 水印 + TTL + 闭环校验 | ✅ [ADR-0013](docs/background/decisions/0013-attribution-token.md) |
| 不照搬 AGPL 签名表 | 明确「自行整理」 | ✅ `judge.md` §4.1 |

**没做 / 遗留**：间接提示注入风险（采集链路喂 LLM 被反注入）· 诱饵多态与再生成 · `responder` 的生成式欺骗响应 —— 均为下轮；spec/fingerprints.md 与 spec/attribution.md 的字段实现时写；本轮为**设计轮**，不含代码。

---

## 2026-09-18 · 补欺骗引擎设计：诱饵面（decoy）+ 蜜罐入口（honeypot）

**做了什么**：补上「改道之后拿什么骗」的设计 —— ① 新增 `decoy` 模块（诱饵面：Developer API / 指令文件 / MCP / 消耗战数据集 / 三类蜜饵 + SSRF，依据 ADR-0010「功能性伪装」）；② 新增 `honeypot` 模块（蜜罐入口与后端池：类型注册 + `config` 开关 + 生命周期 + 后端解析，**不实现具体蜜罐**，依据 ADR-0011）；③ 两条 ADR；④ `config` 契约加 `decoys` / `honeypots` 段；⑤ 新规则 `MD-25`（诱饵面 observe-only）/ `MD-26`（蜜罐经 `honeypot` 管理）。

**改了哪些文件**：`docs/background/decisions/0010-functional-camouflage.md` · `docs/background/decisions/0011-honeypot-entry-external-backends.md` · `docs/background/decisions/README.md` · `docs/design/modules.md` · `docs/modules/decoy.md` · `docs/modules/honeypot.md` · `docs/spec/config.md` · `docs/progress.md` · `docs/modules/README.md` · `docs/design/structure.md` · `docs/plans/2026-09-18-deception-engine-design.md` · `docs/log.md`

**对应文档**：[`docs/modules/decoy.md`](docs/modules/decoy.md) · [`docs/modules/honeypot.md`](docs/modules/honeypot.md) · [`docs/background/decisions/0010-functional-camouflage.md`](docs/background/decisions/0010-functional-camouflage.md) · [`docs/background/decisions/0011-honeypot-entry-external-backends.md`](docs/background/decisions/0011-honeypot-entry-external-backends.md)

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 诱饵面五类形态 | 有设计 | ✅ `decoy.md` §1 |
| 蜜罐只做入口 | 无具体实现，类型 `config` 开关 | ✅ `honeypot.md` §1 |
| 模块清单 | 24 行 / 23 有效 | ✅ `modules.md` §1.1 |
| 悬空链接 | `TC-3` 归零 | ✅ `make trace` 通过 |

**没做 / 遗留**：`judge` 指纹与置信度、会话级有状态判别、归因令牌（蜜标/水印）、间接提示注入风险 —— 均为下轮；spec/decoy.md 与 spec/honeypot.md 的字段实现时写；本轮为**设计轮**，不含代码。

---

## 2026-09-18 · director 阶段 2a 实现（6 项决策已确认）

**做了什么**：让引流真正生效 —— 新增 `director` 模块，把 judge 的风险分映射为三值（`route_origin` / `route_mirage` / `block`）+ 后端名，并按 `policy.gray_pct` 做确定性灰度收敛（同 `decision_id` 同结果，`ST-10`）。`block` 默认不产出（Q5）。熔断（`NI-10`）落在 `control` 服务面。`policy` 暴露阈值/灰度/影子开关。装配层按 `shadow` 选决策器：影子用 `ShadowDecider`（恒放行，回归保持），关闭后用 `director`。

**改了哪些文件**：`core/internal/contract/thresholds.go` · `core/internal/director/`（新：iface.go · director.go · director_test.go）· `core/internal/control/breaker.go` + `breaker_test.go` · `core/internal/control/service.go` · `core/internal/policy/policy.go` + `policy_test.go` · `core/cmd/core/main.go` + `main_test.go` · `docs/modules/director.md` · `docs/spec/config.md` · `docs/progress.md` · `docs/modules/README.md` · `docs/design/structure.md` · `docs/plans/2026-09-17-director-2a.md` · `docs/plans/2026-09-18-director-2a.md` · `docs/log.md`

**对应文档**：[`docs/modules/director.md`](docs/modules/director.md) · [`docs/plans/2026-09-18-director-2a.md`](docs/plans/2026-09-18-director-2a.md) · [`docs/spec/config.md`](docs/spec/config.md) §2.0

**验证**：`make gate` 通过（含 `-race`）· `make dev` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 三值 × 阈值边界 | 按阈值映射 | ✅ `TestClassifyBoundaries` 7 子例 PASS |
| 灰度确定性 | 同 decision_id 同结果 | ✅ `TestGrayDeterministic` PASS |
| 端到端改道 | 接管模式高分 → route_mirage | ✅ `TestDirectorDivertsHighScore` PASS |
| 影子回归 | 影子模式恒 origin | ✅ `make dev` 冒烟 3 样本全 origin |
| 熔断 | 错误率超阈值跳闸 | ✅ `breaker_test.go` 5 例 PASS |

**没做 / 遗留**：`whitelist` 消费（INT-25）未接 · 后端名固定 `mirage`（池选择是阶段 3）· `severity` 仍固定 none · `block` 开关走构造参数（未进 config）· `guard.false_route_budget` 未接。

---

## 2026-09-18 · 删除 mirror 死代码 + block 可见性注释

**做了什么**：① 删除 `edge/mirror` 读请求体塞 header 的死代码（`x-observed-body-prefix` 无人消费，契约无 body 字段）——连同 `MaxBody` 字段、`maxBody()` 方法与只测死数据的 `TestReceiver_BodyIsCapped`；② 在 `edge/proxy` 的 block 分支加注释，点明 403 可见处置是 ADR-0002 已承认的设计。

**改了哪些文件**：`edge/mirror/receiver.go` · `edge/mirror/receiver_test.go` · `edge/proxy/proxy.go` · `docs/plans/2026-09-18-mirror-dead-code-and-block-note.md` · `docs/log.md`

**对应文档**：[`docs/background/decisions/0002-decision-model.md`](docs/background/decisions/0002-decision-model.md)（block 可见性依据）

**验证**：`make gate` 通过（含 `-race`）

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| body 前缀无消费 | grep 核心/api 为空 | ✅ 死数据确认 |
| 删除后编译测试 | 全绿 | ✅ `make gate` 通过（edge/mirror 1.469s · edge/proxy 1.983s） |

**没做 / 遗留**：误导处置读 body（INT-22）留到阶段 2b 契约演进；block 403 可见性不变（设计如此）。

---

## 2026-09-18 · 变更日志改名：Log.md → log.md（小写）

**做了什么**：按用户要求把 `docs/Log.md` 改名为 `docs/log.md`。同步改 `scripts/tracecheck/main.go` 的 5 处硬编码、`.pi/devloop.md` 的 `change_log`、以及 9 个活文档的引用与链接；历史变更包与 `docs/log.md` 旧条目按 audit 门槛③保留原写法。

**改了哪些文件**：`docs/log.md`（由 Log.md 改名）· `scripts/tracecheck/main.go` · `.pi/devloop.md` · `AGENTS.md` · `docs/README.md` · `docs/progress.md` · `docs/modules/README.md` · `.pi/skills/dev-loop-project/SKILL.md` · `docs/plans/_change-package.md` · `docs/plans/README.md` · `docs/kb/dev-workflow.md` · `docs/plans/2026-09-18-log-rename.md`

**对应文档**：[`docs/log.md`](docs/log.md)（变更日志）

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 文件名 | `docs/log.md` 存在，`docs/Log.md` 不存在 | ✅ mv 两步完成 |
| 活文档引用 | 无活文档再写 Log.md | ✅ 仅剩历史快照 |
| 门禁 | tracecheck 认新路径 | ✅ `make trace` 通过 |

**没做 / 遗留**：历史变更包（`docs/plans/2026-09-18-*.md`）与 `docs/log.md` 旧条目里的「Log.md」保留原写法（audit 门槛③历史快照不动）。

---

## 2026-09-18 · 清理 Lua 残留设计 + 判定缓存容量上限（MD-10）

**做了什么**：① 按 ADR-0008（Lua 已移除、L1 改 Go）清理 6 处未跟上的文档残留（`docs/modules/README.md` §4.2 的 adapter-reverse-proxy/sidecar 与 Lua、`docs/modules/_template.md` 语言列表、`docs/design/architecture.md` §7.5/§8.2 的 OpenResty、`docs/design/structure.md` §4.1 的 OpenResty）；② 修 `edge/proxy` 判定缓存无容量上限的 MD-10 缺口 —— 加 `CacheMaxEntries`（默认 65536），满则整体清空。

**改了哪些文件**：`edge/proxy/iface.go` · `edge/proxy/proxy.go` · `edge/proxy/cmd/proxy/main.go` · `edge/proxy/proxy_test.go` · `docs/modules/README.md` · `docs/modules/_template.md` · `docs/design/architecture.md` · `docs/design/structure.md` · `docs/plans/2026-09-18-lua-cleanup-and-cache-cap.md` · `docs/log.md`

**对应文档**：[`docs/background/decisions/0008-edge-language-go.md`](docs/background/decisions/0008-edge-language-go.md)（Lua 移除依据）· [`docs/design/modules.md`](docs/design/modules.md) 的 MD-10（缓存容量上限）

**验证**：`make gate` 通过（含 `-race` 与追溯检查）

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 缓存容量上限 | cap=2 时条目数不超 2 | ✅ `TestCacheRespectsCapacityCap` 通过 |
| Lua 残留归零 | 现行文档无「Lua 是实现语言」漂移 | ✅ 仅剩 language.md 否决记录 + archcheck 门禁（均应保留） |
| 门禁整体 | 全绿 | ✅ fmt · vet · staticcheck · errcheck · archcheck · trace · 许可 · 单测 -race |

**没做 / 遗留**：`language.md` 的 Lua 否决记录与 `archcheck` 的 `.lua` 门禁**保留**（决策依据与防回归，非漂移）；`edge/mirror` 把 body 前缀塞进 header（契约无 body 字段，留待契约演进）；block 决策的 403 可见性（设计议题，非本轮）。

---

## 2026-09-18 · 进度表迁至 docs/progress.md（与 Log.md 同目录）

**做了什么**：把模块实现进度（清单 · 实现方式 · 阶段状态）从 `docs/modules/README.md` 整体迁出，独立成 `docs/progress.md`，与 `docs/Log.md` 同在 `docs/` 根，供人工审计对照；`docs/modules/README.md` 瘦身为模块索引（文档状态 · 下一步 · 新增指南）。同步更新 6 处现行文档的引用。

**改了哪些文件**：`docs/progress.md`（新建）· `docs/modules/README.md`（瘦身）· `docs/README.md` · `AGENTS.md` · `.pi/AGENTS.md` · `docs/design/structure.md` · `.pi/devloop.md`（`doc_layers` 注册 progress.md）· `docs/plans/2026-09-18-progress-relocation.md` · `docs/Log.md`

**对应文档**：[`docs/progress.md`](docs/progress.md)（进度唯一维护处）· [`docs/Log.md`](docs/Log.md)（变更日志）

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 进度表链接不悬空 | `make trace` 无 `TC-3` | ✅ 追溯检查通过 |
| 旧「完成度」引用更新 | 现行文档不再指向 `docs/modules/README.md` 完成度 | ✅ 仅剩历史快照 + §4/§5 引用 |
| 规则 ID 引用真实 | 无 `D-3` | ✅ `AR-3` / `TB-22` / `TB-2` 均命中 |

**没做 / 遗留**：`docs/modules/README.md` §4.2 的 Lua/Rust 表述与 ADR-0008（L1 改 Go、③④ 合并）漂移，属既有问题，本轮未修（另开一条）；历史变更包与旧日志条目按 audit 门槛③不动。

---

## 2026-09-18 · 模块实现方式总览（自研 / 复用开源）

**做了什么**：在模块索引新增 §1a「实现方式总览」，把散落在 `architecture.md`（复用三原则）· `language.md`（分层选型）· `structure.md` §3（存储分工）里的「开源 vs 自研」结论汇总成一张表；并把与实现方式相关的 4 个未决项（D1 / D6 / D7 / L3 是否进 MVP）集中标注去向。

**改了哪些文件**：`docs/modules/README.md`（新增 §1a）· `docs/plans/2026-09-18-progress-overview.md`（本轮变更包）· `docs/Log.md`

**对应文档**：[`docs/design/architecture.md`](docs/design/architecture.md) §1（复用三原则）· [`docs/design/language.md`](docs/design/language.md) §1 · [`docs/design/structure.md`](docs/design/structure.md) §3 · [`docs/background/notes/implementation-discussion.md`](docs/background/notes/implementation-discussion.md) §6.1

**验证**：`make trace` 通过 · `make gate` 通过

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 新增规则 ID 引用真实存在 | 无 `D-3` 悬空 | ✅ `AR-3` / `TB-22` / `TB-2` / `ADR-0008` 均命中 |
| 未决项不当作决策写死 | 标「未决」并给去向 | ✅ 4 项均标 ⏳ + 指向 notes / modules.md §7 |

**没做 / 遗留**：未替用户拍板任何未决项（D1 / D6 / D7 / L3 是否进 MVP 仍待用户决策）；本表是既有结论的汇总，无新决策，故无 ADR。

---

## 2026-09-18 · 技能合并：8 个 → 5 个（项目 6 → 3）

**做了什么**：把触发面重叠、纪律重复的技能合并 —— `spec-first-dev` 并入 [`dev-loop-project`](../../.pi/skills/dev-loop-project/SKILL.md)（开工门禁 + 范围纪律）；`research-survey` + `design-decision` + `adversarial-benchmark` 合并为 [`evidence-and-decisions`](../../.pi/skills/evidence-and-decisions/SKILL.md)（调研 → 决策 → 基准）。加上全局 `dev-loop` / `audit`，**全机共 5 个技能**；项目 `.pi/skills/` 从 6 个降到 3 个。

**改了哪些文件**：`.pi/skills/dev-loop-project/SKILL.md`（重写，吸收开工纪律）· `.pi/skills/evidence-and-decisions/SKILL.md`（新增，合并三份）· `.pi/AGENTS.md` · `AGENTS.md` · `docs/README.md` · `docs/kb/dev-workflow.md` · `docs/kb/removed-skills.md`（追加合并记录）· `docs/background/decisions/README.md` · `docs/background/research/README.md` · `scripts/tracecheck/main.go`（强制名单去掉 `spec-first-dev`）· `~/.pi/agent/skills/dev-loop/SKILL.md` · `~/.pi/agent/skills/audit/SKILL.md` · `docs/plans/2026-09-18-dev-loop.md`（追加第 14 节）· `docs/Log.md`

**对应文档**：[`docs/kb/removed-skills.md`](../kb/removed-skills.md)（合并清单 + 恢复方法 + 恢复后要同步哪 5 处）· [`docs/kb/dev-workflow.md`](../kb/dev-workflow.md) §5（路由图已更新）· [`docs/plans/2026-09-18-dev-loop.md`](2026-09-18-dev-loop.md) §14

**验证**：`make trace` 追溯检查通过 · `make gate` 通过 · `make dev` PASS

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 合并后技能数 | 项目 3 · 全机 5 | ✅ `.pi/skills/` = dev-loop-project · evidence-and-decisions · threat-model |
| 内容不丢 | 原 4 份的实质内容均可定位 | ✅ dev-loop-project §1/§2（原 spec-first-dev）· evidence-and-decisions §1/§2/§3（原三份） |
| 行数收敛 | 少于合并前 | ✅ 592 行 → 492 行（三份，重复纪律已删） |
| 引用不悬空 | 无旧技能名指向已删路径 | ✅ `make trace` 通过（含 `DEV-3` 点名与 `TC-3` 链接） |
| 可恢复 | 原件备份 + 恢复步骤 | ✅ `~/.pi/backups/removed-skills/merged-2026-09-18/`（4 份原件） |

**没做 / 遗留**：`docs/background/research/knowledge-base-audit.md` 仍写旧技能名（属**历史快照**，按 `audit` 门槛 ③ 不改）；合并版技能较长（`evidence-and-decisions` 252 行），若以后只做 ADR 也要整份加载 —— 目前可接受，若变碍事再拆回。

**修正（同日）**：图示对齐 —— 自动补齐右边框后，`§2 一轮时序` 与 `§3 门禁链路` 出现残留的连接符／多余空行；前者重写为无边框的竖向流程，后者重写为缩进树，`§6 失败与返工` 重写为三个等宽并排框（每行 81 列，已核）。

---

## 2026-09-18 · 移除第三方技能 skill-creator（248K · 10 个可执行脚本）

**做了什么**：把第三方技能 `skill-creator` 从 `.pi/skills/` 移出（备份在 `~/.pi/backups/removed-skills/skill-creator-2026-09-18/`），并同步 4 处引用。移除后项目技能库为 **6 个目录，全部自建 Markdown，可执行代码文件数 = 0**。

**改了哪些文件**：`AGENTS.md` · `.pi/AGENTS.md` · `docs/README.md` · `docs/kb/README.md` · `docs/kb/removed-skills.md` · `docs/kb/dev-workflow.md`（路由图）· `~/.pi/agent/skills/dev-loop/SKILL.md`（全局路由表）· `docs/plans/2026-09-18-dev-loop.md`（追加第 13 节）· `docs/Log.md`

**对应文档**：[`docs/kb/removed-skills.md`](../kb/removed-skills.md)（为什么删 + 怎么恢复 + 恢复后要同步哪 5 处）· [`docs/plans/2026-09-18-dev-loop.md`](2026-09-18-dev-loop.md) §13（删除记录）

**验证**：`make trace` 追溯检查通过 · `make gate` 通过 · `make dev` PASS

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 删除前依赖检查（门槛 ①） | 除自身与路由表外无依赖 | ✅ 只有 4 处文字引用（`AGENTS.md` · `.pi/AGENTS.md` · `docs/kb/dev-workflow.md` · 全局路由表），无代码/流程依赖 |
| 可恢复性（门槛 ④） | 有备份且路径可查 | ✅ 248K · 18 文件移至仓库外；路径与重装方法写入 kb |
| 删除后可达性 | 无悬空引用、无点名缺失 | ✅ `make trace` 通过（含 `DEV-3` 技能点名与 `TC-3` 链接） |
| 安全面 | 项目技能里可执行代码归零 | ✅ `find .pi/skills -name '*.py' -o -name '*.sh'` → 0 |

**没做 / 遗留**：`spec-first-dev` 与 `dev-loop` §3③ 仍有部分重叠（未合并，其「开工 4 项检查」清单仍有独立价值）；若要技能评测能力，需按 kb 记录重装。

---

## 2026-09-18 · 规范链路体检 + 学习用图

**做了什么**：把整套开发规范当成被审对象查了一遍 —— 找出 **6 处不合理**（其中 1 处致命：项目实例化技能因同名冲突从未被加载），全部修正；并新增学习用图 [`docs/kb/dev-workflow.md`](../kb/dev-workflow.md)（三层结构 · 一轮时序 · 门禁链路 · 追溯链路 · 技能路由 · 返工路径 · 常见误解）。

**改了哪些文件**：`AGENTS.md` · `.pi/AGENTS.md` · `.pi/skills/dev-loop-project/SKILL.md`（由 `dev-loop` 改名）· `scripts/tracecheck/main.go` · `~/.pi/agent/skills/dev-loop/SKILL.md` · `docs/README.md` · `docs/plans/_change-package.md` · `docs/kb/dev-workflow.md` · `docs/plans/2026-09-18-dev-loop.md`（追加第 12 节）· `docs/Log.md`

**对应文档**：[`docs/kb/dev-workflow.md`](../kb/dev-workflow.md)（图示）· [`docs/plans/2026-09-18-dev-loop.md`](2026-09-18-dev-loop.md) §12（体检结论 + 审视记录）

**验证**：`make trace` 追溯检查通过 · `make gate` 通过 · `make dev` PASS

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 同名技能冲突 | 确认哪个生效 | ✅ pi 源码 `skills.js`（第 329–351 行）：保留**先加载**的；全局先于项目 → 项目那份被丢弃 |
| 引用全局技能的链接 | 应被门禁核 | ✅ 扩展 `DEV-3` 支持 `~/.pi/agent/skills/...` 后，能核到全局 `dev-loop` / `audit` |
| 强制技能名单 | 应含实例化与审视技能 | ✅ 改为 `dev-loop` · `dev-loop-project` · `spec-first-dev` · `audit` |
| `.pi/AGENTS.md` 技能清单 | 与实际一致 | ✅ 原先写「6 个 / 自建 5 个」，实际 7 个目录；已改写并拆出全局层 |
| 项目技能悬空链接 | 应报 `TC-3` | ✅ 改名后依次报出 3 处陈旧链接，全部修正 |

**没做 / 遗留**：`iteration_cmds` 仍只在技能表里说明用途（无工具读它，属人工约定）；图示放在 `kb/`（非规范），若日后规范变动需靠人同步（无机器校验）。

---

## 2026-09-18 · 审视（audit）：查「留下的东西还准不准」

**做了什么**：新增全局技能 `audit`（查漂移 / 无用信息 / 僵尸内容，**四道删除门槛**，产出审视表），接进流程（L 档必做、AGENTS.md 与 dev-loop 同步），并把可机器核的两类接进本项目的追溯检查（新增 `TC-2` 过期状态标记 · `TC-3` 悬空链接）。首次审视即查出 6 条真问题并全部修正。

**改了哪些文件**：`~/.pi/agent/skills/audit/SKILL.md` · `~/.pi/agent/AGENTS.md` · `~/.pi/agent/skills/dev-loop/SKILL.md` · `~/.pi/agent/skills/dev-loop/assets/change-package.template.md` · `~/.pi/agent/skills/dev-loop/assets/devloop.template.md` · `~/.pi/agent/skills/dev-loop/assets/agents-snippet.md` · `~/.pi/agent/skills/dev-loop/scripts/devloop-init.sh` · `scripts/tracecheck/main.go` · `AGENTS.md` · `.pi/devloop.md` · `.pi/skills/dev-loop/SKILL.md` · `docs/plans/_change-package.md` · `docs/README.md` · `docs/modules/judge.md` · `docs/modules/session.md` · `docs/plans/2026-09-18-dev-loop.md`（追加第 11 节）· `docs/Log.md`

**对应文档**：`~/.pi/agent/skills/audit/SKILL.md`（审视做法与删除门槛）· `docs/plans/2026-09-18-dev-loop.md` §11（本节含审视记录）· `.pi/skills/dev-loop/SKILL.md` §5.1（本项目落地）

**验证**：`make trace` 追溯检查通过（新增两类检查后归零）· `make gate` 通过 · 两项注入实验命中预期

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 注入「已存在路径 + 待创建」 | 报 `TC-2` | ✅ 命中 `docs/modules/judge.md:89` |
| 首次扫描本项目技能链接 | 报 `TC-3` | ✅ 命中 4 处层级写错的链接，修正后归零 |
| 「A 已建；B 待写」同一行 | 不误报 | ✅ 0 条 |
| 真实历史缺口 | 首次运行即查出 | ✅ `judge.md` / `session.md` 的「待创建」已过期；`docs/README.md` 的计数已过期 |
| 门禁整体 | 全绿 | ✅ `make gate` 通过 |

**没做 / 遗留**：语义一致性、误导性叙述、内容是否真无用仍靠人读（机器只能核形状）；审视只在 L 档强制，M 档靠判断；本轮无删除动作（六条均为修正）。

---

## 2026-09-18 · 开发规范泛化为跨项目通用（全局技能 + 适配面 + 初始化脚本）

**做了什么**：把只在本仓库成立的开发规范提为**跨项目通用规范** —— 全局三条铁律（`~/.pi/agent/AGENTS.md`）、全局技能 `dev-loop`（S/M/L 分级 · 五步 · 产物形状 · 追溯接口 · 评审 · 返工路径 · 模板与初始化脚本），本项目改为只写差异的实例化。

**改了哪些文件**：`~/.pi/agent/AGENTS.md` · `~/.pi/agent/skills/dev-loop/SKILL.md` · `~/.pi/agent/skills/dev-loop/assets/devloop.template.md` · `~/.pi/agent/skills/dev-loop/assets/change-package.template.md` · `~/.pi/agent/skills/dev-loop/assets/change-log.template.md` · `~/.pi/agent/skills/dev-loop/assets/agents-snippet.md` · `~/.pi/agent/skills/dev-loop/scripts/devloop-init.sh` · `.pi/devloop.md` · `.pi/skills/dev-loop/SKILL.md` · `AGENTS.md` · `scripts/tracecheck/main.go`（日志路径检查支持 `~/` 与绝对路径）· `docs/README.md` · `docs/plans/2026-09-18-dev-loop.md`（追加第 10 节）· `docs/Log.md`

**对应文档**：`docs/plans/2026-09-18-dev-loop.md` §10（泛化的决定与三层结构）· `AGENTS.md` §1 / §4（本项目只写差异）· `.pi/devloop.md`（适配面）

**验证**：`make trace` 追溯检查通过 · `make gate` 通过 · `make dev` PASS · 初始化脚本在空项目 / Python 项目上自测通过且幂等 · 全局技能头部合法（`name` 合规 · `description` 170 字符）

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 空项目初始化 | 生成 4 项骨架并探测验证命令 | ✅ 无标记时写入占位符 `verify_cmd: <本项目的一键验证命令>` |
| Makefile / Python 项目 | 探测到对应验证命令 | ✅ 分别写入 `make gate` 与 `pytest -q` |
| 重复执行初始化 | 不覆盖已有文件 | ✅ 「新增 0 项，跳过 4 项」 |
| 适配面可解析 | YAML 合法且字段齐全 | ✅ 10 个字段全部读出 |
| 本项目追溯 | 零错误 | ✅ 追溯检查通过（1 提示 + 3 登记豁免） |

**没做 / 遗留**：追溯检查工具**不上提**为通用工具（先跑通两三个项目再抽象）；通用技能里的「一键验证命令」仍靠项目自建（脚本只探测并提示）；全局规范对不适用的项目需在该项目 `.pi/devloop.md` 写 `enabled: false` 手动豁免。

---

## 2026-09-18 · 技能强制写进 `AGENTS.md`（+ 门禁核对 `DEV-3`）

**做了什么**：把「开工前先加载 `dev-loop` 技能」从建议变成规范条款 —— `AGENTS.md` §1 / §4 写入强制条款与技能路由表（强制 2 个 · 按需 5 个），并新增门禁检查 `DEV-3`：本文点名的技能文件必须真实存在且头部有 `name` / `description`。

**改了哪些文件**：`AGENTS.md` · `scripts/tracecheck/main.go` · `.pi/skills/dev-loop/SKILL.md` · `docs/README.md` · `docs/plans/2026-09-18-dev-loop.md`（追加第 9 节）· `docs/Log.md`

**对应文档**：`AGENTS.md` §4（技能路由表 + 强制条款）· `.pi/skills/dev-loop/SKILL.md`（配套件表新增「强制条款」一行）· `docs/plans/2026-09-18-dev-loop.md` §9

**验证**：`make gate` 通过 · `make trace` 零错误 · 两项新注入实验按预期报错

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| `AGENTS.md` 点名不存在的技能 | 报 `DEV-3` | ✅ 报「技能文件不存在」+「没有点名强制技能」 |
| 技能文件缺 `name` 头 | 报 `DEV-3` | ✅ 报「缺 YAML 头部（name / description）」 |
| 正常仓库 | 零错误 | ✅ 追溯检查通过（1 提示 + 2 登记豁免） |

**没做 / 遗留**：技能路由表靠人工维护（`DEV-3` 只核「点名的存在 + 强制的被点名」）；技能的具体执行质量（有没有真按五步走）机器核不了，靠变更包与 Log 留痕复查。

---

## 2026-09-18 · 开发循环与追溯门禁（tracecheck · Log · 变更包模板）

**做了什么**：把「设计 → 文档 → 代码 → 测试 → 证据」这条链变成可机器核的固定流程 —— 新增追溯门禁 `make trace`（已进 `make gate`）、变更日志（本文件）、变更包模板与每轮固定动作（技能），并顺手查出两处真实的文档悬空引用。

**改了哪些文件**：`scripts/tracecheck/main.go` · `scripts/tracecheck/allow.txt` · `.pi/skills/dev-loop/SKILL.md` · `docs/plans/_change-package.md` · `docs/plans/2026-09-18-dev-loop.md` · `docs/plans/2026-09-18-policy-2a.md`（按模板重排）· `docs/Log.md` · `Makefile` · `AGENTS.md` · `docs/README.md` · `docs/plans/README.md`

**对应文档**：`AGENTS.md` §4（每轮必须留下四样）· `.pi/skills/dev-loop/SKILL.md`（五步 + 输出形状）· `docs/plans/_change-package.md`（模板）· `docs/design/modules.md` 的 `MD-2` / `MD-17` / `MD-22`（门禁强制的三条）

**验证**：`make gate` 通过（`gate` 现在含 `trace`）· `make trace` 零错误（1 条提示 + 2 条登记豁免）· 八项注入实验均按预期报错 · `make dev` PASS

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 正常仓库 | `make trace` 零错误 | ✅ 「追溯检查通过」 |
| 悬空规则 ID | 报 `D-3` + `文件:行` | ✅ 查出真实历史缺口：`docs/README.md` 的 NI 范围多写了一条（已修正） |
| 模块有代码无文档 | 报 `MD-17` | ✅ 移除 `docs/modules/policy.md` 后即报 |
| 模块有代码无单测 | 报 `MD-22` | ✅ 移除 `policy_test.go` 后即报 |
| 日志缺段 / 引用死路径 | 报 `DEV-2` | ✅ 缺 `**证据**` 段与写假路径均报出 |
| 豁免过期 | 报 `ALLOW` | ✅ 注入匹配不上的豁免后报「豁免已过期」 |
| 文档格式变了 | 拒绝静默放行 | ✅ 前缀解析到 0 个即报错退出 |
| 门禁整体 | 全绿 | ✅ 格式化 · vet · staticcheck · errcheck · archcheck · trace · 许可 · 单测(-race) |

**没做 / 遗留**：规则 ID 未做「逐条映射到代码与测试」（153 条全量映射成本高，本轮只做模块级 + 引用存在性）；门禁查不了「测试覆盖得好不好」与「设计逻辑写得对不对」（那靠评审与场景表）。

---

## 2026-09-18 · `policy` 阶段 2a（策略装载 · 版本 · 校验和 · 规则供给）

**做了什么**：判定引擎第一次有了真实规则来源 —— 从配置文件装载策略、严格校验、产出带版本号与校验和的不可变快照、写入版本台账、供 `judge.RuleSource`。同时闭合阶段 1 的「空规则集静默启动」缺口：配置缺失或非法**必须**启动失败。另配一套开发期验证入口（`make check-config` / `replay` / `smoke` / `dev`）。

**改了哪些文件**：`core/internal/policy/`（新增 `iface.go` · `policy.go` · `policy_test.go`）· `core/cmd/core/main.go` · `core/cmd/core/main_test.go` · `core/internal/control/rules.go`（删除，占位规则源被 `policy` 接管）· `deploy/config/config.example.yaml` · `Makefile` · `scripts/devcheck/main.go` · `scripts/dev/smoke.sh` · `docs/spec/config.md` · `docs/modules/policy.md` · `docs/background/decisions/0009-policy-version-source.md` · `docs/plans/2026-09-18-policy-2a.md`

**对应文档**：`docs/spec/config.md`（配置全表 + JSON Schema）· `docs/modules/policy.md`（九章）· ADR-0009（版本来源）· `docs/design/structure.md` §1.6（接缝已接）

**验证**：`make gate` 通过（含 `-race`）· `make dev` 全绿 · 单测 37 个（`policy`）+ 6 个（装配层）

**证据**：
| 场景 | 期望 | 实测 |
| --- | --- | --- |
| 合法配置 | 装载成功并打印摘要 | ✅ `policy_id=dev-smoke version=1 checksum=2cd6a7d1… 规则=2 条` |
| 权重越界 | 启动失败并指出字段 | ✅ `rules[1].weight=1.5 越界（要求 0 < weight ≤ 1）` |
| 规则回放 | 分数与命中信号正确 | ✅ `0.9（ua-headless,path-git）· 0.3 · 0 · 0.5` |
| 在线冒烟 | 判定面应答且 `decision_id` 幂等 | ✅ 3 样本全通过，`action=origin severity=none` |
| 影子模式回归 | 分数拉满也不处置 | ✅ 恒 `route_origin` |

**没做 / 遗留**：下发面 `Pull` / `Watch` / `Ack` 未实现；`store` / `telemetry` 真实后端未接；`whitelist` 段只解析不消费（消费方是 `director`）；`severity` 档位与 `E2`（TLS 指纹一致性，项目级 P0）仍待。
