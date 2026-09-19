# `licensecheck`

依赖许可审计。拦住传染性或限制性许可，并产出许可台账。

| 项 | 值 |
| --- | --- |
| 阶段 | 1 |
| 依据 | `TB-16` |

## 它是什么

理由不是洁癖：**通过 HTTP 提供服务即触发 AGPL 的披露义务** ——
一个 AGPL 依赖就能让本产品的源码变成必须公开的。所以这是架构级的约束，不是法务收尾工作。

它读 `go.mod` 的依赖在本地模块缓存里的 `LICENSE` / `COPYING` / `NOTICE`，
按文本特征判定许可类型。

## 怎么跑

```sh
make licensecheck     # 审计，有问题则非零退出（门禁的一部分）
make license-ledger   # 重新生成 docs/spec/dependencies.md
go run ./scripts/licensecheck -ledger   # 直接输出台账到标准输出
```

## 三个刻意的设计

**只审真正参与构建的模块。** 用 `go list -deps` 从包级依赖反推模块，
而不是 `go list -m all` 的完整模块图 —— 后者含大量不进二进制的间接依赖，
审它们没有意义，还会把门禁淹掉。当前真正参与构建的只有 7 个模块。

**白名单式判定。** 认得出的宽松许可放行、认得出的限制性许可拦下，
**其余一律报「需人工判定」并让门禁失败**。反过来做（黑名单式）会让没见过的许可
静默通过 —— 而许可识别错的代价是法律风险，不是构建失败。

**多份许可取最严格的那条。** 双许可（如 `Apache-2.0` + `MIT`）看似宽松，
但 `Apache-2.0` + `GPL` 这种组合必须取 GPL 侧 —— 实际用得上的一定是较严的那份。

## 禁止的许可

| 许可 | 为什么要拦 |
| --- | --- |
| AGPL-3.0 | 通过网络提供服务即触发源码披露义务 |
| SSPL | 要求公开整个服务栈的源码 |
| BUSL-1.1 | 商用受限，若干年后才转开源 |
| Elastic-2.0 | 禁止把本产品作为托管服务提供 |
| Commons Clause | 禁止转售 |
| CC-BY-NC | 禁止商用 |
| JSON | 条款含用途限制，不是自由许可 |
| LGPL | Go 是静态链接，LGPL 要求的可重链接无法满足 |
| GPL-2.0 / GPL-3.0 | 强传染，静态链接即需开源 |
| RPL-1.5 / OSL-3.0 / CPAL-1.0 | 传染性或附加署名义务 |

放行的宽松许可：Apache-2.0 · MIT · BSD-2/3-Clause · ISC · MPL-2.0 · Unlicense ·
0BSD · Zlib · PSF-2.0 · CC0-1.0 · BlueOak-1.0.0 · WTFPL · X11。

## 怎么新增一条

在 `main.go` 的 `signatures` 里加一条。**顺序即优先级** ——
`AGPL` 必须排在 `GPL` 之前、`LGPL` 排在 `GPL` 之前，否则
`GNU AFFERO GENERAL PUBLIC LICENSE` 会被 `GPL` 那条先命中。

## 状态

✅ **已实现。**
