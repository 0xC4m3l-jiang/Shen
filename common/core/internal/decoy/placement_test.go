package decoy

import (
	"context"
	"strings"
	"testing"

	"shen/common/core/internal/contract"
)

// ── 解耦验证：领域输出「意图」，字节由媒介模板产出（D05）──────────────────────
//
// 本文件锁三件事：
//  ① 意图是**格式无关的数据**（媒介 / 形态 / 目标 / 值），领域层不再内联 Nginx / SQL / HTML 字节；
//  ② 渲染结果与改造前**逐字节相同**（解耦不得改变对外产出 —— 「保存功能实现」）；
//  ③ 领域层能产出的每个 (媒介, 形态) 都有模板（漏加模板会在门禁里红，不会静默丢投放）。

// allKinds 是领域层能产出的全部诱饵形态。
var allKinds = []contract.DecoyKind{
	contract.DecoyDeveloperAPI,
	contract.DecoyInstructionFile,
	contract.DecoyMCP,
	contract.DecoyDataset,
	contract.DecoyBait,
}

// TestIntentsAreFormatFree 断言意图是数据：五项字段齐备、不含格式字节。
func TestIntentsAreFormatFree(t *testing.T) {
	ctx := context.Background()
	for _, k := range allKinds {
		e := mustEngine(t, newStub(asset("a", k, "/_bait/x", true)))
		intents, err := e.Intents(ctx, "a")
		if err != nil {
			t.Fatalf("%s：Intents 失败：%v", k, err)
		}
		if len(intents) == 0 {
			t.Fatalf("%s：应有投放意图（投放才是最后一公里）", k)
		}
		for _, it := range intents {
			if it.Media == "" || it.Form == "" || it.Target == "" {
				t.Errorf("%s：意图缺少媒介/形态/目标：%+v", k, it)
			}
			if strings.ContainsAny(string(it.Media), "<>{}\"'") {
				t.Errorf("%s：媒介名不得是格式片段：%q", k, it.Media)
			}
		}
	}
}

// TestRenderGoldenBytes 是「解耦不得改变产出」的判据：逐字节对照改造前的片段。
//
// 这些字符串就是改造前 `placements()` 的产物（运营实际粘贴的东西），改一个字都必须是有意的。
func TestRenderGoldenBytes(t *testing.T) {
	ctx := context.Background()
	want := map[contract.DecoyKind][]string{
		contract.DecoyDeveloperAPI: {
			"<!-- 页脚开发者 API（可见） -->\n<section id=\"dev-api\"><a href=\"/_bait/x\">Developer API</a></section>",
			"# nginx：把开发者 API 暴露给自动化客户端\nlocation /_bait/x { proxy_pass tmpl; }",
		},
		contract.DecoyInstructionFile: {
			"# 投放位置：仓库根 / 文档目录（/_bait/x）",
			"echo 'tmpl' > ./_bait/x",
		},
		contract.DecoyMCP: {
			"// MCP 诱饵：把工具服务端点写进开发者文档\n{\"mcpServers\":{\"site-tools\":{\"url\":\"/_bait/x\"}}}",
		},
		contract.DecoyDataset: {
			"// 消耗战数据集：在 JS / 手册中暴露分页接口\nfetch('/_bait/x?page=1')",
		},
		contract.DecoyBait: {
			"-- 投放位置：配置包 / 手册\nINSERT INTO app_accounts(login, secret) VALUES ('/_bait/x', 'tmpl');",
			"<a href=\"/_bait/x\" style=\"display:none\">下载</a>",
		},
	}
	for _, k := range allKinds {
		e := mustEngine(t, newStub(asset("a", k, "/_bait/x", true)))
		got, err := e.Placements(ctx, "a")
		if err != nil {
			t.Fatalf("%s：Placements 失败：%v", k, err)
		}
		expected := want[k]
		if len(got) != len(expected) {
			t.Fatalf("%s：片段数期望 %d，得到 %d（%q）", k, len(expected), len(got), got)
		}
		for i := range expected {
			if got[i] != expected[i] {
				t.Errorf("%s：第 %d 条片段与改造前不同\n期望：%q\n实际：%q", k, i+1, expected[i], got[i])
			}
		}
	}
}

// TestEveryIntentHasTemplate 断言领域层产出的每个 (媒介, 形态) 都在模板表里。
//
// 为什么必须有它：`Render` 对没有模板的形态是**跳过**（保持最小实现）。
// 若没有这条断言，「新增一种投放形态但忘了加模板」会表现为「静默少投一条」，
// 而运营看到的是「片段看起来正常」—— 这里把它变成红灯。
func TestEveryIntentHasTemplate(t *testing.T) {
	for _, k := range allKinds {
		for _, it := range intentsFor(contract.DecoyAsset{Kind: k, Path: "/x", Content: "c"}) {
			key := templateKey{it.Media, it.Form}
			if _, ok := templates[key]; !ok {
				t.Errorf("%s：意图 %+v 没有对应模板（模板表缺 %s/%s）", k, it, it.Media, it.Form)
			}
			if _, ok := commentPrefix[it.Media]; it.Note != "" && !ok {
				t.Errorf("%s：媒介 %s 有说明文字但没有注释语法", k, it.Media)
			}
		}
	}
}

// TestRenderSkipsEmptyTarget 断言不投到空位置（宁可不投，也不投一个空目标）。
func TestRenderSkipsEmptyTarget(t *testing.T) {
	if got := Render([]PlacementIntent{{Media: MediaHTML, Form: formLinkSection, Target: "  "}}); len(got) != 0 {
		t.Fatalf("空目标应被跳过，得到 %q", got)
	}
}

// TestIntentsUnknownKindIsEmpty 断言未登记形态不产出意图（而不是产出半个片段）。
func TestIntentsUnknownKindIsEmpty(t *testing.T) {
	if got := intentsFor(contract.DecoyAsset{Kind: contract.DecoyKind(200), Path: "/x"}); len(got) != 0 {
		t.Fatalf("未登记形态不应产出意图，得到 %+v", got)
	}
}
