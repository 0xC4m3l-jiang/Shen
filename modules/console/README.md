# `modules/console/` —— ③ 管控平台

**观测 + 登记台**：看欺骗层与蜜罐层的流量、判定与链路；登记被保护的反向链接 Web 服务并按服务观测。
**不参与请求级判定**（`AR-10`），**不下发策略、不做控制面** —— 登记表只是记录与观测口径，
生效路由仍由核心策略面下发。

| 子目录 | 是什么 |
| --- | --- |
| [`cmd/console/`](cmd/console/) | 进程入口（env → 核心 gRPC 读面 + `/api/v1` HTTP 面；`*_FILE` 密钥注入） |
| [`internal/api/`](internal/api/) | 版本化接口与中间件链：来源校验 → 认证 → 首次改密闸门 → RBAC → CSRF，**默认拒绝**；SSE 实时流（心跳 + `since` 续传） |
| [`internal/auth/`](internal/auth/) | 本地账号：argon2id 口令、内存会话（令牌 SHA-256 为键，从不落盘）、登录限流锁定、初始管理员引导 |
| [`internal/rbac/`](internal/rbac/) | 四角色静态矩阵（管理员 / 欺骗运维 / 蜜罐运维 / 只读），未登记权限直接 panic |
| [`internal/registry/`](internal/registry/) | 反向链接器登记表（服务名 / 上游 / 域名 / 负责人；乐观并发、原子落盘） |
| [`internal/geoip/`](internal/geoip/) | ip2region 离线归属地（数据内嵌 `data/`，内网 / 保留地址单独分类） |
| [`internal/audit/`](internal/audit/) | 操作审计：JSONL 追加 + 轮转 + 内存环形查询 |
| [`internal/topology/`](internal/topology/) | 内存计数 → 拓扑视图 |
| [`ui/`](ui/) | Vue 3 + Vite + TS 前端（独立工程；两套主题：蜃海（深色）/ 晨雾（亮色），点击才切换；动效恒开，仅尊重系统「减少动态效果」；`npm run build` 产物由 `deploy/docker/console-ui.Dockerfile` 打进 nginx） |

接口契约（**权威**）：[`console-api.md`](../../docs/spec/console-api.md)（分开发布）

## 怎么跑

```bash
go test ./modules/console/...

# 本地起后端 API（需核心在跑）
SHEN_CORE_ADDR=127.0.0.1:9443 SHEN_CONSOLE_LISTEN=127.0.0.1:9445 \
SHEN_CONSOLE_DATA_DIR=/tmp/shen-console go run ./modules/console/cmd/console

# 前端开发服务器（/api 代理到上面的 9445）
cd ui && npm ci && npm run dev

# 容器化一键起（推荐）：仓库根 cp .env.example .env && docker compose up -d --build
```

浏览器访问 `http://127.0.0.1:19444/`（容器）或 Vite 打印的地址（开发）。
默认账号/口令 **admin / admin**（验证缺省，首登强制改密）；`SHEN_CONSOLE_BOOTSTRAP_PASSWORD=random`
可改用随机初始口令（写入数据目录 `bootstrap-admin.txt`），生产必须显式设置或经 secrets 注入。

## 边界（务必遵守）

- 管控台不产生任何判定、不改任何策略；`registry` 的写操作只作用于登记表。
- 会话令牌 / CSRF 令牌从不写盘：API 进程重启后需重新登录（设计取舍，换取令牌不落盘）。
- 归属地数据内嵌 ip2region（Apache-2.0 / MIT 双许可，见 `internal/geoip/data/LICENSE.ip2region`），
  查询纯内存、无出站请求。
