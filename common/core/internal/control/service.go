package control

import (
	"context"
	"log"
	"net/netip"
	"time"

	judgev1 "shen/common/api/judge/v1"
	"shen/common/core/internal/contract"
	"shen/common/core/internal/session"
)

// JudgeService 实现 DeceptionJudge 服务。
type JudgeService struct {
	judgev1.UnimplementedDeceptionJudgeServer
	decider   Decider
	session   session.Session
	breaker   *Breaker         // 可空；为空时不熔断
	isolation IsolationChecker // 可空；为空时不查隔离
	recorder  DecisionRecorder // 可空；为空时不记录（观测面写侧）
	masker    SessionMasker    // 可空；为空时用 session 默认面具器（**始终脱敏**）
	now       func() time.Time // 可注入时钟（MD-6：时间由调用方注入，便于测试与回放）
}

// IsolationChecker 是本服务面对隔离查询的依赖。
//
// 接口由消费方（control）定义，故 isolation 不 import control —— 依赖方向单向。
type IsolationChecker interface {
	Check(ctx context.Context, key contract.SessionKey) (contract.IsolationHit, error)
}

// JudgeOption 是服务面的可选装配项。
type JudgeOption func(*JudgeService)

// WithBreaker 给服务面挂上熔断器（NI-10）。
func WithBreaker(b *Breaker) JudgeOption {
	return func(s *JudgeService) { s.breaker = b }
}

// WithIsolation 给服务面挂上隔离短路（命中则不再调用决策层）。
func WithIsolation(i IsolationChecker) JudgeOption {
	return func(s *JudgeService) { s.isolation = i }
}

// WithDecisionRecorder 给服务面挂上「记录一次判定」（观测面写侧：控制台看流动）。
//
// 记录是**尽力而为**的：失败只记日志，绝不影响判定响应（`NI-1`）。
func WithDecisionRecorder(r DecisionRecorder) JudgeOption {
	return func(s *JudgeService) { s.recorder = r }
}

// WithClock 注入时钟（默认 time.Now）。时间必须由调用方注入，才可测、可回放（MD-6）。
func WithClock(now func() time.Time) JudgeOption {
	return func(s *JudgeService) { s.now = now }
}

// WithSessionMasker 换掉默认的会话面具器（默认 `session.NewMasker("")`）。
//
// 生产部署应传入自己的密钥（`SHEN_SESSION_MASK_KEY`）；不传也不会泄露原值 ——
// 默认实现同样脱敏，只是面具用公开的默认密钥。
func WithSessionMasker(m SessionMasker) JudgeOption {
	return func(s *JudgeService) {
		if m != nil {
			s.masker = m
		}
	}
}

