package syncx

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- SingleFlight tests ---

func TestSingleFlight_Do(t *testing.T) {
	sf := NewSingleFlight[string]()

	var count int32
	val, err := sf.Do("key", func() (string, error) {
		atomic.AddInt32(&count, 1)
		time.Sleep(50 * time.Millisecond)
		return "result", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "result", val)
	assert.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func TestSingleFlight_Concurrent(t *testing.T) {
	sf := NewSingleFlight[int]()

	var callCount int32
	var wg sync.WaitGroup

	// 100 goroutines call the same key at the same time
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			val, err := sf.Do("same-key", func() (int, error) {
				atomic.AddInt32(&callCount, 1)
				time.Sleep(50 * time.Millisecond)
				return 42, nil
			})
			assert.NoError(t, err)
			assert.Equal(t, 42, val)
		}()
	}
	wg.Wait()

	// fn must have been called exactly once
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))
}

func TestSingleFlight_DifferentKeys(t *testing.T) {
	sf := NewSingleFlight[string]()

	var count int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", n)
			_, err := sf.Do(key, func() (string, error) {
				atomic.AddInt32(&count, 1)
				return key, nil
			})
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// Each distinct key triggers one call
	assert.Equal(t, int32(10), atomic.LoadInt32(&count))
}

func TestSingleFlight_Error(t *testing.T) {
	sf := NewSingleFlight[string]()

	val, err := sf.Do("key", func() (string, error) {
		return "", fmt.Errorf("custom error")
	})

	assert.Error(t, err)
	assert.Equal(t, "", val)
	assert.Contains(t, err.Error(), "custom error")
}

func TestSingleFlight_Forget(t *testing.T) {
	sf := NewSingleFlight[string]()

	var count int32
	sf.Do("key", func() (string, error) {
		atomic.AddInt32(&count, 1)
		return "first", nil
	})

	sf.Forget("key")

	sf.Do("key", func() (string, error) {
		atomic.AddInt32(&count, 1)
		return "second", nil
	})

	assert.Equal(t, int32(2), atomic.LoadInt32(&count))
}

func TestSingleFlight_Panic(t *testing.T) {
	sf := NewSingleFlight[int]()

	// The panic must propagate to the caller
	func() {
		defer func() {
			r := recover()
			assert.Equal(t, "boom", r, "expected panic to propagate to caller")
		}()
		sf.Do("key", func() (int, error) {
			panic("boom")
		})
	}()

	// After the panic the key has been cleaned up, so it can be used again
	val, err := sf.Do("key", func() (int, error) { return 42, nil })
	assert.NoError(t, err)
	assert.Equal(t, 42, val)
}

func TestSingleFlight_PanicDoesNotBlockWaiters(t *testing.T) {
	sf := NewSingleFlight[int]()

	var wg sync.WaitGroup
	start := make(chan struct{})

	// The first caller panics
	var leaderPanic any
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { leaderPanic = recover() }()
		sf.Do("key", func() (int, error) {
			close(start)
			time.Sleep(50 * time.Millisecond)
			panic("boom")
		})
	}()

	<-start

	// Waiters must return instead of blocking forever, and see the same panic as
	// the leader
	done := make(chan struct{})
	var waiterPanic any
	go func() {
		defer close(done)
		defer func() { waiterPanic = recover() }()
		sf.Do("key", func() (int, error) { return 7, nil })
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter blocked forever after fn panicked")
	}
	wg.Wait()

	assert.Equal(t, "boom", leaderPanic)
	assert.Equal(t, "boom", waiterPanic,
		"waiter must see the same panic as the leader instead of a zero-value success")
}

// TestSingleFlight_PanicNotReportedAsZeroSuccess is a regression test: when the
// leader panics, a waiter must not report the failure as a success. With the old
// implementation the waiter's sf.Do returned (0, nil) normally.
func TestSingleFlight_PanicNotReportedAsZeroSuccess(t *testing.T) {
	sf := NewSingleFlight[int]()

	var wg sync.WaitGroup
	start := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { _ = recover() }()
		sf.Do("key", func() (int, error) {
			close(start)
			time.Sleep(30 * time.Millisecond)
			panic("fetch failed")
		})
	}()
	<-start

	type outcome struct {
		err       error
		panicked  bool
		returned  bool
		panickedV any
	}
	got := make(chan outcome, 1)
	go func() {
		var o outcome
		defer func() {
			if r := recover(); r != nil {
				o.panicked = true
				o.panickedV = r
			}
			got <- o
		}()
		_, o.err = sf.Do("key", func() (int, error) { return 0, nil })
		o.returned = true
	}()
	wg.Wait()

	select {
	case o := <-got:
		require.True(t, o.panicked, "waiter must panic instead of returning a result")
		assert.False(t, o.returned, "sf.Do must not return normally for the waiter")
		assert.Equal(t, "fetch failed", o.panickedV)
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not return")
	}
}

