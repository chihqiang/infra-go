package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chihqiang/infra-go/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers middleware.go: the global ErrorHandler injection point, plus the helpers shared
// by the test files.

// testKey is a 16-byte AES-128 test key reused by the cryption / content_security tests.
var testKey = []byte("0123456789abcdef")

// perform wraps next in the middleware chain, serves the request, and returns the response
// recorder.
func perform(mw func(http.Handler) http.Handler, next http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)
	return rec
}

// silenceLogger redirects the global logger output to a temporary file so test logs do not
// pollute stdout/stderr. The original global logger is restored when the test ends.
// Middleware tests that emit logs should call this function first.
func silenceLogger(t *testing.T) {
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

// oversizedRequest builds a POST request whose Content-Length is over the limit (to trigger
// MaxBytes and similar limiting middlewares).
func oversizedRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.ContentLength = int64(len(body))
	return req
}

// --- ErrorHandler ---

func TestErrorHandler_Default(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	rec := perform(NewMaxBytes(4).Middleware(), ok, oversizedRequest("0123456789"))

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")
	assert.Contains(t, rec.Body.String(), "request entity too large")
}

func TestErrorHandler_CustomAndRestore(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// inject a custom error renderer (JSON)
	SetErrorHandler(func(_ context.Context, w http.ResponseWriter, status int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"msg":"` + msg + `"}`))
	})

	rec := perform(NewMaxBytes(4).Middleware(), ok, oversizedRequest("0123456789"))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), `"request entity too large"`)

	// SetErrorHandler(nil) restores the default http.Error
	SetErrorHandler(nil)
	rec2 := perform(NewMaxBytes(4).Middleware(), ok, oversizedRequest("0123456789"))
	assert.Contains(t, rec2.Header().Get("Content-Type"), "text/plain")
	assert.Contains(t, rec2.Body.String(), "request entity too large")
}

// customJSONHandler marks whether request-scoped rendering took effect.
func customJSONHandler(_ context.Context, w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"scope":"request","msg":"` + msg + `"}`))
}

// TestErrorHandler_RequestScoped verifies error rendering can take effect per request
// without polluting global state or affecting other requests (httpx's Server relies on it
// to inject the JSON renderer for every request).
func TestErrorHandler_RequestScoped(t *testing.T) {
	silenceLogger(t)
	SetErrorHandler(nil)
	t.Cleanup(func() { SetErrorHandler(nil) })

	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	mw := NewMaxBytes(4).Middleware()

	// not injected: default plain text
	recDefault := perform(mw, ok, oversizedRequest("0123456789"))
	assert.Contains(t, recDefault.Header().Get("Content-Type"), "text/plain")

	// request-scoped JSON injection
	req := oversizedRequest("0123456789")
	req = req.WithContext(ContextWithErrorHandler(req.Context(), customJSONHandler))
	recScoped := perform(mw, ok, req)
	assert.Equal(t, "application/json", recScoped.Header().Get("Content-Type"))
	assert.Contains(t, recScoped.Body.String(), `"scope":"request"`)

	// the global handler is still untouched: later requests keep the plain-text renderer
	recAfter := perform(mw, ok, oversizedRequest("0123456789"))
	assert.Contains(t, recAfter.Header().Get("Content-Type"), "text/plain",
		"a request-scoped injection must not leak into the global handler")
}

// TestErrorHandler_RequestScopedOverridesGlobal verifies the request scope wins over global.
func TestErrorHandler_RequestScopedOverridesGlobal(t *testing.T) {
	silenceLogger(t)
	SetErrorHandler(func(_ context.Context, w http.ResponseWriter, _ int, _ string) {
		w.Header().Set("X-Scope", "global")
	})
	t.Cleanup(func() { SetErrorHandler(nil) })

	ctx := ContextWithErrorHandler(context.Background(), func(_ context.Context, w http.ResponseWriter, _ int, _ string) {
		w.Header().Set("X-Scope", "request")
	})

	rec := httptest.NewRecorder()
	WriteError(ctx, rec, http.StatusForbidden, "x")
	assert.Equal(t, "request", rec.Header().Get("X-Scope"))
}

// TestErrorHandler_GlobalFallback verifies the global value is used when the request carries
// no renderer.
func TestErrorHandler_GlobalFallback(t *testing.T) {
	SetErrorHandler(func(_ context.Context, w http.ResponseWriter, _ int, _ string) {
		w.Header().Set("X-Scope", "global")
	})
	t.Cleanup(func() { SetErrorHandler(nil) })

	rec := httptest.NewRecorder()
	WriteError(context.Background(), rec, http.StatusForbidden, "x")
	assert.Equal(t, "global", rec.Header().Get("X-Scope"))
}

// TestErrorHandler_NilContext verifies a nil context does not panic (falls back to global).
func TestErrorHandler_NilContext(t *testing.T) {
	SetErrorHandler(func(_ context.Context, w http.ResponseWriter, _ int, _ string) {
		w.Header().Set("X-Scope", "global")
	})
	t.Cleanup(func() { SetErrorHandler(nil) })

	rec := httptest.NewRecorder()
	require.NotPanics(t, func() {
		WriteError(nil, rec, http.StatusForbidden, "x")
	})
	assert.Equal(t, "global", rec.Header().Get("X-Scope"))
}

// TestContextWithErrorHandler_Nil verifies a nil renderer is not stored in the context.
func TestContextWithErrorHandler_Nil(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, ctx, ContextWithErrorHandler(ctx, nil))
}
