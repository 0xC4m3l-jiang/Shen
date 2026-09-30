package deception

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"shen/modules/console/internal/filestore"
)

// 存储错误（api 层映射：ErrConflict→409，ErrNotFound→404）。
var (
	ErrConflict = errors.New("数据集已被他人修改（版本不一致），请刷新后重试")
	ErrNotFound = errors.New("版本不存在（可能已超出保留的历史范围）")
)

// Store 持有数据集：单文件 + 历史目录，写侧互斥、读侧返回副本。
//
// 布局：<dir>/dataset.json（当前版本）· <dir>/history/v<n>.json（最近 MaxHistory 个旧版本）。
type Store struct {
	mu   sync.RWMutex
	dir  string
	cur  Dataset
	hist []VersionMeta // 升序
	now  func() time.Time
}

// Open 打开（或新建）数据集目录。文件损坏时**启动失败**并点名历史目录（坏配置不运行）。
func Open(dir string, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(filepath.Join(dir, "history"), 0o700); err != nil {
		return nil, fmt.Errorf("deception: 创建数据目录失败：%w", err)
	}
	s := &Store{dir: dir, now: now}
	var d Dataset
	ok, err := filestore.ReadJSON(s.mainPath(), &d)
	if err != nil {
		return nil, fmt.Errorf("deception: 数据集文件 %s 损坏（%v）—— 可从 %s 取最近的历史版本恢复",
			s.mainPath(), err, filepath.Join(dir, "history"))
	}
	if ok {
		s.cur = d.normalizeNil()
	} else {
		s.cur = Dataset{}.normalizeNil()
	}
	s.hist = s.scanHistory()
	return s, nil
}

func (s *Store) mainPath() string { return filepath.Join(s.dir, "dataset.json") }
func (s *Store) histPath(v uint64) string {
	return filepath.Join(s.dir, "history", "v"+strconv.FormatUint(v, 10)+".json")
}

// Get 返回当前数据集的副本。
func (s *Store) Get() Dataset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur.Clone()
}

// Save 用 next 的**内容域**替换当前数据集（版本 +1、标记已接管、旧版本入历史）。
//
// expected 是调用方看到的版本（乐观锁）；元数据字段（版本 / 投影修订 / 初始化信息）由本函数维护，
// 调用方传入的值被忽略。校验由调用方先做（本函数只管持久化与并发）。
func (s *Store) Save(expected uint64, actor, summary string, next Dataset) (Dataset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur.Version != expected {
		return Dataset{}, ErrConflict
	}
	out := next.Clone()
	out.Version = s.cur.Version + 1
	out.ProjectionRev = s.cur.ProjectionRev
	out.ProjectionDigest = s.cur.ProjectionDigest
	out.Initialized = true
	out.SeedSource, out.SeedChecksum = s.cur.SeedSource, s.cur.SeedChecksum
	out.UpdatedAt, out.UpdatedBy, out.Summary = s.now().UTC(), actor, summary
	if err := s.commitLocked(out, true); err != nil {
		return Dataset{}, err
	}
	return out.Clone(), nil
}

// Seed 用部署配置初始化数据集（仅当尚未接管时生效；已接管返回 false）。
func (s *Store) Seed(seed Dataset, source, checksum string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur.Initialized {
		return false, nil
	}
	out := seed.Clone()
	out.Version = s.cur.Version + 1
	out.ProjectionRev, out.ProjectionDigest = s.cur.ProjectionRev, s.cur.ProjectionDigest
	out.Initialized, out.SeedSource, out.SeedChecksum = true, source, checksum
	out.UpdatedAt, out.UpdatedBy = s.now().UTC(), "system:seed"
	out.Summary = "从部署配置自动初始化（" + source + "）"
	return true, s.commitLocked(out, false)
}

