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
//	architecture.md     AR-33（任何 LLM 生成必须经 ai-capability 的护栏出口）
//	modules.md   §3     MD-4（依赖方向单向 —— Python 侧的 AI 能力独立性）
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
	"path"
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
	children, err := parseContainerChildren(filepath.Join(root, "docs/design/structure.md"))
	if err != nil {
		fatal("解析 structure.md §1.1 的容器子目录失败：%v", err)
	}
	mods, err := parseModules(filepath.Join(root, "docs/design/modules.md"))
	if err != nil {
		fatal("解析 modules.md §1.1 的模块清单失败：%v", err)
	}
	pkgs, err := loadPackages()
	if err != nil {
		fatal("go list 失败：%v", err)
	}

	// 自检：一个平面都解析不出来，说明 modulePath 与 go.mod 的 module 名已经不一致 ——
	// 那样 ST-2 / ST-3 / MD-20 会**静默全过**（项目名仍可能变，ADR-0004）。拒绝静默放行。
	if unknown := countUnknownPlanes(pkgs); len(pkgs) > 0 && unknown == len(pkgs) {
		fatal("所有包都解析不出平面（modulePath=%q）—— 与 go.mod 的 module 名不一致？", modulePath)
	}

	if *dump {
		fmt.Printf("顶层目录（structure.md §1.1，%d 个）：%s\n", len(tops), strings.Join(tops, " "))
		fmt.Printf("模块清单（modules.md §1.1，%d 行）：\n", len(mods))
		for _, m := range mods {
			fmt.Printf("  %-24s %-8s %-14s %s\n", m.Name, m.Stage, m.Langs, m.SrcDir)
		}
		for c, kids := range children {
			fmt.Printf("容器 %s/ 下的平面：%s\n", c, strings.Join(kids, " "))
		}
		return
	}

	var findings []finding
	findings = append(findings, checkTopLevel(root, tops)...)
	findings = append(findings, checkContainerChildren(root, children)...)
	findings = append(findings, checkCrossPlane(pkgs)...)
	findings = append(findings, checkInternalRule(pkgs)...)
	findings = append(findings, checkStoreIsSoleIO(pkgs)...)
	findings = append(findings, checkModulesAgainstList(root, pkgs, mods)...)
	findings = append(findings, checkNoCGO(pkgs, root)...)
	findings = append(findings, checkLanguages(root)...)
	findings = append(findings, checkGuardrailIsSoleExit(root)...)
	findings = append(findings, checkPythonDependencyDirection(root)...)

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

// 容器行下面的平面行，如 `│   ├── deception/`（也接受写全路径的 `│   ├── modules/deception/`）。
var nestedDirLine = regexp.MustCompile(`^[│ ]+[├└]── ([A-Za-z0-9_./-]+)/`)

// parseContainerChildren 解析 §1.1 里**容器**（`modules/` · `common/`）下面列出的子目录。
//
// 为什么需要它：顶层白名单只管仓库根一层 —— 容器一旦建立，`modules/` 下的新目录就脱出了 `ST-1` 的覆盖
// （搬迁前任何新平面都是新顶层目录，必被拦下）。这里把容器子目录也纳入白名单。
// 顺带堵住一个同名碰撞：`modules/core/` 这样的名字会与 `common/core` 混成同一个平面，
// 而白名单只允许 `content/` 下是 `core`，因此它会在这一层被拦下。
func parseContainerChildren(path string) (map[string][]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sec := section(string(b), "### 1.1 顶层", "### 1.2")
	if sec == "" {
		return nil, errors.New("找不到 §1.1 顶层 小节")
	}
	out := map[string][]string{}
	current := ""
	for _, line := range strings.Split(sec, "\n") {
		if m := topDirLine.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if m := nestedDirLine.FindStringSubmatch(line); m != nil && current != "" {
			// 取最后一段：写入的写法可能是相对名，也可能是全路径，两种都认。
			out[current] = append(out[current], filepath.Base(m[1]))
		}
	}
	return out, nil
}

// countUnknownPlanes 数出解析不出平面的包（即 modulePath 前缀不匹配的）。
func countUnknownPlanes(pkgs []pkg) int {
	n := 0
	for _, p := range pkgs {
		if planeOf(p.ImportPath) == "" {
			n++
		}
	}
	return n
}

