# 已知问题与绕行

> 每条都是**实际撞到过**的。按「症状 → 原因 → 结论」写。
> 最后更新：2026-09-19（`K-9`…`K-11` 来自 `E1` spike；`K-12`…`K-15` 来自架构门禁的建设；`K-16`/`K-17` 来自四个接入形态的落地；`K-18` 来自装配开发循环；`K-19` 来自 `policy` 模块的落地；`K-20` 来自策略面端到端验证）

---

## 分类（现行 / 历史）

**现行**（今天仍会撞上）：`K-1` `K-2` `K-3` `K-4` `K-5` `K-6` `K-7` `K-8` · `K-12` `K-13` · `K-14` `K-15` `K-16` · `K-17` `K-18` `K-19` `K-20` · `K-21` `K-22` `K-23`

**历史**（那个方案已经不在了，保留是为记录当时的理由）：`K-9` `K-10` `K-11` —— 都属于 **ext_proc / Envoy WASM 方案**；
该方案已被 [`../background/decisions/0017-caddy-l1-base.md`](../background/decisions/0017-caddy-l1-base.md) 取代（L1 转发改用内嵌 Caddy），
代码里已无 ext_proc。**不要按它们改今天的代码。**

## K-1 · 根目录放 `go.work` 会让 `go build ./...` 失效

**症状**（原始报错）：

```console
$ go build ./...
pattern ./...: directory prefix . does not contain modules listed in go.work or their selected dependencies
```

**原因**：`go.work` 里写了 `use ./src`，于是仓库根**不再是任何模块的根**。`./...` 相对当前目录展开，却找不到归属的模块。

**结论**：**不要用 `go.work`**。`go.mod` 直接放仓库根（见 [`../background/decisions/0007-repo-layout.md`](../background/decisions/0007-repo-layout.md)）。
需要跨多个模块时再评估，不要为了「统一入口」引入它。

---

## K-2 · 代码放 `src/` 会打断自动化测试运行器

**症状**：从仓库根跑 `go test ./...` 失败：

```console
pattern ./...: directory prefix . does not contain main module or its selected dependencies
```

而 `cd src && go test ./...` 正常。

**原因**：`go.mod` 在 `src/` 里 —— Go 工具链、编辑器、任何默认配置都按「仓库根 = 模块根」的约定工作，多一层就全部要特判。

**结论**：**代码不放 `src/`**，按架构平面扁平放（`core/` `edge/` `deception/` `analysis/` `console/`）。
代价是顶层目录多几个，换来的是工具链零摩擦。见 ADR-0007。

---

## K-3 · Go 注释里的 `**加粗**` 不渲染

**症状**：注释读起来是 `Package store 是核心**唯一的 I/O 出口**。` —— 星号让句子断在奇怪的位置。

**原因**：Go 注释是纯文本，不是 Markdown。`go doc` 与编辑器都不解析它。

**结论**：Go 注释用纯文本。要强调就用中文标点或直接改写句子。
（Markdown 文档里的 `**加粗**` 仍然正常使用。）

---

## K-4 · Go 不支持方法重载 → 一个大结构体无法实现多个同签名接口

**症状**（原始报错）：

```console
*Memory does not implement IsolationStore (wrong type for method Get)
  have Get(context.Context, string) (contract.SessionKey, bool, error)
  want Get(context.Context, contract.SessionKey) (contract.IsolationHit, error)
method Memory.Get already declared
```

**原因**：`SessionStore` 与 `IsolationStore` 都有 `Get` / `Put`，但签名不同。Go 不允许同名方法重载。

**结论**：**每个实体一个类型**（`SessionMemory` / `IsolationMemory` / …）。
这同时让单测替身只需实现它用到的那一两个方法。

---

## K-5 · `netip.Addr` 没有 `Equal` 方法

**症状**：`addr.Equal(...) undefined (type netip.Addr has no field or method Equal)`

**原因**：`Equal` 是 `net.IP` 的方法。`netip.Addr` 是**可比较的结构体**，不需要方法。

**结论**：直接用 `==` / `!=`。这也是 `netip` 相对 `net` 的优点之一（可直接做 map 键）。

---

## K-6 · protoc 生成 Go 代码会多套一层目录

