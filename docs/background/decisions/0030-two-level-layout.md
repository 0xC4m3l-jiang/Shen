# 0030. 顶层收成两级：`modules/`（产品功能模块）+ `common/`（公用代码）

- 状态：✅ **已采纳并执行**（2026-09-21，用户指令「将三个核心模块的根目录放到根目录下的一个目录下；
  公用代码模块也要有目录区分；先帮我调整整体项目结构」）
- 日期：2026-09-21
- 影响范围：**顶层目录本身** —— [`structure.md`](../../design/structure.md) §1.1 的白名单与目录树、§1.2–§1.7；
  [`modules.md`](../../design/modules.md) §1.1 的「源码目录」列（24 行）；`scripts/archcheck` 的平面解析
  （`planeOf` 从「取第一段」改为**感知容器**的两级解析）；`scripts/check-leak` 的清单前缀表；
  `Makefile` · `deploy/` · `scripts/` · `.gitignore` · `analysis/tools/genproto.py`；约 184 行活文档路径与链接
- 决策者：用户
- 依据：[ADR-0029](0029-three-module-dirs.md)（**部分被本文取代**：其决定 1 的顶层布局）·
  [`structure.md`](../../design/structure.md) §1.7 的 `ST-1` / `ST-2` ·
  [ADR-0004](0004-terminology.md)（**项目名未定 ⇒ 容器名不得用产品名**）·
  [ADR-0028](0028-three-module-view.md) 决定 1（核心是三者共用的内核，不拆散）

---

## 背景

[ADR-0029](0029-three-module-dirs.md) 把三个大模块的**名字**对齐到了顶层
（`deception/` `honeypot/` `console/`），但顶层仍是「五个平面平铺 + 一堆底座目录」。两个后果：

1. **看不出「哪些是产品功能模块」** —— `core/`（共享内核）与 `analysis/`（L4）和三个模块是同级目录，
   读者要读到文档才知道谁是谁、该往哪个目录开发；
2. **公用代码没有自己的位置** —— 契约 `api/` 与内核 `core/` 散在顶层，没有任何「这是被共用、不是功能模块」的信号。

## 候选

| 候选 | 内容 | 真实优势 | 代价 |
| --- | --- | --- | --- |
| **A** | 两级容器：`modules/{deception,honeypot,console}` + `common/{core,api}`；`analysis/` 留顶层（**采纳**） | ① 「功能模块」与「公用代码」一眼分开；② 改动面 = 124 处 import（机械可批）+ 约 184 行文档；③ `analysis/` 是跨层且另一种语言，留顶层语义正确 | 🟡 顶层多两个容器目录（`ST-1` 白名单要改）；🟡 被移动树深度 +1，树内相对链接要补一层 |
| **B** | 一个 `src/` 装全部五个平面 | 最统一（「所有代码都在 `src/`」） | 🔴 额外 116 处 import（`core/` 81 + `api/` 35）；🔴 且 `src/` **不区分「模块 / 公用」**，没解决第 2 个问题 |
| **C** | `apps/` 装三个模块 | 「可部署单元」语义 | 🔴 `analysis/` 也是可部署的服务却不在里面 ⇒ 名不副实 |
| **D** | 只改名、不建容器 | 零风险 | 🔴 不满足用户要求，且两个问题都还在 |

## 决定

### 决定 1 · 顶层 = 容器 + 平面，两级

```text
modules/            产品功能模块（**开发从这里进去**）
├── deception/      ① 欺骗层（L1 四个接入适配器与处置 + L3 网络欺骗）
├── honeypot/       ② AI 蜜罐层（protocol/ 已建 · shell/ 未建）
└── console/        ③ 管控平台（只读观测台）
common/             公用代码（被 modules/ 共用，**不是**功能模块）
├── core/           共享内核 —— 判定与响应生成的唯一实现（AR-2 / AR-5）
└── api/            跨进程契约 —— .proto + 生成的 Go 桩（叶子；ST-6 禁止手写客户端）
analysis/           L4 分析层（Python）—— 跨 ①②，故既不在 modules/ 也不在 common/
deploy/ docs/ scripts/ vendor/ .pi/
```

`ST-1` 的白名单随之改为「容器（`modules/` `common/`）+ 独立层（`analysis/`）+ 底座」。
`ST-2`（一个进程的源码只在一个顶层目录内）**没有放松**：`planeOf` 改为两级解析后，仍逐包判定。

