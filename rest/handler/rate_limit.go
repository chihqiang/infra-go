package handler

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/chihqiang/infra-go/core/ratelimit"
)

// retryAfterer is implemented by limiters that can suggest a retry delay.
type retryAfterer interface {
	RetryAfter() time.Duration
}

// RateLimitConfig configures RateLimitHandler.
type RateLimitConfig struct {
	Enabled bool `json:",optional"`
	// Limiter is the rate limiter; it is required when Enabled is true.
	Limiter ratelimit.Limiter `json:"-"`
}

// RateLimitHandler rejects requests once the limiter denies them.
type RateLimitHandler struct {
	config RateLimitConfig
}

// NewRateLimitHandler creates a RateLimitHandler.
func NewRateLimitHandler(config RateLimitConfig) *RateLimitHandler {
	return &RateLimitHandler{config: config}
}

// NewRateLimitPerSecondHandler creates a token-bucket limiter permitting rate
// requests per second with bursts up to burst.
func NewRateLimitPerSecondHandler(rate, burst float64) *RateLimitHandler {
	return NewRateLimitHandler(RateLimitConfig{
		Enabled: true,
		Limiter: ratelimit.NewTokenBucket(rate, burst),
	})
}

// Handle wraps next with rate limiting.
func (h *RateLimitHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	if !h.config.Enabled || h.config.Limiter == nil {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		allowed, err := h.config.Limiter.AllowContext(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if !allowed {
			if delay, ok := retryAfter(h.config.Limiter); ok {
				w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(delay)))
			}
			w.WriteHeader(http.StatusTooManyRequests)
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
