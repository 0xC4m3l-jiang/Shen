// Python 运行期依赖的许可审计（`TB-16` 的第二类依赖来源）。
//
// 为什么需要它：Go 侧的审计（`main.go::buildModules`）用 `go list -deps` 反推模块，
// **看不见** `analysis/requirements.txt` 的运行期依赖 —— 于是「依赖都经过许可审计」
// 这句话只成立一半。而 [ADR-0023](../../docs/background/decisions/0023-deception-content-injection.md)
// 的未解决 1/2（模型后端、PII 检测）正卡在「先过许可证台账」上：审计面不补，Python 依赖一个也进不来。
//
// 四条设计决策（前三条与 Go 侧同口径，见 `main.go` 文件头的两条 + 台账页脚）：
//
//  1. **依赖集合取锁文件，许可声明取已安装环境。** 锁文件是「声明要装什么」，
//     环境是「实际装了什么」。两者不一致时**必须失败** —— 宁可门禁红，
//     不可拿 A 版本的元数据去审定 B 版本的许可。
//  2. **审传递闭包，不只审直接依赖。** Go 侧审的是进二进制的全部模块；Python 侧对应的是
//     从直接依赖可达的整个运行期闭包。只审直接依赖会漏掉运行期真的会被加载的传递依赖
//     （本轮实测就有：`grpcio` → `typing-extensions`）。
//  3. **认不出即失败。** 与 Go 侧一致：白名单式判定，报「需人工判定」并让门禁失败。
//     许可识别错的代价是法律风险，比构建失败严重得多。
//  4. **不做依赖解析与版本约束求解。** 只回答「在不在、许可是什么」。
//     PEP 440 的约束语义（`~=` / `>=` / 环境标记求值）不在这里重造 —— 那需要一个真正的解析器，
//     而 `pip` 已经有了；这里要的是**可离线复核**的许可判定。
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// 锁文件与已安装环境
// ─────────────────────────────────────────────────────────────────────────────

// pyDep 是锁文件里的一条运行期依赖。
type pyDep struct {
	Name    string
	Version string
}

// pyMeta 是一个已安装发行版的元数据（只取本审计用得到的字段）。
type pyMeta struct {
	Name      string
	Version   string
	Expr      string   // License-Expression（PEP 639 的 SPDX 表达式，最可靠）
	Classif   string   // Classifier: License :: OSI Approved :: X 里的 X
	FreeText  string   // License: 原文（最不可靠，仅在前两者都没有时用）
	Requires  []string // Requires-Dist 的运行期需求（已剔除「仅在某 extra 下生效」的）
	DistInfo  string   // 来源目录（写进错误信息，便于人工核）
	HasFields bool     // 元数据里是否出现过任何许可字段
}

// pyName 行内注释：`pkg==1.0  # 说明`。锁文件里允许注释（本项目就写了）。
var pyInlineComment = regexp.MustCompile(`\s+#.*$`)

// pyLockfile 解析 `name==version` 形式的锁文件。
//
// 只认 `==`：范围约束（`>=` / `~=`）不是「锁定」，拿不到确定版本就无法核对环境。
// 认不出但仍非空的行**报错**而不是跳过 —— 静默跳过等于悄悄缩小审计范围。
func pyLockfile(path string) ([]pyDep, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("读不到锁文件：%w", err)
	}
	defer func() { _ = f.Close() }()

	var deps []pyDep
	seen := map[string]bool{}
	lineNo := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(pyInlineComment.ReplaceAllString(sc.Text(), ""))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, version, ok := strings.Cut(line, "==")
		name, version = strings.TrimSpace(name), strings.TrimSpace(version)
		if !ok || name == "" || version == "" {
			return nil, fmt.Errorf(
				"%s:%d 不是 `name==version` 形式的锁定行：%q —— 审计范围不能被静默缩小", path, lineNo, line)
		}
		key := normalizePyName(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		deps = append(deps, pyDep{Name: name, Version: version})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("读锁文件失败：%w", err)
	}
	if len(deps) == 0 {
		return nil, fmt.Errorf("%s 里没有可解析的依赖行 —— 空集合会被误读成「没有依赖需要审」", path)
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].Name < deps[j].Name })
	return deps, nil
}

