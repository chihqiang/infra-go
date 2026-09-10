package cache

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖 MemCache 后台 goroutine 的回收（回归）。
// 历史缺陷：scanLoop/statLoop 只监听 Close 关闭的 stop channel，
// 完全不理会构造时传入的 ctx；调用方忘记 Close 时 goroutine 永久泄漏
// （按请求/租户创建缓存实例的场景会持续累积）。

// waitGoroutinesSettle 等待 goroutine 数量回落到阈值以内。
// 后台 goroutine 退出存在调度延迟，因此轮询等待而非立即断言。
func waitGoroutinesSettle(t *testing.T, max int, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		runtime.GC()
		n := runtime.NumGoroutine()
		if n <= max {
			return n
		}
		if time.Now().After(deadline) {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestMemCache_CtxCancelStopsBackgroundGoroutines 回归测试：
// 取消构造时传入的 ctx 应回收后台 goroutine，无需调用 Close。
func TestMemCache_CtxCancelStopsBackgroundGoroutines(t *testing.T) {
	// 先建立基线（含统计 goroutine 的稳态数量）
	baseline := runtime.NumGoroutine()

	const instances = 10
	ctx, cancel := context.WithCancel(context.Background())

	caches := make([]*MemCache, 0, instances)
	for i := 0; i < instances; i++ {
		// expire > 0 才会启动 scanLoop；statLoop 始终启动
		caches = append(caches, NewMemCache(ctx, time.Minute))
	}

	// 此时应有约 2*instances 个后台 goroutine
	running := runtime.NumGoroutine()
	require.Greater(t, running, baseline,
		"background goroutines should be running after creation")

	// 仅取消 ctx，不调用 Close
	cancel()

	// 所有后台 goroutine 都应退出
	got := waitGoroutinesSettle(t, baseline+2, 3*time.Second)
	t.Logf("goroutines: baseline=%d running=%d after cancel=%d (instances=%d)",
		baseline, running, got, instances)

	assert.LessOrEqual(t, got, baseline+2,
		"cancelling ctx must stop scanLoop/statLoop; leaked goroutines indicate a leak")

	// 清理（Close 是幂等的，与 ctx 取消叠加也不应 panic）
	for _, c := range caches {
		c.Close()
	}
}

// TestMemCache_CloseStillWorks 验证原有的 Close 语义未被破坏。
func TestMemCache_CloseStillWorks(t *testing.T) {
	baseline := runtime.NumGoroutine()

	const instances = 10
	ctx := context.Background() // 不取消

	caches := make([]*MemCache, 0, instances)
	for i := 0; i < instances; i++ {
		caches = append(caches, NewMemCache(ctx, time.Minute))
	}

	for _, c := range caches {
		c.Close()
	}

	got := waitGoroutinesSettle(t, baseline+2, 3*time.Second)
	assert.LessOrEqual(t, got, baseline+2, "Close must stop the background goroutines")
}

// TestMemCache_CloseIsIdempotent 验证 Close 可重复调用。
func TestMemCache_CloseIsIdempotent(t *testing.T) {
	c := NewMemCache(context.Background(), time.Minute)
	require.NotPanics(t, func() {
		c.Close()
		c.Close()
		c.Close()
	})
}

// TestMemCache_NilCtxDoesNotPanic 验证 ctx 为 nil 时仍可用（不监听 ctx）。
func TestMemCache_NilCtxDoesNotPanic(t *testing.T) {
	var c *MemCache
	require.NotPanics(t, func() {
		//nolint:staticcheck // 显式验证 nil ctx 的容错
		c = NewMemCache(nil, time.Minute)
	})
	defer c.Close()

	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", "v"))
	v, err := c.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, "v", v)
}

// TestMemCache_NoExpireStartsNoScanLoop 验证未配置过期时间时不启动扫描 goroutine。
func TestMemCache_NoExpireStartsNoScanLoop(t *testing.T) {
	c := NewMemCache(context.Background(), 0) // expire<=0：无 scanLoop
	defer c.Close()

	assert.Equal(t, time.Duration(0), c.scanInterval, "no expiry means no scan interval")
}
