#!/bin/sh
# 本地验证栈（不需要 Docker）：核心 + 影子代理 + 假上游 + 管控台 API + Vite 前端
# + 连接器网关（反向隧道；tunnel 子命令再拉起 shen-connector 全链路实测）。
#
# 与 scripts/shen.sh 的分工：那边是**容器栈**（Docker），这边是**本机直跑**——
# 改了 Go / 前端代码后想立刻在浏览器里验证（主题、判定流点击链路、接入管理等）用它。
#
# 形态与 scripts/dev/smoke.sh 一致：核心影子模式（判定照算、事件照报，但永不改道），
# 代理指到一个本地假上游（python http.server），所以整栈零外部依赖。
#
# 用法（仓库根执行）：
#   scripts/dev/local-stack.sh up        起栈并等就绪（已在跑 ⇒ 先停再起，等于 restart）
#   scripts/dev/local-stack.sh restart   同上（显式写法）
#   scripts/dev/local-stack.sh stop      停栈（含 gateway / connector）
#   scripts/dev/local-stack.sh status    看进程 / 端口 / 健康检查
#   scripts/dev/local-stack.sh seed      经代理造几条判定流量（让判定流 / 链路有数据可点）
#   scripts/dev/local-stack.sh tunnel    反向隧道端到端：签发凭证 → 起连接器 → 真流量过隧道验证
#   scripts/dev/local-stack.sh logs [core|proxy|console|ui|upstream|gateway|connector]
#
# 端口（可用环境变量覆盖）：
#   上游 9000 · 核心 9443 · 业务入口 18080 · 管控台 API 9445 · 前端 5173
#   网关 TLS 9446 · 网关字节桥 9447
#   例：SHEN_LOCAL_VITE_PORT=5174 scripts/dev/local-stack.sh up
#
# 管控台账号：首次 admin/admin（首登强制改密；数据目录在临时目录里，改过一次后重启不用再改）。
# tunnel 子命令自动签发凭证需要能登录管控台：默认 admin/admin，改过口令后用
#   SHEN_CONSOLE_USER=x SHEN_CONSOLE_PASS=y scripts/dev/local-stack.sh tunnel
# 临时产物（二进制 / 配置 / 证书 / 日志 / 控制台数据）全部落在 ${TMPDIR:-/tmp}/shen-<uid>/local-stack/，
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
GATEWAY_ADDR=${SHEN_LOCAL_GATEWAY_ADDR:-127.0.0.1:9446}
BRIDGE_ADDR=${SHEN_LOCAL_BRIDGE_ADDR:-127.0.0.1:9447}
VITE_PORT=${SHEN_LOCAL_VITE_PORT:-5173}
# 隧道演示用的域名（tunnel 子命令签凭证 + 连接器声明 + 验证请求三处一致）。
TUNNEL_HOST=${SHEN_LOCAL_TUNNEL_HOST:-tunnel.local.test}

