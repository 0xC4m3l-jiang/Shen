// Command verify 是**逐模块的功能测试流程**：把「24 个模块各自有没有文档、有没有单测、
// 该守哪些架构规则、有哪些功能场景」汇总成一张表，并按需真的把每个模块的测试跑一遍。
//
// 它为什么存在：`make gate` 把 240+ 用例混在一起跑 —— 绿了，但**看不出哪个模块过没过**；
// 而模块文档里其实早已写好「这个模块怎么验」（§4 关键规则 / §7 测试），只是没人把两边对上。
// 本命令把那两节**解析出来当检查表**，于是：
//
//	① 文档写了却没测的模块 → 报出来（漏写测试就是漏写验证）；
//	② 文档没写规则依据的模块 → 报出来（MD-2 要求 §4 至少引一条规则）；
//	③ 有测试路径但从没跑过 → 真的跑（`go test` / `pytest`），失败即非零退出。
//
// 清单**从文档解析，不硬编码**（`docs/design/modules.md` §1.1 + 各模块文档的 §4/§7）。
//
// 用法：
//
//	go run ./scripts/verify                 # 逐模块跑测试并出表
//	go run ./scripts/verify -no-run         # 只核「有没有证据」（纯解析，<1s；gate 用这个）
//	go run ./scripts/verify -only judge,policy
//	go run ./scripts/verify -json           # 机器可读（给 CI / 报告用）
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"shen/scripts/internal/modules"
)

// ─────────────────────────────────────────────────────────────────────────────
// 数据结构
// ─────────────────────────────────────────────────────────────────────────────

// target 是一个测试目标：一条要跑的命令 + 它能证明什么。
type target struct {
	Kind   string // go / pytest
	Where  string // 显示用（包目录或测试文件）
	Args   []string
	Dir    string // 工作目录（相对仓库根）
	Source string // 它是从文档的哪个位置解析出来的（审计用）
}

// result 是一次实际执行的结果。
type result struct {
	OK     bool
	Cases  int
	Detail string
}

// evidence 是一个模块的完整证据链。
type evidence struct {
	Module    modules.Module
	DocOK     bool
	DocNote   string   // 「无源码 / 没有单测」这类显式豁免的原文摘要
	Rules     []string // §4 关键规则里引用的规则 ID
	Targets   []target // §7 测试里解析出的测试目标
	Scenarios []string // 遥测/流量场景（来自 scripts/traffic/scenarios.json 的 modules 标签）
	Results   map[string]result
	Problems  []string
}

// scenario 是 scripts/traffic/scenarios.json 的一条（只取我们关心的字段）。
type scenario struct {
	ID      string   `json:"id"`
	Group   string   `json:"group"`
	Modules []string `json:"modules"`
	Stack   string   `json:"stack"`
}

// pyPython 是 L4 的 venv Python（pytest 必须跑在锁定的那个环境里，见 TB-16 / check-pydeps）。
const pyPython = "analysis/.venv/bin/python"

// allowedBinaries 是本工具**唯一**允许执行的程序。
//
// 为什么要有白名单：测试目标（包路径 / 测试文件）是从**模块文档**里解析出来的 ——
// 文档是可以被改的。把「可执行程序」钉死在这两个上，文档里就算写了别的字符串，
// 它也只会被当成**参数**（或解析不出来而跳过），不会变成一条命令。
var allowedBinaries = map[string]bool{"go": true, pyPython: true}

var ruleID = regexp.MustCompile(`\b(?:AR|INT|ST|MD|NI|TB|TM|OH|SB|BA|DEV)-\d+\b`)
var backtick = regexp.MustCompile("`([^`]+)`")
var exemptWords = []string{"没有单测", "无源码", "没有源码", "不产生源码", "文档级约定"}

// ─────────────────────────────────────────────────────────────────────────────
// 入口
// ─────────────────────────────────────────────────────────────────────────────

