package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	policyv1 "shen/common/api/policy/v1"
	"shen/common/core/internal/contract"
	"shen/common/core/internal/store"
)

// 本文件守住 AI 欺骗内容（`ai:` 段 + 内容清单）的**装载与投影**：
// 契约见 docs/spec/ai-contract.md §3 / §4 与 docs/spec/config.md §2.13，
// 规则依据 `AR-33` 与 ADR-0023。

// ── 测试用的清单：**独立于装载器的结构**（写在测试里，才测得出一致性）──────────

// testManifest 按 spec 的字段名手写，不复用 manifestDoc ——
// 用装载器自己的结构写夹具，等于「用实现验证实现」，字段改名时测试照样绿。
type testManifest struct {
	ManifestVersion int    `json:"manifest_version"`
	Version         uint64 `json:"version"`
	Selector        string `json:"selector"`
	Variants        int    `json:"variants"`
	GeneratedAt     string `json:"generated_at"`
	Generator       string `json:"generator"`
	Entries         []struct {
		Resource  string `json:"resource"`
		ProfileID string `json:"profile_id"`
		Bodies    []struct {
			VariantID int    `json:"variant_id"`
			ContentID string `json:"content_id"`
			Checksum  string `json:"checksum"`
			Body      string `json:"body"`
			Marker    string `json:"marker"`
		} `json:"bodies"`
	} `json:"entries"`
}

func sha(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func sampleManifest() testManifest {
	var m testManifest
	m.ManifestVersion = ContentManifestSchemaVersion
	m.Version = 1
	m.Selector = ContentSelectorSession
	m.Variants = 2
	m.GeneratedAt = "2026-09-20T00:00:00+00:00"
	m.Generator = "template-v1"
	for _, resource := range []string{"/api/users", "/"} {
		var entry struct {
			Resource  string `json:"resource"`
			ProfileID string `json:"profile_id"`
			Bodies    []struct {
				VariantID int    `json:"variant_id"`
				ContentID string `json:"content_id"`
				Checksum  string `json:"checksum"`
				Body      string `json:"body"`
				Marker    string `json:"marker"`
			} `json:"bodies"`
		}
		entry.Resource = resource
		entry.ProfileID = "site-a"
		for variant := 0; variant < m.Variants; variant++ {
			body := "<html>" + resource + "#" + string(rune('0'+variant)) + "</html>"
			var b struct {
				VariantID int    `json:"variant_id"`
				ContentID string `json:"content_id"`
				Checksum  string `json:"checksum"`
				Body      string `json:"body"`
				Marker    string `json:"marker"`
			}
			b.VariantID = variant
			b.ContentID = "c-test" + string(rune('0'+variant))
			b.Checksum = sha(body)
			b.Body = body
			b.Marker = "</body>"
			entry.Bodies = append(entry.Bodies, b)
		}
		m.Entries = append(m.Entries, entry)
	}
	return m
}

func writeManifest(t *testing.T, m testManifest) string {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("序列化测试清单失败：%v", err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("写测试清单失败：%v", err)
	}
	return path
}

// ── `ai:` 段的解析与校验（docs/spec/config.md §2.13）────────────────────────

func TestAISectionDefaultsToDisabled(t *testing.T) {
	// 不含 ai 段的配置（今天所有现存配置都是这样）必须走默认全关。
	l := mustLoad(t, validYAML)
	cfg, err := l.AI(context.Background())
	if err != nil {
		t.Fatalf("AI() 不应失败：%v", err)
	}
	if cfg.Enabled {
		t.Error("未配置 ai 段时 enabled 必须为 false（默认全关）")
	}
	if cfg.ManifestPath != "" {
		t.Errorf("未配置 ai 段时 manifest 必须为空：%q", cfg.ManifestPath)
	}
	if cfg.Content.Variants != defaultAIVariants {
		t.Errorf("默认 variants 应为 %d，实际 %d", defaultAIVariants, cfg.Content.Variants)
	}
	if cfg.Content.RotateCooldown != defaultAIRotateCooldown {
		t.Errorf("默认 rotate_cooldown 应为 %s，实际 %s", defaultAIRotateCooldown, cfg.Content.RotateCooldown)
	}
	if len(cfg.Kinds) != 1 || cfg.Kinds[0] != "content" {
		t.Errorf("默认 kinds 应为 [content]，实际 %v", cfg.Kinds)
	}
}

func TestAISectionParsed(t *testing.T) {
	yaml := validYAML + `ai:
  enabled: true
  kinds: ["content"]
  model: "local-7b"
  manifest: "/tmp/manifest.json"
  content:
    variants: 4
    rotate_cooldown: "5m"
`
	cfg, err := mustLoad(t, yaml).AI(context.Background())
	if err != nil {
		t.Fatalf("AI() 不应失败：%v", err)
	}
	if !cfg.Enabled || cfg.Model != "local-7b" || cfg.ManifestPath != "/tmp/manifest.json" {
		t.Errorf("ai 段未按配置解析：%+v", cfg)
	}
	if cfg.Content.Variants != 4 || cfg.Content.RotateCooldown != 5*time.Minute {
		t.Errorf("ai.content 未按配置解析：%+v", cfg.Content)
	}
}

func TestAISectionRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"未登记字段": `ai:
  enabled: true
  nope: 1
`,
		"variants 为零": `ai:
  content:
    variants: 0
`,
		"rotate_cooldown 不是时长": `ai:
  content:
    rotate_cooldown: "半小时"
`,
		"rotate_cooldown 非正": `ai:
  content:
    rotate_cooldown: "0s"
`,
		"kind 为空串": `ai:
  enabled: true
  kinds: [""]
`,
		"kind 重复": `ai:
  enabled: true
  kinds: ["content", "content"]
`,
		// 形状校验不关开关：写错了却不报、等打开开关时才爆，那才是坑。
		"关闭态下 kind 为空串": `ai:
  enabled: false
  kinds: [""]
`,
		"关闭态下 kind 重复": `ai:
  enabled: false
  kinds: ["content", "content"]
`,
	}
	for name, section := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(strings.NewReader(validYAML + section)); err == nil {
				t.Fatal("非法 ai 段竟然通过校验（配置校验形同虚设）")
			}
		})
	}
}

