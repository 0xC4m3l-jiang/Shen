// Package deception 是管控台拥有的**欺骗管控数据集**（方案 B，见 docs/plan-console-deception-config.md）。
//
// 边界（三条红线）：
//   - 管控台只拥有**登记数据**（蜜罐池 / 诱饵资产 / 白名单 / 禁止欺骗路径 / 注入规则 / 服务绑定），
//     不下发判定策略（规则 / 阈值 / 灰度 / 影子永远来自核心的部署配置）；
//   - 核心定时拉取本包的投影并**终检**后原子替换；边缘仍只认核心快照（Pull/Ack 不变）；
//   - 管控台不可达或数据非法时，核心保留 last-good —— 业务不受影响。
//
// 合法枚举与核心**同名同值**（管控台禁止 import core/internal，ST-3）；核心终检兜底。
package deception

import "time"

// 合法枚举（与 common/core/internal 的登记值手工对齐）。
var (
	// DecoyKinds 对应 contract.DecoyKind 的五类（ADR-0010）。
	DecoyKinds = []string{"developer_api", "instruction_file", "mcp", "dataset", "bait"}
	// HoneypotTypes 对应 honeypot.KnownTypes 的九类。
	HoneypotTypes = []string{"ssh", "mysql", "redis", "ftp", "elasticsearch",
		"nginx-admin", "web-clone", "internal-wiki", "database"}
	// InjectKinds 对应 contract.InjectKinds 的四类。
	InjectKinds = []string{"developer_api", "instruction_file", "hidden_link", "dataset"}
)

// 规模上限（与核心 overlay 的上限一致；超限直接拒绝，避免 O(n²) 冲突检测失控）。
const (
	MaxDecoys     = 2000
	MaxHoneypots  = 500
	MaxListItems  = 5000
	MaxInjects    = 200
	MaxBindings   = 512
	MaxSnippet    = 64 << 10
	MaxHistory    = 50
	MaxProjection = 1_000_000 // ProjectionRev 必须小于它（核心版本公式：基线×1e6 + rev）
)

// Domain 是数据集的一个可写域（RBAC 按域授权）。
type Domain string

// 可写域。
const (
	DomainHoneypots Domain = "honeypots"
	DomainDecoys    Domain = "decoys"
	DomainWhitelist Domain = "whitelist"
	DomainBlacklist Domain = "blacklist"
	DomainInjects   Domain = "injects"
	DomainBindings  Domain = "bindings"
)

// Domains 是全部可写域（顺序即 UI 展示顺序）。
var Domains = []Domain{DomainHoneypots, DomainDecoys, DomainWhitelist, DomainBlacklist, DomainInjects, DomainBindings}

// Honeypot 是蜜罐池的一项（逻辑名 → 具体蜜罐实例；蜜罐本体不由本项目实现，ADR-0011）。
type Honeypot struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Addr        string `json:"addr"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description,omitempty"`
	TemplateID  string `json:"template_id,omitempty"`
}

// DecoyAsset 是一个诱饵资产（投放什么钩子、改道到哪个蜜罐）。
type DecoyAsset struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Path    string   `json:"path"`
	Hosts   []string `json:"hosts"`   // W7 归属声明：启用时 ≥1 条，禁止裸 *
	Content string   `json:"content"` // 内容模板标识（ST-22），不是页面正文
	Backend string   `json:"backend"` // → Honeypot.Name
	Enabled bool     `json:"enabled"`
	// 以下为管控台本地字段（不投影给核心）。
	TemplateID string `json:"template_id,omitempty"`
	Note       string `json:"note,omitempty"`
}

// Whitelist 是免判定来源（命中即直通，连判定都不做）。
type Whitelist struct {
	SourceCIDRs  []string `json:"source_cidrs"`
	UserAgents   []string `json:"user_agents"`
	PathPrefixes []string `json:"path_prefixes"`
}

// BlackRule 是禁止欺骗路径：命中即绝不改道（判定照常、可观测）。
type BlackRule struct {
	ID         string `json:"id"`
	PathPrefix string `json:"path_prefix"`
	Reason     string `json:"reason"`
}

// Inject 是一条改道侧响应改写规则（数组顺序即执行顺序）。
type Inject struct {
	Kind    string `json:"kind"`
	Snippet string `json:"snippet"`
	Marker  string `json:"marker"`
}

