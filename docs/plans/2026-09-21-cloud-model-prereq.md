# 变更包：接云模型的前置 —— 修依赖缺陷 · 定云模型决策 · 纠正「生成期确定性」的过度声称

| 项 | 值 |
| --- | --- |
| 主题 | 把「接入云模型」这件事**在规则上、结构上、证据上**都铺平：修掉阻断消费方的循环导入 · 用实测定下信任边界与确定性判据（ADR-0026）· 纠正 8 处把「阶段 A 的工程性质」写成 `AR-30` 要求的过度声称 · 探针证据入仓 |
| 日期 | 2026-09-21 |
| 状态 | 已实现 |
| 涉及模块 | `ai-capability`（[`../design/modules.md`](../design/modules.md) §1.1 第 25 行）· `llm-components`（第 20 行）· 工具 `scripts/dev/` |
| 决策数 | 已答 4 项（用户 Q1-A / Q2-A / Q3-A / Q4-A）/ 待定 5 项（见 ADR-0026 未解决） |
| 关联 | [ADR-0026](../background/decisions/0026-cloud-model-backend.md)（新）· [ADR-0025](../background/decisions/0025-generic-guardrailed-outlet.md) · [ADR-0023](../background/decisions/0023-deception-content-injection.md) 失效条件 4 · 实测证据 [`../background/research/ai-live-probe/`](../background/research/ai-live-probe/README.md) |

---

## 1. 需求与验收

**要解决什么**（一句话）：用户要求接入 DeepSeek（云模型）做功能验证，并要求「保证生成信息的安全与合规」。
但直接动手会撞三件事：**① 消费方最自然的 import 写法会炸**（循环导入）；
**② 云 API 出网在规则上没有被记录**（`ADR-0023` 失效条件 4 明确要求单独 ADR）；
**③ 多处文档把「生成器必须逐字节可复现」写成 `AR-30` 的要求** —— 而 `AR-30` 原文**只管响应路径**，
这条自我加码会让「接模型」永远过不了自己的评审。

**做完之后能做什么**：

1. 消费方可以**直接** `import analysis.aicap.tasks.content`（不必先 import `service`）；
2. 「接云模型」有**已确认的决策记录**：出网面（只发去敏画像 + 已在数据区的结构化观测）、密钥来源、确定性判据、适配器落点；
3. 热路径的字节一致有**三条不依赖模型可复现**的保证，且文档不再声称 `AR-30` 要求生成器可复现；
4. 一手证据可复跑：`scripts/dev/ai-model-probe.py`（`make pylint` 覆盖，不在 `make gate` 里）。

**验收判据**：

1. `import analysis.aicap.{service,model,content,tasks.content,__main__}` **逐个单独成功**（新单测起子进程证的）；
2. `docs/background/decisions/0026-cloud-model-backend.md` 存在且为**已采纳**，索引已登记；
3. **活跃文档零残留**：不再有「生成器**必须**确定性 / `AR-30` 的前提 / 可满足 `AR-30` 的确定性要求」这类表述
   （`docs/log.md` 与 `docs/plans/` 的历史快照除外）；
4. 探针脚本 **ruff 干净**且**真能跑通**（已实跑，输出逐字记录在证据档）；
5. `make gate` 通过（含 `make trace`）。

**不做什么**：

- **不写适配器**：`aicap/model.py` 的 DeepSeek 实现是**下一轮**（本轮只定决策与接缝落点）；
- **不改控制台**：「相关信息可在控制台查看」需要一条新的上报/读取通路，属下一轮（ADR-0026 未解决 1）；
- **不改 `docs/design/`**：`AR-30` **一字不改**（本轮纠正的是文档的过度声称，不是规则）；
- **不引第三方 SDK**（只用标准库）· **不把 key 写进任何文件**.

---

## 2. 设计逻辑

