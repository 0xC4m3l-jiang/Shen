# L3 · 网络欺骗层（`netpolicy`）

| 项 | 值 |
| --- | --- |
| 所属层 | L3 |
| 语言 | **声明式 + eBPF**（复用 Cilium / Tetragon，**不自研** eBPF 底座 —— 复用三原则①） |
| 阶段 | **3** |
| 模块文档 | [`../../docs/modules/netpolicy.md`](../../../docs/modules/netpolicy.md) |
| 产物 | `config/` 三份声明式模板（**无源码**） |

## 它是什么

三件事，全部以**声明式产物**交付（本项目不写数据面代码）：

| 能力 | 产物 | 由谁执行 |
| --- | --- | --- |
| **微隔离**：限制幻网内东西向流量 | [`config/microsegmentation.example.yaml`](config/microsegmentation.example.yaml) | Cilium（`CiliumNetworkPolicy`） |
| **假拓扑**：构造看起来真实的内网结构（假主机 / 假服务） | [`config/fake-topology.example.yaml`](config/fake-topology.example.yaml) | Cilium / k8s Service |
| **运行时检测**：基于 eBPF 的行为检测与阻断 | [`config/runtime-detect.example.yaml`](config/runtime-detect.example.yaml) | Tetragon（`TracingPolicy`） |

## 边界（不做的事）

- **不做判定**（`AR-2`）· **不做决策**（`MD-12`）—— 本层执行的是 `policy` 下发的声明式规则；
- **不自研 eBPF 底座**（复用 Cilium / Tetragon）；
- **禁止**向被接入业务之外的任何主机投递载荷或发起连接（`SB-5`）；
- **禁止**把重逻辑（LLM / 蜜罐仿真）塞进本层所在的部署单元（`ST-16`）。

## 事件回流

运行时检测事件应经适配器上报到遥测面（`api/telemetry/v1`），以便与控制台看到的流量对齐。
本层不直接写存储（`MD-20`）。**当前为文档级约定**：接真实 Cilium / Tetragon 时按该约定接。
