# 模块：`policy`

| 项 | 内容 |
| --- | --- |
| 模块名 | `policy` |
| 所属层 | 核心 |
| 实现语言 | Go |
| 源码目录 | `core/internal/policy/` |
| 负责人 | —— |
| 状态 | 阶段 2a —— 策略装载 · 版本 · 校验和 · 规则供给 · **下发与回执**（`api/policy/v1` 的 `Pull` / `Ack`）· 边缘投影（后端表 / 白名单 / **响应改写规则**）；含单测 |
| 最后更新 | 2026-09-19 |

> 权威清单见 [`../design/modules.md`](../design/modules.md) §1.1（第 6 行）。
> 本节之后固定九章，不适用时写「不适用」并说明原因。
>
> 本轮范围与已确认的决定见 [`../plans/2026-09-18-policy-2a.md`](../plans/ARCHIVE.md)；
> 配置文档契约见 [`../spec/config.md`](../spec/config.md)。

## 1. 职责

### 做什么

- **装载**：把配置文件读成策略文档（严格的 schema 校验，见 [`../spec/config.md`](../spec/config.md)）。
- **版本化**：为每次装载产出一个带 `PolicyID` / `Version` / `Checksum` 的**不可变快照**
  （`contract.PolicySnapshot`），并写入版本台账 `store.PolicyStore`。
- **供给规则**：以 `judge.RuleSource` 的形态把规则（数据）交给判定引擎 —— 它就是阶段 1
  「空规则集」的替代者。
- **携带灰度比例**：把 `gray_pct` 放进快照，供 `director` 消费（本模块**不**计算灰度）。
- **装载响应改写规则**：`injects` 段（数据，`ST-24`）→ 投影进载荷的 `inject_rules`；
  **未配置该段**与**配置为空数组**都必须可区分（前者不下发该字段，后者显式下发空数组 —— 运营籍此关掉注入）。
- **下发**（接缝 `S4` 的另一半）：把当前策略**投影**成边缘文档（[`../spec/policy-payload.md`](../spec/policy-payload.md)）
  响应适配器的 `Pull`，并记录它们的 `Ack` 回执 —— 版本号 + 校验和 + 回执三件事一起构成 `ST-8` / `AR-13` 的落地。

### 明确不做什么

- **不做判定** —— 规则求值在 `judge`（`AR-2` / `AR-5`：判定逻辑只在核心实现一次，且只有一处）。
- **不做决策** —— 「去哪儿」是 `director` 的职责；本模块只回答「规则是什么」。
- **不计算灰度** —— 灰度是**逐请求**的函数，属 `director`；本模块只携带比例。
- **不做热重载** —— 不监听文件、不响应信号（无运行时可变状态，`AR-9`）；版本变更 = 新进程 + 新版本号。
- **不做流式下发** —— `api/policy/v1` 的 `Watch` **未实现**，显式返回 `Unimplemented`（[ADR-0018](../background/decisions/0018-policy-plane-pull-model.md)：本轮用 `Pull` 轮询）。
- **不写业务存储** —— 只经 `store.PolicyStore` 落版本台账；**禁止**直连 PostgreSQL / Redis / ClickHouse（`MD-20`）。

## 2. 输入 / 输出契约

| 方向 | 契约 | 定义位置 |
| --- | --- | --- |
| 输入 | 配置文件（YAML） | [`../spec/config.md`](../spec/config.md)（接缝 **S4**） |
| 输出 | `contract.PolicySnapshot`（进程内共享类型） | `core/internal/contract/policy.go` |
| 输出（跨进程） | `api/policy/v1` 的 `PolicySnapshot`（`Pull`）+ `PolicyAck`（回执）—— **消费者是 L1 适配器**（`edge/proxy`） | `api/policy/v1/policy.proto` · 载荷格式见 [`../spec/policy-payload.md`](../spec/policy-payload.md) |
| 消费方契约 | `judge.RuleSource` —— 由**消费方**定义，本模块实现它 | `core/internal/judge/iface.go` |
| 台账契约 | `store.PolicyStore`（`Current` / `Publish` / `RecordAck` / `Acks`） | `core/internal/store/iface.go` |

