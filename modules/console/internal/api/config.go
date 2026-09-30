package api

import (
	"net/http"
	"strconv"
	"strings"

	"shen/modules/console/internal/deception"
	"shen/modules/console/internal/rbac"
)

// 欺骗管控数据集的管理面：`/api/v1/config/*`（方案 B）。
//
// 边界：这里改的是管控台**拥有的登记数据**（蜜罐池 / 诱饵 / 名单 / 注入 / 服务绑定），
// 经核心终检后生效；判定策略（规则 / 阈值 / 灰度 / 影子）不在此处，永远来自核心部署配置。
// 写接口**按域拆分**，使 RBAC 精确到域；全部带 expected_version（整体乐观锁，冲突 409）。

// domainPerm 是每个可写域所需的权限。
var domainPerm = map[deception.Domain]rbac.Permission{
	deception.DomainHoneypots: rbac.ConfigHoneypot,
	deception.DomainDecoys:    rbac.ConfigDeception,
	deception.DomainWhitelist: rbac.ConfigDeception,
	deception.DomainBlacklist: rbac.ConfigDeception,
	deception.DomainInjects:   rbac.ConfigDeception,
	deception.DomainBindings:  rbac.ConfigDeception,
}

func (s *Server) configRoutes(route func(string, rbac.Permission, http.HandlerFunc)) {
	route("GET /api/v1/config/dataset", rbac.ConfigRead, s.withConfig(s.handleGetDataset))
	route("POST /api/v1/config/validate", rbac.ConfigRead, s.withConfig(s.handleValidateDraft))
	route("GET /api/v1/config/versions", rbac.ConfigRead, s.withConfig(s.handleVersions))
	route("GET /api/v1/config/versions/{n}", rbac.ConfigRead, s.withConfig(s.handleVersionAt))
	route("POST /api/v1/config/rollback", rbac.ConfigAdmin, s.withConfig(s.handleRollback))
	route("GET /api/v1/config/templates", rbac.ConfigRead, s.withConfig(s.handleTemplates))
	route("GET /api/v1/config/sync", rbac.ConfigRead, s.withConfig(s.handleSyncStatus))
	for _, d := range deception.Domains {
		route("PUT /api/v1/config/"+string(d), domainPerm[d], s.withConfig(s.handlePutDomain(d)))
	}
}

// withConfig：未装配数据集时统一 503（接口存在，但本实例未启用）。
func (s *Server) withConfig(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Deception == nil || s.cfg.Sync == nil {
			writeError(w, http.StatusServiceUnavailable, "config_disabled", "欺骗管控数据集未启用")
			return
		}
		h(w, r)
	}
}

// serviceRefs 把登记表转成投影 / 校验所需的服务视图。
func (s *Server) serviceRefs() []deception.ServiceRef {
	list := s.registry.List()
	out := make([]deception.ServiceRef, 0, len(list))
	for _, svc := range list {
		out = append(out, deception.ServiceRef{ID: svc.ID, Name: svc.Name, Hosts: svc.Hosts, Enabled: svc.Enabled})
	}
	return out
}

// draftBody 是写 / 预检的请求体：每个域可选；写接口只接受「对应域」这一项。
type draftBody struct {
	ExpectedVersion *uint64                 `json:"expected_version"`
	Honeypots       *[]deception.Honeypot   `json:"honeypots"`
	Decoys          *[]deception.DecoyAsset `json:"decoys"`
	Whitelist       *deception.Whitelist    `json:"whitelist"`
	Blacklist       *[]deception.BlackRule  `json:"blacklist"`
	Injects         *[]deception.Inject     `json:"injects"`
	InjectsProvided *bool                   `json:"injects_provided"`
	Bindings        *[]deception.Binding    `json:"bindings"`
}

// present 返回请求体里出现的域。
func (b draftBody) present() []deception.Domain {
	var out []deception.Domain
	if b.Honeypots != nil {
		out = append(out, deception.DomainHoneypots)
	}
	if b.Decoys != nil {
		out = append(out, deception.DomainDecoys)
	}
	if b.Whitelist != nil {
		out = append(out, deception.DomainWhitelist)
	}
	if b.Blacklist != nil {
		out = append(out, deception.DomainBlacklist)
	}
	if b.Injects != nil || b.InjectsProvided != nil {
		out = append(out, deception.DomainInjects)
	}
	if b.Bindings != nil {
		out = append(out, deception.DomainBindings)
	}
	return out
}

