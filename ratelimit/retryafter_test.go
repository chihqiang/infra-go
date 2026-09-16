package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file verifies RetryAfter on every limiter implementation (used by the http layer to
// build the Retry-After header, RFC 9110 §10.2.3). The returned value must reflect "how long
// to wait until success first becomes possible".

// TestTokenBucket_RetryAfter verifies the wait reported by the token bucket.
func TestTokenBucket_RetryAfter(t *testing.T) {
	t.Run("full bucket reports no wait", func(t *testing.T) {
		tb := NewTokenBucket(10, 5)
		assert.Equal(t, time.Duration(0), tb.RetryAfter(),
			"a bucket with available tokens needs no retry delay")
	})

	t.Run("empty bucket waits for the next token", func(t *testing.T) {
		// rate=10/s -> one token every 100ms
		tb := NewTokenBucket(10, 1)
		require.True(t, tb.Allow(), "first request consumes the initial token")

		wait := tb.RetryAfter()
		assert.Greater(t, wait, time.Duration(0))
		// Allow for scheduling jitter: this should be around one token interval
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

// TestSlidingWindow_RetryAfter verifies the wait reported by the sliding window.
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

// TestRedisTokenBucket_RetryAfter verifies that the Redis token bucket estimates from rate.
func TestRedisTokenBucket_RetryAfter(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	tb := NewRedisTokenBucket(client, "ra:tb", 10, 5)
	// rate=10/s -> one token takes about 100ms
	wait := tb.RetryAfter()
	assert.GreaterOrEqual(t, wait, 100*time.Millisecond)
	assert.LessOrEqual(t, wait, 200*time.Millisecond)

	// With rate<=0 no estimate is possible
	bad := NewRedisTokenBucket(client, "ra:tb2", 0, 5)
	assert.Equal(t, time.Duration(0), bad.RetryAfter())
}

// TestRedisSlidingWindow_RetryAfter verifies that the Redis sliding window reports a
// conservative upper bound.
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

// TestConcurrency_NoRetryAfter verifies that the concurrency limiter reports no Retry-After
// (its hold time is unpredictable and fabricating a value would mislead clients).
func TestConcurrency_NoRetryAfter(t *testing.T) {
	c := NewConcurrency(1)
	_, ok := interface{}(c).(interface{ RetryAfter() time.Duration })
	assert.False(t, ok, "concurrency limiting must not fabricate a retry interval")
}
