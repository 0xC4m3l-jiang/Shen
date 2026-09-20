# 变更包：五维能力审计（反代 / 转发 / 监控 / 配置 / AI 注入）+ KB 能力实况

## 0. 头部

| 项 | 值 |
| --- | --- |
| 主题 | 逐项核验用户点名的五个维度是否已实现（取代码 / 测试 / 活体三类证据），并把**能力实况 · 怎么验 · 缺口**写进知识库 |
| 日期 | 2026-09-20 |
| 状态 | 已验证（五维逐项核验完成；`make gate` 通过） |
| 改动分级 | **S**（新增 1 份 KB 文档 + 2 处索引；不改代码） |
| 涉及范围 | [`../kb/capabilities.md`](../kb/capabilities.md)（新增）· [`../kb/README.md`](../kb/README.md) · [`../kb/quick-tour.md`](../kb/quick-tour.md) |
| 关联 | INT-8 · INT-25 · NI-3/NI-4/NI-5 · ST-10 · AR-29 · AR-15/AR-30/AR-31/AR-32 · ADR-0017/0018 · 缺口汇总见 [`../ops/functional-verification.md`](../ops/functional-verification.md) §2 |

---

## 1. 审计方法与结论

**方法**：每个维度取三类证据 —— 代码（文件与职责）· 测试（测试函数与边界用例）· 活体（可复跑命令或接口）。

| 维度 | 结论 | 关键证据 |
| --- | --- | --- |
| 反向代理 | ✅ 已实现 | `edge/proxy/` 14 文件 · **58 个测试函数** · Caddy 模块 `http.handlers.shen_proxy` · 边界（WebSocket 101 / SSE 不缓冲 / 2 MiB 响应不注入 / 8 MiB 上传不丢字节） |
| 流量转发 | ✅ 已实现 | 放行原样透传（`INT-8`）· 改道转发（注入只在改道侧）· 回落业务（`NI-5`）· 拦截 403（默认关，需 `SHEN_BLOCK_ENABLED`） |
| 流量监控 | ✅ 已实现 | 控制台 **8 个只读接口** · 逐判定日志（`msg=decision`）· **逐请求 DAG（每步三段：请求/响应/为什么）** · 两段式告警 · 指标规格（已采集 10 / 未采集 6）· 接入自检五项 |
| 配置 | ✅ 已实现 | 示例配置 153 行 + 配置规格 429 行 + **漂移守卫单测** · 策略载荷规格 · 适配器/控制台/L4 环境变量表 · compose 与两份验证配方 |
| **注入 AI 欺骗信息** | ⚠️ **部分实现** | 注入机制（`edge/injection` + 策略面 `inject_rules`）✅ · 诱饵资产定义与多态 ✅ · 响应一致性（`AR-30`）✅ · LLM 契约层 ✅；**但无真实模型后端**（`UnconfiguredClient` 显式失败）且 **L4 结论未接到 `policy`/注入** ⇒ 当前注入的是**静态片段**，不是 AI 现场生成的内容 |

**"AI 注入"的关闭路径（两步）**：① 部署侧注入真实 `AnalysisClient`（模型后端，`AR-15` 禁止模板冒充）；
② 把 L4 结论经 `policy` 接成 `inject_rules`（保留黑名单与契约校验；`AR-32` 要求结论**经策略面间接生效**）。

---

## 2. 产物

| 文件 | 动作 | 为什么 |
| --- | --- | --- |
| [`../kb/capabilities.md`](../kb/capabilities.md) | **新增** | 五维实况（状态 · 代码 · 技术点 · 怎么验）+ 其余能力速查 9 项 + **已知缺口 8 条**（含 AI 注入的两步关闭路径） |
| [`../kb/README.md`](../kb/README.md) · [`../kb/quick-tour.md`](../kb/quick-tour.md) | 改 | 索引与能力定位表加入该文档（并修掉两处列数不匹配的表格行） |

---

## 3. 验证证据

```console
$ 审计（三类证据）
  edge/proxy 测试函数 58 · Caddy 模块注册 ✓
  console 只读接口 8 个（summary · flow · events · analysis · graphs · trace · topology · healthz）
  analysis/llm/client.py → UnconfiguredClient（无真实模型后端）
  表格列数复核：capabilities.md · README.md · quick-tour.md 全部一致
$ make gate → 门禁通过。
```

---

## 4. 审视记录

| # | 发现 | 类型 | 证据 | 动作 |
| --- | --- | --- | --- | --- |
| 1 | "注入 AI 欺骗信息"极易被读成"已实现" | 认知风险（会误导排期） | 机制在、内容不在（无模型后端） | 在 capabilities.md §1.5 明确标"部分实现"并给出两步关闭路径 |
| 2 | 缺口散落在 ops / spec / kb 多处 | 查找成本 | 四处引用 | §3 汇总 8 条并指回权威处，避免重复维护 |
| 3 | 我插入的两行表格列数不匹配（kb/README、quick-tour） | 格式缺陷 | 列数检查 3 ≠ 4 / 3 ≠ 5 | 删除残留行 / 补齐列 |
| 4 | 上一条命令把 Python 直接交给 bash（缺 `python3 -` 包装） | 自伤（记录没落盘） | bash 语法错误 | 重写记录，改为单条 python 调用 |
