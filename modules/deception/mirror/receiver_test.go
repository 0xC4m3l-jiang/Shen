package mirror

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"

	judgev1 "shen/common/api/judge/v1"
	telemetryv1 "shen/common/api/telemetry/v1"
)

type fakeJudge struct {
	got []*judgev1.JudgeRequest
	err error
}

func (f *fakeJudge) Judge(_ context.Context, in *judgev1.JudgeRequest, _ ...grpc.CallOption) (*judgev1.JudgeResponse, error) {
	f.got = append(f.got, in)
	if f.err != nil {
		return nil, f.err
	}
	// 影子模式：核心照算判定，但永远返回放行。
	return &judgev1.JudgeResponse{Action: judgev1.Action_ACTION_ORIGIN}, nil
}

type fakeTelemetry struct{ got []*telemetryv1.TelemetryEvent }

func (f *fakeTelemetry) Report(_ context.Context, in *telemetryv1.TelemetryEvent, _ ...grpc.CallOption) (*telemetryv1.ReportAck, error) {
	f.got = append(f.got, in)
	return &telemetryv1.ReportAck{Accepted: 1}, nil
}

func newReceiver(j *fakeJudge, t *fakeTelemetry) *Receiver {
	fixed := time.Unix(1700000000, 0)
	return &Receiver{Judge: j, Report: t, Now: func() time.Time { return fixed }, TrustXFF: true}
}

// TestQueryOfDecodesOnce 断言形态①的观测也带**解码一次**的查询串。
//
// 与形态③④（`deception/proxy`）的 `queryOf` 是**两份独立实现**（适配器必须能各自部署，INT-5）——
// 所以两边各有一例同样的用例：改一处而漏另一处时，只能靠各自那份用例抓住。
func TestQueryOfDecodesOnce(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"file=../../etc/passwd", "file=../../etc/passwd"},
		{"file=%2e%2e%2f%2e%2e%2fetc%2fpasswd", "file=../../etc/passwd"},
		{"q=union+select", "q=union select"},
		{"q=union%20select", "q=union select"},
		{"", ""},
		{"file=%252e%252e%252f", "file=%2e%2e%2f"}, // 只解一层：重复解码会改写真实数据
		{"q=100%", "q=100%"},                       // 非法转义：原样传递，不丢载荷
	}
	for _, c := range cases {
		target := "/download"
		if c.raw != "" {
			target += "?" + c.raw
		}
		if got := queryOf(httptest.NewRequest(http.MethodGet, target, nil)); got != c.want {
			t.Errorf("queryOf(%q) = %q，期望 %q", c.raw, got, c.want)
		}
	}
}

// 观测里 query 与 path 各归其位（path 不含查询串）。
func TestReceiver_ObservationCarriesQuery(t *testing.T) {
	j, tm := &fakeJudge{}, &fakeTelemetry{}
	r := newReceiver(j, tm)

	req := httptest.NewRequest(http.MethodGet, "/search?q=union%20select", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)

	obs := j.got[0].GetObserved()
	if obs.GetPath() != "/search" {
		t.Errorf("path 必须不含查询串，得到 %q", obs.GetPath())
	}
	if obs.GetQuery() != "q=union select" {
		t.Errorf("query 必须是解码一次后的形态，得到 %q", obs.GetQuery())
	}
}

func TestReceiver_ForwardsObservationToCore(t *testing.T) {
	j, tm := &fakeJudge{}, &fakeTelemetry{}
	r := newReceiver(j, tm)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("User-Agent", "HeadlessChrome/120")
	req.Header.Set("Cookie", "sid=abc")
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("镜像接收端必须返回 202，得到 %d", rec.Code)
	}
	if len(j.got) != 1 {
		t.Fatalf("核心应收到 1 次判定请求，得到 %d", len(j.got))
	}
	obs := j.got[0].GetObserved()
	if obs.GetPath() != "/api/me" || obs.GetSourceIp() != "203.0.113.7" {
		t.Fatalf("观测映射不正确: %+v", obs)
	}
	if obs.GetUserAgent() != "HeadlessChrome/120" {
		t.Fatalf("User-Agent 应原样透传，得到 %q", obs.GetUserAgent())
	}
	// 头名归一化为小写，便于核心侧稳定取值。
	if obs.GetHeaders()["cookie"] != "sid=abc" {
		t.Fatalf("Cookie 应以小写键传递，得到 %+v", obs.GetHeaders())
	}
}

// TestReceiver_CoreDownStillReturns202：核心不可用时，镜像源不得受影响。
func TestReceiver_CoreDownStillReturns202(t *testing.T) {
	j := &fakeJudge{err: context.DeadlineExceeded}
	tm := &fakeTelemetry{}
	r := newReceiver(j, tm)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("核心故障时仍必须返回 202，得到 %d", rec.Code)
	}
	if len(tm.got) != 1 {
		t.Fatalf("判定失败也要留下事件，得到 %d 条", len(tm.got))
	}
}

// TestDecisionID_StableWithinWindow：同一窗口内同请求得同一 ID，跨窗口则不同。
func TestDecisionID_StableWithinWindow(t *testing.T) {
	obs := &judgev1.Observation{
		SourceIp: "203.0.113.7",
		Path:     "/api/me",
		Headers:  map[string]string{"cookie": "sid=abc"},
	}
	base := time.Unix(1700000000, 0)
	a := decisionID(obs, time.Minute, base)
	b := decisionID(obs, time.Minute, base.Add(10*time.Second))
	c := decisionID(obs, time.Minute, base.Add(2*time.Minute))

	if a != b {
		t.Fatal("同一时间窗内 decision_id 必须相同")
	}
	if a == c {
		t.Fatal("跨时间窗 decision_id 必须不同")
	}
}
