#!/bin/sh
# Shen 的统一入口：启动 / 检查 / 冒烟 / 日志 / 停止（Linux / macOS / WSL；Windows 原生请用 scripts/shen.ps1）。
#
# 为什么要有它（直接 docker compose 也能用）：
#   ① 首次运行自动生成 .env（管理员默认 admin/admin；只读 API 令牌自动随机），不必手填；
#   ② 启动后**自动等就绪**并打印可访问地址，避免"起来了但还没好"的误判；
#   ③ 所有临时产物只落在 ${TMPDIR:-/tmp}/shen-<uid>/，**不在仓库里留任何文件**（.env 除外，它被 git 忽略）；
#   ④ 日志默认**不跟随**（取尾部即返回）—— `docker logs -f` 会挂住，人和自动化都不友好。
#
# 用法：
#   scripts/shen.sh up [--env dev|prod|verify] [--profile P ...]   起栈（默认：全部模块、构建好的前端）
#   scripts/shen.sh status      看容器状态 + 管控台概览
#   scripts/shen.sh smoke       造三条流量并回显判定结果（快速验证链路）
#   scripts/shen.sh traffic     发**伪造流量**并从观测面核对判定（完整验证；见 demo/traffic/）
#   scripts/shen.sh check       仓库级验证：make gate + make dev
#   scripts/shen.sh doctor      接入自检（INT-17 五项）
#   scripts/shen.sh verify      一键端到端验证：状态 + 全量伪造流量 + L4 核对 + 报告
#   scripts/shen.sh logs [svc]  看日志尾部（不跟随；svc 如 core/proxy/console-api/console-ui/honeypot-web）
#   scripts/shen.sh restart     整栈重建（改配置/换镜像后用；不要单独重启 core）
#   scripts/shen.sh down        停掉并删容器（数据卷保留；彻底清理用 docker compose down -v）
#
# 例：scripts/shen.sh up --profile honeypot        只起蜜罐（core 永远随行）
#     scripts/shen.sh up --env dev                  开发形态（Vite 热更新 + 调试端口）
# 端口冲突时：SHEN_HTTP_PORT=8080 SHEN_CONSOLE_PORT=9444 scripts/shen.sh up
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
ENV_FILE="${ROOT}/.env"
_TMPBASE=${TMPDIR:-/tmp}
RUNDIR=${SHEN_RUNDIR:-${_TMPBASE%/}/shen-$(id -u)}
SHEN_ENV=${SHEN_ENV:-}
PROFILES=""

die() { echo "shen: $*" >&2; exit 1; }

need_docker() {
	command -v docker >/dev/null 2>&1 || die "找不到 docker —— 本地模式请用 scripts/shen.sh local"
	docker info >/dev/null 2>&1 || die "Docker 守护进程没跑 —— 先启动 Docker Desktop / dockerd"
	docker compose version >/dev/null 2>&1 || die "需要 Docker Compose v2（docker compose），旧版 docker-compose v1 不受支持"
}

# env_get KEY：shell 环境优先，其次 .env（与 Compose 的优先级一致）。
env_get() {
	eval "val=\${$1:-}"
	if [ -z "${val}" ] && [ -f "${ENV_FILE}" ]; then
		val=$(grep -E "^$1=" "${ENV_FILE}" | tail -1 | cut -d= -f2- | tr -d '\r')
	fi
	printf '%s' "${val}"
}

random_secret() {
	if command -v openssl >/dev/null 2>&1; then
		openssl rand -base64 36 | tr -d '/+=\n' | cut -c1-32
	else
		python3 -c 'import secrets;print(secrets.token_urlsafe(24))'
	fi
}

# ensure_env：首次运行从模板生成 .env，并填入随机初始口令与只读令牌（已有 .env 时绝不覆盖）。
ensure_env() {
	[ -f "${ENV_FILE}" ] && return 0
	[ -f "${ROOT}/.env.example" ] || die "缺少 .env.example"
	token=$(random_secret)
	sed -e "s|^SHEN_CONSOLE_API_TOKEN=.*|SHEN_CONSOLE_API_TOKEN=${token}|" \
		"${ROOT}/.env.example" >"${ENV_FILE}"
	chmod 600 "${ENV_FILE}" 2>/dev/null || true
	echo "已生成 ${ENV_FILE}（管理员默认 admin/admin 便于验证，首登强制改密；只读令牌为随机值）"
}

parse_opts() {
	while [ $# -gt 0 ]; do
		case "$1" in
		--env) SHEN_ENV="$2"; shift 2 ;;
		--env=*) SHEN_ENV="${1#--env=}"; shift ;;
		--profile) PROFILES="${PROFILES} $2"; shift 2 ;;
		--profile=*) PROFILES="${PROFILES} ${1#--profile=}"; shift ;;
		*) die "未知参数：$1" ;;
		esac
	done
}

