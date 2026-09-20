# 项目速览与能力点定位（新人从这里开始）

> **这是最快的一条路**：看完本文你应该能回答三件事 —— 项目是什么 · 能力点在哪 · 怎么跑起来和怎么定位问题。
> 规则细节去 [`../design/`](../design/README.md)；模块逐个细节去 [`../modules/_map.md`](../modules/_map.md)；运行排障去 [`../ops/runbook.md`](../ops/runbook.md)。

---

## 1. 这是什么（三句话）

1. **面向自主渗透 Agent 的欺骗引擎**：不拦可疑流量，而是把**已识别的自动化对手**透明地送进幻境后端，让它以为自己在打真站；
2. **形态是一个独立服务 + 接入物料**：不要求客户改业务代码（`INT-7`），四种接入形态（旁路镜像 / DNS 引流 / 反向代理前置 / Sidecar）；
3. **三条不可妥协**：① 不影响原始业务（`NI-1`，任何未识别状态**放行到真实业务**）；② 绝不实现控制面（`SB-1`/`SB-2`）；③ 判定与响应生成**只在核心实现一次**（`AR-2`/`AR-5`）。

现在跑到什么程度：MVP + 接管引流 + 处置内容框架已落地；L4 分析（Python）已接入近线 worker；
**蜜罐只做接入架构**（内容与协议栈待专项调研）；能力缺口清单见 [`../ops/functional-verification.md`](../ops/functional-verification.md) §2。

---

## 2. 五层 → 目录（一张图）

```text
接入层 L0   客户自己的 LB / nginx / Envoy（不自研，AR-3）
数据平面 L1 edge/       ① 旁路镜像 · ② DNS 引流 · ③ 反向代理前置 · ④ Sidecar
                       （判定逻辑禁止写在这里，AR-2 / AR-7）
核心         core/      判定 → 决策（三值）→ 会话 · 隔离 · 策略 · 遥测 · 存储 · 服务面 · 欺骗面
执行平面 L2 deception/ 蜜罐协议框架（内容待调研）
        L3 deception/  网络欺骗：微隔离 · 假拓扑 · 运行时检测（声明式，复用 Cilium/Tetragon）
分析平面 L4 analysis/  意图 · 攻击链 · 策略生成 · LLM 契约（Python）+ 近线 worker
控制平面     console/ 只读观测台（禁止参与请求级判定，AR-10）
```

目录用途全表（含语言与"是否源码"）见 [`../modules/_map.md`](../modules/_map.md) §1。

---

## 3. 能力点定位表（我要改/看某个能力 → 去哪）

| 我想… | 模块 | 代码 | 文档 |
| --- | --- | --- | --- |
| 改判定打分 / 规则求值 | `judge` | [`core/internal/judge/`](../../core/internal/judge) | [`../modules/judge.md`](../modules/judge.md) |
| 改三值决策 / 灰度 / 阈值 | `director` | [`core/internal/director/`](../../core/internal/director) | [`../modules/director.md`](../modules/director.md) |
| 改会话身份提取 | `session` | [`core/internal/session/`](../../core/internal/session) | [`../modules/session.md`](../modules/session.md) |
| 改隔离短路 | `isolation` | [`core/internal/isolation/`](../../core/internal/isolation) | [`../modules/isolation.md`](../modules/isolation.md) |
| 改策略装载 / 下发 / 回执 | `policy` | [`core/internal/policy/`](../../core/internal/policy) | [`../modules/policy.md`](../modules/policy.md) · [`../spec/policy-payload.md`](../spec/policy-payload.md) |
| 改事件上报 / 读侧 | `telemetry` | [`core/internal/telemetry/`](../../core/internal/telemetry) | [`../modules/telemetry.md`](../modules/telemetry.md) · [`../spec/events.md`](../spec/events.md) |
| 换存储驱动（Redis 等） | `store` | [`core/internal/store/`](../../core/internal/store) | [`../modules/store.md`](../modules/store.md) |
| 改 gRPC 服务面 / 禁止回显点 | `control` | [`core/internal/control/`](../../core/internal/control) | [`../modules/control.md`](../modules/control.md) |
| 改诱饵资产 | `decoy` | [`core/internal/decoy/`](../../core/internal/decoy) | [`../modules/decoy.md`](../modules/decoy.md) |
| 改幻境后端池 | `honeypot` | [`core/internal/honeypot/`](../../core/internal/honeypot) | [`../modules/honeypot.md`](../modules/honeypot.md) |
| 改欺骗响应 / 一致性 | `responder` | [`core/internal/responder/`](../../core/internal/responder) | [`../modules/responder.md`](../modules/responder.md) |
| 改边缘注入（改道侧改写） | `edge-injection` | [`edge/injection/`](../../edge/injection) | [`../modules/edge-injection.md`](../modules/edge-injection.md) |
| 改反向代理 / 边车 / TLS | `adapter-proxy` | [`edge/proxy/`](../../edge/proxy) | [`../modules/adapter-proxy.md`](../modules/adapter-proxy.md) · [`ADR-0017`](../background/decisions/0017-caddy-l1-base.md) |
| 接旁路镜像（形态①） | `adapter-mirror` | [`edge/mirror/`](../../edge/mirror) | [`../modules/adapter-mirror.md`](../modules/adapter-mirror.md) |
| 配 DNS 引流（形态②） | `adapter-dns` | [`edge/dns/`](../../edge/dns) | [`../modules/adapter-dns.md`](../modules/adapter-dns.md) |
| 改蜜罐协议框架 | `honeypot-protocol` | [`deception/honeypot/`](../../deception/honeypot) | [`../modules/honeypot-protocol.md`](../modules/honeypot-protocol.md) |
| 改 L3 网络欺骗产物 | `netpolicy` | [`deception/netpolicy/`](../../deception/netpolicy) | [`../modules/netpolicy.md`](../modules/netpolicy.md) |
| 意图识别 / 攻击链 / 策略生成 | `intent` · `chain` · `strategy` | [`analysis/intent/`](../../analysis/intent) 等 | [`../modules/intent.md`](../modules/intent.md) 等 |
| LLM 契约 / 超时 / 黑名单 | `llm-components` | [`analysis/llm/`](../../analysis/llm) | [`../modules/llm-components.md`](../modules/llm-components.md) |
| 让 L4 跑起来（近线） | worker | [`analysis/worker.py`](../../analysis/worker.py) | [`ADR-0022`](../background/decisions/0022-l4-near-line-worker.md) |
| 想确认**某能力到底实现了没、怎么验** | — | [`capabilities.md`](capabilities.md) | — |
| 看**流量调度图（意图 vs 实际落点）** | `console` | [`console/`](../../console) | [`../integrate/observability.md`](../integrate/observability.md) |
| 发伪造流量做验证 | — | [`scripts/traffic/`](../../scripts/traffic) | [`../../scripts/traffic/README.md`](../../scripts/traffic/README.md) |
| 看日志字段 / 定位一条请求 | — | 核心与适配器 | [`../spec/logs.md`](../spec/logs.md) |

