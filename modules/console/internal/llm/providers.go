package llm

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"shen/modules/console/internal/filestore"
)

// 领域错误（api 包映射成 HTTP 状态码）。
var (
	ErrNotFound  = errors.New("大模型提供方不存在")
	ErrDuplicate = errors.New("同名提供方已存在")
	ErrConflict  = errors.New("数据已被他人修改，请刷新后重试")
	ErrDisabled  = errors.New("该提供方已停用")
	ErrFull      = errors.New("提供方数量已达上限")
	ErrBusy      = errors.New("上一条消息仍在生成中，请稍候")
	ErrNoModel   = errors.New("该提供方没有这个模型")
)

// ValidationError 是字段校验失败。
type ValidationError struct{ Field, Msg string }

func (e *ValidationError) Error() string { return e.Field + "：" + e.Msg }

const (
	maxProviders = 32
	maxModels    = 20
)

// TestResult 是最近一次连通性测试的结果。
type TestResult struct {
	At        time.Time `json:"at"`
	OK        bool      `json:"ok"`
	Model     string    `json:"model"`
	LatencyMs int64     `json:"latency_ms"`
	Error     string    `json:"error,omitempty"`
	Reply     string    `json:"reply,omitempty"` // 模型回的前几十个字（证明真的通了）
}

// Provider 是落盘记录（含密文）。**永远不要**把它直接序列化给前端 —— 用 View()。
type Provider struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	BaseURL      string      `json:"base_url"`
	Models       []string    `json:"models"`
	DefaultModel string      `json:"default_model"`
	KeySealed    string      `json:"key_sealed"`
	KeyHint      string      `json:"key_hint"`
	Enabled      bool        `json:"enabled"`
	Note         string      `json:"note,omitempty"`
	Version      uint64      `json:"version"`
	CreatedBy    string      `json:"created_by"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	LastTest     *TestResult `json:"last_test,omitempty"`
}

// ProviderView 是对前端的视图：没有密文，只有脱敏提示。
type ProviderView struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	BaseURL      string      `json:"base_url"`
	Models       []string    `json:"models"`
	DefaultModel string      `json:"default_model"`
	KeyHint      string      `json:"key_hint"`
	Enabled      bool        `json:"enabled"`
	Note         string      `json:"note,omitempty"`
	Version      uint64      `json:"version"`
	CreatedBy    string      `json:"created_by"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	LastTest     *TestResult `json:"last_test,omitempty"`
}

// View 返回脱敏视图（深拷贝切片，调用方随便改都不影响存储）。
func (p Provider) View() ProviderView {
	return ProviderView{ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, Models: append([]string(nil), p.Models...),
		DefaultModel: p.DefaultModel, KeyHint: p.KeyHint, Enabled: p.Enabled, Note: p.Note, Version: p.Version,
		CreatedBy: p.CreatedBy, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, LastTest: p.LastTest}
}

// Input 是创建 / 更新的请求体。APIKey 为空表示「不改密钥」（仅更新时合法）。
type Input struct {
	Name         string   `json:"name"`
	BaseURL      string   `json:"base_url"`
	APIKey       string   `json:"api_key"`
	Models       []string `json:"models"`
	DefaultModel string   `json:"default_model"`
	Enabled      bool     `json:"enabled"`
	Note         string   `json:"note"`
	Version      uint64   `json:"version"`
}

// ProviderStore 是提供方登记表：读多写少，RWMutex；每次变更整表原子落盘。
type ProviderStore struct {
	mu    sync.RWMutex
	path  string
	vault *Vault
	items map[string]*Provider
	now   func() time.Time
}

type providerFile struct {
	Providers []*Provider `json:"providers"`
}

// OpenProviders 打开（或新建）登记表。
func OpenProviders(path string, vault *Vault, now func() time.Time) (*ProviderStore, error) {
	if now == nil {
		now = time.Now
	}
	s := &ProviderStore{path: path, vault: vault, items: map[string]*Provider{}, now: now}
	var f providerFile
	if _, err := filestore.ReadJSON(path, &f); err != nil {
		return nil, err
	}
	for _, p := range f.Providers {
		if p != nil && p.ID != "" {
			s.items[p.ID] = p
		}
	}
	return s, nil
}

func (s *ProviderStore) persistLocked() error {
	out := providerFile{Providers: make([]*Provider, 0, len(s.items))}
	for _, p := range s.items {
		out.Providers = append(out.Providers, p)
	}
	sort.Slice(out.Providers, func(i, j int) bool { return out.Providers[i].CreatedAt.Before(out.Providers[j].CreatedAt) })
	return filestore.WriteJSON(s.path, out)
}

