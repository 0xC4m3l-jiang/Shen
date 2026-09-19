#!/usr/bin/env bash
#
# 一键开发验证（make dev）。
#
# 验证的是「这条流水线通不通」，不是「判定准不准」：
#   ① 合法配置能装载、能打印策略摘要
#   ② 非法配置必须被拒（禁止以空规则集静默启动 —— 阶段 1 的未闭合项）
#   ③ 核心能起、判定面能应答、同一 decision_id 幂等（ST-10）
#   ④ 规则 → 分数的回放明细（判定响应不回显分值，只能在这里看，ST-7）
#
# 全程用临时目录里的配置与临时端口，不碰仓库内的文件、不撞已在跑的实例。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

ADDR="${SHEN_DEV_ADDR:-}"
TMP="$(mktemp -d)"
CFG="$TMP/config.yaml"
TPL="$TMP/config.tpl"
BAD="$TMP/bad-config.yaml"
LOG="$TMP/core.log"
BAD_LOG="$TMP/bad.log"
CORE_PID=""

# 自动挑一个空闲端口：开发机上什么都会占端口（实测 19443 被一个音乐播放器占着），
# 写死端口会让「一键验证」变成「看运气」。
pick_port() {
	local p
	for p in $(seq 19500 19550); do
		if ! (exec 3<>/dev/tcp/127.0.0.1/"$p") 2>/dev/null; then
			echo "$p"
			return 0
		fi
	done
	echo "找不到空闲端口（19500-19550 全被占用）" >&2
	return 1
}

cleanup() {
	if [ -n "$CORE_PID" ] && kill -0 "$CORE_PID" 2>/dev/null; then
		kill "$CORE_PID" 2>/dev/null || true
		wait "$CORE_PID" 2>/dev/null || true
	fi
	rm -rf "$TMP"
}
trap cleanup EXIT

fail() {
	echo
	echo "❌ $1"
	if [ -s "$LOG" ]; then
		echo "--- 核心日志 ---"
		cat "$LOG"
	fi
	exit 1
}

cat >"$TPL" <<'YAML'
core:
  listen: "__ADDR__"
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
  policy_id: "dev-smoke"
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

if [ -z "$ADDR" ]; then
	ADDR="127.0.0.1:$(pick_port)"
fi
sed "s|__ADDR__|$ADDR|" "$TPL" >"$CFG"

# 非法配置：权重越界，必须被拒并指出字段。
sed 's/weight: 0.3/weight: 1.5/' "$CFG" >"$BAD"

echo "配置：${CFG}（临时） · 判定面：${ADDR}"
echo
echo "== 1/6 配置干跑：合法配置 =="
SHEN_CONFIG="$CFG" go run ./core/cmd/core -check-config || fail "合法配置装载失败"

echo
echo "== 2/6 配置干跑：非法配置必须被拒 =="
if SHEN_CONFIG="$BAD" go run ./core/cmd/core -check-config >"$BAD_LOG" 2>&1; then
	fail "非法配置竟然通过校验（配置校验形同虚设）"
fi
if ! grep -q "weight" "$BAD_LOG"; then
	echo "--- 非法配置的报错 ---"
	cat "$BAD_LOG"
	fail "报错没有指出出错字段"
fi
grep "weight" "$BAD_LOG" | head -1

echo
echo "== 3/6 起核心（影子模式）：$ADDR =="
SHEN_CONFIG="$CFG" SHEN_LISTEN="$ADDR" go run ./core/cmd/core >"$LOG" 2>&1 &
CORE_PID=$!
for _ in $(seq 1 60); do
	if grep -q "核心已启动" "$LOG"; then
		break
	fi
	kill -0 "$CORE_PID" 2>/dev/null || fail "核心进程提前退出"
	sleep 0.5
done
grep -q "核心已启动" "$LOG" || fail "核心在 30 秒内没有就绪"
cat "$LOG"

echo
echo "== 4/6 在线冒烟（判定面形状 + 幂等） =="
go run ./scripts/devcheck -addr "$ADDR" || fail "在线冒烟失败"
echo
echo "== 5/6 规则回放（离线，打印命中明细） =="
go test -count=1 -run TestRuleReplay -v ./core/cmd/core || fail "规则回放失败"

echo
echo
echo "== 6/6 L4 近线分析（读遥测事件 → 意图/链/策略 → 结论事件） =="
if [ -x "${ROOT}/.venv/bin/python" ]; then
	SHEN_CORE_ADDR="${ADDR}" "${ROOT}/.venv/bin/python" -m analysis.worker \
		--core "${ADDR}" --once || fail "L4 近线分析未通过"
else
	fail "缺 L4 环境：先跑 make pyenv（TB-15：Python 必须过 ruff 与 pytest）"
fi
echo
echo "✅ 通过：配置校验 · 非法配置被拒 · 启动 · 判定面 · 冒烟 · 规则回放 · L4 近线分析"
