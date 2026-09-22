package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// DefaultMaskKey 是不配置密钥时使用的面具密钥。
//
// 它**不是秘密**：作用是把「原始身份值」变成跨进程稳定、不可反查的短标识，
// 让观测面（事件 / L4 / 控制台）能按会话分组，同时**不落原始 Cookie 值**。
// 生产部署应当配置自己的密钥（`SHEN_SESSION_MASK_KEY`），使面具与其它系统不互通、
// 且换密钥即可切断历史关联。
const DefaultMaskKey = "shen-session-mask-v1"

// maskLen 是面具长度（字节）。16 个十六进制字符足够按会话分组，也短到不占事件体积。
const maskLen = 8

// Masker 把会话身份值换成**不透明面具**（HMAC-SHA256 截断）。
//
// 为什么必须有这一层：`Extractor` 返回的 ID 可能是业务 Cookie 的原值（优先级①），
// 而观测面（事件、L4、控制台）是**跨进程、可长期保存**的数据 —— 把原值写进去
// 等于把认证材料复制进观测库（`INT-20` 只约束了「不回传客户端、不上行后端」，
// 这里的口径更严：观测面也不落原值）。
//
// 同密钥 + 同输入 ⇒ 同面具（跨进程、跨重启稳定），因此 L4 的会话分组仍然成立；
// 不同密钥 ⇒ 不同面具，轮换密钥即切断关联。
type Masker struct {
	key []byte
}

// NewMasker 构造面具器；key 为空时用 DefaultMaskKey。
func NewMasker(key string) *Masker {
	if strings.TrimSpace(key) == "" {
		key = DefaultMaskKey
	}
	return &Masker{key: []byte(key)}
}

// Mask 返回面具值；空输入返回空串（未识别身份不得伪装成某个具体会话，`NI-1`）。
func (m *Masker) Mask(id string) string {
	if id == "" {
		return ""
	}
	h := hmac.New(sha256.New, m.key)
	_, _ = h.Write([]byte(id))
	return hex.EncodeToString(h.Sum(nil)[:maskLen])
}
