package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/breaker"
	"github.com/chihqiang/infra-go/httpx/respw"
	"github.com/chihqiang/infra-go/httpx/x"
	"github.com/chihqiang/infra-go/logger"
)

// Breaker is a circuit breaker middleware that protects downstream handlers from cascading
// failure.
// It is based on the Google SRE algorithm from the breaker module; the constructor creates a
// single global breaker named "http" that is shared by all requests (use RouteBreaker if you
// need per-route isolation).
type Breaker struct {
	brk breaker.Breaker
	// retryAfter is the retry interval advertised to clients when the breaker is open;
	// it defaults to breakerRetryAfter.
	retryAfter time.Duration
}

// breakerRetryAfter is the default retry hint interval used when the breaker is open.
//
// It is aligned with the half-open probe period of the breaker algorithm (the breaker's
// internal forcePassDuration is 1 second): once the breaker opens, at most this long is awaited
// before a single probe request is let through, so this is the earliest point at which a client
// could succeed (RFC 9110 §15.6.4 suggests sending this hint with a 503).
const breakerRetryAfter = time.Second

// NewBreaker creates the breaker middleware.
func NewBreaker() *Breaker {
	return &Breaker{
		brk:        breaker.NewBreaker(breaker.WithName("http")),
		retryAfter: breakerRetryAfter,
	}
}

// WithRetryAfter sets the Retry-After hint interval used when the breaker is open
// (RFC 9110 §10.2.3). d <= 0 means the header is not sent.
func (b *Breaker) WithRetryAfter(d time.Duration) *Breaker {
	b.retryAfter = d
	return b
}

// Middleware returns the breaker middleware in the standard func(http.Handler) http.Handler form.
// When the breaker is open it returns 503 Service Unavailable (with Retry-After); successful
// requests (<500) report Accept while failed ones (>=500) report Reject, which drives the
// breaker state.
func (b *Breaker) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			promise, err := b.brk.AllowCtx(r.Context())
			if err != nil {
				logger.WarnCtx(r.Context(), "http request dropped by breaker",
					logger.String("path", r.URL.Path),
					logger.String("remote", x.ClientIP(r)),
					logger.Err(err),
				)
				// RFC 9110 §15.6.4: a server experiencing overload/failure SHOULD send Retry-After
				WriteRetryAfter(r.Context(), w, http.StatusServiceUnavailable,
					b.retryAfter, "service unavailable")
				return
			}

			rec := respw.NewRecorderWriter(w)
			next.ServeHTTP(rec, r)
			if rec.Status() < http.StatusInternalServerError {
				promise.Accept()
			} else {
				promise.Reject(fmt.Sprintf("%d %s", rec.Status(), http.StatusText(rec.Status())))
			}
		})
	}
}
