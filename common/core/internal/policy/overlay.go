package policy

import (
	"errors"
	"fmt"
)

// Overlay 是**管控台拥有的欺骗管控数据**对部署配置的覆盖（方案 B，见
// docs/plan-console-deception-config.md）。
//
// 它只覆盖五个「登记数据」域：蜜罐池 / 诱饵资产 / 白名单 / 禁止欺骗路径 / 注入规则。
// 规则、阈值、灰度、影子、存储等**判定策略与部署参数**永远只来自部署配置 ——
// 管控台不下发判定策略（红线 ①）。
//
// 线格式与管控台 `deception.Projection` **手工对齐**（管控台禁止 import core/internal，ST-3）。
type Overlay struct {
	Honeypots []OverlayHoneypot `json:"honeypots"`
	Decoys    []OverlayDecoy    `json:"decoys"`
	Whitelist OverlayWhitelist  `json:"whitelist"`
	Blacklist []OverlayBlack    `json:"blacklist"`
	Injects   []OverlayInject   `json:"injects"`
	// InjectsProvided 区分「未配置注入规则」（适配器用本地规则）与「显式清空」（没有规则）。
	InjectsProvided bool `json:"injects_provided"`
}

// OverlayHoneypot 对应 honeypots[] 的一项。
type OverlayHoneypot struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Addr    string `json:"addr"`
	Enabled bool   `json:"enabled"`
}

// OverlayDecoy 对应 decoys.assets[] 的一项。
type OverlayDecoy struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Path    string   `json:"path"`
	Hosts   []string `json:"hosts"`
	Content string   `json:"content"`
	Backend string   `json:"backend"`
	Enabled bool     `json:"enabled"`
}

// OverlayWhitelist 对应 whitelist 段。
type OverlayWhitelist struct {
	SourceCIDRs  []string `json:"source_cidrs"`
	UserAgents   []string `json:"user_agents"`
	PathPrefixes []string `json:"path_prefixes"`
}

// OverlayBlack 对应 blacklist[] 的一项。
type OverlayBlack struct {
	ID         string `json:"id"`
	PathPrefix string `json:"path_prefix"`
	Reason     string `json:"reason"`
}

// OverlayInject 对应 injects[] 的一项。
type OverlayInject struct {
	Kind    string `json:"kind"`
	Snippet string `json:"snippet"`
	Marker  string `json:"marker"`
}

// 覆盖数据的规模上限（防御：一份失控的数据集不能把核心拖进 O(n²) 的冲突检测里）。
const (
	maxOverlayDecoys    = 2000
	maxOverlayHoneypots = 500
	maxOverlayList      = 5000
	maxOverlayInjects   = 200
)

// WithOverlay 以装载时的部署配置为基线，套上管控台的覆盖数据，产出一份**全新**的 Loader。
//
// 纯函数：不改 l、不做 I/O、不看时钟。它**完整复用** validate/build —— 管控台侧已经校验过，
// 这里是终检（防止绕过管控台写坏的数据进入判定）。任何一项不合法都返回错误，
// 调用方据此保留 last-good（红线 ③：控制台出错 ≠ 业务受损）。
//
// version 是新快照的策略版本（调用方负责保证单调递增，见 cmd/core 的版本公式）。
func (l *Loader) WithOverlay(o Overlay, version int64) (*Loader, error) {
	if l == nil || len(l.raw) == 0 {
		return nil, errors.New("policy: WithOverlay 需要由 Load 装载的基线")
	}
	if version < 1 {
		return nil, fmt.Errorf("policy: 覆盖后的版本 %d 非法（要求 ≥1）", version)
	}
	if err := o.checkLimits(); err != nil {
		return nil, err
	}
	d, err := decodeConfig(l.raw)
	if err != nil {
		return nil, err // 基线在 Load 时已通过，这里只可能是内部错误
	}
	o.applyTo(d)
	d.Policy.Version = &version

	rules, err := d.validate()
	if err != nil {
		return nil, fmt.Errorf("管控台数据未通过核心终检：%w", err)
	}
	out, err := d.build(rules)
	if err != nil {
		return nil, err
	}
	out.raw = l.raw
	return out, nil
}

