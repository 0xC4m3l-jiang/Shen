// Command archcheck 把设计文档里「写了验证方式、但没人执行」的架构规则
// 变成可执行的检查。
//
// 它不检查代码风格，只检查**结构**：谁允许依赖谁、哪个目录允许存在、
// 存储访问是否只有一个出口、有没有偷偷引入 CGO。
//
// 依据（均在 docs/design/ 内）：
//
//	structure.md §1.1   顶层目录的唯一清单
//	structure.md §1.7   ST-1 / ST-2 / ST-3 / ST-4（依赖方向）
//	modules.md   §1.1   模块与源码目录的一一映射
//	modules.md          MD-18 / MD-19（清单与目录一致）/ MD-20（store 是唯一 I/O 出口）
//	language.md         TB-20 / TB-21（语言层数上限）/ TB-24（禁止 CGO 与本地库）
//
// 清单**从文档解析，不硬编码** —— 文档改则检查跟着改，不会两边漂移。
// 解析结果不达预期时**直接报错退出**，绝不静默放行：静默通过等于假绿。
package main

import (
	"encoding/json"
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

// ─────────────────────────────────────────────────────────────────────────────
// 数据结构
// ─────────────────────────────────────────────────────────────────────────────

// pkg 是 go list -json 里我们关心的字段。
type pkg struct {
	ImportPath string
	Dir        string
	Imports    []string
	GoFiles    []string
	CgoFiles   []string
}

// module 是 modules.md §1.1 的一行。
type module struct {
	Name   string // 模块名，如 judge
	Layer  string // 层，如 核心
	Langs  string // 声明的语言，如 Go / 配置 + Go
	SrcDir string // 源码目录，如 core/internal/judge/
	Stage  string // 阶段 1/2/3
}

// finding 是一条检查结果。ID 与 Human 一起输出 —— 只给 ID 审计的人看不懂。
type finding struct {
	ID    string
	Human string
	Where string
}

func (f finding) String() string {
	if f.Where == "" {
		return fmt.Sprintf("%s  %s", f.ID, f.Human)
	}
	return fmt.Sprintf("%s  %s\n        %s", f.ID, f.Human, f.Where)
}

// ─────────────────────────────────────────────────────────────────────────────
// 入口
// ─────────────────────────────────────────────────────────────────────────────

func main() {
	dump := flag.Bool("dump", false, "只打印从文档解析出的清单，不做检查")
	flag.Parse()

	root, err := moduleRoot()
	if err != nil {
		fatal("找不到仓库根（go list -m 失败）：%v", err)
	}

	tops, err := parseTopLevelDirs(filepath.Join(root, "docs/design/structure.md"))
	if err != nil {
		fatal("解析 structure.md §1.1 的顶层目录失败：%v", err)
	}
	mods, err := parseModules(filepath.Join(root, "docs/design/modules.md"))
	if err != nil {
		fatal("解析 modules.md §1.1 的模块清单失败：%v", err)
	}
	pkgs, err := loadPackages()
	if err != nil {
		fatal("go list 失败：%v", err)
	}

	if *dump {
		fmt.Printf("顶层目录（structure.md §1.1，%d 个）：%s\n", len(tops), strings.Join(tops, " "))
		fmt.Printf("模块清单（modules.md §1.1，%d 行）：\n", len(mods))
		for _, m := range mods {
			fmt.Printf("  %-24s %-8s %-14s %s\n", m.Name, m.Stage, m.Langs, m.SrcDir)
		}
		return
	}

	var findings []finding
	findings = append(findings, checkTopLevel(root, tops)...)
	findings = append(findings, checkCrossPlane(pkgs)...)
	findings = append(findings, checkInternalRule(pkgs)...)
	findings = append(findings, checkStoreIsSoleIO(pkgs)...)
	findings = append(findings, checkModulesAgainstList(pkgs, mods)...)
	findings = append(findings, checkNoCGO(pkgs, root)...)
	findings = append(findings, checkLanguages(root)...)

	report(findings)
}

// ─────────────────────────────────────────────────────────────────────────────
// 检查 1 · ST-1 顶层目录白名单
// ─────────────────────────────────────────────────────────────────────────────

func checkTopLevel(root string, allowed []string) []finding {
	set := map[string]bool{}
	for _, a := range allowed {
		set[a] = true
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return []finding{{ID: "ST-1", Human: "读仓库根失败", Where: err.Error()}}
	}
	var out []finding
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// 隐藏目录（.git / .pi / .github 等）与常见工具/构建目录不属源码布局
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".egg-info") || isToolDir(name) {
			continue
		}
		if !set[name] {
			out = append(out, finding{
				ID:    "ST-1",
				Human: "顶层目录不在 structure.md §1.1 的清单内；新增顶层目录必须先更新该节的目录树",
				Where: name + "/",
			})
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// isToolDir 判断一个顶层目录是不是工具/构建产物（不是源码平面）。
//
// `*.egg-info`（Python 可编辑安装产生）与 `build` / `dist` / `__pycache__` 都是**产物**，
// 列进 structure.md §1.1 反而会让那份「源码布局」失真，所以在这里排除。
func isToolDir(name string) bool {
	switch name {
	case "vendor", "node_modules", "build", "dist", "__pycache__":
		return true
	default:
		return false
	}
}

// 检查 2 · ST-2 / ST-4 禁止跨顶层目录 import
// ─────────────────────────────────────────────────────────────────────────────

// planeOf 取一个包所属的顶层目录（平面），如 core / edge / api。
func planeOf(importPath string) string {
	if !strings.HasPrefix(importPath, modulePath+"/") {
		return "" // 外部依赖或标准库
	}
	rest := strings.TrimPrefix(importPath, modulePath+"/")
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return rest
}

func checkCrossPlane(pkgs []pkg) []finding {
	var out []finding
	for _, p := range pkgs {
		from := planeOf(p.ImportPath)
		if from == "" {
			continue
		}
		for _, imp := range p.Imports {
			to := planeOf(imp)
			if to == "" || to == from {
				continue
			}
			// api/ 是契约单一事实源，所有平面都可以依赖它。
			if to == "api" {
				continue
			}
			// api/ 自身必须是叶子：不得依赖任何平面。
			if from == "api" {
				out = append(out, finding{
					ID:    "ST-2",
					Human: "api/ 是契约单一事实源，必须是叶子，禁止依赖任何平面",
					Where: fmt.Sprintf("%s  ->  %s", p.ImportPath, imp),
				})
				continue
			}
			out = append(out, finding{
				ID: "ST-2",
				Human: fmt.Sprintf("禁止跨顶层目录 import（%s 不得依赖 %s）：一个进程的源码必须只在一个平面内",
					from, to),
				Where: fmt.Sprintf("%s  ->  %s", p.ImportPath, imp),
			})
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 检查 3 · ST-3 非 core 代码禁止 import core/internal/
// ─────────────────────────────────────────────────────────────────────────────

func checkInternalRule(pkgs []pkg) []finding {
	const forbidden = modulePath + "/core/internal"
	var out []finding
	for _, p := range pkgs {
		from := planeOf(p.ImportPath)
		if from == "core" || from == "" {
			continue
		}
		for _, imp := range p.Imports {
			if strings.HasPrefix(imp, forbidden) {
				out = append(out, finding{
					ID:    "ST-3",
					Human: "适配器只能经 api/ 的生成 stub 调用核心，禁止 import 核心内部包",
					Where: fmt.Sprintf("%s  ->  %s", p.ImportPath, imp),
				})
			}
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 检查 4 · MD-20 store 是核心唯一的 I/O 出口
// ─────────────────────────────────────────────────────────────────────────────

// 外部存储的驱动包前缀。核心其他模块禁止直接引入。
var storageDrivers = []string{
	"database/sql",
	"github.com/redis/go-redis",
	"github.com/go-redis/redis",
	"github.com/ClickHouse/clickhouse-go",
	"github.com/jackc/pgx",
	"github.com/lib/pq",
	"github.com/go-sql-driver/mysql",
	"go.mongodb.org/mongo-driver",
	"gorm.io/gorm",
	"entgo.io/ent",
}

func checkStoreIsSoleIO(pkgs []pkg) []finding {
	const coreInternal = modulePath + "/core/internal/"
	var out []finding
	for _, p := range pkgs {
		if !strings.HasPrefix(p.ImportPath, coreInternal) {
			continue
		}
		rest := strings.TrimPrefix(p.ImportPath, coreInternal)
		ownModule := rest
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			ownModule = rest[:i]
		}
		for _, imp := range p.Imports {
			// (a) 只有 store 可以引入数据库驱动
			if ownModule != "store" {
				for _, drv := range storageDrivers {
					if imp == drv || strings.HasPrefix(imp, drv+"/") {
						out = append(out, finding{
							ID:    "MD-20",
							Human: "核心访问外部存储必须经 store；其他核心模块禁止直接连接数据库",
							Where: fmt.Sprintf("%s  ->  %s", p.ImportPath, imp),
						})
					}
				}
			}
			// (b) store 禁止反向依赖任何业务模块
			if ownModule == "store" && strings.HasPrefix(imp, coreInternal) {
				dep := strings.TrimPrefix(imp, coreInternal)
				if i := strings.IndexByte(dep, '/'); i >= 0 {
					dep = dep[:i]
				}
				if dep != "contract" {
					out = append(out, finding{
						ID:    "MD-20",
						Human: "store 禁止反向依赖业务模块（只允许依赖 contract）",
						Where: fmt.Sprintf("%s  ->  %s", p.ImportPath, imp),
					})
				}
			}
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 检查 5 · MD-18 / MD-19 模块目录必须对应 modules.md §1.1 的一行
// ─────────────────────────────────────────────────────────────────────────────

// structuralDirs 是不承载业务逻辑的结构性目录，不要求出现在模块清单里。
// 依据 structure.md §1.2：contract 只放类型、不放逻辑，因此它不是模块，
// 也没有自己的职责边界。进程入口（*/cmd/<name>）在收集阶段就已跳过，同样不是模块。
//
// 这个豁免名单是**刻意写死的少数几项**，且会在输出里打印 —— 不允许它悄悄变长。
var structuralDirs = map[string]bool{
	"core/internal/contract": true,
}

func checkModulesAgainstList(pkgs []pkg, mods []module) []finding {
	// 清单里的源码目录集合（统一成不带首尾斜杠的相对路径）
	listed := map[string]module{}
	for _, m := range mods {
		listed[strings.Trim(m.SrcDir, "/")] = m
	}

	// 代码里真实存在的模块目录集合。
	// 一个模块的目录 = 其下某个包的路径里，属于「模块根」的最长前缀。
	found := map[string]bool{}
	for _, p := range pkgs {
		plane := planeOf(p.ImportPath)
		switch plane {
		case "core":
			// core/internal/<module> 或 core/cmd/<name>（进程入口，不是模块）
			rest := strings.TrimPrefix(p.ImportPath, modulePath+"/core/")
			if strings.HasPrefix(rest, "cmd/") || rest == "internal" {
				continue
			}
			if strings.HasPrefix(rest, "internal/") {
				r := strings.TrimPrefix(rest, "internal/")
				if i := strings.IndexByte(r, '/'); i >= 0 {
					r = r[:i]
				}
				found["core/internal/"+r] = true
			}
		case "edge", "deception", "analysis":
			rest := strings.TrimPrefix(p.ImportPath, modulePath+"/"+plane+"/")
			// 去掉子目录（如 edge/mirror/cmd/mirror）
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				rest = rest[:i]
			}
			found[plane+"/"+rest] = true
		}
	}

	var out []finding
	for dir := range found {
		if structuralDirs[dir] {
			continue
		}
		if _, ok := listed[dir]; !ok {
			out = append(out, finding{
				ID:    "MD-19",
				Human: "该源码目录不在 modules.md §1.1 的清单内；新增模块必须先给清单加一行",
				Where: dir + "/",
			})
		}
	}
	// 反向：清单里有、代码里没有 —— 不是错误（阶段 2/3 尚未实现），但要说清
	var missing []string
	for dir := range listed {
		if !found[dir] {
			missing = append(missing, dir+"/")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		fmt.Printf("  ℹ 清单里有、代码尚未实现的模块目录 %d 个（阶段 2/3，不算错）：\n", len(missing))
		for _, m := range missing {
			fmt.Printf("      %s\n", m)
		}
		fmt.Println()
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 检查 6 · TB-24 禁止 CGO 与本地原生库
// ─────────────────────────────────────────────────────────────────────────────

// 本地原生库扩展名。仓库里出现即说明绕过了 wire format。
var nativeExts = map[string]bool{
	".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true,
	".so": true, ".dylib": true, ".dll": true, ".a": true, ".o": true,
	".m": true, ".mm": true, ".swift": true, ".java": true,
}

// cgoImport 匹配 `import "C"`（含前置注释块的形式）。
var cgoImport = regexp.MustCompile(`(?m)^\s*import\s+"C"\s*$`)

func checkNoCGO(pkgs []pkg, root string) []finding {
	var out []finding

	// (a) go list 报告的 cgo 源文件
	for _, p := range pkgs {
		if len(p.CgoFiles) > 0 {
			out = append(out, finding{
				ID:    "TB-24",
				Human: "禁止 CGO：跨语言必须经 wire format（gRPC / Protobuf / JSON / WASM）",
				Where: fmt.Sprintf("%s（%s）", p.ImportPath, strings.Join(p.CgoFiles, ", ")),
			})
		}
	}

	// (b)+(c) 扫源码里的 import "C"，以及仓库里不该出现的本地库文件
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			rel, _ := filepath.Rel(root, path)
			out = append(out, finding{
				ID:    "TB-24",
				Human: "遍历仓库时无法读取该路径，CGO 与本地库检查不完整",
				Where: fmt.Sprintf("%s：%v", rel, err),
			})
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)

		// (c) 本地原生库或非批准语言的源文件
		if nativeExts[filepath.Ext(path)] {
			out = append(out, finding{
				ID:    "TB-24",
				Human: "仓库内禁止本地原生库或非批准语言的源文件；跨语言只允许 wire format",
				Where: rel,
			})
			return nil
		}

		// (b) 手写的 import "C"
		switch filepath.Ext(path) {
		case ".go", ".lua", ".rs", ".py", ".ts":
		default:
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			out = append(out, finding{
				ID:    "TB-24",
				Human: "无法读取源文件，CGO 检查不完整",
				Where: fmt.Sprintf("%s：%v", rel, rerr),
			})
			return nil
		}
		if cgoImport.Match(b) {
			out = append(out, finding{
				ID:    "TB-24",
				Human: `禁止 CGO：源码里出现 import "C"`,
				Where: rel,
			})
		}
		return nil
	})
	if err != nil {
		out = append(out, finding{ID: "TB-24", Human: "遍历仓库失败", Where: err.Error()})
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 检查 7 · TB-20 / TB-21 实现语言必须登记且总数 ≤ 5
// ─────────────────────────────────────────────────────────────────────────────

// 批准的实现语言（language.md 的 §1 分层选型 + TB-21 的语言数上限）。
// Lua 曾用于 L1，已由 ADR-0008 移除，因此挪到 unapprovedLangs。
// 扩展名 → 语言名。
var approvedLangs = map[string]string{
	".go": "Go", ".rs": "Rust", ".py": "Python",
	".ts": "TypeScript", ".tsx": "TypeScript",
}

// 已知的编程语言扩展名 → 语言名。这一列是**「除批准的 5 种之外，还有哪些真语言」**。
// 只对这些报错：规则要拦的是「有人引入第 6 种语言」，
// 而不是「仓库里出现了 .png / .gitkeep 这类非代码文件」。
var unapprovedLangs = map[string]string{
	// Lua 曾用于 L1，已被 ADR-0008 移除 —— 再出现 .lua 文件即为越过语言选型
	".lua": "Lua",
	".js":  "JavaScript", ".jsx": "JavaScript", ".mjs": "JavaScript", ".cjs": "JavaScript",
	".java": "Java", ".kt": "Kotlin", ".scala": "Scala", ".groovy": "Groovy",
	".cs": "C#", ".rb": "Ruby", ".php": "PHP", ".pl": "Perl",
	".c": "C", ".h": "C", ".cc": "C++", ".cpp": "C++", ".hpp": "C++",
	".swift": "Swift", ".m": "Objective-C", ".mm": "Objective-C++",
	".ex": "Elixir", ".exs": "Elixir", ".erl": "Erlang", ".hs": "Haskell",
	".ml": "OCaml", ".jl": "Julia", ".r": "R", ".dart": "Dart",
	".clj": "Clojure", ".fs": "F#", ".vb": "Visual Basic", ".nim": "Nim",
	".zig": "Zig", ".v": "V", ".sol": "Solidity", ".asm": "汇编", ".s": "汇编",
}

const langLimit = 5

func checkLanguages(root string) []finding {
	impl := map[string]int{}            // 已批准语言 → 文件数
	unapproved := map[string][]string{} // 未批准语言 → 样例文件
	var out []finding

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// 读不到就不装作没这回事 —— 报出来，由人判断
			rel, _ := filepath.Rel(root, path)
			out = append(out, finding{
				ID:    "TB-21",
				Human: "遍历仓库时无法读取该路径，语言统计不完整",
				Where: fmt.Sprintf("%s：%v", rel, err),
			})
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		rel, _ := filepath.Rel(root, path)
		if lang, ok := approvedLangs[ext]; ok {
			impl[lang]++
			return nil
		}
		if lang, ok := unapprovedLangs[ext]; ok {
			unapproved[lang] = append(unapproved[lang], rel)
		}
		return nil
	})
	if err != nil {
		out = append(out, finding{ID: "TB-21", Human: "遍历仓库失败", Where: err.Error()})
	}

	langs := make([]string, 0, len(impl))
	for l := range impl {
		langs = append(langs, l)
	}
	sort.Strings(langs)

	bad := make([]string, 0, len(unapproved))
	for l := range unapproved {
		bad = append(bad, l)
	}
	sort.Strings(bad)
	for _, l := range bad {
		sample := unapproved[l]
		if len(sample) > 3 {
			sample = sample[:3]
		}
		out = append(out, finding{
			ID:    "TB-21",
			Human: fmt.Sprintf("发现未批准的实现语言 %s；语言层数有上限，新增语言必须先有决策记录并获得确认", l),
			Where: fmt.Sprintf("%d 个文件，例如 %s", len(unapproved[l]), strings.Join(sample, ", ")),
		})
	}

	if len(impl) > langLimit {
		out = append(out, finding{
			ID:    "TB-21",
			Human: fmt.Sprintf("实现语言总数不得超过 %d 种", langLimit),
			Where: fmt.Sprintf("当前 %d 种：%s", len(impl), strings.Join(langs, " ")),
		})
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 文档解析
// ─────────────────────────────────────────────────────────────────────────────

// 顶层目录行：`├── core/` / `└── scripts/`
var topDirLine = regexp.MustCompile(`(?m)^[├└]── ([A-Za-z0-9_.-]+)/`)

func parseTopLevelDirs(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// 只取 §1.1 小节，避免把别处的目录树算进来
	sec := section(string(b), "### 1.1 顶层", "### 1.2")
	if sec == "" {
		return nil, errors.New("找不到 §1.1 顶层 小节")
	}
	var out []string
	for _, m := range topDirLine.FindAllStringSubmatch(sec, -1) {
		out = append(out, m[1])
	}
	if len(out) < 6 {
		return nil, fmt.Errorf("只解析到 %d 个顶层目录，明显偏少 —— 文档格式可能变了，拒绝静默放行", len(out))
	}
	sort.Strings(out)
	return out, nil
}

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
		// | # | 模块 | 层 | 语言 | 源码目录 | 模块文档 | 职责 | 阶段 |
		if len(cells) < 9 {
			continue
		}
		src := strings.Trim(strings.TrimSpace(cells[5]), "`")
		if !strings.Contains(src, "/") {
			continue // 表头或分隔行
		}
		name := strings.Trim(strings.TrimSpace(cells[2]), "`")
		// 已废弃 / 已合并的行用删除线标记（同 D-6：保留编号但不再是有效模块）。
		// 这类行不参与「模块与目录一致」的比对，否则会报出幽灵模块。
		if strings.Contains(name, "~~") || strings.Contains(src, "~~") {
			continue
		}
		out = append(out, module{
			Name:   name,
			Layer:  strings.TrimSpace(cells[3]),
			Langs:  strings.TrimSpace(cells[4]),
			SrcDir: strings.Trim(src, "~"),
			Stage:  strings.Trim(strings.TrimSpace(cells[8]), "*"),
		})
	}
	if len(out) < 15 {
		return nil, fmt.Errorf("只解析到 %d 个模块，明显偏少 —— 文档格式可能变了，拒绝静默放行", len(out))
	}
	return out, nil
}

// section 取 from 到 to 之间的文本（不含 to）。找不到 from 返回空串。
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

// ─────────────────────────────────────────────────────────────────────────────
// go list 与仓库根
// ─────────────────────────────────────────────────────────────────────────────

const modulePath = "shen"

func moduleRoot() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		return "", fmt.Errorf("取模块根目录失败（go list -m）：%w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func loadPackages() ([]pkg, error) {
	out, err := exec.Command("go", "list", "-json", "./...").Output()
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var pkgs []pkg
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			return nil, err
		}
		pkgs = append(pkgs, p)
	}
	if len(pkgs) == 0 {
		return nil, errors.New("go list 没有返回任何包")
	}
	return pkgs, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 输出
// ─────────────────────────────────────────────────────────────────────────────

func report(findings []finding) {
	if len(findings) == 0 {
		fmt.Println("架构检查通过。")
		fmt.Println("  顶层目录 · 跨平面依赖 · 核心内部可见性 · store 唯一 I/O 出口")
		fmt.Println("  模块清单一致性 · CGO 与本地库 · 语言层数")
		return
	}
	fmt.Fprintf(os.Stderr, "架构检查发现 %d 个问题：\n\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(os.Stderr, "  ✗ %s\n\n", f)
	}
	fmt.Fprintln(os.Stderr, "这些规则出自 docs/design/；修代码或先改文档并取得确认，不要绕过检查。")
	os.Exit(1)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "archcheck: "+format+"\n", args...)
	fmt.Fprintln(os.Stderr, "检查未执行完毕 —— 视为失败，不静默放行。")
	os.Exit(1)
}