**症状**：生成了 `api/judge/v1/shen/api/judge/v1/judge.pb.go` 这类嵌套路径。

**原因**：没有告诉 protoc 模块根在哪，它把 `go_package` 里的完整路径当成了相对路径。

**结论**：加 `--go_opt=module=<mod>`：

```bash
protoc -I api \
  --go_out=. --go_opt=module=shen \
  --go-grpc_out=. --go-grpc_opt=module=shen \
  api/judge/v1/judge.proto
```

`make generate` 已经封好了这个命令。

---

## K-7 · 独立加粗行会被 markdownlint 判为「用强调当标题」

**症状**：MD036 `Emphasis used instead of a heading`。

**原因**：`**做什么**` 独占一行时，渲染效果与标题一样，但语义不是。

**结论**：用真标题（`### 做什么`）。附带好处是大纲面板能导航到它。

---

## K-8 · macOS 文件名大小写不敏感，会误判「文件有两个」

**症状**：`[ -f AGENTS.MD ]` 与 `[ -f AGENTS.md ]` **同时为真**，看起来像存在两个文件。

**原因**：APFS 默认大小写不敏感 —— 两个路径指向**同一个文件**。

**结论**：做存在性检查时用 `ls -1` 实际列目录，不要用多个大小写变体去 test。
本次开发曾据此误报「`AGENTS.MD` 是重复文件」。

---

## K-9 ·（历史）ext_proc 流不 drain 到 EOF 会泄漏（`CloseSend` 不够）

**症状**：客户端开 200 条 ext_proc 流、每条都 `CloseSend()`，但 goroutine 数从 6 涨到 211，**不回落**。

**原因**：`CloseSend()` 只是**半关发送方向**。流要等对端也关闭、且客户端把 `Recv` 读到 `io.EOF` 才真正终结。只 `CloseSend()` 再配上永不取消的 `context.Background()`，流的 goroutine 就一直挂着。

**结论**：每条流配一个**可取消的 context**，关闭时走完整流程：

```go
ctx, cancel := context.WithCancel(parent)
st, _ := cli.Process(ctx)

// 关闭：半关发送 → 读到 EOF → 释放 context
_ = st.CloseSend()
for {
    if _, err := st.Recv(); err != nil {
        cancel()
        break // io.EOF = 正常
    }
}
```

**为什么重要**：ext_proc 是**每个 HTTP 请求一条流**。即使每流泄漏一点，高并发下也会把内存吃光 —— 而且现象是「随流量缓慢增长」，很难归因。`E1` spike 实测：修掉这个之后 200 条流开合 goroutine 完好回落。

---

## K-10 ·（历史）ext_proc 收到未识别的阶段必须失败关闭

**症状**：不适用 —— 这是**要主动防**的，不是撞到的坑。`E1` spike 里作为测试固化下来了。

**原因**：`ProcessingRequest` 的 `request` 是 oneof，理论上可能为空或出现我方还未支持的阶段。若此时服务端**不回响应**，流会挂住（Envoy 等超时）；若**瞎回一个**，Envoy 会因分支错配报错。

**结论**：`default` 分支回 `ImmediateResponse` + `500`，让请求**失败关闭**：

```go
default:
    return &extprocv3.ProcessingResponse{
        Response: &extprocv3.ProcessingResponse_ImmediateResponse{
            ImmediateResponse: &extprocv3.ImmediateResponse{
                Status: &typev3.HttpStatus{Code: typev3.StatusCode_InternalServerError},
            },
        },
    }
```

**为什么重要**：本项目的第一条约束就是「**不影响原始业务**」。在协议层遇到看不懂的东西时，**绝不能静默** —— 静默等于把请求挂死，比报错更糟。

---

## K-11 ·（历史）每个响应分支必须与请求阶段配对

**症状**：写 ext_proc 服务时最容易犯的错 —— `request_body` 回了 `ResponseHeadersResponse`。

**原因**：`ProcessingResponse` 也是 oneof，六个阶段各有对应分支，但类型上**没有强制配对**（`HeadersResponse` 同时服务 `RequestHeaders` 与 `ResponseHeaders`）—— 编译器不会拦你。

