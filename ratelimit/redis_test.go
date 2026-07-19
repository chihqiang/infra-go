package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMiniRedis 创建一个内嵌的 miniredis 实例并返回 Redis 客户端。
func newMiniRedis(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	cleanup := func() {
		_ = client.Close()
		mr.Close()
	}
	return client, cleanup
}

// --- Redis 令牌桶测试 ---

func TestRedisTokenBucket_Allow(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	tb := NewRedisTokenBucket(client, "test:tb:1", 100, 5)

	// 突发：前 5 个请求应该通过
	for i := 0; i < 5; i++ {
		assert.True(t, tb.Allow(), "request %d should be allowed", i)
	}

	// 第 6 个请求应该被限流
	assert.False(t, tb.Allow(), "request 6 should be rejected")
}

func TestRedisTokenBucket_Refill(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	// rate=1/sec, burst=3：1 秒只生成 1 个令牌
	tb := NewRedisTokenBucket(client, "test:tb:2", 1, 3)

	// 消耗完所有令牌
	for i := 0; i < 3; i++ {
		tb.Allow()
	}
	assert.False(t, tb.Allow(), "4th request should be rejected")

	// 等待 50ms，不足 1 秒，不应该有新令牌
	time.Sleep(50 * time.Millisecond)
	assert.False(t, tb.Allow(), "request after 50ms should still be rejected")

	// 等待足够时间，令牌补充
	time.Sleep(1100 * time.Millisecond)
	assert.True(t, tb.Allow(), "request after 1.1s should be allowed")
}

