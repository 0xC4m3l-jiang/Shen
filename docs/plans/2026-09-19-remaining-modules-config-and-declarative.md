# 变更包：`adapter-dns` 配置收口 · `netpolicy` 声明式产物 · `console` 模块落地

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 按设计把三个「非代码或声明式」模块落到可用状态：`adapter-dns`（纯配置）· `netpolicy`（声明式三份产物）· `console`（观测控制台实现），并登记控制台的语言偏离 |
| 日期 | 2026-09-19 |
| 状态 | 已验证（配置/YAML 可解析 · 控制台端到端已实跑 · `make gate` 通过） |
| 改动分级 | **M**（新增配置与声明式产物 + 文档；不改对外契约） |
| 涉及模块 | `adapter-dns`（§1.1 第 13 行）· `netpolicy`（第 16 行）· `console`（第 21 行） |
| 决策数 | 已答 1 项（控制台实现形态 → [ADR-0020](../background/decisions/0020-console-minimal-static-ui.md)）/ 待定 3 项 |
| 关联 | [ADR-0020](../background/decisions/0020-console-minimal-static-ui.md) · [`../integrate/observability.md`](../integrate/observability.md) · 上一轮 [`2026-09-19-observability-and-console.md`](2026-09-19-observability-and-console.md) |

---

## 1. 需求与验收

**要解决什么**：目标要求「**按设计完成所有模块**（除细节蜜罐）」。盘点后剩下的三类是：
① `adapter-dns` —— 设计明确「**不产生源码**」（`AR-3` 禁止自研 DNS），但此前只有 Corefile 模板、**缺生效/回退/校验说明**；
② `netpolicy` —— 设计为**声明式 + 复用 Cilium/Tetragon**，此前**产物为空**；
③ `console` —— 设计为控制台模块，此前**无实现**（上一轮已交付观测读路径与控制台本体，本轮补文档与偏离登记）。

**验收判据**：

1. `adapter-dns`：README 写清**怎么用 / 怎么确认分流生效（两条 `dig`）/ 怎么回退（`NI-11`）/ 配错影响面**；模块文档状态改准。
2. `netpolicy`：三份声明式产物（微隔离 / 假拓扑 / 运行时检测）**可被 YAML 解析**，命名沿用行业惯例（不出现自曝名，`OH-1`），并把「只观察不阻断」写进模板（与 `MD-25` 同精神）。
3. `console`：模块文档状态/测试/偏离登记齐备；[ADR-0020](../background/decisions/0020-console-minimal-static-ui.md) 记录「Go + 静态页」的取舍与**失效条件**。
4. `make gate` 绿。

**不做什么**：不接真实 Cilium/Tetragon（本层复用现成组件，产物即交付）· 不写 `honeypot-shell` 与蜜罐内容（**目标明确排除**）· 不改规则正文。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | `adapter-dns` 要不要写代码 | **不写**（保持纯配置） | `AR-3` 禁止自研 DNS；交付物是模板 + 运维步骤 |
| ② | `netpolicy` 的「完成」是什么 | **三份可加载的声明式产物 + 事件回流约定** | 数据面复用 Cilium/Tetragon；本项目不写 eBPF（`TB-24` 禁 CGO） |
| ③ | 控制台的语言 | **Go + 静态页**（偏离设计，登记 [ADR-0020](../background/decisions/0020-console-minimal-static-ui.md)） | 用户要求「不要太复杂」且裁定不引前端工具链；`TB-15` 要求语言必须有门禁 |

---

## 3. 追溯矩阵

