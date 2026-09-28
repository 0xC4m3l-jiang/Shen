package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"shen/modules/console/internal/filestore"
	"shen/modules/console/internal/rbac"
)

// 登录相关错误。
var (
	// ErrInvalidCredentials 刻意不区分「用户不存在」与「口令错」（防账号枚举）。
	ErrInvalidCredentials = errors.New("用户名或口令错误")
	ErrDisabled           = errors.New("账号已停用")
)

// LockedError 表示登录因失败过多被暂时锁定。
type LockedError struct{ RetryAfter time.Duration }

func (e *LockedError) Error() string {
	return fmt.Sprintf("登录失败次数过多，请 %d 秒后重试", int(e.RetryAfter.Seconds())+1)
}

// Config 是 Service 的装配参数。
type Config struct {
	DataDir           string
	Params            Params
	SessionIdle       time.Duration
	SessionAbsolute   time.Duration
	MaxSessions       int
	APIToken          string // 只读自动化令牌（空 = 不启用）
	BootstrapUser     string
	BootstrapPassword string // "random"=随机生成写一次性文件；""（未配置）=默认 admin（验证环境缺省）；其它=须过口令策略
	Now               func() time.Time
	Logf              func(format string, args ...any)
}

// BootstrapFile 是随机初始口令的落盘文件名（首次改密后删除）。
const BootstrapFile = "bootstrap-admin.txt"

// Service 聚合账号、会话与限流。
type Service struct {
	users    *UserStore
	sessions *SessionStore
	byUser   *Limiter
	bySource *Limiter
	params   Params
	dataDir  string
	token    [32]byte
	hasToken bool
	now      func() time.Time
	logf     func(format string, args ...any)

	dummyOnce sync.Once
	dummyHash string
}

