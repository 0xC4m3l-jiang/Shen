# Shen —— 面向自主渗透 Agent 的欺骗引擎

> **项目名未定**（[ADR-0004](docs/background/decisions/0004-terminology.md)）：`Shen` 只是当前工作名。
> 它卡住建仓库、二进制名、服务名 —— 定名前不要对外发布。

**它做什么**：不拦截可疑流量，而是把**已识别的自动化对手透明地送进幻境后端**，让它以为自己在打真站。
真实用户与真实业务**完全不受影响** —— 这是最高优先级的硬约束（`NI-1`）。

**它是什么形态**：**一个独立服务 + 接入物料**。不是 SDK、不是业务中间件、不要求客户改一行业务代码（`INT-7` / `INT-9` / `INT-10`）。

---

## 1. 一分钟看懂

```text
                    ┌─────────────────────────────────────────┐
   AI Agent / 扫描器 │  L0 接入层   云 LB / Envoy / nginx       │  复用现成组件，不自研（AR-3）
   正常用户          │              TLS 终结 · 入口路由          │
        │            └────────────────┬────────────────────────┘
        ▼                             │
   HTTPS · 真实域名 · 真实证书          ▼
┌──────────────────────────────────────────────────────────────┐
│  L1 边缘欺骗层（Go，内嵌 Caddy 承担转发与 TLS）                  │
│  白名单 → 本地判定缓存 → 调核心判定 → 按三值路由 → 异步上报        │
│   · 放行 route_origin  → 真实业务（原样透传）                    │
│   · 改道 route_mirage  → 幻境后端（注入诱饵 / AI 欺骗内容，失败回落业务）        │
│   · 拦截 block         → 403（默认不产出）                       │
└───────────────┬──────────────────────────────────────────────┘
                │ gRPC · deadline ≤ 3ms（接缝 S1）
                ▼
┌──────────────────────────────────────────────────────────────┐
│  核心（Go，无状态多副本，可随时重启）                             │
│   判定 judge → 决策 director（三值 + severity 旁路字段）          │
│   会话 · 隔离 · 策略 · 遥测 · 存储 · 服务面                       │
│   欺骗面：诱饵 decoy · 幻境入口 honeypot · 响应生成 responder      │
└───────────────┬──────────────────────────────────────────────┘
                │
                ▼
   L2 蜜罐框架（接入架构）· L3 网络欺骗（声明式）· L4 意图 / 攻击链 / 策略（Python，已接近线 worker）
```

**三条不可妥协**（细节见 [`docs/design/`](docs/design/README.md)）：

| 约束 | 含义 |
| --- | --- |
| **不影响原始业务**（`NI-1`） | 引擎故障 / 超时 / 未识别状态**一律放行**到真实业务；引流后端挂了**回落**业务 |
| **决策只有三值**（`MD-12`） | `route_origin` / `route_mirage` / `block`，**禁止**第四值；加重走 `severity` 旁路字段，**禁止**弹挑战页 |
| **判定只实现一次**（`AR-2` / `AR-5`） | 判定与响应生成只在核心；适配器**禁止**实现任何判定逻辑 |

---

## 2. 现在能跑到什么程度

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| **1 · MVP（只观察）** | 判定 · 会话 · 遥测 · 存储 · 旁路镜像接收端 · 服务面 | ✅ 完成 |
| **2a · 接管与引流** | 策略装载 · 决策（阈值→三值 + 灰度）· 反向代理前置 + 边车（内嵌 Caddy）· DNS 引流模板 | ✅ 完成 |
| **2b · 处置内容** | 诱饵面 · 幻境后端池 · 响应生成 · 隔离 · 边缘注入 | 🟡 模块已实现并单测；**幻境后端池 · 白名单 · 响应改写规则 · AI 欺骗内容清单已能经策略面下发到边缘**（端到端实测：改道 + 注入，`make ai-check`）；诱饵资产到边缘的通路**未通** |
| **3 · 高交互与智能** | 协议仿真蜜罐 · 蜜网 · LLM 会话 · 意图与攻击链 | 🟡 **蜜罐只做接入架构**（用户裁定）：协议注册表 · 运行框架（并发上限/对称回收）· 会话录制契约已就绪；**蜜罐内容与协议栈待专项调研**；其余模块未开始 |

