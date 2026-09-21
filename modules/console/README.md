# `modules/console/` —— ③ 管控平台

**只读观测台**：看欺骗层与蜜罐层的流量日志、判定事件、流量调度图。
**不参与请求级判定**（`AR-10`），**不写库、不下发处置**。

| 子目录 | 是什么 |
| --- | --- |
| [`cmd/console/`](cmd/console/) | 进程入口（env → 核心 gRPC 读面 + HTTP 面 + 静态页） |
| [`internal/`](internal/) | 目前只有 [`internal/topology/`](internal/topology/)（把内存计数聚成拓扑视图）；**核心读面客户端、HTTP 面与 SSE 流都在** [`cmd/console/main.go`](cmd/console/main.go) |
| [`web/`](web/) | 静态页（**无构建步骤**：`assets.go` 内嵌，[ADR-0020](../../docs/background/decisions/0020-console-minimal-static-ui.md)） |

模块文档：[`console.md`](../../docs/modules/console.md) ·
接口契约（**权威**）：[`console-api.md`](../../docs/spec/console-api.md)

## 怎么跑

```bash
go test ./modules/console/...

go run ./modules/console/cmd/console     # 本地起控制台（需核心在跑）
make up                                  # 或一键起全套（核心 + 代理 + 控制台 + L4 + 演示业务站）
```

## 三条边界

- **只读**（[ADR-0020](../../docs/background/decisions/0020-console-minimal-static-ui.md) 决定 2）；
- 观测面是**推送**而不是轮询（核心 `WatchEvents` 服务端流 → 控制台 SSE，[ADR-0027](../../docs/background/decisions/0027-observability-push.md)）；
- 快照**刻意不含**阈值 / 灰度 / 影子模式等核心运行参数，也**不含** `started_at` ——
  准入规矩见 [`console-api.md`](../../docs/spec/console-api.md) §1.2。

> ⚠️ **未解决**：控制台目前**无鉴权、单实例**（[ADR-0020](../../docs/background/decisions/0020-console-minimal-static-ui.md) 未解决）。
> 绑定非回环地址之前必须先解决，否则实时流会把全部事件推给任何连上它的人。
