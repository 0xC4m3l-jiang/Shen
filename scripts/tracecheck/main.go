// Command tracecheck 检查「需求 → 技术设计 → 代码 → 测试 → 验证证据」这条链有没有断。
//
// 它回答一个问题：**这次改动能不能顺着一根线追回去** ——
// 规则 ID 引用的规则真实存在、有代码的模块都有模块文档与单测、
// 每轮的变更包与 Log 条目存在且指向的文件真实存在。
//
// 依据：
//
//	modules.md  MD-2（一模块一份文档，固定九章）· MD-17（实现前建文档）· MD-22（独立可测）
//	structure.md §1.4 模块三文件约定（iface.go + 实现 + 单测）
//	README.md   D-3（规则有唯一 ID）· D-6 / D-8（ID 定义与引用形式、废弃者保留）
//	skills/dev-loop SKILL.md  DEV-1 / DEV-2（变更包与 Log —— 流程约定，不是 design 规则）
//
// 与 archcheck 同规矩：清单从文档解析、不硬编码；解析结果不合预期直接报错退出，不静默放行。
// 已知缺口写在 allow.txt，显式登记并打印 —— 过期豁免同样算问题。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type module struct {
	Name   string
	SrcDir string
	Doc    string
	Langs  string
	Stage  string
}

type finding struct {
	ID    string
	Human string
	Where string
	// Subject 是「出问题的那个东西」（如悬空的规则 ID / 路径）；豁免按它精确匹配。
	Subject string
	// Note 为真时是**提示**而非失败：它指向尚未升格为 design 规则的约定
	// （如 structure.md §1.4 的三文件约定）。提示照样打印，但不阻断门禁。
	Note bool
}

func (f finding) String() string {
	if f.Where == "" {
		return fmt.Sprintf("%s  %s", f.ID, f.Human)
	}
	return fmt.Sprintf("%s  %s\n        %s", f.ID, f.Human, f.Where)
}

// allowEntry 是 allow.txt 的一行：已知缺口，显式登记。
type allowEntry struct {
	Check  string
	Target string
	Reason string
}

func main() {
	dump := flag.Bool("dump", false, "只打印解析结果，不做检查")
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		fatal("找不到仓库根（go list -m 失败）：%v", err)
	}

	prefixes, err := parsePrefixes(filepath.Join(root, "docs/design/README.md"))
	if err != nil {
		fatal("解析规则前缀失败：%v", err)
	}
	mods, err := parseModules(filepath.Join(root, "docs/design/modules.md"))
	if err != nil {
		fatal("解析 modules.md §1.1 的模块清单失败：%v", err)
	}
	defined, err := collectDefined(filepath.Join(root, "docs/design"))
	if err != nil {
		fatal("收集 design/ 的规则定义失败：%v", err)
	}
	registered, err := collectRegistered(filepath.Join(root, "docs/design/README.md"))
	if err != nil {
		fatal("收集已废弃规则登记失败：%v", err)
	}
	allow, err := parseAllow(filepath.Join(root, "scripts/tracecheck/allow.txt"))
	if err != nil {
		fatal("读 allow.txt 失败：%v", err)
	}

	if *dump {
		fmt.Printf("规则前缀（design/README.md）：%s\n", strings.Join(prefixes, " "))
		fmt.Printf("design/ 里的规则定义：%d 条 · 已废弃登记：%d 条\n", len(defined), len(registered))
		fmt.Printf("模块清单（modules.md §1.1，%d 行），已实现的可追模块：\n", len(mods))
		for _, m := range mods {
			if !dirExists(filepath.Join(root, m.SrcDir)) {
				continue
			}
			fmt.Printf("  %-16s %-28s %s\n", m.Name, m.SrcDir, m.Doc)
		}
		return
	}

	var findings []finding
	findings = append(findings, checkModuleChain(root, mods)...)
	findings = append(findings, checkRuleRefs(root, prefixes, defined, registered)...)
	findings = append(findings, checkChangePackage(root)...)
	findings = append(findings, checkSkills(root)...)
	findings = append(findings, checkStaleMarkers(root)...)
	findings = append(findings, checkDanglingLinks(root)...)

	report(root, findings, allow)
}

// ─────────────────────────────────────────────────────────────────────────────
// 检查 1 · MD-2 / MD-17 / MD-22 / structure.md §1.4：模块 ↔ 文档 ↔ 代码 ↔ 测试
// ─────────────────────────────────────────────────────────────────────────────

