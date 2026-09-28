#!/bin/sh
# 本地验证栈（不需要 Docker）：核心 + 影子代理 + 假上游 + 管控台 API + Vite 前端。
#
# 与 scripts/shen.sh 的分工：那边是**容器栈**（Docker），这边是**本机直跑**——
# 改了 Go / 前端代码后想立刻在浏览器里验证（主题、判定流点击链路等）用它。
#
# 形态与 scripts/dev/smoke.sh 一致：核心影子模式（判定照算、事件照报，但永不改道），
# 代理指到一个本地假上游（python http.server），所以整栈零外部依赖。
#
# 用法（仓库根执行）：
#   scripts/dev/local-stack.sh up        起栈并等就绪（已在跑 ⇒ 先停再起，等于 restart）
#   scripts/dev/local-stack.sh restart   同上（显式写法）
#   scripts/dev/local-stack.sh stop      停栈
#   scripts/dev/local-stack.sh status    看进程 / 端口 / 健康检查
#   scripts/dev/local-stack.sh seed      经代理造几条判定流量（让判定流 / 链路有数据可点）
#   scripts/dev/local-stack.sh logs [core|proxy|console|ui|upstream]
#
# 端口（可用环境变量覆盖）：
#   上游 9000 · 核心 9443 · 业务入口 18080 · 管控台 API 9445 · 前端 5173
#   例：SHEN_LOCAL_VITE_PORT=5174 scripts/dev/local-stack.sh up
#
# 管控台账号：首次 admin/admin（首登强制改密；数据目录在临时目录里，改过一次后重启不用再改）。
# 临时产物（二进制 / 配置 / 日志 / 控制台数据）全部落在 ${TMPDIR:-/tmp}/shen-<uid>/local-stack/，
# 仓库里不留任何文件。
set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
RUNDIR=${TMPDIR:-/tmp}/shen-$(id -u)/local-stack
LOGDIR="$RUNDIR/logs"
DATA="$RUNDIR/console-data"
PIDDIR="$RUNDIR/pids"

UPSTREAM_PORT=${SHEN_LOCAL_UPSTREAM_PORT:-9000}
CORE_ADDR=${SHEN_LOCAL_CORE_ADDR:-127.0.0.1:9443}
PROXY_ADDR=${SHEN_LOCAL_PROXY_ADDR:-127.0.0.1:18080}
CONSOLE_ADDR=${SHEN_LOCAL_CONSOLE_ADDR:-127.0.0.1:9445}
VITE_PORT=${SHEN_LOCAL_VITE_PORT:-5173}