> ✅ **策略面（S4）已落地（2026-09-19）**：核心经 `api/policy/v1` 把**改道后端表 · 白名单 · 响应改写规则**下发到适配器，
> 带版本号、校验和与回执（`ST-8` / `AR-13`），实测「核心下发 → 适配器应用 → 真实改道 + 注入」全程跑通。
> ✅ **AI 欺骗内容注入已通（2026-09-20，阶段 A）**：`ai-capability` 离线生成（**强制过护栏**：结构/黑名单/长度/风格）
> → 清单 → 核心装载 → 策略面 `content_manifest` → 适配器按会话哈希确定性命中并注入改道侧；
> 三层开关默认全关（不打开时与以前**逐字节一致**），端到端验收 `make ai-check`（[ADR-0023](docs/background/decisions/0023-deception-content-injection.md)）。
> 仍未接通的是**诱饵资产**（`decoy`）：要先在核心侧定下它「谁产出、存在哪」。
> 详见 [ADR-0018](docs/background/decisions/0018-policy-plane-pull-model.md) 与 [`docs/spec/policy-payload.md`](docs/spec/policy-payload.md)。

规模：**24 个有效模块**（[`docs/design/modules.md`](docs/design/modules.md) §1.1）· 已实现 **18 个 Go 包 + 5 个 Python 模块** · 规则 **159 条**；
测试函数 **Go 271 + Python 71**（口径：`grep -rh '^func Test'` / `'^def test_'`）。

---

## 3. 快速上手

### 3.1 一键起全套（推荐 —— 本机只需要 Docker）

```sh
scripts/shen.sh up      # 等价于 make up；会自动等就绪并打印地址
# 或者： make start
```

> **完整的运行说明**（启动 / 检查 / 修复 / 更新 / 产物卫生）在 [`docs/ops/runbook.md`](docs/ops/runbook.md) —— 排障先看它。

启动后：

| 地址 | 是什么 |
| --- | --- |
| http://127.0.0.1:19444/ | **观测控制台** —— 概览 · 告警 · 流量访问与流动 · L4 分析结论 |
| http://127.0.0.1:18080/ | **业务入口**（经引擎；默认影子模式：只观测、不处置，`INT-11`） |

```sh
curl -s -A "HeadlessChrome/120" http://127.0.0.1:18080/.git/config   # 探针：控制台应显示高分与命中信号
curl -s -A "Mozilla/5.0"        http://127.0.0.1:18080/               # 正常浏览器：应为低分
```

其它命令：`make docker-ps`（状态）· `make docker-log S=core`（日志尾部 50 行，不跟随）· `make down`（停掉）。
宿主端口被占用时：`SHEN_HTTP_PORT=8080 SHEN_CONSOLE_PORT=9444 make up`。
细节与「为什么容器共享网络命名空间」见 [`deploy/docker/README.md`](deploy/docker/README.md)。

### 3.2 本地开发（Go / Python 直接跑）

前置：**Go 1.26+**；开发 L4 与跑门禁另需 **Python 3.13+**（环境建在 `analysis/.venv`，见下）。无外部服务依赖 —— 存储用内存实现。

```sh
# ① 快速内循环检查：构建 + 格式化 + vet + 架构 + 追溯 + 泄漏
make check

# ② 一轮的验收命令：上面的全部 + staticcheck/errcheck + Python 门禁 + 许可审计 + 全部单测（含 -race）
make tools      # 首次：把门禁工具装到 scripts/bin/（离线可重复）
make pyenv      # 首次：建 analysis/.venv 并装锁定依赖
make gate

# ③ 一键开发验证：配置干跑 → 起核心 → 在线冒烟 → 规则回放 → L4 近线分析
make dev
```

看全部命令：`make help`。依赖已 **vendor 入库**（`vendor/`），Go 侧构建与门禁**不需要网络**。

