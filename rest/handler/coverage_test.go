package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chihqiang/infra-go/core/ratelimit"
	"github.com/chihqiang/infra-go/rest/token"
)

func newTestTokenParser(t *testing.T) *token.Parser {
	t.Helper()
	p, err := token.NewParser(token.Config{Secret: "0123456789abcdef"})
	require.NoError(t, err)
	return p
}

func TestAuthHandler_CallbackAndRealm(t *testing.T) {
	callbackCalled := false
	unwrapped := false

	handlerFn := NewAuthHandler(AuthConfig{
		Enabled: true,
		Parser:  newTestTokenParser(t),
		Realm:   "custom-realm",
		Callback: func(w http.ResponseWriter, _ *http.Request, _ error) {
			callbackCalled = true
			if u, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
				unwrapped = u.Unwrap() != nil
			}
			w.Header().Set("X-Reason", "denied")
			_, _ = w.Write([]byte("custom rejection"))
		},
	}).Handle(func(http.ResponseWriter, *http.Request) { t.Fatal("must not reach next") })

	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.True(t, callbackCalled)
	assert.True(t, unwrapped)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.Equal(t, "custom rejection", recorder.Body.String())
	assert.Contains(t, recorder.Header().Get("WWW-Authenticate"), `realm="custom-realm"`)
}

func TestRequestIDHandler_InvalidIncomingID(t *testing.T) {
	var ctxID string
	handlerFn := NewRequestIDHandler(RequestIDConfig{Enabled: true}).Handle(
		func(_ http.ResponseWriter, r *http.Request) {
			ctxID, _ = RequestIDFromContext(r.Context())
		})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(RequestIDHeader, "not a valid id!")
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	assert.NotEmpty(t, ctxID)
	assert.NotEqual(t, "not a valid id!", ctxID)
	assert.Equal(t, ctxID, recorder.Header().Get(RequestIDHeader))
}

func TestRequestIDHandler_KeepsValidIncomingID(t *testing.T) {
	handlerFn := NewRequestIDHandler(RequestIDConfig{Enabled: true}).Handle(
		func(_ http.ResponseWriter, r *http.Request) {
			id, _ := RequestIDFromContext(r.Context())
			assert.Equal(t, "abc-123", id)
		})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(RequestIDHeader, "abc-123")
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)
	assert.Equal(t, "abc-123", recorder.Header().Get(RequestIDHeader))
}

func TestRateLimitHandler_RetryAfter(t *testing.T) {
	limiter := ratelimit.NewTokenBucket(0.001, 1) // one token, almost no refill
	handlerFn := NewRateLimitHandler(RateLimitConfig{Enabled: true, Limiter: limiter}).Handle(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	first := httptest.NewRecorder()
	handlerFn.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusNoContent, first.Code)

	second := httptest.NewRecorder()
	handlerFn.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusTooManyRequests, second.Code)
	assert.NotEmpty(t, second.Header().Get("Retry-After"))
}

type failingLimiter struct{ err error }

func (l failingLimiter) Allow() bool { return false }
func (l failingLimiter) AllowContext(context.Context) (bool, error) {
	return false, l.err
}

type denyingLimiter struct{}

func (denyingLimiter) Allow() bool                                { return false }
func (denyingLimiter) AllowContext(context.Context) (bool, error) { return false, nil }

func TestRateLimitHandler_LimiterErrors(t *testing.T) {
	handlerFn := NewRateLimitHandler(RateLimitConfig{
		Enabled: true,
		Limiter: failingLimiter{err: errors.New("redis down")},
	}).Handle(func(http.ResponseWriter, *http.Request) { t.Fatal("must not reach next") })

	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)

	// A limiter without RetryAfter must still produce 429, without the header.
	denyFn := NewRateLimitHandler(RateLimitConfig{Enabled: true, Limiter: denyingLimiter{}}).Handle(
		func(http.ResponseWriter, *http.Request) { t.Fatal("must not reach next") })
	recorder = httptest.NewRecorder()
	denyFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
	assert.Empty(t, recorder.Header().Get("Retry-After"))
}

func TestSignatureSignRequestHandlesNilRequest(t *testing.T) {
	key := []byte("key")
	signature := SignRequest(key, 1, nil, []byte("body"))
	require.NotEmpty(t, signature)
	assert.Equal(t, signature, Sign(key, 1, http.MethodGet, "/", "", []byte("body")))
	assert.Equal(t, "/", signaturePath(nil))
}

func TestSignatureHandler_InvalidTimestamp(t *testing.T) {
	handlerFn := newSignatureHandler(SignatureConfig{Keys: [][]byte{[]byte("key")}}).Handle(
		func(http.ResponseWriter, *http.Request) { t.Fatal("must not reach next") })

	request := httptest.NewRequest(http.MethodPost, "/pay", strings.NewReader("body"))
	request.Header.Set(HeaderTimestamp, "not-a-number")
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestNewLogHandler_InvalidTrustedProxies(t *testing.T) {
	_, err := NewLogHandler(LogConfig{Enabled: true, TrustedProxies: []string{"not-an-ip"}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid trusted proxy")

	// A disabled handler does not parse its configuration.
	_, err = NewLogHandler(LogConfig{Enabled: false, TrustedProxies: []string{"not-an-ip"}}, nil)
	require.NoError(t, err)
}

func TestPatternHelpers(t *testing.T) {
	assert.Equal(t, "", PatternFromContext(nil))
	ctx := context.Background()
	assert.Equal(t, ctx, ContextWithPattern(ctx, ""))
	assert.Equal(t, "GET /x", PatternFromContext(ContextWithPattern(ctx, "GET /x")))

	// A nil context must not panic; it is replaced by a background context.
	assert.Equal(t, "", PatternFromContext(ContextWithPattern(nil, "")))
	assert.Equal(t, "GET /y", PatternFromContext(ContextWithPattern(nil, "GET /y")))
}

func TestTimeoutConfigDisabled(t *testing.T) {
	called := false
	handlerFn := NewTimeoutHandler(TimeoutConfig{Enabled: false, Timeout: time.Second}).Handle(
		func(http.ResponseWriter, *http.Request) { called = true })
	handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.True(t, called)
}
