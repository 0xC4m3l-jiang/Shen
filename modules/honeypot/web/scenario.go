package web

import (
	"fmt"
	"hash/fnv"
	"time"
)

// 本文件是**场景包**：一套自洽的合成数据（虚构组织 / 版本 / 账号 / 配置 / 审计），确定性生成。
//
// 「自洽」是硬要求（方案 C04）：同一场景里**页面 → API → 数据**必须指同一批对象 ——
// 用户列表里出现的 ID，详情页与 API 里必须存在；分页的 total 必须与条目数相符。
// 一眼就能看出矛盾的假数据比没有假数据更糟。
//
// 所有值都是**虚构素材**（`.example` 域、占位密钥、虚构人名），不含真实密钥与个人信息（SB-3 / SB-4）。

// LoginOutcome 是登录的处理结果（场景级固定，便于两种语义都能被演示与断言）。
type LoginOutcome string

const (
	// LoginFails 表示登录**总是失败**（合成错误页）——「失败语义」那一档。
	LoginFails LoginOutcome = "fail"
	// LoginDemo 表示登录**总是进入合成演示会话**（发一个诱饵专用 cookie）——「合成演示」那一档。
	LoginDemo LoginOutcome = "demo"
)

// Scenario 是一套合成场景包。
type Scenario struct {
	ID      string       // 场景标识（配置里选）
	Org     string       // 虚构组织
	Product string       // 产品与版本
	Host    string       // 合成主机名（`.example`：保留域，永不解析到真实主机）
	Outcome LoginOutcome // 登录语义

	Users  []User
	Config []ConfigItem
	Audit  []AuditEvent
	// PageSize 是默认分页大小；MaxPageSize 是配额上限（超过就夹到这个值，不报错）。
	PageSize    int
	MaxPageSize int
}

// User 是一个合成账号（虚构人名，无真实个人信息）。
type User struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Enabled bool   `json:"enabled"`
	// LastSeen 是场景内固定时间戳（不取系统时钟：内容确定性优先）。
	LastSeen string `json:"last_seen"`
}

// ConfigItem 是一条合成配置（值一律是**占位符**，不是可用凭证）。
type ConfigItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// AuditEvent 是一条合成审计记录（虚构动作与对象，指向场景内的账号）。
type AuditEvent struct {
	At     string `json:"at"`
	Actor  string `json:"actor"` // 场景内账号 ID
	Action string `json:"action"`
	Target string `json:"target"`
}

// ScenarioFor 返回内置场景（未知 id 回落到默认场景 —— 诱饵后端不该因为配置写错就不可用）。
//
// 确定性：所有内容由 `id` 派生（`fnv` 播种），不读时钟、不用随机数 ⇒ 同场景恒同数据。
func ScenarioFor(id string) Scenario {
	if id == "atlas" || id == "" {
		return atlasScenario()
	}
	return atlasScenario()
}

// defaultScenarioID 是内置场景的标识。
const defaultScenarioID = "atlas"

