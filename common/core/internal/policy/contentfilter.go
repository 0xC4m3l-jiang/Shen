package policy

import (
	"fmt"
	"regexp"
	"strings"
)

// 本文件是**内容过滤器**：AR-22（生成内容黑名单）。
//
// 为什么它住在 policy 而不是别处：**清单装载是内容进入引擎的唯一入口**
// （`LoadContentManifest` → `SeedContentStore` → 策略载荷 `content_manifest` → 适配器注入）。
// 生成侧的护栏（`analysis/aicap/guardrail`）是第一道闸；这里是**读侧的第二道** ——
// 清单文件可能被手工改过、被中间的构建步骤改过，而注入路径不该信任「来源已经过护栏」这句话。
//
// 它此前只活在 `responder` 里（那条读取路径按 `(会话, 资源)` 取键，与清单链的
// `(profile, 资源, 变体, 版本)` 根本不是同一个键 ⇒ 永远命中不了、也就永远拦不住）。
// 迁移到清单链后，检查落在**真正在用**的那条路上（详见 `docs/plans/2026-09-23-responder-migration.md`）。
//
// check-leak:filter 本文件就是过滤器：selfPatterns 必须逐字包含 OH-1 禁用词否则拦不住自曝，且只用于匹配从不拼进响应。

// leakPatterns 是**泄露类**：内网地址、真实主机名、真实文件路径。
var leakPatterns = []*regexp.Regexp{
	// 私网 / 回环地址（\b 避免把 110.0 之类误判）
	regexp.MustCompile(`\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|127\.0\.0\.1)\b`),
	// 内网主机名后缀（真实基础设施命名）
	regexp.MustCompile(`\b\S+\.(?:internal|local|lan|corp)\b`),
	// 真实文件路径
	regexp.MustCompile(`/(?:etc/passwd|etc/shadow|root/|home/[a-z])`),
}

// selfPatterns 是**自曝类**：任何会让攻击者推断「这是假的」的表述。
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

// ValidateContentBody 校验一段内容体（AR-22）。命中任一类即返回错误。
//
// 调用方**必须**丢弃不合格内容（清单装载：丢该条并记原因；响应生成：回落安全模板），
// **禁止**把不合格内容发给攻击者。
//
// 长度上限**不在这里**：它是**分用途**的（AR-23）—— 清单内容的界在 `MaxContentBodyBytes`，
// 会话响应的界在生成侧；把两个界混成一个会让「谁该被拦」变得说不清。
func ValidateContentBody(body []byte) error {
	s := string(body)
	low := strings.ToLower(s)
	for _, re := range leakPatterns {
		if m := re.FindString(s); m != "" {
			return fmt.Errorf("命中泄露类（AR-22）：%q", m)
		}
	}
	for _, p := range selfPatterns {
		if strings.Contains(low, p) {
			return fmt.Errorf("命中自曝类（AR-22）：%q", p)
		}
	}
	return nil
}