// applyTo 把请求体里出现的域写进 d（字符串去空白、主机小写）。
func (b draftBody) applyTo(d *deception.Dataset) {
	if b.Honeypots != nil {
		d.Honeypots = make([]deception.Honeypot, 0, len(*b.Honeypots))
		for _, h := range *b.Honeypots {
			h.Name, h.Type, h.Addr = strings.TrimSpace(h.Name), strings.TrimSpace(h.Type), strings.TrimSpace(h.Addr)
			h.Description = strings.TrimSpace(h.Description)
			d.Honeypots = append(d.Honeypots, h)
		}
	}
	if b.Decoys != nil {
		d.Decoys = make([]deception.DecoyAsset, 0, len(*b.Decoys))
		for _, a := range *b.Decoys {
			a.ID, a.Kind, a.Path = strings.TrimSpace(a.ID), strings.TrimSpace(a.Kind), strings.TrimSpace(a.Path)
			a.Content, a.Backend, a.Note = strings.TrimSpace(a.Content), strings.TrimSpace(a.Backend), strings.TrimSpace(a.Note)
			hosts := make([]string, 0, len(a.Hosts))
			for _, h := range a.Hosts {
				if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
					hosts = append(hosts, h)
				}
			}
			a.Hosts = hosts
			d.Decoys = append(d.Decoys, a)
		}
	}
	if b.Whitelist != nil {
		d.Whitelist = deception.Whitelist{SourceCIDRs: trimAll(b.Whitelist.SourceCIDRs),
			UserAgents: trimAll(b.Whitelist.UserAgents), PathPrefixes: trimAll(b.Whitelist.PathPrefixes)}
	}
	if b.Blacklist != nil {
		d.Blacklist = make([]deception.BlackRule, 0, len(*b.Blacklist))
		for _, x := range *b.Blacklist {
			x.ID, x.PathPrefix, x.Reason = strings.TrimSpace(x.ID), strings.TrimSpace(x.PathPrefix), strings.TrimSpace(x.Reason)
			d.Blacklist = append(d.Blacklist, x)
		}
	}
	if b.Injects != nil {
		d.Injects = append([]deception.Inject{}, *b.Injects...)
		d.InjectsProvided = true
	}
	if b.InjectsProvided != nil {
		d.InjectsProvided = *b.InjectsProvided
		if !d.InjectsProvided {
			d.Injects = []deception.Inject{}
		}
	}
	if b.Bindings != nil {
		d.Bindings = append([]deception.Binding{}, *b.Bindings...)
	}
}

func trimAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (s *Server) datasetView(ds deception.Dataset) map[string]any {
	svcs := s.serviceRefs()
	proj, _ := deception.Project(ds, svcs)
	return map[string]any{
		"dataset":    ds,
		"report":     deception.Validate(ds, svcs),
		"projection": map[string]any{"rev": ds.ProjectionRev, "digest": proj.Digest, "decoys": proj.Decoys, "scopes": proj.Services},
		"sync":       s.cfg.Sync.Status(ds.ProjectionRev),
		"seed":       s.cfg.Seed,
		"services":   svcs,
	}
}

func (s *Server) handleGetDataset(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.datasetView(s.cfg.Deception.Get()))
}

// handleValidateDraft 只校验不保存（UI 冲突预检面板，输入防抖后调用）。
func (s *Server) handleValidateDraft(w http.ResponseWriter, r *http.Request) {
	var body draftBody
	if !decodeJSON(w, r, &body) {
		return
	}
	next := s.cfg.Deception.Get()
	body.applyTo(&next)
	svcs := s.serviceRefs()
	proj, _ := deception.Project(next, svcs)
	writeJSON(w, http.StatusOK, map[string]any{"report": deception.Validate(next, svcs),
		"projection": map[string]any{"decoys": proj.Decoys, "scopes": proj.Services, "digest": proj.Digest}})
}

// handlePutDomain 保存一个域：校验 → 乐观锁保存 → 推进投影修订号 → 审计。
func (s *Server) handlePutDomain(d deception.Domain) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body draftBody
		if !decodeJSON(w, r, &body) {
			return
		}
		if got := body.present(); len(got) != 1 || got[0] != d || body.ExpectedVersion == nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "请求体必须且只能包含 expected_version 与「"+string(d)+"」一个域")
			return
		}
		p := principalOf(r)
		cur := s.cfg.Deception.Get()
		if cur.Version != *body.ExpectedVersion {
			s.fail(w, r, deception.ErrConflict)
			return
		}
		next := cur.Clone()
		body.applyTo(&next)
		s.commitDataset(w, r, p, cur, next, *body.ExpectedVersion, "deception.dataset.saved", string(d))
	}
}

