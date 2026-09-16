package middleware

import (
	"net/http"

	"github.com/chihqiang/infra-go/logger"
)

// MaxBytes is a request body size limiting middleware.
// When the request body's Content-Length exceeds n bytes it immediately returns 413 Request Entity
// Too Large; for chunked transfers (no Content-Length) it limits reads with http.MaxBytesReader.
type MaxBytes struct {
	n int64
}

// NewMaxBytes creates the request body size limiting middleware.
// n <= 0 means unlimited.
func NewMaxBytes(n int64) *MaxBytes {
	return &MaxBytes{n: n}
}

// Middleware returns the request body size limiting middleware in the standard
// func(http.Handler) http.Handler form.
func (m *MaxBytes) Middleware() func(http.Handler) http.Handler {
	// n <= 0: unlimited, pass straight through
	if m.n <= 0 {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > m.n {
				logger.WarnCtx(r.Context(), "request body too large",
					logger.Int64("limit", m.n),
					logger.Int64("content_length", r.ContentLength),
					logger.String("path", r.URL.Path),
				)
				writeError(r.Context(), w, http.StatusRequestEntityTooLarge, "request entity too large")
				return
			}

			// Limit the maximum number of bytes read (covers the chunked transfer case)
			r.Body = http.MaxBytesReader(w, r.Body, m.n)
			next.ServeHTTP(w, r)
		})
	}
}
