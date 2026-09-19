# AI Agent 时代的渗透防御研究综述

**主题**：欺骗防御 · 流量代理 · WAF · 负载均衡
**目标**：为一个「面向自主渗透 Agent 的安全检查防御引擎」建立研究基线与空白定位
**状态**：初版（基于 arXiv API 全量检索 + 联网检索）

---

## 0. 摘要

2025–2026 年，自主渗透 Agent（LLM-driven penetration agents）从论文原型进入可复现工程阶段，直接冲击了防御方的三项传统资产：**蜜罐的可信度**、**WAF 的规则有效性**、以及**网络边界的静态性**。与此同时，防御侧出现了两条互不交叉的演进路线：

- **欺骗侧**：LLM 让蜜罐从"低交互脚本"升级为"高交互可信环境"（VelLMes、ShellGames、AdvancedShelLM）；
- **流量侧**：代理/WAF 开始被重新定义为 LLM 应用的策略执行点（GAF、IPI-proxy、HoneyRoute）。

**本综述的核心发现是：这两条路线从未在"HTTP 流量层"汇合**。既有的欺骗系统停留在协议层（SSH/LDAP）或模型服务层，而既有的代理/WAF 系统只做"拦截或放行"，不做"欺骗性响应"。更关键的是，**负载均衡在安全语境下几乎完全被当作性能问题**，从未被当作"诱饵调度器"使用。

这构成了本项目的空白窗口：**把流量代理变成判别点、把负载均衡变成欺骗调度器**。

---

## 1. 研究方法

| 项 | 说明 |
| --- | --- |
| 检索源 | arXiv API（`export.arxiv.org/api/query`，按 relevance + submittedDate）、联网学术检索 |
| 关键词 | 欺骗防御 / deception defense / honeypot / honeytoken / moving target defense；流量代理 / traffic proxy / transparent proxy / middlebox / prompt injection proxy；WAF / web application firewall / adversarial ML；负载均衡 / load balancing / traffic steering / SDN |
| 时间范围 | 重点 2024–2026，经典奠基工作回溯至 2015 |
| 筛选 | 优先 系统/机制类论文 > 评测基准 > 综述；剔除纯红队工具介绍 |

> 注：文中 arXiv ID 与年份为检索时结果，投稿前请以官方页为准复核。

---

## 2. 领域地图

```text
                        ┌─────────────────────────────────────────┐
                        │            攻击侧（自主 Agent）           │
                        │  PentestAgent / HackSynth / AutoPentest │
                        │  + 能够推理 artifact、识破蜜罐            │
                        └────────────────┬────────────────────────┘
                                         │
        ┌────────────────────────────────┼────────────────────────────────┐
        │                                │                                │
   【轴一 欺骗防御】                 【轴二 流量代理】                【轴三 WAF】
   隐藏 / 模拟 / 混淆 / 引诱          观测 / 改写 / 拦截              规则 / ML / 混合
        │                                │                                │
   VelLMes  ShellGames             IPI-proxy  GAF                  WAF-A-MoLE
   Honeyval HoneyRoute             QASM  ChamaleoNet               ModSec-AdvLearn
   PHANTOM(蜜罐令牌)                Temporal Accumulator            X-WAD  WAMM
        │                                │                                │
        └────────────────┬───────────────┴────────────────┬───────────────┘
                         │                                │
                    ❌ 空白区 A                       ❌ 空白区 B
              代理作为欺骗执行点                  WAF 输出"欺骗响应"而非"拦截"
                         │                                │
                         └──────────────┬─────────────────┘
                                        │
                              【轴四 负载均衡：诱饵调度】
                                        │
                          SDN-Defend / RAD / Cyber-AnDe
                          Decoy Routing（仅路由，与 Agent 无关）
                                        │
                                        ▼
                          ★ 本项目：欺骗调度器（Director）
                            判别 → 决定攻击者去往"真实"还是"幻境"
```

---

## 3. 轴一：欺骗防御（Cyber Deception Defense）

### 3.1 理论与分类（奠基层）

