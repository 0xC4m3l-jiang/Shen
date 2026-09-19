# 变更包 · 2026-09-18 · 补判别层设计（指纹 + 会话级判别 + 归因令牌）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 补判别层设计：`judge` 的 Agent 指纹与会话特征消费 · `session` 的会话状态与归因令牌 · 2 条 ADR · config 扩展 |
| 日期 | 2026-09-18 |
| 状态 | 已实现（**设计轮**，不含代码） |
| 涉及模块 | `judge`（扩充）· `session`（扩充） |
| 决策数 | 已答 2 项 / 待定 6 项（见 §7） |
| 关联 | [ADR-0012](../background/decisions/0012-session-level-judgement.md) · [ADR-0013](../background/decisions/0013-attribution-token.md) · 上一轮 [`2026-09-18-deception-engine-design.md`](2026-09-18-deception-engine-design.md) · [`../log.md`](../log.md) 同日条目 |

## 1. 需求与验收

**要解决什么**：上一轮补齐了「拿什么骗」（诱饵面/蜜罐入口），但**判别层**仍缺三项 —— Agent 指纹、会话级有状态判别、归因令牌。这三项是参考实现里判别精度与归因闭环的关键。

**做完之后，用户能做什么 / 看到什么**：判别层有完整机制设计：指纹识别产出「是什么 Agent + 置信度 + 证据链」；会话级特征注入使 `judge` 保持纯函数；归因令牌让跨面溯源成立。

**验收判据**：

1. `judge` 文档含指纹（置信度阶梯 + 证据链）与会话特征消费。
2. `session` 文档含会话状态抽象与归因令牌（蜜标 + 水印）。
3. 2 条 ADR 记录候选、理由、后果、**失效条件**。
4. `config` 有 `fingerprints` / `attribution` 段。
5. `make gate` 全绿。

**不做什么**：

- 不写代码（设计轮）。
- 不照搬参考实现的签名表（AGPL-3.0，许可风险）——只借鉴方法，自行整理。
- 不改 `config.example.yaml` / `policy` 校验（阶段 2b 实现时同步）。

## 2. 设计逻辑

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地 |
| --- | --- | --- | --- | --- |
| ① | 会话级状态放哪 | **`session` 计算特征，注入 `judge`**；`judge` 保持纯函数 | 保住可回放；满足 `AR-9`（状态外置）+ `MD-6`（时钟注入） | [ADR-0012](../background/decisions/0012-session-level-judgement.md) |
| ② | 怎么归因 | **HMAC 蜜标（双角色）+ 凭证水印**，必须带 TTL 与闭环校验 | 归因内生于工作流；修正参考实现的两处缺陷 | [ADR-0013](../background/decisions/0013-attribution-token.md) |
| ③ | 指纹放哪 | `judge` 的子能力（不新开模块） | `AR-2`：判定一处；指纹是判定的一部分 | [`../modules/judge.md`](../modules/judge.md) §4.1 |

**关键取舍**（为什么 `judge` 不持有状态）：

> `D0` 推论 2 要求「路由首跳确定，判别可累积用于加重」。若 `judge` 持有进程内滑窗，
> 多副本会判定不一致（同一会话打到不同副本得到不同结论），且破坏可回放。
> 因此状态外置到 `store`，会话特征**随输入进来**。

**仍未定**（不阻塞本轮）：

| # | 未决 | 去向 |
| --- | --- | --- |
| 1 | 令牌 TTL 具体值 | [ADR-0013](../background/decisions/0013-attribution-token.md) 未解决 |
| 2 | 凭证水印编码方式 | 同上 |
| 3 | 会话状态存储（本地 LRU vs Redis） | [ADR-0012](../background/decisions/0012-session-level-judgement.md)（倾向 Redis） |
| 4 | 特征窗口时长（60s / 300s） | 实测 |
| 5 | 指纹签名库字段与来源 | `spec/`（实现前写） |
| 6 | `contract.SessionFeatures` 的最终字段 | 实现前定 |

