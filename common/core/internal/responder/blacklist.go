package responder

import (
	"fmt"

	"shen/common/core/internal/policy"
)

// 本文件实现 AR-22（生成内容黑名单）与 AR-23（分用途长度上限）。
//
// 为什么必须在**生成侧**拦：伪造响应会直接出现在攻击者屏幕上（OH-2 的判据）。
// 一份「泄露真实内网地址」或「自称 AI」的假数据，比不伪装更糟 ——
// 它既暴露了我们在伪装，又可能把真实资产信息交出去。
//
// check-leak:filter 本文件就是过滤器：selfPatterns 必须逐字包含 OH-1 禁用词否则拦不住自曝，且只用于匹配从不拼进响应。

// maxResponseBytes 是面向攻击者的**会话响应**长度上限（AR-23）。
//
// 真实站点的 API 响应很少超过这个量级；更大的内容会让「一眼假」的概率上升。
const maxResponseBytes = 256 << 10 // 256 KiB

// validateContent 校验生成内容（AR-22 / AR-23）。
//
// 内容黑名单**只有一份实现**（`policy.ValidateContentBody`）——「清单装载」与「响应生成」
// 是同一个内容流的两段，各写一份模式表必然漂移（实测踩过：模式表漂了以后只有一条路径能拦住自曝）。
// 这里只额外管**会话响应**的长度上限（分用途的 AR-23）。
func validateContent(body []byte) error {
	if len(body) > maxResponseBytes {
		return fmt.Errorf("responder: 内容超长（AR-23）：%d > %d 字节", len(body), maxResponseBytes)
	}
	if err := policy.ValidateContentBody(body); err != nil {
		return fmt.Errorf("responder: %w", err)
	}
	return nil
}
