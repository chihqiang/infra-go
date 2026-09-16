package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chihqiang/infra-go/ratelimit"
	"github.com/stretchr/testify/assert"
)

// Covers rate_limit.go: the HTTP rate limiting middleware.
// Originally ratelimit.HTTPRateLimit, now migrated here. The RateLimiter interface has the
// same method set as ratelimit.Limiter, so every ratelimit limiter (TokenBucket,
// SlidingWindow, Redis) can be passed in directly.

// stubLimiter is a RateLimiter test stub returning fixed results to keep tests deterministic.
type stubLimiter struct {
	allowed bool
	err     error
}

func (s *stubLimiter) Allow() bool { return s.allowed }

func (s *stubLimiter) AllowContext(ctx context.Context) (bool, error) { return s.allowed, s.err }

func TestRateLimit_AllowsWithinLimit(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }
	rec := perform(NewRateLimit(&stubLimiter{allowed: true}).Middleware(), ok,
		httptest.NewRequest(http.MethodGet, "/ok", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")
}

func TestRateLimit_RejectsOverLimit(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }
	rec := perform(NewRateLimit(&stubLimiter{allowed: false}).Middleware(), ok,
		httptest.NewRequest(http.MethodGet, "/ok", nil))

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func TestRateLimit_FailOpenOnLimiterError(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }
	// the limiter erroring out fails open: the request is handled normally
	rec := perform(NewRateLimit(&stubLimiter{err: errors.New("redis down")}).Middleware(), ok,
		httptest.NewRequest(http.MethodGet, "/ok", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")
}

func TestRateLimit_NilLimiterDisabled(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }
	// a nil limiter degrades to no limiting: requests pass through as usual, no panic
	mw := NewRateLimit(nil).Middleware()
	for i := 0; i < 5; i++ {
		rec := perform(mw, ok, httptest.NewRequest(http.MethodGet, "/ok", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "ok")
	}
}

func TestRateLimit_SkipsConfiguredPaths(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }
	// the limiter always denies, which shows skipped paths are unaffected
	mw := NewRateLimit(&stubLimiter{allowed: false}, "/healthz").Middleware()

	// skipped paths are not rate limited
	recHealth := perform(mw, ok, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, recHealth.Code)
	assert.Contains(t, recHealth.Body.String(), "ok")

	// non-skipped paths are rate limited
	recAPI := perform(mw, ok, httptest.NewRequest(http.MethodGet, "/api", nil))
	assert.Equal(t, http.StatusTooManyRequests, recAPI.Code)
}

func TestRateLimit_TokenBucketIntegration(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }
	// real in-memory token bucket: rate=0 (no refill), capacity 1, so only the first request
	// passes
	mw := NewRateLimit(ratelimit.NewTokenBucket(0, 1)).Middleware()

	rec1 := perform(mw, ok, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Contains(t, rec1.Body.String(), "ok")

	rec2 := perform(mw, ok, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

func TestRateLimit_SkipPrefixWildcard(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }
	mw := NewRateLimit(&stubLimiter{allowed: false}, "/internal/*").Middleware()

	rec := perform(mw, ok, httptest.NewRequest(http.MethodGet, "/internal/health", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")
}
