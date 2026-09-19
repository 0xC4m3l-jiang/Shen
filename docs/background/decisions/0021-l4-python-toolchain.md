# 0021. L4 分析层引入 Python 工具链（`ruff` + `pytest`，锁定版本）

- 状态：✅ **已采纳**（目标「按设计完成所有模块」要求实现 L4；设计已把 L4 定为 Python）
- 日期：2026-09-19
- 影响范围：`analysis/`（实现）· `Makefile`（门禁）· [`../../design/language.md`](../../design/language.md) `TB-15`
- 决策者：用户（目标指令）· 记录：Agent
- 依据：`TB-2`（离线 AI 能力必须 Python）· `TB-20`（各层必须按 §1 选型）· `TB-15`（CI 必须含 Python 的 `ruff`）· `TB-14`（Python 禁止裸 `except`）

---

## 背景

L4（`intent` / `chain` / `strategy` / `llm-components`）在 [`../../design/language.md`](../../design/language.md) §1 与
[`../../design/modules.md`](../../design/modules.md) §1.1 中**指定为 Python**，理由是 LLM / ATT&CK 映射 / GNN / RL 生态全在 Python。
此前用户曾要求**不引入 Python 工具链**（当时是无必要引入，已回退）。本轮目标明确要求「按设计完成所有模块」，
L4 是实现该目标不可绕过的一块；而 `TB-15` 规定**引入一种语言就必须有它的门禁** —— 二者合起来意味着：**要 L4，就要 Python 门禁**。

## 候选

| 候选 | 内容 | 优势 | 代价 |
| --- | --- | --- | --- |
| **A** | **在 `analysis/` 内引入 Python + 仓库内 `.venv` + 锁定 `ruff`/`pytest`**（采纳） | 合设计（`TB-2`/`TB-20`）· 合门禁（`TB-15`）· 环境隔离在仓库内，不污染系统 | 多一套工具链与一份锁文件 |
| **B** | 用 Go 实现 L4 | 不增语言 | **违反 `TB-20`**（跨层混用语言）；且 AI 生态在 Go 侧极弱 |
| **C** | 继续推迟 L4 | 无新增 | 目标「完成所有模块」不达成 |

## 决定

1. `analysis/` 用 **Python** 实现（依据 `TB-2`）；
2. 工具链**只在仓库内**：`.venv/`（`.gitignore` 已排除），依赖锁定在 `requirements.txt`（运行期：`pyyaml`）与
   `requirements-dev.txt`（门禁：`ruff==0.16.8`、`pytest==9.1.1`）；
3. 门禁接入 `make gate`：`pyfmt-check`（`ruff format --check`）· `pylint`（`ruff check`，含 `TB-14` 的裸 `except` 禁令）·
   `pytest`（L4 单测）；缺环境时**报错退出**，不静默跳过（`NI-1` 不适用于门禁 —— 门禁宁可红也不要假绿）；
4. 升级 `ruff` / `pytest` **必须**同时改锁文件，属可审计变更。

## 后果

| 类型 | 内容 |
| --- | --- |
| ✅ 正面 | L4 按设计落地；Python 侧有了与 Go 侧对等的门禁；`TB-15` 的 Python 项被真正执行 |
| ⚠️ 负面 | 仓库多一套语言与依赖；首次需 `make pyenv` |
| 🔧 需同步 | `docs/design/structure.md` §1.5 · `docs/progress.md` 第 17–20 行 · 四份 L4 模块文档 |

## 失效条件

1. L4 被整体**推迟或移除**（用户改变范围）→ 本记录失效，`.venv` 与 Python 门禁一并移除；
2. L4 需要**在线热路径**能力（延迟预算不允许 Python）→ 重开，重新评估分层（可能拆成「Python 分析 + Go 执行」）；
3. `TB-2` / `TB-20` 被改（分层语言重排）→ 按新规则重评估。

## 未解决

- L4 **未接真实 LLM**：`UnconfiguredClient` 显式失败（`AR-15` 禁止模板冒充模型输出）；接模型需在部署侧注入 `AnalysisClient`；
- 触发链路（遥测事件 → `AR-14` 去重 → L4）**尚未接通**：当前 L4 是**可调用的库 + 单测**，未接入运行时；
- `pyyaml` 是运行期依赖；若不想引入，可把黑名单资源改为 JSON（`AR-24` 只要求资源随版本分发，不限定格式）。
