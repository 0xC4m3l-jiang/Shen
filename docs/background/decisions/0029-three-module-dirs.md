# 0029. 顶层目录与「三个大模块」对齐：`deception/` 接手 ①、`honeypot/` 接手 ②

- 状态：✅ **已采纳并执行**（2026-09-21，用户指令「按照这三个方向进行目录调整并进行代码优化」）
- 日期：2026-09-21
- 影响范围：**顶层目录本身** —— [`structure.md`](../../design/structure.md) §1.1 的白名单与目录树、§1.2–§1.6 的路径；
  [`modules.md`](../../design/modules.md) §1.1 的「源码目录」列（7 行）；`scripts/archcheck` 与 `scripts/check-leak`
  的路径前缀逻辑；`Makefile` · `deploy/` · `scripts/` · `.gitignore`；约 330 行文档路径
- 决策者：用户
- 依据：用户的三模块模型（欺骗层 / AI 蜜罐层 / 管控平台）与同轮指令 ·
  [ADR-0028](0028-three-module-view.md)（**先定「三模块是功能视角、`core/` 不拆散」**，本文执行目录名对齐）·
  [ADR-0007](0007-repo-layout.md)（**部分被本文取代**）· `ST-1`（顶层目录白名单）· `MD-21`（阶段未到的目录不预建）

---

## 背景

[ADR-0028](0028-three-module-view.md) 把「三个大模块」定为**功能视角**、并决定 `core/` 作为**共享内核**保留，
但**没动目录名**：当时 `deception/` 这个名字同时被两个含义占用（L2+L3 执行平面 / 用户口中的 ① 欺骗层），
而用户口中的 ① 在代码里叫 `edge/`。名字与口头说法**不一致**，就是这次要解决的事。

## 候选

| 候选 | 内容 | 真实优势 | 代价 |
| --- | --- | --- | --- |
| **A** | **只改两个顶层名**：`edge/` → `deception/`（① 欺骗层），`deception/honeypot/` → `honeypot/protocol/`（② AI 蜜罐层）；`core/` 与 `analysis/` **不改名**（采纳） | ① Go 侧只需改 **5 处 import**（`shen/edge/*`）；② `core/`(81 处) 的 import **全在自身内部**；`api/`(35 处) 有 11 处来自 `deception/` 与 `console/`，但**不改名就不受影响**；③ `analysis/` 的 Python 包名与 venv 完全不动；④ 名字直接来自用户口语 | 🟡 `analysis/` 仍是**跨 ①②**的一层（不属任何单一大模块，需在文档里写明）；🟡 约 330 行文档路径要跟着改 |
| **B** | **连内核也改名**：`core/` → `kernel/`；并把 `analysis/` 拆成 ①/② 两半 | 名字 100% 贴合三模块 + 内核 | 🔴 **~120 处 Go import** + **Python 包整体重命名**（`analysis.*` → 两个新包 + venv/pytest/ruff/Makefile 全跟着动）；🔴 收益只是名字，风险是整条 Python 工具链 |
| **C** | 不动目录，靠 [ADR-0028](0028-three-module-view.md) 的映射表 | 零风险 | 🔴 用户明确要求改名，且口头说法与目录长期不一致会持续造成误解 |

## 决定

**采纳 A**，并定下三条配套说明。

### 决定 1 · 新顶层：三个模块名 + 共享内核 + 跨层

```text
deception/   ← 原 edge/（proxy · mirror · dns · injection）+ 原 deception/netpolicy/   = ① 欺骗层
honeypot/    ← 原 deception/honeypot/ 变为 honeypot/protocol/（+ shell/ 未来）        = ② AI 蜜罐层
console/                                                                              = ③ 管控平台
core/        ← **不改名**：共享内核（判定与响应生成的唯一实现，不属于任何单个大模块）
analysis/    ← **不改名**：L4 分析（跨 ①②：意图 / 攻击链 / 策略 + AI 能力服务）
api/ deploy/ scripts/ docs/ vendor/ .pi/                                              = 契约与底座
```

`deception/` 为什么能同时装 ① 的 L1 适配器与 L3 网络欺骗：两者都是**对外欺骗的执行面**（一个改响应、一个改网络视图），
本就属于同一个大模块；`netpolicy` 因此**原地不动**（`deception/netpolicy/`）。

### 决定 2 · `honeypot/protocol/` 的命名对上了模块名

原 `deception/honeypot/` 就是模块 14 `honeypot-protocol`。改名后 `honeypot/` 这一层：
`protocol/`（协议仿真，已建框架）· `shell/`（假 shell，**未建**，`MD-21` 不预建）·
将来 `orchestration/`（蜜罐**编排**，见 [ADR-0028](0028-three-module-view.md) 决定 4 —— 归属已划给 `core/internal/honeypot`，实现面另开 ADR）。

### 决定 3 · 历史文档**不追改**（它们是快照）

