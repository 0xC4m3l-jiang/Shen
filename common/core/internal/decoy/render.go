package decoy

import (
	"fmt"
	"strings"
)

// 本文件是**媒介适配器**：把 `PlacementIntent` 渲染成可粘贴的交付字节。
//
// 与 placement.go 的分工是一条硬线：
//
//	placement.go  回答「投什么媒介、哪个形态、放在哪」—— 领域决策；
//	render.go     回答「这段字节长什么样」—— 语法与转义，跟领域无关。
//
// 加一种媒介或调一次措辞只需要动本文件；领域层不认识 Nginx、SQL 或 HTML。

// templateKey 是模板表的键：媒介 + 形态。用 (media, form) 而不是只按媒介，
// 因为同一媒介可以有多种形态（HTML 就有「可见区块」与「隐藏链接」两种，字节完全不同）。
type templateKey struct {
	media PlacementMedia
	form  string
}

// templates 是**媒介模板表**：唯一的字节来源。
//
// 新增媒介 = 在这里加一条；`TestEveryIntentHasTemplate` 会遍历领域层能产出的全部意图
// 断言「每条都有模板」，所以漏加模板会在门禁里红，不会静默丢投放。
var templates = map[templateKey]func(PlacementIntent) string{
	{MediaHTML, formLinkSection}: func(it PlacementIntent) string {
		return fmt.Sprintf(`<section id="dev-api"><a href="%s">Developer API</a></section>`, it.Target)
	},
	{MediaHTML, formHiddenLink}: func(it PlacementIntent) string {
		return fmt.Sprintf(`<a href="%s" style="display:none">下载</a>`, it.Target)
	},
	{MediaNginx, formProxyPass}: func(it PlacementIntent) string {
		return fmt.Sprintf("location %s { proxy_pass %s; }", it.Target, it.Value)
	},
	{MediaShell, formComment}: func(PlacementIntent) string {
		return "" // 只有注释行（由 commentPrefix 产出）
	},
	{MediaShell, formEchoFile}: func(it PlacementIntent) string {
		return fmt.Sprintf("echo '%s' > ./%s", it.Value, strings.TrimPrefix(it.Target, "/"))
	},
	{MediaJSON, formToolConfig}: func(it PlacementIntent) string {
		return fmt.Sprintf(`{"mcpServers":{"site-tools":{"url":"%s"}}}`, it.Target)
	},
	{MediaJS, formFetchPage}: func(it PlacementIntent) string {
		return fmt.Sprintf("fetch('%s?page=1')", it.Target)
	},
	{MediaSQL, formInsertRow}: func(it PlacementIntent) string {
		return fmt.Sprintf("INSERT INTO app_accounts(login, secret) VALUES ('%s', '%s');", it.Target, it.Value)
	},
}

// commentPrefix 把说明文字包装成该媒介的注释语法（前后缀成对）。
var commentPrefix = map[PlacementMedia][2]string{
	MediaHTML:  {"<!-- ", " -->"},
	MediaNginx: {"# ", ""},
	MediaShell: {"# ", ""},
	MediaJSON:  {"// ", ""},
	MediaJS:    {"// ", ""},
	MediaSQL:   {"-- ", ""},
}

// Render 把意图集合渲染成投放片段（保持输入顺序；空产出的意图被跳过）。
//
// 「空产出」只有两种合法情形：形态只有注释（formComment），或目标为空（不投到空位置）。
// 形态没有模板**不算**合法情形 —— 它由 TestEveryIntentHasTemplate 在测试里拦住。
func Render(intents []PlacementIntent) []string {
	out := make([]string, 0, len(intents))
	for _, it := range intents {
		if normalizeTarget(it.Target) == "" {
			continue
		}
		tpl, ok := templates[templateKey{it.Media, it.Form}]
		if !ok {
			continue
		}
		body := tpl(it)
		comment := commentFor(it)
		switch {
		case comment == "" && body == "":
			continue
		case comment == "":
			out = append(out, body)
		case body == "":
			out = append(out, comment)
		default:
			out = append(out, comment+"\n"+body)
		}
	}
	return out
}

// commentFor 按媒介的注释语法包装说明文字；无说明时返回空串。
func commentFor(it PlacementIntent) string {
	if strings.TrimSpace(it.Note) == "" {
		return ""
	}
	brackets, ok := commentPrefix[it.Media]
	if !ok {
		return ""
	}
	return brackets[0] + it.Note + brackets[1]
}
