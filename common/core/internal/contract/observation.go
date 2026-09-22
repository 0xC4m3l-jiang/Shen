package contract

import (
	"net/netip"
	"path"
)

// JudgeRequest 是适配器经接缝 S1（适配器 ↔ 核心的 gRPC）送入核心的判定请求。
type JudgeRequest struct {
	DecisionID string      // 由适配器派生，重试复用同一值
	Session    SessionKey  // 已提取的会话身份
	Observed   Observation // 本次观察
}

// Observation 是一次请求的首跳可用信息。
//
// 会话第一跳的决策就是终局（中途改道会被立刻识破），
// 因此判定只能靠这些首跳信息来做。
type Observation struct {
	SourceIP       netip.Addr
	UserAgent      string
	Method         string
	Path           string
	TLSFingerprint string
	Headers        map[string]string
	// Query 是查询串的**规范化匹配视图**（不含前导 `?`）：适配器**反复解码到不动点**（上限 3 轮）。
	//
	// 为什么要它：载荷常在查询串里（`?file=../../etc/passwd`），只看 Path 时完全不可见。
	// 为什么解到不动点：只解一轮时「二次编码」（`%252e%252e`）仍可绕过（实测缺口）。
	// 为什么有上限：每轮都是对攻击者可控输入的线性工作；上限把成本钉死，
	// 代价是三重及以上编码仍可能绕过（已登记，见 `docs/spec/config.md` §2.4）。
	// 它**只用于匹配**：转发给上游的仍是原始请求行。
	Query string
	// QueryRaw 是客户端**原样发来**的查询串（未解码），供审计与「按编码形态匹配」的规则使用（`AR-31`）。
	QueryRaw string
}

// Field 取用于规则匹配的字段值；未知字段返回空串。
//
// `path_norm` 是**派生字段**（从 `path` 现算，不是独立存储的一份）：归一化后用于匹配。
// 为什么派生而不新增一套 wire 字段：定义只在一处（这里），两个适配器不可能各写一份而漂移；
// 它是纯函数，确定性（`AR-30`），所以「现算」不引入不确定性。
// 为什么不合进 `path`：那会改变**所有**既有规则的匹配面（行为变更）；派生字段让使用者自己选。
func (o Observation) Field(name string) string {
	switch name {
	case "user_agent":
		return o.UserAgent
	case "path":
		return o.Path
	case "path_norm":
		return NormalizePath(o.Path)
	case "query":
		return o.Query
	case "query_raw":
		return o.QueryRaw
	case "method":
		return o.Method
	case "source_ip":
		return o.SourceIP.String()
	case "tls_fingerprint":
		return o.TLSFingerprint
	default:
		return ""
	}
}

// NormalizePath 把路径归一化成**匹配视图**：去点段（`.` / `..`）、合并重复斜杠、去掉尾斜杠。
//
// 用 `path.Clean` 而不是自写循环：它是 Go 标准库的既定语义，且能被同样的单测盯住。
// 与 `query` 的解码同一个道理：**恰好一次**归一化 —— 不重复“清洗”（把 `/a/../../b` 反复解会得到不同结果）。
//
// 边界：空路径返回空串（不凭空造一个 `/`）；不以 `./` 开头的相对路径会归一化成相对形式（`path.Clean` 的行为），
// 判定面收到的一律是绝对路径（HTTP 请求行就是这么写的），所以这不会在真实流量里发生。
func NormalizePath(p string) string {
	if p == "" {
		return ""
	}
	return path.Clean(p)
}
