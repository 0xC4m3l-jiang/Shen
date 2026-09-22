package decoy

import (
	"strings"

	"shen/common/core/internal/contract"
)

// 本文件是**领域层对投放的表达**：只回答「投什么媒介、用哪个形态、放在哪、值是什么」，
// 不产出任何交付字节。
//
// 为什么要拆开（D05）：改造前 `placements()` 直接 `fmt.Sprintf` 出 Nginx 配置行、shell 命令、
// SQL 语句与 HTML 片段 —— 于是「换一种投放媒介」要改领域层的 switch，「调整片段措辞」也要改它。
// 现在领域层只给 `PlacementIntent`，字节交给 `render.go` 的媒介模板表（加媒介 = 加一条模板）。
//
// 另一个直接收益：**可测性**。意图是数据，可以断言「投了几个、什么媒介、指向哪」，
// 不必用字符串 contains 猜（例如「片段里有路径」不等于「投的是对的媒介」）。

// PlacementMedia 是投放媒介（交付字节的语法族）。
type PlacementMedia string

const (
	// MediaHTML 是 HTML 片段（嵌入页面）。
	MediaHTML PlacementMedia = "html"
	// MediaNginx 是 Nginx 配置片段（运维粘贴）。
	MediaNginx PlacementMedia = "nginx"
	// MediaShell 是 shell 命令 / 注释（仓库与手册）。
	MediaShell PlacementMedia = "shell"
	// MediaJSON 是 JSON 片段（工具配置）。
	MediaJSON PlacementMedia = "json"
	// MediaJS 是 JavaScript 片段（前端与手册）。
	MediaJS PlacementMedia = "js"
	// MediaSQL 是 SQL 片段（配置包与手册）。
	MediaSQL PlacementMedia = "sql"
)

// 媒介内的形态名。领域层选形态，媒介适配器决定它在字节上长什么样。
const (
	formLinkSection = "link_section" // 页面里可见的入口区块
	formHiddenLink  = "hidden_link"  // 隐藏链接
	formProxyPass   = "proxy_pass"   // Nginx 反代到诱饵
	formComment     = "comment"      // 只有一行注释（说明投放位置）
	formEchoFile    = "echo_file"    // shell 写文件
	formToolConfig  = "tool_config"  // JSON 工具配置
	formFetchPage   = "fetch_page"   // JS 分页请求
	formInsertRow   = "insert_row"   // SQL 插入行
)

// PlacementIntent 是一次投放的**意图**：媒介 + 形态 + 目标 + 值 + 给人看的说明。
//
// 字段语义（刻意只有这五个，避免它退化成「什么都装」的袋子）：
//
//	Media   投放媒介（决定由哪张媒介模板渲染）
//	Form    媒介内的形态名（同一媒介可有多种形态，例如 HTML 的可见区块与隐藏链接）
//	Target  投放目标：诱饵的触发路径或资源标识
//	Value   诱饵自身的值（内容 / 参数）；只有注释类意图时为空
//	Note    说明文字；非空时渲染成该媒介的注释行
type PlacementIntent struct {
	Media  PlacementMedia
	Form   string
	Target string
	Value  string
	Note   string
}

// intentsFor 按诱饵形态给出投放意图（纯函数：同资产恒同意图，`AR-30`）。
//
// 这里是**唯一**表达「哪类诱饵该投在哪」的地方；它不关心 Nginx 语法、SQL 引号或 HTML 转义。
func intentsFor(a contract.DecoyAsset) []PlacementIntent {
	switch a.Kind {
	case contract.DecoyDeveloperAPI:
		return []PlacementIntent{
			{
				Media:  MediaHTML,
				Form:   formLinkSection,
				Target: a.Path,
				Note:   "页脚开发者 API（可见）",
			},
			{
				Media:  MediaNginx,
				Form:   formProxyPass,
				Target: a.Path,
				Value:  a.Content,
				Note:   "nginx：把开发者 API 暴露给自动化客户端",
			},
		}
	case contract.DecoyInstructionFile:
		return []PlacementIntent{
			{
				Media:  MediaShell,
				Form:   formComment,
				Target: a.Path,
				Note:   "投放位置：仓库根 / 文档目录（" + a.Path + "）",
			},
			{
				Media:  MediaShell,
				Form:   formEchoFile,
				Target: a.Path,
				Value:  a.Content,
			},
		}
	case contract.DecoyMCP:
		return []PlacementIntent{
			{
				Media:  MediaJSON,
				Form:   formToolConfig,
				Target: a.Path,
				Note:   "MCP 诱饵：把工具服务端点写进开发者文档",
			},
		}
	case contract.DecoyDataset:
		return []PlacementIntent{
			{
				Media:  MediaJS,
				Form:   formFetchPage,
				Target: a.Path,
				Note:   "消耗战数据集：在 JS / 手册中暴露分页接口",
			},
		}
	case contract.DecoyBait:
		return []PlacementIntent{
			{
				Media:  MediaSQL,
				Form:   formInsertRow,
				Target: a.Path,
				Value:  a.Content,
				Note:   "投放位置：配置包 / 手册",
			},
			{
				Media:  MediaHTML,
				Form:   formHiddenLink,
				Target: a.Path,
			},
		}
	default:
		return nil
	}
}

// normalizeTarget 去掉路径两侧空白；空路径的意图不投放（宁可不投，也不投到空位置）。
func normalizeTarget(s string) string {
	return strings.TrimSpace(s)
}
