// Command licensecheck 审计依赖许可，拦住传染性或限制性许可。
//
// 依据 TB-16：依赖必须经许可与漏洞审计；禁止引入 AGPL / SSPL / BSL 等
// 传染性或限制性许可的依赖。理由不是洁癖 —— 通过 HTTP 提供服务即触发
// AGPL 的披露义务，那会把这个产品的源码变成必须公开的。
//
// 两条设计决策：
//
//  1. **只审真正参与构建的模块。** `go list -m all` 给出的是完整模块图，
//     里面有大量不进二进制的间接依赖。审它们没有意义，还会把门禁淹掉。
//
//  2. **白名单式判定。** 认得出的宽松许可放行、认得出的限制性许可拦下，
//     其余一律报「需人工判定」并以失败退出。反过来做（黑名单式）会让
//     没见过的许可静默通过 —— 许可识别错的代价是法律风险，不是构建失败。
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// 许可判定
// ─────────────────────────────────────────────────────────────────────────────

// verdict 是判定结果。
type verdict int

const (
	ok         verdict = iota
	unknown            // 认不出 —— 必须人工判定
	restricted         // 认得出且禁止 —— 确定失败
)

func (v verdict) String() string {
	switch v {
	case ok:
		return "允许"
	case restricted:
		return "禁止"
	default:
		return "需人工判定"
	}
}

// sig 是一条许可特征：在许可文本里找到任何 sub 就判定为该许可。
//
// **顺序即优先级**：AGPL 必须排在 GPL 之前、LGPL 排在 GPL 之前，
// 否则 "GNU AFFERO GENERAL PUBLIC LICENSE" 会被 GPL 那条先命中。
type sig struct {
	id     string
	subs   []string
	effect verdict
	why    string
}

var signatures = []sig{
	// ── 明确的传染性 / 限制性许可：拦下 ──────────────────────────────────
	{"AGPL-3.0", []string{"GNU AFFERO GENERAL PUBLIC LICENSE"}, restricted,
		"通过网络提供服务即触发源码披露义务"},
	{"SSPL", []string{"Server Side Public License"}, restricted,
		"要求公开整个服务栈的源码"},
	{"BUSL-1.1", []string{"Business Source License", "BUSINESS SOURCE LICENSE"}, restricted,
		"商用受限，若干年后才转开源"},
	{"Elastic-2.0", []string{"ELASTIC LICENSE"}, restricted,
		"禁止把本产品作为托管服务提供"},
	{"Commons-Clause", []string{"Commons Clause"}, restricted,
		"禁止转售"},
	{"CC-BY-NC", []string{"NonCommercial", "NONCOMMERCIAL", "Non-Commercial"}, restricted,
		"禁止商用"},
	{"JSON", []string{"Good, not Evil"}, restricted,
		"许可条款含用途限制，不是自由许可"},
	{"LGPL", []string{"GNU LESSER GENERAL PUBLIC LICENSE"}, restricted,
		"Go 是静态链接，LGPL 要求的可重链接无法满足"},
	{"GPL-2.0", []string{"GNU GENERAL PUBLIC LICENSE", "Version 2, June 1991"}, restricted,
		"强传染，静态链接即需开源"},
	{"GPL-3.0", []string{"GNU GENERAL PUBLIC LICENSE", "Version 3, 29 June 2007"}, restricted,
		"强传染，静态链接即需开源"},
	{"RPL-1.5", []string{"Reciprocal Public License"}, restricted, "传染性"},
	{"OSL-3.0", []string{"Open Software License"}, restricted, "传染性"},
	{"CPAL-1.0", []string{"Common Public Attribution License"}, restricted, "传染性 + 署名义务"},

	// ── 宽松许可：放行 ───────────────────────────────────────────────────
	{"Apache-2.0", []string{"Apache License", "apache.org/licenses/LICENSE-2.0"}, ok,
		"宽松，需保留声明"},
	{"MIT", []string{"MIT License", "Permission is hereby granted, free of charge"}, ok,
		"宽松，需保留声明"},
	{"BSD-3-Clause", []string{"Redistribution and use in source and binary forms", "Neither the name"}, ok,
		"宽松，禁止用作者名背书"},
	{"BSD-2-Clause", []string{"Redistribution and use in source and binary forms"}, ok, "宽松"},
	{"ISC", []string{"Permission to use, copy, modify, and/or distribute this software for any purpose"}, ok, "宽松，等价 MIT"},
	{"MPL-2.0", []string{"Mozilla Public License"}, ok, "文件级弱传染，只影响其自身文件"},
	{"Unlicense", []string{"This is free and unencumbered software released into the public domain"}, ok, "等同公共领域"},
	{"0BSD", []string{"Zero-Clause BSD"}, ok, "宽松"},
	{"Zlib", []string{"altered source versions must be plainly marked"}, ok, "宽松"},
	{"PSF-2.0", []string{"PYTHON SOFTWARE FOUNDATION LICENSE"}, ok, "宽松"},
	{"CC0-1.0", []string{"CC0 1.0 Universal", "Creative Commons Legal Code"}, ok, "放弃著作权"},
	{"BlueOak-1.0.0", []string{"Blue Oak Model License"}, ok, "宽松"},
	{"WTFPL", []string{"DO WHAT THE FUCK YOU WANT"}, ok, "无限制"},
	{"X11", []string{"X11 License"}, ok, "宽松"},
}