本模块内部的数据结构：

- `Loader` —— 装载结果：持有不可变快照；`Rules` / `Snapshot` 从它读取。
- 配置文档的解析结构（未导出）—— 只在 `policy.go` 内使用，**禁止**外泄（`MD-5`：跨模块共享类型收敛到 `contract`；跨进程契约才放 `api/`）。

## 3. 依赖

| 允许依赖 | 原因 |
| --- | --- |
| `contract` | 共享类型（`Rule` / `PolicySnapshot`） |
| `store`（仅 `PolicyStore` 接口） | 版本台账与**回执台账**都是核心的 I/O，必须经 `store`（`MD-20`） |
| `contract` | 进程内共享类型：`PolicyAck` · `InjectRule`（按 `MD-5` 收敛到 `contract/`） |
| `api/policy/v1` + `google.golang.org/grpc` | 下发面的服务端（`Server`）；**只被动接收**调用，不主动外呼 |
| YAML 解析库（`gopkg.in/yaml.v3`） | `S4` 规定策略为 YAML；许可为宽松（见 [`../spec/dependencies.md`](../spec/dependencies.md)） |

| 禁止依赖 | 原因 |
| --- | --- |
| `judge` / `director` / `control` | 接口由消费方定义，依赖方向**单向**（`MD-4`）；本模块不 import 它们 |
| 任何存储客户端（Redis / ClickHouse / PostgreSQL 驱动） | `MD-20`：只有 `store` 能碰外部存储 |
| 主动外呼（任何客户端连接） | 本模块**只提供**服务端：装载是纯计算 + 台账读写，下发是被动响应（`MD-6`：禁止外呼） |
| 系统时钟 | 快照不含时间字段；判定不可回放的时间源由调用方注入（`MD-6`） |

## 4. 关键规则

| 规则 | 与本模块的关系 |
| --- | --- |
| `ST-24` | 策略**必须**是数据（配置文件），**禁止**编译进代码 —— 本模块是它的实现处 |
| `AR-13` / `ST-8` | 策略**必须**版本化、可灰度、可回滚、可对账；本模块产出 `Version` 与 `Checksum` |
| `MD-6` | 禁止外呼、禁止写业务存储、禁止依赖系统时钟 |
| `MD-12` | 决策取值**禁止**写进本模块的输出契约 —— 快照里只有规则与灰度，没有决策枚举 |
| `MD-20` | 外部存储访问**必须**经 `store` |
| `NI-1` / `NI-5` | 任何未识别或不可用状态**必须**回落放行 —— 本模块的失败不阻断业务（见 §6） |
| `ST-20` / `ST-21` | 密钥**禁止**进策略载荷；密钥类配置项**禁止**有默认值 |

## 5. 状态与生命周期

| 状态 | 存在哪里 | 生命周期 | 多副本一致性 |
| --- | --- | --- | --- |
| 策略快照（只读） | 进程内不可变值（`Loader` 字段） | 进程启动时构造，进程退出即消失 | 天然一致：所有副本装载**同一份**配置文件 |
| 版本台账 | `store.PolicyStore`（生产实现：PostgreSQL，版本只增） | 长于进程 | 由 `store` 侧保证「版本单调递增」 |

> `AR-9`（核心**必须**无状态多副本、进程内**禁止**模块级可变容器）：本模块**没有**模块级变量，
> 快照是**构造后不可变**的实例字段；它不承载请求级状态，多副本间不需要同步。

## 6. 失败模式与降级

