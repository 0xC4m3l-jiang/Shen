package policy

// SHEN-M4-03：把「服务」提升为一级对象 —— 配置段 `services:` 的装载、校验与载荷投影。
//
// 这一层是整条服务维度的**唯一来源**：内容按服务分组（M4-01）、改道按服务选池（M4-02）、
// 审计按服务归属（后续）。所以这里既测「正样本能被解析」，也测「写错了会在启动期拒绝」——
// 配置错误静默通过，比配置错误直接失败危险得多。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	policyv1 "shen/common/api/policy/v1"
	"shen/common/core/internal/store"
)

// servicesYAML 在最小合法配置后面追加 `services:` 段。
func servicesYAML(block string) string {
	return validYAML + block
}

func TestLoadServicesParsesAndProjects(t *testing.T) {
	l := mustLoad(t, servicesYAML(`services:
  - id: "site-a"
    hosts: ["A.Example.COM", "a.example.com"]
    mirage_backend: "hp-a"
  - id: "site-b"
    hosts: ["*.b.example.com"]
`))

	got, err := l.Services(context.Background())
	if err != nil {
		t.Fatalf("取接入单元表失败：%v", err)
	}
	if len(got) != 2 {
		t.Fatalf("接入单元数 = %d，期望 2", len(got))
	}
	if got[0].ID != "site-a" || got[0].MirageBackend != "hp-a" {
		t.Fatalf("site-a 解析不正确：%+v", got[0])
	}
	// Host 归一化到小写（边缘侧按小写匹配，两边口径必须一致）。
	for _, h := range got[0].Hosts {
		if h != strings.ToLower(h) {
			t.Fatalf("Host 未归一化为小写：%q", h)
		}
	}
	if got[1].MirageBackend != "" {
		t.Fatalf("未声明 mirage_backend 的服务不该带后端：%+v", got[1])
	}
}

func TestLoadServicesIsOptional(t *testing.T) {
	// 旧配置（没有 services 段）必须原样可装载 —— 行为与改造前一致。
	l := mustLoad(t, validYAML)
	got, err := l.Services(context.Background())
	if err != nil {
		t.Fatalf("未配置 services 时不该报错：%v", err)
	}
	if len(got) != 0 {
		t.Fatalf("未配置 services 时应为空，实际 %+v", got)
	}
}

func TestLoadServicesRejectsBadInput(t *testing.T) {
	cases := []struct {
		name, block, want string
	}{
		{
			name: "缺 id",
			block: `services:
  - hosts: ["a.example.com"]
`,
			want: "services[0].id",
		},
		{
			name: "id 重复",
			block: `services:
  - id: "site-a"
    hosts: ["a.example.com"]
  - id: "SITE-A"
    hosts: ["a2.example.com"]
`,
			want: "重复",
		},
		{
			name: "缺 hosts（配了等于没配）",
			block: `services:
  - id: "site-a"
`,
			want: "services[0].hosts",
		},
		{
			name: "hosts 里有空串",
			block: `services:
  - id: "site-a"
    hosts: ["a.example.com", "  "]
`,
			want: "hosts[1]",
		},
		{
			name: "通配不在最左标签",
			block: `services:
  - id: "site-a"
    hosts: ["a*.example.com"]
`,
			want: "最左标签",
		},
		{
			name: "mirage_backend 空串",
			block: `services:
  - id: "site-a"
    hosts: ["a.example.com"]
    mirage_backend: "  "
`,
			want: "mirage_backend",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(strings.NewReader(servicesYAML(c.block)))
			if err == nil {
				t.Fatalf("应当拒绝（%s），却装载成功", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("错误信息应点名 %q，实际：%v", c.want, err)
			}
		})
	}
}

// TestPullProjectsServices 断言接入单元表真的进了**下发载荷**（否则边缘拿不到，M4-01/M4-02 空转）。
func TestPullProjectsServices(t *testing.T) {
	srv := NewServer(mustLoad(t, servicesYAML(`services:
  - id: "site-b"
    hosts: ["b.example.com", "a.example.com"]
    mirage_backend: "hp-b"
`)), store.NewPolicyMemory())

	snap, err := srv.Pull(context.Background(), &policyv1.PolicyPullRequest{})
	if err != nil {
		t.Fatalf("Pull 失败：%v", err)
	}
	var doc struct {
		Services []struct {
			ID            string   `json:"id"`
			Hosts         []string `json:"hosts"`
			MirageBackend string   `json:"mirage_backend"`
		} `json:"services"`
	}
	if err := json.Unmarshal(snap.GetPayload(), &doc); err != nil {
		t.Fatalf("载荷不是合法 JSON：%v", err)
	}
	if len(doc.Services) != 1 {
		t.Fatalf("载荷里应有 1 个接入单元，实际 %d", len(doc.Services))
	}
	svc := doc.Services[0]
	if svc.ID != "site-b" || svc.MirageBackend != "hp-b" {
		t.Fatalf("接入单元投影不正确：%+v", svc)
	}
	// Host 必须**排序**后下发：同内容同校验和（AR-30 精神），也让适配器建表可复现。
	if len(svc.Hosts) != 2 || svc.Hosts[0] != "a.example.com" || svc.Hosts[1] != "b.example.com" {
		t.Fatalf("Host 应排序后下发：%v", svc.Hosts)
	}
}

// TestPullOmitsServicesWhenNotConfigured 断言未配置时字段**不出现**（旧载荷字节不变）。
func TestPullOmitsServicesWhenNotConfigured(t *testing.T) {
	srv := NewServer(mustLoad(t, validYAML), store.NewPolicyMemory())
	snap, err := srv.Pull(context.Background(), &policyv1.PolicyPullRequest{})
	if err != nil {
		t.Fatalf("Pull 失败：%v", err)
	}
	if strings.Contains(string(snap.GetPayload()), `"services"`) {
		t.Fatalf("未配置 services 时不应出现该字段：%s", snap.GetPayload())
	}
}
