# `modules/` —— 产品功能模块（三个大模块）

> **开发从这里进去**：一个子目录 = 一个大模块。先看第一张表找到你要改的那件事在哪个子目录。

## 我要开发哪个模块？

| 我要做的事 | 进这个子目录 | 模块文档 | 跑它的测试 |
| --- | --- | --- | --- |
| ① **欺骗层** —— 反向代理转发与 TLS · 响应改写注入 · 假路径 / 蜜饵 · 旁路镜像 · DNS 引流 · 网络欺骗声明式产物 | [`deception/`](deception/README.md) | [`adapter-proxy.md`](../docs/modules/adapter-proxy.md) · [`edge-injection.md`](../docs/modules/edge-injection.md) · [`adapter-mirror.md`](../docs/modules/adapter-mirror.md) · [`adapter-dns.md`](../docs/modules/adapter-dns.md) · [`netpolicy.md`](../docs/modules/netpolicy.md) | `go test ./modules/deception/...` |
| ② **AI 蜜罐层** —— 蜜罐协议仿真入口 · 假 shell（未建） · 后端池对接 | [`honeypot/`](honeypot/README.md) | [`honeypot-protocol.md`](../docs/modules/honeypot-protocol.md) · [`honeypot-shell.md`](../docs/modules/honeypot-shell.md) | `go test ./modules/honeypot/...` |
| ③ **管控平台** —— 只读观测台 · 页面与 HTTP 接口 · 流量日志视图 | [`console/`](console/README.md) | [`console.md`](../docs/modules/console.md) · [`console-api.md`](../docs/spec/console-api.md) | `go test ./modules/console/...` |

## 这里**不**放什么（去隔壁）

| 你想改的东西 | 去哪 |
| --- | --- |
| 判定与响应生成（唯一实现）· 决策三值 · 会话 · 隔离 · 策略 · 遥测 · 存储 | [`../common/core/`](../common/core/) |
| 跨进程契约（`.proto` 与生成桩） | [`../common/api/`](../common/api/) |
| L4 分析（意图 / 攻击链 / 策略 · AI 能力服务） | [`../analysis/`](../analysis/) |
| 门禁与运维脚本 | [`../scripts/`](../scripts/) |
| 部署物料（Dockerfile · compose） | [`../deploy/`](../deploy/) |

## 两条本项目独有的规矩

- **判定与响应生成只在 `common/core/` 实现一次**（`AR-2` / `AR-5`）—— 适配器里**禁止**写判定逻辑；
- 适配器**禁止** import `common/core/internal/`（`ST-3`），只能经 `common/api/` 的生成桩调用 ——
  这条由 Go 的 `internal/` 规则在**编译期**强制，不靠自觉。

## 索引

- 分层 ↔ 目录 ↔ 用什么库 ↔ 怎么跑：[`_map.md`](../docs/modules/_map.md)
- 三个大模块 ↔ 各平面的对应关系：[ADR-0028](../docs/background/decisions/0028-three-module-view.md)
- 为什么是「两层容器 + 一个独立层」：[ADR-0030](../docs/background/decisions/0030-two-level-layout.md)
