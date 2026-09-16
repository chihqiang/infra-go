package breaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testBuckets  = 10
	testInterval = time.Millisecond * 10
)

// getTestGoogleBreaker builds a breaker with a short window to speed up tests.
func getTestGoogleBreaker() *googleBreaker {
	return &googleBreaker{
		k:          5,
		minK:       minK,
		stat:       newRollingWindow(testBuckets, testInterval),
		proba:      newProba(),
		lastPass:   newAtomicNano(),
		protection: protection,
	}
}

func markSuccess(b *googleBreaker, count int) {
	for i := 0; i < count; i++ {
		b.markSuccess()
	}
}

func markFailed(b *googleBreaker, count int) {
	for i := 0; i < count; i++ {
		b.markFailure()
	}
}

// verify polls an assertion until the condition holds (breaker state changes take
// time).
func verify(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(testInterval)
	}
	t.Fatal("condition not met within deadline")
}

func TestGoogleBreakerClose(t *testing.T) {
	b := getTestGoogleBreaker()
	markSuccess(b, 80)
	assert.NoError(t, b.accept())
	markSuccess(b, 120)
	assert.NoError(t, b.accept())
}

func TestGoogleBreakerOpen(t *testing.T) {
	b := getTestGoogleBreaker()
	markSuccess(b, 10)
	assert.NoError(t, b.accept())
	markFailed(b, 100000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		return b.accept() != nil
	})
}

func TestGoogleBreakerRecover(t *testing.T) {
	b := getTestGoogleBreaker()

	// First generate plenty of failures to trip the breaker
	markFailed(b, 100000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		return b.accept() != nil
	})

	// Recover after consecutive successes
	markSuccess(b, 1000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		return b.accept() == nil
	})
}

func TestGoogleBreakerFallback(t *testing.T) {
	b := getTestGoogleBreaker()
	markSuccess(b, 1)
	assert.NoError(t, b.accept())
	markFailed(b, 10000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		// When the breaker is open the fallback takes effect and returns nil
		return b.doReq(func() error {
			return errors.New("any")
		}, func(error) error {
			return nil
		}, defaultAcceptable) == nil
	})
}

func TestGoogleBreakerReject(t *testing.T) {
	b := getTestGoogleBreaker()
	markSuccess(b, 100)
	assert.NoError(t, b.accept())
	markFailed(b, 10000)
	time.Sleep(testInterval)
	assert.ErrorIs(t, b.doReq(func() error {
		return ErrServiceUnavailable
	}, nil, defaultAcceptable), ErrServiceUnavailable)
}

func TestBreakerDoWithAcceptable(t *testing.T) {
	b := NewBreaker(WithName("test-ok"))

	// Business errors are accepted by acceptable, so they are not counted as
	// failures and do not trip the breaker.
	// Note: DoWithAcceptable still returns the actual error from req (acceptable only
	// decides whether it counts as a failure)
	for i := 0; i < 100; i++ {
		err := b.DoWithAcceptable(func() error {
			return errNotFound
		}, func(err error) bool {
			return errors.Is(err, errNotFound)
		})
		assert.ErrorIs(t, err, errNotFound)
	}

	// After plenty of accepted business errors the breaker is still closed
	assert.NoError(t, b.Do(func() error { return nil }))
}

func TestBreakerDoWithFallbackAcceptable(t *testing.T) {
	b := getTestGoogleBreaker()
	markFailed(b, 10000)
	time.Sleep(testInterval * 2)

	// Breaker open: fallback returns nil
	err := b.doReq(func() error {
		return errors.New("upstream down")
	}, func(err error) error {
		return nil
	}, defaultAcceptable)
	assert.NoError(t, err)
}

func TestBreakerAllowPromise(t *testing.T) {
	b := getTestGoogleBreaker()
	markSuccess(b, 10)

	promise, err := b.allow()
	require.NoError(t, err)
	promise.Accept()

	markFailed(b, 100000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		_, err := b.allow()
		return err != nil
	})
}

func TestBreakerPromiseReject(t *testing.T) {
	b := getTestGoogleBreaker()
	markSuccess(b, 10)

	promise, err := b.allow()
	require.NoError(t, err)
	// Reject path: mark a failure
	promise.Reject()

	// The breaker opens after plenty of failures
	markFailed(b, 100000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		_, err := b.allow()
		return err != nil
	})
}

func TestAtomicNanoSetLoad(t *testing.T) {
	a := newAtomicNano()
	assert.Zero(t, a.Load())

	now := time.Now().UnixNano()
	a.Set(now)
	assert.Equal(t, now, a.Load())
}

func TestRollingWindowExpiredBuckets(t *testing.T) {
	b := getTestGoogleBreaker()

	// Write data into the first bucket
	b.stat.add(success)

	// After the window expires the old data must no longer be counted
	time.Sleep(testInterval * (testBuckets + 1))
	b.stat.add(fail)

	result := b.history()
	// The old bucket expired and was reset, so only the freshly added fail remains
	assert.Equal(t, int64(1), result.total)
	assert.Equal(t, int64(0), result.accepts)
}

