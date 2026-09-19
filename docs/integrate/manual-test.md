# 人工测试步骤（一轮 15 分钟）

目标：**看到判定是否按设计工作**，以及**引擎不工作/故障时业务是否照常**。

## 1. 起环境

```sh
scripts/demo/run.sh          # 核心 + 假业务站 + 反向代理 + 控制台
```

## 2. 造三类流量

```sh
BASE=http://127.0.0.1:18080
curl -s -o /dev/null -w "%{http_code} " -A "HeadlessChrome/120" $BASE/.git/config   # 探针 → 高分
curl -s -o /dev/null -w "%{http_code} " -A "Mozilla/5.0"        $BASE/             # 正常 → 低分
curl -s -o /dev/null -w "%{http_code} " -A "HeadlessChrome/120" "$BASE/next?p=2"    # 同 IP 同会话
```

## 3. 核对（控制台 / `api`）

| 期望 | 怎么看 |
| --- | --- |
| 探针得分明显高于正常浏览器 | 控制台「流量访问与流动」的**分值**列 |
| 命中信号能解释分数 | 同一行的**命中信号**列（如 `ua-headless` / `path-git`） |
| 默认不改道、不拦截 | **决策**列全是 `放行`（影子模式 `INT-11`） |
| 同 `(来源, 会话, 路径, 时间窗)` 复用判定 | 核心日志里判定只发生一次；控制台只有一条记录（`ST-10` 缓存） |

```sh
curl -s http://127.0.0.1:19461/api/flow?limit=5 | python3 -m json.tool
```

## 4. 故障注入（`NI-1` 的现场验证）

```sh
# ① 把核心杀掉（控制台会报「读取核心失败」——这是对的）
pkill -f shen-demo-core
curl -s -o /dev/null -w "业务仍应 200：%{http_code}\n" -A "HeadlessChrome/120" http://127.0.0.1:18080/
```

期望：**业务照常 200**（`NI-3` 失败放行）；控制台的告警/流动**停止增长**（核心没了，没有人记）。

## 5. 边界情形的快速检查

| 项 | 命令 | 期望 |
| --- | --- | --- |
| 探针端点未实现 | `curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:18080/__shen/healthz` | 走业务（未实现探针，见 `adapter-proxy.md` §8） |
| 大响应不被注入 | 用假业务站改成 2 MiB 页面（或直接看自动化测试） | 原样透传（`INT-8`） |
| 大上传不丢 body | `curl --data-binary @big -X POST $BASE/upload` | 上游收到完整字节数 |

> 以上自动化版本在 `make gate` 里：`V-1…V-4` 故障注入、转发边界、可见面卫生 —— 人工测试是**补场景**，不是替代它们。
