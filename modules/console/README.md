# `modules/console/` —— ③ 管控平台

**观测 + 登记 + 欺骗管控台**：看欺骗层与蜜罐层的流量、判定与链路；登记被保护的反向链接 Web 服务并按服务观测；
在界面上管理**欺骗管控数据集**（蜜罐池 / 诱饵资产 / 黑白名单 / 注入规则 / 服务级诱饵绑定）。
**不参与请求级判定**（`AR-10`），**不下发判定策略**（规则 / 阈值 / 灰度 / 影子永远来自核心部署配置）——
欺骗管控数据由核心**主动拉取**、终检后热替换，边缘仍只认核心策略面（Pull/Ack 不变）。

| 子目录 | 是什么 |
| --- | --- |
| [`cmd/console/`](cmd/console/) | 进程入口（env → 核心 gRPC 读面 + `/api/v1` HTTP 面；`*_FILE` 密钥注入） |
| [`internal/api/`](internal/api/) | 版本化接口与中间件链：来源校验 → 认证 → 首次改密闸门 → RBAC → CSRF，**默认拒绝**；SSE 实时流（心跳 + `since` 续传） |
| [`internal/auth/`](internal/auth/) | 本地账号：argon2id 口令、内存会话（令牌 SHA-256 为键，从不落盘）、登录限流锁定、初始管理员引导 |
| [`internal/rbac/`](internal/rbac/) | 四角色静态矩阵（管理员 / 欺骗运维 / 蜜罐运维 / 只读），未登记权限直接 panic |
| [`internal/registry/`](internal/registry/) | 反向链接器登记表（服务名 / 上游 / 域名 / 负责人；乐观并发、原子落盘） |
| [`internal/deception/`](internal/deception/) | **欺骗管控数据集**：单文件 + 整体版本号（乐观锁）+ 最近 50 个历史版本；启动时从部署配置自动初始化；与核心同口径的保存前校验（错误阻止保存 / 警告放行）；服务绑定**替换语义**的投影；内置模板（9 类蜜罐 · 9 条诱饵 · 4 类注入片段）；核心上报的同步状态与系统告警 |
| [`internal/geoip/`](internal/geoip/) | ip2region 离线归属地（数据内嵌 `data/`，内网 / 保留地址单独分类） |
| [`internal/audit/`](internal/audit/) | 操作审计：JSONL 追加 + 轮转 + 内存环形查询 |
| [`internal/topology/`](internal/topology/) | 内存计数 → 拓扑视图 |
| [`internal/connector/`](internal/connector/) | **反向隧道接入凭证**：签发（SHA-256 落盘、明文一次性显示）/ 吊销 / 重置、连接器会话状态（心跳 TTL）；`/api/v1/integration/*` 是网关集成面（集成令牌保护，非 RBAC 会话面）——key 表拉取、连接器自动登记、会话事件；管理面 `/api/v1/connectors`（registry 读写权限）。UI 入口「接入管理」页 |
| [`internal/llm/`](internal/llm/) | **大模型接入（基础能力）**：提供方登记（密钥 AES-256-GCM 落盘、只回脱敏提示）、连通测试、按已填信息探测可用模型（`/models`）、token 账本；出站调用经开源库 [go-openai](https://github.com/sashabaranov/go-openai)（Apache-2.0）——**不跟随重定向、错误回显 scrub、响应体积上限**等安全不变量在 `client.go` 里自持。供分析与后续 AI 模块统一调用（UI 入口在「系统 · 大模型接入」），不与任何消费方耦合 |
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

## 欺骗管控（蜜罐池 / 诱饵 / 黑白名单 / 注入 / 版本）

UI 入口「欺骗管控」页（5 个 Tab）；总览、告警、蜜罐层、系统「同步与健康」、服务详情页展示版本对账、蜜罐健康与系统告警。

**数据流**：界面保存 → 校验（与核心同口径）→ 数据集 v+1、投影修订号 r+1 → 核心每 15s 有条件拉取
（`GET /api/v1/integration/deception?since_rev=`，未变化 304）→ `Loader.WithOverlay` 终检 → 原子热替换 →
边缘下一次 `Pull` 拿到新版本并 `Ack` → 核心把「应用结论 / 边缘回执 / 蜜罐 TCP 探测」`POST` 回管控台。

**启动即可见**：`SHEN_CONSOLE_SEED_CONFIG` 指向与核心**同一份**部署配置（compose 已只读挂载）。数据集尚未接管时，
进程启动自动读出 honeypots / decoys / whitelist / blacklist / injects 五个域作为 v1，无需任何导入。
接管后部署配置里这五段再改**不会生效**（系统告警「基线漂移」提示）。找不到部署配置时显示「未初始化」，首次保存即接管。

**策略版本** = 部署配置 `policy.version` × 10⁶ + 投影修订号（重启不倒退，边缘对账无需特判）。之后若关闭同步，
须把 `policy.version` 调高到大于当前生效版本，否则边缘会拒收版本回退。

**失败语义**：管控台不可达 / 数据非法 → 核心保留 last-good 继续判定并上报 `applied=false`；核心重启时优先用本地缓存
（`SHEN_CORE_SYNC_CACHE`，带校验和，损坏即忽略）；数据集未初始化时集成端点返回 `not_initialized`，杜绝「空数据集清空诱饵」；
管控台数据集文件损坏时启动失败并点名 `history/` 目录。

| 接口 | 权限 | 说明 |
| --- | --- | --- |
| `GET /api/v1/config/dataset` | `config:read` | 数据集 + 校验报告 + 投影预览 + 同步状态 + 初始化结论 |
| `PUT /api/v1/config/<域>`（honeypots · decoys · whitelist · blacklist · injects · bindings） | 蜜罐池 `config:honeypot`，其余 `config:deception` | 分域保存，必带 `expected_version`（冲突 409；校验失败 400 并附 `report`） |
| `POST /api/v1/config/validate` | `config:read` | 只校验不保存（界面冲突预检，输入防抖 400ms） |
| `GET /api/v1/config/versions[/{n}]` | `config:read` | 版本时间线 / 某版本内容 |
| `POST /api/v1/config/rollback` | `config:admin` | 以旧内容发布**新版本**（版本号只增） |
| `GET /api/v1/config/templates` · `GET /api/v1/config/sync` | `config:read` | 内置模板与枚举 · 同步状态与系统告警 |
| `GET /api/v1/integration/deception` · `POST …/deception/report` | 核心同步令牌 | 核心专用（Bearer + 来源网段），与网关令牌分权 |

角色：管理员全部；欺骗运维改诱饵 / 名单 / 注入 / 绑定；蜜罐运维改蜜罐池；只读账号只看。审计动作（只记变化）：
`deception.dataset.{initialized,saved,rollback,validate_failed}`、`deception.sync.connected`、`deception.merge.{applied,rejected}`、`honeypot.health.changed`。

| 环境变量 | 进程 | 说明 |
| --- | --- | --- |
| `SHEN_CONSOLE_SEED_CONFIG` | console | 部署配置路径（只读），数据集为空时自动初始化 |
| `SHEN_CONSOLE_CORE_SYNC_TOKEN`（支持 `_FILE`） | console | 核心同步令牌；空 = 同步通道 503（界面可编辑但不下发） |
| `SHEN_CORE_CONSOLE_URL` / `SHEN_CORE_CONSOLE_TOKEN`（支持 `_FILE`） | core | 管控台地址与同一令牌；缺令牌时同步关闭并在日志点名（不阻断启动） |
| `SHEN_CORE_CONSOLE_SYNC_INTERVAL` | core | 拉取间隔（默认 15s，≥2s）；失败指数退避至 2min 并加抖动 |
| `SHEN_CORE_SYNC_CACHE` | core | last-good 缓存（compose：`core-state` 卷的 `/data/deception-sync.json`） |

compose 下两侧令牌取同一个 `SHEN_CORE_SYNC_TOKEN`（见 `.env.example`）。

**排障**：同步条「核心离线」→ 核心日志里的「管控台同步失败」与两侧令牌是否一致；「被拒绝」→ 系统「同步与健康」看原因，
通常是跨域数据失效（如被绑定的服务已删除），修正后自动恢复；边缘长期落后 → 适配器是否在跑、`SHEN_PROXY_POLICY_INTERVAL`。
本轮不含 LLM 生成任务队列（M4）：AI 配置仍由部署配置 `ai:` 段驱动，界面只读展示。

## 边界（务必遵守）

- 管控台不产生任何判定、不改判定策略；`registry` 的写操作只作用于登记表；欺骗管控数据只经核心拉取 + 终检后生效。
- 会话令牌 / CSRF 令牌从不写盘：API 进程重启后需重新登录（设计取舍，换取令牌不落盘）。
- 归属地数据内嵌 ip2region（Apache-2.0 / MIT 双许可，见 `internal/geoip/data/LICENSE.ip2region`），
  查询纯内存、无出站请求。
