# `licensecheck`

依赖许可审计。拦住传染性或限制性许可，并产出许可台账。

| 项 | 值 |
| --- | --- |
| 阶段 | 1（Go 侧）· 3（Python 侧，2026-09-20 补） |
| 依据 | `TB-16` |
| 产出 | 台账 [`../../docs/spec/dependencies.md`](../../docs/spec/dependencies.md)（生成物） |

## 它是什么

理由不是洁癖：**通过 HTTP 提供服务即触发 AGPL 的披露义务** ——
一个 AGPL 依赖就能让本产品的源码变成必须公开的。所以这是架构级的约束，不是法务收尾工作。

审**两类**依赖，因为它们进交付物的方式不同、能拿到的事实也不同：

| 类别 | 依赖集合从哪来 | 许可声明从哪来 |
| --- | --- | --- |
| Go 模块 | `go list -deps` 反推（进二进制的全部模块） | `vendor/<模块>/LICENSE` 或本地模块缓存里的许可文件**正文** |
| Python 运行期依赖 | `analysis/requirements.txt` 出发的**传递闭包** | 已安装发行版的 `*.dist-info/METADATA`（`License-Expression` > `Classifier` > `License`） |

## 怎么跑

```sh
make licensecheck     # 审计，有问题则非零退出（门禁的一部分）
make license-ledger   # 重新生成 docs/spec/dependencies.md
go run ./scripts/licensecheck -ledger   # 直接输出台账到标准输出
```

可选参数（默认值就是本仓库的布局，一般不用给）：

```sh
go run ./scripts/licensecheck -py-lock analysis/requirements.txt -py-venv analysis/.venv
```

## 五个刻意的设计

**只审真正参与构建的东西。** Go 侧用 `go list -deps` 从包级依赖反推模块，
而不是 `go list -m all` 的完整模块图 —— 后者含大量不进二进制的间接依赖。
Python 侧同理：只审**运行期**依赖（`requirements.txt`）的传递闭包，
**开发期依赖**（`requirements-dev.txt` 的 `ruff` / `pytest` / `grpcio-tools`）不进交付物，不审。

**白名单式判定。** 认得出的宽松许可放行、认得出的限制性许可拦下，
**其余一律报「需人工判定」并让门禁失败**。反过来做（黑名单式）会让没见过的许可
静默通过 —— 而许可识别错的代价是法律风险，不是构建失败。

**多份许可取最严格的那条。** 双许可（如 `Apache-2.0` + `MIT`）看似宽松，
但 `Apache-2.0` + `GPL` 这种组合必须取 GPL 侧 —— 实际用得上的一定是较严的那份。
Python 侧的表达式（`A AND B` / `A OR B`）按同一规则取最严；
`WITH` 的右侧是例外条款（放宽），故只看左侧 —— 也是保守方向。

**Python 侧：依赖集合取锁文件，许可声明取已安装环境。** 锁文件是「声明要装什么」，
环境是「实际装了什么」。两者版本不一致时**必须失败**并指向 `make pyenv` ——
宁可门禁红，不可拿 A 版本的元数据去审定 B 版本的许可。

**Python 侧：认不出就失败，不猜。** 别名表只收**无歧义**的写法
（`3-Clause BSD License` 收，`BSD License` 不收 —— 说不清 2 条还是 3 条）。
猜一次就把「认不出」变成「认得出但认错」，后者的代价是法律风险。

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
| LGPL | 弱传染；Python 侧是动态链接，但仍需保留替换能力，从严对待 |
| GPL-2.0 / GPL-3.0 | 强传染 |
| RPL-1.5 / OSL-3.0 / CPAL-1.0 | 传染性或附加署名义务 |

放行的宽松许可：Apache-2.0 · MIT · BSD-2/3-Clause · ISC · MPL-2.0 · Unlicense ·
0BSD · Zlib · PSF-2.0 · CC0-1.0 · BlueOak-1.0.0 · WTFPL · X11 · Python-2.0 · BSL-1.0 · PostgreSQL · HPND。

> `BSL-1.0`（Boost）与 `BUSL-1.1`（Business Source License）是**两个不同的许可**，前者宽松、后者限制性。
> 表里刻意都留着，免得日后有人按「差不多」把它删掉一条。

## 怎么新增一条

- **Go 侧**：在 `main.go` 的 `signatures` 里加一条。**顺序即优先级** ——
  `AGPL` 必须排在 `GPL` 之前、`LGPL` 排在 `GPL` 之前，否则
  `GNU AFFERO GENERAL PUBLIC LICENSE` 会被 `GPL` 那条先命中。
- **Python 侧**：在 `python.go` 的 `spdxVerdicts`（SPDX 标识符）或 `spdxAliases`（声明里的别名）里加一条。

## 没做的事（边界）

- **不做依赖解析与版本约束求解**：只回答「在不在、许可是什么」。
  PEP 440 的约束语义（`~=` / `>=` / 环境标记求值）不在这里重造 —— `pip` 已经有了。
- **不逐文件核对夹带许可**：审的是依赖声明的许可，与 `TB-16` 的既有口径一致。
- **不含漏洞审计**：`TB-16` 同时要求「许可与漏洞审计」，本工具只做许可那一半。
- **无 venv 时 Python 侧直接失败**（报「先跑 make pyenv」），不静默跳过。
  这条不新增环境前置：`make gate` 的 `pylint` / `pytest` 本来就要求 venv。
- **没有单测文件**。这里的证据是**构造性反证**：篡改锁文件 / 元数据后工具**必须失败**
  （记录见 [`../../docs/plans/2026-09-20-ai-oss-reuse.md`](../../docs/plans/2026-09-20-ai-oss-reuse.md) §5–§6）。

## 状态

✅ **已实现**（Go 侧阶段 1；Python 侧 2026-09-20 补，依据 [ADR-0024](../../docs/background/decisions/0024-ai-oss-reuse-boundary.md) 决定 2）。
