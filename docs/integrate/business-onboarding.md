# 把真实业务接进引擎（接入方视角）

## 0. 先选形态

| 形态 | 客户改什么 | 引擎在路径上 | 能不能处置 |
| --- | --- | --- | --- |
| ① 旁路镜像 | 加一条镜像规则 | ❌ 不在 | ❌ 只观察 |
| ② DNS 引流 | 改 DNS 解析 | ✅ | ✅ |
| ③ **反向代理前置** | 把业务 upstream 指向引擎 | ✅ | ✅ |
| ④ Sidecar | 业务 Pod 内注入边车 | ✅ | ✅ |

首次上线**必须**用形态 ①（只观察）或影子模式（`INT-11`）；**禁止**首次即全量接管。

## 1. 形态 ③ 的最短路径（推荐先用它做人工测试）

```sh
# 引擎起在业务前面；SHEN_PROXY_UPSTREAM 指向业务真实地址
SHEN_PROXY_UPSTREAM=http://10.0.0.20:9000 \
SHEN_CORE_ADDR=127.0.0.1:9443 \
SHEN_PROXY_LISTEN=0.0.0.0:8080 \
SHEN_PROXY_SHADOW=true \                 # ★ 首次必须 true（INT-11）
go run ./modules/deception/proxy/cmd/proxy
```

然后把业务域名的 upstream 改到 `引擎:8080`（云 LB / nginx / Ingress 的 upstream，**不改业务代码**，`INT-7`）。

**TLS 归属**（重要）：默认交**客户 L0**终结 TLS（客户的 nginx/LB 就是真实站同款栈 ⇒ TLS 指纹天然一致）；
引擎自终结（`SHEN_PROXY_TLS_MODE=manual|acme`）只在客户没有 L0 时使用，且**启动会打警告**。依据
[`ADR-0019`](../background/decisions/0019-tls-termination-belongs-to-l0.md)。

## 2. 必须配对的几项

| 项 | 为什么 |
| --- | --- |
| `SHEN_PROXY_WHITELIST`（内部网段 / 探针） | 漏了会把运维探针判成攻击者（`INT-25`：白名单先于改道判定） |
| `SHEN_PROXY_TRUST_XFF` | 前面有可信 L0 时才开；业务侧要拿到真实来源 IP（`INT-23`） |
| `SHEN_PROXY_MIRAGE` 或策略面下发的后端表 | 不改道就没有幻境；查不到后端名会**回落业务**（`NI-5`） |
| 核心的 `rules`（见 `deploy/config/config.example.yaml`） | 规则为空 ⇒ 所有分数为 0，等于什么都没在判 |

## 3. 上线后怎么确认「没影响业务」

1. **影子模式观察**：控制台看分值/信号分布，业务日志与升级前后对比（`INT-12` 的阶梯放开）；
2. **对照直连**：直接访问业务地址与经引擎访问各一次，响应应一致（`INT-8`：业务侧响应零改写）；
3. **故障演练**：杀掉引擎，业务必须 100% 正常（`NI-1`；自动化版本见 [人工测试（功能验证 §7）](../ops/functional-verification.md) §4）。

## 4. 解除接入

按 `NI-11`：**移除一段配置即可回退**，不要求重启业务 —— 把 upstream 改回业务地址即可。
