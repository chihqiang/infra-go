// Package retry 提供简洁的重试机制，支持指数退避、固定/线性延迟、自定义重试判定、随机抖动与 Context 取消。
//
// 基本用法：
//
//	err := retry.Do(ctx, func(ctx context.Context) error {
//	    return callRemote(ctx)
//	})
//
// 文件组织：
//
//	config.go  默认常量、类型定义、Option 与 With* 系列
//	retry.go   错误定义、入口 Do*/DoWithRetryConfig、核心 doRetry、辅助函数
//	delay.go   延迟计算 computeDelay/capDelay 与延迟策略
package retry

import "time"

// --- 默认常量 ---

const (
	// defaultMaxRetries 默认最大重试次数。
	defaultMaxRetries = 3
	// defaultDelay 默认初始重试延迟。
	defaultDelay = 100 * time.Millisecond
	// defaultMaxDelay 默认最大重试延迟。
	defaultMaxDelay = 10 * time.Second
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
