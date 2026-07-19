package ratelimit

import (
	"context"
	"errors"
	"sync"
	"time"
)

// 错误定义。
var (
	// ErrLimitExceeded 超出限流。
	ErrLimitExceeded = errors.New("ratelimit: limit exceeded")
)

// Limiter 限流器接口。
// 内存限流器和 Redis 限流器均实现此接口，可自由切换。
type Limiter interface {
	// Allow 检查是否允许请求通过。
	// 返回 true 表示允许，false 表示被限流。
	Allow() bool
	// AllowContext 带 context 的检查，支持超时取消。
	// 对于内存限流器，与 Allow 行为一致。
	// 对于 Redis 限流器，通过 context 控制 Redis 操作超时。
	AllowContext(ctx context.Context) (bool, error)
}

// --- 令牌桶（内存） ---

// TokenBucket 令牌桶限流器。
// 以固定速率生成令牌，请求消耗令牌，支持突发流量。
type TokenBucket struct {
	mu         sync.Mutex
	rate       float64   // 每秒生成令牌数
	burst      float64   // 桶容量（最大令牌数）
	tokens     float64   // 当前令牌数
	lastUpdate time.Time // 上次更新时间
}

// NewTokenBucket 创建令牌桶限流器。
// rate 为每秒生成的令牌数，burst 为桶容量。
// 例如：NewTokenBucket(100, 200) 表示每秒生成 100 个令牌，桶最多存 200 个。
func NewTokenBucket(rate float64, burst float64) *TokenBucket {
	return &TokenBucket{
		rate:       rate,
		burst:      burst,
		tokens:     burst, // 初始满桶
		lastUpdate: time.Now(),
	}
}

// Allow 检查是否允许请求通过。
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	// 计算自上次更新以来生成的令牌数
	elapsed := now.Sub(tb.lastUpdate).Seconds()
	tb.tokens += elapsed * tb.rate

	// 限制令牌数不超过桶容量
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

// AllowContext 带 context 的检查。
func (tb *TokenBucket) AllowContext(ctx context.Context) (bool, error) {
	// 令牌桶是内存计算，无需考虑 context
	return tb.Allow(), nil
}

// Tokens 返回当前令牌数（用于调试/监控）。
func (tb *TokenBucket) Tokens() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.tokens
}

// --- 滑动窗口（内存） ---

// SlidingWindow 滑动窗口限流器。
// 在指定时间窗口内最多允许 limit 次请求。
type SlidingWindow struct {
	mu       sync.Mutex
	limit    int           // 窗口内最大请求数
	window   time.Duration // 时间窗口大小
	requests []time.Time   // 请求时间戳
}

// NewSlidingWindow 创建滑动窗口限流器。
// limit 为窗口内最大请求数，window 为时间窗口大小。
// 例如：NewSlidingWindow(100, time.Second) 表示每秒最多 100 次请求。
func NewSlidingWindow(limit int, window time.Duration) *SlidingWindow {
	return &SlidingWindow{
		limit:    limit,
		window:   window,
		requests: make([]time.Time, 0, limit),
	}
}

// Allow 检查是否允许请求通过。
func (sw *SlidingWindow) Allow() bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-sw.window)

	// 移除窗口外的请求记录
	i := 0
	for ; i < len(sw.requests); i++ {
		if sw.requests[i].After(windowStart) {
			break
		}
	}
	sw.requests = sw.requests[i:]

	// 检查是否超出限制
	if len(sw.requests) >= sw.limit {
		return false
	}

	sw.requests = append(sw.requests, now)
	return true
}

// AllowContext 带 context 的检查。
func (sw *SlidingWindow) AllowContext(ctx context.Context) (bool, error) {
	return sw.Allow(), nil
}

// Count 返回当前窗口内的请求数（用于调试/监控）。
func (sw *SlidingWindow) Count() int {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-sw.window)

	i := 0
	for ; i < len(sw.requests); i++ {
		if sw.requests[i].After(windowStart) {
			break
		}
	}
	return len(sw.requests) - i
}

// --- 并发数限制 ---

// Concurrency 并发数限流器。
// 限制同时处理的请求数量。
type Concurrency struct {
	mu      sync.Mutex
	limit   int
	current int
}

// NewConcurrency 创建并发数限流器。
// limit 为最大并发数。
func NewConcurrency(limit int) *Concurrency {
	return &Concurrency{
		limit: limit,
	}
}

// Allow 检查是否允许请求通过。
// 注意：使用后必须调用 Release 释放。
func (c *Concurrency) Allow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.current >= c.limit {
		return false
	}
	c.current++
	return true
}

// AllowContext 带 context 的检查。
func (c *Concurrency) AllowContext(ctx context.Context) (bool, error) {
	return c.Allow(), nil
}

// Release 释放一个并发槽。
func (c *Concurrency) Release() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.current > 0 {
		c.current--
	}
}

// Current 返回当前并发数（用于调试/监控）。
func (c *Concurrency) Current() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// --- 组合限流器 ---

// Chain 组合多个限流器，所有限流器都通过才允许请求。
type Chain struct {
	limiters []Limiter
}

// NewChain 创建组合限流器。
func NewChain(limiters ...Limiter) *Chain {
	return &Chain{limiters: limiters}
}

// Allow 检查所有限流器是否都允许请求通过。
// 注意：如果某个限流器返回 false，之前已通过的限流器不会回滚。
// 警告：不要在 Chain 中使用 Concurrency 限流器，因为 Concurrency.Allow() 会增加计数，
// 若后续限流器失败不回滚 Release，会导致并发计数虚高。如需组合，请单独使用 Concurrency。
func (c *Chain) Allow() bool {
	for _, l := range c.limiters {
		if !l.Allow() {
			return false
		}
	}
	return true
}

// AllowContext 带 context 的检查。
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

// Add 添加限流器到链中。
func (c *Chain) Add(limiter Limiter) {
	c.limiters = append(c.limiters, limiter)
}