CORE_PORT=${CORE_ADDR#*:}
PROXY_PORT=${PROXY_ADDR#*:}
CONSOLE_PORT=${CONSOLE_ADDR#*:}
GATEWAY_PORT=${GATEWAY_ADDR#*:}
BRIDGE_PORT=${BRIDGE_ADDR#*:}
ENTRY="http://127.0.0.1:${PROXY_PORT}"
UI_URL="http://127.0.0.1:${VITE_PORT}"

die() { echo "local-stack: $*" >&2; exit 1; }

# 后台进程一律**先编译再直跑**，不用 `go run`：它的 PID 是包装器的，kill 会留孤儿
# （smoke.sh 2022-09-22 实测泄漏过 51 个核心进程，同一个坑不踩第二次）。
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
	kill_one tunnel-upstream "$PIDDIR/tunnel-upstream.pid" "$TUNNEL_UPSTREAM_PORT"
	kill_one connector "$PIDDIR/connector.pid" 0
	kill_one gateway "$PIDDIR/gateway.pid" "$GATEWAY_PORT"
	# 桥与网关同进程：网关杀掉后按桥端口再兜底一次（防只占桥不占 TLS 口的异常残留）。
	if command -v lsof >/dev/null 2>&1; then
		orphans=$(lsof -ti tcp:"$BRIDGE_PORT" -sTCP:LISTEN 2>/dev/null || true)
		[ -n "${orphans:-}" ] && kill $orphans 2>/dev/null || true
	fi
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

random_token() {
	if command -v openssl >/dev/null 2>&1; then
		openssl rand -hex 24
	else
		python3 -c 'import secrets;print(secrets.token_hex(24))'
	fi
}

# 集成令牌与自签证书：一次生成、跨重启复用（换了令牌控制台数据里的会话态就乱了）。
ensure_tunnel_materials() {
	if [ ! -s "$RUNDIR/integration-token" ]; then
		random_token >"$RUNDIR/integration-token"
	fi
	if [ ! -s "$RUNDIR/gw-cert.pem" ] || [ ! -s "$RUNDIR/gw-key.pem" ]; then
		openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
			-keyout "$RUNDIR/gw-key.pem" -out "$RUNDIR/gw-cert.pem" \
			-subj "/CN=localhost" >/dev/null 2>&1 || die "生成网关自签证书失败（需要 openssl）"
	fi
}

up_stack() {
	mkdir -p "$RUNDIR" "$LOGDIR" "$PIDDIR" "$DATA"
	ensure_tunnel_materials
	INTEGRATION_TOKEN=$(cat "$RUNDIR/integration-token")
	# 已在跑 ⇒ 等于 restart（用户习惯「重启」语义，不报错）
	if [ -f "$PIDDIR/core.pid" ] || lsof -iTCP:"$CORE_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
		echo "检测到栈已在跑，先停再起……"
		stop_stack
		sleep 1
	fi

	echo "== 编译（核心 / 代理 / 管控台 / 网关）=="
	go build -o "$RUNDIR/core" ./common/core/cmd/core || die "核心编译失败"
	go build -o "$RUNDIR/proxy" ./modules/deception/proxy/cmd/proxy || die "代理编译失败"
	go build -o "$RUNDIR/console" ./modules/console/cmd/console || die "管控台编译失败"
	go build -o "$RUNDIR/gateway" ./modules/connector/gateway/cmd/gateway || die "网关编译失败"

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
		SHEN_PROXY_SHADOW=true SHEN_PROXY_TUNNEL_GATEWAY="$BRIDGE_ADDR" "$RUNDIR/proxy"
	# 管控台：数据目录放临时区（账号 / 偏好在重启间保留）；不设 bootstrap ⇒ 默认 admin/admin。
	# 集成令牌与网关一致（key 拉取 / 自动登记 / 会话上报走它）。
	start_bg console "$PIDDIR/console.pid" "$LOGDIR/console.log" \
		env SHEN_CORE_ADDR="$CORE_ADDR" SHEN_CONSOLE_LISTEN="$CONSOLE_ADDR" \
		SHEN_CONSOLE_DATA_DIR="$DATA" SHEN_CONSOLE_INTEGRATION_TOKEN="$INTEGRATION_TOKEN" "$RUNDIR/console"
	# 连接器网关：TLS 自签（连接器侧 TLS_INSECURE）、key 表 5s 快拉（本地验证要即时生效）。
	start_bg gateway "$PIDDIR/gateway.pid" "$LOGDIR/gateway.log" \
		env SHEN_GATEWAY_LISTEN="$GATEWAY_ADDR" \
		SHEN_GATEWAY_TLS_CERT="$RUNDIR/gw-cert.pem" SHEN_GATEWAY_TLS_KEY="$RUNDIR/gw-key.pem" \
		SHEN_GATEWAY_BRIDGE="$BRIDGE_ADDR" SHEN_GATEWAY_CONSOLE="http://${CONSOLE_ADDR}" \
		SHEN_GATEWAY_INTEGRATION_TOKEN="$INTEGRATION_TOKEN" \
		SHEN_GATEWAY_KEY_REFRESH=5s SHEN_GATEWAY_NODE=local-stack "$RUNDIR/gateway"

	echo "== 等就绪（最长 60s/项）=="
	wait_for "上游" http_ok "http://127.0.0.1:${UPSTREAM_PORT}/"
	wait_for "核心端口" lsof -iTCP:"$CORE_PORT" -sTCP:LISTEN
	wait_for "业务入口" http_ok "$ENTRY/"
	wait_for "管控台 API" http_ok "http://127.0.0.1:${CONSOLE_PORT}/healthz"
	wait_for "网关字节桥" http_ok "http://${BRIDGE_ADDR}/healthz"

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
	echo "  网关     tls://${GATEWAY_ADDR}（连接器拨入；自签证书仅本地）"
	echo "  隧道实测 scripts/dev/local-stack.sh tunnel   （签发凭证 → 起连接器 → 真流量过隧道）"
	echo "  日志     scripts/dev/local-stack.sh logs <core|proxy|console|ui|upstream|gateway|connector>"
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

# ── 反向隧道端到端：签发凭证 → 起连接器 → 验证真流量走隧道 ───────────────────
# 验证的防假阳性设计：连接器指向**独立的第二上游**（含标记文件），
# 直连上游（RUNDIR 目录）没有这个文件 ⇒ 若请求被误回落直连会得到 404，
# 只有真穿隧道才能拿到标记内容。
TUNNEL_UPSTREAM_PORT=${SHEN_LOCAL_TUNNEL_UPSTREAM_PORT:-9100}
tunnel() {
	lsof -iTCP:"$BRIDGE_PORT" -sTCP:LISTEN >/dev/null 2>&1 || die "栈没在跑，先 scripts/dev/local-stack.sh up"
	user=${SHEN_CONSOLE_USER:-admin}
	pass=${SHEN_CONSOLE_PASS:-admin}
	cookie=$(mktemp)

	echo "== ① 登录管控台并签发凭证（${TUNNEL_HOST}）=="
	login=$(curl -s -c "$cookie" -H 'Content-Type: application/json' \
		-d "{\"username\":\"$user\",\"password\":\"$pass\"}" \
		"http://127.0.0.1:${CONSOLE_PORT}/api/v1/auth/login")
	if ! printf '%s' "$login" | grep -q csrf_token; then
		rm -f "$cookie"
		die "登录失败（口令改过的话：SHEN_CONSOLE_USER=x SHEN_CONSOLE_PASS=y 重试）"
	fi
	# 首登强制改密：把默认口令换成演示口令后再继续。
	if printf '%s' "$login" | grep -q '"must_change":true'; then
		csrf=$(printf '%s' "$login" | python3 -c 'import json,sys;print(json.load(sys.stdin)["csrf_token"])')
		curl -s -b "$cookie" -c "$cookie" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
			-d "{\"old_password\":\"$pass\",\"new_password\":\"Local-Stack-2026!\"}" \
			"http://127.0.0.1:${CONSOLE_PORT}/api/v1/auth/password" >/dev/null
		pass="Local-Stack-2026!"
		login=$(curl -s -c "$cookie" -H 'Content-Type: application/json' \
			-d "{\"username\":\"$user\",\"password\":\"$pass\"}" \
			"http://127.0.0.1:${CONSOLE_PORT}/api/v1/auth/login")
	fi
	csrf=$(printf '%s' "$login" | python3 -c 'import json,sys;print(json.load(sys.stdin)["csrf_token"])')
	issued=$(curl -s -b "$cookie" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
		-d "{\"name\":\"隧道演示\",\"hosts\":[\"${TUNNEL_HOST}\"],\"owner\":\"local-stack\"}" \
		"http://127.0.0.1:${CONSOLE_PORT}/api/v1/connectors/credentials")
	key=$(printf '%s' "$issued" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("key",""))')
	if [ -z "$key" ]; then
		# 幂等：同名凭证已存在（上次 tunnel 跑过）⇒ 重置它拿新 key。
		cred_id=$(curl -s -b "$cookie" "http://127.0.0.1:${CONSOLE_PORT}/api/v1/connectors" |
			python3 -c "import json,sys
d=json.load(sys.stdin)
print(next((c['id'] for c in d.get('credentials',[]) if c['name']=='隧道演示'),''))")
		if [ -n "$cred_id" ]; then
			issued=$(curl -s -b "$cookie" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
				-d '{}' "http://127.0.0.1:${CONSOLE_PORT}/api/v1/connectors/credentials/${cred_id}/reset")
			key=$(printf '%s' "$issued" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("key",""))')
		fi
	fi
	if [ -z "$key" ]; then
		rm -f "$cookie"
		die "签发凭证失败：$issued"
	fi
	echo "凭证已就绪（明文只此一次，已注入连接器）：shc-****${key##*shc-????}"

	echo "== ② 编译并启动连接器（指向独立隧道上游，声明 ${TUNNEL_HOST}）=="
	# 隧道专用上游：目录里只有标记文件；直连上游没有它（防「误回落直连」的假阳性）。
	mkdir -p "$RUNDIR/tunnel-business"
	printf 'via-tunnel\n' >"$RUNDIR/tunnel-business/tunnel-ok.txt"
	kill_one tunnel-upstream "$PIDDIR/tunnel-upstream.pid" "$TUNNEL_UPSTREAM_PORT"
	start_bg tunnel-upstream "$PIDDIR/tunnel-upstream.pid" "$LOGDIR/tunnel-upstream.log" \
		python3 -m http.server "$TUNNEL_UPSTREAM_PORT" --bind 127.0.0.1 --directory "$RUNDIR/tunnel-business"
	go build -o "$RUNDIR/connector" ./modules/connector/cmd/shen-connector || die "连接器编译失败"
	start_bg connector "$PIDDIR/connector.pid" "$LOGDIR/connector.log" \
		env SHEN_CONNECTOR_GATEWAY="$GATEWAY_ADDR" SHEN_CONNECTOR_KEY="$key" \
		SHEN_CONNECTOR_UPSTREAM="http://127.0.0.1:${TUNNEL_UPSTREAM_PORT}" \
		SHEN_CONNECTOR_NAME="隧道演示" SHEN_CONNECTOR_HOSTS="$TUNNEL_HOST" \
		SHEN_CONNECTOR_TLS_INSECURE=true "$RUNDIR/connector"

	echo "== ③ 验证：请求经代理 → 隧道 → 连接器 → 隧道专用上游 =="
	# 网关 key 表 5s 一拉 + 连接器自动重试：等握手真正完成（最长 20s），
	# 验证标记文件内容 —— 直连路径必然 404，拿到内容即证明真穿了隧道。
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
	rm -f "$cookie"
	if [ -z "$ok" ]; then
		die "隧道请求未到达隧道专用上游；看日志：logs gateway / logs connector / logs proxy"
	fi
	echo "✓ 隧道路径：Host=${TUNNEL_HOST} 拿到了隧道专用上游的标记内容（直连路径必然 404）。"
	# 回落语义：非隧道域名仍走直连（存量行为不受影响）—— 直连上游的目录列表。
	if curl -sf -o /dev/null "$ENTRY/"; then
		echo "✓ 直连路径：默认 Host 的请求仍直连上游（存量行为不变）。"
	fi
	# 回落语义的反面：隧道域名若误回落直连，应拿不到标记（404）——已在上面隐式验证。
	echo "控制台「接入管理」页应显示：网关已配置、隧道演示在线（RTT 心跳）。"
}

status_stack() {
	for name in upstream core proxy console gateway connector ui; do
		pid=$(pid_of "$PIDDIR/$name.pid")
		alive=dead
		[ -n "${pid:-}" ] && kill -0 "$pid" 2>/dev/null && alive=running
		printf '%-10s %-8s pid=%s\n' "$name" "$alive" "${pid:--}"
	done
	http_ok "http://127.0.0.1:${CONSOLE_PORT}/healthz" && echo "管控台 API healthz: ok" || echo "管控台 API healthz: 不可达"
	http_ok "http://${BRIDGE_ADDR}/healthz" && echo "网关字节桥 healthz: ok" || echo "网关字节桥 healthz: 不可达"
	http_ok "$UI_URL/" && echo "前端: ok" || echo "前端: 不可达"
}

case ${1:-} in
up | restart) up_stack ;;
stop) stop_stack ;;
status) status_stack ;;
seed) seed ;;
tunnel) tunnel ;;
logs)
	name=${2:-core}
	[ -f "$LOGDIR/$name.log" ] || die "没有 $name 的日志（可用：core proxy console ui upstream gateway connector）"
	tail -n 50 "$LOGDIR/$name.log"
	;;
*) die "用法：$0 up|restart|stop|status|seed|tunnel|logs [name]" ;;
esac
