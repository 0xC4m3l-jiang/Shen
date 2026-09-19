# 语言选择

> 规则 ID 前缀 `TB`。（承接原 `tech-baseline.md` §1 的前缀；原 `TB-1`（已废弃·在线组件必须 Rust） / `TB-3` 已废弃，见 [`README.md`](README.md) §4.1。）
> 写作要求见 [`README.md`](README.md) §2。
>
> **已确认**：用户于 2026-09-17 确认 **TB-2、TB-4、TB-14、TB-15、TB-16、TB-20…TB-26 全部通过**，
> 即**采纳分层多语言**（[ADR-0005](../background/decisions/0005-layered-languages.md)）。
> 其中**核心层的语言（Go vs Rust）**已由 [ADR-0006](../background/decisions/0006-core-language-go.md) 正式复查并**确认维持 Go**。
> **来源**：外部设计稿 [`../background/research/deception-engine-design.md`](../background/research/deception-engine-design.md) §二。
> **已确认**：用户于 2026-09-17 确认 **TB-14 / TB-15 / TB-16 / TB-20…TB-26 全部通过**。
> 本文件取代「在线全 Rust + 离线 Python」的旧结论（[ADR-0001](../background/decisions/0001-language-rust.md) → [ADR-0005](../background/decisions/0005-layered-languages.md)）。

---

## 1. 分层选型总表

| 层级 | 职责 | 语言 | 关键理由 |
| --- | --- | --- | --- |
| **L0 接入层** | 流量镜像、TLS 卸载、路由 | **C / Rust**（或直接用现成组件） | 这层**不自研**，复用 Envoy / Nginx / HAProxy |
| **L1 边缘欺骗层** | 投毒 · 假路径 · 引流**执行** | **Go**（[ADR-0008](../background/decisions/0008-edge-language-go.md)） | 独立反向代理进程，**前置代理与 Pod 内边车两种形态共用一份实现**；**判定逻辑不在此层**。转发与 TLS 终结用**内嵌 Caddy** 库承担，本层只写判定胶水（[ADR-0017](../background/decisions/0017-caddy-l1-base.md)） |
| **核心**（判定 + 响应生成） | 决策、会话、隔离、策略、遥测 | **Go** | 无状态多副本、低延迟、gRPC 生态成熟、开发与测试快 |
| **L2 高交互欺骗层** | 协议仿真 + LLM 交互 | **Python**（LLM）+ **Go / Rust**（协议层） | AI 生态在 Python；性能瓶颈在协议层 |
| **L3 网络欺骗层** | 蜜网编排、微隔离、假拓扑 | **Go**（编排）+ **Rust / eBPF**（数据面） | 高并发与网络编程生态强；数据面需内核级 |
| **L4 分析决策层** | 意图识别、攻击链、策略生成 | **Python** | LLM / GNN / RL 生态全在 Python |
| **安全底座** | 沙箱、零信任、单向通道 | **Rust** | 内存安全、无 GC，适合安全关键组件 |
| **控制台** | 可视化、编排 | **TypeScript** | 前端生态成熟 |

| ID | 规则 | 验证方式 |
| --- | --- | --- |
| **TB-2** | 全部**离线** AI 能力（模型训练、内容生成、情报抽取、实验分析）**必须**用 Python 实现。 | 代码评审 + 仓库语言统计 |
| **TB-4** | 新增任何语言层或替换某层语言**必须先**有对应决策记录（ADR）并经用户确认。 | [`../background/decisions/`](../background/decisions/README.md) 检查 |
| **TB-20** | 各层**必须**按 §1 选型实现；**禁止**跨层混用语言（如用 Python 写 L1 适配器、用 Rust 写核心判定）。 | `make archcheck`（语言统计）+ 评审 |
| **TB-21** | 实现语言总数**必须** ≤ 5（Rust / Go / Python / TypeScript）；**禁止**引入第 6 种。 | `make archcheck`（语言统计） |
| **TB-22** | **判定与响应生成逻辑必须用 Go 写在核心内**，且**必须**只有一处实现。 | 代码评审 + [`architecture.md`](architecture.md) 的 AR-2 / AR-5 |

---

## 2. 逐层依据

### L0 接入层 —— 不自研

