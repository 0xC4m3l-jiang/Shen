# 变更包：转发/欺骗路径硬化 —— 修掉对外可见面的代理栈指纹（`OH-2`）+ 蜜罐范围裁定

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | ① 用户裁定「**蜜罐先只做接入架构**，内容与协议栈待专项调研」——登记并同步文档 ② 按该裁定**复查前置的流量转发与流量欺骗**：实测查出两处 **`OH-2` 违规**（`Via: 1.1 Caddy`、错误响应 `Server: Caddy`）并修复 + 锁测试 |
| 日期 | 2026-09-19 |
| 状态 | 已验证（实测三种情形 + 单元 5 子例 + 端到端 1 例；`make gate` 通过；已提交） |
| 改动分级 | **M**（改一个模块的响应头行为 + 测试 + 文档；不改对外契约、不改规则） |
| 涉及模块 | `adapter-proxy`（[`../design/modules.md`](../design/modules.md) §1.1 第 11 行）· `honeypot-protocol`（第 14 行，范围收窄为接入架构）· `honeypot-shell`（第 15 行，推迟） |
| 决策数 | 已答 2 项（用户：蜜罐范围；`Via`/`Server` 的处理方式按规则推导）/ 待定 0 |
| 关联 | [`../design/constraints.md`](../design/constraints.md) 的 `OH-1` / `OH-2` · [ADR-0011](../background/decisions/0011-honeypot-entry-external-backends.md) · [`2026-09-19-deception-l2-l3-frameworks.md`](2026-09-19-deception-l2-l3-frameworks.md) |

---

## 1. 需求与验收

**要解决什么**：用户要求「蜜罐先有接入架构就好，内容后续再调研；**先确认前置的流量转发、流量欺骗是否已经完善**」。
于是对转发路径做了一次**实测式复查**（不看代码猜，直接起真进程看客户端收到什么），查出两处**对手可见的栈指纹**：

| # | 现象（实测） | 违反 |
| --- | --- | --- |
| 1 | 正常转发时响应头含 `Via: 1.1 Caddy` | `OH-2` 适用位置表：HTTP 响应头**禁止**出现内部痕迹 —— 它直接告诉对手「中间有个 Caddy」 |
| 2 | 上游不可达时 502 响应头含 `Server: Caddy` | `OH-2` 表：「`Server` 头 **禁止**（**必须**与上游一致或直接透传）」 |

**做完之后**：客户端在三条路径上都看不到我们的代理栈：正常转发（上游 `Server` 原样透传、无 `Via`）·
上游没给 `Server`（客户端看不到任何 `Server`）· 上游不可达的 502（既无 `Server` 也无 `Via`，状态码不变）。

**验收判据**：

1. `Via` 在**任何**响应里都不出现（含错误响应），且**不转发给上游**（幻境后端若回显请求头也不会露出）。
2. `Server`：上游给了就**原样保留**；只有它等于 Caddy 默认值时删除；错误路径（不经过中间件）由 `BuildConfig` 的 `errors` 路由删。
3. 业务侧响应**其他头与状态码一个字都不改**（`INT-8`）。
4. 单测覆盖清洗器语义（5 子例）+ 端到端覆盖错误路径（1 例）+ 转发语义测试补 `Via` 断言。
5. 蜜罐范围的裁定登记进模块文档（`honeypot-shell` 标推迟、`honeypot-protocol` 接入架构已就绪）、进度表与根 README。
6. `make gate` 绿；本轮已提交。

**不做什么**：

- **不实现蜜罐内容**（命令表 / 内存文件系统 / 会话水印 / 真实协议栈）—— 按用户裁定推迟到专项调研；
- **不改任何规则**（本轮是**按规则修实现**，不是改规则）；
- **不做 HTTP/3 与 WebSocket 的专项验证**（见 §7 遗留）。

---

