// Package registry 是**反向链接器登记表**：每个被引擎保护的 Web 服务（独立项目）一条记录。
//
// 边界（与 README「不做控制面」一致）：
//   - 只是**登记 + 观测归类**：按域名把流量归到服务，供页面查看「是否流入蜃楼」；
//   - **不下发策略、不改后端表**：真正生效的路由仍由核心配置与策略面（Pull/Ack）决定；
//   - **不主动连接上游**：upstream 只做格式校验与展示，管控台是被动组件。
package registry

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"shen/modules/console/internal/filestore"
)

// 登记表错误。
var (
	ErrNotFound  = errors.New("服务不存在")
	ErrConflict  = errors.New("登记已被他人修改（版本不一致），请刷新后重试")
	ErrDuplicate = errors.New("名称或域名已被其他服务登记")
	ErrFull      = errors.New("登记数已达上限")
)

// ValidationError 表示字段校验失败（api 层映射为 400 并回显原因）。
type ValidationError struct{ Field, Reason string }

func (e *ValidationError) Error() string { return e.Field + "：" + e.Reason }

// 上限：防止被登记表撑爆内存或把观测归类变成 O(n²)。
const (
	MaxServices = 512
	MaxHosts    = 32
)

// Service 是一条登记。
type Service struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Upstream    string   `json:"upstream"`
	Hosts       []string `json:"hosts"`
	Owner       string   `json:"owner"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	Version     uint64   `json:"version"`
	// Source 是登记来源：空与 "manual" 为手动；"connector" 为连接器自动登记（反向隧道接入时上报）。
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}

// Input 是创建 / 修改时允许由调用方提供的字段。
type Input struct {
	Name        string   `json:"name"`
	Upstream    string   `json:"upstream"`
	Hosts       []string `json:"hosts"`
	Owner       string   `json:"owner"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
}

type fileShape struct {
	Version  int       `json:"version"`
	Services []Service `json:"services"`
}

// Store 是登记表。
type Store struct {
	mu       sync.RWMutex
	path     string
	services map[string]Service
	now      func() time.Time
}

// Open 打开（或新建）登记文件。
func Open(path string, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	s := &Store{path: path, services: map[string]Service{}, now: now}
	var f fileShape
	ok, err := filestore.ReadJSON(path, &f)
	if err != nil {
		return nil, err
	}
	if ok {
		for _, svc := range f.Services {
			if svc.ID == "" {
				return nil, fmt.Errorf("registry: 登记文件含空 id 记录")
			}
			s.services[svc.ID] = svc
		}
	}
	return s, nil
}