// ── 清单装载：结构性问题拒绝，单条问题丢该条（docs/spec/ai-contract.md §3）────

func TestLoadContentManifestHappyPath(t *testing.T) {
	path := writeManifest(t, sampleManifest())
	m, dropped, err := LoadContentManifest(path, 2)
	if err != nil {
		t.Fatalf("装载清单不应失败：%v", err)
	}
	if len(dropped) != 0 {
		t.Errorf("合法清单不应丢条：%v", dropped)
	}
	if m.Version != 1 || m.Variants != 2 || len(m.Entries) != 2 {
		t.Fatalf("清单内容不对：%+v", m)
	}
}

func TestSeedContentStoreUsesConsistencyKey(t *testing.T) {
	path := writeManifest(t, sampleManifest())
	m, _, err := LoadContentManifest(path, 2)
	if err != nil {
		t.Fatalf("装载失败：%v", err)
	}
	cs := store.NewContentMemory(nil)
	n, err := SeedContentStore(context.Background(), cs, m)
	if err != nil {
		t.Fatalf("落库失败：%v", err)
	}
	if n != 4 {
		t.Fatalf("应写入 4 条（2 资源 × 2 变体），实际 %d", n)
	}
	// 键格式是跨语言契约的一部分（生成侧的 content_key 必须一致）。
	key := contract.ContentKey("site-a", "/api/users", 1, 1)
	if key != "content:site-a:/api/users:1:1" {
		t.Fatalf("内容键格式漂了：%q", key)
	}
	raw, ok, err := cs.Get(context.Background(), key)
	if err != nil || !ok {
		t.Fatalf("内容库里应有 %q：ok=%v err=%v", key, ok, err)
	}
	if string(raw) != "<html>/api/users#1</html>" {
		t.Errorf("内容体不对：%q", raw)
	}
}