## 2. 设计逻辑

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 蜜罐范围（用户裁定） | **只做接入架构**：入口 · 后端池 · 协议契约 · 生命周期；内容层推迟 | 蜜罐内容需要专项调研（`ADR-0011` 明确「默认接第三方」） | `docs/modules/honeypot-shell.md` 状态 · `docs/modules/honeypot-protocol.md` §8 |
| ② | `Via` 怎么处理 | **一律删除**（响应与请求两侧） | 它 100% 是**我们这一跳**的产物（`reverse_proxy` 加的），规则不给它留位置 | `handler.go` 的 `headerSanitizer` + 请求侧 `Header.Del("Via")` |
| ③ | `Server` 怎么处理 | **只在等于 Caddy 默认值（`Caddy`）时删**；上游值一律保留 | `OH-2` 要求「与上游一致或直接透传」；无差别删除会把上游的 `Server` 也抹掉（那也算改写业务响应） | 同上 |
| ④ | 错误路径怎么办 | 在 `BuildConfig` 里加 **`errors` 路由**删 `Server` / `Via` | 实测证明错误响应由 Caddy **服务器层**写出，不经过我们的中间件 —— 只改中间件会漏掉 502 | `embed.go` |
| ⑤ | 要不要设置 `caddyhttp.ServerHeader` | **不要** | 实测确认它是**包级变量且在 init 时已拷贝成切片**，运行时改它无效；用 `errors` 路由才是可靠点 | —— （决策记录在此，避免后人重复尝试） |

**数据流（两处清洗点，缺一不可）**：

```text
请求 ──► 我们的中间件 ──┬─ ①请求侧：删 Via（别让上游/幻境后端看到）
                       └─ ②响应侧：headerSanitizer
                              · 删 Via
                              · 若 Server == "Caddy" → 删；否则原样（上游值）
                                    │
                                    ├─ 正常路径：reverse_proxy 写响应 → 经清洗器 ✓
                                    └─ 错误路径（上游不可达等）：由 Caddy 服务器层写
                                            → 经 BuildConfig 的 errors 路由删头 ✓（③实测已证）
```

---

## 3. 追溯矩阵

| 规则 ID | 模块文档 | 代码 | 测试 | 验证命令 |
| --- | --- | --- | --- | --- |
| `OH-2`（判据：会不会出现在攻击者屏幕上） | [`../design/constraints.md`](../design/constraints.md) §OH-2 适用位置表 | `handler.go` 的 `headerSanitizer` + `embed.go` 的 `errors` 路由 | `TestHeaderSanitizer*`（5 子例）· `TestNoProxyFingerprintOnErrorPath` | `make gate` + 实跑 |
| `OH-1`（禁用词不得出现在对外可见面） | 同上 §OH-1 | 同上（本轮的清洗也覆盖它关注的面） | `make leakcheck` | `make leakcheck` |
| `INT-8`（禁止改写业务侧响应） | [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) §1 | 清洗器**只**动 `Via` / 默认 `Server` | `TestHeaderSanitizerKeepsOtherHeaders` | `make gate` |
| `NI-1`（不影响原始业务） | 同上 §6 | 错误路径仍返回 502 且不改状态码 | `TestNoProxyFingerprintOnErrorPath`（断言 502） | `make gate` |
| `ADR-0011`（蜜罐可选自研） | [`../modules/honeypot-shell.md`](../modules/honeypot-shell.md) §1 | 本轮**不实现**内容层（推迟） | —— | 文档评审 |

