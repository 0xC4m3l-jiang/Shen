package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 本文件的职责：**守住「环境变量 ↔ 文档」的一致性**。
//
// 为什么需要它（本轮审计抓到的真实缺陷）：`SHEN_PROXY_DEGRADED_CACHE_TTL` /
// `SHEN_PROXY_DECOY_LEASE` / `SHEN_PROXY_FORWARD_CREDENTIALS` 曾经**只在文档里存在**、代码从未接线 ——
// 运维照着文档设了，什么也不发生，而且没有任何信号。反向也有：`SHEN_PROXY_CACHE_TTL` 等 8 个变量
// 在代码里可用却从未登记，运维只能读源码才知道。
//
// 判据是两个方向都要成立：
//
//	代码 → 文档：每个可用的开关都必须被文档说明（否则等于隐藏开关）；
//	文档 → 代码：文档写了的开关必须真的存在（否则是**假承诺**，比没写更糟）。
//
// 文档不在发布树里（`docs/` 与代码分开发布）⇒ 缺文件时**跳过并打印原因**，不假装通过。

var envPattern = regexp.MustCompile(`SHEN_PROXY_[A-Z0-9_]+`)

// moduleDocPath 是模块文档（本包在 modules/deception/proxy/cmd/proxy）。
const moduleDocPath = "../../../../../docs/modules/adapter-proxy.md"

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
			"把它们写进 §3.3 的开关全表。", undocumented)
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
