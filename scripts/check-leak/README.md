# `check-leak`

| 项 | 值 |
| --- | --- |
| 阶段 | **1 起**（每次改响应面字符串都要过） |
| 依据 | `OH-1` · `OH-2` · `OH-3` · `OH-4` · `OH-5`（[`docs/design/constraints.md`](../../docs/design/constraints.md)） |
| 门禁 | `make leakcheck`，已接进 `make lint` / `make gate` / `make check` |
| 状态 | ✅ **已实现**（2026-09-19） |

## 它是什么

可观测面泄漏扫描 —— 扫描**字符串字面量**，对照 `OH-1` 禁用清单；并检查 `OH-5`
（决策结果与风险分数**禁止**经响应头回传）。

判据始终是 `OH-2` 那一句：**这个字符串会不会出现在攻击者的屏幕上？**
会 → 禁止；不会 → 允许（行业词出现在源码标识符、配置键、内部日志里是 `TM-6` 明确允许的）。

## 怎么跑

```sh
make leakcheck          # 或：go run ./scripts/check-leak
```

零退出码 = 通过。失败时逐条打印 `文件:行号 + 命中的字面量`，并给出两条出路。

## 扫描范围

从 [`docs/design/modules.md`](../../docs/design/modules.md) §1.1 **解析**出模块目录，
再按「响应面模块」筛选（每个模块都写了入选理由，见 `main.go` 的 `responseSurfaceModules`）：

| 入选 | 理由（为什么它的文本可能上攻击者的屏幕） |
| --- | --- |
| `modules/deception/proxy` · `modules/deception/mirror` · `modules/deception/injection` | 响应头、错误页、403、注入后的蜜罐侧响应体 |
| `common/core/internal/responder` | 伪造响应内容 |
| `common/core/internal/decoy` | 诱饵内容与**投放片段**（会被投放进客户环境） |
| `common/core/internal/judge` · `director` · `control` · `isolation` · `honeypot` | 判定与决策的取值、后端名、对外行为描述 |

不在范围内的（依据 `OH-2` 适用位置表）：配置面（`policy`）、存储面（`store`）、
会话与遥测（`session` / `telemetry`）—— 它们的字符串不进入响应。

> 清单**不硬编码**：模块改名或消失时检查会**直接失败**（拒绝少扫），而不是静默放过。

## 三类豁免（都不是「整目录豁免」，`OH-4` 禁止后者）

| 类型 | 形式 | 依据 |
| --- | --- | --- |
| 规则级 | 内部日志调用的参数 · 错误构造的参数（`errors.New` / `fmt.Errorf`） · struct tag（配置键） · **import 路径**（编译期标识符） | `OH-2` 适用位置表：「服务端配置与内部日志」允许；错误只上行到适配器并被折叠成 fail-open（`NI-3`），且 `ST-7` 另行禁止回显；import 路径只进符号表与构建元数据，不上攻击者的屏幕 —— 顶层目录名一旦含泄漏词（如 ① 欺骗层 `modules/deception/`）就靠这条避免满仓库误报 |
| 文件级 | 文件自己声明 `//check-leak:filter <理由>`（例：`responding` 的 AR-22 黑名单表**必须**逐字包含禁用词，否则拦不住模型自曝）；检查会把它打印出来供人核对 | `OH-4`「逐条显式声明并附理由」 |
| 逐条 | [`allow.txt`](allow.txt)：`<检查ID> <文件> <字面量>  # 理由` —— **精确到文件 + 字面量**，同一字面量换一个文件要重新登记 | `OH-4` |

豁免**会腐烂**：字面量消失后检查会报「豁免已过期」，必须删掉那一行 —— 与 `tracecheck`
的 `allow.txt` 同一规矩（不许僵尸条目）。

## 它**不**保证什么

- 它只扫**字符串字面量**。拼接出来的泄漏（例如从配置读来的值、模板变量）扫不到 —— 那部分只能靠 `OH-2` 的人工核对与集成测试；
- 仓库名 / 二进制名 / 服务名一项目前无从扫描：项目名尚未定（[ADR-0004](../../docs/background/decisions/0004-terminology.md)）。定名后**必须**把它们加进 `banned` 清单。