**结论**：把「阶段 → 响应分支」写成一个函数，并**用单测逐阶段断言**。`E1` spike 的 `TestSixPhasesRoundTrip` 就是这个形状：发一个阶段、收一个响应、断言 `gotPhase(resp) == want`。

**为什么重要**：错配的后果是 Envoy 直接报错，但**报错点在 Envoy 侧**，排查会往错误方向走。

---

## K-12 · 用「默认值 + 比大小」挑最严重项，会退化成静默放过

**症状**：许可检查器把 `BSD-3-Clause` 的依赖显示成「**未识别**」，判定却仍是「**允许**」。自相矛盾。

**原因**：挑最严格许可的代码写成了「给一个默认值，然后逐项比大小」：

```go
worst := sig{id: "", effect: ok}   // ← 默认值等级是 ok
for _, t := range texts {
    s, hit := match(t)
    if !hit { continue }
    if s.effect > worst.effect { worst = s }   // ← 命中的也是 ok，永远不成立
}
return worst.id, worst.effect   // id 还是空的，等级还是 ok
```

命中的许可等级恰好等于默认值等级时，`>` 不成立 → `worst` 从没被赋值 →
**结果退化成「认不出这个许可」，而且判定还是通过**。

讽刺的是：我写这个检查器的目的正是「不许静默放过认不出的许可」，
结果实现里就藏着一条静默放过的路径。

**结论**：挑最值要用「**首次命中即初始化**」，不要用「默认值 + 比大小」：

```go
var worst *sig
for _, t := range texts {
    s, hit := match(t)
    if !hit { continue }
    if worst == nil || s.effect > worst.effect {
        hit := s
        worst = &hit
    }
}
if worst == nil { /* 确实一个都没命中 */ }
```

**更一般的教训**：**凡是「取最严重/最宽/最早」的聚合，默认值都会骗你。**
只要聚合的返回值和初始值可能同等级，比较就会静默失效。用「有没有」而不是「比大小」。

---

## K-13 · 枚举的严重性等级要按「确定程度」排，不是按直觉

**症状**：一份依赖同时含 `GPL` 和一份无法识别的许可时，报告只说「需人工判定」，
**把已知的 GPL 盖掉了**。

**原因**：`verdict` 枚举顺序写成了 `ok, restricted, unknown`，于是「需人工」被当成最严重。

**结论**：按**确定程度**排，让「已知禁止」压过「不确定」：

```go
const (
    ok verdict = iota
    unknown      // 认不出 —— 需要人来看
    restricted   // 认得出且禁止 —— 确定失败
)
```

**为什么重要**：「不确定」不等于「最糟」。已经认出是 GPL 的，比认不出的更该被突出显示。

---

## K-14 · shellcheck 会把 Makefile 当 POSIX shell 分析，对 `$$(` 误报

**症状**：Makefile 里写 `files=$$(gofmt -l .)`（Make 的正确转义：`$$` → shell 看到的 `$`），
shellcheck 报 `SC1036 '(' is invalid here` / `SC1088 Parsing stopped here`。

**原因**：`$$(` 在**原始文本**里确实是「`$$`（PID）后面跟一个括号」，只有经过 Make 展开后
才成为合法的 shell 命令替换。工具如果把 Makefile 原样喂给 shellcheck，就会误报。

**结论**：**别在 recipe 里写命令替换。** 把 shell 逻辑放进真正的 `.sh` 文件：

```text
fmt-check:
<TAB>@scripts/gate/check-fmt.sh    # recipe 只留一条命令
```

（recipe 行**必须以制表符开头** —— 这是 Make 的语法，空格会报 `missing separator`。）

好处有三：recipe 保持声明式；shell 逻辑能被 shellcheck 正经检查；引号与转义不再纠缠。

**教训**：Makefile 的 recipe 是**被 Make 预处理过的 shell**，不是 shell。
凡是「Make 展开后才合法」的写法，都会让按 shell 分析的工具误判。

---

## K-15 · 静态分析工具的告警可能停在文件的上一个版本上

**症状**：一条关于 `Makefile` 第 34 行的告警反复出现，内容是我**几分钟前就已删掉**的代码。
连续三轮编辑都没让它消失。

**原因**：工具的缓存/快照没有随编辑失效。

**结论**：先用**不依赖工具**的方式自证，再决定是否继续修：

