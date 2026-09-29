package connector

import (
	"math/rand"
	"time"
)

// nextBackoff 指数退避 + 随机抖动：base 翻倍封顶 max，再乘 0.5–1.5 的抖动系数。
// 抖动防雪崩：网关重启时所有连接器不会在同一毫秒蜂拥重连。
func nextBackoff(base, max time.Duration) time.Duration {
	next := base * 2
	if next > max {
		next = max
	}
	jitter := 0.5 + rand.Float64() // 0.5–1.5
	scaled := time.Duration(float64(next) * jitter)
	if scaled < time.Millisecond {
		scaled = time.Millisecond
	}
	return scaled
}
