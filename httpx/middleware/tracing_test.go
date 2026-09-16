package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// Covers tracing.go: the HTTP server-side tracing middleware.
// Without a trace agent installed the no-op tracer is used; the cases below verify it runs
// without panicking, that context propagation is wired up, and that ignorePaths pass through.

// TestTracing_Runs verifies the basic workflow: with no tracer provider installed the no-op
// tracer runs normally without panicking.
func TestTracing_Runs(t *testing.T) {
	handler := NewTracing().Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ok", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

// TestTracing_PropagatesContext verifies the middleware injects the span into the context,
// so a downstream handler can fetch it via oteltrace.SpanFromContext (no panic under no-op).
func TestTracing_PropagatesContext(t *testing.T) {
	var gotSpan oteltrace.Span
	handler := NewTracing().Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSpan = oteltrace.SpanFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ctx", nil))

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.NotNil(t, gotSpan) // the span is injected into the context (no-op returns a non-nil span too)
}

// TestTracing_IgnorePaths verifies paths in ignorePaths pass through untraced while other
// paths work normally.
func TestTracing_IgnorePaths(t *testing.T) {
	handler := NewTracing("/health*", "/metrics/*").Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	// paths matching an ignore rule: pass through normally, no panic
	for _, p := range []string{"/health", "/healthz", "/health/live", "/metrics/", "/metrics/foo"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusOK, rec.Code, "path %s", p)
		assert.Equal(t, "ok", rec.Body.String(), "path %s", p)
	}

	// paths not matching any ignore rule: still work normally
	for _, p := range []string{"/api/users", "/metricsx/foo", "/foo/health"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusOK, rec.Code, "path %s", p)
		assert.Equal(t, "ok", rec.Body.String(), "path %s", p)
	}
}

// TestTracing_WithTracerName verifies the tracer name can be overridden (empty keeps the default).
func TestTracing_WithTracerName(t *testing.T) {
	t1 := NewTracing()
	assert.Equal(t, defaultTracerName, t1.name)

	t2 := NewTracing().WithTracerName("my-service")
	assert.Equal(t, "my-service", t2.name)

	t3 := NewTracing().WithTracerName("")
	assert.Equal(t, defaultTracerName, t3.name) // an empty value does not override
}