直接复用 Envoy / Nginx / HAProxy（工业级）或 eBPF 做内核态流量镜像。**这层是基础设施，自研纯属浪费。**

### L1 边缘欺骗层 —— 为什么是 Go

| 候选 | 结论 |
| --- | --- |
| **Go** ✅ **采纳** | 一个二进制覆盖③前置代理与④Pod 边车两种形态；与核心同语言、共享 proto 契约与 `TB-15` 门禁；易于容器化（静态单文件）。见 [ADR-0008](../background/decisions/0008-edge-language-go.md) |
| **Lua (OpenResty)** ❌ **已移除** | 能内嵌客户已有 nginx 是真实优势，但需 LuaJIT 与 C 模块、**Lua 侧无任何静态检查可接入 `TB-15` 门禁**、且调核心需引入非官方 gRPC 库。见 ADR-0008 |
| **Rust (Pingora)** ❌ | 性能上限最高，但 L1 的瓶颈在**跨进程往返**而非语言；引入第二套工具链与依赖审计 |
| **Python** ❌ | GIL 限制，高并发下性能塌方 |

### 核心 —— 为什么是 Go

| 维度 | 说明 |
| --- | --- |
| 开发与测试 | 增量编译 **1–5 s**；`go test -race` / `-fuzz` / `-bench` 内建；`net/http/pprof` 零配置剖析 |
| 接口实现 | 隐式接口使测试替身极轻；新增判定器改动面小 |
| 横向扩展 | 核心只做内存查表 + 外部存储访问，GC 压力小；真正的扩展能力来自**状态外置**（AR-9） |
| 生态 | gRPC / Protobuf 一等公民，与 `api/` 契约单一事实源契合 |

**代价（必须承认）**：`switch` 不强制穷尽 —— 漏改分支不报错。**对冲**：判定规则的枚举分支**必须**有穷尽性测试（`MD-*`）。

### L2 / L4 —— Python（AI 层无争议）

LLM 推理（vLLM / Transformers）、强化学习（PyTorch / Stable-Baselines3）、图分析（NetworkX / PyG）、
MITRE ATT&CK 映射库 —— 生态全在 Python。**性能不够时用 C++/CUDA 写算子由 Python 调用，但逻辑层坚决留 Python。**

### 安全底座 —— Rust 优先

内存安全、无 GC。**禁止**用 C 写（手动内存管理，漏洞风险高）。

---

## 3. 语言对比速查

| 语言 | 优势 | 劣势 | 承担的层 |
| --- | --- | --- | --- |
| **Go** | 并发强、生态好、开发快、工具链内建 | GC 抖动、`switch` 不强制穷尽 | **L1** / 核心 / L2 协议 / L3 编排 |
| **Rust** | 内存安全、无 GC、高性能 | 学习曲线陡、开发慢 | L0 / L1 / L3 数据面 / 底座 |
| **Python** | AI 生态无敌、开发快 | 性能差、GIL | L2 智能 / L4 决策 |
| ~~**Lua**~~ | 内嵌代理、热更新 | 生态小、不适合大逻辑；**无静态检查可接入门禁** | **不再使用**（[ADR-0008](../background/decisions/0008-edge-language-go.md) 移除） |
| **TypeScript** | 前端生态 | 不适合后端核心 | 控制台 |

---

## 4. 质量与安全纪律

> 原 `TB-13` / `TB-14`（禁止未处理的错误返回值） / `TB-15` / `TB-16` 随语言变更重写；原 `TB-17`（内循环用 `cargo check`）已废弃。

| ID | 规则 | 验证方式 |
| --- | --- | --- |
| **TB-14** | 全部源码**禁止**出现未处理的错误返回值；Go **必须**通过 `errcheck`（或等价检查），Python **禁止**裸 `except`（见 [`constraints.md`](constraints.md)）。 | `make errcheck` |
| **TB-15** | CI **必须**通过：格式化检查、静态检查（Go: `go vet` + `staticcheck`；Python: `ruff`；前端: ESLint）、全部单测、数据竞争检测（`go test -race`）。 | `make gate` |
| **TB-16** | 依赖**必须**经许可与漏洞审计；**禁止**引入 AGPL / SSPL / BSL 等传染性或限制性许可的依赖（**通过 HTTP 提供服务即触发 AGPL 披露义务**）。 | `make licensecheck` + 台账 [`../spec/dependencies.md`](../spec/dependencies.md) |
| **TB-23** | Rust 组件**必须**声明 `#![forbid(unsafe_code)]`，**唯一例外**是 eBPF / 内核态数据面 —— 例外处**必须**有安全注释。 | 编译期强制 + 代码评审 |

