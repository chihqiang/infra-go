package handler

import (
	"net/http"
	"runtime/debug"

	"github.com/chihqiang/infra-go/core/logger"
)

// RecoveryConfig configures RecoveryHandler.
type RecoveryConfig struct {
	Enabled bool `json:",default=true"`
}

// RecoveryHandler recovers panics from the next handler and writes HTTP 500.
type RecoveryHandler struct {
	config RecoveryConfig
}

// NewRecoveryHandler creates a RecoveryHandler.
func NewRecoveryHandler(config RecoveryConfig) *RecoveryHandler {
	return &RecoveryHandler{config: config}
}

// Handle wraps next with panic recovery.
func (h *RecoveryHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	if !h.config.Enabled {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				logger.ErrorCtx(r.Context(), "rest: recovered panic",
					logger.Any("panic", recovered),
					logger.String("stack", string(debug.Stack())),
				)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next(w, r)
	}
}
