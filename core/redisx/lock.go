package redisx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chihqiang/ten-go/core/logger"
)

// --- Default constants ---

const (
	// defaultRenewTimeout is the Redis call timeout of a renew operation.
	defaultRenewTimeout = 5 * time.Second
	// defaultTokenLen is the number of random bytes in a lock token.
	defaultTokenLen = 16
	// defaultRetryInterval is the default retry interval of a blocking lock acquisition.
	defaultRetryInterval = 50 * time.Millisecond
	// minLockTTL is the lower bound of a lock TTL.
	// The renew script uses l.ttl.Milliseconds(); a TTL below 1ms would be truncated to
	// 0 and PEXPIRE key 0 would delete the lock immediately, leaving the critical
	// section without mutual exclusion.
	minLockTTL = time.Millisecond
)

// Lock is a distributed lock.
// It is built on Redis SET NX EX and supports automatic renewal and safe release.
//
// Renewal is independent of the context used to acquire the lock: cancelling a
// request context after a successful TryLock must not stop renewing a lock that is
// still protecting a critical section. The renewal goroutine runs until Unlock is
// called, so callers must release the lock.
type Lock struct {
	client    *Client
	key       string
	token     string
	ttl       time.Duration
	done      chan struct{}
	doneOnce  sync.Once // ensures the renewal goroutine is stopped only once
	autoRenew bool
	cancel    context.CancelFunc // stops the renewal goroutine
	lost      chan struct{}      // closed when the lock is no longer owned
	lostOnce  sync.Once
	lostFlag  atomic.Bool
}

// LockOption is a lock option.
type LockOption func(*lockConfig)

// lockConfig is the lock configuration.
type lockConfig struct {
	ttl       time.Duration
	autoRenew bool
}

// WithTTL sets the expiry of the lock.
func WithTTL(ttl time.Duration) LockOption {
	return func(c *lockConfig) {
		c.ttl = ttl
	}
}

// WithAutoRenew enables automatic renewal.
// Once enabled the lock is renewed in the background, so business logic that runs
// longer than the lock expiry is covered. Calling Unlock stops the renewal
// automatically.
func WithAutoRenew() LockOption {
	return func(c *lockConfig) {
		c.autoRenew = true
	}
}

// LockAcquirer acquires locks.
type LockAcquirer struct {
	client *Client
	key    string
	ttl    time.Duration
	opts   []LockOption
}

// Locker creates a lock acquirer.
// key is the lock name and ttl is the default expiry of the lock.
func (c *Client) Locker(key string, ttl time.Duration, opts ...LockOption) *LockAcquirer {
	return &LockAcquirer{
		client: c,
		key:    key,
		ttl:    ttl,
		opts:   opts,
	}
}

// TryLock tries to acquire the lock and returns an error immediately if it is already
// held.
//
// ctx controls only the acquisition. With WithAutoRenew the lock keeps being renewed
// until Unlock is called (or the lock is lost), even if ctx is cancelled meanwhile;
// callers must Unlock to stop the renewal goroutine.
//
// An invalid TTL (<= 0, or below 1ms after being overridden by WithTTL) returns
// ErrInvalidLockTTL without writing to Redis, avoiding a lock that never expires or a
// process-level panic during renewal.
func (la *LockAcquirer) TryLock(ctx context.Context) (*Lock, error) {
	// Apply the options
	lc := &lockConfig{ttl: la.ttl}
	for _, opt := range la.opts {
		opt(lc)
	}

	if err := validateLockTTL(lc.ttl); err != nil {
		return nil, err
	}

	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("redisx: failed to generate lock token: %w", err)
	}

	wrappedKey := la.client.wrapKey(la.key)

	// Acquire the lock with the atomic SET NX EX operation
	ok, err := la.client.client.SetNX(ctx, wrappedKey, token, lc.ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("redisx: failed to acquire lock: %w", err)
	}
	if !ok {
		return nil, ErrLockNotAcquired
	}

	lock := &Lock{
		client:    la.client,
		key:       la.key,
		token:     token,
		ttl:       lc.ttl,
		done:      make(chan struct{}),
		autoRenew: lc.autoRenew,
		lost:      make(chan struct{}),
	}

	// Automatic renewal uses its own lifetime, not the caller's ctx: a cancelled
	// request context must not turn a still-running critical section into an
	// unprotected one.
	if lock.autoRenew {
		renewCtx, cancel := context.WithCancel(context.Background())
		lock.cancel = cancel
		go lock.renewLoop(renewCtx)
	}

	return lock, nil
}

// Lock acquires the lock in a blocking fashion, retrying until it succeeds or the
// context is cancelled. retryInterval is the retry interval; a value <= 0 uses the
// default.
func (la *LockAcquirer) Lock(ctx context.Context, retryInterval time.Duration) (*Lock, error) {
	if retryInterval <= 0 {
		retryInterval = defaultRetryInterval
	}
	// Use a Ticker to reuse a single timer instead of creating a Timer on every
	// iteration.
	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()

	for {
		lock, err := la.TryLock(ctx)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, ErrLockNotAcquired) {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			// keep retrying
		}
	}
}