func TestRedisTokenBucket_AllowContext(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	tb := NewRedisTokenBucket(client, "test:tb:3", 100, 10)
	ok, err := tb.AllowContext(context.Background())
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestRedisTokenBucket_Concurrent(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	// rate=1/sec, burst=50：并发 200 请求，大部分应被限流
	tb := NewRedisTokenBucket(client, "test:tb:4", 1, 50)

	var allowed, rejected int64
	var wg sync.WaitGroup

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tb.Allow() {
				atomic.AddInt64(&allowed, 1)
			} else {
				atomic.AddInt64(&rejected, 1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(200), allowed+rejected)
	// burst=50，并发期间可能有少量令牌补充
	assert.LessOrEqual(t, allowed, int64(55))
	assert.Greater(t, allowed, int64(0))
}

func TestRedisTokenBucket_ContextCancelled(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	tb := NewRedisTokenBucket(client, "test:tb:5", 100, 10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ok, err := tb.AllowContext(ctx)
	assert.False(t, ok)
	assert.Error(t, err)
}

// --- Redis 滑动窗口测试 ---

func TestRedisSlidingWindow_Allow(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	sw := NewRedisSlidingWindow(client, "test:sw:1", 5, 100*time.Millisecond)

	for i := 0; i < 5; i++ {
		assert.True(t, sw.Allow(), "request %d should be allowed", i)
	}

	assert.False(t, sw.Allow(), "request 6 should be rejected")
}

func TestRedisSlidingWindow_Expire(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	sw := NewRedisSlidingWindow(client, "test:sw:2", 3, 50*time.Millisecond)

	for i := 0; i < 3; i++ {
		sw.Allow()
	}
	assert.False(t, sw.Allow())

	time.Sleep(60 * time.Millisecond)

	assert.True(t, sw.Allow())
}

func TestRedisSlidingWindow_AllowContext(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	sw := NewRedisSlidingWindow(client, "test:sw:3", 5, 100*time.Millisecond)
	ok, err := sw.AllowContext(context.Background())
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestRedisSlidingWindow_ContextCancelled(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	sw := NewRedisSlidingWindow(client, "test:sw:4", 5, 100*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ok, err := sw.AllowContext(ctx)
	assert.False(t, ok)
	assert.Error(t, err)
}

func TestRedisSlidingWindow_Concurrent(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	sw := NewRedisSlidingWindow(client, "test:sw:5", 20, 10*time.Second)

	var allowed, rejected int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sw.Allow() {
				atomic.AddInt64(&allowed, 1)
			} else {
				atomic.AddInt64(&rejected, 1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(100), allowed+rejected)
	assert.Equal(t, int64(20), allowed)
	assert.Equal(t, int64(80), rejected)
}

// --- 分布式场景测试 ---

func TestRedisTokenBucket_MultipleInstances(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	// 模拟两个实例共享同一个 Redis
	tb1 := NewRedisTokenBucket(client, "shared:tb", 100, 5)
	tb2 := NewRedisTokenBucket(client, "shared:tb", 100, 5)

	// 实例1 消耗 3 个令牌
	for i := 0; i < 3; i++ {
		require.True(t, tb1.Allow())
	}

	// 实例2 只能消耗 2 个令牌
	assert.True(t, tb2.Allow())
	assert.True(t, tb2.Allow())
	// 第 6 个请求应该被限流
	assert.False(t, tb2.Allow())
}

func TestRedisSlidingWindow_MultipleInstances(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	sw1 := NewRedisSlidingWindow(client, "shared:sw", 5, 10*time.Second)
	sw2 := NewRedisSlidingWindow(client, "shared:sw", 5, 10*time.Second)

	// 实例1 消耗 3 个
	for i := 0; i < 3; i++ {
		require.True(t, sw1.Allow())
	}

	// 实例2 只能消耗 2 个
	assert.True(t, sw2.Allow())
	assert.True(t, sw2.Allow())
	// 第 6 个请求应该被限流
	assert.False(t, sw2.Allow())
}

// --- 工厂函数测试 ---

func TestNewTokenBucketWithStore_Memory(t *testing.T) {
	l := NewTokenBucketWithStore(StoreMemory, nil, "", TokenBucketConfig{Rate: 100, Burst: 10})
	assert.NotNil(t, l)

	// 应该是内存令牌桶
	_, ok := l.(*TokenBucket)
	assert.True(t, ok)
}

func TestNewTokenBucketWithStore_Redis(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	l := NewTokenBucketWithStore(StoreRedis, client, "factory:tb", TokenBucketConfig{Rate: 100, Burst: 10})
	assert.NotNil(t, l)

	// 应该是 Redis 令牌桶
	_, ok := l.(*RedisTokenBucket)
	assert.True(t, ok)

	// 验证功能正常
	assert.True(t, l.Allow())
}

func TestNewSlidingWindowWithStore_Memory(t *testing.T) {
	l := NewSlidingWindowWithStore(StoreMemory, nil, "", SlidingWindowConfig{Limit: 5, Window: time.Second})
	assert.NotNil(t, l)

	_, ok := l.(*SlidingWindow)
	assert.True(t, ok)
}

func TestNewSlidingWindowWithStore_Redis(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	l := NewSlidingWindowWithStore(StoreRedis, client, "factory:sw", SlidingWindowConfig{Limit: 5, Window: time.Second})
	assert.NotNil(t, l)

	_, ok := l.(*RedisSlidingWindow)
	assert.True(t, ok)

	assert.True(t, l.Allow())
}

// --- Chain 混合测试 ---

func TestChain_MixedMemoryAndRedis(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	// 内存令牌桶 + Redis 滑动窗口
	memTB := NewTokenBucket(100, 10)
	redisSW := NewRedisSlidingWindow(client, "chain:sw", 5, 10*time.Second)

	chain := NewChain(memTB, redisSW)

	// 两个限流器都允许前 5 个请求
	for i := 0; i < 5; i++ {
		assert.True(t, chain.Allow(), "request %d should be allowed", i)
	}
	// 第 6 个被 Redis 滑动窗口拒绝
	assert.False(t, chain.Allow())
}

// --- 常量测试 ---

func TestStoreTypeConstants(t *testing.T) {
	assert.Equal(t, StoreType("memory"), StoreMemory)
	assert.Equal(t, StoreType("redis"), StoreRedis)
}