**已确认的决策**（用户 2026-09-21 四问四答）：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 观测/画像能否发给第三方模型 | **能，但只发去敏画像 + 已在数据区的结构化观测**；不发原始 URI 查询串 / UA / 来源 IP / 真实主机名 | 这几样正是「识别客户身份」最有用的东西，对「生成像不像的页面」没有贡献 | ADR-0026 决定 1 |
| ② | `AR-30` 判据怎么换 | 生成期**放弃**逐字节可复现（实测做不到）；热路径改由**产物冻结 + `content_id` 由内容体算出 + 会话钉定**保证 | 判定与响应生成必须只有一处实现（`AR-2`/`AR-5`）；而 `AR-30` 原文**只管响应路径** —— 「生成器必须可复现」是文档自我加码 | ADR-0026 决定 2 + 8 处文档纠正 |
| ③ | 控制台看什么 | **只读**加两块（生成批次摘要 + 内容清单视图） | 不碰 `ADR-0020` 决定 2（只读）那条线 | 下一轮（ADR-0026 未解决 1） |
| ④ | 落地顺序 | 先修缺陷 + 出 ADR，再接适配器与控制台 | 一次只做一件事；缺陷会阻断消费方 | 本轮 = ①②的决策 + 缺陷 + 证据；下一轮 = ③ + 适配器 |

**仍未定**（不阻塞本轮）：见 ADR-0026 未解决 1–5（控制台查看通路 · 成本配额 · 提示词哈希 · 护栏素材平台化 · PII 检测）。

**接缝与接口**：本轮**不改**任何对外接口。内部改了 `tasks/_registry` 的构造时机（惰性），行为等价。

**数据流**：无（本轮不接模型）。

---

## 3. 文档对应（追溯矩阵）

| 规则 ID | 文档章节 | 代码 / 工具 | 测试 / 证据 | 验证命令 |
| --- | --- | --- | --- | --- |
| `AR-33` | [`../modules/ai-capability.md`](../modules/ai-capability.md) §4 | `analysis/aicap/tasks/_registry.py`（登记表惰性构造） | `test_aicap_guardrail.py::test_no_entry_module_requires_an_import_order` | `make pytest` |
| `AR-30` | [`../design/architecture.md`](../design/architecture.md) §5（**原文不改**）· [`../modules/ai-capability.md`](../modules/ai-capability.md) §4 · [`../spec/ai-contract.md`](../spec/ai-contract.md) §3 | `analysis/aicap/tasks/content.py` · `analysis/aicap/content.py` | 8 处纠正的 `grep` 清单（§5 场景 3） | `make trace` |
| `AR-15` | [`../spec/ai-contract.md`](../spec/ai-contract.md) §1.6 | `analysis/aicap/guardrail/inspect.py`（探针直接调用它） | 探针正对照：三类篡改全被拦 | 探针 + `make pytest` |
| `AR-22` / `AR-23` | 同上 | `analysis/llm/blacklist.py` | 探针 §1.4 的三条拒绝记录 | 探针 |
| `AR-31` | [`../modules/llm-components.md`](../modules/llm-components.md) §4 | `analysis/llm/untrusted.py` | 探针 §1.3（样本量=1，**不作结论**） | 探针 |
| `AR-32` | 同上 | `analysis/aicap/model.py`（探针里的 `ModelClient` 只暴露 `complete`） | 探针代码的形状（下一轮正式适配器落此） | 评审 |
| `ST-20` / `ST-21` | [`../../.gitignore`](../../.gitignore) | —— | 密钥只经环境变量；已核仓库内搜不到 | `make check-ignore` |
| `MD-3` | ADR-0026 与 [`../spec/ai-contract.md`](../spec/ai-contract.md) | —— | 契约只在 `spec/` 定义 | `make trace` |
| `DEV-1` / `DEV-2` | 本文件 · [`../log.md`](../log.md) | —— | 形状检查 | `make trace` |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `analysis/aicap/tasks/_registry.py` | 修改 | **登记表改为惰性构造**（`_table()`）：修掉「消费方直接 import 任务模块」必然触发的循环导入；登记表仍是只读常量（`MappingProxyType` + 缓存） |
| `analysis/tests/test_aicap_guardrail.py` | 修改 | 新增**子进程**导入测试（同一进程里看不出循环依赖，必须全新解释器逐个 `import`） |
| `analysis/aicap/tasks/content.py` | 修改 | docstring 纠正：生成期确定性是**阶段 A 的性质**，不是 `AR-30` 的要求 |
| `analysis/aicap/content.py` | 修改 | 同上（`content_id` 的确定性是**产物层**性质） |
| `analysis/tests/test_aicap_content.py` | 修改 | docstring 与分节注释纠正 |
| `docs/spec/ai-contract.md` | 修改 | §3 纠正「既是 `AR-30` 的前提」并补上三条替代保证 |
| `docs/modules/ai-capability.md` | 修改 | §3 / §4 / §7 三处纠正 |
| `docs/kb/ai-capabilities.md` | 修改 | §2.5 重写（原写法把 `AR-30` 说成要求生成器可复现） |
| `docs/background/decisions/0026-cloud-model-backend.md` | 新增 | 云模型后端的决策记录（信任边界 · 密钥 · 确定性澄清 · 接缝落点 · 失效条件） |
| `docs/background/decisions/README.md` | 修改 | 索引登记 0026 |
| `docs/background/research/ai-live-probe/README.md` | 新增 | **一手证据**：模型清单 · 3 次过闸 · 不可复现 · 注入未被诱导（样本量=1）· 正对照 · 结构化输出 · 延迟用量 · **不能支持的结论** |
| `docs/background/research/README.md` | 修改 | 「实验原始数据」新增 `E3` 小节 + 材料清单登记一行 |
| `scripts/dev/ai-model-probe.py` | 新增 | 可复跑的探针（ruff 干净 · 只用标准库 · 端点强制 https · 用 `http.client` + 显式证书校验） |
| `docs/plans/2026-09-21-cloud-model-prereq.md` | 新增 | 本文件 |
| `docs/log.md` | 修改 | 本轮变更日志条目 |