**跑起核心**（默认影子模式：只算判定、不处置，`INT-11` 要求首次上线必须如此）：

```sh
make run                                        # 监听 127.0.0.1:9443
make smoke                                      # 另开一个终端：对判定面发三个样本
SHEN_CORE_ADDR=127.0.0.1:9443 make analysis      # 跑一轮 L4（结论进控制台「分析结论」块）
SHEN_PROXY_UPSTREAM=http://127.0.0.1:9000 \
  go run ./edge/proxy/cmd/proxy                 # 起反向代理前置（③），业务地址指向真实服务
```

---

## 4. 效果长什么样

### 4.1 `make dev` —— 五个步骤全绿

```console
$ make dev
== 1/5 配置干跑：合法配置 ==
2026/09/19 12:56:11 策略已装载 policy_id=dev-smoke version=1 checksum=2cd6a7d1… 规则=2 条 灰度=0%
== 2/5 配置干跑：非法配置必须被拒 ==
core: 装载配置 bad-config.yaml 失败：policy: rules[1].weight=1.5 越界（要求 0 < weight ≤ 1）
== 3/5 起核心（影子模式）：127.0.0.1:19522 ==
核心已启动（影子模式（只算判定、不处置，INT-11））：127.0.0.1:19522
== 4/5 在线冒烟（判定面形状 + 幂等） ==
  [通过] 无头浏览器探源码           action=origin        severity=none  backend=""
  [通过] 正常浏览器              action=origin        severity=none  backend=""
  [通过] 会话 cookie 走会话身份    action=origin        severity=none  backend=""
== 5/5 规则回放（离线，打印命中明细） ==
  样本                     分数   规则数  命中信号
  无头浏览器探源码           0.9      2     ua-headless,path-git
  脚本批量扫路径             0.3      1     path-git
  正常浏览器访问首页          0        0     （无）
✅ 通过：配置校验 · 非法配置被拒 · 启动 · 判定面 · 冒烟 · 规则回放
```

要点：**判定响应里看不到分数与规则名**（`ST-7` 禁止回显）—— 「规则为什么命中」只能在 `make replay` 与启动日志里看。

### 4.2 引擎跑在请求路径上（真二进制 · 真 TLS）

```sh
# 起引擎（TLS 由引擎自己终结，INT-22）
SHEN_PROXY_TLS_MODE=manual \
SHEN_PROXY_CERT_FILE=/tmp/tls.crt SHEN_PROXY_KEY_FILE=/tmp/tls.key \
SHEN_PROXY_UPSTREAM=http://127.0.0.1:9000 \
SHEN_CORE_ADDR=127.0.0.1:9443 \
./proxy
```

```console
$ curl -sk -o /dev/null -w "code=%{http_code} http=%{http_version}\n" https://shop.example.com:18445/path
code=200 http=2
```

实测到的行为（每条都有测试或实跑证据）：

| 场景 | 表现 |
| --- | --- |
| 正常流量 | 原样透传到业务；`Host` 不变；补 `X-Forwarded-For` / `X-Forwarded-Proto`（业务侧仍能拿到真实来源 IP，`INT-23`） |
| 业务侧响应 | **一个字节都不改写**（`INT-8`），只透传 |
| 判定为改道 | 请求进幻境后端（后端表可来自**策略面下发**）；**改道侧** HTML 注入诱饵片段；后端不可达**立刻回落业务**（`NI-1`） |
| 策略面 | 适配器按间隔拉取策略（默认 60s），验 `sha256(payload) == checksum` 后整块应用；**远端覆盖本地、白名单取并集**；失败沿用当前策略并回执 `applied=false` |
| 核心挂了 / 超时 / 返回怪值 | **一律放行**到业务（`NI-3` / `NI-4` / `NI-5`，实跑验证过：断核心后仍 `code=200`） |
| 白名单命中 | 不调核心，直接透传（`INT-25`） |
| 影子模式 | 照算判定、照上报，但**永不改道、永不拦截**（`INT-11`） |
| 判定缓存 | 同 `decision_id` 只调一次核心；缓存有 TTL 与容量上限（`MD-10`），可丢失 |

