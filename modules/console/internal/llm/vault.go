// Package llm 是管控台的**大模型分析**能力：登记模型提供方（密钥加密存储）、连通性测试、
// token 用量记账、以及「选定流量 → 多轮对话深度分析」。
//
// 边界（务必遵守）：
//   - 只做**分析与问答**：模型输出是给人看的文字，不回灌策略、不下发处置、没有任何工具调用（与 AR-32 同一精神）；
//   - 出站只发生在**用户显式操作**时（测试连通 / 发送消息），且只连管理员登记的地址；
//   - 密钥只在内存里短暂解密，落盘是 AES-256-GCM 密文，接口永远只返回脱敏提示（sk-****末4位）；
//   - 流量字段来自攻击者（UA / 路径可被任意构造）：进提示词时明确标注为**不可信数据**。
package llm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"shen/modules/console/internal/filestore"
)

// MasterKeyFile 是自动生成的主密钥文件名（数据卷内，0600）。
const MasterKeyFile = "llm-master.key"

// Vault 用 AES-256-GCM 加解密提供方密钥。
//
// 主密钥来源（优先级从高到低）：
//  1. 显式配置（SHEN_CONSOLE_SECRET_KEY / _FILE）—— 生产推荐，经 secrets 注入，与数据卷分离存放；
//  2. 数据目录下的 llm-master.key —— 首次启动自动生成。便利但**与密文同卷**：
//     拿到整个卷的人能解密，所以生产必须走 1。
type Vault struct {
	aead      cipher.AEAD
	generated bool // 主密钥是否为本次自动生成（启动日志提示用）
}

// OpenVault 按上述优先级取得主密钥。configured 非空时用 SHA-256 派生出 32 字节（任意长度口令都可用，
// 但至少 32 个字符 —— 太短的主密钥等于没有）。
func OpenVault(dataDir, configured string) (*Vault, error) {
	var key []byte
	generated := false
	if configured = strings.TrimSpace(configured); configured != "" {
		if len(configured) < 32 {
			return nil, errors.New("llm: SHEN_CONSOLE_SECRET_KEY 过短（至少 32 个字符）")
		}
		sum := sha256.Sum256([]byte(configured))
		key = sum[:]
	} else {
		path := filepath.Join(dataDir, MasterKeyFile)
		raw, err := os.ReadFile(path)
		switch {
		case err == nil:
			if key, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw))); err != nil || len(key) != 32 {
				return nil, fmt.Errorf("llm: 主密钥文件 %s 已损坏（应为 32 字节 base64）", path)
			}
		case errors.Is(err, fs.ErrNotExist):
			key = make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return nil, fmt.Errorf("llm: 生成主密钥失败：%w", err)
			}
			if err := filestore.WriteBytes(path, []byte(base64.StdEncoding.EncodeToString(key)+"\n")); err != nil {
				return nil, err
			}
			generated = true
		default:
			return nil, fmt.Errorf("llm: 读取主密钥失败：%w", err)
		}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead, generated: generated}, nil
}

// Generated 报告主密钥是否为本次自动生成。
func (v *Vault) Generated() bool { return v.generated }

// Seal 加密明文，返回 base64(nonce || 密文)。associated 绑定到提供方 ID：
// 把 A 的密文挪给 B 会解密失败（防止篡改账号文件时「换绑」密钥）。
func (v *Vault) Seal(plaintext, associated string) (string, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("llm: 生成随机数失败：%w", err)
	}
	out := v.aead.Seal(nonce, nonce, []byte(plaintext), []byte(associated))
	return base64.StdEncoding.EncodeToString(out), nil
}

// Open 解密。失败不回显任何密文或明文片段。
func (v *Vault) Open(sealed, associated string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < v.aead.NonceSize() {
		return "", errors.New("llm: 密钥密文格式错误")
	}
	n := v.aead.NonceSize()
	plain, err := v.aead.Open(nil, raw[:n], raw[n:], []byte(associated))
	if err != nil {
		return "", errors.New("llm: 密钥解密失败（主密钥已更换或文件被改动）")
	}
	return string(plain), nil
}

// Hint 返回密钥的脱敏提示：保留前缀（如 sk-）与末 4 位，中间一律 ****。
// 短于 8 位的密钥只显示 ****（末 4 位在那种长度下泄露比例太高）。
func Hint(key string) string {
	key = strings.TrimSpace(key)
	if len(key) < 8 {
		return "****"
	}
	prefix := ""
	if i := strings.Index(key, "-"); i > 0 && i <= 4 {
		prefix = key[:i+1]
	}
	return prefix + "****" + key[len(key)-4:]
}