// New 打开数据目录中的账号表；空表时引导首个管理员。
func New(cfg Config) (*Service, error) {
	if cfg.DataDir == "" {
		return nil, errors.New("auth: 缺少数据目录")
	}
	if cfg.Params.KeyLen == 0 {
		cfg.Params = DefaultParams
	}
	if cfg.SessionIdle <= 0 {
		cfg.SessionIdle = 30 * time.Minute
	}
	if cfg.SessionAbsolute <= 0 {
		cfg.SessionAbsolute = 12 * time.Hour
	}
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 1024
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	users, err := OpenUserStore(filepath.Join(cfg.DataDir, "users.json"))
	if err != nil {
		return nil, err
	}
	s := &Service{
		users:    users,
		sessions: NewSessionStore(cfg.SessionIdle, cfg.SessionAbsolute, cfg.MaxSessions, cfg.Now),
		byUser:   NewLimiter(5, 15*time.Minute, 15*time.Minute, 4096, cfg.Now),
		bySource: NewLimiter(20, 15*time.Minute, 15*time.Minute, 4096, cfg.Now),
		params:   cfg.Params,
		dataDir:  cfg.DataDir,
		now:      cfg.Now,
		logf:     cfg.Logf,
	}
	if cfg.APIToken != "" {
		if len(cfg.APIToken) < 24 {
			return nil, errors.New("auth: 只读 API 令牌过短（至少 24 个字符）")
		}
		s.token, s.hasToken = sha256.Sum256([]byte(cfg.APIToken)), true
	}
	if users.Count() == 0 {
		if err := s.bootstrap(cfg.BootstrapUser, cfg.BootstrapPassword); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Service) bootstrap(rawUser, password string) error {
	if rawUser == "" {
		rawUser = "admin"
	}
	name, err := NormalizeUsername(rawUser)
	if err != nil {
		return fmt.Errorf("auth: 引导管理员用户名非法：%w", err)
	}
	// 口令三种取值：
	//   "random"   → 随机生成并写入数据目录一次性文件（谨慎部署用；首登改密后删除）
	//   ""（未配置）→ 默认 "admin"，便于本机/容器验证环境的人工登录（用户显式要求的验证缺省）
	//   其它       → 按配置使用，须过完整口令策略
	//
	// 默认 admin **豁免**口令策略（"admin" 只有 5 位，策略要求 ≥12）—— 引导口令是一次性的：
	// 首登强制改密（MustChange），改密时走完整策略；生产叠加（compose.prod.yaml）强制
	// secrets 注入，拿不到默认值。
	generated := password == "random"
	defaulted := password == ""
	if defaulted {
		password = "admin"
	} else if generated {
		if password, err = randomToken(15); err != nil {
			return err
		}
	} else if err := CheckPasswordPolicy(name, password); err != nil {
		return fmt.Errorf("auth: 引导管理员口令：%w", err)
	}
	hash, err := HashPassword(password, s.params)
	if err != nil {
		return err
	}
	now := s.now()
	if err := s.users.Create(User{
		Username: name, Role: rbac.Admin, PasswordHash: hash, MustChange: true,
		CreatedAt: now, UpdatedAt: now, Prefs: DefaultPreferences(),
	}); err != nil {
		return err
	}
	if generated {
		path := filepath.Join(s.dataDir, BootstrapFile)
		body := fmt.Sprintf("username=%s\npassword=%s\n# 首次登录后必须修改口令；修改成功后本文件会被自动删除。\n", name, password)
		if err := filestore.WriteBytes(path, []byte(body)); err != nil {
			return err
		}
		s.logf("console: 已创建初始管理员 %q，随机初始口令写在 %s（仅本机可读，首次改密后删除）", name, path)
	} else if defaulted {
		s.logf("console: WARN 未设置 SHEN_CONSOLE_BOOTSTRAP_PASSWORD ⇒ 初始管理员 %q 使用默认口令 admin（仅限验证环境；生产必须显式设置或用 secrets，首登强制改密）", name)
	} else {
		s.logf("console: 已创建初始管理员 %q（首次登录须改密）", name)
	}
	return nil
}

func (s *Service) dummy() string {
	s.dummyOnce.Do(func() {
		s.dummyHash, _ = HashPassword("shen-dummy-password-for-timing", s.params)
	})
	return s.dummyHash
}

// Login 校验口令并建立会话。source 为来源地址（限流键与审计用）。
func (s *Service) Login(rawUser, password, source string) (string, Session, error) {
	name, nameErr := NormalizeUsername(rawUser)
	userKey, srcKey := "user:"+name, "src:"+source
	if wait, locked := s.byUser.Locked(userKey); locked {
		return "", Session{}, &LockedError{RetryAfter: wait}
	}
	if wait, locked := s.bySource.Locked(srcKey); locked {
		return "", Session{}, &LockedError{RetryAfter: wait}
	}
	u, found := User{}, false
	if nameErr == nil {
		u, found = s.users.Get(name)
	}
	hash := s.dummy()
	if found {
		hash = u.PasswordHash
	}
	ok, err := VerifyPassword(password, hash)
	if err != nil || !ok || !found {
		s.byUser.Fail(userKey)
		s.bySource.Fail(srcKey)
		return "", Session{}, ErrInvalidCredentials
	}
	if u.Disabled {
		return "", Session{}, ErrDisabled
	}
	s.byUser.Success(userKey)
	now := s.now()
	if _, err := s.users.Update(name, func(x *User) error { x.LastLoginAt = &now; return nil }); err != nil {
		s.logf("console: 记录最近登录时间失败：%v", err)
	}
	return s.sessions.Create(u.Username, u.Role, u.MustChange, source)
}

// Authenticate 校验会话令牌。账号已被删除/停用/改角色时会话同步失效。
func (s *Service) Authenticate(token string) (Session, bool) {
	sess, ok := s.sessions.Lookup(token)
	if !ok {
		return Session{}, false
	}
	u, found := s.users.Get(sess.Username)
	if !found || u.Disabled || u.Role != sess.Role {
		s.sessions.Delete(token)
		return Session{}, false
	}
	sess.MustChange = u.MustChange
	return sess, true
}

// Logout 注销会话。
func (s *Service) Logout(token string) { s.sessions.Delete(token) }

// CheckAPIToken 以常量时间比较只读自动化令牌。
func (s *Service) CheckAPIToken(token string) bool {
	if !s.hasToken || token == "" {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(sum[:], s.token[:]) == 1
}

// ChangePassword 修改自己的口令：校验旧口令 → 注销该账号全部会话 → 签发新会话（令牌轮换）。
func (s *Service) ChangePassword(username, oldPassword, newPassword, source string) (string, Session, error) {
	u, ok := s.users.Get(username)
	if !ok {
		return "", Session{}, ErrUserNotFound
	}
	if valid, err := VerifyPassword(oldPassword, u.PasswordHash); err != nil || !valid {
		return "", Session{}, ErrInvalidCredentials
	}
	if oldPassword == newPassword {
		return "", Session{}, fmt.Errorf("%w（新口令不能与旧口令相同）", ErrPasswordPolicy)
	}
	if err := s.setPassword(username, newPassword, false); err != nil {
		return "", Session{}, err
	}
	s.sessions.DeleteUser(username)
	_ = os.Remove(filepath.Join(s.dataDir, BootstrapFile))
	u, _ = s.users.Get(username)
	return s.sessions.Create(u.Username, u.Role, false, source)
}

func (s *Service) setPassword(username, password string, mustChange bool) error {
	if err := CheckPasswordPolicy(username, password); err != nil {
		return err
	}
	hash, err := HashPassword(password, s.params)
	if err != nil {
		return err
	}
	_, err = s.users.Update(username, func(u *User) error {
		u.PasswordHash, u.MustChange, u.UpdatedAt = hash, mustChange, s.now()
		return nil
	})
	return err
}

// Users 返回全部账号视图。
func (s *Service) Users() []UserView {
	list := s.users.List()
	out := make([]UserView, 0, len(list))
	for _, u := range list {
		out = append(out, u.View())
	}
	return out
}

// CreateUser 由管理员创建账号（新账号首次登录必须改密）。
func (s *Service) CreateUser(rawUser string, role rbac.Role, password string) (UserView, error) {
	name, err := NormalizeUsername(rawUser)
	if err != nil {
		return UserView{}, err
	}
	if !rbac.Valid(role) {
		return UserView{}, ErrInvalidRole
	}
	if err := CheckPasswordPolicy(name, password); err != nil {
		return UserView{}, err
	}
	hash, err := HashPassword(password, s.params)
	if err != nil {
		return UserView{}, err
	}
	now := s.now()
	u := User{Username: name, Role: role, PasswordHash: hash, MustChange: true,
		CreatedAt: now, UpdatedAt: now, Prefs: DefaultPreferences()}
	if err := s.users.Create(u); err != nil {
		return UserView{}, err
	}
	return u.View(), nil
}

// UpdateUser 修改角色或停用状态（nil 表示不改）；变更后注销该账号全部会话。
func (s *Service) UpdateUser(username string, role *rbac.Role, disabled *bool) (UserView, error) {
	if role != nil && !rbac.Valid(*role) {
		return UserView{}, ErrInvalidRole
	}
	u, err := s.users.Update(username, func(u *User) error {
		if role != nil {
			u.Role = *role
		}
		if disabled != nil {
			u.Disabled = *disabled
		}
		u.UpdatedAt = s.now()
		return nil
	})
	if err != nil {
		return UserView{}, err
	}
	s.sessions.DeleteUser(username)
	return u.View(), nil
}

// ResetPassword 由管理员重置口令（被重置者下次登录必须改密）。
func (s *Service) ResetPassword(username, password string) error {
	if err := s.setPassword(username, password, true); err != nil {
		return err
	}
	s.sessions.DeleteUser(username)
	return nil
}

// DeleteUser 删除账号并注销其会话。
func (s *Service) DeleteUser(username string) error {
	if err := s.users.Delete(username); err != nil {
		return err
	}
	s.sessions.DeleteUser(username)
	return nil
}

// Preferences 返回账号偏好。
func (s *Service) Preferences(username string) (Preferences, error) {
	u, ok := s.users.Get(username)
	if !ok {
		return Preferences{}, ErrUserNotFound
	}
	return u.Prefs.Normalized(), nil
}

// SetPreferences 保存账号偏好。
func (s *Service) SetPreferences(username string, p Preferences) (Preferences, error) {
	p.Motion = true // 动效不再提供开关
	if err := p.Validate(); err != nil {
		return Preferences{}, err
	}
	u, err := s.users.Update(username, func(u *User) error { u.Prefs = p; return nil })
	if err != nil {
		return Preferences{}, err
	}
	return u.Prefs, nil
}