// normalizePyName 做 PEP 503 规范化（小写 + `-` / `_` / `.` 折叠成 `-`）。
// 发行版名在锁文件里写 `PyYAML`、在 dist-info 目录里写 `pyyaml` —— 不规范化就配不上。
func normalizePyName(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch r {
		case '-', '_', '.':
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		default:
			b.WriteRune(r)
			prevDash = false
		}
	}
	return strings.Trim(b.String(), "-")
}

// pyInstalled 扫虚拟环境里的 `*.dist-info/METADATA`，按规范化发行版名建索引。
//
// 为什么读 METADATA 而不是读目录名：目录名的大小写与写法与发行版名不总一致
// （`pyyaml-6.0.3.dist-info` vs 声明名 `PyYAML`），而 METADATA 里的 `Name:` 是权威声明。
func pyInstalled(venv string) (map[string]pyMeta, error) {
	var metas []string
	for _, lib := range []string{"lib", "lib64"} {
		matches, _ := filepath.Glob(filepath.Join(venv, lib, "python3*", "site-packages", "*.dist-info", "METADATA"))
		metas = append(metas, matches...)
	}
	if len(metas) == 0 {
		return nil, fmt.Errorf(
			"%s 里找不到任何 *.dist-info（site-packages 为空或环境未建）—— 先跑 make pyenv", venv)
	}

	out := map[string]pyMeta{}
	for _, path := range metas {
		meta, err := readPyMeta(path)
		if err != nil {
			return nil, err
		}
		if meta.Name == "" {
			continue
		}
		out[normalizePyName(meta.Name)] = meta
	}
	return out, nil
}

// readPyMeta 解析一个 METADATA 文件（RFC 822 风格：`Key: Value` + 续行以空白开头）。
func readPyMeta(path string) (pyMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return pyMeta{}, fmt.Errorf("读不到元数据：%w", err)
	}
	defer func() { _ = f.Close() }()

	meta := pyMeta{DistInfo: filepath.Dir(path)}
	key, value := "", ""
	flush := func() {
		if key == "" {
			return
		}
		meta.add(key, value)
		key, value = "", ""
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			value += " " + strings.TrimSpace(line) // 续行
			continue
		}
		flush()
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(k), strings.TrimSpace(v)
	}
	flush()
	if err := sc.Err(); err != nil {
		return pyMeta{}, fmt.Errorf("读 %s 失败：%w", path, err)
	}
	if meta.Version == "" {
		return pyMeta{}, fmt.Errorf("%s 缺 Version 字段 —— 无法核对锁文件", path)
	}
	return meta, nil
}

// add 累加一条元数据字段。
func (m *pyMeta) add(key, value string) {
	switch key {
	case "Name":
		m.Name = value
	case "Version":
		m.Version = value
	case "License-Expression":
		m.Expr, m.HasFields = value, true
	case "License":
		m.HasFields = true
		if value != "" && !strings.EqualFold(value, "UNKNOWN") && m.FreeText == "" {
			m.FreeText = value
		}
	case "Classifier":
		const prefix = "License :: OSI Approved :: "
		if rest, ok := strings.CutPrefix(value, prefix); ok {
			m.HasFields = true
			if m.Classif == "" {
				m.Classif = strings.TrimSpace(rest)
			}
		}
	case "Requires-Dist":
		if requiresRun(pyMarkerOf(value)) {
			m.Requires = append(m.Requires, pyRequirementName(value))
		}
	}
}

// pyMarkerOf 取 `Requires-Dist: pkg>=1.0; marker` 里的 marker 部分。
func pyMarkerOf(req string) string {
	_, marker, ok := strings.Cut(req, ";")
	if !ok {
		return ""
	}
	return strings.TrimSpace(marker)
}

// pyRequirementName 取需求里的发行版名（去掉版本约束、环境标记与括号）。
func pyRequirementName(req string) string {
	name := req
	if i := strings.IndexAny(name, ";(<>=!~["); i >= 0 {
		name = name[:i]
	}
	return strings.TrimSpace(name)
}