// licenseFileNames 是探针文件名（不区分大小写的前缀匹配）。
var licenseFileNames = []string{"LICENSE", "LICENCE", "COPYING", "NOTICE"}

// classify 读一个目录下全部许可文件，给出判定。
//
// 取**最严格**的那条：双许可（如 Apache-2.0 + MIT）取宽松的没问题，
// 但 Apache-2.0 + GPL 这种组合必须取 GPL 侧 —— 用得上的一定是较严的那份。
// looksLikeText 判断一个文件是否**像许可文本**（而不是可执行文件 / 大二进制）。
//
// 判据取最保守的两条：大小 ≤ 512 KiB，且前 4 KiB 内没有 NUL 字节。
// 许可文本一定是小体积纯文本；反过来（把二进制当文本读）会产生无法解释的假阳性。
func looksLikeText(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.Size() > 512*1024 {
		return false
	}
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	for _, b := range buf[:n] {
		if b == 0 {
			return false
		}
	}
	return true
}

func classify(dir string) (id string, v verdict, why string, files []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", unknown, fmt.Sprintf("读不到目录：%v", err), nil
	}
	var texts []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		up := strings.ToUpper(e.Name())
		for _, want := range licenseFileNames {
			if strings.HasPrefix(up, want) {
				full := filepath.Join(dir, e.Name())
				if !looksLikeText(full) {
					// 前缀匹配会撞上同名可执行文件（本仓库根的预编译 `licensecheck` 就是
					// `LICENSE…` 开头），从二进制里搜许可串会得到**假阳性** —— 实测它把
					// 我们自己的许可证误报成 AGPL-3.0（规则表字符串就嵌在二进制里）。
					continue
				}
				b, rerr := os.ReadFile(full)
				if rerr != nil {
					continue
				}
				texts = append(texts, string(b))
				files = append(files, e.Name())
				break
			}
		}
	}
	if len(texts) == 0 {
		return "", unknown, "模块里找不到任何许可文件", nil
	}

	// 取**最严格**的那条：多份许可（双许可）时，用得上的一定是较严的那份。
	//
	// 注意不能用「默认值 + 比大小」来挑 —— 若默认值的等级恰好等于命中项的等级，
	// 比较永远不成立，结果会退化成「认不出」，也就是把许可静默放过。
	var worst *sig
	for _, t := range texts {
		s, hit := match(t)
		if !hit {
			continue
		}
		if worst == nil || s.effect > worst.effect {
			hit := s
			worst = &hit
		}
	}
	if worst == nil {
		return "", unknown, "许可文本无法识别 —— 必须人工判定后加入白名单", files
	}
	return worst.id, worst.effect, worst.why, files
}

// match 按 signatures 的顺序找第一条命中的特征。
func match(text string) (sig, bool) {
	for _, s := range signatures {
		for _, sub := range s.subs {
			if strings.Contains(text, sub) {
				return s, true
			}
		}
	}
	return sig{}, false
}

// ─────────────────────────────────────────────────────────────────────────────
// 依赖枚举
// ─────────────────────────────────────────────────────────────────────────────

// buildModule 是一个真正参与构建的模块。
type buildModule struct {
	Path    string // 模块路径
	Version string // 版本
	Dir     string // 本地缓存目录（空 = 未下载）
	Own     bool   // 是不是本项目自己
}

// buildModules 用包级依赖反推模块 —— 只有进了二进制的东西才需要审。
func buildModules() ([]buildModule, error) {
	out, err := exec.Command("go", "list", "-deps",
		"-f", "{{if .Module}}{{.Module.Path}}\t{{.Module.Version}}{{end}}", "./...").Output()
	if err != nil {
		return nil, fmt.Errorf("go list -deps 失败：%w", err)
	}
	seen := map[string]buildModule{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		path, version, _ := strings.Cut(line, "\t")
		if _, dup := seen[path]; dup {
			continue
		}
		seen[path] = buildModule{Path: path, Version: version}
	}

	self, err := selfModulePath()
	if err != nil {
		return nil, err
	}

	mods := make([]buildModule, 0, len(seen))
	for _, m := range seen {
		m.Own = m.Path == self
		if !m.Own {
			dir, derr := moduleDir(m.Path)
			if derr != nil {
				m.Dir = ""
			} else {
				m.Dir = dir
			}
		}
		mods = append(mods, m)
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].Path < mods[j].Path })
	return mods, nil
}

