// Package modules 解析 `docs/design/modules.md` §1.1 的**模块清单**（模块 ↔ 源码目录 ↔ 文档）。
//
// 为什么单独成一个包：**两个**检查要用同一张表 ——
// `scripts/archcheck` 核「目录与清单一致（MD-18/MD-19）」，`scripts/verify` 核
// 「每个模块都有文档、单测与功能场景（MD-17/MD-22）」。各写一份解析器就会漂移：
// 表格格式一变，两份解析器的行为可能不同，而它们本该说同一件事。
//
// 纪律（与其它检查一致）：**清单从文档解析，不硬编码**；解析结果明显偏少时**报错**，
// 绝不静默放行 —— 静默通过等于假绿。
package modules

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Module 是 modules.md §1.1 的一行。
type Module struct {
	Name   string // 模块名，如 judge
	Layer  string // 层，如 核心 / L1 / L4
	Langs  string // 声明的语言，如 Go / Python / 配置
	SrcDir string // 源码目录（仓库存根相对），如 common/core/internal/judge/
	Doc    string // 模块文档路径，如 docs/modules/judge.md
	Stage  string // 阶段 1 / 2a / 2b / 3
}

// clean 去掉单元格里的 markdown 修饰：`**bold**`、反引号与残留的星号。
//
// 不去掉的话，`**`adapter-proxy`**` 这种写法会原样进入报告，谁都没法拿它当模块名去查东西。
func clean(s string) string {
	return strings.Trim(strings.ReplaceAll(s, "**", ""), "` *")
}

// MinExpected 是「解析结果至少该有多少个模块」的下界（当前 24 个有效模块）。
//
// 明显偏少就报错：表格格式变了却悄无声息地少解析出十几个模块，
// 会让依赖这张表的检查全部**静默变松**。
const MinExpected = 15

// Section 取出 `start` 与 `end` 两个标题之间的正文；找不到 `start` 返回空串。
func Section(body, start, end string) string {
	i := strings.Index(body, start)
	if i < 0 {
		return ""
	}
	rest := body[i+len(start):]
	if j := strings.Index(rest, end); j >= 0 {
		return rest[:j]
	}
	return rest
}

// Parse 解析 modules.md §1.1 的表格，返回有效模块（已废弃/已合并的行被跳过）。
func Parse(path string) ([]Module, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sec := Section(string(b), "### 1.1 源码模块", "### 1.2")
	if sec == "" {
		return nil, errors.New("找不到 §1.1 源码模块 小节")
	}
	var out []Module
	for _, line := range strings.Split(sec, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(line, "|")
		// | # | 模块 | 层 | 语言 | 源码目录 | 模块文档 | 职责 | 阶段 |
		if len(cells) < 9 {
			continue
		}
		src := strings.Trim(strings.TrimSpace(cells[5]), "`")
		if !strings.Contains(src, "/") {
			continue // 表头或分隔行
		}
		name := strings.Trim(strings.TrimSpace(cells[2]), "`")
		// 已废弃 / 已合并的行用删除线标记（同 D-6：保留编号但不再是有效模块）。
		// 这类行不参与比对，否则会报出幽灵模块。
		if strings.Contains(name, "~~") || strings.Contains(src, "~~") {
			continue
		}
		out = append(out, Module{
			Name:   clean(name),
			Layer:  clean(strings.TrimSpace(cells[3])),
			Langs:  clean(strings.TrimSpace(cells[4])),
			SrcDir: strings.Trim(clean(src), "~"),
			Doc:    clean(strings.TrimSpace(cells[6])),
			Stage:  strings.Trim(clean(strings.TrimSpace(cells[8])), "*"),
		})
	}
	if len(out) < MinExpected {
		return nil, fmt.Errorf("只解析到 %d 个模块，明显偏少 —— 文档格式可能变了，拒绝静默放行", len(out))
	}
	return out, nil
}
