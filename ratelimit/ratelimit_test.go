package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- 令牌桶测试 ---

func TestTokenBucket_Allow(t *testing.T) {
	// 100 tokens/sec, burst 10
	tb := NewTokenBucket(100, 10)

	// 突发：前 10 个请求应该通过
	for i := 0; i < 10; i++ {
		assert.True(t, tb.Allow(), "request %d should be allowed", i)
	}

	// 第 11 个请求应该被限流
	assert.False(t, tb.Allow(), "request 11 should be rejected")
}

func TestTokenBucket_Refill(t *testing.T) {
	tb := NewTokenBucket(1000, 5) // 1000/sec, burst 5

	// 消耗完所有令牌
	for i := 0; i < 5; i++ {
		tb.Allow()
	}
	assert.False(t, tb.Allow())

	// 等待令牌补充
	time.Sleep(10 * time.Millisecond)

	// 应该有新令牌了
	assert.True(t, tb.Allow())
}

func TestTokenBucket_Concurrent(t *testing.T) {
	tb := NewTokenBucket(10000, 100)

	var allowed, rejected int64
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tb.Allow() {
				mu.Lock()
				allowed++
				mu.Unlock()
			} else {
				mu.Lock()
				rejected++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// 总数 = allowed + rejected = 200
	assert.Equal(t, int64(200), allowed+rejected)
	// 允许的不能超过桶容量（可能有少量令牌补充）
	assert.LessOrEqual(t, allowed, int64(105)) // 允许少量误差
	assert.Greater(t, allowed, int64(0))
}

func TestTokenBucket_AllowContext(t *testing.T) {
	tb := NewTokenBucket(100, 10)
	ok, err := tb.AllowContext(context.Background())
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestTokenBucket_Tokens(t *testing.T) {
	tb := NewTokenBucket(100, 10)
	assert.Equal(t, float64(10), tb.Tokens())

	tb.Allow()
	assert.InDelta(t, 9, tb.Tokens(), 0.1)
}

// --- 滑动窗口测试 ---

func TestSlidingWindow_Allow(t *testing.T) {
	sw := NewSlidingWindow(5, 100*time.Millisecond)

	for i := 0; i < 5; i++ {
		assert.True(t, sw.Allow(), "request %d should be allowed", i)
	}

	assert.False(t, sw.Allow(), "request 6 should be rejected")
}

func TestSlidingWindow_Expire(t *testing.T) {
	sw := NewSlidingWindow(3, 50*time.Millisecond)

	// 消耗 3 个
	for i := 0; i < 3; i++ {
		sw.Allow()
	}
	assert.False(t, sw.Allow())

	// 等待窗口过期
	time.Sleep(60 * time.Millisecond)

	// 窗口已重置
	assert.True(t, sw.Allow())
}

func TestSlidingWindow_Count(t *testing.T) {
	sw := NewSlidingWindow(10, 100*time.Millisecond)

	for i := 0; i < 3; i++ {
		sw.Allow()
	}
	assert.Equal(t, 3, sw.Count())
}

func TestSlidingWindow_AllowContext(t *testing.T) {
	sw := NewSlidingWindow(5, 100*time.Millisecond)
	ok, err := sw.AllowContext(context.Background())
	require.NoError(t, err)
	assert.True(t, ok)
}

// --- 并发数限流器测试 ---

func TestConcurrency_Allow(t *testing.T) {
	c := NewConcurrency(3)

	assert.True(t, c.Allow())
	assert.True(t, c.Allow())
	assert.True(t, c.Allow())
	assert.False(t, c.Allow()) // 第 4 个被拒
}

func TestConcurrency_Release(t *testing.T) {
	c := NewConcurrency(2)

	c.Allow()
	c.Allow()
	assert.False(t, c.Allow())

	c.Release()
	assert.True(t, c.Allow())
}

func TestConcurrency_Current(t *testing.T) {
	c := NewConcurrency(5)
	c.Allow()
	c.Allow()
	assert.Equal(t, 2, c.Current())

	c.Release()
	assert.Equal(t, 1, c.Current())
}

func TestConcurrency_Concurrent(t *testing.T) {
	c := NewConcurrency(10)

	var allowed, rejected int64
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.Allow() {
				mu.Lock()
				allowed++
				mu.Unlock()
			} else {
				mu.Lock()
				rejected++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(10), allowed)
	assert.Equal(t, int64(40), rejected)
}

// --- 组合限流器测试 ---

func TestChain_All(t *testing.T) {
	tb := NewTokenBucket(100, 10)
	sw := NewSlidingWindow(5, time.Second)

	chain := NewChain(tb, sw)

	// 滑动窗口限制 5 个
	for i := 0; i < 5; i++ {
		assert.True(t, chain.Allow())
	}
	// 第 6 个被滑动窗口拒绝
	assert.False(t, chain.Allow())
}

func TestChain_FirstReject(t *testing.T) {
	tb := NewTokenBucket(100, 2)
	sw := NewSlidingWindow(100, time.Second)

	chain := NewChain(tb, sw)

	// 令牌桶限制 2 个
	assert.True(t, chain.Allow())
	assert.True(t, chain.Allow())
	// 第 3 个被令牌桶拒绝
	assert.False(t, chain.Allow())
}

func TestChain_Add(t *testing.T) {
	chain := NewChain()
	chain.Add(NewTokenBucket(100, 10))
	assert.True(t, chain.Allow())
}

func TestChain_AllowContext(t *testing.T) {
	chain := NewChain(NewTokenBucket(100, 10))
	ok, err := chain.AllowContext(context.Background())
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestChain_AllowContext_Cancelled(t *testing.T) {
	chain := NewChain(NewTokenBucket(100, 10))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ok, err := chain.AllowContext(ctx)
	assert.False(t, ok)
	assert.Error(t, err)
}

// --- 错误常量测试 ---

func TestErrorConstants(t *testing.T) {
	assert.Equal(t, "ratelimit: limit exceeded", ErrLimitExceeded.Error())
}
