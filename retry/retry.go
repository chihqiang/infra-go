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
//
// 字段式配置无法区分"未设置"与"显式设为 0"，因此 MaxRetries/Delay/MaxDelay
// 为 0 时一律视为未设置并填充默认值（见 normalize）。
// 需要显式表示"不重试"或"零延迟"时，在 opts 中传入对应 Option
// （Option 在默认值填充之后应用，因此能生效）：
//
//	retry.DoWithRetryConfig(ctx, fn, c, retry.WithMaxRetries(0), retry.WithDelay(0))
func DoWithRetryConfig(ctx context.Context, fn func(ctx context.Context) error, c Config, opts ...Option) error {
	return doRetry(ctx, fn, normalize(c, opts...))
}

// normalize 为未显式设置的字段填充默认值，最后应用 opts 覆盖。
// DoWithRetryConfig 与 Attempts 共用本函数，保证"实际执行次数"与声明一致。
//
// 局限：无法区分"未设置"与"显式设为 0"，0 值一律按未设置处理。
// 需要表达显式 0 时通过 opts 传入——它们在默认值填充之后应用：
//
//	retry.WithMaxRetries(0) // 不重试，仅执行一次
//	retry.WithDelay(0)      // 立即重试，不等待
//	retry.WithMaxDelay(0)   // 不限制延迟上限（见 capDelay）
func normalize(c Config, opts ...Option) Config {
	if c.RetryIf == nil {
		c.RetryIf = func(error) bool { return true }
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
	// Option 在默认值填充之后应用：调用方可借此表达显式 0。
	for _, opt := range opts {
		opt(&c)
	}
	return c
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
			// 用 %w 同时包装哨兵与原始错误，使 errors.Is/As 能识别原始错误类型
			// （旧实现用 %s 拼接，错误链在此断掉，上层无法区分超时/业务错误）。
			return fmt.Errorf("%w: %w", ErrNoRetry, err)
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

	// 保留原始错误链（多重 %w），既有信息不变且 errors.Is/As 可用。
	return fmt.Errorf("%w: last error: %w", ErrMaxRetries, lastErr)
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

// Attempts 返回重试配置生效后的总执行次数（首次 + 重试）。
//
// 与 DoWithRetryConfig 使用同一套默认值与 Option 规则（normalize），
// 因此返回值就是 fn 的最大实际执行次数。
// 旧实现直接返回 c.MaxRetries+1，对零值配置会声称 1 次而实际执行 4 次。
//
// 若调用 DoWithRetryConfig 时传了 opts，这里应传同一组 opts 以保持一致：
//
//	retry.Attempts(c, retry.WithMaxRetries(0)) // 1
func Attempts(c Config, opts ...Option) int {
	return normalize(c, opts...).MaxRetries + 1
}
