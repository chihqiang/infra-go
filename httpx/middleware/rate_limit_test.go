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

// 对应 rate_limit.go：HTTP 限流中间件。
// 原 ratelimit.HTTPRateLimit 迁移至此，接口类型 RateLimiter 与 ratelimit.Limiter
// 方法集一致，ratelimit 的各类限流器（TokenBucket/SlidingWindow/Redis）均可直接传入。

// stubLimiter 是 RateLimiter 的测试桩，返回固定结果，保证测试确定性。
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
	// 限流组件异常时放行（fail-open），请求正常处理
	rec := perform(NewRateLimit(&stubLimiter{err: errors.New("redis down")}).Middleware(), ok,
		httptest.NewRequest(http.MethodGet, "/ok", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")
}

func TestRateLimit_NilLimiterDisabled(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }
	// limiter 为 nil 时降级为不限流：请求照常放行，不 panic
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
	// 限流器恒拒绝，用于验证跳过路径不受影响
	mw := NewRateLimit(&stubLimiter{allowed: false}, "/healthz").Middleware()

	// 跳过路径不参与限流
	recHealth := perform(mw, ok, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, recHealth.Code)
	assert.Contains(t, recHealth.Body.String(), "ok")

	// 非跳过路径被限流
	recAPI := perform(mw, ok, httptest.NewRequest(http.MethodGet, "/api", nil))
	assert.Equal(t, http.StatusTooManyRequests, recAPI.Code)
}

func TestRateLimit_TokenBucketIntegration(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }
	// 真实内存令牌桶联动：rate=0（不补充令牌）、容量 1，仅首个请求能通过
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
