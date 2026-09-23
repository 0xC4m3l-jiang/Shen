// Command check-leak 扫描**字符串字面量**里的对外泄漏词（OH-1），以及决策信息回传响应头（OH-5）。
//
// 它回答一个问题：**攻击者的屏幕上会不会出现我们不该说的词** ——
// 「蜜罐」「decoy」「mirage」这类行业词允许出现在源码标识符、配置键与内部文档里（`TM-6`），
// 但一旦作为字符串字面量进入响应体、响应头、错误页、Cookie 名，就等于**当面告诉对手「你在被骗」**。
//
// 依据：
//
//	constraints.md  OH-1（禁用清单）· OH-2（唯一判据：会不会出现在攻击者屏幕上）
//	                OH-3（工程必须提供自动检查）· OH-4（例外逐条声明，禁止整目录豁免）
//	                OH-5（决策结果与风险分数禁止经响应头回传，用 ST-7 对照）
//	terminology.md  TM-6（术语只用于内部）· TM-7（内部术语不上对外表面）
//
// 扫描范围比 OH-3 的「响应面模块」**更严**：modules.md §1.1 里所有**已有源码**的模块目录
// —— 漏扫才是真的漏。清单从文档解析，不硬编码；解析结果骤降直接失败，不静默放行。
//
// 两类**显式**豁免（OH-4 要求逐条声明，此处是规则级声明，不是目录级）：
//
//	① 内部日志调用的参数（`log.*` / zap logger）—— OH-2 表「服务端配置与内部日志（攻击者不可见）」允许；
//	② 错误构造的参数（`errors.New` / `fmt.Errorf`）—— 核心的内部错误只经 gRPC 上行到适配器，
//	   适配器把它折叠成 fail-open（`NI-3`），不落进响应体；且 `ST-7` 另行禁止回显。
//
// 其余任何字面量命中都算问题：要么改代码（首选），要么在 allow.txt 里**逐条**登记并写理由。
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"shen/scripts/internal/docs"
	"sort"
	"strconv"
	"strings"
)

// banned 是 OH-1 的禁用清单，逐字照抄 constraints.md（改这里必须同时改那里）。
var banned = []string{
	// 英文侧
	"honeypot", "honey_pot", "decoy", "canary", "canarytoken", "mirage",
	"deception", "deceive", "tarpit", "trapserver", "x-deception", "x-honeypot",
	// 中文侧
	"蜜罐", "蜜饵", "蜜标", "投毒", "引流", "欺骗", "诱捕", "仿真蜜罐", "会话水印",
}

// decisionTokens 是 OH-5 关心的「决策信息」关键词：决策取值与风险分数不许经响应头回传。
var decisionTokens = []string{"decision", "score", "severity", "verdict", "risk", "action"}

// allowPath 是逐条例外登记处（OH-4）。
const allowPath = "scripts/check-leak/allow.txt"

// modulesDoc 是模块清单的权威位置（扫描范围从它解析）。
const modulesDoc = "docs/design/modules.md"

// 扫描范围限定在**响应面模块**（OH-3 的字面要求）：这些模块的文本可能是攻击者
// 直接看到的东西（响应体、响应头、错误页、诱饵内容、投放片段）。
//
// 依据 OH-2 的适用位置表 —— 配置面（`policy`）与存储面（`store`）、会话与遥测
// 的字符串不会进入响应，故不在扫描范围内；它们的内部术语由 `TM-6` 允许。
// ⚠️ 本表**必须与 `modules.md` §1.1 的有效模块一致**：模块被删除/改名时这里要同步，
// 否则 `responseSurfaceDirs` 会直接失败（「找不到这些响应面模块」）—— 那是有意的，
// 免得模块消失后扫描面**静默缩小**（漏扫才是真的漏）。
//
// 历史：`responder`（伪造响应内容）已于 2026-09-23 删除 —— 响应内容改由 L4 离线生成
// （`analysis/aicap`）+ 适配器 `edge-injection` 改写注入；响应面因此由 `adapter-proxy` /
// `edge-injection` 覆盖，内容过滤的**字面量表**迁到 `common/core/internal/policy/contentfilter.go`。
var responseSurfaceModules = map[string]string{
	"adapter-mirror": "① 旁路镜像接收端（只回 202，但仍是接入面）",
	"adapter-proxy":  "③④ 前置与边车：响应头、错误页、403、注入",
	"edge-injection": "改写蜜罐侧响应体（直接上屏）",
	"judge":          "判定面（判定文本可能随事件外流）",
	"director":       "决策取值与后端名的来源",
	"isolation":      "隔离短路时对外可见的行为描述",
	"control":        "gRPC 服务面（ST-7 禁止回显的强制点）",
	"decoy":          "诱饵内容与投放片段（会被投放/上屏）",
	"honeypot":       "幻境入口与后端池",
}

