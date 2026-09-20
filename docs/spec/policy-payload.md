# 策略载荷契约（边缘策略文档）

> 本文件是**跨进程契约**（接缝 **S4**，见 [`../design/structure.md`](../design/structure.md) §2.2）：
> 核心经 `api/policy/v1` 的 `PolicySnapshot.payload` 下发、L1 适配器消费。
> 依据：[`../design/architecture.md`](../design/architecture.md) 的 `AR-13`、
> [`../design/structure.md`](../design/structure.md) 的 `ST-8` / `ST-24`、[ADR-0018](../background/decisions/0018-policy-plane-pull-model.md)。
>
> **本文件不是规则文档** —— 它规定「下发的字节长什么样、什么算非法」。
> 与 [`../design/`](../design/README.md) 冲突时以 `design/` 为准。
>
> ⚠️ **两处实现必须同时改**：核心侧 `core/internal/policy/server.go` 的 `edgeDoc` 与
> 适配器侧 `edge/proxy/policy.go` 的 `edgePolicy`。两者**不能共享 Go 类型** ——
> 适配器**禁止** import `core/internal/`（`ST-3`，编译期强制），跨平面只允许 wire format（`TB-24`）。

---

## 1. 传输形状

| 项 | 值 |
| --- | --- |
| 载体 | `PolicySnapshot.payload`（`bytes`，内容是 UTF-8 JSON 对象） |
| 版本号 | 载荷内的 `schema_version`（整数）**与** `PolicySnapshot.version`（策略版本，单调递增） |
| 完整性 | `PolicySnapshot.checksum` = 对 **payload 字节**的 SHA-256（十六进制小写） |
| 拉取 | `Pull`（`api/policy/v1`）；适配器启动拉一次 + 按 `SHEN_PROXY_POLICY_INTERVAL` 轮询 |
| 回执 | `Ack{policy_id, version, adapter_id, applied, reason}`（`applied=false` 时 `reason` **必须**非空） |

**适配器必须先验校验和再应用**：不匹配时**禁止**应用，并回执 `applied=false`、`reason="校验和不匹配"`。
（核心与适配器当前是同主机明文 gRPC；这一层校验是唯一能发现「字节被改过」的手段。）

## 2. 字段表

```json
{
  "schema_version": 1,
  "policy_id": "core-rules",
  "version": 3,
  "backends": [
    { "name": "mirage", "address": "http://10.0.0.9:8080", "enabled": true }
  ],
  "whitelist": { "source_cidrs": ["10.0.0.0/8", "192.168.0.0/16"] },
  "inject_rules": [
    { "kind": "developer_api", "snippet": "<!-- ... -->", "marker": "</body>" }
  ],
  "inject_enabled": false,
  "content_manifest": {
    "version": 1,
    "selector": "session",
    "variants": 8,
    "entries": [
      { "resource": "/api/users", "profile_id": "site-a",
        "bodies": [ { "variant_id": 0, "content_id": "c-1a2b3c4d5e6f7081",
                      "checksum": "9f3c...", "body": "<html>...</html>", "marker": "</body>" } ] }
    ]
  }
}
```

> `inject_rules` 与 `content_manifest` 都是**可选**字段，且「不出现」与「空」语义不同 —— 见下表。

