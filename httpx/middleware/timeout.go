package middleware

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/httpx/respw"
)

// statusClientClosedRequest is used when the client closes the request early
// (non-standard status code 499, an nginx convention).
const statusClientClosedRequest = 499

// Timeout is a request timeout middleware.
// Each request may run for at most duration; on timeout it responds with 503 Service
// Unavailable. A client disconnect returns 499; WebSocket / SSE requests are exempt.
type Timeout struct {
	duration time.Duration
}

// NewTimeout creates the request timeout middleware.
// When duration <= 0 the middleware is disabled (requests pass straight through).
func NewTimeout(duration time.Duration) *Timeout {
	return &Timeout{duration: duration}
}

// Middleware returns the request timeout middleware in the standard
// func(http.Handler) http.Handler form.
func (t *Timeout) Middleware() func(http.Handler) http.Handler {
	// duration <= 0: disabled, pass straight through
	if t.duration <= 0 {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// WebSocket upgrades and long-lived SSE connections are not subject to the timeout
			if r.Header.Get("Upgrade") == "websocket" ||
				r.Header.Get("Accept") == "text/event-stream" {
				next.ServeHTTP(w, r)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), t.duration)
			defer cancel()
			r = r.WithContext(ctx)

			done := make(chan struct{})
			tw := respw.NewTimeoutWriter(w)
			panicChan := make(chan any, 1)
			go func() {
				defer func() {
					if p := recover(); p != nil {
						panicChan <- p
					}
				}()
				next.ServeHTTP(tw, r)
				close(done)
			}()

			select {
			case p := <-panicChan:
				panic(p)
			case <-done:
				// completed normally: flush the buffered header/status/body to the underlying
				// ResponseWriter
				tw.Done()
			case <-ctx.Done():
				if errors.Is(ctx.Err(), context.Canceled) {
					w.WriteHeader(statusClientClosedRequest)
				} else {
					writeError(r.Context(), w, http.StatusServiceUnavailable, "request timeout")
				}
				tw.Timeout()
			}
		})
	}
}