func TestBreakerAllowCtxCancelled(t *testing.T) {
	b := NewBreaker(WithName("test-ctx"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := b.AllowCtx(ctx)
	assert.Error(t, err)

	err = b.DoCtx(ctx, func() error { return nil })
	assert.Error(t, err)
}

func TestBreakerPanic(t *testing.T) {
	b := getTestGoogleBreaker()
	markSuccess(b, 10)

	assert.Panics(t, func() {
		_ = b.doReq(func() error {
			panic("boom")
		}, nil, defaultAcceptable)
	})
}

func TestBreakerName(t *testing.T) {
	b := NewBreaker(WithName("payment"))
	assert.Equal(t, "payment", b.Name())

	// An unspecified name falls back to the default
	d := NewBreaker()
	assert.Equal(t, "breaker", d.Name())
}

func TestGetBreaker(t *testing.T) {
	b1 := GetBreaker("my-service")
	b2 := GetBreaker("my-service")
	assert.Same(t, b1, b2, "breakers with the same name must share an instance")
	assert.Equal(t, "my-service", b1.Name())
}

func TestNoBreakerFor(t *testing.T) {
	NoBreakerFor("no-breaker")
	b := GetBreaker("no-breaker")
	assert.Equal(t, nopBreakerName, b.Name())

	// nopBreaker never trips
	for i := 0; i < 100; i++ {
		_ = b.Do(func() error {
			return errors.New("always fail")
		})
	}
	assert.NoError(t, b.Do(func() error { return nil }))
}

func TestNopBreaker(t *testing.T) {
	b := NopBreaker()

	err := b.Do(func() error { return errors.New("x") })
	assert.Error(t, err)

	promise, err := b.Allow()
	assert.NoError(t, err)
	assert.NotNil(t, promise)
}

func TestDoGlobal(t *testing.T) {
	// Global Do convenience function
	var called bool
	err := Do("global-test", func() error {
		called = true
		return nil
	})
	assert.NoError(t, err)
	assert.True(t, called)
}

func TestRollingWindowHistory(t *testing.T) {
	b := getTestGoogleBreaker()

	b.stat.add(success)
	b.stat.add(success)
	b.stat.add(fail)

	result := b.history()
	assert.Equal(t, int64(2), result.accepts)
	assert.Equal(t, int64(3), result.total)
}

// errNotFound is the sentinel business error used by the DoWithAcceptable test.
var errNotFound = errors.New("not found")

// --- SRE algorithm parameter Option tests ---

// sreOf applies opts through NewBreaker and then extracts the underlying
// googleBreaker so the parameters can be asserted.
func sreOf(opts ...Option) *googleBreaker {
	return NewBreaker(opts...).(*circuitBreaker).throttle.(*loggedThrottle).internalThrottle.(*googleBreaker)
}

func TestDefaultSREConfig(t *testing.T) {
	cfg := defaultSREConfig()
	assert.Equal(t, window, cfg.window, "the default window should be 10s")
	assert.Equal(t, k, cfg.k, "the default K should be 1.5")
	assert.Equal(t, minK, cfg.minK, "the default minK should be 1.1")
	assert.Equal(t, int64(protection), cfg.protection, "the default protection should be 5")
}

func TestWithSREDefaults(t *testing.T) {
	b := sreOf(WithSREDefaults())
	assert.Equal(t, 2*time.Minute, b.stat.interval*time.Duration(b.stat.size),
		"the SRE default window should be 2 minutes")
	assert.Equal(t, 2.0, b.k, "the SRE default K should be 2")
	assert.Equal(t, 1.1, b.minK)
	assert.Equal(t, int64(5), b.protection)
}

func TestBreakerWithWindow(t *testing.T) {
	// Explicit window: overrides the default 10s
	b := sreOf(WithWindow(time.Minute))
	assert.Equal(t, time.Minute, b.stat.interval*time.Duration(b.stat.size))

	// Invalid values are ignored and the default is kept
	b2 := sreOf(WithWindow(0), WithWindow(-time.Second))
	assert.Equal(t, window, b2.stat.interval*time.Duration(b2.stat.size),
		"a non-positive window should be ignored")
}

func TestBreakerWithK(t *testing.T) {
	b := sreOf(WithK(2))
	assert.Equal(t, 2.0, b.k)

	// Invalid values are ignored
	b2 := sreOf(WithK(0), WithK(-1))
	assert.Equal(t, k, b2.k, "a non-positive K should be ignored")
}

func TestBreakerWithMinK(t *testing.T) {
	b := sreOf(WithMinK(1.5))
	assert.Equal(t, 1.5, b.minK)

	b2 := sreOf(WithMinK(0))
	assert.Equal(t, minK, b2.minK, "a non-positive minK should be ignored")
}

func TestBreakerWithProtection(t *testing.T) {
	b := sreOf(WithProtection(20))
	assert.Equal(t, int64(20), b.protection)

	// Negative values are ignored; 0 is a valid value (disables low-traffic protection)
	b2 := sreOf(WithProtection(-1))
	assert.Equal(t, int64(protection), b2.protection, "a negative protection should be ignored")
	b3 := sreOf(WithProtection(0))
	assert.Equal(t, int64(0), b3.protection, "0 protection is valid (no protection)")
}

// TestSREDefaultsStillTrips verifies that with the SRE parameters (2min/K=2) the
// breaker still opens and recovers normally.
// A short window is used as an approximation: build a short-window googleBreaker
// directly but apply the SRE K=2.
func TestSREDefaultsStillTrips(t *testing.T) {
	b := &googleBreaker{
		k:          2,
		minK:       minK,
		protection: 5,
		stat:       newRollingWindow(testBuckets, testInterval),
		proba:      newProba(),
		lastPass:   newAtomicNano(),
	}
	markSuccess(b, 10)
	assert.NoError(t, b.accept())
	markFailed(b, 100000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		return b.accept() != nil
	})

	// Recover
	markSuccess(b, 1000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool {
		return b.accept() == nil
	})
}

// TestWithSREDefaultsEndToEnd goes through the public NewBreaker interface to
// verify that the SRE parameters can be created normally.
func TestWithSREDefaultsEndToEnd(t *testing.T) {
	b := NewBreaker(WithName("sre"), WithSREDefaults())
	assert.NoError(t, b.Do(func() error { return nil }))
	assert.Equal(t, "sre", b.Name())
}