| 失败情形 | 行为 | 是否满足 `NI-1` | 依据 |
| --- | --- | --- | --- |
| `SHEN_CONFIG` 未设置 | **必须**启动失败（非零退出码）并提示以示例文件起步 | ✅ 引擎不在请求路径上；适配器侧 fail-open | `ST-21` 精神 |
| 配置文件缺失 / 不可读 | 同上 | ✅ | 同上 |
| 配置非法（未知键 / 越界 / 枚举外值 / 重复 `id`） | **必须**启动失败，输出字段路径与违反的约束 | ✅ | [`../spec/config.md`](../spec/config.md) §1 |
| `rules` 键缺失 | **必须**启动失败 —— **禁止**以空规则集静默启动 | ✅ | 阶段 1 的未闭合项就是它 |
| `rules: []`（显式空） | 合法；启动日志**必须**打印 `WARN 规则集为空` | ✅ | 与上一条区分：显式配空 ≠ 没配 |
| 版本不递增（`store.PolicyStore.Publish` 拒绝） | **必须**启动失败 | ✅ | `AR-13` / `ST-8` |
| 台账写入失败 | **必须**启动失败（策略未生效就不该对外服务） | ✅ | `NI-3` |
| 运行期 `Rules()` 出错 | 返回错误 → `judge` 上抛 → `control` 返回 gRPC 错误 → 适配器 fail-open | ✅ | `NI-4` / `NI-5` |
| 引擎进程整体故障 | 业务请求 100% 正常（镜像形态不在路径上；2a 形态适配器 fail-open） | ✅ | `NI-1` |
| 资源耗尽 | 快照是启动时一次性分配，运行期只读；受 cgroup 上限约束 | ✅ | `NI-7` |

## 7. 测试

| 类型 | 覆盖什么 | 位置 |
| --- | --- | --- |
| 单元 · 正样本 | 合法文档 → 快照六字段正确；载荷只含三段；`sha256(Payload) == Checksum` | `policy_test.go` |
| 单元 · 负样本 | 每条校验规则一个反例：缺键 / 未知键（顶层与嵌套）/ `shadow: false` / 阈值越界与乱序 / 非法 `listen` / 未实现驱动 / `version: 0` / `gray_pct: 101` / `weight: 0` 与 `>1` / 未知 `field` 与 `op` / 空 `value` / 重复 `id` / 非法 CIDR / `path_prefixes` 不以 `/` 开头 | 同上 |
| 单元 · 边界 | `rules: []` 合法且零规则；`whitelist` 三项为空数组合法 | 同上 |
| 单元 · 可复现性 | 同内容两次装载 `Checksum` 相同；改动任一 `weight` 即改变 | 同上 |
| 单元 · 台账 | 版本单调：替身 `store.PolicyStore` 拒绝同版本二次 `Publish` | 同上（`MD-22`：只用替身） |
| 单元 · 不可变性 | 改写 `Rules()` 返回值不影响后续调用 | 同上 |
| 单元 · 并发 | 多 goroutine 并发 `Rules()` / `Snapshot()`，在 `-race` 下无竞争 | 同上 |
| 单元 · 漂移守卫 | 仓库内 `deploy/config/config.example.yaml` **必须**能通过装载校验 | 同上 |
| 装配层 | 配置 → 策略 → 判定 → 决策的接线，含「影子模式恒放行」的回归与合法性/非法性配置的装载 | `core/cmd/core/main_test.go`（装配层允许依赖具体实现） |
| 开发期工具 | `make check-config`（干跑）· `make replay`（规则回放）· `make smoke`（在线冒烟）· `make dev`（一键全跑） | `Makefile` · `scripts/devcheck/` · `scripts/dev/smoke.sh` |
| 单元 · 下发 | `Pull` 的投影（schema 版本 / 三段数据 / 校验和覆盖 payload / 确定性）· 未知 `policy_id` → `NotFound` · `Watch` → `Unimplemented` | `server_test.go` |
| 单元 · 回执 | `Ack` 落账 · 同适配器同版本幂等（覆盖不新增）· 缺 `adapter_id` → `InvalidArgument` · `applied=false` 必须带原因 | 同上 |
| 单元 · 响应改写规则 | `injects` 校验（空片段 / 未登记分类 → 报错）· 「未配置」与「空数组」可区分 · 载荷带 `inject_rules` 且**保持配置顺序** · 未配置时**省略**该字段 | 同上（`TestInjectsSectionValidation` · `TestInjectsProvidedFlag` · `TestPullCarriesInjectRules` · `TestPullOmitsInjectRulesWhenUnconfigured` · `TestPullEmitsExplicitEmptyInjectRules`） |
| 集成 | 端到端「配置 → 下发给适配器 → 适配器应用 → 真实改道」已实跑（见 [`../plans/2026-09-19-policy-plane.md`](../plans/ARCHIVE.md) §6）；装载→判定的端到端由 `make dev` 与装配层测试覆盖 | —— |
| 故障注入 | 无 —— 失败注入点是「配置非法」，已由单元测试穷尽 | —— |
| 属性测试 | 不适用 —— 校验器是有限枚举判定，表驱动用例已穷尽 | —— |
| 分支穷尽性 | **不适用** —— 本模块不实现判定规则（`MD-8` 的对象是判定器 `judge`） | —— |

