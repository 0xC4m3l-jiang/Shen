// Package auth 是管控台的**身份与会话**：本地账号（argon2id 口令）、服务端会话、CSRF、
// 登录限流与锁定、首次引导管理员、只读自动化令牌。
//
// 边界：它只回答「你是谁、你的会话是否有效」；「你能做什么」由 rbac 包回答，
// 「这个请求从哪来、带没带 CSRF」由 api 包的中间件执行。
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Params 是 argon2id 的代价参数。
type Params struct {
	Memory  uint32 // KiB
	Time    uint32
	Threads uint8
	SaltLen uint32
	KeyLen  uint32
}

// DefaultParams 取 OWASP 口令存储建议的量级（64 MiB / 3 轮 / 2 线程）：单次校验约数十毫秒，
// 足以让离线爆破代价高昂，又不至于让登录接口本身变成资源放大器（登录另有限流）。
var DefaultParams = Params{Memory: 64 * 1024, Time: 3, Threads: 2, SaltLen: 16, KeyLen: 32}

// 解析出的参数上限：防止被篡改的账号文件用天价参数把校验变成拒绝服务。
const (
	maxMemoryKiB = 1 << 20 // 1 GiB
	maxTime      = 16
	maxThreads   = 16
)

// 口令策略。
const (
	MinPasswordLen = 12
	MaxPasswordLen = 256
)

var (
	// ErrPasswordPolicy 表示口令不满足最小策略。
	ErrPasswordPolicy = errors.New("口令不满足策略：长度 12–256，且不得与用户名相同")
	errBadHash        = errors.New("口令哈希格式无法识别")
)

// HashPassword 生成 PHC 格式的 argon2id 哈希：`$argon2id$v=19$m=..,t=..,p=..$salt$hash`。
func HashPassword(password string, p Params) (string, error) {
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: 生成盐失败：%w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// VerifyPassword 以常量时间比较口令与哈希。
func VerifyPassword(password, encoded string) (bool, error) {
	p, salt, want, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func decodeHash(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, errBadHash
	}
	var p Params
	var threads uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &threads); err != nil {
		return Params{}, nil, nil, errBadHash
	}
	if p.Memory == 0 || p.Memory > maxMemoryKiB || p.Time == 0 || p.Time > maxTime ||
		threads == 0 || threads > maxThreads {
		return Params{}, nil, nil, errBadHash
	}
	p.Threads = uint8(threads)
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return Params{}, nil, nil, errBadHash
	}
	key, err := enc.DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return Params{}, nil, nil, errBadHash
	}
	return p, salt, key, nil
}

// CheckPasswordPolicy 校验最小口令策略（长度按字符计，而非字节）。
func CheckPasswordPolicy(username, password string) error {
	n := utf8.RuneCountInString(password)
	if n < MinPasswordLen || n > MaxPasswordLen {
		return ErrPasswordPolicy
	}
	if strings.EqualFold(strings.TrimSpace(password), strings.TrimSpace(username)) {
		return ErrPasswordPolicy
	}
	return nil
}

// randomToken 返回 n 字节随机数的 base64url 编码（会话 ID / CSRF / 初始口令共用）。
func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: 读取随机数失败：%w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
