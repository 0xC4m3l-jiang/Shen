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
		return pathSegmentPrefix(got, m.Value)
	case "contains":
		return strings.Contains(got, m.Value)
	default:
		return false
	}
}

// pathSegmentPrefix 判断 got 是否位于路径段 m 之下：`got == m` 或 `got` 以 `m + "/"` 开头。
//
// 尾斜杠要归一：`/admin/` 与 `/admin` 是同一段（否则“带尾斜杠的配置值”会静默永不命中）。
// 值为空时一律不命中 —— 空路径前缀等于“什么都命中”，那不是规则，是漏洞。
func pathSegmentPrefix(got, m string) bool {
	if m == "" {
		return false
	}
	seg := strings.TrimSuffix(m, "/")
	if seg == "" { // 值就是 "/"：只命中根
		return got == "/" || got == ""
	}
	return got == seg || strings.HasPrefix(got, seg+"/")
}

var _ Judge = (*Engine)(nil)