**接缝（跨进程契约）** 只有这几个：`S1 判定`（edge→core）· 遥测上报 · 观测读取 · `S4 策略面`（core→edge）· 进程内注入。
详见 [`../modules/_map.md`](../modules/_map.md) §3。

---

## 4. 五分钟跑起来

```sh
scripts/shen.sh up          # 起全套（只需 Docker）：核心 + 代理 + 控制台 + L4 + 演示业务站
scripts/shen.sh traffic     # 发伪造流量并核对判定（33 场景；含缺口清单）
scripts/shen.sh verify      # 一键端到端：状态 + 全量流量 + L4 核对 + 报告 + 定位线索
scripts/shen.sh doctor      # 接入自检（INT-17 五项）：链路到底通不通、body 能不能读、会话粘不粘
```

| 地址 | 是什么 |
| --- | --- |
| http://127.0.0.1:19444/ | 观测控制台（概览 / 告警 / 流量访问与流动 / L4 分析结论） |
| http://127.0.0.1:18080/ | 业务入口（经引擎；默认影子模式只观测） |

仓库级验证（写完代码跑这个）：`scripts/shen.sh check`（= `make gate` + `make dev`）。

---

## 5. 开发与定位三招

1. **一条请求三处对齐**（按 `decision_id`）：核心逐判定日志 → 适配器逐请求日志 → 控制台 `/api/flow`。
   命令与字段见 [`../spec/logs.md`](../spec/logs.md) §7；
2. **看判定细节**：控制台 `/api/flow`（分值 / 命中信号 / 决策 / 后端）· `make replay`（离线规则回放）；
3. **看"引擎有没有影响业务"**：`scripts/shen.sh traffic`（所有场景断言状态码）· `make dev`（冒烟 + 回放）。

⚠️ 改了代码记得 `scripts/shen.sh restart`（**带 `--build`**）—— 只重建容器不重建镜像的话，新代码不会生效（见 [`known-issues.md`](known-issues.md) K-22）。

---

## 6. 能力缺口与已知限制（别把这些当 bug 重复发现）

| 去哪看 | 内容 |
| --- | --- |
| [`../ops/functional-verification.md`](../ops/functional-verification.md) §2 | **缺口清单**：查询串不参与判定 · 编码绕过 · 前缀误伤/绕过 · PUT/PATCH 未建模 · 白名单未消费 · `severity` 恒为 none |
| 同上 §3 | **当前环境验不了的能力**：改道 / 拦截 / 注入 / 诱饵 / 蜜罐 / 隔离 / ① ② ④ 形态 / L3 |
| [`known-issues.md`](known-issues.md) | 踩过的坑（工具类 · 运行时类 · 历史类） |
| [`../progress.md`](../progress.md) | 每个模块的完成度（唯一维护处） |

---

## 7. 文档地图（哪类东西去哪）

| 目录 | 是什么 | 约束力 |
| --- | --- | --- |
| [`../design/`](../design/README.md) | 已确认规则（架构 / 语言 / 模块清单 / 目录 / 硬约束 / 术语） | **必须遵守** |
| [`../modules/`](../modules/_map.md) | 模块信息：`_map.md` 目录能力总表 + 一模块一文档（九章） | 与 `design/` 一致时以 `design/` 为准 |
| [`../spec/`](../spec/) | 契约与字典：配置 · 事件 · 策略载荷 · 日志 | 契约以它为准 |
| [`../ops/`](../ops/runbook.md) | 运行与运维：启动/检查/修复/更新 · 功能验证与缺口 | 操作以它为准 |
| [`../integrate/`](../integrate/README.md) | 接入与使用：接入方 / 运维 / 告警 / 流量 / 人工测试 | 操作说明 |
| `../kb/`（本文所在） | 速览 · FAQ · 踩坑 | **不具约束力** |
| [`../background/`](../background/README.md) | ADR（决策与失效条件）· 讨论稿 · 调研材料 | 参考 |
| [`../plans/`](../plans/README.md) | 每轮变更包 | 历史留痕 |