| ID | 标题 | 贡献 | 相关性 |
| --- | --- | --- | --- |
| `2101.10121` | Game-Theoretic and ML-based Approaches for Defensive Deception: A Survey | 博弈论 + ML 两条线统一梳理，覆盖面最广 | ★★★ 分类学基准 |
| `1712.05441` | A Game-Theoretic Taxonomy and Survey of Defensive Deception | 提出欺骗四要素分类框架 | ★★★ 命名与设计词汇 |
| `1903.01442` | Game Theory for Cyber Deception: A Tutorial | 欺骗的博弈建模教学 | ★★ |
| `2008.03210` | A Theory of Hypergames on Graphs for Synthesizing Dynamic Cyber Defense with Deception | 攻击图 + 超博弈，形式化合成动态防御策略 | ★★★ 调度策略的理论工具 |
| `2510.25939` | SoK: Honeypots & LLMs, More Than the Sum of Their Parts? | 明确指出该领域**缺少统一评估**、真实部署进展有限 | ★★★ 必读，直接给出研究缺口 |

**关键洞察**：`2008.03210` 的 hypergame-on-graph 形式化，是本项目"调度器"决策逻辑最合适的理论骨架——它天然支持"防御方对攻击者认知的建模"，也就是"我知道你以为你在攻击哪台机器"。

### 3.2 LLM 驱动的欺骗系统（2025–2026 主战场）

| ID | 标题 | 机制 | 层次 | 相关性 |
| --- | --- | --- | --- | --- |
| `2510.06975` | VelLMes: A high-interaction AI-based deception framework | 多服务 LLM 欺骗框架，含**真人攻击者**评估 | 协议层 | ★★★ |
| `2606.27990` | AdvancedShelLM: A Stateful Multi-Agent LLM Honeypot for SSH Deception | 双 LLM 多 agent 架构提升可信度 | SSH | ★★ |
| `2606.17986` | ShellGames: Speculative LLM-Driven SSH Deception | 投机执行解决长会话延迟与一致性 | SSH | ★★★ 低延迟思路可迁移 |
| `2605.29963` | Honeyval: A Comprehensive Evaluation Framework for LLM-powered HTTP Honeypots | **HTTP 蜜罐的统一评测框架** | HTTP | ★★★ 你做评测可直接复用 |
| `2609.08306` | HoneyRoute: Honeypot-Model Routing for Adversarial LLM Serving | 推理服务层：恶意请求 → 蜜罐模型 | 模型服务层 | ★★★ "路由到蜜罐"的先例 |
| `2605.02992` | PHANTOM: Polymorphic Honeytoken Adaptation with Narrative-Tailored Organisational Mimicry | 多态蜜罐令牌，对抗统计/语法/语义指纹 | 资产层 | ★★★ 反识破的核心手段 |
| `2404.16118` | Act as a Honeytoken Generator! | LLM 自动生成 honeytoken | 资产层 | ★★ |
| `2608.11436` | When Agents Talk: Honeytokens under Shared Memory | Agent 间共享记忆让蜜罐令牌被"传递识破" | 多 agent | ★★★ 新型威胁 |
| `2509.16682` / `2509.01463` | LLM LDAP 蜜罐 / LLMHoney | 单服务 LLM 蜜罐实现 | LDAP/SSH | ★ |
| `2305.00925` | IoTFlowGenerator | 合成 IoT 流量用于欺骗 | 流量层 | ★★ 流量级欺骗的先例 |

**关键洞察**：`2609.08306` HoneyRoute 已经证明"把请求**路由**到蜜罐"是可行的——但它发生在**模型服务层**（推理请求），且只做"检测→路由"二值判断。**没有人把它下沉到 HTTP/网络层，也没有人做多级调度。**

### 3.3 反欺骗：Agent 识破蜜罐（对本系统威胁最大）

