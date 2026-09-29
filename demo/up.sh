#!/bin/sh
# 蜃景商城 · 反向隧道接入演示：一键跑通「真实业务 → 连接器 → 蜃楼」的完整接入。
#
# 前置：蜃楼栈已在跑（网关 + 管控台）。
#   本地验证栈：scripts/dev/local-stack.sh up
#   容器栈：docker compose up -d（含 connector profile；gateway 端口按 .env 发布）
#
# 用法（仓库根执行）：
#   demo/up.sh            # 起业务 + 连接器 + 验证接入记录与管理能力
#   demo/up.sh stop       # 停演示进程（业务 / 连接器；不动蜃楼栈）
#   demo/up.sh verify     # 只做验证（不重复起进程）
#
# 环境变量（对照 docker compose / 远程栈时按需覆盖）：
#   SHEN_CONSOLE=http://127.0.0.1:9445     管控台 API
#   SHEN_GATEWAY=127.0.0.1:9446            连接器网关（TLS）
#   SHEN_ENTRY=http://127.0.0.1:18080      业务入口（proxy）
#   SHEN_UI=http://127.0.0.1:5173          管控台前端（本地栈；容器栈为 :19444）
#   SHEN_CONSOLE_USER / SHEN_CONSOLE_PASS  自动签发凭证用的管理员账号
#   SHEN_DEMO_HOST=demo.shop.local         演示域名
#   SHEN_DEMO_PORT=9001                    业务本机端口
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
CONSOLE=${SHEN_CONSOLE:-http://127.0.0.1:9445}
GATEWAY=${SHEN_GATEWAY:-127.0.0.1:9446}
ENTRY=${SHEN_ENTRY:-http://127.0.0.1:18080}
UI=${SHEN_UI:-http://127.0.0.1:5173}
DEMO_HOST=${SHEN_DEMO_HOST:-demo.shop.local}
DEMO_PORT=${SHEN_DEMO_PORT:-9001}
DEMO_NAME=${SHEN_DEMO_NAME:-蜃景商城}
USER=${SHEN_CONSOLE_USER:-admin}
PASS=${SHEN_CONSOLE_PASS:-admin}
RUNDIR=${TMPDIR:-/tmp}/shen-$(id -u)/demo
PIDDIR="$RUNDIR/pids"
LOGDIR="$RUNDIR/logs"

die() { echo "demo: $*" >&2; exit 1; }
mkdir -p "$PIDDIR" "$LOGDIR"

# 后台进程先编译再直跑（不用 go run：它的 PID 是包装器的，kill 留孤儿——仓库实测教训）。
start_bg() { # name pidfile logfile cmd...
	name=$1 pidfile=$2 logfile=$3; shift 3
	: >"$logfile"
	"$@" >"$logfile" 2>&1 &
	echo $! >"$pidfile"
}

# login 直接设置全局 COOKIE / CSRF（不要放进 $() ：命令替换是子 shell，状态会丢）。
login() {
	COOKIE=$(mktemp "${TMPDIR:-/tmp}/shen-demo-cookie.XXXXXX")
	login=$(curl -s -c "$COOKIE" -H 'Content-Type: application/json' \
		-d "{\"username\":\"$USER\",\"password\":\"$PASS\"}" "$CONSOLE/api/v1/auth/login")
	if ! printf '%s' "$login" | grep -q csrf_token; then
		die "管控台登录失败（${CONSOLE}，账号 ${USER}）：$login"
	fi
	if printf '%s' "$login" | grep -q '"must_change":true'; then
		csrf=$(printf '%s' "$login" | python3 -c 'import json,sys;print(json.load(sys.stdin)["csrf_token"])')
		curl -s -b "$COOKIE" -c "$COOKIE" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
			-d "{\"old_password\":\"$PASS\",\"new_password\":\"Demo-Shop-2026!\"}" \
			"$CONSOLE/api/v1/auth/password" >/dev/null
		PASS="Demo-Shop-2026!"
		login=$(curl -s -c "$COOKIE" -H 'Content-Type: application/json' \
			-d "{\"username\":\"$USER\",\"password\":\"$PASS\"}" "$CONSOLE/api/v1/auth/login")
	fi
	CSRF=$(printf '%s' "$login" | python3 -c 'import json,sys;print(json.load(sys.stdin)["csrf_token"])')
}

api() { # method path [body] —— 带 cookie + CSRF 的已登录请求（URL 必须紧跟 method，body 最后）
	method=$1 path=$2 body=${3:-}
	if [ -n "$body" ]; then
		curl -s -b "$COOKIE" -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' \
			-X "$method" -d "$body" "$CONSOLE/api/v1$path"
	else
		curl -s -b "$COOKIE" -H "X-CSRF-Token: $CSRF" -X "$method" "$CONSOLE/api/v1$path"
	fi
}

stop_demo() {
	for name in connector business; do
		pid=$(cat "$PIDDIR/$name.pid" 2>/dev/null || true)
		[ -n "${pid:-}" ] && kill "$pid" 2>/dev/null || true
		rm -f "$PIDDIR/$name.pid"
	done
	echo "演示进程已停（蜃楼栈不受影响）。"
}

# ── ① 编译：业务 + 连接器 ────────────────────────────────────────────────────
build() {
	echo "== ① 编译业务与连接器 =="
	go build -o "$RUNDIR/business" ./demo/business || die "业务编译失败"
	go build -o "$RUNDIR/connector" ./modules/connector/cmd/shen-connector || die "连接器编译失败"
}

# ── ② 起业务（仅本机回环）───────────────────────────────────────────────────
start_business() {
	echo "== ② 起真实业务（蜃景商城，仅 127.0.0.1:${DEMO_PORT}）=="
	start_bg business "$PIDDIR/business.pid" "$LOGDIR/business.log" \
		env SHEN_DEMO_PORT="$DEMO_PORT" "$RUNDIR/business"
	sleep 1
	curl -sf "http://127.0.0.1:$DEMO_PORT/" >/dev/null || die "业务没起来：logs $LOGDIR/business.log"
	echo "业务就绪：http://127.0.0.1:${DEMO_PORT}（零入站暴露：防火墙可拒绝一切入站）"
}

# ── ③ 签发/复用接入凭证（幂等：已有同名凭证则重置换新 key）────────────────────
issue_credential() {
	echo "== ③ 登录管控台，签发接入凭证 =="
	login
	issued=$(api POST /connectors/credentials "{\"name\":\"$DEMO_NAME\",\"hosts\":[\"$DEMO_HOST\"],\"owner\":\"演示\"}")
	KEY=$(printf '%s' "$issued" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("key",""))')
	if [ -z "$KEY" ]; then
		# 幂等：同名凭证已存在 ⇒ 重置换新 key（旧 key 立即作废，这就是吊销语义）。
		cred_id=$(api GET /connectors | python3 -c "import json,sys
d=json.load(sys.stdin)
print(next((c['id'] for c in d.get('credentials',[]) if c['name']=='$DEMO_NAME'),''))")
		[ -n "$cred_id" ] || die "签发凭证失败：$issued"
		issued=$(api POST "/connectors/credentials/$cred_id/reset" '{}')
		KEY=$(printf '%s' "$issued" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("key",""))')
		[ -n "$KEY" ] || die "重置凭证失败：$issued"
		echo "复用已有凭证并重置换新：shc-****${KEY##*shc-????}"
	else
		echo "凭证已签发（明文只此一次）：shc-****${KEY##*shc-????}"
	fi
}

# ── ④ 起连接器（唯一需要蜃楼信息的接入步骤）─────────────────────────────────
start_connector() {
	echo "== ④ 起连接器（拨号网关 ${GATEWAY}，声明域名 ${DEMO_HOST}）=="
	start_bg connector "$PIDDIR/connector.pid" "$LOGDIR/connector.log" \
		env SHEN_CONNECTOR_GATEWAY="$GATEWAY" SHEN_CONNECTOR_KEY="$KEY" \
		SHEN_CONNECTOR_UPSTREAM="http://127.0.0.1:$DEMO_PORT" \
		SHEN_CONNECTOR_NAME="$DEMO_NAME" SHEN_CONNECTOR_HOSTS="$DEMO_HOST" \
		SHEN_CONNECTOR_TLS_INSECURE=true "$RUNDIR/connector"
	# 等握手：网关 key 表 5s 一拉 + 连接器自动重试（最长 15s）。
	i=0
	while [ $i -lt 15 ]; do
		grep -q '已接入' "$LOGDIR/connector.log" 2>/dev/null && break
		i=$((i + 1))
		sleep 1
	done
	grep -q '已接入' "$LOGDIR/connector.log" 2>/dev/null || die "连接器未接入：tail $LOGDIR/connector.log"
	grep '已接入' "$LOGDIR/connector.log" | tail -1
}

# ── ⑤ 验证：Shen 里的连接记录、自动登记、真实流量、管理能力 ──────────────────
verify() {
	echo "== ⑤ 验证接入：Shen 里能看到 Demo、能管理 =="
	overview=$(api GET /connectors)
	printf '%s' "$overview" | python3 -c "
import json,sys
d=json.load(sys.stdin)
print('  接入记录（凭证）：', [(c['name'], c['key_hint'], c['online']) for c in d['credentials'] if c['name']=='$DEMO_NAME'])
print('  实时会话：      ', [(s['name'], s['online'], s['local_addr'], s['connector_version']) for s in d['sessions'] if s['name']=='$DEMO_NAME'])
print('  网关已配置：    ', d['gateway']['configured'])"

	# 真实流量穿隧道：外部用户视角 = 业务入口 + 域名。
	title=$(curl -s -H "Host: $DEMO_HOST" "$ENTRY/" | grep -o '今日在售' | head -1)
	[ "$title" = "今日在售" ] || die "经隧道的业务请求失败（入口 ${ENTRY}，Host ${DEMO_HOST}）"
	echo "  真实流量：Host=$DEMO_HOST 的请求已穿隧道返回业务首页 ✓"

	# 自动登记：反向链接器页应出现 source=connector 的服务。
	reg=$(api GET /services)
	printf '%s' "$reg" | python3 -c "
import json,sys
d=json.load(sys.stdin)
svc=next((s for s in d['services'] if s.get('source')=='connector'),None)
print('  自动登记：      ', (svc['name'], svc['upstream'], svc['hosts']) if svc else '（未见 connector 来源登记！）')"

	# 管理能力演示：重置密钥（旧 key 即时作废）→ 恢复。
	if [ "${1:-}" = "manage" ]; then
		echo "== ⑥ 管理演示：重置密钥（旧 key 即时作废）=="
		cred_id=$(printf '%s' "$overview" | python3 -c "import json,sys
d=json.load(sys.stdin)
print(next((c['id'] for c in d['credentials'] if c['name']=='$DEMO_NAME'),''))")
		api POST "/connectors/credentials/$cred_id/reset" '{}' >/dev/null
		echo "  密钥已重置：旧连接器将在心跳周期内被拒（重连需要新 key）——这就是吊销即时性。"
		echo "  （演示进程已停；重新运行 demo/up.sh 即可用新 key 恢复接入。）"
		stop_demo
		return 0
	fi
	echo
	echo "演示就绪。打开管控台查看："
	echo "  接入管理  $UI/connectors   （连接器在线、心跳、RTT；可吊销 / 重置）"
	echo "  反向链接器 $UI/services    （自动登记的 demo 服务，来源=连接器）"
	echo "  欺骗层    $UI/deception    （判定流：点任意行看链路）"
	echo "  业务站    经 $ENTRY 访问，带 Host: $DEMO_HOST"
	echo "停止演示：demo/up.sh stop"
}

case ${1:-up} in
up)
	build
	start_business
	issue_credential
	start_connector
	verify
	;;
verify)
	login
	verify
	;;
manage)
	build >/dev/null
	login
	overview=$(api GET /connectors)
	verify manage
	;;
stop) stop_demo ;;
*) die "用法：$0 up|verify|manage|stop" ;;
esac
