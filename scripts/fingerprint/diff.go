package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Verdict 是 E2 的结论（判定标准来自 docs/background/notes/pending-experiments.md，**不可事后调整**）。
type Verdict string

const (
	// VerdictIndistinguishable：A / B 不可区分 → 风险 R-1 关闭。
	VerdictIndistinguishable Verdict = "不可区分"
	// VerdictConfigurable：可区分但只差少数**可配置项** → 调齐后重跑。
	VerdictConfigurable Verdict = "可区分（属可配置项，调齐后重跑）"
	// VerdictCannotAlign：可区分且无法通过配置对齐 → 威胁模型 R-1 必须重估整个欺骗命题。
	VerdictCannotAlign Verdict = "不可对齐（威胁模型 R-1 必须重估）"
)

// DiffClass 是差异的分类。分类**刻意保守**：只有我们确实能用配置改的才算「可配置」。
type DiffClass string

const (
	ClassConfigurable DiffClass = "可配置"
	ClassEssential    DiffClass = "本质差异"
	ClassCert         DiffClass = "仅证书"
)

// Difference 是一项差异。
type Difference struct {
	Item   string
	A      string // 真实站
	B      string // 我方栈
	Class  DiffClass
	Advice string
}

// DiffReport 是一次对比的结果。
type DiffReport struct {
	Verdict     Verdict
	Differences []Difference
	Same        []string
}

// Diff 对比两组采集。
//
// 判定逻辑（写死在这里，避免每次凭感觉解释）：
//   - 只要出现**本质差异**（TLS 版本 / 密码套件 / 服务端扩展序列）→ 不可对齐；
//   - 否则只要有**可配置差异** → 可区分（可调齐）；
//   - 都没有（只剩证书形状）→ 不可区分。
func Diff(a, b *Capture) DiffReport {
	var rep DiffReport
	add := func(item, av, bv string, class DiffClass, advice string) {
		if av == bv {
			rep.Same = append(rep.Same, item)
			return
		}
		rep.Differences = append(rep.Differences, Difference{Item: item, A: av, B: bv, Class: class, Advice: advice})
	}

	add("TLS 协商版本", a.TLSVersion, b.TLSVersion, ClassEssential,
		"版本由 TLS 实现决定：要一致通常得换成与真实站同款的实现（如 OpenSSL 系 / BoringSSL 系），或让 L0 用同款代理")
	add("选用的密码套件", a.CipherSuite, b.CipherSuite, ClassEssential,
		"套件由服务端偏好与实现决定：可先在配置里对齐偏好顺序；若实现不提供该套件，只能换实现")
	add("服务端扩展序列", joinUint16(a.ServerExtensions, true), joinUint16(b.ServerExtensions, true), ClassEssential,
		"扩展的「有哪些 + 什么顺序」是 JA3S 的核心，也是最能一眼辨认的地方；多数实现不暴露该顺序 → 通常需要换 TLS 实现")
	add("ALPN", a.ALPN, b.ALPN, ClassConfigurable,
		"可在配置里把 ALPN 列表对齐（我们这一跳要同时支持 h2 / http/1.1 之类的差异）")
	add("会话票据 / 恢复", boolStr(a.SupportsSession), boolStr(b.SupportsSession), ClassConfigurable,
		"会话恢复是可开关的；真实站关掉恢复时我们也应关掉")
	add("OCSP 装订", boolStr(a.OCSPStapled), boolStr(b.OCSPStapled), ClassConfigurable,
		"OCSP stapling 可开可关，对齐即可")
	add("证书链形状", certShape(a), certShape(b), ClassCert,
		"证书内容本身不算差异（部署时换成业务域名证书）；连链的形状（几级 / 哪种 CA）也一起对齐更稳妥")

	hasEssential, hasConfigurable := false, false
	for _, d := range rep.Differences {
		switch d.Class {
		case ClassEssential:
			hasEssential = true
		case ClassConfigurable:
			hasConfigurable = true
		}
	}
	switch {
	case hasEssential:
		rep.Verdict = VerdictCannotAlign
	case hasConfigurable:
		rep.Verdict = VerdictConfigurable
	default:
		rep.Verdict = VerdictIndistinguishable
	}
	sort.Strings(rep.Same)
	return rep
}

// Render 输出给人看的报告（表格 + 结论 + 后续动作）。
func (r DiffReport) Render() string {
	var sb strings.Builder
	sb.WriteString("E2 · TLS 指纹一致性对比（A = 真实站，B = 我方栈）\n")
	sb.WriteString(strings.Repeat("─", 72) + "\n")

	if len(r.Differences) == 0 {
		sb.WriteString("差异：无\n")
	} else {
		sb.WriteString("差异：\n")
		for _, d := range r.Differences {
			fmt.Fprintf(&sb, "  · [%s] %s\n      A: %s\n      B: %s\n      → %s\n",
				d.Class, d.Item, d.A, d.B, d.Advice)
		}
	}
	if len(r.Same) > 0 {
		sb.WriteString("一致：\n  · " + strings.Join(r.Same, "\n  · ") + "\n")
	}

	fmt.Fprintf(&sb, "\n结论：%s\n", r.Verdict)
	switch r.Verdict {
	case VerdictIndistinguishable:
		sb.WriteString("后续动作：风险 R-1 关闭，现有 TLS 方案可用。\n")
	case VerdictConfigurable:
		sb.WriteString("后续动作：按上面每条的「→」调配置 → 重新采集 → 重跑本对比；调齐前不要把「不可区分」写进任何结论。\n")
	case VerdictCannotAlign:
		sb.WriteString("后续动作：**停下**。按 E2 判定标准，威胁模型 R-1 必须重估整个欺骗命题；\n")
		sb.WriteString("          候选出路：① 换与真实站同款的 TLS 实现；② 让 L0（客户 LB）承担 TLS 终结，我们只收明文；\n")
		sb.WriteString("          ③ 若客户站与我们的栈本就同款（例如都用 Caddy/nginx），则差异自然消失。\n")
	}
	return sb.String()
}

func boolStr(b bool) string {
	if b {
		return "支持"
	}
	return "不支持"
}

func certShape(c *Capture) string {
	if len(c.Certificates) == 0 {
		return "（无证书链）"
	}
	leaf := c.Certificates[0]
	self := "公有链"
	if leaf.IsSelfSigned {
		self = "自签"
	}
	return fmt.Sprintf("%s · 链长 %s · %s · %s · SAN %d",
		self, strconv.Itoa(len(c.Certificates)), leaf.PublicKey, leaf.SigAlg, leaf.SANs)
}
