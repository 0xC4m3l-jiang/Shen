# 变更包：入口 README 精简为「项目介绍 + 项目启动」

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 根 `README.md` 只保留**项目介绍**与**项目启动**；其余内容删除（信息都在 `docs/` 内有更权威的版本） |
| 日期 | 2026-09-21 |
| 状态 | 已实现（门禁绿） |
| 档位 | **S**（纯文档；不改代码、不改规则、不改契约） |
| 关联 | [`docs/README.md`](../README.md)（导航中枢，本轮的入口指针跟着改）· 上一轮 [`2026-09-21-readme-four-layer-arch.md`](2026-09-21-readme-four-layer-arch.md)（四层图来源） |

---

## 1. 需求与验收

**要解决什么**（用户原话拆解）：README 应当**只讲项目做什么**与**怎么启动** ——
关于蜜罐等能力，只说明**后续接入**（不写成像已完成）；删除其余信息，提高内容质量。

**做完后的判据**：README **只含两部分**：① 项目介绍（含四层视图、三条硬约束、使用边界、现状与未接入项）② 项目启动（一键起全套 + 本地开发）；
**凡属实现细节、工程纪律、目录清单、逐项限制的内容不得出现在 README**；README 里**不得出现「蜜罐已完成」这类表达**。

**不做什么**：不改任何代码、不动 `docs/design/`；不新增文档（删除的内容在 `docs/` 内已有权威版本，见 §2）。

---

## 2. 设计逻辑

删掉的六类内容**不是丢弃，而是回到它们各自的权威位置**（这是敢删的前提）：

| README 原章节 | 删掉后去哪找 |
| --- | --- |
| §2 状态表 / §9 已知限制（长表） | [`docs/progress.md`](../progress.md) · 各模块文档「未决项」段 · [`docs/background/notes/pending-experiments.md`](../background/notes/pending-experiments.md) |
| §4 效果长什么样（`make dev` 输出、TLS 实跑、行为表） | [`docs/ops/functional-verification.md`](../ops/functional-verification.md) · [AI 注入验证报告](../ops/ai-injection-2026-09-21/README.md) |
| §5 目录结构 | [`docs/modules/_map.md`](../modules/_map.md)（一张表回答全部） |
| §6 文档地图 | [`docs/README.md`](../README.md)（本轮起它是唯一的入口地图） |
| §7 门禁与追溯 | [`AGENTS.md`](../../AGENTS.md) §4 · [`docs/kb/dev-workflow.md`](../kb/dev-workflow.md) |
| §10 参与开发 | 同上（`AGENTS.md` §4 的开发循环） |

**保留但压缩的两处**（因为它们是「项目是什么」的一部分，且删掉有真实风险/失真风险）：

1. **使用边界**（授权环境 · 不做控制面 · 只用自己的素材 · 对外可见面不自曝）：公开仓库里的安全工具，删掉法律与滥用边界不是「提高质量」；
2. **现状与未接入项**：用户要求「蜜罐只是后续接入」必须能被读到，所以留一段 5 行的阶段表，并在两处显式写明**蜜罐只做了接入架构、具体蜜罐接第三方、协议栈待专项调研**。

**取舍记录**（终端非交互，按 `grill_deck` 的降级形态打印编号 + 推荐答案后推进）：Q1 四层图**保留**但删掉其后的 ASCII 与对照表附录（图是「项目做什么」最省字的表达，附录是给开发者的对账信息，归 `docs/design/`）；
Q2 三条硬约束**压成一行**；Q3 使用边界**压缩保留**；Q4 状态与限制**合成一段**；Q5 目录结构/文档地图/门禁/参与开发**四节全删**。

---

## 3. 追溯矩阵

| 项 | 文档 | 代码/命令 | 验证 |
| --- | --- | --- | --- |
| README 只含介绍 + 启动 | `README.md`（126 行，原 318） | —— | 章节清单见 §5 |
| README 提到的 `make` 目标必须真存在 | `README.md` §3 | `Makefile` | 11 个目标逐个核对：全部 ✓（§6 ②） |
| 入口指针与实际内容一致 | [`docs/README.md`](../README.md) §0 | —— | 「效果什么样」→「怎么启动」；四层图说明改为「有同一视角的 mermaid 图」 |
| 链接不悬空 | `README.md` 全部链接 | `make trace` | ✅ 追溯检查通过 |

