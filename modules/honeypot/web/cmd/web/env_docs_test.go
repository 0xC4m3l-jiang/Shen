package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 本文件的职责：**守住「环境变量 ↔ 模块文档」的一致性**。
//
// 为什么这个模块也需要它（`K-30`，2026-10-11 抓到）：`SHEN_WEB_TRUSTED_PROXIES` 曾经
// **写在模块文档里、写在 compose.yaml 里、也有单测**，但唯一的生产入口从不读它 ——
// 于是「文档写了 / 配置设了 / 测试绿了」同时成立，而线上行为是默认值（登录限流按代理 IP 计数）。
// 那条缺陷本可以被这个测试在写下文档的那一刻挡住。
//
// 判据两个方向都要成立：
//
//	代码 → 文档：每个可用的开关都必须被文档说明（否则等于隐藏开关）；
//	文档 → 代码：文档写了的开关必须真的存在（否则是**假承诺**）。
//
// 文档不在发布树里（`docs/` 与代码分开发布）⇒ 缺文件时**跳过并打印原因**，不假装通过。

var envPattern = regexp.MustCompile(`SHEN_WEB_[A-Z0-9_]+`)

// moduleDocPath 是模块文档（本包在 modules/honeypot/web/cmd/web）。
const moduleDocPath = "../../../../../docs/modules/honeypot-web.md"

func TestEnvVarsMatchModuleDoc(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(moduleDocPath))
	if err != nil {
		t.Skipf("模块文档不在本机（发布树不带 docs/）⇒ 跳过环境变量一致性检查：%v", err)
	}
	doc := map[string]bool{}
	for _, name := range envPattern.FindAllString(string(raw), -1) {
		doc[name] = true
	}

	code := map[string]bool{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(filepath.Clean(name))
		if rerr != nil {
			t.Fatal(rerr)
		}
		for _, v := range envPattern.FindAllString(string(src), -1) {
			code[v] = true
		}
	}
	if len(code) == 0 {
		t.Fatal("没扫到任何环境变量：本用例的扫描路径可能已失效（会静默失去保护）")
	}

	// ① 代码 → 文档
	var undocumented []string
	for name := range code {
		if !doc[name] {
			undocumented = append(undocumented, name)
		}
	}
	sort.Strings(undocumented)
	if len(undocumented) > 0 {
		t.Errorf("这些环境变量在代码里可用但**文档未登记**（等于隐藏开关）：%v\n"+
			"把它们写进 §3.1 的开关全表。", undocumented)
	}

	// ② 文档 → 代码
	var phantom []string
	for name := range doc {
		if !code[name] {
			phantom = append(phantom, name)
		}
	}
	sort.Strings(phantom)
	if len(phantom) > 0 {
		t.Errorf("这些环境变量**文档写了、代码里不存在**（假承诺，比不写更糟）：%v\n"+
			"要么接线，要么从文档里删掉。", phantom)
	}
}
