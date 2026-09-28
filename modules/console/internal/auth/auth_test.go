package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"shen/modules/console/internal/rbac"
)

// fastParams 让测试不必为每次哈希付出 64 MiB 的代价（算法与格式不变）。
var fastParams = Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newService(t *testing.T, mutate func(*Config)) (*Service, *clock, string) {
	t.Helper()
	dir := t.TempDir()
	c := &clock{now: time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)}
	cfg := Config{DataDir: dir, Params: fastParams, Now: c.Now,
		BootstrapUser: "admin", BootstrapPassword: "initial-Admin-Pass-01"}
	if mutate != nil {
		mutate(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, c, dir
}

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse battery", fastParams)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=1024,t=1,p=1$") {
		t.Fatalf("哈希格式不对：%s", h)
	}
	if ok, err := VerifyPassword("correct horse battery", h); err != nil || !ok {
		t.Fatalf("正确口令校验失败：%v %v", ok, err)
	}
	if ok, _ := VerifyPassword("wrong", h); ok {
		t.Fatal("错误口令不应通过")
	}
	for _, bad := range []string{"", "$argon2i$v=19$m=1,t=1,p=1$a$b", "$argon2id$v=19$m=99999999,t=1,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA"} {
		if _, err := VerifyPassword("x", bad); err == nil {
			t.Errorf("非法/超限哈希应报错：%q", bad)
		}
	}
}

func TestPasswordPolicy(t *testing.T) {
	if CheckPasswordPolicy("admin", "short") == nil {
		t.Error("过短口令应被拒绝")
	}
	if CheckPasswordPolicy("administrator", "Administrator") == nil {
		t.Error("与用户名相同的口令应被拒绝")
	}
	if err := CheckPasswordPolicy("admin", "蜃楼海市一二三四五六七八"); err != nil {
		t.Errorf("12 个中文字符应满足长度（按字符计）：%v", err)
	}
}

func TestBootstrapLoginAndMustChange(t *testing.T) {
	s, _, _ := newService(t, nil)
	token, sess, err := s.Login("Admin", "initial-Admin-Pass-01", "127.0.0.1")
	if err != nil {
		t.Fatalf("初始管理员登录失败：%v", err)
	}
	if sess.Role != rbac.Admin || !sess.MustChange || sess.CSRF == "" {
		t.Fatalf("会话字段不对：%+v", sess)
	}
	got, ok := s.Authenticate(token)
	if !ok || got.Username != "admin" {
		t.Fatalf("令牌校验失败：%+v %v", got, ok)
	}
	newTok, fresh, err := s.ChangePassword("admin", "initial-Admin-Pass-01", "brand-new-Pass-2026", "127.0.0.1")
	if err != nil {
		t.Fatalf("改密失败：%v", err)
	}
	if fresh.MustChange {
		t.Error("改密后不应再要求改密")
	}
	if _, ok := s.Authenticate(token); ok {
		t.Error("改密后旧会话必须失效（令牌轮换）")
	}
	if _, ok := s.Authenticate(newTok); !ok {
		t.Error("改密后签发的新会话应有效")
	}
}

func TestGeneratedBootstrapPasswordFile(t *testing.T) {
	s, _, dir := newService(t, func(c *Config) { c.BootstrapPassword = "random" })
	raw, err := os.ReadFile(filepath.Join(dir, BootstrapFile))
	if err != nil {
		t.Fatalf("随机初始口令必须落到一次性文件：%v", err)
	}
	var pw string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "password=") {
			pw = strings.TrimPrefix(line, "password=")
		}
	}
	if len(pw) < MinPasswordLen {
		t.Fatalf("初始口令过短：%q", pw)
	}
	if _, _, err := s.Login("admin", pw, "127.0.0.1"); err != nil {
		t.Fatalf("用文件里的口令登录失败：%v", err)
	}
	if _, _, err := s.ChangePassword("admin", pw, "another-Strong-Pass-9", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, BootstrapFile)); !errors.Is(err, os.ErrNotExist) {
		t.Error("首次改密后必须删除初始口令文件")
	}
}

