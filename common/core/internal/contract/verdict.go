// Package contract 定义核心各模块进程内共享的类型。
//
// 它只放类型与极少的纯函数，不含业务逻辑 —— 业务逻辑属于各自的模块包。
// 对外契约不在这里 —— 那是 api/*.proto（跨进程、跨语言的契约）。
package contract

// Rule 是判定规则的数据形态。
//
// 策略必须是数据，禁止编译进代码。因此规则不是 Go 分支，而是可下发的配置。
type Rule struct {
	ID     string  `json:"id"`
	Weight float64 `json:"weight"`
	Match  Match   `json:"match"`
}

// Match 是声明式匹配条件。判定引擎按 Field/Op/Value 求值，自身不含规则分支。
type Match struct {
	Field string `json:"field"` // user_agent | path | method | source_ip | tls_fingerprint
	Op    string `json:"op"`    // equals | prefix | contains
	Value string `json:"value"`
}

// Verdict 是 judge 的输出：一个风险分、命中的信号、以及可回放的证据链。
type Verdict struct {
	Score    float64
	Signals  []Signal
	Evidence []Evidence
}

// Signal 是一条判定规则的命中记录。
type Signal struct {
	ID     string
	Weight float64
	Detail string
}

// Evidence 是原始观察，供回放与审计。
type Evidence struct {
	Kind  string
	Value string
}
