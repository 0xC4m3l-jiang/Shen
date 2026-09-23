package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── 场景包：装载、校验、选择（方案 C04：数据伪装 = 版本化素材 + 自洽）────────────────

// validPack 造一份**合法**的测试场景包（虚构组织、保留域主机、占位凭证）。
func validPack() packDoc {
	yes := true
	return packDoc{
		PackVersion: PackSchemaVersion,
		Scenarios: []scenarioDoc{{
			ID:          "contoso",
			Org:         "Contoso Labs",
			Product:     "Contoso Portal",
			Host:        "portal.contoso.example",
			Outcome:     string(LoginDemo),
			PageSize:    2,
			MaxPageSize: 10,
			Users: []userDoc{
				{ID: "u-0001", Name: "J. Doe", Role: "owner", Enabled: &yes, LastSeen: "2026-09-01T08:00:00Z"},
				{ID: "u-0002", Name: "K. Rossi", Role: "operator", Enabled: &yes, LastSeen: "2026-09-02T09:30:00Z"},
			},
			Config: []configDoc{
				{Key: "portal.version", Value: "1.4.0"},
				{Key: "portal.secret", Value: "SCENARIO-PLACEHOLDER"},
			},
			Audit: []auditDoc{
				{At: "2026-09-01T08:05:00Z", Actor: "u-0001", Action: "config.update", Target: "portal.version"},
				{At: "2026-09-02T09:45:00Z", Actor: "u-0002", Action: "user.disable", Target: "u-0001"},
			},
		}},
	}
}

func writePackFile(t *testing.T, doc packDoc) string {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scenarios.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestBuiltinPackPassesValidation 断言**内置包与外部包同一套判据**：默认场景不是规则之外的特例。
func TestBuiltinPackPassesValidation(t *testing.T) {
	if _, err := buildPacks(builtinPack()); err != nil {
		t.Fatalf("内置场景包必须合法（它是代码，不合法就是缺陷）：%v", err)
	}
	if _, ok := builtinScenarios[defaultScenarioID]; !ok {
		t.Fatalf("内置场景集合里应有 %q", defaultScenarioID)
	}
}

func TestLoadPacksAcceptsValidPack(t *testing.T) {
	packs, err := LoadPacks(writePackFile(t, validPack()))
	if err != nil {
		t.Fatalf("合法场景包应装载成功：%v", err)
	}
	sc, ok := packs["contoso"]
	if !ok {
		t.Fatalf("应装载到 contoso，得到 %v", packs)
	}
	if sc.Org != "Contoso Labs" || sc.Product != "Contoso Portal" || sc.Host != "portal.contoso.example" {
		t.Fatalf("字段映射有误：%+v", sc)
	}
	if len(sc.Users) != 2 || !sc.Users[0].Enabled || sc.Users[0].LastSeen != "2026-09-01T08:00:00Z" {
		t.Fatalf("账号映射有误：%+v", sc.Users)
	}
}

// TestLoadPacksRejects 断言坏素材**一律拒绝**（不静默丢弃、不带着坏场景起服务）。
func TestLoadPacksRejects(t *testing.T) {
	mutate := func(t *testing.T, f func(*packDoc)) string {
		t.Helper()
		doc := validPack()
		f(&doc)
		return writePackFile(t, doc)
	}
	yes := true

	cases := []struct {
		name string
		path func(*testing.T) string
		want string
	}{
		{"未知键", func(t *testing.T) string {
			t.Helper()
			raw, _ := json.Marshal(validPack())
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			m["extra_key"] = true
			out, _ := json.Marshal(m)
			path := filepath.Join(t.TempDir(), "p.json")
			_ = os.WriteFile(path, out, 0o600)
			return path
		}, "未登记的键"},
		{"版本读不懂", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.PackVersion = 99 })
		}, "pack_version"},
		{"没有场景", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios = nil })
		}, "没有任何场景"},
		{"主机名不在保留域", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios[0].Host = "portal.contoso.com" })
		}, "保留域"},
		{"主机名是字面 IP", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios[0].Host = "203.0.113.10" })
		}, "保留域"},
		{"登录语义未登记", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios[0].Outcome = "maybe" })
		}, "未登记"},
		{"分页上限小于默认值", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios[0].MaxPageSize = 1 })
		}, "不得小于"},
		{"没有账号", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios[0].Users = nil })
		}, "不能为空"},
		{"账号缺 enabled", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios[0].Users[0].Enabled = nil })
		}, "enabled"},
		{"账号 ID 重复", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) {
				d.Scenarios[0].Users[1].ID = d.Scenarios[0].Users[0].ID
				d.Scenarios[0].Audit = nil
			})
		}, "重复"},
		{"审计引用不存在的账号", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios[0].Audit[0].Actor = "u-9999" })
		}, "不在 users 里"},
		{"审计 target 引用不存在的账号", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios[0].Audit[0].Target = "u-9999" })
		}, "不在 users 里"},
		{"时间戳非法", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios[0].Users[0].LastSeen = "yesterday" })
		}, "RFC3339"},
		{"凭证类配置不是占位符", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios[0].Config[1].Value = "sk_live_51H8xQ2eZvKY" })
		}, "占位符"},
		{"配置值指向私网", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios[0].Config[0].Value = "10.24.7.9" })
		}, "真实基础设施"},
		{"配置值用内网后缀", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios[0].Config[0].Value = "db.contoso.internal" })
		}, "真实基础设施"},
		{"场景 id 重复", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios = append(d.Scenarios, d.Scenarios[0]) })
		}, "必须唯一"},
		{"账号显式启用（占位断言：合法 sid）", func(t *testing.T) string {
			return mutate(t, func(d *packDoc) { d.Scenarios[0].Users[0].Enabled = &yes })
		}, ""}, // 这一条是**正样本**：应当通过
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := LoadPacks(c.path(t))
			if c.want == "" {
				if err != nil {
					t.Fatalf("该场景包应合法，却失败：%v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("应当拒绝（期望报错含 %q），却装载成功", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("报错应指出 %q，实际：%v", c.want, err)
			}
		})
	}
}

