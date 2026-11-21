package handler

import (
	"net/http"
	"strconv"

	"github.com/chihqiang/ten-go/core/breaker"
	"github.com/chihqiang/ten-go/rest/internal/response"
)

// BreakerHandler routes requests through a circuit breaker.
type BreakerHandler struct {
	breaker breaker.Breaker
}

// NewBreakerHandler creates a BreakerHandler.
func NewBreakerHandler(br breaker.Breaker) *BreakerHandler {
	return &BreakerHandler{breaker: br}
}

// Handle wraps next with the circuit breaker. Responses with a status of 500 or
// above are reported as failures.
func (h *BreakerHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		promise, err := h.breaker.AllowCtx(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		recorder := response.NewStatusWriter(w)
		completed := false
		// A panicking handler must still resolve the breaker promise,
		// otherwise the request is counted but never classified.
		defer func() {
			if !completed {
				promise.Reject("handler panic")
			}
		}()

		next(recorder, r)
		completed = true

		status := recorder.Status()
		if status >= http.StatusInternalServerError {
			promise.Reject("http status " + strconv.Itoa(status))
			return
		}
		promise.Accept()
	}
}
