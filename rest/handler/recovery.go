package handler

import (
	"net/http"
	"runtime/debug"

	"github.com/chihqiang/infra-go/core/logger"
)

// RecoveryHandler recovers panics from the next handler and returns HTTP 500.
func RecoverHandler(next http.HandlerFunc) http.HandlerFunc {
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
				writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
			}
		}()
		next(w, r)
	}
}
