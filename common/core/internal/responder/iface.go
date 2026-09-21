// Package responder 生成**欺骗式响应**：让幻境返回的响应看起来像真实业务。
//
// 核心不变量（AR-30）：**同一 `(会话, 资源)` 必得同一响应**。
// Agent 必然重复请求同一资源（验证、重试、翻页），同 URL 两个答案 = 一眼假。
//
// 因此本模块**不做任何非确定性的事**：不读系统时钟、不用随机数、不调 LLM。
// 内容优先取自 store 里由 L4 **离线预生成**的缓存；未命中时走**确定性模板**。
// 核心禁止外呼（MD-6），所以 LLM 永远不在本模块的路径上。
//
// 边界：不改写业务侧响应（INT-8，只作用于蜜罐侧）· 不新增 cookie / 响应头（NI-9）。
package responder

import (
	"context"
	"time"

	"shen/common/core/internal/contract"
)

// ContentStore 是本模块对存储的依赖（接口由消费方定义；store.ContentStore 满足它）。
//
// 键就是**一致性键** —— 使 AR-30 在存储层显式化。
type ContentStore interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Put(ctx context.Context, key string, body []byte, ttl time.Duration) error
}

// Responder 是本模块对外的唯一契约。
type Responder interface {
	// Respond 产出伪造响应。同 (SessionID, Resource) 必得同一结果（AR-30）。
	Respond(ctx context.Context, req contract.RespondRequest) (contract.RespondOutput, error)
}
