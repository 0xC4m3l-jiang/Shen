package contract

// Action 是决策的取值，只有三个：放行 / 悄悄改道 / 拦截。
//
// 禁止新增第四个取值，也禁止把「看得见的处置」（挑战页、质询页、
// 验证码、维护页）塞进来 —— 那类处置会让对手察觉自己被骗，
// 与「透明改道」的目标直接冲突。
// 为什么只有三个：docs/background/decisions/0002-decision-model.md
type Action uint8

const (
	ActionOrigin Action = iota // route_origin  放行到真实业务
	ActionMirage               // route_mirage  悄悄改道到蜜罐后端
	ActionBlock                // block         拦截
)

// String 返回约定的唯一写法。
func (a Action) String() string {
	switch a {
	case ActionOrigin:
		return "route_origin"
	case ActionMirage:
		return "route_mirage"
	case ActionBlock:
		return "block"
	default:
		return "unknown"
	}
}

// Severity 是「加重强度」，与 Action 并列输出，不改变决策本身。
//
// 用途：判定出错时不能改道补救，只能对可疑会话施加不可见的压力
// （限速、让数据缺一块）。它必须独立于 Action，不得并入决策枚举。
//
// 档位只能取已登记的值，新增档位必须先实测再登记 ——
// 见 docs/design/terminology.md §4.2。当前只有 SeverityNone 一个合法值。
type Severity uint8

const (
	SeverityNone Severity = iota
)

// String 返回唯一写法。
func (s Severity) String() string {
	switch s {
	case SeverityNone:
		return "none"
	default:
		return "unknown"
	}
}

// Decision 是 director 的输出。
//
// 注意：判定取值只在这里定义一次，禁止在核心之外复制该枚举。
type Decision struct {
	DecisionID string   // 幂等键：重试复用同一值
	Action     Action   // 三值闭集
	Severity   Severity // 旁路字段
	Backend    string   // 仅 ActionMirage 时非空
	Verdict    Verdict
}
