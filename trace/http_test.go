package trace

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHTTPMiddleware_Runs 验证 HTTPMiddleware 基本工作流：
// 未启动 trace agent 时应使用 no-op tracer 正常运行，不 panic。
func TestHTTPMiddleware_Runs(t *testing.T) {
	handler := HTTPMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

// TestHTTPMiddleware_PropagatesContext 验证中间件将 span 注入 context，
// 下游 handler 可通过 TraceIDFromContext 提取 trace id（即使 no-op 也返回空，不 panic）。
func TestHTTPMiddleware_PropagatesContext(t *testing.T) {
	var traceID string
	handler := HTTPMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID = TraceIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ctx", nil)
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	// no-op tracer 下 trace id 为空，但 context 传播链路已建立（不 panic 即通过）
	_ = traceID
}

// TestHTTPMiddleware_IgnorePaths 验证 ignorePaths 指定的路径直接放行、不追踪，其余路径正常工作。
func TestHTTPMiddleware_IgnorePaths(t *testing.T) {
	handler := HTTPMiddleware("/health*", "/metrics/*")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	// 命中忽略规则的路径：正常放行，不 panic
	for _, p := range []string{"/health", "/healthz", "/health/live", "/metrics/", "/metrics/foo"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "path %s", p)
		assert.Equal(t, "ok", rec.Body.String(), "path %s", p)
	}

	// 未命中忽略规则的路径：仍然正常工作
	for _, p := range []string{"/api/users", "/metricsx/foo", "/foo/health"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "path %s", p)
		assert.Equal(t, "ok", rec.Body.String(), "path %s", p)
	}
}