// Rollback 把内容域恢复为历史版本 to 的内容 —— 作为**新版本**发布（版本继续 +1，不倒退）。
func (s *Store) Rollback(to, expected uint64, actor string) (Dataset, error) {
	old, err := s.VersionAt(to)
	if err != nil {
		return Dataset{}, err
	}
	return s.Save(expected, actor, fmt.Sprintf("回滚到 v%d 的内容", to), old)
}

// EnsureProjection 在投影摘要变化时推进 ProjectionRev（只增不减）并持久化；返回当前修订号。
//
// 调用方：集成端点（核心拉取时）与服务登记变更后的投影检查。摘要相同 ⇒ 零写入。
func (s *Store) EnsureProjection(digest string) (uint64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur.ProjectionDigest == digest && s.cur.ProjectionRev > 0 {
		return s.cur.ProjectionRev, false, nil
	}
	if s.cur.ProjectionRev+1 >= MaxProjection {
		return s.cur.ProjectionRev, false, fmt.Errorf("deception: 投影修订号已达上限 %d —— 请调高部署配置的 policy.version 后重置", MaxProjection)
	}
	next := s.cur.Clone()
	next.ProjectionRev++
	next.ProjectionDigest = digest
	if err := s.commitLocked(next, false); err != nil {
		return s.cur.ProjectionRev, false, err
	}
	return next.ProjectionRev, true, nil
}

// History 返回版本时间线（新→旧），含当前版本。
func (s *Store) History() []VersionMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]VersionMeta, 0, len(s.hist)+1)
	if s.cur.Version > 0 {
		out = append(out, metaOf(s.cur))
	}
	for i := len(s.hist) - 1; i >= 0; i-- {
		out = append(out, s.hist[i])
	}
	return out
}

// VersionAt 返回指定版本的完整内容（当前版本或历史版本）。
func (s *Store) VersionAt(v uint64) (Dataset, error) {
	s.mu.RLock()
	if v == s.cur.Version && v > 0 {
		d := s.cur.Clone()
		s.mu.RUnlock()
		return d, nil
	}
	s.mu.RUnlock()
	var d Dataset
	ok, err := filestore.ReadJSON(s.histPath(v), &d)
	if err != nil {
		return Dataset{}, fmt.Errorf("deception: 读历史版本 v%d 失败：%w", v, err)
	}
	if !ok {
		return Dataset{}, ErrNotFound
	}
	return d.normalizeNil(), nil
}

// commitLocked 落盘：archive=true 时先把旧版本写入历史（失败则整体失败，不产生半写状态）。
func (s *Store) commitLocked(next Dataset, archive bool) error {
	if archive && s.cur.Version > 0 {
		if err := filestore.WriteJSON(s.histPath(s.cur.Version), s.cur); err != nil {
			return fmt.Errorf("deception: 写历史版本失败：%w", err)
		}
		s.hist = append(s.hist, metaOf(s.cur))
		s.pruneLocked()
	}
	if err := filestore.WriteJSON(s.mainPath(), next); err != nil {
		return fmt.Errorf("deception: 写数据集失败：%w", err)
	}
	s.cur = next.normalizeNil()
	return nil
}

func (s *Store) pruneLocked() {
	for len(s.hist) > MaxHistory {
		_ = os.Remove(s.histPath(s.hist[0].Version))
		s.hist = s.hist[1:]
	}
}

func (s *Store) scanHistory() []VersionMeta {
	entries, err := os.ReadDir(filepath.Join(s.dir, "history"))
	if err != nil {
		return nil
	}
	var out []VersionMeta
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "v") || !strings.HasSuffix(name, ".json") {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(name, "v"), ".json"), 10, 64)
		if err != nil {
			continue
		}
		var d Dataset
		if ok, err := filestore.ReadJSON(s.histPath(v), &d); err == nil && ok {
			out = append(out, metaOf(d))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

func metaOf(d Dataset) VersionMeta {
	return VersionMeta{Version: d.Version, UpdatedAt: d.UpdatedAt, UpdatedBy: d.UpdatedBy, Summary: d.Summary}
}
