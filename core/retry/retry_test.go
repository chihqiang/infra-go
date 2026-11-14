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

// --- Execution tests ---

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
		return false // do not retry
	}))
	require.Error(t, err)
	assert.True(t, IsNoRetry(err))
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls)) // called only once
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
	assert.Equal(t, int32(2), atomic.LoadInt32(&retryCalls)) // retried twice
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

	// Verify that the delays grow (exponential backoff)
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

	// 3 retries, so the delay must not exceed 3 * 50ms = 150ms (plus some overhead)
	assert.Less(t, elapsed, 300*time.Millisecond)
}

func TestDo_NilFunction(t *testing.T) {
	err := Do(context.Background(), func(ctx context.Context) error {
		return nil
	})
	require.NoError(t, err)
}

// --- Helper function tests ---

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

// TestAttempts_WithOption verifies that Attempts and DoWithRetryConfig share the same defaults
// and Option rules, so passing the same group of opts makes the result match the actual number
// of executions.
func TestAttempts_WithOption(t *testing.T) {
	assert.Equal(t, 1, Attempts(Config{}, WithMaxRetries(0)),
		"an explicit no-retry configuration executes exactly once")
	assert.Equal(t, defaultMaxRetries+1, Attempts(Config{}))
}

// TestDoWithRetryConfig_ExplicitZeroRetries verifies that "no retry", which a struct
// configuration cannot express, can be expressed with WithMaxRetries(0) and is not filled in
// as the default 3 by normalize.
func TestDoWithRetryConfig_ExplicitZeroRetries(t *testing.T) {
	var calls int32
	err := DoWithRetryConfig(context.Background(), func(context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("boom")
	}, Config{MaxRetries: 9}, WithMaxRetries(0))

	assert.ErrorIs(t, err, ErrMaxRetries)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "WithMaxRetries(0) must execute only once")
}

// TestDoWithRetryConfig_ExplicitZeroDelay verifies that WithDelay(0) means retrying
// immediately instead of waiting for the default 100ms initial delay.
func TestDoWithRetryConfig_ExplicitZeroDelay(t *testing.T) {
	var calls int32
	start := time.Now()
	err := DoWithRetryConfig(context.Background(), func(context.Context) error {
		if atomic.AddInt32(&calls, 1) < 2 {
			return errors.New("boom")
		}
		return nil
	}, Config{}, WithDelay(0), WithMaxDelay(0))

	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
	assert.Less(t, time.Since(start), 100*time.Millisecond, "a zero delay must retry immediately")
}

// --- Error chain preservation (regression: previously formatted with %s, which broke
// errors.Is/As) ---

// retryTestHTTPError is a custom error type used to verify errors.As.
// It must be declared at package level: Go does not allow declaring methods on a local type
// defined inside a function.
type retryTestHTTPError struct{ Code int }

func (e *retryTestHTTPError) Error() string {
	return fmt.Sprintf("http error: %d", e.Code)
}

// TestErrorChain_MaxRetries verifies that the original error can still be recognised through
// errors.Is/As once the retry count is exceeded.
// Historical defect: fmt.Errorf("%w: last error: %s", ...) degraded the original error to
// plain text, so callers could not tell a timeout from a refused connection or a business
// error.
func TestErrorChain_MaxRetries(t *testing.T) {
	sentinel := errors.New("downstream unavailable")
	calls := 0

	err := DoWithRetryConfig(context.Background(), func(context.Context) error {
		calls++
		return sentinel
	}, Config{MaxRetries: 1, Delay: time.Millisecond})

	require.Error(t, err)
	assert.Equal(t, 2, calls)

	// Both the sentinel and the original error must be recognisable
	assert.True(t, IsMaxRetries(err))
	assert.True(t, errors.Is(err, ErrMaxRetries))
	assert.True(t, errors.Is(err, sentinel), "underlying error must stay in the chain")

	// The error message stays readable
	assert.Contains(t, err.Error(), "max retries exceeded")
	assert.Contains(t, err.Error(), "downstream unavailable")
}

// TestErrorChain_ContextDeadline verifies that context errors remain recognisable after
// wrapping.
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

// TestErrorChain_NoRetry verifies that the error chain is preserved when RetryIf returns
// false.
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

// TestErrorChain_CustomErrorType verifies that a custom error type can be extracted with
// errors.As.
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

// --- Attempts matches the actual number of executions ---

// TestAttempts_MatchesActualExecutions verifies that the value returned by Attempts matches the
// real number of executions.
// Historical defect: Attempts returned MaxRetries+1 directly, while DoWithRetryConfig treated
// 0 as unset and filled in the default 3, so the two contradicted each other for the same
// Config (1 execution versus 4 in practice).
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

// TestAttempts_ZeroConfigReportsDefaults verifies that a zero-valued configuration reports the
// number of executions that actually take effect.
func TestAttempts_ZeroConfigReportsDefaults(t *testing.T) {
	assert.Equal(t, defaultMaxRetries+1, Attempts(Config{}))
}

// TestWithMaxRetriesZero_ExecutesOnce verifies that the Option path can express "no retry"
// (0 retries).
// A field-based configuration cannot express 0 (it would be treated as unset), which is why the
// Option path exists.
func TestWithMaxRetriesZero_ExecutesOnce(t *testing.T) {
	calls := 0
	err := DoWithConfig(context.Background(), func(context.Context) error {
		calls++
		return errors.New("boom")
	}, WithMaxRetries(0), WithDelay(time.Millisecond))

	require.Error(t, err)
	assert.Equal(t, 1, calls, "WithMaxRetries(0) must execute exactly once")
}

// --- Error constant tests ---

func TestErrorConstants(t *testing.T) {
	assert.Equal(t, "retry: max retries exceeded", ErrMaxRetries.Error())
	assert.Equal(t, "retry: no retry", ErrNoRetry.Error())
}