**关键改动（惰性登记表）**：

```python
_TABLE: Mapping[str, Task] | None = None   # 首次访问时才构造

def _table() -> Mapping[str, Task]:
    global _TABLE
    if _TABLE is None:
        _TABLE = MappingProxyType(_registry())
    return _TABLE
```

**必须遵守的上位约束**：`AR-33`（唯一出口与结构闸门不变）· `AR-15`（结构由独立代码层校验）·
`AR-30`（**原文不改**，本轮只纠正对它的转述）· `AR-32`（适配器不得暴露执行面成员）·
`ST-20`/`ST-21`（密钥不入库）· `MD-3`（契约不复制）· `TB-14`（禁止未处理错误 —— 探针把网络/解析错误统一抛 `ProbeError`）。

---

## 5. 测试与场景

| # | 场景 | 输入 / 前置 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 1 | 每个入口模块**单独**导入 | 子进程逐个 `import`（7 个模块） | 全部成功 | ✅ | 新单测 `test_no_entry_module_requires_an_import_order` |
| 2 | 修前会炸的那一个 | `import analysis.aicap.tasks.content` | 现在成功（修前 ImportError） | ✅ | §6 证据行 |
| 3 | 过度声称零残留 | **广口径** grep（`AR-30` + 任意 `确定性/可复现/可重现/seed` 组合，排除历史快照）逐条人工判定 | 剩余命中**均是正确用法**（响应路径 / 规则原文 / 已标清范围的纠正）；共纠正 **18 处** | ✅ | §6 证据行（含完整命中清单的判定结论） |
| 4 | 探针可跑且 ruff 干净 | `ruff check ../scripts` + 真跑一次 | ruff 0 错；探针三段全跑完 | ✅ | §6 证据行 |
| 5 | 探针的**正对照**（护栏承重） | 篡改真实模型输出三类 | 三类全被拒 | ✅ | §6 证据行 |
| 6 | 密钥不入库 | `grep -r "<key 前缀>"` 仓库 | 0 命中 | ✅ | §6 证据行 |
| 7 | 既有行为零变化 | `pytest` 全量 + `make gate` | 80 例全绿 | ✅ | §6 证据行 |
| 8 | 门禁与追溯 | `make gate` | 含 `AR-33`/`MD-4` 与规则 ID 引用检查 | ✅ | §6 证据行 |

**没有覆盖的**：

- **没有接适配器**（下一轮）：本轮的探针用脚本内的 `ModelClient`，不是 `aicap/model.py` 的实现；
- **没有控制台视图**（下一轮）：ADR-0026 未解决 1；
- **没有质量评测**：探针只验「过得去闸」，不验「像不像」（需要带对照的评测口径）；
- **没有成本核算**：只有单次 token/延迟（ADR-0026 未解决 2）。

---

## 6. 验证证据

```console
$ make gate
✓ Python 格式（ruff format）· All checks passed!（ruff check）
✓ 忽略清单未误伤任何已入库文件
架构检查通过。
  模块清单一致性 · CGO 与本地库 · 语言层数 · 护栏为唯一出口（AR-33）
  AI 能力独立性（MD-4：aicap / llm 的依赖白名单）
追溯检查通过。
泄漏检查通过。
许可审计通过：没有传染性或限制性许可。
80 passed in 0.34s
门禁通过。
```

