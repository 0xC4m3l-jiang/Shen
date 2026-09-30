package deception

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Issue 是一条校验结论（Field 使用 `域[下标].字段` 形态，前端据此定位高亮）。
type Issue struct {
	Domain Domain `json:"domain"`
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// Report 是一次校验的结果：Errors 阻止保存；Warnings 允许保存但要让人看见。
type Report struct {
	Errors   []Issue `json:"errors"`
	Warnings []Issue `json:"warnings"`
}

// OK 报告是否没有错误。
func (r Report) OK() bool { return len(r.Errors) == 0 }

func (r *Report) err(d Domain, field, format string, a ...any) {
	r.Errors = append(r.Errors, Issue{Domain: d, Field: field, Reason: fmt.Sprintf(format, a...)})
}

func (r *Report) warn(d Domain, field, format string, a ...any) {
	r.Warnings = append(r.Warnings, Issue{Domain: d, Field: field, Reason: fmt.Sprintf(format, a...)})
}

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Validate 对整份数据集做保存前校验（与核心终检同口径，并额外检查绑定的可表达性）。
func Validate(ds Dataset, services []ServiceRef) Report {
	r := Report{Errors: []Issue{}, Warnings: []Issue{}}
	hp := validateHoneypots(&r, ds.Honeypots)
	validateDecoyFields(&r, ds.Decoys, hp)
	validateWhitelist(&r, ds.Whitelist)
	validateBlacklist(&r, ds.Blacklist)
	validateInjects(&r, ds.Injects)
	validateBindings(&r, ds, services)
	if ds.ProjectionRev+1 >= MaxProjection {
		r.err(DomainDecoys, "projection_rev", "投影修订号接近上限 %d —— 请调高部署配置 policy.version", MaxProjection)
	}
	if !r.OK() {
		return r // 字段级错误先修，否则冲突检测会产生大量连带噪声
	}
	r.Errors = append(r.Errors, BindingScopeErrors(ds, services)...)
	proj, warns := Project(ds, services)
	r.Warnings = append(r.Warnings, warns...)
	validateConflicts(&r, ds, proj)
	return r
}

// validateHoneypots 返回「名称 → 是否启用」表供诱饵引用检查。
func validateHoneypots(r *Report, hps []Honeypot) map[string]bool {
	out := map[string]bool{}
	if len(hps) > MaxHoneypots {
		r.err(DomainHoneypots, "honeypots", "蜜罐数量 %d 超过上限 %d", len(hps), MaxHoneypots)
	}
	for i, h := range hps {
		f := fmt.Sprintf("honeypots[%d]", i)
		if !namePattern.MatchString(h.Name) {
			r.err(DomainHoneypots, f+".name", "逻辑名须为小写字母/数字开头，仅含 a-z 0-9 . _ -，≤64 字符")
		} else if _, dup := out[h.Name]; dup {
			r.err(DomainHoneypots, f+".name", "逻辑名 %q 重复", h.Name)
		}
		out[h.Name] = h.Enabled
		if !slices.Contains(HoneypotTypes, h.Type) {
			r.err(DomainHoneypots, f+".type", "类型 %q 未登记（可用：%s）", h.Type, strings.Join(HoneypotTypes, " / "))
		}
		if err := checkAddr(h.Addr); err != nil {
			r.err(DomainHoneypots, f+".addr", "%v", err)
		}
		if utf8.RuneCountInString(h.Description) > 256 {
			r.err(DomainHoneypots, f+".description", "说明最长 256 字符")
		}
	}
	return out
}

func checkAddr(addr string) error {
	a := strings.TrimSpace(addr)
	if a == "" {
		return fmt.Errorf("地址不能为空（host:port 或 http(s)://host:port）")
	}
	if strings.Contains(a, "://") {
		u, err := url.Parse(a)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("地址 %q 不是合法的 http(s) URL", a)
		}
		return nil
	}
	if _, port, err := net.SplitHostPort(a); err != nil || port == "" {
		return fmt.Errorf("地址 %q 不是 host:port", a)
	}
	return nil
}

