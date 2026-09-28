package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	telemetryv1 "shen/common/api/telemetry/v1"
	"shen/modules/console/internal/registry"
	"shen/modules/console/internal/topology"
)

func judged(id, host, ip, executed, delivery, action string, shadow bool) map[string]any {
	return map[string]any{"decision_id": id, "method": "GET", "path": "/admin/login", "ua": "HeadlessChrome/120",
		"action": action, "shadow": shadow, "executed": executed, "backend": "web", "status": 200,
		"delivery_result": delivery, "host": host, "source_ip": ip}
}

// seed 造一组覆盖五种落点的流量（全部在 15 分钟窗口内），外加一条窗口外的旧流量。
func seed(h *harness) {
	at := func(min int) time.Time { return h.now.Add(-time.Duration(min) * time.Minute) }
	h.core.add(at(1), judgedEventType, "judged:d1:1", judged("d1", "shop.example.com", "114.114.114.114", "mirage", "", "ACTION_MIRAGE", false))
	h.core.add(at(2), judgedEventType, "judged:d2:2", judged("d2", "shop.example.com", "114.114.114.114", "decoy", "delivered", "", false))
	h.core.add(at(3), judgedEventType, "judged:d3:3", judged("d3", "shop.example.com", "202.96.128.86", "decoy", "backend_unavailable", "", false))
	h.core.add(at(4), judgedEventType, "judged:d4:4", judged("d4", "api.other.com", "10.0.0.8", "origin", "", "ACTION_MIRAGE", true))
	h.core.add(at(5), judgedEventType, "judged:d5:5", judged("d5", "api.other.com", "1.1.1.1", "origin", "", "ACTION_ORIGIN", false))
	h.core.add(at(90), judgedEventType, "judged:old:6", judged("old", "shop.example.com", "1.1.1.1", "mirage", "", "ACTION_MIRAGE", false))
	h.core.add(at(1), decisionEventType, "d1", map[string]any{"decision_id": "d1", "source_ip": "114.114.114.114",
		"action": "route_mirage", "severity": "high", "score": 0.95, "signals": []string{"ua-headless"}})
	h.core.add(at(4), decisionEventType, "d4", map[string]any{"decision_id": "d4", "source_ip": "10.0.0.8",
		"action": "route_mirage", "severity": "none", "score": 0.8})
}

