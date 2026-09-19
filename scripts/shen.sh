#!/bin/sh
# Shen 的统一入口：启动 / 检查 / 冒烟 / 日志 / 停止。
#
# 为什么要有它（Makefile 里已有 make up 等目标）：
#   ① 启动后**自动等就绪**并打印可访问地址，避免"起来了但还没好"的误判；
#   ② 所有临时产物只落在 ${TMPDIR:-/tmp}/shen-<uid>/，**不在仓库里留任何文件**；
#   ③ 日志默认**不跟随**（取尾部即返回）—— `docker logs -f` 会挂住，人和自动化都不友好。
#
# 用法：
#   scripts/shen.sh up          起全套（Docker；默认模式）
#   scripts/shen.sh status      看容器状态 + 控制台概览
#   scripts/shen.sh smoke       造三条流量并回显判定结果（快速验证链路）
#   scripts/shen.sh traffic     发**伪造流量**并从观测面核对判定（完整验证；见 scripts/traffic/）
#   scripts/shen.sh check       仓库级验证：make gate + make dev
#   scripts/shen.sh logs [svc]  看日志尾部（不跟随；svc 如 core/proxy/console/analysis）
#   scripts/shen.sh local       不用 Docker，本地进程起（开发用）
#   scripts/shen.sh down        停掉并删容器
#
# 端口冲突时：SHEN_HTTP_PORT=8080 SHEN_CONSOLE_PORT=9444 scripts/shen.sh up
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
# 去掉 TMPDIR 可能带的尾斜杠，避免出现 ".../T//shen-501" 这种路径
_TMPBASE=${TMPDIR:-/tmp}
RUNDIR=${SHEN_RUNDIR:-${_TMPBASE%/}/shen-$(id -u)}
HTTP_PORT=${SHEN_HTTP_PORT:-18080}
CONSOLE_PORT=${SHEN_CONSOLE_PORT:-19444}
ENTRY="http://127.0.0.1:${HTTP_PORT}"
CONSOLE="http://127.0.0.1:${CONSOLE_PORT}"

compose() { docker compose -f "${ROOT}/deploy/docker/compose.yaml" "$@"; }

die() { echo "shen: $*" >&2; exit 1; }

need_docker() {
	command -v docker >/dev/null 2>&1 || die "找不到 docker —— 本地模式请用 scripts/shen.sh local"
	docker info >/dev/null 2>&1 || die "Docker 守护进程没跑 —— 先启动 Docker Desktop / dockerd"
}

# wait_ready 等控制台可读；核心是否可达由控制台的回答间接证明（ST-17 的语义区分）。
wait_ready() {
	i=0
	while [ "$i" -lt 60 ]; do
		if curl -fsS -m 2 "${CONSOLE}/api/summary" >/dev/null 2>&1; then
			return 0
		fi
		i=$((i + 1))
		sleep 1
	done
	echo "控制台 60 秒内未就绪，当前状态："
	compose ps || true
	return 1
}

print_urls() {
	cat <<EOF

  ✅ 已就绪
     观测控制台   ${CONSOLE}/        （概览 · 告警 · 流量访问与流动 · L4 分析结论）
     业务入口     ${ENTRY}/          （经引擎；影子模式：只观测、不处置，INT-11）

  造点流量看效果：
     curl -s -A "HeadlessChrome/120" ${ENTRY}/.git/config
     curl -s -A "Mozilla/5.0"        ${ENTRY}/

  看状态 / 日志 / 停止：
     scripts/shen.sh status | scripts/shen.sh logs core | scripts/shen.sh down
  临时产物目录（不在仓库里）：${RUNDIR}
EOF
}

cmd_up() {
	need_docker
	mkdir -p "${RUNDIR}"
	SHEN_HTTP_PORT="${HTTP_PORT}" SHEN_CONSOLE_PORT="${CONSOLE_PORT}" \
		compose up -d --build >"${RUNDIR}/up.log" 2>&1 ||
		{ echo "启动失败，日志尾部："; tail -20 "${RUNDIR}/up.log"; exit 1; }
	wait_ready || exit 1
	print_urls
}

cmd_status() {
	need_docker
	compose ps
	echo
	if curl -fsS -m 3 "${CONSOLE}/api/summary" >/dev/null 2>&1; then
		echo "控制台：可读（${CONSOLE}）"
		curl -fsS -m 3 "${CONSOLE}/api/summary" | python3 -m json.tool 2>/dev/null ||
			curl -fsS -m 3 "${CONSOLE}/api/summary"
	else
		echo "控制台：不可读（${CONSOLE}）—— 看日志：scripts/shen.sh logs console"
		return 1
	fi
}

cmd_smoke() {
	need_docker
	echo "经引擎发三条流量（${ENTRY}）："
	for spec in "HeadlessChrome/120 /.git/config" "sqlmap/1.7 /etc/passwd" "Mozilla/5.0 /"; do
		set -- ${spec}
		code=$(curl -s -o /dev/null -m 10 -w '%{http_code}' -A "$1" "${ENTRY}$2" || echo 000)
		printf '  HTTP %s  %s  %s\n' "${code}" "$1" "$2"
	done
	echo
	echo "控制台看到的流动（分值 / 命中信号只在这里可见，ST-7）："
	# 用 here-doc 把脚本交给 python（避免在 shell 字符串里折腾转义引号 —— 那一版真写坏过）
	python3 - "${CONSOLE}" <<'PY' || echo "  （读取失败：scripts/shen.sh status 看控制台）"
import json
import sys
import urllib.request

data = json.load(urllib.request.urlopen(sys.argv[1] + "/api/flow?limit=5", timeout=5))
print(f"  {len(data)} 行")
for row in data:
    at = str(row.get("at", ""))[11:19]
    print(
        f"    {at} {row.get('action', ''):<12} {row.get('method', ''):<4} "
        f"{row.get('path', ''):<16} score={row.get('score', '')!s:<5} signals={row.get('signals')}"
    )
PY
}

cmd_traffic() {
	# 伪造流量 + 判定核对（只用标准库，不需要 venv）
	exec python3 "${ROOT}/scripts/traffic/send.py" --entry "${ENTRY}" --console "${CONSOLE}" "$@"
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
	# 不跟随：取尾部即返回（`docker logs -f` 会挂住，人和自动化都不友好）
	compose logs --tail=80 "$@"
}

cmd_local() {
	exec "${ROOT}/scripts/demo/run.sh"
}

usage() {
	sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
}

case "${1:-help}" in
up) shift; cmd_up "$@" ;;
status) shift; cmd_status "$@" ;;
smoke) shift; cmd_smoke "$@" ;;
traffic) shift; cmd_traffic "$@" ;;
check) shift; cmd_check "$@" ;;
logs) shift; cmd_logs "$@" ;;
local) shift; cmd_local "$@" ;;
down) shift; need_docker; compose down ;;
help | -h | --help) usage ;;
*) die "未知子命令：$1（可用：up status smoke traffic check logs local down help）" ;;
esac
