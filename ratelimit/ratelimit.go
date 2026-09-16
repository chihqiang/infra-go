package ratelimit

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Error definitions.
var (
	// ErrLimitExceeded means the rate limit was exceeded.
	ErrLimitExceeded = errors.New("ratelimit: limit exceeded")
)

// Limiter is the rate limiter interface.
// Both the in-memory limiter and the Redis limiter implement it, so they can be swapped
// freely.
type Limiter interface {
	// Allow reports whether the request is allowed.
	// true means allowed, false means rate limited.
	Allow() bool
	// AllowContext is the context-aware check and supports timeout cancellation.
	// For the in-memory limiter it behaves exactly like Allow.
	// For the Redis limiter the context controls the timeout of the Redis operations.
	AllowContext(ctx context.Context) (bool, error)
}

// --- Token bucket (in-memory) ---

// TokenBucket is a token bucket rate limiter.
// Tokens are generated at a fixed rate and consumed by requests, which allows bursts.
type TokenBucket struct {
	mu         sync.Mutex
	rate       float64   // tokens generated per second
	burst      float64   // bucket capacity (maximum number of tokens)
	tokens     float64   // current number of tokens
	lastUpdate time.Time // last update time
}

// NewTokenBucket creates a token bucket rate limiter.
// rate is the number of tokens generated per second and burst is the bucket capacity.
// For example, NewTokenBucket(100, 200) generates 100 tokens per second and stores at most
// 200 of them.
func NewTokenBucket(rate float64, burst float64) *TokenBucket {
	return &TokenBucket{
		rate:       rate,
		burst:      burst,
		tokens:     burst, // start with a full bucket
		lastUpdate: time.Now(),
	}
}

// Allow reports whether the request is allowed.
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	// Add the tokens generated since the last update
	elapsed := now.Sub(tb.lastUpdate).Seconds()
	tb.tokens += elapsed * tb.rate

	// Never let the token count exceed the bucket capacity
	if tb.tokens > tb.burst {
		tb.tokens = tb.burst
	}
	tb.lastUpdate = now

	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

// AllowContext is the context-aware check.
func (tb *TokenBucket) AllowContext(ctx context.Context) (bool, error) {
	// The token bucket is computed in memory, so the context is irrelevant
	return tb.Allow(), nil
}

// Tokens returns the current number of tokens (for debugging/monitoring).
func (tb *TokenBucket) Tokens() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.tokens
}

// RetryAfter returns the suggested retry delay: the time until the next token is available.
//
// It implements the http-layer Retry-After semantics (RFC 9110 §10.2.3).
// When rate <= 0 the bucket never refills, so 0 is returned to signal that no estimate can
// be given.
func (tb *TokenBucket) RetryAfter() time.Duration {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	if tb.rate <= 0 {
		return 0
	}

	now := time.Now()
	tokens := tb.tokens + now.Sub(tb.lastUpdate).Seconds()*tb.rate
	if tokens >= 1 {
		return 0 // a token is already available
	}

	// Reaching one full token still takes (1-tokens)/rate seconds
	wait := time.Duration((1 - tokens) / tb.rate * float64(time.Second))
	if wait < time.Millisecond {
		// Round up to 1ms so that floating point error cannot produce 0, which would be
		// read as "retry immediately"
		wait = time.Millisecond
	}
	return wait
}

// --- Sliding window (in-memory) ---

// SlidingWindow is a sliding window rate limiter.
// It allows at most limit requests within the given time window.
type SlidingWindow struct {
	mu       sync.Mutex
	limit    int           // maximum number of requests within the window
	window   time.Duration // window size
	requests []time.Time   // request timestamps
}

// NewSlidingWindow creates a sliding window rate limiter.
// limit is the maximum number of requests within the window and window is the window size.
// For example, NewSlidingWindow(100, time.Second) allows at most 100 requests per second.
func NewSlidingWindow(limit int, window time.Duration) *SlidingWindow {
	return &SlidingWindow{
		limit:    limit,
		window:   window,
		requests: make([]time.Time, 0, limit),
	}
}