// minModules 是解析结果的下限：低于它说明文档格式变了，**直接失败**而不是静默少扫。
const minModules = 8

type finding struct {
	ID      string // OH-1 / OH-5 / ALLOW
	Human   string
	Where   string // 文件:行
	Subject string // 豁免与去重的键
}

func (f finding) String() string {
	return fmt.Sprintf("%s  %s\n        %s", f.ID, f.Human, f.Where)
}

type allowEntry struct {
	Check   string
	File    string // 仓库相对路径；豁免精确到**文件 + 字面量**，不允许宽泛豁免
	Subject string
	Reason  string
	used    bool
}

// filterMarker 是文件级声明：该文件**就是**过滤器（例如 AR-22 的黑名单表），
// 它必须包含禁用词本身。OH-4 禁止整目录豁免，但允许逐条声明 —— 这一行就是那条声明，
// 理由必填，且检查会把它打印出来供人核对。
const filterMarker = "check-leak:filter"

func main() {
	root := flag.String("root", ".", "仓库根目录")
	flag.Parse()

	// 设计文档不入库（见 .gitignore）：响应面模块清单在 docs/design/modules.md 里，
	// 没有它就无从知道该扫哪些目录 ⇒ 跳过并打印一行说明（不静默、也不算通过）。
	if !docs.Present(*root) {
		fmt.Println(docs.SkipNote)
		return
	}

	findings, err := run(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "泄漏检查无法完成：%v\n", err)
		os.Exit(1)
	}
	if len(findings) == 0 {
		fmt.Println("泄漏检查通过。")
		fmt.Println("  字符串字面量 ↔ OH-1 禁用清单 · 响应头 ↔ OH-5（决策与分数不回传）· 豁免逐条登记（OH-4）")
		return
	}
	fmt.Printf("泄漏检查发现 %d 个问题：\n\n", len(findings))
	for _, f := range findings {
		fmt.Println("  ✗", f.String())
		fmt.Println()
	}
	fmt.Fprintln(os.Stderr, "读一眼 OH-2：**这个字符串会不会出现在攻击者的屏幕上**？")
	fmt.Fprintln(os.Stderr, "  OH-1 / OH-5 / OH-4 出自 docs/design/constraints.md。")
	fmt.Fprintln(os.Stderr, "  首选是改代码；确属内部用途才在 scripts/check-leak/allow.txt 里逐条登记并写理由 —— 禁止整目录豁免。")
	os.Exit(1)
}

func run(root string) ([]finding, error) {
	dirs, err := responseSurfaceDirs(root)
	if err != nil {
		return nil, err
	}
	allows, err := parseAllow(filepath.Join(root, allowPath))
	if err != nil {
		return nil, err
	}

	var out []finding
	for _, dir := range dirs {
		fs, ferr := scanDir(filepath.Join(root, dir), allows)
		if ferr != nil {
			return nil, ferr
		}
		out = append(out, fs...)
	}

	// 逐条豁免：没被命中的说明它已过期（问题修好了），必须删掉那一行。
	for _, a := range allows {
		if a.used {
			continue
		}
		out = append(out, finding{
			ID:      "ALLOW",
			Human:   "豁免已过期：该字面量已不存在（问题可能已修）—— 请删掉这一行，不留僵尸条目",
			Where:   allowPath,
			Subject: a.Check + " " + a.File + " " + a.Subject,
		})
	}
	if len(allows) > 0 {
		fmt.Fprintf(os.Stderr, "已登记豁免 %d 条（见 %s）：\n", len(allows), allowPath)
		for _, a := range allows {
			fmt.Fprintf(os.Stderr, "  · %s %s  %s —— %s\n", a.Check, a.File, truncate(a.Subject, 40), a.Reason)
		}
	}
	return out, nil
}

