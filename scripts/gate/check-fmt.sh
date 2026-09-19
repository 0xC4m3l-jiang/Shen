#!/bin/sh
# 格式化检查：只要有一个**本仓库的** Go 文件没格式化就失败，并且不改任何文件。
#
# 为什么单独写成脚本而不是塞进 Makefile recipe：
# recipe 里的命令替换必须写成 $$(...)，容易和引号、转义、变量作用域纠缠；
# 放进真实的 shell 文件后可以被 shellcheck 正经检查，也不会被静态分析误报。
#
# 为什么按「包目录」而不是 `gofmt -l .`：
# `vendor/` 里是**第三方副本**，不按本仓库的格式标准要求（也不该由我们去改）。
# `go list ./...` 本身就不包含 vendor 目录，所以用它列出的包目录来查最省事、也最准确。
set -eu

pkgs=$(go list -f '{{.Dir}}' ./...)
files=$(gofmt -l $pkgs)
if [ -n "$files" ]; then
	echo "以下文件未格式化 —— 跑 make fmt 修正："
	echo "$files"
	exit 1
fi

echo "fmt-check 通过"
