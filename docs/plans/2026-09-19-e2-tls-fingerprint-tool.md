# 变更包：`E2`（TLS 指纹一致性）从「待执行实验」变成「可复现工具 + 判定口径」+ 主臂初测

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 交付 `scripts/fingerprint`（采集 + 对比 + 判定）；对真实站与我方栈做**主臂初测**并留存原始数据 |
| 日期 | 2026-09-19 |
| 状态 | 已验证（10 例单测 + 真跑两次采集 + 对比出结论；`make gate` 通过；已提交） |
| 改动分级 | **M**（新增实验工具与原始数据；**不改产品代码、不改规则**） |
| 涉及 | 新增 `scripts/fingerprint/`（工具 + 单测 + README）· `Makefile`（`fp-capture` / `fp-diff`）· [`../background/notes/pending-experiments.md`](../background/notes/pending-experiments.md)（E2 结果段）· [`../background/research/README.md`](../background/research/README.md) + `research/e2-tls-fingerprint/`（原始数据） |
| 决策数 | 已答 3 项（工具口径）· **待用户裁决 1 项**（TLS 终结归属，见 §7） |
| 关联 | 威胁模型 `A2` · `E2`（P0）· [ADR-0017](../background/decisions/0017-caddy-l1-base.md) · `INT-22` |

---

## 1. 需求与验收

**要解决什么**：`E2` 是唯一一条「结论可能推翻整个欺骗命题」的验证（`A2` 不可区分性），却一直停在
「设计」状态 —— 没有工具、没有判定口径、没有数据。上一轮把它定为 **P0**，本轮把它变成**可复现的东西**。

**做完之后**：任何人用两条命令就能采集与对比（`make fp-capture` / `make fp-diff`），
结论由代码判定（不是人解释），原始数据留存可比对，**最坏结论（不可对齐）用退出码 2 拦住后续动作**。

**验收判据**：

1. 采集覆盖：协商版本 · 套件 · ALPN · 会话恢复 · OCSP · **ServerHello 扩展序列** · ClientHello 形状 · 证书链形状。
2. 对比由代码判定三种结论（不可区分 / 可区分可调齐 / 不可对齐），判定标准与 `E2` 文档一致且写死在 `diff.go`。
3. 单测覆盖解析（含**截断容错**）、JA3/JA3S 口径差异（GREASE 过滤）、三种判定。
4. **主臂初测**跑通，原始数据进仓库（`research/e2-tls-fingerprint/`），结论与局限写进 `pending-experiments.md`。
5. 退出码：`0` 可继续；`2` 不可对齐。
6. `make gate` 绿；本轮已提交。

**不做什么**：不改产品代码 · 不改规则 · 不引入第三方指纹库 · 不做 JA4（见 §7）· 不把单站单次结果当最终结论。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | 用现成指纹库还是自己解析？ | **自写握手解析**（Go 标准库 + 约 200 行解析） | 避免「用别人的口径解释自己的结论」；且我们要的字段（扩展顺序）多数库不暴露 |
| ② | 输出 JA3/JA3S 的哈希还是原文？ | **原文**（`ja3_line` / `ja3s_line`） | 比对要看「差在哪一项」，哈希只说「不一样」；顺带避开弱哈希规则（规范要求 MD5，但我们可以不算）|
| ③ | 判定逻辑放哪？ | 写死在 `diff.go`，并把每条的后续动作写进报告 | `E2` 的判定标准**不可事后调整**，所以不能靠人解释 |
| ④ | 怎么表达最坏结论？ | 退出码 `2` | 让脚本/CI 能机械地拦住后续动作（而不是靠人记得） |

**采集方式**：把 TCP 连接包一层双向录制 —— Go 的 `tls.ConnectionState` 不暴露扩展序列，
而扩展的「有哪些 + 什么顺序」正是 JA3S 的核心，也是最能一眼辨认的地方。

---

## 3. 追溯矩阵

