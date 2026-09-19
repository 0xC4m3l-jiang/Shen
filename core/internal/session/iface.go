// Package session 提取会话身份。
//
// 会话身份按三级优先级提取；提取结果禁止上行、禁止回传。
package session

import (
	"context"

	"shen/core/internal/contract"
)

// Session 是本模块对外的唯一契约。
type Session interface {
	Key(ctx context.Context, obs contract.Observation) (contract.SessionKey, error)
}