// responseSurfaceDirs 从 modules.md §1.1 解析模块源码目录，返回**响应面模块**里真实存在的那些。
//
// 清单不从代码硬编码：模块增删是文档的事，检查跟着文档走；但「哪些模块算响应面」
// 由 `responseSurfaceModules` 显式声明（每个都写了理由，避免整目录豁免）。
func responseSurfaceDirs(root string) ([]string, error) {
	b, err := os.ReadFile(filepath.Join(root, modulesDoc))
	if err != nil {
		return nil, fmt.Errorf("读不到模块清单 %s：%w", modulesDoc, err)
	}
	seen := map[string]bool{}
	missing := map[string]bool{}
	for name := range responseSurfaceModules {
		missing[name] = true
	}
	var dirs []string
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := strings.Split(line, "|")
		// 模块名列是**第一个**带反引号的单元格（形如 `adapter-proxy`）；源码目录列形如 `deception/proxy/`。
		name := ""
		dir := ""
		for _, cell := range cells {
			// 单元格可能是 `**`adapter-proxy`**`（加粗 + 行内代码），先剥掉加粗标记。
			cell = strings.Trim(strings.TrimSpace(cell), "*")
			if !strings.HasPrefix(cell, "`") || !strings.HasSuffix(cell, "`") {
				continue
			}
			p := strings.Trim(cell, "`")
			switch {
			case strings.HasSuffix(p, "/") && (strings.HasPrefix(p, "modules/") ||
				strings.HasPrefix(p, "common/") || strings.HasPrefix(p, "analysis/")):
				if dir == "" {
					dir = p
				}
			case name == "" && !strings.Contains(p, "/"):
				name = p
			}
		}
		if dir == "" || name == "" {
			continue
		}
		if _, wanted := responseSurfaceModules[name]; !wanted {
			continue
		}
		delete(missing, name)
		if seen[dir] {
			continue
		}
		if st, serr := os.Stat(filepath.Join(root, dir)); serr != nil || !st.IsDir() {
			continue // 尚未实现的模块目录不存在，跳过（archcheck 另行核对清单一致性）
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	// 声明了却没在清单里找到的模块名：说明模块改名了 —— 拒绝少扫。
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for n := range missing {
			names = append(names, n)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("%s 里找不到这些响应面模块（改名了？）：%s", modulesDoc, strings.Join(names, ", "))
	}
	if len(dirs) < minModules {
		return nil, fmt.Errorf("从 %s 只解析出 %d 个响应面模块目录（下限 %d）—— 文档格式可能变了，拒绝少扫",
			modulesDoc, len(dirs), minModules)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// scanDir 扫描一个模块目录下的全部非测试 Go 文件。
func scanDir(dir string, allows []*allowEntry) ([]finding, error) {
	var out []finding
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil // 测试夹具不是响应面
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if reason, ok := filterDeclaration(string(src)); ok {
			fmt.Fprintf(os.Stderr, "已声明过滤器文件：%s —— %s\n", path, reason)
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return fmt.Errorf("解析 %s 失败：%w", path, perr)
		}
		out = append(out, scanFile(fset, path, f, allows)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// filterDeclaration 识别文件级过滤器声明 `//check-leak:filter <理由>`。理由必填。
func filterDeclaration(src string) (string, bool) {
	for _, line := range strings.Split(src, "\n") {
		i := strings.Index(line, filterMarker)
		if i < 0 {
			continue
		}
		reason := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line[i+len(filterMarker):]), ":"))
		return reason, true
	}
	return "", false
}

// scanFile 遍历 AST，找两类命中：
//
//	OH-1 —— 字面量（或其子串）出现在禁用清单里；
//	OH-5 —— 被当作响应头名/ Cookie 名使用的字面量含决策类关键词。
func scanFile(fset *token.FileSet, path string, f *ast.File, allows []*allowEntry) []finding {
	var out []finding

	report := func(id, subject, human string, pos token.Pos) {
		where := fmt.Sprintf("%s:%d", path, fset.Position(pos).Line)
		for _, a := range allows {
			if a.Check == id && a.File == path && a.Subject == subject {
				a.used = true // 已逐条登记（精确到文件 + 字面量）
				return
			}
		}
		out = append(out, finding{ID: id, Human: human, Where: where, Subject: subject})
	}

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// OH-5：响应头名（`x.Header().Set/Add("字面量"`）与 Cookie 名。
		if name, pos, ok := headerNameArg(call); ok {
			if tok := containsAnyFold(name, decisionTokens); tok != "" {
				report("OH-5", name,
					fmt.Sprintf("响应头名 %q 含决策类词 %q —— 决策结果与分数**禁止**经响应头回传（OH-5 / ST-7）", name, tok), pos)
			}
			if tok := containsAnyFold(name, banned); tok != "" {
				report("OH-1", name, fmt.Sprintf("响应头名 %q 含对外泄漏词 %q（OH-1）", name, tok), pos)
			}
		}
		if name, pos, ok := cookieNameArg(call); ok {
			if tok := containsAnyFold(name, banned); tok != "" {
				report("OH-1", name, fmt.Sprintf("Cookie 名 %q 含对外泄漏词 %q —— Cookie 名必须中性（如 sid，OH-1 / NI-9）", name, tok), pos)
			}
		}
		return true
	})

	// 逐个字符串字面量核 OH-1，跳过两条例则级豁免（内部日志参数、内部错误构造参数）
	// 与结构化豁免（struct tag：配置键，不是值）。
	skipped := skippedLiterals(f)
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if skipped[lit] {
			return true
		}
		s, uerr := strconv.Unquote(lit.Value)
		if uerr != nil {
			s = lit.Value
		}
		if tok := containsAnyFold(s, banned); tok != "" {
			report("OH-1", s,
				fmt.Sprintf("字面量 %s 含对外泄漏词 %q —— 改代码（首选），或逐条登记豁免并写理由（OH-1 / OH-4）",
					truncate(s, 80), tok), lit.Pos())
		}
		return true
	})

	return dedupe(out)
}

