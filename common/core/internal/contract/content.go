package contract

import (
	"strconv"
	"time"
)

// AI 能力服务与欺骗内容的**进程内共享类型**（`MD-5`：跨模块共享的类型收敛到一处）。
//
// 跨进程契约在 [`../../docs/spec/ai-contract.md`](../../docs/spec/ai-contract.md) 与
// [`../../docs/spec/policy-payload.md`](../../docs/spec/policy-payload.md)：本文件只放
// 「核心内部两个模块之间要传的形状」（`cmd/core` 装配 · `policy` 装载与投影 · `store` 落库）。
//
// 依据：`AR-33`（生成必须经护栏出口）· [ADR-0023](../../../docs/background/decisions/0023-deception-content-injection.md)。

// AIConfig 是配置文档 `ai:` 段的解析结果（字段与约束见 `docs/spec/config.md` §2.13）。
//
// **整段可选**：不写该段时为零值 + 默认值（`Enabled=false`），行为与今天逐字节一致。
type AIConfig struct {
	// Enabled 是能力级总开关。它决定策略载荷里的 `inject_enabled`（`ADR-0023` 决定 4）。
	Enabled bool
	// Kinds 是启用的任务种类。阶段 A 只校验形状（生成侧按注册表拒绝未登记的 kind）。
	Kinds []string
	// Model 是模型后端标识。空串在阶段 A 合法（确定性模板生成器不调模型）。
	Model string
	// ManifestPath 是内容清单文件路径；空 = 无内容。
	ManifestPath string
	// Content 是内容册参数（变体数、轮换冷却）。
	Content AIContentConfig
}

// AIContentConfig 是 `ai.content` 段。
type AIContentConfig struct {
	// Variants 是变体数 N；**必须**与清单文件的 `variants` 一致（不一致即拒绝装载）。
	Variants int
	// RotateCooldown 是轮换冷却；阶段 A 只解析与校验（轮换接线在阶段 B）。
	RotateCooldown time.Duration
}

// ContentBody 是清单里的一条内容（一个变体槽位）。
type ContentBody struct {
	VariantID int    `json:"variant_id"`
	ContentID string `json:"content_id"`
	Checksum  string `json:"checksum"`
	Body      string `json:"body"`
	Marker    string `json:"marker,omitempty"`
}

// ServiceConfig 是一个**接入单元**（`SHEN-M4-03`）：服务名 + Host 声明 + 幻境后端。
//
// 它是「服务」这条线的唯一来源：内容按它分组、改道按它选池、审计按它归属。
// 服务是**接入单元**，画像是**生成单元** —— 两者不得混用（一个服务可以换画像）。
type ServiceConfig struct {
	ID            string   // 服务标识（唯一）
	Hosts         []string // Host 声明（可含最左标签通配 `*.x`）
	MirageBackend string   // 该服务改道优先使用的幻境后端逻辑名；空 = 用默认池
}

// ContentEntry 是「一个资源 × 它的各变体」。
type ContentEntry struct {
	Resource string `json:"resource"`
	// Service 是这条内容所属的**接入单元**（`SHEN-M4-01`）。空 = 未声明（对所有服务生效）。
	// 与 `ProfileID` 是两件事：服务是接入单元，画像是生成单元。
	Service   string        `json:"service,omitempty"`
	ProfileID string        `json:"profile_id"`
	Bodies    []ContentBody `json:"bodies"`
}

// ContentManifest 是内容清单（核心内部表示）。
//
// 装载后：内容体进 `store.ContentStore`（键见 `ContentKey`），元数据留在本结构供投影使用。
type ContentManifest struct {
	Version  uint64         `json:"version"`
	Selector string         `json:"selector"`
	Variants int            `json:"variants"`
	Entries  []ContentEntry `json:"entries"`
}

// ContentKey 是内容库的键（一致性键）。
//
// **会话不进键**：会话只决定**选哪个变体**（`variant = hash(会话) mod N`），
// 这是 `AR-30`（同会话同资源同答案）与多态（`ADR-0016`）的划界。
// 三处必须一致：生成侧（Python 的 `content_key`）· 核心装载（本函数）· 文档 §4。
func ContentKey(profileID, resource string, variant int, version uint64) string {
	return "content:" + profileID + ":" + resource + ":" +
		strconv.Itoa(variant) + ":" + strconv.FormatUint(version, 10)
}
