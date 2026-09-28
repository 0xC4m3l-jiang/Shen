package auth

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"shen/modules/console/internal/filestore"
	"shen/modules/console/internal/rbac"
)

// 账号相关错误（api 层按它们映射 HTTP 状态码）。
var (
	ErrUserExists      = errors.New("用户名已存在")
	ErrUserNotFound    = errors.New("用户不存在")
	ErrInvalidUsername = errors.New("用户名须为 3–32 位小写字母、数字或 . _ -，且以字母或数字开头")
	ErrInvalidRole     = errors.New("未知角色")
	ErrLastAdmin       = errors.New("至少保留一个启用中的管理员")
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)

// NormalizeUsername 归一化用户名（去空白、小写）；不合法时返回 ErrInvalidUsername。
func NormalizeUsername(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if !usernamePattern.MatchString(name) {
		return "", ErrInvalidUsername
	}
	return name, nil
}

// Preferences 是按账号保存在服务端的界面偏好（前端不把本地存储当数据源）。
type Preferences struct {
	Theme  string `json:"theme"`
	Motion bool   `json:"motion"`
}

// 主题取值：深色 mirage、亮色 haze（与前端一一对应）。
var validThemes = map[string]bool{"mirage": true, "haze": true}

// DefaultPreferences 是新账号的偏好。
func DefaultPreferences() Preferences { return Preferences{Theme: "mirage", Motion: true} }

// Validate 校验偏好取值。
func (p Preferences) Validate() error {
	if !validThemes[p.Theme] {
		return fmt.Errorf("未知主题 %q（可选 mirage 深色 / haze 亮色）", p.Theme)
	}
	return nil
}

// Normalized 把历史数据归一到当前取值：已下线的主题（如 aurora）回落为 mirage；动效不再可关，恒为开。
func (p Preferences) Normalized() Preferences {
	if !validThemes[p.Theme] {
		p.Theme = "mirage"
	}
	p.Motion = true
	return p
}

// User 是落盘的账号记录。
type User struct {
	Username     string      `json:"username"`
	Role         rbac.Role   `json:"role"`
	PasswordHash string      `json:"password_hash"`
	MustChange   bool        `json:"must_change"`
	Disabled     bool        `json:"disabled"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	LastLoginAt  *time.Time  `json:"last_login_at,omitempty"`
	Prefs        Preferences `json:"preferences"`
}

// UserView 是对外可见的账号视图（**不含口令哈希**）。
type UserView struct {
	Username    string     `json:"username"`
	Role        rbac.Role  `json:"role"`
	MustChange  bool       `json:"must_change"`
	Disabled    bool       `json:"disabled"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

// View 返回去掉敏感字段的视图。
func (u User) View() UserView {
	return UserView{
		Username: u.Username, Role: u.Role, MustChange: u.MustChange, Disabled: u.Disabled,
		CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt, LastLoginAt: u.LastLoginAt,
	}
}

type userFile struct {
	Version int    `json:"version"`
	Users   []User `json:"users"`
}

// UserStore 是账号表：内存为读路径，每次变更整表原子落盘；落盘失败则回滚内存。
type UserStore struct {
	mu    sync.RWMutex
	path  string
	users map[string]User
}

// OpenUserStore 打开（或新建）账号文件。
func OpenUserStore(path string) (*UserStore, error) {
	s := &UserStore{path: path, users: map[string]User{}}
	var f userFile
	ok, err := filestore.ReadJSON(path, &f)
	if err != nil {
		return nil, err
	}
	if ok {
		for _, u := range f.Users {
			if _, err := NormalizeUsername(u.Username); err != nil {
				return nil, fmt.Errorf("auth: 账号文件含非法用户名 %q", u.Username)
			}
			if !rbac.Valid(u.Role) {
				return nil, fmt.Errorf("auth: 账号 %q 的角色 %q 未登记", u.Username, u.Role)
			}
			if u.Prefs.Validate() != nil {
				u.Prefs = DefaultPreferences()
			}
			s.users[u.Username] = u
		}
	}
	return s, nil
}

// Count 返回账号数。
func (s *UserStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users)
}

// Get 返回账号副本。
func (s *UserStore) Get(name string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[name]
	return u, ok
}

// List 返回按用户名排序的账号副本。
func (s *UserStore) List() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out
}

// Create 新增账号。
func (s *UserStore) Create(u User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.users[u.Username]; exists {
		return ErrUserExists
	}
	next := s.cloneLocked()
	next[u.Username] = u
	return s.commitLocked(next)
}

// Update 在锁内修改一条账号；fn 返回错误则不落盘。修改后必须仍有启用中的管理员。
func (s *UserStore) Update(name string, fn func(*User) error) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[name]
	if !ok {
		return User{}, ErrUserNotFound
	}
	if err := fn(&u); err != nil {
		return User{}, err
	}
	next := s.cloneLocked()
	next[name] = u
	if !hasEnabledAdmin(next) {
		return User{}, ErrLastAdmin
	}
	if err := s.commitLocked(next); err != nil {
		return User{}, err
	}
	return u, nil
}

// Delete 删除账号（不得删掉最后一个启用中的管理员）。
func (s *UserStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[name]; !ok {
		return ErrUserNotFound
	}
	next := s.cloneLocked()
	delete(next, name)
	if !hasEnabledAdmin(next) {
		return ErrLastAdmin
	}
	return s.commitLocked(next)
}

func (s *UserStore) cloneLocked() map[string]User {
	next := make(map[string]User, len(s.users)+1)
	for k, v := range s.users {
		next[k] = v
	}
	return next
}

// commitLocked 先落盘再替换内存：落盘失败时内存保持旧值（不出现「页面显示已改、重启后又回来」）。
func (s *UserStore) commitLocked(next map[string]User) error {
	f := userFile{Version: 1, Users: make([]User, 0, len(next))}
	for _, u := range next {
		f.Users = append(f.Users, u)
	}
	sort.Slice(f.Users, func(i, j int) bool { return f.Users[i].Username < f.Users[j].Username })
	if err := filestore.WriteJSON(s.path, f); err != nil {
		return err
	}
	s.users = next
	return nil
}

func hasEnabledAdmin(users map[string]User) bool {
	for _, u := range users {
		if u.Role == rbac.Admin && !u.Disabled {
			return true
		}
	}
	return false
}