// Allow reports whether the request is allowed.
func (sw *SlidingWindow) Allow() bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	sw.pruneExpired(now)

	// Check whether the limit has been exceeded
	if len(sw.requests) >= sw.limit {
		return false
	}

	sw.requests = append(sw.requests, now)
	return true
}

// AllowContext is the context-aware check.
func (sw *SlidingWindow) AllowContext(ctx context.Context) (bool, error) {
	return sw.Allow(), nil
}

// pruneExpired removes the request records that have left the window.
// The caller must hold the lock.
// Elements are moved in place with copy, reusing the backing array to avoid frequent
// allocations.
func (sw *SlidingWindow) pruneExpired(now time.Time) {
	windowStart := now.Add(-sw.window)

	i := 0
	for ; i < len(sw.requests); i++ {
		if sw.requests[i].After(windowStart) {
			break
		}
	}
	if i > 0 {
		// Shift the elements forward in place with copy, reusing the backing array capacity
		copy(sw.requests, sw.requests[i:])
		sw.requests = sw.requests[:len(sw.requests)-i]
	}
}

// Count returns the number of requests in the current window (for debugging/monitoring).
func (sw *SlidingWindow) Count() int {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.pruneExpired(time.Now())
	return len(sw.requests)
}

// RetryAfter returns the suggested retry delay: the time until the oldest record slides out
// of the window.
//
// It implements the http-layer Retry-After semantics (RFC 9110 §10.2.3).
// It returns 0 when the window is not full or holds no records (no meaningful hint).
func (sw *SlidingWindow) RetryAfter() time.Duration {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	sw.pruneExpired(now)
	if len(sw.requests) < sw.limit {
		return 0
	}

	// Quota is only freed once the oldest record leaves the window
	wait := sw.requests[0].Add(sw.window).Sub(now)
	if wait < time.Millisecond {
		wait = time.Millisecond
	}
	return wait
}

// --- Concurrency limiting ---

// Concurrency is a concurrency rate limiter.
// It limits the number of requests processed at the same time.
type Concurrency struct {
	mu      sync.Mutex
	limit   int
	current int
}

// NewConcurrency creates a concurrency limiter.
// limit is the maximum number of concurrent requests.
func NewConcurrency(limit int) *Concurrency {
	return &Concurrency{
		limit: limit,
	}
}

// Allow reports whether the request is allowed.
// Note: Release must be called afterwards.
func (c *Concurrency) Allow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.current >= c.limit {
		return false
	}
	c.current++
	return true
}

// AllowContext is the context-aware check.
func (c *Concurrency) AllowContext(ctx context.Context) (bool, error) {
	return c.Allow(), nil
}

// Release frees one concurrency slot.
func (c *Concurrency) Release() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.current > 0 {
		c.current--
	}
}

// Current returns the current concurrency (for debugging/monitoring).
func (c *Concurrency) Current() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// --- Composite limiter ---

// Chain combines several limiters; the request is allowed only when all of them allow it.
type Chain struct {
	limiters []Limiter
}

// NewChain creates a composite limiter.
func NewChain(limiters ...Limiter) *Chain {
	return &Chain{limiters: limiters}
}

// Allow reports whether every limiter allows the request.
// Note: when one limiter returns false, the limiters that already passed are not rolled
// back.
// Warning: do not use a Concurrency limiter inside a Chain, because Concurrency.Allow()
// increments the counter and a later failure does not roll it back with Release, which
// leaves the concurrency count inflated. If you need to combine them, use Concurrency on its
// own.
func (c *Chain) Allow() bool {
	for _, l := range c.limiters {
		if !l.Allow() {
			return false
		}
	}
	return true
}

// AllowContext is the context-aware check.
func (c *Chain) AllowContext(ctx context.Context) (bool, error) {
	for _, l := range c.limiters {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}
		ok, err := l.AllowContext(ctx)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// Add appends a limiter to the chain.
func (c *Chain) Add(limiter Limiter) {
	c.limiters = append(c.limiters, limiter)
}
