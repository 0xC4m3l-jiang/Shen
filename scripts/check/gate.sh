#!/usr/bin/env bash
# 门禁里「一条命令说不清」的三项检查，合在一个脚本里（Makefile 只负责编排，判定逻辑放这里）。
#
# 用法（仓库任意位置执行均可，脚本会自己定位仓库根）：
#   scripts/check/gate.sh fmt       Go 格式化检查（只检查、不改文件；= make fmt-check，TB-15）
#   scripts/check/gate.sh secrets   密钥泄漏门禁（= make secrets-check）
#   scripts/check/gate.sh pydeps    Python 锁文件 ↔ venv 一致性（= make check-pydeps，TB-16）
#   scripts/check/gate.sh all       依次跑以上三项，任一失败即非零退出
#
# 纪律：**工具或环境缺失时必须失败，不得跳过** —— 静默跳过等于假绿，比没有门禁更糟。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# ── fmt：所有**本仓库的** Go 文件已 gofmt ───────────────────────────────────────
# 为什么按「包目录」而不是 `gofmt -l .`：vendor/ 是第三方副本，不按本仓库的格式标准要求。
# `go list ./...` 本身不含 vendor，用它列出的包目录来查最准。
check_fmt() {
	cd "${ROOT}"
	local pkgs files
	pkgs=$(go list -f '{{.Dir}}' ./...)
	# shellcheck disable=SC2086 # pkgs 需要按空白拆分成多个目录参数
	files=$(gofmt -l ${pkgs})
	if [ -n "${files}" ]; then
		echo "以下文件未格式化 —— 跑 make fmt 修正："
		echo "${files}"
		return 1
	fi
	echo "fmt-check 通过"
}

# ── secrets：拦截「长得像真实密钥的值」进入版本库 ───────────────────────────────
# 与 leakcheck 的分工：leakcheck 管**对外可见面**的禁用串（OH-1/OH-2），这里管**仓库本身**不携带可用凭证。
#
# 两条规则（判据是“形态像真值”，不是“提到了 key 这个词”）：
#   ① sk- 前缀 + 20 位以上字母数字（OpenAI / DeepSeek / Anthropic 风格的服务密钥）；
#   ② `<NAME>_KEY / _TOKEN / _SECRET / _PASSWORD = <12 位以上非占位值>`
#      （占位符放行：your / example / placeholder / xxxx / changeme / dummy / fake / test / <...> / ${...} / 中文说明）。
# 只扫 git 追踪文件（.env 已被 .gitignore 挡住；万一被误 add，这里会拦下）；vendor/ 不适用。
# 注意：git grep 只扫**已追踪**文件 —— 新文件未 `git add` 时全绿是假象。
check_secrets() {
	cd "${ROOT}"
	if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
		echo "secrets-check: 非 git 仓库 —— 跳过（本检查只针对入库文件）"
		return 0
	fi
	local status=0 sk_hits pattern tmp line value assign_leaks=""

	# 规则①：vendor 里的 sk- 前缀 SSH 算法名（sk-ecdsa-…）带连字符后缀，不满足「20 位纯字母数字」。
	sk_hits=$(git grep -nIE 'sk-[A-Za-z0-9]{20,}' -- . ':!vendor' 2>/dev/null || true)
	if [ -n "${sk_hits}" ]; then
		printf '✗ 发现疑似真实服务密钥（sk- 前缀）：\n%s\n' "${sk_hits}"
		status=1
	fi

	# 规则②：只匹配「环境变量命名风格」的键（至少两段大写下划线），值侧要求 12 位以上。
	pattern='([A-Z][A-Z0-9]+_(API_)?KEY|[A-Z][A-Z0-9]+_(TOKEN|SECRET|PASSWORD))[[:space:]]*[:=][[:space:]]*["'"'"']?[A-Za-z0-9][A-Za-z0-9_-]{11,}'
	tmp=$(mktemp)
	git grep -nIE "${pattern}" -- . ':!vendor' >"${tmp}" 2>/dev/null || true
	# 占位判断**只对等号右侧的值**做 —— 对整行 grep -v 会被「文件:行号:」前缀里的 test/fake 字样洗白真泄漏
	# （红测试实测踩过：fake-leak2.md 里的真实形态被自己的文件名放行）。
	while IFS= read -r line; do
		[ -n "${line}" ] || continue
		value=$(printf '%s\n' "${line}" | sed -E 's/^[^:]+:[0-9]+://; s/^[A-Za-z0-9_]+[[:space:]]*[:=][[:space:]]*//; s/^["'"'"']//')
		if ! printf '%s\n' "${value}" | grep -qiE 'your|example|placeholder|xxxx|changeme|change-?me|dummy|fake|test|sample|\$\{|<[^>]+>|随机|占位|此处|替换'; then
			assign_leaks="${assign_leaks}${line}"$'\n'
		fi
	done <"${tmp}"
	rm -f "${tmp}"
	if [ -n "${assign_leaks}" ]; then
		printf '✗ 发现疑似真实密钥赋值（占位符除外）：\n%s' "${assign_leaks}"
		status=1
	fi

	if [ "${status}" -ne 0 ]; then
		echo
		echo "处置：① 该密钥视为已泄漏，立即在服务商控制台吊销并轮换（删文件不解决已发生的泄漏）；"
		echo "      ② 用占位符或环境变量引用替换该值后重新提交；"
		echo "      ③ 若已推送远端，评估 git filter-repo 清历史（需负责人审批）。"
		return 1
	fi
	echo "secrets-check 通过（无 sk- 真值、无密钥类变量的非占位赋值）"
}

