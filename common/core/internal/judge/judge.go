package judge

import (
	"context"
	"strings"

	"shen/common/core/internal/contract"
)

// Engine 是判定引擎。
type Engine struct {
	rules RuleSource
}

// New 构造判定引擎。rules 为 nil 时立即 panic —— 判定不能静默失效。
func New(rules RuleSource) *Engine {
	if rules == nil {
		panic("judge: RuleSource 不能为 nil")
	}
	return &Engine{rules: rules}
}

// Judge 按数据形态的规则求值，产出 Verdict。
//
// 未命中任何规则时返回零分 Verdict —— 由 director 回落「放行到真实业务」。
func (e *Engine) Judge(ctx context.Context, req contract.JudgeRequest) (contract.Verdict, error) {
	rules, err := e.rules.Rules(ctx)
	if err != nil {
		return contract.Verdict{}, err
	}

	v := contract.Verdict{}
	for _, r := range rules {
		if !match(req.Observed, r.Match) {
			continue
		}
		v.Score += r.Weight
		v.Signals = append(v.Signals, contract.Signal{
			ID:     r.ID,
			Weight: r.Weight,
		})
		v.Evidence = append(v.Evidence, contract.Evidence{
			Kind:  r.Match.Field,
			Value: req.Observed.Field(r.Match.Field),
		})
	}
	if v.Score > 1 {
		v.Score = 1
	}
	return v, nil
}

// match 是声明式求值；规则本身是数据，故此处只有少数几个比较算符。
//
// `path_prefix` 与 `prefix` 的差别只在**路径段边界**：
//
//	`prefix`      纯字符串前缀 —— `/.git` 会命中 `/.gitignore`（误伤合法静态文件）
//	`path_prefix` 段边界前缀 —— `/.git` 命中 `/.git` 与 `/.git/config`，不命中 `/.gitignore`
//
// 两者都保留：前者是既有的匹配面（改了就是行为变更），后者是“按路径段”的正确写法。
func match(o contract.Observation, m contract.Match) bool {
	got := o.Field(m.Field)
	switch m.Op {
	case "equals":
		return got == m.Value
	case "prefix":
		return strings.HasPrefix(got, m.Value)
	case "path_prefix":
		return contract.PathSegmentPrefix(got, m.Value)
	case "contains":
		return strings.Contains(got, m.Value)
	default:
		return false
	}
}

// 路径段边界的判定在 `contract.PathSegmentPrefix`（规则匹配与诱饵路由共用一份定义）。

var _ Judge = (*Engine)(nil)