// 未配置口令时的验证缺省：账号/口令均为 admin（用户要求的验证体验）——
// 豁免口令策略、MustChange=true、且**不**写 bootstrap-admin.txt（口令是公开的默认值，写文件无意义）。
func TestBootstrapDefaultAdminPassword(t *testing.T) {
	s, _, dir := newService(t, func(c *Config) { c.BootstrapPassword = "" })
	tok, sess, err := s.Login("admin", "admin", "127.0.0.1")
	if err != nil {
		t.Fatalf("默认 admin/admin 必须能登录：%v", err)
	}
	if !sess.MustChange {
		t.Fatal("默认口令登录后必须处于强制改密状态")
	}
	if _, ok := s.Authenticate(tok); !ok {
		t.Fatal("会话应有效")
	}
	if _, err := os.Stat(filepath.Join(dir, BootstrapFile)); !errors.Is(err, os.ErrNotExist) {
		t.Error("默认口令不应写一次性口令文件")
	}
	// 改回强口令后一切如常
	if _, _, err := s.ChangePassword("admin", "admin", "another-Strong-Pass-9", "127.0.0.1"); err != nil {
		t.Fatalf("默认口令应可改密：%v", err)
	}
	if _, _, err := s.Login("admin", "admin", "127.0.0.1"); err == nil {
		t.Fatal("改密后旧默认口令必须失效")
	}
}

func TestLoginErrorsDoNotRevealAccounts(t *testing.T) {
	s, _, _ := newService(t, nil)
	_, _, errUnknown := s.Login("ghost", "whatever-password", "10.0.0.1")
	_, _, errWrong := s.Login("admin", "whatever-password", "10.0.0.1")
	if !errors.Is(errUnknown, ErrInvalidCredentials) || !errors.Is(errWrong, ErrInvalidCredentials) {
		t.Fatalf("不存在账号与口令错误必须返回同一错误：%v / %v", errUnknown, errWrong)
	}
}

func TestLockoutAfterRepeatedFailures(t *testing.T) {
	s, c, _ := newService(t, nil)
	for i := 0; i < 5; i++ {
		_, _, _ = s.Login("admin", "wrong-password-xx", "10.0.0.2")
	}
	_, _, err := s.Login("admin", "initial-Admin-Pass-01", "10.0.0.3")
	var locked *LockedError
	if !errors.As(err, &locked) {
		t.Fatalf("连续失败后正确口令也应被锁定，实际：%v", err)
	}
	c.Add(16 * time.Minute)
	if _, _, err := s.Login("admin", "initial-Admin-Pass-01", "10.0.0.3"); err != nil {
		t.Fatalf("锁定期过后应可登录：%v", err)
	}
}

func TestSessionIdleAndAbsoluteExpiry(t *testing.T) {
	s, c, _ := newService(t, func(cfg *Config) {
		cfg.SessionIdle, cfg.SessionAbsolute = 10*time.Minute, 30*time.Minute
	})
	token, _, err := s.Login("admin", "initial-Admin-Pass-01", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		c.Add(9 * time.Minute)
		if _, ok := s.Authenticate(token); !ok {
			t.Fatalf("活动中的会话不应因空闲超时失效（第 %d 次）", i)
		}
	}
	c.Add(4 * time.Minute) // 空闲仅 4 分钟，但距创建已 31 分钟
	if _, ok := s.Authenticate(token); ok {
		t.Fatal("超过绝对时长的会话必须失效")
	}
}

