#!/bin/sh
# 本机开发栈（不需要 Docker）：核心 + 影子代理 + 假上游 + 管控台 API + 连接器网关 + Vite 前端。
#
# 与 scripts/shen.sh 的分工：那边是**容器栈**（Docker），这边是**本机直跑** ——
# 改了 Go / 前端代码后想立刻在浏览器里看效果，用这个。
#
# 整栈：
#   scripts/dev.sh up            编译并起整栈，等全部就绪（已在跑 ⇒ 先停再起）
#   scripts/dev.sh update        改完 Go 代码后：重新编译 → 滚动重启后端（编译失败时旧进程保持运行）
#                                前端源码由 Vite 热更新，不用重启；package-lock 变了会自动 npm ci 并重启 Vite
#   scripts/dev.sh status        进程 / 端口 / 健康检查一览
#   scripts/dev.sh stop          停整栈（含网关 / 连接器）
#   scripts/dev.sh logs [名] [N] 看日志尾部（名：core proxy console ui upstream gateway connector；默认 core）
#   scripts/dev.sh seed          经业务入口造 3 条判定流量（判定流 / 链路页有数据可点）
#   scripts/dev.sh tunnel        反向隧道端到端：签发凭证 → 起连接器 → 真流量过隧道验证
#
# 前端（UI）：
#   scripts/dev.sh ui up         起 Vite；若管控台 API 不可达，**自动把最小后端也拉起来**
#                                （最小后端 = 核心 + 管控台 API；代理 / 网关 / 连接器都不需要）
#   scripts/dev.sh ui check      验证 UI 显示：HTTP → 登录 → 浏览器逐页渲染 + 截图（scripts/check/ui.py）
#                                后端没起时会先自动拉起最小后端 ⇒ 单独跑这一条就能逐页验证
#   scripts/dev.sh ui update     重装前端依赖（npm ci）并重启 Vite，随后做一次 HTTP 层检查
#   scripts/dev.sh ui test       前端类型检查 + 单测（vue-tsc + vitest；不需要后端）
#   scripts/dev.sh ui stop       停 Vite，并收掉 `ui up` 自动起的最小后端
#
# 只要前端、后端在别处（Docker 栈等）：
#   SHEN_CONSOLE_API_URL=http://127.0.0.1:19444 scripts/dev.sh ui up
#   注意：后端若设了 SHEN_CONSOLE_ALLOWED_ORIGINS，必须包含 http://127.0.0.1:5173，
#   否则登录会 403 origin_rejected（GET 一般能过，容易被误判成前端 bug）。
#   完全不要后端：SHEN_UI_NO_BACKEND=1 scripts/dev.sh ui up
#
# 端口（可用环境变量覆盖）：
#   上游 9000 · 核心 9443 · 业务入口 18080 · 管控台 API 9445 · 前端 5173 · 网关 TLS 9446 · 网关字节桥 9447
#   例：SHEN_LOCAL_VITE_PORT=5174 scripts/dev.sh up
#
# 管控台账号：首次 admin/admin（首登强制改密）。`tunnel` / `ui check` 需要登录时会把它改成
# ${SHEN_LOCAL_NEW_PASS:-Local-Stack-2026!} 并记在 <临时目录>/console-pass（只针对这个一次性本地栈）。
# `up` / `ui up` / `ui check` 结束时会先**验证**再打印当前可用的账号口令（口令可能被人在界面上改过，
# 直接照抄记录会给你一个登不进去的口令）；实在验不出来就明说「口令未知」并给出重置办法。
# 临时产物（二进制 / 配置 / 证书 / 日志 / 控制台数据）全部落在 ${TMPDIR:-/tmp}/shen-<uid>/local-stack/，
# 仓库里不留任何文件。
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
. "$ROOT/scripts/lib.sh"
cd "$ROOT"

RUNDIR=${TMPDIR:-/tmp}
RUNDIR=${RUNDIR%/}/shen-$(id -u)/local-stack
LOGDIR="$RUNDIR/logs"
DATA="$RUNDIR/console-data"
PIDDIR="$RUNDIR/pids"
PASS_FILE="$RUNDIR/console-pass"
UI_DIR="$ROOT/modules/console/ui"

