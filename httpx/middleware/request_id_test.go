package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 对应 request_id.go：request_id context 工具与 RequestID 中间件。

func TestRequestIDContext_RoundTrip(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "req-123")
	assert.Equal(t, "req-123", RequestIDFromContext(ctx))
	assert.Equal(t, "", RequestIDFromContext(context.Background()))
	assert.Equal(t, "", RequestIDFromContext(nil))
}

func TestRequestID_FromHeader(t *testing.T) {
	var got string
	next := func(w http.ResponseWriter, r *http.Request) {
		got = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequestID, "client-provided-id")

	rec := perform(NewRequestID().Middleware(), next, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "client-provided-id", rec.Header().Get(HeaderRequestID))
	assert.Equal(t, "client-provided-id", got) // 注入的 id 与回写头一致
}

func TestRequestID_Generate(t *testing.T) {
	var got string
	next := func(w http.ResponseWriter, r *http.Request) {
		got = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	rec := perform(NewRequestID().Middleware(), next, req)

	header := rec.Header().Get(HeaderRequestID)
	require.NotEmpty(t, header) // 未带请求头时自动生成
	assert.NotEmpty(t, got)
	assert.Equal(t, header, got)
	assert.Equal(t, "X-Request-Id", HeaderRequestID)
}
