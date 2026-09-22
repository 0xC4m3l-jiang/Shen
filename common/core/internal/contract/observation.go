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
	// Query 是**已解码一次**的 URL 查询串（不含前导 `?`）：`%2e`→`.`、`%20`→空格、`+`→空格。
	//
	// 为什么要它：载荷常在查询串里（`?file=../../etc/passwd`），只看 Path 时完全不可见。
	// 为什么解码：不解码的话 `%2e%2e%2f` 与 `union%20select` 同样不可见（编码即绕过）。
	// 为什么只解一次：重复解码会把真实数据改写成另一个值（双侧解码漏洞的根因）。
	// 解码由**适配器**做（它已经是 HTTP 客户端，`url.QueryUnescape` 就在手边）；
	// 解码失败时原样传递，从不丢数据。
	Query string
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
