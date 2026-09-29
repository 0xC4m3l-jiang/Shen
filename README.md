<div align="center">

<img src="assets/logo.svg" alt="蜃楼 Shen" width="110" height="110">

# 蜃楼 · Shen

**AI 欺骗引擎** —— 把已识别的自动化对手透明送进合成幻境；真实业务零改造、零影响。

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8.svg)](go.mod)
[![Python](https://img.shields.io/badge/Python-3.13%2B-3776AB.svg)](analysis/pyproject.toml)
[![Modules](https://img.shields.io/badge/modules-24%20(with%20evidence%20chain)-2FD4C6.svg)](#5-模块)
[![Gate](https://img.shields.io/badge/make%20gate-static%20%2B%20arch%20%2B%20race%20%2B%20pytest-success.svg)](#6-工程化)

</div>

**导览**：[1 它是什么](#1-它是什么) · [2 快速开始](#2-快速开始) · [3 如何使用](#3-如何使用) · [4 架构](#4-架构) · [5 模块](#5-模块) · [6 工程化](#6-工程化) · [7 成熟度](#7-成熟度) · [8 目录与许可](#8-目录与许可)

---

## 1. 它是什么

不拦截可疑流量，而是**把已识别的自动化对手（扫描器 / 爬虫 / AI Agent）透明送进幻境**：对手以为在打真站，工具、意图与攻击链暴露在受控的合成世界里；真实用户与真实业务**完全不受影响**。

| 常见做法 | 蜃楼的做法 |
| --- | --- |
| 拦截 / 封 IP | **不拦截**：送进幻境，让对手在假世界里暴露工具与攻击链 |
| 业务侧埋 SDK / 改代码 | **零改造、零入站暴露**：连接器从业务侧向外拨号建隧道，业务只听回环 |
| 只给告警 | **可观测**：判定流逐跳链路 · 告警 · 审计 —— 只观测、只登记，不下发策略 |

**三值决策**（判定只产出这三个值，禁第四值）：

| 决策 | 去向 | 对手看到的 | 业务影响 |
| --- | --- | --- | --- |
| `route_origin` 放行 | 真实业务（隧道 / 直连） | 正常业务页面 | 无 |
| `route_mirage` 改道 | 合成幻境 + AI 内容 | 以假乱真的站点，攻击链被完整记录 | 零（请求没到业务） |
| `block` 拦截 | 403 | 明确拒绝（只用于已知恶意） | 零 |

**三条底线**：① 引擎故障 / 超时 / 未识别 ⇒ **一律放行业务**；② 决策**只有三值**，禁止弹挑战页；③ **判定只在核心实现一次**，边缘只执行（`make archcheck` 强制）。

> **使用边界**：仅限你拥有或已获书面授权的环境；禁止实现 C2 / 载荷投递 / 向第三方下发指令；幻境内容必须用自有或已授权素材（禁止伪造第三方品牌）；对外可见面禁止出现「蜜罐 / decoy / mirage」等字样（`make leakcheck` 兜底）。

---

## 2. 快速开始

### 2.1 起栈（Docker）

```sh
make start          # 自动等就绪并打印地址
```

| 地址 | 是什么 |
| --- | --- |
| http://127.0.0.1:19444/ | **管控台**（默认 admin/admin，首登强制改密）|
| http://127.0.0.1:18080/ | **业务入口**（经引擎；默认影子模式：只观测、不处置）|

```sh
curl -s -A "HeadlessChrome/120" http://127.0.0.1:18080/.git/config   # 探针 → 控制台显示高分与命中信号
curl -s -A "Mozilla/5.0"        http://127.0.0.1:18080/               # 正常浏览器 → 低分、原样放行
```

### 2.2 起栈（无 Docker，全本机进程）

```sh
scripts/dev/local-stack.sh up       # 核心（影子）+ 反向代理 + 管控台 + Vite 前端
scripts/dev/local-stack.sh seed     # 造 3 条判定流量（判定流里有数据可点）
scripts/dev/local-stack.sh status   # 进程 / 端口 / 健康检查
```

| 地址 | 是什么 |
| --- | --- |
| http://127.0.0.1:5173/ | 管控台前端 |
| http://127.0.0.1:9445/healthz | 管控台 API |
| http://127.0.0.1:18080/ | 业务入口（经引擎）|

### 2.3 常用命令

| 命令 | 用途 |
| --- | --- |
| `make gate` | 一轮验收：格式 · vet · 架构 · 追溯 · 泄漏 · 许可 + `go test -race` + pytest |
| `make check` | 快速内循环：构建 + 格式 + vet + 架构 + 泄漏 |
| `make dev` | 开发验证：配置干跑 → 起核心 → 冒烟 → 规则回放 → L4 |
| `make traffic` | 伪造流量，从观测面核对判定 / 落点 / 注入 |
| `make verify` | 端到端核对（含 DAG 与 L4 结论） |
| `make doctor` · `make ai-check` | 接入自检 · AI 欺骗内容注入端到端 |
| `make down` · `make docker-ps` · `make docker-log S=core` | 停止 · 状态 · 日志 |

源码直跑（改代码时用，需 **Go 1.26+**；L4 与门禁另需 **Python 3.13+**）：
`make tools && make pyenv` → `make run` → `make smoke`。

---

## 3. 如何使用

### 3.1 接入一个真实业务（三步）

| 步 | 做什么 | 在哪 |
| --- | --- | --- |
| ① | 控制台「接入管理」**签发凭证**：绑定服务名 + 域名白名单；明文只显示一次，落库只存 SHA-256 | 管控台 |
| ② | 把凭证与接入代码交给业务侧，**跑连接器**（独立二进制 env，或进程内 SDK 三行） | 业务侧 |
| ③ | 连接器向外拨号完成 TLS 握手并**自动登记**；控制台看在线 / 心跳 / RTT，可吊销 / 重置 | 管控台 |

一键演示：`demo/up.sh`（蜃景商城经反向隧道接入：签发 → 拨入 → 自动登记 → 真流量验证），
步骤 / 配置 / 截图见 [`demo/README.md`](demo/README.md)；攻击模拟 `demo/attack.sh`（七类探针）。

**四种接入形态**：① 旁路镜像（唯一不在请求路径上，只观察）· ② DNS 引流（纯配置）· ③ 反向代理前置 · ④ Sidecar（③④ 是同一份实现的两个部署位置）。

### 3.2 界面速览（浅色主题「晨雾」）

| | |
| --- | --- |
| ![总览](demo/screenshots/ui-overview-light.png) | ![接入管理](demo/screenshots/ui-connectors-light.png) |
| *总览：新流量 · 来源归属地 · 流入蜃楼占比* | *接入管理：反向隧道凭证与实时会话（在线 / 心跳 / RTT），可吊销 / 重置* |
| ![反向链接器](demo/screenshots/ui-services-light.png) | ![欺骗层](demo/screenshots/ui-deception-light.png) |
| *反向链接器：按服务观测（demo 服务为连接器自动登记）* | *欺骗层：判定流逐行（决策 / 信号 / 风险分），点行看逐跳链路* |

<sub>界面默认深色主题「蜃海」，主题按账号保存在服务端，点击才切换。</sub>

---

## 4. 架构

### 4.1 模块全景

[![模块全景](assets/architecture/panorama.png)](assets/architecture/panorama.html)

<sub>实线 = 流量进入后的流转；虚线 = 建立接入的调用。分层带 = 数据面 / 决策与欺骗面 / 管控与分析面；客户网络里业务只听回环，唯一通路是连接器外拨的隧道（防火墙仅放行出站）。[矢量源图 ↗](assets/architecture/panorama.html)</sub>

### 4.2 两种视角

| 开发者视角（接入与运行） | 流量视角（检测与分流） |
| --- | --- |
| [![开发者视角](assets/architecture/developer-view.png)](assets/architecture/developer-view.html) | [![流量视角](assets/architecture/traffic-view.png)](assets/architecture/traffic-view.html) |
| ①→⑩ 接入链：签发 → 交付 → 部署 → 握手 → 自动登记 → 交引擎 → 回环转发 → 观测吊销 | 外部流量 → 反向代理处理链 → 三值分流：正常流量经隧道回源到后台服务，恶意流量被引进蜜罐 |

### 4.3 一次请求的路径

```text
客户端 → L0 接入（客户侧 LB / nginx，真实域名与证书）→ L1 适配器
  ① 会话身份 → ② 专属诱饵路由 → ③ 白名单 → ④ 本地判定缓存
  → ⑤ 调核心判定（gRPC，预算 ≤ 3ms；失败即放行）→ ⑥ 按三值执行 → ⑦ 异步遥测上报
核心：observer → judge（风险分）→ director（三值 + 灰度）→ 落点
L4（Python）：离线生成幻境内容（唯一护栏出口）· 近线分析（意图 / 攻击链 / 策略）
```

<sub>三张图的矢量源（`assets/architecture/*.html`）可点开交互；页内 `layoutReport()` 有排版自检（穿框 / 交叠 / 标签重叠 / 框压框 / 文字溢出 / 越区 —— 当前 **0 违规、0 交叉**）。</sub>

---

## 5. 模块

四个产品模块（`modules/`）+ 支撑层（`common/` · 分析 `analysis/`）。共 **24 个模块**，
`make verify-modules` 逐个跑测试并核对「文档 ↔ 规则 ↔ 测试 ↔ 功能场景」。

| 层 | 位置 | 职责 |
| --- | --- | --- |
| **L1 边缘** | `modules/deception/` | 反向代理前置 / Sidecar（内嵌 Caddy）：白名单 → 缓存 → 判定 → 三值执行 → 上报；旁路镜像；边缘注入 |
| **L2 幻境** | `modules/honeypot/` | 合成 Web 管理台场景包（登录 → 演示会话 → 总览/用户/配置/审计 + JSON API + 受限写）· 协议仿真框架 |
| **管控台** | `modules/console/` | Go API + Vue 3 UI：总览 · 欺骗层 · 蜜罐层 · 反向链接器 · **接入管理** · 分析 · 告警 · 审计（**只读观测 + 登记**） |
| **连接器** | `modules/connector/` | 反向隧道网关（凭证校验 · 会话池 · 字节桥）+ 业务侧 SDK / 二进制：业务零入站暴露 |
| **核心** | `common/core/` | 判定与响应生成的**唯一实现**：judge（风险分）→ director（三值 + 灰度）· 会话 · 隔离 · 策略面 · 遥测 · 存储 · 诱饵 |
| **契约** | `common/api/` | 跨进程 `.proto` 与生成桩（judge / policy / telemetry / connector） |
| **L4 智能** | `analysis/` | Python：AI 能力护栏出口 · 意图 / 攻击链 / 策略 · 近线 worker |

---

## 6. 工程化

`make gate` 一条命令把「能不能发」问清楚：

```text
gofmt · go vet · staticcheck · errcheck · archcheck · tracecheck · leakcheck · licensecheck
+ go test -race + pytest
```

| 检查 | 防的是什么 |
| --- | --- |
| `archcheck` | 分层与依赖方向 · **判定三值闭集** · 响应路径非确定性 · CGO 禁令 |
| `tracecheck` | 追溯链断裂：模块文档 ↔ 代码 ↔ 单测 · 悬空链接 · 过期状态标记 |
| `leakcheck` | 对外可见面的自曝字面量（豁免必须逐条登记并写理由） |
| `licensecheck` | 依赖许可台账（根 Apache-2.0，第三方各自约束） |
| `secrets-check` | 密钥泄漏：`sk-` 真值与密钥类变量的非占位赋值不得入库 |

**测试与覆盖**（本机实测）：Go **388** 个测试函数（20 个包，`-race` 全绿）· Python **143 passed** ·
`director` / `session` / `isolation` / `honeypot` / `injection` **100%** · `adapter-proxy` 90.1% · `judge` 87.0%。

> 覆盖率**不是**欺骗效果的证据 —— 它只说明代码被跑过；「效果」需要对照实验（见 §7）。

---

## 7. 成熟度

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| 1 · MVP（只观察） | 判定 · 会话 · 遥测 · 存储 · 旁路镜像 · 服务面 | ✅ |
| 2a · 接管与引流 | 策略下发 · 三值 + 灰度 · 反向代理前置 / Sidecar · DNS 模板 | ✅ |
| 2b · 处置内容 | 诱饵面 · 幻境后端池 · 边缘注入 · AI 内容 · 合成 Web 管理台 · 诱饵路由 | 🟡 已端到端实测 |
| 3 · 高交互与智能 | 协议仿真框架 · L4 意图 / 攻击链 / 策略 | 🟡 架构与 L4 已就绪，具体蜜罐接第三方 |
| 效果验证 | 自主进入率 / 有效交互率 / 误伤预算 | ⏸ 需对照实验 |

**明确还没做**（不写「已具备」）：业务侧线索投放 · 线索与场景的绑定契约 · 沙箱编排与就绪门禁 ·
合成交互事件回流核心 · 墓碑持久化 · 多租户 / 一进程多场景。
控制台鉴权已交付（本地账号 + argon2id + 四角色 RBAC + CSRF/Origin + 审计），剩余 OIDC/SSO 与写策略审批流。

---

## 8. 目录与许可

```text
modules/     deception(L1 边缘) · honeypot(L2 幻境) · console(管控台) · connector(网关 / SDK)
common/      core(判定与响应生成的唯一实现) · api(跨进程契约 .proto)
analysis/    L4（Python）：AI 能力护栏出口 · 意图/攻击链/策略 · 近线 worker
deploy/      Docker Compose 叠加档 · 示例配置 · 验证档
scripts/     门禁与工程工具（archcheck · tracecheck · leakcheck · verify · doctor · dev/local-stack.sh）
demo/        可跑通的最小样例：假业务 + 连接器 + 界面截图（入口 demo/README.md）
assets/      logo · 架构图（assets/architecture/：三张图的矢量源 HTML + 导出 PNG）
compose.yaml 部署入口（dev / prod / verify 叠加档在 deploy/docker/）
Makefile     统一入口：make gate · make dev · make traffic · make verify …
vendor/      第三方依赖副本（vendor 模式构建；许可证台账 make licensecheck）
```

| 我要做的事 | 去哪 |
| --- | --- |
| 跑起来 / 用起来 | §2 · §3 |
| 看架构与两张视角图 | §4（矢量源 `assets/architecture/*.html`） |
| 查模块职责与契约 | §5 · 契约在 `common/api/` |
| 复现一次验证 | §2 的 `make gate` / `make dev` / `make traffic` / `make verify` |
| 理解设计与约束 | 设计文档（与仓库**分开发布**）· 模块文档（一模块一文件） |

**许可**：Apache-2.0（见 [LICENSE](LICENSE)）。第三方依赖各受自身许可证约束，可审计（`make licensecheck`）。

<div align="center">
<sub>蜃楼 Shen · 让自动化对手把力气花在不存在的地方</sub>
</div>
