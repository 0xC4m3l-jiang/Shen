package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// UsageRecord 是一次模型调用的记账（成功失败都记：失败的调用有时也计费，且失败率本身是运维信号）。
type UsageRecord struct {
	At             time.Time `json:"at"`
	User           string    `json:"user"`
	ProviderID     string    `json:"provider_id"`
	ProviderName   string    `json:"provider_name"`
	Model          string    `json:"model"`
	Kind           string    `json:"kind"` // test / chat
	ConversationID string    `json:"conversation_id,omitempty"`
	Prompt         int       `json:"prompt_tokens"`
	Completion     int       `json:"completion_tokens"`
	Total          int       `json:"total_tokens"`
	Estimated      bool      `json:"estimated,omitempty"`
	LatencyMs      int64     `json:"latency_ms"`
	OK             bool      `json:"ok"`
	Error          string    `json:"error,omitempty"`
}

// UsageLog 是 token 用量账本：JSONL 追加 + 内存环形窗口。
//
// 并发模型与审计日志一致：ringMu（RWMutex）管内存窗口，fileMu 管磁盘；统计走读锁，
// 慢盘写不阻塞用量页。
type UsageLog struct {
	ringMu sync.RWMutex
	ring   []UsageRecord
	next   int
	full   bool

	fileMu   sync.Mutex
	path     string
	file     *os.File
	written  int64
	maxBytes int64
}

// OpenUsage 打开账本并回读最近 keep 条。
func OpenUsage(path string, keep int) (*UsageLog, error) {
	if keep <= 0 {
		keep = 20000
	}
	l := &UsageLog{ring: make([]UsageRecord, keep), path: path, maxBytes: 20 << 20}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("llm: 创建目录失败：%w", err)
	}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var r UsageRecord
			if json.Unmarshal(sc.Bytes(), &r) == nil {
				l.pushLocked(r)
			}
		}
		_ = f.Close()
	}
	if err := l.openFile(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *UsageLog) openFile() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("llm: 打开用量账本失败：%w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	l.file, l.written = f, info.Size()
	return nil
}

func (l *UsageLog) pushLocked(r UsageRecord) {
	l.ring[l.next] = r
	l.next = (l.next + 1) % len(l.ring)
	if l.next == 0 {
		l.full = true
	}
}

// Record 记一笔账。落盘失败只返回错误（上层打日志），内存窗口照常更新。
func (l *UsageLog) Record(r UsageRecord) error {
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	l.ringMu.Lock()
	l.pushLocked(r)
	l.ringMu.Unlock()

	l.fileMu.Lock()
	defer l.fileMu.Unlock()
	if l.written+int64(len(line)) > l.maxBytes {
		if l.file != nil {
			_ = l.file.Close()
		}
		_ = os.Rename(l.path, l.path+".1")
		if err := l.openFile(); err != nil {
			l.file = nil
		}
	}
	if l.file == nil {
		return fmt.Errorf("llm: 用量账本未打开")
	}
	n, err := l.file.Write(line)
	l.written += int64(n)
	return err
}

// Close 关闭文件。
func (l *UsageLog) Close() error {
	l.fileMu.Lock()
	defer l.fileMu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// Totals 是一组调用的合计。
type Totals struct {
	Requests   int `json:"requests"`
	Failed     int `json:"failed"`
	Prompt     int `json:"prompt_tokens"`
	Completion int `json:"completion_tokens"`
	Total      int `json:"total_tokens"`
	Estimated  int `json:"estimated_requests"` // 服务商没回 usage、由本地估算的调用数
}

func (t *Totals) add(r UsageRecord) {
	t.Requests++
	if !r.OK {
		t.Failed++
	}
	t.Prompt += r.Prompt
	t.Completion += r.Completion
	t.Total += r.Total
	if r.Estimated {
		t.Estimated++
	}
}

// ModelUsage 是按「提供方 × 模型」的合计。
type ModelUsage struct {
	ProviderID   string    `json:"provider_id"`
	ProviderName string    `json:"provider_name"`
	Model        string    `json:"model"`
	LastAt       time.Time `json:"last_at"`
	Totals
}

// DayUsage 是按天的合计（服务端时区）。
type DayUsage struct {
	Date string `json:"date"`
	Totals
}

// UserUsage 是按账号的合计（仅管理员可见）。
type UserUsage struct {
	User string `json:"user"`
	Totals
}

// Summary 是用量页的全部数据。
type Summary struct {
	WindowStart time.Time     `json:"window_start"` // 内存窗口里最早的一笔（更早的只在磁盘账本里）
	Records     int           `json:"records"`
	Totals      Totals        `json:"totals"`
	ByModel     []ModelUsage  `json:"by_model"`
	ByDay       []DayUsage    `json:"by_day"`
	ByUser      []UserUsage   `json:"by_user,omitempty"`
	Recent      []UsageRecord `json:"recent"`
}

// Summarize 在读锁下汇总窗口内的账目。user 非空时只统计该账号；withUsers 决定是否给出按人明细。
func (l *UsageLog) Summarize(now time.Time, days int, user string, withUsers bool) Summary {
	l.ringMu.RLock()
	size := l.next
	if l.full {
		size = len(l.ring)
	}
	records := make([]UsageRecord, 0, size)
	for i := 0; i < size; i++ { // 新的在前
		r := l.ring[(l.next-1-i+len(l.ring))%len(l.ring)]
		if user == "" || r.User == user {
			records = append(records, r)
		}
	}
	l.ringMu.RUnlock()

	out := Summary{Records: len(records)}
	models := map[string]*ModelUsage{}
	users := map[string]*UserUsage{}
	dayTotals := map[string]*Totals{}
	for _, r := range records {
		out.Totals.add(r)
		k := r.ProviderID + "\x00" + r.Model
		m := models[k]
		if m == nil {
			m = &ModelUsage{ProviderID: r.ProviderID, ProviderName: r.ProviderName, Model: r.Model, LastAt: r.At}
			models[k] = m
		}
		m.add(r)
		if withUsers {
			u := users[r.User]
			if u == nil {
				u = &UserUsage{User: r.User}
				users[r.User] = u
			}
			u.add(r)
		}
		d := r.At.In(now.Location()).Format("2006-01-02")
		if dayTotals[d] == nil {
			dayTotals[d] = &Totals{}
		}
		dayTotals[d].add(r)
		if out.WindowStart.IsZero() || r.At.Before(out.WindowStart) {
			out.WindowStart = r.At
		}
	}
	for _, m := range models {
		out.ByModel = append(out.ByModel, *m)
	}
	sort.Slice(out.ByModel, func(i, j int) bool { return out.ByModel[i].Total > out.ByModel[j].Total })
	for _, u := range users {
		out.ByUser = append(out.ByUser, *u)
	}
	sort.Slice(out.ByUser, func(i, j int) bool { return out.ByUser[i].Total > out.ByUser[j].Total })
	if days <= 0 {
		days = 14
	}
	for i := days - 1; i >= 0; i-- { // 连续的日期轴：没有调用的日子显示 0，而不是缺一格
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		day := DayUsage{Date: d}
		if t := dayTotals[d]; t != nil {
			day.Totals = *t
		}
		out.ByDay = append(out.ByDay, day)
	}
	if len(records) > 30 {
		records = records[:30]
	}
	out.Recent = records
	return out
}
