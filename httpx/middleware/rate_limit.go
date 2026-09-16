package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/httpx/x"
	"github.com/chihqiang/infra-go/logger"
)

// RateLimiter is the limiter interface required by the HTTP rate limit middleware.
// Its method set matches the Limiter interface of the ratelimit package, so
// ratelimit.NewTokenBucket / NewSlidingWindow and the Redis limiters (*struct)
// satisfy it out of the box and there is no hard dependency on the ratelimit
// package (middleware stays lightweight and decoupled, bringing in no concrete
// limiter implementation).
type RateLimiter interface {
	// Allow reports whether the request is allowed to pass.
	Allow() bool
	// AllowContext checks with a context and supports timeout cancellation
	// (the Redis limiter uses it to gain timeout control).
	AllowContext(ctx context.Context) (bool, error)
}

// RateLimit is HTTP rate limit middleware backed by a limiter.
// Every request first asks the limiter for a quota and passes when allowed;
// a throttled request gets 429 Too Many Requests.
//
// When a request is throttled and the limiter implements RetryAfterProvider, an
// exact Retry-After is derived from it (RFC 9110 §10.2.3); otherwise it falls
// back to the value configured by WithRetryAfter, and the header is omitted when
// that is unset too.
type RateLimit struct {
	limiter  RateLimiter
	disabled bool // degrades to no rate limiting when limiter is nil (fail-open)
	matcher  *x.PathMatcher

	// retryAfter is the fallback retry interval used when the limiter cannot
	// estimate one; 0 means the header is not sent.
	retryAfter time.Duration
}

// NewRateLimit creates the HTTP rate limit middleware.
// limiter is the limiter implementation (such as ratelimit.NewTokenBucket); the
// whole service shares one instance, freely combining storage and algorithm:
//
//   - single-machine memory: ratelimit.NewTokenBucket(rate, burst) / NewSlidingWindow(limit, window)
//   - distributed (shared by instances): ratelimit.NewRedisTokenBucket / NewRedisSlidingWindow
//   - switch storage in one call: ratelimit.NewTokenBucketWithStore / NewSlidingWindowWithStore
//   - concurrency cap: ratelimit.NewConcurrency
//
// When limiter is nil there is no rate limiting (requests pass straight through)
// and a warning is logged, so that a missing configuration cannot take the
// service down.
//
// skipPaths lists the paths excluded from rate limiting; matching requests pass
// straight through (commonly used for high-frequency liveness endpoints such as
// health checks). Matching follows httpx.WithLogger: an exact match (e.g.
// "/healthz") or a prefix wildcard ending in "*" (e.g. "/internal/*").
func NewRateLimit(limiter RateLimiter, skipPaths ...string) *RateLimit {
	rl := &RateLimit{
		limiter: limiter,
		matcher: x.NewPathMatcher(skipPaths),
	}
	if limiter == nil {
		// Missing limiter: do not panic, just warn and degrade to no rate
		// limiting (fail-open). Requests must be let through, otherwise calling
		// a method on the nil interface at runtime would panic.
		rl.disabled = true
		logger.Warn("middleware: NewRateLimit called with nil limiter, rate limiting disabled")
	}
	return rl
}

// WithRetryAfter sets the fallback retry interval used when the limiter cannot
// provide an exact estimate.
//
// The built-in ratelimit limiters (TokenBucket/SlidingWindow and their Redis
// variants) all implement RetryAfterProvider, so an accurate Retry-After is
// available without calling this method. Use it only when a custom limiter does
// not implement that interface and a hint is still wanted.
// d <= 0 (the default) means the Retry-After header is not sent — better to omit
// it than to report a fabricated duration.
func (rl *RateLimit) WithRetryAfter(d time.Duration) *RateLimit {
	rl.retryAfter = d
	return rl
}

// resolveRetryAfter returns the retry interval to advertise for this throttle.
// The exact value from the limiter is preferred, falling back to the configured
// value.
func (rl *RateLimit) resolveRetryAfter() time.Duration {
	if p, ok := rl.limiter.(RetryAfterProvider); ok {
		if d := p.RetryAfter(); d > 0 {
			return d
		}
	}
	return rl.retryAfter
}

// Middleware returns the rate limit middleware in the standard form
// func(http.Handler) http.Handler.
//
// The decision uses AllowContext and reuses the request context, so the Redis
// limiter automatically gains timeout control.
// When the underlying limiter fails (e.g. Redis is unavailable) requests are let
// through (fail-open) with only an error log, so a rate limiter outage cannot
// bring the whole service down. To count independently per IP/route/user, build
// a limiter per dimension key (for example a Redis limiter distinguished by key)
// and pass it in.
func (rl *RateLimit) Middleware() func(http.Handler) http.Handler {
	// Missing limiter: pass straight through (fail-open)
	if rl.disabled {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Paths matching a skip rule are not rate limited (handled as usual)
			if rl.matcher.Match(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			allowed, err := rl.limiter.AllowContext(r.Context())
			if err != nil {
				// Limiter failure: fail open so a Redis hiccup cannot take down the
				// whole service
				logger.ErrorCtx(r.Context(), "rate limiter check failed, pass through",
					logger.String("path", r.URL.Path),
					logger.Err(err),
				)
				next.ServeHTTP(w, r)
				return
			}
			if !allowed {
				logger.WarnCtx(r.Context(), "http request dropped by rate limiter",
					logger.String("path", r.URL.Path),
					logger.String("remote", x.ClientIP(r)),
				)
				// RFC 6585 §4: a 429 MAY carry Retry-After to indicate when to retry.
				// Client backoff logic commonly relies on this header, so an exact
				// value is provided whenever possible.
				WriteRetryAfter(r.Context(), w, http.StatusTooManyRequests,
					rl.resolveRetryAfter(), http.StatusText(http.StatusTooManyRequests))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