---

## 4. 代码/文件

| 文件 | 改什么 |
| --- | --- |
| `README.md` | 重写：**318 → 126 行**。保留标题 + 项目名未定提示 + §1 它做什么（含四层 mermaid 图 + 三条硬约束一行 + 使用边界四行）+ §2 现状与未接入项 + §3 项目启动（一键起全套 / 本地开发）+ 一行文档入口 |
| `docs/README.md` | §0 两处指针同步（入口 README 的内容边界变了；四层图的描述改为「文字版 ↔ mermaid 图」） |

**没有改**：任何代码、`docs/design/`、`AGENTS.md`。

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 |
| --- | --- | --- | --- |
| 1 | README 章节清单 | 只有「它做什么 / 现状 / 项目启动」 | ✅ 3 节（§1 · §2 · §3） |
| 2 | 是否残留实现细节 / 工程纪律 | 不得出现 | ✅ 目录结构 · 文档地图 · 门禁与追溯 · 参与开发 · 效果演示全删 |
| 3 | 蜜罐的表述 | 必须写成后续接入 | ✅ 图中「蜜罐层（后续接入）」+ §2 写明「蜜罐到此为止只做了接入架构…具体蜜罐接第三方」 |
| 4 | 使用边界 | 保留 | ✅ 压缩为 4 行表 |
| 5 | README 里的 `make` 目标 | 全部存在 | ✅ `start up check tools pyenv gate dev help run smoke analysis docker-ps docker-log down` 逐个核对 |
| 6 | 链接是否悬空 | 无悬空 | ✅ `make trace` 通过 |

**没有覆盖的**：GitHub 网页上的 mermaid 渲染只能在浏览器里看（本轮无法自动核）。

---

## 6. 验证证据

**① 门禁**

```console
$ make gate
门禁通过。          # 含 trace：悬空链接 / 规则 ID / 变更包与日志
```

**② README 的章节与行数**

```console
$ wc -l README.md
     126 README.md          # 原 318 行
$ grep -nE '^#{1,3} ' README.md
1:# Shen —— 面向自主渗透 Agent 的欺骗引擎
5:## 1. 它做什么
86:## 2. 现在能做什么、还没接什么
103:## 3. 项目启动
```

**③ README 里的 make 目标逐个核对**

```console
$ for t in start up check tools pyenv gate dev help run smoke analysis docker-ps docker-log down; do
    grep -qE "^$t:" Makefile && echo "$t ✓" || echo "$t ✗"; done
start ✓ up ✓ check ✓ tools ✓ pyenv ✓ gate ✓ dev ✓ help ✓ run ✓ smoke ✓ analysis ✓ docker-ps ✓ docker-log ✓ down ✓
```

---

## 7. 遗留

| # | 遗留 | 去向 |
| --- | --- | --- |
| 1 | mermaid 在 GitHub 上的渲染只能在浏览器里确认 | 人工看一眼仓库首页 |
| 2 | `docs/plans/` 里已有 12 份 2026-09-21 的实现计划未按 `ARCHIVE.md` 的约定归档（本轮同样未归档，保持现状一致） | 单独一轮做计划归档整理 |
| 3 | 删除的「引擎跑在请求路径上」行为表（实跑证据）只在 `docs/ops/` 有等价记录 | 已核：`functional-verification.md` 与 `adapter-proxy.md` 覆盖同一批行为 |

### 7.1 审视记录（S 档：按「删的是否还有别处权威版本」逐条核过）

| # | 发现 | 动作 |
| --- | --- | --- |
| 1 | `docs/README.md` 写着「想先知道**效果什么样**→ 看根 README」 | ✅ 改为「怎么启动」，并注明根 README 的边界 |
| 2 | `docs/README.md` 称 `../README.md` §1 是「同一张图」（图已换成保留的精简版） | ✅ 改为「同一视角的 mermaid 图，这里是文字版」 |
| 3 | `docs/kb/known-issues.md` 里「不要看 README 的演示描述」 | ✅ 核实：那句讲的是**第三方 pi 包**的 README，与本仓库 README 无关，不改 |