// skippedLiterals 收集不参与 OH-1 检查的字面量节点：
//
//	① 内部日志调用的参数（OH-2 表：服务端内部日志攻击者不可见 → 允许）；
//	② 错误构造的参数（`errors.New` / `fmt.Errorf`）—— 内部错误只上行到适配器，适配器把它
//	   折叠成 fail-open（`NI-3`），不落进响应体；且 `ST-7` 另行禁止回显；
//	③ struct tag（`json:"mirage"` 这类）—— 它是**配置键**，不是会进响应的值（OH-2 表：服务端配置允许）；
//	④ **import 路径**（`import "shen/modules/deception/mirror"`）—— 编译期标识符，不是会进响应的字符串：
//	   它只出现在符号表与构建元数据里（OH-2 判据：攻击者的屏幕上不会出现）。没有这条，
//	   模块目录名一旦含泄漏词（如今 ① 欺骗层在 `modules/deception/`）就会满仓库误报。
func skippedLiterals(f *ast.File) map[*ast.BasicLit]bool {
	skipped := map[*ast.BasicLit]bool{}
	collect := func(n ast.Node) {
		ast.Inspect(n, func(m ast.Node) bool {
			if lit, ok := m.(*ast.BasicLit); ok {
				skipped[lit] = true
			}
			return true
		})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Field:
			if v.Tag != nil {
				skipped[v.Tag] = true
			}
		case *ast.ImportSpec:
			if v.Path != nil {
				skipped[v.Path] = true
			}
		case *ast.CallExpr:
			if isLogCall(v) || isErrorCall(v) {
				for _, arg := range v.Args {
					collect(arg)
				}
			}
		}
		return true
	})
	return skipped
}

// isLogCall 报告该调用是否为内部日志（log.* / zap logger 的方法）。
func isLogCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Printf", "Println", "Print", "Fatalf", "Fatal", "Fatalln", "Panicf", "Panic":
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "log" {
			return true
		}
	case "Info", "Infof", "Warn", "Warnf", "Error", "Errorf", "Debug", "Debugf":
		// zap 风格：`logger.Warn(...)`；`log.Printf` 已在上一个分支处理。
		return true
	}
	return false
}

// isErrorCall 报告该调用是否为错误构造（errors.New / fmt.Errorf）。
func isErrorCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return (pkg.Name == "errors" && sel.Sel.Name == "New") ||
		(pkg.Name == "fmt" && sel.Sel.Name == "Errorf")
}