UPSTREAM_PORT=${SHEN_LOCAL_UPSTREAM_PORT:-9000}
TUNNEL_UPSTREAM_PORT=${SHEN_LOCAL_TUNNEL_UPSTREAM_PORT:-9100}
CORE_ADDR=${SHEN_LOCAL_CORE_ADDR:-127.0.0.1:9443}
PROXY_ADDR=${SHEN_LOCAL_PROXY_ADDR:-127.0.0.1:18080}
CONSOLE_ADDR=${SHEN_LOCAL_CONSOLE_ADDR:-127.0.0.1:9445}
GATEWAY_ADDR=${SHEN_LOCAL_GATEWAY_ADDR:-127.0.0.1:9446}
BRIDGE_ADDR=${SHEN_LOCAL_BRIDGE_ADDR:-127.0.0.1:9447}
VITE_PORT=${SHEN_LOCAL_VITE_PORT:-5173}
# 隧道演示用的域名（签凭证 + 连接器声明 + 验证请求三处一致）。
TUNNEL_HOST=${SHEN_LOCAL_TUNNEL_HOST:-tunnel.local.test}
NEW_PASS=${SHEN_LOCAL_NEW_PASS:-Local-Stack-2026!}
CONSOLE_USER=${SHEN_CONSOLE_USER:-admin}

