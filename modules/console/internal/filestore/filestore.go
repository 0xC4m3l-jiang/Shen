// Package filestore 是管控台本地状态（账号 / 反向链接器登记）的**唯一落盘出口**。
//
// 为什么不用数据库：状态量小（几十个账号、几百条登记），且管控台必须离线可用、无 CGO。
// 为什么要原子写：进程在写一半时被杀（容器重启很常见），直接覆盖会留下半个 JSON ——
// 下次启动读不出来就等于丢了全部账号。这里固定走「临时文件 → fsync → rename → fsync 目录」。
package filestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// MaxFileBytes 是单个状态文件的上限：超过它说明文件被篡改或格式失控，拒绝读取而不是撑爆内存。
const MaxFileBytes = 16 << 20

// ReadJSON 读取 path 并解码到 v。文件不存在时返回 (false, nil)。
func ReadJSON(path string, v any) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("filestore: 读取 %s 失败：%w", path, err)
	}
	if info.Size() > MaxFileBytes {
		return false, fmt.Errorf("filestore: %s 超过 %d 字节上限", path, MaxFileBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("filestore: 读取 %s 失败：%w", path, err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, fmt.Errorf("filestore: %s 不是合法 JSON：%w", path, err)
	}
	return true, nil
}

// WriteJSON 以原子方式把 v 写到 path（权限 0600：账号文件含口令哈希）。
func WriteJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("filestore: 编码失败：%w", err)
	}
	raw = append(raw, '\n')
	return WriteBytes(path, raw)
}

// WriteBytes 原子写入任意字节。
func WriteBytes(path string, raw []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("filestore: 创建目录 %s 失败：%w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("filestore: 创建临时文件失败：%w", err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("filestore: 设置权限失败：%w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("filestore: 写入失败：%w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("filestore: 刷盘失败：%w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("filestore: 关闭临时文件失败：%w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("filestore: 替换 %s 失败：%w", path, err)
	}
	committed = true
	syncDir(dir)
	return nil
}

// syncDir 让 rename 本身持久化。部分平台（Windows）不支持对目录 fsync —— 失败即忽略，
// 那里 rename 的持久性由文件系统自身保证。
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