---

## 5. 目录结构

> **每个目录做什么、每个模块在哪个目录、能力是什么、怎么接起来** —— 看 [`docs/modules/_map.md`](docs/modules/_map.md)（一张表回答全部）。

```text
api/          契约的唯一事实源（.proto；客户端必须生成，禁止手写）
vendor/       Go 依赖副本（入库 ⇒ 构建与门禁离线可用，不需要 Go 代理）
core/         核心：判定 · 决策 · 会话 · 隔离 · 策略 · 遥测 · 存储 · 服务面 · 欺骗面
               └ internal/<模块>/  一模块一目录（iface.go + 实现 + 单测）
               └ cmd/core/         核心进程入口
edge/         L1 边缘：proxy（③前置 + ④边车，内嵌 Caddy）· mirror（①旁路镜像）· dns（②纯配置）· injection（处置）
deception/    L2/L3：协议仿真蜜罐 · 蜜网（阶段 3）
analysis/     L4（Python）：意图 · 攻击链 · 策略生成 · LLM 契约纪律 · 近线 worker
               └ pyproject.toml / requirements*.txt / .venv   ← 本层的工具链与配置全部收在本目录
               └ proto/                由 make pygen 生成的 gRPC 桩
               └ tools/genproto.py     生成脚本（让生成物成为 analysis.proto.* 普通包）
console/      控制台：只读观测（Web UI + 4 个接口；语言偏离见 ADR-0020）
scripts/      门禁与运维工具：archcheck · tracecheck · check-leak · licensecheck · devcheck · demo/（本地演示环境）
               └ bin/                 门禁工具二进制缓存（不入库）
deploy/       部署物料
               └ docker/              Dockerfile × 3 · compose.yaml · README（一键起全套）
               └ config/              配置模板（config.example.yaml）
docs/         全部文档（入口见下）
```

## 6. 文档地图

**从 [`docs/README.md`](docs/README.md) 开始**（导航中枢）。最常去的几处：

| 我想… | 去哪 |
| --- | --- |
| **起一个能看的本地环境**（核心 + 假业务 + 代理 + 观测台） | `make demo`（或 `scripts/demo/run.sh`），然后开 http://127.0.0.1:19461/ |
| **看告警 / 流量 / 请求在引擎里怎么流动** | [`docs/integrate/observability.md`](docs/integrate/observability.md) |
| **把业务接进来**（选形态、改什么、怎么验） | [`docs/integrate/business-onboarding.md`](docs/integrate/business-onboarding.md) |
| **做人工测试**（一轮 15 分钟） | [`docs/ops/functional-verification.md`](docs/ops/functional-verification.md) |
| --- | --- |
| 读**技术基线**（已确认的 153 条规则，动手前必读） | [`docs/design/`](docs/design/README.md) 八份 |
| 看**某个模块**的设计（一模块一文件，固定九章） | [`docs/modules/`](docs/modules/README.md) |
| 看**进度 / 还差什么** | [`docs/progress.md`](docs/progress.md) |
| 看**为什么这么定**、什么条件下会变错 | [`docs/background/decisions/`](docs/background/decisions/README.md)（ADR，含失效条件） |
| 看**还没定什么** | [`docs/background/notes/implementation-discussion.md`](docs/background/notes/implementation-discussion.md) §6 |
| 看**每轮改了什么**（可复核） | [`docs/log.md`](docs/log.md) + [`docs/plans/`](docs/plans/README.md)（变更包，含追溯矩阵与证据） |
| 看**开发规范怎么跑**（门禁 · 追溯 · 一轮时序） | [`docs/kb/dev-workflow.md`](docs/kb/dev-workflow.md) |
| 遇到**没见过的问题** | [`docs/kb/known-issues.md`](docs/kb/known-issues.md) · [`docs/kb/faq.md`](docs/kb/faq.md) |

## 7. 门禁与追溯（这个仓库的纪律）

每次改动都要能**顺着一条线追回去**：需求/规则 → 文档 → 代码 → 测试 → 证据。`make gate` 就是这条线：

