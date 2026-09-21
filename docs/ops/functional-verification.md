# 整体功能验证（怎么跑 · 验到了什么 · 还缺什么）

> 命令：`scripts/shen.sh traffic`（= `make traffic`）· 场景定义：[`../../scripts/traffic/scenarios.json`](../../scripts/traffic/scenarios.json)
> 场景怎么写、字段什么意思：[`../../scripts/traffic/README.md`](../../scripts/traffic/README.md)
> 本文回答三件事：**验到了什么**（带证据）· **还缺什么**（缺口清单 + 怎么关）· **哪些在当前环境里根本验不了**。

---

## 1. 最近一次全量验证结果

```console
$ scripts/shen.sh traffic --check-l4
断言 27/27 通过 · 观察 0 条 · 缺口 8 条 · 出口卫生问题 0 条

── L4 结论核对（AR-12：引用必须真实存在）──
  · 结论 6 条（接受 3）· 遥测里已知判定 135 个
```

分组结果（分数来自控制台 `/api/flow`，`ST-7` 禁止在响应里回显）：

| 分组 | 场景 | 验到了什么 |
| --- | --- | --- |
| 自动化探针 | `probe-git-headless` **0.90**（`ua-headless`+`path-probe`）· `probe-git-normal-ua` **0.30** · `probe-git-head` 0.30 · `headless-home` 0.60 | 规则单独命中与叠加命中都对；无头浏览器 + 源码目录探测能同时出两个信号 |
| 扫描器指纹 | `scanner-sqlmap` 0.70 · `nuclei` **1.00**（触顶截断）· `nikto` 0.60 · `masscan` 0.50 · `script-client` 0.20 | 指纹类规则生效；**分数在 1.0 处截断**（实测 `nuclei` 0.6+0.4） |
| 敏感端点 | `/.env` 0.50 · `/.svn/entries` 0.40 · `/.aws/credentials`（子目录）0.60 · `/admin/login` 0.30 · `/wp-login.php` 0.50 | 前缀与 contains 两种算子都在工作；正常 UA 也能被路径规则命中 |
| 破坏性方法 | `method-delete` 0.50 | 方法维度规则生效（演示站对 DELETE 回 501，属**业务自己**的行为，场景用 `status_in` 声明） |
| 会话与缓存 | `session-reuse`（同会话两次 → **同一** `decision_id`）· `session-distinct`（换会话 → **不同** `decision_id`） | `ST-10` 判定缓存按 `(来源, 会话, 方法, 路径)` 复用，行为与文档一致 |
| 正常对照 | 首页 / 接口 / 静态资源 ×2 全部 **0.00** | 四类正常流量零信号零分（误伤面检查） |
| 边界 | 8KB UA · 2KB 路径 · 空 UA · 非 ASCII 路径 全部业务 200 且 0 分 | 极端输入不炸、不误判；非 ASCII 路径在控制台里存为**解码后**形态 |
| 出口卫生 | 33 个场景全部检查响应头与响应体 | 无 `x-shen*` / `Via` / `Caddy`；响应体里**没有**命中信号名与判定 id（`ST-7`） |
| L4 | 结论 6 条（接受 3）· 引用的证据全部真实存在 | `AR-12` 成立；`AR-14` 去重生效（135 个判定只产出 6 条结论） |

---

## 1.1 AI 欺骗内容注入的验收（2026-09-20 · 阶段 A）

命令：`make ai-check`（= `python3 scripts/dev/ai-inject-check.py`；它自己起业务站 + 幻境站 + 核心 + 适配器 + 控制台，
全程用临时目录与临时端口，不碰仓库内文件）。变更背景：[`../plans/2026-09-20-ai-capability-guardrail.md`](../plans/2026-09-20-ai-capability-guardrail.md) ·
[ADR-0023](../background/decisions/0023-deception-content-injection.md)。

