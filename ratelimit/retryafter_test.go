package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件验证各限流器实现的 RetryAfter（供 http 层生成 Retry-After 头，
// RFC 9110 §10.2.3）。返回值必须反映"最早可能成功的等待时长"。

// TestTokenBucket_RetryAfter 验证令牌桶给出的等待时长。
func TestTokenBucket_RetryAfter(t *testing.T) {
	t.Run("full bucket reports no wait", func(t *testing.T) {
		tb := NewTokenBucket(10, 5)
		assert.Equal(t, time.Duration(0), tb.RetryAfter(),
			"a bucket with available tokens needs no retry delay")
	})

	t.Run("empty bucket waits for the next token", func(t *testing.T) {
		// rate=10/s → 每 100ms 生成 1 个令牌
		tb := NewTokenBucket(10, 1)
		require.True(t, tb.Allow(), "first request consumes the initial token")

		wait := tb.RetryAfter()
		assert.Greater(t, wait, time.Duration(0))
		// 允许调度抖动：应在 1 个令牌间隔附近
		assert.LessOrEqual(t, wait, 200*time.Millisecond)
	})

	t.Run("faster rate yields shorter wait", func(t *testing.T) {
		slow := NewTokenBucket(1, 1)
		fast := NewTokenBucket(100, 1)
		require.True(t, slow.Allow())
		require.True(t, fast.Allow())

		assert.Greater(t, slow.RetryAfter(), fast.RetryAfter(),
			"a lower refill rate means a longer wait")
	})

	t.Run("zero rate cannot estimate", func(t *testing.T) {
		tb := NewTokenBucket(0, 1)
		require.True(t, tb.Allow())
		assert.Equal(t, time.Duration(0), tb.RetryAfter(),
			"a bucket that never refills has no meaningful retry estimate")
	})
}

// TestSlidingWindow_RetryAfter 验证滑动窗口给出的等待时长。
func TestSlidingWindow_RetryAfter(t *testing.T) {
	t.Run("no wait when window is not full", func(t *testing.T) {
		sw := NewSlidingWindow(5, time.Second)
		assert.Equal(t, time.Duration(0), sw.RetryAfter())
	})

	t.Run("waits until the oldest entry leaves the window", func(t *testing.T) {
		const window = 500 * time.Millisecond
		sw := NewSlidingWindow(3, window)
		for i := 0; i < 3; i++ {
			require.True(t, sw.Allow())
		}
		require.False(t, sw.Allow(), "window must be full")

		wait := sw.RetryAfter()
		assert.Greater(t, wait, time.Duration(0))
		assert.LessOrEqual(t, wait, window,
			"wait must not exceed the window length")
	})

	t.Run("quota frees after waiting", func(t *testing.T) {
		const window = 100 * time.Millisecond
		sw := NewSlidingWindow(2, window)
		require.True(t, sw.Allow())
		require.True(t, sw.Allow())
		require.False(t, sw.Allow())

		wait := sw.RetryAfter()
		require.Greater(t, wait, time.Duration(0))

		time.Sleep(wait + 20*time.Millisecond)
		assert.True(t, sw.Allow(), "waiting the reported duration must free a slot")
	})
}

// TestRedisTokenBucket_RetryAfter 验证 Redis 令牌桶基于 rate 给出估计。
func TestRedisTokenBucket_RetryAfter(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	tb := NewRedisTokenBucket(client, "ra:tb", 10, 5)
	// rate=10/s → 一个令牌约 100ms
	wait := tb.RetryAfter()
	assert.GreaterOrEqual(t, wait, 100*time.Millisecond)
	assert.LessOrEqual(t, wait, 200*time.Millisecond)

	// rate<=0 无法估计
	bad := NewRedisTokenBucket(client, "ra:tb2", 0, 5)
	assert.Equal(t, time.Duration(0), bad.RetryAfter())
}

// TestRedisSlidingWindow_RetryAfter 验证 Redis 滑动窗口给出保守上界。
func TestRedisSlidingWindow_RetryAfter(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	const window = 2 * time.Second
	sw := NewRedisSlidingWindow(client, "ra:sw", 10, window)
	assert.Equal(t, window, sw.RetryAfter(),
		"redis cannot know the oldest entry without an extra round trip, so it reports the window as an upper bound")

	bad := NewRedisSlidingWindow(client, "ra:sw2", 10, 0)
	assert.Equal(t, time.Duration(0), bad.RetryAfter())
}

// TestConcurrency_NoRetryAfter 验证并发数限流器不报告 Retry-After
// （其占用时长不可预测，编造数值会误导客户端）。
func TestConcurrency_NoRetryAfter(t *testing.T) {
	c := NewConcurrency(1)
	_, ok := interface{}(c).(interface{ RetryAfter() time.Duration })
	assert.False(t, ok, "concurrency limiting must not fabricate a retry interval")
}
