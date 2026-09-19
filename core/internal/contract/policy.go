package contract

import (
	"net/netip"
	"time"
)

// PolicySnapshot 是 policy 模块的输出。
// 版本只增：回滚不是回退版本号，而是发布一个内容为旧版的新版本。
type PolicySnapshot struct {
	PolicyID string
	Version  uint64
	Checksum string
	GrayPct  uint8
	Payload  []byte
	// Rules 是判定规则，以数据形式随策略下发，不是编译进代码的分支。
	Rules []Rule
}

// PolicyAck 是适配器对某个策略版本的回执（`AR-13`：各层**必须**回执以便版本对账）。
//
// 没有 AdapterID 就只知道「有人应用了」而不知道「谁应用了」—— 对账会变成一笔糊涂账，
// 因此标识随回执一起上行（`api/policy/v1` 的 `PolicyAck.adapter_id`）。
// Applied=false 时 Reason 必须写明原因（校验和不匹配 / 载荷非法 / 后端建不出来）。
type PolicyAck struct {
	PolicyID   string
	Version    uint64
	AdapterID  string
	Applied    bool
	Reason     string
	ReceivedAt time.Time
}

// IsolationHit 是 isolation 模块的查询结果。
type IsolationHit struct {
	Hit       bool
	Reason    string
	ExpiresAt time.Time
}

// Whitelist 是免判定的来源集合（INT-25）。
//
// 三项**任一**命中即视为白名单 —— 内部 IP / 健康检查 / 监控探针
// **必须在引流判定前**放行，否则监控探针会被当作 Agent 处理。
type Whitelist struct {
	SourceCIDRs  []netip.Prefix
	UserAgents   []string
	PathPrefixes []string
}