## 8. 未决项

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | ✅ **已落地（2026-09-19）**：`Pull` + `Ack` 已实现（[ADR-0018](../background/decisions/0018-policy-plane-pull-model.md)）。**`Watch` 仍未实现** —— 需要「亚秒级生效」时再启用（契约已定义，不用改 proto） | 变更生效延迟（一个轮询间隔，默认 60s） | ADR-0018 失效条件 1 |
| 2 | ✅ **已落地（2026-09-19）**：回执实体 `policy_ack` 已登记进 [`../design/structure.md`](../design/structure.md) §3（键 `(policy_id, version, adapter_id)`，幂等覆盖）。**内存实现没有保留期** | 接真实 PostgreSQL 时按 `NI-13` 定保留天数 | structure.md §3 |
| 3 | 人工编写的配置文件与 PostgreSQL 策略台账的最终关系（谁写台账、文件是否仍为源） | 真实存储选型 | 同上 §6.3 |
| 4 | JSON Schema 与 Go 校验器的漂移防护（可用 schema 库或代码生成升级） | 长期一致性 | 同上 §6.4 |
| 5 | ✅ **已消费（2026-09-19）**：`whitelist` 经 `director`（核心内判定前置）与策略面（下发到适配器，`INT-25`）两处生效 | —— | —— |
| 7 | 🟡 **部分落地（2026-09-19）**：**响应改写规则**已接通（`injects` → `inject_rules` → 适配器 → `edge-injection`）；**仍未接通**的是**诱饵资产**与**预生成响应正文** —— 核心侧尚无它们「谁产出、存在哪」的定义 | 阶段 2b 的内容类能力（假路径自答 / 预生成正文）到不了边缘 | [ADR-0018](../background/decisions/0018-policy-plane-pull-model.md)「未解决」 |
| 6 | `rules[].match` 的 `headers` 寻址缺口（`Observation.Headers` 已采集但 `Field()` 取不到） | 一批 Agent 信号不可用 | 同上 §6.6 |

## 9. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 首版：配置装载 · 严格校验 · 不可变快照 · 版本台账 · `judge.RuleSource` 供给 | [`../plans/2026-09-18-policy-2a.md`](../plans/ARCHIVE.md)（9 项决策，用户已确认） |
| 2026-09-19 | **下发面落地**：`Server`（`Pull` 投影 / `Watch` 未实现 / `Ack` 落账）+ `PolicyStore` 扩 `RecordAck` / `Acks`；载荷契约新增 [`../spec/policy-payload.md`](../spec/policy-payload.md)；`PolicyAck` 扩 `adapter_id` / `reason`；测试 12 → 17 | [`../plans/2026-09-19-policy-plane.md`](../plans/ARCHIVE.md) · [ADR-0018](../background/decisions/0018-policy-plane-pull-model.md) · 用户确认（9 项推荐） |
