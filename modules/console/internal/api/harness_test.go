package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	telemetryv1 "shen/common/api/telemetry/v1"
	"shen/modules/console/internal/audit"
	"shen/modules/console/internal/auth"
	"shen/modules/console/internal/geoip"
	"shen/modules/console/internal/registry"
)

const (
	testOrigin   = "http://console.test"
	adminPass    = "initial-Admin-Pass-01"
	adminNewPass = "rotated-Admin-Pass-02"
	apiToken     = "automation-token-0123456789abcdef"
)

// fakeCore 是核心遥测读面的替身：ListEvents 按类型 / 时间 / 条数过滤，WatchEvents 从通道推送。
type fakeCore struct {
	telemetryv1.DeceptionTelemetryClient
	mu      sync.Mutex
	events  []*telemetryv1.TelemetryEvent
	listErr error
	snap    *telemetryv1.CoreSnapshot
	snapErr error
	watch   chan *telemetryv1.WatchEvent
}

func (f *fakeCore) add(t time.Time, eventType, id string, payload any) {
	raw, _ := json.Marshal(payload)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, &telemetryv1.TelemetryEvent{EventId: id, EventType: eventType,
		Payload: raw, CreatedAt: timestamppb.New(t)})
	sort.SliceStable(f.events, func(i, j int) bool {
		return f.events[i].GetCreatedAt().AsTime().After(f.events[j].GetCreatedAt().AsTime())
	})
}

func (f *fakeCore) ListEvents(_ context.Context, in *telemetryv1.ListEventsRequest, _ ...grpc.CallOption) (*telemetryv1.ListEventsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := &telemetryv1.ListEventsResponse{}
	for _, ev := range f.events {
		if in.GetEventType() != "" && ev.GetEventType() != in.GetEventType() {
			continue
		}
		if in.GetSince() != nil && ev.GetCreatedAt().AsTime().Before(in.GetSince().AsTime()) {
			continue
		}
		out.Events = append(out.Events, ev)
		if in.GetLimit() > 0 && len(out.Events) >= int(in.GetLimit()) {
			break
		}
	}
	return out, nil
}

func (f *fakeCore) GetCoreSnapshot(context.Context, *telemetryv1.GetCoreSnapshotRequest, ...grpc.CallOption) (*telemetryv1.CoreSnapshot, error) {
	return f.snap, f.snapErr
}

type fakeStream struct {
	grpc.ClientStream
	ctx context.Context
	ch  chan *telemetryv1.WatchEvent
}

func (s *fakeStream) Recv() (*telemetryv1.WatchEvent, error) {
	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case msg, ok := <-s.ch:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	}
}

func (f *fakeCore) WatchEvents(ctx context.Context, _ *telemetryv1.WatchEventsRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[telemetryv1.WatchEvent], error) {
	if f.watch == nil {
		return nil, errors.New("no stream")
	}
	return &fakeStream{ctx: ctx, ch: f.watch}, nil
}

type harness struct {
	t    *testing.T
	core *fakeCore
	auth *auth.Service
	reg  *registry.Store
	srv  *Server
	h    http.Handler
	now  time.Time
}

func newHarness(t *testing.T, opts ...func(*Config)) *harness {
	t.Helper()
	dir := t.TempDir()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	authSvc, err := auth.New(auth.Config{DataDir: dir,
		Params:        auth.Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32},
		BootstrapUser: "admin", BootstrapPassword: adminPass, APIToken: apiToken})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := registry.Open(filepath.Join(dir, "services.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	geo, err := geoip.Default()
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.Open(filepath.Join(dir, "audit.log"), 100, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = aud.Close() })
	core := &fakeCore{}
	cfg := Config{
		AllowedOrigins: []string{testOrigin},
		TokenSources:   []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		Heartbeat:      40 * time.Millisecond,
		Now:            func() time.Time { return now },
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	srv := New(core, authSvc, reg, geo, aud, cfg)
	return &harness{t: t, core: core, auth: authSvc, reg: reg, srv: srv, h: srv.Handler(), now: now}
}

// client 模拟一个浏览器会话：带 Cookie、同源 Origin、自动回带 CSRF。
type client struct {
	h      *harness
	cookie *http.Cookie
	csrf   string
}

type reqOpt func(*http.Request)

func withHeader(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }
func withRemote(addr string) reqOpt { return func(r *http.Request) { r.RemoteAddr = addr } }
func noCSRF() reqOpt                { return func(r *http.Request) { r.Header.Del("X-CSRF-Token") } }

func (c *client) do(method, path string, body any, opts ...reqOpt) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, "http://console.test"+path, rd)
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("Origin", testOrigin)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	c.h.h.ServeHTTP(rec, req)
	return rec
}

func (h *harness) anon() *client { return &client{h: h} }

// login 登录并在需要时完成首次改密，返回可直接使用的会话。
func (h *harness) login(user, pass, newPass string) *client {
	h.t.Helper()
	c := h.anon()
	rec := c.do("POST", "/api/v1/auth/login", map[string]string{"username": user, "password": pass})
	if rec.Code != http.StatusOK {
		h.t.Fatalf("登录 %s 失败：%d %s", user, rec.Code, rec.Body.String())
	}
	c.absorb(rec)
	var sess sessionView
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)
	if sess.User.MustChange && newPass != "" {
		rec = c.do("POST", "/api/v1/auth/password", map[string]string{"old_password": pass, "new_password": newPass})
		if rec.Code != http.StatusOK {
			h.t.Fatalf("改密失败：%d %s", rec.Code, rec.Body.String())
		}
		c.absorb(rec)
	}
	return c
}

func (c *client) absorb(rec *httptest.ResponseRecorder) {
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "shen_console" {
			c.cookie = ck
		}
	}
	var sess sessionView
	if json.Unmarshal(rec.Body.Bytes(), &sess) == nil && sess.CSRFToken != "" {
		c.csrf = sess.CSRFToken
	}
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("响应不是预期 JSON：%v（%s）", err, rec.Body.String())
	}
	return v
}
