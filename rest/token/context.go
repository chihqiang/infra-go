package token

import "context"

type claimsContextKey struct{}

// WithClaims stores verified claims in ctx.
func WithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

// ClaimsFrom returns the claims stored in ctx by Middleware.
func ClaimsFrom(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey{}).(*Claims)
	return claims, ok
}
