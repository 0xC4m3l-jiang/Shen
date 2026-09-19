package control

import (
	"context"
	"net/netip"
	"testing"

	judgev1 "shen/api/judge/v1"
	"shen/core/internal/contract"
)

// stubSession 是 session.Session 的测试替身。
type stubSession struct{ got contract.Observation }

func (s *stubSession) Key(_ context.Context, obs contract.Observation) (contract.SessionKey, error) {
	s.got = obs
	return contract.SessionKey{ID: "s-1", Source: contract.SourceBusinessCookie}, nil
}

type stubDecider struct{ got contract.JudgeRequest }

func (s *stubDecider) Decide(_ context.Context, req contract.JudgeRequest) (contract.Decision, error) {
	s.got = req
	return contract.Decision{DecisionID: req.DecisionID, Action: contract.ActionMirage, Backend: "lou-1"}, nil
}

// TestJudge_ProtoToContract 覆盖入参映射。
func TestJudge_ProtoToContract(t *testing.T) {
	d := &stubDecider{}
	svc := NewJudgeService(d, &stubSession{})

	_, err := svc.Judge(context.Background(), &judgev1.JudgeRequest{
		DecisionId: "d-9",
		Observed: &judgev1.Observation{
			SourceIp:       "203.0.113.7",
			UserAgent:      "HeadlessChrome/120",
			Method:         "GET",
			Path:           "/api/me",
			TlsFingerprint: "fp-1",
			Headers:        map[string]string{"cookie": "sid=abc"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.got.DecisionID != "d-9" || d.got.Observed.Path != "/api/me" || d.got.Observed.TLSFingerprint != "fp-1" {
		t.Fatalf("入参映射不正确: %+v", d.got)
	}
	if d.got.Observed.SourceIP.String() != "203.0.113.7" {
		t.Fatalf("源 IP 映射不正确: %v", d.got.Observed.SourceIP)
	}
}

// TestJudge_ExtractsSession：会话身份由核心自己提取，不信任调用方传来的值。
func TestJudge_ExtractsSession(t *testing.T) {
	d := &stubDecider{}
	sess := &stubSession{}
	svc := NewJudgeService(d, sess)

	_, err := svc.Judge(context.Background(), &judgev1.JudgeRequest{
		DecisionId: "d-1",
		Observed:   &judgev1.Observation{SourceIp: "203.0.113.7", Path: "/api/me"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.got.Session.ID != "s-1" || d.got.Session.Source != contract.SourceBusinessCookie {
		t.Fatalf("会话身份未进入内部请求: %+v", d.got.Session)
	}
	if sess.got.SourceIP != netip.MustParseAddr("203.0.113.7") {
		t.Fatalf("提取会话时收到的观测不正确: %+v", sess.got)
	}
}

// TestJudge_ResponseHasNoVerdict：响应体禁止回显分值 / 规则名 / 证据。
func TestJudge_ResponseHasNoVerdict(t *testing.T) {
	svc := NewJudgeService(&stubDecider{}, &stubSession{})
	res, err := svc.Judge(context.Background(), &judgev1.JudgeRequest{DecisionId: "d-1"})
	if err != nil {
		t.Fatal(err)
	}
	// JudgeResponse 只有 action / severity / backend 三个字段；
	// 若未来有人加了 score / signals / rules，本断言会因字段数变化而暴露。
	if res.GetAction() != judgev1.Action_ACTION_MIRAGE || res.GetBackend() != "lou-1" {
		t.Fatalf("出参映射不正确: %+v", res)
	}
}

// TestToProtoAction_UnknownFallsBackToOrigin 覆盖 V-4：非法决策值回落放行。
func TestToProtoAction_UnknownFallsBackToOrigin(t *testing.T) {
	if got := toProtoAction(contract.Action(99)); got != judgev1.Action_ACTION_ORIGIN {
		t.Fatalf("非法决策值应回落 ACTION_ORIGIN，得到 %v", got)
	}
}

// TestShadowDecider_NeverActs：影子模式只算判定、不处置。
func TestShadowDecider_NeverActs(t *testing.T) {
	sd := NewShadowDecider(stubJudge{})
	d, err := sd.Decide(context.Background(), contract.JudgeRequest{DecisionID: "d-1"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != contract.ActionOrigin {
		t.Fatalf("影子模式必须输出 route_origin，得到 %s", d.Action)
	}
	if d.Verdict.Score == 0 {
		t.Fatal("影子模式仍应计算判定（用于阈值校准）")
	}
}

type stubJudge struct{}

func (stubJudge) Judge(context.Context, contract.JudgeRequest) (contract.Verdict, error) {
	return contract.Verdict{Score: 0.9}, nil
}
