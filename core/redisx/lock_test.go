package redisx

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMiniRedisClient creates a client backed by miniredis.
func newMiniRedisClient(t *testing.T) (*Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client, err := New(Config{Addr: mr.Addr()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client, mr
}

func TestGenerateToken(t *testing.T) {
	token1, err := generateToken()
	require.NoError(t, err)
	assert.Len(t, token1, 32) // 16 bytes -> 32 hex chars

	token2, err := generateToken()
	require.NoError(t, err)
	assert.NotEqual(t, token1, token2)
}

func TestIsLockNotAcquired(t *testing.T) {
	assert.True(t, IsLockNotAcquired(ErrLockNotAcquired))
	assert.False(t, IsLockNotAcquired(nil))
	assert.False(t, IsLockNotAcquired(ErrLockOwnershipMismatch))
}

func TestLocker(t *testing.T) {
	c, _ := newMiniRedisClient(t)
	la := c.Locker("test-lock", 10*time.Second)
	assert.NotNil(t, la)
}

// --- TryLock ---

func TestTryLock_Success(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniRedisClient(t)

	lock, err := c.Locker("lock", 10*time.Second).TryLock(ctx)
	require.NoError(t, err)
	assert.NotNil(t, lock)

	// The lock key should exist in Redis
	val, err := mr.Get("lock")
	require.NoError(t, err)
	assert.NotEmpty(t, val)

	// The key is deleted after the release
	require.NoError(t, lock.Unlock(ctx))
	assert.False(t, mr.Exists("lock"))
}

func TestTryLock_Conflict(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	la := c.Locker("lock", 10*time.Second)
	lock1, err := la.TryLock(ctx)
	require.NoError(t, err)
	defer lock1.Unlock(ctx)

	// A second acquisition of the same lock must fail
	_, err = la.TryLock(ctx)
	assert.ErrorIs(t, err, ErrLockNotAcquired)
	assert.True(t, IsLockNotAcquired(err))
}

func TestTryLock_ContextCancelled(t *testing.T) {
	c, _ := newMiniRedisClient(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Locker("lock", time.Second).TryLock(ctx)
	assert.Error(t, err)
}

// --- Lock (blocking) ---

func TestLock_BlockUntilReleased(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	la := c.Locker("lock", 10*time.Second)
	lock1, err := la.TryLock(ctx)
	require.NoError(t, err)

	// Concurrent blocking acquisition
	var (
		got    *Lock
		gotErr error
		wg     sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		got, gotErr = la.Lock(ctx, 10*time.Millisecond)
	}()

	// Make sure the blocking acquisition has started trying
	time.Sleep(50 * time.Millisecond)

	// Release the first lock
	require.NoError(t, lock1.Unlock(ctx))

	// Wait for the goroutine to get the lock
	wg.Wait()
	require.NoError(t, gotErr)
	require.NotNil(t, got)
	defer got.Unlock(ctx)
}

func TestLock_ContextCancelledWhileWaiting(t *testing.T) {
	c, _ := newMiniRedisClient(t)

	la := c.Locker("lock", 10*time.Second)
	lock1, err := la.TryLock(context.Background())
	require.NoError(t, err)
	defer lock1.Unlock(context.Background())

	// Cancelled ctx
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = la.Lock(ctx, 5*time.Millisecond)
	assert.Error(t, err)
}

// --- Unlock ---

func TestUnlock_OwnershipMismatch(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniRedisClient(t)

	lock, err := c.Locker("lock", 10*time.Second).TryLock(ctx)
	require.NoError(t, err)

	// A third party deletes the lock key directly, so the token no longer matches
	mr.Del("lock")

	err = lock.Unlock(ctx)
	assert.ErrorIs(t, err, ErrLockOwnershipMismatch)
}

func TestUnlock_Twice(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	lock, err := c.Locker("lock", 10*time.Second).TryLock(ctx)
	require.NoError(t, err)

	require.NoError(t, lock.Unlock(ctx))
	// Second Unlock: the key is gone, so the token does not match
	assert.ErrorIs(t, lock.Unlock(ctx), ErrLockOwnershipMismatch)
}

// --- SetNXWithLock ---

func TestSetNXWithLock_Success(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	var called int32
	err := c.SetNXWithLock(ctx, "lock", 5*time.Second, func(ctx context.Context) error {
		atomic.AddInt32(&called, 1)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&called))

	// The lock should have been released and can be acquired again
	lock, err := c.Locker("lock", 5*time.Second).TryLock(ctx)
	require.NoError(t, err)
	lock.Unlock(ctx)
}

func TestSetNXWithLock_NotAcquired(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	// Hold the lock first
	lock, err := c.Locker("lock", 10*time.Second).TryLock(ctx)
	require.NoError(t, err)
	defer lock.Unlock(ctx)

	var called int32
	err = c.SetNXWithLock(ctx, "lock", 5*time.Second, func(ctx context.Context) error {
		atomic.AddInt32(&called, 1)
		return nil
	})
	assert.ErrorIs(t, err, ErrLockNotAcquired)
	assert.Equal(t, int32(0), atomic.LoadInt32(&called))
}

func TestSetNXWithLock_FnError(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	sentinel := errors.New("fn error")
	err := c.SetNXWithLock(ctx, "lock", 5*time.Second, func(ctx context.Context) error {
		return sentinel
	})
	assert.Same(t, sentinel, err)

	// The lock should be released even if fn returned an error
	lock, err := c.Locker("lock", 5*time.Second).TryLock(context.Background())
	require.NoError(t, err)
	lock.Unlock(context.Background())
}

// --- Automatic renewal ---

func TestLock_AutoRenew(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniRedisClient(t)

	// Short TTL with automatic renewal (renew period = ttl/3)
	lock, err := c.Locker("lock", 300*time.Millisecond, WithAutoRenew()).TryLock(ctx)
	require.NoError(t, err)

	// Loop: sleep for real so that renewLoop performs a renewal (resetting the TTL),
	// then FastForward the virtual clock. Without renewal the lock would have expired
	// after more than 300ms of accumulated fast-forwarding; with renewal it stays alive.
	for i := 0; i < 5; i++ {
		time.Sleep(150 * time.Millisecond) // let renewLoop renew at least once
		mr.FastForward(200 * time.Millisecond)
		assert.True(t, c.client.Exists(ctx, "lock").Val() > 0, "lock should be renewed (iter %d)", i)
	}

	// Renewal stops after the release, so the lock should expire
	require.NoError(t, lock.Unlock(ctx))
	mr.FastForward(500 * time.Millisecond)
	assert.False(t, c.client.Exists(ctx, "lock").Val() > 0, "lock should expire after unlock")
}

func TestLock_NoAutoRenew_Expires(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniRedisClient(t)

	// Short TTL, no renewal
	lock, err := c.Locker("lock", 100*time.Millisecond).TryLock(ctx)
	require.NoError(t, err)
	defer lock.Unlock(ctx)

	// FastForward the virtual clock to let the TTL expire
	mr.FastForward(300 * time.Millisecond)
	assert.False(t, c.client.Exists(ctx, "lock").Val() > 0, "lock should expire without renewal")
}

// --- TTL validation (against never-expiring locks / renewal panics / renewal deleting
// the lock) ---

func TestTryLock_InvalidTTLRejected(t *testing.T) {
	ctx := context.Background()
	c, mr := newMiniRedisClient(t)

	cases := []struct {
		name string
		ttl  time.Duration
	}{
		{"zero", 0},
		{"negative", -time.Second},
		{"sub_millisecond", time.Microsecond},
		{"500us", 500 * time.Microsecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Locker("lock", tc.ttl).TryLock(ctx)
			require.ErrorIs(t, err, ErrInvalidLockTTL)

			// An invalid TTL must not write any Redis key (especially a "never expires" lock)
			assert.False(t, c.client.Exists(ctx, "lock").Val() > 0,
				"no lock key should be created for invalid ttl %v", tc.ttl)
		})
	}
	assert.Empty(t, mr.Keys(), "no key should be created for invalid ttls")
}

func TestTryLock_InvalidTTLViaOption(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	// Locker is given a valid TTL, but WithTTL overrides it with an invalid one -> it must
	// be rejected as well
	_, err := c.Locker("lock", time.Second, WithTTL(0)).TryLock(ctx)
	require.ErrorIs(t, err, ErrInvalidLockTTL)

	_, err = c.Locker("lock", time.Second, WithTTL(-time.Millisecond)).TryLock(ctx)
	require.ErrorIs(t, err, ErrInvalidLockTTL)
}

// TestTryLock_InvalidTTLWithAutoRenewDoesNotPanic verifies that an invalid TTL combined
// with automatic renewal no longer triggers the goroutine panic of time.NewTicker(0)
// (the historical defect terminated the process).
func TestTryLock_InvalidTTLWithAutoRenewDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	require.NotPanics(t, func() {
		_, err := c.Locker("lock", 0, WithAutoRenew()).TryLock(ctx)
		require.ErrorIs(t, err, ErrInvalidLockTTL)
	})

	// Leave a time window: if the old implementation really started renewLoop, the panic
	// would happen here
	time.Sleep(50 * time.Millisecond)
}

// TestLock_InvalidTTLPropagatesThroughLock verifies that the blocking Lock fails fast as
// well instead of spinning until the ctx times out.
func TestLock_InvalidTTLPropagatesThroughLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, _ := newMiniRedisClient(t)

	start := time.Now()
	_, err := c.Locker("lock", 0).Lock(ctx, 10*time.Millisecond)
	require.ErrorIs(t, err, ErrInvalidLockTTL)
	assert.Less(t, time.Since(start), time.Second, "should fail fast, not spin until ctx timeout")
}

// TestSetNXWithLock_InvalidTTL verifies that the convenience helper is protected by the
// TTL validation as well.
func TestSetNXWithLock_InvalidTTL(t *testing.T) {
	ctx := context.Background()
	c, _ := newMiniRedisClient(t)

	executed := false
	err := c.SetNXWithLock(ctx, "lock", 0, func(context.Context) error {
		executed = true
		return nil
	})
	require.ErrorIs(t, err, ErrInvalidLockTTL)
	assert.False(t, executed, "critical section must not run when lock ttl is invalid")
}

func TestValidateLockTTL(t *testing.T) {
	assert.NoError(t, validateLockTTL(minLockTTL))
	assert.NoError(t, validateLockTTL(time.Second))

	assert.ErrorIs(t, validateLockTTL(0), ErrInvalidLockTTL)
	assert.ErrorIs(t, validateLockTTL(-time.Second), ErrInvalidLockTTL)
	assert.ErrorIs(t, validateLockTTL(minLockTTL-time.Nanosecond), ErrInvalidLockTTL)
}
