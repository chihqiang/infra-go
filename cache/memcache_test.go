package cache

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

var bgCtx = context.Background()

// newTestCache creates an in-memory cache and Closes it automatically after the test.
func newTestCache(t *testing.T, expire time.Duration, opts ...MemCacheOption) *MemCache {
	t.Helper()
	c := NewMemCache(bgCtx, expire, opts...)
	t.Cleanup(c.Close)
	return c
}

// TestCacheInterface is a compile-time assertion: MemCache implements the Cache
// interface.
func TestCacheInterface(t *testing.T) {
	var _ Cache = NewMemCache(bgCtx, time.Minute)
}

func TestSetGet(t *testing.T) {
	c := newTestCache(t, time.Minute)

	assert.NoError(t, c.Set(bgCtx, "name", "chihqiang"))
	v, err := c.Get(bgCtx, "name")
	assert.NoError(t, err)
	assert.Equal(t, "chihqiang", v)

	// Not generic: one instance can hold values of several types
	assert.NoError(t, c.Set(bgCtx, "count", 42))
	iv, err := c.Get(bgCtx, "count")
	assert.NoError(t, err)
	assert.Equal(t, 42, iv)
}

func TestGetMiss(t *testing.T) {
	c := newTestCache(t, time.Minute)

	_, err := c.Get(bgCtx, "missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDel(t *testing.T) {
	c := newTestCache(t, time.Minute)

	assert.NoError(t, c.Set(bgCtx, "k", "v"))
	assert.NoError(t, c.Delete(bgCtx, "k"))
	_, err := c.Get(bgCtx, "k")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDeleteMultipleKeys(t *testing.T) {
	c := newTestCache(t, time.Minute)

	assert.NoError(t, c.Set(bgCtx, "a", "1"))
	assert.NoError(t, c.Set(bgCtx, "b", "2"))
	assert.NoError(t, c.Delete(bgCtx, "a", "b"))
	assert.Equal(t, 0, c.Size())
}

func TestExpire(t *testing.T) {
	c := newTestCache(t, 50*time.Millisecond)

	assert.NoError(t, c.Set(bgCtx, "k", "v"))
	_, err := c.Get(bgCtx, "k")
	assert.NoError(t, err)

	// Wait for the expiry (allowing for the 5% jitter, wait a bit longer)
	time.Sleep(120 * time.Millisecond)
	_, err = c.Get(bgCtx, "k")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSetExOverride(t *testing.T) {
	c := newTestCache(t, time.Minute)

	// Write with a short expiry, then rewrite with the default expiry; the old timer
	// must not delete the new value.
	assert.NoError(t, c.SetEx(bgCtx, "k", "old", 30*time.Millisecond))
	time.Sleep(5 * time.Millisecond)
	assert.NoError(t, c.Set(bgCtx, "k", "new"))

	time.Sleep(50 * time.Millisecond)
	v, err := c.Get(bgCtx, "k")
	assert.NoError(t, err, "the new value must not be deleted by the old timer")
	assert.Equal(t, "new", v)
}

func TestLruEvict(t *testing.T) {
	c := newTestCache(t, time.Minute, WithLimit(2))

	assert.NoError(t, c.Set(bgCtx, "a", "1"))
	assert.NoError(t, c.Set(bgCtx, "b", "2"))
	assert.Equal(t, 2, c.Size())

	// Access a so that a becomes most recently used and b least recently used
	_, _ = c.Get(bgCtx, "a")
	assert.NoError(t, c.Set(bgCtx, "c", "3")) // triggers eviction; b should be evicted

	_, err := c.Get(bgCtx, "b")
	assert.ErrorIs(t, err, ErrNotFound, "b should have been evicted by LRU")
	_, err = c.Get(bgCtx, "a")
	assert.NoError(t, err)
	_, err = c.Get(bgCtx, "c")
	assert.NoError(t, err)
}

func TestTake(t *testing.T) {
	c := newTestCache(t, time.Minute)

	var calls int32
	fetch := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		return "db-value", nil
	}

	v, err := c.Take(bgCtx, "k", fetch)
	require.NoError(t, err)
	assert.Equal(t, "db-value", v)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))

	// The second call hits the cache and no longer calls fetch
	v, err = c.Take(bgCtx, "k", fetch)
	require.NoError(t, err)
	assert.Equal(t, "db-value", v)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestTakeConcurrent(t *testing.T) {
	c := newTestCache(t, time.Minute)

	var calls int32
	fetch := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(20 * time.Millisecond) // simulate a slow query
		return "db-value", nil
	}

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			v, err := c.Take(bgCtx, "k", fetch)
			// Note: inside a child goroutine only assert can be used, not require
			// (FailNow calls Goexit in the child goroutine, skipping wg.Done() and
			// hanging the test).
			assert.NoError(t, err)
			assert.Equal(t, "db-value", v)
		}()
	}
	wg.Wait()

	// Concurrent Take calls run fetch only once (stampede protection)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
	assert.Equal(t, 1, c.Size())
}