```sh
sed -n '34p' Makefile | od -c        # 逐字节看这一行到底是什么
grep -c 'gofmt -l' Makefile          # 目标串出现几次
```

若字节级证据表明文件已经正确，就**不要为了消告警去改正确的代码** ——
那会把真实问题改成迎合工具的问题。做一次**整文件写入**（而非增量编辑）通常能强制工具重新分析。

**为什么重要**：为了一条假告警去"修"代码，是最容易把好代码改坏的方式之一。

---

## K-16 · 别把 `sed 's/^/  /'` 加的前缀一起抄进替换串

**症状**：用 python 批量替换文档里的行时，`"未匹配"` 报了 5 次，但我肉眼看着字符串完全一样。

**原因**：上一步用 `sed 's/^/  /'` 查看内容，**它给每行加了两个空格的显示前缀**。
我把那段输出复制下来当搜索串，于是搜索串带着 `  ` 前缀，而文件里的行没有。

```python
# 抄来的（错）：实际文件里没有这两个前导空格
("  ┌─ 步骤 2 · L1 ...", "...")
# 真实的
("┌─ 步骤 2 · L1 ...", "...")
```

**为什么难发现**：终端里带前缀的输出看起来是「正常缩进」，
而且短行与长行混在一起时，两格差异在视觉上被淹没。

**同类坑（同一次会话里又踩到）**：用 `python3 -c "..."`（双引号）批改文档时，
字符串里的 **反引号会被 bash 当成命令替换执行**：

```bash
python3 -c "... s.replace('…', '| ✅ 1 份（`director` 2a…') …"
#                                          ^^^^^^^^^^ bash 先执行了 director，报 command not found
# 结果：写进文档的是被吃掉后的空串 → 「| ✅ 1 份（ 2a，6 项决策未定） |」
```

**只用带引号的 heredoc**：`python3 - <<'PY' … PY`（单引号包住定界符，bash 完全不解析内容）。
本次会话里所有生效的批量文档修改都是这么做的；出错的两次都是用 `-c "…"`。

**结论**：改文档时**不要用 `sed` 的加前缀输出去拼替换串**。
要么按行号精确替换：

```python
L = open(p, encoding="utf-8").read().split("\n")
L[n-1] = new          # 行号定位，不猜缩进
```

要么先 `repr()` 打出真实内容再决定搜索串。

---

## K-17 · 请求路径上的组件不能读请求体

**症状**：把形态①（旁路镜像）的接收端逻辑照搬给形态③（反向代理）时，
业务侧收到的请求体变成空 —— 因为复制端「读观测」时把 `r.Body` 读空了。

**原因**：`edge/mirror` 处理的是流量**副本**，读 body 没有任何副作用，
所以它把 body 塞进了 `headers["x-observed-body-prefix"]`。
但代理要**继续转发**同一个请求，读掉 body 就没得转了；
若改成「读进来再回填」还要全量缓冲，大文件上传会同时拖垮延迟与内存。

**结论**：**路径上的组件只读头与路径，不碰 body。**

而且判定面契约本来就支持这个结论 —— `api/judge/v1` 的 `Observation`
**根本没有 body 字段**，是接收端自己往里塞了一个非契约的头。
契约已经给出了正确答案，是实现时多做了。

**为什么重要**：这类 bug 的表现是「POST 请求到业务那里变成空 body」，
但触发点在另一个进程里，且只在**带 body 的请求**上出现 ——
GET 全绿、健康检查全绿，很容易在联调时被忽略掉。

**顺带的边界**：`INT-22` 要求**启用误导处置**时能读出请求体。
那是阶段 2b 的事，且当前契约没有承载它的字段 —— 已登记为未决项。
「路径上不读 body」与「2b 需要读 body」是两条不同的要求，不要混为一谈。

---

## K-18 · 包的 README 描述的可能是它**没有加载**的那个文件的行为

**症状**：`@evreke/pi-grill-deck` 的 README 与它打包进来的 `grill.ts` 都说
「访谈结束后会把实施计划写到 `docs/plans/`，且 `do not write anywhere else`」。
我据此准备在本项目登记一个 `docs/plans/` 目录的效力定位。

**实际情况**：该包只加载 `index.ts` ——