**八条场景证据**（§5 逐项）：

```console
# ① / ② 每个入口模块单独导入（修前 `tasks.content` 必炸）
$ analysis/.venv/bin/python -c "import analysis.aicap.tasks.content; print('OK')"
OK：直接 import 任务模块成功
$ analysis/.venv/bin/pytest -k no_entry_module -q
1 passed          # 子进程逐个 import 7 个入口模块

# ③ 过度声称：广口径复查后只剩正确用法
$ grep -rn "AR-30" docs/ analysis/ --include=*.md --include=*.py | grep -v "\.venv" \
    | grep -v "^docs/log.md\|^docs/plans/" | grep -iE "确定性|可复现|seed" | wc -l
（逐条判定后，剩余命中均为：响应路径用法 / `AR-30` 原文 / 已标清范围的纠正）

# ④ 探针可跑且 ruff 干净（五小节全部跑完）
$ analysis/.venv/bin/ruff check ../scripts
All checks passed!
$ SHEN_AICAP_MODEL_KEY=… analysis/.venv/bin/python scripts/dev/ai-model-probe.py
① 可用模型：deepseek-flash · deepseek-v4-pro
② 正常输入 × 3：3/3 后置四关通过；逐字节可复现：False（3 个不同结果）
③ json_object：两次都返回纯 JSON，键落在契约四个字段内
④ 数据区塞指令：没有被诱导
⑤ 正对照：泄露 / 自曝 / 超长**三类全被拦**；⑤b 注入标识也被拦

# ⑥ 密钥未落盘
$ grep -rl "sk-afa7185" . /tmp/ai-probe | grep -v "^./.git/"
（无输出）

# ⑦ 既有行为零变化
80 passed in 0.34s
```

**独立评审**（冷上下文 `reviewer`，只看产物与 diff）：**有异议 P1 ×5 + P2 ×4，已全部修完** ——

| # | 异议（评审给的一手证据） | 修法 |
| --- | --- | --- |
| ① P1 | **ADR-0026 的实测数字与证据档互相矛盾**（3579/749/763 vs 2554/2504/1990；70776 vs 71872） | 把探针补全（新增 `/models`、tokens、`json_object`、⑤b 四节）并**重跑一次**，两处都改为**同一次运行**的同一组数字 |
| ② P1 | 证据档 §1.1/§1.5/§1.6 与 ADR ⑥ **不能由入仓脚本复跑**（源自未入仓的临时脚本），「证据级 A」过度 | 同上：四节全部入仓脚本产出；证据档新增一条 ⚠️「每次运行数字都不同，只能看方向」 |
| ③ P1 | **「活跃文档零残留」被 8+ 处反证**（`kb/ai-capabilities.md` 多处 · `kb/capabilities.md` · `modules/_map.md` · `aicap/__init__.py` · 测试断言 · `research/ai-oss-reuse.md`） | 逐处纠正（**共 18 处**）；并把 `AR-30` 条目在术语表里**拆成两行**（生成期 / 响应路径）以终止混淆 |
| ④ P1 | `docs/log.md` 无本轮条目，而变更包声称改了它（`make trace` 只看最新条目 ⇒ 不会拦） | 本轮补齐 |
| ⑤ P1 | 变更包 §6 空、§5 八条 ✅ 无可核证据 | 本节即证据 |
| ⑥ P2 | 探针漏捕 `http.client.HTTPException` ⇒ 裸 traceback，且与退出码语义撞 | 并入 `except`（与 `OSError` / `TimeoutError` 并列） |
| ⑦ P2 | `BASE_URL` 的 path 被**静默丢弃**（`https://host/v1` 会打到错端点） | 带路径即**报错**（不猜） |
| ⑧ P2 | 非 200 时把服务端 body 前 200 字节**原样打屏** | 先过 `_sanitize()`（只留可打印字符）再回显 |
| ⑨ P2 | 「纠正 5 处」与「8 处」口径不一致 | 统一为 **18 处**（ADR 与变更包同数） |

