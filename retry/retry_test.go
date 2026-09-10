package retry

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- 执行测试 ---

func TestDo_Success(t *testing.T) {
	var calls int32
	err := Do(context.Background(), func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestDo_RetryThenSuccess(t *testing.T) {
	var calls int32
	err := DoWithConfig(context.Background(), func(ctx context.Context) error {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return errors.New("temporary error")
		}
		return nil
	}, WithMaxRetries(5), WithDelay(1*time.Millisecond))
	require.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&calls))
}

func TestDo_MaxRetriesExceeded(t *testing.T) {
	var calls int32
	err := DoWithConfig(context.Background(), func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("permanent error")
	}, WithMaxRetries(3), WithDelay(1*time.Millisecond))
	require.Error(t, err)
	assert.True(t, IsMaxRetries(err))
	assert.Equal(t, int32(4), atomic.LoadInt32(&calls)) // 1 + 3 retries
}

func TestDo_RetryIf_False(t *testing.T) {
	var calls int32
	err := DoWithConfig(context.Background(), func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("non-retryable error")
	}, WithMaxRetries(5), WithDelay(1*time.Millisecond), WithRetryIf(func(err error) bool {
		return false // 不重试
	}))
	require.Error(t, err)
	assert.True(t, IsNoRetry(err))
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls)) // 只调用一次
}

func TestDo_OnRetry(t *testing.T) {
	var retryCalls int32
	var calls int32
	err := DoWithConfig(context.Background(), func(ctx context.Context) error {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return errors.New("error")
		}
		return nil
	}, WithMaxRetries(5), WithDelay(1*time.Millisecond), WithOnRetry(func(attempt int, err error) {
		atomic.AddInt32(&retryCalls, 1)
		assert.NotEqual(t, 0, attempt)
	}))
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&retryCalls)) // 重试了 2 次
}

func TestDo_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var calls int32
	err := DoWithConfig(ctx, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("error")
	}, WithDelay(1*time.Millisecond))
	require.Error(t, err)
	assert.Equal(t, int32(0), atomic.LoadInt32(&calls))
}

func TestDo_ContextCancelledDuringDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var calls int32
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := DoWithConfig(ctx, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("error")
	}, WithMaxRetries(10), WithDelay(1*time.Second))
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestDo_ExponentialBackoff(t *testing.T) {
	var calls int32
	var delays []time.Duration
	var lastTime time.Time

	err := DoWithConfig(context.Background(), func(ctx context.Context) error {
		now := time.Now()
		if !lastTime.IsZero() {
			delays = append(delays, now.Sub(lastTime))
		}
		lastTime = now
		n := atomic.AddInt32(&calls, 1)
		if n < 4 {
			return errors.New("error")
		}
		return nil
	}, WithMaxRetries(5), WithDelay(10*time.Millisecond))
	require.NoError(t, err)
	assert.Equal(t, int32(4), atomic.LoadInt32(&calls))

	// 验证延迟递增（指数退避）
	// delays[0] ~ 10ms, delays[1] ~ 20ms, delays[2] ~ 40ms
	require.Len(t, delays, 3)
	assert.Greater(t, delays[1], delays[0])
	assert.Greater(t, delays[2], delays[1])
}

func TestDo_FixedDelay(t *testing.T) {
	var calls int32
	err := DoWithConfig(context.Background(), func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("error")
	}, WithMaxRetries(3), WithDelayFunc(FixedDelay(5*time.Millisecond)))
	require.Error(t, err)
	assert.Equal(t, int32(4), atomic.LoadInt32(&calls))
}

func TestDo_LinearDelay(t *testing.T) {
	var calls int32
	err := DoWithConfig(context.Background(), func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("error")
	}, WithMaxRetries(3), WithDelayFunc(LinearDelay(5*time.Millisecond, 5*time.Millisecond)))
	require.Error(t, err)
	assert.Equal(t, int32(4), atomic.LoadInt32(&calls))
}

func TestDo_ExponentialBackoffFunc(t *testing.T) {
	var calls int32
	err := DoWithConfig(context.Background(), func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("error")
	}, WithMaxRetries(3), WithDelayFunc(ExponentialBackoff(5*time.Millisecond, 3)))
	require.Error(t, err)
	assert.Equal(t, int32(4), atomic.LoadInt32(&calls))
}

func TestDo_Jitter(t *testing.T) {
	var calls int32
	err := DoWithConfig(context.Background(), func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("error")
	}, WithMaxRetries(3), WithDelay(5*time.Millisecond), WithJitter())
	require.Error(t, err)
	assert.Equal(t, int32(4), atomic.LoadInt32(&calls))
}

func TestDo_MaxDelayCap(t *testing.T) {
	var calls int32
	start := time.Now()
	err := DoWithConfig(context.Background(), func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("error")
	}, WithMaxRetries(3), WithDelay(1*time.Second), WithMaxDelay(50*time.Millisecond))
	require.Error(t, err)
	elapsed := time.Since(start)

	// 3 次重试，延迟不应超过 3 * 50ms = 150ms（加上一些开销）
	assert.Less(t, elapsed, 300*time.Millisecond)
}

func TestDo_NilFunction(t *testing.T) {
	err := Do(context.Background(), func(ctx context.Context) error {
		return nil
	})
	require.NoError(t, err)
}

// --- 辅助函数测试 ---

func TestIsMaxRetries(t *testing.T) {
	assert.True(t, IsMaxRetries(ErrMaxRetries))
	wrapped := fmt.Errorf("%w: last error: test", ErrMaxRetries)
	assert.True(t, IsMaxRetries(wrapped))
	assert.False(t, IsMaxRetries(ErrNoRetry))
	assert.False(t, IsMaxRetries(nil))
}

