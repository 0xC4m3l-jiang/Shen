package api

import (
	"net/http"
	"path/filepath"
	"strconv"
	"testing"

	"shen/modules/console/internal/deception"
	"shen/modules/console/internal/rbac"
)

const coreSyncToken = "core-sync-token-0123456789abcdef"

func newConfigHarness(t *testing.T) *harness {
	t.Helper()
	store, err := deception.Open(filepath.Join(t.TempDir(), "deception"), nil)
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := deception.LoadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	return newHarness(t, func(c *Config) {
		c.Deception, c.Sync, c.Templates, c.CoreSyncToken = store, deception.NewSyncState(true, c.Now), tpl, coreSyncToken
	})
}

type datasetResp struct {
	Dataset deception.Dataset `json:"dataset"`
	Report  deception.Report  `json:"report"`
}

var honeypotsBody = []deception.Honeypot{{Name: "mirage-web", Type: "web-clone", Addr: "127.0.0.1:18081", Enabled: true}}

var decoysBody = []deception.DecoyAsset{{ID: "dev-api", Kind: "developer_api", Path: "/portal/api",
	Hosts: []string{"Shop.Example.com"}, Backend: "mirage-web", Enabled: true}}

func TestConfigDomainPermissionsAndSave(t *testing.T) {
	h := newConfigHarness(t)
	admin := h.login("admin", adminPass, adminNewPass)
	for name, role := range map[string]rbac.Role{"ops.d": rbac.DeceptionOperator, "ops.h": rbac.HoneypotOperator, "reader": rbac.Viewer} {
		if rec := admin.do("POST", "/api/v1/users", map[string]any{"username": name, "role": role, "password": "Temporary-Pass-2026"}); rec.Code != http.StatusCreated {
			t.Fatalf("创建 %s 失败：%d", name, rec.Code)
		}
	}
	opsD := h.login("ops.d", "Temporary-Pass-2026", "Deception-Ops-2026!")
	opsH := h.login("ops.h", "Temporary-Pass-2026", "Honeypot-Ops-2026!")
	reader := h.login("reader", "Temporary-Pass-2026", "Reader-Account-2026!")

	if rec := reader.do("GET", "/api/v1/config/dataset", nil); rec.Code != http.StatusOK {
		t.Fatalf("只读账号应能查看：%d", rec.Code)
	}
	if rec := reader.do("PUT", "/api/v1/config/honeypots", map[string]any{"expected_version": 0, "honeypots": honeypotsBody}); rec.Code != http.StatusForbidden {
		t.Fatalf("只读账号不能写：%d", rec.Code)
	}
	if rec := opsD.do("PUT", "/api/v1/config/honeypots", map[string]any{"expected_version": 0, "honeypots": honeypotsBody}); rec.Code != http.StatusForbidden {
		t.Fatalf("欺骗运维不能改蜜罐池：%d", rec.Code)
	}
	rec := opsH.do("PUT", "/api/v1/config/honeypots", map[string]any{"expected_version": 0, "honeypots": honeypotsBody})
	if rec.Code != http.StatusOK {
		t.Fatalf("蜜罐运维应能改蜜罐池：%d %s", rec.Code, rec.Body.String())
	}
	if v := decode[datasetResp](t, rec).Dataset.Version; v != 1 {
		t.Fatalf("保存后版本应为 1：%d", v)
	}
	if rec := opsH.do("PUT", "/api/v1/config/decoys", map[string]any{"expected_version": 1, "decoys": decoysBody}); rec.Code != http.StatusForbidden {
		t.Fatalf("蜜罐运维不能改诱饵：%d", rec.Code)
	}
	// 过期版本 → 409。
	if rec := opsD.do("PUT", "/api/v1/config/decoys", map[string]any{"expected_version": 0, "decoys": decoysBody}); rec.Code != http.StatusConflict {
		t.Fatalf("过期版本应 409：%d", rec.Code)
	}
	// 请求体混入其他域 → 400。
	if rec := opsD.do("PUT", "/api/v1/config/decoys", map[string]any{"expected_version": 1, "decoys": decoysBody, "honeypots": honeypotsBody}); rec.Code != http.StatusBadRequest {
		t.Fatalf("混入其他域应 400：%d", rec.Code)
	}
	rec = opsD.do("PUT", "/api/v1/config/decoys", map[string]any{"expected_version": 1, "decoys": decoysBody})
	if rec.Code != http.StatusOK {
		t.Fatalf("欺骗运维应能改诱饵：%d %s", rec.Code, rec.Body.String())
	}
	if got := decode[datasetResp](t, rec).Dataset.Decoys[0].Hosts[0]; got != "shop.example.com" {
		t.Errorf("主机应被规范成小写：%s", got)
	}
	// 回滚只给管理员。
	if rec := opsD.do("POST", "/api/v1/config/rollback", map[string]any{"to": 1, "expected_version": 2}); rec.Code != http.StatusForbidden {
		t.Fatalf("非管理员不能回滚：%d", rec.Code)
	}
}