# compose：固定项目目录与环境文件（与当前工作目录无关），按 --env 叠加文件、按 --profile 选模块。
compose() {
	ensure_env
	files="-f ${ROOT}/compose.yaml"
	case "${SHEN_ENV}" in
	"") ;;
	dev | prod | verify-mirage) files="${files} -f ${ROOT}/deploy/docker/compose.${SHEN_ENV}.yaml" ;;
	verify) files="${files} -f ${ROOT}/deploy/docker/compose.verify-mirage.yaml" ;;
	*) die "未知 --env：${SHEN_ENV}（可选 dev / prod / verify）" ;;
	esac
	env_file="${ENV_FILE}"
	profile_args=""
	if [ -n "${PROFILES}" ]; then
		# 显式指定模块时，去掉 .env 里的 COMPOSE_PROFILES（否则两者叠加，「只起蜜罐」会变成「全部」）。
		mkdir -p "${RUNDIR}"
		env_file="${RUNDIR}/compose.env"
		grep -v '^COMPOSE_PROFILES=' "${ENV_FILE}" >"${env_file}" || true
		for p in ${PROFILES}; do profile_args="${profile_args} --profile ${p}"; done
	fi
	# shellcheck disable=SC2086 # files / profile_args 需要按空格拆分
	docker compose --project-directory "${ROOT}" --env-file "${env_file}" ${files} ${profile_args} "$@"
}

console_url() { printf 'http://127.0.0.1:%s' "$(env_get SHEN_CONSOLE_PORT | sed 's/^$/19444/')"; }
entry_url() { printf 'http://127.0.0.1:%s' "$(env_get SHEN_HTTP_PORT | sed 's/^$/18080/')"; }

wants_console() {
	[ -z "${PROFILES}" ] && return 0
	case " ${PROFILES} " in *" all "* | *" frontend "* | *" console "*) return 0 ;; esac
	return 1
}

# wait_ready：等管控台（nginx → API）健康；API 的 /healthz 只反映自身存活，核心是否可达看页面顶栏。
wait_ready() {
	wants_console || return 0
	i=0
	while [ "$i" -lt 90 ]; do
		if curl -fsS -m 2 "$(console_url)/healthz" >/dev/null 2>&1; then
			return 0
		fi
		i=$((i + 1))
		sleep 1
	done
	echo "管控台 90 秒内未就绪，当前状态："
	compose ps || true
	return 1
}

print_urls() {
	cat <<EOF

  ✅ 已就绪（模块：${PROFILES:-$(env_get COMPOSE_PROFILES)}；形态：${SHEN_ENV:-默认}）
     管控台       $(console_url)/        账号 $(env_get SHEN_CONSOLE_BOOTSTRAP_USER | sed 's/^$/admin/')
                  默认口令 admin（.env 的 SHEN_CONSOLE_BOOTSTRAP_PASSWORD 可改为 random：随机生成并写入 /data/bootstrap-admin.txt）
     业务入口     $(entry_url)/          （经引擎；影子模式：只观测、不处置，INT-11）

  造点流量看效果：
     curl -s -A "HeadlessChrome/120" $(entry_url)/.git/config
     curl -s -A "Mozilla/5.0"        $(entry_url)/

  看状态 / 日志 / 停止：
     scripts/shen.sh status | scripts/shen.sh logs core | scripts/shen.sh down
  临时产物目录（不在仓库里）：${RUNDIR}
EOF
}

api_get() {
	token=$(env_get SHEN_CONSOLE_API_TOKEN)
	[ -n "${token}" ] || { echo "  （未设置 SHEN_CONSOLE_API_TOKEN，无法读取管控台接口）"; return 1; }
	curl -fsS -m 5 -H "Authorization: Bearer ${token}" "$(console_url)$1"
}

cmd_up() {
	parse_opts "$@"
	need_docker
	ensure_env
	mkdir -p "${RUNDIR}"
	compose up -d --build >"${RUNDIR}/up.log" 2>&1 ||
		{ echo "启动失败，日志尾部："; tail -30 "${RUNDIR}/up.log"; exit 1; }
	wait_ready || exit 1
	print_urls
}

cmd_status() {
	need_docker
	mkdir -p "${RUNDIR}"
	compose ps
	echo
	if api_get /api/v1/system/status >"${RUNDIR:-/tmp}/status.json" 2>/dev/null; then
		echo "管控台：可读（$(console_url)）"
		python3 -m json.tool "${RUNDIR}/status.json" 2>/dev/null || cat "${RUNDIR}/status.json"
	else
		echo "管控台：不可读（$(console_url)）—— 看日志：scripts/shen.sh logs console-api console-ui"
		return 1
	fi
}