func TestLoadContentManifestRejects(t *testing.T) {
	cases := map[string]func(m *testManifest){
		"manifest_version 读不懂": func(m *testManifest) { m.ManifestVersion = 99 },
		"selector 不认识":         func(m *testManifest) { m.Selector = "cookie" },
		"variants 为零":          func(m *testManifest) { m.Variants = 0 },
		"resource 为空":          func(m *testManifest) { m.Entries[0].Resource = "" },
		"resource 重复":          func(m *testManifest) { m.Entries[1].Resource = m.Entries[0].Resource },
		"variant 越界":           func(m *testManifest) { m.Entries[0].Bodies[0].VariantID = 7 },
		"variant 重复":           func(m *testManifest) { m.Entries[0].Bodies[1].VariantID = 0 },
		"内容体为空":                func(m *testManifest) { m.Entries[0].Bodies[0].Body = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := sampleManifest()
			mutate(&m)
			if _, _, err := LoadContentManifest(writeManifest(t, m), 2); err == nil {
				t.Fatal("非法清单竟然被接受（领域禁猜）")
			}
		})
	}
}

func TestLoadContentManifestVariantsMustMatchConfig(t *testing.T) {
	path := writeManifest(t, sampleManifest())
	if _, _, err := LoadContentManifest(path, 8); err == nil {
		t.Fatal("清单 variants 与配置不一致时必须拒绝装载")
	}
}

func TestLoadContentManifestDropsBadBodies(t *testing.T) {
	m := sampleManifest()
	m.Entries[0].Bodies[0].Checksum = sha("别的字节")
	m.Entries[0].Bodies[1].Body = strings.Repeat("x", MaxContentBodyBytes+1)
	got, dropped, err := LoadContentManifest(writeManifest(t, m), 2)
	if err != nil {
		t.Fatalf("单条坏内容不应让整份清单作废：%v", err)
	}
	if len(dropped) != 2 {
		t.Fatalf("应有 2 条被丢弃（校验和不符 + 超限），实际 %d：%v", len(dropped), dropped)
	}
	if len(got.Entries) != 1 || got.Entries[0].Resource != "/" {
		t.Fatalf("坏条全被丢光的资源不应投影：%+v", got.Entries)
	}
}

func TestLoadContentManifestEmptyIsLegal(t *testing.T) {
	m := sampleManifest()
	m.Entries = nil
	got, dropped, err := LoadContentManifest(writeManifest(t, m), 2)
	if err != nil {
		t.Fatalf("空清单合法，不应失败：%v", err)
	}
	if len(dropped) != 0 || len(got.Entries) != 0 {
		t.Errorf("空清单应当解析成 0 个资源：%+v", got)
	}
}

func TestLoadContentManifestMissingFile(t *testing.T) {
	if _, _, err := LoadContentManifest(filepath.Join(t.TempDir(), "nope.json"), 2); err == nil {
		t.Fatal("清单文件不存在时必须报错（不能静默当成空清单）")
	}
}

// ── 投影：开关 + 清单 + 内容体（docs/spec/policy-payload.md §2）──────────────

// pullDoc 按**契约字段名**解析载荷 —— 与 edgeDoc 无关，改字段即测试红。
type pullDoc struct {
	SchemaVersion   int  `json:"schema_version"`
	InjectEnabled   bool `json:"inject_enabled"`
	ContentManifest *struct {
		Version  uint64 `json:"version"`
		Selector string `json:"selector"`
		Variants int    `json:"variants"`
		Entries  []struct {
			Resource  string `json:"resource"`
			ProfileID string `json:"profile_id"`
			Bodies    []struct {
				VariantID int    `json:"variant_id"`
				ContentID string `json:"content_id"`
				Checksum  string `json:"checksum"`
				Body      string `json:"body"`
				Marker    string `json:"marker"`
			} `json:"bodies"`
		} `json:"entries"`
	} `json:"content_manifest"`
}