// NewJudgeService 构造服务端。decider 与 sess 任一为 nil 时 panic。
//
// 会话身份由核心自己提取，不信任调用方传来的值。
func NewJudgeService(d Decider, sess session.Session, opts ...JudgeOption) *JudgeService {
	if d == nil {
		panic("control: Decider 不能为 nil")
	}
	if sess == nil {
		panic("control: session.Session 不能为 nil")
	}
	s := &JudgeService{decider: d, session: sess, masker: session.NewMasker(""), now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Judge 把 proto 请求映射为内部类型，交决策层，再把决策映射回去。
//
// 返回体只含 action / severity / backend —— 不含风险分值、规则名与证据。
//
// NI-10：熔断打开时**不调决策层**，直接返回纯放行。
func (s *JudgeService) Judge(ctx context.Context, in *judgev1.JudgeRequest) (*judgev1.JudgeResponse, error) {
	if s.breaker != nil && !s.breaker.Allow() {
		return passthroughResponse(), nil
	}
	resp, err := s.decide(ctx, in)
	if s.breaker != nil {
		s.breaker.Record(err)
	}
	return resp, err
}

// decide 是真正的决策路径（熔断与隔离短路不在此处）。
func (s *JudgeService) decide(ctx context.Context, in *judgev1.JudgeRequest) (*judgev1.JudgeResponse, error) {
	req, err := s.toContract(ctx, in)
	if err != nil {
		return nil, err
	}
	// 隔离短路：命中则不再调用决策层。
	//
	// 客户端**不可见** —— 返回的仍是「放行」形状，只是不再问核心（与 challenge 不同，见 ADR-0002）。
	// 存储读不到时 fail-open：继续走判定，**禁止**因此阻断业务（NI-10）。
	if s.isolation != nil {
		if hit, ierr := s.isolation.Check(ctx, req.Session); ierr == nil && hit.Hit {
			return passthroughResponse(), nil
		}
	}
	d, err := s.decider.Decide(ctx, req)
	if err != nil {
		return nil, err
	}
	s.record(ctx, req, d)
	return toProto(d), nil
}

// record 把这次判定记进观测面（尽力而为：失败不影响响应，NI-1）。
func (s *JudgeService) record(ctx context.Context, req contract.JudgeRequest, d contract.Decision) {
	if s.recorder == nil {
		return
	}
	signals := make([]string, 0, len(d.Verdict.Signals))
	for _, sig := range d.Verdict.Signals {
		signals = append(signals, sig.ID)
	}
	sourceIP := ""
	if req.Observed.SourceIP.IsValid() { // 未识别来源时留空，别写成 "<nil>"
		sourceIP = req.Observed.SourceIP.String()
	}
	rec := DecisionRecord{
		DecisionID: d.DecisionID,
		SourceIP:   sourceIP,
		Method:     req.Observed.Method,
		Path:       req.Observed.Path,
		Query:      req.Observed.Query,
		UserAgent:  req.Observed.UserAgent,
		SessionID:  s.mask(req.Session.ID),
		Action:     d.Action.String(),
		Severity:   d.Severity.String(),
		Backend:    d.Backend,
		Score:      d.Verdict.Score,
		Signals:    signals,
		At:         s.now(),
	}
	if err := s.recorder.Record(ctx, rec); err != nil {
		log.Printf("control: 判定记录失败（不影响响应）：%v", err)
	}
}

// mask 把会话身份值换成面具。masker 为空时**不落原值**（宁可留空，也不把身份泄进观测面）。
func (s *JudgeService) mask(id string) string {
	if s.masker == nil {
		return ""
	}
	return s.masker.Mask(id)
}

// passthroughResponse 是熔断期间的纯放行响应。
func passthroughResponse() *judgev1.JudgeResponse {
	return &judgev1.JudgeResponse{
		Action:   judgev1.Action_ACTION_ORIGIN,
		Severity: judgev1.Severity_SEVERITY_NONE,
	}
}

// toContract 把 proto 请求转成内部类型，并在此提取会话身份。
func (s *JudgeService) toContract(ctx context.Context, in *judgev1.JudgeRequest) (contract.JudgeRequest, error) {
	req := fromProto(in)
	key, err := s.session.Key(ctx, req.Observed)
	if err != nil {
		return contract.JudgeRequest{}, err
	}
	req.Session = key
	return req, nil
}

func fromProto(in *judgev1.JudgeRequest) contract.JudgeRequest {
	obs := in.GetObserved()
	req := contract.JudgeRequest{DecisionID: in.GetDecisionId()}
	if obs == nil {
		return req
	}
	// 源 IP 解析失败时留零值：判定侧按「未识别」处理，最终回落为放行
	if a, err := netip.ParseAddr(obs.GetSourceIp()); err == nil {
		req.Observed.SourceIP = a
	}
	req.Observed.UserAgent = obs.GetUserAgent()
	req.Observed.Method = obs.GetMethod()
	req.Observed.Path = obs.GetPath()
	req.Observed.Query = obs.GetQuery()
	req.Observed.TLSFingerprint = obs.GetTlsFingerprint()
	req.Observed.Headers = obs.GetHeaders()
	return req
}

func toProto(d contract.Decision) *judgev1.JudgeResponse {
	return &judgev1.JudgeResponse{
		Action:   toProtoAction(d.Action),
		Severity: toProtoSeverity(d.Severity),
		Backend:  d.Backend,
	}
}

func toProtoAction(a contract.Action) judgev1.Action {
	switch a {
	case contract.ActionOrigin:
		return judgev1.Action_ACTION_ORIGIN
	case contract.ActionMirage:
		return judgev1.Action_ACTION_MIRAGE
	case contract.ActionBlock:
		return judgev1.Action_ACTION_BLOCK
	default:
		// 非法值一律按放行处理 —— 任何不确定状态都不得阻断业务
		return judgev1.Action_ACTION_ORIGIN
	}
}

func toProtoSeverity(s contract.Severity) judgev1.Severity {
	switch s {
	case contract.SeverityNone:
		return judgev1.Severity_SEVERITY_NONE
	default:
		return judgev1.Severity_SEVERITY_NONE
	}
}
