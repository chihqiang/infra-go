package breaker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file focuses on coverage of the public facade (circuitBreaker/NewBreaker), the
// package-level convenience functions (breakers.go) and the whole nopBreaker API:
// those paths used to be largely at 0%.
// It reuses the white-box helpers from breaker_test.go: getTestGoogleBreaker/markFailed/verify.

// trippedGoogleBreaker builds an internal googleBreaker that is already open
// (rejecting requests).
func trippedGoogleBreaker(t *testing.T) *googleBreaker {
	t.Helper()
	b := getTestGoogleBreaker()
	markFailed(b, 100000)
	time.Sleep(testInterval * 2)
	verify(t, func() bool { return b.accept() != nil })
	return b
}

// facadeWithThrottle builds the public facade but swaps the underlying algorithm for
// inner, so the state can be controlled precisely.
func facadeWithThrottle(t *testing.T, inner internalThrottle) *circuitBreaker {
	t.Helper()
	cb := NewBreaker(WithName("facade")).(*circuitBreaker)
	cb.throttle = newLoggedThrottle(cb.name, inner)
	return cb
}

// TestFacade_AllowAndPromise covers the public Allow() success path, the Promise
// Accept/Reject methods and the errorWindow record
// (promiseWithReason.Reject → errorWindow.add).
func TestFacade_AllowAndPromise(t *testing.T) {
	b := NewBreaker(WithName("facade-promise"))

	// Accept: no failure reason is recorded
	promise, err := b.Allow()
	require.NoError(t, err)
	promise.Accept()

	// Reject(reason): records the most recent failure reason
	promise, err = b.Allow()
	require.NoError(t, err)
	promise.Reject("downstream timeout")

	// errorWindow should have recorded that reason (white-box assertion)
	cb := b.(*circuitBreaker)
	ew := cb.throttle.(*loggedThrottle).errWin
	assert.Contains(t, ew.String(), "downstream timeout")
}

// TestFacade_CtxNormalVariants covers the normal (default) branch of each public Ctx
// method: the existing tests only covered the ctx-cancelled branch, so the normal
// pass-through path is added here.
func TestFacade_CtxNormalVariants(t *testing.T) {
	b := NewBreaker(WithName("facade-ctx-normal"))
	ctx := context.Background()

	// AllowCtx, normal case
	promise, err := b.AllowCtx(ctx)
	require.NoError(t, err)
	promise.Accept()

	// DoCtx, normal case
	require.NoError(t, b.DoCtx(ctx, func() error { return nil }))

	// DoWithAcceptableCtx, normal case: the business error is accepted by acceptable
	// and not counted as a failure
	boom := errors.New("business error")
	err = b.DoWithAcceptableCtx(ctx, func() error { return boom }, func(err error) bool {
		return errors.Is(err, boom)
	})
	assert.ErrorIs(t, err, boom)
	// The breaker stays closed after plenty of accepted errors
	assert.NoError(t, b.DoCtx(ctx, func() error { return nil }))
}

// TestFacade_OpenState covers the public Allow/Do returning ErrServiceUnavailable
// when the breaker is open, and drives the full loggedThrottle.logError branch
// (which prints errorWindow.String).
func TestFacade_OpenState(t *testing.T) {
	b := facadeWithThrottle(t, trippedGoogleBreaker(t))

	_, err := b.Allow()
	assert.ErrorIs(t, err, ErrServiceUnavailable)

	err = b.Do(func() error { return nil })
	assert.ErrorIs(t, err, ErrServiceUnavailable)

	err = b.DoWithAcceptable(func() error { return nil }, defaultAcceptable)
	assert.ErrorIs(t, err, ErrServiceUnavailable)
}

// TestFacade_RejectedPromiseIsNil is a regression test: Allow must return a nil
// Promise when the breaker is open.
// Historical defect: it always returned a promiseWithReason whose inner promise was
// nil (a non-nil shell), so callers that ignore err and use the value directly
// (e.g. a deferred p.Accept()) panicked with a nil dereference, and it also broke
// the Breaker.Allow contract.
func TestFacade_RejectedPromiseIsNil(t *testing.T) {
	b := facadeWithThrottle(t, trippedGoogleBreaker(t))

	promise, err := b.Allow()
	require.ErrorIs(t, err, ErrServiceUnavailable)
	assert.Nil(t, promise, "rejected Allow must return a nil Promise")

	// When allowed it still returns a usable Promise
	b2 := NewBreaker(WithName("facade-promise-ok"))
	promise, err = b2.Allow()
	require.NoError(t, err)
	require.NotNil(t, promise, "allowed Allow must return a usable Promise")
	require.NotPanics(t, func() { promise.Accept() })
}