| 规则 / 依据 | 文档位置 | 代码 | 测试 | 验证命令 |
| --- | --- | --- | --- | --- |
| `A2`（不可区分性是唯一命题） | [`../background/notes/threat-model.md`](../background/notes/threat-model.md) | `scripts/fingerprint/` 全体 | `TestDiff*`（三种结论） | `make fp-diff` |
| `E2`（判定标准不可事后调整） | [`../background/notes/pending-experiments.md`](../background/notes/pending-experiments.md) | `diff.go` 的 `Diff` | 同上 | 同上 |
| `ADR-0017`（Caddy 终结 TLS） | [`../background/decisions/0017-caddy-l1-base.md`](../background/decisions/0017-caddy-l1-base.md) | —— （本轮结论构成其**重估输入**） | —— | §7 待裁决 |
| `INT-22`（TLS 可读才允许误导处置） | [`../design/integration.md`](../design/integration.md) | —— （候选出路 ② 仍满足它） | —— | 设计评审 |
| `SB-6`（引擎禁止主动连接） | [`../design/constraints.md`](../design/constraints.md) | 工具是**离线实验工具**，不是引擎；采集目标是运维指定的站点 | —— | 代码评审 |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `scripts/fingerprint/main.go` | 新增 | CLI：`capture`（采集）/ `diff`（对比）；结果 JSON 含元信息（可复现） |
| `scripts/fingerprint/tls.go` | 新增 | 双向录制连接 · 握手消息抽取（含截断容错）· ClientHello/ServerHello 解析 · JA3/JA3S 原文 · 证书形状摘要 |
| `scripts/fingerprint/diff.go` | 新增 | 差异分类（可配置 / 本质 / 仅证书）+ 三种结论 + 报告渲染（含每条的后续动作） |
| `scripts/fingerprint/fingerprint_test.go` | 新增 | 10 例：解析（客户端/服务端）· 截断容错 · JA3/JA3S 口径 · GREASE 过滤 · 三种判定 |
| `scripts/fingerprint/README.md` | 新增 | 为什么它最重要 · 用法 · 采什么 · 三种结论 · **它不保证什么** |
| `Makefile` | 改 | `fp-capture` / `fp-diff`（含参数校验；缺参数直接失败） |
| [`../background/notes/pending-experiments.md`](../background/notes/pending-experiments.md) | 改 | 新增 `E2` 结果段（方法 · 数据 · 结论 · **局限** · 建议动作）+ 更新待办状态 |
| [`../background/research/README.md`](../background/research/README.md) + `research/e2-tls-fingerprint/` | 新增 | 原始数据入仓并登记（证据级 A：工具可复现） |

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | 解析 ClientHello | 版本/套件/扩展顺序/曲线 正确 | ✅ | `TestParseClientHello` |
| 2 | 解析 ServerHello | 扩展顺序原样 | ✅ | `TestParseServerHello` |
| 3 | 录到半条记录 | 按可用字节解析、不报错 | ✅ | `TestParseToleratesTruncatedTail`（该测试**先失败后修**：原实现遇截断直接放弃） |
| 4 | JA3 / JA3S 原文 | 首段 771 / 772，套件与扩展序列正确 | ✅ | `TestJA3AndJA3SLines` |
| 5 | GREASE 口径 | JA3 不过滤、JA3S 过滤 | ✅ | `TestGREASEFilterOnlyForJA3S` |
| 6-8 | 三种判定 | 全一致→不可区分；只差可配置→可调齐；扩展顺序不同→不可对齐 | ✅ | `TestDiffIndistinguishable` / `TestDiffConfigurableOnly` / `TestDiffCannotAlignOnExtensionOrder` |
| 9 | 仅证书差异 | 仍判「不可区分」 | ✅ | `TestDiffCertOnlyIsStillIndistinguishable` |
| 10 | **真跑：采真实站** | 出 JSON + JA3S | ✅ | `example.com:443` → TLS 1.3 · `JA3S: 772,4865,51-43` |
| 11 | **真跑：采我方栈** | 出 JSON + JA3S | ✅ | 本机 Caddy（自签）→ TLS 1.3 · `JA3S: 772,4865,43-51` |
| 12 | **真跑：对比** | 出结论与后续动作 | ✅ | `不可对齐` + 三条候选出路；退出码 `2` |

**没有覆盖的**：多次采样与稳定性 · JA4 · 客户真实站（见 §7）。

---