func main() {
	root := repoRoot()
	jsonOut := flag.Bool("json", false, "输出机器可读的 JSON")
	noRun := flag.Bool("no-run", false, "只核证据是否齐备，不真的跑测试")
	only := flag.String("only", "", "只核这些模块（逗号分隔）")
	list := flag.Bool("list", false, "只列出模块与解析出的证据，不做判定")
	flag.Parse()

	mods, err := modules.Parse(filepath.Join(root, "docs/design/modules.md"))
	fatalOn(err, "解析 modules.md §1.1 失败")

	scenarios := loadScenarios(filepath.Join(root, "scripts/traffic/scenarios.json"))
	onlySet := map[string]bool{}
	for _, name := range strings.Split(*only, ",") {
		if name = strings.TrimSpace(name); name != "" {
			onlySet[name] = true
		}
	}

	// 同一个测试目标（例：test_aicap_l4_tasks.py）会被多个模块引用 —— 只跑一次、共享结果。
	// 不共享的话，同一份 pytest 会被启动 N 次，总时长随模块数线性膨胀（实测 2m35s → 目标 <60s）。
	shared := map[string]result{}
	var all []evidence
	for _, m := range mods {
		if len(onlySet) > 0 && !onlySet[m.Name] {
			continue
		}
		all = append(all, inspect(root, m, scenarios, !*noRun, shared))
	}

	if *list {
		for _, e := range all {
			fmt.Printf("%-20s %-24s 规则 %d · 测试目标 %d · 场景 %d\n",
				e.Module.Name, e.Module.SrcDir, len(e.Rules), len(e.Targets), len(e.Scenarios))
		}
		return
	}
	if *jsonOut {
		out, _ := json.MarshalIndent(all, "", "  ")
		fmt.Println(string(out))
	} else {
		report(all)
	}
	if failed := countFailures(all); failed > 0 {
		os.Exit(1)
	}
}

// inspect 收集一个模块的全部证据（并按需真的跑测试）。
func inspect(
	root string, m modules.Module, scenarios []scenario, run bool, shared map[string]result,
) evidence {
	e := evidence{Module: m, Results: map[string]result{}}

	// ① 文档（MD-17：目录存在 ⇒ 文档必须存在）
	docPath := filepath.Join(root, m.Doc)
	body, err := os.ReadFile(docPath)
	if err != nil {
		e.Problems = append(e.Problems, fmt.Sprintf("模块文档缺失（MD-17）：%s", m.Doc))
		return e
	}
	e.DocOK = true
	doc := string(body)

	// ② §4 关键规则：至少引一条规则 ID（MD-2 的「追不回设计依据」那条）
	sec4 := modules.Section(doc, "## 4.", "## 5.")
	e.Rules = dedup(ruleID.FindAllString(sec4, -1))
	if len(e.Rules) == 0 && !isExempt(sec4) {
		e.Problems = append(e.Problems, "§4 关键规则里没有引用任何规则 ID（追不回设计依据，MD-2）")
	}

	// ③ §7 测试：解析出测试目标；显式写「没有单测」的模块记豁免
	//
	// 另外：**状态行写明「推迟」的模块**（如 honeypot-shell，用户裁定推迟）也不该算失败 ——
	// 它没有测试是因为它还没实现，而「还没实现」已经在文档里登记、且 `progress.md` 有状态。
	// 把这种情况报成失败只会训练人忽略这个检查（假红比漏报更坏）。
	status := statusLine(doc)
	deferred := strings.Contains(status, "推迟") || strings.Contains(status, "未建") ||
		strings.Contains(status, "尚未实现")
	sec7 := modules.Section(doc, "## 7.", "## 8.")
	if deferred {
		e.DocNote = "状态行登记为「推迟 / 未建」—— 本轮不要求它有单测（" + firstLine(status) + "）"
	} else {
		e.Targets = parseTargets(root, m, sec7)
		switch {
		case len(e.Targets) > 0:
			// 有目标就照常跑 —— 豁免**不能**靠「§7 里出现过某句话」来跳过有测试的模块
		case isExempt(sec7):
			e.DocNote = "文档显式说明本模块无单测 / 无源码（豁免）"
		default:
			e.Problems = append(e.Problems, "§7 测试里没有解析出任何测试目标（模块必须可独立测试，MD-22）")
		}
	}

	// ④ 功能场景：按 scenarios.json 的 modules 标签归属
	for _, s := range scenarios {
		for _, owner := range s.Modules {
			if owner == m.Name {
				label := s.ID
				if s.Stack != "" && s.Stack != "shadow" {
					label += "（需 " + s.Stack + " 形态起栈）"
				}
				e.Scenarios = append(e.Scenarios, label)
			}
		}
	}

	// ⑤ 真的跑（默认；-no-run 只核证据）
	if run {
		for _, t := range e.Targets {
			if cached, ok := shared[strings.Join(t.Args, " ")+"@"+t.Dir]; ok {
				e.Results[targetKey(t)] = cached
				if !cached.OK {
					e.Problems = append(e.Problems, fmt.Sprintf("测试失败：%s（%s）", t.Where, cached.Detail))
				}
				continue
			}
			if !allowedBinaries[t.Args[0]] {
				e.Problems = append(e.Problems, fmt.Sprintf(
					"测试目标要执行 %q —— 不在白名单（只允许 go 与 %s），拒绝执行（%s）",
					t.Args[0], pyPython, t.Source))
				continue
			}
			bin := t.Args[0]
			if bin == pyPython {
				// 绝对路径：cmd.Dir 会切到 analysis/，而 pyPython 是仓库存根相对路径
				bin = filepath.Join(root, pyPython)
			}
			if _, err := os.Stat(bin); err != nil && bin != "go" {
				e.Problems = append(e.Problems, fmt.Sprintf("测试目标要执行的程序不存在：%s（先跑 make pyenv）", bin))
				continue
			}
			cmd := exec.Command(bin, t.Args[1:]...)
			cmd.Dir = filepath.Join(root, t.Dir)
			out, err := cmd.CombinedOutput()
			res := result{OK: err == nil, Detail: summarize(string(out))}
			res.Cases = countCases(t.Kind, string(out))
			// 「跑绿了但一个用例都没数出来」不许当成通过：它多半意味着
			// （a）解析规则过期（输出格式变了），或（b）文档里的测试名/过滤词已经与代码脱节。
			if res.OK && res.Cases == 0 {
				res.OK = false
				res.Detail = "跑完了但数不出用例数（输出格式变了？或文档里的测试名已与代码脱节）"
			}
			shared[strings.Join(t.Args, " ")+"@"+t.Dir] = res
			e.Results[targetKey(t)] = res
			if !res.OK {
				e.Problems = append(e.Problems, fmt.Sprintf("测试失败：%s（%s）", t.Where, res.Detail))
			}
		}
	}
	return e
}

