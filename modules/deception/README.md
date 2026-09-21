# `modules/deception/` —— ① 欺骗层

**对外欺骗的执行面**：改响应（L1 处置）与改网络视图（L3）。
判定与响应生成**不在这里**（`AR-2` / `AR-5`，唯一实现在 [`../../common/core/`](../../common/core/)）。

| 子目录 | 是什么 | 语言 | 模块文档 | 测试 |
| --- | --- | --- | --- | --- |
| [`proxy/`](proxy/) | ③ 反向代理前置 + ④ Pod 内边车（**同一份实现**，内嵌 Caddy）：判定胶水 + 按三值处置 | Go | [`adapter-proxy.md`](../../docs/modules/adapter-proxy.md) | `go test ./modules/deception/proxy/` |
| [`injection/`](injection/) | L1 处置逻辑：投毒改写 · 假路径 · 蜜饵注入（**被适配器引用，不独立部署**，`ST-5`） | Go | [`edge-injection.md`](../../docs/modules/edge-injection.md) | `go test ./modules/deception/injection/` |
| [`mirror/`](mirror/) | ① 旁路镜像接收端（四个接入形态里**唯一不在请求路径上**的） | Go | [`adapter-mirror.md`](../../docs/modules/adapter-mirror.md) | `go test ./modules/deception/mirror/` |
| [`dns/`](dns/) | ② DNS 引流（**纯配置**，无源码） | 声明式 | [`adapter-dns.md`](../../docs/modules/adapter-dns.md) | —— |
| [`netpolicy/`](netpolicy/) | L3 网络欺骗：微隔离 · 假拓扑 · 运行时检测（声明式产物，复用 Cilium / Tetragon，**无源码**） | 声明式 | [`netpolicy.md`](../../docs/modules/netpolicy.md) | —— |

## 怎么跑

```bash
go test ./modules/deception/...            # 单测（含 Caddy 兼容性测试）

# 起一个代理进程（本地；上游指向你的业务站）
SHEN_PROXY_UPSTREAM=http://127.0.0.1:9000 go run ./modules/deception/proxy/cmd/proxy

make ai-check                              # 端到端 17 项：改道 + AI 欺骗内容注入
```

## 三条硬约束

- **不写判定逻辑**（`AR-2`）；**只能改蜜罐侧的响应**，不改真实业务的（`INT-8`）；
- 对外可见面**禁止**出现 `honeypot` / `蜜罐` / `蜜饵` / `投毒` 等词（`OH-1`）——
  判据是「会不会出现在攻击者的屏幕上」，`make leakcheck` 守这条；
- 首次上线**必须影子模式**（只观测、不处置，`INT-11`）。

## 与核心的接缝

`S1 判定`（本层 → 核心，`common/api/judge/v1`）· `S4 策略面`（核心 → 本层，`common/api/policy/v1`，`Pull` + `Ack`）·
遥测上报（`common/api/telemetry/v1`）。**禁止** import `common/core/internal/`（`ST-3`）。
