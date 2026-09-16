package middleware

import (
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/logger"
)

// maxConnsRetryAfter is the default retry hint interval used when the concurrency limit is
// exceeded.
//
// Concurrency saturation is usually a short-lived pile-up, hence the short hint value;
// RFC 9110 §15.6.4 suggests attaching Retry-After to a 503 response.
const maxConnsRetryAfter = time.Second

// MaxConns is a concurrency limiting middleware.
// It is a lightweight semaphore built on a buffered channel, used purely for counting (no Wait
// semantics are needed); when concurrency exceeds the limit it immediately returns 503 Service
// Unavailable to prevent connection exhaustion.
type MaxConns struct {
	sem chan struct{}
	// retryAfter is the retry interval advertised to clients when the limit is exceeded; it
	// defaults to maxConnsRetryAfter.
	retryAfter time.Duration
}

// NewMaxConns creates the concurrency limiting middleware.
// n <= 0 means unlimited.
func NewMaxConns(n int) *MaxConns {
	if n <= 0 {
		return &MaxConns{}
	}
	return &MaxConns{sem: make(chan struct{}, n), retryAfter: maxConnsRetryAfter}
}

// WithRetryAfter sets the Retry-After hint interval used when the concurrency limit is exceeded
// (RFC 9110 §10.2.3). d <= 0 means the header is not sent.
func (m *MaxConns) WithRetryAfter(d time.Duration) *MaxConns {
	m.retryAfter = d
	return m
}

// Middleware returns the concurrency limiting middleware in the standard func(http.Handler)
// http.Handler form.
func (m *MaxConns) Middleware() func(http.Handler) http.Handler {
	// n <= 0: unlimited, pass straight through
	if m.sem == nil {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case m.sem <- struct{}{}:
				defer func() { <-m.sem }()
				next.ServeHTTP(w, r)
			default:
				logger.WarnCtx(r.Context(), "too many concurrent connections",
					logger.Int("limit", cap(m.sem)),
					logger.String("path", r.URL.Path),
				)
				// RFC 9110 §15.6.4: a 503 caused by server overload SHOULD provide Retry-After
				WriteRetryAfter(r.Context(), w, http.StatusServiceUnavailable,
					m.retryAfter, "too many concurrent connections")
			}
		})
	}
}
