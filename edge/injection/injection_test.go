package injection

import (
	"bytes"
	"strings"
	"testing"
)

const page = `<!doctype html><html><body><h1>hi</h1></body></html>`

func mustEngine(t *testing.T, rules ...Rule) *Engine {
	t.Helper()
	e, err := New(rules)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	return e
}

// ── 注入 ───────────────────────────────────────────────────────────────────

func TestInjectBeforeBodyClose(t *testing.T) {
	e := mustEngine(t, Rule{Kind: "developer_api", Snippet: `<section id="dev-api"></section>`})
	got, changed := e.Inject("text/html; charset=utf-8", []byte(page))
	if !changed {
		t.Fatal("应发生改写")
	}
	s := string(got)
	if !strings.Contains(s, `<section id="dev-api">`) {
		t.Fatalf("片段未注入：%s", s)
	}
	// 片段必须在 </body> 之前
	if strings.Index(s, "dev-api") > strings.Index(s, "</body>") {
		t.Errorf("片段应注入在 </body> 之前：%s", s)
	}
	// 原内容必须保留（只增不改）
	if !strings.Contains(s, "<h1>hi</h1>") {
		t.Errorf("原内容必须保留：%s", s)
	}
}

func TestInjectMultipleRules(t *testing.T) {
	e := mustEngine(t,
		Rule{Kind: "developer_api", Snippet: "<!-- A -->"},
		Rule{Kind: "hidden_link", Snippet: "<!-- B -->"},
	)
	got, changed := e.Inject("text/html", []byte(page))
	if !changed {
		t.Fatal("应发生改写")
	}
	if !bytes.Contains(got, []byte("<!-- A -->")) || !bytes.Contains(got, []byte("<!-- B -->")) {
		t.Errorf("两条规则都应注入：%s", got)
	}
}

func TestCustomMarker(t *testing.T) {
	e := mustEngine(t, Rule{Snippet: "<bait/>", Marker: "</head>"})
	body := []byte(`<html><head></head><body></body></html>`)
	got, changed := e.Inject("text/html", body)
	if !changed {
		t.Fatal("应发生改写")
	}
	if strings.Index(string(got), "<bait/>") > strings.Index(string(got), "</head>") {
		t.Errorf("应按自定义标记注入：%s", got)
	}
}

// ── 不注入的情形（必须原样返回，绝不阻断）────────────────────────────────

func TestNonHTMLUntouched(t *testing.T) {
	e := mustEngine(t, Rule{Snippet: "<bait/>"})
	for _, ct := range []string{"application/json", "text/plain", "image/png", ""} {
		body := []byte(`{"a":1}`)
		got, changed := e.Inject(ct, body)
		if changed {
			t.Errorf("%q：非 HTML 不得改写", ct)
		}
		if !bytes.Equal(got, body) {
			t.Errorf("%q：必须原样返回", ct)
		}
	}
}

func TestMarkerAbsentUntouched(t *testing.T) {
	e := mustEngine(t, Rule{Kind: "x", Snippet: "<!-- A -->", Marker: "</footer>"})
	body := []byte(page)
	got, changed := e.Inject("text/html", body)
	if changed {
		t.Error("找不到标记不得算作改写")
	}
	if !bytes.Equal(got, body) {
		t.Error("找不到标记必须原样返回")
	}
}

func TestNoRulesUntouched(t *testing.T) {
	e := mustEngine(t)
	body := []byte(page)
	got, changed := e.Inject("text/html", body)
	if changed || !bytes.Equal(got, body) {
		t.Error("无规则时应原样返回")
	}
}

func TestEmptyBodyUntouched(t *testing.T) {
	e := mustEngine(t, Rule{Snippet: "x"})
	if got, changed := e.Inject("text/html", nil); changed || len(got) != 0 {
		t.Error("空响应体应原样返回")
	}
}

// ── 构造校验 ───────────────────────────────────────────────────────────────

func TestNewRejectsEmptySnippet(t *testing.T) {
	for _, s := range []string{"", "   "} {
		if _, err := New([]Rule{{Kind: "x", Snippet: s}}); err == nil {
			t.Errorf("空片段 %q 应被拒绝", s)
		}
	}
}

func TestNewDefaultsMarker(t *testing.T) {
	e := mustEngine(t, Rule{Snippet: "<!-- A -->"})
	if e.rules[0].Marker != "</body>" {
		t.Errorf("空标记应默认为 </body>，得到 %q", e.rules[0].Marker)
	}
}

// ── 确定性：同输入必得同输出 ───────────────────────────────────────────────

func TestInjectDeterministic(t *testing.T) {
	e := mustEngine(t, Rule{Snippet: "<!-- A -->"})
	first, _ := e.Inject("text/html", []byte(page))
	for i := 0; i < 5; i++ {
		got, _ := e.Inject("text/html", []byte(page))
		if !bytes.Equal(first, got) {
			t.Fatal("注入必须是纯变换（同输入同输出）")
		}
	}
}