func TestTakeFetchError(t *testing.T) {
	c := newTestCache(t, time.Minute)

	_, err := c.Take(bgCtx, "k", func() (any, error) {
		return nil, assert.AnError
	})
	assert.Error(t, err)
	assert.Equal(t, 0, c.Size(), "a failed fetch must not write to the cache")
}

func TestNoExpire(t *testing.T) {
	c := newTestCache(t, 0) // no expiry by default

	assert.NoError(t, c.Set(bgCtx, "k", "v"))
	time.Sleep(20 * time.Millisecond)
	_, err := c.Get(bgCtx, "k")
	assert.NoError(t, err, "expire=0 never expires")
}

func TestClose(t *testing.T) {
	c := NewMemCache(bgCtx, time.Minute)
	assert.NoError(t, c.Set(bgCtx, "k", "v"))
	c.Close()
	c.Close() // idempotent, does not panic
}

// --- Additional cases: options, edge behaviour, coverage gaps, bug regressions ---

func TestWithName(t *testing.T) {
	c := NewMemCache(bgCtx, time.Minute, WithName("users"))
	assert.Equal(t, "users", c.name)
	t.Cleanup(c.Close)

	d := NewMemCache(bgCtx, time.Minute)
	assert.Equal(t, defaultCacheName, d.name)
	t.Cleanup(d.Close)
}

func TestNoLimitKeepsAll(t *testing.T) {
	c := newTestCache(t, time.Minute)
	for i := 0; i < 1000; i++ {
		assert.NoError(t, c.Set(bgCtx, fmt.Sprintf("k%d", i), i))
	}
	assert.Equal(t, 1000, c.Size(), "no key should be evicted when capacity is unlimited")
}

func TestOverwrite(t *testing.T) {
	c := newTestCache(t, time.Minute)

	assert.NoError(t, c.Set(bgCtx, "k", "old"))
	assert.NoError(t, c.Set(bgCtx, "k", "new"))
	v, err := c.Get(bgCtx, "k")
	assert.NoError(t, err)
	assert.Equal(t, "new", v)
	assert.Equal(t, 1, c.Size(), "an overwrite must not increase the element count")
}

func TestDeleteMissingKey(t *testing.T) {
	c := newTestCache(t, time.Minute)
	assert.NoError(t, c.Delete(bgCtx, "nope"))
}

func TestDeleteOnLimitedCache(t *testing.T) {
	c := newTestCache(t, time.Minute, WithLimit(2))

	assert.NoError(t, c.Set(bgCtx, "a", "1"))
	assert.NoError(t, c.Set(bgCtx, "b", "2"))
	assert.NoError(t, c.Delete(bgCtx, "a")) // triggers keyLru.remove

	_, err := c.Get(bgCtx, "a")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = c.Get(bgCtx, "b")
	assert.NoError(t, err)
	assert.Equal(t, 1, c.Size())

	// After the deletion the LRU record of a is gone, so setting it again works
	assert.NoError(t, c.Set(bgCtx, "a", "new"))
	assert.Equal(t, 2, c.Size())
}

