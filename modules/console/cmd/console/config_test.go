package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"

	telemetryv1 "shen/common/api/telemetry/v1"
)

// fakeTelemetry 只实现本文件用到的方法；其余由嵌入的接口兜底 ——
// 一旦测试走到了没实现的路径就是 nil panic，比静默返回零值更容易发现。
type fakeTelemetry struct {
	telemetryv1.DeceptionTelemetryClient
	snap *telemetryv1.CoreSnapshot
	err  error
}

func (f fakeTelemetry) GetCoreSnapshot(
	context.Context, *telemetryv1.GetCoreSnapshotRequest, ...grpc.CallOption,
) (*telemetryv1.CoreSnapshot, error) {
	return f.snap, f.err
}

// 「配置」接口的键必须是 snake_case（与项目其余载荷一致），且 policy / ai 两块都在。
//
// 为什么这条要测：页面直接按这些键取值，键名漂了就整块显示「—」，
// 而 Go 的编译期**看不出** JSON 键的漂移 —— 只能靠这条测试钉住。
func TestHandleConfig_EmitsSnakeCaseShape(t *testing.T) {
	srv := &server{client: fakeTelemetry{snap: &telemetryv1.CoreSnapshot{
		PolicyId: "site-a", PolicyVersion: 7, PolicyChecksum: "abc", RuleCount: 3, WhitelistCount: 4,
		AiEnabled: true, AiKinds: []string{"content"}, AiModel: "deepseek-flash",
		AiManifestPath: "/m.json", AiContentVariants: 8, AiRotateCooldown: "30m0s",
		AiManifestLoaded: true, AiManifestVersion: 2, AiManifestResources: 5, AiManifestContents: 16,
	}}}

	rec := httptest.NewRecorder()
	srv.handleConfig(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%s", rec.Code, rec.Body.String())
	}

	var got map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是预期形状：%v（%s）", err, rec.Body.String())
	}
	for _, key := range []string{"policy_id", "version", "checksum", "rule_count", "whitelist_count"} {
		if _, ok := got["policy"][key]; !ok {
			t.Errorf("policy 缺键 %q（当前：%v）", key, got["policy"])
		}
	}
	for _, key := range []string{
		"enabled", "kinds", "model", "manifest_path", "variants", "rotate_cooldown",
		"manifest_loaded", "manifest_version", "manifest_resources", "manifest_contents",
	} {
		if _, ok := got["ai"][key]; !ok {
			t.Errorf("ai 缺键 %q（当前：%v）", key, got["ai"])
		}
	}
	if got["ai"]["manifest_contents"] != float64(16) {
		t.Errorf("manifest_contents 应为 16，实际 %v", got["ai"]["manifest_contents"])
	}
}

// 核心未装配快照读侧 ⇒ 控制台返回**错误**，不得伪造一份全零的「正常」配置。
//
// 与核心侧的 Unimplemented 是同一条决定的另一半：全零配置看起来完全正常
// （version=0、变体=0），运维会以为引擎真的这么配的 —— 报错才是对的（AR-15 的精神）。
func TestHandleConfig_CoreUnimplementedIsAnError(t *testing.T) {
	srv := &server{client: fakeTelemetry{err: errors.New("core: 未装配快照读侧")}}

	rec := httptest.NewRecorder()
	srv.handleConfig(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("取不到快照时不得返回 200：%s", rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("错误响应形状不对：%v", err)
	}
	if got["error"] == "" {
		t.Errorf("错误响应必须带 error 字段：%s", rec.Body.String())
	}
}
