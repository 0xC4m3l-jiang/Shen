# scripts/ 下各 shell 脚本共用的小函数（POSIX sh；只被 source，不单独执行）。
#
# 用法：
#   . "$ROOT/scripts/lib.sh"
#   ensure_go; ensure_node          # PATH 里没有 go / npm 时按常见安装位置自寻址
#   wait_for "管控台" 60 http_ok http://127.0.0.1:9445/healthz
#
# 为什么要工具链自寻址：IDE 内置终端、CI 最小环境的 PATH 里常常没有 Go / Node。
# 这里按「PATH 已有 ⇒ 常见安装位置」查找，找到就把该目录插到 PATH 最前；都找不到才报错。

# python3 需要 ≥3.13（L4 的锁定依赖要求）；系统自带的往往过旧，找到新版目录就前置。
ensure_python3() {
	_hint=$1; shift
	if command -v python3 >/dev/null 2>&1; then
		_v=$(python3 -c 'import sys;print("%d.%d" % sys.version_info[:2])' 2>/dev/null || echo 0.0)
		_maj=${_v%%.*}; _min=${_v#*.}
		if [ "$_maj" -gt 3 ] || { [ "$_maj" -eq 3 ] && [ "$_min" -ge 13 ]; }; then
			return 0
		fi
	fi
	for _dir in "$@"; do
		if [ -x "$_dir/python3" ]; then
			PATH="$_dir:$PATH"
			export PATH
			return 0
		fi
	done
	echo "$_SCRIPT_NAME: 找不到 python3 ≥3.13（$_hint）" >&2
	return 1
}

_SCRIPT_NAME=${0##*/}

die() {
	echo "${_SCRIPT_NAME}: $*" >&2
	exit 1
}

# ensure_tool 工具名 提示 [候选目录...]
ensure_tool() {
	_tool=$1
	_hint=$2
	shift 2
	command -v "$_tool" >/dev/null 2>&1 && return 0
	for _dir in "$@"; do
		if [ -x "$_dir/$_tool" ]; then
			PATH="$_dir:$PATH"
			export PATH
			return 0
		fi
	done
	echo "${_SCRIPT_NAME}: 找不到 $_tool（$_hint）" >&2
	return 1
}

ensure_go() {
	ensure_tool go "请安装 Go 1.26+ 或把它加进 PATH" \
		"$HOME/.workbuddy/binaries/go/go/bin" /usr/local/go/bin /opt/homebrew/bin
}

ensure_node() {
	# shellcheck disable=SC2086 # 通配符需要展开成多个候选目录
	ensure_tool npm "请安装 Node.js 20+ 或把它加进 PATH" \
		"$HOME"/.workbuddy/binaries/node/versions/*/bin /opt/homebrew/bin /usr/local/bin
}

# 随机串：优先 openssl，退回 python3。
random_secret() {
	if command -v openssl >/dev/null 2>&1; then
		openssl rand -hex 24
	else
		python3 -c 'import secrets;print(secrets.token_hex(24))'
	fi
}

http_ok() { curl -sf -m 3 -o /dev/null "$1"; }

port_listening() { lsof -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1; }

# wait_for 描述 超时秒数 命令...：命令成功即返回 0；超时返回 1（由调用方决定怎么报错）。
wait_for() {
	_desc=$1
	_limit=$2
	shift 2
	_i=0
	while [ "$_i" -lt "$_limit" ]; do
		"$@" >/dev/null 2>&1 && return 0
		_i=$((_i + 1))
		sleep 1
	done
	echo "${_SCRIPT_NAME}: 等待 ${_desc} 超时（${_limit}s）" >&2
	return 1
}
