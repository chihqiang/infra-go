package retry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// --- 默认常量 ---

const (
	// defaultMaxRetries 默认最大重试次数。
	defaultMaxRetries = 3
	// defaultDelay 默认初始重试延迟。
	defaultDelay = 100 * time.Millisecond
	// defaultMaxDelay 默认最大重试延迟。
	defaultMaxDelay = 10 * time.Second
)

// 错误定义。
var (
	// ErrMaxRetries 超过最大重试次数。
	ErrMaxRetries = errors.New("retry: max retries exceeded")
	// ErrNoRetry 不再重试（用于 RetryIf 返回 false 时包装最终错误）。
	ErrNoRetry = errors.New("retry: no retry")
)

// RetryIfFunc 判断是否需要重试的函数。
// 返回 true 表示需要重试，false 表示不再重试。
type RetryIfFunc func(error) bool

// OnRetryFunc 每次重试前的回调函数。
// attempt 为当前重试次数（从 1 开始）。
type OnRetryFunc func(attempt int, err error)

// DelayFunc 计算重试延迟时间的函数。
// attempt 为当前重试次数（从 1 开始），上一次的延迟为 previousDelay。
type DelayFunc func(attempt int, previousDelay time.Duration) time.Duration

// Config 重试配置。
type Config struct {
	// MaxRetries 最大重试次数，默认 3。
	// 总执行次数 = MaxRetries + 1（首次执行 + 重试次数）。
	MaxRetries int
	// Delay 初始重试延迟，默认 100 毫秒。
	Delay time.Duration
	// MaxDelay 最大重试延迟，默认 10 秒。
	// 指数退避时延迟不会超过此值。
	MaxDelay time.Duration
	// DelayFunc 自定义延迟计算函数。
	// 设置后会覆盖默认的延迟策略。
	DelayFunc DelayFunc
	// RetryIf 自定义重试判定函数。
	// 默认所有 error 都重试。
	RetryIf RetryIfFunc
	// OnRetry 每次重试前的回调函数。
	OnRetry OnRetryFunc
	// Jitter 是否添加随机抖动，避免惊群效应，默认 false。
	// 启用后会在延迟基础上添加 0~50% 的随机时间。
	Jitter bool
}

// Option 配置选项。
type Option func(*Config)

// WithMaxRetries 设置最大重试次数。
func WithMaxRetries(max int) Option {
	return func(c *Config) {
		c.MaxRetries = max
	}
}

// WithDelay 设置初始重试延迟。
func WithDelay(delay time.Duration) Option {
	return func(c *Config) {
		c.Delay = delay
	}
}

// WithMaxDelay 设置最大重试延迟。
func WithMaxDelay(maxDelay time.Duration) Option {
	return func(c *Config) {
		c.MaxDelay = maxDelay
	}
}

// WithDelayFunc 设置自定义延迟计算函数。
func WithDelayFunc(fn DelayFunc) Option {
	return func(c *Config) {
		c.DelayFunc = fn
	}
}

// WithRetryIf 设置自定义重试判定函数。
func WithRetryIf(fn RetryIfFunc) Option {
	return func(c *Config) {
		c.RetryIf = fn
	}
}

// WithOnRetry 设置每次重试前的回调函数。
func WithOnRetry(fn OnRetryFunc) Option {
	return func(c *Config) {
		c.OnRetry = fn
	}
}

// WithJitter 启用随机抖动。
func WithJitter() Option {
	return func(c *Config) {
		c.Jitter = true
	}
}