| ID | 标题 | 结论 | 威胁等级 |
| --- | --- | --- | --- |
| `2609.08093` | LLM-Based Penetration Testing in the Presence of Honeypots | LLM 攻击者能推理异构 artifact 识破传统蜜罐；传统蜜罐依赖"真实性 + 隐蔽性"的前提失效 | 🔴 致命 |
| `2607.26998` | AgentSnare: Learning to Delay, Divert, and Defuse Autonomous Penetration Agents | 现有防御依赖**静态、孤立的诱饵**；高级 agent 会渐进识别并绕过 | 🔴 致命 |
| USENIX Sec'25 | Cloak, Honey, Trap: Proactive Defenses Against LLM Agents | 利用 LLM 弱点（偏见/记忆/tokenization）主动欺骗与反制 | 🟠 可借鉴 |
| `2605.21956` | Detecting Offensive Cyber Agents: A Detection-in-Depth Approach | 分层检测自主攻击 agent | 🟠 检测思路 |
| BTH thesis | Identifying LLM-Powered Cyber Attacks with Timing Analysis and Honeytoken-Based Deception | 现有 SSH 蜜罐只做"脚本 vs 人"二分类，无法识别 LLM 攻击者；提出时序分析 | 🟠 特征工程参考 |

**这是本项目必须正面回答的问题**：你的欺骗如果也是"静态、孤立"的，会被同样识破。`AgentSnare` 给出的方向是 **learning to delay/divert**——即**调度器需要学习**，而不是查表。

### 3.4 主动防御 / 移动目标防御（MTD）

| ID | 标题 | 机制 |
| --- | --- | --- |
| `2603.20981` | Cyber Deception for Mission Surveillance via Hypergame-Theoretic DRL | 超博弈 + 深度强化学习决策欺骗 |
| `2607.12199` | MTD-Playground: An Attacker-Aware Evaluation Framework for Network MTD | 攻击者感知的 MTD 评测框架 |
| `2606.15229` | LSTM Look-Ahead MTD Based on Historical Malicious Scan | 用历史扫描预测做 IP 洗牌 |
| `2506.20770` | Perry: A High-level Framework for Accelerating Cyber Deception Experimentation | 欺骗实验平台（可移植、易扩展） |
| `1704.01482` / `2410.02254` | CHAOS (SDN MTD) / MTDNS | SDN 路径随机化 / DNS 弹性 |
| `2108.13980` | Incorporating Deception into CyberBattleSim for Autonomous Defense | RL 攻击者 vs 欺骗元素的实验 |

---

## 4. 轴二：流量代理（Traffic Proxy）

**这是被低估、却与本项目结合最紧的一轴。**

| ID | 标题 | 机制 | 对本项目价值 |
| --- | --- | --- | --- |
| `2605.11868` | **IPI-proxy**: An Intercepting Proxy for Red-Teaming Web-Browsing AI Agents | 拦截代理，注入/检测间接提示注入 | ★★★ 证明"代理是 agent 安全的天然执行点" |
| `2606.20746` | Amplify, Don't Create: Temporal Accumulation for Slow-Burn Prompt Injection | **代理侧时序累加器**，捕获分布式弱指令攻击 | ★★★ 直接给出"代理侧有状态判别"的设计范式 |
| `2601.15824` | **Generative Application Firewall (GAF)** | 把 prompt 过滤/guardrail/脱敏统一为单一执行点，类比 WAF | ★★★ 你的架构可对标概念 |
| `2607.08282` | Multi-Agent Firewall Architecture for Privacy Protection | 用户侧多 agent 防火墙 | ★★ |
| `2602.03354` | QASM: QUIC-Aware Stateful Middleboxes | HTTP/3/QUIC 下中间盒的流识别难题 | ★★ 工程约束（加密流量） |
| `2508.12496` | ChamaleoNet: Programmable Passive Probe | 透明被动探针，生产网可见性 | ★★ 观测层组件 |
| `2008.02979` | Role-Based Deception in Enterprise Networks | 网络层角色欺骗（让攻击者误判主机价值） | ★★★ 调度器的目标函数来源 |
| `1710.05527` | Decoy Routing: Placing Decoy Routers in the Internet | 路由器作为代理 | ★★ 历史先例 |

**关键洞察**：

1. `2606.20746` 的"代理侧时序累加"证明了**代理必须是有状态的**，且**单事件判别必然失效**——这直接支持"调度器需要会话级状态"。
2. `2602.03354` 提醒：**加密流量（QUIC/HTTP3）会让流量级判别退化**。所以你的判别不应只依赖 payload，需要结合**行为/时序/连接图**特征。

