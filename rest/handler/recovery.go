package handler

import (
	"net/http"
	"runtime/debug"

	"github.com/chihqiang/ten-go/core/logger"
	"github.com/chihqiang/ten-go/rest/internal/response"
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
//
// The writer is tracked so a panic that happens after the response was
// committed can abort the connection instead of writing a 500 that cannot be
// sent. Without the abort the client would see a cleanly terminated but
// silently truncated "successful" response.
func (h *RecoveryHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	if !h.config.Enabled {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		tracked := response.NewStatusWriter(w)
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				logger.ErrorCtx(r.Context(), "rest: recovered panic",
					logger.Any("panic", recovered),
					logger.String("stack", string(debug.Stack())),
				)
				if tracked.Committed() {
					panic(http.ErrAbortHandler)
				}
				tracked.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next(tracked, r)
	}
}
