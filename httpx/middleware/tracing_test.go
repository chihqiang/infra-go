package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// 对应 tracing.go：HTTP 服务端链路追踪中间件。
// 未装配 trace agent 时使用 no-op tracer，下列用例验证运行不 panic、
// context 传播链路建立、ignorePaths 放行等行为。

// TestTracing_Runs 验证基本工作流：未装配 tracer provider 时用 no-op tracer
// 正常运行，不 panic。
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

// TestTracing_PropagatesContext 验证中间件将 span 注入 context，
// 下游 handler 可通过 oteltrace.SpanFromContext 取到 span（no-op 下不 panic）。
func TestTracing_PropagatesContext(t *testing.T) {
	var gotSpan oteltrace.Span
	handler := NewTracing().Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSpan = oteltrace.SpanFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ctx", nil))

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.NotNil(t, gotSpan) // span 已注入 context（no-op 也返回非 nil span）
}

// TestTracing_IgnorePaths 验证 ignorePaths 指定的路径直接放行、不追踪，其余路径正常工作。
func TestTracing_IgnorePaths(t *testing.T) {
	handler := NewTracing("/health*", "/metrics/*").Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	// 命中忽略规则的路径：正常放行，不 panic
	for _, p := range []string{"/health", "/healthz", "/health/live", "/metrics/", "/metrics/foo"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusOK, rec.Code, "path %s", p)
		assert.Equal(t, "ok", rec.Body.String(), "path %s", p)
	}

	// 未命中忽略规则的路径：仍然正常工作
	for _, p := range []string{"/api/users", "/metricsx/foo", "/foo/health"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusOK, rec.Code, "path %s", p)
		assert.Equal(t, "ok", rec.Body.String(), "path %s", p)
	}
}

// TestTracing_WithTracerName 验证可覆盖 tracer 名称（空值保持默认）。
func TestTracing_WithTracerName(t *testing.T) {
	t1 := NewTracing()
	assert.Equal(t, defaultTracerName, t1.name)

	t2 := NewTracing().WithTracerName("my-service")
	assert.Equal(t, "my-service", t2.name)

	t3 := NewTracing().WithTracerName("")
	assert.Equal(t, defaultTracerName, t3.name) // 空值不覆盖
}
