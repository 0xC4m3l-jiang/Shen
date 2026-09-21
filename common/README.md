# `common/` —— 公用代码（被 `modules/` 共用，不单独交付）

| 子目录 | 是什么 | 谁可以 import | 谁在用 |
| --- | --- | --- | --- |
| [`core/`](core/) | **共享内核**：判定 · 决策 · 会话 · 隔离 · 策略 · 遥测 · 存储 · 服务面 · 欺骗面 | 只有 `core/` 子树内（Go 的 `internal/` 规则，`ST-3`） | 三个模块都经 gRPC 调用它 |
| [`api/`](api/) | **跨进程契约**：`.proto` + 生成的 Go 桩（叶子，无业务逻辑） | 所有层都可以 | 三个模块 + `analysis/` |

## 为什么 `core/` 不拆进三个模块

它是**判定与响应生成的唯一实现**（`AR-2` / `AR-5`）—— 一旦按模块拆开，
「只实现一次」就失去了结构支撑。依据 [ADR-0028](../docs/background/decisions/0028-three-module-view.md) 决定 1
与 [ADR-0030](../docs/background/decisions/0030-two-level-layout.md) 决定 1。

## 怎么跑

```bash
go test ./common/core/...        # 核心单测（含 -race 由 make gate 跑）
go test ./common/api/...         # 契约包（生成物 + 夹具）

make generate                    # 由 common/api/ 的 .proto 重新生成 Go 桩（生成物入库，应零 diff）
```

## 契约的规矩

- **唯一事实源**是 `common/api/` 下的 `.proto`；**禁止**手写客户端（`ST-6`）；
- 改契约要同时改：`.proto` → 生成物 → 契约文档（[`docs/spec/`](../docs/spec/README.md)）→ 两侧的夹具与测试；
- `console-api.md` 里「核心 gRPC 读面」的字段准入规矩，见该文档 §1.2。
