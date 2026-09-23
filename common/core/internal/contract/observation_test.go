package contract

import "testing"

// TestNormalizePath 断言 `path_norm` 的归一化口径（它与 `path` 是两个字段：一个是原样，一个是匹配视图）。
//
// 为什么要单独立一例：它决定「前缀绕过」能不能被关闭 —— 归一化错了，
// `/static/../.git/config` 依旧绕开路径规则，而文档会说它已经支持（那比不支持更糟）。
func TestNormalizePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/", "/"},
		{"", ""}, // 不凭空造一个 "/"
		{"/a/b", "/a/b"},
		{"/a/b/", "/a/b"},   // 尾斜杠去掉
		{"//a///b", "/a/b"}, // 重复斜杠合并
		{"/a/./b", "/a/b"},  // `.` 段去掉
		{"/static/../.git/config", "/.git/config"}, // 前缀绕过的关法
		{"/a/b/../../c", "/c"},
		{"/../etc/passwd", "/etc/passwd"}, // 越到根以上：按标准库语义停在根
		{"/a/%2e%2e/b", "/a/%2e%2e/b"},    // 不做二次解码：编码的点段留着
	}
	for _, c := range cases {
		if got := NormalizePath(c.in); got != c.want {
			t.Errorf("NormalizePath(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestPathSegmentPrefix 断言路径段边界的判定（规则匹配与诱饵路由共用这一份定义）。
//
// 这是「前缀」类规则最容易错的地方：纯字符串前缀会把 `/.gitignore` 当成 `/.git` 之下。
func TestPathSegmentPrefix(t *testing.T) {
	cases := []struct {
		got, want string
		ok        bool
	}{
		{"/.git", "/.git", true},
		{"/.git/config", "/.git", true},
		{"/.gitignore", "/.git", false}, // 典型误伤
		{"/.gitlab/ci.yml", "/.git", false},
		{"/admin", "/admin", true},
		{"/admin/login", "/admin", true},
		{"/administrator", "/admin", false}, // 典型误伤
		{"/admin/", "/admin", true},         // want 带尾斜杠：同一段
		{"/adminx", "/admin/", false},
		{"/", "/", true},
		{"", "/", true}, // 根之下：空路径也当根（与 path.Clean 的语义一致）
		{"/anything", "", false},
	}
	for _, c := range cases {
		if got := PathSegmentPrefix(c.got, c.want); got != c.ok {
			t.Errorf("PathSegmentPrefix(%q, %q) = %v，期望 %v", c.got, c.want, got, c.ok)
		}
	}
}

// TestFieldPathNormIsDerivedNotStored 断言 `path_norm` 是**派生**字段：它跟着 Path 走，不是另一份状态。
func TestFieldPathNormIsDerivedNotStored(t *testing.T) {
	o := Observation{Path: "/static/../.git/config"}
	if got, want := o.Field("path_norm"), "/.git/config"; got != want {
		t.Fatalf("Field(path_norm) = %q，期望 %q", got, want)
	}
	if got := o.Field("path"); got != "/static/../.git/config" {
		t.Fatalf("path 必须保持原样（归一化是另一个字段的事），得到 %q", got)
	}
	if o.Field("不存在的字段") != "" {
		t.Fatal("未知字段必须返回空串")
	}
	// query 与 query_raw 是两个字段：一个是规范化匹配视图，一个是原样审计值（AR-31）。
	q := Observation{Query: "file=../etc/passwd", QueryRaw: "file=%2e%2e%2fetc%2fpasswd"}
	if got := q.Field("query"); got != "file=../etc/passwd" {
		t.Fatalf("query 应返回规范化视图，得到 %q", got)
	}
	if got := q.Field("query_raw"); got != "file=%2e%2e%2fetc%2fpasswd" {
		t.Fatalf("query_raw 应返回原样值，得到 %q", got)
	}
}
