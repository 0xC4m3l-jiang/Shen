# 变更包 · 2026-09-18 · 补欺骗引擎设计（诱饵面 + 蜜罐入口）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 补「如何实现欺骗」的设计：新增 `decoy`（诱饵面）与 `honeypot`（蜜罐入口）两个模块 + 两条 ADR + 契约扩展 |
| 日期 | 2026-09-18 |
| 状态 | 已实现（**设计轮**，不含代码） |
| 涉及模块 | `decoy`（新，[`../design/modules.md`](../design/modules.md) §1.1 第 23 行）· `honeypot`（新，第 24 行） |
| 决策数 | 已答 2 项（功能性伪装 / 蜜罐只做入口）/ 待定 6 项（见 §7） |
| 关联 | [ADR-0010](../background/decisions/0010-functional-camouflage.md) · [ADR-0011](../background/decisions/0011-honeypot-entry-external-backends.md) · [`../log.md`](../log.md) 同日条目 |

## 1. 需求与验收

**要解决什么**：本项目已实现「判别 → 决策 → 透明路由」，但**没有回答「改道之后拿什么骗」**。参考实现（AgentCapture）的 AI 欺骗引擎能力（功能性伪装诱饵面、Agent 指纹、归因令牌、会话级判别）在本设计中缺失。

**做完之后，用户能做什么 / 看到什么**：欺骗引擎有完整的**机制设计**——诱饵面五类形态、蜜罐入口与后端池、归因与判别增强的落点；蜜罐只做入口，具体蜜罐接第三方。

**验收判据**：

1. 新增 2 个模块（`decoy` / `honeypot`）进入 §1.1 权威清单，各有一份九章模块文档。
2. 2 条 ADR 记录候选、理由、后果、**失效条件**。
3. `config` 契约有 `decoys` / `honeypots` 段（开关）。
4. 蜜罐**不实现**具体类型，只登记类型清单 + config 开关。
5. `make gate` 全绿。

**不做什么**：

- **不写任何代码**（设计轮）。
- **不实现任何具体蜜罐**（[ADR-0011](../background/decisions/0011-honeypot-entry-external-backends.md)）。
- 不改 `config.example.yaml` / `policy` 校验（阶段 2b 实现时同步）。

## 2. 设计逻辑

**已确认的决策**（用户指示 + 行业调研）：

| # | 问题 | 决定 | 理由 | 落地 |
| --- | --- | --- | --- | --- |
| ① | 欺骗的核心机制是什么 | **功能性伪装**（伪装成站点合法功能），否决显式提示注入 | 对齐模型拒绝「服从」不拒绝「使用 API」；实测 0/8 服从 | [ADR-0010](../background/decisions/0010-functional-camouflage.md) |
| ② | 蜜罐怎么定位 | **只做入口与后端池**，具体蜜罐接第三方 | 核心价值在欺骗引擎；蜜罐是可替换件 | [ADR-0011](../background/decisions/0011-honeypot-entry-external-backends.md) |

**外部依据（调研）**：

| 来源 | 支撑了什么 |
| --- | --- |
| AgentCapture 逆向（`research/agentcapture-deception-report.md`） | 功能性伪装三阶段；令牌双角色；蜜饵 observe-only；会话级判别 |
| MITRE Engage（Matrix / Handbook） | 欺骗活动的目标分类（Expose / Affect / Elicit）与 Prepare 纪律 |
| GenPot / llmockapi / LLMApi | 「按 spec 生成可信响应」的工程可行性 |
| LLM Agent Honeypot（arXiv 2410.13919） | 蜜罐为检测目标的最小形态 |
| HoneyMCP 系列 | MCP 诱饵的形态 |
| Hive-AI | **间接提示注入**是采集链路的反向风险（登记为遗留） |

**仍未定**（不阻塞本轮设计）：

| # | 未决 | 去向 |
| --- | --- | --- |
| 1 | 诱饵多态与再生成机制 | [ADR-0010](../background/decisions/0010-functional-camouflage.md) 未解决 |
| 2 | 归因令牌 TTL 与轮换 | 同上 |
| 3 | 蜜罐接入契约形态 | [ADR-0011](../background/decisions/0011-honeypot-entry-external-backends.md) 未解决 |
| 4 | Agent 指纹库与置信度阶梯 | `judge` 模块文档（下轮） |
| 5 | 会话级有状态判别的存储 | `session` / `judge`（下轮） |
| 6 | `spec/decoy.md` / `spec/honeypot.md` 字段 | 实现前写 |

