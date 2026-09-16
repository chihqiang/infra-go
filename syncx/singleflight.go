package syncx

import (
	"context"
	"fmt"
	"sync"
)

// --- SingleFlight ---

// singleFlightCall represents one in-flight call.
// done is a lazily created completion channel shared by every waiter, so each
// waiter does not have to start a goroutine of its own.
type singleFlightCall[T any] struct {
	wg  sync.WaitGroup
	val T
	err error
	// panicVal records the panic value thrown by fn. It is written before
	// wg.Done() / close(done), so waiters can read it safely. When it is non-nil
	// the waiters re-panic, guaranteeing that every caller observes the same
	// outcome as the leader (otherwise a waiter would get the zero value plus a
	// nil error and mistake the call for a success).
	panicVal any
	done     chan struct{}
}

// SingleFlight guards against cache stampedes: concurrent calls for the same key
// run only once and the result is shared with every caller.
//
// Use cases:
//   - Cache stampede protection: a burst of concurrent requests for one key
//     passes through to the underlying data source only once.
//   - Duplicate request coalescing: several goroutines asking for the same
//     resource trigger one actual fetch.
//
// Usage:
//
//	sf := syncx.NewSingleFlight[string]()
//	val, err := sf.Do("user:123", func() (string, error) {
//	    return fetchUserFromDB(123)
//	})
type SingleFlight[T any] struct {
	mu    sync.Mutex
	calls map[string]*singleFlightCall[T]
}

// NewSingleFlight creates a new SingleFlight instance.
func NewSingleFlight[T any]() *SingleFlight[T] {
	return &SingleFlight[T]{
		calls: make(map[string]*singleFlightCall[T]),
	}
}

// Do runs fn, collapsing concurrent calls for the same key into a single
// execution. If a call with the same key is already running, the current call
// waits for its result.
//
// If the leader's fn panics, every waiter receives the same panic value and
// panics as well, so no caller can mistake a "zero value + nil error" for a
// success.
func (sf *SingleFlight[T]) Do(key string, fn func() (T, error)) (T, error) {
	sf.mu.Lock()
	if call, ok := sf.calls[key]; ok {
		sf.mu.Unlock()
		call.wg.Wait()
		call.repanic()
		return call.val, call.err
	}

	call := &singleFlightCall[T]{}
	call.wg.Add(1)
	sf.calls[key] = call
	sf.mu.Unlock()

	sf.run(call, fn)

	sf.mu.Lock()
	delete(sf.calls, key)
	sf.mu.Unlock()

	call.repanic()
	return call.val, call.err
}

// DoCtx runs fn with context cancellation support.
// If the context is cancelled, waiting calls return a context error, but the
// call that is already executing is not interrupted.
// Note: after cancellation the internal goroutine that waits for the result
// lives until fn finishes; it exits on its own once fn returns, so it does not
// leak permanently.
//
// As with Do, when the leader's fn panics every waiter receives the same panic
// value.
func (sf *SingleFlight[T]) DoCtx(ctx context.Context, key string, fn func(context.Context) (T, error)) (T, error) {
	// Fast path: check the context first
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}

	sf.mu.Lock()
	if call, ok := sf.calls[key]; ok {
		// Lazily create the shared done channel so that all waiters reuse a single
		// goroutine. It is created while the lock is held, and later waiters simply
		// reuse it instead of starting another goroutine.
		if call.done == nil {
			call.done = make(chan struct{})
			go func() {
				call.wg.Wait()
				close(call.done)
			}()
		}
		sf.mu.Unlock()

		// Wait for the result or for the context to be cancelled
		select {
		case <-call.done:
			call.repanic()
			return call.val, call.err
		case <-ctx.Done():
			var zero T
			return zero, fmt.Errorf("singleflight: context cancelled: %w", ctx.Err())
		}
	}

	call := &singleFlightCall[T]{}
	call.wg.Add(1)
	sf.calls[key] = call
	sf.mu.Unlock()

	sf.run(call, func() (T, error) { return fn(ctx) })

	sf.mu.Lock()
	delete(sf.calls, key)
	sf.mu.Unlock()

	call.repanic()
	return call.val, call.err
}

// run executes fn, records the result (including any panic value) on call, and
// finally wakes the waiters.
//
// The panic value is written to call.panicVal before wg.Done(): waiters
// establish a happens-before edge through wg.Wait() / <-done, so reading it is
// safe.
func (sf *SingleFlight[T]) run(call *singleFlightCall[T], fn func() (T, error)) {
	defer func() {
		if r := recover(); r != nil {
			call.panicVal = r
		}
		call.wg.Done()
	}()
	call.val, call.err = fn()
}

// repanic rethrows the leader's panic value when its fn panicked.
// The goroutine reading call.panicVal must already be synchronised through
// wg.Wait() / <-done.
func (c *singleFlightCall[T]) repanic() {
	if c.panicVal != nil {
		panic(c.panicVal)
	}
}

// Forget drops the call record for key, so the next Do call runs fn again.
// Calls that are already in flight are unaffected.
func (sf *SingleFlight[T]) Forget(key string) {
	sf.mu.Lock()
	delete(sf.calls, key)
	sf.mu.Unlock()
}