func TestConfigValidationRejectsAndRollbackIncrements(t *testing.T) {
	h := newConfigHarness(t)
	admin := h.login("admin", adminPass, adminNewPass)
	admin.do("PUT", "/api/v1/config/honeypots", map[string]any{"expected_version": 0, "honeypots": honeypotsBody})

	bad := []deception.DecoyAsset{{ID: "x", Kind: "developer_api", Path: "/a", Hosts: []string{"*"}, Backend: "nope", Enabled: true}}
	rec := admin.do("PUT", "/api/v1/config/decoys", map[string]any{"expected_version": 1, "decoys": bad})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("校验不通过应 400：%d", rec.Code)
	}
	if rep := decode[datasetResp](t, rec).Report; len(rep.Errors) < 2 {
		t.Fatalf("应逐条返回错误：%+v", rep.Errors)
	}
	// 预检不保存。
	pre := admin.do("POST", "/api/v1/config/validate", map[string]any{"decoys": bad})
	if pre.Code != http.StatusOK || decode[datasetResp](t, pre).Report.OK() {
		t.Fatalf("预检应返回错误报告：%d", pre.Code)
	}
	admin.do("PUT", "/api/v1/config/decoys", map[string]any{"expected_version": 1, "decoys": decoysBody})
	rec = admin.do("POST", "/api/v1/config/rollback", map[string]any{"to": 1, "expected_version": 2})
	if rec.Code != http.StatusOK {
		t.Fatalf("回滚失败：%d %s", rec.Code, rec.Body.String())
	}
	d := decode[datasetResp](t, rec).Dataset
	if d.Version != 3 || len(d.Decoys) != 0 {
		t.Fatalf("回滚应生成 v3 并恢复 v1 内容：v%d decoys=%d", d.Version, len(d.Decoys))
	}
	if rec := admin.do("GET", "/api/v1/config/versions/2", nil); rec.Code != http.StatusOK {
		t.Fatalf("应能查看历史版本：%d", rec.Code)
	}
}

func TestCoreSyncEndpoints(t *testing.T) {
	h := newConfigHarness(t)
	anon := h.anon()
	auth := withHeader("Authorization", "Bearer "+coreSyncToken)

	if rec := anon.do("GET", "/api/v1/integration/deception", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("无令牌应 401：%d", rec.Code)
	}
	if rec := anon.do("GET", "/api/v1/integration/deception", nil, auth, withRemote("203.0.113.9:1")); rec.Code != http.StatusForbidden {
		t.Fatalf("来源不在允许网段应 403：%d", rec.Code)
	}
	rec := anon.do("GET", "/api/v1/integration/deception", nil, auth)
	if st := decode[map[string]any](t, rec)["state"]; st != "not_initialized" {
		t.Fatalf("未接管应返回 not_initialized：%v", st)
	}

	admin := h.login("admin", adminPass, adminNewPass)
	admin.do("PUT", "/api/v1/config/honeypots", map[string]any{"expected_version": 0, "honeypots": honeypotsBody})
	rec = anon.do("GET", "/api/v1/integration/deception", nil, auth)
	body := decode[struct {
		State      string               `json:"state"`
		Projection deception.Projection `json:"projection"`
	}](t, rec)
	if body.State != "ok" || body.Projection.Rev == 0 || len(body.Projection.Honeypots) != 1 {
		t.Fatalf("应返回投影：%+v", body)
	}
	rev := body.Projection.Rev
	if rec := anon.do("GET", "/api/v1/integration/deception?since_rev="+strconv.FormatUint(rev, 10), nil, auth); rec.Code != http.StatusNotModified {
		t.Fatalf("未变化应 304：%d", rec.Code)
	}
	report := deception.CoreReport{AppliedRev: rev, PolicyVersion: 1_000_000 + rev, Applied: false, Reason: "终检失败", Source: "cache"}
	if rec := anon.do("POST", "/api/v1/integration/deception/report", report, auth); rec.Code != http.StatusOK {
		t.Fatalf("上报失败：%d %s", rec.Code, rec.Body.String())
	}
	alerts := decode[map[string]any](t, admin.do("GET", "/api/v1/alerts", nil))["system_alerts"].([]any)
	found := false
	for _, a := range alerts {
		if a.(map[string]any)["id"] == "merge_rejected" {
			found = true
		}
	}
	if !found {
		t.Fatalf("告警页应出现 merge_rejected：%+v", alerts)
	}
}