`docs/plans/` · `docs/background/` · `docs/log.md` 里出现旧路径（`edge/proxy` 等）**保持原样**：
它们记录的是**当时**的仓库形状。这在追溯检查里也是既定口径（这三类目录不参与悬空链接与过期标记检查）。
`docs/design/` · `docs/modules/` · `docs/spec/` · `docs/ops/` · `docs/integrate/` 与根 `README.md`
属**活文档，已全部更新**；`docs/kb/` 在追溯检查里按**沉淀**跳过，本轮也一并更新了（但它不享受
「活文档必须同步」这条约束 —— 口径不要混）。

## 理由

**为什么是 A 而不是 B**：B 的最强优点是「名字 100% 贴合」。而 A 用**两个顶层名 + 一张映射表**就把它做到 95%：
用户说的三个模块 `deception/` `honeypot/` `console/` 现在**都是顶层目录名**；
剩下的 `core/` 与 `analysis/` 是**共享内核**与**跨层**，它们本来就不属于任何单个大模块 —— 不改名反而是**正确**的语义，
不是妥协。[ADR-0028](0028-three-module-view.md) 已经论证过「核心必须只有一个」。

**代价对比（实测数）**：A = Go 5 处 import + 12 个脚本/物料文件 + ~330 行文档；
B = 额外 ~120 处 import + Python 包重命名（`analysis.*` → 两个包，venv / pytest / ruff / Makefile 全动）。

## 后果

**正面**：

- 口头说法与目录名对齐：① 欺骗层 = `deception/` · ② AI 蜜罐层 = `honeypot/` · ③ 管控平台 = `console/`；
- Go 侧改动面极小（5 处 import），**门禁（`archcheck`/`trace`）能兜住**改名引入的断链与清单不一致；
- `honeypot/` 的下一层直接对应模块名（`protocol/` `shell/`），为**蜜罐编排**预留了 `orchestration/` 的位置；
- `analysis/` 的 Python 工具链（venv · pyproject · 3 个运行期依赖）**一行未动**。

**负面 / 代价**：

- **顶层目录白名单变了**（`structure.md` §1.1）—— 这是已确认基线，本次由用户指令授权修改；
- `deception/` 这个名字的含义**变了**（旧：L2+L3；新：① 欺骗层含 L1+L3）——
  读旧文档（`docs/plans/` `docs/background/`）时要注意这个词义漂移；
- 约 330 行文档路径改动，且**机械替换会留下语义痕迹**（本次实际留下 6 处：重复的目录树行、
  仍写 `edge` 的映射句、`均已建` 重复项等）—— 必须人工复核，门禁抓不到这一类。

**引入的新风险**：

- `docs/plans/` 等历史文档里的路径**现在指向不存在的目录**（刻意保留的快照）⇒ 新人若从旧变更包里的路径去找代码会找空。
  缓解：活文档 `_map.md` §1 给出「当前目录 ↔ 模块」的唯一权威映射；`structure.md` §1.1 是白名单权威。
- `.gitignore` / Dockerfile / compose / 脚本里的路径已同步（否则构建或门禁会红）。

**被排除掉的可能性**：`core/` 改名 `kernel/`（失效条件 1）；`analysis/` 拆成 ①/② 两半（未解决 1）。

## 失效条件

1. **`core/` 必须改名**（例如对外交付要求目录名不含「核心」这类内部词）⇒ B 进入比较：
   要额外付 ~120 处 import + Python 包重命名的代价，本 ADR 的决定 1 需改写；
2. **`analysis/` 的跨层性成为实际障碍**（例如 ① 与 ② 需要独立的依赖集 / 独立发版）⇒ 拆包进入比较（未解决 1）；
3. **`deception/` 的词义漂移造成真实误解**（例如接入方按旧含义理解）⇒ 需要更强的命名（如 `deception-l1l3/`）或回退；
4. **`honeypot/` 下出现第三个不属蜜罐的模块** ⇒ 说明「按大模块收拢」的假设不成立，需要重新划界。

**复查日期**：2026-12-21（或蜜罐编排立项时，以先到者为准）。

## 未解决

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | **`analysis/` 是否拆成 ①（`aicap`）与 ②（分析四件套）两半** | 依赖集与发版的独立性 | 失效条件 2；需要时单独 ADR |
| 2 | **蜜罐编排的目录位置与 ADR**（`honeypot/orchestration/`？能力归属仍在 `core/internal/honeypot`） | 起容器/进程的实现面 | [ADR-0028](0028-three-module-view.md) 未解决 3 |
| 3 | 历史文档里的旧路径是否要加「路径已迁移」横幅 | 新人读旧变更包时的困惑 | 本轮刻意不加（会改动大量历史文件）；若实际造成困扰再加 |
| 4 | `deception/` 一词的旧义（L2+L3）在旧文档中的残留 | 词义漂移 | 同上 |
| 5 | **`check-leak` 新增的「import 路径」规则级豁免类在 `docs/design/constraints.md` 的 `OH-2` 位置表里没有落点** —— 检查器已按「编译期标识符不上攻击者的屏幕」排除，但该理由尚未写进已确认基线（改它需用户确认，见 `AGENTS.md` §3） | 基线与本轮的检查器精度之间有一处口径差 | 提议给 `OH-2` 表补一行「编译期标识符（import 路径）」；**待确认** |