// checkContainerChildren 核对容器的实际子目录都在 §1.1 里列着。
func checkContainerChildren(root string, children map[string][]string) []finding {
	var out []finding
	for container, allowed := range children {
		set := map[string]bool{}
		for _, a := range allowed {
			set[a] = true
		}
		entries, err := os.ReadDir(filepath.Join(root, container))
		if err != nil {
			continue // 容器本身没建：那是顶层检查的事
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".egg-info") || isToolDir(name) {
				continue
			}
			if !set[name] {
				out = append(out, finding{
					ID: "ST-1",
					Human: fmt.Sprintf("%s/ 下的子目录不在 structure.md §1.1 的清单内；"+
						"容器里新增平面必须先更新该节的目录树", container),
					Where: container + "/" + name + "/",
				})
			}
		}
	}
	return out
}

// 检查 2 · ST-2 / ST-4 禁止跨顶层目录 import
// ─────────────────────────────────────────────────────────────────────────────

// planeOf 取一个包所属的顶层目录（平面），如 core / deception / api。
// planeOf 返回包所属的**平面**（= 代码真正归属的那一层）。
//
// 顶层是「容器 + 平面」两级结构（structure.md §1.1）：`modules/<平面>/…` 与 `common/<平面>/…`
// 的第一段是**容器**（modules = 产品功能模块，common = 公用代码），第二段才是平面；
// `analysis/` 等则直接以平面作顶层目录。契约因此归 `api`、内核归 `core`。
func planeOf(importPath string) string {
	if !strings.HasPrefix(importPath, modulePath+"/") {
		return "" // 外部依赖或标准库
	}
	rest := strings.TrimPrefix(importPath, modulePath+"/")
	first, tail, _ := strings.Cut(rest, "/")
	switch first {
	case "modules", "common":
		if tail == "" {
			return first // 容器自身（没包任何包）
		}
		plane, _, _ := strings.Cut(tail, "/")
		return plane
	}
	return first
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
	const forbidden = modulePath + "/common/core/internal"
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
	const coreInternal = modulePath + "/common/core/internal/"
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
// planeRoot 是每个平面的**源码根前缀**（与 modules.md §1.1「源码目录」列同一坐标系）。
// 顶层是「容器 + 平面」两级（structure.md §1.1）：modules = 产品功能模块，common = 公用代码。
var planeRoot = map[string]string{
	"core":      "common/core/",
	"api":       "common/api/",
	"deception": "modules/deception/",
	"honeypot":  "modules/honeypot/",
	"console":   "modules/console/",
	"analysis":  "analysis/",
}

var structuralDirs = map[string]bool{
	"common/core/internal/contract": true,
}

func checkModulesAgainstList(root string, pkgs []pkg, mods []module) []finding {
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
			// common/core/internal/<module> 或 common/core/cmd/<name>（进程入口，不是模块）
			rest := strings.TrimPrefix(p.ImportPath, modulePath+"/"+planeRoot["core"])
			if strings.HasPrefix(rest, "cmd/") || rest == "internal" {
				continue
			}
			if strings.HasPrefix(rest, "internal/") {
				r := strings.TrimPrefix(rest, "internal/")
				if i := strings.IndexByte(r, '/'); i >= 0 {
					r = r[:i]
				}
				found[planeRoot["core"]+"internal/"+r] = true
			}
		case "deception", "honeypot", "analysis":
			base := planeRoot[plane]
			rest := strings.TrimPrefix(p.ImportPath, modulePath+"/"+base)
			// 去掉子目录（如 modules/deception/proxy/cmd/proxy）
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				rest = rest[:i]
			}
			found[base+rest] = true
		}
	}

	// 非 Go 模块（L4 的 Python · L3 的声明式 · 纯配置模块）：`go list` 看不到它们，
	// 因此改用「目录存在且有文件」判定 —— 否则它们会被列进「代码尚未实现」的提示里，
	// 让那行提示变成噪声（读的人会以为这些模块没做）。依据：structure.md §1.5 的「已建 / 未建」。
	for dir := range listed {
		if found[dir] {
			continue
		}
		if dirHasFiles(filepath.Join(root, dir)) {
			found[dir] = true
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
	// 反向：清单里有、代码里没有 —— 不是错误（阶段 2/3 尚未实现），但要说清。
	// 分两类说：目录都没建 vs 目录建了但里面是空的 —— 后者的排查方向完全不同。
	var missing, empty []string
	for dir := range listed {
		if found[dir] {
			continue
		}
		if dirExists(filepath.Join(root, dir)) {
			empty = append(empty, dir+"/")
			continue
		}
		missing = append(missing, dir+"/")
	}
	sort.Strings(missing)
	sort.Strings(empty)
	if len(missing)+len(empty) > 0 {
		fmt.Printf("  ℹ 清单里有、代码尚未实现的模块目录 %d 个（阶段 2/3，不算错）：\n", len(missing)+len(empty))
		for _, dir := range missing {
			fmt.Printf("      %s（目录尚未创建）\n", dir)
		}
		for _, dir := range empty {
			fmt.Printf("      %s（目录已建，里面还没有文件）\n", dir)
		}
		fmt.Println()
	}
	return out
}

// dirHasFiles 报告目录是否存在且含至少一个普通文件（不递归计数空目录）。
// 非 Go 模块的「已实现」判据：不靠扩展名白名单（那会把声明式 / 配置模块漏掉），
// 只看「有没有东西」——目录里放了东西就说明有人在维护它。
func dirHasFiles(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			return true
		}
		if dirHasFiles(filepath.Join(path, e.Name())) {
			return true
		}
	}
	return false
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
// 检查 8 · AR-33 模型客户端只能被 ai-capability 的出口与接缝 import
// ─────────────────────────────────────────────────────────────────────────────

