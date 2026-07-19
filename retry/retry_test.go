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

// --- 延迟策略测试 ---

func TestExponentialBackoff_Func(t *testing.T) {
	fn := ExponentialBackoff(10*time.Millisecond, 2)
	assert.Equal(t, 10*time.Millisecond, fn(1, 0))
	assert.Equal(t, 20*time.Millisecond, fn(2, 0))
	assert.Equal(t, 40*time.Millisecond, fn(3, 0))
}

func TestFixedDelay_Func(t *testing.T) {
	fn := FixedDelay(100 * time.Millisecond)
	assert.Equal(t, 100*time.Millisecond, fn(1, 0))
	assert.Equal(t, 100*time.Millisecond, fn(2, 0))
	assert.Equal(t, 100*time.Millisecond, fn(3, 0))
}

func TestLinearDelay_Func(t *testing.T) {
	fn := LinearDelay(10*time.Millisecond, 5*time.Millisecond)
	assert.Equal(t, 10*time.Millisecond, fn(1, 0))
	assert.Equal(t, 15*time.Millisecond, fn(2, 0))
	assert.Equal(t, 20*time.Millisecond, fn(3, 0))
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

// --- 默认配置测试 ---

func TestDefaultConfig(t *testing.T) {
	c := defaultConfig()
	assert.Equal(t, 3, c.MaxRetries)
	assert.Equal(t, 100*time.Millisecond, c.Delay)
	assert.Equal(t, 10*time.Second, c.MaxDelay)
	assert.NotNil(t, c.RetryIf)
	assert.True(t, c.RetryIf(errors.New("test")))
}

func TestDefaultConfig_WithOptions(t *testing.T) {
	c := defaultConfig(
		WithMaxRetries(10),
		WithDelay(200*time.Millisecond),
		WithMaxDelay(30*time.Second),
		WithJitter(),
	)
	assert.Equal(t, 10, c.MaxRetries)
	assert.Equal(t, 200*time.Millisecond, c.Delay)
	assert.Equal(t, 30*time.Second, c.MaxDelay)
	assert.True(t, c.Jitter)
}

func TestComputeDelay_Default(t *testing.T) {
	c := defaultConfig()
	// attempt 1: 100ms * 2^0 = 100ms
	d := computeDelay(c, 1, 0)
	assert.Equal(t, 100*time.Millisecond, d)
	// attempt 2: 100ms * 2^1 = 200ms
	d = computeDelay(c, 2, 0)
	assert.Equal(t, 200*time.Millisecond, d)
	// attempt 3: 100ms * 2^2 = 400ms
	d = computeDelay(c, 3, 0)
	assert.Equal(t, 400*time.Millisecond, d)
}

func TestComputeDelay_CustomFunc(t *testing.T) {
	c := defaultConfig(WithDelayFunc(FixedDelay(50 * time.Millisecond)))
	d := computeDelay(c, 1, 0)
	assert.Equal(t, 50*time.Millisecond, d)
}

func TestComputeDelay_MaxDelayCap(t *testing.T) {
	c := defaultConfig(WithDelay(1*time.Hour), WithMaxDelay(100*time.Millisecond))
	d := computeDelay(c, 1, 0)
	assert.Equal(t, 100*time.Millisecond, d)
}

// --- 错误常量测试 ---

func TestErrorConstants(t *testing.T) {
	assert.Equal(t, "retry: max retries exceeded", ErrMaxRetries.Error())
	assert.Equal(t, "retry: no retry", ErrNoRetry.Error())
}