func selfModulePath() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Path}}").Output()
	if err != nil {
		return "", fmt.Errorf("取本模块路径失败：%w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// moduleDir 取模块在本地缓存里的目录。未下载时返回错误 —— 不静默当成「无许可文件」。
// moduleDir 返回模块在**本地**的目录。
//
// 优先 `vendor/`：仓库带 vendor 时，被编译的就是那份副本，许可证文件也在里面；
// 而且这让许可审计**不再依赖 Go 模块缓存**（离线也能跑 —— 本机访问不了 proxy.golang.org，
// 没有 vendor 时连依赖都下不动）。vendor 里没有才回退到模块缓存。
func moduleDir(path string) (string, error) {
	if dir, ok := vendorDir(path); ok {
		return dir, nil
	}
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", path).Output()
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", errors.New("模块未下载到本地缓存，且仓库里没有 vendor/ 副本")
	}
	return dir, nil
}

// vendorDir 判断 vendor/<模块路径> 是否为目录（Go 的 vendor 布局与模块路径一一对应）。
func vendorDir(path string) (string, bool) {
	dir := filepath.Join("vendor", filepath.FromSlash(path))
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, true
	}
	return "", false
}

// ─────────────────────────────────────────────────────────────────────────────
// 主流程
// ─────────────────────────────────────────────────────────────────────────────

// row 是一行审计结果，也用来生成台账。
type row struct {
	Module  string
	Version string
	License string
	Verdict verdict
	Note    string
	Files   []string
	Lang    string // langGo / langPython：台账里分节，审计里分组
	Own     bool   // 本项目自身：它没有许可证不是「依赖问题」
}

// 依赖来源的语言标签。台账与审计输出按它分节 —— 两类的判定口径不同，混在一张表里说不清。
const (
	langGo     = "go"
	langPython = "python"
)

func main() {
	ledger := flag.Bool("ledger", false, "输出 markdown 台账到标准输出，不做审计")
	pyLock := flag.String("py-lock", "analysis/requirements.txt", "Python 运行期依赖的锁文件")
	pyVenv := flag.String("py-venv", "analysis/.venv", "Python 虚拟环境目录（读已安装发行版的元数据）")
	flag.Parse()

	mods, err := buildModules()
	if err != nil {
		fail("枚举依赖失败：%v", err)
	}
	// 仓库根：门禁从仓库根调用本工具（Makefile 里就是 `go run ./scripts/licensecheck`）。
	// 读不到就退化为「未声明」提示，不因为读路径失败而误报依赖问题。
	root, rerr := os.Getwd()
	if rerr != nil {
		root = ""
	}
	goRows := goModuleRows(mods, root)

	// Python 侧失败 = 审计未执行完毕 = 门禁失败（见 python.go 文件头设计决策 1/3）。
	pyRowList, err := pyRows(*pyLock, *pyVenv)
	if err != nil {
		fail("Python 依赖审计未完成：%v", err)
	}

	if *ledger {
		printLedger(goRows, pyRowList)
		return
	}
	audit(goRows, pyRowList)
}

// goModuleRows 把 Go 模块枚举结果转成审计行。`root` 是本仓库根（读本项目自己的 LICENSE 用）。
func goModuleRows(mods []buildModule, root string) []row {
	var rows []row
	for _, m := range mods {
		if m.Own {
			// 本项目自己：**读根目录的 LICENSE**（与依赖同一套识别规则）。
			// 为什么要读而不是写死：许可证一旦定下来（2026-09-23 定为 Apache-2.0），
			// 写死的「未声明」就变成一条**过期的信号** —— 门禁天天报一个已经不存在的问题，
			// 人就会学会忽略它（这比不检查更糟）。
			if root != "" {
				id, v, why, files := classify(root)
				if id != "" {
					rows = append(rows, row{
						Module: m.Path, Version: "（本项目）", License: id, Own: true, Lang: langGo,
						Verdict: v, Note: fmt.Sprintf("%s（%s）", why, strings.Join(files, ", ")),
						Files: files,
					})
					continue
				}
			}
			rows = append(rows, row{
				Module: m.Path, Version: "（本项目）", License: "未声明", Own: true, Lang: langGo,
				Verdict: unknown, Note: "本项目自身尚无 LICENSE 文件；属产品决策，交付前必须定",
			})
			continue
		}
		if m.Dir == "" {
			rows = append(rows, row{
				Module: m.Path, Version: m.Version, License: "未知", Lang: langGo,
				Verdict: unknown, Note: "模块未下载到本地缓存，无法判定",
			})
			continue
		}
		id, v, why, files := classify(m.Dir)
		if id == "" {
			id = "未识别"
		}
		rows = append(rows, row{
			Module: m.Path, Version: m.Version, License: id, Lang: langGo,
			Verdict: v, Note: why, Files: files,
		})
	}
	return rows
}