func checkModuleChain(root string, mods []module) []finding {
	var out []finding
	listedDocs := map[string]module{}

	for _, m := range mods {
		listedDocs[filepath.Clean(m.Doc)] = m
		if !dirExists(filepath.Join(root, m.SrcDir)) {
			continue // 还没实现的模块（阶段 2/3）不要求文档与测试
		}

		docPath := filepath.Join(root, m.Doc)
		if !fileExists(docPath) {
			out = append(out, finding{
				ID:    "MD-17",
				Human: "模块目录已存在，但模块文档不存在（文档必须先于实现）",
				Where: m.Name + " → " + m.Doc,
			})
			continue
		}
		out = append(out, checkDocSections(docPath, m)...)
		out = append(out, checkModuleFiles(root, m)...)
	}

	// 反向：docs/modules/ 里不许有清单外的孤儿文档
	dir := filepath.Join(root, "docs/modules")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return append(out, finding{ID: "MD-2", Human: "读 docs/modules/ 失败", Where: err.Error()})
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, "_") || name == "README.md" {
			continue
		}
		rel := filepath.Clean(filepath.Join("docs/modules", name))
		if _, ok := listedDocs[rel]; !ok {
			out = append(out, finding{
				ID:    "MD-2",
				Human: "模块文档不在 modules.md §1.1 的清单里（要么补清单一行，要么删该文档）",
				Where: rel,
			})
		}
	}
	return out
}

// checkDocSections 校验固定九章（MD-2：一节都不许删）。
func checkDocSections(path string, m module) []finding {
	b, err := os.ReadFile(path)
	if err != nil {
		return []finding{{ID: "MD-2", Human: "读模块文档失败", Where: path + ": " + err.Error()}}
	}
	body := string(b)
	var out []finding
	for i := 1; i <= 9; i++ {
		if !strings.Contains(body, fmt.Sprintf("\n## %d.", i)) {
			out = append(out, finding{
				ID:    "MD-2",
				Human: fmt.Sprintf("模块文档缺第 %d 章（模板九章一节都不许删；不适用也要写「不适用」并说明原因）", i),
				Where: m.Doc,
			})
		}
	}
	if !hasRuleRef(body) {
		out = append(out, finding{
			ID:    "MD-2",
			Human: "模块文档没有引用任何规则 ID —— 追不回设计依据（§4 关键规则 至少要有一条）",
			Where: m.Doc,
		})
	}
	return out
}

// checkModuleFiles 校验 Go 模块的三文件约定与单测存在（MD-22 / structure.md §1.4）。
func checkModuleFiles(root string, m module) []finding {
	if !strings.Contains(m.Langs, "Go") {
		return nil // 纯配置模块（如 ② DNS 引流）没有源码
	}
	src := filepath.Join(root, m.SrcDir)
	files, err := goFiles(src)
	if err != nil || len(files) == 0 {
		return nil // 目录里还没有 Go 代码，交给 archcheck 的清单检查
	}

	var out []finding
	hasTest, hasIface := false, false
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			hasTest = true
		}
		if f == "iface.go" {
			hasIface = true
		}
	}
	if !hasTest {
		out = append(out, finding{
			ID:    "MD-22",
			Human: "模块只有实现、没有单测文件（*_test.go）—— 独立可运行的测试套件是模块的定义之一",
			Where: m.SrcDir,
		})
	}
	if !hasIface {
		out = append(out, finding{
			ID:    "TC-1",
			Human: "模块缺 iface.go（structure.md §1.4 的三文件约定：导出的接口单独成文件）—— 该约定尚未升格为规则，故只提示不拦截",
			Where: m.SrcDir,
			Note:  true,
		})
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 检查 2 · D-3 / D-6 / D-8：规则 ID 引用必须真实存在
// ─────────────────────────────────────────────────────────────────────────────

// parsePrefixes 从 design/README.md 的规则总数行解析前缀清单（如 AR / INT / ST …）。
// parsePrefixes 从设计基线索引里解析**规则前缀**清单。
//
// 清单**从文档解析**、不硬编码：文档里记着「规则总数 N 条：`AR` a · `INT` b …」，
// 本函数就认这一行。解析结果明显偏少时**直接失败**，不静默放行（否则门禁会假装通过）。
func parsePrefixes(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var line string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, "规则总数") {
			line = l
			break
		}
	}
	if line == "" {
		return nil, errors.New("找不到包含「规则总数」的那一行 —— 文档格式可能变了")
	}
	var out []string
	for _, m := range backtickPrefix.FindAllStringSubmatch(line, -1) {
		if p := m[1]; !contains(out, p) {
			out = append(out, p)
		}
	}
	if len(out) < 8 {
		return nil, fmt.Errorf("只解析到 %d 个规则前缀，明显偏少 —— 拒绝静默放行", len(out))
	}
	return out, nil
}

