# 变更包：伪造流量与判定核对脚本（`scripts/traffic/`）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 给人工测试一个**声明式**的伪造流量工具：发请求 + 从观测面核对判定，带断言与退出码，能进自动化 |
| 日期 | 2026-09-19 |
| 状态 | 已验证（对 Docker 栈实跑：断言 `5/5` 通过 · 观察 5 条 · 响应头卫生 0 问题） |
| 改动分级 | **M**（新增工具与场景 + 一处控制台接口修正 + 门禁覆盖扩大） |
| 涉及范围 | `scripts/traffic/`（新增）· `scripts/shen.sh` · `Makefile` · `console/cmd/console/main.go` · `docs/ops/runbook.md` · `docs/integrate/manual-test.md` |
| 决策数 | 已答 3 项（核对走观测面 · 每请求独立会话 · 观察类不下断言）/ 待定 3 项 |
| 关联 | `ST-7` · `ST-10` · `NI-1` · `OH-2` · `AR-11` · `INT-11` · [`../../scripts/traffic/README.md`](../../scripts/traffic/README.md) |

---

## 1. 需求与验收

**用户要什么**：整理出一套**能向欺骗引擎发送伪造流量的验证脚本**，方便后续做测试。

**验收判据**：

1. 场景**声明式**且可扩展：一次请求 = 一条 JSON（方法 / 路径 / 头 / body + 期望）。
2. 期望能断言：分数上下界 · 命中信号（任一 / 全部）· 决策取值；**观察类**可只报告不断言。
3. 核对必须走**观测面**（控制台），因为判定响应禁止回显分值（`ST-7`）。
4. 所有场景**额外**校验：业务未被影响（`NI-1`）与响应头卫生（`OH-2`：无 `x-shen*` / `Via` / `Caddy`）。
5. 只用**标准库**（不需要 venv / 任何依赖）；有退出码，可进自动化；`--json` 输出。
6. 入口统一：`scripts/shen.sh traffic` · `make traffic`；运行手册与人工测试文档都指向它。

**不做什么**：不做压测（不带并发/速率控制）· 不实现协议级攻击载荷 · 不改产品行为。

---

## 2. 设计逻辑

| # | 问题 | 决定 | 理由 |
| --- | --- | --- | --- |
| ① | 判定结果从哪核对 | **观测面**（控制台 `/api/flow`） | 响应里没有分值/信号（`ST-7`），看响应只会得到"业务内容" |
| ② | 怎么让每个场景独立判定 | **每条请求带唯一 `sid` cookie** | 判定键是 `(来源, 会话, 方法, 路径)`（`ST-10`）；实测仅换查询串**不生效**（第二场景吃到了第一场景 0.9 的分） |
| ③ | 示例配置没覆盖的流量怎么办 | 标 `observe_only`：只发不判 | 不替用户编期望值；等加了规则再收紧断言（避免脚本与真实配置漂移） |
| ④ | 上游业务自己报 5xx/501 怎么办 | 场景可声明 `status_in` | `NI-1` 管的是"引擎不得影响业务"，不是"业务必须支持某方法"（演示站没有 POST 处理器） |
| ⑤ | 语言与依赖 | **Python 标准库** | 谁都能跑：不需要 venv、不需要装包、跨平台 |
| ⑥ | `scripts/*.py` 一直没进门禁 | 纳入 `ruff` 检查 | `TB-15` 要求 Python 过 ruff；此前只查 `analysis/`，是个漏洞 |

---

## 3. 追溯矩阵

| 规则 | 文档 | 产物 | 验证 |
| --- | --- | --- | --- |
| `ST-7`（判定细节不回显） | [`../../scripts/traffic/README.md`](../../scripts/traffic/README.md) | 脚本只从控制台读分值/信号 | 实跑输出 |
| `ST-10`（判定缓存/幂等键） | 同上「两个容易踩的点」 | 每请求唯一 `sid`；`--no-session-nonce` 复现复用 | 实跑：修复前 0.9/0.6 复用 → 修复后 0.30/0.00 |
| `NI-1`（不影响业务） | [`../ops/runbook.md`](../ops/runbook.md) §2.1 | 每个场景断言状态码（默认 < 500，可用 `status_in` 声明例外） | 实跑：10 条场景业务全正常 |
| `OH-2`（可见面卫生） | 同上 | `header_findings()` 检查 `x-shen*` / `Via` / `Caddy` | 实跑：0 问题 |
| `AR-11`（异步上报） | 同上 | 等判定出现（`--wait`，默认 8 秒），等不到即报失败 | 实跑 |
| `TB-15`（Python 门禁） | [`../../Makefile`](../../Makefile) | `pylint` / `pyfmt-check` 覆盖 `analysis/` + `scripts/` | `make gate` |

---

## 4. 代码与产物

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `scripts/traffic/scenarios.json` | 新增 | 10 个场景（5 断言 + 5 观察）：探针 · 敏感文件 · 爆破 · 注入 · 穿越 · Actuator · 正常对照 |
| `scripts/traffic/send.py` | 新增 | 发送 + 观测面核对 + 断言 + 汇总；标准库；`--json`；退出码 0/1/2 |
| `scripts/traffic/README.md` | 新增 | 怎么用 · 期望字段 · 为什么走观测面 · 两个易踩点 · 退出码 |
| `scripts/shen.sh` | 改 | 新增 `traffic` 子命令（自动带上 entry / console） |
| `Makefile` | 改 | 新增 `make traffic`；`pylint` / `pyfmt-check` 覆盖 `scripts/` |
| `console/cmd/console/main.go` | 改 | `/api/flow`、`/api/analysis` 空列表返回 `[]` 而非 `null`（Go nil 切片序列化陷阱） |
| `docs/ops/runbook.md` · `docs/integrate/manual-test.md` | 改 | 命令表 + §2.1 伪造流量验证 + 故障表两条 + 人工测试改指向脚本 |

