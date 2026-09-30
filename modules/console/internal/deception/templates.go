package deception

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed templates/*.json
var templateFS embed.FS

// HoneypotTemplate 是一份内置蜜罐登记模板（预填表单，不是蜜罐实现 —— ADR-0011）。
type HoneypotTemplate struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	Name           string   `json:"name"`
	DefaultPort    int      `json:"default_port"`
	SuggestedName  string   `json:"suggested_name"`
	Description    string   `json:"description"`
	DecoyTemplates []string `json:"decoy_templates"`
}

// DecoyTemplate 是一份内置诱饵模板（推荐路径 + 内容模板标识 + 适配的蜜罐类型）。
type DecoyTemplate struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Name          string   `json:"name"`
	Path          string   `json:"path"`
	Content       string   `json:"content"`
	HoneypotTypes []string `json:"honeypot_types"`
	Description   string   `json:"description"`
}

// InjectTemplate 是一份内置注入片段模板。
type InjectTemplate struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Snippet     string `json:"snippet"`
	Marker      string `json:"marker"`
	Description string `json:"description"`
}

// Templates 是内置模板库（只读）。
type Templates struct {
	Honeypots []HoneypotTemplate `json:"honeypots"`
	Decoys    []DecoyTemplate    `json:"decoys"`
	Injects   []InjectTemplate   `json:"injects"`
}

// LoadTemplates 读取内嵌模板并自检枚举（模板写错属于构建缺陷，启动即失败）。
func LoadTemplates() (Templates, error) {
	var t Templates
	for name, dst := range map[string]any{
		"templates/honeypots.json": &t.Honeypots,
		"templates/decoys.json":    &t.Decoys,
		"templates/injects.json":   &t.Injects,
	} {
		raw, err := templateFS.ReadFile(name)
		if err != nil {
			return Templates{}, err
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			return Templates{}, fmt.Errorf("deception: 内置模板 %s 解析失败：%w", name, err)
		}
	}
	for _, h := range t.Honeypots {
		if !contains(HoneypotTypes, h.Type) {
			return Templates{}, fmt.Errorf("deception: 蜜罐模板 %s 的类型 %q 未登记", h.ID, h.Type)
		}
	}
	for _, d := range t.Decoys {
		if !contains(DecoyKinds, d.Kind) || NormalizePath(d.Path) != d.Path {
			return Templates{}, fmt.Errorf("deception: 诱饵模板 %s 非法（kind=%q path=%q）", d.ID, d.Kind, d.Path)
		}
	}
	for _, i := range t.Injects {
		if !contains(InjectKinds, i.Kind) {
			return Templates{}, fmt.Errorf("deception: 注入模板 %s 的分类 %q 未登记", i.ID, i.Kind)
		}
	}
	return t, nil
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
