// Package audit 是管控台的**操作审计**：登录、登出、改密、账号变更、反向链接器登记变更。
//
// 追加写 JSON Lines（一行一条，易于 `tail`/`grep` 与日志采集），内存保留最近 N 条供页面查询。
// 边界：**不记录口令、令牌、Cookie**；detail 只放对象标识与变更的字段名。
//
// 并发模型：两把锁各管各的 ——
//   - ringMu（RWMutex）：环形缓冲。Record 写、Recent 读；页面查询走读锁，彼此不阻塞。
//   - fileMu（Mutex）：审计文件句柄、已写字节与轮转。磁盘 I/O 只阻塞并发写，
//     不再拖住 Recent —— 否则一次慢盘写会让审计页卡住。
//
// JSON 编码在两把锁之外完成（Record 里最贵的部分）。
package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry 是一条审计记录。
type Entry struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`  // 操作者用户名；未登录为空
	Role   string    `json:"role"`   // 操作者角色
	Action string    `json:"action"` // 如 auth.login / registry.update
	Target string    `json:"target"` // 操作对象（用户名 / 服务 id）
	Result string    `json:"result"` // ok / denied / failed
	Source string    `json:"source"` // 来源地址
	Detail string    `json:"detail,omitempty"`
}

// Log 是审计日志。
type Log struct {
	ringMu sync.RWMutex
	ring   []Entry
	next   int
	full   bool

	fileMu   sync.Mutex
	path     string
	file     *os.File
	written  int64
	maxBytes int64
	now      func() time.Time
}

// Open 打开（追加）审计文件，并回读最近 keep 条到内存。maxBytes 超过后轮转为 `.1`。
func Open(path string, keep int, maxBytes int64, now func() time.Time) (*Log, error) {
	if keep <= 0 {
		keep = 1000
	}
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	if now == nil {
		now = time.Now
	}
	l := &Log{path: path, maxBytes: maxBytes, ring: make([]Entry, keep), now: now}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("audit: 创建目录失败：%w", err)
	}
	l.replay()
	if err := l.openFile(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Log) openFile() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("audit: 打开 %s 失败：%w", l.path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("audit: 读取 %s 大小失败：%w", l.path, err)
	}
	l.file, l.written = f, info.Size()
	return nil
}

// replay 把磁盘上的最近记录读回内存（坏行跳过：审计文件可能被截断在半行）。
// 只在 Open（单线程初始化）里调用，无需加锁。
func (l *Log) replay() {
	f, err := os.Open(l.path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			l.pushLocked(e)
		}
	}
}

func (l *Log) pushLocked(e Entry) {
	l.ring[l.next] = e
	l.next = (l.next + 1) % len(l.ring)
	if l.next == 0 {
		l.full = true
	}
}

// Record 追加一条记录；落盘失败只影响持久化，内存仍保留（审计不应阻断正常操作，但会返回错误供上层打日志）。
func (l *Log) Record(e Entry) error {
	if e.At.IsZero() {
		e.At = l.now().UTC()
	}
	line, err := json.Marshal(e) // 锁外编码
	if err != nil {
		return fmt.Errorf("audit: 编码失败：%w", err)
	}
	line = append(line, '\n')

	l.ringMu.Lock() // 内存可见性：先入环形缓冲，再落盘
	l.pushLocked(e)
	l.ringMu.Unlock()

	l.fileMu.Lock() // 磁盘 I/O 只与并发写竞争，不阻塞 Recent
	defer l.fileMu.Unlock()
	if l.written+int64(len(line)) > l.maxBytes {
		l.rotateLocked()
	}
	if l.file == nil {
		return fmt.Errorf("audit: 文件未打开")
	}
	n, err := l.file.Write(line)
	l.written += int64(n)
	if err != nil {
		return fmt.Errorf("audit: 写入失败：%w", err)
	}
	return nil
}

func (l *Log) rotateLocked() {
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
	_ = os.Rename(l.path, l.path+".1")
	if err := l.openFile(); err != nil {
		l.file = nil
	}
}

// Recent 返回最近 limit 条（新的在前）；actor 非空时只返回该操作者的记录。
func (l *Log) Recent(limit int, actor string) []Entry {
	l.ringMu.RLock()
	defer l.ringMu.RUnlock()
	size := l.next
	if l.full {
		size = len(l.ring)
	}
	out := make([]Entry, 0, min(limit, size))
	for i := 0; i < size && len(out) < limit; i++ {
		idx := (l.next - 1 - i + len(l.ring)) % len(l.ring)
		e := l.ring[idx]
		if actor != "" && e.Actor != actor {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Close 关闭文件。
func (l *Log) Close() error {
	l.fileMu.Lock()
	defer l.fileMu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
