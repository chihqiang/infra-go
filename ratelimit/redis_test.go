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

// newMiniRedis creates an embedded miniredis instance and returns a Redis client.
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

// --- Redis token bucket tests ---

func TestRedisTokenBucket_Allow(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	// Use an extremely small rate (1 token / 1000 seconds) so that almost no token is
	// replenished during the test and the assertions do not depend on machine speed.
	// Previously rate=100 (one token every 10ms), so under load the 6th request could be
	// allowed after a token had been replenished, causing occasional failures.
	tb := NewRedisTokenBucket(client, "test:tb:1", 0.001, 5)

	// Burst: the first 5 requests should pass
	for i := 0; i < 5; i++ {
		assert.True(t, tb.Allow(), "request %d should be allowed", i)
	}

	// The 6th request must be rate limited
	assert.False(t, tb.Allow(), "request 6 should be rejected")
}

func TestRedisTokenBucket_Refill(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	// rate=1/sec, burst=3: only one token is generated per second
	tb := NewRedisTokenBucket(client, "test:tb:2", 1, 3)

	// Consume every token
	for i := 0; i < 3; i++ {
		tb.Allow()
	}
	assert.False(t, tb.Allow(), "4th request should be rejected")

	// Wait 50ms, which is less than one second, so no new token should appear
	time.Sleep(50 * time.Millisecond)
	assert.False(t, tb.Allow(), "request after 50ms should still be rejected")

	// Wait long enough for a token to be replenished
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

	// Use an extremely small rate (1 token / 1000 seconds) so that almost no token is
	// replenished during the test and the assertions do not depend on machine speed.
	// Previously rate=1/sec with a burst of 55 tokens, so if 200 concurrent requests took
	// longer than 5 seconds in a slow environment such as -race, extra tokens were let
	// through.
	tb := NewRedisTokenBucket(client, "test:tb:4", 0.001, 50)

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
	// burst=50, so a few tokens may be replenished during the concurrent run
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

// --- Redis sliding window tests ---

func TestRedisSlidingWindow_Allow(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	// Use a long enough window (10s) so that all 6 calls fall inside the same window and the
	// assertions do not depend on machine speed. Previously the window was 100ms, so under
	// load the first 5 calls could cross the window boundary and the 6th was allowed.
	sw := NewRedisSlidingWindow(client, "test:sw:1", 5, 10*time.Second)

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

// --- Distributed scenario tests ---

func TestRedisTokenBucket_MultipleInstances(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	// Simulate two instances sharing the same Redis.
	// Use an extremely small rate so that almost no token is replenished during the test.
	// Previously rate=100 (one token every 10ms), so under -race the 5 calls could take
	// longer than 10ms, a token would be replenished and the 6th request was wrongly
	// allowed.
	tb1 := NewRedisTokenBucket(client, "shared:tb", 0.001, 5)
	tb2 := NewRedisTokenBucket(client, "shared:tb", 0.001, 5)

	// Instance 1 consumes 3 tokens
	for i := 0; i < 3; i++ {
		require.True(t, tb1.Allow())
	}

	// Instance 2 can only consume 2 tokens
	assert.True(t, tb2.Allow())
	assert.True(t, tb2.Allow())
	// The 6th request must be rate limited
	assert.False(t, tb2.Allow())
}

func TestRedisSlidingWindow_MultipleInstances(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	sw1 := NewRedisSlidingWindow(client, "shared:sw", 5, 10*time.Second)
	sw2 := NewRedisSlidingWindow(client, "shared:sw", 5, 10*time.Second)

	// Instance 1 consumes 3
	for i := 0; i < 3; i++ {
		require.True(t, sw1.Allow())
	}

	// Instance 2 can only consume 2
	assert.True(t, sw2.Allow())
	assert.True(t, sw2.Allow())
	// The 6th request must be rate limited
	assert.False(t, sw2.Allow())
}

// --- Factory function tests ---

func TestNewTokenBucketWithStore_Memory(t *testing.T) {
	l := NewTokenBucketWithStore(StoreMemory, nil, "", TokenBucketConfig{Rate: 100, Burst: 10})
	assert.NotNil(t, l)

	// It must be an in-memory token bucket
	_, ok := l.(*TokenBucket)
	assert.True(t, ok)
}

func TestNewTokenBucketWithStore_Redis(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	l := NewTokenBucketWithStore(StoreRedis, client, "factory:tb", TokenBucketConfig{Rate: 100, Burst: 10})
	assert.NotNil(t, l)

	// It must be a Redis token bucket
	_, ok := l.(*RedisTokenBucket)
	assert.True(t, ok)

	// Verify that it works
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

// --- Mixed Chain tests ---

func TestChain_MixedMemoryAndRedis(t *testing.T) {
	client, cleanup := newMiniRedis(t)
	defer cleanup()

	// An in-memory token bucket plus a Redis sliding window
	memTB := NewTokenBucket(100, 10)
	redisSW := NewRedisSlidingWindow(client, "chain:sw", 5, 10*time.Second)

	chain := NewChain(memTB, redisSW)

	// Both limiters allow the first 5 requests
	for i := 0; i < 5; i++ {
		assert.True(t, chain.Allow(), "request %d should be allowed", i)
	}
	// The 6th is rejected by the Redis sliding window
	assert.False(t, chain.Allow())
}

// --- Constant tests ---

func TestStoreTypeConstants(t *testing.T) {
	assert.Equal(t, StoreType("memory"), StoreMemory)
	assert.Equal(t, StoreType("redis"), StoreRedis)
}
