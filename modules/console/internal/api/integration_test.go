package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"shen/modules/console/internal/connector"
)

const integrationToken = "integration-token-0123456789abcdef"

// newConnectorHarness 装配带连接器服务与集成令牌的 harness。
func newConnectorHarness(t *testing.T, opts ...func(*Config)) *harness {
	t.Helper()
	svc, err := connector.Open(filepath.Join(t.TempDir(), "connector-credentials.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	all := append([]func(*Config){
		func(c *Config) { c.Connector = svc; c.IntegrationToken = integrationToken },
	}, opts...)
	return newHarness(t, all...)
}

// 管理面：签发（一次性明文）→ 吊销 → 重置；密钥纪律与 RBAC。
func TestConnectorCredentialManagement(t *testing.T) {
	h := newConnectorHarness(t)
	admin := h.login("admin", adminPass, adminNewPass)
	if rec := admin.do("POST", "/api/v1/users", map[string]any{"username": "reader", "role": "viewer", "password": "Temporary-Pass-2026"}); rec.Code != http.StatusCreated {
		t.Fatalf("创建只读账号失败：%d %s", rec.Code, rec.Body.String())
	}
	reader := h.login("reader", "Temporary-Pass-2026", "Reader-Account-2026!")

	// ① 签发：响应带一次性明文；视图不带哈希。
	rec := admin.do("POST", "/api/v1/connectors/credentials", map[string]any{
		"name": "商城", "hosts": []string{"shop.example.com"}, "owner": "电商组"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("签发失败：%d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"key":"shc-`) || strings.Contains(body, "key_hash") {
		t.Fatalf("响应应带一次性明文且无哈希：%s", body)
	}
	key := decode[struct {
		Key        string `json:"key"`
		Credential struct {
			ID      string   `json:"id"`
			KeyHint string   `json:"key_hint"`
			Hosts   []string `json:"hosts"`
		} `json:"credential"`
	}](t, rec)
	if key.Credential.ID == "" || key.Credential.KeyHint != "shc-****"+key.Key[len(key.Key)-4:] {
		t.Fatalf("视图不对：%+v", key)
	}

	// ② 域名与登记表冲突：先手动登记另一个服务占了域名。
	if rec := admin.do("POST", "/api/v1/services", map[string]any{"name": "门户", "upstream": "http://10.0.0.9",
		"hosts": []string{"portal.example.com"}, "owner": "运维", "enabled": true}); rec.Code != http.StatusCreated {
		t.Fatalf("登记门户失败：%d", rec.Code)
	}
	rec = admin.do("POST", "/api/v1/connectors/credentials", map[string]any{
		"name": "别的东西", "hosts": []string{"portal.example.com"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("占用他人域名应 409：%d %s", rec.Code, rec.Body.String())
	}

	// ③ 总览：凭证在线态、网关未配置标记。
	overview := decode[struct {
		Gateway     struct{ Configured bool } `json:"gateway"`
		Credentials []struct {
			KeyHint string `json:"key_hint"`
			Online  bool   `json:"online"`
		} `json:"credentials"`
		Kpi struct {
			TotalCredentials int `json:"total_credentials"`
		} `json:"kpi"`
	}](t, admin.do("GET", "/api/v1/connectors", nil))
	if len(overview.Credentials) != 1 || overview.Credentials[0].Online || overview.Gateway.Configured || overview.Kpi.TotalCredentials != 1 {
		t.Fatalf("总览不对：%+v", overview)
	}

	// ④ 重置与吊销。
	rec = admin.do("POST", "/api/v1/connectors/credentials/"+key.Credential.ID+"/reset", map[string]any{})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"key":"shc-`) {
		t.Fatalf("重置应返回新明文：%d %s", rec.Code, rec.Body.String())
	}
	if rec := admin.do("POST", "/api/v1/connectors/credentials/"+key.Credential.ID+"/revoke", map[string]any{}); rec.Code != http.StatusOK {
		t.Fatalf("吊销失败：%d", rec.Code)
	}

	// ⑤ RBAC：只读账号可看不可写。
	if rec := reader.do("GET", "/api/v1/connectors", nil); rec.Code != http.StatusOK {
		t.Fatalf("只读账号应能看接入总览：%d", rec.Code)
	}
	if rec := reader.do("POST", "/api/v1/connectors/credentials", map[string]any{"name": "X", "hosts": []string{"x.example.com"}}); rec.Code != http.StatusForbidden {
		t.Fatalf("只读账号签发应 403：%d", rec.Code)
	}

	// ⑥ 审计与响应都不得出现明文。
	auditBody := admin.do("GET", "/api/v1/audit", nil).Body.String()
	if strings.Contains(auditBody, key.Key) {
		t.Fatal("审计出现明文 key")
	}
}

// 集成面：令牌校验、key 拉取、自动登记（手动不被覆盖）、会话上报与 KPI。
func TestIntegrationEndpoints(t *testing.T) {
	h := newConnectorHarness(t)
	admin := h.login("admin", adminPass, adminNewPass)
	gw := h.anon()
	auth := withHeader("Authorization", "Bearer "+integrationToken)
	badAuth := withHeader("Authorization", "Bearer wrong-token")

	// ① 令牌：错的 401；没装配连接器服务时（普通 harness）503。
	if rec := gw.do("GET", "/api/v1/integration/keys", nil, badAuth); rec.Code != http.StatusUnauthorized {
		t.Fatalf("错误集成令牌应 401：%d", rec.Code)
	}
	plain := newHarness(t)
	plainAdmin := plain.login("admin", adminPass, adminNewPass)
	if rec := plainAdmin.do("GET", "/api/v1/integration/keys", nil, auth); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("未装配连接器应 503：%d", rec.Code)
	}

	// ② 网关拉取 key 表（签发一个凭证后）；网关联系标记置位。
	issue := admin.do("POST", "/api/v1/connectors/credentials", map[string]any{
		"name": "商城", "hosts": []string{"shop.example.com"}})
	cred := decode[struct {
		Credential struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"credential"`
	}](t, issue)
	keys := decode[struct {
		Keys []struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Hosts   []string `json:"hosts"`
			KeyHash string   `json:"key_hash"`
		} `json:"keys"`
	}](t, gw.do("GET", "/api/v1/integration/keys", nil, auth))
	if len(keys.Keys) != 1 || keys.Keys[0].ID != cred.Credential.ID || len(keys.Keys[0].KeyHash) != 64 {
		t.Fatalf("key 表不对：%+v", keys)
	}
	overview := decode[struct {
		Gateway struct{ Configured bool } `json:"gateway"`
	}](t, admin.do("GET", "/api/v1/connectors", nil))
	if !overview.Gateway.Configured {
		t.Fatal("网关拉取后 configured 应为 true")
	}

	// ③ 自动登记：创建 → 更新 → 手动同名不被覆盖。
	reg := gw.do("POST", "/api/v1/integration/register", map[string]any{
		"name": "商城", "hosts": []string{"shop.example.com"}, "local_addr": "http://127.0.0.1:8080"}, auth)
	if reg.Code != http.StatusOK || !strings.Contains(reg.Body.String(), `"created"`) {
		t.Fatalf("自动登记应创建：%d %s", reg.Code, reg.Body.String())
	}
	reg = gw.do("POST", "/api/v1/integration/register", map[string]any{
		"name": "商城", "hosts": []string{"shop.example.com", "api.example.com"}, "local_addr": "http://127.0.0.1:8080"}, auth)
	if !strings.Contains(reg.Body.String(), `"updated"`) {
		t.Fatalf("再次登记应更新：%s", reg.Body.String())
	}
	// 手动登记**先存在**的服务：连接器不得覆盖（manual_kept，记录原样保留）。
	if rec := admin.do("POST", "/api/v1/services", map[string]any{"name": "门户", "upstream": "http://10.0.0.9",
		"hosts": []string{"portal.example.com"}, "owner": "运维", "enabled": true}); rec.Code != http.StatusCreated {
		t.Fatalf("手动登记门户失败：%d %s", rec.Code, rec.Body.String())
	}
	reg = gw.do("POST", "/api/v1/integration/register", map[string]any{
		"name": "门户", "hosts": []string{"portal.example.com"}, "local_addr": "http://127.0.0.1:9999"}, auth)
	if !strings.Contains(reg.Body.String(), "manual_kept") {
		t.Fatalf("手动登记存在时应不覆盖：%s", reg.Body.String())
	}
	list := decode[struct {
		Services []struct {
			Name     string `json:"name"`
			Upstream string `json:"upstream"`
			Source   string `json:"source"`
		} `json:"services"`
	}](t, admin.do("GET", "/api/v1/services", nil))
	if len(list.Services) != 2 {
		t.Fatalf("应有两条登记（连接器 + 手动）：%+v", list.Services)
	}
	for _, svc := range list.Services {
		if svc.Name == "门户" && (svc.Upstream != "http://10.0.0.9" || svc.Source != "manual") {
			t.Fatalf("手动登记应原样保留：%+v", svc)
		}
		if svc.Name == "商城" && svc.Source != "connector" {
			t.Fatalf("自动登记应标注来源：%+v", svc)
		}
	}

	// ④ 会话上报：上线 → KPI → 心跳（无新审计）→ 下线。
	up := gw.do("POST", "/api/v1/integration/sessions", map[string]any{
		"session_id": "sess-1", "credential_id": cred.Credential.ID, "name": "商城",
		"hosts": []string{"shop.example.com"}, "local_addr": "http://127.0.0.1:8080",
		"connector_ip": "203.0.113.10", "connector_version": "v0.1.0", "gateway_node": "gw-1",
		"online": true, "rtt_ms": 42}, auth)
	if up.Code != http.StatusOK || !strings.Contains(up.Body.String(), `"online"`) {
		t.Fatalf("会话上线失败：%d %s", up.Code, up.Body.String())
	}
	ov := decode[struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
			Online    bool   `json:"online"`
			RttMs     int64  `json:"rtt_ms"`
		} `json:"sessions"`
		Kpi struct {
			OnlineConnections int   `json:"online_connections"`
			RttMedianMs       int64 `json:"rtt_median_ms"`
		} `json:"kpi"`
	}](t, admin.do("GET", "/api/v1/connectors", nil))
	if len(ov.Sessions) != 1 || !ov.Sessions[0].Online || ov.Kpi.OnlineConnections != 1 || ov.Kpi.RttMedianMs != 42 {
		t.Fatalf("会话视图不对：%+v", ov)
	}
	// 心跳：不再产生过渡。
	if rec := gw.do("POST", "/api/v1/integration/sessions", map[string]any{
		"session_id": "sess-1", "credential_id": cred.Credential.ID, "name": "商城", "online": true, "rtt_ms": 43}, auth); !strings.Contains(rec.Body.String(), `""`) {
		t.Fatalf("心跳过渡应为空：%s", rec.Body.String())
	}
	// 下线（计划内）→ 审计含 connector.session.offline。
	gw.do("POST", "/api/v1/integration/sessions", map[string]any{
		"session_id": "sess-1", "credential_id": cred.Credential.ID, "name": "商城", "online": false, "planned_close": true}, auth)
	auditBody := admin.do("GET", "/api/v1/audit", nil).Body.String()
	if !strings.Contains(auditBody, "connector.session.offline") || !strings.Contains(auditBody, "connector.session.online") {
		t.Fatalf("审计应记录上下线过渡：%s", auditBody)
	}
}