CORE_PORT=${CORE_ADDR#*:}
PROXY_PORT=${PROXY_ADDR#*:}
CONSOLE_PORT=${CONSOLE_ADDR#*:}
GATEWAY_PORT=${GATEWAY_ADDR#*:}
BRIDGE_PORT=${BRIDGE_ADDR#*:}
ENTRY="http://127.0.0.1:${PROXY_PORT}"
CONSOLE_URL="http://127.0.0.1:${CONSOLE_PORT}"
UI_URL="http://127.0.0.1:${VITE_PORT}"
API_TARGET=${SHEN_CONSOLE_API_URL:-http://${CONSOLE_ADDR}}

# ── 进程管理 ─────────────────────────────────────────────────────────────────
# 后台进程一律**先编译再直跑**，不用 `go run`：它的 PID 是包装器的，kill 会留孤儿
# （check/smoke.sh 2026-09-22 实测泄漏过 51 个核心进程，同一个坑不踩第二次）。

start_bg() { # 名 命令...
	_n=$1
	shift
	: >"$LOGDIR/$_n.log"
	"$@" >"$LOGDIR/$_n.log" 2>&1 &
	echo $! >"$PIDDIR/$_n.pid"
}

pid_of() { [ -f "$PIDDIR/$1.pid" ] && cat "$PIDDIR/$1.pid" || true; }

alive() {
	_p=$(pid_of "$1")
	[ -n "$_p" ] && kill -0 "$_p" 2>/dev/null
}

# term_wait PID...：先 TERM，最多等 5 秒；还活着就 KILL。
# 实测踩过：长连接没断时网关的优雅退出会一直挂着，只发 TERM 的话端口永远释放不了，下次 up 直接 bind 失败。
term_wait() {
	[ $# -gt 0 ] || return 0
	kill "$@" 2>/dev/null || true
	_i=0
	while [ "$_i" -lt 10 ]; do
		_left=""
		for _x in "$@"; do kill -0 "$_x" 2>/dev/null && _left="$_left $_x"; done
		[ -n "$_left" ] || return 0
		_i=$((_i + 1))
		sleep 0.5
	done
	# shellcheck disable=SC2086 # _left 是空格分隔的 PID 列表
	kill -9 $_left 2>/dev/null || true
}

kill_port() {
	[ -n "${1:-}" ] || return 0
	# shellcheck disable=SC2046 # 需要按空白拆成多个 PID
	term_wait $(lsof -ti tcp:"$1" -sTCP:LISTEN 2>/dev/null || true)
}

kill_one() { # 名 [端口] —— 先按 PID 杀，再按监听端口兜底（npm/vite 会派生子进程）
	_p=$(pid_of "$1")
	[ -z "$_p" ] || term_wait "$_p"
	kill_port "${2:-}"
	# 端口可能被「TERM 之后才开始真正退出」的子进程占着（vite 尤其明显）：
	# 再确认几轮，直到端口真的释放 —— 否则紧接着的 ui up / ui check 会撞上一个半死的进程。
	if [ -n "${2:-}" ]; then
		_i=0
		while port_listening "$2" && [ "$_i" -lt 6 ]; do
			kill_port "$2"
			sleep 0.5
			_i=$((_i + 1))
		done
	fi
	rm -f "$PIDDIR/$1.pid"
}

need_stack() {
	alive core || die "栈没在跑，先 scripts/dev.sh up"
}

# ── 编译与配置 ───────────────────────────────────────────────────────────────

# build_backend：先编到 new/，全部成功才替换 —— update 时编译失败不影响正在跑的旧进程。
build_backend() {
	ensure_go || exit 1
	mkdir -p "$RUNDIR/new"
	echo "== 编译（核心 / 代理 / 管控台 / 网关${1:+ / 连接器}）=="
	go build -o "$RUNDIR/new/core" ./common/core/cmd/core || die "核心编译失败"
	go build -o "$RUNDIR/new/proxy" ./modules/deception/proxy/cmd/proxy || die "代理编译失败"
	go build -o "$RUNDIR/new/console" ./modules/console/cmd/console || die "管控台编译失败"
	go build -o "$RUNDIR/new/gateway" ./modules/connector/gateway/cmd/gateway || die "网关编译失败"
	if [ -n "${1:-}" ]; then
		go build -o "$RUNDIR/new/connector" ./modules/connector/cmd/shen-connector || die "连接器编译失败"
	fi
	mv "$RUNDIR"/new/* "$RUNDIR"/
}

# 集成令牌与自签证书：一次生成、跨重启复用（换了令牌控制台数据里的会话态就乱了）。
ensure_materials() {
	mkdir -p "$RUNDIR" "$LOGDIR" "$PIDDIR" "$DATA"
	[ -s "$RUNDIR/integration-token" ] || random_secret >"$RUNDIR/integration-token"
	if [ ! -s "$RUNDIR/gw-cert.pem" ] || [ ! -s "$RUNDIR/gw-key.pem" ]; then
		openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
			-keyout "$RUNDIR/gw-key.pem" -out "$RUNDIR/gw-cert.pem" \
			-subj "/CN=localhost" >/dev/null 2>&1 || die "生成网关自签证书失败（需要 openssl）"
	fi
	# 核心配置：与 check/smoke.sh 同一套开发口径（影子模式、内存存储、两条规则）。
	cat >"$RUNDIR/core.yaml" <<YAML
core:
  listen: "${CORE_ADDR}"
shadow: true
session:
  cookie_name: "sid"
thresholds:
  route_mirage: 0.70
  block: 0.95
guard:
  false_route_budget: 0.001
store:
  driver: "memory"
  redis: {addr: "", password: ""}
  clickhouse: {addr: "", database: ""}
  postgres: {dsn: ""}
policy:
  policy_id: "local-stack"
  version: 1
  gray_pct: 0
rules:
  - id: "ua-headless"
    weight: 0.6
    match: {field: "user_agent", op: "contains", value: "HeadlessChrome"}
  - id: "path-git"
    weight: 0.3
    match: {field: "path", op: "prefix", value: "/.git"}
whitelist:
  source_cidrs: []
  user_agents: []
  path_prefixes: []
YAML
}

# ── 各服务的启动（up 与 update 共用同一份启动参数）─────────────────────────────

start_upstream() {
	kill_one upstream "$UPSTREAM_PORT"
	start_bg upstream python3 -m http.server "$UPSTREAM_PORT" --bind 127.0.0.1 --directory "$RUNDIR"
	wait_for "上游" 30 http_ok "http://127.0.0.1:${UPSTREAM_PORT}/" || die "上游没起来：scripts/dev.sh logs upstream"
}

start_core() {
	kill_one core "$CORE_PORT"
	start_bg core env SHEN_CONFIG="$RUNDIR/core.yaml" "$RUNDIR/core"
	wait_for "核心端口" 60 port_listening "$CORE_PORT" || die "核心没起来：scripts/dev.sh logs core"
}

start_proxy() {
	kill_one proxy "$PROXY_PORT"
	start_bg proxy env SHEN_PROXY_UPSTREAM="http://127.0.0.1:${UPSTREAM_PORT}" \
		SHEN_PROXY_LISTEN="$PROXY_ADDR" SHEN_CORE_ADDR="$CORE_ADDR" \
		SHEN_PROXY_SHADOW=true SHEN_PROXY_TUNNEL_GATEWAY="$BRIDGE_ADDR" "$RUNDIR/proxy"
	wait_for "业务入口" 60 http_ok "$ENTRY/" || die "代理没起来：scripts/dev.sh logs proxy"
}

# 管控台：数据目录放临时区（账号 / 偏好在重启间保留）；不设 bootstrap ⇒ 默认 admin/admin。
# 集成令牌与网关一致（key 拉取 / 自动登记 / 会话上报走它）。
start_console() {
	kill_one console "$CONSOLE_PORT"
	start_bg console env SHEN_CORE_ADDR="$CORE_ADDR" SHEN_CONSOLE_LISTEN="$CONSOLE_ADDR" \
		SHEN_CONSOLE_DATA_DIR="$DATA" SHEN_CONSOLE_INTEGRATION_TOKEN="$(cat "$RUNDIR/integration-token")" \
		"$RUNDIR/console"
	wait_for "管控台 API" 60 http_ok "$CONSOLE_URL/healthz" || die "管控台没起来：scripts/dev.sh logs console"
}

# 连接器网关：TLS 自签（连接器侧 TLS_INSECURE）、key 表 5s 快拉（本地验证要即时生效）。
start_gateway() {
	kill_one gateway "$GATEWAY_PORT"
	kill_port "$BRIDGE_PORT" # 桥与网关同进程：防只占桥不占 TLS 口的异常残留
	start_bg gateway env SHEN_GATEWAY_LISTEN="$GATEWAY_ADDR" \
		SHEN_GATEWAY_TLS_CERT="$RUNDIR/gw-cert.pem" SHEN_GATEWAY_TLS_KEY="$RUNDIR/gw-key.pem" \
		SHEN_GATEWAY_BRIDGE="$BRIDGE_ADDR" SHEN_GATEWAY_CONSOLE="http://${CONSOLE_ADDR}" \
		SHEN_GATEWAY_INTEGRATION_TOKEN="$(cat "$RUNDIR/integration-token")" \
		SHEN_GATEWAY_KEY_REFRESH=5s SHEN_GATEWAY_NODE=local-stack "$RUNDIR/gateway"
	wait_for "网关字节桥" 60 http_ok "http://${BRIDGE_ADDR}/healthz" || die "网关没起来：scripts/dev.sh logs gateway"
}

# 连接器的启动参数（含凭证）由 tunnel 写进 connector.env（600 权限，只在临时目录）。
start_connector() {
	[ -f "$RUNDIR/connector.env" ] || return 0
	kill_one connector
	# shellcheck disable=SC1091 # 运行时生成的文件
	. "$RUNDIR/connector.env"
	start_bg connector env SHEN_CONNECTOR_GATEWAY="$GATEWAY_ADDR" SHEN_CONNECTOR_KEY="$CONNECTOR_KEY" \
		SHEN_CONNECTOR_UPSTREAM="http://127.0.0.1:${TUNNEL_UPSTREAM_PORT}" \
		SHEN_CONNECTOR_NAME="隧道演示" SHEN_CONNECTOR_HOSTS="$TUNNEL_HOST" \
		SHEN_CONNECTOR_TLS_INSECURE=true "$RUNDIR/connector"
}

# ── 前端 ─────────────────────────────────────────────────────────────────────

# ui_deps [force]：node_modules 缺失、或 package-lock.json 比上次安装新 ⇒ npm ci。返回 0 表示装过。
ui_deps() {
	ensure_node || exit 1
	if [ -n "${1:-}" ] || [ ! -f "$UI_DIR/node_modules/.package-lock.json" ] ||
		[ "$UI_DIR/package-lock.json" -nt "$UI_DIR/node_modules/.package-lock.json" ]; then
		echo "== 安装前端依赖（npm ci）=="
		(cd "$UI_DIR" && npm ci --silent --no-audit --no-fund) || die "npm ci 失败"
		return 0
	fi
	return 1
}

# ui_healthy：前端**稳定**在服务。
# 为什么是「连续两次都通」而不只是一次 http_ok：一个刚被 TERM、正在退出的 Vite 端口可能还开着一次，
# 只探一次会把它当成可用 —— 紧接着的逐页遍历就会在中途撞上 ERR_CONNECTION_REFUSED（实测踩过）。
ui_healthy() {
	http_ok "$UI_URL/" || return 1
	sleep 1
	http_ok "$UI_URL/"
}

# ui_start：npm run dev 会派生子进程，停止靠端口兜底。后端由 ensure_ui_backend 负责。
ui_start() {
	ensure_node || exit 1
	kill_one ui "$VITE_PORT" # 先保证端口真的空出来，再起新的
	: >"$LOGDIR/ui.log"
	(cd "$UI_DIR" && SHEN_CONSOLE_API_URL="$API_TARGET" \
		npm run dev -- --port "$VITE_PORT" --host 127.0.0.1 --strictPort) >"$LOGDIR/ui.log" 2>&1 &
	echo $! >"$PIDDIR/ui.pid"
	wait_for "前端" 90 ui_healthy || die "Vite 没起来：scripts/dev.sh logs ui"
}

ui_check() {
	python3 "$ROOT/scripts/check/ui.py" --url "$UI_URL" \
		--pass-file "$PASS_FILE" --change-pass "$NEW_PASS" "$@" || return 1
	print_login
	return 0
}

# console_login_ok 口令：这个口令真的能登录吗（POST /api/v1/auth/login）。
console_login_ok() {
	curl -sf -m 5 -o /dev/null -X POST -H 'Content-Type: application/json' \
		-d "{\"username\":\"${CONSOLE_USER}\",\"password\":\"$1\"}" \
		"$API_TARGET/api/v1/auth/login"
}

# print_login：告诉你**现在**能用哪个口令登录。
# 为什么是「验证过」而不是直接把 console-pass 打出来：口令可能被人从界面上改过（此时记录已过期），
# 报一个错的口令比不报更糟 —— 会让人以为脚本坏了。
# 代价：每次 up / check 会多一条 auth.login 审计（验证必然是一次真登录）；而账号 15 分钟内错 5 次会锁，
# 所以只在两个候选上试，且第二个候选（默认 admin）仅在没有记录时试 —— 正常情况下一次就中。
print_login() {
	http_ok "$API_TARGET/healthz" || return 0
	_pw=""
	if [ -s "$PASS_FILE" ]; then
		_pw=$(cat "$PASS_FILE")
		console_login_ok "$_pw" || _pw="" # 记录过期 ⇒ 不作数，也不去乱试别的
	elif console_login_ok admin; then
		_pw=admin # 还没改过密：默认 admin（首登强制改密）
	fi
	if [ -n "$_pw" ]; then
		if [ "$_pw" = admin ]; then
			printf '  登录       %s/  账号 %s / %s（首登强制改密）\n' "$UI_URL" "$CONSOLE_USER" "$_pw"
		else
			printf '  登录       %s/  账号 %s / %s\n' "$UI_URL" "$CONSOLE_USER" "$_pw"
		fi
		return 0
	fi
	echo "  登录       ${UI_URL}/  账号 ${CONSOLE_USER} / **口令未知**（已被改过，且不在 ${PASS_FILE} 里）"
	echo "             用 SHEN_CONSOLE_PASS=<口令> scripts/dev.sh ui check；或删掉控制台数据目录后重来："
	echo "             rm -rf ${DATA} && scripts/dev.sh ui stop && scripts/dev.sh ui up（会清空登记 / 凭证 / 审计）"
}

# ui_target_is_local：API 目标是不是「本脚本自己管的那台管控台」。
# 是，才可能自动起后端；指向 Docker 栈 / 远端时只提示 —— 不擅自在本机拉一套进程去顶替它。
ui_target_is_local() {
	case "$API_TARGET" in
	"http://${CONSOLE_ADDR}" | "http://127.0.0.1:${CONSOLE_PORT}" | "http://localhost:${CONSOLE_PORT}") return 0 ;;
	esac
	return 1
}

# ensure_ui_backend：UI 要能登录、能逐页看，至少得有「核心 + 管控台 API」。
# 这两个不可达时自动拉起来 —— 否则 `ui up` 只能看登录页，`ui check` 一路 ✗，等于白跑。
# 只要前端（例如后端在 Docker 栈 / 只调样式）：SHEN_UI_NO_BACKEND=1。
ensure_ui_backend() {
	http_ok "$API_TARGET/healthz" && return 0
	if ! ui_target_is_local; then
		echo "提示：API ${API_TARGET} 不可达，且它不是本机的管控台 —— 本脚本不会替你启动外部后端。"
		echo "      后端那边要确认来源白名单：宿主 Vite 的 Origin 是 http://127.0.0.1:${VITE_PORT}，"
		echo "      后端若设了 SHEN_CONSOLE_ALLOWED_ORIGINS 必须包含它，否则登录 403 origin_rejected。"
		return 1
	fi
	if [ -n "${SHEN_UI_NO_BACKEND:-}" ]; then
		echo "提示：SHEN_UI_NO_BACKEND 已设置 —— 只起前端（API ${API_TARGET} 不可达，登录会失败）。"
		return 1
	fi
	echo "== 管控台 API 不可达 → 自动起最小后端（核心 + 管控台 API；不起代理 / 网关 / 连接器）=="
	ensure_materials
	need_min_build && build_min_backend
	alive core || start_core
	alive console || start_console
	: >"$RUNDIR/ui-backend.started" # 记一笔：ui stop 时只收自己起的这两个
	return 0
}

# 二进制不在、或源码比它新 ⇒ 需要重编。纯改样式时不该让 `ui up` 等编译。
need_min_build() {
	[ -x "$RUNDIR/core" ] && [ -x "$RUNDIR/console" ] || return 0
	[ -n "$(find common/core modules/console common/api -name '*.go' -newer "$RUNDIR/core" -print -quit 2>/dev/null)" ]
}

# build_min_backend：ui 单独跑时只编译它需要的两个二进制（核心 + 管控台），不碰代理 / 网关。
build_min_backend() {
	ensure_go || exit 1
	echo "== 编译（核心 / 管控台）=="
	mkdir -p "$RUNDIR/new"
	go build -o "$RUNDIR/new/core" ./common/core/cmd/core || die "核心编译失败"
	go build -o "$RUNDIR/new/console" ./modules/console/cmd/console || die "管控台编译失败"
	mv "$RUNDIR"/new/* "$RUNDIR"/
}

cmd_ui() {
	mkdir -p "$LOGDIR" "$PIDDIR"
	sub=${1:-}
	[ $# -gt 0 ] && shift
	case "$sub" in
	up)
		backend_ready=""
		ensure_ui_backend && backend_ready=1
		ui_deps || true
		ui_start
		echo "前端已就绪：${UI_URL}/（API → ${API_TARGET}）"
		[ -z "$backend_ready" ] || print_login
		echo "逐页验证显示：scripts/dev.sh ui check（HTTP → 登录 → 逐页渲染 + 截图）"
		;;
	update)
		ensure_ui_backend || true
		ui_deps force || true
		ui_start
		ui_check --http-only
		echo "要看页面渲染：scripts/dev.sh ui check"
		;;
	check)
		# 单独跑 `ui check` 也成立：后端 / 前端没起就分别拉起来（规则同 up），然后逐页验证。
		ensure_ui_backend || true
		if ! ui_healthy; then
			echo "== 前端未在稳定服务 → 先起 Vite =="
			ui_deps || true
			ui_start
		fi
		ui_check "$@"
		;;
	test)
		ui_deps || true
		(cd "$UI_DIR" && npm run typecheck && npm test)
		;;
	stop)
		kill_one ui "$VITE_PORT"
		if [ -f "$RUNDIR/ui-backend.started" ]; then
			echo "顺带收掉 ui up 自动起的最小后端（核心 + 管控台 API）……"
			kill_one console "$CONSOLE_PORT"
			kill_one core "$CORE_PORT"
			rm -f "$RUNDIR/ui-backend.started"
		fi
		echo "前端已停。"
		;;
	logs) tail -n "${1:-50}" "$LOGDIR/ui.log" ;;
	*) die "用法：scripts/dev.sh ui up|update|check|test|stop|logs" ;;
	esac
}

# ── 整栈命令 ─────────────────────────────────────────────────────────────────

stop_stack() {
	rm -f "$RUNDIR/ui-backend.started" # 整栈的启停覆盖全部进程，ui 那份「谁起的」记录随之失效
	kill_one tunnel-upstream "$TUNNEL_UPSTREAM_PORT"
	kill_one connector
	kill_one gateway "$GATEWAY_PORT"
	kill_port "$BRIDGE_PORT"
	kill_one ui "$VITE_PORT"
	kill_one console "$CONSOLE_PORT"
	kill_one proxy "$PROXY_PORT"
	kill_one core "$CORE_PORT"
	kill_one upstream "$UPSTREAM_PORT"
	echo "已停。"
}

print_ready() {
	cat <<EOF

就绪：
  前端       ${UI_URL}/
  管控台 API ${CONSOLE_URL}/healthz
  业务入口   ${ENTRY}/          （经代理；判定流要有数据先跑 seed）
  网关       tls://${GATEWAY_ADDR}  （连接器拨入；自签证书仅本地）
EOF
	print_login
	cat <<EOF
下一步：
  scripts/dev.sh seed        造判定流量        scripts/dev.sh ui check   验证页面显示
  scripts/dev.sh update      改完 Go 代码后     scripts/dev.sh status     看状态
  scripts/dev.sh tunnel      隧道端到端         scripts/dev.sh logs core  看日志
EOF
}

cmd_up() {
	command -v lsof >/dev/null 2>&1 || die "需要 lsof（按端口判断进程是否在跑）"
	ensure_materials
	rm -f "$RUNDIR/ui-backend.started" # 整栈接管：后续由 dev.sh stop 统一收尾
	if alive core || port_listening "$CORE_PORT"; then
		echo "检测到栈已在跑，先停再起……"
		stop_stack
		sleep 1
	fi
	build_backend
	echo "== 起栈 =="
	start_upstream
	start_core
	start_proxy
	start_console
	start_gateway
	ui_deps || true
	ui_start
	print_ready
}

cmd_update() {
	need_stack
	ensure_materials
	with_connector=""
	alive connector && with_connector=1
	build_backend "$with_connector"
	echo "== 滚动重启后端（核心 → 代理 → 管控台 → 网关${with_connector:+ → 连接器}）=="
	start_core
	start_proxy
	start_console
	start_gateway
	[ -z "$with_connector" ] || start_connector
	if ui_deps; then
		ui_start
	elif ! alive ui; then
		ui_start
	fi
	echo "已更新：后端已用新代码重启；前端源码由 Vite 热更新（依赖变了已自动 npm ci）。"
	echo "验证：scripts/dev.sh status · scripts/dev.sh ui check"
}

cmd_status() {
	printf '%-16s %-7s %-7s %s\n' NAME PID PORT 状态
	for spec in upstream:$UPSTREAM_PORT core:$CORE_PORT proxy:$PROXY_PORT console:$CONSOLE_PORT \
		gateway:$GATEWAY_PORT connector:- tunnel-upstream:$TUNNEL_UPSTREAM_PORT ui:$VITE_PORT; do
		name=${spec%%:*}
		port=${spec#*:}
		pid=$(pid_of "$name")
		if alive "$name"; then
			state=运行
		elif [ "$port" != - ] && port_listening "$port"; then
			state=端口被占 # 不是本脚本拉起的（或 PID 文件丢了）：up 会按端口清掉再起
			pid=$(lsof -ti tcp:"$port" -sTCP:LISTEN 2>/dev/null | head -1)
		else
			state=停止
		fi
		printf '%-16s %-7s %-7s %s\n' "$name" "${pid:--}" "$port" "$state"
	done
	echo
	check_line() { if http_ok "$2"; then echo "  ✓ $1  $2"; else echo "  ✗ $1  $2"; fi; }
	check_line "业务入口  " "$ENTRY/"
	check_line "管控台 API" "$CONSOLE_URL/healthz"
	check_line "网关字节桥" "http://${BRIDGE_ADDR}/healthz"
	check_line "前端      " "$UI_URL/"
	echo "  临时目录：$RUNDIR"
}

cmd_logs() {
	name=${1:-core}
	[ -f "$LOGDIR/$name.log" ] || die "没有 $name 的日志（可用：core proxy console ui upstream gateway connector tunnel-upstream）"
	tail -n "${2:-50}" "$LOGDIR/$name.log"
}

cmd_seed() {
	port_listening "$PROXY_PORT" || die "栈没在跑，先 scripts/dev.sh up"
	echo "== 经业务入口造流量 =="
	# ① HeadlessChrome + /.git：0.6+0.3=0.9 ⇒ 影子改道（判定流里有「改道」可点）
	curl -s -A "Mozilla/5.0 (HeadlessChrome/120)" -o /dev/null "$ENTRY/.git/config" && echo "已发：HeadlessChrome /.git/config（高分，影子改道）"
	# ② 只带无头 UA：0.6 ⇒ 放行
	curl -s -A "Mozilla/5.0 (HeadlessChrome/120)" -o /dev/null "$ENTRY/admin/login" && echo "已发：HeadlessChrome /admin/login（低分，放行）"
	# ③ 普通浏览器：不命中规则 ⇒ 放行
	curl -s -A "Mozilla/5.0 (Macintosh) Safari/605" -o /dev/null "$ENTRY/" && echo "已发：普通浏览器 / （放行）"
	echo "事件由代理异步上报，稍等两三秒再刷新前端。"
}

json_field() { python3 -c "import json,sys;print(json.load(sys.stdin).get('$1',''))"; }

# console_login 用户 口令 cookie文件：登录；首登强制改密时换成 NEW_PASS 并记进 PASS_FILE。输出 csrf。
console_login() {
	_u=$1
	_pw=$2
	_ck=$3
	_login=$(curl -s -c "$_ck" -H 'Content-Type: application/json' \
		-d "{\"username\":\"$_u\",\"password\":\"$_pw\"}" "$CONSOLE_URL/api/v1/auth/login")
	if ! printf '%s' "$_login" | grep -q csrf_token && [ "$_pw" != "$NEW_PASS" ]; then
		# 默认口令不行时试一次本脚本会改成的口令（PASS_FILE 丢了但栈改过密的情形）。
		_pw=$NEW_PASS
		_login=$(curl -s -c "$_ck" -H 'Content-Type: application/json' \
			-d "{\"username\":\"$_u\",\"password\":\"$_pw\"}" "$CONSOLE_URL/api/v1/auth/login")
		printf '%s' "$_login" | grep -q csrf_token && printf '%s\n' "$_pw" >"$PASS_FILE"
	fi
	printf '%s' "$_login" | grep -q csrf_token || return 1
	if printf '%s' "$_login" | grep -q '"must_change":true'; then
		_csrf=$(printf '%s' "$_login" | json_field csrf_token)
		curl -s -b "$_ck" -c "$_ck" -H "X-CSRF-Token: $_csrf" -H 'Content-Type: application/json' \
			-d "{\"old_password\":\"$_pw\",\"new_password\":\"$NEW_PASS\"}" \
			"$CONSOLE_URL/api/v1/auth/password" >/dev/null
		printf '%s\n' "$NEW_PASS" >"$PASS_FILE"
		chmod 600 "$PASS_FILE"
		_login=$(curl -s -c "$_ck" -H 'Content-Type: application/json' \
			-d "{\"username\":\"$_u\",\"password\":\"$NEW_PASS\"}" "$CONSOLE_URL/api/v1/auth/login")
	fi
	printf '%s' "$_login" | json_field csrf_token
}

# ── 反向隧道端到端：签发凭证 → 起连接器 → 验证真流量走隧道 ─────────────────────
# 防假阳性设计：连接器指向**独立的第二上游**（含标记文件），直连上游（RUNDIR 目录）没有这个文件
# ⇒ 若请求被误回落直连会得到 404，只有真穿隧道才能拿到标记内容。
cmd_tunnel() {
	port_listening "$BRIDGE_PORT" || die "栈没在跑，先 scripts/dev.sh up"
	ensure_go || exit 1
	user=${SHEN_CONSOLE_USER:-admin}
	pass=${SHEN_CONSOLE_PASS:-}
	[ -n "$pass" ] || { [ -s "$PASS_FILE" ] && pass=$(cat "$PASS_FILE"); } || true
	pass=${pass:-admin}
	cookie=$(mktemp)
	trap 'rm -f "$cookie"' EXIT

	echo "== ① 登录管控台并签发凭证（${TUNNEL_HOST}）=="
	csrf=$(console_login "$user" "$pass" "$cookie") ||
		die "登录失败（口令改过的话：SHEN_CONSOLE_USER=x SHEN_CONSOLE_PASS=y 重试）"
	issued=$(curl -s -b "$cookie" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
		-d "{\"name\":\"隧道演示\",\"hosts\":[\"${TUNNEL_HOST}\"],\"owner\":\"local-stack\"}" \
		"$CONSOLE_URL/api/v1/connectors/credentials")
	key=$(printf '%s' "$issued" | json_field key)
	if [ -z "$key" ]; then
		# 幂等：同名凭证已存在（上次 tunnel 跑过）⇒ 重置它拿新 key。
		cred_id=$(curl -s -b "$cookie" "$CONSOLE_URL/api/v1/connectors" |
			python3 -c "import json,sys
d=json.load(sys.stdin)
print(next((c['id'] for c in d.get('credentials',[]) if c['name']=='隧道演示'),''))")
		if [ -n "$cred_id" ]; then
			issued=$(curl -s -b "$cookie" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
				-d '{}' "$CONSOLE_URL/api/v1/connectors/credentials/${cred_id}/reset")
			key=$(printf '%s' "$issued" | json_field key)
		fi
	fi
	[ -n "$key" ] || die "签发凭证失败：$issued"
	# 只露末 4 位（旧写法 ${key##*shc-????} 删的是前缀，实际打印出了几乎整把 key）。
	echo "凭证已就绪（明文只此一次，已注入连接器）：shc-****${key#"${key%????}"}"

	echo "== ② 编译并启动连接器（指向独立隧道上游，声明 ${TUNNEL_HOST}）=="
	mkdir -p "$RUNDIR/tunnel-business"
	printf 'via-tunnel\n' >"$RUNDIR/tunnel-business/tunnel-ok.txt"
	kill_one tunnel-upstream "$TUNNEL_UPSTREAM_PORT"
	start_bg tunnel-upstream python3 -m http.server "$TUNNEL_UPSTREAM_PORT" --bind 127.0.0.1 \
		--directory "$RUNDIR/tunnel-business"
	go build -o "$RUNDIR/connector" ./modules/connector/cmd/shen-connector || die "连接器编译失败"
	(umask 077 && printf 'CONNECTOR_KEY=%s\n' "$key" >"$RUNDIR/connector.env")
	start_connector

	echo "== ③ 验证：请求经代理 → 隧道 → 连接器 → 隧道专用上游 =="
	# 网关 key 表 5s 一拉 + 连接器自动重试：等握手真正完成（最长 20s）。
	ok=""
	i=0
	while [ $i -lt 20 ]; do
		body=$(curl -s -H "Host: ${TUNNEL_HOST}" "$ENTRY/tunnel-ok.txt" || true)
		if [ "$body" = "via-tunnel" ]; then
			ok=1
			break
		fi
		i=$((i + 1))
		sleep 1
	done
	[ -n "$ok" ] || die "隧道请求未到达隧道专用上游；看日志：logs gateway / logs connector / logs proxy"
	echo "✓ 隧道路径：Host=${TUNNEL_HOST} 拿到了隧道专用上游的标记内容（直连路径必然 404）。"
	if curl -sf -o /dev/null "$ENTRY/"; then
		echo "✓ 直连路径：默认 Host 的请求仍直连上游（存量行为不变）。"
	fi
	echo "控制台「接入管理」页应显示：网关已配置、隧道演示在线（RTT 心跳）。"
}

usage() { sed -n '2,41p' "$ROOT/scripts/dev.sh" | sed 's/^# \{0,1\}//'; }

cmd=${1:-help}
[ $# -gt 0 ] && shift
case "$cmd" in
up | restart) cmd_up ;;
update) cmd_update ;;
status) cmd_status ;;
stop | down) stop_stack ;;
logs) cmd_logs "$@" ;;
seed) cmd_seed ;;
tunnel) cmd_tunnel ;;
ui) cmd_ui "$@" ;;
help | -h | --help) usage ;;
*) die "未知子命令：$cmd（scripts/dev.sh help 查看用法）" ;;
esac