# ── pydeps：锁文件里**逐条** `pkg==ver`，venv 里必须存在且版本相同 ─────────────
# 与 licensecheck 故意重叠：那边是合规判据（许可），这里是环境判据（环境比锁文件旧时绿也可能是假绿）。
# 检查方向刻意单向：venv 里多出来的传递依赖不报 —— 报出来只会训练人忽略这个检查。
check_pydeps() {
	local py="${ROOT}/analysis/.venv/bin/python"
	local locks=("${ROOT}/analysis/requirements.txt" "${ROOT}/analysis/requirements-dev.txt")
	if [ ! -x "${py}" ]; then
		echo "✗ 缺 Python 环境（analysis/.venv）—— 先跑 make pyenv"
		echo "  禁止跳过本检查 —— 静默跳过等于假绿。"
		return 1
	fi

	# 从**实际安装**的发行版读版本，规范名与 pip 一致：小写 + `_`/`.` → `-`（PEP 503）。
	local installed
	installed="$(
		"${py}" - <<'PYEOF'
import importlib.metadata as md

for dist in md.distributions():
    raw = dist.metadata["Name"]
    if raw:
        print(f"{raw.lower().replace('_', '-').replace('.', '-')}={dist.version}")
PYEOF
	)"

	local fail=0 checked=0 lock raw line name want canon got
	for lock in "${locks[@]}"; do
		if [ ! -f "${lock}" ]; then
			echo "✗ 锁文件不存在：${lock}"
			return 1
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
			elif [ "${got}" != "${want}" ]; then
				echo "✗ ${name}：锁文件 ${want} vs 环境 ${got} 不一致"
				fail=1
			fi
		done <"${lock}"
	done

	if [ "${fail}" -ne 0 ]; then
		echo
		echo "锁文件与环境不一致 —— 跑 make pyenv 重建环境（锁文件是权威，环境向它对齐）。"
		return 1
	fi
	echo "✓ Python 依赖版本一致（${checked} 条 pin：analysis/requirements.txt + requirements-dev.txt）"
}

case "${1:-}" in
fmt) check_fmt ;;
secrets) check_secrets ;;
pydeps) check_pydeps ;;
all)
	rc=0
	check_fmt || rc=1
	check_secrets || rc=1
	check_pydeps || rc=1
	exit "${rc}"
	;;
*)
	sed -n '4,8p' "$0" | sed 's/^# \{0,1\}//'
	exit 2
	;;
esac
