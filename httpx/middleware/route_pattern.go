package middleware

import "context"

// routePatternCtxKey is the context key for the route template.
// A dedicated unexported type is used to avoid clashing with keys of other
// packages.
type routePatternCtxKey struct{}

// PatternFromContext returns the route template the request matched (such as
// "GET /users/{id}").
//
// Why it is needed: global middleware wraps the ServeMux **outside** it, and at
// that point net/http has not yet written the matched template into r.Pattern
// (the ServeMux does that only when dispatching to the matched handler), so
// reading r.Pattern from global middleware is always empty.
// httpx.Server pre-matches the route before entering the middleware chain and
// puts the template into the context, so middleware that aggregates per route
// (such as the circuit breaker or metrics) gets a stable template instead of a
// concrete path.
//
// It returns "" when unset.
func PatternFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	pattern, _ := ctx.Value(routePatternCtxKey{}).(string)
	return pattern
}

// ContextWithPattern writes the route template into the context.
// It is mainly used internally by httpx.Server; other frameworks may call it
// themselves in order to reuse middleware that depends on the route template.
func ContextWithPattern(ctx context.Context, pattern string) context.Context {
	if pattern == "" {
		return ctx
	}
	return context.WithValue(ctx, routePatternCtxKey{}, pattern)
}