```console
$ make ai-check
✓ 清单生成成功：aicap: 生成 16 条 / 护栏拒绝 0 条 · 清单 v1 variants=8 entries=2 bytes=8294
✓ 关卡：护栏拒绝全部内容 ⇒ 退出码 1 且**不写清单**：exit=1 清单存在=False
✓ 改道侧与「未注入基线」逐字节一致：经引擎 9eef5471e0ca88c0 vs 幻境直连 9eef5471e0ca88c0
✓ 业务侧与业务基线逐字节一致：经引擎 3e535d75f9418bcc vs 业务直连 3e535d75f9418bcc
✓ 逐请求事件：改道侧上报 inject=disabled：1 条改道侧请求，取值 ['disabled']
✓ 改道侧响应含注入内容：b'<html><body>MIRAGE-BACKEND<section class="service-detail">\n  <h2>Servi'…
✓ 注入是**插入**：幻境自己的正文仍在：字节 40 → 374
✓ 业务侧响应**逐字节不变**（INT-8）：3e535d75f9418bcc == 3e535d75f9418bcc
✓ 同会话同资源三次 → 响应 sha256 相同：sha256=744905218c9cee91，三次长度 [374, 374, 374]
✓ 16 个会话落在 ≥4 个不同变体上（多态生效）：命中 8 个变体（N=8）
✓ 逐请求事件：inject=applied 且带 content_id：20 条，例：c-a680814e9bb7e66d
✓ DAG 出现「内容注入」跳且三段文字齐全：内容注入 (L1) ⇒ c-2bdcafd96596f6ab
✓ 核心（关闭态 v3）已重启：pid=84890
✓ 适配器进程未被重启：pid=84887
✓ 关闭后：改道侧响应回到原样（不再注入）：9eef5471e0ca88c0 == 9eef5471e0ca88c0
✓ 关闭后：响应体里没有注入片段：字节 40 → 40
✓ 逐请求事件回到 inject=disabled（下发级开关生效）：1 条 disabled

✅ 全部通过（17 项）
```

| 验收判据 | 结论 |
| --- | --- |
| ① 关闭态与基线**逐字节一致** | ✅ 改道侧与业务侧都是 |
| ② 打开态：改道侧注入 + 业务侧零改写（`INT-8`） | ✅ 40 → 374 字节；业务侧 sha256 不变 |
| ③ `AR-30`：同会话一致 + 跨会话分布 | ✅ 同会话三次同 sha256；16 会话 → 8 个变体 |
| ④ 关卡：未过护栏的内容**零入库** | ✅ CLI 退出码 1 且不写清单（单测另覆盖四关各自拒） |
| ⑤ 秒级关闭：不重启适配器即停止注入 | ✅ 适配器 pid 不变；下一条请求 `inject=disabled` 且响应回到原样 |
| ⑥ 观测：DAG 有「内容注入」跳 | ✅ 五段文字齐全 + 逐请求事件带 `content_id` |

> ⚠️ **⑤ 的一个边界（已知）**：「秒级」指**从核心到适配器**的传播（适配器下一次 `Pull`，秒级）；
> 核心侧的开关变更本身需要重启核心（配置只在启动装载，见 [`../spec/config.md`](../spec/config.md) §1）。
>
> ⚠️ **未覆盖**：真实模型后端（阶段 B）· 跨节点部署 · 注入的延迟压测 · 内容质量的对抗性评估。

---

## 2. 缺口清单（设计有 / 该有，当前没做到）

