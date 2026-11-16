package handler

import (
	"net/http"
	"strconv"

	"github.com/chihqiang/infra-go/core/breaker"
	"github.com/chihqiang/infra-go/rest/internal/response"
)

// BreakerConfig configures BreakerHandler.
type BreakerConfig struct {
	Enabled bool `json:",optional"`
	// Breaker is required when Enabled is true.
	Breaker breaker.Breaker `json:"-"`
}

// BreakerHandler routes requests through a circuit breaker.
type BreakerHandler struct {
	config BreakerConfig
}

// NewBreakerHandler creates a BreakerHandler.
func NewBreakerHandler(config BreakerConfig) *BreakerHandler {
	return &BreakerHandler{config: config}
}

// Handle wraps next with the circuit breaker. Responses with a status of 500 or
// above are reported as failures.
func (h *BreakerHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	if !h.config.Enabled || h.config.Breaker == nil {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		promise, err := h.config.Breaker.AllowCtx(r.Context())
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
