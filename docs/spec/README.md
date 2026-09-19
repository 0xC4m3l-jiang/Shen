# 规格与字典（`docs/spec/`）

> **契约与字典的唯一去处**：对外 / 跨模块的确定性约定。字段、载荷、开关的**权威**在这里；
> 规则（为什么这么定）在 [`../design/`](../design/README.md)，模块内部细节在 [`../modules/`](../modules/_map.md)。

| 文件 | 是什么 | 谁读它 |
| --- | --- | --- |
| [`config.md`](config.md) | 核心配置的字段全表 + 约束 + 规范性 JSON Schema（含 **规则匹配语义的实测行为**） | 运维改配置 · 开发加配置项 |
| [`events.md`](events.md) | 遥测事件载荷契约：事件信封 + `decision` / `analysis` 两种载荷 + 跨语言夹具 | 核心写 · 控制台与 L4 读 |
| [`policy-payload.md`](policy-payload.md) | 策略载荷：改道后端表 · 白名单 · 注入规则（缺省 vs 显式空的语义） | `policy` 写 · 适配器读 |
| [`logs.md`](logs.md) | **日志字典**：组件 → 日志点 · 逐判定字段表 · 开关 · 落在哪 · 与事件的区别 · 定位手法 | 排查问题的人 |
| [`dependencies.md`](dependencies.md) | 依赖台账（生成物） | 许可审计 |

**待建**：`metrics.md`（每指标口径含分母 / 单位 / 采集点 / 正常范围 / 越界动作 —— `AR-28`）。

## 改这些文件时必须同步的地方

| 改了 | 还要改 |
| --- | --- |
| `config.md` 的字段 | [`../../deploy/config/config.example.yaml`](../../deploy/config/config.example.yaml)（有单测守着它必须能装载：`core/internal/policy` 的 `TestExampleConfigLoads`） |
| `events.md` 的键名 | 夹具 [`../../api/telemetry/v1/testdata/decision_event.json`](../../api/telemetry/v1/testdata/decision_event.json) + 两侧契约测试（Go 与 Python 读同一夹具） |
| `logs.md` 的字段 | 打日志的那段代码（核心 `decisionRecorder` · 适配器 handler） |