// List 返回全部提供方的脱敏视图（按创建时间）。
func (s *ProviderStore) List() []ProviderView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ProviderView, 0, len(s.items))
	for _, p := range s.items {
		out = append(out, p.View())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Get 返回一份拷贝。
func (s *ProviderStore) Get(id string) (Provider, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.items[id]
	if !ok {
		return Provider{}, false
	}
	cp := *p
	cp.Models = append([]string(nil), p.Models...)
	return cp, true
}

// Credentials 解密取得调用所需的地址与密钥（只在发起调用的那一刻用）。
func (s *ProviderStore) Credentials(id string) (Provider, string, error) {
	p, ok := s.Get(id)
	if !ok {
		return Provider{}, "", ErrNotFound
	}
	key, err := s.vault.Open(p.KeySealed, p.ID)
	if err != nil {
		return Provider{}, "", err
	}
	return p, key, nil
}

// Create 登记新提供方。
func (s *ProviderStore) Create(in Input, actor string) (ProviderView, error) {
	if strings.TrimSpace(in.APIKey) == "" {
		return ProviderView{}, &ValidationError{"api_key", "新登记的提供方必须填写 API Key"}
	}
	clean, err := normalize(in)
	if err != nil {
		return ProviderView{}, err
	}
	id, err := newID("llm")
	if err != nil {
		return ProviderView{}, err
	}
	sealed, err := s.vault.Seal(strings.TrimSpace(in.APIKey), id)
	if err != nil {
		return ProviderView{}, err
	}
	now := s.now().UTC()
	p := &Provider{ID: id, Name: clean.Name, BaseURL: clean.BaseURL, Models: clean.Models, DefaultModel: clean.DefaultModel,
		KeySealed: sealed, KeyHint: Hint(in.APIKey), Enabled: clean.Enabled, Note: clean.Note, Version: 1,
		CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) >= maxProviders {
		return ProviderView{}, ErrFull
	}
	if s.nameTakenLocked(p.Name, "") {
		return ProviderView{}, ErrDuplicate
	}
	s.items[id] = p
	if err := s.persistLocked(); err != nil {
		delete(s.items, id)
		return ProviderView{}, err
	}
	return p.View(), nil
}

// Update 修改提供方（乐观并发：Version 必须等于当前版本）。APIKey 为空则保留原密钥。
func (s *ProviderStore) Update(id string, in Input) (ProviderView, error) {
	clean, err := normalize(in)
	if err != nil {
		return ProviderView{}, err
	}
	var sealed, hint string
	if key := strings.TrimSpace(in.APIKey); key != "" {
		if sealed, err = s.vault.Seal(key, id); err != nil {
			return ProviderView{}, err
		}
		hint = Hint(key)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.items[id]
	if !ok {
		return ProviderView{}, ErrNotFound
	}
	if in.Version != cur.Version {
		return ProviderView{}, ErrConflict
	}
	if s.nameTakenLocked(clean.Name, id) {
		return ProviderView{}, ErrDuplicate
	}
	prev := *cur
	cur.Name, cur.BaseURL, cur.Models, cur.DefaultModel = clean.Name, clean.BaseURL, clean.Models, clean.DefaultModel
	cur.Enabled, cur.Note = clean.Enabled, clean.Note
	if sealed != "" {
		cur.KeySealed, cur.KeyHint = sealed, hint
		cur.LastTest = nil // 换了密钥，旧的测试结论不再成立
	}
	if prev.BaseURL != cur.BaseURL {
		cur.LastTest = nil
	}
	cur.Version++
	cur.UpdatedAt = s.now().UTC()
	if err := s.persistLocked(); err != nil {
		*cur = prev
		return ProviderView{}, err
	}
	return cur.View(), nil
}

// Delete 删除提供方。
func (s *ProviderStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.items[id]
	if !ok {
		return ErrNotFound
	}
	delete(s.items, id)
	if err := s.persistLocked(); err != nil {
		s.items[id] = cur
		return err
	}
	return nil
}

// RecordTest 记下最近一次测试结果（不改版本号：它是观测，不是配置变更）。
func (s *ProviderStore) RecordTest(id string, res TestResult) (ProviderView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.items[id]
	if !ok {
		return ProviderView{}, ErrNotFound
	}
	cur.LastTest = &res
	if err := s.persistLocked(); err != nil {
		return ProviderView{}, err
	}
	return cur.View(), nil
}

func (s *ProviderStore) nameTakenLocked(name, except string) bool {
	for id, p := range s.items {
		if id != except && strings.EqualFold(p.Name, name) {
			return true
		}
	}
	return false
}

// normalize 校验并规范化输入（不含密钥）。
func normalize(in Input) (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n == 0 || n > 40 {
		return in, &ValidationError{"name", "长度 1–40"}
	}
	base, err := NormalizeBaseURL(in.BaseURL)
	if err != nil {
		return in, err
	}
	in.BaseURL = base
	seen := map[string]bool{}
	var models []string
	for _, m := range in.Models {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			continue
		}
		if len(m) > 80 || strings.ContainsAny(m, " \t\r\n/\\?#") {
			return in, &ValidationError{"models", fmt.Sprintf("模型名 %q 不合法", m)}
		}
		seen[m] = true
		models = append(models, m)
	}
	if len(models) == 0 || len(models) > maxModels {
		return in, &ValidationError{"models", fmt.Sprintf("至少 1 个、至多 %d 个模型", maxModels)}
	}
	in.Models = models
	in.DefaultModel = strings.TrimSpace(in.DefaultModel)
	if in.DefaultModel == "" {
		in.DefaultModel = models[0]
	}
	if !seen[in.DefaultModel] {
		return in, &ValidationError{"default_model", "默认模型必须在模型列表里"}
	}
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > 200 {
		return in, &ValidationError{"note", "至多 200 字"}
	}
	return in, nil
}

// NormalizeBaseURL 校验接口地址：必须 https；http 只允许本机回环与 host.docker.internal
// （本地模型如 Ollama / vLLM）。不允许带凭据、查询串、片段 —— 密钥只走 Authorization 头。
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", &ValidationError{"base_url", "不是合法地址（例：https://api.deepseek.com）"}
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", &ValidationError{"base_url", "不能包含账号、查询参数或 #片段"}
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !localHost(u.Hostname()) {
			return "", &ValidationError{"base_url", "明文 http 只允许本机模型（127.0.0.1 / localhost / host.docker.internal）；公网服务必须 https"}
		}
	default:
		return "", &ValidationError{"base_url", "只支持 https（本机模型可用 http）"}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.Scheme + "://" + u.Host + u.Path, nil
}

func localHost(h string) bool {
	h = strings.ToLower(h)
	if h == "localhost" || h == "host.docker.internal" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func newID(prefix string) (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("llm: 生成 ID 失败：%w", err)
	}
	return prefix + "-" + hex.EncodeToString(b), nil
}