func TestOverviewAggregation(t *testing.T) {
	h := newHarness(t)
	seed(h)
	svc, err := h.reg.Create(registry.Input{Name: "商城", Upstream: "http://10.0.0.5", Hosts: []string{"shop.example.com"}, Enabled: true}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	c := h.login("admin", adminPass, adminNewPass)
	ov := decode[Overview](t, c.do("GET", "/api/v1/overview?window=15m", nil))
	st := ov.Stats
	if st.Total != 5 {
		t.Fatalf("窗口外的旧流量不应计入：total=%d", st.Total)
	}
	if st.InMirage != 2 || st.ShadowMirage != 1 || st.DecoyFailed != 1 || st.Alerts != 1 {
		t.Errorf("流入蜃楼口径不对：%+v", st)
	}
	if st.ByLayer["decoy"] != 2 || st.ByLayer["mirage"] != 1 || st.ByLayer["origin"] != 2 {
		t.Errorf("分层统计不对：%v", st.ByLayer)
	}
	if st.UniqueIPs != 4 || ov.Sources[0].IP != "114.114.114.114" || ov.Sources[0].Count != 2 {
		t.Errorf("来源排行不对：%+v", ov.Sources)
	}
	if ov.Sources[0].Geo.City != "南京市" {
		t.Errorf("来源应带中文归属地：%+v", ov.Sources[0].Geo)
	}
	var sawPrivate bool
	for _, g := range ov.Geo {
		sawPrivate = sawPrivate || (g.Scope == "private" && g.Label == "内网地址")
	}
	if !sawPrivate {
		t.Errorf("内网来源应单独归类：%+v", ov.Geo)
	}
	var trendTotal int
	for _, b := range ov.Trend {
		for _, n := range b.ByLayer {
			trendTotal += n
		}
	}
	if len(ov.Trend) != trendBuckets || trendTotal != 5 {
		t.Errorf("趋势分桶应覆盖全部行：桶=%d 合计=%d", len(ov.Trend), trendTotal)
	}
	if ov.Recent[0].DecisionID != "d1" || ov.Recent[0].Score == nil || *ov.Recent[0].Score != 0.95 ||
		ov.Recent[0].ServiceID != svc.ID || !ov.Recent[0].Alert {
		t.Errorf("最近流量应合并判定分值、服务归类与告警：%+v", ov.Recent[0])
	}
	if ov.Window.Truncated || ov.Window.Seconds != 900 || ov.Services != 1 {
		t.Errorf("窗口描述不对：%+v services=%d", ov.Window, ov.Services)
	}
}

func TestServiceTrafficAndUnregisteredHosts(t *testing.T) {
	h := newHarness(t)
	seed(h)
	c := h.login("admin", adminPass, adminNewPass)
	rec := c.do("POST", "/api/v1/services", map[string]any{"name": "商城", "upstream": "http://10.0.0.5",
		"hosts": []string{"SHOP.example.com:443"}, "owner": "电商组", "enabled": true})
	if rec.Code != http.StatusCreated {
		t.Fatalf("登记失败：%d %s", rec.Code, rec.Body.String())
	}
	svc := decode[registry.Service](t, rec)

	detail := decode[serviceTraffic](t, c.do("GET", "/api/v1/services/"+svc.ID+"/traffic", nil))
	if detail.Stats.Total != 3 || detail.Stats.InMirage != 2 || len(detail.Rows) != 3 {
		t.Fatalf("单服务流量应只含本服务域名：%+v", detail.Stats)
	}
	funnel := map[string]int{}
	for _, f := range detail.Funnel {
		funnel[f.Key] = f.Count
	}
	if funnel["requests"] != 3 || funnel["decoy_hit"] != 2 || funnel["in_mirage"] != 2 {
		t.Errorf("漏斗不对：%+v", detail.Funnel)
	}

	list := decode[struct {
		Services []serviceSummary `json:"services"`
	}](t, c.do("GET", "/api/v1/services", nil))
	if len(list.Services) != 1 || list.Services[0].Stats.Total != 3 || len(list.Services[0].Spark) != trendBuckets {
		t.Errorf("服务卡片摘要不对：%+v", list.Services)
	}

	un := decode[struct {
		Hosts []struct {
			Host  string `json:"host"`
			Count int    `json:"count"`
		} `json:"hosts"`
	}](t, c.do("GET", "/api/v1/services/unregistered", nil))
	if len(un.Hosts) != 1 || un.Hosts[0].Host != "api.other.com" || un.Hosts[0].Count != 2 {
		t.Errorf("未登记域名发现不对：%+v", un.Hosts)
	}

	upd := map[string]any{"version": 1, "name": "商城", "upstream": "http://10.0.0.6", "hosts": []string{"shop.example.com"}, "enabled": true}
	if rec := c.do("PUT", "/api/v1/services/"+svc.ID, upd); rec.Code != http.StatusOK {
		t.Fatalf("按版本更新失败：%d %s", rec.Code, rec.Body.String())
	}
	if rec := c.do("PUT", "/api/v1/services/"+svc.ID, upd); rec.Code != http.StatusConflict {
		t.Errorf("旧版本更新应 409：%d", rec.Code)
	}
	if rec := c.do("DELETE", "/api/v1/services/"+svc.ID, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("删除缺版本应 400：%d", rec.Code)
	}
	if rec := c.do("DELETE", "/api/v1/services/"+svc.ID+"?version=2", nil); rec.Code != http.StatusNoContent {
		t.Errorf("删除失败：%d %s", rec.Code, rec.Body.String())
	}
}

func TestHoneypotDeliveriesAndTrace(t *testing.T) {
	h := newHarness(t)
	seed(h)
	c := h.login("admin", adminPass, adminNewPass)
	d := decode[deliveriesView](t, c.do("GET", "/api/v1/honeypot/deliveries", nil))
	if d.Total != 3 || d.ByResult["backend_unavailable"] != 1 || len(d.Failures) != 1 || d.Interactions["connected"] != false {
		t.Errorf("投递视图不对：%+v", d)
	}
	tr := decode[traceView](t, c.do("GET", "/api/v1/deception/trace?decision_id=d1", nil))
	if tr.Judged == nil || tr.Decision == nil || !tr.Alerts.Real || !tr.Alerts.HighRisk {
		t.Errorf("追踪应按载荷 decision_id 关联到执行记录与判定：%+v", tr)
	}
	h.core.listErr = errString("down")
	if rec := c.do("GET", "/api/v1/overview", nil); rec.Code != http.StatusBadGateway {
		t.Errorf("核心不可用应 502：%d", rec.Code)
	}
	if rec := c.do("GET", "/api/v1/services", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "traffic_error") {
		t.Errorf("核心不可用时登记列表仍应可用并说明原因：%d %s", rec.Code, rec.Body.String())
	}
}

// 判定流里点击某条记录看链路：graphs 按 decision_id 过滤，只返回那一条；
// 查不到的（已被缓冲挤出 / 只有判定没有执行记录）返回空数组而不是报错。
func TestGraphsFilterByDecisionID(t *testing.T) {
	h := newHarness(t)
	seed(h)
	c := h.login("admin", adminPass, adminNewPass)
	one := decode[[]topology.RequestGraph](t, c.do("GET", "/api/v1/deception/graphs?decision_id=d1", nil))
	if len(one) != 1 || one[0].DecisionID != "d1" || len(one[0].Chain) == 0 {
		t.Fatalf("按 decision_id 过滤应只返回该判定的链路：%+v", one)
	}
	// d1 是高分拦截告警：链路应合并核心判定的动作与信号。
	if one[0].Action == "" || one[0].Score != 0.95 {
		t.Errorf("链路应合并核心判定：%+v", one[0])
	}
	miss := decode[[]topology.RequestGraph](t, c.do("GET", "/api/v1/deception/graphs?decision_id=missing", nil))
	if len(miss) != 0 {
		t.Fatalf("查不到的 decision 应返回空数组：%+v", miss)
	}
	all := decode[[]topology.RequestGraph](t, c.do("GET", "/api/v1/deception/graphs", nil))
	if len(all) < 6 {
		t.Errorf("不带过滤应返回全部链路：%d", len(all))
	}
}

func TestStreamPushesEnrichedRowsAndEndsOnLogout(t *testing.T) {
	h := newHarness(t)
	h.core.watch = make(chan *telemetryv1.WatchEvent, 4)
	c := h.login("admin", adminPass, adminNewPass)
	ts := httptest.NewServer(h.h)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/stream", nil)
	req.AddCookie(c.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("应为事件流：%d %s", resp.StatusCode, ct)
	}
	raw, _ := json.Marshal(judged("d9", "shop.example.com", "114.114.114.114", "mirage", "", "ACTION_MIRAGE", false))
	h.core.watch <- &telemetryv1.WatchEvent{Body: &telemetryv1.WatchEvent_Event{Event: &telemetryv1.TelemetryEvent{
		EventId: "judged:d9:1", EventType: judgedEventType, Payload: raw, CreatedAt: timestamppb.New(h.now)}}}

	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	var gotRow, gotPing, gotClosed bool
	deadline := time.After(3 * time.Second)
	for !gotClosed {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("流在收到 closed 帧前意外结束")
			}
			switch {
			case strings.HasPrefix(line, "data: "):
				var f sseFrame
				_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f)
				if f.Kind == "event" && f.Row != nil && f.Row.InMirage && f.Row.Geo.City == "南京市" {
					gotRow = true
					h.auth.Logout(c.cookie.Value) // 会话被注销 ⇒ 下一次心跳应结束流
				}
				if f.Kind == "closed" && f.Code == "unauthenticated" {
					gotClosed = true
				}
			case line == ": ping":
				gotPing = true
			}
		case <-deadline:
			t.Fatalf("超时：row=%v ping=%v closed=%v", gotRow, gotPing, gotClosed)
		}
	}
	if !gotRow {
		t.Error("推送帧应带合并好的请求行（含归属地、流入蜃楼）")
	}
}

