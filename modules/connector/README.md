# `modules/connector/` —— ⑦ 反向隧道接入（连接器网关 + SDK）

**让真实 Web 业务零暴露地接入蜃楼的反向代理**：业务侧连接器向外拨号建立
TLS 长连接，网关校验控制台签发的 auth key，代理经隧道把请求送达业务——
业务不开任何入站端口（ngrok / Teleport 式反向隧道），接入即自动登记。

| 项 | 值 |
| --- | --- |
| 所属层 | L0 之外（接入通道，不在判定路径上） |
| 语言 | Go（[ADR-0008](../../../docs/background/decisions/0008-edge-language-go.md)） |
| 数据面 | **yamux over TLS 1.3**（`hashicorp/yamux`，MPL-2.0） |
| 控制面 | yamux 流 0 上的长度前缀 protobuf（[`common/api/connector/v1`](../../common/api/connector/v1/connector.proto)） |
| 模块清单 | 第 27 行（`modules.md` §1.1） |

## 它是什么

三个交付物，一个能力：

| 交付物 | 位置 | 给谁用 |
| --- | --- | --- |
| **网关** `gateway/` | 公网 TLS 监听 `:9446` | 平台侧：管连接器会话池、校验 key、给代理提供字节桥 |
| **Go SDK** `sdk/` | `connector.Connect(ctx, cfg)` | Go 业务：进程内三行代码接入 |
| **二进制** `cmd/shen-connector/` | 独立进程 | 非 Go 业务：一个进程接入 |

## 接入链路（数据怎么流）

```text
客户端/攻击者 ──▶ proxy :8080（判定 + 按 Host 路由）
                    │ route_origin 且 Host 命中隧道
                    ▼
              回环 TCP :9447 + 首行目标 Host        ← gateway 的字节桥
                    │ 映射到对应连接器会话的一条 yamux 流
                    ▼
              TLS :9446 ──▶ 连接器（业务侧拨入）
                    │ 流的另一端，纯 io.Copy
                    ▼
              真实业务 127.0.0.1:8080（零入站暴露）
```

控制面：连接器 TLS 拨入后，yamux 会话的**流 0** 跑一次握手
（`HandshakeRequest`：auth key + 服务元数据），校验通过即建立会话；
网关持集成令牌调控制台 `/api/v1/integration/*`（key 哈希表拉取、
自动注册上报、会话事件），**控制台自身不出站**。

## 为什么数据面是 yamux 而不是 gRPC（选型记录）

调研结论（2026-09-29，五选一）：

| 候选 | 判定 | 一句话 |
| --- | --- | --- |
| gRPC 双向流 | 弃 | 每消息叠加 protobuf + gRPC 帧 + 双层流控；反向隧道数据面无头部先例 |
| HTTP/2 多路复用 | 弃 | Go 标准库不暴露裸 h2 流，实现代价反常地高（ngrok 的路，但不是 Go 的路） |
| QUIC | 缓 | 丢包链路最优，但 quic-go 是大依赖；**演进方向**：只换 yamux 底层 |
| HTTP/1.1 反向连接池 | 弃 | 并发 = 连接数，socket 与 TLS 握手放大（ngrok v1 已淘汰的路） |
| **yamux over TLS** | **选定** | **流即 `net.Conn`：整条隧道两端都是 io.Copy 字节管道，零序列化** |

yamux 的三个白拿性质：**流即 `net.Conn`**（代理侧 stdlib
`http.Transport` 的连接池直接复用，keepalive 即流复用）；**内置
keepalive ping**（心跳不用自己造）；**大响应天然流式**（yamux 帧即
分块，无「整条响应物化为一条大消息」的内存峰值）。frp 的 TCP 面十年
生产验证同款（fatedier 维护的 fork），Teleport 反向隧道同路线。

许可：hashicorp/yamux 为 MPL-2.0（文件级弱传染，只影响其自身文件），
`scripts/licensecheck` 白名单 `ok` 档。

## auth key 模型（v1：key；v2：+mTLS）