// TestLoadPacksMissingFile 断言文件不存在时明确报错（由装配层决定是启动失败还是回落）。
func TestLoadPacksMissingFile(t *testing.T) {
	if _, err := LoadPacks(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("文件不存在应报错")
	}
	if _, err := LoadPacks("  "); err == nil {
		t.Fatal("空路径应报错")
	}
}

// TestSelectScenarioPrefersPackThenFallsBack 断言选择语义：包里有就用包的，没有就回落内置（不报错）。
func TestSelectScenarioPrefersPackThenFallsBack(t *testing.T) {
	packs, err := buildPacks(validPack())
	if err != nil {
		t.Fatal(err)
	}
	if sc := SelectScenario(packs, "contoso"); sc.Org != "Contoso Labs" {
		t.Fatalf("应选中包里的场景，得到 %+v", sc)
	}
	// 未知 id / 空 id / nil 包：一律回落内置默认场景（诱饵后端不该因配置写错整体不可用）。
	for _, id := range []string{"", "nope"} {
		if sc := SelectScenario(packs, id); sc.ID != defaultScenarioID {
			t.Fatalf("id=%q 应回落到内置场景，得到 %q", id, sc.ID)
		}
	}
	if sc := SelectScenario(nil, "anything"); sc.ID != defaultScenarioID {
		t.Fatalf("nil 包应回落到内置场景，得到 %q", sc.ID)
	}
}

// TestHandlerServesExternalPack 断言装载外部包之后，**服务出来的是那一套场景**（而不是内置的）。
func TestHandlerServesExternalPack(t *testing.T) {
	packs, err := buildPacks(validPack())
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{ScenarioID: "contoso", Packs: packs, Rand: fixedRand{3}})

	page := do(h, http.MethodGet, "/admin/login", "", nil)
	if !strings.Contains(page.Body.String(), "Contoso Labs") || !strings.Contains(page.Body.String(), "Contoso Portal") {
		t.Fatalf("登录页应展示外部场景的组织与产品，得到：%s", page.Body.String())
	}
	cookie := demoSessionCookie(t, h)
	api := do(h, http.MethodGet, "/api/v1/admin/users?page=1", "", map[string]string{"Cookie": cookie})
	if !strings.Contains(api.Body.String(), "J. Doe") || !strings.Contains(api.Body.String(), "u-0001") {
		t.Fatalf("API 应返回外部场景的账号，得到：%s", api.Body.String())
	}
}
