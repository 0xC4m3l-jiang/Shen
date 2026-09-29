package main

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// config 是控制台 API 进程的全部运行参数（一律来自环境变量；密钥支持 `*_FILE` 形式）。
type config struct {
	CoreAddr          string
	Listen            string
	DataDir           string
	AllowedOrigins    []string
	TrustedProxies    []netip.Prefix
	TokenSources      []netip.Prefix
	APIToken          string
	IntegrationToken  string // 网关集成令牌（/api/v1/integration/* 专用；与只读令牌分权）
	SecretKey         string // 大模型密钥的加密主密钥；空 = 数据目录自动生成
	CookieSecure      bool
	BootstrapUser     string
	BootstrapPassword string
	SessionIdle       time.Duration
	SessionTTL        time.Duration
	AlertScore        float64
	GeoDB             string
	MaxStreams        int
}

// 默认值。API 默认只监听回环：浏览器经同源的 nginx 前端访问，不直接暴露。
const (
	defaultCoreAddr     = "127.0.0.1:9443"
	defaultListen       = "127.0.0.1:9445"
	defaultTokenSources = "127.0.0.1/32,::1/128"
)

func loadConfig(getenv func(string) string) (config, error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	c := config{
		CoreAddr:      get("SHEN_CORE_ADDR", defaultCoreAddr),
		Listen:        get("SHEN_CONSOLE_LISTEN", defaultListen),
		DataDir:       get("SHEN_CONSOLE_DATA_DIR", defaultDataDir()),
		BootstrapUser: get("SHEN_CONSOLE_BOOTSTRAP_USER", "admin"),
		GeoDB:         get("SHEN_CONSOLE_GEOIP_DB", ""),
	}
	var errs []error
	var err error
	if c.APIToken, err = secret(getenv, "SHEN_CONSOLE_API_TOKEN"); err != nil {
		errs = append(errs, err)
	}
	if c.IntegrationToken, err = secret(getenv, "SHEN_CONSOLE_INTEGRATION_TOKEN"); err != nil {
		errs = append(errs, err)
	}
	if c.BootstrapPassword, err = secret(getenv, "SHEN_CONSOLE_BOOTSTRAP_PASSWORD"); err != nil {
		errs = append(errs, err)
	}
	if c.SecretKey, err = secret(getenv, "SHEN_CONSOLE_SECRET_KEY"); err != nil {
		errs = append(errs, err)
	}
	for _, o := range splitList(get("SHEN_CONSOLE_ALLOWED_ORIGINS", "")) {
		if !strings.HasPrefix(o, "http://") && !strings.HasPrefix(o, "https://") {
			errs = append(errs, fmt.Errorf("SHEN_CONSOLE_ALLOWED_ORIGINS：%q 须以 http:// 或 https:// 开头", o))
			continue
		}
		c.AllowedOrigins = append(c.AllowedOrigins, o)
	}
	if c.TrustedProxies, err = prefixes(get("SHEN_CONSOLE_TRUSTED_PROXIES", "")); err != nil {
		errs = append(errs, fmt.Errorf("SHEN_CONSOLE_TRUSTED_PROXIES：%w", err))
	}
	if c.TokenSources, err = prefixes(get("SHEN_CONSOLE_API_TOKEN_SOURCES", defaultTokenSources)); err != nil {
		errs = append(errs, fmt.Errorf("SHEN_CONSOLE_API_TOKEN_SOURCES：%w", err))
	}
	if c.CookieSecure, err = boolean(get("SHEN_CONSOLE_COOKIE_SECURE", "false")); err != nil {
		errs = append(errs, fmt.Errorf("SHEN_CONSOLE_COOKIE_SECURE：%w", err))
	}
	if c.SessionIdle, err = duration(get("SHEN_CONSOLE_SESSION_IDLE", "30m")); err != nil {
		errs = append(errs, fmt.Errorf("SHEN_CONSOLE_SESSION_IDLE：%w", err))
	}
	if c.SessionTTL, err = duration(get("SHEN_CONSOLE_SESSION_TTL", "12h")); err != nil {
		errs = append(errs, fmt.Errorf("SHEN_CONSOLE_SESSION_TTL：%w", err))
	}
	if c.AlertScore, err = strconv.ParseFloat(get("SHEN_CONSOLE_ALERT_SCORE", "0.9"), 64); err != nil || c.AlertScore <= 0 || c.AlertScore > 1 {
		errs = append(errs, errors.New("SHEN_CONSOLE_ALERT_SCORE 须为 (0,1] 之间的小数"))
	}
	if c.MaxStreams, err = strconv.Atoi(get("SHEN_CONSOLE_MAX_STREAMS", "64")); err != nil || c.MaxStreams <= 0 {
		errs = append(errs, errors.New("SHEN_CONSOLE_MAX_STREAMS 须为正整数"))
	}
	return c, errors.Join(errs...)
}

// secret 读取密钥：`KEY_FILE` 优先（Compose secrets / K8s Secret 挂载），其次 `KEY`。
// 两者同时设置视为配置错误 —— 不猜哪个才是运维想要的。
func secret(getenv func(string) string, key string) (string, error) {
	file, direct := strings.TrimSpace(getenv(key+"_FILE")), getenv(key)
	if file != "" && direct != "" {
		return "", fmt.Errorf("%s 与 %s_FILE 只能设置一个", key, key)
	}
	if file == "" {
		return strings.TrimSpace(direct), nil
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("读取 %s_FILE（%s）失败：%w", key, file, err)
	}
	return strings.TrimSpace(string(raw)), nil
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "shen-console")
	}
	return "shen-console-data"
}

func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func prefixes(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range splitList(raw) {
		if !strings.Contains(item, "/") {
			a, err := netip.ParseAddr(item)
			if err != nil {
				return nil, fmt.Errorf("%q 不是合法地址或网段", item)
			}
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("%q 不是合法网段", item)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

func boolean(raw string) (bool, error) {
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("%q 不是布尔值", raw)
}

func duration(raw string) (time.Duration, error) {
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%q 不是正的时长（如 30m、12h）", raw)
	}
	return d, nil
}