```json
{ "pi": { "extensions": ["./index.ts"], "skills": ["./skills"] } }
```

而 `index.ts` **不 import `grill.ts`**（它只 import `lib.ts`）。
`grill.ts` 是**打进来但永远不会被加载**的一份上游副本，
那条「写计划」的指令在它里面 —— 于是**根本不会执行**。

**结论**：判断一个 pi 包真正做了什么，**看 `package.json` 的 `pi` 清单 + 入口文件的 import**，
不要看 README 的演示描述。三步：

```sh
node -p "require('<pkg>/package.json').pi"     # 到底加载哪些文件
grep -n "^import" <pkg>/<入口>.ts              # 入口实际引用了谁
grep -rln "要查的行为" <pkg>/*.ts              # 命中的文件是否在被加载的那条链上
```

**为什么重要**：README 常描述**上游或历史版本**的行为。
照它去配置目录、写规则、设计集成，会为一段不存在的代码建立约定 ——
而且因为「文档写了」，后面的人很难怀疑它。

**顺带记下这条**：包体里有未被加载的文件是**正常现象**（vendored 副本、备用入口、构建产物）。
它不是缺陷，但它是「README 可能对不上实际行为」的可靠信号。

---

## K-19 · "只改代码、最后一起验" 在长链路模块上会翻倍返工

**症状**：`policy` 模块（配置 → 校验 → 快照 → 判定引擎）写完之后跑单测，报的是
**误调度**类的低级错（夹具的 YAML 被 mutator 替换坏、浮点累加 `0.6+0.3=0.8999999999999999`），
不是逻辑错 —— 而且要一路翻到测试文件才知道哪儿错了。

**原因**：这条链路有 4 跳（YAML → 校验 → 快照 → `judge`），
每跳都有独立的失败模式。只看**最终**那一跳的输出，无法定位是哪一跳坏了；
`-race` 与门禁更晚才发现问题，返工成本翻倍。

**结论**：给它配一条**一条命令跑完的分层验证**，并且**每加一跳就重跑**：

```sh
make check-config   # 只验第 1–3 跳：装载 + 校验 + 打印策略摘要（不开端口）
make replay         # 验第 4 跳：规则 → 分数 → 命中信号（判定响应不回显分值，只能在这里看）
make smoke          # 验服务面：形状、三值闭集、decision_id 幂等
make dev            # 全部：配置干跑（合法 + 非法）→ 起核心 → 冒烟 → 回放 → 关核心
```

**两条附带结论**：

1. **判定响应禁止回显分值（`ST-7`）意味着「规则命中」在响应里永远看不见** ——
   必须有一条**离线回放**通道（`make replay`），否则整个模块只能靠启动日志判断生死。
2. **别写死端口**。开发机上 `19443` 实测被某个音乐播放器占着，
   `make dev` 因此直接失败。改成自动挑空闲端口后，「一键验证」才真的是一键。

---

## K-20 · 判定缓存的键不含 UA ⇒ 同一个 60s 窗口内「谁先到谁定调」

**症状**（策略面端到端验证时实测，可稳定复现）：

```console
# 同一个 IP、同一路径、不带 Cookie，先后发两次（同一个 60s 时间窗）
$ curl -A "HeadlessChrome/120"         http://<引擎>/      → MIRAGE-BODY   # 探针 → 改道
$ curl -A "Mozilla/5.0 (Macintosh)"    http://<引擎>/      → MIRAGE-BODY   # 真实用户 → **也被改道**

# 换个会话（Cookie 不同 → decision_id 不同）
$ curl -A "Mozilla/5.0" -H "Cookie: sid=normal-user-1" http://<引擎>/?u=1 → REAL-BUSINESS ✅
# 再用探针 UA + 同一个 Cookie（命中缓存，不重新判定）
$ curl -A "HeadlessChrome/120" -H "Cookie: sid=normal-user-1" http://<引擎>/?u=1 → REAL-BUSINESS ⚠️
```

**原因**：`decision_id` 按 `(来源标识, 会话, 路径, 时间窗)` 派生（`ST-10`），**不含 UA / 指纹**；
适配器的判定缓存（`AR-6` 第 2 件事）以它为键。于是同一个 60s 窗口内：