| 字段 | 类型 | 必填 | 含义与约束 |
| --- | --- | --- | --- |
| `schema_version` | int | ✅ | 载荷格式版本。当前唯一合法值 **`1`**。适配器读到更高的值**必须**拒绝应用（领域禁猜） |
| `policy_id` | string | ✅ | 策略集标识，与 `PolicySnapshot.policy_id` 一致 |
| `version` | uint64 | ✅ | 策略版本，与 `PolicySnapshot.version` 一致；单调递增，回滚即发布新版本 |
| `backends[]` | array | ✅（可为 `[]`） | 幻境后端表：逻辑名 → 可拨号地址。核实在 `route_mirage` 时给出的 `backend` 就是这里的 `name` |
| `backends[].name` | string | ✅ | 逻辑名。空串或含 `\x00` 非法 |
| `backends[].address` | string | ✅ | **必须**带 scheme 的 URL（`http://host:port` 或 `https://host:port`）。无 scheme / 非法 → 该条被**丢弃并记账**，不影响其余条目 |
| `backends[].enabled` | bool | ✅ | `false` = 该后端不下发到适配器的生效表 |
| `whitelist.source_cidrs[]` | string[] | ✅（可为 `[]`） | 免判定的来源网段（`INT-25`）。非法 CIDR → **整份载荷拒绝**（白名单写错会误伤运维探针，不能静默忽略） |
| `inject_rules[]` | array | ⚪ **可选** | 响应改写规则（注入到**改道侧** HTML 响应，`INT-8`）。**不出现** = 适配器继续用本地 env 规则；**显式空数组** = 明确「没有规则」（运营籍此主动关掉注入）。数组**顺序即执行顺序**，适配器不得重排 |
| `inject_rules[].kind` | string | ⚪ 可选 | 分类：`developer_api` / `instruction_file` / `hidden_link` / `dataset`（或空串 = 未分类）。**只用于组织与审计，不改变注入行为** —— 因此新增分类不需要改任何执行代码 |
| `inject_rules[].snippet` | string | ✅（该条存在时） | 注入片段，**非空**（空片段非法，整份载荷拒绝） |
| `inject_rules[].marker` | string | ⚪ 可选 | 插入位置标记；空 = 执行方用 `</body>`。找不到标记的规则被**跳过**（不阻断响应） |
| `inject_enabled` | bool | ✅ | **AI 欺骗内容注入的下发级开关**。`false` ⇒ 适配器**不注入内容**（上报 `inject=disabled`），判定与幻境转发照常；它**不**影响 `inject_rules`（静态规则有自己的显式关闭手段：空数组）。来源：核心配置 `ai.enabled`（默认 `false`） |
| `content_manifest` | object | ⚪ **可选** | AI 欺骗内容清单（**投影**自 [`ai-contract.md`](ai-contract.md) §3 的清单文件，**不含** `manifest_version` / `generated_at` / `generator`）。**不出现** = 没有内容（适配器报 `inject=no_content`）；**出现但 `entries: []`** = 同样没有内容（两者语义相同，都不算错） |
| `content_manifest.version` | uint64 | ✅（该对象存在时） | **内容**版本；轮换时递增。参与适配器的会话钉定（`(variant_id, version)`） |
| `content_manifest.selector` | string | ✅ | 变体选择器；当前唯一合法值 `session`（适配器读到别的值**必须**不注入并记 `no_content`） |
| `content_manifest.variants` | int | ✅ | N ≥ 1；变体总数。适配器取 `variant_id = fnv1a(会话键) mod N` |
| `content_manifest.entries[]` | array | ✅（可为 `[]`） | 按**资源**分组的条目；`resource` 是请求路径的**精确值** |
| `content_manifest.entries[].resource` | string | ✅ | 非空；同一份清单内不重复（核心已在装载期保证） |
| `content_manifest.entries[].profile_id` | string | ✅ | 画像标识（审计用） |
| `content_manifest.entries[].bodies[]` | array | ✅ | 该资源的各变体内容体；`variant_id` **必须**落在 `[0, variants)` |
| `content_manifest.entries[].bodies[].variant_id` | int | ✅ | 变体槽位；同一资源内不重复 |
| `content_manifest.entries[].bodies[].content_id` | string | ✅ | 内容标识（进逐请求事件，用于定位「这一条上的是哪份内容」） |
| `content_manifest.entries[].bodies[].checksum` | string | ✅ | `sha256(body)` 的小写十六进制；适配器注入**前**逐条验证，不符则该条不可用（报 `no_content`） |
| `content_manifest.entries[].bodies[].body` | string | ✅ | 内容体（非空）。单条上限 64 KiB（核心在装载期已限制） |
| `content_manifest.entries[].bodies[].marker` | string | ⚪ 可选 | 插入位置标记；空 = 执行方用 `</body>` |

**禁止**出现的字段：`gray_pct`（灰度在核心内按请求收敛，`INT-12` / `ADR-0018` §决定 9）、
规则与阈值（只在核心用）、诱饵资产内容（阶段 2b 未接通路，见 ADR-0018「未解决」）。
适配器**必须**忽略不认识的字段（向前兼容），但**必须**拒绝读不懂的 `schema_version`。

> `content_manifest` 是 `ai-contract.md` 的**投影**（同一份内容在两个地方出现：清单文件与载荷）。
> 改动它**必须**同时改：生成侧产出 · 核心侧投影 · 适配器消费 · 本文。两边字段名不一致时，
> 适配器**必须**按「内容不可用」处理（报 `no_content`，不报错、不阻断）。

## 3. 合并语义（适配器侧）

| 数据 | 与本地 env 的关系 | 为什么 |
| --- | --- | --- |
| 后端表 | **按名覆盖**：远端同名项覆盖本地；本地独有的项**保留** | 策略面是 `ST-24` 的正式通路；本地项是拉不到时的兜底（`NI-1`） |
| 白名单 | **并集**（只增不减） | 白名单是防误伤的护栏：缩小它会把内部 IP / 运维探针送进判定（`INT-25`） |
| 注入规则 | **字段不出现** → 保留本地 env 规则；**出现**（含空数组）→ 整份取代远端 | 内容类配置允许被替换；同时给运营一个「显式关掉注入」的手段（空数组） |
| AI 内容注入的开关 | `inject_enabled` 与适配器的本地兜底开关（`SHEN_PROXY_INJECT_CONTENT`）取**与**：两者都为真才注入 | 下发级开关让运营能**不改适配器配置**就停掉内容注入（`ADR-0023` 决定 4）；本地兜底保证策略面拉不到时也能急停 |
| 内容清单 | **整块取代**（不做合并）：远端清单是当前唯一事实 | 内容与版本必须自洽（版本 + 校验和）；合并两份清单无法定义"哪份的变体生效" |

失败语义（**任一都不影响请求路径**，`NI-1`）：拉取失败 → 沿用当前策略；校验和不匹配 → 拒绝应用并回执；
`schema_version` 读不懂 → 拒绝应用并回执；坏后端地址 → 只丢那一条。

## 4. 不变量（改动本契约时必须保持）

1. **确定性**：同内容必须产出同一串字节（字段序固定 + 数组按名排序），否则适配器每次轮询都会以为策略变了；
2. **校验和覆盖下发字节**：`checksum` 不是配置文件校验和（那是核心台账用的另一个值）；
3. **回执幂等**：同一 `(policy_id, version, adapter_id)` 重复上报只保留最新一条 —— 适配器重启会重报；
4. **失败不改写生效策略**：任何校验失败都不得留下「半应用」状态（远端状态是**整块原子替换**的）；
5. **内容体只在改道侧生效**：`content_manifest` 的存在**禁止**影响 `route_origin` 分支的任何字节（`INT-8` / `NI-1`）。
