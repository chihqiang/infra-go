package handler

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/chihqiang/infra-go/core/ratelimit"
	"github.com/chihqiang/infra-go/rest/chain"
)

// retryAfterer is implemented by limiters that can suggest a retry delay.
type retryAfterer interface {
	RetryAfter() time.Duration
}

// RateLimitHandler rejects requests once limiter denies them. Throttled responses
// carry Retry-After when the limiter suggests a delay, and a limiter failure is
// treated as unavailable (503) rather than allowed.
func RateLimitHandler(limiter ratelimit.Limiter) chain.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		if limiter == nil {
			return next
		}

		return func(w http.ResponseWriter, r *http.Request) {
			allowed, err := limiter.AllowContext(r.Context())
			if err != nil {
				writeError(w, r, http.StatusServiceUnavailable, "rate limiter unavailable")
				return
			}
			if !allowed {
				if delay, ok := retryAfter(limiter); ok {
					w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(delay)))
				}
				writeError(w, r, http.StatusTooManyRequests, "too many requests")
				return
			}
			next(w, r)
		}
	}
}

// RateLimitPerSecondHandler creates a token bucket permitting rate requests per second
// with bursts up to burst.
func RateLimitPerSecondHandler(rate, burst float64) chain.Middleware {
	return RateLimitHandler(ratelimit.NewTokenBucket(rate, burst))
}

// MaxConnsHandler caps in-flight requests, rejecting excess traffic with 503.
func MaxConnsHandler(limit int) chain.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		if limit <= 0 {
			return next
		}

		guard := ratelimit.NewConcurrency(limit)
		return func(w http.ResponseWriter, r *http.Request) {
			if !guard.Allow() {
				writeError(w, r, http.StatusServiceUnavailable, "too many connections")
				return
			}
			defer guard.Release()

			next(w, r)
		}
	}
}

func retryAfter(limiter ratelimit.Limiter) (time.Duration, bool) {
	suggester, ok := limiter.(retryAfterer)
	if !ok {
		return 0, false
	}
	delay := suggester.RetryAfter()
	if delay <= 0 {
		return 0, false
	}
	return delay, true
}

// retryAfterSeconds rounds up to whole seconds so small delays never become 0.
func retryAfterSeconds(delay time.Duration) int {
	seconds := int(math.Ceil(delay.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}
