#!/usr/bin/env bash
# 一键起「可以看」的本地环境：核心 + 假业务站 + 反向代理 + 观测控制台。
#
# 它服务于**人工测试**：起完就打印四条地址，打开控制台就能看到
# 「谁访问了什么、被判成什么、然后去了哪」。
#
# 为什么需要它：本项目的判定分数与命中信号**不能**从响应里看
# （`ST-7` 禁止回显），只能从观测面看 —— 所以人工测试必须先把观测面支起来。
#
# 用法：scripts/demo/run.sh        （Ctrl-C 停止，退出时清理所有子进程）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CORE_LISTEN="${SHEN_LISTEN:-127.0.0.1:19460}"
CONSOLE_LISTEN="${SHEN_CONSOLE_LISTEN:-127.0.0.1:19461}"
PROXY_LISTEN="${SHEN_PROXY_LISTEN:-127.0.0.1:18080}"
BUSINESS_PORT="${SHEN_DEMO_BUSINESS_PORT:-19080}"

cd "$ROOT"
echo "构建（core / proxy / console）…"
# 所有临时产物统一落在 ${TMPDIR:-/tmp}/shen-<uid>/ —— 仓库里不留任何文件（与 scripts/shen.sh 一致）。
_TMPBASE=${TMPDIR:-/tmp}
RUNDIR=${SHEN_RUNDIR:-${_TMPBASE%/}/shen-$(id -u)}
mkdir -p "$RUNDIR"
go build -o "$RUNDIR/core" ./core/cmd/core
go build -o "$RUNDIR/proxy" ./deception/proxy/cmd/proxy
go build -o "$RUNDIR/console" ./console/cmd/console

pids=()
cleanup() {
  echo
  echo "清理子进程…"
  for p in "${pids[@]:-}"; do kill "$p" 2>/dev/null || true; done
}
trap cleanup EXIT INT TERM

# ① 假业务站（放行时请求原样落到这里）
python3 "$ROOT/scripts/demo/business.py" "$BUSINESS_PORT" &
pids+=($!)

# ② 核心（示例配置：含两条观察用规则；默认影子模式 = 只观测不处置）
SHEN_CONFIG="${SHEN_CONFIG:-deploy/config/config.example.yaml}" \
  SHEN_LISTEN="$CORE_LISTEN" "$RUNDIR/core" &
pids+=($!)

# ③ 反向代理接入（形态③）：业务地址指向假站点
SHEN_PROXY_UPSTREAM="http://127.0.0.1:$BUSINESS_PORT" \
  SHEN_CORE_ADDR="$CORE_LISTEN" \
  SHEN_PROXY_LISTEN="$PROXY_LISTEN" \
  SHEN_PROXY_POLICY_INTERVAL=60s "$RUNDIR/proxy" &
pids+=($!)

# ④ 观测控制台（只读）
SHEN_CORE_ADDR="$CORE_LISTEN" SHEN_CONSOLE_LISTEN="$CONSOLE_LISTEN" "$RUNDIR/console" &
pids+=($!)

sleep 3
echo
echo "──────────────────────────────────────────────────────────────"
echo "  已启动（Ctrl-C 停止）"
echo "  · 控制台（先看这个）    http://$CONSOLE_LISTEN/"
echo "  · 业务侧入口（经引擎）  http://$PROXY_LISTEN/    ← 访问它就会产生判定记录"
echo "  · 核心判定面（gRPC）    $CORE_LISTEN"
echo "  · 假业务站（直连对照）  http://127.0.0.1:$BUSINESS_PORT/"
echo
echo "  试一试："
echo "    curl -s -A HeadlessChrome/120 http://$PROXY_LISTEN/"
echo "    curl -s -A HeadlessChrome/120 http://$PROXY_LISTEN/.git/config"
echo "    curl -s http://$CONSOLE_LISTEN/api/flow?limit=5"
echo "  （判定分数与命中信号只在控制台可见 —— 判定响应禁止回显，ST-7）"
echo "──────────────────────────────────────────────────────────────"
wait
