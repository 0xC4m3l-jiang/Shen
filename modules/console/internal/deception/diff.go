package deception

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DiffSummary 生成「各域增删改计数」的一行摘要（版本时间线与审计用）。
func DiffSummary(old, next Dataset) string {
	var parts []string
	add := func(label string, d delta) {
		a, r, c := d.added, d.removed, d.changed
		if a+r+c == 0 {
			return
		}
		var seg []string
		if a > 0 {
			seg = append(seg, fmt.Sprintf("+%d", a))
		}
		if r > 0 {
			seg = append(seg, fmt.Sprintf("-%d", r))
		}
		if c > 0 {
			seg = append(seg, fmt.Sprintf("~%d", c))
		}
		parts = append(parts, label+" "+strings.Join(seg, " "))
	}
	add("蜜罐", keyed(old.Honeypots, next.Honeypots, func(h Honeypot) string { return h.Name }))
	add("诱饵", keyed(old.Decoys, next.Decoys, func(d DecoyAsset) string { return d.ID }))
	add("白名单", listDiff(flatWhitelist(old.Whitelist), flatWhitelist(next.Whitelist)))
	add("黑名单", keyed(old.Blacklist, next.Blacklist, func(b BlackRule) string { return b.ID }))
	inj := keyed(old.Injects, next.Injects, func(i Inject) string { return i.Kind + "\x00" + i.Snippet })
	if old.InjectsProvided != next.InjectsProvided {
		inj.changed++
	}
	add("注入", inj)
	add("绑定", keyed(old.Bindings, next.Bindings, func(b Binding) string { return b.ServiceID }))
	if len(parts) == 0 {
		return "无内容变化"
	}
	return strings.Join(parts, " · ")
}

type delta struct{ added, removed, changed int }

func keyed[T any](old, next []T, key func(T) string) (d delta) {
	om := map[string]string{}
	for _, x := range old {
		om[key(x)] = jsonOf(x)
	}
	nm := map[string]bool{}
	for _, x := range next {
		k := key(x)
		nm[k] = true
		if prev, ok := om[k]; !ok {
			d.added++
		} else if prev != jsonOf(x) {
			d.changed++
		}
	}
	for k := range om {
		if !nm[k] {
			d.removed++
		}
	}
	return d
}

func listDiff(old, next []string) delta {
	return keyed(old, next, func(s string) string { return s })
}

func jsonOf(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

// flatWhitelist 把三段名单拍平成新切片（带段前缀，避免不同段的同名值互相抵消；不共享底层数组）。
func flatWhitelist(w Whitelist) []string {
	out := make([]string, 0, len(w.SourceCIDRs)+len(w.UserAgents)+len(w.PathPrefixes))
	for _, v := range w.SourceCIDRs {
		out = append(out, "c:"+v)
	}
	for _, v := range w.UserAgents {
		out = append(out, "u:"+v)
	}
	for _, v := range w.PathPrefixes {
		out = append(out, "p:"+v)
	}
	return out
}