CORE_PORT=${CORE_ADDR#*:}
PROXY_PORT=${PROXY_ADDR#*:}
CONSOLE_PORT=${CONSOLE_ADDR#*:}
ENTRY="http://127.0.0.1:${PROXY_PORT}"
UI_URL="http://127.0.0.1:${VITE_PORT}"

die() { echo "local-stack: $*" >&2; exit 1; }

# 后台进程一律**先编译再直跑**，不用 `go run`：它的 PID 是包装器的，kill 会留孤儿
# （smoke.sh 2026-09-22 实测泄漏过 51 个核心进程，同一个坑不踩第二次）。
start_bg() { # name pidfile logfile cmd...
	name=$1 pidfile=$2 logfile=$3; shift 3
	: >"$logfile"
	"$@" >"$logfile" 2>&1 &
	echo $! >"$pidfile"
}

pid_of() { [ -f "$1" ] && cat "$1" || true; }

kill_one() { # name pidfile port —— 先按 PID 杀，再按端口兜底（npm/vite 会派生子进程）
	pid=$(pid_of "$2")
	[ -n "${pid:-}" ] && kill "$pid" 2>/dev/null || true
	# 端口兜底：PID 文件可能已删 / 子进程不在进程组里。只杀监听该端口的进程。
	if command -v lsof >/dev/null 2>&1; then
		orphans=$(lsof -ti tcp:"$3" -sTCP:LISTEN 2>/dev/null || true)
		[ -n "${orphans:-}" ] && kill $orphans 2>/dev/null || true
	fi
	rm -f "$2"
}

stop_stack() {
	kill_one ui "$PIDDIR/ui.pid" "$VITE_PORT"
	kill_one console "$PIDDIR/console.pid" "$CONSOLE_PORT"
	kill_one proxy "$PIDDIR/proxy.pid" "$PROXY_PORT"
	kill_one core "$PIDDIR/core.pid" "$CORE_PORT"
	kill_one upstream "$PIDDIR/upstream.pid" "$UPSTREAM_PORT"
	echo "已停。"
}

wait_for() { # desc cmd...
	desc=$1; shift
	i=0
	while [ $i -lt 60 ]; do
		if "$@" >/dev/null 2>&1; then return 0; fi
		i=$((i + 1))
		sleep 1
	done
	die "等待 ${desc} 超时；看日志：scripts/dev/local-stack.sh logs"
}

http_ok() { curl -sf -o /dev/null "$1"; }

up_stack() {
	mkdir -p "$RUNDIR" "$LOGDIR" "$PIDDIR" "$DATA"
	# 已在跑 ⇒ 等于 restart（用户习惯「重启」语义，不报错）
	if [ -f "$PIDDIR/core.pid" ] || lsof -iTCP:"$CORE_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
		echo "检测到栈已在跑，先停再起……"
		stop_stack
		sleep 1
	fi

	echo "== 编译（核心 / 代理 / 管控台）=="
	go build -o "$RUNDIR/core" ./common/core/cmd/core || die "核心编译失败"
	go build -o "$RUNDIR/proxy" ./modules/deception/proxy/cmd/proxy || die "代理编译失败"
	go build -o "$RUNDIR/console" ./modules/console/cmd/console || die "管控台编译失败"

	# 核心配置：与 smoke.sh 同一套开发口径（影子模式、内存存储、两条规则）。
	CFG="$RUNDIR/core.yaml"
	cat >"$CFG" <<YAML
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

	echo "== 起栈 =="
	start_bg upstream "$PIDDIR/upstream.pid" "$LOGDIR/upstream.log" \
		python3 -m http.server "$UPSTREAM_PORT" --bind 127.0.0.1 --directory "$RUNDIR"
	start_bg core "$PIDDIR/core.pid" "$LOGDIR/core.log" \
		env SHEN_CONFIG="$CFG" "$RUNDIR/core"
	start_bg proxy "$PIDDIR/proxy.pid" "$LOGDIR/proxy.log" \
		env SHEN_PROXY_UPSTREAM="http://127.0.0.1:${UPSTREAM_PORT}" \
		SHEN_PROXY_LISTEN="$PROXY_ADDR" SHEN_CORE_ADDR="$CORE_ADDR" \
		SHEN_PROXY_SHADOW=true "$RUNDIR/proxy"
	# 管控台：数据目录放临时区（账号 / 偏好在重启间保留）；不设 bootstrap ⇒ 默认 admin/admin。
	start_bg console "$PIDDIR/console.pid" "$LOGDIR/console.log" \
		env SHEN_CORE_ADDR="$CORE_ADDR" SHEN_CONSOLE_LISTEN="$CONSOLE_ADDR" \
		SHEN_CONSOLE_DATA_DIR="$DATA" "$RUNDIR/console"

	echo "== 等就绪（最长 60s/项）=="
	wait_for "上游" http_ok "http://127.0.0.1:${UPSTREAM_PORT}/"
	wait_for "核心端口" lsof -iTCP:"$CORE_PORT" -sTCP:LISTEN
	wait_for "业务入口" http_ok "$ENTRY/"
	wait_for "管控台 API" http_ok "http://127.0.0.1:${CONSOLE_PORT}/healthz"

	# 前端最后起（依赖 API 代理目标已在跑）。npm run dev 会派生子进程，停止靠端口兜底。
	if [ ! -d "$ROOT/modules/console/ui/node_modules" ]; then
		echo "前端依赖未装，先 npm ci（只此一次）……"
		(cd "$ROOT/modules/console/ui" && npm ci --silent)
	fi
	: >"$LOGDIR/ui.log"
	(cd "$ROOT/modules/console/ui" && npm run dev -- --port "$VITE_PORT" --host 127.0.0.1 --strictPort) \
		>"$LOGDIR/ui.log" 2>&1 &
	echo $! >"$PIDDIR/ui.pid"
	wait_for "前端" http_ok "$UI_URL/"

	echo
	echo "就绪："
	echo "  前端     ${UI_URL}/      （账号 admin/admin，首登强制改密）"
	echo "  管控台API http://${CONSOLE_ADDR}/healthz"
	echo "  业务入口 ${ENTRY}/      （经代理；判定流要有数据可先跑 seed）"
	echo "  日志     scripts/dev/local-stack.sh logs <core|proxy|console|ui|upstream>"
}

seed() {
	lsof -iTCP:"$PROXY_PORT" -sTCP:LISTEN >/dev/null 2>&1 || die "栈没在跑，先 scripts/dev/local-stack.sh up"
	echo "== 经业务入口造流量 =="
	# ① HeadlessChrome + /.git：0.6+0.3=0.9 ⇒ 影子改道（判定流里有「改道」可点）
	curl -s -A "Mozilla/5.0 (HeadlessChrome/120)" -o /dev/null "$ENTRY/.git/config" && echo "已发：HeadlessChrome /.git/config（高分，影子改道）"
	# ② 只带无头 UA：0.6 ⇒ 放行
	curl -s -A "Mozilla/5.0 (HeadlessChrome/120)" -o /dev/null "$ENTRY/admin/login" && echo "已发：HeadlessChrome /admin/login（低分，放行）"
	# ③ 普通浏览器：不命中规则 ⇒ 放行
	curl -s -A "Mozilla/5.0 (Macintosh) Safari/605" -o /dev/null "$ENTRY/" && echo "已发：普通浏览器 / （放行）"
	echo "事件由代理异步上报，稍等两三秒再刷新前端。"
}

status_stack() {
	for name in upstream core proxy console ui; do
		pid=$(pid_of "$PIDDIR/$name.pid")
		alive=dead
		[ -n "${pid:-}" ] && kill -0 "$pid" 2>/dev/null && alive=running
		printf '%-10s %-8s pid=%s\n' "$name" "$alive" "${pid:--}"
	done
	http_ok "http://127.0.0.1:${CONSOLE_PORT}/healthz" && echo "管控台 API healthz: ok" || echo "管控台 API healthz: 不可达"
	http_ok "$UI_URL/" && echo "前端: ok" || echo "前端: 不可达"
}

case ${1:-} in
up | restart) up_stack ;;
stop) stop_stack ;;
status) status_stack ;;
seed) seed ;;
logs)
	name=${2:-core}
	[ -f "$LOGDIR/$name.log" ] || die "没有 $name 的日志（可用：core proxy console ui upstream）"
	tail -n 50 "$LOGDIR/$name.log"
	;;
*) die "用法：$0 up|restart|stop|status|seed|logs [name]" ;;
esac