// List 返回全部登记（按名称排序）。
func (s *Store) List() []Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Service, 0, len(s.services))
	for _, svc := range s.services {
		out = append(out, clone(svc))
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Get 返回一条登记。
func (s *Store) Get(id string) (Service, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	svc, ok := s.services[id]
	return clone(svc), ok
}

// Create 新增登记。
func (s *Store) Create(in Input, actor string) (Service, error) {
	norm, err := normalize(in)
	if err != nil {
		return Service{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.services) >= MaxServices {
		return Service{}, ErrFull
	}
	if err := s.checkUniqueLocked("", norm); err != nil {
		return Service{}, err
	}
	id, err := newID()
	if err != nil {
		return Service{}, err
	}
	now := s.now().UTC()
	svc := Service{ID: id, Name: norm.Name, Upstream: norm.Upstream, Hosts: norm.Hosts,
		Owner: norm.Owner, Description: norm.Description, Enabled: norm.Enabled,
		Source: "manual", Version: 1, CreatedAt: now, UpdatedAt: now, UpdatedBy: actor}
	next := s.cloneLocked()
	next[id] = svc
	if err := s.commitLocked(next); err != nil {
		return Service{}, err
	}
	return clone(svc), nil
}

// Update 整体替换可编辑字段；expected 为调用方看到的版本（乐观并发控制）。
func (s *Store) Update(id string, expected uint64, in Input, actor string) (Service, error) {
	norm, err := normalize(in)
	if err != nil {
		return Service{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.services[id]
	if !ok {
		return Service{}, ErrNotFound
	}
	if cur.Version != expected {
		return Service{}, ErrConflict
	}
	if err := s.checkUniqueLocked(id, norm); err != nil {
		return Service{}, err
	}
	cur.Name, cur.Upstream, cur.Hosts = norm.Name, norm.Upstream, norm.Hosts
	cur.Owner, cur.Description, cur.Enabled = norm.Owner, norm.Description, norm.Enabled
	cur.Version++
	cur.UpdatedAt, cur.UpdatedBy = s.now().UTC(), actor
	next := s.cloneLocked()
	next[id] = cur
	if err := s.commitLocked(next); err != nil {
		return Service{}, err
	}
	return clone(cur), nil
}

// Delete 删除登记（同样要求版本一致，避免删掉别人刚改过的记录）。
func (s *Store) Delete(id string, expected uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.services[id]
	if !ok {
		return ErrNotFound
	}
	if cur.Version != expected {
		return ErrConflict
	}
	next := s.cloneLocked()
	delete(next, id)
	return s.commitLocked(next)
}

// UpsertConnector 是**连接器自动登记**的入口（来源=connector 的 upsert，与手动登记共存）。
//
// 语义（保守取向：运维的手动决策永远优先）：
//   - 同名服务不存在 → 创建（Source=connector，Enabled=true，Upstream=连接器上报的本地地址）；
//   - 同名且同为连接器来源 → 刷新域名与上游（版本+1，UpdatedBy=actor）；
//   - 同名但是**手动登记** → **原样返回、不改动**（created=false）——运维手动登记的服务
//     不被自动流程覆盖。
//
// 域名冲突仍然校验：声明域名若属于**其他**服务，拒绝（自动流程无权抢占）。
func (s *Store) UpsertConnector(name string, hosts []string, upstream, actor string) (Service, bool, error) {
	clean := Input{Name: name, Upstream: upstream, Hosts: hosts, Enabled: true}
	clean.Name = strings.TrimSpace(clean.Name)
	if clean.Name == "" {
		return Service{}, false, &ValidationError{"name", "服务名不能为空"}
	}
	normalized := make([]string, 0, len(hosts))
	seen := map[string]bool{}
	for _, raw := range hosts {
		h, err := NormalizePattern(raw)
		if err != nil {
			return Service{}, false, &ValidationError{"hosts", err.Error()}
		}
		if !seen[h] {
			seen[h] = true
			normalized = append(normalized, h)
		}
	}
	if len(normalized) == 0 || len(normalized) > MaxHosts {
		return Service{}, false, &ValidationError{"hosts", fmt.Sprintf("域名数量须为 1–%d 个", MaxHosts)}
	}
	sort.Strings(normalized)
	clean.Hosts = normalized
	up, err := normalizeUpstream(upstream)
	if err != nil {
		return Service{}, false, &ValidationError{"upstream", err.Error()}
	}
	clean.Upstream = up

	s.mu.Lock()
	defer s.mu.Unlock()
	for id, svc := range s.services {
		if strings.EqualFold(svc.Name, clean.Name) {
			if svc.Source != "connector" {
				return clone(svc), false, nil // 手动登记：不动
			}
			prev := svc
			svc.Hosts, svc.Upstream = clean.Hosts, clean.Upstream
			svc.Version++
			svc.UpdatedAt, svc.UpdatedBy = s.now().UTC(), actor
			next := s.cloneLocked()
			next[id] = svc
			if err := s.commitLocked(next); err != nil {
				s.services[id] = prev
				return Service{}, false, err
			}
			return clone(svc), false, nil
		}
	}
	if len(s.services) >= MaxServices {
		return Service{}, false, ErrFull
	}
	if err := s.checkUniqueLocked("", clean); err != nil {
		return Service{}, false, err
	}
	id, err := newID()
	if err != nil {
		return Service{}, false, err
	}
	now := s.now().UTC()
	svc := Service{ID: id, Name: clean.Name, Upstream: clean.Upstream, Hosts: clean.Hosts,
		Enabled: true, Source: "connector", Version: 1, CreatedAt: now, UpdatedAt: now, UpdatedBy: actor}
	next := s.cloneLocked()
	next[id] = svc
	if err := s.commitLocked(next); err != nil {
		return Service{}, false, err
	}
	return clone(svc), true, nil
}

// Match 把请求 Host 归到登记的服务：精确域名优先，其次最长的通配后缀；未命中返回 ("", false)。
// 停用的服务不参与归类（它的流量显示为「未登记」，避免误以为仍在观测）。
func (s *Store) Match(host string) (string, bool) {
	h := NormalizeHost(host)
	if h == "" {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	bestID, bestLen := "", -1
	for _, svc := range s.services {
		if !svc.Enabled {
			continue
		}
		for _, pattern := range svc.Hosts {
			switch {
			case pattern == h:
				return svc.ID, true
			case strings.HasPrefix(pattern, "*."):
				suffix := pattern[1:] // ".example.com"
				if strings.HasSuffix(h, suffix) && len(h) > len(suffix) && len(suffix) > bestLen {
					bestID, bestLen = svc.ID, len(suffix)
				}
			}
		}
	}
	return bestID, bestID != ""
}

func (s *Store) checkUniqueLocked(selfID string, in Input) error {
	hosts := map[string]bool{}
	for _, h := range in.Hosts {
		hosts[h] = true
	}
	for id, svc := range s.services {
		if id == selfID {
			continue
		}
		if strings.EqualFold(svc.Name, in.Name) {
			return fmt.Errorf("%w：名称 %q", ErrDuplicate, in.Name)
		}
		for _, h := range svc.Hosts {
			if hosts[h] {
				return fmt.Errorf("%w：域名 %q 已属于 %q", ErrDuplicate, h, svc.Name)
			}
		}
	}
	return nil
}

func (s *Store) cloneLocked() map[string]Service {
	next := make(map[string]Service, len(s.services)+1)
	for k, v := range s.services {
		next[k] = v
	}
	return next
}

func (s *Store) commitLocked(next map[string]Service) error {
	f := fileShape{Version: 1, Services: make([]Service, 0, len(next))}
	for _, svc := range next {
		f.Services = append(f.Services, svc)
	}
	sort.Slice(f.Services, func(i, j int) bool { return f.Services[i].ID < f.Services[j].ID })
	if err := filestore.WriteJSON(s.path, f); err != nil {
		return err
	}
	s.services = next
	return nil
}

func clone(svc Service) Service {
	svc.Hosts = append([]string(nil), svc.Hosts...)
	return svc
}

func newID() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("registry: 生成 id 失败：%w", err)
	}
	return "svc-" + hex.EncodeToString(buf), nil
}

// NormalizeHost 与适配器 request_judged 的 host 同一口径：小写、去端口、去尾点。
func NormalizeHost(raw string) string {
	h := strings.ToLower(strings.TrimSpace(raw))
	if hostOnly, _, err := net.SplitHostPort(h); err == nil {
		h = hostOnly
	}
	h = strings.Trim(h, "[]")
	return strings.TrimSuffix(h, ".")
}

var labelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// NormalizePattern 校验并规范化一个域名模式（小写、去端口、去尾点；支持最左一级通配）。
// 供登记与接入凭证共用同一校验口径。
func NormalizePattern(raw string) (string, error) { return normalizePattern(raw) }

func normalizePattern(raw string) (string, error) {
	h := NormalizeHost(raw)
	if h == "" {
		return "", errors.New("域名不能为空")
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.String(), nil
	}
	rest := h
	if strings.HasPrefix(h, "*.") {
		rest = h[2:]
	}
	if strings.Contains(rest, "*") {
		return "", fmt.Errorf("%q：只支持最左一级通配（如 *.example.com）", raw)
	}
	labels := strings.Split(rest, ".")
	if len(rest) > 253 || (strings.HasPrefix(h, "*.") && len(labels) < 2) {
		return "", fmt.Errorf("%q 不是合法域名", raw)
	}
	for _, l := range labels {
		if !labelPattern.MatchString(l) {
			return "", fmt.Errorf("%q 不是合法域名", raw)
		}
	}
	return h, nil
}

func normalize(in Input) (Input, error) {
	out := Input{Enabled: in.Enabled}
	out.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(out.Name); n == 0 || n > 64 {
		return Input{}, &ValidationError{"name", "名称长度须为 1–64 个字符"}
	}
	out.Owner = strings.TrimSpace(in.Owner)
	if utf8.RuneCountInString(out.Owner) > 64 {
		return Input{}, &ValidationError{"owner", "负责人最长 64 个字符"}
	}
	out.Description = strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(out.Description) > 512 {
		return Input{}, &ValidationError{"description", "描述最长 512 个字符"}
	}
	up, err := normalizeUpstream(in.Upstream)
	if err != nil {
		return Input{}, &ValidationError{"upstream", err.Error()}
	}
	out.Upstream = up
	if len(in.Hosts) == 0 || len(in.Hosts) > MaxHosts {
		return Input{}, &ValidationError{"hosts", fmt.Sprintf("域名数量须为 1–%d 个", MaxHosts)}
	}
	seen := map[string]bool{}
	for _, raw := range in.Hosts {
		h, err := normalizePattern(raw)
		if err != nil {
			return Input{}, &ValidationError{"hosts", err.Error()}
		}
		if h == "*" {
			return Input{}, &ValidationError{"hosts", "禁止裸 *"}
		}
		if !seen[h] {
			seen[h] = true
			out.Hosts = append(out.Hosts, h)
		}
	}
	sort.Strings(out.Hosts)
	return out, nil
}

func normalizeUpstream(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 {
		return "", errors.New("上游地址不能为空且不超过 2048 字符")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("上游地址须为 http(s)://主机[:端口][/路径]")
	}
	if u.User != nil {
		return "", errors.New("上游地址不得内嵌账号口令")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("上游地址不得带查询串或锚点")
	}
	return u.String(), nil
}
