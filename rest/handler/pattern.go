package handler

import "context"

type patternContextKey struct{}

// ContextWithPattern stores the matched route template in ctx. Global
// middleware runs before http.ServeMux dispatches, so r.Pattern is still empty
// there; the server pre-matches the route and injects it instead.
func ContextWithPattern(ctx context.Context, pattern string) context.Context {
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