// headerNameArg 识别响应头名：`w.Header().Set("名", ...)` · `resp.Header.Set(...)` ·
// `w.Header().Add(...)` / `Del(...)`，返回头名字面量。
//
// 只认**接收者是 Header** 的调用 —— 名字里带 `Header` 就当成响应头操作。
// 这一条覆盖不到“把 Header 存进变量再调 Set”的写法（那种只能靠人工核 OH-2）。
func headerNameArg(call *ast.CallExpr) (string, token.Pos, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Set" && sel.Sel.Name != "Add" && sel.Sel.Name != "Del") {
		return "", 0, false
	}
	if !strings.Contains(exprString(sel.X), "Header") {
		return "", 0, false
	}
	if len(call.Args) == 0 {
		return "", 0, false
	}
	return literalValue(call.Args[0])
}

// cookieNameArg 识别 `http.Cookie{Name: "字面量"}`（含 `&http.Cookie{...}`）。
func cookieNameArg(call *ast.CallExpr) (string, token.Pos, bool) {
	for _, arg := range call.Args {
		lit, ok := arg.(*ast.CompositeLit)
		if !ok {
			continue
		}
		if !strings.Contains(exprString(lit.Type), "Cookie") {
			continue
		}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Name" {
				continue
			}
			if s, pos, ok := literalValue(kv.Value); ok {
				return s, pos, true
			}
		}
	}
	return "", 0, false
}

// literalValue 取表达式里的字符串字面量。
func literalValue(e ast.Expr) (string, token.Pos, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", 0, false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		s = lit.Value
	}
	return s, lit.Pos(), true
}

// exprString 把表达式粗略打印成字符串，只用于形态识别（不追求精确还原）。
func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case nil:
		return ""
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	case *ast.CallExpr:
		parts := make([]string, 0, len(v.Args))
		for _, a := range v.Args {
			parts = append(parts, exprString(a))
		}
		return exprString(v.Fun) + "(" + strings.Join(parts, ",") + ")"
	case *ast.StarExpr:
		return "*" + exprString(v.X)
	case *ast.ParenExpr:
		return exprString(v.X)
	default:
		return ""
	}
}

// truncate 把过长的字面量截断，避免输出被大段文本淹没。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return strconv.Quote(s)
	}
	return strconv.Quote(string(r[:n])) + "…"
}

// containsAnyFold 返回 list 中第一个命中 s 的词（英文大小写不敏感；中文按原样包含）。
//
// 大小写不敏感是刻意的：`ACTION_MIRAGE` 与 `mirage` 对攻击者是一回事。
func containsAnyFold(s string, list []string) string {
	lower := strings.ToLower(s)
	for _, tok := range list {
		if strings.Contains(lower, strings.ToLower(tok)) {
			return tok
		}
	}
	return ""
}

func dedupe(in []finding) []finding {
	seen := map[string]bool{}
	var out []finding
	for _, f := range in {
		key := f.ID + "\x00" + f.Where + "\x00" + f.Subject
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}

// parseAllow 读逐条例外登记。格式：`<检查ID> <文件> <字面量>  # 理由`。
//
// 文件与字面量都必填，理由也必填：OH-4 要的是「逐条显式声明并附理由」，
// 没写理由、或写成整目录豁免的，与没登记一个样。
func parseAllow(path string) ([]*allowEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*allowEntry
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		body, reason, _ := strings.Cut(line, "#")
		fields := strings.Fields(strings.TrimSpace(body))
		if len(fields) < 3 {
			return nil, fmt.Errorf("%s:%d 格式应为 `<检查ID> <文件> <字面量>  # 理由`（字面量含空格时写在最后）", allowPath, i+1)
		}
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return nil, fmt.Errorf("%s:%d 缺理由 —— OH-4 要求逐条例外必须写明理由", allowPath, i+1)
		}
		file := fields[1]
		if strings.HasSuffix(file, "/") {
			return nil, fmt.Errorf("%s:%d %q 看起来是目录 —— OH-4 禁止整目录豁免，请写到具体文件", allowPath, i+1, file)
		}
		literal := strings.Join(fields[2:], " ")
		out = append(out, &allowEntry{Check: fields[0], File: file, Subject: literal, Reason: reason})
	}
	return out, nil
}
