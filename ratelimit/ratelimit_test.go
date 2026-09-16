package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Token bucket tests ---

func TestTokenBucket_Allow(t *testing.T) {
	// 100 tokens/sec, burst 10
	tb := NewTokenBucket(100, 10)

	// Burst: the first 10 requests should pass
	for i := 0; i < 10; i++ {
		assert.True(t, tb.Allow(), "request %d should be allowed", i)
	}

	// The 11th request must be rate limited
	assert.False(t, tb.Allow(), "request 11 should be rejected")
}

func TestTokenBucket_Refill(t *testing.T) {
	tb := NewTokenBucket(1000, 5) // 1000/sec, burst 5

	// Consume every token
	for i := 0; i < 5; i++ {
		tb.Allow()
	}
	assert.False(t, tb.Allow())

	// Wait for tokens to be replenished
	time.Sleep(10 * time.Millisecond)

	// A new token should be available now
	assert.True(t, tb.Allow())
}

func TestTokenBucket_Concurrent(t *testing.T) {
	// A rate of 0 replenishes no tokens, so the result is independent of execution timing:
	// this avoids flaky failures under -race, where slow goroutines would let a high rate
	// replenish tokens.
	tb := NewTokenBucket(0, 100)

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

	// total = allowed + rejected = 200
	assert.Equal(t, int64(200), allowed+rejected)
	// Under concurrency exactly the bucket capacity of 100 tokens is consumed, never more
	assert.Equal(t, int64(100), allowed)
	assert.Equal(t, int64(100), rejected)
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

// --- Sliding window tests ---

func TestSlidingWindow_Allow(t *testing.T) {
	sw := NewSlidingWindow(5, 100*time.Millisecond)

	for i := 0; i < 5; i++ {
		assert.True(t, sw.Allow(), "request %d should be allowed", i)
	}

	assert.False(t, sw.Allow(), "request 6 should be rejected")
}

func TestSlidingWindow_Expire(t *testing.T) {
	sw := NewSlidingWindow(3, 50*time.Millisecond)

	// Consume 3
	for i := 0; i < 3; i++ {
		sw.Allow()
	}
	assert.False(t, sw.Allow())

	// Wait for the window to expire
	time.Sleep(60 * time.Millisecond)

	// The window has been reset
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

// --- Concurrency limiter tests ---

func TestConcurrency_Allow(t *testing.T) {
	c := NewConcurrency(3)

	assert.True(t, c.Allow())
	assert.True(t, c.Allow())
	assert.True(t, c.Allow())
	assert.False(t, c.Allow()) // the 4th is rejected
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

// --- Composite limiter tests ---

func TestChain_All(t *testing.T) {
	tb := NewTokenBucket(100, 10)
	sw := NewSlidingWindow(5, time.Second)

	chain := NewChain(tb, sw)

	// The sliding window allows 5
	for i := 0; i < 5; i++ {
		assert.True(t, chain.Allow())
	}
	// The 6th is rejected by the sliding window
	assert.False(t, chain.Allow())
}

func TestChain_FirstReject(t *testing.T) {
	tb := NewTokenBucket(100, 2)
	sw := NewSlidingWindow(100, time.Second)

	chain := NewChain(tb, sw)

	// The token bucket allows 2
	assert.True(t, chain.Allow())
	assert.True(t, chain.Allow())
	// The 3rd is rejected by the token bucket
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

// --- Error constant tests ---

func TestErrorConstants(t *testing.T) {
	assert.Equal(t, "ratelimit: limit exceeded", ErrLimitExceeded.Error())
}
