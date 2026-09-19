// Package injection 是 L1 的**投毒改写 / 假路径 / 蜜饵注入**执行点。
//
// 它在请求路径上，但**只做改写这一件事**：不做判定、不做决策、不做 LLM 推理、
// 不维护会话状态、不写核心状态（AR-7 / MD-9）。
//
// 改写**只能**作用于蜜罐侧的响应（INT-8）—— 业务侧响应一律原样透传。
// 它被适配器引用，**不独立部署**（ST-5）。
//
// 本包位于 edge/，按 ST-3 **禁止** import core/internal/：类型自带，不跨层取核心内部类型。
package injection

// Rule 是一条注入规则（**数据**，随配置下发，不是编译进代码的分支 —— ST-24）。
type Rule struct {
	Kind    string // developer_api | instruction_file | hidden_link | dataset
	Snippet string // 注入片段
	Marker  string // 注入位置标记；为空时用 </body>
}

// Injector 是本模块对外的唯一契约。
type Injector interface {
	// Inject 把诱饵注入响应体。
	//
	// 返回 (改写后的响应体, 是否真的改写了)。
	// 任何无法注入的情形（非 HTML、找不到标记、无规则）都**原样返回**，绝不阻断。
	Inject(contentType string, body []byte) ([]byte, bool)
}