func TestExpireOnLimitedCache(t *testing.T) {
	c := newTestCache(t, 30*time.Millisecond, WithLimit(2))

	assert.NoError(t, c.Set(bgCtx, "a", "1"))
	assert.NoError(t, c.Set(bgCtx, "b", "2"))
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, 0, c.Size(),
		"a capacity-limited cache must be empty after expiry (keyLru.remove path)")
}

// TestLruEvictThenReSet is a regression test: the old timer is still pending after an
// LRU eviction, and when the same key is written again the old timer must not delete
// the new value by mistake (the version number must increase monotonically).
func TestLruEvictThenReSet(t *testing.T) {
	c := newTestCache(t, time.Minute, WithLimit(2))

	// a is tracked by a short-expiry timer
	assert.NoError(t, c.SetEx(bgCtx, "a", "old", 100*time.Millisecond))
	assert.NoError(t, c.Set(bgCtx, "b", "x"))
	assert.NoError(t, c.Set(bgCtx, "c", "y")) // LRU evicts a (a is least recently used)

	_, err := c.Get(bgCtx, "a")
	assert.ErrorIs(t, err, ErrNotFound, "a should already have been evicted by LRU")

	// Write a again before the old timer fires
	assert.NoError(t, c.SetEx(bgCtx, "a", "new", time.Minute))

	time.Sleep(150 * time.Millisecond) // past the old timer's firing point
	v, err := c.Get(bgCtx, "a")
	assert.NoError(t, err, "the old timer must not delete the rewritten value")
	assert.Equal(t, "new", v)
}

func TestSetExFallback(t *testing.T) {
	// Default is 1 minute; passing 0 or a negative value falls back to the default
	// instead of expiring immediately
	c := newTestCache(t, time.Minute)
	assert.NoError(t, c.SetEx(bgCtx, "a", "v1", 0))
	assert.NoError(t, c.SetEx(bgCtx, "b", "v2", -time.Second))

	time.Sleep(20 * time.Millisecond)
	_, err := c.Get(bgCtx, "a")
	assert.NoError(t, err)
	_, err = c.Get(bgCtx, "b")
	assert.NoError(t, err)
}

func TestTakeAfterExpire(t *testing.T) {
	c := newTestCache(t, 30*time.Millisecond)

	var calls int32
	fetch := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		return "v", nil
	}

	v, err := c.Take(bgCtx, "k", fetch)
	require.NoError(t, err)
	assert.Equal(t, "v", v)

	time.Sleep(80 * time.Millisecond) // expired
	v, err = c.Take(bgCtx, "k", fetch)
	require.NoError(t, err)
	assert.Equal(t, "v", v)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls), "must fetch again after expiry")
}

func TestTakeDistinctKeys(t *testing.T) {
	c := newTestCache(t, time.Minute)

	var callsA, callsB int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = c.Take(bgCtx, "a", func() (any, error) { atomic.AddInt32(&callsA, 1); return "a", nil })
		}()
		go func() {
			defer wg.Done()
			_, _ = c.Take(bgCtx, "b", func() (any, error) { atomic.AddInt32(&callsB, 1); return "b", nil })
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), atomic.LoadInt32(&callsA), "SingleFlight must be isolated per key")
	assert.Equal(t, int32(1), atomic.LoadInt32(&callsB))
}

func TestConcurrentMixedOps(t *testing.T) {
	c := newTestCache(t, time.Minute)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("k%d", (seed+i)%20)
				switch (seed + i) % 3 {
				case 0:
					_ = c.Set(bgCtx, key, i)
				case 1:
					_, _ = c.Get(bgCtx, key)
				default:
					_ = c.Delete(bgCtx, key)
				}
			}
		}(g)
	}
	wg.Wait()

	// The main purpose is to exercise -race for data races; not panicking is enough
	assert.True(t, c.Size() >= 0)
}

func TestNewUnstableClamp(t *testing.T) {
	u := newUnstable(-0.5)
	assert.Equal(t, 0.0, u.deviation, "a negative deviation must be clamped to 0")

	u = newUnstable(2.0)
	assert.Equal(t, 1.0, u.deviation, "a deviation above the upper bound must be clamped to 1")
}

