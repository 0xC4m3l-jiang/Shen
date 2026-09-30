package policy

import (
	"context"
	"sync"
	"testing"

	policyv1 "shen/common/api/policy/v1"
	"shen/common/core/internal/store"
)

// TestLiveConcurrentSwapAndRead 在 -race 下断言：并发 Swap 与读取安全，且每次 Pull 的
// 版本、校验和、载荷来自同一份快照（不会读到"版本是新的、载荷是旧的"）。
func TestLiveConcurrentSwapAndRead(t *testing.T) {
	base := mustLoad(t, serverYAML)
	live := NewLive(base)
	srv := NewServerLive(live, store.NewPolicyMemory())
	ctx := context.Background()

	versions := make([]*Loader, 0, 20)
	for i := int64(1); i <= 20; i++ {
		l, err := base.WithOverlay(sampleOverlay(), 3_000_000+i)
		if err != nil {
			t.Fatal(err)
		}
		versions = append(versions, l)
	}
	sums := map[uint64]string{}
	for _, l := range versions {
		s, _ := srv.edgePayload(ctx, l, l.snap)
		sums[l.snap.Version] = string(s)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, l := range versions {
			live.Swap(l)
		}
	}()
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				got, err := srv.Pull(ctx, &policyv1.PolicyPullRequest{})
				if err != nil {
					t.Error(err)
					return
				}
				if want, ok := sums[got.GetVersion()]; ok && want != string(got.GetPayload()) {
					t.Errorf("版本 %d 的载荷与它自己的快照不一致", got.GetVersion())
					return
				}
				if _, err := live.Rules(ctx); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if live.Current() != versions[len(versions)-1] {
		t.Error("最终持有的应是最后一次 Swap 的快照")
	}
	live.Swap(nil) // nil 被忽略
	if live.Current() == nil {
		t.Error("Swap(nil) 不能清空持有者")
	}
}
