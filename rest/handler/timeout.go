// Package handler provides struct-based HTTP middleware handlers.
package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/rest/internal/response"
)

// TimeoutConfig configures TimeoutHandler.
//
// The handler buffers the response so it can discard it when the deadline is
// hit. That makes streaming impossible behind it: Flush returns
// http.ErrHandlerTimeout and Hijack returns http.ErrNotSupported, so leave the
// middleware disabled for SSE, long polling and WebSocket endpoints.
type TimeoutConfig struct {
	Enabled bool          `json:",optional"`
	Timeout time.Duration `json:",optional"`
}

// TimeoutHandler bounds handler execution.
type TimeoutHandler struct {
	config TimeoutConfig
}

// NewTimeoutHandler creates a TimeoutHandler.
func NewTimeoutHandler(config TimeoutConfig) *TimeoutHandler {
	return &TimeoutHandler{config: config}
}

// Handle wraps next with the timeout.
func (h *TimeoutHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	if !h.config.Enabled || h.config.Timeout <= 0 {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), h.config.Timeout)
		defer cancel()

		buffered := response.NewBufferedWriter()
		done := make(chan struct{})
		panicked := make(chan any, 1)
		go func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					panicked <- recovered
					return
				}
				close(done)
			}()
			next(buffered, r.WithContext(ctx))
		}()

		deadline, hasDeadline := ctx.Deadline()
		writtenBeforeDeadline := func() bool {
			writtenAt, ok := buffered.WrittenAt()
			return ok && (!hasDeadline || writtenAt.Before(deadline))
		}

		select {
		case recovered := <-panicked:
			// Re-panic on this goroutine so the outer RecoveryHandler can see it.
			panic(recovered)
		case <-done:
			// A handler that returns without writing anything *because* the deadline
			// fired must still produce the timeout response; a handler that wrote
			// before the deadline keeps its response.
			if writtenBeforeDeadline() || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				if err := buffered.WriteTo(w); err != nil {
					return
				}
				return
			}
			writeTimeout(w)
		case <-ctx.Done():
			// The handler and the deadline can become ready at the same time. Keep a
			// response that was written before the deadline; otherwise answer 503.
			select {
			case <-done:
				if writtenBeforeDeadline() {
					if err := buffered.WriteTo(w); err != nil {
						return
					}
					return
				}
			default:
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				writeTimeout(w)
			}
		}
	}
}

// writeTimeout writes the 503 response used when the deadline is exceeded.
func writeTimeout(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, "Request Timeout")
}