// defaultConfig 返回带默认值的配置。
func defaultConfig(opts ...Option) Config {
	c := Config{
		MaxRetries: defaultMaxRetries,
		Delay:      defaultDelay,
		MaxDelay:   defaultMaxDelay,
		RetryIf:    func(err error) bool { return true },
	}
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// Do 执行函数，失败时根据配置自动重试。
// 使用默认配置。
func Do(ctx context.Context, fn func(ctx context.Context) error) error {
	c := defaultConfig()
	return doRetry(ctx, fn, c)
}

// DoWithConfig 执行函数，失败时根据配置自动重试。
func DoWithConfig(ctx context.Context, fn func(ctx context.Context) error, opts ...Option) error {
	c := defaultConfig(opts...)
	return doRetry(ctx, fn, c)
}

// DoWithRetryConfig 执行函数，失败时根据指定配置自动重试。
func DoWithRetryConfig(ctx context.Context, fn func(ctx context.Context) error, c Config) error {
	if c.RetryIf == nil {
		c.RetryIf = func(err error) bool { return true }
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = defaultMaxRetries
	}
	if c.Delay == 0 {
		c.Delay = defaultDelay
	}
	if c.MaxDelay == 0 {
		c.MaxDelay = defaultMaxDelay
	}
	return doRetry(ctx, fn, c)
}

// doRetry 重试核心逻辑。
func doRetry(ctx context.Context, fn func(ctx context.Context) error, c Config) error {
	var lastErr error
	var delay time.Duration

	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		// 检查 context 是否已取消
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("retry: context cancelled: %w", err)
		}

		// 执行函数
		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err

		// 最后一次不再重试
		if attempt >= c.MaxRetries {
			break
		}

		// 检查是否需要重试
		if !c.RetryIf(err) {
			return fmt.Errorf("%w: %s", ErrNoRetry, err.Error())
		}

		// 计算延迟
		delay = computeDelay(c, attempt+1, delay)

		// 回调
		if c.OnRetry != nil {
			c.OnRetry(attempt+1, err)
		}

		// 等待延迟
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry: context cancelled during delay: %w", ctx.Err())
		case <-time.After(delay):
		}
	}

	return fmt.Errorf("%w: last error: %s", ErrMaxRetries, lastErr.Error())
}

// computeDelay 计算重试延迟。
func computeDelay(c Config, attempt int, previousDelay time.Duration) time.Duration {
	// 如果有自定义延迟函数，使用它
	if c.DelayFunc != nil {
		d := c.DelayFunc(attempt, previousDelay)
		return capDelay(d, c.MaxDelay, c.Jitter)
	}

	// 默认指数退避：delay * 2^(attempt-1)
	d := time.Duration(float64(c.Delay) * math.Pow(2, float64(attempt-1)))
	return capDelay(d, c.MaxDelay, c.Jitter)
}

// capDelay 限制延迟不超过最大值，并可选添加抖动。
func capDelay(d, maxDelay time.Duration, jitter bool) time.Duration {
	if d > maxDelay {
		d = maxDelay
	}
	if d < 0 {
		d = 0
	}
	if jitter && d > 0 {
		// 添加 0~50% 的随机抖动
		half := int64(d) / 2
		if half > 0 {
			jitterAmount := time.Duration(rand.Int64N(half))
			d += jitterAmount
			if d > maxDelay {
				d = maxDelay
			}
		}
	}
	return d
}

// --- 延迟策略 ---

// ExponentialBackoff 指数退避延迟。
// base 为基础延迟，factor 为乘数因子，attempt 为当前重试次数。
func ExponentialBackoff(base time.Duration, factor float64) DelayFunc {
	return func(attempt int, _ time.Duration) time.Duration {
		return time.Duration(float64(base) * math.Pow(factor, float64(attempt-1)))
	}
}

// FixedDelay 固定延迟。
func FixedDelay(delay time.Duration) DelayFunc {
	return func(_ int, _ time.Duration) time.Duration {
		return delay
	}
}

// LinearDelay 线性增长延迟。
// base 为基础延迟，increment 为每次重试的增加量。
func LinearDelay(base, increment time.Duration) DelayFunc {
	return func(attempt int, _ time.Duration) time.Duration {
		return base + time.Duration(attempt-1)*increment
	}
}

// --- 辅助函数 ---

// IsMaxRetries 判断错误是否为超过最大重试次数。
func IsMaxRetries(err error) bool {
	return errors.Is(err, ErrMaxRetries)
}

// IsNoRetry 判断错误是否为不再重试。
func IsNoRetry(err error) bool {
	return errors.Is(err, ErrNoRetry)
}

// Attempts 返回重试配置中的总执行次数（首次 + 重试）。
func Attempts(c Config) int {
	return c.MaxRetries + 1
}