func validateDecoyFields(r *Report, ds []DecoyAsset, hp map[string]bool) {
	if len(ds) > MaxDecoys {
		r.err(DomainDecoys, "decoys", "诱饵数量 %d 超过上限 %d", len(ds), MaxDecoys)
	}
	seen := map[string]bool{}
	for i, a := range ds {
		f := fmt.Sprintf("decoys[%d]", i)
		if !namePattern.MatchString(a.ID) {
			r.err(DomainDecoys, f+".id", "id 须为小写字母/数字开头，仅含 a-z 0-9 . _ -")
		} else if seen[a.ID] {
			r.err(DomainDecoys, f+".id", "id %q 重复", a.ID)
		}
		seen[a.ID] = true
		if !slices.Contains(DecoyKinds, a.Kind) {
			r.err(DomainDecoys, f+".kind", "形态 %q 未登记（可用：%s）", a.Kind, strings.Join(DecoyKinds, " / "))
		}
		switch {
		case !strings.HasPrefix(a.Path, "/"):
			r.err(DomainDecoys, f+".path", "路径必须以 / 开头")
		case NormalizePath(a.Path) != a.Path:
			r.err(DomainDecoys, f+".path", "路径不是归一化形态（应写作 %q）", NormalizePath(a.Path))
		}
		for j, h := range a.Hosts {
			if _, err := NormalizeHostPattern(h); err != nil {
				r.err(DomainDecoys, fmt.Sprintf("%s.hosts[%d]", f, j), "主机 %q %v", h, err)
			}
		}
		if !a.Enabled {
			continue
		}
		if strings.TrimSpace(a.Backend) == "" {
			r.err(DomainDecoys, f+".backend", "启用中的诱饵必须指定幻境后端（部署未就绪不投放线索）")
		} else if enabled, ok := hp[a.Backend]; !ok {
			r.err(DomainDecoys, f+".backend", "后端 %q 不在蜜罐池中", a.Backend)
		} else if !enabled {
			r.err(DomainDecoys, f+".backend", "后端 %q 未启用 —— 先启用蜜罐或停用本诱饵", a.Backend)
		}
	}
}

func validateWhitelist(r *Report, w Whitelist) {
	if len(w.SourceCIDRs) > MaxListItems || len(w.UserAgents) > MaxListItems || len(w.PathPrefixes) > MaxListItems {
		r.err(DomainWhitelist, "whitelist", "名单条目超过上限 %d", MaxListItems)
	}
	for i, c := range w.SourceCIDRs {
		if _, err := netip.ParsePrefix(strings.TrimSpace(c)); err != nil || strings.TrimSpace(c) != c {
			r.err(DomainWhitelist, fmt.Sprintf("whitelist.source_cidrs[%d]", i), "%q 不是合法的 CIDR（如 10.0.0.0/8）", c)
		}
	}
	for i, ua := range w.UserAgents {
		if strings.TrimSpace(ua) == "" {
			r.err(DomainWhitelist, fmt.Sprintf("whitelist.user_agents[%d]", i), "UA 不能为空")
		}
	}
	for i, p := range w.PathPrefixes {
		if !strings.HasPrefix(p, "/") {
			r.err(DomainWhitelist, fmt.Sprintf("whitelist.path_prefixes[%d]", i), "路径前缀必须以 / 开头")
		} else if p == "/" {
			r.warn(DomainWhitelist, fmt.Sprintf("whitelist.path_prefixes[%d]", i), "前缀 / 只命中根路径（不是全部路径）")
		}
	}
}

func validateBlacklist(r *Report, bl []BlackRule) {
	if len(bl) > MaxListItems {
		r.err(DomainBlacklist, "blacklist", "规则数超过上限 %d", MaxListItems)
	}
	seen := map[string]bool{}
	for i, b := range bl {
		f := fmt.Sprintf("blacklist[%d]", i)
		if !namePattern.MatchString(b.ID) {
			r.err(DomainBlacklist, f+".id", "id 须为小写字母/数字开头，仅含 a-z 0-9 . _ -")
		} else if seen[b.ID] {
			r.err(DomainBlacklist, f+".id", "id %q 重复", b.ID)
		}
		seen[b.ID] = true
		switch {
		case !strings.HasPrefix(b.PathPrefix, "/"):
			r.err(DomainBlacklist, f+".path_prefix", "路径前缀必须以 / 开头")
		case NormalizePath(b.PathPrefix) != b.PathPrefix:
			r.err(DomainBlacklist, f+".path_prefix", "不是归一化形态（应写作 %q）", NormalizePath(b.PathPrefix))
		}
		if strings.TrimSpace(b.Reason) == "" {
			r.err(DomainBlacklist, f+".reason", "必须写明原因（审计与回滚时要能回答「为什么不欺骗」）")
		}
	}
}

