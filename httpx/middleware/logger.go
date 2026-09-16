package middleware

import (
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/httpx/respw"
	"github.com/chihqiang/infra-go/httpx/x"
	"github.com/chihqiang/infra-go/logger"
)

// AccessLogger is a request access log middleware.
// It logs the method, path, status code, response byte count and latency of every request.
type AccessLogger struct {
	matcher *x.PathMatcher
}

// NewAccessLogger creates the access log middleware.
// skipPaths lists the paths that are not logged, commonly used for high-frequency liveness
// endpoints such as health checks and heartbeats.
// Matching is either exact (e.g. "/healthz") or a prefix wildcard ending in "*" (e.g.
// "/internal/*").
func NewAccessLogger(skipPaths ...string) *AccessLogger {
	return &AccessLogger{matcher: x.NewPathMatcher(skipPaths)}
}

// Middleware returns the access log middleware in the standard func(http.Handler) http.Handler
// form. When used together with the trace package, the logger's Ctx extractor automatically adds
// trace_id/span_id.
func (l *AccessLogger) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := respw.NewRecorderWriter(w)
			next.ServeHTTP(rec, r)
			// Paths matching an ignore rule are not written to the access log (the business logic
			// still runs as usual)
			if l.matcher.Match(r.URL.Path) {
				return
			}
			logger.InfoCtx(r.Context(), "http request",
				logger.String("method", r.Method),
				logger.String("path", r.URL.Path),
				logger.String("query", r.URL.RawQuery),
				logger.String("remote", x.ClientIP(r)),
				logger.Int("status", rec.Status()),
				logger.Int("bytes", rec.Bytes()),
				logger.Duration("latency", time.Since(start)),
			)
		})
	}
}
