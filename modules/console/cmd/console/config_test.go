package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfigDefaults(t *testing.T) {
	c, err := loadConfig(envOf(map[string]string{}))
	if err != nil {
		t.Fatalf("默认配置不应报错：%v", err)
	}
	if c.Listen != "127.0.0.1:9445" || c.CoreAddr != "127.0.0.1:9443" {
		t.Errorf("默认监听应为回环 9445、核心 9443：%+v", c)
	}
	if c.CookieSecure || c.SessionIdle != 30*time.Minute || c.SessionTTL != 12*time.Hour || c.AlertScore != 0.9 {
		t.Errorf("默认值不对：%+v", c)
	}
	if len(c.TokenSources) != 2 {
		t.Errorf("只读令牌默认只允许回环来源：%v", c.TokenSources)
	}
}

func TestLoadConfigParsesListsAndRejectsGarbage(t *testing.T) {
	c, err := loadConfig(envOf(map[string]string{
		"SHEN_CONSOLE_ALLOWED_ORIGINS": "http://127.0.0.1:19444, http://localhost:19444",
		"SHEN_CONSOLE_TRUSTED_PROXIES": "127.0.0.1, 10.0.0.0/8",
		"SHEN_CONSOLE_COOKIE_SECURE":   "true",
		"SHEN_CONSOLE_SESSION_IDLE":    "10m",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.AllowedOrigins) != 2 || len(c.TrustedProxies) != 2 || !c.CookieSecure || c.SessionIdle != 10*time.Minute {
		t.Fatalf("解析结果不对：%+v", c)
	}

	_, err = loadConfig(envOf(map[string]string{
		"SHEN_CONSOLE_ALLOWED_ORIGINS": "evil.example",
		"SHEN_CONSOLE_TRUSTED_PROXIES": "not-a-cidr",
		"SHEN_CONSOLE_COOKIE_SECURE":   "maybe",
		"SHEN_CONSOLE_ALERT_SCORE":     "2",
	}))
	if err == nil {
		t.Fatal("非法配置必须报错")
	}
	for _, key := range []string{"ALLOWED_ORIGINS", "TRUSTED_PROXIES", "COOKIE_SECURE", "ALERT_SCORE"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("错误信息应一次性列出全部问题，缺 %s：%v", key, err)
		}
	}
}

func TestSecretFromFileAndConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("  file-token-value-0123456789  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(envOf(map[string]string{"SHEN_CONSOLE_API_TOKEN_FILE": path}))
	if err != nil || c.APIToken != "file-token-value-0123456789" {
		t.Fatalf("应从 *_FILE 读取并去空白：%q %v", c.APIToken, err)
	}
	if _, err := loadConfig(envOf(map[string]string{
		"SHEN_CONSOLE_API_TOKEN_FILE": path, "SHEN_CONSOLE_API_TOKEN": "x",
	})); err == nil {
		t.Fatal("同时设置 KEY 与 KEY_FILE 应报错")
	}
	if _, err := loadConfig(envOf(map[string]string{"SHEN_CONSOLE_BOOTSTRAP_PASSWORD_FILE": "/nonexistent/file"})); err == nil {
		t.Fatal("*_FILE 指向不存在的文件应报错")
	}
}
