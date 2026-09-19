package contract

// Thresholds 是决策阈值：风险分到三值的映射边界。
//
// 它是**核心自身的运行参数**，不是对外契约 —— 禁止写进 api/*.proto 的对外响应
// 或策略载荷（MD-12：决策取值与阈值禁止写进对外契约）。
//
// 阈值必须集中定义、可由配置覆盖（ST-23 / INT-24）。
type Thresholds struct {
	// Mirage：score >= Mirage 时考虑改道（route_mirage）。
	Mirage float64
	// Block：score >= Block 时考虑拦截（block）—— 仅在 block 开关打开时生效。
	Block float64
}