// Binding 是服务级诱饵绑定：**替换语义** —— 服务一旦绑定，就只使用绑定的诱饵集。
type Binding struct {
	ServiceID string   `json:"service_id"`
	DecoyIDs  []string `json:"decoy_ids"`
}

// Dataset 是整份欺骗管控数据集（单文件、整体版本号、乐观锁）。
type Dataset struct {
	// Version 每次保存 / 回滚 +1（乐观锁的依据；回滚也不倒退）。
	Version uint64 `json:"version"`
	// ProjectionRev 是**投影修订号**：投影内容（含服务主机带来的变化）每变一次 +1，只增不减。
	// 核心的策略版本 = 部署配置 version × 1e6 + ProjectionRev（重启不倒退，边缘对账无需特判）。
	ProjectionRev    uint64 `json:"projection_rev"`
	ProjectionDigest string `json:"projection_digest"`
	// Initialized=false 表示尚未接管（没有部署配置可供初始化，且未保存过）：
	// 集成端点返回 not_initialized，核心继续用部署配置 —— 杜绝「空数据集清空诱饵」。
	Initialized  bool      `json:"initialized"`
	SeedSource   string    `json:"seed_source,omitempty"`
	SeedChecksum string    `json:"seed_checksum,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
	UpdatedBy    string    `json:"updated_by"`
	Summary      string    `json:"summary,omitempty"` // 本版本的变更摘要（版本时间线用）

	Honeypots       []Honeypot   `json:"honeypots"`
	Decoys          []DecoyAsset `json:"decoys"`
	Whitelist       Whitelist    `json:"whitelist"`
	Blacklist       []BlackRule  `json:"blacklist"`
	Injects         []Inject     `json:"injects"`
	InjectsProvided bool         `json:"injects_provided"`
	Bindings        []Binding    `json:"bindings"`
}

// VersionMeta 是版本时间线的一行。
type VersionMeta struct {
	Version   uint64    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
	Summary   string    `json:"summary"`
}

// Clone 深拷贝（读侧返回副本，写侧在副本上改）。
func (d Dataset) Clone() Dataset {
	out := d
	out.Honeypots = append([]Honeypot(nil), d.Honeypots...)
	out.Decoys = make([]DecoyAsset, len(d.Decoys))
	for i, a := range d.Decoys {
		a.Hosts = append([]string(nil), a.Hosts...)
		out.Decoys[i] = a
	}
	out.Whitelist = Whitelist{
		SourceCIDRs:  append([]string(nil), d.Whitelist.SourceCIDRs...),
		UserAgents:   append([]string(nil), d.Whitelist.UserAgents...),
		PathPrefixes: append([]string(nil), d.Whitelist.PathPrefixes...),
	}
	out.Blacklist = append([]BlackRule(nil), d.Blacklist...)
	out.Injects = append([]Inject(nil), d.Injects...)
	out.Bindings = make([]Binding, len(d.Bindings))
	for i, b := range d.Bindings {
		b.DecoyIDs = append([]string(nil), b.DecoyIDs...)
		out.Bindings[i] = b
	}
	return out.normalizeNil()
}

// normalizeNil 把 nil 切片规范成空切片（JSON 输出 [] 而不是 null，前端不必判空）。
func (d Dataset) normalizeNil() Dataset {
	if d.Honeypots == nil {
		d.Honeypots = []Honeypot{}
	}
	if d.Decoys == nil {
		d.Decoys = []DecoyAsset{}
	}
	if d.Whitelist.SourceCIDRs == nil {
		d.Whitelist.SourceCIDRs = []string{}
	}
	if d.Whitelist.UserAgents == nil {
		d.Whitelist.UserAgents = []string{}
	}
	if d.Whitelist.PathPrefixes == nil {
		d.Whitelist.PathPrefixes = []string{}
	}
	if d.Blacklist == nil {
		d.Blacklist = []BlackRule{}
	}
	if d.Injects == nil {
		d.Injects = []Inject{}
	}
	if d.Bindings == nil {
		d.Bindings = []Binding{}
	}
	return d
}

// ServiceRef 是投影 / 校验所需的已登记服务视图（来自 registry，避免本包依赖它）。
type ServiceRef struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Hosts   []string `json:"hosts"`
	Enabled bool     `json:"enabled"`
}
