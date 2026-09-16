// Package retry provides a concise retry mechanism with exponential backoff, fixed/linear
// delays, custom retry predicates, random jitter and Context cancellation.
//
// Basic usage:
//
//	err := retry.Do(ctx, func(ctx context.Context) error {
//	    return callRemote(ctx)
//	})
//
// File layout:
//
//	config.go  default constants, type definitions, Option and the With* helpers
//	retry.go   error definitions, entry points Do*/DoWithRetryConfig, core doRetry, helpers
//	delay.go   delay computation computeDelay/capDelay and the delay strategies
package retry

import "time"

// --- Default constants ---

const (
	// defaultMaxRetries is the default maximum number of retries.
	defaultMaxRetries = 3
	// defaultDelay is the default initial retry delay.
	defaultDelay = 100 * time.Millisecond
	// defaultMaxDelay is the default maximum retry delay.
	defaultMaxDelay = 10 * time.Second
)

// RetryIfFunc decides whether another retry is needed.
// Returning true means retry again and false means stop retrying.
type RetryIfFunc func(error) bool

// OnRetryFunc is the callback invoked before every retry.
// attempt is the current retry number (starting at 1).
type OnRetryFunc func(attempt int, err error)

// DelayFunc computes the retry delay.
// attempt is the current retry number (starting at 1) and previousDelay is the delay used
// last time.
type DelayFunc func(attempt int, previousDelay time.Duration) time.Duration

// Config is the retry configuration.
//
// Limitation: an explicit 0 cannot be expressed with this struct (MaxRetries=0 means no
// retry and Delay=0 means retry immediately, but a field value of 0 is treated as unset and
// filled with the default).
// Use the Option form when those semantics are needed, for example
// retry.DoWithConfig(ctx, fn, retry.WithMaxRetries(0), retry.WithDelay(0))
// or retry.DoWithRetryConfig(ctx, fn, c, retry.WithMaxRetries(0)).
type Config struct {
	// MaxRetries is the maximum number of retries; defaults to 3.
	// Total executions = MaxRetries + 1 (the first execution plus the retries).
	MaxRetries int
	// Delay is the initial retry delay; defaults to 100 milliseconds.
	Delay time.Duration
	// MaxDelay is the maximum retry delay; defaults to 10 seconds.
	// Exponential backoff never exceeds this value; WithMaxDelay(0) explicitly means no upper
	// limit.
	MaxDelay time.Duration
	// DelayFunc is a custom delay computation function.
	// Setting it overrides the default delay strategy.
	DelayFunc DelayFunc
	// RetryIf is a custom retry predicate.
	// By default every error is retried.
	RetryIf RetryIfFunc
	// OnRetry is the callback invoked before every retry.
	OnRetry OnRetryFunc
	// Jitter reports whether random jitter is added to avoid the thundering herd effect;
	// defaults to false.
	// When enabled, a random 0~50% is added on top of the delay.
	Jitter bool
}

// Option is a configuration option.
type Option func(*Config)

// WithMaxRetries sets the maximum number of retries.
func WithMaxRetries(max int) Option {
	return func(c *Config) {
		c.MaxRetries = max
	}
}

// WithDelay sets the initial retry delay.
func WithDelay(delay time.Duration) Option {
	return func(c *Config) {
		c.Delay = delay
	}
}

// WithMaxDelay sets the maximum retry delay.
func WithMaxDelay(maxDelay time.Duration) Option {
	return func(c *Config) {
		c.MaxDelay = maxDelay
	}
}

// WithDelayFunc sets a custom delay computation function.
func WithDelayFunc(fn DelayFunc) Option {
	return func(c *Config) {
		c.DelayFunc = fn
	}
}

// WithRetryIf sets a custom retry predicate.
func WithRetryIf(fn RetryIfFunc) Option {
	return func(c *Config) {
		c.RetryIf = fn
	}
}

// WithOnRetry sets the callback invoked before every retry.
func WithOnRetry(fn OnRetryFunc) Option {
	return func(c *Config) {
		c.OnRetry = fn
	}
}

// WithJitter enables random jitter.
func WithJitter() Option {
	return func(c *Config) {
		c.Jitter = true
	}
}

// defaultConfig returns a configuration populated with the default values.
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