---

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `edge/proxy/handler.go` | 改 | 新增 `headerSanitizer`（删 `Via`；仅当 `Server` 为 Caddy 默认值时删；透传 `Flush` / `Hijack` / `Unwrap`）· `ServeHTTP` 入口套清洗器并删请求侧 `Via` · `caddyDefaultServerHeader` 常量 |
| `edge/proxy/embed.go` | 改 | `Server.Errors` 加一条路由：`{"handler":"headers","response":{"delete":["Server","Via"]}}`（错误路径的清洗点） |
| `edge/proxy/proxy_test.go` | 改 | 新增 3 个测试（含 5 个子例）：默认值删除 / 上游值保留 / 大小写与空白 / `Write` 路径 / 只动这两个头 |
| `edge/proxy/embed_test.go` | 改 | 新增 `TestNoProxyFingerprintOnErrorPath`（上游不可达 → 502 且无指纹）；给转发语义测试补 `Via` 断言 |
| [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) | 改 | §1 增「可见面卫生」为第 3 项职责 · §4 增 `OH-1`/`OH-2` 行 · §6 增「上游不可达（错误路径）」行 · §7 增两类测试 · §9 增变更记录 |
| [`../modules/honeypot-shell.md`](../modules/honeypot-shell.md) | 改 | 状态改为 ⏸ **推迟**（先接入架构，内容待调研） |
| [`../progress.md`](../progress.md) · [`../../README.md`](../../README.md) | 改 | 第 15 行标推迟；阶段 3 行改为「蜜罐只做接入架构」 |

**必须遵守的上位约束**：`OH-1` / `OH-2` · `INT-8` · `NI-1` · `ADR-0011`。

---

## 5. 测试与场景

**实测（真进程 · 真转发，客户端视角的响应头）**：

| # | 场景 | 修复前 | 修复后（实测） |
| --- | --- | --- | --- |
| 1 | 上游自带 `Server` + 正常转发 | `Via: 1.1 Caddy` 存在 | ✅ 无 `Via`；上游 `Server` 原样（实测保留了上游自己的两行 `Server`） |
| 2 | 上游**不带** `Server` | `Via` + `Server: Caddy` | ✅ 只剩上游默认值，**没有** `Server: Caddy`、**没有** `Via` |
| 3 | 上游不可达（502） | `Via` + `Server: Caddy` | ✅ 502 · 无 `Server` · 无 `Via` · `Content-Length: 0` |

**单元与端到端（`make gate` 覆盖）**：

| # | 场景 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- |
| 4 | `Server` 恰为 `Caddy` / `caddy` / ` Caddy ` | 都删除 | ✅ | `TestHeaderSanitizerStripsProxyFingerprints` |
| 5 | `Server` 为上游值（`nginx/1.24.0`） | 原样保留 | ✅ | 同上 |
| 6 | 只 `Write` 不 `WriteHeader` | 同样清洗 | ✅ | `TestHeaderSanitizerAlsoCleansOnWrite` |
| 7 | 业务响应头与状态码 | 一个字都不改（`INT-8`） | ✅ | `TestHeaderSanitizerKeepsOtherHeaders`（含 `Content-Type` / 自定义头 / 201） |
| 8 | 错误路径指纹 | 502 且无 `Server` / `Via` | ✅ | `TestNoProxyFingerprintOnErrorPath` |
| 9 | 正常转发无 `Via` | 无 | ✅ | `TestForwardingSemanticsWithTLS`（本轮补断言） |

**没有覆盖的**：

- **HTTP/3（h3）与 WebSocket 升级**的专项验证：本轮的清洗器与 `errors` 路由都不该影响它们（清洗器透传 `Flush`/`Hijack`，且 h3 的响应头同一路径），但**没有实测**；
- **`Alt-Svc` 头**：Caddy 会广播 h3 支持（内容形如 `h3=":端口"`）。真实站点启用 h3 时也有这个头，**判定为正常**，未做处理 —— 若将来要求「与上游完全一致」需再议；
- **多个上游 `Server`** 的情形（上游自己发多行）：实测原样透传（未做去重）——保留原样更符合「透传」。

---

## 6. 验证证据

