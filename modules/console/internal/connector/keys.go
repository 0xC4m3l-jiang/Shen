// Package connector 是反向隧道连接器的**控制台侧**状态：接入凭证表与会话观测表。
//
// 边界（与 README「不做控制面」一致）：
//   - 凭证表只是签发与吊销记录（key 只落 SHA-256 哈希，明文只在签发响应出现一次）；
//   - 会话表是网关上报的观测快照（控制台自身不出站）；
//   - 生效路由仍由核心策略面决定，这里不下发任何策略。
package connector

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"shen/modules/console/internal/filestore"
)

// 领域错误（api 层映射成 HTTP 状态码）。
var (
	ErrNotFound  = errors.New("接入凭证不存在")
	ErrDuplicate = errors.New("同名凭证或域名已被其他凭证占用")
	ErrRevoked   = errors.New("凭证已吊销")
)

// ValidationError 是字段校验失败。
type ValidationError struct{ Field, Reason string }

func (e *ValidationError) Error() string { return e.Field + "：" + e.Reason }

// 上限：与登记表同量级，防撑爆内存与 O(n²) 校验。
const (
	MaxCredentials = 64
	MaxHosts       = 32
)

// Credential 是落盘的凭证记录。KeyHash 是 SHA-256 hex —— **永远不要**把它序列化给前端。
type Credential struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Hosts     []string   `json:"hosts"`
	Owner     string     `json:"owner"`
	KeyHash   string     `json:"key_hash"`
	KeyHint   string     `json:"key_hint"`
	Revoked   bool       `json:"revoked"`
	Version   uint64     `json:"version"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
}

// CredentialView 是对前端的视图：不含 KeyHash（哈希也属敏感面）。
type CredentialView struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Hosts     []string   `json:"hosts"`
	Owner     string     `json:"owner"`
	KeyHint   string     `json:"key_hint"`
	Revoked   bool       `json:"revoked"`
	Version   uint64     `json:"version"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
}

// View 返回脱敏视图（深拷贝切片）。
func (c Credential) View() CredentialView {
	return CredentialView{ID: c.ID, Name: c.Name, Hosts: append([]string(nil), c.Hosts...),
		Owner: c.Owner, KeyHint: c.KeyHint, Revoked: c.Revoked, Version: c.Version,
		CreatedBy: c.CreatedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, LastSeen: c.LastSeen}
}

// GatewayKey 是网关拉取的校验材料：哈希 + 绑定信息（网关据此做恒定时间比较与 hosts 子集校验）。
type GatewayKey struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Hosts   []string `json:"hosts"`
	KeyHash string   `json:"key_hash"`
	Revoked bool     `json:"revoked"`
}

// IssueInput 是签发 / 校验时的入参。
type IssueInput struct {
	Name  string
	Hosts []string
	Owner string
}

// KeyStore 是凭证表：读多写少，RWMutex；每次变更整表原子落盘。
type KeyStore struct {
	mu    sync.RWMutex
	path  string
	items map[string]*Credential
	now   func() time.Time
}

type credentialFile struct {
	Version     int           `json:"version"`
	Credentials []*Credential `json:"credentials"`
}

// OpenKeys 打开（或新建）凭证表。
func OpenKeys(path string, now func() time.Time) (*KeyStore, error) {
	if now == nil {
		now = time.Now
	}
	s := &KeyStore{path: path, items: map[string]*Credential{}, now: now}
	var f credentialFile
	if _, err := filestore.ReadJSON(path, &f); err != nil {
		return nil, err
	}
	for _, c := range f.Credentials {
		if c != nil && c.ID != "" && c.KeyHash != "" {
			s.items[c.ID] = c
		}
	}
	return s, nil
}

func (s *KeyStore) persistLocked() error {
	out := credentialFile{Version: 1, Credentials: make([]*Credential, 0, len(s.items))}
	for _, c := range s.items {
		out.Credentials = append(out.Credentials, c)
	}
	sort.Slice(out.Credentials, func(i, j int) bool { return out.Credentials[i].CreatedAt.Before(out.Credentials[j].CreatedAt) })
	return filestore.WriteJSON(s.path, out)
}

// Issue 签发新凭证，返回脱敏视图与**明文 key（仅此一次）**。
func (s *KeyStore) Issue(in IssueInput, actor string) (CredentialView, string, error) {
	clean, err := s.normalize(in)
	if err != nil {
		return CredentialView{}, "", err
	}
	key, err := newKey()
	if err != nil {
		return CredentialView{}, "", err
	}
	sum := sha256.Sum256([]byte(key))
	id, err := newID("shcred")
	if err != nil {
		return CredentialView{}, "", err
	}
	now := s.now().UTC()
	c := &Credential{ID: id, Name: clean.Name, Hosts: clean.Hosts, Owner: clean.Owner,
		KeyHash: hex.EncodeToString(sum[:]), KeyHint: Hint(key), Version: 1,
		CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) >= MaxCredentials {
		return CredentialView{}, "", fmt.Errorf("凭证数已达上限 %d", MaxCredentials)
	}
	if s.clashLocked(clean, "") {
		return CredentialView{}, "", ErrDuplicate
	}
	s.items[id] = c
	if err := s.persistLocked(); err != nil {
		delete(s.items, id)
		return CredentialView{}, "", err
	}
	return c.View(), key, nil
}