// TestErrorWindow_ConcurrentStringAndAdd is a regression test for concurrent
// reads and writes of errorWindow.
// Historical defect: String() read ew.count before taking the lock to make the
// slice, racing with add, which updates count under the lock (detectable with -race).
func TestErrorWindow_ConcurrentStringAndAdd(t *testing.T) {
	b := facadeWithThrottle(t, trippedGoogleBreaker(t))
	ew := b.throttle.(*loggedThrottle).errWin

	const workers = 16
	const iterations = 200

	var wg sync.WaitGroup
	// Concurrent writers: simulate many requests reporting failure reasons at once
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				ew.add(fmt.Sprintf("worker-%d-err-%d", n, j))
			}
		}(i)
	}
	// Concurrent readers: logError calls String() when the breaker is open
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = ew.String()
			}
		}()
	}
	wg.Wait()

	// The ring buffer keeps at most numHistoryReasons entries
	assert.LessOrEqual(t, len(strings.Split(ew.String(), "\n")), numHistoryReasons)
}

// TestFacade_ConcurrentOpenAndAllow calls Allow concurrently while the breaker is
// open, covering the concurrent path of logError + errorWindow.String (checked with
// -race).
func TestFacade_ConcurrentOpenAndAllow(t *testing.T) {
	b := facadeWithThrottle(t, trippedGoogleBreaker(t))

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = b.Allow()
		}()
	}
	wg.Wait()
}

// TestFacade_FallbackVariantsWhenOpen covers each Fallback variant running the
// degradation logic while the breaker is open.
func TestFacade_FallbackVariantsWhenOpen(t *testing.T) {
	fbErr := errors.New("fallback-result")

	t.Run("DoWithFallback", func(t *testing.T) {
		b := facadeWithThrottle(t, trippedGoogleBreaker(t))
		err := b.DoWithFallback(func() error { return errors.New("upstream down") },
			func(err error) error {
				assert.ErrorIs(t, err, ErrServiceUnavailable)
				return fbErr
			})
		assert.ErrorIs(t, err, fbErr)
	})

	t.Run("DoWithFallbackCtx", func(t *testing.T) {
		b := facadeWithThrottle(t, trippedGoogleBreaker(t))
		err := b.DoWithFallbackCtx(context.Background(),
			func() error { return errors.New("upstream down") },
			func(err error) error {
				assert.ErrorIs(t, err, ErrServiceUnavailable)
				return fbErr
			})
		assert.ErrorIs(t, err, fbErr)
	})

	t.Run("DoWithFallbackAcceptable", func(t *testing.T) {
		b := facadeWithThrottle(t, trippedGoogleBreaker(t))
		err := b.DoWithFallbackAcceptable(
			func() error { return errors.New("upstream down") },
			func(err error) error {
				assert.ErrorIs(t, err, ErrServiceUnavailable)
				return fbErr
			},
			func(err error) bool { return false })
		assert.ErrorIs(t, err, fbErr)
	})

	t.Run("DoWithFallbackAcceptableCtx", func(t *testing.T) {
		b := facadeWithThrottle(t, trippedGoogleBreaker(t))
		err := b.DoWithFallbackAcceptableCtx(context.Background(),
			func() error { return errors.New("upstream down") },
			func(err error) error {
				assert.ErrorIs(t, err, ErrServiceUnavailable)
				return fbErr
			},
			func(err error) bool { return false })
		assert.ErrorIs(t, err, fbErr)
	})
}

// TestFacade_FallbackIgnoredWhenClosed covers that fallback does not step in while
// the breaker is closed and the request error is returned as is (only acceptable
// decides whether it counts as a failure).
func TestFacade_FallbackIgnoredWhenClosed(t *testing.T) {
	b := facadeWithThrottle(t, getTestGoogleBreaker())
	boom := errors.New("boom")

	err := b.DoWithFallback(func() error { return boom },
		func(error) error {
			t.Fatal("fallback must not run when breaker is closed")
			return nil
		})
	assert.ErrorIs(t, err, boom)
}

