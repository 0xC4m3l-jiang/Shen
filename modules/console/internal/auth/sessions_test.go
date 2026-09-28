package auth

import (
	"sync"
	"testing"
	"time"

	"shen/modules/console/internal/rbac"
)

// 并发回归：Lookup 是每请求热路径。升级读写锁后，过期删除走「读锁判定→写锁删除」，
// 必须与并发 Create / DeleteUser / Lookup 竞争下保持一致（谁都不许 panic 或复活已删会话）。
func TestSessionStoreConcurrent(t *testing.T) {
	c := &clock{now: time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)}
	s := NewSessionStore(10*time.Minute, time.Hour, 64, c.Now)
	tok, _, err := s.Create("u", rbac.Admin, false, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				switch (w + i) % 4 {
				case 0, 1: // 读路径占大头：命中未过期会话并滑动活跃时间
					if sess, ok := s.Lookup(tok); !ok || sess.Username != "u" {
						t.Errorf("活跃会话丢失（w=%d i=%d）", w, i)
						return
					}
				case 2: // 并发新建（同一时刻最多 64 个会话，触发淘汰路径）
					if _, _, err := s.Create("x", rbac.Viewer, false, "127.0.0.1"); err != nil {
						t.Errorf("Create: %v", err)
						return
					}
				case 3: // 注销他人会话：Lookup 命中的会话随时可能消失，ok=false 是合法结果
					s.DeleteUser("x")
				}
			}
		}(w)
	}
	wg.Wait()
	if _, ok := s.Lookup(tok); !ok {
		t.Fatal("被并发 Create/DeleteUser 波及：目标会话不应被删除")
	}
}

// 过期会话在读锁判定后被并发 Create 抢先占位：删除必须只针对仍过期的那条。
func TestLookupExpiredUpgradeRacesWithCreate(t *testing.T) {
	c := &clock{now: time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)}
	s := NewSessionStore(time.Millisecond, time.Hour, 0, c.Now)
	tok, _, _ := s.Create("u", rbac.Viewer, false, "127.0.0.1")
	c.Add(10 * time.Millisecond) // 令牌过期
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.Lookup(tok) }() // 并发触发过期删除
	}
	wg.Wait()
	if _, ok := s.Lookup(tok); ok {
		t.Fatal("过期会话必须被删除")
	}
	// 之后同一账号可重新登录（新会话不受残留影响）
	if _, _, err := s.Create("u", rbac.Viewer, false, "127.0.0.1"); err != nil {
		t.Fatalf("重新登录失败：%v", err)
	}
	if n := s.Len(); n != 1 {
		t.Fatalf("重新登录后应恰有 1 个会话，实际 %d", n)
	}
}
