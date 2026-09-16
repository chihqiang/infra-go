package breaker

import (
	"context"
	"errors"
	"time"
)

// ErrServiceUnavailable is returned when the breaker is open (request rejected).
var ErrServiceUnavailable = errors.New("breaker: service unavailable")

// Acceptable reports whether an error is acceptable (not counted as a failure).
// For example, 4xx business errors are usually caller mistakes and should not
// trip the breaker:
//
//	b.DoWithAcceptable(req, func(err error) bool {
//	    return errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnauthorized)
//	})
type Acceptable func(err error) bool

// Fallback is the degradation logic executed when the breaker is open.
type Fallback func(err error) error

// Promise is returned by Breaker.Allow; the caller must report the outcome after
// the request completes:
//   - success: call Accept()
//   - failure: call Reject(reason); reason is logged as the failure cause
type Promise interface {
	// Accept tells the breaker that this call succeeded.
	Accept()
	// Reject tells the breaker that this call failed.
	// reason is the failure cause; when the breaker opens, the most recent
	// failure reasons are logged.
	Reject(reason string)
}

// Breaker defines the breaker interface, based on the Google SRE adaptive
// overload algorithm.
//
// Three states:
//   - Closed: error rate below the threshold; requests are served and counted
//   - Open: error rate above the threshold; further requests fail fast (ErrServiceUnavailable)
//   - Half-Open: after a cool-down a probe request is allowed; success closes the breaker
//
// Usage:
//
//	b := breaker.NewBreaker(breaker.WithName("payment-gateway"))
//	err := b.Do(func() error { return callPaymentAPI(req) })
type Breaker interface {
	// Name returns the breaker name, used to tell breakers apart in logs and metrics.
	Name() string

	// Allow checks whether a request may pass.
	// On success it returns a Promise (the caller must Accept/Reject it later);
	// when the breaker is open it returns ErrServiceUnavailable.
	Allow() (Promise, error)
	// AllowCtx is like Allow, but returns the context error directly when the
	// context is already cancelled.
	AllowCtx(ctx context.Context) (Promise, error)

	// Do runs the request; when the breaker is open it returns
	// ErrServiceUnavailable immediately without running the request.
	// A panic in the request counts as a failure and is re-panicked.
	Do(req func() error) error
	// DoCtx is like Do, but returns the context error directly when the context
	// is already cancelled.
	DoCtx(ctx context.Context, req func() error) error

	// DoWithAcceptable is like Do, but acceptable decides exactly which errors
	// count as failures.
	DoWithAcceptable(req func() error, acceptable Acceptable) error
	// DoWithAcceptableCtx is like DoWithAcceptable, with context support.
	DoWithAcceptableCtx(ctx context.Context, req func() error, acceptable Acceptable) error

	// DoWithFallback is like Do, but runs the fallback logic when the breaker is open.
	DoWithFallback(req func() error, fallback Fallback) error
	// DoWithFallbackCtx is like DoWithFallback, with context support.
	DoWithFallbackCtx(ctx context.Context, req func() error, fallback Fallback) error

	// DoWithFallbackAcceptable is like DoWithFallback, plus a custom
	// error-accepting policy.
	DoWithFallbackAcceptable(req func() error, fallback Fallback, acceptable Acceptable) error
	// DoWithFallbackAcceptableCtx is like DoWithFallbackAcceptable, with context support.
	DoWithFallbackAcceptableCtx(ctx context.Context, req func() error, fallback Fallback,
		acceptable Acceptable) error
}

// sreConfig holds the Google SRE adaptive throttling algorithm parameters.
// Defaults live in the package constants in sre.go; they can be customised via
// Option, e.g. to match the values recommended by the official SRE book
// (Handling Overload, chapter 21, Client-Side Throttling): a 2-minute window
// and amplification factor K=2, via WithSREDefaults().
type sreConfig struct {
	window     time.Duration // statistical window length
	k          float64       // request amplification factor
	minK       float64       // lower bound of the amplification factor
	protection int64         // low-traffic protection threshold
}

// defaultSREConfig returns the default SRE algorithm parameters
// (aggressive mode: 10s window / K=1.5).
func defaultSREConfig() sreConfig {
	return sreConfig{
		window:     window,
		k:          k,
		minK:       minK,
		protection: protection,
	}
}

// Option customises a breaker.
type Option func(*circuitBreaker)

// WithName sets the breaker name, used to tell breakers apart in logs and metrics.
func WithName(name string) Option {
	return func(b *circuitBreaker) { b.name = name }
}

// WithWindow sets the statistical window length.
// Smaller windows react faster to bursts but are more sensitive to sporadic or
// low-traffic clients; the official SRE book recommends 2 minutes. Defaults to
// 10 seconds. Non-positive values are ignored.
func WithWindow(d time.Duration) Option {
	return func(b *circuitBreaker) {
		if d > 0 {
			b.sre.window = d
		}
	}
}

// WithK sets the request amplification factor K: requests start to be rejected
// probabilistically once requests reach K*accepts. Smaller K is more aggressive
// (more rejections), larger K is more permissive. The official SRE book
// recommends 2. Defaults to 1.5. Non-positive values are ignored.
func WithK(k float64) Option {
	return func(b *circuitBreaker) {
		if k > 0 {
			b.sre.k = k
		}
	}
}

// WithMinK sets the lower bound of the amplification factor, so that a run of
// failures cannot drag the weight down far enough to become over-aggressive.
// Defaults to 1.1. Non-positive values are ignored.
func WithMinK(k float64) Option {
	return func(b *circuitBreaker) {
		if k > 0 {
			b.sre.minK = k
		}
	}
}

// WithProtection sets the low-traffic protection threshold: when the total
// number of requests in the window is below it, no request is rejected, so
// sporadic or low-traffic clients are not punished (matching the advice in the
// docs for occasionally-calling clients). Defaults to 5. Negative values are
// ignored.
func WithProtection(n int64) Option {
	return func(b *circuitBreaker) {
		if n >= 0 {
			b.sre.protection = n
		}
	}
}

// WithSREDefaults uses the parameters recommended by the official SRE book
// (Handling Overload, chapter 21, Client-Side Throttling): a 2-minute
// statistical window and amplification factor K=2.
// Compared with the default aggressive mode (10s / K=1.5) it stays closer to
// the original algorithm: a little more backend capacity is wasted, but state
// propagates more steadily, which suits services with steady traffic. It can be
// combined with WithWindow/WithK for fine-tuning.
func WithSREDefaults() Option {
	return func(b *circuitBreaker) {
		b.sre.window = 2 * time.Minute
		b.sre.k = 2
		b.sre.minK = minK
		b.sre.protection = protection
	}
}

// defaultAcceptable is the default error-accepting policy: only a nil error
// counts as success.
func defaultAcceptable(err error) bool {
	return err == nil
}