- **形态**：`shc-` 前缀 + 32 字节随机 base62（刻意避开 secrets-check
  规则①的 `sk-` 形态，避免自家门禁误拦测试假值——真实密钥也永远不落库）。
- **落盘**：控制台只存 SHA-256 哈希（复用会话令牌的成熟模式）；
  明文只在**签发响应里出现一次**，日志与审计只记 hint（`shc-****末4位`）。
- **作用域**：key ⇄ 服务名 + hosts 白名单。连接器握手声明的 hosts
  必须是白名单子集，服务名不得改——一个凭证只能服务它声明的那个服务。
- **校验**：恒定时间比较（防时序侧信道）；网关定期从控制台拉取哈希表，
  吊销在拉取周期内生效（默认 30s）。
- **重置**：换 key 后旧 key 立即作废，存量连接在心跳周期（15s）内被拒。
- **v2 预留**：`HandshakeRequest.client_cert_pem`（字段 16）为 mTLS
  第二因子占位；升级时网关加 ClientCAs 校验，协议面零改动。

## 边界声明（对登记表旧边界的修订）

`modules/console/internal/registry` 原声明「**不主动连接上游**：管控台是
被动组件」。本模块保持该精神但修订口径：

- 控制台**仍然不出站**——出站的是网关（拉 key 表、上报注册与会话事件），
  控制台只被动接收；
- 登记表从「仅手动」扩展为「手动 + 连接器自动」共存：自动登记标注
  `source=connector`，与手动登记同表同观测，不互相覆盖；
- **不下发策略**的边界不变：隧道只影响「请求怎么到业务」，不触碰
  策略面 Pull/Ack 链路（改道后端、白名单仍由核心策略面决定）。

## 线协议（流 0 的分帧）

每条消息：`4 字节大端长度 + protobuf 字节`。握手 → 应答之后，流 0 上
可发 `SessionEvent`（v1 仅 `kind="closing"`：连接器计划内下线，审计
把「主动下线」与「意外掉线」分开记账）。数据流（≥1）无任何帧头：**连
接器 accept 到一条流，就把它与本地业务地址的一条 TCP 连接做双向
io.Copy**——隧道里跑的就是 HTTP 本身。

## 环境变量

网关（`cmd/gateway`）：

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `SHEN_GATEWAY_LISTEN` | `0.0.0.0:9446` | 连接器拨入的 TLS 监听 |
| `SHEN_GATEWAY_TLS_CERT` / `SHEN_GATEWAY_TLS_KEY` | 必填 | TLS 证书与私钥（`*_FILE` 形式支持 secrets 挂载） |
| `SHEN_GATEWAY_BRIDGE` | `127.0.0.1:9447` | 供 proxy 取流的回环字节桥监听（只绑回环） |
| `SHEN_GATEWAY_CONSOLE` | `http://127.0.0.1:9445` | 控制台地址（集成面） |
| `SHEN_GATEWAY_INTEGRATION_TOKEN` | 必填 | 控制台集成令牌（`*_FILE` 支持） |
| `SHEN_GATEWAY_KEY_REFRESH` | `30s` | key 哈希表拉取周期（吊销生效上限） |
| `SHEN_GATEWAY_SESSION_TTL` | `60s` | 心跳过期（yamux keepalive 15s） |

连接器（SDK Config 同名映射）：

| 变量 | 说明 |
| --- | --- |
| `SHEN_CONNECTOR_GATEWAY` | 网关地址（host:port） |
| `SHEN_CONNECTOR_KEY` | auth key（`shc-...`） |
| `SHEN_CONNECTOR_UPSTREAM` | 本地真实业务地址 |
| `SHEN_CONNECTOR_HOSTS` | 声明域名（逗号分隔，凭证白名单子集） |
| `SHEN_CONNECTOR_NAME` | 服务名（须与凭证一致） |

## 怎么自己验一遍

单测（不碰真网络）：

```sh
go test ./modules/connector/... -race
```

端到端（本地栈，含真 TLS 与真隧道流量）：

```sh
scripts/dev/local-stack.sh up        # 含 gateway + shen-connector
scripts/dev/local-stack.sh seed      # 造判定流量（其中带隧道路由的 Host）
```
