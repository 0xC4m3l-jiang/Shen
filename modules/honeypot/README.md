# `modules/honeypot/` —— ② AI 蜜罐层

**蜜罐侧**：把攻击者引到幻境入口，并（将来）在里面给他一个可交互的假环境。

| 子目录 | 是什么 | 状态 |
| --- | --- | --- |
| [`protocol/`](protocol/) | 蜜罐协议仿真入口：协议注册表 · 运行框架 · 最小适配器 | ✅ **框架已建**（阶段 3）；真实协议栈**待专项调研** |
| `shell/`（未建） | 假 shell：命令表分发 · 内存文件系统 · 文件投递 | ⏸ **推迟**（用户裁定：蜜罐只做接入架构）；阶段未到，目录**不预建** —— [ADR-0007](../../docs/background/decisions/0007-repo-layout.md) 的阶段划分 |

模块文档：[`honeypot-protocol.md`](../../docs/modules/honeypot-protocol.md) ·
[`honeypot-shell.md`](../../docs/modules/honeypot-shell.md)

## 怎么跑

```bash
go test ./modules/honeypot/...        # 单测
```

## 边界（现在就能定下来的）

- **本层不实现具体蜜罐**：只做**接入架构**与**后端池**
  （[ADR-0011](../../docs/background/decisions/0011-honeypot-entry-external-backends.md)）——
  真正跑容器 / 进程的**编排**归属在 [`../../common/core/`](../../common/core/) 的 `honeypot` 模块，实现面另开 ADR；
- 蜜罐的「启动」当前按**路由**落地：判定为 `route_mirage` 时，核心把该会话引到已登记的幻境后端；
- 本层与核心之间只有 **gRPC 契约**（[`../../common/api/`](../../common/api/)），**禁止** import `common/core/internal/`（`ST-3`）。