## 6. 验证证据

```console
$ make fp-capture ADDR=example.com:443 OUT=/tmp/fp/real.json
已写入 /tmp/fp/real.json（TLS TLS 1.3 · TLS_AES_128_GCM_SHA256 · ALPN ""）
	JA3S: 772,4865,51-43

$ make fp-capture ADDR=127.0.0.1:18443 SNI=shop.example.com OUT=/tmp/fp/ours.json
已写入 /tmp/fp/ours.json（TLS TLS 1.3 · TLS_AES_128_GCM_SHA256 · ALPN ""）
	JA3S: 772,4865,43-51

$ make fp-diff A=/tmp/fp/real.json B=/tmp/fp/ours.json
差异：
  · [本质差异] 服务端扩展序列   A: 51-43   B: 43-51
  · [可配置]   OCSP 装订        A: 支持    B: 不支持
  · [仅证书]   证书链形状        A: 公有链·链长4·ECDSA   B: 自签·链长1·RSA
一致：ALPN · TLS 协商版本 · 会话票据/恢复 · 选用的密码套件
结论：不可对齐（威胁模型 R-1 必须重估）        # 退出码 2

$ go test ./scripts/fingerprint/ -count=1
ok  shen/scripts/fingerprint   0.435s      # 10 例
```

**关键指标**：新增工具 **1 个**（3 个源文件 + 10 例单测）· 新增 Make 目标 **2 个** · 原始数据入库 **2 份** ·
产品代码改动 **0 行** · 规则改动 **0 条** · `E2` 从「设计」推进到「主臂初测有结论」。

---

## 7. 遗留与未决（含**待你裁决**项）

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | ⚠️ **TLS 终结归属需重估（待你裁决）**：主臂初测显示「我方内嵌 Caddy 终结 TLS」与公有站栈在 **ServerHello 扩展顺序**上可区分（JA3S 不同）。<br>候选：① **默认交客户 L0 终结**（客户 L0 = 真实站同款栈 ⇒ 指纹天然一致，且仍满足 `INT-22`）；② 保持自终结但接受可区分；③ 换与真实站同款的 TLS 实现 | 直接关系 `A2`（唯一命题）与 [`ADR-0017`](../background/decisions/0017-caddy-l1-base.md) 的成立 | 你裁决后开 ADR-0017 重估（或新增 ADR） |
| 2 | **A 组只采了 1 个公有站、1 次** | 结论对「客户站栈是否与我方同款」高度敏感 | 对客户真实站重复 ≥3 次 |
| 3 | **JA4 / JA4S 未做** | 新出现的指纹口径（JA4 已过滤 GREASE、含 ALPN 等） | 需要时再扩展（解析层已就绪） |
| 4 | 未采「我方→上游」方向的实测（`client_*` 已能采，未做真实对比） | 幻境后端/业务侧看到我们的 ClientHello 指纹 | 需要时补 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `E2` 长期停在「设计」而它是 P0 ——**没有工具就没有结论**，风险靠争论解决 | 实验缺口 | `pending-experiments.md` 待办状态 | 交付工具 + 判定口径 + 主臂初测 | ✅ |
| 2 | 初版解析器遇**截断字节直接放弃**（测试暴露） | 缺陷 | `TestParseToleratesTruncatedTail` 首次运行失败 | 改为「按可用字节解析」并写明这是取证工具的刻意取舍 | ✅ |
| 3 | 初版用 MD5 算 JA3/JA3S 哈希（规范如此），触发弱哈希规则 | 设计选择 | 静态检查告警 | 改为输出**规范原文**（比对更实用，顺带避开）| ✅ |
| 4 | `Makefile` 新增目标若用跨行 `if` 会被 shellcheck 判解析失败（此前踩过） | 前车之鉴 | `SC1089` | 一律写单行守卫 | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 交付 `E2` 工具（采集/对比/判定 · 10 例单测）· Makefile 两个入口 · 主臂初测：**不可对齐**（ServerHello 扩展顺序 `51-43` vs `43-51`）· 原始数据与结论入仓 · 提出「TLS 终结归属」待裁决 | 威胁模型 `A2` · `E2`（P0）· `INT-22` |
