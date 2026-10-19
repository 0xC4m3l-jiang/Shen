package api

// SHEN-M4-03：删除服务必须**级联吊销**它的接入凭证（不留孤儿数据）。
//
// 这条用例走的是**真实 HTTP 路径**（登录 → 建服务 → 建凭证 → 删除服务），
// 而不是直接调两个 store：级联的价值就在「同一次操作里两个存储都变了」，
// 只测函数会把最容易出错的那一步（api 里忘了接线）漏掉。

import (
	"net/http"
	"path/filepath"
	"testing"

	"shen/modules/console/internal/connector"
)

func TestDeleteServiceCascadesCredentialRevocation(t *testing.T) {
	dir := t.TempDir()
	keysSvc, err := connector.Open(filepath.Join(dir, "keys.json"), nil)
	if err != nil {
		t.Fatalf("打开凭证表失败：%v", err)
	}
	h := newHarness(t, func(c *Config) { c.Connector = keysSvc })
	c := h.login("admin", adminPass, adminNewPass)

	// ① 登记一个服务（走 API：契约与真实调用一致）。
	rec := c.do(http.MethodPost, "/api/v1/services", map[string]any{
		"name":     "site-a",
		"upstream": "http://127.0.0.1:8080",
		"hosts":    []string{"a.example.com"},
		"enabled":  true,
	})
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("建服务失败：%d %s", rec.Code, rec.Body.String())
	}
	svcList := h.reg.List()
	if len(svcList) != 1 {
		t.Fatalf("登记表应有 1 条，实际 %d", len(svcList))
	}
	svc := svcList[0]

	// ② 给同名服务签一条接入凭证。
	if _, _, err := keysSvc.Keys().Issue(connector.IssueInput{
		Name:  "site-a",
		Hosts: []string{"a.example.com"},
	}, "admin"); err != nil {
		t.Fatalf("签发凭证失败：%v", err)
	}

	// ③ 删除服务 ⇒ 凭证必须被连带吊销。
	rec = c.do(http.MethodDelete, "/api/v1/services/"+svc.ID+"?version="+itoa(svc.Version), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("删除服务失败：%d %s", rec.Code, rec.Body.String())
	}
	creds := keysSvc.Keys().List()
	if len(creds) != 1 {
		t.Fatalf("凭证记录应保留（留痕不删除），实际 %d 条", len(creds))
	}
	if !creds[0].Revoked {
		t.Fatal("删除服务后，其凭证必须被吊销（否则残留的凭证仍能接入）")
	}
}

func TestDeleteServiceWithoutConnectorStillWorks(t *testing.T) {
	// 未启用连接器（`Connector == nil`）时，删除服务不得因为级联而失败。
	h := newHarness(t)
	c := h.login("admin", adminPass, adminNewPass)
	rec := c.do(http.MethodPost, "/api/v1/services", map[string]any{
		"name":     "site-a",
		"upstream": "http://127.0.0.1:8080",
		"hosts":    []string{"a.example.com"},
		"enabled":  true,
	})
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("建服务失败：%d %s", rec.Code, rec.Body.String())
	}
	svc := h.reg.List()[0]
	rec = c.do(http.MethodDelete, "/api/v1/services/"+svc.ID+"?version="+itoa(svc.Version), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("未启用连接器时删除也应成功，实际 %d %s", rec.Code, rec.Body.String())
	}
}

// itoa 是测试内的小工具（避免为一个用例引入 strconv 依赖面）。
func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