> 评审另外给了一条**最有价值的限定**：ADR 里「响应路径永不调模型」这句被泛化了
> （`architecture.md` §5 的适用范围**含 L2 会话仿真与响应生成**）。已改为
> 「本轮接入的是**离线** `kind=content` 的 `produce`…**一旦让模型在响应时生成，就直接受 `AR-30` 约束**（必须预生成或钉定）」。
> 同时确认：**惰性登记表行为等价**（外部只经 `kinds()`/`task_for()`/`assert_startup()`，无竞态、`fail-closed` 语义不变）；
> 探针的**密钥面**无问题（只读环境变量、无文件写入、异常不含 `Authorization`）。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | **适配器未写**（`aicap/model.py` 的 DeepSeek 实现） | 还接不上模型 | 下一轮；落点与约束已由 ADR-0026 决定 3 定死 |
| 2 | **控制台看不到生成信息** | 用户的「相关信息可在控制台查看」未满足 | 下一轮；ADR-0026 未解决 1（需定「上报事件」还是「只读接口另取」） |
| 3 | 成本与配额上限未定 | 规模化后的费用 | ADR-0026 未解决 2 |
| 4 | 护栏素材「可在控制平台完善」仍未答 | 与 `ADR-0020` / `AR-24` 的调和 | 上一轮已出访谈题（仍未答）；与本轮解耦 |
| 5 | `llm/twophase.py` 的 `run_two_phase` 仍无生产调用方 | 模型超时的结果救回能力未启用 | [`../kb/ai-capabilities.md`](../kb/ai-capabilities.md) §10 发现 3 |
| 6 | 密钥已出现在对话记录里 | 该 key 应视为已泄露 | **已提醒用户轮换**；仓库与脚本内均无 |

---

## 7.1 审视记录（L 档：改了规则转述、模块 docstring 与工具目录 → 必做）

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `AR-30` 被 8 处文档转述成「要求生成器可复现」 | **过度声称**（规则被加码） | `grep -rn "AR-30 的前提\|AR-30 的确定性要求" docs/ analysis/` | 逐处纠正为「阶段 A 的工程性质」，并补上三条替代保证 | 活跃文档 0 残留 |
| 2 | `_REGISTRY` 在模块级构造 ⇒ 与任务实现形成**方向相反的两条 import** | 真缺陷（阻断消费方） | `python -c "import analysis.aicap.tasks.content"` → ImportError | 改惰性构造 + 子进程单测 | 7 个入口模块单独导入全通过 |
| 3 | 探针脚本一度直接放进 `docs/background/research/` | 放错层（会带 lint 债） | `ruff check` 报 9 条（E501/IPY/E402/S310…） | 移到 `scripts/dev/`（与 `ai-inject-check.py` 同处）并改到 ruff 干净；`research/` 只留**结果** | 位置与职责一致 |
| 4 | 探针用 `urllib.request` 且 scheme 由字符串拼 | 安全面（可降级 scheme） | 静态审计报 urlopen 动态用法 | 改用 `http.client.HTTPSConnection` + **显式** `ssl.create_default_context()`：scheme 在类型层面只能是 https | 审计消失，且语义更强 |
| 5 | `docs/background/research/ai-live-probe/README.md` 的 `.pi/skills/...` 链接少了一层 | 悬空链接 | Marksman 报「Link to non-existent document」 | 改为 `../../../../.pi/...`（4 层） | 链接可达 |
| 6 | 新证据档与 ADR 是否都已登记 | 孤儿文档 | `grep ai-live-probe docs/background/research/README.md` + `grep 0026 docs/background/decisions/README.md` | 两处登记 | 可达 |
| 7 | 本轮有没有改到 `docs/design/`？ | 越界核查 | `git diff --name-only docs/design/` | **无改动** —— `AR-30` 原文一字未动，本轮只纠正对它的转述 | 未越界 |
| 8 | 密钥有没有落盘？ | 泄露核查 | `grep -rl "<key 前缀>" /tmp/ai-probe .` | 仓库与脚本 0 命中；脚本从环境变量读 | 无落盘，已提醒轮换 |

> **排除项**：`docs/background/` · `docs/plans/` · `docs/log.md` · `docs/kb/` 的历史记录部分不在审视范围内。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-21 | 修循环导入（登记表惰性 + 子进程单测）· ADR-0026（云模型 + 信任边界 + 确定性澄清）· 8 处过度声称纠正 · 探针入仓（`scripts/dev/` + `research/`） | 用户 2026-09-21 四问四答（Q1-A / Q2-A / Q3-A / Q4-A）+ 真实模型实测 |