// parseTargets 从 §7 的表格里解析测试目标。
//
// 支持的写法（三种都是仓库里真实存在的）：
//
//	`judge_test.go`                          裸文件名 → 模块自己的目录
//	`analysis/tests/test_l4_modules.py`      仓库存根相对路径
//	[`../../common/core/internal/control/telemetry_test.go`](…)   文档相对路径
//
// `同上` 继承上一行的位置 —— 文档里大量这么写，不处理就会漏掉一半目标。
func parseTargets(root string, m modules.Module, sec string) []target {
	var out []target
	seen := map[string]bool{}
	var last []target

	for _, line := range strings.Split(sec, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		if strings.Contains(line, "同上") {
			out = append(out, last...)
			continue
		}
		var row []target
		for _, raw := range backtick.FindAllStringSubmatch(line, -1) {
			tok := strings.TrimSpace(raw[1])
			switch {
			case strings.HasSuffix(tok, "_test.go"):
				pkg := m.SrcDir
				// 文档相对路径（`../../common/...`）与存根相对路径都按「文件所在包的目录」算
				if strings.Contains(tok, "/") {
					if p := resolvePath(tok); p != "" {
						pkg = filepath.ToSlash(filepath.Dir(p))
					}
				}
				row = append(row, target{
					Kind: "go", Where: "go test ./" + pkg,
					Args: []string{"go", "test", "-count=1", "-v", "./" + pkg},
					Dir:  ".", Source: m.Doc + " §7",
				})
			case strings.HasSuffix(tok, ".py") && strings.Contains(tok, "test"):
				p := resolvePath(tok)
				if p == "" {
					continue
				}
				// 刻意**不**补 `-q`：pyproject 的 addopts 已经是 `-q`，再补一次变 `-qq`，
				// 那样连「7 passed in 0.03s」这行都被抑制，用例数就数不出来了（实测踩过）。
				args := []string{pyPython, "-m", "pytest", strings.TrimPrefix(p, "analysis/")}
				// 文档里的 `（test_intent_*）` 是 pytest 的 -k 过滤（去掉通配符）
				if k := filterFor(line, tok); k != "" {
					args = append(args, "-k", k)
				}
				row = append(row, target{
					Kind: "pytest", Where: p, Args: args,
					Dir: "analysis", Source: m.Doc + " §7",
				})
			}
		}
		for _, t := range row {
			// key 必须含参数：同一个测试文件的**不同过滤词**是两个目标
			// （只按文件去重会把第二个丢掉 —— 静默少测）
			key := t.Where + "|" + strings.Join(t.Args, " ")
			if !seen[key] {
				seen[key] = true
				out = append(out, t)
			}
		}
		if len(row) > 0 {
			last = row
		}
	}
	// 兜底：§7 里没写清路径、但模块目录下确实有测试时，按目录跑（不漏测）
	if len(out) == 0 {
		if entries, err := os.ReadDir(filepath.Join(root, m.SrcDir)); err == nil {
			for _, en := range entries {
				if strings.HasSuffix(en.Name(), "_test.go") {
					out = append(out, target{
						Kind: "go", Where: "go test ./" + m.SrcDir,
						Args: []string{"go", "test", "-count=1", "-v", "./" + m.SrcDir},
						Dir:  ".", Source: m.Doc + " §7（未写明路径，按目录兜底）",
					})
					break
				}
			}
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 小工具
// ─────────────────────────────────────────────────────────────────────────────

func repoRoot() string {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	fatalOn(err, "定位仓库根失败（go list -m）")
	return strings.TrimSpace(string(out))
}

func loadScenarios(path string) []scenario {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw struct {
		Scenarios []scenario `json:"scenarios"`
	}
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	return raw.Scenarios
}

// statusLine 取模块文档头部状态行的原文（`| 状态 | … |`）。
func statusLine(doc string) string {
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "| 状态 |") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// firstLine 取状态行的前若干字符（报告里只显示一句，不搬整段）。
func firstLine(s string) string {
	runes := []rune(s)
	if len(runes) > 60 {
		return string(runes[:60]) + "…"
	}
	return s
}

func isExempt(sec string) bool {
	for _, w := range exemptWords {
		if strings.Contains(sec, w) {
			return true
		}
	}
	return false
}

// resolvePath 把文档里那种相对路径换算成仓库存根相对路径；认不出来就返回空串。
func resolvePath(tok string) string {
	if strings.HasPrefix(tok, "../") {
		// 模块文档在 docs/modules/ 下，`../../x` = 存根相对
		return filepath.ToSlash(filepath.Clean(filepath.Join("docs/modules", tok)))
	}
	if strings.HasPrefix(tok, "analysis/") || strings.HasPrefix(tok, "common/") || strings.HasPrefix(tok, "modules/") {
		return tok
	}
	return ""
}

// filterFor 取 `（test_xxx_*）` 里的过滤词，转成 pytest 的 `-k` 表达式。
//
// 两条纪律（都是踩过的坑）：
//
//	① `（` 必须**紧跟**在测试路径后面 —— 否则会把同一行里的 Markdown 链接当成过滤词
//	   （实测：`…test_worker.py` + `make analysis`（[ADR-0022](…)）会解析出 `[ADR-0022](…)`，
//	   pytest 直接报「Wrong expression passed to -k」）；
//	② 只有当每一段都像测试名（`[A-Za-z_][A-Za-z0-9_]*`）时才生成 `-k`，
//	   否则宁可不加过滤（跑整个文件）也不猜 —— 猜错就是假红。
func filterFor(line, tok string) string {
	idx := strings.Index(line, tok)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(line[idx+len(tok):])
	// 路径在文档里通常写成 `` `path`（`test_xxx_*`） `` —— 紧跟路径的是**收尾反引号**，
	// 所以要先吃掉它（只吃一个）再看后面是不是 `（`。
	// 这一条不吃掉的话，**所有**过滤词都会解析失败、静默退化成「跑整个文件」：
	// 用例数不再属于该模块，而表里看不出来。
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "`"))
	if !strings.HasPrefix(rest, "（") {
		return ""
	}
	closeIdx := strings.Index(rest, "）")
	if closeIdx < 0 {
		return ""
	}
	inner := rest[len("（"):closeIdx]
	var names []string
	for _, part := range strings.FieldsFunc(inner, func(r rune) bool {
		return r == '·' || r == ',' || r == '、' || r == ' '
	}) {
		name := strings.Trim(part, "`*")
		if name == "" {
			continue
		}
		// 允许文档写 `test_intent_*` 这种前缀写法：pytest 的 -k 本来就是**子串**匹配，
		// 去掉结尾的 `*` 即得等价（`test_intent` 会匹配 test_intent_* 全部）。
		// 不支持这一写法，文档里的过滤词就会整条失效 —— 跑整个文件、用例数也不是该模块的。
		trimmed := strings.TrimSuffix(name, "*")
		if trimmed == "" || !testName.MatchString(trimmed) {
			return "" // 有一项不像测试名 ⇒ 整条不加过滤，跑整个文件
		}
		names = append(names, trimmed)
	}
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, " or ")
}

