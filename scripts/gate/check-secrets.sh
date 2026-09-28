#!/bin/sh
# 密钥泄漏门禁：拦截「长得像真实密钥的值」进入版本库。
#
# 为什么单独一个脚本而不塞进 check-leak：check-leak 管的是**对外可见面**的禁用串
# （OH-1/OH-2：会不会出现在攻击者屏幕上）；这里管的是**仓库本身**不要携带可用凭证 ——
# 判据不同（有没有“能直接拿去调 API 的值”），豁免逻辑也不同，混在一起会互相污染。
#
# 两条规则（判据是“形态像真值”，不是“提到了 key 这个词”）：
#   ① sk- 前缀 + 20 位以上字母数字（OpenAI / DeepSeek / Anthropic 风格的服务密钥）；
#   ② 已入库文件里 `<NAME>_KEY / <NAME>_TOKEN / <NAME>_SECRET / <NAME>_PASSWORD = <12 位以上非占位值>`
#      （占位符放行：your / example / placeholder / xxxx / changeme / dummy / fake / test /
#       <...> / ${...} / 中文说明等 —— 文档里写占位示例是正当需求）。
#
# 只扫 git 追踪文件（.env 已被 .gitignore 挡在门外；万一被误 add，本检查会拦下）；
# vendor/ 是第三方副本，不适用本仓库的门禁。
#
# 处置顺序（写死在报错信息里，让人不用翻文档）：
#   1. 该密钥视为已泄漏 —— 去服务商控制台**吊销并签发新 key**（删文件不解决已发生的泄漏）；
#   2. 从提交里移除该值（占位符或环境变量引用代替）；
#   3. 若已推送远端：轮换密钥后评估是否需要清历史（git filter-repo，需负责人审批）。
set -eu

# 无 git 仓库（例如源码快照拷贝）时无法界定“已入库文件”，跳过并说明 —— 不给假绿也不误杀。
if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	echo "secrets-check: 非 git 仓库 —— 跳过（本检查只针对入库文件）"
	exit 0
fi

status=0

report_leak() {
	echo "✗ $1"
	status=1
}

disposal() {
	echo
	echo "处置：① 该密钥视为已泄漏，立即在服务商控制台吊销并轮换（删文件不解决已发生的泄漏）；"
	echo "      ② 用占位符或环境变量引用替换该值后重新提交；"
	echo "      ③ 若已推送远端，评估 git filter-repo 清历史（需负责人审批）。"
}

# 规则①：sk- 风格服务密钥。vendor 里第三方代码的 sk- 前缀 SSH 算法名（sk-ecdsa-…）
# 都带连字符后缀，不会满足「20 位纯字母数字」。
sk_hits=$(git grep -nIE 'sk-[A-Za-z0-9]{20,}' -- . ':!vendor' 2>/dev/null || true)
[ -z "${sk_hits}" ] || report_leak "发现疑似真实服务密钥（sk- 前缀）：
${sk_hits}"

# 规则②：密钥类变量被赋了非占位的实值。
# 只匹配「环境变量命名风格」的键（至少两段大写下划线，如 SHEN_AI_KEY / DEEPSEEK_API_KEY），
# 避免误伤普通字符串键；值侧要求 12 位以上（admin 之类弱默认值不在此列，由口令策略管）。
pattern='([A-Z][A-Z0-9]+_(API_)?KEY|[A-Z][A-Z0-9]+_(TOKEN|SECRET|PASSWORD))[[:space:]]*[:=][[:space:]]*["'"'"']?[A-Za-z0-9][A-Za-z0-9_-]{11,}'
tmp=$(mktemp)
trap 'rm -f "${tmp}"' EXIT
git grep -nIE "${pattern}" -- . ':!vendor' >"${tmp}" 2>/dev/null || true

# 占位/示例值放行（文档与模板的正当写法）。**只对等号右侧的值**判断 ——
# 不能对整行 grep -v：命中行自带「文件:行号:」前缀，路径里的 test/fake 字样会把真泄漏洗白
# （红测试实测踩过：fake-leak2.md 里的真实形态被自己的文件名放行）。
assign_leaks=""
while IFS= read -r line; do
	[ -n "${line}" ] || continue
	# 剥掉「文件:行号:」前缀与「变量名=」及引号，只留值。
	value=$(printf '%s\n' "${line}" | sed -E 's/^[^:]+:[0-9]+://; s/^[A-Za-z0-9_]+[[:space:]]*[:=][[:space:]]*//; s/^["'"'"']//')
	if ! printf '%s\n' "${value}" | grep -qiE 'your|example|placeholder|xxxx|changeme|change-?me|dummy|fake|test|sample|\$\{|<[^>]+>|随机|占位|此处|替换'; then
		assign_leaks="${assign_leaks}${line}
"
	fi
done <"${tmp}"
[ -z "${assign_leaks}" ] || report_leak "发现疑似真实密钥赋值（占位符除外）：
${assign_leaks}"

if [ "${status}" -ne 0 ]; then
	disposal
	exit 1
fi

echo "secrets-check 通过（无 sk- 真值、无密钥类变量的非占位赋值）"