// TestFacade_CtxCancelled covers the context-cancelled branch of the three public
// Ctx variants (DoWithAcceptableCtx/DoWithFallbackCtx/DoWithFallbackAcceptableCtx).
func TestFacade_CtxCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := NewBreaker(WithName("facade-ctx-cancel"))

	err := b.DoWithAcceptableCtx(ctx, func() error { return nil }, defaultAcceptable)
	assert.Error(t, err)

	err = b.DoWithFallbackCtx(ctx, func() error { return nil }, func(error) error { return nil })
	assert.Error(t, err)

	err = b.DoWithFallbackAcceptableCtx(ctx, func() error { return nil },
		func(error) error { return nil }, defaultAcceptable)
	assert.Error(t, err)
}

// TestNopBreakerFullAPI covers every nopBreaker method apart from Do/Allow, plus the
// nopPromise Accept/Reject methods (all previously at 0%).
func TestNopBreakerFullAPI(t *testing.T) {
	b := NopBreaker()
	ctx := context.Background()

	// AllowCtx returns nopPromise
	promise, err := b.AllowCtx(ctx)
	require.NoError(t, err)
	promise.Accept()
	promise.Reject("ignored")

	// Each Do variant: req runs directly, acceptable/fallback are ignored, and a panic
	// does not trip anything
	called := 0
	req := func() error { called++; return nil }
	assert.NoError(t, b.DoCtx(ctx, req))
	assert.NoError(t, b.DoWithAcceptable(req, func(error) bool { return false }))
	assert.NoError(t, b.DoWithAcceptableCtx(ctx, req, func(error) bool { return false }))
	assert.NoError(t, b.DoWithFallback(req, func(error) error { return errors.New("fb") }))
	assert.NoError(t, b.DoWithFallbackCtx(ctx, req, func(error) error { return errors.New("fb") }))
	assert.NoError(t, b.DoWithFallbackAcceptable(req, func(error) error { return errors.New("fb") },
		func(error) bool { return false }))
	assert.NoError(t, b.DoWithFallbackAcceptableCtx(ctx, req,
		func(error) error { return errors.New("fb") }, func(error) bool { return false }))
	assert.Equal(t, 7, called, "every variant should run req once")

	// nop does not degrade: a failing req is returned as is and fallback has no effect
	boom := errors.New("boom")
	err = b.DoWithFallback(func() error { return boom }, func(error) error { return nil })
	assert.ErrorIs(t, err, boom)
}

// TestGlobalBreakerHelpers covers the package-level convenience functions in
// breakers.go (Do is already covered by existing tests; the remaining 7 variants are
// added here). NoBreakerFor registers a nop, so the behaviour is deterministic and
// does not depend on the time window.
func TestGlobalBreakerHelpers(t *testing.T) {
	name := fmt.Sprintf("global-helpers-%d", time.Now().UnixNano())
	NoBreakerFor(name)
	ctx := context.Background()
	boom := errors.New("boom")

	// DoCtx
	var called int
	assert.NoError(t, DoCtx(ctx, name, func() error { called++; return nil }))
	assert.Equal(t, 1, called)

	// DoWithAcceptable / DoWithAcceptableCtx: return the actual error from req (accepted
	// errors are still returned)
	for _, fn := range []func() error{
		func() error {
			return DoWithAcceptable(name, func() error { return boom }, func(error) bool { return true })
		},
		func() error {
			return DoWithAcceptableCtx(ctx, name, func() error { return boom }, func(error) bool { return true })
		},
	} {
		assert.ErrorIs(t, fn(), boom)
	}

	// DoWithFallback family: nop ignores fallback and the error is returned as is
	fbRun := false
	assert.ErrorIs(t, DoWithFallback(name, func() error { return boom },
		func(error) error { fbRun = true; return nil }), boom)
	assert.ErrorIs(t, DoWithFallbackCtx(ctx, name, func() error { return boom },
		func(error) error { fbRun = true; return nil }), boom)
	assert.ErrorIs(t, DoWithFallbackAcceptable(name, func() error { return boom },
		func(error) error { fbRun = true; return nil }, func(error) bool { return false }), boom)
	assert.ErrorIs(t, DoWithFallbackAcceptableCtx(ctx, name, func() error { return boom },
		func(error) error { fbRun = true; return nil }, func(error) bool { return false }), boom)
	assert.False(t, fbRun, "nop must not run fallback")
}