// commitDataset 是保存与回滚共用的提交路径。
func (s *Server) commitDataset(w http.ResponseWriter, r *http.Request, p principal,
	cur, next deception.Dataset, expected uint64, action, target string) {
	svcs := s.serviceRefs()
	report := deception.Validate(next, svcs)
	if !report.OK() {
		s.record(r, p, "deception.dataset.validate_failed", target, "rejected",
			strconv.Itoa(len(report.Errors))+" 个错误："+report.Errors[0].Field+" "+report.Errors[0].Reason)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "校验未通过：" + report.Errors[0].Field + " " + report.Errors[0].Reason, "code": "validation_failed",
			"report": report,
		})
		return
	}
	summary := deception.DiffSummary(cur, next)
	if action == "deception.dataset.rollback" {
		summary = target + "（" + summary + "）"
	}
	saved, err := s.cfg.Deception.Save(expected, p.Username, summary, next)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	proj, _ := deception.Project(saved, svcs)
	if rev, _, err := s.cfg.Deception.EnsureProjection(proj.Digest); err != nil {
		s.cfg.Logf("console: 推进投影修订号失败：%v", err)
	} else {
		s.cfg.Sync.NoteRev(rev)
		saved.ProjectionRev = rev
	}
	s.record(r, p, action, target, "ok", "v"+strconv.FormatUint(saved.Version, 10)+" · "+summary)
	writeJSON(w, http.StatusOK, s.datasetView(s.cfg.Deception.Get()))
}

func (s *Server) handleVersions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"versions": s.cfg.Deception.History()})
}

func (s *Server) handleVersionAt(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.ParseUint(r.PathValue("n"), 10, 64)
	if err != nil || n == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "版本号必须是正整数")
		return
	}
	ds, err := s.cfg.Deception.VersionAt(n)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dataset": ds})
}

// handleRollback 把内容恢复为历史版本（作为新版本发布，版本号继续递增）。
func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	var body struct {
		To              uint64 `json:"to"`
		ExpectedVersion uint64 `json:"expected_version"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	cur := s.cfg.Deception.Get()
	if cur.Version != body.ExpectedVersion {
		s.fail(w, r, deception.ErrConflict)
		return
	}
	old, err := s.cfg.Deception.VersionAt(body.To)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	next := cur.Clone()
	next.Honeypots, next.Decoys, next.Whitelist = old.Honeypots, old.Decoys, old.Whitelist
	next.Blacklist, next.Injects, next.InjectsProvided, next.Bindings = old.Blacklist, old.Injects, old.InjectsProvided, old.Bindings
	s.commitDataset(w, r, principalOf(r), cur, next, body.ExpectedVersion,
		"deception.dataset.rollback", "回滚到 v"+strconv.FormatUint(body.To, 10))
}

func (s *Server) handleTemplates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"templates": s.cfg.Templates,
		"enums": map[string]any{"decoy_kinds": deception.DecoyKinds, "honeypot_types": deception.HoneypotTypes,
			"inject_kinds": deception.InjectKinds}})
}

func (s *Server) handleSyncStatus(w http.ResponseWriter, _ *http.Request) {
	ds := s.cfg.Deception.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"sync": s.cfg.Sync.Status(ds.ProjectionRev), "alerts": s.cfg.Sync.Alerts(ds, s.cfg.Seed),
		"seed": s.cfg.Seed, "dataset_version": ds.Version, "initialized": ds.Initialized,
	})
}

// systemAlerts 供告警页 / 总览复用（未启用数据集时为空）。
func (s *Server) systemAlerts() []deception.SystemAlert {
	if s.cfg.Deception == nil || s.cfg.Sync == nil {
		return []deception.SystemAlert{}
	}
	return s.cfg.Sync.Alerts(s.cfg.Deception.Get(), s.cfg.Seed)
}

// configSyncSummary 供总览 / 系统状态复用（未启用时返回 nil）。
func (s *Server) configSyncSummary() *deception.SyncStatus {
	if s.cfg.Deception == nil || s.cfg.Sync == nil {
		return nil
	}
	st := s.cfg.Sync.Status(s.cfg.Deception.Get().ProjectionRev)
	return &st
}
