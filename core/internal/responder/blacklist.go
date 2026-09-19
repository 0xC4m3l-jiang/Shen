package responder

import (
	"fmt"
	"regexp"
	"strings"
)

// 本文件实现 AR-22（生成内容黑名单）与 AR-23（分用途长度上限）。
//
// 为什么必须在**生成侧**拦：伪造响应会直接出现在攻击者屏幕上（OH-2 的判据）。
// 一份「泄露真实内网地址」或「自称 AI」的假数据，比不伪装更糟 ——
// 它既暴露了我们在伪装，又可能把真实资产信息交出去。
//
// check-leak:filter 本文件就是过滤器：selfPatterns 必须逐字包含 OH-1 禁用词否则拦不住自曝，且只用于匹配从不拼进响应。

// maxResponseBytes 是面向攻击者的会话响应长度上限（AR-23）。
//
// 真实站点的 API 响应很少超过这个量级；更大的内容会让「一眼假」的概率上升。
const maxResponseBytes = 256 << 10 // 256 KiB

// 泄露类：内网地址、真实主机名、真实文件路径。
var leakPatterns = []*regexp.Regexp{
	// 私网 / 回环地址（\b 避免把 110.0 之类误判）
	regexp.MustCompile(`\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|127\.0\.0\.1)\b`),
	// 内网主机名后缀（真实基础设施命名）
	regexp.MustCompile(`\b\S+\.(?:internal|local|lan|corp)\b`),
	// 真实文件路径
	regexp.MustCompile(`/(?:etc/passwd|etc/shadow|root/|home/[a-z])`),
}

// 自曝类：任何会让攻击者推断「这是假的」的表述。
//
// 与 OH-1 同源 —— 判据始终是「会不会出现在攻击者的屏幕上」（OH-2）。
var selfPatterns = []string{
	// 中文
	"我是 ai", "作为语言模型", "作为 ai", "这是蜜罐", "这是蜜饵", "蜜罐", "蜜饵", "蜜标",
	"投毒", "引流", "欺骗", "诱捕", "会话水印",
	// 英文（OH-1 清单）
	"honeypot", "honey_pot", "decoy", "canary", "canarytoken",
	"mirage", "deception", "deceive", "tarpit", "trapserver",
	"x-deception", "x-honeypot", "x-agent-canary",
	// 模型自述
	"i am an ai", "i'm an ai", "as a language model", "as an ai",
}

// validateContent 校验生成内容（AR-22 / AR-23）。
//
// 命中任一类即返回错误；调用方据此**丢弃该内容**并回落到安全模板，
// **禁止**把不合格内容发给攻击者。
func validateContent(body []byte) error {
	if len(body) > maxResponseBytes {
		return fmt.Errorf("responder: 内容超长（AR-23）：%d > %d 字节", len(body), maxResponseBytes)
	}
	s := string(body)
	low := strings.ToLower(s)
	for _, re := range leakPatterns {
		if m := re.FindString(s); m != "" {
			return fmt.Errorf("responder: 命中泄露类（AR-22）：%q", m)
		}
	}
	for _, p := range selfPatterns {
		if strings.Contains(low, p) {
			return fmt.Errorf("responder: 命中自曝类（AR-22）：%q", p)
		}
	}
	return nil
}