// BaseVersion 返回部署配置里写的 policy.version（覆盖版本公式的基数）。
func (l *Loader) BaseVersion() int64 {
	d, err := decodeConfig(l.raw)
	if err != nil || d.Policy == nil || d.Policy.Version == nil {
		return int64(l.snap.Version)
	}
	return *d.Policy.Version
}

func (o Overlay) checkLimits() error {
	switch {
	case len(o.Decoys) > maxOverlayDecoys:
		return fmt.Errorf("policy: 覆盖数据的诱饵资产 %d 条超过上限 %d", len(o.Decoys), maxOverlayDecoys)
	case len(o.Honeypots) > maxOverlayHoneypots:
		return fmt.Errorf("policy: 覆盖数据的蜜罐 %d 个超过上限 %d", len(o.Honeypots), maxOverlayHoneypots)
	case len(o.Whitelist.SourceCIDRs) > maxOverlayList, len(o.Whitelist.UserAgents) > maxOverlayList,
		len(o.Whitelist.PathPrefixes) > maxOverlayList, len(o.Blacklist) > maxOverlayList:
		return fmt.Errorf("policy: 覆盖数据的名单条目超过上限 %d", maxOverlayList)
	case len(o.Injects) > maxOverlayInjects:
		return fmt.Errorf("policy: 覆盖数据的注入规则 %d 条超过上限 %d", len(o.Injects), maxOverlayInjects)
	}
	return nil
}

// applyTo 把覆盖数据写进 configDoc（五个域整体替换，不做逐条合并 —— 所有权非黑即白）。
func (o Overlay) applyTo(d *configDoc) {
	hps := make([]honeypotDoc, 0, len(o.Honeypots))
	for _, h := range o.Honeypots {
		hps = append(hps, honeypotDoc{Name: ptr(h.Name), Type: ptr(h.Type), Addr: ptr(h.Addr), Enabled: ptr(h.Enabled)})
	}
	d.Honeypots = &hps

	assets := make([]decoyAssetDoc, 0, len(o.Decoys))
	for _, a := range o.Decoys {
		hosts := append([]string(nil), a.Hosts...)
		doc := decoyAssetDoc{
			ID: ptr(a.ID), Kind: ptr(a.Kind), Path: ptr(a.Path), Hosts: &hosts,
			Content: ptr(a.Content), Enabled: ptr(a.Enabled),
		}
		if a.Backend != "" {
			doc.Backend = ptr(a.Backend)
		}
		assets = append(assets, doc)
	}
	d.Decoys = &decoysDoc{Assets: &assets}

	d.Whitelist = &whitelistDoc{
		SourceCIDRs:  ptr(nonNil(o.Whitelist.SourceCIDRs)),
		UserAgents:   ptr(nonNil(o.Whitelist.UserAgents)),
		PathPrefixes: ptr(nonNil(o.Whitelist.PathPrefixes)),
	}

	bl := make([]blacklistDoc, 0, len(o.Blacklist))
	for _, b := range o.Blacklist {
		bl = append(bl, blacklistDoc{ID: ptr(b.ID), PathPrefix: ptr(b.PathPrefix), Reason: ptr(b.Reason)})
	}
	d.Blacklist = &bl

	if !o.InjectsProvided {
		d.Injects = nil
		return
	}
	inj := make([]injectDoc, 0, len(o.Injects))
	for _, r := range o.Injects {
		doc := injectDoc{Snippet: ptr(r.Snippet)}
		if r.Kind != "" {
			doc.Kind = ptr(r.Kind)
		}
		if r.Marker != "" {
			doc.Marker = ptr(r.Marker)
		}
		inj = append(inj, doc)
	}
	d.Injects = &inj
}

func ptr[T any](v T) *T { return &v }