| # | 缺口 | 类别 | 实测证据 | 影响 | 怎么关 |
| --- | --- | --- | --- | --- | --- |
| 1 | **查询串对判定不可见**：判定用的 `path` **不含** query，载荷放在参数里的攻击完全看不到 | 未覆盖 | `/download?file=../../etc/passwd` → **0 分 0 信号**；`/search?q=union+select` → 0 分 | SQLi / 穿越 / SSRF 等**最主流的入口**默认不判 | 给观测加 `query`（或 `raw_uri`）字段并允许规则匹配；落地后把这两条场景改回断言（`min_score` 0.65 / 0.75） |
| 2 | **编码即可绕过**：规则匹配原始字符串，`%2e%2e%2f`、`union%20select` 都不命中 | 精度 | 两条编码场景均 0 分（同 #1 的编码面） | 对手只需一次 URL 编码就能过 | 匹配前对 path/query 做一次解码与归一化（注意代价与幂等，`AR-30` 只约束响应内容，不受影响） |
| 3 | **前缀规则误伤合法文件**：`path-probe`（前缀 `/.git`）命中 `/.gitignore` | 精度 | `/.gitignore` → **0.30**（信号 `path-probe`） | 正常站点访问 `/.gitignore`（真实存在）会被当成探测 | 改为路径段边界匹配，或对具体文件用 `equals`；并在 [`../spec/config.md`](../spec/config.md) §2.4 写明前缀语义 |
| 4 | **前缀规则可被前缀绕过**：`/static/../.git/config` 不以 `/.git` 开头 ⇒ `path-probe` 不命中 | 精度 | 该请求只被 `path-traversal` 抓到（**0.70**），`path-probe` 未命中 | 攻击者加一层无害前缀即可躲开路径规则 | 匹配前归一化路径（去 `..`、合并重复斜杠）；需评估与上游行为的一致性 |
| 5 | **PUT / PATCH 未建模**：只登记了 DELETE | 未覆盖 | `PUT /api/items/1` → 0 分 0 信号 | 破坏性方法覆盖不全（写操作正是数据损失的入口） | 按业务语义补齐方法维度规则，或提供"推荐基线规则集" |
| 6 | ~~**白名单（`INT-25`）未消费**：配置里 `whitelist` 段只解析与校验~~ | ✅ **已闭合** | ✅ **已实现（核心侧）**：`loader.Whitelist()` → `director.whitelisted()`（源网段 / UA / 路径前缀，三项任一命中即 `route_origin`，且**先于**引流判定）；单测 `TestWhitelistByUserAgentSkipsJudgement`。边缘侧另由适配器实现 | 原风险（内部探针命规则后被引流）**已消**，`NI-1` 不再有此缺口 | —— |
| 7 | **`severity` 恒为 `none`** ⇒ 控制台"告警"永远为 0 | 未实现（档位未定） | `analyze` 观测：所有判定 `severity=none`；控制台 `alerts: 0` | 没有"告警"这个可用信号，运营只能看分值 | 定档位（信息/低/中/高）并在 `director` 产出；这是设计里已登记的未决项 |

> 缺口 #1/#2 是**同一根因的两个面**（query 不可见 + 字符串匹配），建议一起修。
> 缺口 #3/#4 也是同一类（前缀语义），修的时候就该把 `docs/spec/config.md` 的说法写准。

---

## 3. 当前环境里**验不了**的东西（需要别的接入形态或非影子模式）

| 能力 | 为什么验不了 | 怎么才能验 |
| --- | --- | --- |
| `route_mirage`（改道）· 注入 | 影子模式只观测（`INT-11`）且**没有登记幻境后端** ⇒ 所有改道都会回落业务（`NI-5`） | 非影子模式 + 在 `honeypots` 里登记一个可用后端；再加 `injects` 规则 |
| `block`（拦截） | 同上；且阈值 `block: 0.95`，示例规则最高叠加恰好触顶 1.0 | 非影子模式 + 一条高分规则 |
| 诱饵面 `decoy` | 示例配置里 `decoys.assets[].enabled: false`（默认关，`MD-25` observe-only） | 打开资产（仍须 observe-only） |
| 蜜罐后端池 `honeypot` | 未登记后端（"只做入口，不实现具体蜜罐"） | 起一个真实蜜罐并把 `type/addr` 登记进配置 |
| `isolation`（隔离短路） | 需要先命中"隔离"路径（当前处置未产出 block） | 非影子模式 + 隔离规则 |
| ② DNS 引流 · ① 旁路镜像 · ④ Sidecar | 当前栈只起 ③ 前置形态 | 分别按 [`../integrate/business-onboarding.md`](../integrate/business-onboarding.md) 部署对应形态 |
| L3 网络欺骗（Cilium/Tetragon） | 需要 K8s 集群 | 集群侧加载 [`../../modules/deception/netpolicy/config/`](../../modules/deception/netpolicy/config) 的声明式产物 |
| 真实 LLM 路径（`AR-19`…`AR-21` 双阶段收尾） | 未注入模型后端（`UnconfiguredClient` 显式失败） | 部署侧注入 `AnalysisClient`；否则只跑确定性部分 |