func pullDocOf(t *testing.T, srv *Server) (pullDoc, map[string]any) {
	t.Helper()
	snap, err := srv.Pull(context.Background(), &policyv1.PolicyPullRequest{})
	if err != nil {
		t.Fatalf("Pull 失败：%v", err)
	}
	var doc pullDoc
	if err := json.Unmarshal(snap.GetPayload(), &doc); err != nil {
		t.Fatalf("载荷不是合法 JSON：%v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(snap.GetPayload(), &raw); err != nil {
		t.Fatalf("载荷不是合法 JSON：%v", err)
	}
	return doc, raw
}

func TestPullWithoutContentKeepsDefaultOff(t *testing.T) {
	srv, _ := newServerFixture(t)
	doc, raw := pullDocOf(t, srv)
	if doc.InjectEnabled {
		t.Error("未装配内容时 inject_enabled 必须为 false（默认全关，ADR-0023 决定 4）")
	}
	if _, ok := raw["inject_enabled"]; !ok {
		t.Error("inject_enabled 必须恒出现（适配器要能区分「明确关闭」与「没谈这件事」）")
	}
	if _, ok := raw["content_manifest"]; ok {
		t.Error("未装配内容时载荷**不得**出现 content_manifest")
	}
}

func TestPullProjectsContentManifest(t *testing.T) {
	srv, _ := newServerFixture(t)
	m, _, err := LoadContentManifest(writeManifest(t, sampleManifest()), 2)
	if err != nil {
		t.Fatalf("装载失败：%v", err)
	}
	cs := store.NewContentMemory(nil)
	if _, err := SeedContentStore(context.Background(), cs, m); err != nil {
		t.Fatalf("落库失败：%v", err)
	}
	srv.WithContent(ContentSource{Enabled: true, Manifest: m, Store: cs})

	doc, _ := pullDocOf(t, srv)
	if !doc.InjectEnabled {
		t.Fatal("ai.enabled=true 时 inject_enabled 必须为 true")
	}
	if doc.ContentManifest == nil {
		t.Fatal("有内容时必须投影 content_manifest")
	}
	cm := doc.ContentManifest
	if cm.Version != 1 || cm.Selector != ContentSelectorSession || cm.Variants != 2 {
		t.Errorf("清单头部不对：%+v", cm)
	}
	if len(cm.Entries) != 2 {
		t.Fatalf("应投影 2 个资源，实际 %d", len(cm.Entries))
	}
	// 内容体**来自内容库**（投影只读库，不持有副本）。
	if got := cm.Entries[0].Bodies[0].Body; got != "<html>/api/users#0</html>" {
		t.Errorf("内容体应来自内容库：%q", got)
	}
	if cm.Entries[0].Bodies[0].Checksum != sha(cm.Entries[0].Bodies[0].Body) {
		t.Error("校验和必须与内容体一致（适配器据此再验一次）")
	}
}

func TestPullSkipsContentWhenStoreEmpty(t *testing.T) {
	srv, _ := newServerFixture(t)
	m, _, err := LoadContentManifest(writeManifest(t, sampleManifest()), 2)
	if err != nil {
		t.Fatalf("装载失败：%v", err)
	}
	// 清单有元数据，但内容库是空的（模拟「装载了但没落库」）：不得投影半份清单。
	srv.WithContent(ContentSource{Enabled: true, Manifest: m, Store: store.NewContentMemory(nil)})
	doc, raw := pullDocOf(t, srv)
	if doc.ContentManifest != nil {
		t.Error("内容库为空时不得投影 content_manifest（宁可 no_content，不可注入错内容）")
	}
	if _, ok := raw["inject_enabled"]; !ok {
		t.Error("开关仍然要下发")
	}
}

func TestPullEnabledWithoutManifest(t *testing.T) {
	// `ai.enabled=true` + `ai.manifest=""`：合法的组合（开关开着但没内容）。
	srv, _ := newServerFixture(t)
	srv.WithContent(ContentSource{Enabled: true, Store: store.NewContentMemory(nil)})
	doc, _ := pullDocOf(t, srv)
	if !doc.InjectEnabled || doc.ContentManifest != nil {
		t.Errorf("开关开着但无内容时：inject_enabled=true 且无清单，实际 %v / %+v",
			doc.InjectEnabled, doc.ContentManifest)
	}
}