// TestSingleFlight_PanicNilValueIsNotSwallowed verifies that when fn calls
// panic(nil), waiters still observe the failure (Go 1.21+ surfaces it as
// *runtime.PanicNilError).
func TestSingleFlight_PanicNilValueIsNotSwallowed(t *testing.T) {
	sf := NewSingleFlight[int]()

	var wg sync.WaitGroup
	start := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { _ = recover() }()
		sf.Do("key", func() (int, error) {
			close(start)
			time.Sleep(30 * time.Millisecond)
			panic(nil) //nolint:staticcheck // verifies the runtime-converted panic is still propagated
		})
	}()
	<-start

	panicked := make(chan bool, 1)
	go func() {
		recovered := false
		defer func() {
			if recover() != nil {
				recovered = true
			}
			panicked <- recovered
		}()
		_, _ = sf.Do("key", func() (int, error) { return 5, nil })
	}()
	wg.Wait()

	select {
	case p := <-panicked:
		assert.True(t, p, "panic(nil) must not be silently converted into a successful zero value")
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not return")
	}
}

// --- DoCtx tests ---

func TestSingleFlight_DoCtx_Success(t *testing.T) {
	sf := NewSingleFlight[string]()
	ctx := context.Background()

	var count int32
	val, err := sf.DoCtx(ctx, "key", func(ctx context.Context) (string, error) {
		atomic.AddInt32(&count, 1)
		return "ok", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", val)
	assert.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func TestSingleFlight_DoCtx_Concurrent(t *testing.T) {
	sf := NewSingleFlight[int]()

	var count int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			val, err := sf.DoCtx(context.Background(), "key", func(ctx context.Context) (int, error) {
				atomic.AddInt32(&count, 1)
				time.Sleep(30 * time.Millisecond)
				return 7, nil
			})
			require.NoError(t, err)
			assert.Equal(t, 7, val)
		}()
	}
	wg.Wait()

	// fn must run exactly once and every waiter shares the result
	assert.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func TestSingleFlight_DoCtx_Cancelled(t *testing.T) {
	sf := NewSingleFlight[string]()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sf.DoCtx(ctx, "key", func(ctx context.Context) (string, error) {
		return "result", nil
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestSingleFlight_DoCtx_WaiterCancelled(t *testing.T) {
	sf := NewSingleFlight[string]()

	release := make(chan struct{})
	started := make(chan struct{})

	// The leader calls and blocks inside fn
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		val, err := sf.DoCtx(context.Background(), "key", func(ctx context.Context) (string, error) {
			close(started)
			<-release
			return "done", nil
		})
		require.NoError(t, err)
		assert.Equal(t, "done", val)
	}()
	<-started

	// The waiter's context is already cancelled: it must return an error instead of
	// blocking forever
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := sf.DoCtx(ctx, "key", func(ctx context.Context) (string, error) {
		return "unused", nil
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")

	// Release the leader and confirm it still completes without getting stuck
	close(release)
	wg.Wait()
}

func TestSingleFlight_DoCtx_Panic(t *testing.T) {
	sf := NewSingleFlight[int]()

	assert.Panics(t, func() {
		sf.DoCtx(context.Background(), "key", func(ctx context.Context) (int, error) {
			panic("boom")
		})
	})

	// After the panic the key has been cleaned up, so it can run again
	val, err := sf.DoCtx(context.Background(), "key", func(ctx context.Context) (int, error) {
		return 1, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, val)
}

func TestSingleFlight_DoCtx_ErrorShared(t *testing.T) {
	sf := NewSingleFlight[string]()

	started := make(chan struct{})
	release := make(chan struct{})
	// The buffer capacity must be >= the number of senders (2), otherwise a late
	// sender would block forever before the main goroutine Waits.
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	// Leader: block inside fn so that any later caller is guaranteed to act as a
	// "waiter" instead of running fn itself.
	go func() {
		defer wg.Done()
		_, err := sf.DoCtx(context.Background(), "key", func(ctx context.Context) (string, error) {
			close(started)
			<-release
			return "", fmt.Errorf("db down")
		})
		errCh <- err
	}()

	<-started // the leader holds the lock and has entered fn

	// Waiter: entering now it must be a waiter and share the leader's error.
	// Its fallback fn returns the same error, avoiding flakiness under extreme
	// scheduling (where the waiter could unexpectedly become the leader).
	go func() {
		defer wg.Done()
		_, err := sf.DoCtx(context.Background(), "key", func(ctx context.Context) (string, error) {
			return "", fmt.Errorf("db down")
		})
		errCh <- err
	}()

	// Give the waiter time to enter the waiting state, then release the leader
	time.Sleep(20 * time.Millisecond)
	close(release)

	wg.Wait()
	close(errCh)

	for err := range errCh {
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db down")
	}
}