// atlasScenario 是内置的「Atlas 控制台」场景：一家虚构机器人公司的内部管理台。
func atlasScenario() Scenario {
	const (
		org  = "Northwind Robotics"
		host = "atlas.northwind.example"
	)
	seed := seedOf(defaultScenarioID)
	users := []User{
		{ID: id("u", seed, 1), Name: "A. Vance", Role: "owner", Enabled: true, LastSeen: at(0)},
		{ID: id("u", seed, 2), Name: "M. Okonkwo", Role: "admin", Enabled: true, LastSeen: at(35)},
		{ID: id("u", seed, 3), Name: "L. Perez", Role: "operator", Enabled: true, LastSeen: at(120)},
		{ID: id("u", seed, 4), Name: "R. Sato", Role: "auditor", Enabled: false, LastSeen: at(1440)},
	}
	cfg := []ConfigItem{
		{Key: "deployment.region", Value: "eu-central-1"},
		{Key: "console.version", Value: "4.7.2+build.2199"},
		{Key: "database.host", Value: host},
		{Key: "database.name", Value: "atlas_prod"},
		{Key: "storage.bucket", Value: "northwind-atlas-artifacts"},
		{Key: "integrations.webhook", Value: "https://hooks.northwind.example/atlas"},
		// ⚠️ 占位符，不是可用凭证：任何真实密钥**禁止**出现在场景包里（C04 的验收判据）。
		{Key: "database.password", Value: "SCENARIO-PLACEHOLDER-NOT-A-CREDENTIAL"},
		{Key: "api.token", Value: "SCENARIO-PLACEHOLDER-NOT-A-CREDENTIAL"},
	}
	audit := []AuditEvent{
		{At: at(2), Actor: users[1].ID, Action: "config.update", Target: "storage.bucket"},
		{At: at(17), Actor: users[1].ID, Action: "user.disable", Target: users[3].ID},
		{At: at(41), Actor: users[2].ID, Action: "deployment.create", Target: "atlas-worker-7"},
		{At: at(96), Actor: users[0].ID, Action: "policy.update", Target: "auth.mfa.required"},
		{At: at(180), Actor: users[2].ID, Action: "deployment.rollback", Target: "atlas-worker-7"},
		{At: at(305), Actor: users[1].ID, Action: "config.read", Target: "database.host"},
		{At: at(420), Actor: users[3].ID, Action: "audit.export", Target: "2026-09"},
	}
	return Scenario{
		ID:          defaultScenarioID,
		Org:         org,
		Product:     "Atlas Console",
		Host:        host,
		Outcome:     LoginDemo,
		Users:       users,
		Config:      cfg,
		Audit:       audit,
		PageSize:    3,
		MaxPageSize: 20,
	}
}

// seedOf 由场景 id 派生稳定种子（同 id 恒同种子）。
func seedOf(id string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return h.Sum32()
}

// id 生成场景内稳定对象 ID（形似真实 ID，但完全由场景种子决定）。
func id(prefix string, seed uint32, n int) string {
	return fmt.Sprintf("%s-%08x%02d", prefix, seed, n)
}

// baseUnix 是场景内的**固定基准时间**（2026-09-14T09:30:00Z）：内容不取系统时钟（可复现），
// 时间戳一律是「基准 + 偏移分钟」（负数即"过去"）。改它等于改整场景的时间线。
const baseUnix = 1789378200

// at 返回「基准 + n 分钟」的 RFC3339 时间戳（确定性：不读系统时钟）。
func at(minutes int) string {
	return time.Unix(baseUnix+int64(minutes)*60, 0).UTC().Format(time.RFC3339)
}

// Page 是分页结果（HTML 与 API **共用**同一个分页函数 ⇒ 页面与 API 不会各算一套）。
type Page[T any] struct {
	Items    []T  `json:"items"`
	Page     int  `json:"page"`
	PageSize int  `json:"page_size"`
	Total    int  `json:"total"`
	NextPage *int `json:"next_page"` // nil = 没有下一页（不用 0 表示"没有"，避免歧义）
}

// paginate 按 (page, pageSize) 切片；pageSize 超过场景上限就**夹到上限**（配额而非报错）。
func paginate[T any](items []T, page, pageSize, maxPageSize int) Page[T] {
	if pageSize <= 0 {
		pageSize = 3
	}
	if maxPageSize > 0 && pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	total := len(items)
	if page < 1 {
		page = 1
	}
	start := (page - 1) * pageSize
	out := Page[T]{Page: page, PageSize: pageSize, Total: total}
	if start >= total {
		return out
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	out.Items = append(out.Items, items[start:end]...)
	if end < total {
		next := page + 1
		out.NextPage = &next
	}
	return out
}

// 编译期断言 `Handler` 满足 http.Handler 放在 handler.go（与实现同文件，读起来更顺）。
