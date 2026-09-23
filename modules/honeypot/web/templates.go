package web

import "html/template"

// 本文件是**场景的样式**：HTML 模板随代码分发（`ST-22`：模板**禁止**放在运行期可变存储）。
//
// 三条纪律：
//
//	① 全部走 `html/template`（自动转义）—— 合成页面同样不该有注入面；
//	② **不出现**任何标识我们实现的字样（`OH-1` / `OH-2`：判据是「会不会出现在攻击者的屏幕上」）；
//	③ 不引外部资源（字体/CDN/统计）：诱饵后端**不发起任何出站连接**（隔离约束的一部分），
//	   页面自包含 ⇒ 引用外链等于暴露一个可被对手观察的信标。

const pageHead = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Product}} · {{.Org}}</title>
<style>
 body{font:14px/1.5 system-ui,-apple-system,Segoe UI,Roboto,sans-serif;margin:0;color:#1c2430;background:#f4f6f9}
 header{background:#22303f;color:#e8eef5;padding:14px 22px;display:flex;gap:18px;align-items:center}
 header b{font-weight:600}
 header nav a{color:#b9c9da;text-decoration:none;margin-right:14px}
 main{max-width:940px;margin:26px auto;background:#fff;border:1px solid #dde3ea;border-radius:6px;padding:22px 26px}
 table{border-collapse:collapse;width:100%}
 th,td{text-align:left;padding:8px 10px;border-bottom:1px solid #e6ebf1}
 th{background:#f7f9fc;font-weight:600;color:#455468}
 code{background:#f2f4f7;padding:1px 5px;border-radius:3px}
 .muted{color:#6b7a8c}
 .err{color:#a4342c;font-weight:600}
 footer{padding:12px 22px;color:#7b8798}
 label{display:block;margin:10px 0 4px;color:#455468}
 input{width:100%;padding:8px 10px;border:1px solid #cbd4de;border-radius:4px}
 button{margin-top:16px;padding:9px 16px;background:#22303f;color:#fff;border:0;border-radius:4px;cursor:pointer}
</style></head><body>`

// tmplLogin 是合成登录页：没有框架指纹、没有外部资源、字段与真实产品同类。
var tmplLogin = template.Must(template.New("login").Parse(pageHead + `
<header><b>{{.Product}}</b><span class="muted">{{.Org}}</span></header>
<main>
  <h2>Sign in</h2>
  <p class="muted">Console access is restricted to organisation members.</p>
  <form method="post" action="/admin/login" autocomplete="off">
    <label for="username">Username</label>
    <input id="username" name="username" type="text" required>
    <label for="password">Password</label>
    <input id="password" name="password" type="password">
    <button type="submit">Continue</button>
  </form>
</main>
<footer>{{.Org}} internal systems</footer></body></html>`))

// tmplDashboard 是控制台首页（需合成会话）。
var tmplDashboard = template.Must(template.New("dash").Parse(pageHead + `
<header><b>{{.Product}}</b><span class="muted">{{.Org}}</span>
 <nav><a href="/admin/users">Users</a><a href="/admin/config">Configuration</a><a href="/admin/audit">Audit log</a></nav>
</header>
<main>
  <h2>Overview</h2>
  <p class="muted">Deployment <code>{{.Host}}</code></p>
  <table><tr><th>Section</th><th>Entries</th></tr>
   <tr><td>Users</td><td>{{.Users}}</td></tr>
   <tr><td>Configuration</td><td>{{.Config}}</td></tr>
   <tr><td>Audit log</td><td>{{.Audit}}</td></tr>
  </table>
</main>
<footer>{{.Org}} internal systems</footer></body></html>`))

// tmplList 是合成列表页（用户 / 配置 / 审计共用；分页元数据与 API 同源）。
var tmplList = template.Must(template.New("list").Parse(pageHead + `
<header><b>{{.Product}}</b><span class="muted">{{.Org}}</span>
 <nav><a href="/admin/users">Users</a><a href="/admin/config">Configuration</a><a href="/admin/audit">Audit log</a></nav>
</header>
<main>
  <h2>{{.Title}}</h2>
  <table>
    <tr>{{range .Headers}}<th>{{.}}</th>{{end}}</tr>
    {{range .Rows}}<tr>{{range .Cells}}<td>{{.}}</td>{{end}}</tr>{{end}}
  </table>
  <p class="muted">Page {{.Meta.Page}} · {{.Meta.Total}} entries · page size {{.Meta.PageSize}}</p>
  {{if .Meta.PrevHref}}<a href="{{.Meta.PrevHref}}">Previous</a>{{end}}
  {{if .Meta.HasNext}}<a href="{{.Meta.NextHref}}">Next</a>{{end}}
</main>
<footer>{{.Org}} internal systems</footer></body></html>`))

// tmplError 是合成错误页：中性措辞、无栈信息、无实现指纹。
var tmplError = template.Must(template.New("err").Parse(pageHead + `
<header><b>Console</b></header>
<main>
  <h2 class="err">{{.Title}}</h2>
  <p class="muted">{{.Detail}}</p>
  <p class="muted">Reference: {{.Code}}</p>
</main>
<footer>internal systems</footer></body></html>`))
