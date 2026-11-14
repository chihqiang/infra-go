package retry

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Error definitions.
var (
	// ErrMaxRetries means the maximum number of retries was exceeded.
	ErrMaxRetries = errors.New("retry: max retries exceeded")
	// ErrNoRetry means no further retry is made (used to wrap the final error when RetryIf
	// returns false).
	ErrNoRetry = errors.New("retry: no retry")
)

// Do runs the function and retries automatically on failure, using the default configuration.
func Do(ctx context.Context, fn func(ctx context.Context) error) error {
	c := defaultConfig()
	return doRetry(ctx, fn, c)
}

// DoWithConfig runs the function and retries automatically on failure according to the given
// options.
func DoWithConfig(ctx context.Context, fn func(ctx context.Context) error, opts ...Option) error {
	c := defaultConfig(opts...)
	return doRetry(ctx, fn, c)
}

// DoWithRetryConfig runs the function and retries automatically on failure according to the
// given configuration.
//
// A field-based configuration cannot tell "unset" from "explicitly set to 0", so a
// MaxRetries/Delay/MaxDelay of 0 is always treated as unset and filled with the default (see
// normalize).
// To express "no retry" or "zero delay" explicitly, pass the matching Option in opts
// (Options are applied after the defaults have been filled in, so they do take effect):
//
//	retry.DoWithRetryConfig(ctx, fn, c, retry.WithMaxRetries(0), retry.WithDelay(0))
func DoWithRetryConfig(ctx context.Context, fn func(ctx context.Context) error, c Config, opts ...Option) error {
	return doRetry(ctx, fn, normalize(c, opts...))
}

// normalize fills in the default values for the fields that were not set explicitly and then
// applies the opts on top.
// DoWithRetryConfig and Attempts share this function, which keeps "the number of executions"
// consistent with what was declared.
//
// Limitation: "unset" cannot be told apart from "explicitly set to 0", so a 0 value is always
// treated as unset.
// To express an explicit 0, pass it through opts - they are applied after the defaults have
// been filled in:
//
//	retry.WithMaxRetries(0) // no retry, execute only once
//	retry.WithDelay(0)      // retry immediately, without waiting
//	retry.WithMaxDelay(0)   // no delay ceiling (see capDelay)
func normalize(c Config, opts ...Option) Config {
	if c.RetryIf == nil {
		c.RetryIf = func(error) bool { return true }
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = defaultMaxRetries
	}
	if c.Delay == 0 {
		c.Delay = defaultDelay
	}
	if c.MaxDelay == 0 {
		c.MaxDelay = defaultMaxDelay
	}
	// Options are applied after the defaults have been filled in: callers use this to express
	// an explicit 0.
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// doRetry is the core retry logic.
func doRetry(ctx context.Context, fn func(ctx context.Context) error, c Config) error {
	var lastErr error
	var delay time.Duration

	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		// Check whether the context is already cancelled
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("retry: context cancelled: %w", err)
		}

		// Run the function
		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err

		// Do not retry after the last attempt
		if attempt >= c.MaxRetries {
			break
		}

		// Check whether a retry is needed
		if !c.RetryIf(err) {
			// %w wraps both the sentinel and the original error so that errors.Is/As can
			// recognise the original error type (the old implementation formatted with %s,
			// which broke the error chain here and left callers unable to tell a timeout
			// from a business error).
			return fmt.Errorf("%w: %w", ErrNoRetry, err)
		}

		// Compute the delay
		delay = computeDelay(c, attempt+1, delay)

		// Invoke the callback
		if c.OnRetry != nil {
			c.OnRetry(attempt+1, err)
		}

		// Wait for the delay (a single Timer is reused, avoiding a new one per attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("retry: context cancelled during delay: %w", ctx.Err())
		case <-timer.C:
		}
	}

	// Keep the original error chain (multiple %w), so the existing information is unchanged
	// and errors.Is/As keep working.
	return fmt.Errorf("%w: last error: %w", ErrMaxRetries, lastErr)
}

// --- Helper functions ---

// IsMaxRetries reports whether the error means the maximum number of retries was exceeded.
func IsMaxRetries(err error) bool {
	return errors.Is(err, ErrMaxRetries)
}

// IsNoRetry reports whether the error means no further retry is made.
func IsNoRetry(err error) bool {
	return errors.Is(err, ErrNoRetry)
}

// Attempts returns the total number of executions (first attempt plus retries) once the retry
// configuration takes effect.
//
// It uses the same defaults and Option rules as DoWithRetryConfig (normalize), so the return
// value is the maximum number of times fn is actually executed.
// The old implementation returned c.MaxRetries+1 directly, claiming 1 execution for a
// zero-valued configuration while 4 actually happened.
//
// When opts were passed to DoWithRetryConfig, pass the same set here to stay consistent:
//
//	retry.Attempts(c, retry.WithMaxRetries(0)) // 1
func Attempts(c Config, opts ...Option) int {
	return normalize(c, opts...).MaxRetries + 1
}
