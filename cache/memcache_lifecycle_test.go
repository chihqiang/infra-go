package cache

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers the reclamation of MemCache background goroutines (regression).
// Historical defect: scanLoop/statLoop only watched the stop channel closed by Close
// and ignored the ctx passed at construction time entirely; when the caller forgot to
// call Close the goroutines leaked forever (and kept accumulating in setups that
// create a cache instance per request/tenant).

// waitGoroutinesSettle waits for the goroutine count to fall back to the threshold.
// Background goroutines take some scheduling delay to exit, so this polls instead of
// asserting immediately.
func waitGoroutinesSettle(t *testing.T, max int, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		runtime.GC()
		n := runtime.NumGoroutine()
		if n <= max {
			return n
		}
		if time.Now().After(deadline) {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestMemCache_CtxCancelStopsBackgroundGoroutines is a regression test:
// cancelling the ctx passed at construction time must reclaim the background
// goroutines without calling Close.
func TestMemCache_CtxCancelStopsBackgroundGoroutines(t *testing.T) {
	// Establish a baseline first (the steady-state count including the statistics
	// goroutine).
	baseline := runtime.NumGoroutine()

	const instances = 10
	ctx, cancel := context.WithCancel(context.Background())

	caches := make([]*MemCache, 0, instances)
	for i := 0; i < instances; i++ {
		// scanLoop only starts when expire > 0; statLoop always starts
		caches = append(caches, NewMemCache(ctx, time.Minute))
	}

	// There should now be about 2*instances background goroutines
	running := runtime.NumGoroutine()
	require.Greater(t, running, baseline,
		"background goroutines should be running after creation")

	// Cancel only the ctx, without calling Close
	cancel()

	// Every background goroutine should exit
	got := waitGoroutinesSettle(t, baseline+2, 3*time.Second)
	t.Logf("goroutines: baseline=%d running=%d after cancel=%d (instances=%d)",
		baseline, running, got, instances)

	assert.LessOrEqual(t, got, baseline+2,
		"cancelling ctx must stop scanLoop/statLoop; leaked goroutines indicate a leak")

	// Cleanup (Close is idempotent; overlapping with the ctx cancellation must not
	// panic).
	for _, c := range caches {
		c.Close()
	}
}

// TestMemCache_CloseStillWorks verifies that the original Close semantics are intact.
func TestMemCache_CloseStillWorks(t *testing.T) {
	baseline := runtime.NumGoroutine()

	const instances = 10
	ctx := context.Background() // not cancelled

	caches := make([]*MemCache, 0, instances)
	for i := 0; i < instances; i++ {
		caches = append(caches, NewMemCache(ctx, time.Minute))
	}

	for _, c := range caches {
		c.Close()
	}

	got := waitGoroutinesSettle(t, baseline+2, 3*time.Second)
	assert.LessOrEqual(t, got, baseline+2, "Close must stop the background goroutines")
}

// TestMemCache_CloseIsIdempotent verifies that Close can be called repeatedly.
func TestMemCache_CloseIsIdempotent(t *testing.T) {
	c := NewMemCache(context.Background(), time.Minute)
	require.NotPanics(t, func() {
		c.Close()
		c.Close()
		c.Close()
	})
}

// TestMemCache_NilCtxDoesNotPanic verifies that a nil ctx still works (ctx is not
// watched).
func TestMemCache_NilCtxDoesNotPanic(t *testing.T) {
	var c *MemCache
	require.NotPanics(t, func() {
		//nolint:staticcheck // explicitly verifying tolerance of a nil ctx
		c = NewMemCache(nil, time.Minute)
	})
	defer c.Close()

	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", "v"))
	v, err := c.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, "v", v)
}

// TestMemCache_NoExpireStartsNoScanLoop verifies that no scan goroutine is started
// when no expiry is configured.
func TestMemCache_NoExpireStartsNoScanLoop(t *testing.T) {
	c := NewMemCache(context.Background(), 0) // expire <= 0: no scanLoop
	defer c.Close()

	assert.Equal(t, time.Duration(0), c.scanInterval, "no expiry means no scan interval")
}
