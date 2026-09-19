package control

import (
	"errors"
	"testing"
	"time"
)

func TestBreakerStaysClosedBelowMinSamples(t *testing.T) {
	b := NewBreaker(BreakerConfig{MinSamples: 10, MaxFailRatio: 0.5})
	for i := 0; i < 9; i++ {
		b.Record(errors.New("boom"))
	}
	if b.Opened() {
		t.Error("样本数不足时不得跳闸")
	}
	if !b.Allow() {
		t.Error("未跳闸时应放行")
	}
}

func TestBreakerOpensOnHighFailRatio(t *testing.T) {
	b := NewBreaker(BreakerConfig{MinSamples: 10, MaxFailRatio: 0.5})
	for i := 0; i < 10; i++ {
		b.Record(errors.New("boom"))
	}
	if !b.Opened() {
		t.Fatal("错误率 100% 应跳闸")
	}
	if b.Allow() {
		t.Error("跳闸后 Allow 应为 false（调用方须纯放行，NI-10）")
	}
}

func TestBreakerStaysClosedOnLowFailRatio(t *testing.T) {
	b := NewBreaker(BreakerConfig{MinSamples: 10, MaxFailRatio: 0.5})
	for i := 0; i < 10; i++ {
		var err error
		if i == 0 {
			err = errors.New("boom")
		}
		b.Record(err)
	}
	if b.Opened() {
		t.Error("错误率 10% 不得跳闸（阈值 50%）")
	}
}

func TestBreakerHalfOpenAfterCooldown(t *testing.T) {
	now := time.Unix(1700000000, 0)
	b := NewBreaker(BreakerConfig{
		MinSamples:   2,
		MaxFailRatio: 0.5,
		Cooldown:     time.Second,
		Now:          func() time.Time { return now },
	})
	b.Record(errors.New("boom"))
	b.Record(errors.New("boom"))
	if !b.Opened() {
		t.Fatal("应跳闸")
	}
	if b.Allow() {
		t.Error("冷却期内应拒绝")
	}

	now = now.Add(time.Second) // 冷却期满
	if !b.Allow() {
		t.Error("冷却期满应半开放行")
	}
	if b.Opened() {
		t.Error("半开后 Opened 应为 false")
	}
}

func TestBreakerIgnoresRecordsWhileOpen(t *testing.T) {
	now := time.Unix(1700000000, 0)
	b := NewBreaker(BreakerConfig{
		MinSamples:   2,
		MaxFailRatio: 0.5,
		Cooldown:     time.Hour,
		Now:          func() time.Time { return now },
	})
	b.Record(errors.New("boom"))
	b.Record(errors.New("boom"))
	if !b.Opened() {
		t.Fatal("应跳闸")
	}
	// 跳闸期间请求没走决策，成功记录不得把它“冲”回闭合。
	for i := 0; i < 100; i++ {
		b.Record(nil)
	}
	if !b.Opened() {
		t.Error("跳闸期间的 Record 不得改变状态")
	}
}
