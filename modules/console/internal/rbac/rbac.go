// Package rbac 是管控台的**角色—权限静态矩阵**。
//
// 为什么是静态矩阵而不是可配置：管控台只有四个角色，且「欺骗层 / 蜜罐层分离管理」是产品约定，
// 不是运营偏好。把矩阵写死在代码里，授权规则就能被表驱动测试完整钉住，
// 也不会出现「有人把只读账号配成可写」这类运行期漂移。
//
// 授权口径：**默认拒绝**。未登记的角色、未登记的权限一律返回 false。
package rbac

import "sort"

// Role 是账号的角色。
type Role string

// 四个角色（与产品约定一一对应，禁止新增第五种而不改矩阵与测试）。
const (
	Admin             Role = "admin"              // 管理员：全部权限 + 用户与审计
	DeceptionOperator Role = "deception_operator" // 欺骗运维：欺骗层视图 + 反向链接器登记
	HoneypotOperator  Role = "honeypot_operator"  // 蜜罐运维：蜜罐层视图
	Viewer            Role = "viewer"             // 只读：各层只读视图，不可写
)

// Permission 是一项 API 权限（`资源:动作`）。
type Permission string

// 权限清单。新增 API 必须先在这里登记并落进矩阵，否则路由注册即失败（见 api 包）。
const (
	OverviewRead  Permission = "overview:read"  // 总览：新流量 / 来源 / 归属地
	DeceptionRead Permission = "deception:read" // 欺骗层：判定流 / 链路 / 策略快照
	HoneypotRead  Permission = "honeypot:read"  // 蜜罐层：投递结果 / 后端
	RegistryRead  Permission = "registry:read"  // 反向链接器：查看登记与流量
	RegistryWrite Permission = "registry:write" // 反向链接器：增删改登记（**只改登记表，不下发策略**）
	AnalysisRead  Permission = "analysis:read"  // L4 分析结论
	AlertsRead    Permission = "alerts:read"    // 告警
	StreamRead    Permission = "stream:read"    // 实时事件流
	SelfManage    Permission = "self:manage"    // 改自己的口令与偏好
	UsersAdmin    Permission = "users:admin"    // 用户与角色管理
	AuditRead     Permission = "audit:read"     // 审计日志
	// 大模型分析。拆成两级：登记/改密钥/测连通是**高权限**（密钥 = 钱 + 出站通道），只给管理员；
	// 用模型分析流量会消耗 token，给各运维角色，但**只读账号不给**（只读意味着零副作用，含计费）。
	LLMUse   Permission = "llm:use"   // 选流量 → 选模型 → 对话分析；查看 token 用量
	LLMAdmin Permission = "llm:admin" // 登记 / 修改 / 删除大模型提供方、测试连通性
)

// matrix 是唯一的授权事实源。
var matrix = map[Role]map[Permission]bool{
	Admin: set(OverviewRead, DeceptionRead, HoneypotRead, RegistryRead, RegistryWrite,
		AnalysisRead, AlertsRead, StreamRead, SelfManage, UsersAdmin, AuditRead, LLMUse, LLMAdmin),
	DeceptionOperator: set(OverviewRead, DeceptionRead, RegistryRead, RegistryWrite,
		AnalysisRead, AlertsRead, StreamRead, SelfManage, LLMUse),
	HoneypotOperator: set(OverviewRead, HoneypotRead, RegistryRead,
		AnalysisRead, AlertsRead, StreamRead, SelfManage, LLMUse),
	Viewer: set(OverviewRead, DeceptionRead, HoneypotRead, RegistryRead,
		AnalysisRead, AlertsRead, StreamRead, SelfManage),
}

func set(perms ...Permission) map[Permission]bool {
	out := make(map[Permission]bool, len(perms))
	for _, p := range perms {
		out[p] = true
	}
	return out
}

// Valid 报告角色是否登记在矩阵里。
func Valid(r Role) bool {
	_, ok := matrix[r]
	return ok
}

// Can 报告角色是否拥有权限（默认拒绝）。
func Can(r Role, p Permission) bool {
	return matrix[r][p]
}

// Known 报告权限是否已登记（任一角色拥有即视为登记）。
func Known(p Permission) bool {
	for _, perms := range matrix {
		if perms[p] {
			return true
		}
	}
	return false
}

// PermissionsOf 返回角色拥有的权限（排序，便于前端与测试稳定比较）。
func PermissionsOf(r Role) []Permission {
	out := make([]Permission, 0, len(matrix[r]))
	for p := range matrix[r] {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Roles 返回全部角色（固定顺序：权限从大到小）。
func Roles() []Role {
	return []Role{Admin, DeceptionOperator, HoneypotOperator, Viewer}
}