| 规则 ID | 文档 | 代码 / 产物 | 验证 |
| --- | --- | --- | --- |
| `AR-3`（L0/DNS 禁止自研） | [`../modules/adapter-dns.md`](../modules/adapter-dns.md) §1 | `edge/dns/config/Corefile.example`（纯配置） | 两条 `dig` 校验（README） |
| `NI-11`（移除一段配置即可回退） | [`../../edge/dns/README.md`](../../edge/dns/README.md) | 同上（TTL 30s + 回退开关注释） | 文档评审 |
| `SB-5`（禁止向业务之外投递/连接） | [`../../deception/netpolicy/README.md`](../../deception/netpolicy/README.md) | `microsegmentation.example.yaml`（默认拒绝东西向 + 仅允许幻网内） | YAML 解析 + 评审 |
| `OH-1`（自曝名禁止出现在对手可见处） | 同上 | `fake-topology.example.yaml`（用 `gitlab` / `jenkins` / `db-01` 等惯例名） | `make leakcheck` |
| `MD-25`（诱饵面 observe-only） | 同上 | `runtime-detect.example.yaml`（`action: Post` 只观察不阻断） | 模板注释 + 评审 |
| `AR-10`（控制面不参与判定） | [`../modules/console.md`](../modules/console.md) §1 | `console/cmd/console`（只读接口） | 代码评审 |
| `TB-20` / `TB-15`（语言与门禁） | [ADR-0020](../background/decisions/0020-console-minimal-static-ui.md) | 控制台 Go 实现 + 偏离登记 | `make trace` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `edge/dns/README.md` | 重写 | 补「怎么用 / 两条 `dig` 校验 / 回退（`NI-11`）/ 配错影响面」——此前只有 Corefile 与模板注释 |
| `deception/netpolicy/README.md` | 新增 | 三件能力（微隔离 / 假拓扑 / 运行时检测）的产物与执行者、边界（`AR-2` / `SB-5` / `ST-16`）、事件回流约定 |
| `deception/netpolicy/config/{microsegmentation,fake-topology,runtime-detect}.example.yaml` | 新增 | 可直接 `kubectl apply` 的声明式产物 |
| `docs/modules/{adapter-dns,netpolicy,console}.md` | 改 | 状态改准 + §7 测试方式（含「无源码模块怎么验」）+ console 的偏离登记 |
| `docs/background/decisions/0020-console-minimal-static-ui.md` | 新增 | 控制台实现形态的决策与**失效条件** |
| `docs/background/decisions/README.md` · `docs/progress.md` · `README.md` | 改 | 决策索引 · 三个模块行 · 根 README 增「起环境/看观测/接入/人工测试」四个入口 |

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 |
| --- | --- | --- | --- |
| 1 | netpolicy 三份 YAML 可解析 | `yaml.safe_load_all` 通过、`kind` 正确 | ✅ `CiliumNetworkPolicy`×2 · `Service`×3 · `TracingPolicy`×1 |
| 2 | 模板不含自曝名 | 行业惯例命名 | ✅ `make leakcheck` |
| 3 | 控制台端到端（上一轮已跑） | 页面/接口可见分值与信号 | ✅ `score=0.90 signals=[ua-headless,path-git]` |
| 4 | `make gate` | 绿 | ✅ |

**没有覆盖的**：真实集群加载（需 Cilium/Tetragon）· `adapter-dns` 的真实 DNS 解析验证（需接入演练环境）。

---

## 6. 验证证据

```console
$ python3 -c "yaml.safe_load_all(...)"      # 三份模板均可解析
$ make gate
门禁通过。
```

**关键指标**：新增声明式产物 **3 份** · 新增/重写文档 **6 份** · 新增 ADR **1 份** · 代码改动 **0 行**（本批为配置与文档）。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | **`analysis/*` 四个模块仍未实现**（`intent` / `chain` / `strategy` / `llm-components`，设计为 **Python**） | 目标「完成所有模块」的最后一块 | 需先定 Python 门禁（用户此前裁定不引入 → 冲突待裁） |
| 2 | netpolicy 未接真实 Cilium/Tetragon | 运行时事件尚未回流 | 接入演练 |
| 3 | 控制台无鉴权、单实例 | 生产化需定 | [ADR-0020](../background/decisions/0020-console-minimal-static-ui.md) §未解决 |
| 4 | 蜜罐细节（`honeypot-shell` / 协议栈内容） | **目标明确排除** | 保持推迟 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `adapter-dns` 的「完成」此前没有判据（只有模板） | 漏写（无验收方式） | 模块文档 §7 为空 | 补「两条 `dig`」为验收方式 | ✅ |
| 2 | `netpolicy` 在 `progress.md` 标记 ⏳，与「声明式产物已就绪」不符 | 过期状态 | 本轮产物 | 改为 🟡 并写明「无源码」 | ✅ |
| 3 | `console` 语言与设计不符（TypeScript → Go+静态页） | 偏离未登记 | `language.md` §1 控制台行 | 新增 [ADR-0020](../background/decisions/0020-console-minimal-static-ui.md) 并在模块文档 §7 注明 | ✅ |
| 4 | 我写的 YAML 注释有两处超 80 列、且一次补丁用了嵌套 heredoc 导致 shell 解析错乱 | 自伤（2 次） | 静态检查告警 + 命令输出 | 折行修复；改用 `write` 工具落盘并核实 git 状态 | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | `adapter-dns` 配置收口（生效/回退/校验说明）· `netpolicy` 三份声明式产物 · `console` 模块文档与偏离登记（[ADR-0020](../background/decisions/0020-console-minimal-static-ui.md)）· 进度与根 README 同步 | 目标「按设计完成所有模块（除蜜罐细节）」· `AR-3` · `NI-11` · `SB-5` · `OH-1` · `MD-25` · `AR-10` |
