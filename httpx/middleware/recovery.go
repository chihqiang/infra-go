package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/chihqiang/infra-go/httpx/x"
	"github.com/chihqiang/infra-go/logger"
)

// Recovery is the panic recovery middleware.
// It catches panics raised in handlers, logs the stack and returns 500 so the
// process does not crash.
type Recovery struct{}

// NewRecovery creates the Recovery middleware.
func NewRecovery() *Recovery {
	return &Recovery{}
}

// Middleware returns the panic recovery middleware in the standard form
// func(http.Handler) http.Handler.
func (r *Recovery) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorCtx(req.Context(), "panic recovered",
						logger.Any("panic", rec),
						logger.String("method", req.Method),
						logger.String("path", req.URL.Path),
						logger.String("remote", x.ClientIP(req)),
						logger.String("stack", string(debug.Stack())),
					)
					// Render the error with the request context, consistent with the
					// other middleware (timeout, rate limit, size limit), so the body
					// carries the request_id — a 500 is exactly when that correlation
					// ID matters most.
					// Reading a value from the context is unaffected by cancellation
					// (ctx.Value does not consult Done), so the request_id is still
					// available even when the request was already cancelled (e.g. the
					// client disconnected early).
					writeError(req.Context(), w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, req)
		})
	}
}
