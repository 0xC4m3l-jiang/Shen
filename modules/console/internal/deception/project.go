package deception

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Projection 是核心拉取的响应体（与 common/core/internal/policy.Overlay **手工对齐**）。
type Projection struct {
	Rev             uint64           `json:"rev"`
	Digest          string           `json:"digest"`
	Honeypots       []ProjHoneypot   `json:"honeypots"`
	Decoys          []ProjDecoy      `json:"decoys"`
	Whitelist       Whitelist        `json:"whitelist"`
	Blacklist       []BlackRule      `json:"blacklist"`
	Injects         []Inject         `json:"injects"`
	InjectsProvided bool             `json:"injects_provided"`
	Services        []ProjectedScope `json:"-"` // 仅供 UI 预览（不下发）
}

// ProjHoneypot 是下发给核心的蜜罐项（去掉管控台本地字段）。
type ProjHoneypot struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Addr    string `json:"addr"`
	Enabled bool   `json:"enabled"`
}

// ProjDecoy 是下发给核心的诱饵项（主机已按绑定替换语义改写）。
type ProjDecoy struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Path    string   `json:"path"`
	Hosts   []string `json:"hosts"`
	Content string   `json:"content"`
	Backend string   `json:"backend"`
	Enabled bool     `json:"enabled"`
}

// ProjectedScope 描述一个绑定服务最终吃到的诱饵集（UI 预览用）。
type ProjectedScope struct {
	ServiceID string   `json:"service_id"`
	DecoyIDs  []string `json:"decoy_ids"`
}

// Project 计算下发投影（纯函数）。服务绑定采用**替换语义**：
//
//   - 被绑定的资产：主机 = 自身主机 − 其他绑定服务的主机 ∪ 绑定它的服务的主机；
//   - 未绑定的资产：主机 = 自身主机 − 所有绑定服务的主机（精确相等才减）；
//   - 投影后启用却没有主机的资产被剔除（给出警告）—— 它已经没有可接管的站点。
//
// 通配主机覆盖了绑定服务主机的「不可表达」情形由 BindingScopeErrors 报错（见下）。
func Project(ds Dataset, services []ServiceRef) (Projection, []Issue) {
	ix := indexBindings(ds, services)
	warns := ix.warns
	hostsOf, boundHosts, boundTo := ix.hostsOf, ix.boundHosts, ix.boundTo

	p := Projection{
		Honeypots:       make([]ProjHoneypot, 0, len(ds.Honeypots)),
		Decoys:          make([]ProjDecoy, 0, len(ds.Decoys)),
		Whitelist:       ds.Clone().Whitelist,
		Blacklist:       append([]BlackRule{}, ds.Blacklist...),
		Injects:         append([]Inject{}, ds.Injects...),
		InjectsProvided: ds.InjectsProvided,
	}
	for _, h := range ds.Honeypots {
		p.Honeypots = append(p.Honeypots, ProjHoneypot{Name: h.Name, Type: h.Type, Addr: h.Addr, Enabled: h.Enabled})
	}
	for i, a := range ds.Decoys {
		mine := map[string]bool{}
		for _, sid := range boundTo[a.ID] {
			mine[sid] = true
		}
		set := map[string]bool{}
		for _, raw := range a.Hosts {
			h, err := NormalizeHostPattern(raw)
			if err != nil {
				continue
			}
			if owner, bound := boundHosts[h]; bound && !mine[owner] {
				continue // 该主机属于另一个已绑定的服务：替换语义下不再吃本资产
			}
			set[h] = true
		}
		for sid := range mine {
			for _, h := range hostsOf[sid] {
				set[h] = true
			}
		}
		hosts := make([]string, 0, len(set))
		for h := range set {
			hosts = append(hosts, h)
		}
		sort.Strings(hosts)
		if a.Enabled && len(hosts) == 0 {
			warns = append(warns, Issue{Domain: DomainDecoys, Field: fmt.Sprintf("decoys[%d].hosts", i),
				Reason: fmt.Sprintf("诱饵 %q 的主机都已归属到绑定了其他诱饵集的服务：本次不下发", a.ID)})
			continue
		}
		p.Decoys = append(p.Decoys, ProjDecoy{ID: a.ID, Kind: a.Kind, Path: a.Path, Hosts: hosts,
			Content: a.Content, Backend: a.Backend, Enabled: a.Enabled})
	}
	for _, b := range ds.Bindings {
		p.Services = append(p.Services, ProjectedScope{ServiceID: b.ServiceID, DecoyIDs: append([]string{}, b.DecoyIDs...)})
	}
	p.Digest = digestOf(p)
	return p, warns
}

type bindingIndex struct {
	hostsOf    map[string][]string // 服务 id → 归一化主机
	boundHosts map[string]string   // 已绑定服务的主机 → 服务 id
	boundTo    map[string][]string // 诱饵 id → 绑定它的服务 id
	warns      []Issue
}

func indexBindings(ds Dataset, services []ServiceRef) bindingIndex {
	ix := bindingIndex{hostsOf: map[string][]string{}, boundHosts: map[string]string{}, boundTo: map[string][]string{}}
	for _, s := range services {
		for _, h := range s.Hosts {
			if n, err := NormalizeHostPattern(h); err == nil {
				ix.hostsOf[s.ID] = append(ix.hostsOf[s.ID], n)
			} else {
				ix.warns = append(ix.warns, Issue{Domain: DomainBindings, Field: "bindings",
					Reason: fmt.Sprintf("服务 %s 的域名 %q 无法用于诱饵归属（%v），已忽略", s.Name, h, err)})
			}
		}
	}
	for _, b := range ds.Bindings {
		for _, h := range ix.hostsOf[b.ServiceID] {
			ix.boundHosts[h] = b.ServiceID
		}
		for _, id := range b.DecoyIDs {
			ix.boundTo[id] = append(ix.boundTo[id], b.ServiceID)
		}
	}
	return ix
}

// BindingScopeErrors 找出「未绑定资产的通配主机覆盖了绑定服务的精确主机」：
// 替换语义要求该服务不再吃这条资产，但通配无法"减去"一个具体主机 —— 这是**不可表达**的，
// 因此是**错误**（阻止保存）。
func BindingScopeErrors(ds Dataset, services []ServiceRef) []Issue {
	ix := indexBindings(ds, services)
	boundHosts, boundTo := ix.boundHosts, ix.boundTo
	var out []Issue
	for i, a := range ds.Decoys {
		if !a.Enabled {
			continue
		}
		mine := map[string]bool{}
		for _, sid := range boundTo[a.ID] {
			mine[sid] = true
		}
		for _, raw := range a.Hosts {
			h, err := NormalizeHostPattern(raw)
			if err != nil {
				continue
			}
			for bh, owner := range boundHosts {
				if mine[owner] || bh == h || !hostPatternsIntersect(h, bh) {
					continue
				}
				out = append(out, Issue{Domain: DomainBindings, Field: fmt.Sprintf("decoys[%d].hosts", i),
					Reason: fmt.Sprintf("不可表达：诱饵 %q 的主机 %s 覆盖了已绑定服务的主机 %s —— 请改为精确主机，或把该诱饵加入那个服务的绑定", a.ID, h, bh)})
			}
		}
	}
	return out
}

// digestOf 是投影内容的摘要（不含 Rev/Digest 自身）：同内容必得同摘要。
func digestOf(p Projection) string {
	p.Rev, p.Digest = 0, ""
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
