package control

import (
	"context"

	"shen/core/internal/contract"
	"shen/core/internal/judge"
)

// ShadowDecider 是影子模式下的决策实现。
//
// 首次上线必须运行在影子模式 —— 只观测、不处置、不回写。
// 因此它照算判定（用于阈值校准），但永远输出 route_origin，绝不改道、绝不拦截。
//
// 阶段 2（接管后）由 director 模块替换本实现，做真实三值决策与后端选择。
type ShadowDecider struct {
	judge judge.Judge
}

// NewShadowDecider 构造影子决策器。
func NewShadowDecider(j judge.Judge) *ShadowDecider {
	if j == nil {
		panic("control: judge.Judge 不能为 nil")
	}
	return &ShadowDecider{judge: j}
}

// Decide 算判定但不处置。
func (s *ShadowDecider) Decide(ctx context.Context, req contract.JudgeRequest) (contract.Decision, error) {
	v, err := s.judge.Judge(ctx, req)
	if err != nil {
		return contract.Decision{}, err
	}
	return contract.Decision{
		DecisionID: req.DecisionID,
		Action:     contract.ActionOrigin, // 影子模式：永不改道、永不拦截
		Severity:   contract.SeverityNone,
		Verdict:    v,
	}, nil
}

var _ Decider = (*ShadowDecider)(nil)