func TestCacheStatLoop(t *testing.T) {
	stop := make(chan struct{})
	st := &cacheStat{
		name:         "test",
		sizeCallback: func() int { return 3 },
		interval:     10 * time.Millisecond,
		stop:         stop,
		ctx:          bgCtx,
	}
	go st.statLoop()
	defer close(stop)

	// First period has no hits: the total == 0 continue branch is taken
	time.Sleep(25 * time.Millisecond)

	// Produce hits and misses: the statistics output branch is taken
	st.IncrementHit()
	st.IncrementMiss()
	st.IncrementHit()
	time.Sleep(25 * time.Millisecond)

	// After the counters were swapped to zero and no new counts arrive, they end at zero
	time.Sleep(25 * time.Millisecond)
	assert.Zero(t, atomic.LoadUint64(&st.hit))
	assert.Zero(t, atomic.LoadUint64(&st.miss))
}

func TestIncrement(t *testing.T) {
	c := newTestCache(t, time.Minute)

	// key does not exist: initialized to delta
	assert.NoError(t, c.Increment(bgCtx, "count", 5))
	v, err := c.Get(bgCtx, "count")
	assert.NoError(t, err)
	assert.Equal(t, int64(5), v)

	// already exists: accumulated
	assert.NoError(t, c.Increment(bgCtx, "count", 3))
	v, err = c.Get(bgCtx, "count")
	assert.NoError(t, err)
	assert.Equal(t, int64(8), v)
}

func TestDecrement(t *testing.T) {
	c := newTestCache(t, time.Minute)

	// key does not exist: initialized to -delta
	assert.NoError(t, c.Decrement(bgCtx, "count", 2))
	v, err := c.Get(bgCtx, "count")
	assert.NoError(t, err)
	assert.Equal(t, int64(-2), v)

	// already exists: subtracted
	assert.NoError(t, c.Decrement(bgCtx, "count", 3))
	v, err = c.Get(bgCtx, "count")
	assert.NoError(t, err)
	assert.Equal(t, int64(-5), v)
}

func TestIncrementKeepsType(t *testing.T) {
	c := newTestCache(t, time.Minute)

	// The value stored by Set is an int and stays an int after Increment
	assert.NoError(t, c.Set(bgCtx, "n", 10))
	assert.NoError(t, c.Increment(bgCtx, "n", 1))
	v, err := c.Get(bgCtx, "n")
	assert.NoError(t, err)
	assert.Equal(t, 11, v)

	// Not a numeric type: an error is returned and the original value is unchanged
	assert.NoError(t, c.Set(bgCtx, "s", "hello"))
	assert.Error(t, c.Increment(bgCtx, "s", 1))
	s, err := c.Get(bgCtx, "s")
	assert.NoError(t, err)
	assert.Equal(t, "hello", s)
}

func TestExpireSetTTL(t *testing.T) {
	c := newTestCache(t, time.Minute)
	assert.NoError(t, c.Set(bgCtx, "k", "v"))

	// Set a short expiry; the key is still readable before it elapses
	assert.NoError(t, c.Expire(bgCtx, "k", 30*time.Millisecond))
	_, err := c.Get(bgCtx, "k")
	assert.NoError(t, err)

	// Wait for the expiry (allowing for the 5% jitter, wait a bit longer)
	time.Sleep(80 * time.Millisecond)
	_, err = c.Get(bgCtx, "k")
	assert.ErrorIs(t, err, ErrNotFound)

	// a missing key returns ErrNotFound
	err = c.Expire(bgCtx, "missing", time.Minute)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestExpireImmediate(t *testing.T) {
	c := newTestCache(t, time.Minute)
	assert.NoError(t, c.Set(bgCtx, "k", "v"))

	// ttl <= 0 expires immediately
	assert.NoError(t, c.Expire(bgCtx, "k", 0))
	_, err := c.Get(bgCtx, "k")
	assert.ErrorIs(t, err, ErrNotFound)
}