```console
# 修复前（实测）
$ curl -s -D - -o /dev/null http://127.0.0.1:18300/
HTTP/1.1 200 OK
Server: BaseHTTP/0.6 Python/3.14.6
Via: 1.1 Caddy                       ← 对手可见的栈指纹

# 修复后（实测）
$ curl -s -D - -o /dev/null http://127.0.0.1:18301/
HTTP/1.1 200 OK
Server: nginx/1.24.0                 ← 上游的值：原样透传
（无 Via）

$ curl -s -D - -o /dev/null http://127.0.0.1:18303/     # 上游不可达
HTTP/1.1 502 Bad Gateway
（无 Server、无 Via）

$ make gate
门禁通过。

$ go test ./edge/proxy/ -count=1
ok  shen/edge/proxy      # 含本轮新增 4 个测试（8 例断言）
```

**关键指标**：修掉 **2 处** `OH-2` 违规 · 新增测试 **4 个**（`edge/proxy` 39 → 43）· 清洗点 **2 处**（正常路径 + 错误路径）· 业务响应改写 **0 处**（`INT-8` 未动）· 规则改动 **0 条**。

---

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | **蜜罐内容层推迟**（命令表 · 内存 FS · 会话水印 · 真实协议栈） | 蜜罐「像不像真的」暂无实现 | 专项调研后设计（用户裁定） |
| 2 | HTTP/3 与 WebSocket 未实测 | 清洗器是否影响升级类请求未验证（代码上透传 `Hijack`/`Flush`） | 下一次转发验证一并做 |
| 3 | `Alt-Svc`（h3 广播）保持默认 | 与「Server 必须与上游一致」的严格口径未对齐 | 待评估：真实站点也常见该头，暂判正常 |
| 4 | 流式（SSE / 分块）与大文件传输未在本轮复测 | 注入 transport 只缓冲小 HTML（既有设计），但未再跑一次 | 下一次转发验证一并做 |
| 5 | 上游发多行 `Server` 时原样透传（未去重） | 少数站点会看到两个 `Server` 行 | 若客户要求「单行」再处理 |

---

## 7.1 审视记录

| # | 发现 | 类型 | 证据（命令 / 位置） | 动作 | 结果 |
| --- | --- | --- | --- | --- | --- |
| 1 | `docs/modules/adapter-proxy.md` 全文没有提「对外可见面卫生」，而实现的响应头行为直接决定 `OH-1`/`OH-2` 是否成立 | 漏写 | 模块文档 §1/§4/§6 | 增第 3 项职责 + `OH` 规则行 + 失败模式行 + 测试行 | ✅ |
| 2 | `docs/modules/honeypot-shell.md` 状态仍写「设计（阶段 3）」，与用户新裁定（先接入架构）不一致 | 过期状态 | 用户裁定 | 改为 ⏸ 推迟并写明原因 | ✅ |
| 3 | `docs/progress.md` 第 15 行、根 `README.md` 阶段 3 行未反映「蜜罐只做接入架构」 | 过期状态 | `docs/progress.md` | 同步 | ✅ |
| 4 | 上一轮把 `honeypot-protocol` 的框架做完后，未在文档里明确「协议栈属内容层，随蜜罐内容一起调研」 | 边界不清 | `docs/modules/honeypot-protocol.md` §8 | 已在 §8 列明（上轮加的第 3 条未决项即此） | ✅ |
| 5 | 尝试通过 `caddyhttp.ServerHeader` 关掉 `Server` 头会**无效**（包级变量在 init 时已拷贝） | 认知（避免后人踩坑） | `modules/caddyhttp/server.go:306-307`（上游源码） | 记录在 §2 决策表第 ⑤ 条 | ✅ |

> 历史记录类文件（`docs/background/` · `docs/plans/` · `docs/log.md` · `docs/kb/`）不在审视范围。

---

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-19 | 转发/欺骗路径硬化：清掉 `Via`（响应 + 请求两侧）· `Server` 仅删 Caddy 默认值 · 错误路径经 `errors` 路由清洗；新增 4 个测试（`edge/proxy` 39 → 43）；蜜罐范围收窄为「接入架构」并同步四份文档 | 用户裁定（蜜罐范围 + 复查转发/欺骗）· `OH-1` / `OH-2` · `INT-8` · `NI-1` |