// pyOptionalExtra 匹配「仅在某个 extra 下才需要」的环境标记。
//
// 本项目不安装任何 extras（`pip install -r requirements.txt`），所以这类需求不参与运行期闭包。
// 只认 `==` 与 `in`：`extra != "x"` / `extra not in "x"` 的含义相反 —— 那些条件在**没有** extra 时成立，
// 因此必须**保留**。正则刻意不匹配它们（`extra` 后面跟 `!` 或 `not` 都不命中）。
var pyOptionalExtra = regexp.MustCompile(`extra\s*(==|in\b)`)

// requiresRun 判断一条 Requires-Dist 是否在「不装 extras」的前提下成立。
func requiresRun(marker string) bool {
	return !pyOptionalExtra.MatchString(marker)
}

// ─────────────────────────────────────────────────────────────────────────────
// SPDX 与自由文本的许可判定
// ─────────────────────────────────────────────────────────────────────────────

// spdxEntry 是一个 SPDX 标识符的判定。
//
// 与 Go 侧的 `signatures` 分开：那边匹配的是**许可正文的子串**，
// 这里匹配的是**声明出来的标识符**（`License-Expression` / Classifier / `License` 字段）。
// 两类输入的可靠度不同，混在一张表里会让「为什么这条允许」变得说不清。
type spdxEntry struct {
	id     string
	effect verdict
	why    string
}

// spdxVerdicts 是**登记过的** SPDX 标识符。没登记的 → 需人工判定（白名单式）。
var spdxVerdicts = map[string]spdxEntry{
	// ── 限制性：拦下（口径与 README 的「禁止的许可」一表一致）──────────────
	"agpl-3.0":          {"AGPL-3.0", restricted, "通过网络提供服务即触发源码披露义务"},
	"agpl-3.0-only":     {"AGPL-3.0-only", restricted, "通过网络提供服务即触发源码披露义务"},
	"agpl-3.0-or-later": {"AGPL-3.0-or-later", restricted, "通过网络提供服务即触发源码披露义务"},
	"sspl-1.0":          {"SSPL-1.0", restricted, "要求公开整个服务栈的源码"},
	"busl-1.1":          {"BUSL-1.1", restricted, "商用受限，若干年后才转开源"},
	"elastic-2.0":       {"Elastic-2.0", restricted, "禁止把本产品作为托管服务提供"},
	"cc-by-nc-4.0":      {"CC-BY-NC-4.0", restricted, "禁止商用"},
	"cc-by-nc-sa-4.0":   {"CC-BY-NC-SA-4.0", restricted, "禁止商用"},
	"json":              {"JSON", restricted, "许可条款含用途限制，不是自由许可"},
	"gpl-2.0":           {"GPL-2.0", restricted, "强传染（动态链接的 Python 包同样有传染风险，从严）"},
	"gpl-2.0-only":      {"GPL-2.0-only", restricted, "强传染"},
	"gpl-2.0-or-later":  {"GPL-2.0-or-later", restricted, "强传染"},
	"gpl-3.0":           {"GPL-3.0", restricted, "强传染"},
	"gpl-3.0-only":      {"GPL-3.0-only", restricted, "强传染"},
	"gpl-3.0-or-later":  {"GPL-3.0-or-later", restricted, "强传染"},
	"lgpl-2.1":          {"LGPL-2.1", restricted, "弱传染；Python 是动态链接，但仍需保留替换能力，从严对待"},
	"lgpl-2.1-only":     {"LGPL-2.1-only", restricted, "弱传染"},
	"lgpl-2.1-or-later": {"LGPL-2.1-or-later", restricted, "弱传染"},
	"lgpl-3.0":          {"LGPL-3.0", restricted, "弱传染"},
	"lgpl-3.0-only":     {"LGPL-3.0-only", restricted, "弱传染"},
	"rpl-1.5":           {"RPL-1.5", restricted, "传染性"},
	"osl-3.0":           {"OSL-3.0", restricted, "传染性"},
	"cpal-1.0":          {"CPAL-1.0", restricted, "传染性 + 署名义务"},

	// ── 宽松：放行（口径与 README 的「放行的宽松许可」一表一致）────────────
	"mit":           {"MIT", ok, "宽松，需保留声明"},
	"apache-2.0":    {"Apache-2.0", ok, "宽松，需保留声明"},
	"bsd-2-clause":  {"BSD-2-Clause", ok, "宽松"},
	"bsd-3-clause":  {"BSD-3-Clause", ok, "宽松，禁止用作者名背书"},
	"isc":           {"ISC", ok, "宽松，等价 MIT"},
	"mpl-2.0":       {"MPL-2.0", ok, "文件级弱传染，只影响其自身文件"},
	"unlicense":     {"Unlicense", ok, "等同公共领域"},
	"0bsd":          {"0BSD", ok, "宽松"},
	"zlib":          {"Zlib", ok, "宽松"},
	"psf-2.0":       {"PSF-2.0", ok, "宽松"},
	"cc0-1.0":       {"CC0-1.0", ok, "放弃著作权"},
	"blueoak-1.0.0": {"BlueOak-1.0.0", ok, "宽松"},
	"wtfpl":         {"WTFPL", ok, "无限制"},
	"x11":           {"X11", ok, "宽松"},
	"python-2.0":    {"Python-2.0", ok, "宽松（PSF 的旧标识）"},
	"bsl-1.0":       {"BSL-1.0", ok, "Boost 软件许可，宽松（注意与 BUSL-1.1 不同）"},
	"postgresql":    {"PostgreSQL", ok, "宽松"},
	"hpnd":          {"HPND", ok, "宽松，历史 MIT 变体"},
}

