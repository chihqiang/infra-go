package handler

import (
	"net/http"

	"github.com/chihqiang/ten-go/core/logger"
	"github.com/chihqiang/ten-go/rest/internal/realip"
)

// clientIP resolves the client address used in handler logs, preferring
// forwarding headers and falling back to the direct peer.
func clientIP(r *http.Request) string {
	return realip.FromRequest(r)
}

// logReject records a request rejected by a middleware. The method, path,
// matched route pattern and resolved client IP are attached to every entry;
// request_id and trace_id come from the logger context extractors.
func logReject(r *http.Request, msg string, fields ...logger.Field) {
	fields = append([]logger.Field{
		logger.String("method", r.Method),
		logger.String("path", r.URL.Path),
		logger.String("ip", clientIP(r)),
	}, fields...)
	if pattern := PatternFromContext(r.Context()); pattern != "" {
		fields = append(fields, logger.String("pattern", pattern))
	}

	logger.ErrorCtx(r.Context(), msg, fields...)
}