- **误调度**：探针先到 ⇒ 同 IP + 同会话 + 同路径的真实用户被改道（上例第 2 行）；
- **漏调度**：真实用户先到 ⇒ 同窗口的探针不会被改道（上例最后一行）。

**结论**：这不是实现 bug —— 它是 `ST-10` 的键与 `AR-6` 的缓存语义**共同推出**的行为，
代价落在 `guard.false_route_budget`（误调度率护栏）上，而**误调度率的验收口径（`D12`）尚未定**。
因此：**不要**在适配器侧「顺手」把 UA 拼进 `decision_id`（那会改掉已确认的 `ST-10`，
且让跨适配器缓存一致性无从谈起）。正确做法是单独一轮，在「缓存键 × 误调度预算」之间做一次明确取舍，
并把结论写成规则。现状见 [ADR-0018](../background/decisions/0018-policy-plane-pull-model.md) 的「未解决」。

## K-21 · 单独 `docker compose restart core` 会让整栈"容器都 Up 但端口不通"

**症状**：`docker compose ps` 五个容器全是 `Up`，但控制台/业务入口 `curl` 得到 HTTP 000（连接被拒）；日志里没有任何报错。

**原因**：`core` 是这套 compose 的**网络命名空间持有者**（其它服务用 `network_mode: service:core` 加入它）。
**任何让 core 换命名空间的操作**都会造成这个后果 —— 包括"看起来无辜"的 `up -d --build`：
compose 只重建**有变化**的服务，于是 core 换了新命名空间，而没被重建的兄弟服务仍留在旧的里面。
症状除了宿主端口不通（HTTP 000），还包括**容器间地址变成 `connection refused`**（例如幻境后端 / 业务源站拨不通）。

**结论**：整栈重建 —— `scripts/shen.sh restart`（等价 `docker compose ... up -d --force-recreate`）。**永远不要单独 restart core**。

---

## K-22 · 改了代码但行为/日志没变 —— `--force-recreate` 不重建镜像

**症状**：新加的日志一行都不出现、改的逻辑没生效，但容器确实重启过了。

**原因**：`docker compose up -d --force-recreate` 只**重建容器**，用的是**旧镜像**；代码改动没进镜像。

**结论**：用带构建的方式 —— `scripts/shen.sh restart`（已含 `--build`）或 `make docker-build` 后再 `up -d`。
判据：`docker images` 里 `shen-*` 的时间戳应晚于你最后一次改代码。

---

## K-23 · 查询串里的攻击"看不见"（当前已知能力缺口，别当 bug 查）

**症状**：`/download?file=../../etc/passwd`、`/search?q=union+select` 这类请求判定 **0 分 0 信号**；
但把同样的载荷放进**路径**（如 `/../../etc/passwd`）就有分。

**原因**：判定用的观测字段 `path` **不含查询串**（实测确认，见 [`../spec/config.md`](../spec/config.md) §2.4）；
规则又是按原始字符串匹配，所以 URL 编码一次（`%2e%2e%2f`）也能绕过。

**结论**：这是**已登记的能力缺口**，不是判错。缺口清单与关闭路径见
[`../ops/functional-verification.md`](../ops/functional-verification.md) §2（#1 / #2）。修复前不要指望参数型攻击被判到。

## K-24 · 重启后**第一条**请求常常"没有判定记录"（判定超时后放行）

**症状**：`scripts/shen.sh doctor` 的 ① 报"该 POST 拿到上游响应但没有判定记录"；或者你在控制台里找不到刚发的那条请求。
适配器日志里那条是 `失败=rpc error: code = DeadlineExceeded desc = context deadline exceeded`。

**原因**：判定调用有 **3ms 预算**（`AR-29`）。重启（或任何原因导致 gRPC 通道重连）后的第一条请求，
建连耗时吃掉了预算 ⇒ 判定超时 ⇒ 引擎按 `NI-3` **放行到真实业务**（业务不受影响），但**这条没有判定记录**。

**结论**：这是**设计内的失败放行**（`NI-1` 优先级高于观测完整性），不是漏判。要观测就再发一次（第二条通常命中）；
要彻底消除，需要连接预热或在通道就绪前不计时（属性能/语义取舍，未做）。