// Unlock releases the lock and stops automatic renewal.
// A Lua script ensures that only the owner of the lock can release it.
func (l *Lock) Unlock(ctx context.Context) error {
	l.stopRenew()

	// Lua script: delete only when the token matches
	const unlockScript = `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`

	wrappedKey := l.client.wrapKey(l.key)
	result, err := l.client.client.Eval(ctx, unlockScript, []string{wrappedKey}, l.token).Result()
	if err != nil {
		return fmt.Errorf("redisx: failed to release lock: %w", err)
	}

	n, ok := result.(int64)
	if !ok || n == 0 {
		return ErrLockOwnershipMismatch
	}

	return nil
}

// stopRenew stops the renewal goroutine. It is idempotent.
func (l *Lock) stopRenew() {
	l.doneOnce.Do(func() {
		if l.cancel != nil {
			l.cancel()
		}
		if l.autoRenew {
			close(l.done)
		}
	})
}

// Lost is closed when renewal discovers that this process no longer owns the lock
// (it expired and was taken, or the key was deleted). It is only meaningful with
// WithAutoRenew; without renewal it never closes.
func (l *Lock) Lost() <-chan struct{} {
	if l == nil {
		return nil
	}
	return l.lost
}

// IsLost reports whether Lost has been closed.
func (l *Lock) IsLost() bool {
	return l != nil && l.lostFlag.Load()
}

// markLost publishes that the lock is no longer owned. A normal Unlock closes done
// first, and a renewal racing with it must not report a lost lock.
func (l *Lock) markLost() {
	select {
	case <-l.done:
		return
	default:
	}
	l.lostOnce.Do(func() {
		l.lostFlag.Store(true)
		close(l.lost)
	})
}

// validateLockTTL checks whether the lock TTL is usable.
// A TTL <= 0 is treated by Redis as "never expires" and time.NewTicker(ttl/3) would
// panic; a TTL < 1ms is truncated to 0 by the millisecond rounding of the renew
// script, which deletes the lock immediately.
func validateLockTTL(ttl time.Duration) error {
	if ttl < minLockTTL {
		return fmt.Errorf("%w: %v (must be >= %v)", ErrInvalidLockTTL, ttl, minLockTTL)
	}
	return nil
}

// renewLoop is the background automatic renewal loop.
// On a renewal failure it logs and keeps trying: a transient failure should not
// abandon renewal, otherwise the lock would be released early when the TTL expires and
// the critical section would lose its mutual exclusion. ctx is the internal lifetime of
// the lock (cancelled by Unlock), not the context used to acquire it.
func (l *Lock) renewLoop(ctx context.Context) {
	// TryLock has already validated the TTL; this is a safety net so that NewTicker
	// never receives a non-positive value and panics.
	interval := l.ttl / 3
	if interval < minLockTTL {
		interval = minLockTTL
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	const renewScript = `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("pexpire", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	wrappedKey := l.client.wrapKey(l.key)
	for {
		select {
		case <-l.done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), defaultRenewTimeout)
			result, err := l.client.client.Eval(ctx, renewScript, []string{wrappedKey}, l.token, l.ttl.Milliseconds()).Result()
			cancel()

			switch {
			case err != nil:
				// Transient Redis failure: log it and keep trying to renew.
				// If Redis stays unavailable for a long time the lock expires by TTL, which
				// keeps mutual exclusion from breaking silently.
				logger.Error("redisx: failed to renew lock, will retry",
					logger.String("key", l.key),
					logger.Err(err))
			case result == int64(0):
				// The Lua script returning 0 means the token does not match: another client
				// holds the lock (or the key was deleted). Stop renewing and notify the
				// owner through Lost.
				l.markLost()
				return
			}
		}
	}
}

// generateToken generates a random token that identifies the owner of the lock.
func generateToken() (string, error) {
	b := make([]byte, defaultTokenLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// --- Convenience helpers ---

// SetNXWithLock runs a function under a distributed lock with automatic renewal, so
// fn may run longer than ttl without losing mutual exclusion. The lock is released
// automatically once fn returns. A failed acquisition returns ErrLockNotAcquired; if
// the lock is lost while fn runs (the key was deleted or taken over), the returned
// error contains ErrLockLost.
func (c *Client) SetNXWithLock(ctx context.Context, key string, ttl time.Duration, fn func(ctx context.Context) error) error {
	lock, err := c.Locker(key, ttl, WithAutoRenew()).TryLock(ctx)
	if err != nil {
		return err
	}
	defer func() {
		// Release the lock with an independent context so that a cancelled business ctx
		// cannot prevent the release
		_ = lock.Unlock(context.Background())
	}()

	fnErr := fn(ctx)
	if lock.IsLost() {
		if fnErr != nil {
			return errors.Join(fnErr, ErrLockLost)
		}
		return ErrLockLost
	}
	return fnErr
}

// IsLockNotAcquired reports whether the error means the lock was not acquired.
func IsLockNotAcquired(err error) bool {
	return errors.Is(err, ErrLockNotAcquired)
}