// modelClientImport 匹配**import 模型客户端**的语句：
//
//	from ..llm.client import X       （包内相对，带点号）
//	from analysis.llm.client import   （绝对）
//	from analysis.llm import client   （分两步：先包再成员 —— 同样是拿到客户端）
//	from ..llm import client
//	from .client import X            （在 analysis/llm/ 内部）
//	import analysis.llm.client
//
// 只看 import 语句 —— 注释里提到 `llm.client` 不算（文档需要能引用它）。
var modelClientImport = regexp.MustCompile(
	`(?m)^\s*(?:` +
		`from\s+\.*[\w.]*\bllm\.client\s+import` + `|` + // from …llm.client import X
		`from\s+\.*[\w.]*\bllm\s+import\s+client\b` + `|` + // from …llm import client
		`from\s+\.client\s+import` + `|` + // 在 llm/ 内部：from .client import X
		`import\s+\.*[\w.]*\bllm\.client\b` + // import …llm.client
		`)`,
)

// guardrailSoleExitPrefixes 是**允许** import 模型客户端的路径前缀：
//
//	analysis/aicap/service.py  —— 唯一出口（`AR-33`）
//	analysis/aicap/model.py    —— 模型接缝（取客户端 + 核对无执行面，`AR-32`）
//	analysis/llm/              —— 客户端自身所在层
//	analysis/tests/            —— 测试（不是生成路径）
var guardrailSoleExitPrefixes = []string{
	"analysis/aicap/service.py",
	"analysis/aicap/model.py",
	"analysis/llm/",
	"analysis/tests/",
}

// checkGuardrailIsSoleExit 把 `AR-33` 的「无绕过路径」变成可执行检查：
// 任何生成**必须**走 `ai-capability` 的护栏出口；别处一旦直接 import 模型客户端，
// 就等于开了一条不过护栏的生成路径（它会绕开 schema / 黑名单 / 长度 / 风格四关）。
func checkGuardrailIsSoleExit(root string) []finding {
	dir := filepath.Join(root, "analysis")
	if !dirExists(dir) {
		return []finding{{
			ID:    "AR-33",
			Human: "找不到 analysis/ 目录 —— 护栏结构检查无法执行（不静默放行）",
			Where: "analysis/",
		}}
	}

	scanned, guarded := 0, 0
	var out []finding
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			rel, _ := filepath.Rel(root, path)
			out = append(out, finding{
				ID:    "AR-33",
				Human: "遍历 analysis/ 失败，护栏结构检查不完整",
				Where: fmt.Sprintf("%s：%v", rel, err),
			})
			return nil
		}
		if d.IsDir() {
			if name := d.Name(); strings.HasPrefix(name, ".") || name == "__pycache__" {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".py" {
			return nil
		}
		scanned++
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if isGuardrailExempt(rel) {
			guarded++
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			out = append(out, finding{
				ID:    "AR-33",
				Human: "无法读取源文件，护栏结构检查不完整",
				Where: fmt.Sprintf("%s：%v", rel, rerr),
			})
			return nil
		}
		if modelClientImport.Match(b) {
			out = append(out, finding{
				ID:    "AR-33",
				Human: "除 ai-capability 的出口（service.py）与接缝（model.py）外，禁止 import 模型客户端 —— 那不是绕过护栏了吗",
				Where: rel,
			})
		}
		return nil
	})
	if err != nil {
		out = append(out, finding{ID: "AR-33", Human: "遍历 analysis/ 失败", Where: err.Error()})
	}

	// 解析结果不达预期就直接报错：扫描不到 Python 文件说明路经变了，不能静默通过。
	if scanned == 0 {
		out = append(out, finding{
			ID:    "AR-33",
			Human: "analysis/ 下一个 .py 文件都没扫到 —— 路径或过滤条件可能坏了，拒绝静默放行",
			Where: "analysis/",
		})
	}
	if guarded == 0 {
		out = append(out, finding{
			ID:    "AR-33",
			Human: "护栏出口与接缝文件都不存在（analysis/aicap/service.py 是 AR-33 的判据）",
			Where: "analysis/aicap/",
		})
	}
	return out
}

