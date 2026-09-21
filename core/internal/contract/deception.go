package contract

// 本文件是「欺骗面」相关的进程内共享类型：诱饵资产、幻境后端、伪造响应。
// 它们跨模块使用（decoy / honeypot / responder / edge-injection），
// 因此按 MD-5 收敛到 contract，禁止各自定义同名类型。

// DecoyKind 是诱饵面的形态（五类，见 ADR-0010）。
type DecoyKind uint8

const (
	DecoyDeveloperAPI    DecoyKind = iota // ① 功能性伪装：开发者 API（注册 → 心跳）
	DecoyInstructionFile                  // ② 指令文件蜜饵（AGENTS.md / CLAUDE.md）
	DecoyMCP                              // ③ MCP 诱饵（JSON-RPC 工具服务）
	DecoyDataset                          // ④ 消耗战数据集（无限分页假数据）
	DecoyBait                             // ⑤ 三类蜜饵 + SSRF 蜜饵
)

// String 返回约定的唯一写法。
func (k DecoyKind) String() string {
	switch k {
	case DecoyDeveloperAPI:
		return "developer_api"
	case DecoyInstructionFile:
		return "instruction_file"
	case DecoyMCP:
		return "mcp"
	case DecoyDataset:
		return "dataset"
	case DecoyBait:
		return "bait"
	default:
		return "unknown"
	}
}

// InjectRule 是「响应改写规则」：注入到**改道侧** HTML 响应的片段。
//
// 它是**数据**（随配置与策略下发，`ST-24`），不是编译进代码的分支；
// 它跨模块使用（`policy` 装载 → 下发给适配器 → `edge-injection` 执行），因此按 `MD-5` 收敛到 `contract`。
//
// 契约见 [`docs/spec/policy-payload.md`] 的 `inject_rules[]`。两端（核心 `policy.edgeDoc`
// 与适配器 `deception.proxy.edgePolicy`）**手工对齐**，因为适配器禁止 import `core/internal/`（`ST-3`）。
type InjectRule struct {
	// Kind 是分类（见 InjectKinds），可为空串（未分类）。分类只用于组织与审计，不改变注入行为。
	Kind string
	// Snippet 是注入片段（非空）。
	Snippet string
	// Marker 是插入位置标记；为空时执行方用 `</body>`。
	Marker string
}

// InjectKinds 是登记过的注入分类（唯一事实源：[`docs/spec/policy-payload.md`]）。
//
// 分类只描述「钩子是哪种诱饵形态」，**不参与注入行为** —— 因此新增分类不需要改执行代码。
var InjectKinds = []string{"developer_api", "instruction_file", "hidden_link", "dataset"}

// DecoyAsset 是一个诱饵资产的**定义**。
//
// 它是数据（可配置、可持久化），不是行为 —— 内容生成由 responder 负责，
// 注入执行由 edge-injection 负责（职责分离，见 ADR-0010）。
type DecoyAsset struct {
	ID      string    // 唯一标识
	Kind    DecoyKind // 五类之一
	Path    string    // 触发路径（前缀匹配）
	Content string    // 内容模板标识（模板随代码分发，ST-22）
	Enabled bool
}

// HoneypotBackend 是幻境后端池的一个条目：逻辑名 → 具体蜜罐实例。
//
// director 在 route_mirage 中给出的就是这里的 Name（逻辑名）。
// 具体蜜罐**不由本项目实现**（ADR-0011），只是被登记与解析。
type HoneypotBackend struct {
	Name    string // 逻辑名（route_mirage 的 Backend 字段）
	Type    string // 蜜罐类型：ssh / mysql / redis / ftp / …（见 docs/modules/honeypot.md §1）
	Addr    string // 后端地址
	Enabled bool
	Healthy bool // 运行期健康；由装配层或健康检查更新
}

// RespondRequest 是 responder 的输入。
//
// 注意它**不含 decoy 资产的具体字段**（除 ID/Kind）：responder 只按
// (SessionID, Resource) 与资产标识生成响应，不解析资产定义 —— 保持接口解耦。
type RespondRequest struct {
	SessionID string // 会话键（一致性种子的一半）
	Resource  string // 资源键：方法 + 路径 + query（一致性种子的另一半）
	AssetID   string // 命中的诱饵资产标识（可为空 = 无资产命中）
	Kind      DecoyKind
}

// RespondOutput 是 responder 的输出：一个伪造响应。
//
// 一致性不变量（AR-30）：同 (SessionID, Resource) 必得同一 Response。
type RespondOutput struct {
	Status  int
	Headers map[string]string
	Body    []byte
}
