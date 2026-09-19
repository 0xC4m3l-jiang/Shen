package responder

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"

	"shen/core/internal/contract"
)

// Engine 是 Responder 的实现。它无状态（AR-9）：响应是输入的纯函数。
type Engine struct {
	content ContentStore
}

// New 构造响应生成器。content 为 nil 时返回错误。
func New(cs ContentStore) (*Engine, error) {
	if cs == nil {
		return nil, fmt.Errorf("responder: ContentStore 不能为 nil")
	}
	return &Engine{content: cs}, nil
}

// ConsistencyKey 是一致性键（AR-30）：`(会话, 资源)` → 同一键。
//
// 导出它，是为了让**预生成侧**（L4）与**读取侧**（本模块）用同一个键，
// 否则预生成的内容永远命中不了。
func ConsistencyKey(sessionID, resource string) string {
	return sessionID + "\x00" + resource
}

// Respond 产出伪造响应。
//
// 读取顺序：① store 里的预生成内容 → ② 确定性模板兜底。
// store 报错时**不向上抛**：响应生成失败不应成为业务链路上的失败点，
// 回落确定性模板即可（两份都是确定性的，一致性不变量不受影响）。
func (e *Engine) Respond(ctx context.Context, req contract.RespondRequest) (contract.RespondOutput, error) {
	if strings.TrimSpace(req.SessionID) == "" {
		return contract.RespondOutput{}, fmt.Errorf("responder: SessionID 不能为空（一致性键的一半）")
	}
	if strings.TrimSpace(req.Resource) == "" {
		return contract.RespondOutput{}, fmt.Errorf("responder: Resource 不能为空（一致性键的一半）")
	}

	key := ConsistencyKey(req.SessionID, req.Resource)
	if body, ok, err := e.content.Get(ctx, key); err == nil && ok && len(body) > 0 {
		// AR-22：预生成内容必须过黑名单；不合格就丢弃，回落安全模板。
		if validateContent(body) == nil {
			return output(req.Kind, body), nil
		}
	}
	return output(req.Kind, templateBody(req)), nil
}

// output 组装响应。**只设 Content-Type** —— 不得新增 cookie 或响应头（NI-9 / MD-23）。
func output(kind contract.DecoyKind, body []byte) contract.RespondOutput {
	return contract.RespondOutput{
		Status:  200,
		Headers: map[string]string{"Content-Type": contentTypeFor(kind)},
		Body:    body,
	}
}

func contentTypeFor(kind contract.DecoyKind) string {
	switch kind {
	case contract.DecoyInstructionFile:
		return "text/plain; charset=utf-8"
	default:
		return "application/json; charset=utf-8"
	}
}

// seedOf 由一致性键派生种子：同 (会话, 资源, 资产) 恒得同一种子。
func seedOf(req contract.RespondRequest) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(ConsistencyKey(req.SessionID, req.Resource) + "\x00" + req.AssetID))
	return h.Sum32()
}

// templateBody 是确定性模板兜底：**不含当前时间、不含随机数**。
//
// map 经 encoding/json 序列化时键是有序的，因此同一 seed 必得同一字节串（AR-30）。
func templateBody(req contract.RespondRequest) []byte {
	seed := seedOf(req)
	switch req.Kind {
	case contract.DecoyDeveloperAPI:
		return mustJSON(map[string]any{
			"content": "站点公开的开发者接口",
			"client_api": map[string]any{
				"note":          "任务清单仅向已注册客户端下发",
				"register":      "/portal/api/client-register",
				"heartbeat":     "/portal/api/client-heartbeat",
				"poll_interval": 30,
			},
			"tenant": fmt.Sprintf("t-%08x", seed),
		})
	case contract.DecoyDataset:
		rows := make([]map[string]any, 0, 4)
		n := 3 + int(seed%3)
		for i := 0; i < n; i++ {
			rows = append(rows, map[string]any{
				"id":    fmt.Sprintf("row-%08x-%02d", seed, i),
				"value": (seed + uint32(i)*2654435761) % 100000,
			})
		}
		return mustJSON(map[string]any{
			"items":     rows,
			"total":     1000 + int(seed%100),
			"next_page": 2,
		})
	case contract.DecoyInstructionFile:
		return []byte(fmt.Sprintf("# 站点接入说明\n\nbuild: %08x\nregion: %s\n", seed, regionOf(seed)))
	default:
		return mustJSON(map[string]any{
			"ok":  true,
			"ref": fmt.Sprintf("%08x", seed),
		})
	}
}

func regionOf(seed uint32) string {
	regions := []string{"cn-north", "cn-east", "ap-south"}
	return regions[seed%uint32(len(regions))]
}

// mustJSON 序列化；失败时返回一个固定的合法 JSON（确定性兜底）。
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"ok":false}`)
	}
	return b
}

var _ Responder = (*Engine)(nil)
