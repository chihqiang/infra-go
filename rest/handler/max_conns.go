package handler

import (
	"net/http"

	"github.com/chihqiang/infra-go/core/ratelimit"
)

// MaxConnsConfig configures MaxConnsHandler.
type MaxConnsConfig struct {
	Enabled bool `json:",optional"`
	Limit   int  `json:",optional"`
}

// MaxConnsHandler caps in-flight requests and rejects excess traffic with 503.
type MaxConnsHandler struct {
	config MaxConnsConfig
	guard  *ratelimit.Concurrency
}

// NewMaxConnsHandler creates a MaxConnsHandler.
func NewMaxConnsHandler(config MaxConnsConfig) *MaxConnsHandler {
	var guard *ratelimit.Concurrency
	if config.Enabled && config.Limit > 0 {
		guard = ratelimit.NewConcurrency(config.Limit)
	}
	return &MaxConnsHandler{config: config, guard: guard}
}

// Handle wraps next with the concurrency limit.
func (h *MaxConnsHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	if !h.config.Enabled || h.guard == nil {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if !h.guard.Allow() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		defer h.guard.Release()

		next(w, r)
	}
}
