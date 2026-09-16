package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// HeaderRequestID is the HTTP header name used for the request ID.
const HeaderRequestID = "X-Request-Id"

// requestIDKey is the context key type for request_id (a private type to avoid
// clashing with other libraries).
type requestIDKey struct{}

// ContextWithRequestID injects request_id into the context.
// httpx.ContextWithRequestID in the httpx main package delegates to this
// implementation, ensuring the request_id in the response and the ID injected by
// the RequestID middleware are read from the same key.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFromContext extracts request_id from the context, returning an empty
// string when absent.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// RequestID is the request_id middleware.
type RequestID struct{}

// NewRequestID creates the RequestID middleware.
func NewRequestID() *RequestID {
	return &RequestID{}
}

// Middleware returns the request_id middleware in the standard form
// func(http.Handler) http.Handler.
// It reads the X-Request-Id request header and generates one (google/uuid) when
// absent, injects it into the context and writes it back to the X-Request-Id
// response header.
func (m *RequestID) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(HeaderRequestID)
			if id == "" {
				id = uuid.NewString()
			}
			w.Header().Set(HeaderRequestID, id)
			next.ServeHTTP(w, r.WithContext(ContextWithRequestID(r.Context(), id)))
		})
	}
}