## 3. 文档对应（追溯矩阵）

本轮是设计轮，无代码，矩阵对齐「决策 → 规则 → 模块文档 → 契约」：

| 决策 / 规则 | 模块文档 | 契约 | 验证命令 |
| --- | --- | --- | --- |
| [ADR-0010](../background/decisions/0010-functional-camouflage.md) | [`../modules/decoy.md`](../modules/decoy.md) | [`../spec/config.md`](../spec/config.md) §2.8 | `make trace` |
| [ADR-0011](../background/decisions/0011-honeypot-entry-external-backends.md) | [`../modules/honeypot.md`](../modules/honeypot.md) | [`../spec/config.md`](../spec/config.md) §2.9 | `make trace` |
| `MD-25`（诱饵面 observe-only） | `decoy.md` §4 | —— | `make trace`（ID 存在性） |
| `MD-26`（蜜罐经 `honeypot` 管理） | `honeypot.md` §4 | —— | `make trace`（ID 存在性） |
| `MD-18`（模块清单） | [`../design/modules.md`](../design/modules.md) §1.1 | —— | `make archcheck` |

## 4. 文档实现（本轮的文件清单）

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `docs/background/decisions/0010-functional-camouflage.md` | 新增 | 欺骗核心机制的决策记录 |
| `docs/background/decisions/0011-honeypot-entry-external-backends.md` | 新增 | 蜜罐定位的决策记录 |
| `docs/background/decisions/README.md` | 改 | 登记 0010 / 0011 |
| `docs/design/modules.md` | 改 | §1.1 加 `decoy`(23) / `honeypot`(24)；新增 `MD-25` / `MD-26` |
| `docs/modules/decoy.md` | 新增 | 诱饵面模块（九章） |
| `docs/modules/honeypot.md` | 新增 | 蜜罐入口模块（九章） |
| `docs/spec/config.md` | 改 | §2.0 消费状态 + §2.8 `decoys` + §2.9 `honeypots` |
| `docs/progress.md` | 改 | §1 加 2 行；清单总数 24 行 / 23 有效 |
| `docs/modules/README.md` | 改 | §4.1 加 decoy / honeypot 两句 |
| `docs/design/structure.md` | 改 | §1.2 目录树加 `core/internal/decoy/` · `core/internal/honeypot/` |

## 5. 场景（设计覆盖度）

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | 诱饵面五类形态 | Developer API / 指令文件 / MCP / 数据集 / 蜜饵 均有设计 | ✅ | `decoy.md` §1 |
| 2 | 蜜罐只做入口 | 无任何具体蜜罐实现，只有类型清单 + 开关 | ✅ | `honeypot.md` §1 |
| 3 | 蜜罐类型可扩展不改代码 | 类型 = 配置项 | ✅ | `honeypot.md` §1 · `config.md` §2.9 |
| 4 | 诱饵面不阻断 | observe-only 作为规则 `MD-25` | ✅ | `modules.md` §2 |

## 6. 验证证据

```console
$ make trace
追溯检查通过。

$ make gate
门禁通过。
```

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | `judge` 的 Agent 指纹库与置信度阶梯未设计 | 判别精度 | 下轮（`judge.md` §扩充） |
| 2 | 会话级有状态判别（滑窗 / 时序）未设计 | 「单事件判别必然失效」 | 下轮（`session` / `judge`） |
| 3 | 归因令牌（蜜标 + 凭证水印）未设计 | 归因闭环 | 下轮（`session`） |
| 4 | 间接提示注入风险（Hive-AI） | 采集链路喂 LLM 会被反注入 | 登记待设计 |
| 5 | `spec/decoy.md` / `spec/honeypot.md` 字段 | 实现前的契约 | 实现时写 |

## 7.1 审视记录（L 档）

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `decoy.md` / `honeypot.md` 指向 `spec/README.md` | 悬空链接 | `make trace` 报 `TC-3` | 改为纯文本待建路径 | ✅ 归零 |
| 2 | `honeypot-protocol` / `honeypot-shell` 从「必做」变「可选自研」 | 定位变更 | ADR-0011 | 在 `modules.md` §1.1 保留原行；ADR 说明 | ✅ |
| 3 | `config.md` 加段但不改 `config.yaml`/schema | 有意延迟 | §2.8/§2.9 标注「阶段 2b 实现时加入」 | 明确写出，避免误以为已生效 | ✅ |

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 补欺骗引擎设计：新增 decoy / honeypot + 2 条 ADR + config 契约 | 用户指示 + 行业调研 |
