// Package middleware provides reusable HTTP middleware.
package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/rest/chain"
	"github.com/chihqiang/infra-go/rest/internal/response"
)

// Timeout bounds handler execution. Timing out before any response is sent
// produces a JSON 503; slower handlers keep running until they notice the
// canceled context.
//
// A panic in the wrapped handler is recovered on the worker goroutine and
// re-panicked on the caller's goroutine, so middleware outside the timeout
// (notably RecoverHandler) still handles it instead of the process crashing.
func TimeoutHandler(timeout time.Duration) chain.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		if timeout <= 0 {
			return next
		}

		return func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			buffered := response.NewBufferedWriter()
			done := make(chan struct{})
			panicked := make(chan any, 1)
			go func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						// Buffered so a panic arriving after the timeout response
						// cannot block the goroutine forever.
						panicked <- recovered
						return
					}
					close(done)
				}()
				next(buffered, r.WithContext(ctx))
			}()

			select {
			case recovered := <-panicked:
				// Re-panic on this goroutine: recover() only works on the
				// panicking goroutine, so the outer RecoverHandler can only see
				// the panic if it is raised here.
				panic(recovered)
			case <-done:
				if err := buffered.WriteTo(w); err != nil {
					return
				}
			case <-ctx.Done():
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					writeError(w, r, http.StatusServiceUnavailable, "request timed out")
				}
			}
		}
	}
}