---

## 5. 测试与场景

| # | 场景 | 期望 | 实测 |
| --- | --- | --- | --- |
| 1 | 全场景实跑（Docker 栈） | 断言全过 | ✅ `断言 5/5 通过 · 观察 5 条 · 响应头卫生问题 0 条` |
| 2 | 探针（UA+路径） | ≥0.8 且命中 `ua-headless`/`path-probe` | ✅ `0.90 ua-headless,path-probe` |
| 3 | 仅路径命中 | 0.2–0.5 且命中 `path-probe` | ✅ `0.30 path-probe` |
| 4 | 仅 UA 命中 | 0.5–0.7 且命中 `ua-headless` | ✅ `0.60 ua-headless` |
| 5 | 正常用户对照 | ≤0.1 | ✅ `0.00`（两条） |
| 6 | 观察类（`.env` / 爆破 / 注入 / 穿越 / Actuator） | 只报告，不断言 | ✅ 5 条观察 |
| 7 | 响应头卫生 | 无 `x-shen*` / `Via` / `Caddy` | ✅ 0 问题 |
| 8 | 退出码 | 断言全过 → 0 | ✅ 0 |
| 9 | `scripts/` 静态检查 | ruff 全绿 | ✅ `All checks passed` |

**没有覆盖的**：并发/速率（非压测工具）· 真实 TLS 入口（默认对明文入口）· Windows 上的 `Cookie` 头合并行为。

---

## 6. 验证证据

```console
$ scripts/shen.sh traffic
场景                   HTTP     分数  决策           信号
probe-git-headless    200   0.90  route_origin ua-headless,path-probe  ✓
probe-git-normal-ua   200   0.30  route_origin path-probe  ✓
headless-home         200   0.60  route_origin ua-headless  ✓
normal-home-firefox   200   0.00  route_origin —  ✓
normal-api-safari     200   0.00  route_origin —  ✓
env-probe             200   0.00  route_origin —  观察
wp-login-brute        501   0.00  route_origin —  观察
sqli-admin            200   0.00  route_origin —  观察
path-traversal        200   0.00  route_origin —  观察
actuator-env          200   0.00  route_origin —  观察

断言 5/5 通过 · 观察 5 条 · 响应头卫生问题 0 条
```

**关键指标**：新增场景 **10** 条（断言 5 / 观察 5）· 新增文件 **3** 个 · 发现并修掉真缺陷 **2** 处（判定复用误判、控制台空列表返回 `null`）· 门禁覆盖扩大 **1 处**（`scripts/` 纳入 ruff）。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | 观察类场景（5 条）尚无断言 | 这些流量当前只"看得见"，没有回归保护 | 配置里加规则后同步补期望 |
| 2 | 无并发 / 速率控制 | 不能做压测 | 压测另有 `make bench` / `E3` 计划 |
| 3 | 期望值与示例配置**手工同步** | 改了规则忘改场景会假红 | 可考虑让脚本读 policy 的规则清单做校验 |
| 4 | 会话 nonce 依赖 cookie 名（默认 `sid`） | 换配置里的 `session.cookie_name` 需传参 | 已提供 `--session-cookie` |
| 5 | 未覆盖 TLS 入口与跨节点形态 | 真实接入演练仍需手工 | 见 [`../design/integration.md`](../design/integration.md) |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 脚本第一版只换查询串，第二场景吃到第一场景的判定 | **缺陷（脚本认知错）** | `probe-git-normal-ua` 得 `0.90 ua-headless,path-probe`（UA 明明是正常浏览器） | 先读代码确认缓存按 `decision_id`（`(来源,会话,方法,路径)`）→ 改为每请求唯一 `sid` cookie | ✅ 修正后 `0.30 path-probe` |
| 2 | 控制台空列表返回 `null` 而非 `[]` | **缺陷**（Go nil 切片序列化） | 脚本报「`/api/flow` 期望列表，实际 NoneType」 | 控制台改用 `make([]T,0)`；脚本对 `null` 也容忍 | ✅ |
| 3 | `wp-login-brute` 报「业务被影响（NI-1）」 | 断言写错（把业务行为当引擎问题） | 演示站没实现 POST，回 501 | 新增 `status_in` 字段声明可接受状态码，并在场景里写明缘由 | ✅ |
| 4 | `scripts/*.py` 从未进 Python 门禁 | 门禁漏洞 | `pylint` 只跑 `analysis/` | `Makefile` 覆盖 `analysis/` + `scripts/`，并修掉暴露出的 7 条问题 | ✅ |
| 5 | 我的折行脚本把参数定义改坏（`--no-session-nonce` 少右括号） | 自伤 | `python3 -c ast.parse` 校验 + 目视 | 复原并按 100 列收短 help 文案 | ✅ |

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 新增伪造流量验证工具（`scenarios.json` + `send.py` + README）· 入口 `scripts/shen.sh traffic` / `make traffic` · 每请求独立会话以取得独立判定 · 控制台空列表返回 `[]` · `scripts/` 纳入 ruff 门禁 · 运行手册与人工测试文档更新 | 用户要求（伪造流量验证脚本）· `ST-7` · `ST-10` · `NI-1` · `OH-2` · `AR-11` · `TB-15` |
