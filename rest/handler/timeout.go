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
			go func() {
				defer close(done)
				next(buffered, r.WithContext(ctx))
			}()

			select {
			case <-done:
				if err := buffered.WriteTo(w); err != nil {
					return
				}
			case <-ctx.Done():
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					writeStatusJSON(w, http.StatusServiceUnavailable, "request timed out")
				}
			}
		}
	}
}
