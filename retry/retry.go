package retry

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// 错误定义。
var (
	// ErrMaxRetries 超过最大重试次数。
	ErrMaxRetries = errors.New("retry: max retries exceeded")
	// ErrNoRetry 不再重试（用于 RetryIf 返回 false 时包装最终错误）。
	ErrNoRetry = errors.New("retry: no retry")
)

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

		// 等待延迟（复用单个 Timer，避免每次创建）
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("retry: context cancelled during delay: %w", ctx.Err())
		case <-timer.C:
		}
	}

	return fmt.Errorf("%w: last error: %s", ErrMaxRetries, lastErr.Error())
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