func validateInjects(r *Report, in []Inject) {
	if len(in) > MaxInjects {
		r.err(DomainInjects, "injects", "注入规则 %d 条超过上限 %d", len(in), MaxInjects)
	}
	for i, x := range in {
		f := fmt.Sprintf("injects[%d]", i)
		if strings.TrimSpace(x.Snippet) == "" {
			r.err(DomainInjects, f+".snippet", "注入片段不能为空")
		} else if len(x.Snippet) > MaxSnippet {
			r.err(DomainInjects, f+".snippet", "片段超过 %d 字节", MaxSnippet)
		}
		if x.Kind != "" && !slices.Contains(InjectKinds, x.Kind) {
			r.err(DomainInjects, f+".kind", "分类 %q 未登记（可用：%s）", x.Kind, strings.Join(InjectKinds, " / "))
		}
	}
}

func validateBindings(r *Report, ds Dataset, services []ServiceRef) {
	if len(ds.Bindings) > MaxBindings {
		r.err(DomainBindings, "bindings", "绑定数超过上限 %d", MaxBindings)
	}
	svc := map[string]bool{}
	for _, s := range services {
		svc[s.ID] = true
	}
	ids := map[string]bool{}
	for _, a := range ds.Decoys {
		ids[a.ID] = true
	}
	seen := map[string]bool{}
	for i, b := range ds.Bindings {
		f := fmt.Sprintf("bindings[%d]", i)
		if !svc[b.ServiceID] {
			r.err(DomainBindings, f+".service_id", "服务 %q 未登记（或已删除）", b.ServiceID)
		}
		if seen[b.ServiceID] {
			r.err(DomainBindings, f+".service_id", "服务 %q 重复绑定", b.ServiceID)
		}
		seen[b.ServiceID] = true
		for j, id := range b.DecoyIDs {
			if !ids[id] {
				r.err(DomainBindings, fmt.Sprintf("%s.decoy_ids[%d]", f, j), "诱饵 %q 不存在", id)
			}
		}
	}
}

// validateConflicts 在**投影后**的数据上做跨段检查（主机已按绑定语义改写）。
func validateConflicts(r *Report, ds Dataset, p Projection) {
	index := map[string]int{}
	for i, a := range ds.Decoys {
		index[a.ID] = i
	}
	for i := range p.Decoys {
		a := p.Decoys[i]
		if !a.Enabled {
			continue
		}
		f := fmt.Sprintf("decoys[%d]", index[a.ID])
		for j := 0; j < i; j++ {
			b := p.Decoys[j]
			if !b.Enabled || !hostsIntersect(a.Hosts, b.Hosts) || !pathsOverlap(a.Path, b.Path) {
				continue
			}
			kind := "同一路由"
			if a.Path != b.Path {
				kind = "嵌套路由"
			}
			r.err(DomainDecoys, f+".path", "与诱饵 %q（%s）%s且主机相交 —— 谁接管没有明确答案，请区分 Host 或路径", b.ID, b.Path, kind)
		}
		for k, bl := range ds.Blacklist {
			if pathsOverlap(a.Path, bl.PathPrefix) {
				r.err(DomainDecoys, f+".path", "落在禁止欺骗路径 blacklist[%d]（%s）下 —— 先停用诱饵或移除该规则", k, bl.PathPrefix)
			}
		}
		for k, w := range ds.Whitelist.PathPrefixes {
			if pathSegmentPrefix(a.Path, NormalizePath(w)) {
				r.warn(DomainWhitelist, fmt.Sprintf("whitelist.path_prefixes[%d]", k),
					"白名单前缀 %s 覆盖了诱饵 %q 的路径：核心判定面对这些请求直接放行（边缘诱饵路由仍生效）", w, a.ID)
			}
		}
	}
}