cmd_smoke() {
	need_docker
	echo "经引擎发三条流量（$(entry_url)）："
	for spec in "HeadlessChrome/120 /.git/config" "sqlmap/1.7 /etc/passwd" "Mozilla/5.0 /"; do
		set -- ${spec}
		code=$(curl -s -o /dev/null -m 10 -w '%{http_code}' -A "$1" "$(entry_url)$2" || echo 000)
		printf '  HTTP %s  %s  %s\n' "${code}" "$1" "$2"
	done
	sleep 1
	echo
	echo "管控台看到的判定（分值 / 命中信号只在这里可见，ST-7）："
	# 用 here-doc 把脚本交给 python（避免在 shell 字符串里折腾转义引号 —— 那一版真写坏过）
	mkdir -p "${RUNDIR}"
	api_get "/api/v1/deception/flow?limit=5" >"${RUNDIR}/flow.json" && python3 - "${RUNDIR}/flow.json" <<'PY' || echo "  （读取失败：scripts/shen.sh status 看管控台）"
import json
import sys

data = json.load(open(sys.argv[1], encoding="utf-8"))
print(f"  {len(data)} 行")
for row in data:
    at = str(row.get("at", ""))[11:19]
    geo = (row.get("geo") or {}).get("label", "")
    print(
        f"    {at} {row.get('action', ''):<12} {row.get('method', ''):<4} "
        f"{row.get('path', ''):<16} score={row.get('score', '')!s:<5} 归属地={geo}"
    )
PY
}

with_token() {
	SHEN_CONSOLE_API_TOKEN=$(env_get SHEN_CONSOLE_API_TOKEN)
	export SHEN_CONSOLE_API_TOKEN
}

cmd_traffic() {
	with_token
	exec python3 "${ROOT}/demo/traffic/send.py" --entry "$(entry_url)" --console "$(console_url)" "$@"
}

cmd_restart() {
	# 两件必须一起做的事（都实测踩过）：
	#   ① **整栈重建**：core 是网络命名空间的持有者，单独重启它会让兄弟服务留在旧命名空间 ⇒ 容器都 Up 但端口不通；
	#   ② **带上 --build**：`--force-recreate` 只重建容器、**不重建镜像**。
	parse_opts "$@"
	need_docker
	ensure_env
	mkdir -p "${RUNDIR}"
	compose up -d --build --force-recreate >"${RUNDIR}/restart.log" 2>&1 ||
		{ echo "重启失败，日志尾部："; tail -30 "${RUNDIR}/restart.log"; exit 1; }
	wait_ready || exit 1
	echo "✅ 已整栈重建（不要单独 restart core：兄弟容器会留在旧网络命名空间）"
}

cmd_doctor() {
	with_token
	exec python3 "${ROOT}/scripts/doctor/doctor.py" --entry "$(entry_url)" --console "$(console_url)" "$@"
}

cmd_verify() {
	need_docker
	with_token
	mkdir -p "${RUNDIR}"
	ts=$(date +%Y%m%d-%H%M%S)
	report="${RUNDIR}/verify-${ts}.json"
	echo "== 1/3 容器与观测面 =="
	cmd_status || true
	echo
	echo "== 2/3 伪造流量 + 判定核对（--check-l4）=="
	set +e
	python3 "${ROOT}/demo/traffic/send.py" \
		--entry "$(entry_url)" --console "$(console_url)" --check-l4 --check-graph --explain --report "${report}" "$@"
	rc=$?
	set -e
	echo
	echo "== 3/3 报告与定位线索 =="
	echo "  报告：${report}"
	echo "  逐判定日志：scripts/shen.sh logs core | grep msg=decision"
	echo "  适配器侧：  scripts/shen.sh logs proxy | grep 'proxy: 判定'"
	return $rc
}

cmd_check() {
	echo "== 仓库级验证 1/2：make gate =="
	make -C "${ROOT}" gate
	echo
	echo "== 仓库级验证 2/2：make dev =="
	make -C "${ROOT}" dev
}

cmd_logs() {
	need_docker
	compose logs --tail=80 "$@"
}

usage() {
	sed -n '2,26p' "$0" | sed 's/^# \{0,1\}//'
}

case "${1:-help}" in
up) shift; cmd_up "$@" ;;
status) shift; cmd_status "$@" ;;
smoke) shift; cmd_smoke "$@" ;;
traffic) shift; cmd_traffic "$@" ;;
check) shift; cmd_check "$@" ;;
doctor) shift; cmd_doctor "$@" ;;
verify) shift; cmd_verify "$@" ;;
logs) shift; cmd_logs "$@" ;;
restart) shift; cmd_restart "$@" ;;
down) shift; parse_opts "$@"; need_docker; compose down ;;
ps) shift; parse_opts "$@"; need_docker; compose ps ;;
help | -h | --help) usage ;;
*) die "未知子命令：$1（可用：up status smoke traffic doctor verify check logs restart down ps help）" ;;
esac
