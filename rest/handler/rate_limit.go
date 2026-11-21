package handler

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/chihqiang/ten-go/core/logger"
	"github.com/chihqiang/ten-go/core/ratelimit"
)

// retryAfterer is implemented by limiters that can suggest a retry delay.
type retryAfterer interface {
	RetryAfter() time.Duration
}

// RateLimitHandler rejects requests once the limiter denies them.
type RateLimitHandler struct {
	limiter ratelimit.Limiter
}

// NewRateLimitHandler creates a RateLimitHandler. It panics when limiter is nil,
// since every request would otherwise be rejected as a limiter failure.
func NewRateLimitHandler(limiter ratelimit.Limiter) *RateLimitHandler {
	if limiter == nil {
		panic("handler: RateLimitHandler requires a limiter")
	}
	return &RateLimitHandler{limiter: limiter}
}

// NewRateLimitPerSecondHandler creates a token-bucket limiter permitting rate
// requests per second with bursts up to burst.
func NewRateLimitPerSecondHandler(rate, burst float64) *RateLimitHandler {
	return NewRateLimitHandler(ratelimit.NewTokenBucket(rate, burst))
}

// Handle wraps next with rate limiting.
func (h *RateLimitHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		allowed, err := h.limiter.AllowContext(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			logReject(r, "rest: rate limiter unavailable", logger.Err(err))
			return
		}
		if !allowed {
			var fields []logger.Field
			if delay, ok := retryAfter(h.limiter); ok {
				w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(delay)))
				fields = append(fields, logger.Duration("retry_after", delay))
			}
			w.WriteHeader(http.StatusTooManyRequests)
			logReject(r, "rest: rate limit exceeded", fields...)
			return
		}
		next(w, r)
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
