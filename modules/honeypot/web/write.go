package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// 本文件是**受限写路径**的入口（方案 `C06`）：动作集只有两条，且都改的是**合成对象**。
//
// 三条硬边界（写在最前面，读代码的人先看到）：
//
//	① **不执行真实命令 / SQL**：只有内存里的结构体字段赋值，没有任何出站、没有数据库、没有 shell；
//	② **只作用于本会话**：写命中的是 `sessionOf(r)` 拿到的那份 `*sessionState`（跨会话不串）；
//	③ **配额与幂等**：值没变 ⇒ 幂等成功（不追加审计、不推进版本、不消耗配额）；
//	   版本不符 ⇒ 409（可重试、不双写）；写次数或审计条目到顶 ⇒ 429（受控拒绝，不是 500）。

// writeResult 是一次写请求要回给客户端的东西（JSON 直接序列化它）。
type writeResult struct {
	Kind    string `json:"kind"`            // users / config
	ID      string `json:"id,omitempty"`    // 账号 id 或配置键
	Value   any    `json:"value,omitempty"` // 变更后的值（enabled 或配置值）
	Version uint64 `json:"version"`         // 提交后的会话状态版本（CAS 依据）
	Changed bool   `json:"changed"`         // false = 幂等命中（值本来就一样）
}

// apiWriteTarget 从写路径里解析出 (资源, 对象标识)。
//
// 形状：`/admin/api/users/{id}` · `/admin/api/config/{key}`（key 允许带点，如 `database.name`）。
func apiWriteTarget(path string) (resource, id string, err error) {
	trimmed, ok := strings.CutPrefix(path, adminPrefix+"/api/")
	if !ok {
		return "", "", errNotConfigured
	}
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", "", errNotConfigured
	}
	switch parts[0] {
	case "users", "config":
		return parts[0], parts[1], nil
	default:
		return "", "", errNotConfigured
	}
}

