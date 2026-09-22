// Package docs 回答一个很窄的问题：这份检出的仓库里，**设计文档在不在**。
//
// 为什么需要它：本仓库只发布**项目本身的代码与运行 / 测试逻辑** —— 设计文档（docs/）、
// Agent 工作流规范（AGENTS.md、.pi/）与门禁工具的预编译副本都不入库（见 .gitignore）。
// 维护者本机有这些文档，所以那里的门禁应当**照常全跑**；别人 clone 出来没有文档时，
// 依赖文档的检查要**跳过并打印一行说明** —— 不静默、也不算通过。
package docs

import (
	"os"
	"path/filepath"
)

// marker 是「文档齐备」的判据：模块清单是几乎所有文档类检查的入口。
const marker = "docs/design/modules.md"

// Present 报告设计文档是否可用（以模块清单为准）。
func Present(root string) bool {
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(marker)))
	return err == nil
}

// SkipNote 是跳过时打印的那一行。统一文案，免得各工具各说各话、让人以为门禁坏了。
const SkipNote = "跳过：设计文档不在本仓库（docs/ 不入库；见 README §3.3 与 .gitignore）—— 本项检查依赖它"
