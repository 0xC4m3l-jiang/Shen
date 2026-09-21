package contract

// CoreSnapshot 是核心当前生效状态的**只读摘要** —— 控制台「配置」页的数据源。
//
// 为什么单独一个类型而不是直接回 proto（`MD-5`）：进程内共享的类型收敛到 `contract`，
// proto 只出现在 `control` 的接缝处。这样将来换读侧协议、或加第二个消费方（如诊断脚本），
// 都不必动提供方。
//
// **字段取舍的规矩**（见 `docs/spec/console-api.md` §2.1）：
//
//  1. 已经允许离开核心的（策略版本 / 校验和 / AI 配置 —— 它们本就在策略载荷里）；
//  2. 比前者更弱的描述性信息（白名单**条数** —— 载荷里本来就有全量 CIDR 列表）。
//
// 因此**不含**：阈值 / 灰度 / 误调度预算 / 影子模式。它们是核心运行参数
// （`thresholds.go` 明确禁止写进 `api/*.proto` 的对外响应；`ST-23` 只要求集中定义与可配置）。
type CoreSnapshot struct {
	Policy PolicyState
	AI     AIState
}

// PolicyState 是策略面当前生效的状态（`AR-13` / `ST-8`）。
type PolicyState struct {
	PolicyID  string
	Version   uint64
	Checksum  string
	RuleCount int
	// WhitelistCount 是白名单**条数**（不摊开内容：那是接入方的资产面）。
	WhitelistCount int
}

// AIState 是 AI 能力当前的装载状态（`ADR-0023` / `ADR-0026`）。
type AIState struct {
	Enabled        bool
	Kinds          []string
	Model          string
	ManifestPath   string
	Variants       int
	RotateCooldown string
	// ManifestLoaded=false 且 Enabled=true ⇒「开关开了但没有内容」——
	// 这是最容易被漏掉的一种配置错误，控制台要能一眼看出来。
	ManifestLoaded bool
	// ManifestVersion / Resources / Contents 来自**已装载**的清单（未装载时为零值）。
	ManifestVersion   uint64
	ManifestResources int
	ManifestContents  int
}
