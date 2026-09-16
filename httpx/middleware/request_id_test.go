package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers request_id.go: the request_id context helpers and the RequestID middleware.

func TestRequestIDContext_RoundTrip(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "req-123")
	assert.Equal(t, "req-123", RequestIDFromContext(ctx))
	assert.Equal(t, "", RequestIDFromContext(context.Background()))
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
	assert.Equal(t, "client-provided-id", got) // the injected id matches the header written back
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
	require.NotEmpty(t, header) // generated automatically when no header is provided
	assert.NotEmpty(t, got)
	assert.Equal(t, header, got)
	assert.Equal(t, "X-Request-Id", HeaderRequestID)
}