// testName 是「看起来像测试函数名」的判据（pytest -k 只接标识符）。
var testName = regexp.MustCompile(`^test_[A-Za-z0-9_]+$`)

func dedup(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// targetKey 是「同一文件 + 同一组参数」的唯一键。
//
// 为什么不用 `Where`（文件名）当键：§7 里同一个文件常按不同过滤词列多次
// （例：`test_l4_modules.py`（`test_intent_*`）与（`test_strategy_*`）），
// 只按文件当键会让后一次的结果**覆盖**前一次 —— 用例数少算、失败也会被盖掉。
func targetKey(t target) string { return t.Where + "|" + strings.Join(t.Args, " ") }

func countCases(kind, out string) int {
	if kind == "go" {
		return strings.Count(out, "--- PASS:") + strings.Count(out, "--- FAIL:")
	}
	re := regexp.MustCompile(`(\d+) passed`)
	if m := re.FindStringSubmatch(out); m != nil {
		// 用 Atoi 而不是 Sscanf：Sscanf 的返回值必须处理（TB-14），而这里解析不出来的正确语义是 0
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 0
}

func summarize(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "FAIL") || strings.HasPrefix(line, "--- FAIL") ||
			strings.Contains(line, "failed") || strings.HasPrefix(line, "ok  ") {
			return line
		}
	}
	return strings.TrimSpace(strings.Split(out, "\n")[0])
}

