package handler

import (
	"context"
	"net/http"

	"github.com/chihqiang/ten-go/core/logger"
	"github.com/chihqiang/ten-go/rest/internal/realip"
)

type patternContextKey struct{}

type requestIDContextKey struct{}

// ContextWithPattern stores the matched route template in ctx. Global
// middleware runs before http.ServeMux dispatches, so r.Pattern is still empty
// there; the server pre-matches the route and injects it instead. A nil ctx is
// replaced by context.Background().
func ContextWithPattern(ctx context.Context, pattern string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if pattern == "" {
		return ctx
	}
	return context.WithValue(ctx, patternContextKey{}, pattern)
}

// PatternFromContext returns the route template stored by ContextWithPattern,
// such as "GET /users/{id}", or an empty string when unset.
func PatternFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	pattern, _ := ctx.Value(patternContextKey{}).(string)
	return pattern
}

// ContextWithRequestID returns a context carrying requestID in the same slot
// used by RequestIDHandler, so RequestIDFromContext and response helpers can
// read it. An empty requestID leaves ctx unchanged.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

// RequestIDFromContext returns the request ID stored by RequestIDHandler or
// ContextWithRequestID.
func RequestIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	id, ok := ctx.Value(requestIDContextKey{}).(string)
	return id, ok
}

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
