# 规格与字典（`docs/spec/`）

> **契约与字典的唯一去处**：对外 / 跨模块的确定性约定。字段、载荷、开关的**权威**在这里；
> 规则（为什么这么定）在 [`../design/`](../design/README.md)，模块内部细节在 [`../modules/`](../modules/_map.md)。

| 文件 | 是什么 | 谁读它 |
| --- | --- | --- |
| [`config.md`](config.md) | 核心配置的字段全表 + 约束 + 规范性 JSON Schema（含 **规则匹配语义的实测行为**） | 运维改配置 · 开发加配置项 |
| [`events.md`](events.md) | 遥测事件载荷契约：事件信封 + `decision` / `analysis` 两种载荷 + 跨语言夹具 | 核心写 · 控制台与 L4 读 |
| [`policy-payload.md`](policy-payload.md) | 策略载荷：改道后端表 · 白名单 · 注入规则（缺省 vs 显式空的语义）· **AI 内容开关与内容清单** | `policy` 写 · 适配器读 |
| [`ai-contract.md`](ai-contract.md) | **AI 能力服务与欺骗内容**：`TaskSpec` / `Envelope` / 任务注册表 / 护栏档案 / 内容对象 / 清单文件 / 核心侧内容库的键 | 生成侧写 · 核心装载与投影 · 适配器消费 |
| [`logs.md`](logs.md) | **日志字典**：组件 → 日志点 · 逐判定字段表 · 开关 · 落在哪 · 与事件的区别 · 定位手法 | 排查问题的人 |
| [`metrics.md`](metrics.md) | **指标字典**：已采集 10 项（口径 / 采集点 / 阈值）· 未采集 6 项（各带关法） | 看板与告警的人 |
| [`console-api.md`](console-api.md) | **控制台读面**：核心 gRPC 的两个读方法（`ListEvents` / `GetCoreSnapshot`）+ 控制台 10 个 HTTP 只读接口 + **快照字段取舍的规矩** | 改控制台的人 · 要加读侧字段的人 |
| [`dependencies.md`](dependencies.md) | 依赖台账（生成物） | 许可审计 |

> ✅ **本目录当前无待建项**（`metrics.md` 已建于 2026-09-20）。

## 改这些文件时必须同步的地方

| 改了 | 还要改 |
| --- | --- |
| `config.md` 的字段 | [`../../deploy/config/config.example.yaml`](../../deploy/config/config.example.yaml)（有单测守着它必须能装载：`common/core/internal/policy` 的 `TestExampleConfigLoads`） |
| `events.md` 的键名 | 夹具 [`../../common/api/telemetry/v1/testdata/decision_event.json`](../../common/api/telemetry/v1/testdata/decision_event.json) 与 [`../../common/api/telemetry/v1/testdata/request_judged_event.json`](../../common/api/telemetry/v1/testdata/request_judged_event.json) + 两侧契约测试（Go 与 Python 读同一夹具） |
| `ai-contract.md` 的字段 | 生成侧（`analysis/aicap/`）· 核心侧（`common/core/internal/policy/`）· 适配器侧（`modules/deception/proxy/`）三处 + `policy-payload.md` 的投影 |
| `console-api.md` 的字段 | 提供方（`common/core/internal/control/telemetry.go` 的映射 · `common/core/internal/contract/snapshot.go`）· 消费方（`modules/console/cmd/console/main.go` 的 `*View` · `modules/console/web/index.html` 的取键）；**有键名单测钉住**：`go test ./modules/console/cmd/console/` |
| `metrics.md` 的指标 | 采集点那一段代码 + `spec/logs.md`（同一处日志常同时是采集点） |
| `logs.md` 的字段 | 打日志的那段代码（核心 `decisionRecorder` · 适配器 handler） |
