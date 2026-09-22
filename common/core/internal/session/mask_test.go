package session

import (
	"strings"
	"testing"
)

// TestMasker_StableAndOpaque 断言面具的三个性质：
//  1. 同密钥 + 同输入 ⇒ 同面具（跨进程/重启稳定，L4 才能按会话分组）；
//  2. 不同输入 ⇒ 不同面具；
//  3. 面具里**不含**原值（观测面不得落原始 Cookie 值）。
func TestMasker_StableAndOpaque(t *testing.T) {
	a := NewMasker("k1")
	b := NewMasker("k1")
	c := NewMasker("k2")

	raw := "sessid=abcdef0123456789"
	if got, want := a.Mask(raw), b.Mask(raw); got != want {
		t.Fatalf("同密钥应得同面具：%q vs %q", got, want)
	}
	if a.Mask(raw) == c.Mask(raw) {
		t.Fatal("换密钥必须换面具（否则轮换无法切断关联）")
	}
	if a.Mask(raw) == a.Mask(raw+"x") {
		t.Fatal("不同输入不应撞成同一面具")
	}
	if strings.Contains(a.Mask(raw), raw) {
		t.Fatal("面具不得包含原值")
	}
	if got := a.Mask(""); got != "" {
		t.Fatalf("空身份必须留空（未识别不得伪装成具体会话），得到 %q", got)
	}
}

// TestMasker_DefaultKey 断言不配置密钥时也有稳定面具（默认值必须写在文档里）。
func TestMasker_DefaultKey(t *testing.T) {
	empty := NewMasker("")
	if got, want := empty.Mask("s-1"), NewMasker(DefaultMaskKey).Mask("s-1"); got != want {
		t.Fatalf("空密钥应等价于默认密钥：%q vs %q", got, want)
	}
	if got := empty.Mask("s-1"); len(got) != maskLen*2 {
		t.Fatalf("面具长度应为 %d 个十六进制字符，得到 %q", maskLen*2, got)
	}
}
