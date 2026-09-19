#!/bin/sh
# 格式化检查：只要有一个 Go 文件没格式化就失败，并且不改任何文件。
#
# 为什么单独写成脚本而不是塞进 Makefile recipe：
# recipe 里的命令替换必须写成 $$(...)，容易和引号、转义、变量作用域纠缠；
# 放进真实的 shell 文件后可以被 shellcheck 正经检查，也不会被静态分析误报。
set -eu

files=$(gofmt -l .)
if [ -n "$files" ]; then
	echo "以下文件未格式化 —— 跑 make fmt 修正："
	echo "$files"
	exit 1
fi

echo "fmt-check 通过"