```text
make gate
├─ fmt-check · vet · staticcheck · errcheck      静态检查（TB-14 / TB-15）
├─ archcheck    架构与依赖方向、模块清单一致、store 唯一 I/O 出口、语言层数、CGO
├─ trace        模块文档↔代码↔单测 · 规则 ID 引用存在性 · 变更包与日志形状 · 悬空链接
├─ leakcheck    可观测面泄漏：字符串字面量 ↔ OH-1 禁用清单 · 决策与分数禁止回传响应头（OH-5）
├─ licensecheck 依赖许可审计（拦 AGPL / SSPL / BSL，TB-16）
└─ test -race   全部单测 + 数据竞争检测
```

**不绿不算完成**：门禁失败时不得声称完成，也不得注释或跳过检查（全局规范与 [`AGENTS.md`](AGENTS.md) §4）。

## 8. 使用边界（务必先读）

| 项 | 说明 |
| --- | --- |
| **仅限授权环境** | 本引擎**仅限**你拥有或已获书面授权的站点与环境使用（`SB-7`） |
| **不做控制面** | 项目**禁止**实现 C2、载荷投递、木马、向第三方 Agent 下发指令（`SB-1` / `SB-2`）；引擎是**被动组件**，不主动连接目标（`SB-6`） |
| **只用自己的素材** | 幻境内容**必须**基于自有素材或已获授权的站点；**禁止**伪造第三方品牌（`SB-3` / `SB-4`） |
| **对外可见面不得自曝** | 攻击者可见的任何位置**禁止**出现「蜜罐 / decoy / mirage / 决策分数」等字样（`OH-1`…`OH-5`，有自动检查兜底） |
| **许可** | 本项目自身尚未声明 LICENSE（属产品决策）；依赖许可台账见 [`docs/spec/dependencies.md`](docs/spec/dependencies.md) |

## 9. 已知限制与未完成

| # | 限制 | 影响 |
| --- | --- | --- |
| 1 | **诱饵资产**不经策略面下发 | 诱饵面的内容在请求路径上还看不到（改道后端表 · 白名单 · 响应改写规则 · **AI 欺骗内容**已能下发）；真实模型后端与风格画像属阶段 B（[ADR-0023](docs/background/decisions/0023-deception-content-injection.md)） |
| 2 | `director` 已能产出真实三值，但端到端「改道」尚未演练 | 影子模式之外的真实引流需要接一次演练 |
| 3 | **`AR-29`（业务路径 P99 额外延迟 ≤ 5 ms）换底座后未重测** | 未测前不得声称形态 ③④ 可上线（实验 `E3`） |
| 4 | `NI-12` 要求的 `V-1…V-5` 故障注入测试未纳入 CI；`AR-26` 的两条启动期断言未做 | 决策路径改动缺回归防线 |
| 5 | `NI-7`（cgroup 上限）· `ST-17`（探针）无部署物料；`scripts/doctor` · `scripts/sentinel` 仍是占位 | 交付形态与接入自检尚不完整 |
| 6 | 存储仍是内存实现 | 重启即丢；真实 Redis / ClickHouse / PostgreSQL 未接 |
| 7 | `tls_mode=acme` 只做了装配与校验 | 未在真实公网域名验证签发与续期 |

> 这些不是「以后再说」的客气话 —— 每一条都被登记到了具体位置：[`docs/progress.md`](docs/progress.md) · 各模块文档的「未决项」段 · [`docs/background/notes/pending-experiments.md`](docs/background/notes/pending-experiments.md)。

## 10. 参与开发

1. 读 [`AGENTS.md`](AGENTS.md)（项目强制规范，最优先）
2. 读 [`docs/design/`](docs/design/README.md) 八份（技术基线）
3. 确认待定项不阻塞你（项目名未定 + 各 ADR 的「未解决」段）
4. 按 [`docs/kb/dev-workflow.md`](docs/kb/dev-workflow.md) 的一轮时序走：访谈 → 定稿 → 开发 → 验证（`make gate` + `make dev`）→ 记录（变更包 + [`docs/log.md`](docs/log.md)）→ 审视
