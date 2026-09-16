package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chihqiang/infra-go/httpx/middleware"
	"github.com/chihqiang/infra-go/jwt"
	"github.com/chihqiang/infra-go/logger"
	"github.com/chihqiang/infra-go/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Forwarding layer smoke tests: they verify that the httpx.With* convenience helpers
// (internal_middleware.go) correctly adapt the standard middleware from the
// httpx/middleware subpackage into httpx.Middleware and register it via Server.Use.
// The core behavior (the logic of each middleware) is already tested file by file in
// the httpx/middleware subpackage, so this only covers the wiring of representative
// middleware to guard the adapter layer against regressions.

// silenceHttpxLogger silences the global logger so test logs do not pollute
// stdout/stderr.
func silenceHttpxLogger(t *testing.T) {
	t.Helper()
	tmpLog := filepath.Join(t.TempDir(), "test.log")
	l := logger.New(logger.Config{Output: []string{tmpLog}, Caller: false})
	old := logger.GetGlobal()
	logger.SetGlobal(l)
	t.Cleanup(func() {
		logger.SetGlobal(old)
		_ = l.Sync()
	})
}

func TestMiddlewareCompat_Recovery(t *testing.T) {
	silenceHttpxLogger(t)
	s := newTestServer()
	// RequestID outermost: the request_id lands in the context first so Recovery can read it
	s.Use(WithRequestID(), WithRecovery())
	s.AddRoute(Route{
		Method: "GET", Path: "/panic", Handler: func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/panic", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "internal server error")

	// Recovery renders the error with the request context, so the panic response carries
	// the request_id too, consistent with the other middleware (timeout, rate limit,
	// size limit) and easy to correlate with logs.
	id := rec.Header().Get(middleware.HeaderRequestID)
	require.NotEmpty(t, id, "Recovery combined with RequestID should write the response header")
	assert.Contains(t, rec.Body.String(), `"request_id":"`+id+`"`,
		"the panic response body should carry the request_id to help diagnose the 500")
}

func TestMiddlewareCompat_RequestID(t *testing.T) {
	s := newTestServer()
	s.Use(WithRequestID())
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, rec.Header().Get(middleware.HeaderRequestID))
}

func TestMiddlewareCompat_Cors(t *testing.T) {
	s := newTestServer()
	s.Use(WithCors("*"))
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec := doRequestWithHeaders(t, s, http.MethodGet, "/ok", nil,
		map[string]string{"Origin": "http://allowed.com"})
	assert.Equal(t, http.StatusOK, rec.Code)
	// allowAll echoes the concrete Origin (rather than "*"): combined with
	// Allow-Credentials the wildcard would be rejected by browsers.
	assert.Equal(t, "http://allowed.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Origin", rec.Header().Get("Vary"))
}

func TestMiddlewareCompat_Timeout(t *testing.T) {
	s := newTestServer()
	s.Use(WithTimeout(30 * time.Millisecond))
	s.AddRoute(Route{
		Method: "GET", Path: "/slow", Handler: func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(300 * time.Millisecond)
			OkJSON(w, "ok")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/slow", nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestMiddlewareCompat_MaxBytes(t *testing.T) {
	silenceHttpxLogger(t)
	s := newTestServer()
	s.Use(WithMaxBytes(4))
	s.AddRoute(Route{
		Method: "POST", Path: "/upload", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec := doRequest(t, s, http.MethodPost, "/upload", strings.NewReader("0123456789"))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestMiddlewareCompat_ErrorResponseUsesUnifiedJSON(t *testing.T) {
	// RequestID + Recovery combined: the error response should be the unified httpx JSON.
	// This verifies that the per-request error rendering wired up by the Server
	// (middleware.ContextWithErrorHandler) works, rather than the default plain-text
	// http.Error.
	silenceHttpxLogger(t)
	s := newTestServer()
	s.Use(WithRequestID(), WithRecovery())
	s.AddRoute(Route{
		Method: "GET", Path: "/panic", Handler: func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/panic", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	// The RequestID middleware still writes the response header.
	assert.NotEmpty(t, rec.Header().Get(middleware.HeaderRequestID))
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rec.Body.String(), "internal server error")
}

// TestMiddlewareCompat_NoGlobalErrorHandlerMutation is a regression test: importing
// httpx must not rewrite the process-wide global error rendering of httpx/middleware.
//
// Historical defect: httpx called middleware.SetErrorHandler in init(), so merely
// importing httpx would silently change the error response format of gin/echo routes in
// the same process (they use the middleware subpackage too), and since imports are
// independent of call order, users could not opt out.
// The injection is now per request via the Server, and this test pins down that the
// global is never written.
func TestMiddlewareCompat_NoGlobalErrorHandlerMutation(t *testing.T) {
	// A call that bypasses the httpx Server and uses the middleware subpackage directly
	// should get the default plain text
	rec := httptest.NewRecorder()
	middleware.WriteError(context.Background(), rec, http.StatusForbidden, "denied")

	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain",
		"importing httpx must not rewrite the global error rendering of middleware")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "denied")
}

// TestMiddlewareCompat_ScopedHandlerDoesNotLeak verifies that after a request is
// dispatched through httpx, the global error rendering is still not rewritten (the
// scope is limited to that request).
func TestMiddlewareCompat_ScopedHandlerDoesNotLeak(t *testing.T) {
	silenceHttpxLogger(t)
	s := newTestServer()
	s.Use(WithRecovery())
	s.AddRoute(Route{
		Method: http.MethodGet, Path: "/panic", Handler: func(http.ResponseWriter, *http.Request) {
			panic("boom")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/panic", nil)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	// The request itself should be the unified httpx JSON
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	// Once the request is over, the global is still the default plain text
	plain := httptest.NewRecorder()
	middleware.WriteError(context.Background(), plain, http.StatusForbidden, "denied")
	assert.Contains(t, plain.Header().Get("Content-Type"), "text/plain",
		"the rendering of an httpx request should stay scoped to that request and not leak globally")
}

func TestMiddlewareCompat_Tracing(t *testing.T) {
	// WithTracing wiring: after registration a normal request returns 200; a path
	// matching ignorePaths passes straight through
	s := newTestServer()
	s.Use(WithTracing("/health*"))
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})
	s.AddRoute(Route{
		Method: "GET", Path: "/health", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "health")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")

	recHealth := doRequest(t, s, http.MethodGet, "/health", nil)
	assert.Equal(t, http.StatusOK, recHealth.Code)
	assert.Contains(t, recHealth.Body.String(), "health")
}

func TestMiddlewareCompat_RateLimit(t *testing.T) {
	silenceHttpxLogger(t)
	// WithRateLimit wiring: a real token bucket with rate=0 and capacity 1 → the first
	// request gets 200, the second 429
	s := newTestServer()
	s.Use(WithRateLimit(ratelimit.NewTokenBucket(0, 1)))
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec1 := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec1.Code)

	rec2 := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

func TestMiddlewareCompat_RateLimitSkipPaths(t *testing.T) {
	silenceHttpxLogger(t)
	s := newTestServer()
	s.Use(WithRateLimit(ratelimit.NewTokenBucket(0, 1), "/healthz"))
	s.AddRoute(Route{
		Method: "GET", Path: "/healthz", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "health")
		},
	})
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	// Skipped paths are unaffected by rate limiting (may be accessed repeatedly)
	rec1 := doRequest(t, s, http.MethodGet, "/healthz", nil)
	rec2 := doRequest(t, s, http.MethodGet, "/healthz", nil)
	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Equal(t, http.StatusOK, rec2.Code)

	// Non-skipped path: the first passes, the second is throttled
	rec3 := doRequest(t, s, http.MethodGet, "/ok", nil)
	rec4 := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec3.Code)
	assert.Equal(t, http.StatusTooManyRequests, rec4.Code)
}
func TestMiddlewareCompat_WithJWT(t *testing.T) {
	silenceHttpxLogger(t)
	j, err := jwt.New(jwt.Config{Secret: "test-secret-key"})
	require.NoError(t, err)

	s := newTestServer()
	s.Use(WithJWT(j, func(r *http.Request) string { return r.Header.Get("X-Token") }))
	s.AddRoute(Route{
		Method: "GET", Path: "/me", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, jwt.ClaimsFromContext(r.Context())[jwt.ClaimKeyUserID])
		},
	})

	// No token → 401 (unified httpx JSON, confirming the middleware error mechanism is
	// injected)
	rec := doRequest(t, s, http.MethodGet, "/me", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rec.Body.String(), "token is missing")

	// Valid token → pass through, and the handler reads user_id from the business claims
	token, err := j.GenerateAccessToken(jwt.Claims{jwt.ClaimKeyUserID: "user-123"})
	require.NoError(t, err)
	recOK := doRequestWithHeaders(t, s, http.MethodGet, "/me", nil,
		map[string]string{"X-Token": token})
	assert.Equal(t, http.StatusOK, recOK.Code)
	assert.Contains(t, recOK.Body.String(), "user-123")
}

func TestAsMiddleware_Adapter(t *testing.T) {
	// AsMiddleware registers a standard func(http.Handler) http.Handler middleware into
	// the httpx server.
	// A custom standard middleware (standard net/http shape) is used here to verify it
	// can be registered and takes effect.
	s := newTestServer()
	s.Use(AsMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Custom-Middleware", "1")
			next.ServeHTTP(w, r)
		})
	}))
	s.AddRoute(Route{
		Method: "GET", Path: "/ok", Handler: func(w http.ResponseWriter, r *http.Request) {
			OkJSON(w, "ok")
		},
	})

	rec := doRequest(t, s, http.MethodGet, "/ok", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "1", rec.Header().Get("X-Custom-Middleware"))
}