func TestIsNoRetry(t *testing.T) {
	assert.True(t, IsNoRetry(ErrNoRetry))
	assert.False(t, IsNoRetry(ErrMaxRetries))
	assert.False(t, IsNoRetry(nil))
}

func TestAttempts(t *testing.T) {
	c := Config{MaxRetries: 5}
	assert.Equal(t, 6, Attempts(c))
}

// --- 错误链保留（回归：此前用 %s 拼接，errors.Is/As 失效）---

// retryTestHTTPError 是用于验证 errors.As 的自定义错误类型。
// 必须定义在包级别：Go 不允许为函数内定义的局部类型声明方法。
type retryTestHTTPError struct{ Code int }

func (e *retryTestHTTPError) Error() string {
	return fmt.Sprintf("http error: %d", e.Code)
}

// TestErrorChain_MaxRetries 验证超过重试次数时，原始错误仍可通过 errors.Is/As 识别。
// 历史缺陷：fmt.Errorf("%w: last error: %s", ...) 把原始错误降级为文本，
// 上层无法判断到底是超时、连接被拒还是业务错误。
func TestErrorChain_MaxRetries(t *testing.T) {
	sentinel := errors.New("downstream unavailable")
	calls := 0

	err := DoWithRetryConfig(context.Background(), func(context.Context) error {
		calls++
		return sentinel
	}, Config{MaxRetries: 1, Delay: time.Millisecond})

	require.Error(t, err)
	assert.Equal(t, 2, calls)

	// 哨兵与原始错误都必须可识别
	assert.True(t, IsMaxRetries(err))
	assert.True(t, errors.Is(err, ErrMaxRetries))
	assert.True(t, errors.Is(err, sentinel), "underlying error must stay in the chain")

	// 错误信息保持可读
	assert.Contains(t, err.Error(), "max retries exceeded")
	assert.Contains(t, err.Error(), "downstream unavailable")
}

// TestErrorChain_ContextDeadline 验证包装后仍能识别 context 错误。
func TestErrorChain_ContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := DoWithRetryConfig(ctx, func(context.Context) error {
		return context.DeadlineExceeded
	}, Config{MaxRetries: 2, Delay: 100 * time.Millisecond})

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled),
		"context error must remain detectable, got: %v", err)
}

// TestErrorChain_NoRetry 验证 RetryIf 返回 false 时同样保留错误链。
func TestErrorChain_NoRetry(t *testing.T) {
	sentinel := errors.New("fatal: bad request")

	err := DoWithRetryConfig(context.Background(), func(context.Context) error {
		return sentinel
	}, Config{
		MaxRetries: 3,
		Delay:      time.Millisecond,
		RetryIf:    func(error) bool { return false },
	})

	require.Error(t, err)
	assert.True(t, IsNoRetry(err))
	assert.True(t, errors.Is(err, sentinel), "underlying error must stay in the chain")
	assert.Contains(t, err.Error(), "fatal: bad request")
}

// TestErrorChain_CustomErrorType 验证自定义错误类型可用 errors.As 提取。
func TestErrorChain_CustomErrorType(t *testing.T) {
	sentinel := &retryTestHTTPError{Code: 503}

	err := DoWithRetryConfig(context.Background(), func(context.Context) error {
		return sentinel
	}, Config{MaxRetries: 1, Delay: time.Millisecond})

	require.Error(t, err)

	var target *retryTestHTTPError
	require.True(t, errors.As(err, &target), "errors.As must reach the original error")
	assert.Equal(t, 503, target.Code)
}

// --- Attempts 与实际执行次数一致 ---

// TestAttempts_MatchesActualExecutions 验证 Attempts 的返回值与真实执行次数一致。
// 历史缺陷：Attempts 直接返回 MaxRetries+1，而 DoWithRetryConfig 会把 0 当作
// 未设置填充为默认 3，两者对同一份 Config 给出矛盾结论（1 次 vs 实际 4 次）。
func TestAttempts_MatchesActualExecutions(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"zero config uses defaults", Config{}},
		{"explicit max retries", Config{MaxRetries: 2}},
		{"max retries 1", Config{MaxRetries: 1}},
		{"max retries 5", Config{MaxRetries: 5}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			cfg := tc.cfg
			cfg.Delay = time.Millisecond

			_ = DoWithRetryConfig(context.Background(), func(context.Context) error {
				calls++
				return errors.New("boom")
			}, cfg)

			expected := Attempts(tc.cfg)
			assert.Equal(t, expected, calls,
				"Attempts() must equal the number of executions")
		})
	}
}

// TestAttempts_ZeroConfigReportsDefaults 验证零值配置报告的是生效后的次数。
func TestAttempts_ZeroConfigReportsDefaults(t *testing.T) {
	assert.Equal(t, defaultMaxRetries+1, Attempts(Config{}))
}

// TestWithMaxRetriesZero_ExecutesOnce 验证 Option 路径可以表达"不重试"（0 次重试）。
// 字段式配置无法表达 0（会被当作未设置），这是 Option 路径存在的意义。
func TestWithMaxRetriesZero_ExecutesOnce(t *testing.T) {
	calls := 0
	err := DoWithConfig(context.Background(), func(context.Context) error {
		calls++
		return errors.New("boom")
	}, WithMaxRetries(0), WithDelay(time.Millisecond))

	require.Error(t, err)
	assert.Equal(t, 1, calls, "WithMaxRetries(0) must execute exactly once")
}

// --- 错误常量测试 ---

func TestErrorConstants(t *testing.T) {
	assert.Equal(t, "retry: max retries exceeded", ErrMaxRetries.Error())
	assert.Equal(t, "retry: no retry", ErrNoRetry.Error())
}
