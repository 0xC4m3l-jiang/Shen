package filestore

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")
	type doc struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := WriteJSON(path, doc{Name: "蜃楼", Count: 3}); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	var got doc
	ok, err := ReadJSON(path, &got)
	if err != nil || !ok {
		t.Fatalf("读取失败：ok=%v err=%v", ok, err)
	}
	if got.Name != "蜃楼" || got.Count != 3 {
		t.Fatalf("回读不一致：%+v", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("状态文件权限应为 0600，实际 %o", perm)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("目录里不应残留临时文件：%v", entries)
	}
}

func TestMissingFileIsNotAnError(t *testing.T) {
	var v map[string]any
	ok, err := ReadJSON(filepath.Join(t.TempDir(), "absent.json"), &v)
	if ok || err != nil {
		t.Fatalf("缺失文件应返回 (false, nil)，实际 (%v, %v)", ok, err)
	}
}

func TestCorruptFileIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{half"), 0o600); err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if _, err := ReadJSON(path, &v); err == nil {
		t.Fatal("损坏文件必须报错，不能当作空状态")
	}
}
