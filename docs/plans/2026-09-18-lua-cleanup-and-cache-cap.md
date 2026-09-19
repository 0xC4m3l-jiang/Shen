# 变更包 · 2026-09-18 · 清理 Lua 残留设计 + 判定缓存容量上限（MD-10）

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 按 ADR-0008 清理 6 处 Lua 残留文档 + 修 `edge/proxy` 缓存无容量上限的 MD-10 缺口 |
| 日期 | 2026-09-18 |
| 状态 | 已实现 |
| 涉及模块 | `adapter-proxy`（edge/proxy）· 全部模块（文档清理） |
| 决策数 | 已答 1 项（缓存满则清空）/ 待定 0 项 |
| 关联 | [`../background/decisions/0008-edge-language-go.md`](../background/decisions/0008-edge-language-go.md) · `docs/Log.md` 同日条目 |

## 1. 需求与验收

**要解决什么**：① ADR-0008 已拍板「L1 用 Go、移除 Lua」，但 6 处文档仍写着 Lua/OpenResty/adapter-reverse-proxy，误导后续开发；② `edge/proxy` 的判定缓存只有 TTL 无容量上限，违反 `MD-10`。

**做完之后，用户能做什么 / 看到什么**：文档与代码不再有 Lua 残留歧义；判定缓存内存有界，符合 `MD-10`。

**验收判据**：

1. 现行文档（`docs/design/` · `docs/modules/` · `docs/README.md` · `docs/progress.md` · `AGENTS.md`）里不再有「Lua 是实现语言」的漂移。
2. 缓存有容量上限，且上限可测。
3. `make gate` 全绿。

**不做什么**：

- 不删 `language.md` 的 Lua 否决记录、`archcheck` 的 `.lua` 门禁、`AGENTS.md` 的「Lua 已移除」说明 —— 它们是决策依据与防回归，不是漂移。
- 不动 `edge/mirror` 把 body 前缀塞 header 的 hack（契约无 body 字段，留待契约演进）。
- 不动 block 决策的 403 可见性（设计议题，非本轮）。

## 2. 设计逻辑

**已确认的决策**：

| # | 问题 | 决定 | 理由 | 落地位置 |
| --- | --- | --- | --- | --- |
| ① | 缓存超限怎么淘汰 | 满则整体清空，不做逐条 LRU | 缓存本就可丢失（未命中重调核心，语义等价）；清空比 LRU 简单，且能抗「大量不同路径填满缓存」的对抗性填满 | [`../../edge/proxy/proxy.go`](../../edge/proxy/proxy.go) 的 `decisionCache.put` |

**数据流（失败路径）**：缓存满 → 清空 → 下一请求未命中 → 调核心判定 → 核心失败也折叠成放行（`NI-3`/`NI-4`/`NI-5`），业务不受影响。

## 3. 追溯矩阵

| 规则 ID | 模块文档 | 代码 | 测试 | 验证命令 |
| --- | --- | --- | --- | --- |
| `MD-10`（缓存必须有容量上限） | `edge/proxy/iface.go` 的 `CacheMaxEntries` 注释 | `edge/proxy/proxy.go` 的 `decisionCache.put` | `proxy_test.go::TestCacheRespectsCapacityCap` | `make gate` |
| ADR-0008（Lua 移除，L1 用 Go） | `docs/design/architecture.md` §7.5/§8.2 · `docs/design/structure.md` §4.1 · `docs/modules/README.md` §4.2 | 无代码变更（文档清理） | — | `make trace` |

## 4. 代码实现

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| `edge/proxy/iface.go` | 改 | `Config` 加 `CacheMaxEntries` 字段 |
| `edge/proxy/proxy.go` | 改 | `decisionCache` 加 `cap` 字段 + `put` 满则清空；`New` 取默认值 65536 |
| `edge/proxy/cmd/proxy/main.go` | 改 | 新增环境变量 `SHEN_PROXY_CACHE_MAX` |
| `edge/proxy/proxy_test.go` | 改 | 加容量上限单测 |
| `docs/modules/README.md` | 改 | §4.2 去掉 adapter-reverse-proxy/sidecar 与 Lua，改写为 adapter-proxy 现状 |
| `docs/modules/_template.md` | 改 | 语言列表去掉 Lua |
| `docs/design/architecture.md` | 改 | §7.5 部署拓扑与 §8.2 四形态表的 OpenResty/adapter-reverse-proxy/sidecar |
| `docs/design/structure.md` | 改 | §4.1 部署拓扑的 OpenResty |

## 5. 测试与场景

| # | 场景 | 输入 / 前置 | 期望 | 实测 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 1 | 缓存容量上限 | cap=2，连 put 3 个不同 id | 条目数 ≤ 2 | ✅ | `TestCacheRespectsCapacityCap` |
| 2 | Lua 残留归零 | `grep -rn Lua docs/design docs/modules` | 仅剩否决记录/门禁 | ✅ | 见 §6 |

## 6. 验证证据

```console
$ make gate
门禁通过。  （fmt · vet · staticcheck · errcheck · archcheck · trace · licensecheck · go test -race）
```

## 7. 遗留与未决

| # | 遗留 | 影响 | 去向 |
| --- | --- | --- | --- |
| 1 | `edge/mirror` 把 body 前缀塞进 header（`x-observed-body-prefix`） | 契约无 body 字段时的权宜 | 契约演进时定 |
| 2 | block 决策返回 403，是对手可见的处置 | 欺骗系统的可见性权衡 | 设计议题 |

## 7.1 审视记录（L 档）

| # | 发现 | 类型 | 证据 | 动作 | 结果 |
| --- | --- | --- | --- | --- |
| 1 | 6 处文档 Lua/OpenResty 漂移 | 漂移 | `grep -rn "Lua\|OpenResty" docs/design docs/modules` | 全部改为 Go / 现状 | ✅ 归零（保留否决记录） |
| 2 | `newDecisionCache` 签名变更后调用点 | 一致性 | `go build ./...` | 两处调用点（`New` + 测试）同步更新 | ✅ 编译通过 |
| 3 | `language.md` / `archcheck` / `AGENTS.md` 的 Lua 字样 | 保留项 | 逐一核对语义 | 否决记录与门禁保留（非漂移） | ✅ 不误删 |

## 8. 变更记录

| 日期 | 变更 | 依据 |
| --- | --- | --- |
| 2026-09-18 | 清理 Lua 残留 + 缓存容量上限 | ADR-0008 · MD-10 |
