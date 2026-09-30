# `scripts/check/` —— 门禁与验证工具

**Makefile 只负责编排，判定逻辑都在这里。** 每个工具只做一件事，失败即非零退出。
平时用 make 目标即可；想单独跑某一项时看下表的「直接跑」列（均在仓库根执行）。

## 门禁（`make gate` / `make check` 的组成部分）

| 工具 | make 目标 | 直接跑 | 检查什么 | 依据 |
| --- | --- | --- | --- | --- |
| `gate.sh fmt` | `fmt-check` | `scripts/check/gate.sh fmt` | Go 文件已 gofmt（只检查、不改文件；vendor 除外） | `TB-15` |
| `gate.sh secrets` | `secrets-check` | `scripts/check/gate.sh secrets` | 已追踪文件里没有 `sk-` 真值、没有 `*_KEY/_TOKEN/_SECRET/_PASSWORD` 的非占位赋值 | 密钥门禁 |
| `gate.sh pydeps` | `check-pydeps` | `scripts/check/gate.sh pydeps` | `analysis/requirements*.txt` 的每条 `pkg==ver` 在 venv 里版本一致 | `TB-16` |
| [`archcheck/`](archcheck/) | `archcheck` | `go run ./scripts/check/archcheck` | 顶层目录、跨平面 import、store 唯一 I/O 出口、语言层数、CGO、护栏出口 | `ST-*` · `MD-*` · `TB-*` · `AR-33` |
| `tracecheck/` | `trace` | `go run ./scripts/check/tracecheck` | 模块文档 ↔ 代码 ↔ 单测、规则 ID 引用存在（豁免登记在 `allow.txt`） | `MD-2/17/22` · `D-3/D-8` |
| [`leakcheck/`](leakcheck/) | `leakcheck` | `go run ./scripts/check/leakcheck` | 字符串字面量不出现对外禁用词、响应头不回传决策与分数（豁免在 `allow.txt`） | `OH-1…OH-5` |
| [`licensecheck/`](licensecheck/) | `licensecheck` | `go run ./scripts/check/licensecheck` | Go / Python 依赖许可，拦 AGPL / SSPL / BUSL 等 | `TB-16` |
| `verify/` | `verify-evidence` | `go run ./scripts/check/verify -no-run` | 每个模块的证据链齐备（文档 · 规则依据 · 测试目标 · 功能场景） | `MD-17/22` |
| `bin/` | `tools` | `make tools` | 固定版本的 `staticcheck` / `errcheck`（本地缓存，不入库） | `TB-14/15` |

`internal/` 是 `archcheck` / `verify` / `tracecheck` / `leakcheck` 共用的解析包（模块清单、设计文档位置），不单独运行。
设计文档不在本仓库时，依赖它的检查会打印「跳过：设计文档不在本仓库」—— 这是**明示的跳过**，不是通过。

## 运行时验证（需要栈在跑，或自己起一个）

| 工具 | 入口 | 验证什么 |
| --- | --- | --- |
| `ui.py` | `scripts/dev.sh ui check` · `scripts/shen.sh ui-check` | 管控台 UI 真的显示出来：HTTP → 登录 → 浏览器逐页渲染 + 截图（见 [`../README.md`](../README.md) §3） |
| `smoke.sh` | `make dev` | 配置干跑 → 非法配置被拒 → 起核心 → 判定面冒烟与幂等 → 规则回放 → L4 近线分析（自己起核心、自己收尾） |
| `devcheck/` | `make smoke` | 对已在跑的核心发判定请求：响应形状 + 同一 `decision_id` 幂等（`ST-10`） |
| `doctor.py` | `make doctor` · `scripts/shen.sh doctor` | 接入自检 `INT-17` 五项：body 可读 · TLS 终结 / 透传 · 会话粘性 · 实境与幻境可区分 · 引擎在请求路径上 |
| `ai-inject.py` | `make ai-check` · `make ai-check-llm` | AI 欺骗内容注入端到端：关闭态字节一致 · 打开态注入 · `AR-30` 稳定性 · 护栏关卡 · 秒级关闭 · DAG |
| `render-dag.py` | `make ai-dag` | 把 `ai-inject.py --dag-out` 落盘的 DAG JSON 渲染成 Mermaid（报告用） |
| `verify/` | `make verify-modules` | 逐模块跑单测并出表（约 30s，不需要 Docker） |
| [`fingerprint/`](fingerprint/) | `make fp-capture` · `make fp-diff` | TLS 指纹采集与对比（实验 E2：经引擎与直连是否可区分） |

`doctor.py` 的结果有四种：**通过 · 失败 · 约束**（验到了但限制你能做什么）**· 无法判定**（写明原因）。
完整说明见 [`docs/integrate/doctor.md`](../../docs/integrate/doctor.md)（设计文档与仓库分开发布）。

## 纪律

- **工具缺失时必须失败，不得跳过。** 静默跳过等于假绿，比没有门禁更糟：
  `staticcheck` / `errcheck` 没装、`analysis/.venv` 不存在时，对应目标直接报错并指向 `make tools` / `make pyenv`。
- **豁免逐条登记并写理由**（`tracecheck/allow.txt` · `leakcheck/allow.txt`），禁止整目录豁免；字面量消失后豁免会报「已过期」。
- **`secrets` 只扫已追踪文件**：新文件没 `git add` 时全绿是假象。测试里要用假密钥时让它**不长成真值的形态**（如带连字符 `sk-live-…`），不要给测试文件开豁免。
- 改检查本身时先写红用例（`analysis/tests/test_gate_pydeps.py` 就是 `gate.sh pydeps` 的红绿矩阵）。