### 决定 2 · `analysis/` 留顶层，并写下理由

它**不是**公用库（有自己一条近线流水线：意图 → 攻击链 → 策略），**也不是**三大模块之一
（用户的三模块不含它），而且是**另一种语言与运行时**（Python + venv）。
放进 `common/` 会同时错两件事：把一条流水线叫「公用代码」，以及暗示它和 `core/` 一样是被人 import 的库。
它的归属问题与 [ADR-0029](0029-three-module-dirs.md) 未解决 1 是同一个。

### 决定 3 · 容器名用中性的 `modules/` 与 `common/`

项目名尚未定（[ADR-0004](0004-terminology.md)），因此不能用产品名做容器名。
`modules/` 直译用户说的「大模块」，`common/` 直译「公用代码」——两者都不带技术语义。
**刻意避开 `pkg/`**：Go 里它专指「供外部引用的库」，与这三者（与 `core/`）恰好相反。

### 决定 4 · 历史文档不追改（同 [ADR-0029](0029-three-module-dirs.md) 决定 3）

`docs/plans/` · `docs/background/` · `docs/log.md` 里的旧路径是**当时**的快照，保持原样。
活文档（`docs/design` · `docs/modules` · `docs/spec` · `docs/ops` · `docs/integrate` · `docs/kb` · 根 `README.md`）
已全部更新 —— **包括被移动树内部**的相对链接（下移一层后要补一个 `../`，本轮实测 12 条）。

## 后果

**正面**：

- 「我要开发 ①」→ 进 `modules/deception/`；② → `modules/honeypot/`；③ → `modules/console/`；一眼可辨；
- 公用代码第一次有了自己的位置：`common/core` 与 `common/api`，「被共用」在目录层面看得见；
- 三个模块目录各有一份 README（本轮新建），进入即知道「这里有什么 / 文档在哪 / 怎么跑」。

**负面 / 代价**：

- 124 处 Go import 前缀变化（`shen/core/…` → `shen/common/core/…` 等），全部机械替换 + 构建验证；
- 约 184 行文档路径与链接要改，且**被移动树内部的相对链接必须补一层**；
- `docker build` 的 `COPY` 与 `SERVICE`、`compose.yaml`、`Makefile` 的 `go run ./...`、
  `.gitignore` 的产物兜底规则、`analysis/tools/genproto.py` 的 proto 根，都要跟着改 —— 漏一个到构建时才炸。

**引入的新风险**：

- **机械替换只改路径、不改语义**：本轮实际发生过「`common/core/internal/` 里的 `honeypot` 被 sed 成
  `modules/honeypot/`」——靠人工复核修回。任何将来的批量路径改写都要按这条复核；
- `planeOf` 这类「取第一段当平面」的实现会被两级结构打穿。本轮已改成显式感知容器，
  但**任何将来新增的容器目录都要同时改它**（已写进 `scripts/archcheck/main.go` 的函数注释）。

## 失效条件

1. **`analysis/` 也需要收进容器**（例如它开始只被 ① 使用）⇒ 说明「跨层」的理由不再成立，决定 2 需改写；
2. **容器下出现非三大模块的目录**（如 `modules/utils/`）⇒ `modules/` 的「产品功能模块」语义被稀释，需重新划界或改名；
3. **再增加一级目录深度**（如 `modules/edge/deception/`）⇒ 相对链接与 `planeOf` 的复杂度再上一层，需先算清收益；
4. **`common/` 里出现只有单一模块使用的东西** ⇒ 它不是「公用」，应移回该模块。

**复查日期**：2027-01-21（或蜜罐编排立项时，以先到者为准）。

## 未解决

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | **`analysis/` 是否拆成 ①（`aicap`）与 ②（分析四件套）两半** | 依赖集与发版的独立性 | 失效条件 1 · 需单独 ADR（同 [ADR-0029](0029-three-module-dirs.md) 未解决 1） |
| 2 | 模块目录下 **README 的粒度**（本轮建的是「模块级 + 已有子模块级」两层） | 读者找入口的成本 | 等实际使用反馈；不够细再补子模块级 |
| 3 | `common/` 是否再细分（如 `common/lib/`） | 将来公用代码变多时的可读性 | 失效条件 4 触发时再议 |