func TestRoleChangeAndDisableRevokeSessions(t *testing.T) {
	s, _, _ := newService(t, nil)
	if _, err := s.CreateUser("ops.deception", rbac.DeceptionOperator, "deception-Pass-2026"); err != nil {
		t.Fatal(err)
	}
	token, sess, err := s.Login("ops.deception", "deception-Pass-2026", "127.0.0.1")
	if err != nil || sess.Role != rbac.DeceptionOperator {
		t.Fatalf("登录失败：%v %+v", err, sess)
	}
	viewer := rbac.Viewer
	if _, err := s.UpdateUser("ops.deception", &viewer, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Authenticate(token); ok {
		t.Fatal("改角色后旧会话必须失效（否则旧权限残留）")
	}
	disabled := true
	if _, err := s.UpdateUser("admin", nil, &disabled); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("停用最后一个管理员应被拒绝：%v", err)
	}
	if err := s.DeleteUser("admin"); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("删除最后一个管理员应被拒绝：%v", err)
	}
}

func TestUserValidationAndPersistence(t *testing.T) {
	s, _, dir := newService(t, nil)
	if _, err := s.CreateUser("A", rbac.Viewer, "viewer-Pass-2026"); !errors.Is(err, ErrInvalidUsername) {
		t.Errorf("过短用户名应被拒绝：%v", err)
	}
	if _, err := s.CreateUser("reader", rbac.Role("root"), "viewer-Pass-2026"); !errors.Is(err, ErrInvalidRole) {
		t.Errorf("未知角色应被拒绝：%v", err)
	}
	if _, err := s.CreateUser("reader", rbac.Viewer, "viewer-Pass-2026"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser("Reader", rbac.Viewer, "viewer-Pass-2026"); !errors.Is(err, ErrUserExists) {
		t.Errorf("大小写不同的同名账号应被拒绝：%v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "viewer-Pass-2026") {
		t.Fatal("账号文件里不得出现明文口令")
	}
	reopened, err := New(Config{DataDir: dir, Params: fastParams})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(reopened.Users()); n != 2 {
		t.Fatalf("重启后账号应为 2 个，实际 %d", n)
	}
}

func TestAPITokenAndPreferences(t *testing.T) {
	s, _, _ := newService(t, func(c *Config) { c.APIToken = "automation-token-0123456789abcdef" })
	if !s.CheckAPIToken("automation-token-0123456789abcdef") || s.CheckAPIToken("automation-token-0123456789abcdeX") || s.CheckAPIToken("") {
		t.Fatal("只读令牌比较结果不对")
	}
	if _, err := New(Config{DataDir: t.TempDir(), Params: fastParams, APIToken: "short"}); err == nil {
		t.Fatal("过短令牌应拒绝启动")
	}
	if _, err := s.SetPreferences("admin", Preferences{Theme: "neon"}); err == nil {
		t.Fatal("未知主题应被拒绝")
	}
	// 主题只剩深色 mirage / 亮色 haze；已下线的 aurora 拒绝写入。动效不再可关：写 false 也存成 true。
	if _, err := s.SetPreferences("admin", Preferences{Theme: "aurora"}); err == nil {
		t.Fatal("已下线的 aurora 主题应被拒绝")
	}
	p, err := s.SetPreferences("admin", Preferences{Theme: "haze", Motion: false})
	if err != nil || p.Theme != "haze" || !p.Motion {
		t.Fatalf("保存偏好失败：%+v %v", p, err)
	}
	// 历史数据里的 aurora 读出时归一为 mirage（老账号不会卡在一个不存在的主题上）
	if got := (Preferences{Theme: "aurora"}).Normalized(); got.Theme != "mirage" || !got.Motion {
		t.Fatalf("旧主题应归一为 mirage：%+v", got)
	}
}

func TestSessionStoreCapacity(t *testing.T) {
	c := &clock{now: time.Unix(0, 0)}
	st := NewSessionStore(time.Hour, time.Hour, 2, c.Now)
	first, _, _ := st.Create("a", rbac.Viewer, false, "")
	c.Add(time.Second)
	_, _, _ = st.Create("b", rbac.Viewer, false, "")
	c.Add(time.Second)
	_, _, _ = st.Create("c", rbac.Viewer, false, "")
	if st.Len() != 2 {
		t.Fatalf("会话数应受上限约束，实际 %d", st.Len())
	}
	if _, ok := st.Lookup(first); ok {
		t.Fatal("满额时应淘汰最久未活动的会话")
	}
}
