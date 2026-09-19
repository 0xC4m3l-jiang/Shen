# 在 Docker 里跑起整套（推荐入口）

前置：**只需要 Docker**（Compose 随 Docker Desktop / Docker Engine 一起装）。本机不需要 Go、Node、Python。

```sh
make up          # 起全套（首跑会构建镜像）
make docker-ps   # 看状态
make docker-logs # 看日志（make docker-logs S=core 只看核心）
make down        # 停掉
```

启动后：

| 地址 | 是什么 |
| --- | --- |
| http://127.0.0.1:19444/ | **观测控制台** —— 概览 · 告警 · 流量访问与流动 · L4 分析结论 |
| http://127.0.0.1:18080/ | **业务入口**（经引擎；影子模式：只观测、不处置，`INT-11`） |

造点流量再看控制台：

```sh
curl -s -A "HeadlessChrome/120" http://127.0.0.1:18080/.git/config   # 探针：应得高分
curl -s -A "Mozilla/5.0"        http://127.0.0.1:18080/               # 正常浏览器：应得低分
```

## 起了哪些容器

| 服务 | 是什么 | 端口 |
| --- | --- | --- |
| `core` | 核心：判定与响应生成的**唯一**实现（`AR-2`）；**拥有本栈的网络命名空间** | 9443（只回环，不发布） |
| `proxy` | L1 适配器：反向代理前置（接入形态③） | 容器内 8080 → 宿主 18080（只绑回环；`SHEN_HTTP_PORT` 可改） |
| `console` | 控制台：只读观测（不参与请求级判定，`AR-10`） | 容器内 9444 → 宿主 19444（只绑回环；`SHEN_CONSOLE_PORT` 可改） |
| `analysis` | L4 近线分析 worker | 无 |
| `business` | 演示用假业务站（**不是**产品的一部分） | 19080（只回环） |

## 为什么除 core 外都 `network_mode: service:core`

核心的判定面是**明文 gRPC**，而设计规则只允许它监听**回环地址**
（`assertPlaintextListenIsLocal` 兜底；跨节点部署必须换 mTLS，见 [`../../docs/design/structure.md`](../../docs/design/structure.md) §4）。

让其它容器**加入 core 的网络命名空间**后，它们与核心共享同一个回环 —— 「同机部署」这个前提
在容器里依然成立，规则不必放宽。端口发布写在 `core` 上，发布的就是这个共享命名空间里的监听。

> 想让引擎直接对外服务（而不是只绑本机）？把 `compose.yaml` 里的 `127.0.0.1:${SHEN_HTTP_PORT:-18080}:8080` 改成 `8080:8080`，
> 并自行在其前面加一层客户侧 L0（TLS 终结默认交给 L0，见 [`ADR-0019`](../../docs/background/decisions/0019-tls-termination-belongs-to-l0.md)）。

## 换成你自己的业务

1. `compose.yaml` 里把 `proxy` 的 `SHEN_PROXY_UPSTREAM` 指向你的服务地址；
2. 删掉 `business` 服务（它只是演示用的假站点）；
3. 把 `core` 挂载的配置换成你的：`SHEN_CONFIG` 指向的 YAML（模板见 [`../config/config.example.yaml`](../config/config.example.yaml)）。

排查顺序：`make docker-ps` → `make docker-logs S=core` → `make docker-logs S=proxy` → 控制台页面顶部的错误提示。