func countFailures(all []evidence) int {
	n := 0
	for _, e := range all {
		if len(e.Problems) > 0 {
			n++
		}
	}
	return n
}

func report(all []evidence) {
	fmt.Printf("逐模块功能测试流程（模块清单解析自 docs/design/modules.md §1.1，共 %d 个）\n\n", len(all))
	fmt.Printf("%-20s %-6s %-4s %-4s %-6s %-12s %s\n", "模块", "层", "文档", "规则", "测试", "实测", "功能场景")
	fmt.Println(strings.Repeat("-", 104))
	for _, e := range all {
		doc, tests, ran := "✅", "✅", "—"
		if !e.DocOK {
			doc = "❌"
		}
		switch {
		case e.DocNote != "":
			tests = "豁免"
		case len(e.Targets) == 0:
			tests = "❌"
		}
		if len(e.Results) > 0 {
			cases, ok := 0, true
			for _, r := range e.Results {
				cases += r.Cases
				ok = ok && r.OK
			}
			if ok {
				ran = fmt.Sprintf("✅ %d 例", cases)
			} else {
				ran = "❌ 失败"
			}
		}
		fmt.Printf("%-20s %-6s %-4s %-4d %-6s %-12s %s\n",
			e.Module.Name, e.Module.Layer, doc, len(e.Rules), tests, ran, strings.Join(e.Scenarios, " · "))
		if len(e.Problems) > 0 {
			for _, p := range e.Problems {
				fmt.Printf("%-20s   ↳ %s\n", "", p)
			}
		}
	}
	fmt.Println(strings.Repeat("-", 104))
	failures := countFailures(all)
	if failures == 0 {
		fmt.Println("✅ 全部模块的证据链齐备（文档 · 规则依据 · 测试目标 · 功能场景）")
		return
	}
	fmt.Printf("✗ %d 个模块的证据链有问题（见上面的 ↳）\n", failures)
	sort.Strings([]string{})
}

func fatalOn(err error, msg string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s：%v\n", msg, err)
		os.Exit(2)
	}
}