---

## 4. 运维坑：**不要单独重启 `core`**

`core` 是这套 compose 里**网络命名空间的持有者**（其它服务通过 `network_mode: service:core` 加入）。
单独 `docker compose restart core` 会重建命名空间，兄弟服务仍挂在**旧**命名空间上 ⇒
宿主端口映射指向新命名空间、里面没有监听者 ⇒ **控制台彻底不可达**（实测：HTTP 000，容器却显示 `Up`）。

```sh
scripts/shen.sh restart          # 正确做法：整栈重建（= up -d --force-recreate）
docker compose -f deploy/docker/compose.yaml up -d --force-recreate   # 等价
```

排查顺序：`scripts/shen.sh status` → `scripts/shen.sh logs console` → 若"容器 Up 但端口不通"，先想命名空间。

---

## 5. 这套验证**覆盖不到**的方法论边界（诚实说明）

1. **它验证的是"我们定义的行为"**，不是"攻击是否被真的骗到" —— 后者是欺骗有效性（`E2`/`E3` 类实验）的范畴；
2. **场景是白盒期望**：期望值按当前示例规则算的，改规则要同步改场景，否则会假红/假绿；
3. **没有并发与长稳**：不是压测，也不测内存/连接泄漏（`make bench` 与后续实验负责）；
4. **没有 TLS 入口**：默认对明文入口发流量（TLS 归 L0，见 [`../background/decisions/0019-tls-termination-belongs-to-l0.md`](../background/decisions/0019-tls-termination-belongs-to-l0.md)）。

---

## 6. 怎么把它用起来

```sh
scripts/shen.sh up                      # 起栈
scripts/shen.sh traffic                 # 全量验证（看断言 + 缺口两段）
scripts/shen.sh traffic --group 扫描器指纹
scripts/shen.sh traffic --only session-reuse
scripts/shen.sh traffic --json > /tmp/verify.json     # 给自动化/留档
```

**加一条自己的场景**：在 `scenarios.json` 里照格式加一条（`id/group/method/path/headers/expect`），
先写成 `observe_only` 看引擎实际怎么判，再收紧成断言。


---

## 7. 人工测试（15 分钟一轮）

起环境与造流量见 §1 / [`../ops/runbook.md`](../ops/runbook.md) 与 [`../../scripts/traffic/README.md`](../../scripts/traffic/README.md)。下面只留**人工才做的**两件事。

### 7.1 故障注入（`NI-1` 的现场验证）

```sh
# ① 把核心杀掉（控制台会报「读取核心失败」——这是对的）
pkill -f shen-demo-core
curl -s -o /dev/null -w "业务仍应 200：%{http_code}\n" -A "HeadlessChrome/120" http://127.0.0.1:18080/
```

期望：**业务照常 200**（`NI-3` 失败放行）；控制台的告警/流动**停止增长**（核心没了，没有人记）。

### 7.2 边界情形的快速检查

| 项 | 命令 | 期望 |
| --- | --- | --- |
| 探针端点未实现 | `curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:18080/__shen/healthz` | 走业务（未实现探针，见 `adapter-proxy.md` §8） |
| 大响应不被注入 | 用假业务站改成 2 MiB 页面（或直接看自动化测试） | 原样透传（`INT-8`） |
| 大上传不丢 body | `curl --data-binary @big -X POST $BASE/upload` | 上游收到完整字节数 |

> 以上自动化版本在 `make gate` 里：`V-1…V-4` 故障注入、转发边界、可见面卫生 —— 人工测试是**补场景**，不是替代它们。