---

## 5. 边界

| ID | 规则 | 验证方式 |
| --- | --- | --- |
| **TB-24** | 跨语言**禁止**使用 FFI / CGO / 共享内存 / 嵌入式解释器；**必须**经 wire format（gRPC / Protobuf / JSON / WASM）通信。 | `make archcheck`（CGO 与本地库）+ 评审 |
| **TB-25** | **一个进程只允许一种语言**；**禁止**单进程内混跑两种运行时。 | 部署清单评审 |
| **TB-26** | 每个跨语言接缝**必须**带版本号，且**必须**能在不重启对端的情况下演进。 | 契约评审 |

> 接缝清单与契约定义见 [`structure.md`](structure.md) 与 [`modules.md`](modules.md)。
>
> ⭐ **门禁的落地方式（2026-09-17 定）**
>
> | 规则 | 命令 | 实际跑的检查 |
> | --- | --- | --- |
> | `TB-14` | `make errcheck` | `errcheck`（版本固定在 `Makefile` 里） |
> | `TB-15` | `make gate` | `check-fmt.sh` · `go vet` · `staticcheck` · `go test -race` |
> | `TB-16` | `make licensecheck` | `scripts/licensecheck/`；台账用 `make license-ledger` 重新生成 |
>
> `make gate` 是 CI 的唯一入口，按顺序跑完全部检查，任一失败即非零退出。
> 工具装在 `.bin/`（`make tools` 预装，版本固定以保证可复现）。
>
> **工具缺失时门禁失败，不跳过。** 静默跳过比没有门禁更糟 —— 它给了「检查过了」的错觉。

---

## 6. 被否决的候选（保留记录，避免重复讨论）

| 候选 | 内容 | 否决理由 |
| --- | --- | --- |
| **A. 在线全 Rust + 离线 Python**（原基线） | `shen-core` 纯逻辑 crate，同时编译为 ext_proc 服务与 Envoy WASM filter | 该方案的**核心收益**是「判别逻辑一处实现、两个编译目标共用」。但 R1 的收益在**分层选型下依然可达**（核心唯一 = AR-2/AR-5），而代价（异步 Rust 的迭代成本、AI 生成代码的编译—修复长循环）不再必要。详见 [ADR-0005](../background/decisions/0005-layered-languages.md) |
| **全 Python + 现成代理** | 全 Python 主体，最快出原型 | 性能受限；L1 边缘与核心热路径不可行 |
| **Go 主体 + Python AI** | 无分层，Go 打天下 | 边缘层毫秒级延迟与热更新能力不足；AI 层无法用 Go 替代 |
| **禁止引入第三种语言**（原 `TB-3`） | 最多两种语言 | 与分层架构不兼容：控制台需 TypeScript。改为**语言数上限**（TB-21） |
| **L1 用 Lua (OpenResty)** | 把 L1 处置逻辑内嵌进客户已有 nginx | 需 LuaJIT 与 C 模块，容器化成本高；**Lua 侧无静态检查可接入 `TB-15` 门禁**（门禁全建立在 Go 工具链上）；调核心需引入非官方 gRPC 库。改用 Go，见 [ADR-0008](../background/decisions/0008-edge-language-go.md) |

---

## 7. 未决项

| # | 未决 | 去向 |
| --- | --- | --- |
| 1 | ~~L1 边缘层用 **Lua (OpenResty)** 还是 **Rust (Pingora)**~~ | ✅ **已结案**：两者都不用，改用 **Go**，见 [ADR-0008](../background/decisions/0008-edge-language-go.md) |
| 2 | 核心是否需要语言级并发框架选型（如 gRPC 库） | 实现期决定 |
| 3 | L3 网络欺骗层是否进入 MVP | 见 [`../background/notes/implementation-discussion.md`](../background/notes/implementation-discussion.md) |