// spdxAliases 把**声明里常见的写法**规范化到 `spdxVerdicts` 的键。
//
// 只收**无歧义**的别名：`BSD License` 这种说不清 2 还是 3 条的一律不收 —— 猜一次就是把
// 「认不出」变成「认得出但认错」，而后者的代价是法律风险（`main.go` 文件头设计决策 2）。
var spdxAliases = map[string]string{
	"mit-license":                        "mit",
	"mit-licence":                        "mit",
	"the-mit-license":                    "mit",
	"apache-2":                           "apache-2.0",
	"apache-2.0-license":                 "apache-2.0",
	"apache-software-license":            "apache-2.0",
	"apache-license-2.0":                 "apache-2.0",
	"bsd-2":                              "bsd-2-clause",
	"bsd-2-clause-license":               "bsd-2-clause",
	"bsd-3":                              "bsd-3-clause",
	"3-clause-bsd":                       "bsd-3-clause",
	"3-clause-bsd-license":               "bsd-3-clause",
	"bsd-3-clause-license":               "bsd-3-clause",
	"new-bsd-license":                    "bsd-3-clause",
	"mozilla-public-license-2.0":         "mpl-2.0",
	"mpl-2":                              "mpl-2.0",
	"the-unlicense":                      "unlicense",
	"public-domain":                      "unlicense",
	"python-software-foundation-license": "psf-2.0",
	"psf":                                "psf-2.0",
	"isc-license":                        "isc",
	"zlib-license":                       "zlib",
}

// spdxSplit 切分 SPDX 表达式；`WITH` 的**右侧是例外条款**（放宽），故只看左侧。
var spdxSplit = regexp.MustCompile(`\s+(?:AND|OR|WITH)\s+`)

// spdxVerdict 判定一个许可声明（SPDX 表达式或自由文本写法），取**最严格**的一条。
//
// 取最严格与 Go 侧同规则（双许可 `Apache-2.0 + GPL` 必须按 GPL 判）：
// 用得上的一定是较严的那份。`WITH` 例外条款会让许可**更**宽松，所以只看左侧是保守方向。
func spdxVerdict(declared string) (string, verdict, string) {
	tokens := spdxSplit.Split(declared, -1)
	var worstID, worstWhy string
	worst := ok
	found := false
	for _, raw := range tokens {
		token := strings.Trim(strings.TrimSpace(raw), "()")
		if token == "" {
			continue
		}
		entry, okEntry := spdxLookup(token)
		if !okEntry {
			if !found || unknown > worst {
				worstID, worstWhy, worst = "未识别", fmt.Sprintf(
					"许可标识符 %q 未登记 —— 需人工判定后加入 spdxVerdicts", token), unknown
			}
			found = true
			continue
		}
		if !found || entry.effect > worst {
			worstID, worstWhy, worst = entry.id, entry.why, entry.effect
		}
		found = true
	}
	if !found {
		return "", unknown, "许可声明为空"
	}
	return worstID, worst, worstWhy
}