// 缓存命中时 executed=cache 只表示「未重新判定」：实际落点在 dispatched。
// 回归用例：此前把缓存命中一律归为「真实业务」，流入蜃楼被系统性低估（本机联调实测发现）。
func TestCacheHitUsesDispatchedLanding(t *testing.T) {
	h := newHarness(t)
	cached := func(id, dispatched string) map[string]any {
		m := judged(id, "shop.example.com", "1.1.1.1", "cache", "", "ACTION_MIRAGE", false)
		m["dispatched"] = dispatched
		return m
	}
	h.core.add(h.now.Add(-time.Minute), judgedEventType, "judged:c1:1", cached("c1", "mirage"))
	h.core.add(h.now.Add(-time.Minute), judgedEventType, "judged:c2:1", cached("c2", "origin_fallback"))
	h.core.add(h.now.Add(-time.Minute), judgedEventType, "judged:c3:1", cached("c3", "")) // 老适配器：无 dispatched
	c := h.login("admin", adminPass, adminNewPass)
	ov := decode[Overview](t, c.do("GET", "/api/v1/overview", nil))
	if ov.Stats.InMirage != 1 || ov.Stats.ByLayer["mirage"] != 2 || ov.Stats.ByLayer["fallback"] != 1 {
		t.Fatalf("缓存命中应按 dispatched 归层：%+v", ov.Stats)
	}
}

// 核心事件流打不开时必须返回非 200：浏览器 EventSource 据此走 onerror（显示「重连中」），
// 而不是先 onopen（显示「实时」）再收到 closed —— 本机联调实测到过这种「假实时」闪烁。
func TestStreamCoreUnavailableIsNot200(t *testing.T) {
	h := newHarness(t) // h.core.watch == nil ⇒ WatchEvents 直接报错
	c := h.login("admin", adminPass, adminNewPass)
	rec := c.do("GET", "/api/v1/stream", nil)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "core_unavailable") {
		t.Fatalf("期望 503 core_unavailable，实际 %d %s", rec.Code, rec.Body.String())
	}
	if n := h.srv.streams.Load(); n != 0 {
		t.Fatalf("失败的流不应占用并发名额：%d", n)
	}
}