---

## 5. 轴三：WAF

### 5.1 攻：绕过与对抗

| ID | 标题 | 贡献 |
| --- | --- | --- |
| `2001.01952` | WAF-A-MoLE: Evading WAFs through Adversarial ML | 奠基：用对抗 ML 生成绕过 payload |
| `2308.04964` | ModSec-AdvLearn: Countering Adversarial SQL Injections with Robust ML | 直接对标 OWASP CRS，四组默认配置的鲁棒性分析 |
| `2401.02615` | AdvSQLi: Generating Adversarial SQLi against Real-world WAF-as-a-service | 真实云 WAF 的可绕过性 |
| `2312.07885` | RAT: RL-Driven and Adaptive Testing for Vulnerability Discovery in WAFs | 强化学习驱动 WAF 漏洞挖掘 |
| `2504.08176` | GenXSS: AI-Driven Framework for Automated Detection of XSS Attacks in WAFs | 生成式绕过发现 |

### 5.2 防：检测与架构

| ID | 标题 | 贡献 |
| --- | --- | --- |
| `2512.06390` | **Web Technologies Security in the AI Era: A Survey of CDN-Enhanced Defenses** | ⭐ 把 CDN/边缘定义为 ML 检测、限流、隔离的执行点 |
| `2608.27172` | X-WAD: eXplainable Web Anomaly Detection | 只建模正常行为（API 驱动架构），可解释 |
| `2512.23610` | WAMM: AI-Based Framework for Dataset Refinement and Model Evaluation | 多分类 Web 攻击检测 + 数据集精炼，暴露规则系统局限 |
| `2608.28889` | Enhancing WAFs with ML for SQL Injection Detection | DistilBERT + 堆叠集成 |
| — | PhantomWall (IEEE WCCST'26) | Transformer-Autoencoder 集成的自适应零日 WAF |
| — | A hybrid DL and attention fusion framework (Sci Rep) | 云 WAF 零日威胁检测，指出规则系统对新变体检测率 < 12% |
| — | 多层防御框架对抗 ML-WAF 对抗攻击 | 三层防御 |
| — | **CheeseWAF** (OSS) | LLM WAF + 缓存，专门解决"逐请求同步调 LLM 延迟过高" |

**关键洞察**：

- 规则型 WAF 对新变体检测率 **< 12%**（Sci Rep），这为"不靠拦截靠欺骗"提供了量化动机。
- `2512.06390` 把 **CDN 边缘**当作执行点——**这与"代理 + 负载均衡"在物理上是同一个位置**。你的架构在工程上有现成落点。

---

## 6. 轴四：负载均衡

**产出最薄的一轴，且安全语境下几乎只关心"性能/可用性"。**

| ID/来源 | 标题 | 视角 |
| --- | --- | --- |
| `1904.05926` | Method of Self-Similar Load Balancing in Network Intrusion Detection System | ★★ 最贴近"安全 + 负载均衡"：按多重分形特性分配检测负载 |
| MDPI Sensors | SDN-Defend: Lightweight Online Attack Detection and Mitigation for DDoS in SDN | 控制器负载与检测缓解 |
| TIFS'24 | Cyber-AnDe: Cybersecurity Framework with adaptive sampling | 采样率 vs 检测精度 vs 资源开销的权衡 |
| SJSU | RAD: Robust and Agile System against Fault and Anomaly Traffic in SDN | 流量分析器 / 流量调度器 / 规则管理器三段式 |
| — | LBHMARS: Load Balancing Heterogeneous Multipath Authenticated Routing | 多路径认证 + 负载均衡 |
| `1804.10740` | Heavy Hitters over Interval Queries | LB/网络安全的测量基础件 |
| `2410.02254` | MTDNS | DNS 层弹性调度 |

**关键洞察（也是本项目最大的机会）**：
> 在全部检索结果中，**没有任何工作把负载均衡器当作"诱饵调度器"**——即按攻击者画像，把请求分配到真实后端或幻境后端，并把这个分配本身当作防御机制。
>
> 负载均衡天然具备欺骗调度器所需的三要素：**（1）它已经坐在所有流量的必经路径上；（2）它已经具备后端池与路由能力；（3）它的调度决策对客户端不可见**（客户端无法区分"我被分到了真后端"还是"我被分到了蜜罐后端"）。

---

## 7. 交叉空白分析（本项目的立足点）

| # | 交叉 | 已有工作 | 空白 | 机会评级 |
| --- | --- | --- | --- | --- |
| A | 代理 × 欺骗 | HoneyRoute（模型服务层路由）、VelLMes（协议层）、Honeyval（HTTP 评测） | **没人把 HTTP/SOCKS 代理本身当作欺骗执行点** | ⭐⭐⭐ |
| B | 负载均衡 × 欺骗 | Decoy Routing（仅抗审查，与 agent 无关） | **没人把 LB 当诱饵调度器** | ⭐⭐⭐ |
| C | WAF × Agent 流量 | GAF 提出"LLM 应用的 WAF" | **没人做"agent 流量指纹识别 + 欺骗式响应"**（识别后返回构造好的假响应，而非拦截） | ⭐⭐⭐ |
| D | 欺骗 × 评测基准 | MTD-Playground、BountyBench、Honeyval 各自成形 | **没有"欺骗防御 vs 自主渗透 agent"的对抗 benchmark** | ⭐⭐ |
| E | 加密流量 × 判别 | QASM（QUIC 中间盒） | **QUIC/HTTP3 下如何维持会话级欺骗一致性** | ⭐⭐ |
| F | 多 agent 共享记忆 × 欺骗 | `2608.11436` Honeytokens under Shared Memory | **蜜罐被 agent 群体"众包识破"后的重生成机制** | ⭐⭐ |

**一句话定位**：
> 现有工作要么在**协议层**做可信欺骗，要么在**流量层**做二值拦截；本项目在**流量代理层的调度点**上，把"拦截/放行"的二值决策升级为**「放行 / 拦截 / 误导」三值决策**，并用负载均衡作为执行机构。

---

## 8. 对本引擎（欺骗调度器）的设计启示

### 8.1 必须回答的四个设计问题

| 问题 | 来自哪篇 | 你的答案应当是 |
| --- | --- | --- |
| 静态诱饵会被渐进识破，怎么办？ | `2607.26998` AgentSnare | 调度器需要**学习**（RL / 超博弈），且诱饵需要**多态**（`2605.02992`） |
| 单事件判别必然失效，怎么办？ | `2606.20746` | 判别必须**会话级有状态**（时序累加器） |
| 加密流量让 payload 判别退化，怎么办？ | `2602.03354` QASM | 判别需融合**行为/时序/连接图**特征，而非只看内容 |
| 你怎么知道防御有效？ | `2510.25939` SoK、`2605.29963` Honeyval | 必须建**对抗 bake-off**：自主渗透 agent vs 你的调度器 |

### 8.2 建议的架构分层（Director 视角）

```text
   ┌─────────────┐
   │  Sensor     │  代理/WAF 提供：会话流、请求特征、时序
   └──────┬──────┘
          ▼
   ┌─────────────┐
   │  Judge      │  判别：人 / 脚本 / LLM-Agent（多类，非二值）
   └──────┬──────┘  指标：TPR on agent, FPR on benign, 判别延迟
          ▼
   ┌─────────────┐
   │  Director   │  ★ 核心：三值决策 + 后端选择 + 诱饵多态度
   └──────┬──────┘  策略：规则 → 博弈 → 学习（逐级演进）
          ▼
   ┌─────────────┐
   │  Backends   │  真实后端池 ←→ 幻境后端池（LB 执行）
   └─────────────┘
          │
          └──► 回馈：诱饵被识破的信号 → 触发再生成
```

**关键指标（建议从第一天就开始记录）**：

- **诱导率**（attacker engagement time）：攻击者在幻境停留时长
- **识破延迟**（time-to-detect-decoy）：攻击者多久发现是假的（越高越好）
- **误调度率**（benign → decoy）：正常用户被误导入幻境的比率（必须极低）
- **情报产率**：每单位攻击者会话提取的 TTP 数量
- **调度开销**：P99 延迟增加

### 8.3 立即可复用的评测资产

- `2605.29963` **Honeyval**：HTTP 蜜罐评测框架 → 直接改造为"幻境"评测
- `2607.12199` **MTD-Playground**：攻击者感知评测 → 改造为"调度器"评测
- `2601.15824` **GAF**：概念对标，明确你的"三值决策"与它的差异
- OSS：**CheeseWAF**（LLM WAF 缓存）、**mitmproxy**（代理底座）

---

## 9. 必读清单（按优先级）

1. `2609.08093` — LLM-Based Penetration Testing in the Presence of Honeypots ← **威胁模型起点**
2. `2607.26998` — AgentSnare ← **必须超越的对手**
3. `2605.11868` — IPI-proxy ← **代理作为执行点的证据**
4. `2606.20746` — Temporal Accumulation ← **代理侧有状态判别范式**
5. `2510.25939` — SoK: Honeypots & LLMs ← **研究缺口总览**
6. `2601.15824` — Generative Application Firewall ← **架构对标**
7. `2512.06390` — CDN-Enhanced Defenses Survey ← **部署位置与 ML 检测全景**
8. `2605.29963` — Honeyval ← **评测框架可直接复用**
9. `2605.02992` — PHANTOM ← **反识破的多态手段**
10. `2008.03210` — Hypergames on Graphs ← **调度策略的理论骨架**

---

## 10. 参考文献

### 欺骗防御 / 蜜罐

```text
2101.10121  Game-Theoretic and ML-based Approaches for Defensive Deception: A Survey
1712.05441  A Game-Theoretic Taxonomy and Survey of Defensive Deception
1903.01442  Game Theory for Cyber Deception: A Tutorial
2008.03210  A Theory of Hypergames on Graphs for Synthesizing Dynamic Cyber Defense with Deception
2510.25939  SoK: Honeypots & LLMs, More Than the Sum of Their Parts?
2510.06975  VelLMes: A high-interaction AI-based deception framework
2606.27990  AdvancedShelLM: A Stateful Multi-Agent LLM Honeypot for SSH Deception
2606.17986  ShellGames: Speculative LLM-Driven SSH Deception
2605.29963  Honeyval: A Comprehensive Evaluation Framework for LLM-powered HTTP Honeypots
2609.08306  HoneyRoute: Honeypot-Model Routing for Adversarial LLM Serving
2605.02992  PHANTOM: Polymorphic Honeytoken Adaptation with Narrative-Tailored Organisational Mimicry
2404.16118  Act as a Honeytoken Generator! An Investigation into Honeytoken Generation with LLMs
2608.11436  When Agents Talk: Honeytokens under Shared Memory
2509.16682  Design and Development of an Intelligent LLM-based LDAP Honeypot
2509.01463  LLMHoney: A Real-Time SSH Honeypot with LLM-Driven Dynamic Response Generation
2305.00925  IoTFlowGenerator: Crafting Synthetic IoT Device Traffic Flows for Cyber Deception
2008.02979  Role-Based Deception in Enterprise Networks
2505.00465  HoneyWin: High-Interaction Windows Honeypot in Enterprise Environment
```

### 反欺骗 / Agent 攻防

```text
2609.08093  LLM-Based Penetration Testing in the Presence of Honeypots
2607.26998  AgentSnare: Learning to Delay, Divert, and Defuse Autonomous Penetration Agents
2605.21956  Detecting Offensive Cyber Agents: A Detection-in-Depth Approach
2505.15216  BountyBench: Dollar Impact of AI Agent Attackers and Defenders
2504.05408  Frontier AI's Impact on the Cybersecurity Landscape
(USENIX Sec'25) Cloak, Honey, Trap: Proactive Defenses Against LLM Agents
(BTH) Identifying LLM-Powered Cyber Attacks with Timing Analysis and Honeytoken-Based Deception
```

### 流量代理 / LLM 应用安全

```text
2605.11868  IPI-proxy: An Intercepting Proxy for Red-Teaming Web-Browsing AI Agents
2606.20746  Amplify, Don't Create: Temporal Accumulation for Slow-Burn Prompt Injection
2601.15824  Introducing the Generative Application Firewall (GAF)
2607.08282  Multi-Agent Firewall Architecture for Privacy Protection
2602.03354  QASM: A Novel Framework for QUIC-Aware Stateful Middleboxes
2508.12496  ChamaleoNet: Programmable Passive Probe for Enhanced Visibility
1710.05527  The Devils in The Details: Placing Decoy Routers in the Internet
2507.13169  Prompt Injection 2.0: Hybrid AI Threats
2603.17419  Caging the Agents: A Zero Trust Security Architecture for Autonomous AI
2510.06445  A Survey on Agentic Security: Applications, Threats and Defenses
```

### WAF

```text
2001.01952  WAF-A-MoLE: Evading Web Application Firewalls through Adversarial ML
2308.04964  ModSec-AdvLearn: Countering Adversarial SQL Injections with Robust ML
2401.02615  AdvSQLi: Generating Adversarial SQL Injections against Real-world WAF-as-a-service
2312.07885  RAT: RL-Driven and Adaptive Testing for Vulnerability Discovery in WAFs
2504.08176  GenXSS: an AI-Driven Framework for Automated Detection of XSS Attacks in WAFs
2512.06390  Web Technologies Security in the AI Era: A Survey of CDN-Enhanced Defenses
2608.27172  X-WAD: eXplainable Web Anomaly Detection
2512.23610  Enhanced Web Payload Classification Using WAMM
2608.28889  Enhancing Web Application Firewalls with ML for SQL Injection Detection
2603.02963  Multi-Agent Honeypot-Based Request-Response Context Dataset for SQLi Detection
(OSS)       CheeseWAF — LLM-supported WAF with caching
```

### 负载均衡 / SDN 安全

```text
1904.05926  Method of Self-Similar Load Balancing in Network Intrusion Detection System
1804.10740  Heavy Hitters over Interval Queries
(MDPI)      SDN-Defend: Lightweight Online Attack Detection and Mitigation for DDoS in SDN
(TIFS'24)   Cyber-AnDe: Cybersecurity Framework with adaptive sampling
(SJSU)      RAD: Robust and Agile System against Fault and Anomaly Traffic in SDN
2410.02254  MTDNS: Moving Target Defense for Resilient DNS Infrastructure
1704.01482  CHAOS: an SDN-based Moving Target Defense System
```

### MTD / 主动防御

```text
2603.20981  Cyber Deception for Mission Surveillance via Hypergame-Theoretic DRL
2607.12199  MTD-Playground: An Attacker-Aware Evaluation Framework for Network MTD
2606.15229  LSTM Look-Ahead Moving Target Defense Based on Historical Malicious Scan
2506.20770  Perry: A High-level Framework for Accelerating Cyber Deception Experimentation
2108.13980  Incorporating Deception into CyberBattleSim for Autonomous Defense
```

### 攻击侧 Agent（威胁模型参考）

```text
2411.05185  PentestAgent: Incorporating LLM Agents to Automated Penetration Testing
2412.01778  HackSynth: LLM Agent and Evaluation Framework for Autonomous Penetration Testing
2505.10321  AutoPentest: Enhancing Vulnerability Management With Autonomous LLM Agents
2512.11143  Automated Penetration Testing with LLM Agents and Classical Planning
2509.07939  Guided Reasoning in LLM-Driven Penetration Testing Using Structured Attack Trees
2602.17622  What Makes a Good LLM Agent for Real-world Penetration Testing?
2607.09653  VEXAIoT: Autonomous IoT Vulnerability EXploitation using AI Agents
```

---

## 附：待办与后续

- [ ] 逐篇深读 Top10，产出「方法 / 数据集 / 评测指标 / 局限」对比表
- [ ] 复核所有 arXiv ID 与发表 venue
- [ ] 补充非 arXiv 来源（USENIX / NDSS / CCS / S&P 近三年）
- [ ] 明确威胁模型：对手是哪种 agent（单 agent / 多 agent / 带记忆 / 带工具集）
- [ ] 确认评测基准选型（Honeyval vs MTD-Playground vs 自建）
