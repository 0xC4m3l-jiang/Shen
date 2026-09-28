package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRecordRecentAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	l, err := Open(path, 3, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, actor := range []string{"alice", "bob", "alice", "carol"} {
		if err := l.Record(Entry{Actor: actor, Action: "auth.login", Result: "ok",
			At: time.Unix(int64(i), 0).UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	got := l.Recent(10, "")
	if len(got) != 3 || got[0].Actor != "carol" || got[2].Actor != "bob" {
		t.Fatalf("环形缓冲应保留最近 3 条且新的在前：%+v", got)
	}
	if only := l.Recent(10, "alice"); len(only) != 1 {
		t.Fatalf("按操作者过滤失败：%+v", only)
	}
	_ = l.Close()

	reopened, err := Open(path, 10, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	if n := len(reopened.Recent(10, "")); n != 4 {
		t.Fatalf("重启后应从磁盘回读 4 条，实际 %d", n)
	}
}

func TestRotateAndSkipCorruptLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	if err := os.WriteFile(path, []byte("{broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Open(path, 10, 200, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if n := len(l.Recent(10, "")); n != 0 {
		t.Fatalf("坏行应被跳过，实际读到 %d 条", n)
	}
	for i := 0; i < 5; i++ {
		if err := l.Record(Entry{Actor: "admin", Action: "registry.update", Detail: strings.Repeat("x", 40)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("超过上限应轮转出 .1 文件：%v", err)
	}
}

// 并发回归：Record（写盘 + 环形缓冲）与 Recent（页面查询）并发执行。
// 拆分 ringMu/fileMu 后 Recent 不应被慢写阻塞，也不得看到撕裂的环形缓冲。
func TestLogConcurrentRecordAndRecent(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "audit.log"), 128, 1<<20, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	var wg sync.WaitGroup
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if err := l.Record(Entry{Actor: fmt.Sprintf("w%d", w), Action: "test"}); err != nil {
					t.Errorf("Record: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			for _, e := range l.Recent(64, "") {
				if e.Action != "test" { // 环形缓冲撕裂会读出零值
					t.Errorf("Recent 读到撕裂条目：%+v", e)
					return
				}
			}
		}
	}()
	wg.Wait()
	// 环形缓冲容量 128：600 条只保留最近的 128 条（回读磁盘才是全量审计）。
	if got := len(l.Recent(1000, "")); got != 128 {
		t.Fatalf("环形缓冲容量 128，应保留最近 128 条，实际 %d", got)
	}
}