// collectDefined 收集 design/ 里被**加粗**定义的规则 ID（D-8：定义用加粗形式）。
func collectDefined(dir string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range boldID.FindAllStringSubmatch(string(b), -1) {
			out[m[1]] = filepath.Base(path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out) < 100 {
		return nil, fmt.Errorf("只收集到 %d 条规则定义，明显偏少 —— 拒绝静默放行", len(out))
	}
	return out, nil
}

// collectRegistered 收集 design/README.md 里以行内代码登记过的 ID —— 含已废弃与旧编号（§4.1 / §4.1.1）。
func collectRegistered(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, m := range backtickID.FindAllStringSubmatch(string(b), -1) {
		out[m[1]] = true
	}
	return out, nil
}

func checkRuleRefs(root string, prefixes []string, defined map[string]string, registered map[string]bool) []finding {
	re := idPattern(prefixes)
	var out []finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") && filepath.Base(path) != "AGENTS.md" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(b), "\n") {
			for _, m := range re.FindAllString(line, -1) {
				if _, ok := defined[m]; ok {
					continue
				}
				if registered[m] {
					continue
				}
				out = append(out, finding{
					ID:    "D-3",
					Human: fmt.Sprintf("引用了不存在的规则 ID %s（未在 design/ 定义，也未在 design/README.md §4 登记为已废弃）", m),
					Where: fmt.Sprintf("%s:%d", rel, i+1),
				})
			}
		}
		return nil
	})
	if err != nil {
		return append(out, finding{ID: "D-3", Human: "遍历失败", Where: err.Error()})
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 检查 3 · DEV-1 / DEV-2：变更包与变更日志（流程约定，见 .pi/skills/dev-loop/SKILL.md）
// ─────────────────────────────────────────────────────────────────────────────

var logEntryMarkers = []string{"**做了什么**", "**改了哪些文件**", "**验证**", "**证据**"}

func checkChangePackage(root string) []finding {
	var out []finding

	tpl := filepath.Join(root, "docs/plans/_change-package.md")
	if !fileExists(tpl) {
		out = append(out, finding{
			ID:    "DEV-1",
			Human: "变更包模板不存在 —— 每轮开发必须按固定形状产出（需求 / 设计逻辑 / 追溯矩阵 / 代码 / 测试 / 证据）",
			Where: "docs/plans/_change-package.md",
		})
	}

	logPath := filepath.Join(root, "docs/log.md")
	b, err := os.ReadFile(logPath)
	if err != nil {
		return append(out, finding{ID: "DEV-2", Human: "变更日志不存在", Where: "docs/log.md"})
	}
	entry := newestEntry(string(b))
	if entry == "" {
		return append(out, finding{
			ID:    "DEV-2",
			Human: "变更日志里没有条目（每条以 `## <日期> · <主题>` 开头）",
			Where: "docs/log.md",
		})
	}
	for _, marker := range logEntryMarkers {
		if !strings.Contains(entry, marker) {
			out = append(out, finding{
				ID:    "DEV-2",
				Human: fmt.Sprintf("最新条目缺 %s 段 —— 缺了就无法核「做了什么 / 对应谁 / 验证过没有」", marker),
				Where: "docs/log.md 最新条目",
			})
		}
	}
	if !strings.Contains(entry, "make gate") {
		out = append(out, finding{
			ID:    "DEV-2",
			Human: "最新条目没有提到 make gate —— 未过门禁的改动不得记为完成",
			Where: "docs/log.md 最新条目",
		})
	}
	for _, p := range backtickPaths(entry) {
		if !fileExists(filepath.Join(root, p)) && !dirExists(filepath.Join(root, p)) {
			out = append(out, finding{
				ID:    "DEV-2",
				Human: "最新条目引用的路径不存在（日志与代码已经漂移）",
				Where: p,
			})
		}
	}
	return out
}

// newestEntry 取第一个 `## ` 标题到下一个 `## ` 之间的内容。
func newestEntry(md string) string {
	lines := strings.Split(md, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "## ") {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	for j := start + 1; j < len(lines); j++ {
		if strings.HasPrefix(lines[j], "## ") {
			return strings.Join(lines[start:j], "\n")
		}
	}
	return strings.Join(lines[start:], "\n")
}

// backtickPaths 取行内代码里像路径的片段（含 /，不含占位符与网址）。
func backtickPaths(s string) []string {
	var out []string
	for _, m := range backtick.FindAllStringSubmatch(s, -1) {
		p := strings.TrimSpace(strings.SplitN(m[1], "#", 2)[0])
		if !strings.Contains(p, "/") || strings.ContainsAny(p, "<>* ") || strings.Contains(p, ":://") {
			continue
		}
		if strings.Contains(p, "...") {
			continue // 省略号是占位，不是路径
		}
		p = strings.TrimSuffix(p, "/")
		// 目录列表里常写 `~/`、`./`、`../`、`kb/` 这类前缀或裸目录名 —— 它们不是具体路径，不核。
		if !strings.Contains(p, "/") {
			continue
		}
		switch p {
		case "~", ".", "..", "":
			continue
		}
		out = append(out, p)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// allow.txt · 已知缺口显式登记
// ─────────────────────────────────────────────────────────────────────────────

func parseAllow(path string) ([]allowEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // 没有豁免文件 = 没有豁免
		}
		return nil, err
	}
	var out []allowEntry
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		body, reason, _ := strings.Cut(l, "#")
		fields := strings.Fields(body)
		if len(fields) < 2 {
			return nil, fmt.Errorf("allow.txt 这行缺目标：%q", l)
		}
		out = append(out, allowEntry{Check: fields[0], Target: fields[1], Reason: strings.TrimSpace(reason)})
	}
	return out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 解析与工具
// ─────────────────────────────────────────────────────────────────────────────

var (
	backtickID     = regexp.MustCompile("`([A-Z]{1,3}-[0-9]+)`")
	backtickPrefix = regexp.MustCompile("`([A-Z]{1,4})`")
	boldID         = regexp.MustCompile(`\*\*([A-Z]{1,3}-[0-9]+)\*\*`)
	backtick       = regexp.MustCompile("`([^`]+)`")
	moduleCells    = 9
)

func idPattern(prefixes []string) *regexp.Regexp {
	return regexp.MustCompile(`\b(?:` + strings.Join(prefixes, "|") + `)-[0-9]+\b`)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func hasRuleRef(s string) bool {
	return regexp.MustCompile(`\b(?:AR|INT|ST|MD|NI|TB|TM|SB|OH|BA|D)-[0-9]+\b`).MatchString(s)
}

// parseModules 解析 modules.md §1.1 的模块表：`| # | 模块 | 层 | 语言 | 源码目录 | 模块文档 | 职责 | 阶段 |`
func parseModules(path string) ([]module, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sec := section(string(b), "### 1.1 源码模块", "### 1.2")
	if sec == "" {
		return nil, errors.New("找不到 §1.1 源码模块 小节")
	}
	var out []module
	for _, line := range strings.Split(sec, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < moduleCells {
			continue
		}
		src := strings.Trim(strings.TrimSpace(cells[5]), "`")
		if !strings.Contains(src, "/") {
			continue // 表头或分隔行
		}
		name := strings.Trim(strings.TrimSpace(cells[2]), "`")
		if strings.Contains(name, "~~") || strings.Contains(src, "~~") {
			continue // 已废弃 / 已合并的行（D-6：保留编号但不再是有效模块）
		}
		out = append(out, module{
			Name:   name,
			Langs:  strings.TrimSpace(cells[4]),
			SrcDir: strings.Trim(src, "~"),
			Doc:    strings.Trim(strings.TrimSpace(cells[6]), "`"),
			Stage:  strings.Trim(strings.TrimSpace(cells[8]), "*"),
		})
	}
	if len(out) < 15 {
		return nil, fmt.Errorf("只解析到 %d 个模块，明显偏少 —— 拒绝静默放行", len(out))
	}
	return out, nil
}

func section(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	s = s[i:]
	if j := strings.Index(s, to); j > 0 {
		s = s[:j]
	}
	return s
}

func goFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

func skipDir(name string) bool {
	switch name {
	case ".git", ".bin", ".pi", "node_modules", "vendor", "testdata":
		return true
	}
	return strings.HasPrefix(name, ".")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// repoRoot 用 go list -m 取模块根，与 archcheck 同法（不依赖 cwd）。
func repoRoot() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", errors.New("go list -m 返回空目录")
	}
	return dir, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 输出
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// 检查 4 · DEV-3：AGENTS.md 点名的技能必须真实存在（技能是强制的，不能只写在文档里）
// ─────────────────────────────────────────────────────────────────────────────

// skillRef 从 AGENTS.md 里抓技能文件路径（项目内 `.pi/skills/...` 与全局 `~/.pi/agent/skills/...`）。
var skillRef = regexp.MustCompile(`((?:~/\.pi/agent|\.pi)/skills/[a-z0-9-]+/SKILL\.md)`)

func checkSkills(root string) []finding {
	const agents = "AGENTS.md"
	b, err := os.ReadFile(filepath.Join(root, agents))
	if err != nil {
		return []finding{{ID: "DEV-3", Human: "读不到 AGENTS.md，无法核对它点名的技能", Where: agents}}
	}
	home, _ := os.UserHomeDir()
	seen := map[string]bool{}
	var out []finding
	for _, m := range skillRef.FindAllStringSubmatch(string(b), -1) {
		rel := m[1]
		if seen[rel] {
			continue
		}
		seen[rel] = true
		path := filepath.Join(root, rel)
		if strings.HasPrefix(rel, "~/") {
			path = filepath.Join(home, strings.TrimPrefix(rel, "~/"))
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			out = append(out, finding{ID: "DEV-3", Human: "AGENTS.md 点名的技能文件不存在", Where: rel, Subject: rel})
			continue
		}
		text := string(body)
		if !strings.Contains(text, "name:") || !strings.Contains(text, "description:") {
			out = append(out, finding{ID: "DEV-3", Human: "技能文件缺 name / description 头部（Agent 靠它决定何时加载）", Where: rel, Subject: rel})
		}
	}
	if len(seen) == 0 {
		out = append(out, finding{ID: "DEV-3", Human: "AGENTS.md 没有点名任何技能文件 —— 技能是强制的，至少要有全局与项目两个 dev-loop"})
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 检查 5 · TC-2 / TC-3：过期状态标记 与 悬空链接（技能 audit §6 里机器能核的两类）
// ─────────────────────────────────────────────────────────────────────────────

// liveDocs 返回**活文档**（审视范围内的文档）。
//
// 排除历史记录类文件：docs/plans/ 与 docs/background/ 是当时的快照、docs/log.md 是日志、
// docs/kb/ 是踩坑沉淀 —— 它们写下过时内容是正确的，不该被审视。
func liveDocs(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if skipDir(d.Name()) || rel == "docs/plans" || rel == "docs/background" || rel == "docs/kb" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || rel == "docs/log.md" {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return out
}

// statusWords 是「未完成」的状态词：它们与一个**已存在**的路径写在同一行，就是过期标记。
var statusWords = []string{"待建", "待创建", "待补", "尚未创建"}

func checkStaleMarkers(root string) []finding {
	var out []finding
	for _, rel := range liveDocs(root) {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			hit := ""
			for _, w := range statusWords {
				if strings.Contains(line, w) {
					hit = w
					break
				}
			}
			if hit == "" {
				continue
			}
			for _, p := range backtickPaths(line) {
				if pathExists(root, p) {
					out = append(out, finding{
						ID:      "TC-2",
						Human:   fmt.Sprintf("同一行既写「%s」又点名一个**已存在**的路径 —— 状态标记过期", hit),
						Where:   fmt.Sprintf("%s:%d", rel, i+1),
						Subject: rel,
					})
					break
				}
			}
		}
	}
	return out
}

// mdLink 抓 markdown 链接目标。
var mdLink = regexp.MustCompile(`\]\(([^)]+)\)`)

func checkDanglingLinks(root string) []finding {
	home, _ := os.UserHomeDir()
	var out []finding
	for _, rel := range liveDocs(root) {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			for _, m := range mdLink.FindAllStringSubmatch(line, -1) {
				target := strings.TrimSpace(strings.SplitN(m[1], "#", 2)[0])
				if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
					continue
				}
				var p string
				switch {
				case strings.HasPrefix(target, "~/"):
					p = filepath.Join(home, strings.TrimPrefix(target, "~/"))
				case strings.HasPrefix(target, "/"):
					p = target
				default:
					p = filepath.Join(root, filepath.Dir(rel), target)
				}
				if _, serr := os.Stat(filepath.Clean(p)); serr != nil {
					out = append(out, finding{
						ID:      "TC-3",
						Human:   fmt.Sprintf("悬空链接：目标 %q 不存在", target),
						Where:   fmt.Sprintf("%s:%d", rel, i+1),
						Subject: rel + " -> " + target,
					})
				}
			}
		}
	}
	return out
}

// pathExists 解析文档里写的路径：支持仓库相对路径、绝对路径、与 ~/ 开头的家目录路径。
// 容忍 `路径:行号` 与 `路径:起-止` 这种行号写法（常见的引用形式）。
func pathExists(root, p string) bool {
	if i := strings.LastIndex(p, ":"); i > 0 {
		if isLineRef(p[i+1:]) {
			p = p[:i]
		}
	}
	switch {
	case strings.HasPrefix(p, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
	case filepath.IsAbs(p):
		// 原样使用
	default:
		p = filepath.Join(root, p)
	}
	_, err := os.Stat(p)
	return err == nil
}

// isLineRef 判断 `12` / `12-34` 这类行号写法。
func isLineRef(s string) bool {
	if s == "" {
		return false
	}
	for _, part := range strings.SplitN(s, "-", 2) {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func report(root string, findings []finding, allow []allowEntry) {
	var kept, waived []finding
	used := map[int]bool{}
	for _, f := range findings {
		matched := false
		for i, a := range allow {
			if a.Check != f.ID {
				continue
			}
			if a.Target == "*" || strings.Contains(f.Where, a.Target) || strings.Contains(f.Human, a.Target) {
				used[i] = true
				waived = append(waived, f)
				matched = true
				break
			}
		}
		if !matched {
			kept = append(kept, f)
		}
	}

	// 提示（Note）：尚未升格为规则的约定 —— 照常打印，但不阻断门禁。
	var notes []finding
	keptNoNotes := kept[:0:0]
	for _, f := range kept {
		if f.Note {
			notes = append(notes, f)
			continue
		}
		keptNoNotes = append(keptNoNotes, f)
	}
	kept = keptNoNotes
	if len(notes) > 0 {
		fmt.Printf("提示 %d 条（尚未升格为规则的约定，不阻断门禁）：\n", len(notes))
		for _, f := range notes {
			fmt.Printf("  ℹ %s  %s\n        %s\n", f.ID, f.Human, f.Where)
		}
		fmt.Println()
	}

	if len(waived) > 0 {
		fmt.Printf("已登记豁免 %d 条（见 scripts/tracecheck/allow.txt）：\n", len(waived))
		for _, f := range waived {
			fmt.Printf("  · %s  %s\n", f.ID, f.Where)
		}
		fmt.Println()
	}

	// 过期豁免：登记了但这次没命中 —— 豁免会腐烂，必须删。
	stale := 0
	for i, a := range allow {
		if !used[i] {
			stale++
			kept = append(kept, finding{
				ID:    "ALLOW",
				Human: fmt.Sprintf("豁免已过期（检查通过、但 allow.txt 还留着）：%s %s —— 删掉这行", a.Check, a.Target),
				Where: a.Reason,
			})
		}
	}

	if len(kept) == 0 {
		fmt.Println("追溯检查通过。")
		fmt.Println("  模块文档↔代码↔单测 · 规则 ID 引用存在性 · 变更包与变更日志 · AGENTS.md 点名的技能 · 过期状态标记 · 悬空链接")
		return
	}

	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Where < kept[j].Where })
	fmt.Fprintf(os.Stderr, "追溯检查发现 %d 个问题：\n\n", len(kept))
	for _, f := range kept {
		fmt.Fprintf(os.Stderr, "  ✗ %s\n\n", f)
	}
	fmt.Fprintln(os.Stderr, "顺着一根线追不回去的改动，等于没做完：")
	fmt.Fprintln(os.Stderr, "  MD-2 / MD-17 / MD-22 出自 docs/design/modules.md；D-3 / D-8 出自 docs/design/README.md；")
	fmt.Fprintln(os.Stderr, "  DEV-1 / DEV-2 出自 .pi/skills/dev-loop/SKILL.md。修代码或先改文档并取得确认，不要绕过检查。")
	_ = root
	_ = stale
	os.Exit(1)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "tracecheck: "+format+"\n", args...)
	fmt.Fprintln(os.Stderr, "检查未执行完毕 —— 视为失败，不静默放行。")
	os.Exit(1)
}
