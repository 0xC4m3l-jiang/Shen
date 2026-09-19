# 5 分钟上手：起环境 → 造流量 → 看观测台

## 方式一：一键 Docker（**推荐**；本机只需要 Docker）

```sh
make up
```

它会构建镜像并起五个容器：**核心** · **反向代理前置**（接入形态③）· **观测控制台** · **L4 近线分析** · **演示用假业务站**。

| 地址 | 是什么 |
| --- | --- |
| `http://127.0.0.1:19444/` | **观测控制台** —— 先看这个 |
| `http://127.0.0.1:18080/` | **业务侧入口**（经引擎）—— 访问它就会产生判定记录 |

另开一个终端造两类流量：

```sh
curl -s -A "HeadlessChrome/120" http://127.0.0.1:18080/.git/config   # 探针：应得高分
curl -s -A "Mozilla/5.0"        http://127.0.0.1:18080/               # 正常浏览器：应得低分
```

回到控制台刷新，你会看到（实测输出）：

```text
10:34:08 route_origin GET /.git/config  score=0.90 signals=[ua-headless, path-probe]
10:34:08 route_origin GET /etc/passwd   score=0.00 signals=[]
```

**看到 `route_origin` 是对的** —— 默认影子模式只观测不处置（`INT-11`）。
等 20 秒左右（L4 默认轮询间隔），「分析结论（L4）」块会出现意图与策略结论；想看全量：

```sh
curl -s http://127.0.0.1:19444/api/analysis | python3 -m json.tool
```

停掉：`make down`。日志：`make docker-log S=core`（尾部 50 行，**不跟随**）· `make docker-logs S=core`（跟随，Ctrl-C 退出）。
宿主端口被占用时：`SHEN_HTTP_PORT=8080 SHEN_CONSOLE_PORT=9444 make up`。

> 为什么除核心外都共享核心的网络命名空间、为什么端口只绑本机回环：见 [`../../deploy/docker/README.md`](../../deploy/docker/README.md)。

## 方式二：本地直接跑（开发用）

前置：**Go 1.26+**；要跑 L4 与门禁另需 **Python 3.13+**（环境建在 `analysis/.venv`）。

```sh
scripts/demo/run.sh     # 核心 + 假业务站 + 反向代理 + 控制台（都跑在本机进程里）
SHEN_CORE_ADDR=127.0.0.1:19460 make analysis   # 跑一轮 L4
```

> 分数与信号来自 `deploy/config/config.example.yaml` 里的两条示例规则。
> 若把它改回 `rules: []`，所有分数都会是 0 —— 那时控制台「什么都看不见」，属正常。