// isGuardrailExempt 报告该路径是否被允许 import 模型客户端。
func isGuardrailExempt(rel string) bool {
	for _, prefix := range guardrailSoleExitPrefixes {
		if strings.HasSuffix(prefix, "/") {
			if strings.HasPrefix(rel, prefix) {
				return true
			}
			continue
		}
		if rel == prefix {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// 检查 9 · MD-4 Python 侧的依赖方向（AI 能力必须独立、可被第二个消费方复用）
// ─────────────────────────────────────────────────────────────────────────────

// independenceAllowed 是「从某个顶层子包出发，允许依赖哪些顶层子包」（`MD-4` 依赖方向单向）。
//
// 为什么只列这两个：它们是 **AI 能力本体** —— 用户要求它「独立、解耦、可被第二个消费方复用」。
// 消费方侧（`intent` / `chain` / `strategy` / `worker` / `tests`）**不在表里**：
// 方向是**消费方 → 能力**，它们依赖 ai-capability 是设计意图，不是耦合。
//
//	aicap  → 只允许 `analysis.llm` 与自己（能力不依赖纪律层之外的任何东西）
//	llm    → 只允许自己（纪律层不依赖出口，否则就是反向依赖）
var independenceAllowed = map[string]map[string]bool{
	"aicap": {"aicap": true, "llm": true},
	"llm":   {"llm": true},
}

// pyAbsImport 抓**绝对**导入里的仓内目标：`from analysis.X… import` / `import analysis.X…`。
var pyAbsImport = regexp.MustCompile(
	`(?m)^\s*(?:from\s+(analysis(?:\.[\w]+)*)\s+import|import\s+(analysis(?:\.[\w]+)*))`,
)

// pyRelImport 抓**相对**导入的点号数与点号后的第一段名字：`from ...llm.contract import X`。
//
// 这只是个文本近似 —— 它不解析 Python 的 import 语法，也看不见 `importlib.import_module`。
// 与 `AR-33` 的模型客户端检查同口径：**只看 import 语句**（注释与文档串里的示例不算），
// 并且只在这两种形式**都不能解析**时报告「无法解析」，不静默放过。
var pyRelImport = regexp.MustCompile(`(?m)^\s*from\s+(\.+)([\w]*)`)

// checkPythonDependencyDirection 把「AI 能力独立」变成可执行检查。
//
// 它拦两类事：
//
//	① `analysis/aicap/**` 依赖了 `analysis.llm` 之外的东西
//	   （例如回头去 import `analysis.intent` —— 能力反过来依赖消费者）；
//	② `analysis/llm/**` 依赖了 `analysis.aicap` —— 纪律层反向依赖出口。
//
// 扫描不到任何受约束文件时**报错**，不静默通过（路径或过滤条件坏了必须是可见的失败）。
func checkPythonDependencyDirection(root string) []finding {
	dir := filepath.Join(root, "analysis")
	if !dirExists(dir) {
		return []finding{{
			ID:    "MD-4",
			Human: "找不到 analysis/ 目录 —— Python 依赖方向检查无法执行（不静默放行）",
			Where: "analysis/",
		}}
	}

	scanned := 0
	var out []finding
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			rel, _ := filepath.Rel(root, p)
			out = append(out, finding{
				ID:    "MD-4",
				Human: "遍历 analysis/ 失败，依赖方向检查不完整",
				Where: fmt.Sprintf("%s：%v", rel, walkErr),
			})
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			// `.venv` / `.pytest_cache` 等隐藏目录、pycache、以及生成物 proto/ 不属源码
			if strings.HasPrefix(name, ".") || name == "__pycache__" || name == "proto" {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".py" {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		owner := pyOwnerSubpackage(rel)
		allowed, constrained := independenceAllowed[owner]
		if !constrained {
			return nil
		}
		scanned++
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			out = append(out, finding{
				ID:    "MD-4",
				Human: "无法读取源文件，依赖方向检查不完整",
				Where: fmt.Sprintf("%s：%v", rel, rerr),
			})
			return nil
		}
		for _, target := range pyImportTargets(rel, string(b)) {
			if !allowed[target] {
				out = append(out, finding{
					ID: "MD-4",
					Human: fmt.Sprintf(
						"禁止依赖 analysis.%s —— analysis/%s 只允许依赖：%s（AI 能力必须独立、依赖方向单向）",
						target, owner, strings.Join(sortedKeys(allowed), " / ")),
					Where: rel,
				})
			}
		}
		return nil
	})
	if err != nil {
		out = append(out, finding{ID: "MD-4", Human: "遍历 analysis/ 失败", Where: err.Error()})
	}
	if scanned == 0 {
		out = append(out, finding{
			ID:    "MD-4",
			Human: "analysis/aicap 与 analysis/llm 下一个 .py 都没扫到 —— 路径或过滤条件可能坏了，拒绝静默放行",
			Where: "analysis/",
		})
	}
	return out
}

// pyOwnerSubpackage 取模块所属的**顶层**子包：`analysis/aicap/tasks/x.py` → `aicap`。
// 直接放在 `analysis/` 下的文件（如 `worker.py`）返回 ""（它是装配层，不在此检查范围）。
func pyOwnerSubpackage(rel string) string {
	parts := strings.Split(strings.TrimPrefix(rel, "analysis/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

// pyImportTargets 解析出该文件**指向 analysis 下顶层子包**的依赖目标（已去重、已排序）。
//
// 返回的每个元素都是 `analysis.<target>` 里的 `<target>`；指向标准库 / 第三方 /
// 本包内模块的导入**不入结果**（它们不构成跨子包依赖）。
func pyImportTargets(rel, src string) []string {
	pkgParts := pyPackageParts(rel)
	seen := map[string]bool{}
	var out []string
	add := func(target string) {
		if target == "" || seen[target] {
			return
		}
		seen[target] = true
		out = append(out, target)
	}

	for _, m := range pyAbsImport.FindAllStringSubmatch(src, -1) {
		path := m[1]
		if path == "" {
			path = m[2]
		}
		// `analysis` / `analysis.a.b` → 第一段子包名
		if rest, ok := strings.CutPrefix(path, "analysis."); ok {
			add(strings.SplitN(rest, ".", 2)[0])
		}
	}

	for _, m := range pyRelImport.FindAllStringSubmatch(src, -1) {
		dots, name := len(m[1]), m[2]
		if name == "" {
			// `from . import x` / `from .. import x`：指向包本身，解析不出子包 —— 跳过
			continue
		}
		// 相对导入：D 个点 = 从当前包向上走 D-1 层（Python 的语义）
		up := dots - 1
		if up > len(pkgParts) {
			// 点号数超出 analysis 包 —— Python 自己也会报错，这里显式指出，不静默放过
			add("???")
			continue
		}
		base := pkgParts[:len(pkgParts)-up]
		if len(base) == 0 {
			add(name) // 落在 analysis 包上 → name 就是顶层子包名
			continue
		}
		add(base[0]) // 落在某个子包内部 → 顶层子包是 base 的第一段
	}

	sort.Strings(out)
	return out
}

// pyPackageParts 取模块所在包相对于 analysis 的路径段：
// `analysis/aicap/tasks/x.py` → `[aicap tasks]`；`analysis/aicap/x.py` → `[aicap]`。
func pyPackageParts(rel string) []string {
	dir := path.Dir(strings.TrimPrefix(rel, "analysis/"))
	if dir == "." || dir == "" {
		return nil
	}
	return strings.Split(dir, "/")
}

// sortedKeys 返回集合的键（已排序）—— 错词信息里列出来的「允许集合」必须稳定。
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ─────────────────────────────────────────────────────────────────────────────
// 输出
// ─────────────────────────────────────────────────────────────────────────────

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func report(findings []finding) {
	if len(findings) == 0 {
		fmt.Println("架构检查通过。")
		fmt.Println("  顶层目录 · 跨平面依赖 · 核心内部可见性 · store 唯一 I/O 出口")
		fmt.Println("  模块清单一致性 · CGO 与本地库 · 语言层数 · 护栏为唯一出口（AR-33）")
		fmt.Println("  AI 能力独立性（MD-4：aicap / llm 的依赖白名单）")
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
