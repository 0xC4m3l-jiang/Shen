# 模块：`session`

| 项 | 内容 |
| --- | --- |
| 模块名 | `session` |
| 所属层 | 核心 |
| 实现语言 | Go |
| 源码目录 | `core/internal/session/` |
| 负责人 | —— |
| 状态 | 阶段 1 已实现；阶段 2b 扩充会话状态与归因令牌 |
| 最后更新 | 2026-09-17 |

> 权威清单见 [`../design/modules.md`](../design/modules.md) §1.1。
> 本节之后固定九章，不适用时写「不适用」并说明原因。

## 1. 职责

### 做什么

- 按**三级优先级**提取会话身份：① 业务自身的 session cookie → ② TLS session ticket → ③ 源 IP + UA 指纹。
- 三级全不可用时退到指纹兜底，**始终返回一个可用的键**。
- **会话状态抽象**（阶段 2b，[ADR-0012](../background/decisions/0012-session-level-judgement.md)）：经 `StateSource` 读会话级特征
  （速度 / 挑战逃逸 / 首末次时间），供 `judge` 消费。
- **归因令牌**（阶段 2b，[ADR-0013](../background/decisions/0013-attribution-token.md)）：派生会话蜜标（HMAC）与凭证水印。

### 明确不做什么

- **不落库** —— 会话状态的读写由 `store.SessionStore` 负责（经 `StateSource` 接口），本模块只做提取与派生。
- **不在客户端留下新痕迹** —— 不得新增 cookie、不得新增响应头。
- **不把键回传或上行** —— 提取结果只在内核内部使用。
- **不持有状态** —— 会话状态存储在外；本模块是**无状态派生**（`AR-9`）。

## 2. 输入 / 输出契约

| 方向 | 契约 | 定义位置 |
| --- | --- | --- |
| 输入 | `contract.Observation`（首跳可用信息） | `core/internal/contract/` |
| 输出 | `contract.SessionKey`（键 + 来源 + 归因） | 同上 |
| 输出 | `contract.SessionFeatures`（速度 / 挑战逃逸 / 首末次时间，[ADR-0012](../background/decisions/0012-session-level-judgement.md)） | 同上 |
| 输出 | 归因令牌（蜜标 HMAC + 凭证水印，[ADR-0013](../background/decisions/0013-attribution-token.md)） | 同上 |

## 3. 依赖

| 允许依赖 | 原因 |
| --- | --- |
| `contract` | 共享类型 |
| 标准库 `crypto/sha256` · `crypto/hmac` | 指纹兜底与令牌派生需要稳定哈希 |
| `StateSource`（接口，由 `store.SessionStore` 实现） | 读会话特征（阶段 2b，[ADR-0012](../background/decisions/0012-session-level-judgement.md)） |

| 禁止依赖 | 原因 |
| --- | --- |
| `store` 具体类型 | 只依赖接口（依赖方向单向） |
| `net/http` | 只需要头里的字符串，不需要整个 HTTP 栈（也让单测更轻） |

## 4. 关键规则

| 规则 | 与本模块的关系 |
| --- | --- |
| `INT-19` | 三级优先级的顺序由本模块实现 |
| `INT-20` | 键与令牌**禁止**上行到业务、**禁止**回传客户端 |
| `NI-9` | **禁止**新增客户端可见的 Cookie 或响应头 —— 归因令牌不得以新 Cookie 形式落地 |
| `AR-9` | 无状态：会话状态存外（经 `StateSource`），不在进程内累积 |
| `ST-21` | 令牌密钥**禁止**有默认值；缺失时进程**必须**启动失败 |

## 5. 状态与生命周期

| 状态 | 存在哪里 | 生命周期 | 多副本一致性 |
| --- | --- | --- | --- |
| 会话身份 | 无（纯派生） | —— | 天然一致 |
| 会话特征 | `store.SessionStore`（Redis TTL，经 `StateSource`） | 短 TTL 滑窗 | 最终一致（外置） |
| 归因令牌密钥 | 启动期注入（**禁止**默认值，`ST-21`） | 进程生命周期 | 各副本一致（同密钥） |

## 6. 失败模式与降级

| 失败情形 | 行为 | 是否满足 `NI-1` | 依据 |
| --- | --- | --- | --- |
| 无业务 cookie | 退到优先级 ② | ✅ | `INT-19` |
| 也无 TLS ticket | 退到优先级 ③（指纹） | ✅ | 同上 |
| 三级全不可用 | 用零值 IP + 空 UA 算指纹，仍返回一个键 | ✅ | 不阻断、不 panic |

> 本模块**不返回错误** —— 会话身份提取失败不应成为业务链路上的失败点。

## 7. 测试

| 类型 | 覆盖什么 | 位置 |
| --- | --- | --- |
| 单元 | 优先级 ①：业务 cookie 命中 | `session_test.go` |
| 单元 | 优先级 ②：无 cookie 时退到 TLS ticket | 同上 |
| 单元 | 优先级 ③：退到指纹，且同输入得同键（稳定） | 同上 |
| 单元 | 提取过程不得改动请求头（无痕迹） | 同上 |

## 8. 未决项

| # | 未决 | 阻塞什么 | 去向 |
| --- | --- | --- | --- |
| 1 | cookie 名的来源 | 多租户接入 | ✅ 已定义：[`../spec/config.md`](../spec/config.md) §2.2 的 `session.cookie_name` |
| 2 | TLS session ticket 的真实获取方式 —— 当前用握手指纹**近似** | 会话身份准确度 | 接入层能力实测后确定 |
| 3 | 指纹档在换出口 IP 时会失效 | 会话粘性覆盖面 | 已知局限 |
| 4 | **令牌 TTL 的具体值** | 归因可靠性 vs 泄露窗口 | [ADR-0013](../background/decisions/0013-attribution-token.md) 未解决（实测） |
| 5 | **凭证水印的编码方式**（是否自然） | 不被识破为假凭证 | 同上 |
| 6 | 会话特征窗口时长（60s / 300s） | 速度信号校准 | 与阈值一起实测 |

## 9. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-17 | 首版：三级优先级提取 + 无痕迹断言 | 阶段 1 基础开发 |
| 2026-09-18 | 扩充：会话状态抽象（`StateSource`）+ 归因令牌（蜜标 + 凭证水印） | [ADR-0012](../background/decisions/0012-session-level-judgement.md) · [ADR-0013](../background/decisions/0013-attribution-token.md) |
