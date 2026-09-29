#!/bin/sh
# 恶意流量模拟：对着「蜃景商城」（demo 业务）发起一波典型攻击探针，
# 全部经蜃楼业务入口（引擎路径）发送，用于演示判定 / 改道 / 拦截与告警观测。
#
# 前置：demo/up.sh 已跑（demo 业务经隧道在线），且蜃楼栈在跑。
# 用法（仓库根执行）：
#   demo/attack.sh                 # 默认打 demo.shop.local
#   demo/attack.sh 其他入口 域名    # 例：demo/attack.sh http://127.0.0.1:18080 demo.shop.local
#
# 判定结果在哪里看：
#   管控台「欺骗层 · 判定流」——每条请求一行（决策 / 信号 / 风险分，点行看链路）；
#   管控台「告警」——满足告警条件的请求单独成列表。
set -eu

ENTRY=${1:-${SHEN_ENTRY:-http://127.0.0.1:18080}}
HOST=${2:-${SHEN_DEMO_HOST:-demo.shop.local}}

say() { printf '%s\n' "$*"; }

say "== 恶意流量模拟：目标 ${HOST}（经 ${ENTRY}，全部走引擎路径）=="
n=0

# ① 敏感文件探测：.git 仓库泄漏（命中 path-git 规则）
n=$((n + 1)); say "$n) 敏感文件探测：GET /.git/config"
curl -s -o /dev/null -H "Host: $HOST" "$ENTRY/.git/config" || true

# ② 环境变量泄漏探测：.env
n=$((n + 1)); say "$n) 环境变量泄漏探测：GET /.env"
curl -s -o /dev/null -H "Host: $HOST" "$ENTRY/.env" || true

# ③ 后台路径爆破：admin / wp-admin / manager
n=$((n + 1)); say "$n) 后台路径爆破：/admin · /wp-admin · /manager"
for p in admin wp-admin manager; do
	curl -s -o /dev/null -H "Host: $HOST" "$ENTRY/$p" || true
done

# ④ 无头浏览器扫描器指纹（命中 ua-headless 规则）
n=$((n + 1)); say "$n) 无头扫描器指纹：HeadlessChrome 抓首页与商品页"
curl -s -o /dev/null -A "Mozilla/5.0 (HeadlessChrome/120)" -H "Host: $HOST" "$ENTRY/" || true
curl -s -o /dev/null -A "HeadlessChrome/120" -H "Host: $HOST" "$ENTRY/product?id=1" || true

# ⑤ 注入探针：查询串 SQL 注入形态
n=$((n + 1)); say "$n) 注入探针：/product?id=1' OR '1'='1"
curl -s -o /dev/null -H "Host: $HOST" "$ENTRY/product?id=1%27%20OR%20%271%27%3D%271" || true

# ⑥ 路径穿越
n=$((n + 1)); say "$n) 路径穿越：/../../etc/passwd"
curl -s -o /dev/null --path-as-is -H "Host: $HOST" "$ENTRY/../../etc/passwd" || true

# ⑦ 组合拳：无头 UA + 敏感路径（权重叠加，通常触发更高风险分）
n=$((n + 1)); say "$n) 组合拳：HeadlessChrome + /.git/config（权重叠加）"
curl -s -o /dev/null -A "Mozilla/5.0 (HeadlessChrome/120)" -H "Host: $HOST" "$ENTRY/.git/config" || true

say ""
say "已发送 $n 组探针。观测入口："
say "  管控台「欺骗层 · 判定流」：每条一行（决策 / 信号 / 风险分，点行看逐跳链路）"
say "  管控台「告警」：满足条件的请求单独成列表"
say "  （本地栈默认影子模式：判定照算、事件照报，但业务不受影响——首次上线必须如此。）"
