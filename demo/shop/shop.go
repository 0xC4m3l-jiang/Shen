// Package shop 是演示用的**真实业务应用**：一个典型的电商站（商品 / 登录 / 订单 / 后台）。
//
// 它对蜃楼一无所知——不知道反向代理、不知道判定、更不知道欺骗；
// 这正是接入演示要表达的：**业务零改造**，接入全部发生在业务之外（连接器 / 网关 / 代理）。
//
// 页面刻意做成「真实业务的样子」，因为欺骗效果演示需要它们：
//   - /login 表单 —— 攻击者会尝试撞库与表单注入；
//   - /admin     —— 扫描器最爱探测的路径；
//   - /api/orders—— 带 session 的数据接口。
//
// 只监听 127.0.0.1（loopback）：真实业务**零入站暴露**是零信任接入的前提——
// 外部流量只能经蜃楼的隧道进来，而不是直接打进业务端口。
package shop

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Product 是一件在售商品（演示数据）。
type Product struct {
	ID    int
	Name  string
	Price string
	Desc  string
}

var products = []Product{
	{1, "蜃景无线耳机", "¥499", "主动降噪 · 30 小时续航 · 蓝牙 5.3"},
	{2, "海市机械键盘", "¥699", "热插拔轴体 · 三模连接 · Gasket 结构"},
	{3, "幻影智能手表", "¥1299", "血氧监测 · 14 天续航 · 50m 防水"},
	{4, "楼阁便携储能", "¥2199", "600Wh · 交流逆变 · 太阳能输入"},
}

type session struct {
	user   string
	expiry time.Time
}

// Store 是极简内存会话表（演示用；真实业务换成 Redis / DB 即可，接入方式不受影响）。
type Store struct {
	mu       sync.Mutex
	sessions map[string]session
}

func NewStore() *Store { return &Store{sessions: map[string]session{}} }

// Login 校验口令并签发会话 cookie（演示账号：admin / shop123）。
func (s *Store) Login(user, pass string) (string, bool) {
	if user == "" || pass != "shop123" {
		return "", false
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[tok] = session{user: user, expiry: time.Now().Add(2 * time.Hour)}
	s.mu.Unlock()
	return tok, true
}

func (s *Store) Valid(token string) (string, bool) {
	s.mu.Lock()
	sess, ok := s.sessions[token]
	s.mu.Unlock()
	if !ok || time.Now().After(sess.expiry) {
		return "", false
	}
	return sess.user, true
}

var tpl = template.Must(template.New("t").Parse(`<!DOCTYPE html>
<html lang="zh"><head><meta charset="utf-8"><title>{{.Title}} · 蜃景商城</title>
<style>
body{font-family:-apple-system,"PingFang SC",sans-serif;margin:0;background:#f6f8fa;color:#1f2937}
header{background:#0f172a;color:#fff;padding:14px 32px;display:flex;justify-content:space-between}
header a{color:#7dd3fc;text-decoration:none;margin-left:18px}
main{max-width:960px;margin:32px auto;padding:0 16px}
.card{background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:20px;margin-bottom:16px}
.price{color:#dc2626;font-weight:600}
.muted{color:#6b7280;font-size:13px}
form{display:flex;flex-direction:column;gap:10px;max-width:320px}
input{border:1px solid #d1d5db;border-radius:8px;padding:9px 12px}
button{background:#2563eb;color:#fff;border:0;border-radius:8px;padding:10px;cursor:pointer}
table{width:100%;border-collapse:collapse}
td,th{border-bottom:1px solid #e5e7eb;padding:8px;text-align:left}
</style></head><body>
<header><b>蜃景商城</b><nav><a href="/">首页</a><a href="/login">登录</a><a href="/admin">后台</a><a href="/api/orders">订单 API</a></nav></header>
<main>{{.Body}}</main></body></html>`))

func page(w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tpl.Execute(w, map[string]string{"Title": title, "Body": body})
}

// New 返回业务站的根 Handler（独立进程与 SDK 内嵌两种接入共用这一份业务代码）。
func New(store *Store) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString("<h1>今日在售</h1>")
		for _, p := range products {
			fmt.Fprintf(&b, `<div class="card"><b>%s</b> <span class="price">%s</span><br><span class="muted">%s</span><br><a href="/product?id=%d">查看详情 →</a></div>`, p.Name, p.Price, p.Desc, p.ID)
		}
		page(w, "首页", b.String())
	})

	mux.HandleFunc("/product", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		for _, p := range products {
			if fmt.Sprint(p.ID) == id {
				page(w, p.Name, fmt.Sprintf(`<div class="card"><h2>%s</h2><p class="price">%s</p><p>%s</p></div>`, p.Name, p.Price, p.Desc))
				return
			}
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if tok, ok := store.Login(r.FormValue("user"), r.FormValue("pass")); ok {
				http.SetCookie(w, &http.Cookie{Name: "shop_session", Value: tok, Path: "/", HttpOnly: true})
				http.Redirect(w, r, "/admin", http.StatusFound)
				return
			}
			page(w, "登录", `<div class="card"><p style="color:#dc2626">口令错误</p></div>`)
			return
		}
		page(w, "登录", `<div class="card"><form method="post"><b>商家登录</b>
<input name="user" placeholder="账号（演示：admin）"><input name="pass" type="password" placeholder="口令（演示：shop123）">
<button>登录</button></form><p class="muted">演示账号 admin / shop123</p></div>`)
	})

	mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie("shop_session")
		if err == nil {
			if user, ok := store.Valid(ck.Value); ok {
				page(w, "后台", fmt.Sprintf(`<div class="card"><h2>运营后台</h2><p>欢迎，%s。当前在线商品 %d 件。</p></div>`, user, len(products)))
				return
			}
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})

	mux.HandleFunc("/api/orders", func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie("shop_session")
		if err == nil {
			if _, ok := store.Valid(ck.Value); ok {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `[{"order":"SO-2026-0929","amount":499,"status":"已发货"},{"order":"SO-2026-0930","amount":2199,"status":"待支付"}]`)
				return
			}
		}
		http.Error(w, `{"error":"未登录"}`, http.StatusUnauthorized)
	})

	return mux
}