// audit 打印审计结果并决定退出码。
func audit(goRows, pyRows []row) {
	fmt.Printf("依赖许可审计：Go 模块 %d 个（参与构建）· Python 运行期依赖 %d 个（含传递闭包）\n\n",
		len(goRows), len(pyRows))

	// 本项目自身缺 LICENSE 只提示、不失败（不是 TB-16 的管辖范围）
	var bad []row
	for _, group := range []struct {
		title string
		rows  []row
	}{{"Go 模块", goRows}, {"Python 运行期依赖", pyRows}} {
		fmt.Printf("%s（%d）\n", group.title, len(group.rows))
		for _, r := range group.rows {
			mark := "✓"
			if r.Verdict != ok {
				mark = "✗"
			}
			fmt.Printf("  %s %-52s %-16s %s\n", mark, r.Module, r.License, r.Verdict)
			if r.Verdict != ok {
				fmt.Printf("      %s\n", r.Note)
				if !r.Own {
					bad = append(bad, r)
				}
			}
		}
		fmt.Println()
	}

	if len(bad) > 0 {
		fmt.Fprintf(os.Stderr, "许可审计发现 %d 个问题：\n\n", len(bad))
		for _, r := range bad {
			fmt.Fprintf(os.Stderr, "  ✗ %s %s\n    %s：%s\n\n",
				r.Module, r.Version, r.Verdict, r.Note)
		}
		fmt.Fprintln(os.Stderr, "禁止的许可必须换依赖；「需人工判定」的经评审确认后加入")
		fmt.Fprintln(os.Stderr, "scripts/licensecheck 的登记表（Go 侧 signatures · Python 侧 spdxVerdicts），")
		fmt.Fprintln(os.Stderr, "并在注释里写清依据。")
		os.Exit(1)
	}
	fmt.Println("许可审计通过：没有传染性或限制性许可。")
}

func printLedger(goRows, pyRows []row) {
	fmt.Println("# 依赖许可台账")
	fmt.Println()
	fmt.Println("> ⚠️ **本文件由 `make license-ledger` 生成，禁止手改。**")
	fmt.Println("> 依据 `TB-16`（依赖必须经许可与漏洞审计）。审计逻辑见 [`../../scripts/licensecheck/`](../../scripts/licensecheck/)；")
	fmt.Println("> 门禁项见 `make gate`。")
	fmt.Println()
	fmt.Println("## 1. Go 模块（参与构建）")
	fmt.Println()
	printTable("模块", goRows)
	fmt.Println()
	fmt.Println("**判定口径**：只审**真正参与构建**的模块（`go list -deps` 反推），")
	fmt.Println("完整模块图里的间接依赖不进二进制，不需要审。")
	fmt.Println()
	fmt.Println("## 2. Python 运行期依赖（L4 分析层）")
	fmt.Println()
	printTable("发行版", pyRows)
	fmt.Println()
	fmt.Println("**判定口径**：从 `analysis/requirements.txt` 出发的**运行期传递闭包**；")
	fmt.Println("开发期依赖（`analysis/requirements-dev.txt`）不进交付物，与 Go 侧同口径不审。")
	fmt.Println("许可声明取自已安装发行版的 `*.dist-info/METADATA`（`License-Expression` > `Classifier` > `License`）；")
	fmt.Println("锁文件与环境的版本不一致即失败 —— 不允许拿 A 版本的元数据审定 B 版本的许可。")
	fmt.Println()
	fmt.Println("认不出的许可一律报「需人工判定」并让门禁失败 —— 许可识别错的代价是法律风险，")
	fmt.Println("比构建失败严重得多，因此宁可多报一次。")
}

func printTable(firstCol string, rows []row) {
	fmt.Printf("| %s | 版本 | 许可 | 判定 | 说明 |\n", firstCol)
	fmt.Println("| --- | --- | --- | --- | --- |")
	for _, r := range rows {
		fmt.Printf("| `%s` | %s | %s | %s | %s |\n",
			r.Module, r.Version, r.License, r.Verdict, r.Note)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "licensecheck: "+format+"\n", args...)
	fmt.Fprintln(os.Stderr, "审计未执行完毕 —— 视为失败，不静默放行。")
	os.Exit(1)
}
