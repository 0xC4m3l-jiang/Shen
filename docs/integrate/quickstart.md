# 5 分钟上手：起环境 → 造流量 → 看观测台

前置：**Go 1.26+**（`go version`）· **Python 3**（只用于本地假业务站）。

```sh
# 一条命令起全部：核心 + 假业务站 + 反向代理（形态③）+ 观测控制台
scripts/demo/run.sh
```

它打印四条地址，其中两条是要用的：

| 地址 | 是什么 |
| --- | --- |
| `http://127.0.0.1:19461/` | **观测控制台**（先看这个） |
| `http://127.0.0.1:18080/` | **业务侧入口**（经引擎）—— 访问它就会产生判定记录 |

另开一个终端造两类流量：

```sh
curl -s -A "HeadlessChrome/120" http://127.0.0.1:18080/.git/config   # 探针：应得高分
curl -s -A "Mozilla/5.0"        http://127.0.0.1:18080/             # 正常浏览器：应得低分
```

回到控制台刷新，你会看到（实测输出）：

```
16:55:01 route_origin GET /.git/config  score=0.90 signals=[ua-headless, path-git]
16:55:01 route_origin GET /x            score=0.60 signals=[ua-headless]
16:55:01 route_origin GET /y            score=0.00 signals=[]
```

**看到 `route_origin` 是对的** —— 默认影子模式只观测不处置（`INT-11`）。

> 分数与信号来自 `deploy/config/config.example.yaml` 里的两条示例规则。
> 若你把它改回 `rules: []`，所有分数都会是 0 —— 那时控制台「什么都看不见」，属正常。
