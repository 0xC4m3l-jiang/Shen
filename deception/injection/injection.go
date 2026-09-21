package injection

import (
	"bytes"
	"fmt"
	"strings"
)

const defaultMarker = "</body>"

// Engine 是 Injector 的实现。
//
// 它是**无状态**的纯变换：同样的输入必得同样的输出（无时钟、无随机）。
type Engine struct {
	rules []Rule
}

// New 构造注入器。空规则集合法（此时什么都不注入）；规则缺片段时报错。
func New(rules []Rule) (*Engine, error) {
	out := make([]Rule, 0, len(rules))
	for i, r := range rules {
		if strings.TrimSpace(r.Snippet) == "" {
			return nil, fmt.Errorf("injection: rules[%d] 的注入片段不能为空", i)
		}
		if strings.TrimSpace(r.Marker) == "" {
			r.Marker = defaultMarker
		}
		out = append(out, r)
	}
	return &Engine{rules: out}, nil
}

// Inject 把全部规则注入 HTML 响应。
//
// 只处理 `text/html`：对其他内容类型一律原样返回（不误伤 JSON / 二进制）。
// 找不到标记的规则被跳过，不算失败。返回是否**至少**发生了一次改写。
func (e *Engine) Inject(contentType string, body []byte) ([]byte, bool) {
	if !isHTML(contentType) || len(e.rules) == 0 || len(body) == 0 {
		return body, false
	}

	out := body
	changed := false
	for _, r := range e.rules {
		idx := bytes.Index(out, []byte(r.Marker))
		if idx < 0 {
			continue // 该页没有注入点，跳过（不阻断）
		}
		next := make([]byte, 0, len(out)+len(r.Snippet))
		next = append(next, out[:idx]...)
		next = append(next, r.Snippet...)
		next = append(next, out[idx:]...)
		out = next
		changed = true
	}
	return out, changed
}

// isHTML 判断内容类型是否为 HTML。
func isHTML(contentType string) bool {
	ct := strings.ToLower(contentType)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.TrimSpace(ct) == "text/html"
}

var _ Injector = (*Engine)(nil)
