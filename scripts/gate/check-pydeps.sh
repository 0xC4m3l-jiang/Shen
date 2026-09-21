#!/usr/bin/env bash
# 锁文件 ↔ venv 一致性检查（依据 `TB-16` · ADR-0024 决定 2 的推论）。
#
# 为什么需要它（不是重复 licensecheck）：
#   · `scripts/licensecheck` 审的是**许可**，它顺带发现的版本不一致只是副产品；
#   · 而「锁文件是权威、环境必须向它对齐」本身是一条独立的工程约束 ——
#     环境比锁文件旧时，`pytest` / `ruff` 跑的是另一套版本，绿也可能是假绿。
#   两者**故意重叠**：一个是合规判据，一个是环境判据。
#
# 检查方向（刻意单向）：锁文件里**逐条**要求的 `pkg==ver`，venv 里必须存在且版本相同。
#   venv 里多出来的东西（pip 的传递依赖、setuptools）**不报** ——
#   它们在两个锁文件里本来就不被声明，报出来只会训练人忽略这个检查。
#
# 缺 venv 时**直接失败**：静默跳过等于假绿（`scripts/gate/README.md` 的纪律）。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PY="${ROOT}/analysis/.venv/bin/python"
LOCKS=("${ROOT}/analysis/requirements.txt" "${ROOT}/analysis/requirements-dev.txt")

if [ ! -x "${PY}" ]; then
	echo "✗ 缺 Python 环境（analysis/.venv）—— 先跑 make pyenv"
	echo "  禁止跳过本检查 —— 静默跳过等于假绿。"
	exit 1
fi

# 从**实际安装**的发行版读版本（不是从锁文件推），规范名与 pip 一致：小写 + `_`/`.` → `-`。
installed="$(
	"${PY}" - <<'PYEOF'
import importlib.metadata as md


def canon(name: str) -> str:
    """PEP 503 的规范名（`importlib.metadata` 的查询语义与它一致）。"""
    return name.lower().replace("_", "-").replace(".", "-")


for dist in md.distributions():
    raw = dist.metadata["Name"]
    if raw:
        print(f"{canon(raw)}={dist.version}")
PYEOF
)"

fail=0
checked=0

for lock in "${LOCKS[@]}"; do
	if [ ! -f "${lock}" ]; then
		echo "✗ 锁文件不存在：${lock}"
		exit 1
	fi
	while IFS= read -r raw || [ -n "${raw}" ]; do
		line="${raw%%#*}"
		line="$(printf '%s' "${line}" | tr -d '[:space:]')"
		[ -n "${line}" ] || continue
		case "${line}" in
		*'==') ;;
		*'=='[0-9A-Za-z]*) ;;
		*)
			echo "✗ ${lock##*/} 里出现非严格定版的行：${raw}"
			echo "  锁文件只允许 'pkg==ver'（版本必须可复算，禁止范围约束）。"
			fail=1
			continue
			;;
		esac
		name="${line%%==*}"
		want="${line##*==}"
		canon="$(printf '%s' "${name}" | tr '[:upper:]' '[:lower:]' | tr '_.' '--')"
		got="$(printf '%s\n' "${installed}" | grep -m1 "^${canon}=" | cut -d= -f2- || true)"
		checked=$((checked + 1))
		if [ -z "${got}" ]; then
			echo "✗ ${name}：锁文件要求 ${want}，venv 里**没有装**"
			fail=1
			continue
		fi
		if [ "${got}" != "${want}" ]; then
			echo "✗ ${name}：锁文件 ${want} vs 环境 ${got} 不一致"
			fail=1
		fi
	done <"${lock}"
done

if [ "${fail}" -ne 0 ]; then
	echo
	echo "锁文件与环境不一致 —— 跑 make pyenv 重建环境（锁文件是权威，环境向它对齐）。"
	exit 1
fi

echo "✓ Python 依赖版本一致（${checked} 条 pin：analysis/requirements.txt + requirements-dev.txt）"
