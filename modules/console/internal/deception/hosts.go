package deception

import (
	"errors"
	"fmt"
	"net"
	"path"
	"strings"
)

// 本文件的判据与核心**逐条同口径**（common/core/internal/policy 的 normalizeHostPattern /
// hostPatternsIntersect 与 contract.NormalizePath / PathSegmentPrefix）。两边不一致时，
// 管控台放行的数据会被核心终检拒绝 —— 那不会伤业务（last-good），但会让运营困惑，所以必须对齐。

// NormalizeHostPattern 归一化一条主机声明：精确主机名或 `*.` 前缀通配，禁止裸 `*`。
func NormalizeHostPattern(raw string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(raw))
	h = strings.TrimSuffix(h, ".")
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	if h == "" {
		return "", errors.New("为空")
	}
	if h == "*" {
		return "", errors.New("裸 * 不被接受（那是对所有主机宣示所有权）—— 用精确主机名或 *.domain")
	}
	body := strings.TrimPrefix(h, "*.")
	if body == "" {
		return "", errors.New("通配缺少域名（应为 *.example.com）")
	}
	if strings.HasPrefix(h, "*.") && !strings.Contains(body, ".") {
		return "", fmt.Errorf("通配过宽：%q 至少要写成 *.example.com", h)
	}
	if len(h) > 253 {
		return "", errors.New("过长（>253）")
	}
	for i, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
		case r == '*' && i == 0:
		default:
			return "", fmt.Errorf("含非法字符 %q（只允许字母/数字/连字符/点，以及开头的 *.）", r)
		}
	}
	return h, nil
}

// hostsIntersect 报告两组归属声明是否可能命中同一个 Host（空列表 = 匹配任何主机）。
func hostsIntersect(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, x := range a {
		for _, y := range b {
			if hostPatternsIntersect(x, y) {
				return true
			}
		}
	}
	return false
}

func hostPatternsIntersect(x, y string) bool {
	if x == y {
		return true
	}
	xs, xWild := strings.CutPrefix(x, "*.")
	ys, yWild := strings.CutPrefix(y, "*.")
	switch {
	case xWild && yWild:
		return strings.HasSuffix(xs, "."+ys) || strings.HasSuffix(ys, "."+xs)
	case xWild:
		return strings.HasSuffix(y, "."+xs) && len(y) > len(xs)+1
	case yWild:
		return strings.HasSuffix(x, "."+ys) && len(x) > len(ys)+1
	default:
		return false
	}
}

// NormalizePath 与 contract.NormalizePath 同义（path.Clean）。
func NormalizePath(p string) string {
	if p == "" {
		return ""
	}
	return path.Clean(p)
}

// pathSegmentPrefix 与 contract.PathSegmentPrefix 同义：按路径段边界的前缀匹配。
func pathSegmentPrefix(got, want string) bool {
	if want == "" {
		return false
	}
	seg := strings.TrimSuffix(want, "/")
	if seg == "" {
		return got == "/" || got == ""
	}
	return got == seg || strings.HasPrefix(got, seg+"/")
}

// pathsOverlap 报告两条路径是否相同或嵌套。
func pathsOverlap(a, b string) bool {
	return a == b || pathSegmentPrefix(a, b) || pathSegmentPrefix(b, a)
}
