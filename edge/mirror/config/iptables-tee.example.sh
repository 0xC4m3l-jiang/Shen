#!/usr/bin/env bash
# 形态① 旁路镜像 —— iptables TEE 示例（仅限自有或书面授权环境）
#
# 作用：把入站 443 的流量复制一份到引擎所在主机。
# 注意：TEE 目标须与镜像源在同一网段；跨网段需配合路由。
#
# 依据：批准的接入形态①；仅限拥有或获得书面授权的环境使用。
set -euo pipefail

ENGINE_IP="${ENGINE_IP:?用法: ENGINE_IP=<引擎IP> $0}"
PORT="${PORT:-443}"

iptables -t mangle -A PREROUTING -p tcp --dport "$PORT" -j TEE --gateway "$ENGINE_IP"
echo "已添加镜像规则：:${PORT} -> ${ENGINE_IP}"
echo "移除：iptables -t mangle -D PREROUTING -p tcp --dport ${PORT} -j TEE --gateway ${ENGINE_IP}"