// handleAdminWrite 处理 JSON 写请求（`POST /admin/api/{users,config}/{id}`）。
//
// 请求体：`{"enabled":true}` / `{"value":"..."}`，可选 `{"version":N}` 做 CAS。
func (h *Handler) handleAdminWrite(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.sessionOf(r)
	if !ok {
		// 未认证的写尝试**也留痕**：这是"有人试图改状态"的唯一线索，
		// 而它恰恰是没有会话时最值得看的一类交互（与登录失败同样处理）。
		h.emitFor(r, Event{Kind: EventStateChange, Outcome: "rejected"})
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		return
	}
	resource, id, err := apiWriteTarget(r.URL.Path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}

	// 配额：请求体上限与登录表单同一套（不做部分解析）。
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBody)
	var body struct {
		Enabled *bool   `json:"enabled"`
		Value   *string `json:"value"`
		Version *uint64 `json:"version"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "payload_too_large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_request"})
		return
	}

	var changed bool
	var wErr error
	switch resource {
	case "users":
		if body.Enabled == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_request"})
			return
		}
		changed, wErr = sess.setUserEnabled(id, *body.Enabled, h.now(), body.Version)
	case "config":
		if body.Value == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_request"})
			return
		}
		changed, wErr = sess.setConfigValue(id, *body.Value, h.now(), body.Version)
	}
	if wErr != nil {
		h.writeFailed(w, r, id, wErr)
		return
	}

	// 事件：写操作也要留痕（`C06` 的"行为仿真"要能被观测到，否则交互深度只能靠猜）。
	outcome := "noop"
	if changed {
		outcome = "applied"
	}
	h.emitFor(r, Event{Kind: EventStateChange, Outcome: outcome, User: id})

	res := writeResult{Kind: resource, ID: id, Version: sess.version, Changed: changed}
	switch resource {
	case "users":
		if i, ferr := sess.findUser(id); ferr == nil {
			res.Value = sess.users[i].Enabled
		}
	case "config":
		if i, ferr := sess.findConfig(id); ferr == nil {
			res.Value = sess.config[i].Value
		}
	}
	writeJSON(w, http.StatusOK, res)
}

// handleAdminPageWrite 处理页面表单写请求（浏览器链路）。
//
// 成功 ⇒ **POST/Redirect/GET**（303 回到列表页）：浏览器刷新不会重复提交，
// 而且重定向后的 GET 会重新读取**会话状态**，写后读在页面上一眼可见。
func (h *Handler) handleAdminPageWrite(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.sessionOf(r)
	if !ok {
		h.emitFor(r, Event{Kind: EventStateChange, Outcome: "rejected"})
		http.Redirect(w, r, "/admin/login", http.StatusFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBody)
	if err := r.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.render(w, http.StatusRequestEntityTooLarge, tmplError,
				h.errViewOf("Request too large", http.StatusRequestEntityTooLarge,
					"The submitted form exceeds the allowed size."))
			return
		}
		h.render(w, http.StatusBadRequest, tmplError,
			h.errViewOf("Bad request", http.StatusBadRequest, "The submitted form could not be read."))
		return
	}

	// 页面路径的形状与 API 一致：`/admin/users/{id}`、`/admin/config/{key}`。
	trimmed := strings.TrimPrefix(r.URL.Path, adminPrefix+"/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || parts[1] == "" {
		h.render(w, http.StatusNotFound, tmplError,
			h.errViewOf("Not found", http.StatusNotFound, "The requested page does not exist."))
		return
	}
	resource, id := parts[0], parts[1]
	version, verr := parseVersion(r.PostForm.Get("version"))
	if verr != nil {
		h.pageWriteFailed(w, r, resource, verr)
		return
	}

	var changed bool
	var wErr error
	redirect := ""
	switch resource {
	case "users":
		enabled, perr := parseEnabled(r.PostForm.Get("enabled"))
		if perr != nil {
			h.pageWriteFailed(w, r, resource, perr)
			return
		}
		changed, wErr = sess.setUserEnabled(id, enabled, h.now(), version)
		redirect = adminPrefix + "/users"
	case "config":
		changed, wErr = sess.setConfigValue(id, r.PostForm.Get("value"), h.now(), version)
		redirect = adminPrefix + "/config"
	default:
		h.render(w, http.StatusNotFound, tmplError,
			h.errViewOf("Not found", http.StatusNotFound, "The requested page does not exist."))
		return
	}
	if wErr != nil {
		h.pageWriteFailed(w, r, resource, wErr)
		return
	}

	outcome := "noop"
	if changed {
		outcome = "applied"
	}
	h.emitFor(r, Event{Kind: EventStateChange, Outcome: outcome, User: id})
	// 303：把浏览器转成 GET（刷新不重复提交），并让页面**重新读**会话状态（写后读可见）。
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// writeFailed 把状态机错误映射成 API 状态码（不靠字符串匹配 err.Error()）。
func (h *Handler) writeFailed(w http.ResponseWriter, r *http.Request, id string, err error) {
	h.emitFor(r, Event{Kind: EventStateChange, Outcome: "rejected", User: id})
	code, key := writeErrorStatus(err)
	if code == http.StatusConflict {
		// 冲突要带上**当前版本**，否则客户端无从重试（这正是"可重试不双写"的前提）。
		writeJSON(w, code, map[string]any{"error": key, "version": h.currentVersion(r)})
		return
	}
	writeJSON(w, code, map[string]string{"error": key})
}

// pageWriteFailed 同 `writeFailed`，但按页面语义渲染（浏览器看到的是合成错误页）。
func (h *Handler) pageWriteFailed(w http.ResponseWriter, r *http.Request, resource string, err error) {
	code, _ := writeErrorStatus(err)
	back := adminPrefix + "/" + resource
	if resource != "users" && resource != "config" {
		back = adminPrefix + "/"
	}
	h.emitFor(r, Event{Kind: EventStateChange, Outcome: "rejected", User: resource})
	w.Header().Set("Location", back) // 让错误页也能回到列表（不影响状态码）
	h.render(w, code, tmplError, h.errViewOf(writeErrorTitle(code), code, writeErrorDetail(code)))
}

// currentVersion 取当前会话的版本（用于 409 的可重试信息）。
func (h *Handler) currentVersion(r *http.Request) uint64 {
	if sess, ok := h.sessionOf(r); ok {
		return sess.version
	}
	return 0
}

// writeErrorStatus 把受控错误映射成 (HTTP 状态码, 错误键)。
func writeErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, errNotConfigured):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, errInvalidValue):
		return http.StatusBadRequest, "invalid_value"
	case errors.Is(err, errVersionConflict):
		return http.StatusConflict, "version_conflict"
	case errors.Is(err, errWriteQuota):
		return http.StatusTooManyRequests, "quota_exceeded"
	default:
		return http.StatusInternalServerError, "unavailable"
	}
}

// writeErrorTitle / writeErrorDetail 是合成错误页的措辞（中性，不暴露实现）。
func writeErrorTitle(code int) string {
	switch code {
	case http.StatusNotFound:
		return "Not found"
	case http.StatusBadRequest:
		return "Invalid request"
	case http.StatusConflict:
		return "Conflict"
	case http.StatusTooManyRequests:
		return "Too many changes"
	default:
		return "Unavailable"
	}
}

func writeErrorDetail(code int) string {
	switch code {
	case http.StatusNotFound:
		return "The requested resource does not exist."
	case http.StatusBadRequest:
		return "The submitted value was not accepted."
	case http.StatusConflict:
		return "The resource changed since it was loaded. Reload and try again."
	case http.StatusTooManyRequests:
		return "Too many changes in this session. Try again later."
	default:
		return "The console is temporarily unavailable."
	}
}