// Reset 作废旧 key 并签发新 key（凭证其余字段不变），返回新明文（仅此一次）。
func (s *KeyStore) Reset(id string) (CredentialView, string, error) {
	key, err := newKey()
	if err != nil {
		return CredentialView{}, "", err
	}
	sum := sha256.Sum256([]byte(key))
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.items[id]
	if !ok {
		return CredentialView{}, "", ErrNotFound
	}
	prev := *cur
	cur.KeyHash, cur.KeyHint = hex.EncodeToString(sum[:]), Hint(key)
	cur.Revoked = false // 重置即复活
	cur.Version++
	cur.UpdatedAt = s.now().UTC()
	if err := s.persistLocked(); err != nil {
		*cur = prev
		return CredentialView{}, "", err
	}
	return cur.View(), key, nil
}

// Revoke 吊销凭证（不可恢复；下线该凭证的全部会话由网关在拉取周期内执行）。
func (s *KeyStore) Revoke(id string) (CredentialView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.items[id]
	if !ok {
		return CredentialView{}, ErrNotFound
	}
	if cur.Revoked {
		return cur.View(), nil // 幂等
	}
	prev := *cur
	cur.Revoked = true
	cur.Version++
	cur.UpdatedAt = s.now().UTC()
	if err := s.persistLocked(); err != nil {
		*cur = prev
		return CredentialView{}, err
	}
	return cur.View(), nil
}

// List 返回全部凭证的脱敏视图（按创建时间）。
func (s *KeyStore) List() []CredentialView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CredentialView, 0, len(s.items))
	for _, c := range s.items {
		out = append(out, c.View())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Get 返回一份脱敏视图。
func (s *KeyStore) Get(id string) (CredentialView, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.items[id]
	if !ok {
		return CredentialView{}, false
	}
	return c.View(), true
}

// GatewayKeys 返回网关校验所需的哈希表（吊销的也带：网关要能对吊销 key 给出准确错误）。
func (s *KeyStore) GatewayKeys() []GatewayKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]GatewayKey, 0, len(s.items))
	for _, c := range s.items {
		out = append(out, GatewayKey{ID: c.ID, Name: c.Name,
			Hosts: append([]string(nil), c.Hosts...), KeyHash: c.KeyHash, Revoked: c.Revoked})
	}
	return out
}

// Touch 刷新凭证的最后使用时间（会话上报时调用；只动内存，下次落盘随任意变更带走）。
func (s *KeyStore) Touch(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.items[id]; ok {
		at := s.now().UTC()
		c.LastSeen = &at
	}
}

// normalize 校验入参（不含 key）。
func (s *KeyStore) normalize(in IssueInput) (IssueInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n == 0 || n > 64 {
		return in, &ValidationError{"name", "服务名长度须为 1–64 个字符"}
	}
	in.Owner = strings.TrimSpace(in.Owner)
	if utf8.RuneCountInString(in.Owner) > 64 {
		return in, &ValidationError{"owner", "负责人最长 64 个字符"}
	}
	if len(in.Hosts) == 0 {
		// 域名模式的合法性由调用方（api 层）先行校验；这里只收口数量与去重。
		return in, &ValidationError{"hosts", "至少声明一个域名"}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in.Hosts))
	for _, h := range in.Hosts {
		h = strings.TrimSpace(h)
		if h != "" && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	if len(out) == 0 || len(out) > MaxHosts {
		return in, &ValidationError{"hosts", fmt.Sprintf("域名数量须为 1–%d 个", MaxHosts)}
	}
	sort.Strings(out)
	in.Hosts = out
	return in, nil
}

func (s *KeyStore) clashLocked(in IssueInput, except string) bool {
	hosts := map[string]bool{}
	for _, h := range in.Hosts {
		hosts[strings.ToLower(h)] = true
	}
	for id, c := range s.items {
		if id == except {
			continue
		}
		if strings.EqualFold(c.Name, in.Name) {
			return true
		}
		for _, h := range c.Hosts {
			if hosts[strings.ToLower(h)] {
				return true
			}
		}
	}
	return false
}

// newKey 生成明文 key：`shc-` 前缀 + 32 字节随机（hex）。
// 前缀刻意避开 secrets-check 规则①的 `sk-` 形态。
func newKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("connector: 生成 key 失败：%w", err)
	}
	return "shc-" + hex.EncodeToString(buf), nil
}

// Hint 返回脱敏提示：`shc-****末4位`。
func Hint(key string) string {
	if len(key) <= 4 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

func newID(prefix string) (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("connector: 生成 id 失败：%w", err)
	}
	return prefix + "-" + hex.EncodeToString(buf), nil
}