// spdxLookup 查表：先按标识符，再按别名。
func spdxLookup(token string) (spdxEntry, bool) {
	key := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(token)), " ", "-")
	if e, ok := spdxVerdicts[key]; ok {
		return e, true
	}
	if canon, ok := spdxAliases[key]; ok {
		return spdxVerdicts[canon], true
	}
	return spdxEntry{}, false
}

// declaredLicense 取一个发行版声明出来的许可，并说明取自哪个字段。
//
// 优先级 = 可靠性：`License-Expression`（PEP 639 的机器可读 SPDX）>
// `Classifier: License ::`（PyPI 的受控词表）> `License:`（自由文本，什么都能写）。
func declaredLicense(m pyMeta) (string, string) {
	switch {
	case m.Expr != "":
		return m.Expr, "License-Expression"
	case m.Classif != "":
		return m.Classif, "Classifier"
	case m.FreeText != "":
		return m.FreeText, "License"
	default:
		return "", ""
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 闭包与行组装
// ─────────────────────────────────────────────────────────────────────────────

// pyRows 枚举「读锁文件 → 走运行期闭包 → 判定许可」的全部结果。
//
// 任一环节不成立都产出 `unknown` 行（门禁会因此失败），**不**静默跳过：
// 审计范围被悄悄缩小的门禁比没有门禁更危险。
func pyRows(lockPath, venv string) ([]row, error) {
	locked, err := pyLockfile(lockPath)
	if err != nil {
		return nil, err
	}
	installed, err := pyInstalled(venv)
	if err != nil {
		return nil, err
	}
	lockedVersion := map[string]string{}
	for _, d := range locked {
		lockedVersion[normalizePyName(d.Name)] = d.Version
	}

	// 广度优先走闭包：直接依赖的版本要核，传递依赖的版本只作展示。
	queue := make([]string, 0, len(locked))
	for _, d := range locked {
		queue = append(queue, normalizePyName(d.Name))
	}
	seen := map[string]bool{}
	var rows []row
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true

		meta, ok := installed[name]
		if !ok {
			rows = append(rows, row{
				Module: name, Version: lockedVersion[name], Lang: langPython,
				License: "未知", Verdict: unknown,
				Note: "环境里没有这个发行版 —— 先跑 make pyenv（审计的是实际运行的东西，不是锁文件上的字）",
			})
			continue
		}
		if want, isDirect := lockedVersion[name]; isDirect && want != meta.Version {
			rows = append(rows, row{
				Module: meta.Name, Version: meta.Version, Lang: langPython,
				License: "未审", Verdict: unknown,
				Note: fmt.Sprintf(
					"锁文件 %s vs 环境 %s 不一致 —— 先跑 make pyenv（禁止用 A 版本的元数据审定 B 版本的许可）",
					want, meta.Version),
			})
			continue
		}

		declared, source := declaredLicense(meta)
		id, v, why := spdxVerdict(declared)
		if declared == "" {
			why = "元数据里没有任何许可字段（License-Expression / Classifier / License）—— 需人工判定"
		} else {
			why = fmt.Sprintf("%s（取自 %s）", why, source)
		}
		if id == "" {
			id = "未识别"
		}
		r := row{Module: meta.Name, Version: meta.Version, Lang: langPython, License: id, Verdict: v, Note: why}
		if meta.Version == "" {
			r.Verdict = unknown
		}
		rows = append(rows, r)

		for _, req := range meta.Requires {
			dep := normalizePyName(req)
			if dep != "" && !seen[dep] {
				queue = append(queue, dep)
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Module < rows[j].Module })
	return rows, nil
}
