// Package web 只放**静态资源**（控制台页面），以 embed 方式编进二进制。
//
// 为什么单独成包：`go:embed` 的路径**相对包目录**且不允许 `..`，
// 所以资源必须与声明它的包放在一起。这样控制台就是一个**单文件二进制**，没有前端构建步骤。
package web

import _ "embed"

// IndexHTML 是控制台首页（纯静态 HTML + 原生 fetch；不引入任何前端工具链）。
//
//go:embed index.html
var IndexHTML []byte