## 3. 文档对应（追溯矩阵）

| 决策 / 规则 | 模块文档 | 契约 | 验证命令 |
| --- | --- | --- | --- |
| [ADR-0010](../background/decisions/0010-functional-camouflage.md) | [`../modules/judge.md`](../modules/judge.md) §4.1（指纹） | [`../spec/config.md`](../spec/config.md) §2.10 | `make trace` |
| [ADR-0012](../background/decisions/0012-session-level-judgement.md) | `judge.md` §1/§5 · [`../modules/session.md`](../modules/session.md) §1/§5 | —— | `make trace` |
| [ADR-0013](../background/decisions/0013-attribution-token.md) | `session.md` §1/§4 | [`../spec/config.md`](../spec/config.md) §2.11 | `make trace` |
| `AR-9` / `MD-6` | `judge.md` §5（无状态）· `session.md` §5 | —— | `make trace`（ID 存在性） |
| `INT-19` / `INT-20` / `NI-9` / `ST-21` | `session.md` §4 | —— | `make trace` |

## 4. 文档实现（本轮文件清单）

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `docs/background/decisions/0012-session-level-judgement.md` | 新增 | 会话级判别的落点（状态外置 vs 进程内） |
| `docs/background/decisions/0013-attribution-token.md` | 新增 | 归因令牌（蜜标双角色 + 水印） |
| `docs/background/decisions/README.md` | 改 | 登记 0012 / 0013 |
| `docs/modules/judge.md` | 改 | §1 职责 · §2 输入（`SessionFeatures`）· §4.1 指纹置信度阶梯 · §5 无状态说明 · §8 未决 |
| `docs/modules/session.md` | 改 | §1 职责（会话状态 + 归因令牌）· §2 输出 · §3 依赖（`StateSource`）· §4 规则 · §5 状态 · §8 未决 |
| `docs/spec/config.md` | 改 | §2.0 消费状态 + §2.10 `fingerprints` + §2.11 `attribution` |

## 5. 场景（设计覆盖度）

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | Agent 指纹 | UA/头签名 + 置信度阶梯 + 证据链 | ✅ | `judge.md` §4.1 |
| 2 | 会话级判别 | 特征注入，`judge` 仍纯函数 | ✅ | `judge.md` §5 · ADR-0012 |
| 3 | 归因令牌 | 双角色 + 水印 + TTL + 闭环校验 | ✅ | ADR-0013 |
| 4 | 不照搬 AGPL 签名表 | 明确「自行整理」 | ✅ | `judge.md` §4.1 |
| 5 | 无新 cookie | 保持 `NI-9` | ✅ | `session.md` §4 |

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
| 1 | 间接提示注入风险（采集链路喂 LLM 被反注入，Hive-AI） | 分析管线安全 | 登记待设计，落入 `llm-components` / `chain` |
| 2 | 诱饵多态与再生成机制 | 对抗众包识破 | [ADR-0010](../background/decisions/0010-functional-camouflage.md) 未解决 |
| 3 | `responder` 的生成式欺骗响应设计 | 假数据可信度 | 下轮（`responder.md`） |
| 4 | `spec/fingerprints.md` / `spec/attribution.md` 字段 | 实现前契约 | 实现时写 |

## 7.1 审视记录（L 档）

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `judge.md` 原写「本模块无状态」但未说明会话级信息从哪来 | 缺口 | `judge.md` §5 | 补「会话特征随输入进来」并指 ADR-0012 | ✅ |
| 2 | `session.md` 原写「不落库」但新增会话状态需要存储 | 边界调整 | `session.md` §1 | 明确「只依赖 `StateSource` 接口，不依赖 `store` 具体类型」 | ✅ |
| 3 | 参考实现的签名表被其许可约束（AGPL） | 许可风险 | 逆向报告 §8.3 | 在 `judge.md` §4.1 写明「自行整理」 | ✅ |

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 补判别层设计：指纹 + 会话级判别 + 归因令牌 + 2 条 ADR | 用户指示 + 调研 |
