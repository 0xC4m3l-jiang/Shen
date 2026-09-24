package main

import (
	"strings"
	"testing"
	"time"
)

// 本文件钉住进程入口的**环境变量解析纪律**：取值写错 ⇒ **启动失败**（不猜、不静默回落）。
//
// 为什么单独立一例：这些开关里有**安全闸门**（`SHEN_PROXY_SHADOW` 影子模式 `INT-11`、
// `SHEN_PROXY_FORWARD_CREDENTIALS` 凭证剥离 `W4`）—— 把 `yes-please` 猜成 true 或 false
// 等于替运维在安全开关上做决定。审计项 §1.5 记录的"两套纪律"就是在这里统一的。

func TestEnvBoolIsStrict(t *testing.T) {
	cases := []struct {
		raw      string
		fallback bool
		want     bool
		wantErr  bool
	}{
		{"", true, true, false}, // 未设置 ⇒ 默认
		{"", false, false, false},
		{"1", false, true, false},
		{"0", true, false, false},
		{"true", false, true, false},
		{"TRUE", false, true, false},
		{"False", true, false, false},
		{"yes", false, true, false},
		{"no", true, false, false},
		{"on", false, true, false},
		{"off", true, false, false},
		{"  true  ", false, true, false}, // 两侧空白容忍
		{"yes-please", true, false, true},
		{"maybe", false, false, true},
		{"2", false, false, true},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			t.Setenv("SHEN_PROXY_TEST_BOOL", c.raw)
			got, err := envBool("SHEN_PROXY_TEST_BOOL", c.fallback)
			if c.wantErr {
				if err == nil {
					t.Fatalf("%q 应被判为非法（启动失败），却解析成了 %v", c.raw, got)
				}
				if !strings.Contains(err.Error(), "SHEN_PROXY_TEST_BOOL") ||
					!strings.Contains(err.Error(), "true/false") {
					t.Fatalf("报错必须点名变量并给出合法取值，实际：%v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%q 应当合法：%v", c.raw, err)
			}
			if got != c.want {
				t.Fatalf("%q ⇒ %v，期望 %v", c.raw, got, c.want)
			}
		})
	}
}

func TestEnvDurationAndIntAreStrict(t *testing.T) {
	t.Setenv("SHEN_PROXY_TEST_DUR", "3ms")
	if got, err := envDuration("SHEN_PROXY_TEST_DUR", time.Second); err != nil || got != 3*time.Millisecond {
		t.Fatalf("合法时长应解析成功：%v / %v", got, err)
	}
	t.Setenv("SHEN_PROXY_TEST_DUR", "3 millis")
	if _, err := envDuration("SHEN_PROXY_TEST_DUR", time.Second); err == nil {
		t.Fatal("非法时长必须报错（不静默回落）")
	}

	t.Setenv("SHEN_PROXY_TEST_INT", "7")
	if got, err := envInt("SHEN_PROXY_TEST_INT", 1); err != nil || got != 7 {
		t.Fatalf("合法整数应解析成功：%v / %v", got, err)
	}
	t.Setenv("SHEN_PROXY_TEST_INT", "7.5")
	if _, err := envInt("SHEN_PROXY_TEST_INT", 1); err == nil {
		t.Fatal("非法整数必须报错")
	}

	// 负数是**有意支持**的（降级缓存/租约用它表达"关闭/永久"），不能被当成非法。
	t.Setenv("SHEN_PROXY_TEST_DUR", "-1h")
	if got, err := envDuration("SHEN_PROXY_TEST_DUR", 0); err != nil || got != -time.Hour {
		t.Fatalf("负时长必须被接受（关闭/永久语义）：%v / %v", got, err)
	}

	t.Setenv("SHEN_PROXY_TEST_STR", "  hello  ")
	if got := env("SHEN_PROXY_TEST_STR", "fallback"); got != "  hello  " {
		t.Fatalf("env() 原样返回（不 Trim —— 取值里的空白可能是刻意的）：%q", got)
	}
}
